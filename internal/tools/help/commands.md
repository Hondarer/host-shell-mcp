# Running commands

## exec_run

Runs a command to completion and returns `exit_code`, `stdout`, `stderr`, and `duration_ms`. A non-zero `exit_code` is returned as a normal result rather than a tool error.

- `command` runs in a shell (`sh` on Linux, `default_shell` from `sys_get_info` on Windows) and supports pipes, redirects, and `&&`.
- `argv` executes a program directly without a shell, avoiding quoting and globbing. Prefer `argv` when arguments contain spaces, quotes, or untrusted input.
- `shell` overrides the shell: `sh`, `bash`, or `pwsh` on Linux; `cmd`, `powershell`, or `pwsh` on Windows.
- Each call spawns a new process; `cd` and `export` do not persist across calls. Pass `cwd` and `env` instead.
- Standard input is closed unless `stdin` is set, so interactive prompts fail immediately instead of hanging. Pass non-interactive flags (`-y`, `--batch`, `DEBIAN_FRONTEND=noninteractive`) or run a job with `stdin_open` (see topic `jobs`).
- `timeout_sec` caps execution time (subject to the server default and maximum). On timeout, the entire process tree is killed and the call returns `timed_out: true` with the output collected so far.
- Output exceeding the server limit retains the head and tail separated by an omission marker and sets `stdout_truncated: true`. Filter verbose commands (`| tail -n 100`, `--quiet`) or redirect output to a file and read it with `fs_read`.
- Output is decoded to UTF-8 using `output_encoding` (default `auto`; see topic `encoding`).
- When the client supplies a progress token, `exec_run` and `exec_sudo` emit progress notifications every second (elapsed time, output byte count, and latest line). Canceling the request kills the process tree immediately.

## exec_sudo (Linux)

Runs `sudo -n -- sh -c <command>` (with `-u <user>` when `user` is set). Without `password`, `exec_sudo` never prompts and fails with `"a password is required"` if no NOPASSWD sudoers rule matches. When sudoers restricts NOPASSWD to specific command paths and arguments, invoke `sudo` directly via `exec_run` with `argv: ["sudo", "-n", "/usr/bin/systemctl", "restart", "app"]`.

When the user provides a sudo password, pass it in `password`. host-shell then runs `sudo -A` with itself as the askpass helper: `sudo` receives the password only when it prompts, the target command never sees it on stdin, the audit log omits it, and any occurrence in command output is replaced with `[REDACTED]`. Pass `password` only when explicitly provided by the user. Because `proc_kill`, `svc_control`, and `log_query` with `sudo: true` do not accept a password, use `exec_sudo` when a password is required. Programs that prompt on a terminal can also run in a `pty` job (`job_start` with `pty: true`, then `job_write`).

## Choosing an execution tool

| Scenario | Tool |
|---|---|
| Completes within a minute without interactive input | `exec_run` |
| Builds, test suites, downloads, servers, `tail -f` | `job_start` + `job_read` |
| Programs reading piped stdin interactively | `job_start` (`stdin_open: true`) + `job_write` |
| Password prompts, REPLs, `ssh`, `top`, or terminal-only programs | `job_start` (`pty: true`) + `job_write` |
| Reading or modifying files | `fs_*` tools instead of `cat`, `sed -i`, or `echo >` |
