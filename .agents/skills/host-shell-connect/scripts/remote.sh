#!/bin/sh
# 接続先でコマンドを実行します。中継サーバーを経由する場合は ssh を入れ子にし、各段の引用符を組み立てます。
#
# 使い方: remote.sh ROUTE COMMAND
#   ROUTE   経由するホストと接続先を、手元に近い順にカンマで区切ったもの (例: hop1,target)
#   COMMAND 接続先のシェルが解釈するコマンド行
# 標準入力は接続先のコマンドに渡します。中継サーバーのシェルは POSIX sh 互換であることを前提とします。
#
# 環境変数:
#   HOST_SHELL_SSH_OPTS  各段の ssh に渡すオプション (既定: -o BatchMode=yes)
set -eu

if [ $# -ne 2 ]; then
	echo "usage: $0 HOST[,HOST...] COMMAND" >&2
	exit 2
fi
route=$1
cmd=$2
opts=${HOST_SHELL_SSH_OPTS:--o BatchMode=yes}

# q は、POSIX sh が 1 つの引数として解釈するよう、単一引用符で囲みます。
q() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

# shellcheck disable=SC2046
set -- $(printf '%s' "$route" | tr ',' ' ')
if [ $# -eq 0 ]; then
	echo "remote.sh: empty route" >&2
	exit 2
fi

# 内側 (接続先) から順に、1 つ手前のホストで実行するコマンド行を組み立てる。
s=$cmd
i=$#
while [ "$i" -gt 1 ]; do
	eval "h=\${$i}"
	s="ssh -T $opts $h $(q "$s")"
	i=$((i - 1))
done
# shellcheck disable=SC2086
exec ssh -T $opts "$1" "$s"
