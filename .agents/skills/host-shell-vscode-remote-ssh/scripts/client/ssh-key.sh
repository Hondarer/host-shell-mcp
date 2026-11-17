#!/bin/sh
# Create, on the machine that runs VS Code (Linux or macOS), the dedicated SSH key and known_hosts
# for one target, and print the ~/.ssh/config block for the user to add. ~/.ssh/config itself is
# never modified.
# Usage: ssh-key.sh key NAME
#          create ~/.ssh/host-shell-vscode-NAME_ed25519 (kept when present) and print its public key
#        ssh-key.sh trust NAME LISTEN_PORT USER "HOST_KEY"
#          write ~/.ssh/host-shell-vscode-NAME_known_hosts for 127.0.0.1:LISTEN_PORT with HOST_KEY
#          (the "host key:" line of authorize.sh) and print the Host block
set -eu
usage() {
  echo "usage: $0 key NAME | trust NAME LISTEN_PORT USER \"HOST_KEY\"" >&2
  exit 2
}
mode=${1:-}
name=${2:-}
case $name in '' | *[!A-Za-z0-9._-]*) usage ;; esac
alias=host-shell-vscode-$name
dir=$HOME/.ssh
key=$dir/${alias}_ed25519
known=$dir/${alias}_known_hosts
mkdir -p "$dir"
chmod 700 "$dir"

case $mode in
  key)
    [ $# -eq 2 ] || usage
    if [ -f "$key" ]; then
      echo "key exists: $key" >&2
    else
      ssh-keygen -q -t ed25519 -N '' -C "$alias" -f "$key"
      echo "created: $key" >&2
    fi
    cat "$key.pub"
    ;;
  trust)
    [ $# -eq 5 ] || usage
    port=$3
    user=$4
    case $port in '' | *[!0-9]*) usage ;; esac
    case $user in '' | *[!A-Za-z0-9._-]*) usage ;; esac
    # Keep the key type and the key; drop the comment.
    hk=$(printf '%s\n' "$5" | sed 's/^host key: *//' | awk 'NF >= 2 { print $1, $2; exit }')
    case $hk in ssh-ed25519\ *) ;; *) echo "error: HOST_KEY must be the ssh-ed25519 line from authorize.sh" >&2; exit 2 ;; esac
    umask 077
    printf '[127.0.0.1]:%s %s\n' "$port" "$hk" >"$known"
    echo "wrote: $known" >&2
    cat <<BLOCK
Host $alias
    HostName 127.0.0.1
    Port $port
    User $user
    IdentityFile ~/.ssh/${alias}_ed25519
    IdentitiesOnly yes
    UserKnownHostsFile ~/.ssh/${alias}_known_hosts
    StrictHostKeyChecking yes
BLOCK
    ;;
  *) usage ;;
esac
