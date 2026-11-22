#!/usr/bin/env bash
set -euo pipefail

PYESTRO_REPO="git+https://github.com/carlosxfelipe/pyestro.git"
PYESTRO_WEB_REPO="git+https://github.com/carlosxfelipe/pyestro-web.git"

DRY_RUN=false
AUTO_YES=false
UPGRADE=false
SKIP_PLAYWRIGHT=false
TARGETS=()

usage() {
  cat <<'EOF'
🚀 Usage: ./install_pyestro.sh [OPTIONS] [TARGET...]

Interactively (y/n) installs pyestro and/or pyestro-web via `uv tool install`,
and optionally installs Playwright Chromium for pyestro-web.

Targets:
  pyestro        Install pyestro
  pyestro-web    Install pyestro-web
  all            Install both pyestro and pyestro-web

Options:
  -y, --yes              Skip prompts and answer yes (defaults to all if no target given)
      --upgrade          Pass --upgrade to `uv tool install`
      --skip-playwright  Skip `uv tool run --from pyestro-web playwright install chromium`
  -n, --dry-run          Print commands without executing them
  -h, --help             Show this help message
EOF
}

log_info() {
  printf '%s\n' "$*"
}

find_uv_bin() {
  if command -v uv >/dev/null 2>&1; then
    command -v uv
    return 0
  fi
  local candidate
  for candidate in \
    "${HOME}/.local/bin/uv" \
    "${HOME}/.cargo/bin/uv" \
    "/opt/homebrew/bin/uv" \
    "/usr/local/bin/uv"; do
    if [ -x "$candidate" ]; then
      printf '%s\n' "$candidate"
      return 0
    fi
  done
  return 1
}

ask_yes_no() {
  local prompt="$1"
  if [ "$AUTO_YES" = true ]; then
    return 0
  fi

  local reply=""
  while true; do
    printf '%s [y/n]: ' "$prompt"
    if ! IFS= read -r reply; then
      printf '\n'
      return 1
    fi
    case "$reply" in
    [yY] | [yY][eE][sS] | [sS] | [sS][iI][mM])
      return 0
      ;;
    [nN] | [nN][oO] | [nN][aA][oO] | [nN][ãÃ][oO])
      return 1
      ;;
    *)
      log_info "⚠️  Please answer 'y' or 'n'."
      ;;
    esac
  done
}

run_uv_command() {
  if [ "$DRY_RUN" = true ]; then
    printf '🧪 [dry-run]'
    printf ' %q' "$@"
    printf '\n'
    return 0
  fi
  "$@"
}

install_tool() {
  local uv_bin="$1"
  local pkg="$2"
  local repo_url="$3"
  local args=("tool" "install")

  if [ "$UPGRADE" = true ]; then
    args+=("--upgrade")
  fi
  args+=("$repo_url")

  log_info "📦 Installing $pkg..."
  run_uv_command "$uv_bin" "${args[@]}"
  if [ "$DRY_RUN" = false ]; then
    log_info "✅ $pkg installed."
  fi
}

install_playwright_chromium() {
  local uv_bin="$1"
  log_info "🎭 Installing Playwright Chromium for pyestro-web..."
  run_uv_command "$uv_bin" tool run --from pyestro-web playwright install chromium
  if [ "$DRY_RUN" = false ]; then
    log_info "✅ Playwright Chromium installed."
  fi
}

while [ "$#" -gt 0 ]; do
  case "$1" in
  pyestro | pyestro-web)
    TARGETS+=("$1")
    ;;
  all)
    TARGETS+=("pyestro" "pyestro-web")
    ;;
  -y | --yes)
    AUTO_YES=true
    ;;
  --upgrade)
    UPGRADE=true
    ;;
  --skip-playwright)
    SKIP_PLAYWRIGHT=true
    ;;
  -n | --dry-run)
    DRY_RUN=true
    ;;
  -h | --help)
    usage
    exit 0
    ;;
  *)
    printf '❌ Unknown argument: %s\n\n' "$1" >&2
    usage >&2
    exit 1
    ;;
  esac
  shift
done

if ! UV_BIN="$(find_uv_bin)"; then
  log_info "❌ 'uv' was not found. Install it from https://docs.astral.sh/uv/ first."
  exit 1
fi

INSTALL_PYESTRO=false
INSTALL_PYESTRO_WEB=false
INSTALL_PLAYWRIGHT=false

if [ "${#TARGETS[@]}" -gt 0 ]; then
  for t in "${TARGETS[@]}"; do
    case "$t" in
    pyestro) INSTALL_PYESTRO=true ;;
    pyestro-web) INSTALL_PYESTRO_WEB=true ;;
    esac
  done
elif [ "$AUTO_YES" = true ]; then
  INSTALL_PYESTRO=true
  INSTALL_PYESTRO_WEB=true
else
  if ask_yes_no "📱 Install pyestro (${PYESTRO_REPO})?"; then
    INSTALL_PYESTRO=true
  else
    log_info "⏭️  Skipping pyestro."
  fi

  if ask_yes_no "🌐 Install pyestro-web (${PYESTRO_WEB_REPO})?"; then
    INSTALL_PYESTRO_WEB=true
  else
    log_info "⏭️  Skipping pyestro-web."
  fi
fi

if [ "$SKIP_PLAYWRIGHT" = false ]; then
  if [ "$INSTALL_PYESTRO_WEB" = true ] || command -v pyestro-web >/dev/null 2>&1; then
    if ask_yes_no "🎭 Install Playwright Chromium (uv tool run --from pyestro-web playwright install chromium)?"; then
      INSTALL_PLAYWRIGHT=true
    else
      log_info "⏭️  Skipping Playwright Chromium."
    fi
  fi
fi

if [ "$INSTALL_PYESTRO" = false ] && [ "$INSTALL_PYESTRO_WEB" = false ] && [ "$INSTALL_PLAYWRIGHT" = false ]; then
  log_info "ℹ️  Nothing selected to install."
  exit 0
fi

log_info ""
if [ "$INSTALL_PYESTRO" = true ]; then
  install_tool "$UV_BIN" "pyestro" "$PYESTRO_REPO"
fi

if [ "$INSTALL_PYESTRO_WEB" = true ]; then
  install_tool "$UV_BIN" "pyestro-web" "$PYESTRO_WEB_REPO"
fi

if [ "$INSTALL_PLAYWRIGHT" = true ]; then
  install_playwright_chromium "$UV_BIN"
fi

log_info ""
log_info "🔍 Current status:"
for pkg in "pyestro" "pyestro-web"; do
  if found_path="$(command -v "$pkg" 2>/dev/null)"; then
    log_info "  ✅ $pkg on PATH: $found_path"
  else
    log_info "  ⚠️  $pkg on PATH: missing"
  fi
done
