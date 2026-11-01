# host-shell Operations Guide

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/operations.md](../ja/operations.md)) is the single source of truth (SSOT) and takes precedence if they differ.

This guide covers permission design, restriction options, and audit log management for operating `host-shell` safely.  
For initial setup, see the [User Guide](user-guide.md); for detailed specifications, see the [Reference](reference.md).

## Contents

- [Security model](#security-model)
- [Dedicated user and key](#dedicated-user-and-key)
- [Restriction options](#restriction-options)
- [Pinning the command to start](#pinning-the-command-to-start)
- [Configuring sudo](#configuring-sudo)
- [Approval on the agent side](#approval-on-the-agent-side)
- [Managing the audit log](#managing-the-audit-log)
- [Updating](#updating)
- [Checklist before operation](#checklist-before-operation)

## Security model

By default, `host-shell` runs with the full permissions of the connecting user. Exposing it to an agent without restriction options grants the same access as an SSH login under that user.

In production, combine the following four layers.

| Layer | Mechanism | Protection |
|---|---|---|
| Target OS | Dedicated user, file permissions, `sudoers` rules | Enforced boundary of allowed operations |
| host-shell | [Restriction options](#restriction-options) | Published tools, allowed paths, allowed commands, destination hosts, user confirmation |
| Agent | Pre-execution approval for mutating tools | Prevention of unintended changes |
| Audit | `host-shell` audit log | Auditing and tracing executed operations |

Table: Security layers

## Dedicated user and key

Create a dedicated OS user for the agent on the target with minimal privileges.

- For investigation only, use an unprivileged user with read access to logs and source files.
- Grant write access via group permissions or ACLs only to directories that require modification.
- Grant administrator privileges for specific commands via [Configuring sudo](#configuring-sudo).

Generate a dedicated SSH key for the agent, separate from personal or other service keys. Revoking this key immediately cuts off agent access.

```bash
ssh-keygen -t ed25519 -f ~/.ssh/id_host_shell -C "host-shell agent"
```

Configure the dedicated key per target in the client's `~/.ssh/config`.

```{.text caption="~/.ssh/config (client)"}
Host target
    HostName 192.0.2.10
    User hsagent
    IdentityFile ~/.ssh/id_host_shell
    IdentitiesOnly yes
    RequestTTY no
```

When connecting through a jump host, add the corresponding next-hop configuration to `~/.ssh/config` on the jump host.

## Restriction options

Startup options limit the operations allowed through `host-shell` (unspecified items remain unrestricted; see [Restrictions (policy) in the Reference](reference.md#restrictions-policy)).

| Use case | Example options |
|---|---|
| Troubleshooting only (no changes) | `--read-only` |
| Reading logs and configs only | `--tools files,search --read-only --allow-read-path /var/log --allow-read-path /etc/app` |
| Development in a specific directory | `--tools files,search,exec --allow-path /srv/app --allow-command git,make,go,npm,node` |
| Require user confirmation before changes | `--confirm destructive` |
| Block external network connections | `--allow-host localhost,127.0.0.1,::1,unix:/var/run/*.sock` |
| Suppress secrets in output and child environments | `--redact --unset-env 'AWS_*,*TOKEN*,*SECRET*,*PASSWORD*'` |

Table: Examples of restriction options

Keep the following points in mind when configuring restriction options.

- Restrictions validate tool arguments only. If the `exec` group (`exec_run`, etc.) is published, spawned commands can access arbitrary files and network destinations. To enforce strict boundaries, exclude `exec` via `--tools` or restrict allowed binaries via `--allow-command` (allowing shells such as `sh` or `powershell` permits arbitrary command execution).
- `--deny-command` matches command line strings against regular expressions and can be bypassed by rephrasing commands. Use it as a safeguard against accidental commands, not as a security boundary.
- `--confirm` requires a client that supports MCP elicitation (unsupported clients receive an error on covered tools).
- `--redact` masks only secrets matching known patterns. Protect sensitive files from being read using OS file permissions.
- Pin startup options in the target's `authorized_keys` as shown in [Pinning the command to start](#pinning-the-command-to-start) so users and agents cannot override them.

## Pinning the command to start

Setting `command=` in the target's `authorized_keys` restricts the dedicated key to running `host-shell`.  
Even if the key is compromised, interactive shell access and port forwarding are blocked, and all tool calls are recorded in the audit log.

> [!IMPORTANT]
> host-shell's [HTTP proxy through the client](client-proxy.md) and [port forwarding](port-forward.md) do not use sshd forwarding, so `no-port-forwarding` or `AllowTcpForwarding no` cannot stop them. They are available only when the target is started with `--client-proxy`, `--allow-forward-local`, or `--allow-forward-remote`. To forbid them, leave these options out of `command=`.

```{.text caption="~hsagent/.ssh/authorized_keys (Linux target)"}
command="/opt/host-shell/host-shell --mcp",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

```{.text caption="C:\Users\hsagent\.ssh\authorized_keys (Windows target)"}
command="C:/tools/host-shell.exe --mcp",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

With `command=`, any command supplied by the client is ignored and `host-shell` always starts (the agent registration command only needs `ssh -T target`).  
Including [restriction options](#restriction-options) in `command=` prevents the client from removing them.

```{.text caption="~hsagent/.ssh/authorized_keys (pinned to read-only)"}
command="/opt/host-shell/host-shell --mcp --read-only --redact",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

`no-pty` disables PTY allocation for the SSH session itself without affecting pseudo-terminals created by `host-shell` for `pty` jobs.  
Without restriction options, `host-shell` can run arbitrary commands with the connecting user's permissions. Enforce upper privilege bounds via the OS user and `sudoers`.

```{.text caption="~hsagent/.ssh/authorized_keys (permitting remote forwards only, listening on port 15432)"}
command="/opt/host-shell/host-shell --mcp --allow-forward-remote --allow-forward-remote-port 15432",no-pty,no-port-forwarding,no-agent-forwarding,no-X11-forwarding ssh-ed25519 AAAA... host-shell agent
```

To pin the command per user in `sshd_config`, use `ForceCommand`.

```{.text caption="/etc/ssh/sshd_config (Linux target)"}
Match User hsagent
    ForceCommand /opt/host-shell/host-shell --mcp
    AllowTcpForwarding no
    X11Forwarding no
    PermitTTY no
```

## Configuring sudo

`exec_sudo` can run commands without a `NOPASSWD` rule when given the `password` argument. The password is supplied via askpass only when `sudo` prompts for it and is never written to the command's standard input or the audit log ([exec_sudo in the Reference](reference.md#exec_sudo)).  
However, the password string passes through the agent and LLM service. To operate without passing passwords, configure `NOPASSWD` rules in `sudoers` for specific commands.  
The `sudo` option on administration tools (`proc_kill`, `svc_control`, `log_query`) only supports passwordless `sudo -n`.

### Allowing specific commands (recommended)

List allowed commands by absolute path.

```{.text caption="/etc/sudoers.d/hsagent"}
hsagent ALL=(root) NOPASSWD: /usr/bin/systemctl status *, /usr/bin/systemctl restart nginx, /usr/bin/journalctl *
```

Have the agent invoke `sudo -n` via `argv` in `exec_run`.

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

Because `exec_sudo` executes `sudo -n -- sh -c '<command>'` internally, it cannot be used with `sudoers` rules that permit only specific commands.

### Allowing all commands

Using `exec_sudo` with `NOPASSWD` requires allowing `sh`, which grants full root access. Restrict this to test environments where full root access is acceptable.

```{.text caption="/etc/sudoers.d/hsagent"}
hsagent ALL=(root) NOPASSWD: ALL
```

Always edit `sudoers` files with `visudo -f /etc/sudoers.d/hsagent` to validate syntax.

## Approval on the agent side

Mutating tools carry the `destructiveHint` annotation, and read-only tools carry `readOnlyHint` ([Reference](reference.md#tool-annotations)).  
Configure the agent to require user approval before running at least the following tools:

- `exec_run`, `job_start`, `job_write`, `job_stop`
- `fs_edit`, `fs_write`, `fs_move`, `fs_copy`, `fs_remove`
- `exec_sudo`
- `net_request_http`, `net_exchange` (can send outbound requests from the target)
- `proc_kill`, `svc_control`, `reg_write`, `reg_delete`
- `transfer_push`, `transfer_pull` (when using `bridge`)

If the agent does not support per-tool approval, or to enforce approval on the target side, pass `--confirm destructive`.  
`fs_mkdir` only creates new directories without modifying existing data, so it sets `destructiveHint: false`.

See the [User Guide](user-guide.md#claude-code) for an example of Claude Code approval settings.  
Because read-only tools can still read sensitive files, restrict readable paths via OS user permissions and `--allow-read-path`.

## Managing the audit log

### Location

The default base path for the audit log is shown below (override with `--log-file`).  
The active log file inserts the target's local date before the extension (for example, `audit-2026-10-31.log`).

| OS | Default base path |
|---|---|
| Linux | `~/.local/state/host-shell/audit.log` |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: Default base path of the audit log

The audit log is written with the connecting user's permissions. To prevent tampering by the agent, forward logs to an external log collector.

### Daily files and retention

`host-shell` rotates log files daily by the target's local date, retains the current day plus the 5 most recent days, and automatically deletes older files (no `logrotate` configuration is needed).  
Each record opens, appends to, and closes the file, preventing interleaved lines across concurrent sessions.  
For retention beyond 5 days, forward logs to an external log management system.

Undated `audit.log` files are not deleted automatically; remove them manually if unneeded.

### Forwarding and monitoring

The audit log uses JSON Lines format (one JSON object per line), allowing collectors such as rsyslog (`imfile`) and Fluent Bit to ingest `audit-*.log` directly.

Example `jq` queries:

```bash
# Failed tool calls
jq -c 'select(.msg == "tool_call" and .is_error)' audit-*.log

# Mutating tool calls
jq -c 'select(.tool | IN("exec_run", "job_start", "job_write", "exec_sudo", "fs_edit", "fs_write", "fs_move", "fs_copy", "fs_remove"))' audit-*.log
```

### What is recorded

Command lines (`command`, `argv`) and file paths are logged in full.  
Written file contents (`content` in `fs_write`) log only character count and SHA-256 hash; replacement strings (`old_string`, `new_string` in `fs_edit`) and standard input (`stdin`, `input` in `job_write`) log only character count. See [Audit log in the Reference](reference.md#audit-log) for details.

## Updating

1. Transfer the new executable to the target under a temporary name ([Deploying in the User Guide](user-guide.md#deploying-to-the-target)).
2. Verify its SHA-256 hash against the local binary.
3. Stop the agent's MCP connection (on Windows, a running `host-shell.exe` cannot be overwritten).
4. Replace the executable.
5. Verify the new version with `--version` and reconnect the agent.

## Checklist before operation

- [ ] Created a dedicated OS user and SSH key for the agent
- [ ] Verified the dedicated user's file read and write permissions
- [ ] Selected required restriction options (`--read-only`, `--tools`, `--allow-path`, etc.)
- [ ] Restricted `sudoers` rules to required commands only
- [ ] Pinned the startup command and restriction options via `command=` in `authorized_keys` or `ForceCommand` in `sshd_config`
- [ ] Passed `-T` to every SSH hop and verified that non-interactive shells produce no extra output on standard output
- [ ] Configured pre-execution approval for mutating tools in the agent
- [ ] Configured the audit log path and external log forwarding if long-term retention is required

## Restricting client network access

[HTTP proxy through the client](client-proxy.md) describes startup, restrictions, proxy_status, environment precedence, and limits.

[Port forwarding](port-forward.md) describes the per-direction permission options, destination restrictions by the target's `--allow-host`, unauthenticated listeners, and audit records.
