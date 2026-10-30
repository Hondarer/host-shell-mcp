# host-shell overview

host-shell runs **on the remote host** over SSH stdio (including multi-hop routes) and executes every operation there. Without port forwarding, SFTP, or secondary connections, these tools are the only interface to the host.

- Paths, programs, environment variables, and permissions belong to the remote host and the user running host-shell. Local files are not visible.
- All text sent and received is UTF-8. Host encodings (Shift_JIS, EUC-JP, UTF-16, etc.) are converted on the remote host. See topic `encoding`.
- Each call returns at most the server output limit. Larger results are truncated and flagged with `truncated` or `stdout_truncated`. Narrow the request instead of retrying the same call.
- Tool errors include a message explaining what to change.
- The administrator may restrict tools, paths, commands, or network destinations. See `sys_info` (`tool_groups`, `restrictions`) and topic `policy`; rejected calls state the reason.

## Tools at a glance

| Task | Tool |
|---|---|
| Inspect host OS, shell, encodings, and user | `sys_info` |
| List a directory | `fs_list` |
| Find files by name or path glob (`**/*.py`) | `search_files` |
| Search file contents (regex, context lines) | `search_grep` |
| Read a text file by lines, or any file by byte range | `fs_read` |
| Replace exact text in a file | `fs_edit` |
| Create or overwrite a file, append, write at an offset | `fs_write` |
| Create directories, move, copy, delete | `fs_mkdir`, `fs_move`, `fs_copy`, `fs_remove` |
| File attributes | `fs_stat` |
| List file sizes and SHA-256 hashes (compare directory trees) | `fs_manifest` |
| Run a command and wait | `exec_run` |
| Long-running or interactive commands | `job_start`, `job_read`, `job_write`, `job_stop`, `job_list` |
| Interactive terminal programs (password prompts, REPLs, ssh) | `job_start` with `pty` |
| Run as root (Linux, requires a NOPASSWD sudoers rule) | `sudo_run` |
| Send HTTP requests from the host | `net_http` |
| Send and receive over raw TCP/TLS (Redis, SMTP, etc.) | `net_tcp` |
| Check DNS, ports, and certificates | `net_check` |
| Listening ports, network interfaces | `net_listen`, `net_interfaces` |
| List or signal processes | `sys_processes`, `proc_kill` |
| Services (systemd, Windows services) | `svc_list`, `svc_control` |
| System logs (journald, Windows Event Log) | `log_query` |
| Disk space | `sys_disks` |
| Windows registry | `reg_read`, `reg_write`, `reg_delete` |
| Transfer files between local and remote hosts (requires `host-shell bridge`) | `transfer_push`, `transfer_pull` |

Tools are exposed only when their tool group is enabled (`tool_groups` in `sys_info`).

## Recommended workflow

1. Call `sys_info` once to check `os`, `default_shell`, `cwd`, `home`, and the encodings.
2. Locate code with `search_files` and `search_grep` instead of `ls -R` or `grep -r` via `exec_run`: the dedicated tools enforce output limits, work identically on Linux and Windows, and convert encodings.
3. Read needed ranges with `fs_read` using `line_start` and `line_numbers: true`.
4. Edit with `fs_edit`, which preserves encoding, BOM, and line endings and fails safely when the target text is missing or ambiguous. Use `fs_write` only for new files or full rewrites.
5. Build, test, and run with `exec_run`. Use `job_start` for commands taking over a minute or requiring interactive input (`pty` when a terminal is required).
6. Inspect host services directly: call `net_http` against `localhost` without port forwarding, and check logs and state with `log_query` or `svc_control` (`status`).

## Topics

Call `help` with `topic` set to one of: `overview`, `files`, `edit`, `search`, `commands`,
`jobs`, `network`, `admin`, `transfer`, `encoding`, `windows`, `policy`, `limits`.
