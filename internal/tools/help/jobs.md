# Background jobs

1. `job_start` takes the same arguments as `exec_run` and returns at once with `id` (e.g. `job-1`).
   Without `timeout_sec` a job has no time limit.
2. `job_read` with `offset: 0` returns output so far (stdout and stderr interleaved) and `next_offset`.
   Pass `next_offset` as `offset` next time to get only new output. A negative `offset` reads the
   last bytes (`-4000` for the tail).
3. `status.running` becomes false and `status.exit_code` is set when the job ends.
4. `job_stop` kills the job and its child processes. `job_list` shows all jobs of this session
   (the 50 most recent finished jobs are kept).

Jobs and their child processes end when host-shell exits (the session closes), even if host-shell is
killed. To keep a program running after the session, start it outside host-shell: on Linux with
`systemd-run --user --unit=NAME ...`, on Windows as a service or a scheduled task (`schtasks`,
`Register-ScheduledTask`).

Each job keeps the most recent part of its output (four times the output limit). If the job writes
faster than you read, older output is discarded and `dropped` says how many bytes you missed.
`more: true` means output beyond `next_offset` is already available: read again right away.

## Waiting for the right moment

`job_read` with `wait_sec` (up to 300) waits instead of polling, and returns early when the job ends.

| Add | Returns when |
|---|---|
| nothing | any new output arrived |
| `until: "Listening on"` | the new output matches the regular expression (`matched: true`) |
| `until: "\\$ $"` or `">>> $"` | a prompt is shown (end of the output) |
| `idle_ms: 500` | output arrived and then stayed quiet for 500 ms |

## Input

Start with `stdin_open: true` (pipe) or `pty: true` (terminal), then send input with `job_write`:

```json
{"id": "job-3", "input": "y\n"}
{"id": "job-3", "close": true}
```

- Input is sent as is; add `\n` to submit a line. `close: true` sends end of input (like Ctrl-D).
- `stdin_encoding` of `job_start` also applies to `job_write`.

## Terminal (pty)

`pty: true` runs the program in a pseudo terminal (Linux pty, Windows ConPTY). Use it for programs
that refuse to work without a terminal: `sudo` or `ssh` asking for a password, `python`/`node`/`psql`
REPLs with prompts, `top`, installers, and tools that buffer or hide output when piped.

- stdout and stderr are one stream. Input is always open.
- `job_read` removes colours, cursor movement and other escape sequences, and keeps only the final
  text of lines redrawn with carriage returns. Set `raw: true` to get the bytes as the terminal got them.
- For full-screen programs (`top`, `vi`, `less`, installers with menus) use `job_read` with
  `screen: true`: it returns what the terminal shows now, one line per row, plus `cursor_row` and
  `cursor_col`. Combine it with `wait_sec` and `idle_ms` so the program has finished drawing.
- In `job_write`, control keys are characters: `\u0003` Ctrl-C, `\u0004` Ctrl-D, `\u001b` Esc,
  `\u001b[A` up arrow, `\t` tab. Some programs expect `\r` for Enter. `cols` and `rows` resize the terminal
  (default 120x40).
- Typical exchange: `job_write` the command, then `job_read` with `wait_sec: 30, idle_ms: 500` or with
  `until` set to the prompt.
- `TERM` is `xterm-256color` on Linux unless you set it in `env`.

## Signals

`job_stop` with `signal`:

- `kill` (default): kill the whole process tree.
- Linux: `interrupt`, `terminate`, `hangup`, `quit`, `stop`, `continue`, `usr1`, `usr2` go to the
  job's process group, then `job_stop` waits up to `wait_sec` (default 5) and returns the status.
- pty jobs (also on Windows): `interrupt` types Ctrl-C into the terminal.

Without a pty, many programs buffer output or disable prompts: use flags such as `python -u` or
`stdbuf -oL` to get output promptly, or use `pty`.

Jobs are stopped when the MCP session ends (the SSH connection closes). For work that must survive,
start it detached on the host (`nohup ... &`, `systemd-run`, a scheduled task) and check its log files.
