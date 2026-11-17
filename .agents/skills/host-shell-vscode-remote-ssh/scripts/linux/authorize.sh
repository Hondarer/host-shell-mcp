#!/bin/sh
# Register the client public key for the inner sshd and print the sshd host public key, which the
# client records in its known_hosts. The host key is created on the first run and kept afterwards.
# Usage: authorize.sh "PUBLIC_KEY_LINE"   (replaces the registered key)
#        authorize.sh                     (keeps the registered key; prints the host key)
set -eu
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

require_cmd ssh-keygen
umask 077
mkdir -p "$HSVR_SSHD_DIR"
chmod 700 "$HSVR_SSHD_DIR"
key=$HSVR_SSHD_DIR/ssh_host_ed25519_key
[ -f "$key" ] || ssh-keygen -q -t ed25519 -N '' -C "$HSVR_NAME" -f "$key"

if [ $# -ge 1 ]; then
  case $1 in
    *'
'*) die "pass exactly one public key line" ;;
    ssh-ed25519\ * | ecdsa-sha2-*\ * | ssh-rsa\ * | sk-ssh-ed25519@openssh.com\ * | sk-ecdsa-sha2-*\ *) ;;
    *) die "not an OpenSSH public key line: pass the contents of the client's .pub file" ;;
  esac
  printf '%s\n' "$1" >"$HSVR_SSHD_DIR/authorized_keys.tmp"
  ssh-keygen -l -f "$HSVR_SSHD_DIR/authorized_keys.tmp" >/dev/null 2>&1 ||
    { rm -f "$HSVR_SSHD_DIR/authorized_keys.tmp"; die "ssh-keygen rejected the public key"; }
  mv "$HSVR_SSHD_DIR/authorized_keys.tmp" "$HSVR_SSHD_DIR/authorized_keys"
  echo "authorized key: $(ssh-keygen -l -f "$HSVR_SSHD_DIR/authorized_keys")"
fi
[ -f "$HSVR_SSHD_DIR/authorized_keys" ] || die "no authorized key yet; pass the client public key"
chmod 600 "$HSVR_SSHD_DIR/authorized_keys"
echo "host key: $(cat "$key.pub")"
