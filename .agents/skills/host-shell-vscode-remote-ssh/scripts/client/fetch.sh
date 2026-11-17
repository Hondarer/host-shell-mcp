#!/bin/sh
# Fetch, on the client, the VS Code CLI and server for one client commit and check them against
# the SHA-256 published by the VS Code update service. Runs on Linux and macOS clients.
# Usage: fetch.sh COMMIT [x64|arm64] [OUT_DIR]
#   COMMIT is the second line of `code --version` on the machine running VS Code.
#   x64 / arm64 is the CPU of the target (default x64).
#   OUT_DIR defaults to the skill's packages/ folder.
# Result: OUT_DIR/<COMMIT>-<ARCH>/{cli.tar.gz,server.tar.gz,SHA256SUMS,COMMIT}; place.sh takes
# that folder. A folder that already verifies is kept as it is.
set -eu
usage() {
  echo "usage: $0 COMMIT [x64|arm64] [OUT_DIR]" >&2
  exit 2
}
C=${1:-}
A=${2:-x64}
OUT=${3:-$(cd "$(dirname "$0")/../.." && pwd)/packages}
case $C in *[!0-9a-f]* | '') usage ;; esac
[ ${#C} -eq 40 ] || usage
case $A in x64 | arm64) ;; *) usage ;; esac
command -v curl >/dev/null 2>&1 || { echo "error: command not found: curl" >&2; exit 1; }

sha256_check() {
  if command -v sha256sum >/dev/null 2>&1; then
    (cd "$1" && sha256sum -c --quiet SHA256SUMS)
  else
    (cd "$1" && shasum -a 256 -c --quiet SHA256SUMS)
  fi
}

D=$OUT/$C-$A
if [ -f "$D/COMMIT" ] && [ -f "$D/SHA256SUMS" ] && sha256_check "$D" 2>/dev/null; then
  echo "verified distribution already present: $D"
  exit 0
fi
API=https://update.code.visualstudio.com/api/versions/commit:$C
mkdir -p "$D"
rm -f "$D/COMMIT" "$D/SHA256SUMS"
: >"$D/SHA256SUMS.tmp"
# The CLI build for Alpine is statically linked and runs on glibc targets too; Remote - SSH uses it.
for pair in "cli-alpine-$A cli.tar.gz" "server-linux-$A server.tar.gz"; do
  set -- $pair
  meta=$(curl -fsS --retry 3 --connect-timeout 20 "$API/$1/stable") ||
    { echo "error: the update service has no $1 build for $C" >&2; exit 1; }
  # The response is one JSON object; pick the fields without a JSON tool.
  url=$(printf '%s' "$meta" | sed -n 's/.*"url":"\([^"]*\)".*/\1/p')
  sha=$(printf '%s' "$meta" | sed -n 's/.*"sha256hash":"\([0-9a-f]\{64\}\)".*/\1/p')
  ver=$(printf '%s' "$meta" | sed -n 's/.*"productVersion":"\([^"]*\)".*/\1/p')
  [ -n "$url" ] && [ -n "$sha" ] || { echo "error: no download information for $1 at $C" >&2; exit 1; }
  echo "downloading $1 $ver"
  curl -fL --retry 3 --retry-delay 2 --connect-timeout 20 -o "$D/$2.part" "$url"
  mv "$D/$2.part" "$D/$2"
  printf '%s  %s\n' "$sha" "$2" >>"$D/SHA256SUMS.tmp"
done
mv "$D/SHA256SUMS.tmp" "$D/SHA256SUMS"
sha256_check "$D" || { echo "error: SHA-256 mismatch in $D" >&2; exit 1; }
printf '%s\n' "$C" >"$D/COMMIT"
echo "saved $D ($(du -sh "$D" | cut -f1))"
