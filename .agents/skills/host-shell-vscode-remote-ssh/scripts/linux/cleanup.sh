#!/bin/sh
# Stop the VS Code Server that Remote - SSH leaves running and remove its runtime files in the
# temporary directory.
# The server outlives the host-shell session, so run this at the start (start.sh does) and at the end.
# Usage: cleanup.sh [--purge]
#   --purge also deletes ~/.vscode-server (placed versions, extensions, user data) and this skill's
#   folder (kit, sshd configuration and keys). ~/.copilot is left alone because Copilot CLI shares it.
set -eu
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

uid=$(id -u)
if pkill -u "$uid" -f "$HSVR_PROC_PATTERN"; then
  sleep 2
  pkill -9 -u "$uid" -f "$HSVR_PROC_PATTERN" 2>/dev/null || true
fi
echo "VS Code Server processes left: $(pgrep -u "$uid" -f "$HSVR_PROC_PATTERN" | wc -l)"
# Sockets and folders that the server, the agent host and the terminals create in the temporary
# directory. Removed only when no VS Code Server of this HOME is left, since they are named by
# random IDs rather than by HOME.
if vscode_running; then
  echo "kept $HSVR_TMP_DIR entries: VS Code Server processes are still running" >&2
else
  n=$(find "$HSVR_TMP_DIR" -maxdepth 1 -user "$uid" \( -name 'code-*' -o -name 'vscode-*' \) | wc -l)
  find "$HSVR_TMP_DIR" -maxdepth 1 -user "$uid" \( -name 'code-*' -o -name 'vscode-*' \) -exec rm -rf {} +
  echo "removed $HSVR_TMP_DIR entries: $n"
fi

if [ "${1:-}" = "--purge" ]; then
  du -sh "$HSVR_VSCODE_DIR" "$HSVR_DATA_DIR" 2>/dev/null || true
  rm -rf "$HSVR_VSCODE_DIR" "$HSVR_DATA_DIR"
  echo "purged $HSVR_VSCODE_DIR and $HSVR_DATA_DIR"
fi
