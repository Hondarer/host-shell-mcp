# Windows hosts

- The default shell is PowerShell (`powershell`, or as configured); `cmd` and `pwsh` are available via
  `shell`. Check `default_shell` in `sys_info`.
- PowerShell scripts are passed with `-EncodedCommand`, so quotes, `$` and newlines in `command`
  need no extra escaping. The exit code of the last native command is returned. Output is UTF-8.
- For `cmd`, the command line is passed to `cmd /d /s /c` unchanged, with cmd quoting rules.
- If output of a native program run from PowerShell is garbled, run it with `shell: "cmd"`;
  host-shell then converts from the console code page (`console_encoding`).
- Paths may use `\` or `/`. Drive letters are needed for absolute paths (`C:\work`).
- Prefer `fs_*` tools over `Remove-Item`, `Copy-Item` and friends: they work the same on both systems.
- `sudo_run` does not exist on Windows. Administrative rights depend on the account host-shell runs as.
- Stopping a job or a timeout kills the whole process tree (`taskkill /T /F`).
