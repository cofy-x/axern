import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("security_check", Path(__file__).with_name("security-check.py"))
security = importlib.util.module_from_spec(spec)
spec.loader.exec_module(security)


class ScannerEvidenceTests(unittest.TestCase):
    def report(self):
        return json.dumps({"config": {
            "scanner_name": "govulncheck", "scanner_version": "v1.9.0",
            "scan_level": "symbol", "scan_mode": "source", "go_version": "go1.26.10",
        }}) + "\n" + json.dumps({"SBOM": {
            "go_version": "go1.26.10", "modules": [{"path": "example/module"}],
            "roots": ["example/module/package"],
        }})

    def test_zero_exit_go_finding_still_fails(self):
        report = self.report() + "\n" + json.dumps({"finding": {"osv": "GO-example", "trace": [{"module": "dependency"}]}})
        with self.assertRaisesRegex(ValueError, "GO-example"):
            security.check_go(report, 0, "example/module")

    def test_empty_or_malformed_go_evidence_fails(self):
        for raw in ("", "{}", "[]", '{"config":'):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                security.check_go(raw, 0, "example/module")

    def test_completed_zero_finding_go_scan(self):
        security.check_go(self.report(), 0, "example/module")

    def test_config_only_wrong_module_or_wrong_scanner_fails(self):
        for report, module in (
            (self.report().splitlines()[0], "example/module"),
            (self.report(), "omitted/module"),
            (self.report().replace("v1.9.0", "v1.8.0"), "example/module"),
            (self.report().replace("example/module/package", "unrelated/package"), "example/module"),
        ):
            with self.subTest(module=module), self.assertRaises(ValueError):
                security.check_go(report, 0, module)

    def test_npm_unavailable_is_not_zero_vulnerabilities(self):
        for report in ({}, {"error": {"message": "service unavailable"}}, {"metadata": {"vulnerabilities": {"high": 1}}}):
            with self.assertRaises(ValueError):
                security.check_npm(json.dumps(report), 0)

    def test_muted_npm_advisory_is_not_a_clean_scan(self):
        with self.assertRaises(ValueError):
            security.check_npm(json.dumps({"metadata": {"vulnerabilities": {"high": 0}}, "muted": [{"id": "ignored-advisory"}]}), 0)

    def test_python_skipped_dependency_or_finding_fails(self):
        for dependency in ({"name": "unresolved", "skip_reason": "not found"}, {"name": "vulnerable", "vulns": [{"id": "example"}]}):
            with self.assertRaises(ValueError):
                security.check_python(json.dumps({"dependencies": [dependency]}), 0)

    def test_python_and_npm_success_need_successful_exit(self):
        npm = json.dumps({"metadata": {"vulnerabilities": {"low": 0, "moderate": 0, "high": 0, "critical": 0}}})
        python = json.dumps({"dependencies": [{"name": "audited", "vulns": []}]})
        security.check_npm(npm, 0)
        security.check_python(python, 0)
        for check, report in ((security.check_npm, npm), (security.check_python, python)):
            with self.assertRaises(ValueError):
                check(report, 1)

    def test_complete_graph_rejects_missing_pages_and_latent_findings(self):
        for report in ({}, {"results": []}, {"results": [{"next_page_token": "remaining"}]}, {"results": [{"vulns": [{"id": "test-only-vulnerability"}]}]}):
            with self.assertRaises(ValueError):
                security.check_module_graph(report, 1)
        security.check_module_graph({"results": [{}, {}]}, 2)

    def test_nonshipping_classification_requires_exact_identity_proof_and_advisory(self):
        query = {"package": {"name": security.TOOL_MODULE[0]}, "version": security.TOOL_MODULE[1]}
        report = {"results": [{"vulns": [{"id": "GO-2026-6778"}]}]}
        classified = security.check_module_graph(report, 1, [query], True)
        self.assertEqual(classified[0]["advisories"], ["GO-2026-6778"])
        for changed_report, changed_query, proof in (
            (report, query, False),
            ({"results": [{"vulns": [{"id": "new-advisory"}]}]}, query, True),
            (report, dict(query, version="v2.8.4"), True),
            (report, dict(query, package={"name": "another/module"}), True),
        ):
            with self.assertRaises(ValueError):
                security.check_module_graph(changed_report, 1, [changed_query], proof)

    def test_new_owner_or_replaced_owner_invalidates_nonshipping_proof(self):
        modules = [{"Path": "github.com/cilium/ebpf", "Version": "v0.22.0"}]
        edge = security.TOOL_OWNER + " " + "@".join(security.TOOL_MODULE)
        security.check_tool_owner(modules, edge)
        for changed_modules, changed_edges in (
            (modules, edge + "\nnew/owner " + "@".join(security.TOOL_MODULE)),
            ([dict(modules[0], Version="v0.23.0")], edge),
            ([dict(modules[0], Replace={"Path": "local"})], edge),
        ):
            with self.assertRaises(ValueError):
                security.check_tool_owner(changed_modules, changed_edges)

    def test_loaded_or_unaudited_upstream_tool_is_never_nonshipping(self):
        security.check_package_closure("example/module\n", 0)
        for packages, code in (("", 0), ("example/module", 1),
                               (security.TOOL_MODULE[0] + "/registry/client", 0),
                               ("github.com/google/go-containerregistry/cmd/crane", 0)):
            with self.assertRaises(ValueError):
                security.check_package_closure(packages, code)

    def test_import_proof_includes_cgo_and_static_shipping_targets(self):
        targets = security.closure_targets(["./runtime/axnoded", "./apps/cli", "./sdk/go"])
        for target in (("./runtime/axnoded", "linux", "arm64", "1"),
                       ("./runtime/axnoded", "linux", "amd64", "0"),
                       ("./apps/cli", "darwin", "arm64", "1"),
                       ("./sdk/go", "darwin", "amd64", "0")):
            self.assertIn(target, targets)

    def test_absolute_member_cannot_drop_macos_import_proof(self):
        members = security.normalize_members([str(security.ROOT / "apps/cli"), str(security.ROOT / "sdk/go")])
        self.assertIn(("apps/cli", "darwin", "arm64", "1"), security.closure_targets(members))
        with self.assertRaises(ValueError):
            security.normalize_members(["apps/cli", str(security.ROOT / "apps/cli")])
        with self.assertRaises(ValueError):
            security.normalize_members([str(security.ROOT.parent)])

    def test_agent_base_installer_is_audited_outside_uv_graph(self):
        requirements = security.python_requirements("grpcio==1.81.0\n", {"artifacts": {"pip": {"version": "26.3"}}})
        self.assertIn("pip==26.3\n", requirements)
        self.assertIn("grpcio==1.81.0\n", requirements)
        for lock in ({}, {"artifacts": {"pip": {"version": "26.3\nmalicious==1"}}}):
            with self.assertRaises(ValueError):
                security.python_requirements("grpcio==1.81.0", lock)

    def test_auxiliary_graph_requires_root_and_locked_unreplaced_dependencies(self):
        root = {"Path": "example/upstream", "Main": True}
        dependency = {"Path": "example/dependency", "Version": "v1.2.3"}

        def stream(modules):
            return "\n".join(json.dumps(module) for module in modules)

        self.assertEqual(security.auxiliary_graph(stream([root, dependency]), 0, root["Path"]),
                         {("example/dependency", "v1.2.3")})
        for modules, code in (([], 0), ([dependency], 0), ([root], 1),
                              ([root, dict(root)], 0), ([dict(root, Path="wrong/root")], 0),
                              ([root, dict(dependency, Version="")], 0),
                              ([root, dict(dependency, Replace={"Path": "local"})], 0),
                              ([dict(root, Replace={"Path": "local"}), dependency], 0),
                              ([root, dependency, dict(dependency)], 0),
                              ([root, dependency, dict(dependency, Version="v2.0.0")], 0),
                              ([root, {"Path": security.TOOL_MODULE[0], "Version": security.TOOL_MODULE[1]}], 0)):
            with self.subTest(modules=modules, code=code), self.assertRaises(ValueError):
                security.auxiliary_graph(stream(modules), code, root["Path"])

    def test_auxiliary_main_query_binds_the_pinned_owner_and_version(self):
        self.assertEqual(set(security.AUXILIARY_MODULES), {"postgres", "registry"})
        for name, identity in security.AUXILIARY_MODULES.items():
            key = "gosu" if name == "postgres" else "distribution"
            pins = {key: {"module": identity[0], "version": identity[1]}}
            self.assertEqual(security.auxiliary_identity(name, pins, [identity[0]]), identity)
            for changed, declarations in (({}, [identity[0]]), (pins, ["self-declared/owner"]),
                                           ({key: dict(pins[key], version="changed")}, [identity[0]])):
                with self.subTest(name=name), self.assertRaises(ValueError):
                    security.auxiliary_identity(name, changed, declarations)


if __name__ == "__main__":
    unittest.main()
