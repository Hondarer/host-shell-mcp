# Running commands

## exec_run

Runs one command, waits for it, and returns `exit_code`, `stdout`, `stderr` and `duration_ms`.
A non-zero exit code is a normal result, not a tool error.

- `command` is a command line for a shell (`sh` on Linux, the configured shell on Windows; see
  `default_shell` in `sys_info`). Pipes, redirects and `&&` work.
- `argv` runs a program directly with exact arguments and no shell: no quoting problems, no globbing.
  Prefer it when arguments contain spaces, quotes or user data.
- `shell` picks another shell: `sh`, `bash`, `pwsh` on Linux; `cmd`, `powershell`, `pwsh` on Windows.
- Every call is a new process. `cd` and `export` do not carry over: pass `cwd` and `env`.
- Standard input is empty unless you pass `stdin`, so a command that asks a question fails instead
  of hanging. Use non-interactive flags (`-y`, `--batch`, `DEBIAN_FRONTEND=noninteractive`) or a job
  with `stdin_open` (topic `jobs`).
- `timeout_sec` limits the run (server default and maximum apply). On timeout the whole process
  tree is killed and `timed_out: true` is returned with the output so far.
- Output beyond the limit keeps the head and tail with a marker in the middle and sets
  `stdout_truncated`. Reduce output (`| tail -n 100`, `--quiet`, redirect to a file and `fs_read` it).
- Output is converted to UTF-8 (`output_encoding`, default `auto`). See topic `encoding`.
- If your client sends a progress token, `exec_run` and `sudo_run` report progress every second
  (elapsed time, output size, last line). If your client cancels the request, the process tree is
  killed at once.

## sudo_run (Linux)

`sudo -n -- sh -c <command>`, optionally `-u user`. It never prompts: without a matching NOPASSWD
sudoers rule it fails with "a password is required". Narrow rules are often written for specific
commands; then run `exec_run` with `argv: ["sudo", "-n", "/usr/bin/systemctl", "restart", "app"]`.
If the user gives you the password, run `sudo` in a pty job and type it with `job_write`
(the password is not written to the audit log).

## Choosing

| Situation | Use |
|---|---|
| Finishes within a minute, no input needed | `exec_run` |
| Builds, test suites, downloads, servers, `tail -f` | `job_start` + `job_read` |
| Programs reading stdin interactively | `job_start` with `stdin_open` + `job_write` |
| Password prompts, REPLs, `ssh`, `top`, anything that needs a terminal | `job_start` with `pty` + `job_write` |
| Reading or changing files | `fs_*` tools rather than `cat`, `sed -i`, `echo >` |
