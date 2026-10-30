#!/bin/sh
# Sends the host-shell executable to the target over ssh's standard input. Uses neither SFTP nor port forwarding.
# Skips sending when the target already has a file with the same content. After sending, compares SHA-256 with the local file.
#
# Usage: deploy.sh ROUTE TYPE REMOTE_PATH
#   TYPE         target type, as printed by detect.sh (linux-amd64, linux-arm64, windows-amd64, windows-arm64)
#   REMOTE_PATH  install path on the target; must not contain spaces or quotes (e.g. /opt/host-shell/host-shell, C:/tools/host-shell.exe)
# The executable sent is the one in this skill's bin/.
set -eu

if [ $# -ne 3 ]; then
	echo "usage: $0 HOST[,HOST...] TYPE REMOTE_PATH" >&2
	exit 2
fi
route=$1 type=$2 path=$3
dir=$(cd "$(dirname "$0")" && pwd)
bin=$(cd "$dir/.." && pwd)/bin

case $type in
linux-amd64 | linux-arm64) os=linux file=$bin/host-shell-$type ;;
windows-amd64 | windows-arm64) os=windows file=$bin/host-shell-$type.exe ;;
*) echo "unknown TYPE: $type (use linux-amd64, linux-arm64, windows-amd64 or windows-arm64)" >&2; exit 2 ;;
esac
case $path in
*[[:space:]\'\"]*) echo "REMOTE_PATH must not contain spaces or quotes: $path" >&2; exit 2 ;;
esac
if [ ! -f "$file" ]; then
	echo "no such file: $file" >&2
	echo "build it with 'make dist' in the host-shell-mcp repository (it copies the binaries into this skill)." >&2
	exit 1
fi
version=$(cat "$bin/VERSION" 2>/dev/null || echo unknown)

if command -v sha256sum >/dev/null 2>&1; then
	local_sum=$(sha256sum "$file" | cut -d' ' -f1)
else
	local_sum=$(shasum -a 256 "$file" | cut -d' ' -f1)
fi

case $os in
linux)
	query="sha256sum $path 2>/dev/null || true"
	# Write to a temporary file and rename it, so a running host-shell can be replaced.
	upload="mkdir -p \$(dirname $path) && cat > $path.new && chmod 755 $path.new && mv -f $path.new $path && sha256sum $path"
	;;
windows)
	ps="powershell -NoProfile -NonInteractive -Command"
	query="$ps \"if (Test-Path '$path') { (Get-FileHash '$path').Hash }\""
	upload="$ps \"\$p='$path'; New-Item -ItemType Directory -Force (Split-Path \$p) | Out-Null; \$i=[Console]::OpenStandardInput(); \$o=[IO.File]::Create(\$p + '.new'); \$i.CopyTo(\$o); \$o.Close(); Move-Item -Force (\$p + '.new') \$p; (Get-FileHash \$p).Hash\""
	;;
esac

hash_of() { tr -d '\r' | tr 'A-F' 'a-f' | grep -oE '[0-9a-f]{64}' | head -n 1; }

remote_sum=$("$dir/remote.sh" "$route" "$query" </dev/null | hash_of || true)
if [ "$remote_sum" = "$local_sum" ]; then
	echo "up to date: $path (version $version, sha256 $local_sum)"
	exit 0
fi

echo "sending host-shell $version ($type) to $path ..." >&2
remote_sum=$("$dir/remote.sh" "$route" "$upload" <"$file" | hash_of || true)
if [ "$remote_sum" != "$local_sum" ]; then
	echo "SHA-256 mismatch: local $local_sum, remote ${remote_sum:-unknown}" >&2
	exit 1
fi
echo "deployed: $path (version $version, sha256 $local_sum)"
