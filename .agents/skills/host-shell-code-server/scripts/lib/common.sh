#!/bin/bash
# Shared settings and helpers for the host-shell-code-server Linux scripts; sourced, not run.
# Derived from Hondarer/code-server-launcher (MIT License). download.sh also runs on macOS
# clients, so this file stays compatible with bash 3.2.

HSCS_NAME="host-shell-code-server"
HSCS_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

_hscs_port="${CODE_SERVER_PORT:-}"
# shellcheck disable=SC1091
. "${HSCS_ROOT}/version.env"
if [[ -n "${_hscs_port}" ]]; then
  CODE_SERVER_PORT="${_hscs_port}"
fi
unset _hscs_port

HSCS_PACKAGES_DIR="${HSCS_ROOT}/packages"
HSCS_DEFAULTS_DIR="${HSCS_ROOT}/defaults"
HSCS_RELEASE_URL="https://github.com/coder/code-server/releases/download/v${CODE_SERVER_VERSION}"

# Locations on the target. They differ from code-server-launcher's, so the two never share an
# installation, settings or extensions.
HSCS_LIB_DIR="${HOME}/.local/lib/${HSCS_NAME}"
HSCS_INSTALL_DIR="${HSCS_LIB_DIR}/code-server-${CODE_SERVER_VERSION}"
HSCS_BIN="${HSCS_INSTALL_DIR}/bin/code-server"
HSCS_DATA_DIR="${HOME}/.local/share/${HSCS_NAME}"
HSCS_USER_DATA_DIR="${HSCS_DATA_DIR}/user-data"
HSCS_EXTENSIONS_DIR="${HSCS_DATA_DIR}/extensions"
HSCS_CONFIG_DIR="${HOME}/.config/${HSCS_NAME}"
HSCS_CONFIG_FILE="${HSCS_CONFIG_DIR}/config.yaml"
# Created once every default extension is installed; while it exists, install leaves extensions alone.
HSCS_MARKER="${HSCS_USER_DATA_DIR}/.host-shell-code-server-defaults"
# code-server writes coder-logs and heartbeat here whatever --user-data-dir says.
HSCS_SHARED_DATA_DIR="${XDG_DATA_HOME:-${HOME}/.local/share}/code-server"

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  local cmd
  for cmd in "$@"; do
    command -v "${cmd}" >/dev/null 2>&1 || die "command not found: ${cmd}"
  done
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | cut -d' ' -f1
  else
    shasum -a 256 "$1" | cut -d' ' -f1
  fi
}

file_sha256_matches() {
  [[ -f "$1" ]] && [[ "$(sha256_of "$1")" == "$2" ]]
}

# asset_name TYPE and asset_sha256 TYPE describe the release archive for linux-amd64,
# linux-arm64 or windows-amd64.
asset_name() {
  echo "code-server-${CODE_SERVER_VERSION}-$1.tar.gz"
}

asset_sha256() {
  case "$1" in
    linux-amd64) echo "${CODE_SERVER_LINUX_AMD64_SHA256}" ;;
    linux-arm64) echo "${CODE_SERVER_LINUX_ARM64_SHA256}" ;;
    windows-amd64) echo "${CODE_SERVER_WINDOWS_AMD64_SHA256}" ;;
    *) return 1 ;;
  esac
}

# linux_type prints the release type for this machine, or fails for an unsupported architecture.
linux_type() {
  case "$(uname -m)" in
    x86_64) echo linux-amd64 ;;
    aarch64 | arm64) echo linux-arm64 ;;
    *) return 1 ;;
  esac
}

code_server_installed() {
  [[ -x "${HSCS_BIN}" && -x "${HSCS_INSTALL_DIR}/lib/node" ]]
}

# The code-server command line with this tool's configuration, settings and extensions.
code_server_cli() {
  "${HSCS_BIN}" --config "${HSCS_CONFIG_FILE}" --user-data-dir "${HSCS_USER_DATA_DIR}" \
    --extensions-dir "${HSCS_EXTENSIONS_DIR}" "$@"
}

# code_server_running succeeds while a code-server started with this tool's configuration runs.
code_server_running() {
  local p cmdline
  for p in /proc/[0-9]*; do
    [[ "${p#/proc/}" == "$$" ]] && continue
    cmdline="$(tr '\0' ' ' <"${p}/cmdline" 2>/dev/null || true)"
    if [[ "${cmdline}" == *"--config ${HSCS_CONFIG_FILE} "* && "${cmdline}" != *"--install-extension"* && "${cmdline}" != *"--list-extensions"* ]]; then
      return 0
    fi
  done
  return 1
}
