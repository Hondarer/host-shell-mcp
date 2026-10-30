# Background jobs

1. `job_start` accepts the same arguments as `exec_run` and returns immediately with `id` (e.g. `job-1`). Unless `timeout_sec` is set, jobs run without a time limit.
2. `job_read` with `offset: 0` returns combined stdout/stderr output and `next_offset`. Pass `next_offset` as `offset` on subsequent calls to read incremental output, or pass a negative `offset` (e.g. `-4000`) to read the tail.
3. When the job exits, `status.running` becomes `false` and `status.exit_code` is populated.
4. `job_stop` terminates the job and its child processes. `job_list` lists active jobs and the 50 most recent finished jobs in the current session.

All jobs and their child processes are terminated when the MCP session ends (when the SSH connection closes), even if host-shell is killed. To keep a process running across sessions, launch it detached outside host-shell (e.g. `systemd-run --user --unit=NAME ...` or `nohup ... &` on Linux; a Windows service or scheduled task via `schtasks` or `Register-ScheduledTask` on Windows) and inspect its log files.

Each job buffers the most recent output up to four times the server output limit. If a job produces output faster than it is read, older bytes are discarded and reported in `dropped`. `more: true` indicates additional buffered output is available after `next_offset`.

## Waiting for output conditions

`job_read` with `wait_sec` (up to 300) blocks until a condition is met or the job exits:

| Arguments | Returns when |
|---|---|
| `wait_sec` only | Any new output arrives |
| `until: "Listening on"` | New output matches the regular expression (`matched: true`) |
| `until: "\\$ $"` or `">>> $"` | A shell or REPL prompt appears at the end of output |
| `idle_ms: 500` | New output arrives and remains quiet for 500 ms |

## Standard input

Start the job with `stdin_open: true` (pipe) or `pty: true` (pseudo-terminal), then send input with `job_write`:

```json
{"id": "job-3", "input": "y\n"}
{"id": "job-3", "close": true}
```

- `input` is written verbatim; append `\n` to submit a line. `close: true` closes the input stream (EOF / Ctrl-D).
- `stdin_encoding` set on `job_start` applies to `job_write`.

## Pseudo-terminal (pty)

`pty: true` runs the job in a pseudo-terminal (Linux pty, Windows ConPTY). Use it for programs that require a terminal, such as `sudo` or `ssh` password prompts, interactive REPLs (`python`, `node`, `psql`), full-screen tools (`top`), installers, and programs that buffer or suppress output when piped.

- stdout and stderr are merged into a single stream, and input is always open.
- By default, `job_read` strips ANSI color and cursor escape sequences and collapses lines overwritten via carriage return (`\r`). Pass `raw: true` to receive unmodified terminal bytes.
- For full-screen applications (`top`, `vi`, `less`, menu-driven installers), pass `screen: true` to `job_read` to get the rendered terminal grid (one line per row) plus `cursor_row` and `cursor_col`. Combine with `wait_sec` and `idle_ms` to read after screen redraws settle.
- Send control characters via `job_write`: `\u0003` (Ctrl-C), `\u0004` (Ctrl-D), `\u001b` (Esc), `\u001b[A` (Up arrow), `\t` (Tab), or `\r` (Enter for programs expecting CR). Resize the terminal with `cols` and `rows` (default 120x40).
- Typical interactive cycle: send a command with `job_write`, then call `job_read` with `wait_sec: 30, idle_ms: 500` or `until` matching the prompt.
- On Linux, `TERM` defaults to `xterm-256color` unless overridden in `env`.

## Signals and output buffering

`job_stop` accepts `signal`:

- `kill` (default): kills the entire process tree immediately.
- Linux signals (`interrupt`, `terminate`, `hangup`, `quit`, `stop`, `continue`, `usr1`, `usr2`): sent to the job's process group, after which `job_stop` waits up to `wait_sec` (default 5) and returns the updated status.
- `pty` jobs (Linux and Windows): `interrupt` writes Ctrl-C (`\u0003`) to the terminal.

Without `pty: true`, many programs block-buffer piped stdout or disable prompts; pass unbuffered flags (`python -u`, `stdbuf -oL`) or run with `pty: true`.
