# Runtime Images

Axnoded runtime images define workload profiles for OCI sandboxes. Every image
can run under the default sandboxd PID 1 model; optional sandboxd providers are
discovered from the image environment and installed tools.

## Base Runtime Boundaries

`server-base` is the small service rootfs. It owns the `axern` user, hardened
SSH, nginx, supervisord, locale, health page, and basic process/network tools.
It intentionally has no Go, Node.js, Poetry, pipx, compiler, editor, or project
Python environment contract. Python may still appear as an implementation
dependency of Ubuntu's supervisor package; applications must not rely on it.

`coding-base` inherits `server-base` and is the default persistent coding
workspace. It adds fixed Go, Node.js/pnpm, Python/venv, uv, compilation and
diagnostic tools. Projects own their `pyproject.toml` and `.venv`; the image
does not pre-create a project environment.

`desktop-base` also inherits `server-base`, independently. It adds only the
Python and X11/browser dependencies needed by Playwright and computer-use. It
does not inherit the coding toolchain.

## Agent Bundles

Claude Code and Codex are published only as scratch-based, relocatable bundles,
not runtime templates. The control-plane Agent Bundle Catalog selects their
versioned image and absolute bundle binary path. A workload mounts the selected
image read-only at `/opt/axern/agents/<agent>`.

`claude-code-bundle` exposes `/bin/claude`. `codex-bundle` exposes `/bin/codex`
and carries its own Node.js executable and npm package, so the bundle does not
depend on the workspace's Node.js version. The mounted commands resolve to
`/opt/axern/agents/claude-code/bin/claude` and
`/opt/axern/agents/codex/bin/codex`.

The task rootfs and bundle have separate responsibilities: `coding-base`
supplies the coding workspace and shell, while a bundle supplies exactly one
agent tool. Provider tokens and endpoints are injected at runtime and are never
baked into either image.

## Python Function Worker

`python311` includes the Axern Python SDK and exposes
`python3 -m axern_sdk.function.worker`. Controld-created Function worker
Services use that module as their entrypoint, download the uploaded bundle from
the configured controld bundle endpoint, load the manifest handler, and serve
`/healthz` plus `/invoke` over HTTP for gatewayd dispatch.

## Desktop Base Contract

`desktop-base` is Axern's verified desktop-capable profile for sandboxd
`computer_use`. User images can provide the same capability when they satisfy
the same contract.

Required environment:

- `DISPLAY` set to the desktop display, currently `:99` for `desktop-base`

Required commands unless replaced by sandboxd command hooks:

- screenshot backend: `import` from ImageMagick, or
  `AXERN_SANDBOXD_SCREENSHOT_CMD`
- display/input backend: `xdotool`, or explicit
  `AXERN_SANDBOXD_DISPLAY_CMD`, `AXERN_SANDBOXD_MOUSE_CMD`, and
  `AXERN_SANDBOXD_KEYBOARD_CMD`
- a running X11 display that can answer `xdotool getdisplaygeometry`

`desktop-base` provides this contract with:

- `Xvfb :99`
- `fluxbox`
- Playwright-managed Chromium exposed through `/usr/local/bin/chromium`
- ImageMagick
- `xdotool`
- the inherited `server-base` SSH and nginx process set

The provider status endpoint reports dependency checks for display env,
screenshot backend, display backend, input backend, and display server
readiness. Generic images without the contract continue to run normally; they
simply do not advertise the optional `computer_use` capability.

## Browser Provider Contract

The sandboxd `browser` provider is optional and belongs to desktop/browser
profiles. Generic sandboxes should keep running without it.

The provider is discovered when one of these is present:

- `AXERN_SANDBOXD_BROWSER_CMD` set to the browser executable name
- `AXERN_SANDBOXD_BROWSER_OPEN_CMD` for profile-managed browser launch hooks
- an installed supported browser command on `PATH`

Supported executable discovery checks `chromium`, `chromium-browser`,
`google-chrome`, `google-chrome-stable`, and `firefox`. `desktop-base` installs
a pinned Python Playwright release and uses Playwright-managed Chromium instead
of Ubuntu's snap-backed browser packages so the browser can run inside OCI
sandboxes and can also be driven by Python Playwright code in user workloads.
Hook-based profiles can
also define `AXERN_SANDBOXD_BROWSER_CLOSE_CMD`; sandboxd passes the requested
URL to open hooks through `AXERN_BROWSER_URL`.

The provider exposes internal sandboxd status, open, and close operations. It
does not make browser access public by itself; product-level exposure must still
go through Axern gateway/tunnel APIs.
