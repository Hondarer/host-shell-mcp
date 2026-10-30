#!/bin/sh
# Runs a command on the target. Through jump hosts, nests ssh and builds the quoting for each hop.
#
# Usage: remote.sh ROUTE COMMAND
#   ROUTE   hops and the target, nearest first, separated by commas (e.g. hop1,target)
#   COMMAND command line interpreted by the target's shell
# Standard input is passed to the command on the target. Jump hosts are assumed to have a POSIX sh compatible shell.
#
# Environment:
#   HOST_SHELL_SSH_OPTS  options passed to ssh at each hop (default: -o BatchMode=yes)
set -eu

if [ $# -ne 2 ]; then
	echo "usage: $0 HOST[,HOST...] COMMAND" >&2
	exit 2
fi
route=$1
cmd=$2
opts=${HOST_SHELL_SSH_OPTS:--o BatchMode=yes}

# q wraps its argument in single quotes so that POSIX sh reads it as one argument.
q() {
	printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"
}

# shellcheck disable=SC2046
set -- $(printf '%s' "$route" | tr ',' ' ')
if [ $# -eq 0 ]; then
	echo "remote.sh: empty route" >&2
	exit 2
fi

# Build the command line to run on each previous host, from the innermost (the target) outwards.
s=$cmd
i=$#
while [ "$i" -gt 1 ]; do
	eval "h=\${$i}"
	s="ssh -T $opts $h $(q "$s")"
	i=$((i - 1))
done
# shellcheck disable=SC2086
exec ssh -T $opts "$1" "$s"
