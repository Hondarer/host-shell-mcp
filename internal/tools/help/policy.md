# Restrictions (policy)

The administrator who registered host-shell can restrict it with options. Without options nothing is
restricted and every tool can do anything the host user can. `sys_info` lists `tool_groups` and
`restrictions`; the table at the top of `help` shows them too.

| Option | Effect on you |
|---|---|
| `--tools` | Only some tool groups or tools exist (`files`, `search`, `exec`, `net`, `admin`). Others are not listed. |
| `--read-only` | Only tools that do not change the host. `net_http` only GET/HEAD/OPTIONS, `net_tcp` cannot send. |
| `--allow-path`, `--allow-read-path` | File tools work only under these directories (read-only for the second). Symbolic links are resolved before checking. |
| `--allow-command` | Only these programs can be run. Shell command lines need the shell itself allowed; use `argv` to run an allowed program directly. |
| `--deny-command` | Command lines matching a pattern are refused. |
| `--allow-host` | Network tools reach only these hosts, networks, ports or Unix sockets. |
| `--unset-env` | Some environment variables (secrets) are not passed to commands. |
| `--confirm` | The user is asked to approve each call of some tools. If your client cannot ask, those calls fail. |
| `--redact` | Secrets in results (keys, tokens, passwords) are replaced with `[REDACTED]`. Do not write such text back into files. |

A refused call returns an error starting with `policy:` that names the rule. Do not try to work around
it (for example by running a shell command to read a path the file tools refuse); ask the user instead.
