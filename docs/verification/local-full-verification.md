# Local Full Verification Checklist

Use this checklist before larger handoffs, release/runtime changes, or any
change that should prove both local truth environments plus the repository-wide
verification pass.

Run from the repository root on the same commit and worktree:

```bash
make local-compose-refresh-verify
make kind-refresh-verify
bash ./scripts/verify-all.sh
make local-compose-nydus-smoke
make kind-axern-nydus-smoke
```

This covers:

- Docker Compose local truth refresh and core smoke.
- Repo-managed kind local truth refresh and core smoke.
- Repository-standard lint, build, unit test, proto, architecture, and E2E
  verification entrypoints.
- Axern's own Compose Nydus path:
  `registry source image -> nydus builder -> registry Nydus image -> imagemgr
  -> imagefsd -> axnoded -> sandbox`.
- Axern's own kind Nydus path using the same repo-managed local Nydus image
  flow.

Expected result:

- All five commands complete successfully.
- The final `git status --short` only shows intentional source changes.
- If the Nydus image is missing, the Nydus smoke may build and push
  `localhost:5001/axern/nydus-smoke:dev` into the repo-managed local registry.

Optional additions:

```bash
bash ./scripts/verify-all.sh --include-local-storage
bash ./scripts/verify-all.sh --include-bpfnet-generate-check
bash ./scripts/verify-all.sh --include-proto-breaking
```

Use these when the change touches storage/volume behavior, generated bpfnet tc
artifacts, or proto compatibility.

Production or externally built Nydus images can be checked explicitly:

```bash
NYDUS_TEST_IMAGE=<registry/ref:tag-or-digest> make local-compose-nydus-smoke
NYDUS_TEST_IMAGE=<registry/ref:tag-or-digest> make kind-axern-nydus-smoke
```

`NYDUS_TEST_IMAGE` is an override path. The default smoke remains
self-contained and builds the local Nydus fixture through the repo-managed
builder and registry.
