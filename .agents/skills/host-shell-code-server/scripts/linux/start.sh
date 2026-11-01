#!/bin/bash
# Run code-server in the foreground on 127.0.0.1 (start it with host-shell's job_start).
# It listens on loopback only because it is reached through host-shell's port forwarding.
# CODE_SERVER_WORKSPACE selects the folder to open (default: the home directory);
# CODE_SERVER_PORT overrides the port in version.env.

set -euo pipefail
# shellcheck source=../lib/common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/common.sh"

[[ $# -eq 0 ]] || die "usage: $0 (no arguments)"
workspace="${CODE_SERVER_WORKSPACE:-${HOME}}"

code_server_installed || die "code-server is not installed; run install.sh first"
[[ -f "${HSCS_CONFIG_FILE}" ]] || die "configuration not found: ${HSCS_CONFIG_FILE}; run install.sh first"
[[ -d "${workspace}" ]] || die "workspace folder not found: ${workspace}"
if (: <>"/dev/tcp/127.0.0.1/${CODE_SERVER_PORT}") 2>/dev/null; then
  die "port ${CODE_SERVER_PORT} on 127.0.0.1 is already in use"
fi

echo "starting code-server ${CODE_SERVER_VERSION} on 127.0.0.1:${CODE_SERVER_PORT}"
echo "workspace: ${workspace}"
echo "configuration: ${HSCS_CONFIG_FILE}"
# Inside a VS Code terminal these make code-server hand over to that VS Code instead of listening.
unset VSCODE_IPC_HOOK_CLI ELECTRON_RUN_AS_NODE
cd "${workspace}"
exec "${HSCS_BIN}" --config "${HSCS_CONFIG_FILE}" --bind-addr "127.0.0.1:${CODE_SERVER_PORT}" \
  --auth password --ignore-last-opened --user-data-dir "${HSCS_USER_DATA_DIR}" \
  --extensions-dir "${HSCS_EXTENSIONS_DIR}" "${workspace}"
