#!/bin/sh
# Shared settings and helpers for the host-shell-vscode-remote-ssh target scripts; sourced, not run.
# The scripts use POSIX sh so that they run on minimal Linux targets.

HSVR_NAME=host-shell-vscode-remote-ssh
HSVR_DATA_DIR=$HOME/.local/share/$HSVR_NAME
HSVR_SSHD_DIR=$HSVR_DATA_DIR/sshd
HSVR_SSHD_CONFIG=$HSVR_SSHD_DIR/sshd_config
# The inner sshd listens on 127.0.0.1 only; the bridge rule --forward-allow-local must name this port.
HSVR_SSHD_PORT=${HSVR_SSHD_PORT:-2222}
HSVR_SSHD=${HSVR_SSHD:-/usr/sbin/sshd}

# Layout that Remote - SSH and the VS Code CLI use on the target.
HSVR_VSCODE_DIR=$HOME/.vscode-server
HSVR_SERVERS_DIR=$HSVR_VSCODE_DIR/cli/servers
HSVR_LRU=$HSVR_SERVERS_DIR/lru.json
# The real CLI binaries; $HSVR_VSCODE_DIR/code-<C> is a wrapper that execs them.
HSVR_CLI_DIR=$HSVR_VSCODE_DIR/host-shell-cli
# Disk space (MiB) that place.sh needs for one version: the extracted server (641 MiB measured
# with 1.144.1 linux-x64) and CLI (34 MiB), with headroom.
HSVR_PLACE_MIB=${HSVR_PLACE_MIB:-768}

# VS Code creates its sockets and folders in the temporary directory of Node.js (TMPDIR or /tmp).
HSVR_TMP_DIR=${TMPDIR:-/tmp}

# Programs under this user's ~/.vscode-server and the terminal shells they start. Matching the
# start of the command line keeps a caller whose arguments merely mention the path out of the
# match, and naming $HOME keeps other VS Code installations of the same user (such as a
# Remote - WSL server with a different HOME) out of it.
hsvr_home_re=$(printf '%s' "$HSVR_VSCODE_DIR" | sed 's/[][\.*^$+?(){}|]/\\&/g')
HSVR_PROC_PATTERN="^($hsvr_home_re/|[^ ]*bash --init-file $hsvr_home_re/)"

die() {
  echo "error: $*" >&2
  exit 1
}

require_cmd() {
  for c in "$@"; do
    command -v "$c" >/dev/null 2>&1 || die "command not found: $c"
  done
}

# check_commit C: C must be a full VS Code commit (40 lowercase hex digits).
check_commit() {
  case $1 in
    *[!0-9a-f]* | '') die "invalid commit (expected 40 lowercase hex digits): $1" ;;
  esac
  [ ${#1} -eq 40 ] || die "invalid commit (expected 40 lowercase hex digits): $1"
}

# free_mib DIR prints the free space of the file system holding DIR (or its nearest existing parent).
free_mib() {
  d=$1
  while [ ! -d "$d" ]; do d=$(dirname "$d"); done
  df -Pk "$d" | awk 'NR == 2 { print int($4 / 1024) }'
}

vscode_running() {
  pgrep -u "$(id -u)" -f "$HSVR_PROC_PATTERN" >/dev/null 2>&1
}

# wrapped C succeeds when ~/.vscode-server/code-C is a wrapper written by place.sh.
wrapped() {
  grep -q '^VSCODE_CLI_INITIAL_AH_VERSION=' "$HSVR_VSCODE_DIR/code-$1" 2>/dev/null
}
