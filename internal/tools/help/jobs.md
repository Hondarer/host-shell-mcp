# Background jobs

1. `job_start` takes the same arguments as `exec_run` and returns at once with `id` (e.g. `job-1`).
   Without `timeout_sec` a job has no time limit.
2. `job_read` with `offset: 0` returns output so far (stdout and stderr interleaved) and `next_offset`.
   Pass `next_offset` as `offset` next time to get only new output. Set `wait_sec` (up to 30) to wait
   for new output or the end of the job instead of polling quickly.
3. `status.running` becomes false and `status.exit_code` is set when the job ends.
4. `job_stop` kills the job and its child processes. `job_list` shows all jobs of this session.

Each job keeps the most recent part of its output (four times the output limit). If the job writes
faster than you read, older output is discarded and `dropped` says how many bytes you missed.
`more: true` means output beyond `next_offset` is already available: read again right away.

## Interactive input

Start with `stdin_open: true`, then send input with `job_write`:

```json
{"id": "job-3", "input": "y\n"}
{"id": "job-3", "close": true}
```

- Input is sent as is; add `\n` to submit a line. `close: true` sends end of input (like Ctrl-D).
- `stdin_encoding` of `job_start` also applies to `job_write`.
- Many programs buffer output or disable prompts when not attached to a terminal. host-shell
  does not provide a terminal (no PTY): prefer non-interactive options when they exist, and use
  flags such as `python -u` or `stdbuf -oL` to get output promptly.

Jobs are stopped when the MCP session ends (the SSH connection closes). For work that must survive,
start it detached on the host (`nohup ... &`, `systemd-run`, a scheduled task) and check its log files.
