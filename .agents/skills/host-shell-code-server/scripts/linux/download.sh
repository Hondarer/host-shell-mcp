#!/bin/bash
# Fetch, on the client, everything install needs offline on a target of each TYPE:
# the code-server release archive and the default extensions in defaults/extensions.txt.
# Both are verified by SHA-256 and stored under packages/. Runs on Linux and macOS clients.
# Usage: download.sh TYPE...   (TYPE: linux-amd64, linux-arm64, windows-amd64)

set -euo pipefail
# shellcheck source=../lib/common.sh
. "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/../lib/common.sh"

usage() {
  echo "usage: $0 TYPE...   (TYPE: linux-amd64, linux-arm64, windows-amd64)" >&2
  exit 2
}
[[ $# -gt 0 ]] || usage
for type in "$@"; do
  asset_sha256 "${type}" >/dev/null || usage
done

require_cmd curl python3
command -v sha256sum >/dev/null 2>&1 || require_cmd shasum

for type in "$@"; do
  name="$(asset_name "${type}")"
  dest="${HSCS_PACKAGES_DIR}/${name}"
  sha="$(asset_sha256 "${type}")"
  if file_sha256_matches "${dest}" "${sha}"; then
    echo "verified archive already present: ${dest}"
  else
    echo "downloading ${HSCS_RELEASE_URL}/${name}"
    mkdir -p "${HSCS_PACKAGES_DIR}"
    rm -f "${dest}.partial"
    curl -fL --retry 3 --retry-delay 2 --connect-timeout 20 -o "${dest}.partial" "${HSCS_RELEASE_URL}/${name}"
    if ! file_sha256_matches "${dest}.partial" "${sha}"; then
      rm -f "${dest}.partial"
      die "SHA-256 mismatch: ${name}"
    fi
    mv "${dest}.partial" "${dest}"
    echo "saved ${dest}"
  fi
  python3 "${HSCS_ROOT}/scripts/lib/resolve_extensions.py" --type "${type}" \
    --vscode "${CODE_SERVER_VSCODE_VERSION}" --list "${HSCS_DEFAULTS_DIR}/extensions.txt" \
    --out "${HSCS_PACKAGES_DIR}/extensions/${type}"
done
