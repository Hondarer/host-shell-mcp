# Windows hosts

- The default shell is PowerShell (`powershell`, or as configured in `default_shell` in `sys_get_info`). Pass `"shell": "cmd"` or `"shell": "pwsh"` to `exec_run` or `job_start` to override per call, or start host-shell with `--windows-shell cmd` to change the server default.
- PowerShell scripts are passed base64-encoded, so quotes, `$`, and newlines in `command` require no extra escaping, and output is UTF-8. Errors go to stderr and `Write-Warning` to stdout, both as plain text; progress output is suppressed. Like `sh -c`, the exit code reflects the last statement (0 on success, the native exit code if a native program failed, or 1 if a cmdlet failed). Prepend `$ErrorActionPreference = 'Stop'` to halt on the first error.
- PowerShell runs in a UTF-8 console without a window, so `stdin` of any Unicode text reaches `$input` intact (topic `encoding`). Its messages, and those of programs it runs, may therefore be in English on non-English systems.
- For `cmd`, `command` is passed verbatim to `cmd /d /s /c` using `cmd.exe` quoting rules.
- If a native program invoked from PowerShell produces garbled output, run it with `shell: "cmd"` so host-shell decodes using the console code page (`console_encoding`).
- Paths accept `\` or `/`; absolute paths require a drive letter (e.g. `C:\work`).
- Prefer `fs_*` tools over `Remove-Item`, `Copy-Item`, and similar cmdlets for cross-platform consistency.
- `exec_sudo` is not available on Windows; privileges depend on the account running host-shell.
- Stopping a job or hitting a timeout terminates the process tree (`taskkill /T /F`). Only the `kill` signal is supported, except in `pty` jobs where `interrupt` sends Ctrl-C.
- `pty` uses ConPTY (requires Windows 10 1809 / Windows Server 2019 or later).
- `svc_list` and `svc_control` use the Service Control Manager; starting, stopping, or reconfiguring services requires an elevated administrator session. `log_query` reads the Event Log (`log`: `System`, `Application`, `Security`, or a channel name; `unit`: provider name); reading `Security` requires administrator privileges.
- `reg_read`, `reg_write`, and `reg_delete` access the Windows registry. Writing to `HKLM\...` requires administrator privileges; `HKCU\...` does not.
- Client proxy (`--client-proxy`): `curl.exe` (Schannel) fails with `CRYPT_E_REVOCATION_OFFLINE` when certificate revocation servers are unreachable; pass `--ssl-revoke-best-effort`. Windows PowerShell 5.1 `Invoke-WebRequest` ignores `HTTPS_PROXY`; pass `-Proxy` and `-ProxyCredential` built from the variable.

## Remote DNS on Windows

Windows has no per-process resolver hook. With `--remote-dns --remote-dns-listen 127.0.0.1:53` on the target (and `--remote-dns` on the bridge), host-shell serves A/AAAA queries on that loopback address through the bridge; `sys_get_info` reports it as `remote_dns_listen`. Programs that query DNS servers themselves (for example Node.js `dns.resolve4`) use it when the network adapter has no DNS server. For the OS resolver (`getaddrinfo`, .NET, curl, ping), queries must be routed to it. The change is machine-wide: ask the user and obtain explicit approval before making it, either way below; never make it on your own judgment.

`--remote-dns-configure` (with `--remote-dns-listen 127.0.0.1:53`, in an elevated administrator session) adds the settings at startup and removes them on exit: an NRPT rule commented `host-shell-remote-dns pid=<pid>` and, only when there is no IPv4 default route, a loopback default route (metric 9999, active store only, so a reboot removes it). This is a paired add and remove, not a volatile setting: the rule is persistent, and if host-shell is killed it remains until the next start with the option removes leftovers of sessions that no longer run. `sys_get_info` lists the changes as `remote_dns_configured`. While the added route exists, direct outbound connections time out (about 21 seconds) instead of failing at once.

Without that option, an administrator can apply the rule by hand (with the same approval):

```powershell
Add-DnsClientNrptRule -Namespace '.' -NameServers 127.0.0.1 -Comment host-shell-remote-dns
Clear-DnsClientCache
# after the session
Get-DnsClientNrptRule | Where-Object Comment -eq 'host-shell-remote-dns' | ForEach-Object { Remove-DnsClientNrptRule -Name $_.Name -Force }
```

`getaddrinfo` returns IPv4 answers only when an IPv4 default route exists; on a network without a default gateway it reports "no data of the requested type". A target with a default gateway needs only the NRPT rule. Otherwise any next hop works, including the loopback interface (`New-NetRoute -DestinationPrefix 0.0.0.0/0 -InterfaceIndex (Get-NetIPAddress -IPAddress 127.0.0.1).InterfaceIndex -NextHop 0.0.0.0 -RouteMetric 9999 -PolicyStore ActiveStore`). `Resolve-DnsName` works without one. While the rule remains and no session serves the address, lookups fail immediately.
