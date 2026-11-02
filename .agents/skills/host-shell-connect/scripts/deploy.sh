#!/bin/sh
# Deploys the host-shell binary from ../bin/ to the target over the standard input of the route (SSH hops,
# optionally ending in a container exec; no SFTP or port forwarding).
# Skips upload if the remote binary already matches the local SHA-256 hash. After upload, verifies the remote
# SHA-256 hash and executes `host-shell --version`; removes the binary if execution fails.
#
# Usage: deploy.sh ROUTE TYPE [REMOTE_PATH]   (ROUTE as in remote.sh)
#   TYPE         target platform from detect.sh (linux-amd64, linux-arm64, windows-amd64, windows-arm64)
#   REMOTE_PATH  destination path on the target (must not contain spaces or quotes).
#                Defaults to user-owned paths tried in order:
#                  Linux:   /tmp/host-shell-<uid>/host-shell, then .local/bin/host-shell (relative to $HOME)
#                  Windows: %TEMP%/host-shell/host-shell.exe, then %LOCALAPPDATA%/Programs/host-shell/host-shell.exe
# On Linux, also places the prebuilt remote DNS adapter from ../bin/ beside the binary as libhostshell_dns.so
# (used with --remote-dns --remote-dns-library) and checks that the target's dynamic loader can preload it.
# Prints a "dns adapter ..." line, then the deployed target path on the final line for use with probe.sh
# and MCP registration.
set -eu

if [ $# -lt 2 ] || [ $# -gt 3 ]; then
	echo "usage: $0 HOST[,HOST...] TYPE [REMOTE_PATH]" >&2
	exit 2
fi
route=$1 type=$2
dir=$(cd "$(dirname "$0")" && pwd)
bin=$(cd "$dir/.." && pwd)/bin

case $type in
linux-amd64 | linux-arm64) os=linux file=$bin/host-shell-$type ;;
windows-amd64 | windows-arm64) os=windows file=$bin/host-shell-$type.exe ;;
*) echo "unknown TYPE: $type (use linux-amd64, linux-arm64, windows-amd64 or windows-arm64)" >&2; exit 2 ;;
esac
if [ ! -f "$file" ]; then
	echo "no such file: $file" >&2
	echo "build it with 'make dist' in the host-shell-mcp repository (it copies the binaries into this skill)." >&2
	exit 1
fi
version=$(cat "$bin/VERSION" 2>/dev/null || echo unknown)

sum_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d' ' -f1
	else
		shasum -a 256 "$1" | cut -d' ' -f1
	fi
}
local_sum=$(sum_of "$file")

remote() { "$dir/remote.sh" "$route" "$1"; }
hash_of() { tr -d '\r' | tr 'A-F' 'a-f' | grep -oE '[0-9a-f]{64}' | head -n 1; }
ps="powershell -NoProfile -NonInteractive -Command"

# Reject target directories that another user could replace or modify (critical under /tmp):
# the directory must not be a symlink, must be owned by the current user or root, and must not be group- or world-writable.
safe_dir='if [ -L "$d" ]; then echo "unsafe directory (symbolic link): $d" >&2; exit 1; fi
set -- $(ls -ldn "$d")
case $1 in ?????w????* | ????????w?*) echo "unsafe directory (writable by group or others): $d" >&2; exit 1 ;; esac
if [ "$3" != "$(id -u)" ] && [ "$3" != 0 ]; then echo "unsafe directory (owned by uid $3): $d" >&2; exit 1; fi'

# Prints "dir-exists" if the parent directory exists, followed by the file's SHA-256 hash if present.
query_cmd() {
	case $os in
	linux) printf 'd=$(dirname %s); if [ -d "$d" ]; then echo dir-exists; { %s\n} && sha256sum %s 2>/dev/null; fi; true' "$1" "$safe_dir" "$1" ;;
	windows) printf '%s "$p=%s; if (Test-Path -PathType Container (Split-Path $p)) { %s }; if (Test-Path $p) { (Get-FileHash $p).Hash }"' "$ps" "'$1'" "'dir-exists'" ;;
	esac
}
# Fails before writing when the target file system has less free space than the file ($size bytes) needs,
# so the next default path (for example outside a small /tmp) is tried instead.
space_check() {
	printf '%s' 'a=$(df -Pk "$d" 2>/dev/null | tail -n 1 | tr -s " " | cut -d" " -f4)
case $a in "" | *[!0-9]*) ;; *) if [ "$a" -lt '"$size_kib"' ]; then echo "not enough disk space in $d: '"$size_kib"' KiB needed, $a KiB available" >&2; exit 1; fi ;; esac'
}
# Writes to a temporary .new file and renames it into place so an active Linux binary can be replaced atomically.
upload_cmd() {
	case $os in
	linux) printf 'd=$(dirname %s); [ -d "$d" ] || mkdir -p -m 700 "$d" || exit 1\n%s\n%s\necho writing; cat > %s.new && chmod 755 %s.new && mv -f %s.new %s && sha256sum %s' "$1" "$safe_dir" "$(space_check)" "$1" "$1" "$1" "$1" "$1" ;;
	windows) printf '%s "$p=%s; New-Item -ItemType Directory -Force (Split-Path $p) | Out-Null; $f=$null; try { $f=(New-Object IO.DriveInfo ([IO.Path]::GetPathRoot($p))).AvailableFreeSpace } catch {}; if ($f -ne $null -and $f -lt %s) { [Console]::Error.WriteLine(%s + [int64]($f / 1KB) + %s); exit 1 }; %s; $i=[Console]::OpenStandardInput(); $o=[IO.File]::Create($p + %s); $i.CopyTo($o); $o.Close(); Move-Item -Force ($p + %s) $p; (Get-FileHash $p).Hash"' "$ps" "'$1'" "$size" "'not enough disk space: $size_kib KiB needed, '" "' KiB available'" "'writing'" "'.new'" "'.new'" ;;
	esac
}
# Removes the binary (and its parent directory if created by this run and empty).
remove_cmd() {
	case $os in
	linux) printf 'rm -f %s %s.new; %s' "$1" "$1" "$([ "$2" = yes ] && echo true || echo "rmdir \$(dirname $1) 2>/dev/null; true")" ;;
	windows) printf '%s "$p=%s; Remove-Item -Force -ErrorAction SilentlyContinue $p, ($p + %s); $d=Split-Path $p; if (%s -and -not (Get-ChildItem -Force $d)) { Remove-Item $d }"' "$ps" "'$1'" "'.new'" "$([ "$2" = yes ] && echo '$false' || echo '$true')" ;;
	esac
}

# put_file uploads local file $1 to remote path $2 unless an identical file is already there ($3 names it in
# messages). Sets status ("up to date" or "deployed") and dir_existed for remove_cmd.
put_file() {
	src_sum=$(sum_of "$1")
	size=$(wc -c <"$1" | tr -d ' ')
	size_kib=$(((size + 1023) / 1024))
	out=$(remote "$(query_cmd "$2")" </dev/null 2>&1 || true)
	dir_existed=no
	case $out in *dir-exists*) dir_existed=yes ;; esac
	remote_sum=$(printf '%s\n' "$out" | hash_of || true)
	if [ "$remote_sum" = "$src_sum" ]; then
		status="up to date"
		return 0
	fi
	echo "sending $3 to $2 ..." >&2
	out=$(remote "$(upload_cmd "$2")" <"$1" || true)
	remote_sum=$(printf '%s\n' "$out" | hash_of || true)
	if [ "$remote_sum" != "$src_sum" ]; then
		echo "could not install at $2 (SHA-256 local $src_sum, remote ${remote_sum:-unknown})" >&2
		# Remove partial artifacts only if upload started (never touch a directory rejected by safe_dir).
		case $out in *writing*) remote "$(remove_cmd "$2" "$dir_existed")" </dev/null >/dev/null 2>&1 || true ;; esac
		return 1
	fi
	status=deployed
}

# deploy_adapter places the prebuilt remote DNS adapter beside the Linux binary at $1. It never fails the
# deployment: without a usable adapter, --remote-dns falls back to compiling one on the target (needs cc).
deploy_adapter() {
	[ "$os" = linux ] || return 0
	lib=$bin/libhostshell_dns-$type.so
	if [ ! -f "$lib" ]; then
		echo "dns adapter unavailable: no $(basename "$lib") in bin/ (build it with 'make dns-lib' or use a release archive)"
		return 0
	fi
	# The adapter targets glibc only; musl's loader would load it without complaint, so check for glibc's loader.
	case $type in
	linux-amd64) loader=/lib64/ld-linux-x86-64.so.2 ;;
	linux-arm64) loader=/lib/ld-linux-aarch64.so.1 ;;
	esac
	if ! remote "test -e $loader" </dev/null >/dev/null 2>&1; then
		echo "dns adapter unavailable: the target has no glibc ($loader); --remote-dns is not supported there"
		return 0
	fi
	adir=$(dirname "$1")
	if ! put_file "$lib" "$adir/libhostshell_dns.so" "remote DNS adapter ($type)"; then
		echo "dns adapter unavailable: transfer failed"
		return 0
	fi
	# Absolute path for --remote-dns-library; a relative binary path resolves from the same login directory.
	apath=$(remote "cd $adir && pwd" </dev/null | tr -d '\r' | tail -n 1)/libhostshell_dns.so
	# The dynamic loader reports (on stderr) a preload library it cannot load, e.g. on musl or an older glibc.
	err=$(remote "LD_PRELOAD=$apath uname 2>&1 >/dev/null" </dev/null 2>&1 || true)
	if [ -n "$err" ]; then
		echo "dns adapter unavailable: the target cannot preload it ($(printf '%s' "$err" | head -n 1))"
		remote "rm -f $apath" </dev/null >/dev/null 2>&1 || true
		return 0
	fi
	echo "dns adapter $status: $apath"
}

# try_path uploads the binary to $1 (if needed) and verifies execution.
try_path() {
	path=$1
	case $path in
	*[[:space:]\'\"]*) echo "skipped $path: the path must not contain spaces or quotes" >&2; return 1 ;;
	esac
	put_file "$file" "$path" "host-shell $version ($type)" || return 1
	if ! remote "$path --version" </dev/null 2>&1 | grep -q '^host-shell '; then
		echo "cannot run $path on the target (e.g. a noexec mount or an execution policy)" >&2
		if [ "$status" = deployed ]; then
			remote "$(remove_cmd "$path" "$dir_existed")" </dev/null >/dev/null 2>&1 || true
			echo "removed $path again" >&2
		fi
		return 1
	fi
	result="$status: $path (version $version, sha256 $local_sum)"
	deploy_adapter "$path"
	echo "$result"
}

if [ $# -eq 3 ]; then
	try_path "$3"
	exit
fi

case $os in
linux)
	uid=$(remote 'id -u' </dev/null | tr -dc '0-9')
	if [ -z "$uid" ]; then
		echo "could not determine the user ID on the target" >&2
		exit 1
	fi
	set -- "/tmp/host-shell-$uid/host-shell" ".local/bin/host-shell"
	;;
windows)
	out=$(remote "$ps \"[IO.Path]::GetTempPath(); \$env:LOCALAPPDATA\"" </dev/null | tr -d '\r' | tr '\\' '/')
	temp=$(printf '%s\n' "$out" | sed -n 1p | sed 's:/*$::')
	appdata=$(printf '%s\n' "$out" | sed -n 2p | sed 's:/*$::')
	if [ -z "$temp" ] || [ -z "$appdata" ]; then
		echo "could not determine %TEMP% and %LOCALAPPDATA% on the target (output: ${out:-none})" >&2
		exit 1
	fi
	set -- "$temp/host-shell/host-shell.exe" "$appdata/Programs/host-shell/host-shell.exe"
	;;
esac
for p in "$@"; do
	if try_path "$p"; then
		exit 0
	fi
done
echo "no default location worked; give REMOTE_PATH explicitly (ask the user for a location they own)" >&2
exit 1
