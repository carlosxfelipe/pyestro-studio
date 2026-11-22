#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

DRY_RUN=false
INCLUDE_VENVS=false
RESET_STUDIO_CONFIG=false
TARGETS=()

usage() {
  cat <<'EOF'
🧹 Usage: ./uninstall_pyestro.sh [OPTIONS] [TARGET...]

Removes installed pyestro and/or pyestro-web packages so you can test
Pyestro Studio in clean/missing-dependency scenarios.

Targets:
  pyestro        Remove only pyestro
  pyestro-web    Remove only pyestro-web
  all            Remove both pyestro and pyestro-web (default)

Options:
  --include-venvs        Also remove local sibling .venv directories
                         (../pyestro/.venv and ../pyestro-web/.venv)
  --reset-studio-config  Also remove Pyestro Studio saved environment config
  -n, --dry-run          Print actions without deleting anything
  -h, --help             Show this help message
EOF
}

log_info() {
  printf '%s\n' "$*"
}

remove_path() {
  local target="$1"
  if [ -L "$target" ] || [ -e "$target" ]; then
    if [ "$DRY_RUN" = true ]; then
      log_info "🧪 [dry-run] rm -rf $target"
    else
      rm -rf "$target"
      log_info "  🗑️  Removed: $target"
    fi
  fi
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

uninstall_via_uv() {
  local pkg="$1"
  local uv_bin
  if uv_bin="$(find_uv_bin)"; then
    if [ "$DRY_RUN" = true ]; then
      log_info "🧪 [dry-run] $uv_bin tool uninstall $pkg"
    else
      if "$uv_bin" tool uninstall "$pkg" >/dev/null 2>&1; then
        log_info "  ✅ Uninstalled via uv tool: $pkg"
      fi
    fi
  fi

  local uv_tool_dir="${UV_TOOL_DIR:-${XDG_DATA_HOME:-${HOME}/.local/share}/uv/tools}"
  remove_path "${uv_tool_dir}/${pkg}"
}

uninstall_via_pipx() {
  local pkg="$1"
  if command -v pipx >/dev/null 2>&1; then
    if [ "$DRY_RUN" = true ]; then
      log_info "🧪 [dry-run] pipx uninstall $pkg"
    else
      if pipx list --short 2>/dev/null | grep -qx "$pkg"; then
        pipx uninstall "$pkg"
      fi
    fi
  fi
}

uninstall_via_pip() {
  local pkg="$1"
  local py_bin
  local checked=""
  for py_bin in \
    "$(command -v python3 2>/dev/null || true)" \
    "$(command -v python 2>/dev/null || true)" \
    "/opt/homebrew/bin/python3" \
    "/usr/local/bin/python3"; do
    [ -n "$py_bin" ] || continue
    [ -x "$py_bin" ] || continue
    case " $checked " in
    *" $py_bin "*) continue ;;
    esac
    checked="$checked $py_bin"

    if "$py_bin" -m pip show "$pkg" >/dev/null 2>&1; then
      if [ "$DRY_RUN" = true ]; then
        log_info "🧪 [dry-run] $py_bin -m pip uninstall -y $pkg"
      else
        "$py_bin" -m pip uninstall -y "$pkg"
      fi
    fi
  done
}

remove_leftover_binaries() {
  local pkg="$1"
  local bin_dir
  for bin_dir in \
    "${XDG_BIN_HOME:-}" \
    "${HOME}/.local/bin" \
    "${HOME}/.cargo/bin" \
    "/opt/homebrew/bin" \
    "/usr/local/bin"; do
    [ -n "$bin_dir" ] || continue
    local candidate="${bin_dir}/${pkg}"
    if [ -L "$candidate" ] || [ -e "$candidate" ]; then
      if [ -w "$bin_dir" ] || [ -w "$candidate" ] || [ "$DRY_RUN" = true ]; then
        remove_path "$candidate"
      else
        log_info "  ⚠️  Skipped (not writable without elevated permissions): $candidate"
      fi
    fi
  done
}

remove_local_venv() {
  local pkg="$1"
  local repo_venv
  for repo_venv in \
    "${SCRIPT_DIR}/${pkg}/.venv" \
    "$(dirname "$SCRIPT_DIR")/${pkg}/.venv"; do
    remove_path "$repo_venv"
  done
}

reset_studio_config() {
  log_info "⚙️  Resetting Pyestro Studio saved environment config..."
  remove_path "${HOME}/Library/Application Support/com.carlosxfelipe.pyestro-studio"
  remove_path "${XDG_CONFIG_HOME:-${HOME}/.config}/com.carlosxfelipe.pyestro-studio"
}

while [ "$#" -gt 0 ]; do
  case "$1" in
  pyestro | pyestro-web)
    TARGETS+=("$1")
    ;;
  all)
    TARGETS+=("pyestro" "pyestro-web")
    ;;
  --include-venvs)
    INCLUDE_VENVS=true
    ;;
  --reset-studio-config)
    RESET_STUDIO_CONFIG=true
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

if [ "${#TARGETS[@]}" -eq 0 ]; then
  TARGETS=("pyestro" "pyestro-web")
fi

# Deduplicate targets while preserving order.
UNIQUE_TARGETS=()
for t in "${TARGETS[@]}"; do
  seen=false
  for u in "${UNIQUE_TARGETS[@]+"${UNIQUE_TARGETS[@]}"}"; do
    if [ "$u" = "$t" ]; then
      seen=true
      break
    fi
  done
  if [ "$seen" = false ]; then
    UNIQUE_TARGETS+=("$t")
  fi
done

for pkg in "${UNIQUE_TARGETS[@]}"; do
  log_info "🧹 Removing $pkg..."
  uninstall_via_uv "$pkg"
  uninstall_via_pipx "$pkg"
  uninstall_via_pip "$pkg"
  remove_leftover_binaries "$pkg"
  if [ "$INCLUDE_VENVS" = true ]; then
    remove_local_venv "$pkg"
  fi
done

if [ "$RESET_STUDIO_CONFIG" = true ]; then
  reset_studio_config
fi

log_info ""
log_info "🔍 Current status:"
for pkg in "pyestro" "pyestro-web"; do
  if found_path="$(command -v "$pkg" 2>/dev/null)"; then
    log_info "  📦 $pkg on PATH: $found_path"
  else
    log_info "  ✅ $pkg on PATH: missing"
  fi
  sibling_venv="$(dirname "$SCRIPT_DIR")/${pkg}/.venv"
  if [ -d "$sibling_venv" ]; then
    log_info "  ℹ️  $pkg sibling .venv: present ($sibling_venv)"
  fi
done
