#!/bin/sh
# Show the placed VS Code versions and the running VS Code processes, and warn about servers that
# place.sh did not place (the agent host or Remote - SSH downloaded them) or that run unpinned.
# Usage: status.sh   (exit status 1 when a warning was printed)
. "$(cd "$(dirname "$0")" && pwd)/common.sh"

warn=0
w() {
  echo "WARNING: $*" >&2
  warn=1
}
me=$(id -u)
pin=$(grep -o 'VSCODE_CLI_INITIAL_AH_VERSION=[0-9a-f]*' "$HSVR_SSHD_CONFIG" 2>/dev/null | sed 's/.*=//')
echo "pin in sshd config: ${pin:-none}"
echo "lru.json: $(cat "$HSVR_LRU" 2>/dev/null || echo missing)"
for d in "$HSVR_SERVERS_DIR"/Stable-*; do
  [ -d "$d" ] || continue
  c=${d##*/Stable-}
  v=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' "$d/server/package.json" 2>/dev/null | head -n 1)
  if wrapped "$c"; then
    cli=wrapper
  elif [ -x "$HSVR_VSCODE_DIR/code-$c" ]; then
    cli=unwrapped
  else
    cli=none
  fi
  echo "placed: Stable-$c version=${v:-?} cli=$cli size=$(du -sh "$d" | cut -f1)"
  case $cli in
    none) w "Stable-$c has no CLI: the agent host probably downloaded it" ;;
    unwrapped) [ "$c" = "$pin" ] || w "code-$c is not a place.sh wrapper: its agent host is not pinned" ;;
  esac
done

# The agent host supervisor runs its server in its own process group; that server must be the
# supervisor's own commit.
sup=$(pgrep -u "$me" -af "^$hsvr_home_re/[^ ]*code-[0-9a-f]* .*agent host" |
  sed -n 's/^\([0-9]*\) .*code-\([0-9a-f]\{40\}\) .*agent host.*/\1:\2/p')
for s in $sup; do
  pid=${s%%:*}
  c=${s#*:}
  g=$(ps -o pgid= -p "$pid" | tr -d ' ')
  ah=$(ps -o pgid= -o args= -u "$me" | awk -v g="$g" '$1 == g' |
    sed -n 's/.*\/Stable-\([0-9a-f]\{40\}\)\/server\/node .*/\1/p' | sort -u | tr '\n' ' ')
  echo "agent host supervisor: code-$c (pid $pid) runs: ${ah:-no server}"
  for a in $ah; do
    [ "$a" = "$c" ] || w "agent host of code-$c runs Stable-$a, not its own commit"
  done
done

for c in $(pgrep -u "$me" -af "^$hsvr_home_re/cli/servers/Stable-" |
  sed -n 's/.*\/Stable-\([0-9a-f]\{40\}\)\/server\/node .*/\1/p' | sort -u); do
  echo "running server: Stable-$c"
  wrapped "$c" || [ "$c" = "$pin" ] || w "running server Stable-$c is not a pinned client commit"
done
echo "VS Code processes: $(pgrep -u "$me" -f "$HSVR_PROC_PATTERN" | wc -l)"
exit $warn
