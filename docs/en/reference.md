# host-shell Reference

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/reference.md](../ja/reference.md)) is the single source of truth (SSOT) and takes precedence if they differ.

This reference specifies the startup options, MCP server behavior, tool inputs and outputs, process management, and audit log format of `host-shell`.  
For setup and usage examples, see the [User Guide](user-guide.md); for security and operational design, see the [Operations Guide](operations.md).

## Contents

- [Starting](#starting)
- [Restrictions (policy)](#restrictions-policy)
- [Behavior as an MCP server](#behavior-as-an-mcp-server)
- [Common specification of all tools](#common-specification-of-all-tools)
- [Encodings and line endings](#encodings-and-line-endings)
- [Tool list](#tool-list)
- [Usage guidance](#usage-guidance)
- [Running commands](#running-commands)
- [Long-running execution (jobs)](#long-running-execution-jobs)
- [File operations](#file-operations)
- [Search](#search)
- [Checking differences](#checking-differences)
- [Privileged execution](#privileged-execution)
- [Environment information](#environment-information)
- [Network](#network)
- [Administration](#administration)
- [Windows registry](#windows-registry)
- [Transfer with the local machine (bridge)](#transfer-with-the-local-machine-bridge)
- [Starting and ending processes](#starting-and-ending-processes)
- [Audit log](#audit-log)
- [Limitations](#limitations)

## Starting

### Syntax

```text
host-shell --mcp [options]                      (target)
host-shell bridge [bridge-options] -- COMMAND...  (local)
host-shell --version
```

`--mcp` starts the process as an MCP server listening on standard input and output.  
`bridge` is a local relay subcommand that connects to `host-shell` on the target (see [Transfer with the local machine (bridge)](#transfer-with-the-local-machine-bridge)).  
If none of `--mcp`, `bridge`, or `--version` is specified, `host-shell` prints usage information to standard error and exits with code 2.

### Options

| Option | Value | Default | Description |
|---|---|---|---|
| `--mcp` | None | | Run as an MCP server over standard input and output |
| `--version` | None | | Print the version to standard output and exit |
| `--log-file` | Path | See [Audit log](#audit-log) | Base path for the audit log (written to a daily file with the date inserted before the extension) |
| `--no-log` | None | | Disable audit logging (takes precedence over `--log-file`) |
| `--max-output` | Bytes (1024 or more) | `65536` | Maximum output size per call |
| `--timeout` | Duration (e.g., `90s`, `5m`) | `1m` | Default command execution timeout |
| `--max-timeout` | Duration | `30m` | Maximum timeout that tool arguments may request (must be at least `--timeout`) |
| `--windows-shell` | `cmd`, `powershell`, `pwsh` | `powershell` | Default shell on Windows when `shell` is omitted |
| `--tools` | Group and tool names (comma-separated) | `all` | Tools to publish (see [Restrictions (policy)](#restrictions-policy)) |
| `--read-only` | None | | Hide tools that modify the target |
| `--confirm` | Group and tool names, `destructive` | None | Tools that require user confirmation before execution |
| `--allow-path` | Directory (repeatable) | None | Directories that file tools may read and write |
| `--allow-read-path` | Directory (repeatable) | None | Directories that file tools may read (read-only) |
| `--allow-command` | Program name globs (comma-separated, repeatable) | None | Programs allowed to execute |
| `--deny-command` | Regular expression (repeatable) | None | Command line patterns to reject |
| `--allow-host` | Destinations (comma-separated, repeatable) | None | Network destinations that network tools may connect to |
| `--unset-env` | Environment variable name globs (comma-separated, repeatable) | None | Environment variables excluded from child processes |
| `--redact` | None | | Mask secrets matching known patterns in tool output |
| `--redact-pattern` | Regular expression (repeatable) | None | Additional regular expression patterns to mask |
| `--client-proxy` | None | | Enables the [HTTP proxy through the client](client-proxy.md) when paired with `--proxy` on `bridge` |
| `--allow-forward-local` | None | | Permits local forwards and SOCKS5 connections originating from the target via `bridge` ([Port forwarding](port-forward.md)) |
| `--allow-forward-remote` | None | | Permits remote forwards listening on the target's loopback via `bridge` ([Port forwarding](port-forward.md)) |
| `--allow-forward-remote-port` | `PORT` or `PORT-PORT` (comma-separated, repeatable) | None (unrestricted) | Restricts target listening ports for remote forwards; requires `--allow-forward-remote` ([Port forwarding](port-forward.md)) |

Table: Startup options

Options may also be specified with a single hyphen, such as `-mcp`.  
Passing an unknown flag or an invalid value type prints usage information to standard error and exits with code 2; passing an out-of-range value or invalid regular expression prints the error reason to standard error and exits with code 1.

### Input, output and exit

| Item | Behavior |
|---|---|
| Standard input | Reads newline-delimited MCP JSON-RPC messages |
| Standard output | Writes only MCP JSON-RPC messages (never writes any other output) |
| Standard error | Writes diagnostic messages such as startup errors |
| Working directory | Current working directory at startup (typically the user's home directory over SSH) |
| Normal exit | When standard input closes (SSH disconnect) or upon receiving `SIGINT` or `SIGTERM` (exit code 0) |
| Cleanup on exit | Terminates all running background jobs |

Table: Input, output and exit

Each SSH session runs an independent `host-shell` process. Concurrent connections from multiple agents do not share background jobs or other in-memory state.

## Restrictions (policy)

When no restriction options are specified, `host-shell` publishes all tools and runs with the full permissions of the connecting user.  
Only explicitly configured policies are enforced. Active restrictions are reported to the agent via `restrictions` in [`sys_get_info`](#sys_get_info), the summary table at the top of `help_get`, and `instructions` in the `initialize` response. Tool calls that violate a policy are rejected before execution with an error prefixed by `policy:`.

### Choosing tools (--tools)

Tools are organized into the following groups. `help_get` and `sys_get_info` (the `core` group) are always published.

| Group | Tools |
|---|---|
| `files` | `fs_list`, `fs_stat`, `fs_read`, `fs_edit`, `fs_write`, `fs_mkdir`, `fs_move`, `fs_copy`, `fs_remove`, `fs_manifest` |
| `search` | `search_grep`, `search_find` |
| `exec` | `exec_run`, `exec_sudo`, `job_start`, `job_read`, `job_write`, `job_stop`, `job_list` |
| `net` | `net_request_http`, `net_exchange`, `net_check`, `net_list_listeners`, `net_list_interfaces` |
| `admin` | `proc_list`, `proc_kill`, `svc_list`, `svc_control`, `log_query`, `disk_list`, `reg_read`, `reg_write`, `reg_delete` |

Table: Tool groups

`--tools` accepts comma-separated group names, tool names, `all`, `destructive` (mutating tools), and `readonly` (read-only tools), evaluated from left to right. A leading `-` excludes matching tools, while `+` (or no prefix) includes them. OS-specific tool names (`exec_sudo`, `reg_*`) are accepted without error on all platforms and are published only on supported operating systems.

| Value | Published tools |
|---|---|
| `all` (default) | All tools |
| `files,search` | File and search tools only |
| `all,-admin,-exec_sudo` | All tools except administration tools and `exec_sudo` |
| `files,exec,-fs_remove` | File and command execution tools except `fs_remove` |

Table: Examples of --tools

### Read-only (--read-only)

Hides tools that do not carry the `readOnlyHint` annotation, except for `net_request_http` and `net_exchange`, which remain available under the following restrictions:

- `net_request_http`: Only `GET`, `HEAD`, and `OPTIONS` methods are permitted, and `save_to` is disabled
- `net_exchange`: `send` is disabled (only receiving server-initiated data is permitted)

`--read-only` and `--tools` control published MCP tools only; they do not restrict traffic over the [HTTP proxy through the client](client-proxy.md) or [port forwarding](port-forward.md). Port forwarding is enabled only when `--allow-forward-local` or `--allow-forward-remote` is set. Even on a `--read-only` target, enabling `--allow-forward-local` allows a local forward to write to target services.

### User confirmation (--confirm)

Prompts the user for approval via MCP elicitation before running matching tools, and executes the call only if approved (accepts the same selector syntax as `--tools`).

- For clients using protocol version `2026-07-28`, `host-shell` returns an `inputRequests` confirmation prompt in the tool result per SEP-2322 (multi round-trip requests). When the client resends the call with the user's response, `host-shell` verifies the HMAC-signed `requestState` (bound to a per-startup secret, the tool name, and the exact arguments) before executing the tool.
- For clients on other supported protocol versions, the SDK sends an `elicitation/create` request to the client and waits for the response before continuing.
- If the client does not support elicitation, matching tool calls fail with an error without executing.
- Confirmation prompts are logged in the audit log as `<tool name> (confirmation requested)`.

### File locations (--allow-path, --allow-read-path)

Validates file path arguments passed to tools.

| Category | Checked for read access | Checked for write access |
|---|---|---|
| File operations | `path` in `fs_list`, `fs_stat`, `fs_read`, `fs_manifest`; `source` in `fs_copy` | `path` in `fs_edit`, `fs_write`, `fs_mkdir`, `fs_remove`; `source` and `destination` in `fs_move`; `destination` in `fs_copy` |
| Search | `path` in `search_grep`, `search_find` | |
| Running commands | `cwd` in `exec_run`, `job_start`, `exec_sudo` | |
| Network | `body_file`, `ca_cert`, `client_cert`, `client_key` in `net_request_http` | `save_to` in `net_request_http` |

Table: What the location checks cover

- Read operations are permitted under both `--allow-path` and `--allow-read-path`, whereas write operations are permitted only under `--allow-path` (specifying only `--allow-read-path` blocks all writes).
- Paths are converted to absolute paths and existing symbolic links are resolved before validation. Symbolic links pointing outside allowed directories are rejected.
- Path comparisons on Windows are case-insensitive.

### Commands (--allow-command, --deny-command)

Validates programs launched by `exec_run`, `job_start`, and `exec_sudo`.

- When `--allow-command` is set, the base name of the executable (on Windows, lowercased and stripped of extensions such as `.exe`) must match at least one configured glob.
- Using `command` (shell command lines) requires the shell itself (`sh`, `bash`, `cmd`, `powershell`, `pwsh`) to be allowed. Because allowing a shell permits running arbitrary commands through it, omit shells from `--allow-command` and have the agent pass `argv` when restricting runnable binaries.
- `exec_sudo` requires both `sudo` and `sh` to be allowed.
- `--deny-command` rejects calls whose command string (or space-joined `argv`) matches a regular expression. Because command strings can be rephrased, use `--deny-command` as a safeguard against accidental commands rather than a security boundary.

### Network destinations (--allow-host)

Validates target destinations in `net_request_http` (including redirect targets), `net_exchange`, and `net_check`.

| Format | Meaning |
|---|---|
| `localhost`, `*.internal` | Host name glob (all ports allowed) |
| `api.internal:443`, `*:80` | Host name and port globs |
| `10.0.0.0/8`, `10.0.0.0/8:5432` | CIDR range (for host names, every resolved IP address must fall within the range) |
| `unix:/var/run/*.sock` | Unix domain socket path glob |

Table: How to write --allow-host

### Environment variables (--unset-env)

Removes environment variables matching the specified globs from child processes spawned by `exec_run`, `job_start`, `exec_sudo`, and administration tools (for example, `--unset-env 'AWS_*,*TOKEN*'`). Variables explicitly passed via the tool's `env` argument are preserved.

### Masking (--redact, --redact-pattern)

Scans all strings in tool results (`structuredContent` and `content`) and replaces detected secrets with `[REDACTED]`.

| Kind | Example patterns |
|---|---|
| PEM private keys | Body between `-----BEGIN ... PRIVATE KEY-----` and `-----END ... PRIVATE KEY-----` |
| Authorization headers | `Authorization: Bearer ...` |
| Key-value pairs | Values in `password=...`, `api_key: ...`, `DB_PASSWORD=...`, `"token": "..."` |
| URL credentials | Password portion of `https://user:password@host` |
| Known tokens | AWS access key IDs, GitHub and Slack tokens, `sk-...`, JWTs |

Table: What is masked

`--redact-pattern` registers custom regular expressions (effective even without `--redact`).  
Because writing redacted file output back via `fs_write` would overwrite secrets with literal `[REDACTED]`, the `policy` topic in `help_get` warns agents against doing so.

### Limits of the restrictions

Policy flags validate tool arguments only; they do not enforce OS-level sandboxing.

- When the `exec` group is enabled, spawned commands can access files and the network directly. To enforce strict boundaries, exclude `exec` via `--tools` or restrict runnable executables via `--allow-command`.
- Where hard privilege isolation is required, run `host-shell` under a dedicated low-privilege OS user (see the [Operations Guide](operations.md)).

## Behavior as an MCP server

| Item | Description |
|---|---|
| Transport | stdio (newline-delimited JSON-RPC 2.0) |
| Protocol versions | `2024-11-05`, `2025-03-26`, `2025-06-18`, `2025-11-25`, `2026-07-28` (negotiated per client request) |
| Server name | `host-shell` |
| Capabilities | `tools`, `resources`, `logging` |
| Progress notifications | When a request includes `progressToken`, `exec_run` and `exec_sudo` emit `notifications/progress` every second, and `net_request_http` (`save_to`) emits progress as bytes arrive |
| Cancellation | Receiving `notifications/cancelled` aborts the active request and kills any spawned command together with its process tree |
| User confirmation | Uses MCP elicitation for tools matching `--confirm` ([User confirmation](#user-confirmation---confirm)) |
| Incoming message limit | 16 MiB per message |
| SDK | [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0 |

Table: MCP server specification

### Guidance for the agent

To allow agents to operate effectively using only MCP protocol metadata without external documentation, `host-shell` provides guidance at four levels:

| Layer | Content | When provided |
|---|---|---|
| `instructions` in `initialize` result | Target host name, OS and architecture, working directory, default shell and encodings, output limit and timeouts, tool selection rules, process isolation per call, and active policy restrictions | At session initialization |
| Tool `title` and `description` | Purpose of each tool, when to prefer it, and related tools | On `tools/list` |
| Input and output schema `description` | Meaning and default of each field, and how to paginate partial reads | On `tools/list` |
| [`help_get`](#help_get) tool and resources | Topic-specific workflows, argument examples, Windows caveats, and server limits | On demand |

Table: Guidance for the agent

Guidance and error messages are written in English for agents and include actionable recovery hints (for example, reporting matching line numbers and suggesting `replace_all` when `fs_edit` matches multiple occurrences). Each `help_get` topic is also exposed as an MCP resource (`host-shell://help/<topic>`, `text/markdown`).

## Common specification of all tools

### Result format

Tool results return a structured JSON object in `structuredContent` and the serialized JSON string in `content` (`TextContent`). Each tool's output schema is advertised via `outputSchema` in `tools/list`.  
As an exception, `help_get` returns plain Markdown text in `content` and omits `outputSchema`.

### Errors

A tool result sets `isError: true` with an explanatory message in `content` when:

- Arguments fail schema validation (missing required fields, type mismatches, or unknown fields)
- The underlying operation fails (for example, file not found or permission denied)
- A command process cannot be started (executable not found, unsupported shell, etc.)

If a command starts and exits with a non-zero status, `host-shell` returns a normal tool result (`isError: false`); inspect `exit_code` to determine whether the command succeeded.

### Path interpretation

All paths are evaluated on the target machine, and relative paths resolve against `host-shell`'s working directory. On Windows, both `/` and `\` are accepted. Returned paths are absolute unless noted otherwise.

### Output limits

Every tool caps its output size or entry count. The default byte limit is 64 KiB (configurable via `--max-output`).

| Tool | Limit scope | Behavior when exceeded |
|---|---|---|
| `exec_run`, `exec_sudo` | Standard output and standard error streams individually | Retains the first half and last half with an omission marker in between, and sets `stdout_truncated` / `stderr_truncated` to `true` |
| `job_read` | Bytes per call | Continue reading from `next_offset` |
| `fs_read` | Bytes and lines per call | Sets `truncated: true` and returns the continuation offset (`next_offset` / `next_line`) |
| `proc_list` | Total serialized output | Retains the head and tail separated by an omission marker |
| `net_request_http` | Response body | Returns the leading bytes and sets `body_truncated: true` (unlimited when `save_to` is used) |
| `net_exchange` | Received bytes | Stops reading at the limit and sets `truncated: true` |
| `log_query` | Total serialized output | Drops the oldest log entries and sets `truncated: true` |
| `fs_list`, `search_*`, `fs_manifest` | Maximum entry count specified in arguments | Sets `truncated: true` |

Table: Output limits

The omission marker has the following format:

```text

... [host-shell: 123456 bytes omitted] ...

```

### Encodings

All tool text inputs and outputs use UTF-8. When command output or files on the target use another encoding, `host-shell` converts to and from UTF-8 automatically (see [Encodings and line endings](#encodings-and-line-endings)).

### Tool annotations

Each tool includes MCP annotations to guide approval policies on the agent side:

| Annotation | Tools |
|---|---|
| `readOnlyHint: true` | `help_get`, `job_read`, `job_list`, `fs_list`, `fs_stat`, `fs_read`, `search_grep`, `search_find`, `fs_manifest`, `sys_get_info`, `proc_list`, `net_check`, `net_list_listeners`, `net_list_interfaces`, `svc_list`, `log_query`, `disk_list`, `reg_read` |
| `destructiveHint: false`, `idempotentHint: true` | `fs_mkdir` |
| `destructiveHint: true` | `exec_run`, `job_start`, `job_write`, `job_stop`, `fs_edit`, `fs_write`, `fs_move`, `fs_copy`, `fs_remove`, `exec_sudo`, `net_request_http`, `net_exchange`, `proc_kill`, `svc_control`, `reg_write`, `reg_delete` |

Table: Tool annotations

Only `net_request_http`, `net_exchange`, and `net_check` (which can communicate with external networks) set `openWorldHint: true`; all other tools set `openWorldHint: false`.

## Encodings and line endings

MCP messages always use UTF-8. `host-shell` handles target encodings (Shift_JIS, EUC-JP, UTF-16, etc.) and line endings on the target machine.

### Default encodings of the target

Auto-detection (`auto`) and `system` use two target default encodings depending on context:

| Kind | Used for | Windows | Linux |
|---|---|---|---|
| Console | Command output and standard input | OEM code page (`GetOEMCP`) | Locale encoding |
| File | Reading, writing, and searching text files | ANSI code page (`GetACP`) | Locale encoding |

Table: Default encodings of the target

On Japanese Windows, both default to Shift_JIS (CP932), or UTF-8 when "Beta: Use Unicode UTF-8 for worldwide language support" is enabled.  
On Linux, the encoding suffix after `.` is parsed from the first non-empty variable among `LC_ALL`, `LC_CTYPE`, and `LANG` (for example, `ja_JP.eucJP` selects EUC-JP; `C`, `POSIX`, or unset variables default to UTF-8).  
Check `console_encoding` and `file_encoding` in [`sys_get_info`](#sys_get_info) for the active values.

### Specifying encodings

Encoding arguments (`output_encoding`, `stdin_encoding`, `charset`) accept:

| Value | Meaning |
|---|---|
| `auto` | Auto-detect (valid for read arguments only; default when omitted) |
| `utf-8` | UTF-8 (no conversion) |
| `system` | Target default encoding |
| Encoding name | `shift_jis`, `euc-jp`, `iso-2022-jp`, `utf-16le`, `utf-16be`, `gbk`, `gb18030`, `big5`, `euc-kr`, `windows-1252`, etc. |
| Code page number | `cp932`, `932`, `cp437`, `cp65001`, etc. |

Table: Specifying encodings

Encoding names are case-insensitive. All names and aliases from the [WHATWG Encoding Standard](https://encoding.spec.whatwg.org/#names-and-labels) (`sjis`, `windows-31j`, `latin1`, etc.) as well as common aliases such as `eucjp` and `ujis` are supported (`shift_jis` maps to Windows-31J / CP932).

### Detection

When reading with `auto`, `host-shell` detects the encoding in the following order:

1. If a BOM (`UTF-8`, `UTF-16LE`, or `UTF-16BE`) is present at the start, use that encoding and strip the BOM from the returned text.
2. Without a BOM, if the leading bytes match the byte distribution of UTF-16, decode as `UTF-16LE` or `UTF-16BE`.
3. If the byte sequence is valid UTF-8, treat it as UTF-8.
4. Otherwise, decode using the target's default encoding.

For command output (`exec_run`, `job_start`, `exec_sudo`), `search_grep`, and line-range reads in `fs_read`, steps 3 and 4 are evaluated per line so output mixing UTF-8 and Shift_JIS decodes cleanly (byte-range reads in `fs_read` evaluate the read chunk as a whole).

BOM-less UTF-16 detection inspects up to the first 4096 bytes (for command output, once the first 64 bytes arrive or the command exits) in 2-byte units and classifies the stream as UTF-16 when all of the following hold:

- Null code units (`0x0000`) make up at most 1% of units
- Code units with `0x00` in one byte position make up at least 5% of units (the byte position containing `0x00` determines LE vs. BE)
- Code units with `0x00` in the opposite byte position make up at most 2% of units (tolerating up to 1 unit in short strings)
- All UTF-16 surrogate pairs are well-formed

UTF-16 text containing ASCII characters or line breaks satisfies these rules, while binary and compressed data are excluded. Very short BOM-less UTF-16 strings consisting solely of CJK characters without ASCII or newlines may not be auto-detected; specify `charset` explicitly in that case.

When conversion from a non-UTF-8 encoding occurs, the detected source encoding is reported in `stdout_encoding`, `stderr_encoding`, or `charset`. Invalid byte sequences that cannot be decoded are replaced with `U+FFFD`.

### Not splitting characters

Chunked reading tools never split multibyte characters across chunk boundaries. Any incomplete trailing byte sequence is deferred to `next_offset`, and truncated command output strips partial boundary characters before inserting the omission marker.

### Line endings

Reading preserves original line endings (`CRLF` and `LF`) unchanged, and `fs_read` reports the detected line ending style in `newline` (`lf`, `crlf`, `cr`, or `mixed`).  
Setting `newline` to `lf` or `crlf` in `fs_write` normalizes line endings on write. Passing the `charset`, `bom`, and `newline` returned by `fs_read` into `fs_write` preserves the file's original format.

### What is converted

| Tool | Argument | Default |
|---|---|---|
| `exec_run`, `job_start`, `exec_sudo` | `output_encoding` (stdout and stderr) | `auto` (console) |
| | `stdin_encoding` (stdin) | `utf-8` (for PowerShell on Windows, see [Shells](#shells)) |
| `job_write` | (inherits `stdin_encoding` from `job_start`) | Same as `job_start` |
| `proc_list` | None | `auto` (console) |
| `fs_read` (`text`) | `charset` | `auto` (file) |
| `fs_edit` | `charset` | `auto` (file; preserves original encoding, BOM, and line endings on write) |
| `fs_write` (`text`) | `charset`, `newline`, `bom` | `utf-8`, `keep` (unchanged), `false` (no BOM) |
| `search_grep` | `charset` | `auto` (file) |

Table: What encoding conversion covers

Reading and writing in `base64` mode transfer raw bytes without encoding or line-ending conversion.

## Tool list

| Category | Tool | Description |
|---|---|---|
| Usage guidance | [`help_get`](#help_get) | Returns agent usage guidance |
| Running commands | [`exec_run`](#exec_run) | Runs a command to completion and returns its output |
| Long-running execution | [`job_start`](#job_start) | Starts a command in the background |
| | [`job_read`](#job_read) | Reads output and status from a job |
| | [`job_write`](#job_write) | Writes to a job's standard input |
| | [`job_stop`](#job_stop) | Sends a signal to or terminates a job |
| | [`job_list`](#job_list) | Lists active and recent jobs |
| File operations | [`fs_list`](#fs_list) | Lists directory entries |
| | [`fs_stat`](#fs_stat) | Returns file or directory attributes |
| | [`fs_read`](#fs_read) | Reads file contents |
| | [`fs_edit`](#fs_edit) | Replaces exact strings in a file |
| | [`fs_write`](#fs_write) | Writes or appends to a file |
| | [`fs_mkdir`](#fs_mkdir) | Creates directories |
| | [`fs_move`](#fs_move) | Moves or renames a file or directory |
| | [`fs_copy`](#fs_copy) | Copies a file or directory |
| | [`fs_remove`](#fs_remove) | Removes a file or directory |
| Search | [`search_grep`](#search_grep) | Searches file contents by regex or literal string |
| | [`search_find`](#search_find) | Finds files and directories by glob |
| Checking differences | [`fs_manifest`](#fs_manifest) | Lists file sizes, modes, and SHA-256 hashes |
| Privileged execution | [`exec_sudo`](#exec_sudo) | Runs a command via `sudo` (Linux only) |
| Environment information | [`sys_get_info`](#sys_get_info) | Returns target OS info, enabled tool groups, and active restrictions |
| Network | [`net_request_http`](#net_request_http) | Sends HTTP / HTTPS requests from the target |
| | [`net_exchange`](#net_exchange) | Sends and receives data over TCP, TLS, or Unix sockets |
| | [`net_check`](#net_check) | Checks DNS resolution, TCP connectivity, and TLS certificates in parallel |
| | [`net_list_listeners`](#net_list_listeners) | Lists listening ports and owning processes |
| | [`net_list_interfaces`](#net_list_interfaces) | Lists network interfaces |
| Administration | [`proc_list`](#proc_list) | Lists running processes |
| | [`proc_kill`](#proc_kill) | Signals or terminates a process |
| | [`svc_list`](#svc_list) | Lists system or user services |
| | [`svc_control`](#svc_control) | Queries, starts, stops, or restarts a service |
| | [`log_query`](#log_query) | Queries system logs (journald / Windows Event Log) |
| | [`disk_list`](#disk_list) | Lists file system or drive usage |
| Windows registry | [`reg_read`](#reg_read) | Reads Windows registry keys and values (Windows only) |
| | [`reg_write`](#reg_write) | Writes Windows registry keys and values (Windows only) |
| | [`reg_delete`](#reg_delete) | Deletes Windows registry keys and values (Windows only) |
| Transfer with the local machine | [`transfer_push`](#transfer_push) | Uploads local files or directories to the target (`bridge` only) |
| | [`transfer_pull`](#transfer_pull) | Downloads target files or directories to the local machine (`bridge` only) |
| Client HTTP proxy | [`proxy_status`](client-proxy.md#proxy_status-bridge) | Returns client HTTP proxy status and recent connection events (`bridge` with `--proxy` only) |
| Port forwarding | [`forward_status`](port-forward.md#tools-bridge) | Lists port forwards and recent connection events (`bridge` with forwarding enabled only) |
| | [`forward_open`](port-forward.md#tools-bridge) | Opens a new port forward (`bridge` with allow rules only) |
| | [`forward_close`](port-forward.md#tools-bridge) | Closes a forward opened by `forward_open` (`bridge` with allow rules only) |

Table: Tool list

Only tools enabled by [`--tools`](#choosing-tools---tools) and [`--read-only`](#read-only---read-only) are advertised. In the tables below, arguments with an empty "Required" column are optional.

## Usage guidance

### help_get

Returns Markdown usage guidance for agents.

| Argument | Type | Required | Description |
|---|---|---|---|
| `topic` | string | | Topic name (`overview` when omitted; `all` concatenates all topics) |

Table: Arguments of help

| topic | Content |
|---|---|
| `overview` | Architecture overview, tool quick reference, and recommended workflow (prefixed with the target host summary) |
| `files` | `fs_read`, `fs_write`, directory/file management tools, and `fs_manifest` |
| `edit` | `fs_edit` usage and replacement rules |
| `search` | Glob syntax, `search_grep` output modes, and `search_find` |
| `commands` | `exec_run`, `exec_sudo`, and choosing the right execution tool |
| `jobs` | Background jobs, wait conditions (`until`, `idle_ms`), standard input, pseudo-terminals, and signals |
| `network` | `net_*` tools, client HTTP proxy, and port forwarding |
| `admin` | Process, service, system log, disk, and Windows registry tools |
| `transfer` | `transfer_push`, `transfer_pull`, and chunked transfers without `bridge` |
| `encoding` | Character encoding and line-ending conversion |
| `windows` | Notes for Windows targets |
| `policy` | Policy restriction flags and how agents should handle them |
| `limits` | Output limits, timeouts, error handling, and audit logging (prefixed with the target host summary) |

Table: Topics of help

The target host summary includes the host name, OS and architecture, user, working directory, default shell, default encodings, output limit, command timeouts, enabled tool groups, and active policy restrictions. Requesting an unknown `topic` returns an error listing the valid topic names.  
Each topic is also available as an MCP resource at `host-shell://help/<topic>`.

## Running commands

### exec_run

Runs a command, waits for it to finish, and returns the result. Specify either `command` (a shell command line) or `argv` (started directly without a shell).

| Argument | Type | Required | Description |
|---|---|---|---|
| `command` | string | * | Command line passed to the shell |
| `argv` | string[] | * | Program and arguments started without a shell (the first element is the program) |
| `shell` | string | | Shell that interprets `command` (the OS default shell when omitted; see [Shells](#shells)) |
| `cwd` | string | | Working directory (the working directory of `host-shell` when omitted) |
| `env` | object | | Environment variables to add or override (a map of keys and values) |
| `stdin` | string | | Text passed to standard input (closed after writing; empty input when omitted) |
| `timeout_sec` | integer | | Timeout in seconds (`--timeout` when omitted; capped at `--max-timeout`) |
| `output_encoding` | string | | Encoding of standard output and standard error (`auto` when omitted; see [Specifying encodings](#specifying-encodings)) |
| `stdin_encoding` | string | | Encoding to convert `stdin` into (`utf-8` when omitted). For PowerShell on Windows, a value other than `utf-8` is passed on the assumption that PowerShell reads that code page ([Shells](#shells)). `auto` cannot be specified |

Table: Arguments of exec_run (* specify either `command` or `argv`)

Each call starts a new process, so a previous `cd` or environment change does not carry over (use `cwd` and `env`).

| Output | Type | Description |
|---|---|---|
| `exit_code` | integer | Exit code (`-1` when terminated by a signal) |
| `stdout` | string | Standard output |
| `stderr` | string | Standard error |
| `stdout_truncated` | boolean | `true` when standard output was truncated |
| `stderr_truncated` | boolean | `true` when standard error was truncated |
| `stdout_encoding` | string | Original encoding when standard output was converted from a non-UTF-8 encoding |
| `stderr_encoding` | string | Original encoding when standard error was converted from a non-UTF-8 encoding |
| `timed_out` | boolean | `true` when killed due to a timeout |
| `cancelled` | boolean | `true` when killed because the client cancelled the request |
| `duration_ms` | integer | Execution time (milliseconds) |

Table: Output of exec_run

On timeout, the command and its descendant processes are killed, and the output collected so far is returned with `timed_out: true`.  
When the request includes a `progressToken`, `notifications/progress` (elapsed seconds, output bytes, and the last line) is sent every second, and on `notifications/cancelled` the process tree is killed immediately.

```json
{"command": "systemctl status nginx --no-pager", "timeout_sec": 10}
```

```json
{"argv": ["git", "-C", "/srv/app", "log", "-5", "--oneline"]}
```

```json
{"command": "Get-Service | Where-Object Status -eq Running", "shell": "powershell"}
```

```json
{"command": "dir C:\\work", "shell": "cmd"}
```

## Long-running execution (jobs)

Run long-running commands, such as builds and log monitoring, as background jobs.

```mermaid
---
title: Job states
---
stateDiagram-v2
    direction LR
    [*] --> running : job_start
    note right of running
        job_stop (signal other than kill)
        returns to running
    end note
    running --> ended : command ends
    running --> ended : job_stop, timeout, host-shell exits
    ended --> [*] : more than 50 finished jobs,<br/>or host-shell exits
```

- Job IDs are numbered sequentially per `host-shell` process (`job-1`, `job-2`, and so on).
- Input and output use either pipes (the default) or a pseudo-terminal (`pty: true`).
- Standard output and standard error are merged into a single stream and stored after conversion to UTF-8 (`offset` and `output_bytes` are byte counts of the converted UTF-8).
- The output ring buffer retains at least the most recent four times `--max-output` (256 KiB by default) and discards older output beyond that.
- Up to the 50 most recent finished jobs are retained; older ones are discarded.
- When the `host-shell` process exits (for example, on SSH disconnection), all running jobs are stopped.

### job_start

Starts a command in the background and returns its job status immediately. It accepts the same arguments as [`exec_run`](#exec_run) plus the following (`timeout_sec` has no limit when omitted, and is not capped by `--max-timeout` when specified).

| Argument | Type | Required | Description |
|---|---|---|---|
| `stdin_open` | boolean | | Keep standard input open so input can be sent later with [`job_write`](#job_write) (`stdin`, if specified, is sent first) |
| `pty` | boolean | | Start with a pseudo-terminal |
| `cols` | integer | | Number of terminal columns for `pty` (`120` when omitted) |
| `rows` | integer | | Number of terminal rows for `pty` (`40` when omitted) |

Table: Arguments specific to job_start

#### Pseudo-terminal (pty)

With `pty: true`, the command is started attached to a pseudo-terminal. Use it for programs that require a terminal (`sudo`, `ssh`, REPLs, `top`, programs that buffer output when connected to a pipe, and so on).

| Item | Linux | Windows |
|---|---|---|
| Implementation | pty (creates a new session and sets the controlling terminal) | ConPTY (Windows 10 1809 / Windows Server 2019 or later) |
| Environment variables | Sets `TERM` to `xterm-256color` if unset | Unchanged |
| PowerShell | | Started without `-NonInteractive` (`Read-Host` and similar commands work) |

Table: Pseudo-terminal

- Standard output and standard error are merged as terminal output, and input remains open.
- If descendant processes keep the terminal open after the command exits, the terminal is closed after waiting up to 2 seconds.
- Output is also fed to a virtual terminal emulator (`github.com/charmbracelet/x/vt`), allowing `job_read` with `screen: true` to return the current screen. The virtual terminal also responds to terminal queries such as cursor position requests.

The output schema is [Job status](#job-status).

### job_read

Reads job output starting from the specified offset and returns it along with the current job status.

| Argument | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Job ID |
| `offset` | integer | | Read start offset (cumulative bytes from the start, the previous `next_offset` when continuing, or a negative value to count backward from the end) |
| `max_bytes` | integer | | Maximum bytes to read (defaults to and is capped at `--max-output`) |
| `wait_sec` | integer | | Maximum seconds to wait (up to 300 seconds; returns immediately when the job finishes) |
| `until` | string | | Wait until output after `offset` matches this regular expression |
| `idle_ms` | integer | | After output arrives, wait until no new output is produced for this many milliseconds |
| `raw` | boolean | | For `pty` jobs, return raw output without stripping terminal control sequences |
| `screen` | boolean | | For `pty` jobs, return the current terminal screen instead of stream output |

Table: Arguments of job_read

When only `wait_sec` is specified, `job_read` returns as soon as new output arrives after `offset`.  
For `pty` jobs, output is cleaned up as follows before returning (`until` is also matched against the cleaned text); specify `raw: true` to receive unprocessed output.

- ANSI escape sequences for colors, cursor movement, window titles, and similar controls are stripped.
- CRLF is normalized to LF, and when a bare CR overwrites earlier text on the same line (such as progress bars), only the final content of the line is kept.
- Backspaces erase the preceding character, and control characters other than tab and newline are stripped.
- If an escape sequence is split at the end of a chunk, output up to just before that sequence is returned and `next_offset` is set to the start of the sequence.

| Output | Type | Description |
|---|---|---|
| `status` | object | [Job status](#job-status) |
| `matched` | boolean | `true` when the `until` regular expression matched |
| `output` | string | Output read |
| `next_offset` | integer | Offset for the next read |
| `more` | boolean | `true` when unread output remains after `next_offset` |
| `dropped` | integer | Bytes before `offset` that were discarded from the ring buffer |
| `cursor_row`, `cursor_col` | integer | Cursor row and column when `screen: true` is set (0-based) |

Table: Output of job_read

#### Screen state (screen)

With `screen: true`, for full-screen programs such as `top`, `vi`, and `less`, `output` returns the current terminal screen (`rows` lines with trailing spaces stripped, joined by newlines), and `cursor_row` and `cursor_col` return the cursor position. `offset` and `max_bytes` are ignored, and the screen state and cumulative byte count (`next_offset`) are captured once the wait conditions (`wait_sec`, `until`, `idle_ms`) are met (returns an error for non-`pty` jobs).

```json
{"id": "job-3", "screen": true, "wait_sec": 5, "idle_ms": 500}
```

### job_write

Sends input to a job started with `stdin_open` or `pty`. It can also close standard input or resize a PTY terminal.

| Argument | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Job ID |
| `input` | string | | Text to send (no newline is added automatically; include a trailing `\n` to send a line) |
| `close` | boolean | | Close standard input after sending `input` (for `pty` jobs, sends Ctrl-D on Linux and Ctrl-Z + Enter on Windows) |
| `cols`, `rows` | integer | | Resize the terminal of a `pty` job (both are required) |

Table: Arguments of job_write (at least one of `input`, `close`, or `cols` and `rows` is required)

| Output | Type | Description |
|---|---|---|
| `status` | object | [Job status](#job-status) |
| `bytes_written` | integer | Bytes written (after encoding conversion) |

Table: Output of job_write

`input` is converted to the `stdin_encoding` specified at `job_start` before being sent. If the child process does not read from input and the write does not complete within 10 seconds, an error is returned while the remaining data continues to be written in the background.  
For `pty` jobs, send control keys as characters (Ctrl-C is `\u0003`, Ctrl-D is `\u0004`, Esc is `\u001b`, and Up arrow is `\u001b[A`).

```json
{"id": "job-3", "input": "y\n"}
```

```json
{"id": "job-4", "input": "\u0003"}
```

### job_stop

Sends a signal to a job or kills its entire process tree, waits for it to terminate, and returns its status.

| Argument | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Job ID |
| `signal` | string | | `kill` (default), or on Linux `interrupt`, `terminate`, `hangup`, `quit`, `stop`, `continue`, `usr1`, `usr2` (`interrupt` is also available on Windows for `pty` jobs) |
| `wait_sec` | integer | | Seconds to wait for termination (`5` when omitted) |

Table: Arguments of job_stop

| signal | Behavior |
|---|---|
| `kill` | Kills the job together with its descendant processes ([How processes are ended](#how-processes-are-ended)) |
| `interrupt` (`pty` jobs) | Sends Ctrl-C to the terminal (both Linux and Windows) |
| Others (Linux) | Sends the signal to the job's process group |

Table: Signals of job_stop

Signals other than `kill` may not terminate the process, so check `status.running` in the result. Sending `kill` to an already finished job is not an error, whereas other signals return an error.

### job_list

Returns the status of all jobs started in the current session, in the order they were started (no arguments).

| Output | Type | Description |
|---|---|---|
| `jobs` | object[] | Array of [Job status](#job-status) |

Table: Output of job_list

### Job status

| Field | Type | Description |
|---|---|---|
| `id` | string | Job ID |
| `command` | string | Command executed (for `argv`, the arguments joined by spaces) |
| `pid` | integer | Process ID |
| `pty` | boolean | `true` for a pseudo-terminal job |
| `running` | boolean | `true` while running |
| `stdin_open` | boolean | `true` when standard input is open and `job_write` can be used |
| `exit_code` | integer | Exit code (present only after the job has ended) |
| `signal` | string | Signal sent by the last `job_stop` (other than `kill`) |
| `output_encoding` | string | Original encoding when output was converted from a non-UTF-8 encoding |
| `error` | string | Error that occurred while waiting for the process (normally omitted) |
| `started_at` | string | Start time (RFC 3339) |
| `ended_at` | string | End time (RFC 3339; present only after the job has ended) |
| `output_bytes` | integer | Cumulative output bytes produced so far |

Table: Job status

## File operations

### fs_list

Returns the entries of a directory sorted by name. Symbolic links are reported as links without following their targets.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | | Directory path (the working directory when omitted) |
| `max_entries` | integer | | Maximum number of entries (`1000` when omitted) |

Table: Arguments of fs_list

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path of the directory |
| `entries` | object[] | Array of entries (`name`, `type` [`file`/`dir`/`symlink`/`other`], `size`, `mode`, `mod_time` [RFC 3339]) |
| `truncated` | boolean | `true` when truncated at `max_entries` |

Table: Output of fs_list

### fs_stat

Returns the attributes of a file or directory. For a symbolic link, it returns the attributes of the link itself along with its target (`link_target`).

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | Target path |

Table: Arguments of fs_stat

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `entry` | object | Attributes (same format as elements of [`fs_list`](#fs_list)) |
| `link_target` | string | Target of a symbolic link |

Table: Output of fs_stat

### fs_read

Reads a file. Depending on the arguments, it operates in one of three modes.

| Mode | Selection | Typical use |
|---|---|---|
| Line range (text) | Specify `line_start` (`encoding: "text"`) | Reading a specific line range of source code or logs |
| Byte range (text) | Omit `line_start` (`encoding: "text"`) | Reading sequentially from the start or a given byte offset |
| Byte range (base64) | Specify `encoding: "base64"` | Reading raw binary files |

Table: How fs_read reads

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | File path |
| `encoding` | string | | `text` (default) or `base64` |
| `charset` | string | | Encoding in `text` mode (`auto` when omitted; see [Specifying encodings](#specifying-encodings)) |
| `line_start` | integer | | First line to read (1-based; when specified, `offset` and `length` are ignored) |
| `line_count` | integer | | Number of lines to read (`2000` when omitted) |
| `line_numbers` | boolean | | Prefix each line with a 6-digit right-aligned line number and a tab (only with `line_start`) |
| `offset` | integer | | Starting byte offset (`0` when omitted) |
| `length` | integer | | Maximum bytes to read (up to the output limit when omitted) |

Table: Arguments of fs_read

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `size` | integer | File size (bytes) |
| `encoding` | string | `text` or `base64` |
| `charset` | string | Encoding used in `text` mode (can be passed to `fs_write` to preserve the format) |
| `bom` | boolean | `true` when the file starts with a BOM (the BOM itself is not included in `content`) |
| `newline` | string | Line ending style in `content` (`lf`, `crlf`, `cr`, `mixed`) |
| `content` | string | Content read |
| `eof` | boolean | `true` when the end of the file was reached |
| `next_offset` | integer | Byte offset for the next read in byte range mode |
| `next_line` | integer | Starting line for the next read in line range mode (only when `eof: false`) |
| `truncated` | boolean | `true` when the output limit was reached before completing the requested range |

Table: Output of fs_read

- In `text` mode, `fs_read` reads up to `--max-output` measured after conversion to UTF-8. In line range mode, it returns complete lines up to the limit; if a single line exceeds the limit, that line is truncated and `next_line` advances to the following line.
- In `base64` mode, it reads up to 3/4 of `--max-output` (48 KiB by default) so that the base64-encoded size fits within `--max-output`.
- The line number prefix added by `line_numbers: true` (`     12\t`) is not part of the file content; do not include it when passing text to `fs_edit` or `fs_write`.

```json
{"path": "/var/log/nginx/error.log", "line_start": 1, "line_count": 200}
```

```json
{"path": "C:/work/script.bat", "charset": "shift_jis", "line_start": 1}
```

```json
{"path": "C:/tools/app.exe", "encoding": "base64", "offset": 0}
```

### fs_write

Writes to a file. To modify part of an existing file, prefer [`fs_edit`](#fs_edit) over `fs_write`, which resends the entire file.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | File path |
| `content` | string | Yes | Content to write |
| `encoding` | string | | `text` (default) or `base64` |
| `charset` | string | | Encoding in `text` mode (`utf-8` when omitted; `auto` cannot be specified) |
| `newline` | string | | Line ending conversion in `text` mode: `keep` (default, no conversion), `lf`, `crlf` |
| `bom` | boolean | | Prepend a BOM (only for `utf-8`, `utf-16le`, and `utf-16be`, and only when writing from the start of the file) |
| `mode` | string | | `overwrite` (default), `append`, `create` |
| `offset` | integer | | Byte offset to start writing at (overwrites only the written byte range without truncating the file) |
| `make_dirs` | boolean | | Create missing parent directories with mode `0755` |
| `perm` | string | | Octal permissions for a newly created file (for example `0600`; `0644` by default; ignored on Windows) |
| `atomic` | boolean | | Write to a temporary file and replace by renaming (only with `mode: "overwrite"` and no `offset`) |

Table: Arguments of fs_write

| mode | Without `offset` | With `offset` |
|---|---|---|
| `overwrite` (default) | Creates the file if missing; otherwise truncates and rewrites the entire file | Creates the file if missing; overwrites only the written length starting at `offset` without truncating |
| `append` | Creates the file if missing; otherwise appends to the end | Not allowed (error) |
| `create` | Creates the file if missing; returns an error if it already exists | Creates the file if missing and writes from `offset` (error if it already exists) |

Table: mode and offset of fs_write

Unlike omitting `offset`, specifying `offset: 0` does not truncate the file. Writing at an `offset` past the end of the file fills the gap with zero bytes.  
In `text` mode, line ending conversion, encoding conversion, and BOM insertion are performed in that order. If the content contains characters that cannot be represented in `charset`, `fs_write` returns an error indicating the character and its position without modifying the file.

#### Atomic replacement (atomic)

By default, `overwrite` rewrites the existing file in place, which preserves ownership, permissions, and hard links, but leaves partial content visible while writing or if interrupted.  
With `atomic: true` (supported by `fs_write` and `fs_edit`), the file is replaced atomically:

1. If the target path is a symbolic link, the link target is resolved so the underlying file is replaced.
2. A temporary file (`.<name>.host-shell-tmp-<random>`) is created in the same directory, written, and flushed with `fsync`.
3. The permissions, owner, and group of the existing file are copied to the temporary file (if ownership cannot be preserved—for example, when the file is owned by another user and `host-shell` is not running as root—the temporary file is removed and an error is returned).
4. The temporary file is renamed over the target file (if the rename fails, the temporary file is removed).

ACLs, extended attributes, and hard links are not preserved. On Windows, replacing a file that another process holds open may fail.

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `bytes_written` | integer | Bytes written |
| `size` | integer | File size after writing (bytes) |

Table: Output of fs_write

```json
{"path": "/srv/data/image.bin", "content": "AAECAw==", "encoding": "base64", "offset": 1048576}
```

```json
{"path": "C:/work/run.bat", "content": "@echo off\necho 完了\n", "charset": "shift_jis", "newline": "crlf"}
```

### fs_edit

Replaces exact string matches in an existing file. It automatically detects the file's encoding, BOM, and line endings and writes the file back in the same format.  
Specify either a single replacement (`old_string` and `new_string`) or multiple sequential replacements (`edits`).

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | File path |
| `old_string` | string | * | String to replace (must match exactly, including whitespace, indentation, and newlines) |
| `new_string` | string | | Replacement string (an empty string deletes the match) |
| `replace_all` | boolean | | Replace all occurrences (when omitted, returns an error unless `old_string` matches exactly once) |
| `edits` | object[] | * | Array of replacements (`old_string`, `new_string`, `replace_all`), applied sequentially to the result of each preceding edit |
| `charset` | string | | File encoding (`auto` when omitted) |
| `dry_run` | boolean | | Validate the replacements and generate the diff snippet without writing to the file |
| `atomic` | boolean | | Replace atomically via a temporary file (same as [`fs_write`](#atomic-replacement-atomic)) |

Table: Arguments of fs_edit (* specify either `old_string` or `edits`)

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `replacements` | integer | Total number of replacements made |
| `lines` | integer[] | Starting line numbers of each replaced region after editing (up to 50) |
| `snippet` | string | Line-numbered excerpt showing the replaced regions with 2 lines of surrounding context (up to 80 lines) |
| `charset` | string | Encoding used for reading and writing |
| `bom` | boolean | Whether the file has a BOM (preserved from the original file) |
| `newline` | string | Line ending style after editing |
| `size` | integer | File size after editing (bytes) |
| `sha256` | string | SHA-256 hash of the file after editing |
| `dry_run` | boolean | `true` when `dry_run` was specified |

Table: Output of fs_edit

- If any replacement fails, the file is left untouched.
- If `old_string` is not found verbatim but matches a line when leading and trailing whitespace is ignored, the error message reports that line number. If `old_string` matches multiple locations and `replace_all` is not set, the error reports up to 10 matching line numbers.
- In CRLF files, LF line endings in `old_string` and `new_string` match and are written as CRLF.
- Encoding is detected for the file as a whole; if re-encoding the decoded text does not reproduce the original bytes (for example, in a file with mixed encodings), `fs_edit` returns an error to prevent corruption.
- Target files are limited to 32 MiB.

```json
{"path": "/etc/app/config.yml", "old_string": "  port: 8080\n", "new_string": "  port: 9090\n"}
```

```json
{"path": "src/main.c", "edits": [
  {"old_string": "#include <stdio.h>\n", "new_string": "#include <stdio.h>\n#include <string.h>\n"},
  {"old_string": "MAX_LEN 64", "new_string": "MAX_LEN 256", "replace_all": true}
]}
```

### fs_mkdir

Creates a directory (creating parent directories recursively with mode `0755`). If the directory already exists, it succeeds with `created: false`; if a file exists at that path, it returns an error.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | Directory path to create |

Table: Arguments of fs_mkdir

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `created` | boolean | `true` when newly created, `false` when it already existed |

Table: Output of fs_mkdir

### fs_move

Moves or renames a file or directory. Across file systems (or drives on Windows), it copies the source and then deletes the original (if the copy fails partway through, the original is left intact).

| Argument | Type | Required | Description |
|---|---|---|---|
| `source` | string | Yes | Source path |
| `destination` | string | Yes | Destination path (the full destination path, not the parent directory) |
| `overwrite` | boolean | | Overwrite an existing file at the destination (returns an error if the destination is a directory) |
| `make_dirs` | boolean | | Create missing parent directories of the destination |

Table: Arguments of fs_move

| Output | Type | Description |
|---|---|---|
| `source` | string | Absolute path of the source |
| `destination` | string | Absolute path of the destination |
| `copied` | boolean | `true` when moved by copying and deleting across file systems |

Table: Output of fs_move

### fs_copy

Copies a file, or copies a directory recursively when `recursive: true` is set.

| Argument | Type | Required | Description |
|---|---|---|---|
| `source` | string | Yes | Source path |
| `destination` | string | Yes | Destination path (the full destination path) |
| `recursive` | boolean | | Required to copy a directory |
| `overwrite` | boolean | | Overwrite existing files at the destination (directories are merged) |
| `make_dirs` | boolean | | Create missing parent directories of the destination |

Table: Arguments of fs_copy

| Output | Type | Description |
|---|---|---|
| `source` | string | Absolute path of the source |
| `destination` | string | Absolute path of the destination |
| `files` | integer | Number of files copied |
| `dirs` | integer | Number of directories created |
| `symlinks` | integer | Number of symbolic links recreated |
| `bytes` | integer | Total bytes copied |

Table: Output of fs_copy

File permissions and modification times are preserved, and symbolic links are recreated pointing to the same target (special files other than regular files, directories, and symbolic links are skipped). Specifying a destination inside the source directory returns an error.

### fs_remove

Deletes files, symbolic links, and directories. Deleting the root of a file system (`/`, `C:\`, and so on) or the home directory of the user running `host-shell` is refused.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | Path to delete (for a symbolic link, deletes the link itself) |
| `recursive` | boolean | | Required to delete a non-empty directory |
| `missing_ok` | boolean | | Do not return an error if the target does not exist |

Table: Arguments of fs_remove

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `removed` | boolean | `true` when deleted (`false` when missing with `missing_ok`) |
| `type` | string | Type of entry deleted (`file`, `dir`, `symlink`, `other`) |

Table: Output of fs_remove

## Search

`search_grep`, `search_find`, and `fs_manifest` walk the specified directory recursively in name order.

- Symbolic links are not followed.
- Directories matching names in `exclude_dirs` are not entered (`[".git", ".hg", ".svn"]` when omitted; pass an empty array `[]` to exclude nothing).
- Entries that cannot be read (for example, due to insufficient permissions) are skipped and counted in `skipped`.
- Result paths are returned relative to the `path` argument in the same form (if `path` is relative, result paths are relative as well).

### glob

The glob syntax used in `include` (`search_grep`, `fs_manifest`) and `pattern` (`search_find`) works as follows (on Windows, matching is case-insensitive and `\` is also treated as a path separator).

| Pattern | Meaning |
|---|---|
| Without `/` (`*.go`) | Matched against the file's base name (the last path element) |
| With `/` (`src/**/*.ts`) | Matched against the path relative to `path` (`/`-separated; a leading `./` is ignored) |
| `*`, `?` | Any sequence of characters, or any single character, within a single path element |
| `**` | Zero or more directory levels (`**/test/*.py`) |
| `[abc]`, `[a-z]` | Character class |
| `{a,b}` | Any of the comma-separated alternatives (`*.{yml,yaml}`, no nesting) |

Table: Glob syntax

### search_grep

Searches file contents using a regular expression or literal string.

| Argument | Type | Required | Description |
|---|---|---|---|
| `pattern` | string | Yes | Regular expression ([RE2 syntax](https://github.com/google/re2/wiki/Syntax); no backreferences or lookahead) |
| `path` | string | | Directory or file to search (the working directory when omitted) |
| `include` | string | | [Glob](#glob) filtering files to search (for example `*.go`, `src/**/*.ts`) |
| `exclude_dirs` | string[] | | Directory names to exclude from the walk |
| `gitignore` | boolean | | Skip files ignored by `.gitignore` (`true` when omitted; see [Handling of .gitignore](#handling-of-gitignore)) |
| `ignore_case` | boolean | | Case-insensitive matching |
| `literal` | boolean | | Treat `pattern` as a literal string rather than a regular expression |
| `output_mode` | string | | `content` (default; matching lines with context), `files` (paths of matching files only), `count` (match count per file) |
| `context` | integer | | Number of context lines before and after each match in `content` mode (`grep -C`) |
| `before`, `after` | integer | | Number of context lines before or after each match (overrides `context`; `grep -B` / `-A`) |
| `max_matches` | integer | | Maximum number of results (matching lines for `content`, files for `files` and `count`; `200` when omitted) |
| `max_file_bytes` | integer | | Skip files larger than this size (10 MiB when omitted) |
| `charset` | string | | File encoding (`auto` when omitted) |
| `multiline` | boolean | | Allow patterns to match across multiple lines against the entire file |

Table: Arguments of search_grep

| Output | Type | Description |
|---|---|---|
| `matches` | object[] | Array of matches in `content` mode (`path`, `line`, `end_line` [with `multiline`], `text` [up to 500 bytes], `before`, `after`, `charset`) |
| `files` | object[] | Array of matching files in `files` and `count` modes (`path`, `count`) |
| `files_scanned` | integer | Number of files scanned |
| `files_matched` | integer | Number of files with at least one match |
| `skipped` | integer | Number of entries that could not be read |
| `skipped_large` | integer | Number of files skipped because they exceeded the size limit |
| `truncated` | boolean | `true` when truncated at `max_matches` |

Table: Output of search_grep

`search_grep` skips non-regular files, binary files containing a NUL byte in the first 8000 bytes (unless a UTF-16 BOM is present or UTF-16 is specified in `charset`), files larger than `max_file_bytes`, and (when `gitignore: true`) files matched by ignore rules. If a file contains a line longer than 1 MiB, searching stops just before that line.

#### Searching across lines (multiline)

With `multiline: true`, the entire file is converted to UTF-8 and matched in RE2 `(?sm)` mode (`.` matches newlines, and `^` and `$` match the start and end of each line). Each match reports its first line (`line`), last line (`end_line`), and matched text (`text`, up to 500 bytes). Empty matches are excluded, and files over 32 MiB are counted in `skipped_large`.

```json
{"pattern": "func \\w+\\(ctx context\\.Context.*?\\n\\}", "path": "/srv/app", "include": "*.go", "multiline": true}
```

```json
{"pattern": "TODO", "path": "/srv/app", "include": "*.{c,h}", "output_mode": "count"}
```

```json
{"pattern": "connect(", "literal": true, "path": "/srv/app/src", "context": 3}
```

### Handling of .gitignore

By default (`gitignore: true`), `search_grep` and `search_find` evaluate the following ignore files in order and skip matching paths (the user's global `core.excludesFile` is not read):

1. `.git/info/exclude` in the repository (when the search starts inside a Git repository)
2. `.gitignore` files at each directory level from the repository root (the directory containing `.git`) down to the search start directory
3. `.gitignore` files in each directory entered during the walk

Rules in deeper directories and on later lines within the same file take precedence. Negation (`!pattern`), directory-only patterns (`pattern/`), and `**` are supported; however, ignored directories are not entered, so files inside an ignored directory cannot be re-included with `!`.

### search_find

Finds files and directories whose name or relative path matches a [glob](#glob).

| Argument | Type | Required | Description |
|---|---|---|---|
| `pattern` | string | | Glob pattern (matches all entries when omitted) |
| `path` | string | | Directory to search from (the working directory when omitted) |
| `type` | string | | `file`, `dir`, or omitted (both) |
| `exclude_dirs` | string[] | | Directory names to exclude from the walk |
| `gitignore` | boolean | | Skip paths ignored by `.gitignore` (`true` when omitted) |
| `sort` | string | | `path` (default, walk order) or `mtime` (newest first; collects and sorts up to 100,000 entries) |
| `max_results` | integer | | Maximum number of results (`1000` when omitted) |

Table: Arguments of search_find

| Output | Type | Description |
|---|---|---|
| `paths` | string[] | Array of matching paths (`path` itself is not included) |
| `skipped` | integer | Number of entries that could not be read |
| `truncated` | boolean | `true` when truncated at `max_results` (or at the 100,000-entry limit when sorting by `mtime`) |

Table: Output of search_find

## Checking differences

### fs_manifest

Returns the relative path, size, permissions, and SHA-256 hash of regular files under a directory (or for a single file). Use it to compare hashes against a local source tree and identify only the files that differ.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | | Base directory or single file (the working directory when omitted) |
| `include` | string | | [Glob](#glob) filtering files to include |
| `exclude_dirs` | string[] | | Directory names to exclude from the walk (`[".git", ".hg", ".svn"]` when omitted) |
| `max_files` | integer | | Maximum number of files (`2000` when omitted) |

Table: Arguments of fs_manifest

| Output | Type | Description |
|---|---|---|
| `root` | string | Absolute path of the base directory |
| `files` | object[] | Array of file records (`path` [always `/`-separated], `size`, `mode` [octal string], `sha256` [lowercase hex]) |
| `skipped` | integer | Number of entries that could not be read |
| `truncated` | boolean | `true` when truncated at `max_files` |

Table: Output of fs_manifest

## Privileged execution

### exec_sudo

Runs a command via `sudo` on a Linux target (not published on Windows). The output schema is the same as [`exec_run`](#exec_run).

| Argument | Type | Required | Description |
|---|---|---|---|
| `command` | string | Yes | Command line to run |
| `user` | string | | Target user (`root` when omitted) |
| `cwd` | string | | Working directory |
| `stdin` | string | | Text passed to the command's standard input |
| `password` | string | | `sudo` password (supplied via askpass only when `sudo` prompts for it) |
| `timeout_sec` | integer | | Timeout in seconds (`--timeout` when omitted; capped at `--max-timeout`) |
| `output_encoding`, `stdin_encoding` | string | | Input and output encodings (same as [`exec_run`](#exec_run)) |

Table: Arguments of exec_sudo

When `password` is omitted, `exec_sudo` runs `sudo -n [-u <user>] -- sh -c '<command>'`. If `sudo` requires a password, it exits with an error and returns the message in `stderr`. In environments where `NOPASSWD` is granted only to specific commands rather than `sh`, invoke `sudo -n` directly via `argv` of `exec_run` instead of `exec_sudo` (see the [Operations Guide](operations.md#configuring-sudo)).

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

#### Passing a password

When `password` is provided, `exec_sudo` runs `sudo -A [-u <user>] -- sh -c '<command>'` using `host-shell` itself as the `SUDO_ASKPASS` helper:

1. For each call, it creates a temporary directory with mode `0700` and a named pipe (FIFO) with mode `0600`, writing the password to the pipe up to 3 times.
2. It starts `sudo` with `SUDO_ASKPASS` set to the `host-shell` executable path and `HOST_SHELL_ASKPASS` set to the FIFO path (neither environment variable contains the password itself).
3. Only if `sudo` prompts for a password does the helper run, read one line from the FIFO, and return it to `sudo`; the FIFO and temporary directory are removed immediately afterward.

This ensures the password never leaks into the command's standard input even when `NOPASSWD` is active or credentials are cached. The audit log records `password: "<redacted>"`, and if the password string appears in `stdout` or `stderr`, it is replaced with `[REDACTED]` regardless of whether `--redact` is enabled.

## Environment information

### sys_get_info

Returns information about the target environment, published tool groups, and active policy restrictions (no arguments).

| Output | Type | Description |
|---|---|---|
| `os`, `arch` | string | OS (`linux` / `windows`) and architecture (`amd64` / `arm64`) |
| `hostname` | string | Host name |
| `user`, `uid` | string | User name (`DOMAIN\user` on Windows) and UID (SID on Windows) |
| `home`, `cwd` | string | Home directory and current working directory of `host-shell` |
| `path_separator` | string | Path separator (`/` or `\`) |
| `default_shell` | string | Default shell for `exec_run` |
| `console_encoding`, `file_encoding` | string | Default encodings for console output and text files ([Default encodings of the target](#default-encodings-of-the-target)) |
| `num_cpu` | integer | Number of logical CPUs |
| `pid`, `executable` | integer, string | Process ID and executable path of `host-shell` |
| `version`, `go_version` | string | `host-shell` version and the Go version used to build it |
| `tool_groups` | string[] | Published tool groups |
| `restrictions` | string[] | Active policy restrictions (`item: value`; omitted when none are set) |
| `client_proxy`, `proxy_url_file` | boolean, string | Whether the client HTTP proxy is enabled, and the path to its credential URL file |
| `forward_local`, `forward_remote` | boolean | Port forwarding directions permitted on the target (`--allow-forward-local`, `--allow-forward-remote`) |
| `remote_forwards` | object[] | Active remote forwards listening on the target (`forward_id`, `address`; omitted when none are active) |

Table: Output of sys_get_info

## Network

All network tools originate connections from the target machine (`localhost` refers to the target itself). Destinations can be restricted with [`--allow-host`](#network-destinations---allow-host).

### net_request_http

Sends an HTTP or HTTPS request and returns the status, headers, and response body.

| Argument | Type | Required | Description |
|---|---|---|---|
| `url` | string | Yes | Request URL (`http` or `https`) |
| `method` | string | | HTTP method (`GET` when omitted) |
| `headers` | object | | Request headers (`Host` may also be specified) |
| `body` | string | | Request body (mutually exclusive with `body_file`) |
| `body_encoding` | string | | `text` (default, UTF-8) or `base64` |
| `body_file` | string | | Path of a file on the target to send as the request body |
| `unix_socket` | string | | Path of a Unix domain socket to connect to (for example `/var/run/docker.sock`) |
| `timeout_sec` | integer | | Timeout in seconds (`--timeout` when omitted; capped at `--max-timeout`) |
| `no_redirects` | boolean | | Do not follow redirects; return 3xx responses as-is (follows up to 10 redirects by default) |
| `insecure` | boolean | | Skip TLS certificate verification |
| `ca_cert` | string | | Path on the target to an additional trusted CA certificate (PEM) |
| `client_cert`, `client_key` | string | | Paths on the target to the client certificate and private key (PEM) for mTLS |
| `proxy` | string | | Proxy URL or `none` (uses environment variables such as `HTTP_PROXY` when omitted) |
| `response_encoding` | string | | `auto` (default), `text`, `base64` |
| `max_bytes` | integer | | Maximum response body bytes to return (defaults to and is capped at `--max-output`) |
| `save_to` | string | | Stream the response body directly to a file on the target instead of returning it in `body` (no size limit) |

Table: Arguments of net_request_http

| Output | Type | Description |
|---|---|---|
| `status`, `status_text`, `proto` | integer, string, string | Status code, status text, and protocol (`HTTP/1.1`, etc.) |
| `headers` | object | Response headers (multiple values joined with `, `) |
| `body`, `body_encoding`, `body_charset` | string | Response body, its encoding (`text` / `base64`), and original character encoding |
| `body_bytes`, `body_truncated` | integer, boolean | Bytes received and whether the body was truncated |
| `saved_to` | string | Absolute path of the file written when `save_to` is used |
| `final_url`, `redirects` | string, string[] | Final URL and intermediate redirect URLs when redirects were followed |
| `tls` | object | TLS version and server certificate details (`subject`, `issuer`, `dns_names`, `not_before`, `not_after`, `days_left`, `verified`, `verify_error`) |
| `duration_ms` | integer | Elapsed time (milliseconds) |

Table: Output of net_request_http

```json
{"url": "http://localhost:8080/health"}
```

```json
{"url": "http://localhost/v1.43/containers/json", "unix_socket": "/var/run/docker.sock"}
```

### net_exchange

Connects over TCP, TLS, or a Unix domain socket to send and receive data.

| Argument | Type | Required | Description |
|---|---|---|---|
| `host` | string | | Host name or IP address (`localhost` when omitted) |
| `port` | integer | * | Port number |
| `unix_socket` | string | * | Path of a Unix domain socket to connect to instead of `host` and `port` |
| `tls`, `insecure` | boolean | | Whether to use TLS, and whether to skip certificate verification |
| `send`, `send_encoding` | string | | Data to send after connecting and its format (`text` [default] / `base64`) |
| `close_write` | boolean | | Half-close the write side after sending |
| `until` | string | | Stop reading once received data matches this regular expression |
| `idle_ms` | integer | | Milliseconds of inactivity before stopping read (`1000` when omitted) |
| `timeout_sec` | integer | | Overall timeout in seconds (`10` when omitted) |
| `max_bytes` | integer | | Maximum bytes to receive |
| `receive_encoding` | string | | `auto` (default; `text` if valid UTF-8, otherwise `base64`), `text`, `base64` |

Table: Arguments of net_exchange (* specify either `port` or `unix_socket`)

The output fields are `remote_addr`, `sent_bytes`, `received`, `receive_encoding`, `received_bytes`, `matched`, `closed_by_peer`, `truncated`, `tls`, and `duration_ms`.

```json
{"port": 6379, "send": "PING\r\n", "until": "\r\n"}
```

### net_check

Runs DNS resolution, TCP connection checks, and TLS handshakes with certificate verification against multiple targets in parallel.

| Argument | Type | Required | Description |
|---|---|---|---|
| `targets` | string[] | Yes | Array of targets (up to 100; `host:port`, `host` [DNS resolution only], or a URL [`https://` also checks TLS]) |
| `tls` | boolean | | Perform a TLS handshake and certificate check on all targets |
| `timeout_ms` | integer | | Per-target timeout in milliseconds (`5000` when omitted) |

Table: Arguments of net_check

Each element of `results` in the output contains `target`, `host`, `port`, `addresses`, `dns_ms`, `connected`, `connect_ms`, `tls` (same format as [`net_request_http`](#net_request_http)), and `error`.

### net_list_listeners

Returns listening TCP and UDP sockets along with the owning processes (read from `/proc/net/{tcp,tcp6,udp,udp6}` and `/proc/<pid>/fd` on Linux, and from `netstat -ano` and `tasklist` on Windows).

| Argument | Type | Required | Description |
|---|---|---|---|
| `port` | integer | | Filter by port number |
| `protocol` | string | | `tcp` or `udp` |
| `process` | string | | Filter by substring in the process name (case-insensitive) |

Table: Arguments of net_list_listeners

Each element of `listeners` in the output contains `protocol` (`tcp`, `tcp6`, `udp`, `udp6`), `address`, `port`, `pid`, and `process` (on Linux without root privileges, `pid` is empty for processes owned by other users, which is noted in `note`).

### net_list_interfaces

Returns the network interfaces (`name`, `index`, `mtu`, `mac`, `flags`, `addresses` [CIDR notation]) (no arguments).

## Administration

Tools in the `admin` group inspect and manage processes, services, system logs, and disks using a consistent schema across operating systems.

### proc_list

Returns the list of running processes (read from `/proc` on Linux, the Win32 API on Windows, and `ps` on macOS).

| Argument | Type | Required | Description |
|---|---|---|---|
| `filter` | string | | Filter by substring in the process name or command line (case-insensitive) |
| `pid` | integer | | Return only the specified PID and its descendant processes |
| `max` | integer | | Maximum number of results (`500` when omitted) |

Table: Arguments of proc_list

| Field | Type | Description | Linux | Windows |
|---|---|---|---|---|
| `pid`, `ppid` | integer | Process ID and parent process ID | `/proc/<pid>/stat` | Toolhelp |
| `name` | string | Process name | `/proc/<pid>/stat` (`comm`) | Executable name |
| `command_line` | string | Command line | `/proc/<pid>/cmdline` | `NtQueryInformationProcess` |
| `user` | string | User running the process | `/proc/<pid>/status` (UID resolved) | Process token (`DOMAIN\user`) |
| `state` | string | Process state (`R`, `S`, `D`, `Z`, `T`, etc.) | `/proc/<pid>/stat` | None |
| `start_time` | string | Start time (RFC 3339) | `/proc/<pid>/stat` | `GetProcessTimes` |
| `memory_bytes` | integer | Physical memory usage (RSS / working set) | `/proc/<pid>/statm` | `GetProcessMemoryInfo` |

Table: Fields of proc_list

The output contains `processes` (sorted by PID), `total` (total matches after filtering), and `truncated`. Fields that cannot be read (for example, due to insufficient permissions) are omitted.

### proc_kill

Sends a signal to or terminates the process with the specified PID (for jobs started with `job_start`, use [`job_stop`](#job_stop)).

| Argument | Type | Required | Description |
|---|---|---|---|
| `pid` | integer | Yes | Process ID |
| `signal` | string | | Linux: `terminate` (default), `kill`, `interrupt`, `hangup`, `quit`, `stop`, `continue`, `usr1`, `usr2`. Windows: `kill` (default and only supported value) |
| `tree` | boolean | | Include descendant processes (walks `/proc` on Linux; runs `taskkill /T /F` on Windows) |
| `sudo` | boolean | | Run `sudo -n kill` on Linux |

Table: Arguments of proc_kill

The output contains `signaled` (array of signaled PIDs) and `exited` (`true` if the process exited within 3 seconds).

### svc_list

Returns the list of services (up to 2000; sets `truncated: true` beyond that). On Linux it queries `systemctl list-units` and `list-unit-files`; on Windows it queries the Service Control Manager with read-only access (no administrator privileges required).

| Argument | Type | Required | Description |
|---|---|---|---|
| `filter` | string | | Filter by substring in the service name or description (case-insensitive) |
| `state` | string | | `running`, `stopped`, `failed`, `all` (default) |
| `user` | boolean | | Query user systemd units on Linux (`systemctl --user`) |

Table: Arguments of svc_list

| Field | Linux | Windows |
|---|---|---|
| `name`, `description` | Unit name (`nginx.service`) and description | Service name and display name |
| `state` | `running`, `stopped`, `failed`, `starting`, `stopping` | `running`, `stopped`, `starting`, `stopping`, `paused` |
| `sub_state` | systemd SUB state (`running`, `exited`, `dead`, etc.) | |
| `start_mode` | `enabled`, `disabled`, `static`, `masked`, etc. | `auto`, `delayed-auto`, `manual`, `disabled` |
| `pid` | | Process ID |

Table: Fields of svc_list

### svc_control

Queries the status of a single service, or performs an action such as starting or stopping it and returns the updated status (when `--read-only` is set, only `status` is permitted).

| Argument | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Service name (on Windows, the service name rather than the display name) |
| `action` | string | | `status` (default), `start`, `stop`, `restart`, `reload` (Linux only), `enable`, `disable` |
| `user` | boolean | | Operate on user systemd units on Linux |
| `sudo` | boolean | | Run `sudo -n systemctl ...` on Linux |
| `wait_sec` | integer | | Seconds to wait for start or stop to complete on Windows (`30` when omitted) |

Table: Arguments of svc_control

| Output | Type | Description |
|---|---|---|
| `service` | object | Service status in the same format as [`svc_list`](#svc_list) |
| `properties` | object | Detailed properties (`FragmentPath`, `MainPID`, `Result`, etc. on Linux; `BinaryPath`, `Account`, `Dependencies`, `ExitCode`, etc. on Windows) |
| `status_text`, `output` | string | Output of `systemctl status --lines=20` on Linux, and output of the control command |

Table: Output of svc_control

### log_query

Queries the system log (`journalctl -o json` on Linux, `wevtutil qe /f:RenderedXml` on Windows) and returns a structured array in chronological order (oldest first, newest last).

| Argument | Type | Required | Description |
|---|---|---|---|
| `unit` | string | | Linux: systemd unit name. Windows: event provider (source) name |
| `log` | string | | Windows: channel name (`System` [default], `Application`, `Security`, `Setup`, etc.). Linux: `kernel` for kernel messages only |
| `since`, `until` | string | | Start and end times (relative durations such as `30m`, `2h`, `1d`, timestamps like `2026-10-30 09:00`, or RFC 3339) |
| `priority` | string | | Minimum severity (Linux: `emerg` through `debug`; Windows: `critical`, `error`, `warning`, `information`, `verbose`) |
| `event_id` | integer | | Windows event ID |
| `grep` | string | | Case-insensitive regular expression filter on the message text |
| `lines` | integer | | Maximum number of most recent entries to return (`100` when omitted, up to `2000`) |
| `user`, `sudo` | boolean | | Read the user journal on Linux, or run `sudo -n journalctl` |

Table: Arguments of log_query

Each element of `entries` in the output contains `time` (RFC 3339), `priority`, `source`, `pid`, `event_id` (Windows), and `message`. If the output size limit is exceeded, the oldest entries are dropped and `truncated: true` is set.

### disk_list

Returns disk usage for mounted file systems (Linux) or drives (Windows).

| Argument | Type | Required | Description |
|---|---|---|---|
| `all` | boolean | | On Linux, include pseudo file systems (`proc`, `sysfs`, `cgroup`, etc.) |

Table: Arguments of disk_list

Each element of `disks` in the output contains `mount`, `device` (Linux), `type`, `total_bytes`, `used_bytes`, `free_bytes`, `used_percent`, `total`, and `free`.

## Windows registry

Registry tools (in the `admin` group) are published only on Windows targets.  
Specify keys starting from the root hive (`HKLM`, `HKCU`, `HKCR`, `HKU`, `HKCC`, or full names such as `HKEY_LOCAL_MACHINE`); both `\` and `/` are accepted as separators. Setting `view` to `"64"` or `"32"` accesses the corresponding WOW64 registry view on 64-bit Windows.

### reg_read

| Argument | Type | Required | Description |
|---|---|---|---|
| `key` | string | Yes | Registry key path |
| `value` | string | | Value name to read (an empty string reads the default value; when omitted, returns all subkeys and values) |
| `view` | string | | `"64"` or `"32"` |

Table: Arguments of reg_read

The output contains `key`, `subkeys`, and `values` (`name`, `type`, `data`, `expanded`). `data` is formatted according to the value type:

| type | Format of data |
|---|---|
| `REG_SZ`, `REG_EXPAND_SZ` | String (for `REG_EXPAND_SZ`, the value with environment variables expanded is also returned in `expanded`) |
| `REG_MULTI_SZ` | Array of strings (`string[]`) |
| `REG_DWORD`, `REG_QWORD` | Number |
| Others (`REG_BINARY`, etc.) | Base64 string |

Table: Format of registry values

### reg_write

Creates or updates a registry value (creating the key if it does not exist).

| Argument | Type | Required | Description |
|---|---|---|---|
| `key` | string | Yes | Registry key path |
| `value` | string | | Value name (an empty string writes the default value; when omitted, only the key is created) |
| `type` | string | | `REG_SZ` (default), `REG_EXPAND_SZ`, `REG_MULTI_SZ`, `REG_DWORD`, `REG_QWORD`, `REG_BINARY` |
| `data` | any | | Value data to write |
| `view` | string | | `"64"` or `"32"` |

Table: Arguments of reg_write

The output contains `key`, `created_key`, and `value`.

### reg_delete

Deletes a registry value or key (deleting a root hive itself is refused).

| Argument | Type | Required | Description |
|---|---|---|---|
| `key` | string | Yes | Registry key path |
| `value` | string | | Value name to delete (an empty string deletes the default value; when omitted, deletes the key itself) |
| `recursive` | boolean | | Delete the key and all of its subkeys recursively (without this flag, keys with subkeys return an error) |
| `view` | string | | `"64"` or `"32"` |

Table: Arguments of reg_delete

## Transfer with the local machine (bridge)

`host-shell bridge` is a client-side relay subcommand. It acts as an MCP server toward the agent, spawns the command that connects to `host-shell` on the target, and adds tools for file transfer, port forwarding, and HTTP proxying between the local machine and the target.

```mermaid
---
title: bridge configuration
---
flowchart TB
    subgraph client ["Client"]
        agent["Agent"]
        bridge["host-shell bridge"]
        local["Local files"]
    end
    subgraph target ["Target"]
        remote["host-shell --mcp"]
        files["Target files"]
    end
    agent -->|"MCP (stdio)"| bridge
    bridge -->|"MCP (stdio of nested ssh)"| remote
    bridge -->|"transfer_push, transfer_pull"| local
    remote --> files
```

### Syntax

```text
host-shell bridge [options] -- COMMAND [ARGS...]
```

`COMMAND` is the command that starts `host-shell --mcp` on the target. The standard error of `COMMAND` is passed through directly to the standard error of `bridge`.

| Option | Value | Description |
|---|---|---|
| `--allow-local-path` | Directory (repeatable) | Local directories that file transfers may read and write |
| `--allow-local-read-path` | Directory (repeatable) | Local directories that `transfer_push` may read |
| `--no-transfer` | None | Relay only, without adding the file transfer tools (`transfer_push`, `transfer_pull`) |
| `--proxy`, `--proxy-allow-host` | None, `HOST[:PORT]` (repeatable) | Enable the [HTTP proxy through the client](client-proxy.md) and allow destinations |
| `--forward-local`, `--forward-remote` | `[BIND:]PORT:HOST:HOSTPORT` (repeatable) | Persistent local and remote forwards ([Port forwarding](port-forward.md)) |
| `--forward-socks` | `[BIND:]PORT` (repeatable) | Persistent SOCKS5 proxy (requires `--forward-allow-local`) |
| `--forward-allow-local`, `--forward-allow-remote` | `HOST[:PORT]` (repeatable) | Destinations allowed for `forward_open` and SOCKS5 |

Table: Options of bridge

### Relay

- Target tools and resources are published with the same names, descriptions, and schemas, and tool calls, progress notifications (`progressToken`), cancellation notifications, log notifications, and confirmation requests (`--confirm`) are relayed transparently.
- `instructions` in the `initialize` response combines the target's guidance with instructions for the `bridge` tools.
- `bridge` connects to the target using the client name `host-shell-bridge`; only `fs_read` (`base64`) calls from this client name are allowed to read chunks of up to 1 MiB at a time.
- When the target session disconnects, `bridge` also exits.

### transfer_push

Transfers a local file or directory to the target.

| Argument | Type | Required | Description |
|---|---|---|---|
| `local_path` | string | Yes | Local path (relative paths are resolved against the working directory of `bridge`) |
| `remote_path` | string | Yes | Target path (for a single file, an existing directory or a trailing `/` places the file inside that directory under its original name) |
| `overwrite` | boolean | | Overwrite existing files whose content differs (files with identical content are always skipped) |
| `exclude` | string[] | | Globs of file and directory names to exclude (for example `[".git", "node_modules", "*.pyc"]`) |
| `dry_run` | boolean | | Return the list of files that would be transferred without writing anything |
| `parallel` | integer | | Number of concurrent file transfers (1 to 16; `4` when omitted) |

Table: Arguments of transfer_push

Each file is transferred as follows (on failure, `.host-shell-part` is removed; empty directories and symbolic links are skipped):

1. The SHA-256 of any existing target file is checked with `fs_manifest`; if it matches, the file is skipped (`unchanged`), and if it differs while `overwrite` is `false`, the file is skipped (`exists`).
2. The file is written to `<destination>.host-shell-part` in 1 MiB chunks using `fs_write` (`base64`, `offset`), applying the local executable bit via `perm` on the first chunk.
3. The SHA-256 is verified with `fs_manifest`, and if it matches, the temporary file is renamed to its final path with `fs_move`.

| Output | Type | Description |
|---|---|---|
| `source`, `destination` | string | Source and destination paths |
| `transferred`, `unchanged`, `failed` | integer | Files transferred (or planned when `dry_run` is set), skipped as unchanged, and failed (`exists` + `error`) |
| `bytes` | integer | Total bytes transferred |
| `files` | object[] | Per-file results (`path`, `bytes`, `status` [`sent`/`unchanged`/`exists`/`would-send`/`skipped`/`error`], `error`). When more than 500 files are processed, `unchanged` entries are omitted and `truncated: true` is set |
| `duration_ms` | integer | Elapsed time (milliseconds) |

Table: Output of transfer_push and transfer_pull

### transfer_pull

Transfers a file or directory from the target to the local machine. Arguments and output match [`transfer_push`](#transfer_push) (`local_path` is the destination). After checking SHA-256 hashes with `fs_manifest` on the target, changed files are read in 1 MiB chunks via `fs_read` (`base64`) into `<local destination>.host-shell-part`, verified by SHA-256, given the source permissions, and renamed into place.

### Client HTTP proxy and port forwarding

`bridge` multiplexes TCP streams over the MCP standard I/O channel, enabling an HTTP/HTTPS proxy through the client (`proxy_status`) and TCP / SOCKS5 port forwarding (`forward_open`, `forward_close`, `forward_status`).

- For startup options, destination rules, environment variable precedence, and `proxy_status` details, see [HTTP proxy through the client](client-proxy.md).
- For local forwards, remote forwards, SOCKS5, permission matching, and `forward_open`, `forward_close`, and `forward_status` details, see [Port forwarding over SSH stdio](port-forward.md).

## Starting and ending processes

### Shells

The `command` argument is passed to the shell selected by `shell` as follows:

| shell | OS | Invocation |
|---|---|---|
| `sh` | Linux (default) | `sh -c '<command>'` |
| `bash` | Linux | `bash -c '<command>'` |
| `cmd` | Windows | `"%ComSpec%" /d /s /c "<command>"` |
| `powershell` | Windows (default) | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command "$input \| . ([ScriptBlock]::Create(...<base64>...))"` |
| `pwsh` | Windows, Linux | `pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command "$input \| . ([ScriptBlock]::Create(...<base64>...))"` |

Table: How shells are started

PowerShell is the default shell on Windows because passing the command line as UTF-16LE base64 avoids quote interpretation by `cmd` and SSH and normalizes output to UTF-8.  
`-Command` receives a short loader that decodes and executes the base64 script, piping standard input via `$input` in the current scope. `-EncodedCommand` is avoided because Windows PowerShell 5.1 writes errors, warnings, and progress streams to standard error as CLIXML (XML) when started with `-EncodedCommand`.

The following statements are prepended to the PowerShell script:

| Statement | Purpose |
|---|---|
| `$ProgressPreference = 'SilentlyContinue'` | Suppress progress output (initial module loading, `Invoke-WebRequest`, etc.) |
| `if ($PSStyle) { $PSStyle.OutputRendering = 'PlainText' }` | Suppress PowerShell 7 ANSI color sequences (no-op on 5.1) |
| `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8` | Normalize PowerShell output to UTF-8 |

Table: Statements prepended to a PowerShell script

Errors are written to standard error and `Write-Warning` messages to standard output as plain text (standard `-Command` behavior).  
Exit code handling is appended to the script so exit codes follow the same rules as `sh -c` on Linux:

| Result of the last statement | Exit code |
|---|---|
| Succeeded (`$?` is true) | `0` |
| Failed, and a native command set a non-zero `$LASTEXITCODE` | `$LASTEXITCODE` |
| Failed, and `$LASTEXITCODE` is 0 or unset (cmdlet failure) | `1` |
| Exited via `exit N` | `N` |
| Terminated by an unhandled exception (`throw`, etc.) | `1` |

Table: PowerShell exit codes

If a native command invoked from PowerShell outputs Shift_JIS and appears garbled, specify `"shell": "cmd"` (`cmd` is started with `/d` to disable AutoRun and receives the command string directly).

#### Standard input of PowerShell on Windows

PowerShell decodes standard input (`$input`) using the console input code page at process startup; changing `[Console]::InputEncoding` inside the script does not affect `$input`.  
Therefore, `host-shell` allocates a hidden console with input and output code pages set to 65001 (UTF-8) and starts PowerShell attached to that console, allowing arbitrary Unicode text in `stdin` regardless of the target's system code page.

| Condition | Default `stdin_encoding` | Console used by PowerShell |
|---|---|---|
| `stdin_encoding` omitted or `utf-8` | `utf-8` | `host-shell` hidden console (65001) |
| `stdin_encoding` set to a value other than `utf-8` | Specified value | PowerShell's own console (code page unchanged) |
| `host-shell` standard I/O is a console (started directly in a terminal) | `system` with `shell`, `utf-8` with `argv` | PowerShell's own console |

Table: Standard input of PowerShell on Windows

- This applies to calls with `shell` set to `powershell` or `pwsh`, and `argv` calls whose first element is `powershell` or `pwsh` (with or without `.exe` or a directory path). `argv` arguments are not modified. `pty` jobs use ConPTY and are unaffected.
- The hidden console is allocated once on the first PowerShell call (approx. 100 ms). When `host-shell` is started directly in an interactive terminal, no hidden console is created so the user's console code page is not modified.
- `cmd` and other native binaries run in their own consoles (OEM code page).
- On non-English systems (such as Japanese Windows), PowerShell running in the code page 65001 console and native commands spawned from it may emit English diagnostic messages (Windows falls back to English when the console code page cannot represent the UI language).

### Starting with argv

With `argv`, the executable is started directly without a shell (`PATH` is searched, and on Windows arguments are escaped using standard MSVC rules).

### Environment variables and standard input

Child processes inherit the environment variables of `host-shell` with any variables matching `--unset-env` removed and any variables in `env` added or overridden.  
When `stdin` is omitted, standard input is connected to `/dev/null` (or `NUL` on Windows); child processes never inherit the standard input or output of `host-shell` itself (the MCP transport channel).

### How processes are ended

On timeout, cancellation, `job_stop` (`kill`), or `host-shell` exit, commands are killed together with their descendant processes.

| OS | Termination method | Processes that can escape |
|---|---|---|
| Linux | Started in a new process group (a new session for `pty`), and `SIGKILL` is sent to the entire process group | Processes that moved to a different process group or session via `setsid`, etc. |
| Windows | The process tree is terminated with `taskkill /T /F` | Descendant processes orphaned from the tree because their parent exited first (terminated by the Job Object when `host-shell` exits) |

Table: How processes are ended

Even if `host-shell` itself is killed abruptly (`kill -9`, SSH disconnection, etc.), the following OS mechanisms prevent child processes from being left behind:

| OS | Mechanism | Processes that may remain |
|---|---|---|
| Linux | Sets `PR_SET_PDEATHSIG(SIGKILL)` on direct child processes | Grandchild processes that do not exit when the direct child dies |
| Windows | Places `host-shell` into a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` at startup, which automatically includes all descendant processes | None (breaking out of the Job Object is not permitted) |

Table: What happens to commands when host-shell exits

Start daemons and services that must outlive the MCP session via `systemd-run`, a Windows service, or Task Scheduler rather than spawning them directly as child processes of `host-shell`.  
If descendant processes hold the output pipes open after the main command exits, `host-shell` waits up to 2 seconds before closing the pipes and returning the result.

## Audit log

`host-shell` records every tool call to a log file on the target in JSON Lines format (one JSON object per line).

### Location

| OS | Default base path |
|---|---|
| Linux | `$XDG_STATE_HOME/host-shell/audit.log` (`~/.local/state/host-shell/audit.log` when `XDG_STATE_HOME` is unset) |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: Default base path of the audit log

Logs are written to daily files named `<base name>-YYYY-MM-DD<extension>` (for example `audit-2026-10-31.log`) based on the target's local date.

- In addition to the current day's file, the 5 most recent daily files are retained; older `<base name>-YYYY-MM-DD<extension>` files are deleted automatically at startup and on date rollover (an undated `audit.log` file is never deleted).
- The log directory is created with mode `0700` and log files with mode `0600`. Each record opens, appends to, and closes the file, preventing interleaved lines even when multiple sessions write concurrently.
- If the log file cannot be opened, `host-shell` refuses to start (pass `--no-log` when audit logging is not needed).

### Format

```json
{"time":"2026-10-30T08:15:42.42+09:00","level":"INFO","msg":"tool_call","tool":"fs_read","args":{"path":"/nonexistent"},"duration_ms":0,"is_error":true,"error":"open /nonexistent: no such file or directory"}
```

| msg | When recorded | Fields |
|---|---|---|
| `server_start` | At startup | `version`, `pid` |
| `tool_call` | When a tool call finishes (including policy denials and `<tool name> (confirmation requested)`) | `tool`, `args`, `duration_ms`, `is_error`, `error` (on error) |
| `server_stop` | At exit | `error` (`<nil>` on normal exit) |

Table: Audit log records

### Values not recorded

`args` records the tool arguments, with the following fields masked to protect secrets and keep log files compact:

| Tool | Argument | Recorded value |
|---|---|---|
| `exec_run`, `job_start`, `exec_sudo` | `stdin` | `<N chars>` |
| `exec_sudo` | `password` | `<redacted>` |
| `job_write` | `input` | `<N chars>` |
| `fs_edit` | `old_string`, `new_string` (including inside `edits`) | `<N chars>` |
| `fs_write` | `content` | `<N chars, sha256 ...>` |
| `net_request_http` | `body`, plus `Authorization`, `Proxy-Authorization`, `Cookie`, and `X-Api-Key` in `headers` | `<N chars>` |
| `net_exchange` | `send` | `<N chars>` |

Table: Values replaced in the audit log

Because `command` and `argv` are logged verbatim, do not pass secrets such as passwords on the command line; pass them via `stdin` instead.

## Limitations

- Pseudo-terminals are supported only via `job_start` with `pty: true` (`exec_run` always runs non-interactively over pipes). To inspect full-screen programs (`vi`, `top`, etc.), use `job_read` with `screen: true`.
- Automatic encoding detection checks only BOMs, UTF-16 byte distribution, and UTF-8 validity; it does not distinguish between non-UTF-8 encodings such as Shift_JIS and EUC-JP. When reading or writing files in an encoding other than the target's default, specify `charset` explicitly.
- A short non-UTF-8 byte sequence may occasionally happen to be valid UTF-8 and appear garbled; in that case, specify `output_encoding` or `charset` explicitly.
- `net_list_listeners`, `svc_list`, `svc_control`, `log_query`, and `disk_list` are supported only on Linux and Windows (service and log tools on Linux require systemd).
- Startup restriction flags (policy options) validate tool arguments rather than enforcing OS-level sandboxing (see [Limits of the restrictions](#limits-of-the-restrictions)).
- Following Go 1.26 platform requirements, supported operating systems are Windows 10 / Windows Server 2016 or later (`pty` requires Windows 10 1809 / Windows Server 2019 or later), Linux kernel 3.2 or later, and macOS 12 or later (`bridge` only).
