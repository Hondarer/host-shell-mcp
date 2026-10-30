# Administration

## Processes

- `sys_processes` lists processes as objects: `pid`, `ppid`, `name`, `command_line`, `user`, `state`,
  `start_time`, `memory_bytes`. `filter` matches the name or command line; `pid` returns one process
  with its descendants (follow `ppid` for the tree); `max` (default 500) caps the list.
- `proc_kill` sends a signal to a pid: Linux `terminate` (default), `kill`, `interrupt`, `hangup`, ...;
  Windows `kill`. `tree` includes descendants. On Linux `sudo: true` sends it as root.
  For jobs started with `job_start`, use `job_stop`.

## Services

- `svc_list` lists services with `state` (running, stopped, failed, ...), `start_mode`
  (enabled/disabled on Linux; auto/manual/disabled on Windows) and pid. Filter with `filter` and `state`.
- `svc_control` with `action`: `status` (default; details and, on Linux, `systemctl status` with recent
  log lines), `start`, `stop`, `restart`, `reload` (Linux), `enable`, `disable`. The result is the status
  after the action.
- Linux: system units need root. Set `sudo: true` (sudo -n, needs a NOPASSWD rule) unless host-shell runs
  as root. `user: true` works on the user's own units (`systemctl --user`).
- Windows: changes need an elevated session.

## Logs

`log_query` returns structured entries (time, priority, source, pid, message), newest last.

```json
{"unit": "nginx", "since": "1h", "priority": "warning"}
{"log": "kernel", "since": "30m"}
{"log": "Application", "unit": "MSSQLSERVER", "priority": "error", "lines": 50}
{"log": "System", "event_id": 7036, "since": "1d", "grep": "stopped"}
```

- Linux reads the systemd journal (`journalctl`). Users not in the `systemd-journal` or `adm` group see
  only their own entries; `sudo: true` reads everything.
- Windows reads the Event Log (`wevtutil`). `log` is the log or channel name, `unit` the provider.
- `since`/`until`: `30m`, `2h`, `1d` or a timestamp. `grep` is a case-insensitive regular expression.
- For applications that write their own log files, use `fs_read` with a negative `offset` (tail) or `search_grep`.

## Disks

`sys_disks` lists file systems (Linux) or drives (Windows) with total, used and free space.

## Windows registry

```json
{"key": "HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion", "value": "ProductName"}
{"key": "HKCU\\Software\\MyApp", "value": "Level", "type": "REG_DWORD", "data": 3}
{"key": "HKCU\\Software\\MyApp", "recursive": true}
```

- `reg_read` without `value` lists subkeys and all values. Data is a string, an array of strings
  (REG_MULTI_SZ), a number (REG_DWORD, REG_QWORD) or base64 (binary). `expanded` resolves `%VARS%`.
- `reg_write` creates the key if needed; omit `value` to only create the key.
- `reg_delete` deletes a value (`value`) or a key (`recursive` for keys with subkeys).
- `view`: `64` or `32` selects the WOW64 registry view.
