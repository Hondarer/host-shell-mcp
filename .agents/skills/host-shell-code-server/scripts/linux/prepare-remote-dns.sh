#!/bin/bash
# Create/verify an optional patched copy; never modify the original installation.
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/common.sh"
[[ $# -eq 0 ]] || die "usage: $0 (no arguments)"
code_server_installed || die "code-server is not installed; run install.sh first"
type="$(linux_type)" || die "unsupported remote DNS patch architecture"
"${HSCS_INSTALL_DIR}/lib/node" "${HSCS_ROOT}/scripts/linux/remote-dns/patch.mjs" \
  "${HSCS_INSTALL_DIR}" "${HSCS_INSTALL_DIR}-remote-dns-v1" "${type}"
