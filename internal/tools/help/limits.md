# Limits, errors, and audit

- **Output limit.** Each tool result is capped at the server output limit (shown above). Command output retains the head and tail separated by `... [host-shell: N bytes omitted] ...`. File reads and searches stop at the limit and return continuation fields (`next_line`, `next_offset`, `truncated`).
- **Timeouts.** `exec_run` and `sudo_run` apply `timeout_sec` or the server default, up to the server maximum. Background jobs run without a timeout unless `timeout_sec` is specified.
- **Input size.** A single MCP message supports up to approximately 16 MiB. Split larger writes into chunks.
- **Errors.** Invalid arguments and failed operations return a tool error describing what to adjust. Commands that exit with a non-zero status return a normal result with `exit_code` set.
- **Terminal.** `exec_run` and standard jobs run without a terminal. Set `pty: true` on `job_start` for programs that require a tty.
- **Progress and cancellation.** Long-running `exec_run`, `sudo_run`, `net_http` downloads, and bridge transfers emit progress notifications when the client provides a progress token. Canceling a request terminates the operation and kills its process tree.
- **Restrictions.** Policy rules may restrict tools, paths, executables, and network targets; see `sys_info` and topic `policy`.
- **Session lifecycle.** Background jobs terminate when the MCP session closes. No state persists across sessions.
- **Audit log.** Every tool call is recorded in the host audit log; file contents and stdin are logged by byte size and SHA-256 hash only.
