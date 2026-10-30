# Windows hosts

- The default shell is PowerShell (`powershell`, or as configured in `default_shell` in `sys_info`). Pass `"shell": "cmd"` or `"shell": "pwsh"` to `exec_run` or `job_start` to override per call, or start host-shell with `--windows-shell cmd` to change the server default.
- PowerShell commands run via `-EncodedCommand`, so quotes, `$`, and newlines in `command` require no extra escaping, and output is UTF-8. Like `sh -c`, the exit code reflects the last statement (0 on success, the native exit code if a native program failed, or 1 if a cmdlet failed). Prepend `$ErrorActionPreference = 'Stop'` to halt on the first error.
- For `cmd`, `command` is passed verbatim to `cmd /d /s /c` using `cmd.exe` quoting rules.
- If a native program invoked from PowerShell produces garbled output, run it with `shell: "cmd"` so host-shell decodes using the console code page (`console_encoding`).
- Paths accept `\` or `/`; absolute paths require a drive letter (e.g. `C:\work`).
- Prefer `fs_*` tools over `Remove-Item`, `Copy-Item`, and similar cmdlets for cross-platform consistency.
- `sudo_run` is not available on Windows; privileges depend on the account running host-shell.
- Stopping a job or hitting a timeout terminates the process tree (`taskkill /T /F`). Only the `kill` signal is supported, except in `pty` jobs where `interrupt` sends Ctrl-C.
- `pty` uses ConPTY (requires Windows 10 1809 / Windows Server 2019 or later).
- `svc_list` and `svc_control` use the Service Control Manager; starting, stopping, or reconfiguring services requires an elevated administrator session. `log_query` reads the Event Log (`log`: `System`, `Application`, `Security`, or a channel name; `unit`: provider name); reading `Security` requires administrator privileges.
- `reg_read`, `reg_write`, and `reg_delete` access the Windows registry. Writing to `HKLM\...` requires administrator privileges; `HKCU\...` does not.
