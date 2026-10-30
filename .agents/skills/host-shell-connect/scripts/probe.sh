#!/bin/sh
# 接続先の host-shell を MCP サーバーとして起動し、初期化とツールの一覧の取得に応答するかを確かめます。
# エージェントが実際に使うのと同じ経路 (ssh の標準入出力) で確かめます。
#
# 使い方: probe.sh ROUTE REMOTE_PATH [HOST_SHELL_OPTIONS...]
#
# 環境変数:
#   PROBE_WAIT  応答を待つ秒数 (既定: 5)
set -eu

if [ $# -lt 2 ]; then
	echo "usage: $0 HOST[,HOST...] REMOTE_PATH [OPTIONS...]" >&2
	exit 2
fi
route=$1 path=$2
shift 2
dir=$(cd "$(dirname "$0")" && pwd)

init='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"host-shell-probe","version":"1"}}}'
initialized='{"jsonrpc":"2.0","method":"notifications/initialized"}'
list='{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'

out=$( (printf '%s\n' "$init" "$initialized" "$list"; sleep "${PROBE_WAIT:-5}") |
	"$dir/remote.sh" "$route" "$path --mcp $*" 2>"${TMPDIR:-/tmp}/host-shell-probe.$$" | tr -d '\r') || true
err=$(cat "${TMPDIR:-/tmp}/host-shell-probe.$$" 2>/dev/null || true)
rm -f "${TMPDIR:-/tmp}/host-shell-probe.$$"

first=$(printf '%s\n' "$out" | head -n 1)
case $first in
'{"jsonrpc"'*) ;;
'')
	echo "NG: no response from host-shell." >&2
	[ -n "$err" ] && printf 'stderr:\n%s\n' "$err" >&2
	exit 1
	;;
*)
	echo "NG: the first line is not JSON-RPC. The login shell of the host (or a hop) prints something:" >&2
	printf '%s\n' "$first" | head -c 300 >&2
	echo >&2
	exit 1
	;;
esac

server=$(printf '%s' "$first" | grep -oE '"serverInfo":\{[^}]*\}' || true)
tools=$(printf '%s\n' "$out" | grep '"id":2' | grep -oE '"name":"[a-z_]+"' | sed 's/"name":"//; s/"//' | tr '\n' ' ')
if [ -z "$tools" ]; then
	echo "NG: initialize succeeded but tools/list returned nothing. Try PROBE_WAIT=15." >&2
	exit 1
fi
echo "OK: $server"
echo "tools: $tools"
