#!/bin/bash
# Run code-server in the foreground on 127.0.0.1 (start it with host-shell's job_start).
# It listens on loopback only because it is reached through host-shell's port forwarding.
# CODE_SERVER_WORKSPACE selects the folder to open (default: the home directory);
# It listens on the first free port from CODE_SERVER_PORT_FIRST to CODE_SERVER_PORT_LAST (version.env);
# CODE_SERVER_PORT pins a single port instead. The agent reads the port from code-server's
# "HTTP server listening on" line and forwards to it.
# code-server's session socket (where its extension host registers browser windows) defaults to
# <user-data-dir>/code-server-ipc.sock. A Unix socket path holds at most 107 bytes on Linux, so when
# a long HOME makes that path longer, the socket goes to XDG_RUNTIME_DIR or, without it, to a
# private folder /tmp/host-shell-code-server-<uid>. If that folder is unsafe, code-server keeps the
# default path and only warns that it cannot create the socket.

set -euo pipefail
# shellcheck source=../lib/common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/common.sh"

[[ $# -eq 0 ]] || die "usage: $0 (no arguments)"
workspace="${CODE_SERVER_WORKSPACE:-${HOME}}"

code_server_installed || die "code-server is not installed; run install.sh first"
[[ -f "${HSCS_CONFIG_FILE}" ]] || die "configuration not found: ${HSCS_CONFIG_FILE}; run install.sh first"
[[ -d "${workspace}" ]] || die "workspace folder not found: ${workspace}"
# A port counts as in use when a connection to it succeeds. python3 and ss are not required on the target.
port_in_use() { (: <>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }
if [[ -n "${CODE_SERVER_PORT:-}" ]]; then
  port="${CODE_SERVER_PORT}"
  ! port_in_use "${port}" || die "port ${port} on 127.0.0.1 is already in use"
else
  port=""
  for ((p = CODE_SERVER_PORT_FIRST; p <= CODE_SERVER_PORT_LAST; p++)); do
    if ! port_in_use "${p}"; then
      port="${p}"
      break
    fi
  done
  [[ -n "${port}" ]] || die "no free port in ${CODE_SERVER_PORT_FIRST}-${CODE_SERVER_PORT_LAST} on 127.0.0.1"
fi

# Prints the session socket to use instead of the default, or nothing to keep the default.
session_socket() {
  local LC_ALL=C dir owner
  local socket="${HSCS_USER_DATA_DIR}/code-server-ipc.sock"
  ((${#socket} > 107)) || return 0
  if [[ -n "${XDG_RUNTIME_DIR:-}" && -d "${XDG_RUNTIME_DIR}" ]]; then
    echo "${XDG_RUNTIME_DIR%/}/${HSCS_NAME}-ipc.sock"
    return 0
  fi
  dir="/tmp/${HSCS_NAME}-$(id -u)"
  # Another user could replace or read a folder they own or can write to (same rule as host-shell's deploy.sh).
  if [[ -L "${dir}" ]]; then
    echo "warning: unsafe directory (symbolic link): ${dir}; keeping the default session socket" >&2
    return 0
  fi
  if [[ ! -d "${dir}" ]] && ! mkdir -m 700 "${dir}" 2>/dev/null; then
    echo "warning: cannot create ${dir}; keeping the default session socket" >&2
    return 0
  fi
  # shellcheck disable=SC2046
  set -- $(ls -ldn "${dir}")
  owner="$3"
  case "$1" in
  ?????w????* | ????????w?*)
    echo "warning: unsafe directory (writable by group or others): ${dir}; keeping the default session socket" >&2
    return 0
    ;;
  esac
  if [[ "${owner}" != "$(id -u)" && "${owner}" != 0 ]]; then
    echo "warning: unsafe directory (owned by uid ${owner}): ${dir}; keeping the default session socket" >&2
    return 0
  fi
  echo "${dir}/ipc.sock"
}
session_args=()
socket="$(session_socket)"
if [[ -n "${socket}" ]]; then
  session_args=(--session-socket "${socket}")
fi

echo "starting code-server ${CODE_SERVER_VERSION} on 127.0.0.1:${port}"
echo "workspace: ${workspace}"
echo "configuration: ${HSCS_CONFIG_FILE}"
[[ -z "${socket}" ]] || echo "session socket: ${socket} (the default path exceeds the Unix socket limit)"
# Inside a VS Code terminal these make code-server hand over to that VS Code instead of listening.
unset VSCODE_IPC_HOOK_CLI ELECTRON_RUN_AS_NODE
cd "${workspace}"
exec "${HSCS_BIN}" --config "${HSCS_CONFIG_FILE}" --bind-addr "127.0.0.1:${port}" \
  --auth password --ignore-last-opened --user-data-dir "${HSCS_USER_DATA_DIR}" \
  --extensions-dir "${HSCS_EXTENSIONS_DIR}" ${session_args[@]+"${session_args[@]}"} "${workspace}"
