# Runtime Images

Axnoded runtime images define workload profiles for OCI sandboxes. Every image can run under the default sandboxd PID 1 model; optional sandboxd providers are discovered from the image environment and installed tools.

## Base Runtime Boundaries

`server-base` is the small service rootfs. It owns the `axern` user, hardened SSH, nginx, supervisord, locale, health page, and basic process/network tools. It intentionally has no Go, Node.js, Poetry, pipx, compiler, editor, or project Python environment contract. Python may still appear as an implementation dependency of Ubuntu's supervisor package; applications must not rely on it.

`coding-base` inherits `server-base` and provides a coding-oriented ephemeral sandbox rootfs. It adds fixed Go, Node.js/pnpm, Python/venv, uv, compilation and diagnostic tools. Projects own their `pyproject.toml` and `.venv`; the image does not pre-create a project environment.

`desktop-base` also inherits `server-base`, independently. It adds only the Python, Playwright, and X11 dependencies needed by caller-owned browser workloads and Computer Use. It does not inherit the coding toolchain.

## Desktop Base Contract

`desktop-base` is Axern's verified desktop-capable profile for sandboxd `computer_use`. User images can provide the same capability when they satisfy the same contract.

Required environment:

- `DISPLAY` set to the desktop display, currently `:99` for `desktop-base`

Required commands unless replaced by sandboxd command hooks:

- screenshot backend: `import` from ImageMagick, or `AXERN_SANDBOXD_SCREENSHOT_CMD`
- display/input backend: `xdotool`, or explicit `AXERN_SANDBOXD_DISPLAY_CMD`, `AXERN_SANDBOXD_MOUSE_CMD`, and `AXERN_SANDBOXD_KEYBOARD_CMD`
- a running X11 display that can answer `xdotool getdisplaygeometry`

`desktop-base` provides this contract with:

- `Xvfb :99`
- `fluxbox`
- Playwright-managed Chromium exposed through `/usr/local/bin/chromium`
- ImageMagick
- `xdotool`
- the inherited `server-base` SSH and nginx process set

The provider status endpoint reports dependency checks for display env, screenshot backend, display backend, input backend, and display server readiness. Generic images without the contract continue to run normally; they simply do not advertise the optional `computer_use` capability.

`desktop-base` installs a pinned Python Playwright release and Playwright-managed Chromium instead of Ubuntu's snap-backed browser packages. These are workload dependencies, not an Axern-managed browser provider: callers start, drive, and stop browser processes through their own code, process APIs, or Computer Use.

The build helper accepts `PLAYWRIGHT_DOWNLOAD_HOST` for an operator-selected browser artifact mirror. It is a build argument only, not runtime configuration; leaving it empty uses Playwright's official download sources. Regional workspaces should supply the mirror through their existing accelerated build entrypoint.
