#!/bin/sh
# Detects the target's OS and architecture and prints the executable type to deploy (linux-amd64 etc.).
#
# Usage: detect.sh ROUTE
set -eu

if [ $# -ne 1 ]; then
	echo "usage: $0 HOST[,HOST...]" >&2
	exit 2
fi
dir=$(cd "$(dirname "$0")" && pwd)

# Linux (and other systems with uname)
if out=$("$dir/remote.sh" "$1" 'uname -sm' 2>/dev/null); then
	case $out in
	Linux\ x86_64* | Linux\ amd64*) echo linux-amd64; exit 0 ;;
	Linux\ aarch64* | Linux\ arm64*) echo linux-arm64; exit 0 ;;
	Linux\ *) echo "unsupported Linux architecture: $out" >&2; exit 1 ;;
	esac
fi

# Windows (cmd, the default OpenSSH shell). Also reached when uname exists, e.g. from Git for Windows.
out=$("$dir/remote.sh" "$1" 'echo %OS% %PROCESSOR_ARCHITECTURE%' 2>/dev/null | tr -d '\r') || true
case $out in
Windows_NT\ AMD64*) echo windows-amd64; exit 0 ;;
Windows_NT\ ARM64*) echo windows-arm64; exit 0 ;;
esac

echo "could not detect the OS of the host (output: ${out:-none})." >&2
echo "If the sshd default shell on Windows is PowerShell, determine the type manually." >&2
exit 1
