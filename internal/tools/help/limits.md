# Limits, errors and audit

- **Output limit.** Each result holds at most the server output limit (shown below). Command output
  keeps the head and the tail with `... [host-shell: N bytes omitted] ...` in between. File reads and
  searches stop at the limit and tell you how to continue (`next_line`, `next_offset`, `truncated`).
- **Timeouts.** `exec_run` and `sudo_run` use `timeout_sec` or the server default, capped by the
  server maximum. Jobs have no limit unless `timeout_sec` is given.
- **Input size.** One MCP message can carry about 16 MiB. Send large files in chunks.
- **Errors.** Invalid arguments and failed operations return a tool error whose message explains what
  to change. A command that runs and exits non-zero is not an error: check `exit_code`.
- **No terminal.** Commands run without a TTY. Full-screen and password-prompting programs do not work.
- **Session.** Background jobs end with the MCP session. Nothing else is kept between sessions.
- **Audit log.** Every tool call is logged on the host (file contents and stdin are not logged, only
  their size and hash).
