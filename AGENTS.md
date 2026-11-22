# AGENTS.md

Guidance for coding agents and contributors working on **Pyestro Studio**.

## UI Guidelines — GNOME HIG & GTK 4.x+

All UI styles, layouts, and interactions in `ui/` (`ui/index.html`, `ui/app.css`, `ui/app.js`) **must** follow the [GNOME Human Interface Guidelines (HIG)](https://developer.gnome.org/hig/) using **GTK 4.x+ / libadwaita** patterns as the visual and behavioral reference:

- **Layout & Navigation:**
  - Use a full-width window shell (`.adw-window` with a single `.adw-headerbar` and centered `.adw-view-switcher` / `.adw-view-tab` navigation, without a sidebar), and boxed `.boxed-list` / `.card` sections with consistent `12px` card radius and `7px` control radius.
  - Ensure all pages and multi-column grids remain usable down to the minimum window size (`1024x768`, `glaze.HintMin`) without horizontal clipping or overlapping controls (use flexible `minmax(...)` / `auto-fit` grids and `min-width: 0` on flex/grid children).
- **Color, Contrast & Light/Dark Parity:**
  - Every UI surface, border, and semantic state must adapt to both dark and light color schemes via CSS custom properties in `:root` and `@media (prefers-color-scheme: light)`.
  - Semantic tokens (`--accent-*`, `--success-*`, `--warning-*`, `--destructive-*`) must maintain strong legibility and WCAG AA contrast in **both** light and dark modes (e.g., use deep, high-contrast foreground/border tones on light backgrounds instead of pale yellows). Never hardcode dark-only or light-only foregrounds/borders outside theme tokens.
- **Controls, Typography & Feedback:**
  - Follow GTK 4 / Adwaita widget semantics: `.adw-view-switcher` / `.adw-view-tab` headerbar navigation, `.adw-btn-primary` (suggested action) for primary actions, `.target-segmented` linked/segmented controls for mutually exclusive modes, `.status-pill` badges for compact state indicators, inline callouts/banners for actionable warnings, and `.adw-toast` for non-blocking transient notifications.
  - Keep labels concise, scannable, and clear; use monospace (`--font-mono`) for paths, YAML, logs, and selectors.
  - Use crisp monochrome SVG icons (`currentColor`) rather than decorative clutter in controls and navigation items.

## Architecture & Project Structure

- `main.go` — Pure Go (`CGO_ENABLED=0`) desktop entry point using **Glaze** (`WKWebView` / `WebKitGTK` / `WebView2`), native menus, file dialogs, and `AppAPI` bindings.
- `studio/env.go` — Host tool detection (`uv`, `adb`, `pyestro`, `pyestro-web`). Only detects installed packages (`PATH` / `uv tool` directories), with no local `.venv` or custom `pyestro`/`pyestro-web` path overrides.
- `studio/manager.go` & `studio/worker.py` — Persistent Python JSON-RPC sidecar manager and `uv tool` installer (`pyestro` and `pyestro-web` + Playwright Chromium).
- `studio/types.go` — Shared Go/JSON contracts between backend and frontend.
- `store/store.go` — Encrypted local persistence helper.
- `ui/` — Embedded frontend assets (`ui/index.html`, `ui/app.css`, `ui/app.js`).

## Packaging & Scope Rules

- Do **not** reintroduce local `.venv` auto-detection or custom local path fields for `pyestro` or `pyestro-web`.
- Do **not** mention internal developer helper scripts (`*.sh`) inside the Studio UI, backend error messages, or user-facing documentation.

## Tooling & Checks

Always run formatting, static checks, and tests after making changes:

```bash
./fmt.sh
go vet ./...
go test -count=1 ./...
```
