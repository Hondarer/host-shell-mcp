#!/bin/bash
# Run code-server in the foreground on 127.0.0.1 (start it with host-shell's job_start).
# It listens on loopback only because it is reached through host-shell's port forwarding.
# CODE_SERVER_WORKSPACE selects the folder to open (default: the home directory);
# It listens on the first free port from CODE_SERVER_PORT_FIRST to CODE_SERVER_PORT_LAST (version.env);
# CODE_SERVER_PORT pins a single port instead. The agent reads the port from code-server's
# "HTTP server listening on" line and forwards to it.

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

echo "starting code-server ${CODE_SERVER_VERSION} on 127.0.0.1:${port}"
echo "workspace: ${workspace}"
echo "configuration: ${HSCS_CONFIG_FILE}"
# Inside a VS Code terminal these make code-server hand over to that VS Code instead of listening.
unset VSCODE_IPC_HOOK_CLI ELECTRON_RUN_AS_NODE
cd "${workspace}"
exec "${HSCS_BIN}" --config "${HSCS_CONFIG_FILE}" --bind-addr "127.0.0.1:${port}" \
  --auth password --ignore-last-opened --user-data-dir "${HSCS_USER_DATA_DIR}" \
  --extensions-dir "${HSCS_EXTENSIONS_DIR}" "${workspace}"
