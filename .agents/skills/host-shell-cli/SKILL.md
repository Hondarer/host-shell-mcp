---
name: host-shell-cli
description: Use a host-shell target (an SSH host or container prepared with the host-shell-connect skill) through shell commands when the agent cannot use MCP servers, for example because registering MCP servers is disabled. Starts a local host-shell session, lists the tools, calls them with host-shell call, and stops the session. Use for requests such as "operate target with host-shell, but MCP is not allowed here" or "use host-shell from the command line".
---

# Using a host-shell target without MCP

`host-shell session start` keeps the connection to the target (the same bridge that `host-shell bridge` runs for MCP clients) running on this machine. `host-shell call TOOL ...` and `host-shell tools` connect to that session once per command, so an agent that can only run shell commands gets the same tools as through MCP: `exec_run`, `job_*`, `fs_*`, `search_*`, `transfer_push`, `transfer_pull`, `forward_*` and the rest.

If the host-shell tools are already available to you through MCP, use them instead of this skill.

The user documentation is `docs/en/cli-without-mcp.md`.

## Before you start

1. **Check that the user's organization allows it.** A rule that disables MCP may mean "agents must not reach other machines", not just "no MCP servers". Before the first use, ask the user to confirm that their policy allows using the target through shell commands. Do not use this skill to work around a restriction the user has not cleared.
2. **The target must be prepared.** Use the `host-shell-connect` skill to check the route, deploy `host-shell` and run `probe.sh`. At its last step ("Presenting the registration command"), choose this skill instead of MCP registration.
3. **Find the local host-shell.** Use `host-shell` on `PATH`, or the executable for this machine in `.agents/skills/host-shell-connect/bin/` (`host-shell-linux-amd64`, `host-shell-darwin-arm64`, `host-shell-windows-amd64.exe`, ...; check with `uname -sm` or `$env:PROCESSOR_ARCHITECTURE`). Below, `host-shell` means that path.

## Start a session

The session command is the registration command of `host-shell-connect` with `session start` in place of `bridge`. Bridge options (`--proxy`, `--forward-*`, `--remote-dns`, `--allow-local-path`, `--no-transfer`) go before `--`, target options after `--mcp`, as for `bridge`.

Start it yourself in the background when the route needs no typing (key authentication, or passwords stored with `host-shell ssh-pass`) and the target is not started with `--confirm`:

```bash
host-shell session start --detach -- ssh -T -o BatchMode=yes hop1 ssh -T -o BatchMode=yes target /tmp/host-shell-1000/host-shell --mcp
```

It prints the session state as JSON when the target is connected, or the session log and exit status 1 when it fails. A detached session cannot ask anything, so it fails rather than waiting for a password.

Ask the user to run it in their own terminal, without `--detach`, when:

- the route asks for a password or a second factor, or
- the target command has `--confirm`: a foreground session asks the user in its terminal before such tools run; a detached session refuses them.

```bash
host-shell session start -- ssh -T hop1 ssh -T target /tmp/host-shell-1000/host-shell --mcp --confirm destructive
```

The session prints `session "default" is ready` when it is connected. Then continue with the commands below. The session runs until the user presses Ctrl+C or you run `host-shell session stop`.

For several targets, give each session a name with `--name NAME` and select it with `--session NAME` (or `HOST_SHELL_SESSION=NAME`) in `call` and `tools`.

## Learn the tools

```bash
host-shell tools                       # one line per tool
host-shell tools --instructions        # target OS, shell, encodings, limits and enabled features
host-shell tools --schema exec_run     # full description and input schema
host-shell call help_get topic=commands
```

Read `--instructions` once per session: it says whether the target is Windows or Linux, which shell `exec_run` uses, and which features (transfer, port forwarding, proxy) are on.

## Call tools

`TOOL` comes right after `call`; options and arguments follow.

```bash
host-shell call exec_run command='ls -la /var/log'
host-shell call fs_read path=/etc/hosts
host-shell call exec_run argv=sh argv=-c argv='echo "$HOME"'   # arrays of strings: repeat the key
host-shell call fs_write path=/tmp/app.conf content=@app.conf   # value from a local file
host-shell call job_start command='make test' --quiet
```

- `KEY=VALUE` is converted to the type in the input schema (string, integer, number, `true`/`false`, JSON for arrays and objects). `KEY=@FILE` reads the value from a local file; write `@@` for a value starting with `@`.
- When a value contains quotes that your shell would mangle (PowerShell commands, JSON, scripts), pass all arguments as JSON on standard input:

  ```bash
  host-shell call exec_run --json - <<'EOF'
  {"command": "Get-ChildItem 'C:\\Program Files' | Select-Object -First 5", "shell": "powershell"}
  EOF
  ```

- The tool's JSON result is printed on one line to standard output, as an MCP client would receive it. Progress goes to standard error (`--quiet` hides it).
- Exit status: `0` success (for `exec_run`, read `exit_code` in the JSON: the command's own failure is still a successful call); `1` the tool reported an error, printed to standard error; `2` wrong usage (unknown tool or argument; the message lists the valid arguments); `3` no session, or the session or target connection was lost; `130` interrupted.
- Your shell tool has a time limit. For anything that may take long, use `job_start` and then `job_read` with `wait_sec` (and `until` for a pattern): it waits on the target, so you need no local `sleep` between calls. Or give `exec_run` a `timeout_sec` shorter than your shell's limit. When your shell kills `host-shell call`, the call is cancelled on the target.
- Run one `host-shell` command per shell call, without variables for the executable or other commands mixed in, so that permission rules for `host-shell` match.
- `transfer_push` and `transfer_pull` copy files between this machine and the target without passing the contents through your context; local paths are relative to the directory where you run `host-shell call`.

The other skills (`host-shell-code-server`, `host-shell-vscode-remote-ssh`, `host-shell-coding-agents`) describe their steps with MCP tool names. Run each step as `host-shell call TOOL` with the same arguments. Where they ask the user to add options to the registration, add them to `session start` instead (stop and start the session again).

## Tools that need confirmation

If a call fails with "this tool needs the user's confirmation, but this session cannot ask", the target was started with `--confirm`. Never try to answer the confirmation yourself and never remove `--confirm` from the target command. Tell the user which call needs approval and ask them to start the session in the foreground in their terminal (stop the detached one first). When a foreground session asks, wait for the user to answer in that terminal; a declined call fails with exit status 1.

## Permissions

Each tool is a separate command prefix, so agent permission rules can allow tools one by one, for example in Claude Code:

```json
{"permissions": {"allow": ["Bash(host-shell call fs_read:*)", "Bash(host-shell call search_grep:*)", "Bash(host-shell tools:*)"]}}
```

Suggest such rules to the user if they want fewer prompts; do not edit permission settings yourself.

## End the session

```bash
host-shell session stop                 # or: host-shell session stop --name NAME
host-shell session list                 # sessions still running
```

Stop the session when the work is done. Like an MCP session, it holds target resources only while connected: stopping it ends the commands and jobs it started on the target and closes its port forwards and proxy. If the session must stay up for a forward (for example code-server in the user's browser), tell the user that it is still running and how to stop it. `--idle-timeout 30m` on `session start` ends a session that no `host-shell call` has used for that long.

## Troubleshooting

| Symptom | Action |
|---|---|
| `no session "NAME" is running` (exit status 3) | Run `host-shell session list`. Start the session, or pass the right `--session`. |
| `session start --detach` prints a log and exits 1 | Read the log: it is the output of the route (ssh errors) and of the target. Check the route with `host-shell-connect` (`remote.sh`, `probe.sh`). |
| A detached session ended later | Its log is `NAME.log` next to the socket shown by `host-shell session status` (or in the session directory: `$XDG_RUNTIME_DIR/host-shell/sessions`, `/tmp/host-shell-UID/sessions`, `%LOCALAPPDATA%\host-shell\sessions`). |
| `socket path ... is too long` | Set `HOST_SHELL_SESSION_DIR` to a short directory that only the user can access. |
| `... is accessible to other users` | The session directory must be private (mode 0700). Fix it as the message says or choose another directory with `HOST_SHELL_SESSION_DIR`. |

## Report

When you finish, tell the user which session you started (name, detached or foreground), whether it is still running, and how to stop it.
