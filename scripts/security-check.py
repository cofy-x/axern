#!/usr/bin/env python3
"""Audit every locked source dependency; findings and unavailable scans fail."""
from __future__ import annotations

import argparse
import concurrent.futures
import json
import os
from pathlib import Path
import re
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[1]
GOVULNCHECK = "golang.org/x/vuln/cmd/govulncheck@v1.9.0"
PIP_AUDIT = "pip-audit==2.10.1"
TOOL_MODULE = ("github.com/docker/distribution", "v2.8.3+incompatible")
TOOL_OWNER = "github.com/cilium/ebpf@v0.22.0"
TOOL_FINDINGS = {"GHSA-685w-q87j-wqw3", "GO-2026-6778"}
AUXILIARY_MODULES = {
    "postgres": ("github.com/tianon/gosu", "1.20"),
    "registry": ("github.com/distribution/distribution/v3", "v3.1.2"),
}


def json_stream(raw: str) -> list[dict]:
    decoder, records = json.JSONDecoder(), []
    while raw.strip():
        record, end = decoder.raw_decode(raw.lstrip())
        if not isinstance(record, dict):
            raise ValueError("scanner record is not an object")
        records.append(record)
        raw = raw.lstrip()[end:]
    return records


def check_go(raw: str, returncode: int, expected_module: str) -> None:
    records = json_stream(raw)
    configs = [r["config"] for r in records if "config" in r]
    if returncode or len(configs) != 1 or any(configs[0].get(key) != value for key, value in {
        "scanner_name": "govulncheck", "scanner_version": "v1.9.0",
        "scan_level": "symbol", "scan_mode": "source",
    }.items()):
        raise ValueError("Go vulnerability scan did not complete")
    sboms = [r["SBOM"] for r in records if "SBOM" in r]
    if len(sboms) != 1:
        raise ValueError("Go scan has no complete module evidence")
    sbom = sboms[0]
    if sbom.get("go_version") != configs[0].get("go_version") or not sbom.get("go_version"):
        raise ValueError("Go scan toolchain evidence differs")
    if expected_module not in {entry.get("path") for entry in sbom.get("modules", [])}:
        raise ValueError("Go scan omitted its workspace module")
    if not any(root == expected_module or root.startswith(expected_module + "/") for root in sbom.get("roots", [])):
        raise ValueError("Go scan omitted its workspace packages")
    # JSON mode may exit zero while reporting vulnerabilities. Findings at
    # module/package level still require remediation, even without a callsite.
    findings = [r["finding"] for r in records if "finding" in r]
    if findings:
        raise ValueError("Go vulnerability findings: " + ", ".join(sorted({f["osv"] for f in findings})))


def check_npm(raw: str, returncode: int) -> None:
    report = json.loads(raw)
    counts = report.get("metadata", {}).get("vulnerabilities")
    if returncode or not isinstance(counts, dict) or not counts or any(counts.values()) or report.get("muted"):
        raise ValueError("npm audit failed or reported vulnerabilities")


def check_python(raw: str, returncode: int) -> None:
    report = json.loads(raw)
    dependencies = report.get("dependencies")
    if returncode or not isinstance(dependencies, list) or not dependencies:
        raise ValueError("Python vulnerability scan did not complete")
    for dependency in dependencies:
        if dependency.get("skip_reason") or "vulns" not in dependency:
            raise ValueError("Python dependency could not be audited: " + dependency.get("name", "unknown"))
        if dependency["vulns"]:
            raise ValueError("Python vulnerability findings: " + dependency["name"])


def python_requirements(exported: str, image_lock: dict) -> str:
    # pip is installed in the Agent base image outside uv's workspace graph.
    version = image_lock.get("artifacts", {}).get("pip", {}).get("version", "")
    if not re.fullmatch(r"[0-9]+(?:\.[0-9]+){1,2}", version):
        raise ValueError("Agent image installer has no stable audited version")
    return exported.rstrip() + "\n\n# Locked Agent base image installer\npip==" + version + "\n"


def check_module_graph(report: dict, expected_results: int, queries: list[dict] | None = None,
                       non_shipping_proven: bool = False) -> list[dict]:
    results = report.get("results")
    if not isinstance(results, list) or len(results) != expected_results:
        raise ValueError("incomplete Go module graph audit")
    classified = []
    for index, result in enumerate(results):
        if not isinstance(result, dict) or result.get("error") or result.get("next_page_token"):
            raise ValueError("Go module graph query did not complete")
        if result.get("vulns"):
            query = queries[index] if queries else {}
            identity = (query.get("package", {}).get("name"), query.get("version"))
            ids = {v["id"] for v in result["vulns"]}
            if non_shipping_proven and identity == TOOL_MODULE and ids and ids <= TOOL_FINDINGS:
                classified.append({"module": identity[0], "version": identity[1],
                                   "advisories": sorted(ids), "owner": TOOL_OWNER,
                                   "classification": "non-shipping upstream development tool"})
                continue
            raise ValueError("Go module graph vulnerability findings: " + ", ".join(v["id"] for v in result["vulns"]))
    return classified


def check_tool_owner(modules: list[dict], edges: str) -> None:
    owners = [m for m in modules if m.get("Path") == TOOL_OWNER.split("@")[0]]
    if len(owners) != 1 or owners[0].get("Version") != "v0.22.0" or owners[0].get("Replace"):
        raise ValueError("upstream development tool owner changed")
    incoming = {tuple(line.split()) for line in edges.splitlines()
                if line.split()[-1:] == ["@".join(TOOL_MODULE)]}
    if incoming != {(TOOL_OWNER, "@".join(TOOL_MODULE))}:
        raise ValueError("upstream development tool dependency provenance changed")


def check_package_closure(raw: str, returncode: int) -> None:
    packages = raw.splitlines()
    if returncode or not packages:
        raise ValueError("cross-platform package enumeration did not complete")
    if any(package == TOOL_MODULE[0] or package.startswith(TOOL_MODULE[0] + "/")
           or "/cmd/crane" in package for package in packages):
        raise ValueError("upstream development tool entered an Axern package closure")


def auxiliary_graph(raw: str, returncode: int, expected_module: str) -> set[tuple[str, str]]:
    if returncode:
        raise ValueError("auxiliary module graph failed")
    modules = json_stream(raw)
    roots = [module for module in modules if module.get("Main")]
    if len(roots) != 1 or roots[0].get("Path") != expected_module or roots[0].get("Replace"):
        raise ValueError("auxiliary module graph has no matching source root")
    dependencies = set()
    paths = {expected_module}
    for module in modules:
        if module is roots[0]:
            continue
        if module.get("Replace") or not module.get("Path") or not module.get("Version"):
            raise ValueError("unqualified auxiliary dependency")
        identity = (module["Path"], module["Version"])
        if module["Path"] in paths:
            raise ValueError("duplicate auxiliary dependency")
        # The one non-shipping classification belongs exclusively to the
        # separately proven Axern workspace graph. Never inherit it for an
        # auxiliary upstream build, including test or foreign-platform tools.
        if identity == TOOL_MODULE:
            raise ValueError("workspace tool classification cannot cover auxiliary dependencies")
        paths.add(module["Path"])
        dependencies.add(identity)
    return dependencies


def auxiliary_identity(name: str, pins: dict, declarations: list[str]) -> tuple[str, str]:
    expected_module, version = AUXILIARY_MODULES[name]
    upstream = pins.get("gosu" if name == "postgres" else "distribution", {})
    if declarations != [expected_module] or upstream.get("version") != version:
        raise ValueError("auxiliary source owner or upstream version differs from the audit scope")
    if name == "registry" and upstream.get("module") != expected_module:
        raise ValueError("auxiliary source module differs from its pins")
    return expected_module, version


def closure_targets(members: list[str]) -> list[tuple[str, str, str, str]]:
    return [(member, system, arch, cgo) for member in members
            for system in ("linux", "darwin") for arch in ("amd64", "arm64") for cgo in ("0", "1")
            if system == "linux" or Path(member).as_posix() in ("apps/cli", "sdk/go")]


def normalize_members(members: list[str]) -> list[str]:
    normalized = []
    for member in members:
        directory = (ROOT / member).resolve()
        if not directory.is_relative_to(ROOT) or not (directory / "go.mod").is_file():
            raise ValueError("Go workspace member is outside the source checkout or missing")
        normalized.append(directory.relative_to(ROOT).as_posix())
    if not normalized or len(set(normalized)) != len(normalized):
        raise ValueError("invalid Go workspace members")
    return normalized


def prove_non_shipping(go: str, members: list[str], modules: list[dict], output: Path) -> None:
    edges = subprocess.run([go, "mod", "graph"], cwd=ROOT, capture_output=True, text=True, timeout=120)
    (output / "go-tool-provenance.txt").write_text(edges.stdout)
    if edges.returncode:
        raise ValueError("upstream tool provenance enumeration failed")
    check_tool_owner(modules, edges.stdout)

    targets = closure_targets(members)

    def enumerate_packages(target: tuple[str, str, str, str]) -> dict:
        member, system, arch, cgo = target
        env = dict(os.environ, GOOS=system, GOARCH=arch, CGO_ENABLED=cgo)
        result = subprocess.run([go, "list", "-mod=readonly", "-deps", "-test", "-f", "{{.ImportPath}}", "./..."],
                                cwd=ROOT / member, env=env, capture_output=True, text=True, timeout=300)
        label = "go-tool-closure-" + member.replace("/", "-").strip(".-") + "-" + system + "-" + arch + "-cgo" + cgo
        (output / (label + ".txt")).write_text(result.stdout)
        (output / (label + ".stderr")).write_text(result.stderr)
        check_package_closure(result.stdout, result.returncode)
        return {"module": member, "goos": system, "goarch": arch, "cgo": cgo, "packages": len(result.stdout.splitlines())}

    with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
        evidence = list(executor.map(enumerate_packages, targets))
    paths = subprocess.run(["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
                           cwd=ROOT, capture_output=True, check=True, timeout=30).stdout.decode().split("\0")
    scanned = []
    for name in sorted(set(paths)):
        path = ROOT / name
        if (not name or not path.is_file() or path.suffix in {".md", ".sum", ".lock"}
                or name in {"scripts/security-check.py", "scripts/test_security_check.py", "pnpm-lock.yaml"}):
            continue
        try:
            source = path.read_text()
        except UnicodeError:
            continue
        if re.search(r"\bcrane\b|github\.com/docker/distribution", source):
            raise ValueError("own source references the upstream development tool: " + name)
        scanned.append(name)
    (output / "go-nonshipping-proof.json").write_text(json.dumps({"closures": evidence, "own_source_files": scanned}, indent=2))


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output-dir", type=Path, help="New directory for private scanner reports")
    args = parser.parse_args()
    output = args.output_dir or Path(tempfile.mkdtemp(prefix="axern-security-"))
    if args.output_dir:
        output.mkdir(parents=True, exist_ok=False, mode=0o700)
    output = output.resolve()
    go, pnpm, uv = (os.environ.get(name, fallback) for name, fallback in (("GO", "go"), ("PNPM", "pnpm"), ("UV", "uv")))

    def command(label: str, argv: list[str], cwd: Path = ROOT, timeout: int = 600,
                env: dict[str, str] | None = None) -> subprocess.CompletedProcess:
        result = subprocess.run(argv, cwd=cwd, env=env, capture_output=True, text=True, timeout=timeout)
        (output / (label + ".json")).write_text(result.stdout)
        (output / (label + ".stderr")).write_text(result.stderr)
        return result

    started = time.monotonic()
    summary = {"passed": False, "go_modules": [], "reports": str(output)}
    try:
        work = subprocess.run([go, "work", "edit", "-json"], cwd=ROOT, capture_output=True, text=True, check=True, timeout=30)
        members = normalize_members([entry["DiskPath"] for entry in json.loads(work.stdout)["Use"]])

        def audit_member(entry: str) -> str:
            directory = (ROOT / entry).resolve()
            if not directory.is_relative_to(ROOT) or not (directory / "go.mod").is_file():
                raise ValueError("Go workspace member is outside the source checkout")
            label = "go-" + str(directory.relative_to(ROOT)).replace("/", "-")
            result = command(label, [go, "run", GOVULNCHECK, "-json", "./..."], directory)
            declarations = [line.split()[1].strip('"') for line in (directory / "go.mod").read_text().splitlines() if line.split()[:1] == ["module"]]
            if len(declarations) != 1:
                raise ValueError("invalid Go module declaration")
            check_go(result.stdout, result.returncode, declarations[0])
            return str(directory.relative_to(ROOT))

        with concurrent.futures.ThreadPoolExecutor(max_workers=3) as executor:
            # Await every scan even if one fails, preserving complete reports.
            futures = [executor.submit(audit_member, member) for member in members]
            failures = []
            for future in futures:
                try:
                    summary["go_modules"].append(future.result())
                except Exception as error:
                    failures.append(str(error))
            if failures:
                raise ValueError("; ".join(failures))

        # Source scans do not load test-only and foreign-platform imports.
        # Query the complete resolved graph as well, including the toolchain.
        graph = command("go-module-graph", [go, "list", "-m", "-json", "all"])
        version = command("go-version", [go, "env", "GOVERSION"])
        if graph.returncode or version.returncode or not version.stdout.strip().startswith("go"):
            raise ValueError("Go module graph could not be resolved")
        own_modules = set()
        for member in members:
            declarations = [line.split()[1].strip('"') for line in (ROOT / member / "go.mod").read_text().splitlines() if line.split()[:1] == ["module"]]
            if len(declarations) != 1:
                raise ValueError("invalid Go workspace declaration")
            own_modules.add(declarations[0])
        packages = {("stdlib", version.stdout.strip().removeprefix("go"))}
        modules = json_stream(graph.stdout)
        for original in modules:
            module = original.get("Replace") or original
            if original["Path"] in own_modules and original.get("Main"):
                continue
            if not module.get("Version"):
                raise ValueError("unversioned dependency outside audited workspace: " + module["Path"])
            packages.add((module["Path"], module["Version"]))
        # Auxiliary service recipes build independently pinned upstream Go
        # programs outside go.work. Audit their exact prepared source and full
        # module graphs as well; workspace scans cannot establish this evidence.
        summary["auxiliary_go_modules"] = []
        host = command("auxiliary-go-host", [go, "env", "-json", "GOHOSTOS", "GOHOSTARCH"])
        if host.returncode:
            raise ValueError("auxiliary audit host could not be established")
        host_target = json.loads(host.stdout)
        if host_target.get("GOHOSTOS") not in ("linux", "darwin") or host_target.get("GOHOSTARCH") not in ("amd64", "arm64"):
            raise ValueError("unsupported auxiliary source audit host")
        auxiliary_env = dict(os.environ, GOWORK="off", GOTOOLCHAIN="local", GOENV="off", GOFLAGS="-mod=readonly",
                             CGO_ENABLED="0", GOOS=host_target["GOHOSTOS"], GOARCH=host_target["GOHOSTARCH"])
        summary["auxiliary_source_target"] = {"goos": auxiliary_env["GOOS"], "goarch": auxiliary_env["GOARCH"], "cgo": "0"}
        for name, (expected_module, upstream_version) in AUXILIARY_MODULES.items():
            prepared = output / (name + "-source")
            preparation = command("auxiliary-" + name + "-prepare", [
                "python3", str(ROOT / "deploy/images" / name / "build.py"),
                "--prepare-only", "--go", go, "--output", str(prepared),
            ], env=auxiliary_env)
            if preparation.returncode:
                raise ValueError("auxiliary source preparation failed: " + name)
            source = prepared / "source"
            declarations = [line.split()[1].strip('"') for line in (source / "go.mod").read_text().splitlines()
                            if line.split()[:1] == ["module"]]
            lock = "pins.json" if name == "postgres" else "artifacts.lock.json"
            auxiliary_identity(name, json.loads((ROOT / "deploy/images" / name / lock).read_text()), declarations)
            result = command("auxiliary-" + name + "-go", [go, "run", GOVULNCHECK, "-json", "./..."],
                             source, env=auxiliary_env)
            check_go(result.stdout, result.returncode, expected_module)
            graph = command("auxiliary-" + name + "-graph", [go, "list", "-m", "-json", "all"],
                            source, env=auxiliary_env)
            packages.update(auxiliary_graph(graph.stdout, graph.returncode, expected_module))
            packages.add((expected_module, upstream_version))
            summary["auxiliary_go_modules"].append({"image": name, "module": expected_module,
                                                    "upstream_version": upstream_version})

        queries = [{"package": {"ecosystem": "Go", "name": name}, "version": value} for name, value in sorted(packages)]
        if len(queries) > 1000:
            raise ValueError("Go module graph exceeds one complete audit batch")
        payload = json.dumps({"queries": queries}).encode()
        (output / "go-module-queries.json").write_bytes(payload)
        request = urllib.request.Request("https://api.osv.dev/v1/querybatch", data=payload, headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(request, timeout=60) as response:
            result = response.read()
        (output / "go-module-audit.json").write_bytes(result)
        report = json.loads(result)
        # A known, unused upstream development-tool dependency remains visible
        # in the full graph. Classify it only after fresh import and build-source
        # evidence for every shipping OS/architecture; never mute scan records.
        if any(entry.get("vulns") for entry in report.get("results", [])):
            prove_non_shipping(go, members, modules, output)
        summary["non_shipping_upstream_tool_findings"] = check_module_graph(
            report, len(queries), queries, (output / "go-nonshipping-proof.json").is_file())
        summary["go_dependency_versions"] = len(queries)

        result = command("npm", [pnpm, "audit", "--json"])
        check_npm(result.stdout, result.returncode)
        requirements = output / "requirements.txt"
        exported = command("python-export", [uv, "export", "--locked", "--all-packages", "--all-extras", "--all-groups", "--no-emit-workspace", "--no-hashes"])
        if exported.returncode:
            raise ValueError("locked Python dependency export failed")
        requirements.write_text(python_requirements(exported.stdout, json.loads((ROOT / "images/agents/versions.lock.json").read_text())))
        result = command("python", [uv, "tool", "run", "--from", PIP_AUDIT, "pip-audit", "--disable-pip", "--no-deps", "--progress-spinner", "off", "--format", "json", "--requirement", str(requirements)])
        check_python(result.stdout, result.returncode)
        summary["passed"] = True
    except Exception as error:
        summary["error"] = str(error)
    summary["elapsed_seconds"] = time.monotonic() - started
    (output / "summary.json").write_text(json.dumps(summary, indent=2) + "\n")
    print(json.dumps(summary))
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
