#!/bin/sh
# Executes a command on the target host over SSH, nesting and shell-quoting `ssh -T` across any intermediate jump hosts.
#
# Usage: remote.sh ROUTE COMMAND
#   ROUTE   comma-separated hop list from nearest jump host to target (e.g. hop1,target)
#   COMMAND command line passed to the target host's login shell
# Forwards standard input to the remote command. Intermediate jump hosts must use a POSIX-compatible login shell.
#
# Environment:
#   HOST_SHELL_SSH_OPTS  SSH options applied at each hop (default: -o BatchMode=yes)
set -eu

if [ $# -ne 2 ]; then
	echo "usage: $0 HOST[,HOST...] COMMAND" >&2
	exit 2
fi
route=$1
cmd=$2
opts=${HOST_SHELL_SSH_OPTS:--o BatchMode=yes}

# q single-quotes its argument for safe evaluation by a POSIX shell.
q() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

# shellcheck disable=SC2046
set -- $(printf '%s' "$route" | tr ',' ' ')
if [ $# -eq 0 ]; then
	echo "remote.sh: empty route" >&2
	exit 2
fi

# Wrap the command in nested ssh invocations from the target host outward to the first hop.
s=$cmd
i=$#
while [ "$i" -gt 1 ]; do
	eval "h=\${$i}"
	s="ssh -T $opts $h $(q "$s")"
	i=$((i - 1))
done
# shellcheck disable=SC2086
exec ssh -T $opts "$1" "$s"
