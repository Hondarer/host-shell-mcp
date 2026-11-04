---
name: host-shell-connect
description: Deploy, update, or verify host-shell (an MCP server running on the SSH target or inside a podman, docker or Kubernetes container) over SSH or container exec, and prepare the agent registration command. Use for requests such as "put host-shell on target", "make target operable over MCP through hop1", "put host-shell into the web container", or "update host-shell".
---

# Preparing a host-shell connection

Deploy `host-shell` to the target machine or container, verify that it starts as an MCP server over the SSH route or container exec, and prepare the agent registration command.  
Do not start the MCP server in this skill; the agent starts it after registration.

Below, `$SKILL` refers to the directory containing `SKILL.md` (`.agents/skills/host-shell-connect` in the repository).

## Folder layout

| Path | Contents |
|---|---|
| `scripts/` | Scripts for route checks, environment detection, deployment, and startup verification |
| `bin/host-shell-linux-amd64`, `bin/host-shell-linux-arm64` | Linux executables |
| `bin/host-shell-windows-amd64.exe`, `bin/host-shell-windows-arm64.exe` | Windows executables |
| `bin/host-shell-darwin-amd64`, `bin/host-shell-darwin-arm64` | macOS executables (used locally for `host-shell bridge`) |
| `bin/libhostshell_dns-linux-amd64.so`, `bin/libhostshell_dns-linux-arm64.so` | Remote DNS adapters built and tested by CI (release archives; `make dns-lib` builds the host architecture). `deploy.sh` places the matching one beside the Linux binary for [remote DNS](#optional-remote-dns) |
| `bin/VERSION` | Executable version |

Table: Folder layout

If `bin/` is missing or outdated, run `make dist` at the repository root (requires Go 1.26 or later; copies `dist/` to `bin/`).  
Without the repository, use `host-shell-mcp-<version>/.agents/skills/host-shell-connect/` from the GitHub Release archive `host-shell-mcp-<version>-bin.zip` or `.tar.gz` (ask the user to obtain the archive).

## Scripts

No script uses SFTP or port forwarding.

| Script | Description |
|---|---|
| `remote.sh ROUTE COMMAND` | Runs `COMMAND` on the target over nested `ssh` hops (and `exec -i` into a container when the route ends with one) and forwards standard input |
| `detect.sh ROUTE` | Prints the target environment type (`linux-amd64`, `linux-arm64`, `windows-amd64`, `windows-arm64`) |
| `deploy.sh ROUTE TYPE [REMOTE_PATH]` | Transfers the `TYPE` executable from `bin/` over the route's standard input (on Windows, Windows PowerShell 5.1 reads it asynchronously, because synchronous reads stall on the pipe OpenSSH provides), verifies its SHA-256, and checks startup. Skips transfer if an identical file exists; removes the file if startup fails. Tries default locations in order when `REMOTE_PATH` is omitted. On glibc Linux targets, also places the remote DNS adapter beside it as `libhostshell_dns.so` and checks that the target can preload it |
| `probe.sh ROUTE REMOTE_PATH [OPTIONS...]` | Sends MCP `initialize` and `tools/list` requests to `host-shell` on the target and verifies the responses |

Table: Scripts

Specify `ROUTE` as a comma-separated list of jump hosts and the target from the local side (for example `hop1,target`, or `target` for a direct connection).  
Host names defined in `~/.ssh/config` are supported. Each hop connects with `-T -o BatchMode=yes` (override with `HOST_SHELL_SSH_OPTS`).  
Jump host shells must be POSIX `sh` compatible.

The last element of `ROUTE` may be a container instead of an SSH host; see [Containers](#containers).

## Procedure

### Information needed

Determine the following items from the user's request and ask only about unclear items.

| Item | Example | Default |
|---|---|---|
| Route (`ROUTE`) | `hop1,target` | None (always confirm) |
| Location (`REMOTE_PATH`) | `/tmp/host-shell-1000/host-shell` | Omitted (`deploy.sh` tries default candidates in order) |
| Registration name | `host-shell-target` | `host-shell-<target host name>` |
| `host-shell` options | `--timeout 2m`, `--windows-shell cmd`, restriction options | None (unrestricted) |
| Whether to use `bridge` | Enables file transfer between the local machine and the target | Use `bridge` |

Table: Information needed

Add restriction options only when requested by the user.

| Requirement | Option |
|---|---|
| Read-only access | `--read-only` |
| Limit published tools | `--tools files,search` (groups: `files`, `search`, `exec`, `net`, and `admin`, with exclusions such as `all,-admin`) |
| Limit accessible paths | `--allow-path /srv/app`, `--allow-read-path /var/log` |
| Limit runnable programs | `--allow-command git,make,go` |
| Require user confirmation before changes | `--confirm destructive` (requires a client supporting elicitation) |
| Limit network destinations | `--allow-host localhost,10.0.0.0/8` |
| Mask secrets in output | `--redact`, `--unset-env 'AWS_*,*TOKEN*'` |

Table: Restriction options

#### Location

To avoid modifying system directories on the target, place the binary in a user-owned temporary directory or under the home directory. When `REMOTE_PATH` is omitted, `deploy.sh` tries the following candidates in order.

| Target | 1. Temporary directory | 2. Home directory |
|---|---|---|
| Linux | `/tmp/host-shell-<uid>/host-shell` | `.local/bin/host-shell` (relative to home) |
| Windows | `%TEMP%/host-shell/host-shell.exe` | `%LOCALAPPDATA%/Programs/host-shell/host-shell.exe` |

Table: Default locations

- If `/tmp` is mounted `noexec`, execution policies (such as AppLocker) block startup, directory permissions are unsafe, or the file system has less free space than the binary (about 12 MiB), `deploy.sh` falls back to the second candidate. The free space is checked before anything is written.
- The `/tmp` directory is created with mode `700` to prevent replacement by other users. Symbolic links and directories owned or writable by other users are rejected.
- Use system directories such as `/opt`, `/usr/local`, or `C:/` only when explicitly requested (`sudo` is not used).
- Files in `/tmp` and `%TEMP%` may be removed on reboot or by periodic cleanup (such as `systemd-tmpfiles`). If removed, rerun the deployment procedure (agent registration does not change).
- On Linux, if `/tmp/host-shell-<uid>` is deleted, another local user could recreate it with a fake binary. On multi-user systems with untrusted users, specify `.local/bin/host-shell` explicitly (`deploy.sh ROUTE TYPE .local/bin/host-shell`).
- Use a path without spaces or quotes. On Windows, candidates containing spaces in `%TEMP%` or `%LOCALAPPDATA%` are skipped.
- `.local/bin/host-shell` can be registered as a relative path because `sshd` starts commands in the user's home directory (avoid `~`, which the local shell would expand).

### Checking the SSH route

```bash
"$SKILL"/scripts/remote.sh hop1,target true | od -c | head
```

- Empty output means the route is clean. On Windows targets `true` is not a `cmd` command; its error goes to standard error and the check still works.
- Any output means a shell startup file on a jump host or the target writes to standard output during non-interactive sessions, which corrupts MCP communication. Ask the user to silence non-interactive output in their startup file and stop (for bash, add `case $- in *i*) ;; *) return;; esac` at the top of `.bashrc`).
- On authentication errors (`Permission denied`, `Host key verification failed`), ask the user to configure passwordless SSH for each hop and stop.

### Detecting the target environment

```bash
"$SKILL"/scripts/detect.sh hop1,target
```

Pass the output (for example `linux-amd64`) as `TYPE` in the next step.  
If detection fails (for example on Windows when `sshd` uses PowerShell as the default shell), ask the user for the OS and architecture.  
Verify that the corresponding binary exists in `$SKILL/bin/`. If missing, build or obtain it as described in [Folder layout](#folder-layout).

### Deploying the binary

Before writing to the target, show the user the route, version (`bin/VERSION`), environment type, and target path, and obtain their consent.

```bash
"$SKILL"/scripts/deploy.sh hop1,target linux-amd64
```

Use the path printed in `deployed: <location>` or `up to date: <location>` for subsequent steps and registration (the examples below use `/tmp/host-shell-1000/host-shell`).  
To specify a path explicitly, pass it as the third argument.

- `up to date`: An identical binary already exists; transfer was skipped.
- `deployed`: Transfer and SHA-256 verification succeeded.
- `could not install`: Placement at that path failed (when using default candidates, the script tries the next candidate). If all candidates fail on Windows, the default shell of `sshd` may not be `cmd` (`deploy.sh` invokes `powershell -Command "..."` from `cmd`).
- `could not determine %TEMP% and %LOCALAPPDATA%` (Windows): the default shell of `sshd` is not `cmd`. `detect.sh` and `deploy.sh` need `cmd`, while `host-shell` itself (and `probe.sh`) also works under a PowerShell default shell. Ask the user to set `DefaultShell` back to `cmd`, or to place the binary another way.
- `cannot run`: The binary failed to start and was removed.
- `no default location worked`: Ask the user for a writable user-owned path and rerun with it as the third argument.
- On Windows, a running `host-shell.exe` cannot be overwritten. Disconnect the active agent session and rerun.

On Linux, a line about the remote DNS adapter precedes the final line. Keep it for [remote DNS](#optional-remote-dns); it never makes the deployment fail.

- `dns adapter deployed: <path>` / `dns adapter up to date: <path>`: The adapter is beside the binary and loads on the target. `<path>` is absolute; use it as `--remote-dns-library`.
- `dns adapter unavailable: no libhostshell_dns-<TYPE>.so in bin/`: `bin/` came from `make dist` alone. Use a release archive or run `make dns-lib` on a matching Linux machine, then rerun `deploy.sh`. Otherwise `--remote-dns` compiles the adapter on the target, which needs `cc` and glibc headers.
- `dns adapter unavailable: the target has no glibc`: Remote DNS is not supported on this target (for example Alpine/musl).
- `dns adapter unavailable: the target cannot preload it (...)`: The adapter was removed again. Report the loader message to the user; `--remote-dns` would need `cc` on the target.

### Checking startup

```bash
"$SKILL"/scripts/remote.sh hop1,target '/tmp/host-shell-1000/host-shell --version'
"$SKILL"/scripts/probe.sh hop1,target /tmp/host-shell-1000/host-shell
```

`probe.sh` printing `OK:` with tool names such as `sys_get_info` confirms the server works.  
If startup options will be used at registration, append the same options to `probe.sh` to verify them.  
If `NG:` is printed, follow the error message. On high-latency routes, retry with `PROBE_WAIT=15`.  
An `open audit log` error means the audit log path is not writable; specify a writable path with `--log-file` or disable logging with `--no-log`.

### Presenting the registration command

Present the registration command and configuration examples to the user; run registration commands only if the user asks.

Nested `ssh -T` commands start `host-shell` on the target.  
By default, run `host-shell bridge` locally in front of `ssh` to relay target tools and enable file transfer (`transfer_push`, `transfer_pull`).

For the local `bridge` binary, specify the absolute path of the matching executable in `$SKILL/bin/` (check with `uname -sm`: `host-shell-linux-amd64` for Linux x86_64, `host-shell-darwin-arm64` for macOS arm64, `.exe` for Windows). To keep the registered path stable across skill updates, copy the binary to `~/.local/bin/host-shell` or a similar location first.

Claude Code (with `bridge`):

```bash
claude mcp add host-shell-target -- /path/to/host-shell bridge -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

Claude Code (without `bridge`):

```bash
claude mcp add host-shell-target -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

JSON-configured agents such as VS Code (`.vscode/mcp.json`):

```json
{
  "servers": {
    "host-shell-target": {
      "type": "stdio",
      "command": "/path/to/host-shell",
      "args": ["bridge", "--", "ssh", "-T", "-o", "BatchMode=yes", "hop1", "ssh", "-T", "-o", "BatchMode=yes", "target", "/tmp/host-shell-1000/host-shell", "--mcp"]
    }
  }
}
```

Append target restriction options after `--mcp`. To restrict local transfer paths, add `--allow-local-path DIR` after `bridge`.

After registration, instruct the user to reload the MCP server in their agent (in Claude Code, run `/mcp` or restart) and verify that `sys_get_info` returns the target's host name and OS.

## WSL on Windows targets

When the user wants to use Linux in WSL 2 on a Windows target, recommend running sshd inside WSL and reaching it with the Windows sshd as a ProxyJump host; WSL is then an ordinary Linux route and every script here works unchanged. Follow "Connecting to Linux in WSL" in `docs/en/user-guide.md` for the setup (port, systemd, `.wslconfig` idle timeouts, host key). Without sshd in WSL, `ssh -T <windows> wsl -d <distribution> -- <host-shell> --mcp` works for registration, but `remote.sh`, `detect.sh`, `deploy.sh`, and `probe.sh` cannot target it (the Windows `cmd` hop interprets `|`, `>`, and `&&`); place the binary manually as that section describes.

## Containers

`host-shell` can also run inside a running Linux container, reached via the container runtime's `exec -i` subcommand instead of SSH (see `docs/en/container.md`).

| Last `ROUTE` element | Command executed | Extra options (environment variable) |
|---|---|---|
| `podman:CONTAINER` | `podman exec -i ... CONTAINER sh -c COMMAND` | `HOST_SHELL_PODMAN_OPTS` (e.g. `--user dev`) |
| `docker:CONTAINER` | `docker exec -i ... CONTAINER sh -c COMMAND` | `HOST_SHELL_DOCKER_OPTS` (e.g. `--user dev`) |
| `kubectl:POD` | `kubectl exec -i ... POD -- sh -c COMMAND` | `HOST_SHELL_KUBECTL_OPTS` (e.g. `-n app -c web`) |

Table: Container route elements

- Examples: `podman:web` (a container on the local machine), `hop1,podman:web` (a container on `hop1`). A container element is allowed only as the final element of `ROUTE`.
- `podman` is verified on a real environment; `docker` is verified via Podman's Docker-compatible CLI; `kubectl` is unverified. Windows containers are not supported.
- The deployment scripts (`detect.sh`, `deploy.sh`, `probe.sh`) work unchanged. Skip SSH-specific checks (shell startup files and SSH authentication). Default installation paths inside the container are `/tmp/host-shell-<uid>/host-shell`, followed by `.local/bin/host-shell`.
- The container must provide `sh` and basic utilities (`uname`, `id`, `dirname`, `ls`, `sha256sum`, `mkdir`, `chmod`, `mv`, `cat`, `rm`). For containers without a shell (such as distroless images), the scripts cannot be used; show the user the manual `podman cp` / `docker cp` steps in `docs/en/container.md` (`kubectl cp` requires `tar` inside the container).
- Files placed inside a container are lost when the container is recreated (though preserved across restarts); rerun deployment after recreation.

Registration commands start `host-shell` directly with `exec -i` (always pass `-i` and never `-t`, as allocating a TTY corrupts the JSON-RPC stream). Include the same extra runtime options used in the route:

```bash
claude mcp add host-shell-web -- /path/to/host-shell bridge -- podman exec -i web /tmp/host-shell-0/host-shell --mcp
claude mcp add host-shell-web -- /path/to/host-shell bridge -- docker exec -i --user dev web /tmp/host-shell-1000/host-shell --mcp
claude mcp add host-shell-web -- /path/to/host-shell bridge -- kubectl exec -i -n app -c web web-0 -- /tmp/host-shell-1000/host-shell --mcp
claude mcp add host-shell-web -- /path/to/host-shell bridge -- ssh -T -o BatchMode=yes hop1 podman exec -i web /tmp/host-shell-0/host-shell --mcp
```

## Uninstalling

When asked to remove `host-shell`, delete its installation directory (`/tmp/host-shell-<uid>/`, `%TEMP%/host-shell/`, or `.local/bin/host-shell` together with `.local/bin/libhostshell_dns.so`).  
Ask whether to delete the audit log (`~/.local/state/host-shell/` on Linux, `%LOCALAPPDATA%/host-shell/` on Windows), and show how to unregister the server from the agent (`claude mcp remove <name>` in Claude Code).

## Report

When finished, report the following items together:

- Route and target environment type
- Installation path, version, SHA-256, and whether the binary was newly deployed (`deployed`) or already current (`up to date`)
- On Linux, the `dns adapter` result (path, or why it is unavailable)
- `probe.sh` result (version and number of published tools)
- Registration command (with or without `bridge` and any restriction options) and how to reload the MCP server

## Optional client HTTP proxy

When proxy-aware programs on the target need outbound HTTP/HTTPS access through the local client network, see `docs/en/client-proxy.md`. Pass `--client-proxy` to the target `host-shell` and `--proxy` with explicit `--proxy-allow-host HOST[:PORT]` rules to `bridge`. Traffic is multiplexed over the existing SSH stdio stream without SSH port forwarding. Use `proxy_status` to diagnose denied requests, and never broaden user-approved host rules autonomously. `sys_get_info` reports the path to the user-only credential URL file (`proxy_url_file`); never print or paste its contents. Leave this feature disabled unless requested by the user.

## Optional remote DNS

When the user asks for client-side name resolution on a Linux target (for example a CLI that fails with `failed to lookup address information` on a target without DNS), see `docs/en/remote-dns.md`. The user does not need to build or transfer anything: use the adapter that `deploy.sh` placed and reported (`dns adapter deployed: <path>` or `up to date`).

- Add `--remote-dns` to `bridge` (before `--`).
- Add `--remote-dns --remote-dns-library <path>` after `--mcp`, with `<path>` copied from the `dns adapter` line.
- If the adapter is unavailable because `bin/` lacks it, the target may still compile one: add only `--remote-dns` on both ends if the target has `cc` (check with `remote.sh ROUTE 'command -v cc'`). Otherwise explain the reason from the `dns adapter` line and leave remote DNS disabled.
- `bridge` is required. Do not pass `--remote-dns` options to `probe.sh` (without a bridge, startup stops with `bridge must enable --remote-dns`); probe without them.
- Redeploying with `deploy.sh` updates the adapter at the same path, so registration does not change.

```bash
claude mcp add host-shell-target -- /path/to/host-shell bridge --remote-dns -- ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp --remote-dns --remote-dns-library /tmp/host-shell-1000/libhostshell_dns.so
```

After reloading, verify that `sys_get_info` reports `remote_dns: true`. Remote DNS permits arbitrary client-side lookups independently of HTTP proxy rules. Leave it disabled unless requested by the user.

Windows targets have no adapter. Add `--remote-dns --remote-dns-listen 127.0.0.1:53` after `--mcp` (and `--remote-dns` to `bridge`); `sys_get_info` then reports `remote_dns_listen`. The OS resolver uses this loopback DNS server only through an NRPT rule, and Windows `getaddrinfo` also needs an IPv4 default route; both change name resolution for the whole machine (see "Windows targets" in `docs/en/remote-dns.md`). `--remote-dns-configure` makes host-shell add the rule (and a loopback default route when there is no IPv4 default route) at startup and remove them on exit. Both are volatile: the rule lives in a volatile registry key and the route in the ActiveStore, so nothing is written to disk and a restart clears them. If the target host-shell is killed, they remain until the next start with the option cleans them up or the machine restarts.

Before adding `--remote-dns-configure` to a registration, or running the manual NRPT or route commands through `exec_run`, ask the user and obtain explicit approval. Tell them that while the settings exist they change name resolution for every user and service on the target machine (lookups by OS services such as Windows Defender and Windows Update are relayed to the client network too), which settings are added, and that they remain until the next start or a restart if host-shell is killed. Use the volatile manual procedure in `docs/en/remote-dns.md`, never `Add-DnsClientNrptRule`, which writes a persistent rule. Never add the option or run those commands on your own judgment, and do not treat approval for an earlier session or another target as approval. Without approval, register without `--remote-dns-configure` and only show the user the manual procedure. The option needs an administrator connecting with an elevated token (the default for administrators over Windows OpenSSH public key authentication). After connecting, verify `remote_dns_configured` in `sys_get_info`.

```bash
claude mcp add host-shell-target -- /path/to/host-shell bridge --remote-dns -- ssh -T -o BatchMode=yes target C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe --mcp --remote-dns --remote-dns-listen 127.0.0.1:53 --remote-dns-configure
```

## Optional port forwarding

When the user asks for port forwarding in an environment where SSH port forwarding is unavailable, see `docs/en/port-forward.md`. Pass `--forward-local [BIND:]PORT:HOST:HOSTPORT` (listen locally, connect from the target), `--forward-remote` (listen on the target, connect from the client), or `--forward-socks [BIND:]PORT` (with `--forward-allow-local` rules) to `bridge`. Add `--forward-allow-local` or `--forward-allow-remote HOST[:PORT]` when the agent should be able to open forwards dynamically with `forward_open`. The target must also permit each direction with `--allow-forward-local` or `--allow-forward-remote`. Listeners bind to loopback only and do not authenticate local connections. Use `forward_status` to diagnose issues, and never broaden user-approved rules autonomously. Leave port forwarding disabled unless requested by the user.

## Optional code-server

When the user wants to run code-server (VS Code in the browser) on the target, use the `host-shell-code-server` skill (`.agents/skills/host-shell-code-server`). Its registration command requires `--allow-forward-local` on the target and `--forward-allow-local '127.0.0.1:80[0-7]?'` on `bridge` (code-server binds to the first free port in `8000`–`8079`); see `docs/en/code-server.md`.

