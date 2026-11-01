#!/bin/bash
# Remove code-server installed by install.sh. Settings, user data, extensions and the kit stay
# unless --purge is given. Stop code-server (job_stop) first.
# Usage: uninstall.sh [--purge]

set -euo pipefail
# shellcheck source=../lib/common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/common.sh"

purge=0
case "${1:-}" in
  "") ;;
  --purge) purge=1 ;;
  *) die "usage: $0 [--purge]" ;;
esac

if code_server_running; then
  die "code-server is running; stop it (job_stop) before uninstalling"
fi

rm -rf "${HSCS_LIB_DIR}"
echo "removed code-server: ${HSCS_LIB_DIR}"

if [[ "${purge}" -eq 1 ]]; then
  # The kit, including this script, lives under the data folder; bash keeps reading it after removal.
  rm -rf "${HSCS_CONFIG_DIR}" "${HSCS_DATA_DIR}"
  # Only the files code-server wrote outside --user-data-dir; the folder may be shared.
  rm -rf "${HSCS_SHARED_DATA_DIR}/coder-logs"
  rm -f "${HSCS_SHARED_DATA_DIR}/heartbeat"
  rmdir "${HSCS_SHARED_DATA_DIR}" 2>/dev/null || true
  echo "removed the configuration, user data, extensions and kit"
else
  echo "kept the configuration, user data, extensions and kit (use --purge to remove them)"
fi
