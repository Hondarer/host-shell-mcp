# Restrictions (policy)

The administrator can restrict host-shell via command-line flags. When no policy flags are set, all tools operate with the full permissions of the host user. Active restrictions appear in `sys_get_info` (`tool_groups`, `restrictions`) and at the top of `help_get`.

| Flag | Effect |
|---|---|
| `--tools` | Exposes only the specified tool groups (`files`, `search`, `exec`, `net`, `admin`) or individual tools; all others are omitted. |
| `--read-only` | Exposes only non-mutating tools. Restricts `net_request_http` to `GET`, `HEAD`, and `OPTIONS`, and disables sending in `net_exchange`. |
| `--allow-path`, `--allow-read-path` | Restricts file tools to the listed directories (`--allow-read-path` permits read-only access). Symbolic links are resolved before checking. |
| `--allow-command` | Permits only the listed executables. Shell commands require the shell itself to be allowed; pass `argv` to invoke an allowed executable directly. |
| `--deny-command` | Rejects command lines matching the specified pattern. |
| `--allow-host` | Restricts network tools to the listed hosts, CIDR ranges, ports, or Unix sockets. |
| `--unset-env` | Removes specified environment variables (such as secrets) before starting commands. |
| `--confirm` | Prompts the user via MCP elicitation to approve each call to matching tools; fails if the client does not support elicitation. |
| `--redact` | Replaces detected secrets (keys, tokens, passwords) in tool results with `[REDACTED]`. Do not write `[REDACTED]` placeholders back into files. |

Rejected calls return an error prefixed with `policy:` naming the violated rule. Do not attempt to bypass policy restrictions (for example, by invoking a shell command to read a path blocked by file policy); ask the user instead.
