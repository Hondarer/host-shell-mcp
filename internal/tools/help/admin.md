# Administration

## Processes

- `proc_list` returns process objects with `pid`, `ppid`, `name`, `command_line`, `user`, `state`, `start_time`, and `memory_bytes`. `filter` matches process name or command line; `pid` returns a single process and its descendants (linked via `ppid`); `max` (default 500) caps the result count.
- `proc_kill` sends a signal to `pid`: `terminate` (default), `kill`, `interrupt`, `hangup`, etc. on Linux; `kill` on Windows. `tree: true` signals all descendants. On Linux, `sudo: true` signals via `sudo -n`. To stop a job started with `job_start`, use `job_stop` instead.

## Services

- `svc_list` lists services with `state` (`running`, `stopped`, `failed`, etc.), `start_mode` (`enabled`/`disabled` on Linux; `auto`/`manual`/`disabled` on Windows), and `pid`. Filter with `filter` and `state`.
- `svc_control` performs `action` and returns the resulting service status: `status` (default; includes recent `systemctl status` log lines on Linux), `start`, `stop`, `restart`, `reload` (Linux only), `enable`, or `disable`.
- Linux: managing system units requires root. Pass `sudo: true` (runs `sudo -n`, requiring a NOPASSWD rule) unless host-shell runs as root, or pass `user: true` for user units (`systemctl --user`).
- Windows: modifying services requires an elevated session.

## Logs

`log_query` returns structured log entries (`time`, `priority`, `source`, `pid`, `message`) in chronological order (newest last).

```json
{"unit": "nginx", "since": "1h", "priority": "warning"}
{"log": "kernel", "since": "30m"}
{"log": "Application", "unit": "MSSQLSERVER", "priority": "error", "lines": 50}
{"log": "System", "event_id": 7036, "since": "1d", "grep": "stopped"}
```

- Linux queries the systemd journal (`journalctl`). Users outside the `systemd-journal` and `adm` groups see only their own entries unless `sudo: true` is set.
- Windows queries the Event Log (`wevtutil`), where `log` is the log or channel name and `unit` is the provider.
- `since` and `until` accept relative durations (`30m`, `2h`, `1d`) or timestamps. `grep` filters messages by case-insensitive regular expression.
- For application log files, use `fs_read` with a negative `offset` (tail) or `search_grep`.

## Disks

`disk_list` lists mounted file systems (Linux) or drives (Windows) with total, used, and free bytes.

## Windows registry

```json
{"key": "HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion", "value": "ProductName"}
{"key": "HKCU\\Software\\MyApp", "value": "Level", "type": "REG_DWORD", "data": 3}
{"key": "HKCU\\Software\\MyApp", "recursive": true}
```

- `reg_read` reads a named `value`, or lists subkeys and all values when `value` is omitted. Values are returned as a string, string array (`REG_MULTI_SZ`), number (`REG_DWORD`, `REG_QWORD`), or base64 string (binary). `expanded: true` expands `%VARS%` in `REG_EXPAND_SZ` values.
- `reg_write` sets a value (creating the key if needed) or creates the key alone when `value` is omitted.
- `reg_delete` deletes a value (`value`) or a key (`recursive: true` required when subkeys exist).
- `view` (`64` or `32`) selects the WOW64 registry view.
