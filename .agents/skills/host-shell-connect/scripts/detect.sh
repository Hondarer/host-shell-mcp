#!/bin/sh
# 接続先の OS とアーキテクチャを調べ、配布する実行ファイルの種類 (linux-amd64 など) を出力します。
#
# 使い方: detect.sh ROUTE
set -eu

if [ $# -ne 1 ]; then
	echo "usage: $0 HOST[,HOST...]" >&2
	exit 2
fi
dir=$(cd "$(dirname "$0")" && pwd)

# Linux (および uname のある環境)
if out=$("$dir/remote.sh" "$1" 'uname -sm' 2>/dev/null); then
	case $out in
	Linux\ x86_64* | Linux\ amd64*) echo linux-amd64; exit 0 ;;
	Linux\ aarch64* | Linux\ arm64*) echo linux-arm64; exit 0 ;;
	Linux\ *) echo "unsupported Linux architecture: $out" >&2; exit 1 ;;
	esac
fi

# Windows (OpenSSH の既定のシェルの cmd)。Git for Windows などの uname がある場合もここに来る。
out=$("$dir/remote.sh" "$1" 'echo %OS% %PROCESSOR_ARCHITECTURE%' 2>/dev/null | tr -d '\r') || true
case $out in
Windows_NT\ AMD64*) echo windows-amd64; exit 0 ;;
Windows_NT\ ARM64*) echo windows-arm64; exit 0 ;;
esac

echo "could not detect the OS of the host (output: ${out:-none})." >&2
echo "If the sshd default shell on Windows is PowerShell, determine the type manually." >&2
exit 1
