# host-shell overview

host-shell runs **on the remote host** and performs every operation there. It is reached through
plain SSH standard input/output (often several hops), so there is no port forwarding, no SFTP and
no second connection: these tools are the only way to touch the host.

- Paths, programs, environment variables and permissions are those of the remote host and the
  user host-shell runs as. Your local files are not visible.
- All text you send and receive is UTF-8. Host encodings (Shift_JIS, EUC-JP, UTF-16 ...) are
  converted on the host side. See topic `encoding`.
- Each call returns at most the server output limit. Larger results are cut, and a `truncated`
  (or `stdout_truncated`) flag tells you so. Narrow the request instead of retrying the same call.
- Errors come back as tool errors with a message that says what to change.
- The administrator may have limited the tools, paths, commands or network destinations. `sys_info`
  (`tool_groups`, `restrictions`) and topic `policy` show what applies; a refused call says why.

## Tools at a glance

| Task | Tool |
|---|---|
| Learn about the host (OS, shell, encodings, user) | `sys_info` |
| List a directory | `fs_list` |
| Find files by name or path glob (`**/*.py`) | `search_files` |
| Search file contents (regex, context lines) | `search_grep` |
| Read a text file by lines, or any file by byte range | `fs_read` |
| Change part of a file | `fs_edit` |
| Create or overwrite a file, append, write at an offset | `fs_write` |
| Create directories, move, copy, delete | `fs_mkdir`, `fs_move`, `fs_copy`, `fs_remove` |
| File attributes | `fs_stat` |
| Size and SHA-256 of many files (compare trees) | `fs_manifest` |
| Run a command and wait | `exec_run` |
| Long-running or interactive commands | `job_start`, `job_read`, `job_write`, `job_stop`, `job_list` |
| Programs that need a terminal (password prompts, REPLs, ssh) | `job_start` with `pty` |
| Run as root (Linux, NOPASSWD sudoers rule needed) | `sudo_run` |
| Call an HTTP API or web page reachable from the host (no port forwarding needed) | `net_http` |
| Talk to a non-HTTP TCP service (Redis, SMTP, ...) | `net_tcp` |
| Check DNS, ports and certificates | `net_check` |
| Listening ports, network interfaces | `net_listen`, `net_interfaces` |
| Processes; signal or kill one | `sys_processes`, `proc_kill` |
| Services (systemd, Windows services) | `svc_list`, `svc_control` |
| System log (journald, Windows Event Log) | `log_query` |
| Disk space | `sys_disks` |
| Windows registry | `reg_read`, `reg_write`, `reg_delete` |
| Copy files between your machine and the host (with host-shell bridge) | `transfer_push`, `transfer_pull` |

Tools appear only when their group is enabled (`tool_groups` in `sys_info`).

## Recommended workflow

1. Call `sys_info` once. Note `os`, `default_shell`, `cwd`, `home` and the encodings.
2. Locate code with `search_files` and `search_grep` instead of `ls -R` or `grep -r` through
   `exec_run`: the dedicated tools are bounded, portable between Linux and Windows, and handle encodings.
3. Read with `fs_read` using `line_start` and `line_numbers: true`. Read only what you need.
4. Edit with `fs_edit`. It keeps the file encoding, BOM and line endings, and fails safely
   when the text to replace is missing or ambiguous. Use `fs_write` only for new files or full rewrites.
5. Build, test and run with `exec_run`. Use `job_start` for anything that may take longer than a
   minute or that needs input while it runs (`pty` when it needs a terminal).
6. To check a service the host runs, use `net_http` against `localhost` instead of asking for
   port forwarding, and `log_query` or `svc_control` (`status`) for its logs and state.

## Topics

Call `help` with `topic` set to one of: `overview`, `files`, `edit`, `search`, `commands`,
`jobs`, `network`, `admin`, `transfer`, `encoding`, `windows`, `policy`, `limits`.
