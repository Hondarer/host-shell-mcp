#!/bin/sh
# Executes a command on the target over nested `ssh -T` hops, shell-quoting it for each jump host.
# The last element of the route may instead be a container, entered with `exec -i` of its runtime.
#
# Usage: remote.sh ROUTE COMMAND
#   ROUTE   comma-separated hop list from nearest jump host to target (e.g. hop1,target).
#           The last element may be podman:CONTAINER, docker:CONTAINER or kubectl:POD
#           (e.g. podman:web, or hop1,podman:web to enter a container on hop1).
#   COMMAND command line passed to the target host's login shell, or to `sh -c` in a container
# Forwards standard input to the remote command. Intermediate jump hosts must use a POSIX-compatible login shell.
#
# Environment:
#   HOST_SHELL_SSH_OPTS      SSH options applied at each hop (default: -o BatchMode=yes)
#   HOST_SHELL_PODMAN_OPTS   extra options for `podman exec` (e.g. --user dev)
#   HOST_SHELL_DOCKER_OPTS   extra options for `docker exec` (e.g. --user dev)
#   HOST_SHELL_KUBECTL_OPTS  extra options for `kubectl exec` (e.g. -n app -c web)
set -eu

if [ $# -ne 2 ]; then
	echo "usage: $0 HOST[,HOST...][,podman:NAME|docker:NAME|kubectl:POD] COMMAND" >&2
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

# A container element turns the command into an `exec -i` of its runtime; never use -t, which
# would put a terminal between host-shell's JSON-RPC stream and the client.
eval "last=\${$#}"
hops=$#
s=$cmd
case $last in
podman:* | docker:* | kubectl:*)
	runtime=${last%%:*}
	name=${last#*:}
	if [ -z "$name" ]; then
		echo "remote.sh: empty name in route element $last" >&2
		exit 2
	fi
	case $runtime in
	podman) s="podman exec -i ${HOST_SHELL_PODMAN_OPTS:-} $(q "$name") sh -c $(q "$cmd")" ;;
	docker) s="docker exec -i ${HOST_SHELL_DOCKER_OPTS:-} $(q "$name") sh -c $(q "$cmd")" ;;
	kubectl) s="kubectl exec -i ${HOST_SHELL_KUBECTL_OPTS:-} $(q "$name") -- sh -c $(q "$cmd")" ;;
	esac
	hops=$(($# - 1))
	;;
esac
i=1
while [ "$i" -le "$hops" ]; do
	eval "h=\${$i}"
	case $h in
	podman:* | docker:* | kubectl:*)
		echo "remote.sh: a container element must be the last element of the route: $h" >&2
		exit 2
		;;
	esac
	i=$((i + 1))
done
if [ "$hops" -eq 0 ]; then
	# The container runs on this machine.
	eval "exec $s"
fi

# Wrap the command in nested ssh invocations from the target host outward to the first hop.
i=$hops
while [ "$i" -gt 1 ]; do
	eval "h=\${$i}"
	s="ssh -T $opts $h $(q "$s")"
	i=$((i - 1))
done
# shellcheck disable=SC2086
exec ssh -T $opts "$1" "$s"
