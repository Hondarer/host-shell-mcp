#!/bin/sh
# Offline tests for the host-shell-vscode-remote-ssh scripts. They run the target and client
# scripts against a temporary HOME and TMPDIR with fake VS Code archives and a fake sshd; no
# network, real sshd or VS Code is used. Requires sh, tar, gzip, sha256sum, pgrep and ssh-keygen.
# cleanup.sh stops processes, so run the tests in a disposable container, for example:
#   podman run --rm -v "$PWD/.agents/skills/host-shell-vscode-remote-ssh:/skill:ro" IMAGE \
#     sh /skill/tests/scripts.test.sh
# Usage: sh tests/scripts.test.sh
set -eu
SKILL=$(cd "$(dirname "$0")/.." && pwd)
T=$(mktemp -d)
trap 'rm -rf "$T"' EXIT
export HOME=$T/home TMPDIR=$T/tmp
mkdir -p "$HOME" "$TMPDIR"
fails=0
pass() { echo "ok   $*"; }
fail() {
  echo "FAIL $*"
  fails=$((fails + 1))
}
# check and check_fails keep the output of the command in $T/out; $T/last holds the output of
# the previous check, so that a check can inspect it.
run() {
  [ -f "$T/out" ] && mv "$T/out" "$T/last"
  "$@" >"$T/out" 2>&1
}
check() { # check DESCRIPTION COMMAND...
  d=$1
  shift
  if run "$@"; then pass "$d"; else
    fail "$d"
    sed 's/^/     /' "$T/out"
  fi
}
check_fails() { # check_fails DESCRIPTION PATTERN COMMAND...
  d=$1
  p=$2
  shift 2
  if run "$@"; then
    fail "$d (succeeded)"
  elif grep -q -- "$p" "$T/out"; then pass "$d"; else
    fail "$d (no \"$p\")"
    sed 's/^/     /' "$T/out"
  fi
}

# make_dist COMMIT VERSION: a folder in the layout fetch.sh writes.
make_dist() {
  d=$T/dist/$1-x64
  mkdir -p "$d" "$T/build/$1/cli" "$T/build/$1/srv/vscode-server-linux-x64"
  printf '#!/bin/sh\necho "code %s (commit %s)"\n' "$2" "$1" >"$T/build/$1/cli/code"
  chmod 755 "$T/build/$1/cli/code"
  printf '{\n  "commit": "%s",\n  "quality": "stable"\n}\n' "$1" >"$T/build/$1/srv/vscode-server-linux-x64/product.json"
  printf '{\n  "version": "%s"\n}\n' "$2" >"$T/build/$1/srv/vscode-server-linux-x64/package.json"
  tar -C "$T/build/$1/cli" -czf "$d/cli.tar.gz" code
  tar -C "$T/build/$1/srv" -czf "$d/server.tar.gz" vscode-server-linux-x64
  (cd "$d" && sha256sum cli.tar.gz server.tar.gz >SHA256SUMS)
  echo "$1" >"$d/COMMIT"
}

L=$SKILL/scripts/linux
A=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
B=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
make_dist $A 1.0.0
make_dist $B 1.0.1
V=$HOME/.vscode-server

# place.sh
check "place first version" sh "$L/place.sh" "$T/dist/$A-x64"
check "wrapper pins its own commit" grep -q "^VSCODE_CLI_INITIAL_AH_VERSION=$A exec \"$V/host-shell-cli/code-$A\"" "$V/code-$A"
check "wrapper runs the CLI" sh -c "'$V/code-$A' --version | grep -q 'commit $A'"
check "server extracted" test -f "$V/cli/servers/Stable-$A/server/product.json"
check "place second version" sh "$L/place.sh" "$T/dist/$B-x64"
check "lru.json lists the newest first" test "$(cat "$V/cli/servers/lru.json")" = "[\"Stable-$B\",\"Stable-$A\"]"
check "status reports both without warnings" sh -c "sh '$L/status.sh' | grep -c 'cli=wrapper' | grep -qx 2"

touch "$V/cli/servers/Stable-$A/server/marker"
check "placing again keeps the placed server" sh -c "sh '$L/place.sh' '$T/dist/$A-x64' | grep -q 'kept the placed server' && test -f '$V/cli/servers/Stable-$A/server/marker'"
check "placing again puts the version first" test "$(cat "$V/cli/servers/lru.json")" = "[\"Stable-$A\",\"Stable-$B\"]"
rm -f "$V/cli/servers/Stable-$A/server/product.json"
check "an incomplete server is placed again" sh -c "sh '$L/place.sh' '$T/dist/$A-x64' | grep -q '^placed Stable-$A' && ! test -f '$V/cli/servers/Stable-$A/server/marker'"
cp -r "$T/dist/$A-x64" "$T/bad"
printf 'x' >>"$T/bad/cli.tar.gz"
check_fails "place rejects a SHA-256 mismatch" "SHA-256 mismatch" sh "$L/place.sh" "$T/bad"
check_fails "place rejects a missing file" "missing" sh "$L/place.sh" "$T/nowhere"
make_dist dddddddddddddddddddddddddddddddddddddddd 1.0.2
check_fails "place checks free space" "not enough disk space" env HSVR_PLACE_MIB=999999999 sh "$L/place.sh" "$T/dist/dddddddddddddddddddddddddddddddddddddddd-x64"

# status.sh warnings
mkdir -p "$V/cli/servers/Stable-cccccccccccccccccccccccccccccccccccccccc/server"
check_fails "status warns about a server without CLI" "has no CLI" sh "$L/status.sh"
rm -rf "$V/cli/servers/Stable-cccccccccccccccccccccccccccccccccccccccc"
printf '#!/bin/sh\n' >"$V/code-$A.real"
cp "$V/code-$A" "$T/wrapper-a"
cp "$V/code-$A.real" "$V/code-$A"
chmod 755 "$V/code-$A"
check_fails "status warns about an unwrapped CLI" "not a place.sh wrapper" sh "$L/status.sh"
cp "$T/wrapper-a" "$V/code-$A"
rm -f "$V/code-$A.real"
check "status is clean again" sh "$L/status.sh"

# place.sh --prune
check_fails "prune rejects an unplaced commit" "not placed" sh "$L/place.sh" --prune cccccccccccccccccccccccccccccccccccccccc
check_fails "prune rejects a short commit" "invalid commit" sh "$L/place.sh" --prune abc
check "prune keeps only the given commit" sh "$L/place.sh" --prune $B
check "pruned version is gone" sh -c "! test -e '$V/cli/servers/Stable-$A' && ! test -e '$V/code-$A' && ! test -e '$V/host-shell-cli/code-$A'"
check "lru.json lists the kept version only" test "$(cat "$V/cli/servers/lru.json")" = "[\"Stable-$B\"]"

# authorize.sh
ssh-keygen -q -t ed25519 -N '' -C test -f "$T/client_key"
check_fails "authorize needs a key the first time" "no authorized key" sh "$L/authorize.sh"
check_fails "authorize rejects a private key" "not an OpenSSH public key" sh "$L/authorize.sh" "$(head -n 1 "$T/client_key")"
check_fails "authorize rejects a broken key" "rejected" sh "$L/authorize.sh" "ssh-ed25519 AAAAbroken test"
check "authorize registers the key" sh "$L/authorize.sh" "$(cat "$T/client_key.pub")"
check "authorize prints the host key" grep -q '^host key: ssh-ed25519 ' "$T/last"
hostkey=$(sed -n 's/^host key: //p' "$T/last")
[ -n "$hostkey" ] || fail "host key captured"
S=$HOME/.local/share/host-shell-vscode-remote-ssh/sshd
check "authorized_keys and host key are private" sh -c "test \$(stat -c %a '$S/authorized_keys') = 600 && test \$(stat -c %a '$S') = 700"
check "authorize keeps the host key" sh -c "sh '$L/authorize.sh' | grep -qF '$hostkey'"

# start.sh with a fake sshd that records how it was called
cat >"$T/sshd" <<'EOF'
#!/bin/sh
echo "fake-sshd $*" >>"$HOME/sshd.calls"
EOF
chmod 755 "$T/sshd"
check "start without proxy" env -u HTTPS_PROXY HSVR_SSHD="$T/sshd" sh "$L/start.sh"
check "start runs sshd in the foreground" grep -q "^fake-sshd -D -e -f $S/sshd_config\$" "$HOME/sshd.calls"
check "start validates the config first" grep -q "^fake-sshd -t -f $S/sshd_config.tmp\$" "$HOME/sshd.calls"
check "config listens on loopback port 2222" sh -c "grep -qx 'Port 2222' '$S/sshd_config' && grep -qx 'ListenAddress 127.0.0.1' '$S/sshd_config'"
check "config allows public keys only" sh -c "grep -qx 'PasswordAuthentication no' '$S/sshd_config' && grep -qx 'KbdInteractiveAuthentication no' '$S/sshd_config' && ! grep -q '^UsePAM' '$S/sshd_config'"
check "config has no SetEnv and no sftp" sh -c "! grep -q '^SetEnv' '$S/sshd_config' && ! grep -q '^Subsystem' '$S/sshd_config'"
check "start with proxy and port" env HTTPS_PROXY=http://u:p@127.0.0.1:40000 HSVR_SSHD_PORT=2300 HSVR_SSHD="$T/sshd" sh "$L/start.sh"
check "config passes the proxy" grep -q '^SetEnv HTTPS_PROXY=http://u:p@127.0.0.1:40000 .*NO_PROXY=127.0.0.1,localhost,::1' "$S/sshd_config"
check "config uses the given port" grep -qx 'Port 2300' "$S/sshd_config"
check "start does not print the proxy URL" sh -c "! grep -q 'u:p@' '$T/last'"
check "start with --commit" env -u HTTPS_PROXY HSVR_SSHD="$T/sshd" sh "$L/start.sh" --commit $B
check "config pins the agent host" grep -q "^SetEnv VSCODE_CLI_INITIAL_AH_VERSION=$B\$" "$S/sshd_config"
check "status reports the pin" sh -c "sh '$L/status.sh' | grep -qx 'pin in sshd config: $B'"
check_fails "start rejects a bad commit" "invalid commit" env HSVR_SSHD="$T/sshd" sh "$L/start.sh" --commit xyz
check_fails "start needs sshd" "sshd not found" env HSVR_SSHD="$T/missing" sh "$L/start.sh"

# cleanup.sh: a fake server under this HOME is stopped; one under another HOME is left running.
mkdir -p "$V/fake" "$T/other/.vscode-server/fake"
for d in "$V/fake" "$T/other/.vscode-server/fake"; do
  # A link to sleep keeps the path as the first word of the command line, as a real server has.
  ln -s "$(command -v sleep)" "$d/server"
  "$d/server" 60 >/dev/null 2>&1 &
done
sleep 1
check "status counts the server of this HOME only" sh -c "sh '$L/status.sh' | grep -qx 'VS Code processes: 1'"
check "cleanup stops the server of this HOME" sh -c "sh '$L/cleanup.sh' | grep -qx 'VS Code Server processes left: 0'"
check "cleanup leaves another HOME alone" pgrep -f "^$T/other/\.vscode-server/fake/server"
pkill -f "^$T/other/\.vscode-server/fake/server" || true
rm -rf "$V/fake" "$T/other"
mkdir -p "$TMPDIR/vscode-ipc-test" "$TMPDIR/code-test"
check "cleanup removes VS Code entries in TMPDIR" sh -c "sh '$L/cleanup.sh' | grep -qx 'removed $TMPDIR entries: 2'"
check "cleanup without purge keeps files" sh -c "sh '$L/cleanup.sh' && test -d '$V' && test -d '$S'"
mkdir -p "$HOME/.copilot"
check "cleanup --purge removes the server and skill data" sh -c "sh '$L/cleanup.sh' --purge && ! test -e '$V' && ! test -e '$HOME/.local/share/host-shell-vscode-remote-ssh'"
check "cleanup --purge keeps ~/.copilot" test -d "$HOME/.copilot"

# client ssh-key.sh
C=$SKILL/scripts/client
check "ssh-key creates a key" sh "$C/ssh-key.sh" key t1
check "ssh-key prints the public key" grep -q '^ssh-ed25519 .* host-shell-vscode-t1$' "$T/last"
pub1=$(grep '^ssh-ed25519' "$T/last" || true)
[ -n "$pub1" ] || fail "public key captured"
check "ssh-key keeps an existing key" sh -c "sh '$C/ssh-key.sh' key t1 | grep -qF '$pub1'"
check "ssh-key trust writes known_hosts" sh "$C/ssh-key.sh" trust t1 42222 dev "host key: $hostkey"
check "ssh-key prints the Host block" sh -c "grep -qx 'Host host-shell-vscode-t1' '$T/last' && grep -qx '    Port 42222' '$T/last' && grep -qx '    StrictHostKeyChecking yes' '$T/last'"
check "known_hosts names the forwarded port" grep -q "^\[127.0.0.1\]:42222 ssh-ed25519 [A-Za-z0-9+/=]*\$" "$HOME/.ssh/host-shell-vscode-t1_known_hosts"
check_fails "ssh-key rejects a bad name" "usage" sh "$C/ssh-key.sh" key "a b"
check_fails "ssh-key rejects a non-ed25519 host key" "HOST_KEY" sh "$C/ssh-key.sh" trust t1 42222 dev "ssh-rsa AAAA"

# fetch.sh argument checks (no network)
check_fails "fetch rejects a short commit" "usage" sh "$C/fetch.sh" abc
check_fails "fetch rejects an unknown arch" "usage" sh "$C/fetch.sh" $A riscv64
mkdir -p "$T/fetched"
cp -r "$T/dist/$A-x64" "$T/fetched/"
check "fetch keeps a verified folder" sh -c "sh '$C/fetch.sh' $A x64 '$T/fetched' | grep -q 'already present'"

# The scripts stay ASCII-only.
check "scripts are ASCII-only" sh -c "! LC_ALL=C grep -n '[^[:print:][:space:]]' '$SKILL'/scripts/*/*"

echo "failures: $fails"
[ "$fails" -eq 0 ]
