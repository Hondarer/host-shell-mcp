# host-shell User Guide

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/user-guide.md](../ja/user-guide.md)) is the single source of truth (SSOT) and takes precedence if they differ.

This guide explains how to deploy `host-shell` on a target, register it with an agent, and use it for common tasks.  
For tool inputs and outputs, see the [Reference](reference.md); for security and operational design, see the [Operations Guide](operations.md).

## Contents

- [How it works](#how-it-works)
- [Requirements](#requirements)
- [Getting the executables](#getting-the-executables)
- [Deploying to the target](#deploying-to-the-target)
- [Checking the SSH route](#checking-the-ssh-route)
- [Registering with the agent](#registering-with-the-agent)
- [Verifying the setup](#verifying-the-setup)
- [Usage](#usage)
- [Troubleshooting](#troubleshooting)

## How it works

`host-shell` is an MCP server deployed on the remote machine you want to operate (the target).  
You register the SSH command that starts `host-shell` with your agent as an MCP stdio server. Because the MCP stdio transport exchanges JSON-RPC messages over the child process's standard input and output, the SSH session's standard input and output serve directly as the MCP channel.

```mermaid
---
title: host-shell deployment
---
flowchart TB
    subgraph client ["Client"]
        agent["Agent"]
        ssh0["ssh"]
    end
    subgraph jump ["Jump host"]
        ssh1["ssh"]
    end
    subgraph target ["Target (Windows / Linux)"]
        hs["host-shell"]
        os["Shell / OS"]
    end
    agent -->|"stdio (MCP)"| ssh0
    ssh0 -->|"SSH session"| ssh1
    ssh1 -->|"SSH session (stdio)"| hs
    hs -->|"process start, file operations"| os
```

Even across jump hosts, SSH port forwarding (`-L`, `-R`, `-D`, `ProxyJump`, `-W`) and SFTP are not used. Each hop runs a nested `ssh` command, so connections work even when `sshd` sets `AllowTcpForwarding no`.

When the SSH connection itself from the local machine to the first host must pass through an authenticated HTTP proxy (for example, in environments where users connect using the proxy settings of Tera Term), set `host-shell connect` as `ProxyCommand` in the Host block of `~/.ssh/config`. See [SSH route proxy](ssh-proxy.md) for the procedure.

- A single SSH connection stays open for the entire MCP session, avoiding per-call reconnection overhead even over multi-hop routes.
- `host-shell` normalizes differences between Windows and Linux (shells, quoting, path separators), allowing the agent to invoke the same tools with identical arguments.
- Output limits, timeouts, and audit logs are managed centrally on the target.

### Using bridge

Running `host-shell bridge` on the client side enables direct file and directory transfers between the local machine and the target.

```mermaid
---
title: Configuration with bridge
---
flowchart TB
    subgraph client ["Client"]
        agent["Agent"]
        bridge["host-shell bridge"]
        ssh0["ssh"]
        local["Local files"]
    end
    subgraph target ["Target (Windows / Linux)"]
        hs["host-shell --mcp"]
    end
    agent -->|"stdio (MCP)"| bridge
    bridge -->|"stdio (MCP)"| ssh0
    ssh0 -->|"SSH session (multi-hop possible)"| hs
    bridge -->|"transfer_push, transfer_pull"| local
```

`bridge` relays all tools exposed by the target's `host-shell` and adds `transfer_push` (local to target) and `transfer_pull` (target to local).  
File contents stream directly between `bridge` and the target's `host-shell` without passing through the agent context, supporting large files and directory trees. When enabled on both ends, `bridge` also provides an [HTTP proxy through the client](client-proxy.md), [port forwarding](port-forward.md), and [client-side name resolution (remote-dns)](remote-dns.md) over the same SSH session.  
`bridge` is a subcommand of the same `host-shell` binary; use the executable matching the client OS (Linux, Windows, or macOS).

### Runs only while connected

`host-shell` runs on the target only while the MCP session (the SSH connection) lasts. When the connection ends, it terminates the processes it started and closes the listeners it opened, as far as possible, before exiting.  
Because it leaves no resident process or listening port behind, `host-shell` holds target resources (processes, memory, ports) only while you use it, and no path to operate the target through `host-shell` remains while it is not in use.

```mermaid
flowchart TD
    Start["MCP connection starts"] --> Active["Connected: run commands and jobs<br/>Use permitted forwards and proxy"]
    Active --> Detect["Disconnect detected / server exits"]
    Detect --> Cleanup["Terminate started processes where possible<br/>Close listeners and relayed connections<br/>Remove proxy URL file"]
    Cleanup --> Exit["host-shell exits"]
    Exit --> Remain["Installed executables, audit logs,<br/>and created or modified files remain"]
```

| Cleaned up on disconnect | Details |
|---|---|
| Commands and jobs | Terminates processes started by `exec_run`, `exec_sudo`, and `job_start`, including their descendants |
| Port forwarding | Closes remote forward listeners on the target, local forward and SOCKS5 listeners on `bridge`, and established connections |
| HTTP proxy through the client | Closes the loopback listener on the target and relayed connections, and deletes the URL file that contains the credential |
| Client-side name resolution (`remote-dns`) | Closes the UNIX domain socket on the target and removes the per-session temporary directory containing the adapter |

Table: Cleaned up on disconnect

Even when `host-shell` itself is killed, OS facilities (the parent death signal on Linux, a Job Object on Windows) keep child processes from lingering. On Linux, however, processes that leave their process group on their own (for example with `setsid`) cannot be caught ([How processes are ended in the Reference](reference.md#how-processes-are-ended)).  
After disconnection, only the deployed executable, the audit log, and files created or changed by tools remain on the target.

Keeping processes or services running after disconnection (such as building resident services) is outside the scope of `host-shell`. Managing existing services with `svc_control` (starting, stopping, and so on) is in scope, but `host-shell` provides no means to keep what it started running after the session ends.

## Requirements

| Location | Requirement |
|---|---|
| Client | An agent supporting MCP stdio servers (Claude Code, VS Code GitHub Copilot, etc.) and the OpenSSH client. Using `bridge` requires the `host-shell` binary for the client OS (Linux, Windows, or macOS 12+) |
| Jump host | OpenSSH client and server, plus credentials (such as keys) for the next hop |
| Target | OpenSSH server on Windows 10 / Windows Server 2016+ or Linux kernel 3.2+ (`amd64` / `arm64`) |

Table: Requirements

Because the `ssh` process started by the agent cannot answer interactive password prompts, every hop must authenticate non-interactively via public keys or `ssh-agent`.

## Getting the executables

Each OS and architecture uses a single static binary with no external dependencies.

| OS and architecture | File | Role |
|---|---|---|
| Linux (x86_64) | `host-shell-linux-amd64` | Target, local (`bridge`) |
| Linux (aarch64) | `host-shell-linux-arm64` | Target, local (`bridge`) |
| Windows (x64) | `host-shell-windows-amd64.exe` | Target, local (`bridge`) |
| Windows (ARM64) | `host-shell-windows-arm64.exe` | Target, local (`bridge`) |
| macOS (Intel) | `host-shell-darwin-amd64` | Local (`bridge`) |
| macOS (Apple silicon) | `host-shell-darwin-arm64` | Local (`bridge`) |

Table: Executables

The same binary runs as an MCP server on the target with `--mcp` and as a local relay on the client with `bridge`.  
To build all six binaries in `dist/` from source (requires Go 1.26 or later; see the [Development Guide](development.md)):

```bash
make dist
```

## Deploying to the target

To avoid modifying system directories on the target, place the binary in a user-owned path in the following order of preference.

| Target | 1. Temporary directory (recommended) | 2. Home directory |
|---|---|---|
| Linux | `/tmp/host-shell-<uid>/host-shell` | `.local/bin/host-shell` (relative to home) |
| Windows | `%TEMP%/host-shell/host-shell.exe` | `%LOCALAPPDATA%/Programs/host-shell/host-shell.exe` |

Table: Where to place the binary

- `<uid>` is the target user's UID (`id -u`). Examples below use `/tmp/host-shell-1000/host-shell` on Linux and `C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe` on Windows.
- If `/tmp` is mounted `noexec` or Windows execution policies (such as AppLocker) block binaries in `%TEMP%`, use the home directory path instead.
- Create `/tmp/host-shell-<uid>` owned by your user with mode `700` so other local users cannot replace the binary.
- Files in `/tmp` and `%TEMP%` may be removed on reboot or by periodic cleanup; redeploy to the same path if removed (agent registration remains unchanged).
- On Linux, if `/tmp/host-shell-<uid>` is deleted, another user could create a directory with the same name and plant a fake binary. On multi-user machines with untrusted users, use `.local/bin/host-shell`.
- System directories such as `/opt` are intended for administrator-managed setups with a dedicated service user ([Operations Guide](operations.md)).
- Avoid spaces in the installation path because command lines are re-parsed at each SSH hop.
- On Linux, `.local/bin/host-shell` works as a relative path because `sshd` starts commands in the user's home directory (do not use `~`, which is expanded by the local shell).

`scripts/deploy.sh` in `.agents/skills/host-shell-connect` automates candidate selection, permission checks, SHA-256 verification, and startup testing. On glibc Linux targets, it also places the adapter for [remote DNS](remote-dns.md) in the same directory.

To deploy into a running container, see [Connecting into containers](container.md).

### Sending over SSH only

When SFTP and port forwarding are unavailable, stream the binary over SSH standard input and verify its SHA-256 hash afterward.

Linux target (direct connection):

```bash
ssh target id -u
ssh target 'mkdir -m 700 /tmp/host-shell-1000 && cat > /tmp/host-shell-1000/host-shell && chmod 755 /tmp/host-shell-1000/host-shell' \
    < dist/host-shell-linux-amd64
ssh target sha256sum /tmp/host-shell-1000/host-shell
sha256sum dist/host-shell-linux-amd64
```

`mkdir -m 700` fails if the directory already exists. When redeploying, verify with `ls -ld /tmp/host-shell-1000` that you own the directory with mode `drwx------`, then transfer without `mkdir`.

Linux target (via jump host):

```bash
ssh hop1 "ssh target 'mkdir -m 700 /tmp/host-shell-1000 && cat > /tmp/host-shell-1000/host-shell && chmod 755 /tmp/host-shell-1000/host-shell'" \
    < dist/host-shell-linux-amd64
ssh hop1 "ssh target sha256sum /tmp/host-shell-1000/host-shell"
```

The outer `"..."` is parsed by the local shell and the inner `'...'` by the jump host shell. Keep the output redirection (`>`) inside the inner quotes so it runs on the target.

Windows target (when `sshd` uses `cmd` as the default shell):

```bash
ssh target "echo %TEMP%"
recv=$(cat <<'EOF'
powershell -NoProfile -NonInteractive -Command "$ErrorActionPreference='Stop'; $p='C:\Users\alice\AppData\Local\Temp\host-shell\host-shell.exe'; New-Item -ItemType Directory -Force (Split-Path $p) | Out-Null; $o=[IO.File]::Create($p); $s=[Console]::OpenStandardInput(); $h=$s.GetType().GetField('_handle', [Reflection.BindingFlags]'NonPublic,Instance').GetValue($s); try { $i=New-Object IO.FileStream($h, [IO.FileAccess]::Read, 65536, $true) } catch { $i=$s }; try { $i.CopyTo($o) } finally { $o.Close() }; (Get-FileHash $p).Hash"
EOF
)
ssh target "$recv" < dist/host-shell-windows-amd64.exe
sha256sum dist/host-shell-windows-amd64.exe
```

Windows PowerShell 5.1 on the target writes standard input to the file as is and prints its SHA-256 at the end. Replace `$p` with the destination path. The command goes into the variable `recv` through a here-document so that the local shell does not interpret its `'` and `$`. If no hash is printed, check that `DefaultShell` in `sshd_config` is `cmd`.

OpenSSH for Windows (Win32-OpenSSH) gives the command an asynchronous I/O pipe (opened with `FILE_FLAG_OVERLAPPED`) as standard input. Reading that pipe synchronously stalls after about 128 KiB. A plain `CopyTo` from `[Console]::OpenStandardInput()` is such a read, and other .NET Framework programs stall the same way. The procedure above reopens the same handle as an asynchronous `FileStream`, so it does not stall. When the handle is synchronous, `FileStream` refuses it and the procedure falls back to the ordinary read.

This procedure and the Windows transfer in `deploy.sh` were checked on Windows 11 Enterprise (build 26300, amd64) with Windows PowerShell 5.1.26100 and the OpenSSH_for_Windows 9.5p2 `sshd`: a deployment of about 13 MiB with matching SHA-256, and standard input that arrives late (Verified; [validation record, Japanese](../prompts/past/261105-windows-ssh-stdin-validation.md)). Windows Server, arm64, and other OpenSSH versions are Unverified. The procedure targets standard input over SSH; feeding standard input from a file locally on the target is out of scope.

Once `host-shell` is running, you can also upload newer versions via `fs_write` (`base64` with `offset`) or `transfer_push` and swap the binary with `exec_run`. On Windows, a running `host-shell.exe` cannot be overwritten in place; upload to a temporary name and rename it before the next session.

### Removing

Delete the installation directory (`/tmp/host-shell-1000/`, `%TEMP%/host-shell/`, etc.) and, if needed, the audit log directory (`~/.local/state/host-shell/` on Linux, `%LOCALAPPDATA%/host-shell/` on Windows).

### Checking the target

Verify that the binary starts over SSH:

```bash
ssh -T target /tmp/host-shell-1000/host-shell --version
ssh -T target C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe --version
```

Printing `host-shell <version>` confirms deployment.

## Checking the SSH route

### Do not allocate a pseudo-terminal

Pass `-T` to every SSH hop to disable pseudo-terminal allocation (also check that `RequestTTY` is not `yes` or `force` in `~/.ssh/config`). Allocating a PTY corrupts JSON-RPC framing through newline translation and echo.

### Keep the target shell quiet

`sshd` invokes commands through the target user's login shell. If shell startup files (`.bashrc`, `.profile`, etc.) write to standard output during non-interactive sessions, JSON-RPC communication breaks. Confirm that the following commands produce no output:

```bash
ssh -T target true | od -c | head
ssh -T hop1 ssh -T target true | od -c | head
```

If any bytes are printed, guard interactive output in the startup files. For bash, place this line at the top of `.bashrc`:

```bash
case $- in *i*) ;; *) return;; esac
```

### Authentication on jump hosts

In nested SSH connections, each jump host initiates the next `ssh` hop. Either place the next-hop key on each jump host or forward your local `ssh-agent` with `-A` (requires `AllowAgentForwarding yes` in each jump host's `sshd_config`).

### Connection script

For multi-hop routes or long startup flags, register a local wrapper script with the agent:

```{.bash caption="~/bin/host-shell-target (Linux / macOS client)"}
#!/bin/sh
# Connect to host-shell on target through hop1
exec ssh -T -o BatchMode=yes -o ServerAliveInterval=30 hop1 \
    ssh -T -o BatchMode=yes -o ServerAliveInterval=30 target \
    /tmp/host-shell-1000/host-shell --mcp
```

```{.bat caption="C:\Users\me\bin\host-shell-target.cmd (Windows client)"}
@echo off
rem Connect to host-shell on target through hop1
ssh -T -o BatchMode=yes -o ServerAliveInterval=30 hop1 ssh -T -o BatchMode=yes -o ServerAliveInterval=30 target C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe --mcp
```

`BatchMode=yes` fails immediately instead of hanging on password prompts, and `ServerAliveInterval` prevents idle connection drops.

Pass `host-shell` options after `--mcp`:

```text
/tmp/host-shell-1000/host-shell --mcp --max-output 131072 --timeout 2m
C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe --mcp --windows-shell cmd
```

## Registering with the agent

For registration commands targeting `host-shell` inside a container, see [Connecting into containers](container.md#registration-commands).

### Claude Code

With `bridge` (recommended):

```bash
claude mcp add host-shell -- ~/.local/bin/host-shell bridge -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
```

Without `bridge`:

```bash
claude mcp add host-shell -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
```

`~/.local/bin/host-shell` is the client binary (for example, `host-shell-darwin-arm64` on Apple silicon macOS).  
Place target restriction options ([Operations Guide](operations.md#restriction-options)) after `--mcp`, and local transfer path restrictions after `bridge` as `--allow-local-path DIR`.

Using a wrapper script:

```bash
claude mcp add host-shell -- ~/bin/host-shell-target
```

Add `--scope project` to save the configuration in `.mcp.json` for team sharing. When registering multiple targets, give each a distinct server name such as `host-shell-web01` and `host-shell-db01`.

To auto-approve read-only tools in Claude Code, add the following to `.claude/settings.json`:

```{.json caption=".claude/settings.json"}
{
  "permissions": {
    "allow": [
      "mcp__host-shell__help_get",
      "mcp__host-shell__sys_get_info",
      "mcp__host-shell__proc_list",
      "mcp__host-shell__disk_list",
      "mcp__host-shell__svc_list",
      "mcp__host-shell__log_query",
      "mcp__host-shell__net_check",
      "mcp__host-shell__net_list_listeners",
      "mcp__host-shell__net_list_interfaces",
      "mcp__host-shell__fs_list",
      "mcp__host-shell__fs_stat",
      "mcp__host-shell__fs_read",
      "mcp__host-shell__search_grep",
      "mcp__host-shell__search_find",
      "mcp__host-shell__fs_manifest",
      "mcp__host-shell__job_read",
      "mcp__host-shell__job_list"
    ]
  }
}
```

### VS Code

```{.json caption=".vscode/mcp.json"}
{
  "servers": {
    "host-shell": {
      "type": "stdio",
      "command": "C:/Users/alice/AppData/Local/Programs/host-shell/host-shell.exe",
      "args": ["bridge", "--", "ssh", "-T", "hop1", "ssh", "-T", "target", "C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe", "--mcp"]
    }
  }
}
```

Without `bridge`, set `command` to `"ssh"` and `args` to `["-T", "hop1", "ssh", "-T", "target", "C:/Users/alice/AppData/Local/Temp/host-shell/host-shell.exe", "--mcp"]`. With a wrapper script, set `command` to the script path and leave `args` empty.

### Other agents

Any MCP client supporting `stdio` servers can register `host-shell bridge -- ssh ... host-shell --mcp` (or `ssh ... host-shell --mcp`) as its server command.

## Verifying the setup

### Without an agent

Send a single `initialize` request and verify that a JSON-RPC response is returned (`sleep` keeps standard input open until the response arrives):

```bash
(printf '%s\n' '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"check","version":"0"}}}'; sleep 2) \
    | ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
```

A single line matching `{"jsonrpc":"2.0","id":1,"result":{...,"serverInfo":{"name":"host-shell",...}}}` confirms normal operation. If any text appears before that line, see [Keep the target shell quiet](#keep-the-target-shell-quiet).

You can also test tools interactively in a browser with [MCP Inspector](https://github.com/modelcontextprotocol/inspector):

```bash
npx @modelcontextprotocol/inspector ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp
```

### From the agent

Ask the agent to "call sys_get_info of host-shell and report the target's OS and host name".

## Usage

Agents select tools automatically using the initialize `instructions`, tool descriptions, and the `help_get` tool, so you do not need to specify tool names in prompts.

### What the agent is told

`host-shell` provides built-in guidance over MCP:

- Initialize `instructions`: target host name, OS, default shell, encodings, output limit, tool selection guidelines, and active policy restrictions.
- Tool and argument `description` fields: each tool's role, when to prefer it, and how to paginate reads.
- `help_get` tool and resources: step-by-step usage, argument examples, and Windows notes by topic (`files`, `edit`, `search`, `jobs`, `network`, `admin`, `transfer`, `policy`, etc.), also exposed as `host-shell://help/<topic>` resources.

If an agent picks suboptimal tools, prompt it to "read host-shell's help before working" (in Claude Code, you can also reference resources with `@`).

### Investigating

| Example prompt | Primary tools used |
|---|---|
| Check the target's OS, free disk space, and running services | `sys_get_info`, `disk_list`, `svc_list` |
| Show warnings logged by nginx in the last hour | `log_query` (`unit`, `since`, `priority`) |
| Request `localhost:8080/health` on the target | `net_request_http` |
| Check connectivity to DB port 5432 and TLS certificate expiration | `net_check` (`tls`) |
| Find the process listening on port 8080 | `net_list_listeners` |
| Find today's ERROR lines in `/var/log/app` | `search_find` (`sort: mtime`), `search_grep` |
| Read and explain the `server` blocks in `nginx.conf` | `search_grep` (`context`), `fs_read` |
| Count C files calling `connect(` in the repository | `search_grep` (`literal`, `output_mode: count`) |
| Count running Java processes | `proc_list` |

Table: Investigation examples

For large files, `fs_read` with `line_start` and `line_count` reads only the requested line range (for example, "read the last 200 lines of the log").

### Preparing target information and sources on the client

While `host-shell` describes the target OS and its own tools automatically, organizing the connection route along with target-specific context and assets in your local workspace (where the agent runs) streamlines setup, eliminates exploratory round-trips, and makes investigation much more effective:

- **SSH route, network topology, and authentication methods**: Because you need to tell the agent how to reach the target when deploying or registering `host-shell` (including via the `host-shell-connect` skill) or when hopping further into internal hosts or containers, organize the route details in advance: jump hosts and hop order, container runtime and container names (`podman` / `docker` / `kubectl`), authentication methods at each hop (`~/.ssh/config` host aliases, key file paths, whether `sudo` is required—without writing passwords or secret values themselves), and target network constraints (whether outbound internet/DNS is available or client proxy and port forwarding are needed).
- **File system layout**: Documenting application install directories, configuration and log paths, and systemd unit or Windows service names in local agent instructions (`AGENTS.md`, `CLAUDE.md`, etc.) lets the agent read the right paths immediately without scanning directories (especially helpful when [`--allow-path` or `--allow-read-path`](operations.md#restriction-options) restricts accessible directories).
- **Target diagnostic commands and scripts**: Listing the paths and arguments of target-specific diagnostic commands, operational wrappers, or admin CLIs allows the agent to run established procedures directly—and pairs naturally with `--allow-command` policies that permit only approved diagnostic scripts. You can also keep diagnostic scripts on the client and upload them to a temporary directory on the target via `transfer_push`.
- **Source code for investigation**: Even when the target only has compiled binaries deployed, keeping the corresponding source tree on the client lets the agent correlate logs, metrics, and stack traces from the target directly against the local source code.

When both the client and the target have a copy of the source or configuration tree, use `fs_manifest` to compare SHA-256 hashes and read only modified files:

```text
Get the fs_manifest of /srv/app/src on the target, compare it with the SHA-256 of the local ./src,
read only the files that differ with fs_read, and explain the differences.
```

`fs_manifest` normalizes path separators to `/`, making comparisons consistent across Linux and Windows targets.

### Making changes

| Example prompt | Primary tools used |
|---|---|
| Set `max_connections` to 200 in the config file | `fs_read`, `fs_edit` |
| Rename a function across the source tree | `search_grep` (`output_mode: files`), `fs_edit` (`replace_all`) |
| Restart the service and verify its status | `svc_control` (`restart`, with `sudo` on Linux) |
| Terminate a runaway process | `proc_list`, `proc_kill` |
| Update a registry value (Windows) | `reg_read`, `reg_write` |
| Deploy a script to `/opt/tools/` and mark it executable | `fs_mkdir`, `fs_write` (`perm`) |
| Move old logs to `archive` and delete logs older than a year | `search_find`, `fs_move`, `fs_remove` |

Table: Change examples

`fs_edit` modifies existing files in place by sending only the surrounding lines, preserving the file's encoding, BOM, and line endings, and returning a line-numbered snippet so the file does not need to be re-read.  
`fs_move`, `fs_copy`, and `fs_remove` provide OS-independent file management.

Because mutating tools carry `destructiveHint`, enable pre-execution approval in your agent.

### Long-running work

Run tasks taking tens of seconds or longer (builds, test suites, large copies) as background jobs. Prompting the agent to "run it as a job and follow its output" starts the command with `job_start` and polls output with `job_read` (`wait_sec`).  
Commands run with `exec_run` also emit progress notifications (elapsed time and latest line) when the client supports them, and kill the entire process tree on cancellation.

```text
job_start   {"command": "make -C /srv/app test"}             -> {"id": "job-1", "running": true, ...}
job_read    {"id": "job-1", "offset": 0, "wait_sec": 20}     -> {"output": "...", "next_offset": 4096, ...}
job_read    {"id": "job-1", "offset": 4096, "wait_sec": 20}  -> {"output": "", "status": {"running": false, "exit_code": 0}}
```

For interactive programs over pipes, set `stdin_open: true` on `job_start` and send input via `job_write`.  
For programs requiring a terminal (`sudo`, `ssh`, or REPLs such as `python` and `psql`), set `pty: true`, wait for prompts with `until` in `job_read`, and send input with `job_write`.

```text
job_start  {"command": "psql -h db.internal app", "pty": true}       -> {"id": "job-2", "pty": true, ...}
job_read   {"id": "job-2", "wait_sec": 20, "until": "Password.*: $"}  -> {"matched": true, ...}
job_write  {"id": "job-2", "input": "********\n"}
job_read   {"id": "job-2", "wait_sec": 20, "until": "app=> $"}        -> {"matched": true, ...}
job_write  {"id": "job-2", "input": "select count(*) from users;\n"}
job_read   {"id": "job-2", "wait_sec": 20, "idle_ms": 500}            -> {"output": " count \n-------\n  4213\n..."}
```

`job_stop` forcefully kills the job by default. On Linux, `signal` can send signals such as `interrupt` (in `pty` jobs, `interrupt` sends Ctrl-C on both Linux and Windows).

All running jobs terminate when the SSH session disconnects ([Runs only while connected](#runs-only-while-connected)). Check that jobs complete while you are connected.

### Transferring files

When registered with `bridge`, prompts such as "transfer local `dist/` to `/srv/app/dist` on the target" or "download `/var/log/app` into the local `logs/` directory" use `transfer_push` and `transfer_pull`.

- File data bypasses the agent context, streaming in 1 MiB chunks into a temporary file verified by SHA-256 before renaming.
- Free space on the target is checked before the transfer starts; if it is insufficient, nothing is written. When placing large files by other means, check the free space first with `available_bytes` of `fs_stat`.
- Identical files at the destination are skipped automatically, allowing repeated runs to act as a differential sync.
- Existing files with different content are skipped unless `overwrite: true` is set. `exclude` filters out patterns such as `.git` or `node_modules`, and Linux executable bits are preserved.

Files accessible via HTTP/HTTPS from the target can be downloaded directly to a target path using `save_to` in `net_request_http`.

Without `bridge`, files can be transferred in chunks via `fs_read` and `fs_write` (`base64`, `offset`), though this passes data through the agent context and is practical only up to a few megabytes (`offset` writes are idempotent and can resume from `fs_stat` `size`).

### Port forwarding, HTTP proxy, name resolution, and remote IDEs

Even where SSH port forwarding (`-L`, `-R`, `-D`), outbound network access, or external DNS is unavailable, enabling the corresponding options on both `bridge` and the target provides network relaying and remote development environments over the existing SSH session's standard input and output:

- **[Port forwarding](port-forward.md)**: Supports local forwards (`--forward-local`), remote forwards (`--forward-remote`), SOCKS5 (`--forward-socks`), and dynamic forwards via `forward_open`.
- **[HTTP proxy through the client](client-proxy.md)**: Routes HTTP and HTTPS requests from proxy-aware target applications through the `bridge` network (`--proxy`, `--proxy-allow-host`, `--client-proxy`).
- **[Client-side name resolution (remote-dns)](remote-dns.md)**: Relays `getaddrinfo` name resolution to `bridge` for Linux/glibc programs that resolve destination hostnames before connecting to the HTTP proxy (`--remote-dns`).
- **Deploying IDEs and coding agents to closed-network targets**: Combine file transfers, port forwarding, and the client proxy to [inject code-server](code-server.md) (opened in a local browser), connect local desktop VS Code via [VS Code Remote - SSH](vscode-remote-ssh.md), or run [CLI coding agents](coding-agents.md) on offline targets.

### Encodings and line endings

Even on Shift_JIS or EUC-JP targets, `host-shell` converts text to and from UTF-8 on the target so the agent always works in UTF-8.

- Command output, file reads, and searches auto-detect UTF-8 and fall back to the target's default encoding when non-UTF-8 bytes are found.
- `fs_write` writes UTF-8 by default; specify `charset` to convert encoding and `newline` to normalize line endings.
- `fs_edit` preserves the original file's encoding, BOM, and line endings automatically. When overwriting an entire file with `fs_write`, pass the `charset`, `bom`, and `newline` values returned by `fs_read` to keep its format.

### Windows targets

- `exec_run` and `job_start` default to PowerShell. To use `cmd` syntax, pass `"shell": "cmd"` per call or start the server with `--windows-shell cmd`.
- PowerShell commands are encoded in base64, avoiding SSH and `cmd` quoting issues. Errors and warnings are returned as plain text (warnings on standard output).
- Shift_JIS output from `cmd` and native binaries is converted to UTF-8 automatically, with the source encoding reported in `stdout_encoding`.
- `exec_sudo` is unavailable on Windows. Run privileged operations (service control, `HKLM` registry writes, etc.) from a `host-shell` instance started under an administrator account.
- Dedicated tools handle Windows services (`svc_list`, `svc_control`), Event Log (`log_query`), and registry keys (`reg_read`, `reg_write`, `reg_delete`).
- `pty` jobs use ConPTY (Windows 10 1809 / Windows Server 2019 or later).
- `job_stop`, `proc_kill`, and timeouts terminate processes directly through the Windows API (`TerminateProcess`); jobs and timeouts also terminate descendants.

#### Non-administrator connecting users

When the connecting user is not an administrator (Users or Power Users), deployment and the main tools still work. The SSH connection is a network logon, however, so Windows denies access to service management (SCM), the Event Log, and CIM/WMI. The same restriction applies to commands outside `host-shell` (`sc.exe`, `Get-Service`, `Get-CimInstance`, `Get-Net*`, `taskkill`).

| Subject | Result for a non-administrator connecting user |
|---|---|
| Deployment (`deploy.sh`, the user guide procedure), `probe.sh` | Succeeds |
| `exec_run`, `job_start` (including ConPTY), file operations, `transfer_push` and `transfer_pull` | Succeeds |
| `job_stop`, `proc_kill`, termination on timeout | Succeeds (including descendants) |
| `proc_list`, `net_list_listeners`, `disk_list`, `reg_read`, `reg_write` and `reg_delete` on HKCU | Succeeds |
| `reg_write` on HKLM | Denied |
| `svc_list`, `svc_control` | Denied (`open service control manager: Access is denied.`) |
| `log_query` | Denied, including `System` and `Application` |
| HTTP proxy through the client, local and remote forwards | Succeeds |
| Installing, starting, and stopping code-server | Succeeds |
| The `remote-dns` loopback DNS server | Succeeds. Routing the OS resolver to it (`--remote-dns-configure`, the manual procedure) needs administrator rights and is refused |

Table: What works for non-administrator connecting users

This was checked on a Windows 11 Enterprise (build 26300, amd64) virtual machine with the connecting user removed from Administrators, once in Users only and once also in Power Users (Verified; [validation record, Japanese](../prompts/past/261105-windows-standard-user-validation.md)). Both gave the same results. Domain users, Windows Server, and arm64 are Unverified.

#### Connecting to Linux in WSL

To use a WSL 2 Linux distribution on a Windows target, run sshd inside WSL and connect with the Windows sshd as a jump host (ProxyJump); this is the recommended configuration. From the client, WSL is then an ordinary Linux SSH target, so the host-shell-connect skill's scripts (`detect.sh`, `deploy.sh`, `probe.sh`) and registration steps work unchanged. It reuses the Windows sshd's public key authentication and does not expose a WSL port to the network.

```mermaid
flowchart LR
    Client["Client<br/>ssh (ProxyJump)"] -->|"SSH (22)"| WinSSHD["Windows sshd"]
    WinSSHD -->|"TCP forward<br/>127.0.0.1:2222"| WSLSSHD["sshd in WSL"]
    WSLSSHD --> HS["host-shell (Linux)"]
```

Preparation (run the WSL steps as root; from the Windows target, use `wsl -d <distribution> -u root -- ...`):

1. Install `openssh-server` in WSL. If the target cannot reach the internet, start `host-shell` inside WSL with `--client-proxy`, allow only the package sources on `bridge` (for Ubuntu, `archive.ubuntu.com:80` and `security.ubuntu.com:80`), and run `apt-get install openssh-server`.
2. Set the listening port and authentication methods, and enable systemd.

   ```bash
   printf 'Port 2222\nPasswordAuthentication no\nKbdInteractiveAuthentication no\n' > /etc/ssh/sshd_config.d/10-host-shell-wsl.conf
   printf '\n[boot]\nsystemd=true\n' >> /etc/wsl.conf
   ```

3. Put the public key in the connecting user's `~/.ssh/authorized_keys` (directory 700, file 600).
4. Stop WSL's automatic shutdown in `.wslconfig` in the Windows user's profile folder. By default the distribution stops about 15 seconds after the last session ends, and sshd stops listening with it.

   ```ini
   [general]
   instanceIdleTimeout=-1

   [wsl2]
   vmIdleTimeout=-1
   ```

5. After `wsl --shutdown`, start WSL once (`wsl -d <distribution> -- true`) and check that `systemctl is-active ssh.socket` reports `active`. Ubuntu 24.04 uses socket activation, so `ssh.socket` is generated from the `Port` value.
6. Obtain the WSL sshd host key (`/etc/ssh/ssh_host_ed25519_key.pub`) through the Windows target and add it to the client's known_hosts as `[127.0.0.1]:2222 ssh-ed25519 ...`.

An example `~/.ssh/config` on the client. The Windows sshd must have `AllowTcpForwarding` enabled (the default).

```text
Host win-target
    HostName 192.0.2.10
    User alice

Match host wsl-target exec "ssh -n win-target wsl.exe -d ubuntu --exec true"

Host wsl-target
    HostName 127.0.0.1
    Port 2222
    User alice
    ProxyJump win-target
```

From then on, treat `wsl-target` as an ordinary Linux target (for example `scripts/deploy.sh wsl-target linux-amd64`). The key points of this configuration:

- The `Match ... exec` line starts WSL on the Windows target on every connection to `wsl-target`. After Windows restarts, sshd does not listen until something starts WSL, so without this line the connection fails (`Connection closed by UNKNOWN port 65535`). It adds about 12 seconds when WSL is stopped and about 1 second when it is running. It leaves no autostart setting on Windows.
- The ssh inside `Match ... exec` does not inherit the `-F` given to the client ssh. When you pass the configuration file with `-F`, give the same `-F` to the ssh inside `exec` too. On a Windows client, use `/` as the path separator: the path ProxyJump hands to the jump ssh loses its `\` characters.
- Chaining the WSL start and `ssh -W` with `&&` in `ProxyCommand` does not work. OpenSSH prefixes `ProxyCommand` with `exec`, so the output of the first ssh is taken as the SSH stream.
- Set `HostName` to `127.0.0.1`, not `localhost`. In mirrored networking (`networkingMode=mirrored`), the WSL sshd does not answer connections from Windows to `::1` port 2222, so the connection fails when `localhost` resolves to `::1`. In NAT mode (the default), either form works.
- Restarting WSL empties `/tmp`, which removes `/tmp/host-shell-<uid>/host-shell` placed by `deploy.sh`. Run `deploy.sh` again, as after a Linux reboot.

In mirrored mode the WSL sshd also listens on the Windows IP addresses, but the Hyper-V firewall blocks direct connections from the network by default (WSL's `DefaultInboundAction` is `Block`). A direct connection needs an inbound allow rule created with `New-NetFirewallHyperVRule`, which is a setting that stays on Windows. ProxyJump does not need this rule.

If sshd cannot be set up, host-shell can also be started through `wsl.exe` from the Windows sshd:

```bash
ssh -T win-target wsl -d ubuntu -- /home/alice/.local/bin/host-shell --mcp
```

Standard input and output arrive intact on this route, but the skill's scripts do not work: the Windows `cmd` in between interprets `|`, `>`, and `&&` inside single quotes before passing them to `wsl.exe` (`sh -c 'a | b'` is split). Place files with `transfer_push` on the Windows side and copy them inside WSL from `/mnt/c/...`, and pass commands in double quotes or as base64.

| Configuration | Checked | Environment | Status |
|---|---|---|---|
| sshd in WSL with ProxyJump (NAT mode) | `detect.sh` (`linux-amd64`), `deploy.sh` (placement and `up to date`), `probe.sh`, and through bridge: `exec_run`, pty jobs, `transfer_push` and `transfer_pull` of about 13 MiB (SHA-256 match), `svc_list`, remote forwards. With the `.wslconfig` settings, connecting still works 30 seconds or more after the sessions close. `HostName` both `localhost` and `127.0.0.1` | Windows 11 Enterprise (build 26300) Hyper-V virtual machine, WSL 3.0.3, Ubuntu 24.04 (systemd, openssh-server 9.6p1). Client ssh: Git for Windows OpenSSH 10.6p1 for the scripts; that and Windows OpenSSH 9.5p1 for the operations through bridge | Verified |
| Connecting after Windows restarts | Without `Match ... exec`, the connection fails. With it, `detect.sh`, `deploy.sh`, and `probe.sh` right after the restart, and the operations above through bridge after `wsl --shutdown`. Starting WSL with `Match ... exec` works with both ssh clients | Same | Verified |
| Mirrored networking (`networkingMode=mirrored`) | With ProxyJump to `HostName 127.0.0.1`, the scripts and operations through bridge above (Git for Windows OpenSSH), and running a command with Windows OpenSSH. `::1` does not answer. Direct connections from the network are blocked by default and work after allowing them with `New-NetFirewallHyperVRule` | Same | Verified |
| Route through `wsl.exe` | Starting MCP, 1,020,000 bytes of standard input, symbol interpretation by cmd | Same | Verified |

Table: Verification status of connecting to Linux in WSL

The history is in [261105-wsl-connection-validation.md](../prompts/past/261105-wsl-connection-validation.md) and [261105-wsl-boot-mirrored-validation.md](../prompts/past/261105-wsl-boot-mirrored-validation.md) (Japanese only).

## Troubleshooting

| Symptom | Cause and remedy |
|---|---|
| Agent reports "cannot connect" or "server exited" | Run the registered command locally with `--version` to verify the SSH route and binary path, and confirm `--mcp` is included |
| JSON parse error during initialization | A shell startup file on the target writes to standard output ([how to check](#keep-the-target-shell-quiet)) |
| Connection hangs without responding | An SSH hop is waiting for a password or host key confirmation. Run `ssh hop1 ssh target` interactively once to record host keys, and add `BatchMode=yes` |
| Pseudo-terminal warning or corrupted line breaks | Pass `-T` to every `ssh` hop and check `RequestTTY` in `~/.ssh/config` |
| Server exits immediately with `open audit log` | The audit log directory is not writable. Pass a writable path with `--log-file` or disable logging with `--no-log` |
| `sudo: a password is required` | No `NOPASSWD` rule matches in `sudoers` ([Operations Guide](operations.md#configuring-sudo)). Pass the password via `password` in `exec_sudo` |
| Tool returns an error starting with `policy:` | Blocked by target startup options. Check active rules in `restrictions` of `sys_get_info` |
| Tool covered by `--confirm` fails with `cannot ask the user` | The agent client does not support MCP elicitation. Remove `--confirm` and use agent-side approval settings |
| `transfer_push` and `transfer_pull` are missing | Registered without `bridge`, or the `files` group is disabled on the target (`bridge` logs the reason to standard error at startup) |
| `bridge` exits with `remote host-shell disconnected` | The underlying SSH session closed. Inspect the `ssh` error in the agent's MCP log (`bridge` standard error) |
| Command stops with `timed_out` | Increase `timeout_sec` (up to `--max-timeout`) or run the command as a background job with `job_start` |
| Garbled Japanese text | Check `console_encoding` and `file_encoding` in `sys_get_info`, and specify `output_encoding` or `charset` explicitly when files or commands use a non-default encoding ([Encodings and line endings in the Reference](reference.md#encodings-and-line-endings)) |
| `host-shell.exe` is deleted or blocked on Windows | Antivirus false positive; configure an exclusion or sign the binary |
| Public key authentication fails for a Windows administrator | Windows OpenSSH reads public keys for `Administrators` group members from `C:\ProgramData\ssh\administrators_authorized_keys` |

Table: Troubleshooting

If the issue persists, inspect the failed tool call and error message in the target's audit log ([Audit log in the Reference](reference.md#audit-log)).
