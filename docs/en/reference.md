# host-shell Reference

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/reference.md](../ja/reference.md)) is the master and takes precedence if they differ.

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

Only with `--mcp` does it serve as an MCP server on standard input and output.  
`bridge` is a subcommand that relays the target's `host-shell` on the local side (see [Transfer with the local machine (bridge)](#transfer-with-the-local-machine-bridge)).  
If none of `--mcp`, `bridge` and `--version` is given, it prints the usage to standard error and exits with exit code 2.

### Options

| Option | Value | Default | Description |
|---|---|---|---|
| `--mcp` | None | | Serve as an MCP server on standard input and output |
| `--version` | None | | Print the version to standard output and exit |
| `--log-file` | Path | See [Audit log](#audit-log) | Base path of the audit log (written to a file with the date inserted before the extension) |
| `--no-log` | None | | Do not record the audit log (takes precedence over `--log-file`) |
| `--max-output` | Bytes (1024 or more) | `65536` | Size limit per output |
| `--timeout` | Duration (for example `90s`, `5m`) | `1m` | Default timeout for running commands |
| `--max-timeout` | Duration | `30m` | Upper limit of the timeout that tool arguments can request (at least `--timeout`) |
| `--windows-shell` | `cmd`, `powershell`, `pwsh` | `powershell` | Default shell on Windows when `shell` is omitted |
| `--tools` | Group and tool names (comma-separated) | `all` | Tools to publish (see [Restrictions (policy)](#restrictions-policy)) |
| `--read-only` | None | | Do not publish tools that change the target |
| `--confirm` | Group and tool names, `destructive` | None | Tools that ask the user for confirmation before running |
| `--allow-path` | Directory (can be repeated) | None | Directories the file tools may read and write |
| `--allow-read-path` | Directory (can be repeated) | None | Directories the file tools may only read |
| `--allow-command` | Program name globs (comma-separated, can be repeated) | None | Programs allowed to start |
| `--deny-command` | Regular expression (can be repeated) | None | Command line patterns that are refused |
| `--allow-host` | Destinations (comma-separated, can be repeated) | None | Destinations the network tools may connect to |
| `--unset-env` | Environment variable name globs (comma-separated, can be repeated) | None | Environment variables not passed to child processes |
| `--redact` | None | | Mask secrets matching known patterns in tool output |
| `--redact-pattern` | Regular expression (can be repeated) | None | Additional regular expression patterns to mask |

Table: Startup options

Options can also be given with a single hyphen, as in `-mcp`.  
An unknown option or a value of the wrong type prints the usage to standard error and exits with exit code 2; a value out of range or an invalid regular expression prints the reason to standard error and exits with exit code 1.

### Input, output and exit

| Item | Behavior |
|---|---|
| Standard input | Receives MCP JSON-RPC messages, one per line |
| Standard output | Writes only MCP JSON-RPC messages (nothing else at all) |
| Standard error | Writes diagnostic messages such as startup failures |
| Working directory | The current directory at startup (usually the user's home directory over SSH) |
| Normal exit | When standard input closes (SSH disconnection) or on SIGINT or SIGTERM (exit code 0) |
| On exit | Stops all running jobs |

Table: Input, output and exit

One SSH session corresponds to one `host-shell` process. Simultaneous connections from several agents run as independent processes and share no state such as jobs.

## Restrictions (policy)

Without restriction options, `host-shell` publishes all tools and runs without restriction with the connecting user's permissions.  
Only what the options specify is restricted, and the restrictions in effect are announced to the agent in `restrictions` of [`sys_get_info`](#sys_get_info), the table at the top of `help_get`, and `instructions` in the initialize response. Calls that violate a restriction are not run and return an error starting with `policy:`.

### Choosing tools (--tools)

Tools are classified into the following groups. `help_get` and `sys_get_info` (the `core` group) are always published.

| Group | Tools |
|---|---|
| `files` | `fs_list`, `fs_stat`, `fs_read`, `fs_edit`, `fs_write`, `fs_mkdir`, `fs_move`, `fs_copy`, `fs_remove`, `fs_manifest` |
| `search` | `search_grep`, `search_find` |
| `exec` | `exec_run`, `exec_sudo`, `job_start`, `job_read`, `job_write`, `job_stop`, `job_list` |
| `net` | `net_request_http`, `net_exchange`, `net_check`, `net_list_listeners`, `net_list_interfaces` |
| `admin` | `proc_list`, `proc_kill`, `svc_list`, `svc_control`, `log_query`, `disk_list`, `reg_read`, `reg_write`, `reg_delete` |

Table: Tool groups

`--tools` takes group names, tool names, `all`, `destructive` (tools that make changes) and `readonly` (read-only tools), comma-separated, evaluated from left to right. A leading `-` removes and `+` (or no prefix) adds. The names of OS-specific tools (`exec_sudo`, `reg_*`) can be given without error on unsupported operating systems (they are not published there).

| Value | Published tools |
|---|---|
| `all` (default) | All tools |
| `files,search` | Only the file and search tools |
| `all,-admin,-exec_sudo` | All tools except the administration tools and `exec_sudo` |
| `files,exec,-fs_remove` | The file and command tools without `fs_remove` |

Table: Examples of --tools

### Read-only (--read-only)

Tools without the `readOnlyHint` annotation are not published. However, `net_request_http` and `net_exchange` are published with these restrictions.

- `net_request_http`: only the methods `GET`, `HEAD` and `OPTIONS` are allowed, and `save_to` cannot be used
- `net_exchange`: `send` cannot be used (only receiving data the destination sends first)

### User confirmation (--confirm)

When a specified tool is called, the user is asked for approval through MCP elicitation, and the tool runs only if approved (the value format is the same as `--tools`).

- For clients of protocol version `2026-07-28`, a confirmation request (`inputRequests`) is returned as the result, following multi round-trip requests (SEP-2322). When the client sends the same call again with the answer, it runs (a `requestState` signed with a per-startup secret key makes only a resend of the same tool with the same arguments valid).
- For clients using other supported protocol versions, the SDK sends `elicitation/create`, receives the answer and continues.
- If the client does not support elicitation, the tool is not run and an error is returned.
- Confirmation requests are recorded in the audit log as `<tool name> (confirmation requested)`.

### File locations (--allow-path, --allow-read-path)

Arguments that take file paths are checked.

| Category | Checked as read | Checked as write |
|---|---|---|
| File operations | `path` of `fs_list`, `fs_stat`, `fs_read`, `fs_manifest`, and `source` of `fs_copy` | `path` of `fs_edit`, `fs_write`, `fs_mkdir`, `fs_remove`, `source` and `destination` of `fs_move`, and `destination` of `fs_copy` |
| Search | `path` of `search_grep`, `search_find` | |
| Running commands | `cwd` of `exec_run`, `job_start`, `exec_sudo` | |
| Network | `body_file`, `ca_cert`, `client_cert`, `client_key` of `net_request_http` | `save_to` of `net_request_http` |

Table: What the location checks cover

- Reading is allowed under `--allow-path` or `--allow-read-path`, and writing only under `--allow-path` (with only `--allow-read-path`, writing anywhere is refused).
- Before the check, paths are made absolute and symbolic links in the existing part are resolved. Access through a symbolic link that points outside the allowed directories is refused.
- On Windows, comparison ignores case.

### Commands (--allow-command, --deny-command)

The programs started by `exec_run`, `job_start` and `exec_sudo` are checked.

- With `--allow-command`, the name of the program to start (the base name; on Windows, lowercased and without an extension such as `.exe`) must match one of the globs.
- For `command` (a shell command line), the shell itself (`sh`, `bash`, `cmd`, `powershell`, `pwsh`) must be allowed. Allowing a shell allows starting any program through it, so to limit the programs, do not allow shells and have the agent use `argv`.
- `exec_sudo` needs both `sudo` and `sh` to be allowed.
- `--deny-command` refuses calls whose command line (for `argv`, the arguments joined with spaces) matches a regular expression (writing it differently bypasses it, so it is meant to prevent mistakes).

### Network destinations (--allow-host)

The destinations of `net_request_http` (including redirect targets), `net_exchange` and `net_check` are checked.

| Format | Meaning |
|---|---|
| `localhost`, `*.internal` | Host name glob (all ports allowed) |
| `api.internal:443`, `*:80` | Host name and port globs |
| `10.0.0.0/8`, `10.0.0.0/8:5432` | CIDR range (for a host name, all resolved IP addresses must be in the range) |
| `unix:/var/run/*.sock` | Glob of a Unix domain socket path |

Table: How to write --allow-host

### Environment variables (--unset-env)

Environment variables matching the given globs are removed from the child processes started by `exec_run`, `job_start`, `exec_sudo` and the administration tools (for example, `--unset-env 'AWS_*,*TOKEN*'`). Variables given explicitly in the `env` tool argument are passed as they are.

### Masking (--redact, --redact-pattern)

Secret patterns are detected in all strings of tool results (`structuredContent` and `content`) and replaced with `[REDACTED]`.

| Kind | Example patterns |
|---|---|
| PEM private keys | The body from `-----BEGIN ... PRIVATE KEY-----` to `-----END ... PRIVATE KEY-----` |
| Authorization headers | `Authorization: Bearer ...` |
| Key and value pairs | The value part of `password=...`, `api_key: ...`, `DB_PASSWORD=...`, `"token": "..."` |
| URL credentials | The password part of `https://user:password@host` |
| Known tokens | AWS access key IDs, GitHub and Slack tokens, `sk-...`, JWTs |

Table: What is masked

`--redact-pattern` adds regular expressions (it works even without `--redact`).  
Writing masked file contents back with `fs_write` as they are would write `[REDACTED]` into the file, so the `policy` topic of `help_get` warns the agent.

### Limits of the restrictions

These restrictions are checks on tool arguments, not OS permission boundaries.

- If the `exec` group is published, the commands it starts can access files and the network directly. To restrict access strictly, remove `exec` with `--tools`, or limit the programs that can run with `--allow-command`.
- If an enforced permission boundary is needed, run `host-shell` as a dedicated OS user with minimal permissions (see the [Operations Guide](operations.md)).

## Behavior as an MCP server

| Item | Description |
|---|---|
| Transport | stdio (newline-delimited JSON-RPC 2.0) |
| Protocol versions | `2024-11-05`, `2025-03-26`, `2025-06-18`, `2025-11-25`, `2026-07-28` (chosen according to the client's request) |
| Server name | `host-shell` |
| Capabilities | `tools`, `resources`, `logging` |
| Progress notifications | When a request has a `progressToken`, `exec_run` and `exec_sudo` send `notifications/progress` every second, and `save_to` of `net_request_http` sends them as data arrives |
| Cancellation | On `notifications/cancelled`, the request is interrupted and the running command is killed with its whole process tree |
| User confirmation | Elicitation is used for tools covered by `--confirm` ([User confirmation](#user-confirmation---confirm)) |
| Incoming message limit | 16 MiB per message |
| SDK | [modelcontextprotocol/go-sdk](https://github.com/modelcontextprotocol/go-sdk) v1.8.0 |

Table: MCP server specification

### Guidance for the agent

So that the agent can use it from MCP information alone, without external documents, guidance is provided in four layers.

| Layer | Content | When it is obtained |
|---|---|---|
| `instructions` in the initialize response | Target host name, OS and architecture, working directory, default shell and encodings, output limit and timeout, how to choose among the published tools, the fact that each call is an independent process, and the restrictions in effect | On connection |
| Tool `title` and `description` | What the tool does, when to use it, which tools go with it | When `tools/list` is fetched |
| `description` in the input and output schemas | Meaning and default of each argument, how to continue reading in parts | When `tools/list` is fetched |
| The [`help_get`](#help_get) tool and resources | Procedures by topic, argument examples, Windows notes, limits | When called as needed |

Table: Guidance for the agent

Guidance and error messages are written in English for agents, and include information to decide the next step when an error occurs (for example, the line numbers and a pointer to `replace_all` when `fs_edit` matches in several places). The content of `help_get` is also published as MCP resources (`host-shell://help/<topic>`, `text/markdown`).

## Common specification of all tools

### Result format

Tool results are returned as a JSON object in `structuredContent`, and the same JSON as a string in the text of `content`. The output schema of each tool is published in `outputSchema` of `tools/list`.  
As an exception, `help_get` returns only Markdown text in `content` and has no `outputSchema`.

### Errors

In the following cases, `isError` of the result is `true` and the text of `content` gives the reason.

- The arguments do not fit the schema (a required argument is missing, a type does not match, an undefined argument is given)
- The operation itself failed, for example because a file does not exist or permissions are insufficient
- The command could not be started (the executable does not exist, an unsupported shell was given, and so on)

When a started command exits with a non-zero exit code, the result is `isError: false`, so judge success by `exit_code` in the result.

### Path interpretation

Paths are interpreted on the target machine, and relative paths start from the working directory of `host-shell`. On Windows, both `/` and `\` can be used. Paths in results are absolute unless stated otherwise.

### Output limits

Every tool has a limit on output size or count. The default byte limit is 64 KiB (changeable with `--max-output`).

| Tool | Unit of the limit | Behavior when the limit is exceeded |
|---|---|---|
| `exec_run`, `exec_sudo` | Each of standard output and standard error | Keeps half at the head and half at the tail, inserts an omission marker, and sets `stdout_truncated` and so on to `true` |
| `job_read` | Bytes per read | Reading can continue from `next_offset` |
| `fs_read` | Bytes and lines per read | Returns `truncated: true` and the position for the next read (`next_offset` / `next_line`) |
| `proc_list` | The whole output | Keeps the head and tail and inserts an omission marker |
| `net_request_http` | Response body | Returns only the head and sets `body_truncated: true` (no limit with `save_to`) |
| `net_exchange` | Received data | Stops receiving when the limit is reached and sets `truncated: true` |
| `log_query` | The whole output | Drops the oldest log entries and sets `truncated: true` |
| `fs_list`, `search_*`, `fs_manifest` | The maximum count given in the arguments | Sets `truncated: true` |

Table: Output limits

The omission marker looks like this.

```text

... [host-shell: 123456 bytes omitted] ...

```

### Encodings

Text in tool input and output is always UTF-8. When the target's command output or files are not UTF-8, `host-shell` converts to and from UTF-8 (see [Encodings and line endings](#encodings-and-line-endings)).

### Tool annotations

Each tool carries MCP tool annotations as a guide for approval control on the agent side.

| Annotation | Tools |
|---|---|
| `readOnlyHint: true` | `help_get`, `job_read`, `job_list`, `fs_list`, `fs_stat`, `fs_read`, `search_grep`, `search_find`, `fs_manifest`, `sys_get_info`, `proc_list`, `net_check`, `net_list_listeners`, `net_list_interfaces`, `svc_list`, `log_query`, `disk_list`, `reg_read` |
| `destructiveHint: false`, `idempotentHint: true` | `fs_mkdir` |
| `destructiveHint: true` | `exec_run`, `job_start`, `job_write`, `job_stop`, `fs_edit`, `fs_write`, `fs_move`, `fs_copy`, `fs_remove`, `exec_sudo`, `net_request_http`, `net_exchange`, `proc_kill`, `svc_control`, `reg_write`, `reg_delete` |

Table: Tool annotations

Only `net_request_http`, `net_exchange`, and `net_check`, which can act on external networks, have `openWorldHint: true` (the others have `false`).

## Encodings and line endings

MCP communication is always in UTF-8, and `host-shell` absorbs the differences from the target's encodings (Shift_JIS, EUC-JP, UTF-16 and so on) and line endings on the target side.

### Default encodings of the target

Detection (`auto`) and `system` use one of two default encodings depending on the use.

| Kind | Use | Windows | Linux |
|---|---|---|---|
| Console | Command output and standard input | OEM code page (`GetOEMCP`) | The locale's encoding |
| File | Reading, writing and searching text files | ANSI code page (`GetACP`) | The locale's encoding |

Table: Default encodings of the target

On Japanese Windows, both are Shift_JIS (CP932) (UTF-8 when Windows' "Use Unicode UTF-8 for worldwide language support" is enabled).  
On Linux, the encoding name after `.` is taken from the first value set among the environment variables `LC_ALL`, `LC_CTYPE` and `LANG`, in that order (for example, `ja_JP.eucJP` is EUC-JP; `C` and `POSIX` without an encoding part, or no value, mean UTF-8).  
The actual values can be checked in `console_encoding` and `file_encoding` of [`sys_get_info`](#sys_get_info).

### Specifying encodings

Encoding arguments (`output_encoding`, `stdin_encoding`, `charset`) take the following values.

| Value | Meaning |
|---|---|
| `auto` | Detection (only for reading arguments; the default when omitted) |
| `utf-8` | UTF-8 (no conversion) |
| `system` | The target's default encoding |
| Encoding name | `shift_jis`, `euc-jp`, `iso-2022-jp`, `utf-16le`, `utf-16be`, `gbk`, `gb18030`, `big5`, `euc-kr`, `windows-1252` and so on |
| Code page number | `cp932`, `932`, `cp437`, `cp65001` and so on |

Table: Specifying encodings

Case is ignored. In addition to the names and aliases of the [WHATWG Encoding Standard](https://encoding.spec.whatwg.org/#names-and-labels) (`sjis`, `windows-31j`, `latin1` and so on), common names such as `eucjp` and `ujis` are accepted (`shift_jis` is treated as Windows-31J / CP932).

### Detection

Detection when reading proceeds in this order.

1. If there is a BOM (UTF-8, UTF-16LE, UTF-16BE) at the start, that encoding is used, and the BOM itself is removed from the result.
2. Without a BOM, if the leading bytes have the characteristics of UTF-16, it is detected as UTF-16LE or UTF-16BE.
3. If the bytes are valid UTF-8, it is UTF-8.
4. Otherwise, it is converted from the target's default encoding.

For command output (`exec_run`, `job_start`, `exec_sudo`), `search_grep`, and line-range reads of `fs_read`, steps 3 and 4 are decided per line, so output that mixes UTF-8 and Shift_JIS is also converted correctly (byte-range reads of `fs_read` decide on the whole range read).

BOM-less UTF-16 detection inspects up to the first 4096 bytes (for command output, at the time the first 64 bytes arrive or at exit) in 2-byte units, and treats the data as UTF-16 when all of the following hold.

- Units where both bytes are 0 (`U+0000`) are 1% or less
- Units where one byte is 0 are 5% or more (the side where the 0 appears decides LE or BE)
- Units where the other byte is 0 are 2% or less (up to one unit is allowed, for short text)
- All surrogate pairs are correct combinations

UTF-16 text that contains ASCII characters or newlines meets these conditions, and binary or compressed data is excluded. Short UTF-16 text of only Japanese characters without newlines or ASCII characters may not be detected, so give `charset` explicitly.

The encoding used for conversion is returned in fields such as `stdout_encoding` and `charset`. Invalid bytes that no encoding can interpret are replaced with the replacement character (`U+FFFD`).

### Not splitting characters

Tools that read in parts do not split multibyte characters at chunk boundaries. An incomplete byte sequence at the end is left for the next read position (`next_offset`), and when the output of `exec_run` and so on is cut, the split characters at the boundary are removed before the omission marker is inserted.

### Line endings

Reading does not convert line endings; CRLF and LF are returned as they are. `fs_read` returns the kind of line endings in the content in `newline` (`lf`, `crlf`, `cr`, `mixed`).  
Giving `lf` or `crlf` in `newline` of `fs_write` unifies the line endings when writing. Passing the `charset`, `bom` and `newline` that `fs_read` returned to `fs_write` keeps the original file format.

### What is converted

| Tool | Argument | Default |
|---|---|---|
| `exec_run`, `job_start`, `exec_sudo` | `output_encoding` (standard output and standard error) | `auto` (console) |
| | `stdin_encoding` (standard input) | `utf-8` (for PowerShell on Windows, see [Shells](#shells)) |
| `job_write` | (inherits `stdin_encoding` of `job_start`) | Same as `job_start` |
| `proc_list` | None | `auto` (console) |
| `fs_read` (`text`) | `charset` | `auto` (file) |
| `fs_edit` | `charset` | `auto` (file; written back with the encoding, BOM and line endings found when reading) |
| `fs_write` (`text`) | `charset`, `newline`, `bom` | `utf-8`, `keep` (no conversion), `false` (no BOM) |
| `search_grep` | `charset` | `auto` (file) |

Table: What encoding conversion covers

Reading and writing in `base64` mode convert neither encodings nor line endings.

## Tool list

| Category | Tool | Description |
|---|---|---|
| Usage guidance | [`help_get`](#help_get) | Returns usage guidance for agents |
| Running commands | [`exec_run`](#exec_run) | Runs a command, waits for it to end and returns the result |
| Long-running execution | [`job_start`](#job_start) | Starts a command as a background job |
| | [`job_read`](#job_read) | Gets the output and status of a job |
| | [`job_write`](#job_write) | Sends data to the standard input of a job |
| | [`job_stop`](#job_stop) | Sends a signal to a job or kills it |
| | [`job_list`](#job_list) | Lists the jobs |
| File operations | [`fs_list`](#fs_list) | Lists the entries of a directory |
| | [`fs_stat`](#fs_stat) | Gets the attributes of a file or directory |
| | [`fs_read`](#fs_read) | Reads a file |
| | [`fs_edit`](#fs_edit) | Replaces strings in a file |
| | [`fs_write`](#fs_write) | Writes to a file |
| | [`fs_mkdir`](#fs_mkdir) | Creates a directory |
| | [`fs_move`](#fs_move) | Moves or renames files and directories |
| | [`fs_copy`](#fs_copy) | Copies files and directories |
| | [`fs_remove`](#fs_remove) | Deletes files and directories |
| Search | [`search_grep`](#search_grep) | Searches file contents by regular expression or string |
| | [`search_find`](#search_find) | Searches file names and paths by glob |
| Checking differences | [`fs_manifest`](#fs_manifest) | Gets a list of SHA-256 hashes of files |
| Privileged execution | [`exec_sudo`](#exec_sudo) | Runs a command with `sudo` (Linux only) |
| Environment information | [`sys_get_info`](#sys_get_info) | Gets the target's environment, the published tool groups and the restrictions in effect |
| Network | [`net_request_http`](#net_request_http) | Sends HTTP / HTTPS requests from the target |
| | [`net_exchange`](#net_exchange) | Sends and receives over TCP / TLS / Unix sockets from the target |
| | [`net_check`](#net_check) | Checks name resolution, TCP connections and TLS certificates in parallel |
| | [`net_list_listeners`](#net_list_listeners) | Lists listening ports and the processes that own them |
| | [`net_list_interfaces`](#net_list_interfaces) | Lists network interfaces |
| Administration | [`proc_list`](#proc_list) | Lists processes |
| | [`proc_kill`](#proc_kill) | Sends a signal to a process or ends it |
| | [`svc_list`](#svc_list) | Lists services |
| | [`svc_control`](#svc_control) | Gets the status of, starts, stops or restarts a service |
| | [`log_query`](#log_query) | Searches and gets the system log (journald / Event Log) |
| | [`disk_list`](#disk_list) | Gets disk usage |
| Windows registry | [`reg_read`](#reg_read) | Reads registry keys and values (Windows only) |
| | [`reg_write`](#reg_write) | Writes registry keys and values (Windows only) |
| | [`reg_delete`](#reg_delete) | Deletes registry keys and values (Windows only) |
| Transfer with the local machine | [`transfer_push`](#transfer_push) | Transfers files from the local machine to the target (only through `bridge`) |
| | [`transfer_pull`](#transfer_pull) | Transfers files from the target to the local machine (only through `bridge`) |

Table: Tool list

Only the tools published by [`--tools`](#choosing-tools---tools) and [`--read-only`](#read-only---read-only) are listed. In the following tables, arguments with an empty "Required" column are optional.

## Usage guidance

### help_get

Returns usage guidance for agents as Markdown text.

| Argument | Type | Required | Description |
|---|---|---|---|
| `topic` | string | | Topic name. `overview` when omitted; `all` returns all topics concatenated |

Table: Arguments of help

| topic | Content |
|---|---|
| `overview` | How it works, a quick tool reference, recommended procedures (with the target summary at the top) |
| `files` | `fs_read`, `fs_write`, the file operation tools, `fs_manifest` |
| `edit` | How to use `fs_edit` and its replacement rules |
| `search` | Glob syntax, the output modes of `search_grep`, `search_find` |
| `commands` | `exec_run`, `exec_sudo`, when to use which tool |
| `jobs` | Background jobs, wait conditions (`until`, `idle_ms`), sending input, pseudo-terminals, signals |
| `network` | How to use the `net_*` tools, with argument examples |
| `admin` | Processes, services, the system log, disks, registry operations |
| `transfer` | `transfer_push`, `transfer_pull`, and transfer in parts without `bridge` |
| `encoding` | Handling of encodings and line endings |
| `windows` | Notes for Windows targets |
| `policy` | Restriction options and their effect on the agent's behavior (with the target summary at the top) |
| `limits` | Output limits, timeouts, error handling, the audit log (with the target summary at the top) |

Table: Topics of help

The target summary includes the host name, OS and architecture, user, working directory, default shell, default encodings, output limit, timeout, the table of published tool groups, and the table of restrictions in effect. An unknown `topic` returns an error with the list of valid topics.  
Each topic can also be read as the MCP resource `host-shell://help/<topic>`.

## Running commands

### exec_run

Runs a command, waits for it to end and returns the result. Give either `command`, a shell command line, or `argv`, started directly without a shell.

| Argument | Type | Required | Description |
|---|---|---|---|
| `command` | string | * | Command line passed to the shell |
| `argv` | string[] | * | Program and arguments started without a shell (the first element is the program) |
| `shell` | string | | Shell that interprets `command`. The OS default shell when omitted (see [Shells](#shells)) |
| `cwd` | string | | Working directory. The working directory of `host-shell` when omitted |
| `env` | object | | Environment variables to add or override (a map of keys and values) |
| `stdin` | string | | Text passed to standard input (closed after writing; empty input when omitted) |
| `timeout_sec` | integer | | Timeout in seconds. `--timeout` when omitted; the upper limit is `--max-timeout` |
| `output_encoding` | string | | Encoding of standard output and standard error. `auto` when omitted ([Specifying encodings](#specifying-encodings)) |
| `stdin_encoding` | string | | Encoding `stdin` is converted to. `utf-8` when omitted. For PowerShell on Windows, a value other than `utf-8` is passed on the assumption that PowerShell reads that code page ([Shells](#shells)). `auto` cannot be given |

Table: Arguments of exec_run (* exactly one of `command` and `argv` is required)

Each call starts a new process, so a previous `cd` or environment change is not carried over (use `cwd` and `env`).

| Output | Type | Description |
|---|---|---|
| `exit_code` | integer | Exit code (`-1` when ended by a signal and so on) |
| `stdout` | string | Standard output |
| `stderr` | string | Standard error |
| `stdout_truncated` | boolean | `true` when standard output was cut |
| `stderr_truncated` | boolean | `true` when standard error was cut |
| `stdout_encoding` | string | The original encoding when standard output was converted from something other than UTF-8 |
| `stderr_encoding` | string | The original encoding when standard error was converted from something other than UTF-8 |
| `timed_out` | boolean | `true` when killed because of the timeout |
| `cancelled` | boolean | `true` when killed because the client cancelled the request |
| `duration_ms` | integer | Run time (milliseconds) |

Table: Output of exec_run

On timeout, the command is killed with its descendant processes, and the output so far is returned with `timed_out: true`.  
When the request has a `progressToken`, `notifications/progress` (elapsed seconds, output bytes, last line) is sent every second, and on `notifications/cancelled` the process tree is killed at once.

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

Run commands that take a long time, such as builds and log monitoring, as background jobs.

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

- Job IDs are numbered per `host-shell` process, as `job-1`, `job-2` and so on.
- There are two kinds of input and output: pipes (the default) and a pseudo-terminal (`pty: true`).
- Standard output and standard error are merged into one stream and kept after conversion to UTF-8 (`offset` and `output_bytes` are byte counts of the converted UTF-8).
- The output ring buffer keeps at least the most recent four times `--max-output` (256 KiB by default) and discards older output beyond that.
- Up to the 50 most recent finished jobs are kept; older ones are discarded.
- When the `host-shell` process exits (on SSH disconnection), all running jobs stop.

### job_start

Starts a command in the background and returns the job status at once. The arguments are those of [`exec_run`](#exec_run) plus the following (when `timeout_sec` is omitted there is no limit, and when it is given it is not capped by `--max-timeout`).

| Argument | Type | Required | Description |
|---|---|---|---|
| `stdin_open` | boolean | | Keep standard input open so that input can be sent later with [`job_write`](#job_write) (`stdin`, if given, is sent first) |
| `pty` | boolean | | Start with a pseudo-terminal |
| `cols` | integer | | Number of terminal columns for `pty` (`120` when omitted) |
| `rows` | integer | | Number of terminal rows for `pty` (`40` when omitted) |

Table: Arguments specific to job_start

#### Pseudo-terminal (pty)

With `pty: true`, the command is started with a pseudo-terminal. Use it for programs that need a terminal (`sudo`, `ssh`, REPLs, `top`, programs that buffer output when connected to a pipe, and so on).

| Item | Linux | Windows |
|---|---|---|
| Implementation | pty (creates a new session and makes it the controlling terminal) | ConPTY (Windows 10 1809 / Windows Server 2019 or later) |
| Environment variables | Sets `TERM` to `xterm-256color` if it is not set | No change |
| PowerShell | | Started without `-NonInteractive` (`Read-Host` and so on can be used) |

Table: Pseudo-terminal

- Standard output and standard error are merged as terminal output, and input is always open.
- If descendant processes still hold the terminal after the command ends, the terminal is closed after waiting up to 2 seconds.
- The output is also fed to a virtual terminal emulator (`github.com/charmbracelet/x/vt`), and `screen: true` of `job_read` gets the current screen. The virtual terminal also answers terminal queries such as cursor position requests.

The output schema is [Job status](#job-status).

### job_read

Reads the output of a job from the given offset and returns it with the current job status.

| Argument | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Job ID |
| `offset` | integer | | Read start position (cumulative bytes from the start, the previous `next_offset` when continuing, or a negative value to count bytes from the end) |
| `max_bytes` | integer | | Maximum bytes to read (the default and upper limit are `--max-output`) |
| `wait_sec` | integer | | Maximum seconds to wait (up to 300 seconds; returns at once when the job ends) |
| `until` | string | | Wait until the output after `offset` matches this regular expression |
| `idle_ms` | integer | | After output arrives, wait until no new output comes for this many milliseconds |
| `raw` | boolean | | For `pty` jobs, return the output as it is without removing terminal control sequences |
| `screen` | boolean | | For `pty` jobs, return the current terminal screen instead of the stream output |

Table: Arguments of job_read

With only `wait_sec`, it returns as soon as new output arrives after `offset`.  
The output of `pty` jobs is cleaned up as follows before it is returned (`until` is also matched against the cleaned text); only with `raw: true` is it returned unprocessed.

- ANSI escape sequences for colors, cursor movement, window titles and so on are removed
- CRLF is unified to LF, and for overwrites by CR in the middle of a line (progress bars and so on), only the final content is kept
- Backspace erases the previous character, and control characters other than tab and newline are removed
- For an escape sequence cut off at the end of a chunk, output up to just before it is returned and `next_offset` is set to the start of that sequence

| Output | Type | Description |
|---|---|---|
| `status` | object | [Job status](#job-status) |
| `matched` | boolean | `true` when the regular expression of `until` matched |
| `output` | string | The output read |
| `next_offset` | integer | Position to start the next read |
| `more` | boolean | `true` when unread output remains after `next_offset` |
| `dropped` | integer | Bytes before `offset` discarded from the ring buffer |
| `cursor_row`, `cursor_col` | integer | Cursor row and column with `screen: true` (0-based) |

Table: Output of job_read

#### Screen state (screen)

With `screen: true`, for full-screen programs such as `top`, `vi` and `less`, the current terminal screen (`rows` lines, trailing spaces removed) is returned in `output` separated by newlines, and the cursor position in `cursor_row` and `cursor_col`. `offset` and `max_bytes` are ignored, and the screen and the cumulative byte count (`next_offset`) at the moment the wait of `wait_sec`, `until` and `idle_ms` completes are returned (an error for jobs other than `pty` jobs).

```json
{"id": "job-3", "screen": true, "wait_sec": 5, "idle_ms": 500}
```

### job_write

Sends input to a job started with `stdin_open` or `pty`. It can also close the input and change the terminal size of a PTY.

| Argument | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Job ID |
| `input` | string | | Text to send (no newline is added, so include a trailing `\n` to send a line) |
| `close` | boolean | | Close standard input after sending `input` (in `pty` jobs, sends Ctrl-D on Linux and Ctrl-Z + Enter on Windows) |
| `cols`, `rows` | integer | | Change the terminal size of a `pty` job (both are needed) |

Table: Arguments of job_write (one of `input`, `close`, or `cols` and `rows` is required)

| Output | Type | Description |
|---|---|---|
| `status` | object | [Job status](#job-status) |
| `bytes_written` | integer | Bytes written (after encoding conversion) |

Table: Output of job_write

`input` is converted to the `stdin_encoding` of `job_start` before sending. If the child process does not read and the write does not complete within 10 seconds, an error is returned (the rest continues to be sent in the background).  
In `pty` jobs, control keys are sent as characters (Ctrl-C is `\u0003`, Ctrl-D is `\u0004`, Esc is `\u001b`, Up arrow is `\u001b[A`).

```json
{"id": "job-3", "input": "y\n"}
```

```json
{"id": "job-4", "input": "\u0003"}
```

### job_stop

Sends a signal to a job or kills it with its whole process tree, waits for it to end and returns the status.

| Argument | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Job ID |
| `signal` | string | | `kill` (default), or on Linux `interrupt`, `terminate`, `hangup`, `quit`, `stop`, `continue`, `usr1`, `usr2` (`interrupt` is also available on Windows for `pty` jobs) |
| `wait_sec` | integer | | Seconds to wait for the end (`5` when omitted) |

Table: Arguments of job_stop

| signal | Behavior |
|---|---|
| `kill` | Kills the job with its descendant processes ([How processes are ended](#how-processes-are-ended)) |
| `interrupt` (`pty` jobs) | Sends Ctrl-C to the terminal (both Linux and Windows) |
| Others (Linux) | Sends the signal to the job's process group |

Table: Signals of job_stop

Signals other than `kill` may not end the process, so check `status.running` in the result. `kill` on a finished job is not an error, but other signals are.

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
| `command` | string | The command run (for `argv`, the arguments joined with spaces) |
| `pid` | integer | Process ID |
| `pty` | boolean | `true` for a pseudo-terminal job |
| `running` | boolean | `true` while running |
| `stdin_open` | boolean | `true` when standard input is open and `job_write` can be used |
| `exit_code` | integer | Exit code (only after it ended) |
| `signal` | string | The signal sent by the last `job_stop` (except `kill`) |
| `output_encoding` | string | The original encoding when the output was converted from something other than UTF-8 |
| `error` | string | An error that occurred while waiting for the process (usually omitted) |
| `started_at` | string | Start time (RFC 3339) |
| `ended_at` | string | End time (RFC 3339; only after it ended) |
| `output_bytes` | integer | Cumulative output bytes so far |

Table: Job status

## File operations

### fs_list

Returns the entries of a directory in name order. For symbolic links, it returns information on the link itself and does not follow it.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | | Directory path (the working directory when omitted) |
| `max_entries` | integer | | Maximum number of entries (`1000` when omitted) |

Table: Arguments of fs_list

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path of the directory |
| `entries` | object[] | Array of entries (`name`, `type` [`file`/`dir`/`symlink`/`other`], `size`, `mode`, `mod_time` [RFC 3339]) |
| `truncated` | boolean | `true` when cut off at `max_entries` |

Table: Output of fs_list

### fs_stat

Returns the attributes of a file or directory. For a symbolic link, it returns the attributes of the link itself and its target (`link_target`).

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | Target path |

Table: Arguments of fs_stat

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `entry` | object | Attributes (same format as the elements of [`fs_list`](#fs_list)) |
| `link_target` | string | Target of a symbolic link |

Table: Output of fs_stat

### fs_read

Reads a file. Depending on the arguments, it works in one of three modes.

| Mode | How to select it | Use |
|---|---|---|
| Line range (text) | Give `line_start` (`encoding: "text"`) | Reading a specific range of lines of source code or logs |
| Byte range (text) | Omit `line_start` (`encoding: "text"`) | Reading in order from the start or a given offset |
| Byte range (base64) | Give `encoding: "base64"` | Reading binary files as they are |

Table: How fs_read reads

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | File path |
| `encoding` | string | | `text` (default) or `base64` |
| `charset` | string | | Encoding in `text` mode (`auto` when omitted; [Specifying encodings](#specifying-encodings)) |
| `line_start` | integer | | First line to read (1-based; when given, `offset` and `length` are ignored) |
| `line_count` | integer | | Number of lines to read (`2000` when omitted) |
| `line_numbers` | boolean | | Prefix each line with a 6-digit right-aligned line number and a tab (only with `line_start`) |
| `offset` | integer | | Byte position to start reading (`0` when omitted) |
| `length` | integer | | Maximum bytes to read (up to the limit when omitted) |

Table: Arguments of fs_read

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `size` | integer | File size (bytes) |
| `encoding` | string | `text` or `base64` |
| `charset` | string | Encoding used in `text` mode (can be passed to `fs_write` to keep the format) |
| `bom` | boolean | `true` when there is a BOM at the start (the BOM itself is not included in `content`) |
| `newline` | string | Kind of line endings in `content` (`lf`, `crlf`, `cr`, `mixed`) |
| `content` | string | The content read |
| `eof` | boolean | `true` when the end of the file was reached |
| `next_offset` | integer | Position to start the next read in byte range mode |
| `next_line` | integer | Line to start the next read in line range mode (only when `eof: false`) |
| `truncated` | boolean | `true` when the output limit was reached in the middle of the requested range |

Table: Output of fs_read

- In `text` mode, it reads up to `--max-output` measured after conversion to UTF-8. In line range mode, it returns the lines up to just before the limit; a single line that exceeds the limit is cut and reading moves on to the next line.
- In `base64` mode, it reads up to 3/4 of `--max-output` (48 KiB by default) so that the encoded size fits in `--max-output`.
- The line number prefix added by `line_numbers: true` (`     12\t`) is not part of the file content, so do not pass it to `fs_edit` or `fs_write`.

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

Writes to a file. To change part of an existing file, use [`fs_edit`](#fs_edit) instead of `fs_write`, which sends the whole file again.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | File path |
| `content` | string | Yes | Content to write |
| `encoding` | string | | `text` (default) or `base64` |
| `charset` | string | | Encoding in `text` mode (`utf-8` when omitted; `auto` cannot be given) |
| `newline` | string | | Line ending conversion in `text` mode: `keep` (default, no conversion), `lf`, `crlf` |
| `bom` | boolean | | Add a BOM at the start (only for `utf-8`, `utf-16le` and `utf-16be`, and only when writing at the start of the file) |
| `mode` | string | | `overwrite` (default), `append`, `create` |
| `offset` | integer | | Byte position to start writing (overwrites only the given range without truncating the file) |
| `make_dirs` | boolean | | Create missing parent directories with mode `0755` |
| `perm` | string | | Octal permissions for a new file (for example `0600`, `0644` by default, ignored on Windows) |
| `atomic` | boolean | | Write to a temporary file and replace by renaming (only with `mode: "overwrite"` and no `offset`) |

Table: Arguments of fs_write

| mode | Without `offset` | With `offset` |
|---|---|---|
| `overwrite` (default) | Creates the file if missing; otherwise truncates it and rewrites the whole | Creates the file if missing; overwrites only the written length from `offset` without truncating |
| `append` | Creates the file if missing; otherwise appends to the end | Cannot be given (error) |
| `create` | Creates the file if missing; error if it exists | Creates the file if missing and writes from `offset` (error if it exists) |

Table: mode and offset of fs_write

Unlike omitting it, giving `0` as `offset` does not truncate the file. An `offset` beyond the end of the file fills the gap from the end to `offset` with zero bytes.  
In `text` mode, line ending conversion, encoding conversion and BOM addition are done in that order. If the content has characters that `charset` cannot represent, it is an error that shows the character and its position, and nothing is written to the file.

#### Atomic replacement (atomic)

The default `overwrite` rewrites the existing file in place, which keeps the owner, permissions and links, but incomplete content can be visible during writing or after an interruption.  
With `atomic: true` (`fs_write` and `fs_edit`), the file is replaced atomically as follows.

1. If the target is a symbolic link, the real file it points to becomes the target.
2. A temporary file (`.<name>.host-shell-tmp-<random>`) is created in the same directory, written and synced with `fsync`.
3. The mode, owner and group of the existing file are carried over (if they cannot be, for example because the owner differs and the user is not root, the temporary file is removed and an error is returned).
4. The target file is replaced by renaming (on failure, the temporary file is removed).

ACLs, extended attributes and hard links are not carried over. On Windows, a file other processes have open may not be replaceable.

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

Replaces exactly matching strings in an existing file. It detects the file's encoding, BOM and line endings, and writes back in the same format.  
Give either a single replacement (`old_string` and `new_string`) or several replacements (`edits`).

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | File path |
| `old_string` | string | * | String to replace (must match exactly, including whitespace, indentation and newlines) |
| `new_string` | string | | Replacement string (an empty string deletes) |
| `replace_all` | boolean | | Replace every match (when omitted, it is an error unless there is exactly one match) |
| `edits` | object[] | * | Array of replacements (`old_string`, `new_string`, `replace_all`), applied in order, each to the result of the previous one |
| `charset` | string | | Encoding of the file (`auto` when omitted) |
| `dry_run` | boolean | | Only check the replacement and build the excerpt; do not write to the file |
| `atomic` | boolean | | Replace atomically through a temporary file (same as [`fs_write`](#atomic-replacement-atomic)) |

Table: Arguments of fs_edit (* exactly one of `old_string` and `edits` is required)

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `replacements` | integer | Total number of places replaced |
| `lines` | integer[] | Starting line number of each replaced place after the replacement (up to 50) |
| `snippet` | string | Excerpt with line numbers of the replaced places and 2 lines around each (up to 80 lines) |
| `charset` | string | Encoding used for reading and writing |
| `bom` | boolean | Whether there is a BOM (the original state is kept) |
| `newline` | string | Kind of line endings after the replacement |
| `size` | integer | File size after the replacement (bytes) |
| `sha256` | string | SHA-256 of the file after the replacement |
| `dry_run` | boolean | `true` when `dry_run` was given |

Table: Output of fs_edit

- If any replacement fails, the file is not changed at all.
- If `old_string` is not found but a line matches when leading and trailing whitespace is ignored, the error includes that line number. If it matches in several places and `replace_all` is not given, the matching line numbers (up to 10) are returned.
- In CRLF files, LF in `old_string` and `new_string` is matched and written as CRLF.
- The encoding is detected as a single one for the whole file; if re-encoding the decoded text does not reproduce the original bytes (mixed encodings and so on), it is an error to prevent corruption.
- The limit for the target file is 32 MiB.

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

Creates a directory (parent directories are created recursively with mode `0755`). If the directory already exists, it is not an error and `created: false` is returned; if a file of the same name exists, it is an error.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | Path of the directory to create |

Table: Arguments of fs_mkdir

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `created` | boolean | `true` when newly created, `false` when it already existed |

Table: Output of fs_mkdir

### fs_move

Moves or renames a file or directory. Across file systems (drives on Windows), it copies and then deletes the source (if the copy fails partway, the source is left).

| Argument | Type | Required | Description |
|---|---|---|---|
| `source` | string | Yes | Source path |
| `destination` | string | Yes | Destination path (the full path after the move, not the parent directory) |
| `overwrite` | boolean | | Overwrite an existing file at the destination (an error if the destination is a directory) |
| `make_dirs` | boolean | | Create missing parent directories of the destination |

Table: Arguments of fs_move

| Output | Type | Description |
|---|---|---|
| `source` | string | Absolute path of the source |
| `destination` | string | Absolute path of the destination |
| `copied` | boolean | `true` when moved by copy and delete across file systems |

Table: Output of fs_move

### fs_copy

Copies a file. With `recursive: true`, it copies a directory recursively.

| Argument | Type | Required | Description |
|---|---|---|---|
| `source` | string | Yes | Source path |
| `destination` | string | Yes | Destination path (the full path after the copy) |
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

File permissions and modification times are carried over, and symbolic links are recreated as links pointing to the same target (special files other than regular files, directories and symbolic links are skipped). A destination inside the source directory is an error.

### fs_remove

Deletes files, symbolic links and directories. Deleting the root of a file system (`/`, `C:\` and so on) or the home directory itself of the user running `host-shell` is refused.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | Yes | Path to delete (for a symbolic link, the link itself is deleted) |
| `recursive` | boolean | | Required to delete a non-empty directory |
| `missing_ok` | boolean | | Do not treat a missing target as an error |

Table: Arguments of fs_remove

| Output | Type | Description |
|---|---|---|
| `path` | string | Absolute path |
| `removed` | boolean | `true` when deleted (`false` when it was missing with `missing_ok`) |
| `type` | string | Kind of what was deleted (`file`, `dir`, `symlink`, `other`) |

Table: Output of fs_remove

## Search

`search_grep`, `search_find` and `fs_manifest` walk the given directory recursively in name order.

- Symbolic links are not followed.
- They do not descend into the directory names given in `exclude_dirs` (`[".git", ".hg", ".svn"]` when omitted; nothing is excluded with an empty array `[]`).
- Entries that cannot be read, for example because of insufficient permissions, are skipped, and their number is returned in `skipped`.
- Paths in results are relative to the `path` argument in the same form (if `path` is relative, results are relative too).

### glob

The glob syntax used in `include` of `search_grep` and `fs_manifest` and in `pattern` of `search_find` is as follows (on Windows, case is ignored and `\` is also a separator).

| Format | Meaning |
|---|---|
| Without `/` (`*.go`) | Matched against the base name (the last element of the path) |
| With `/` (`src/**/*.ts`) | Matched against the path relative to `path` (`/`-separated; a leading `./` is ignored) |
| `*`, `?` | Any string, or any one character, within one path element |
| `**` | Zero or more directory levels (`**/test/*.py`) |
| `[abc]`, `[a-z]` | Character class |
| `{a,b}` | One of the strings (`*.{yml,yaml}`, no nesting) |

Table: Glob syntax

### search_grep

Searches file contents by regular expression or literal string.

| Argument | Type | Required | Description |
|---|---|---|---|
| `pattern` | string | Yes | Regular expression ([RE2 syntax](https://github.com/google/re2/wiki/Syntax); no backreferences or lookahead) |
| `path` | string | | Directory or file to search (the working directory when omitted) |
| `include` | string | | [Glob](#glob) of the files to search (for example `*.go`, `src/**/*.ts`) |
| `exclude_dirs` | string[] | | Directory names to exclude from the walk |
| `gitignore` | boolean | | Exclude what `.gitignore` ignores (`true` when omitted; [Handling of .gitignore](#handling-of-gitignore)) |
| `ignore_case` | boolean | | Ignore case |
| `literal` | boolean | | Search for `pattern` as a fixed string without metacharacters |
| `output_mode` | string | | `content` (default; matching lines and the lines around them), `files` (only the paths of matching files), `count` (number of matching lines per file) |
| `context` | integer | | Number of lines to include before and after matching lines in `content` mode (`grep -C`) |
| `before`, `after` | integer | | Number of lines to include before or after matching lines (override `context`, `grep -B` / `-A`) |
| `max_matches` | integer | | Maximum number of results (matching lines for `content`, files for `files` and `count`, `200` when omitted) |
| `max_file_bytes` | integer | | Skip files larger than this (10 MiB when omitted) |
| `charset` | string | | Encoding of the files (`auto` when omitted) |
| `multiline` | boolean | | Match patterns that span several lines against the whole file |

Table: Arguments of search_grep

| Output | Type | Description |
|---|---|---|
| `matches` | object[] | Array of matches in `content` mode (`path`, `line`, `end_line` [with `multiline`], `text` [up to 500 bytes], `before`, `after`, `charset`) |
| `files` | object[] | Array of matching files in `files` and `count` modes (`path`, `count`) |
| `files_scanned` | integer | Number of files checked |
| `files_matched` | integer | Number of files with a match |
| `skipped` | integer | Number of entries that could not be read |
| `skipped_large` | integer | Number of files skipped because they exceeded the size limit |
| `truncated` | boolean | `true` when cut off at `max_matches` |

Table: Output of search_grep

Skipped are: anything other than regular files, binary files with NUL in the first 8000 bytes (except with a UTF-16 BOM or UTF-16 given in `charset`), files over `max_file_bytes`, and, with `gitignore: true`, ignored files. In a file with a line longer than 1 MiB, the search stops just before that line.

#### Searching across lines (multiline)

With `multiline: true`, the whole file is converted to UTF-8 and matched in RE2's `(?sm)` mode (`.` also matches a newline; `^` and `$` match at the start and end of each line). Each match returns its first line (`line`), last line (`end_line`) and matched text (`text`, up to 500 bytes) (empty matches are excluded; files over 32 MiB are counted in `skipped_large`).

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

By default (`gitignore: true`), `search_grep` and `search_find` evaluate the following rule files in order and skip what they ignore (the global `core.excludesFile` is not read).

1. The repository's `.git/info/exclude` (when the search starts inside a repository)
2. The `.gitignore` of each level from the repository root (the directory with `.git`) down to where the search starts
3. The `.gitignore` of each directory entered during the walk

Rules of deeper levels and later lines in the same file take precedence (negation with `!pattern`, directory-only rules with `pattern/`, and `**` are supported; ignored directories are not entered, so files inside them cannot be brought back with `!`).

### search_find

Searches for entries whose file name or relative path matches a [glob](#glob).

| Argument | Type | Required | Description |
|---|---|---|---|
| `pattern` | string | | Glob pattern (all entries when omitted) |
| `path` | string | | Directory to start the search from (the working directory when omitted) |
| `type` | string | | `file`, `dir`, or omitted (both) |
| `exclude_dirs` | string[] | | Directory names to exclude from the walk |
| `gitignore` | boolean | | Exclude what `.gitignore` ignores (`true` when omitted) |
| `sort` | string | | `path` (default, walk order) or `mtime` (most recently modified first; up to 100,000 entries are collected and sorted) |
| `max_results` | integer | | Maximum number of results (`1000` when omitted) |

Table: Arguments of search_find

| Output | Type | Description |
|---|---|---|
| `paths` | string[] | Array of matching paths (`path` itself is not included) |
| `skipped` | integer | Number of entries that could not be read |
| `truncated` | boolean | `true` when cut off at `max_results` (or the 100,000 limit when sorting by `mtime`) |

Table: Output of search_find

## Checking differences

### fs_manifest

Returns a list of the relative path, size, permissions and SHA-256 hash of the regular files under the given directory (or of a single file). Use it to compare hashes with a local source tree and find only the files that differ.

| Argument | Type | Required | Description |
|---|---|---|---|
| `path` | string | | Base directory or a single file (the working directory when omitted) |
| `include` | string | | [Glob](#glob) of the files to include |
| `exclude_dirs` | string[] | | Directory names to exclude from the walk (`[".git", ".hg", ".svn"]` when omitted) |
| `max_files` | integer | | Maximum number of files (`2000` when omitted) |

Table: Arguments of fs_manifest

| Output | Type | Description |
|---|---|---|
| `root` | string | Absolute path of the base directory |
| `files` | object[] | Array of file information (`path` [always `/`-separated], `size`, `mode` [octal string], `sha256` [lowercase hexadecimal]) |
| `skipped` | integer | Number of entries that could not be read |
| `truncated` | boolean | `true` when cut off at `max_files` |

Table: Output of fs_manifest

## Privileged execution

### exec_sudo

Runs a command through `sudo` on a Linux target (not published on Windows). The output schema is the same as [`exec_run`](#exec_run).

| Argument | Type | Required | Description |
|---|---|---|---|
| `command` | string | Yes | Command line to run |
| `user` | string | | User to run as (`root` when omitted) |
| `cwd` | string | | Working directory |
| `stdin` | string | | Text passed to the command's standard input |
| `password` | string | | `sudo` password (passed through askpass only when `sudo` asks for it) |
| `timeout_sec` | integer | | Timeout in seconds (`--timeout` when omitted; the upper limit is `--max-timeout`) |
| `output_encoding`, `stdin_encoding` | string | | Encodings of input and output (same as [`exec_run`](#exec_run)) |

Table: Arguments of exec_sudo

Without `password`, it runs `sudo -n [-u <user>] -- sh -c '<command>'`. If the configuration needs a password, it ends with an error and the message is returned in `stderr`. Where only specific commands are allowed with `NOPASSWD`, call `sudo -n` directly through `argv` of `exec_run` instead of `exec_sudo` (see the [Operations Guide](operations.md#configuring-sudo)).

```json
{"argv": ["sudo", "-n", "/usr/bin/systemctl", "restart", "nginx"]}
```

#### Passing a password

With `password`, it runs `sudo -A [-u <user>] -- sh -c '<command>'` with `host-shell` itself as the `SUDO_ASKPASS` helper.

1. For each call, it creates a temporary directory with mode `0700` and a named pipe (FIFO) with mode `0600`, and writes the password up to 3 times.
2. It starts `sudo` with the path of `host-shell` in `SUDO_ASKPASS` and the path of the FIFO in `HOST_SHELL_ASKPASS` (the environment variables themselves do not contain the password).
3. Only when `sudo` asks for the password does the helper start, read one line from the FIFO and return it; afterwards the FIFO and the temporary directory are removed.

This way, the password never gets into the command's standard input, even with `NOPASSWD` or with cached authentication. The audit log records `password: "<redacted>"`, and if the password string appears in the output (`stdout`, `stderr`), it is replaced with `[REDACTED]` whether or not `--redact` is given.

## Environment information

### sys_get_info

Returns the target's environment, the published tool groups and the policy restrictions in effect (no arguments).

| Output | Type | Description |
|---|---|---|
| `os`, `arch` | string | OS (`linux` / `windows`) and architecture (`amd64` / `arm64`) |
| `hostname` | string | Host name |
| `user`, `uid` | string | User name (`DOMAIN\user` on Windows) and UID (SID on Windows) |
| `home`, `cwd` | string | Home directory and the working directory of `host-shell` |
| `path_separator` | string | Path separator (`/` or `\`) |
| `default_shell` | string | Default shell of `exec_run` |
| `console_encoding`, `file_encoding` | string | Default encodings of the console and of text files ([Default encodings of the target](#default-encodings-of-the-target)) |
| `num_cpu` | integer | Number of logical CPUs |
| `pid`, `executable` | integer, string | Process ID and executable path of `host-shell` |
| `version`, `go_version` | string | Version of `host-shell` and the Go version it was built with |
| `tool_groups` | string[] | List of published tool groups |
| `restrictions` | string[] | List of restrictions in effect (`item: value`, omitted when there are none) |

Table: Output of sys_get_info

## Network

All network tools originate connections from the target machine (`localhost` means the target itself). Destinations can be restricted with [`--allow-host`](#network-destinations---allow-host).

### net_request_http

Sends an HTTP / HTTPS request and returns the status, headers and response body.

| Argument | Type | Required | Description |
|---|---|---|---|
| `url` | string | Yes | Request URL (`http` or `https`) |
| `method` | string | | HTTP method (`GET` when omitted) |
| `headers` | object | | Request headers (`Host` can also be given) |
| `body` | string | | Request body (exclusive with `body_file`) |
| `body_encoding` | string | | `text` (default, UTF-8) or `base64` |
| `body_file` | string | | Path of a file on the target sent as the request body |
| `unix_socket` | string | | Path of a Unix domain socket to connect to (for example `/var/run/docker.sock`) |
| `timeout_sec` | integer | | Timeout in seconds (`--timeout` when omitted; the upper limit is `--max-timeout`) |
| `no_redirects` | boolean | | Do not follow redirects and return 3xx responses as they are (by default up to 10 are followed) |
| `insecure` | boolean | | Skip verification of the TLS certificate |
| `ca_cert` | string | | Path on the target of an additional CA certificate (PEM) to trust |
| `client_cert`, `client_key` | string | | Paths on the target of the client certificate and private key (PEM) for mTLS |
| `proxy` | string | | Proxy URL or `none` (when omitted, follows environment variables such as `HTTP_PROXY`) |
| `response_encoding` | string | | `auto` (default), `text`, `base64` |
| `max_bytes` | integer | | Maximum bytes of the response body to get (the default and upper limit are `--max-output`) |
| `save_to` | string | | Save the response body directly to a file on the target instead of including it in the result (no size limit) |

Table: Arguments of net_request_http

| Output | Type | Description |
|---|---|---|
| `status`, `status_text`, `proto` | integer, string, string | Status code, status text, protocol (`HTTP/1.1` and so on) |
| `headers` | object | Response headers (multiple values joined with `, `) |
| `body`, `body_encoding`, `body_charset` | string | Body, its encoding (`text` / `base64`), the original character encoding |
| `body_bytes`, `body_truncated` | integer, boolean | Bytes received and whether the body was cut |
| `saved_to` | string | Absolute path saved to with `save_to` |
| `final_url`, `redirects` | string, string[] | The final URL and the URLs passed through when redirects were followed |
| `tls` | object | TLS version and server certificate information (`subject`, `issuer`, `dns_names`, `not_before`, `not_after`, `days_left`, `verified`, `verify_error`) |
| `duration_ms` | integer | Time taken (milliseconds) |

Table: Output of net_request_http

```json
{"url": "http://localhost:8080/health"}
```

```json
{"url": "http://localhost/v1.43/containers/json", "unix_socket": "/var/run/docker.sock"}
```

### net_exchange

Connects over TCP, TLS or a Unix domain socket and sends and receives data.

| Argument | Type | Required | Description |
|---|---|---|---|
| `host` | string | | Host name or IP address (`localhost` when omitted) |
| `port` | integer | * | Port number |
| `unix_socket` | string | * | Path of a Unix domain socket to connect to instead of `host` and `port` |
| `tls`, `insecure` | boolean | | Whether to use TLS, and whether to skip certificate verification |
| `send`, `send_encoding` | string | | Data to send after connecting and its format (`text` [default] / `base64`) |
| `close_write` | boolean | | Half-close the writing side after sending |
| `until` | string | | Stop receiving when the received data matches this regular expression |
| `idle_ms` | integer | | Milliseconds to wait without traffic (`1000` when omitted) |
| `timeout_sec` | integer | | Overall timeout in seconds (`10` when omitted) |
| `max_bytes` | integer | | Maximum bytes to receive |
| `receive_encoding` | string | | `auto` (default; `text` if valid UTF-8, otherwise `base64`), `text`, `base64` |

Table: Arguments of net_exchange (* exactly one of `port` and `unix_socket` is required)

The output is `remote_addr`, `sent_bytes`, `received`, `receive_encoding`, `received_bytes`, `matched`, `closed_by_peer`, `truncated`, `tls` and `duration_ms`.

```json
{"port": 6379, "send": "PING\r\n", "until": "\r\n"}
```

### net_check

Runs name resolution, TCP connections and TLS handshakes with certificate checks against several destinations in parallel.

| Argument | Type | Required | Description |
|---|---|---|---|
| `targets` | string[] | Yes | Array of destinations (up to 100; `host:port`, `host` [name resolution only], or a URL [`https://` also checks TLS]) |
| `tls` | boolean | | Do the TLS handshake and certificate check for all destinations |
| `timeout_ms` | integer | | Timeout per destination (milliseconds; `5000` when omitted) |

Table: Arguments of net_check

Each element of `results` in the output contains `target`, `host`, `port`, `addresses`, `dns_ms`, `connected`, `connect_ms`, `tls` (same format as [`net_request_http`](#net_request_http)) and `error`.

### net_list_listeners

Returns the listening TCP and UDP sockets and the processes that own them (from `/proc/net/{tcp,tcp6,udp,udp6}` and `/proc/<pid>/fd` on Linux, and `netstat -ano` and `tasklist` on Windows).

| Argument | Type | Required | Description |
|---|---|---|---|
| `port` | integer | | Only the given port number |
| `protocol` | string | | `tcp` or `udp` |
| `process` | string | | Only those whose process name contains this string (case-insensitive) |

Table: Arguments of net_list_listeners

Each element of `listeners` in the output is `protocol` (`tcp`, `tcp6`, `udp`, `udp6`), `address`, `port`, `pid` and `process` (on Linux without root, `pid` is empty for processes of other users, and `note` says so).

### net_list_interfaces

Returns the list of network interfaces (`name`, `index`, `mtu`, `mac`, `flags`, `addresses` [CIDR notation]) (no arguments).

## Administration

Tools (the `admin` group) that handle processes, services, the system log and disks in a format common to the operating systems.

### proc_list

Returns the list of processes (from `/proc` on Linux, the Win32 API on Windows, and `ps` on macOS).

| Argument | Type | Required | Description |
|---|---|---|---|
| `filter` | string | | Only those whose process name or command line contains this string (case-insensitive) |
| `pid` | integer | | Only the given PID and its descendant processes |
| `max` | integer | | Maximum number of results (`500` when omitted) |

Table: Arguments of proc_list

| Field | Type | Description | Linux | Windows |
|---|---|---|---|---|
| `pid`, `ppid` | integer | Process ID and parent process ID | `/proc/<pid>/stat` | Toolhelp |
| `name` | string | Process name | `/proc/<pid>/stat` (`comm`) | Executable name |
| `command_line` | string | Command line | `/proc/<pid>/cmdline` | `NtQueryInformationProcess` |
| `user` | string | User running it | `/proc/<pid>/status` (UID resolved) | Process token (`DOMAIN\user`) |
| `state` | string | Process state (`R`, `S`, `D`, `Z`, `T` and so on) | `/proc/<pid>/stat` | None |
| `start_time` | string | Start time (RFC 3339) | `/proc/<pid>/stat` | `GetProcessTimes` |
| `memory_bytes` | integer | Physical memory used (RSS / working set) | `/proc/<pid>/statm` | `GetProcessMemoryInfo` |

Table: Fields of proc_list

The output is `processes` (in PID order), `total` (the total after filtering) and `truncated`. Fields that cannot be obtained, for example because of insufficient permissions, are omitted.

### proc_kill

Sends a signal to the process with the given PID or ends it (for jobs started with `job_start`, use [`job_stop`](#job_stop)).

| Argument | Type | Required | Description |
|---|---|---|---|
| `pid` | integer | Yes | Process ID |
| `signal` | string | | Linux: `terminate` (default), `kill`, `interrupt`, `hangup`, `quit`, `stop`, `continue`, `usr1`, `usr2`. Windows: `kill` (default, the only one) |
| `tree` | boolean | | Include descendant processes (walking `/proc` on Linux, `taskkill /T /F` on Windows) |
| `sudo` | boolean | | Use `sudo -n kill` on Linux |

Table: Arguments of proc_kill

The output is `signaled` (array of PIDs signaled) and `exited` (`true` when the process disappeared within 3 seconds).

### svc_list

Returns the list of services (up to 2000; `truncated: true` beyond that). On Linux it comes from `systemctl list-units` and `list-unit-files`, and on Windows from the Service Control Manager (read access only, which needs no administrator rights).

| Argument | Type | Required | Description |
|---|---|---|---|
| `filter` | string | | Only those whose service name or description contains this string (case-insensitive) |
| `state` | string | | `running`, `stopped`, `failed`, `all` (default) |
| `user` | boolean | | On Linux, the user's systemd units (`systemctl --user`) |

Table: Arguments of svc_list

| Field | Linux | Windows |
|---|---|---|
| `name`, `description` | Unit name (`nginx.service`) and description | Service name and display name |
| `state` | `running`, `stopped`, `failed`, `starting`, `stopping` | `running`, `stopped`, `starting`, `stopping`, `paused` |
| `sub_state` | systemd's SUB state (`running`, `exited`, `dead` and so on) | |
| `start_mode` | `enabled`, `disabled`, `static`, `masked` and so on | `auto`, `delayed-auto`, `manual`, `disabled` |
| `pid` | | Process ID |

Table: Fields of svc_list

### svc_control

Gets the status of a single service, or performs an operation such as start or stop and returns the status afterwards (with `--read-only`, only `status` can be used).

| Argument | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Service name (on Windows, the service name, not the display name) |
| `action` | string | | `status` (default), `start`, `stop`, `restart`, `reload` (Linux only), `enable`, `disable` |
| `user` | boolean | | On Linux, the user's units |
| `sudo` | boolean | | Use `sudo -n systemctl ...` on Linux |
| `wait_sec` | integer | | On Windows, seconds to wait for start or stop to complete (`30` when omitted) |

Table: Arguments of svc_control

| Output | Type | Description |
|---|---|---|
| `service` | object | Status in the same format as [`svc_list`](#svc_list) |
| `properties` | object | Detailed properties (`FragmentPath`, `MainPID`, `Result` and so on on Linux; `BinaryPath`, `Account`, `Dependencies`, `ExitCode` and so on on Windows) |
| `status_text`, `output` | string | The output of `systemctl status --lines=20` on Linux, and the output of the operation command |

Table: Output of svc_control

### log_query

Returns the system log (`journalctl -o json` on Linux, `wevtutil qe /f:RenderedXml` on Windows) as a structured array (oldest first, latest last).

| Argument | Type | Required | Description |
|---|---|---|---|
| `unit` | string | | Linux: systemd unit name. Windows: event provider (source) name |
| `log` | string | | Windows: channel name (`System` [default], `Application`, `Security`, `Setup` and so on). Linux: `kernel` for kernel messages only |
| `since`, `until` | string | | Start and end time (relative times such as `30m`, `2h`, `1d`, `2026-10-30 09:00`, or RFC 3339) |
| `priority` | string | | Minimum severity (Linux: `emerg` to `debug`, Windows: `critical`, `error`, `warning`, `information`, `verbose`) |
| `event_id` | integer | | Windows event ID |
| `grep` | string | | Regular expression filter on the message text (case-insensitive) |
| `lines` | integer | | Maximum number of entries from the latest (`100` when omitted, up to `2000`) |
| `user`, `sudo` | boolean | | On Linux, read the user journal, or use `sudo -n journalctl` |

Table: Arguments of log_query

Each element of `entries` in the output is `time` (RFC 3339), `priority`, `source`, `pid`, `event_id` (Windows) and `message`. When the output size limit is exceeded, the oldest entries are dropped and `truncated: true` is set.

### disk_list

Returns the disk usage of file systems (Linux) or drives (Windows).

| Argument | Type | Required | Description |
|---|---|---|---|
| `all` | boolean | | On Linux, also include pseudo file systems (`proc`, `sysfs`, `cgroup` and so on) |

Table: Arguments of disk_list

Each element of `disks` in the output is `mount`, `device` (Linux), `type`, `total_bytes`, `used_bytes`, `free_bytes`, `used_percent`, `total` and `free`.

## Windows registry

Tools (the `admin` group) published only on Windows targets.  
Keys are written from the root (`HKLM`, `HKCU`, `HKCR`, `HKU`, `HKCC`, or `HKEY_LOCAL_MACHINE` and so on), and both `\` and `/` can be used as separators. With `"64"` or `"32"` in `view`, the corresponding WOW64 registry view is accessed on 64-bit Windows.

### reg_read

| Argument | Type | Required | Description |
|---|---|---|---|
| `key` | string | Yes | Registry key path |
| `value` | string | | Name of the value to read (an empty string is the default value; when omitted, the list of subkeys and all values are returned) |
| `view` | string | | `"64"` or `"32"` |

Table: Arguments of reg_read

The output is `key`, `subkeys` and `values` (`name`, `type`, `data`, `expanded`). The representation of `data` per type is as follows.

| type | Format of data |
|---|---|
| `REG_SZ`, `REG_EXPAND_SZ` | String (for `REG_EXPAND_SZ`, the value with environment variables expanded is also stored in `expanded`) |
| `REG_MULTI_SZ` | Array of strings (`string[]`) |
| `REG_DWORD`, `REG_QWORD` | Number |
| Others (`REG_BINARY` and so on) | base64 string |

Table: Format of registry values

### reg_write

Creates or updates a registry value (the key is created if it does not exist).

| Argument | Type | Required | Description |
|---|---|---|---|
| `key` | string | Yes | Registry key path |
| `value` | string | | Name of the value (an empty string is the default value; when omitted, only the key is created) |
| `type` | string | | `REG_SZ` (default), `REG_EXPAND_SZ`, `REG_MULTI_SZ`, `REG_DWORD`, `REG_QWORD`, `REG_BINARY` |
| `data` | any | | Value to write |
| `view` | string | | `"64"` or `"32"` |

Table: Arguments of reg_write

The output is `key`, `created_key` and `value`.

### reg_delete

Deletes a registry value or key (deleting a root key itself is refused).

| Argument | Type | Required | Description |
|---|---|---|---|
| `key` | string | Yes | Registry key path |
| `value` | string | | Name of the value to delete (an empty string is the default value; when omitted, the key itself is deleted) |
| `recursive` | boolean | | Delete the key recursively including subkeys (without it, subkeys make it an error) |
| `view` | string | | `"64"` or `"32"` |

Table: Arguments of reg_delete

## Transfer with the local machine (bridge)

`host-shell bridge` is a relay subcommand that runs on the client side. It acts as an MCP server toward the agent, relays to the target's `host-shell` it starts as a child process, and adds tools for transferring files between the local machine and the target.

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

`COMMAND` is the command that starts `host-shell --mcp` on the target. The standard error of `COMMAND` is passed through to the standard error of `bridge` as it is.

| Option | Value | Description |
|---|---|---|
| `--allow-local-path` | Directory (can be repeated) | Local directories file transfers may read and write |
| `--allow-local-read-path` | Directory (can be repeated) | Local directories `transfer_push` may only read |
| `--no-transfer` | None | Only relay, without adding the transfer tools (`transfer_push`, `transfer_pull`) |

Table: Options of bridge

### Relay

- The target's tools and resources are published with the same names, descriptions and schemas, and calls, progress notifications (`progressToken`), cancellation notifications, log notifications and confirmation requests (`--confirm`) are relayed transparently.
- `instructions` in the initialize response returns the target's guidance with guidance for the file transfer tools added.
- It connects to the target with the client name `host-shell-bridge`, and only `fs_read` (`base64`) from this client name may read chunks of up to 1 MiB at a time.
- When the session to the target is disconnected, `bridge` also exits.

### transfer_push

Transfers a local file or directory to the target.

| Argument | Type | Required | Description |
|---|---|---|---|
| `local_path` | string | Yes | Local path (relative paths are based on the working directory of `bridge`) |
| `remote_path` | string | Yes | Path on the target (for a single file, an existing directory or a trailing `/` places it inside under the same name) |
| `overwrite` | boolean | | Overwrite existing files with different content (files with the same content are always skipped) |
| `exclude` | string[] | | Array of globs of file and directory names to exclude (for example `[".git", "node_modules", "*.pyc"]`) |
| `dry_run` | boolean | | Do not transfer; only return the list of files concerned |
| `parallel` | integer | | Number of files transferred at the same time (1 to 16; `4` when omitted) |

Table: Arguments of transfer_push

Each file is transferred as follows (on failure, `.host-shell-part` is removed; empty directories and symbolic links are skipped).

1. The SHA-256 of an existing file is checked with `fs_manifest` on the target; if it matches, the transfer is skipped (`unchanged`); if it does not match and `overwrite` is `false`, the file is skipped (`exists`).
2. It is written to `<destination>.host-shell-part` in 1 MiB units by calling `fs_write` (`base64`, `offset`) (the local executable permission is applied as `perm` on the first write).
3. The SHA-256 is verified with `fs_manifest`, and if it matches, the file is renamed to its real name with `fs_move`.

| Output | Type | Description |
|---|---|---|
| `source`, `destination` | string | Source and destination paths |
| `transferred`, `unchanged`, `failed` | integer | Files transferred (planned with `dry_run`), files skipped as unchanged, failures (`exists` + `error`) |
| `bytes` | integer | Total bytes transferred |
| `files` | object[] | Result per file (`path`, `bytes`, `status` [`sent`/`unchanged`/`exists`/`would-send`/`skipped`/`error`], `error`). Beyond 500 entries, `unchanged` ones are omitted and `truncated: true` is set |
| `duration_ms` | integer | Time taken (milliseconds) |

Table: Output of transfer_push and transfer_pull

### transfer_pull

Transfers a file or directory on the target to the local machine. The arguments and output are the same as [`transfer_push`](#transfer_push) (`local_path` is the destination). The SHA-256 is checked with `fs_manifest` on the target, and except for unchanged files, the file is read with `fs_read` (`base64`) in 1 MiB units into `<local destination>.host-shell-part`, then renamed after SHA-256 verification and applying the permissions.

## Starting and ending processes

### Shells

The `command` argument is passed to the shell chosen by `shell` in the following form.

| shell | OS | How it is started |
|---|---|---|
| `sh` | Linux (default) | `sh -c '<command>'` |
| `bash` | Linux | `bash -c '<command>'` |
| `cmd` | Windows | `"%ComSpec%" /d /s /c "<command>"` |
| `powershell` | Windows (default) | `powershell.exe -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command "$input \| . ([ScriptBlock]::Create(...<base64>...))"` |
| `pwsh` | Windows, Linux | `pwsh -NoProfile -NonInteractive -ExecutionPolicy Bypass -OutputFormat Text -Command "$input \| . ([ScriptBlock]::Create(...<base64>...))"` |

Table: How shells are started

PowerShell is the default shell on Windows because passing the command line as UTF-16LE base64 avoids quote interpretation by `cmd` and SSH and unifies output to UTF-8.  
`-Command` receives only a short loader that decodes and runs the base64 script, passing standard input as `$input` in the current scope. `-EncodedCommand` is not used because Windows PowerShell 5.1 writes errors, warnings, and progress to standard error as CLIXML (XML) when started with `-EncodedCommand`.

The following statements are prepended to the script.

| Statement | Purpose |
|---|---|
| `$ProgressPreference = 'SilentlyContinue'` | Suppress progress output (initial module loading, `Invoke-WebRequest`, etc.) |
| `if ($PSStyle) { $PSStyle.OutputRendering = 'PlainText' }` | Suppress PowerShell 7 ANSI color sequences (no-op on 5.1) |
| `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8` | Unify PowerShell output to UTF-8 |

Table: What is added before a PowerShell script

Errors are written to standard error and `Write-Warning` messages to standard output as plain text (`-Command` behavior).  
Exit code handling is appended to the script so exit codes follow the same rules as `sh -c` on Linux.

| Result of the last statement run | Exit code |
|---|---|
| Succeeded (`$?` is true) | `0` |
| Failed, and a native command set a non-zero `$LASTEXITCODE` | `$LASTEXITCODE` |
| Failed, and `$LASTEXITCODE` is 0 or unset (cmdlet failure) | `1` |
| Exited via `exit N` | `N` |
| Terminated by an unhandled error (`throw`, etc.) | `1` |

Table: PowerShell exit codes

If a native command invoked from PowerShell outputs Shift_JIS and appears garbled, specify `"shell": "cmd"` (`cmd` is started with `/d` to disable AutoRun and receives the command string directly).

#### Standard input of PowerShell on Windows

PowerShell decodes standard input (`$input`) using the console input code page at process startup; changing `[Console]::InputEncoding` inside the script does not affect `$input`.  
Therefore, `host-shell` allocates a hidden console with input and output code pages set to 65001 (UTF-8) and starts PowerShell attached to that console, allowing arbitrary Unicode text in `stdin` regardless of the target's system code page.

| Condition | Default of `stdin_encoding` | Console PowerShell uses |
|---|---|---|
| `stdin_encoding` omitted or `utf-8` | `utf-8` | `host-shell` console (65001) |
| `stdin_encoding` set to a value other than `utf-8` | Specified value | PowerShell's own console (code page unchanged) |
| `host-shell` standard I/O is a console (started directly in a terminal) | `system` with `shell`, `utf-8` with `argv` | PowerShell's own console |

Table: Standard input of PowerShell on Windows

- This applies to calls with `shell` set to `powershell` or `pwsh`, and `argv` calls whose first element is `powershell` or `pwsh` (with or without `.exe` or a directory path). `argv` arguments are not modified. `pty` jobs use ConPTY and are unaffected.
- The hidden console is allocated once on the first PowerShell call (approx. 100 ms). When `host-shell` is started directly in an interactive terminal, no hidden console is created so the user's console code page is not modified.
- `cmd` and other native binaries run in their own consoles (OEM code page).
- On non-English systems (such as Japanese Windows), PowerShell running in the code page 65001 console and native commands spawned from it may emit English diagnostic messages (Windows falls back to English when the console code page cannot represent the UI language).

### Starting with argv

With `argv`, the executable is started directly without a shell (`PATH` is searched, and on Windows the arguments are escaped by the standard MSVC rules).

### Environment variables and standard input

Child processes inherit the environment variables of `host-shell`, with the removals of `--unset-env` applied, and the variables of the `env` argument added or overridden.  
When `stdin` is omitted, standard input is empty (`/dev/null` or `NUL`), and the standard input and output of `host-shell` itself (the MCP channel) are never inherited by child processes.

### How processes are ended

On timeout, cancellation, `job_stop` (`kill`) and when `host-shell` exits, commands are killed together with their descendant processes.

| OS | How they are killed | Processes that cannot be caught |
|---|---|---|
| Linux | Started in a new process group (a new session for `pty`), and `SIGKILL` is sent to the whole group | Processes that left for another process group or session with `setsid` and so on |
| Windows | The process tree is killed with `taskkill /T /F` | Descendant processes orphaned from the tree because their parent ended first (when `host-shell` exits, they are ended by the Job Object) |

Table: How processes are ended

Even when `host-shell` itself is killed (`kill -9`, the SSH session breaks and so on), the following mechanisms keep child processes from being left behind.

| OS | Mechanism | What may be left behind |
|---|---|---|
| Linux | `PR_SET_PDEATHSIG(SIGKILL)` is set on direct child processes | Grandchild processes that do not stop when the direct child ends |
| Windows | At startup, `host-shell` puts itself into a Job Object with `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`, so all descendant processes belong to it automatically | Nothing (leaving the Job Object is not allowed) |

Table: What happens to commands when host-shell exits

Start daemons and services that must keep running after the session ends through `systemd-run`, a Windows service or the Task Scheduler, not directly as child processes of `host-shell`.  
If descendant processes keep the output pipes open after the command ends, the pipes are closed after waiting up to 2 seconds and the result is returned.

## Audit log

`host-shell` records every tool call in a log file on the target in JSON Lines format, one record per line.

### Location

| OS | Default base path |
|---|---|
| Linux | `$XDG_STATE_HOME/host-shell/audit.log` (`~/.local/state/host-shell/audit.log` when `XDG_STATE_HOME` is not set) |
| Windows | `%LOCALAPPDATA%\host-shell\audit.log` |

Table: Default base path of the audit log

The log is written to `<base name>-YYYY-MM-DD<extension>` (for example `audit-2026-10-31.log`) per local date of the target.

- In addition to the current day, the 5 most recent days are kept, and older `<base name>-YYYY-MM-DD<extension>` files are deleted automatically at startup and when the date changes (an `audit.log` without a date is not deleted).
- The directory is created with mode `0700` and log files with mode `0600`. Each record opens, appends and closes the file, so lines do not get mixed even when several sessions write at the same time.
- If the log file cannot be opened, `host-shell` fails to start (use `--no-log` when no records are needed).

### Format

```json
{"time":"2026-10-30T08:15:42.42+09:00","level":"INFO","msg":"tool_call","tool":"fs_read","args":{"path":"/nonexistent"},"duration_ms":0,"is_error":true,"error":"open /nonexistent: no such file or directory"}
```

| msg | When it is recorded | Fields |
|---|---|---|
| `server_start` | At startup | `version`, `pid` |
| `tool_call` | When a tool call completes (including policy refusals and `<tool name> (confirmation requested)`) | `tool`, `args`, `duration_ms`, `is_error`, `error` (on error) |
| `server_stop` | At exit | `error` (`<nil>` on normal exit) |

Table: Audit log records

### Values not recorded

`args` records the tool arguments, but the following fields are masked to protect secrets and keep the log from growing too large.

| Tool | Argument | Value recorded |
|---|---|---|
| `exec_run`, `job_start`, `exec_sudo` | `stdin` | `<N chars>` |
| `exec_sudo` | `password` | `<redacted>` |
| `job_write` | `input` | `<N chars>` |
| `fs_edit` | `old_string`, `new_string` (including inside `edits`) | `<N chars>` |
| `fs_write` | `content` | `<N chars, sha256 ...>` |
| `net_request_http` | `body`, and `Authorization`, `Proxy-Authorization`, `Cookie`, `X-Api-Key` of `headers` | `<N chars>` |
| `net_exchange` | `send` | `<N chars>` |

Table: Values replaced in the audit log

`command` and `argv` are recorded as they are, so do not put secrets such as passwords in command lines; pass them through `stdin` and so on.

## Limitations

- Pseudo-terminals are available only with `pty: true` of `job_start` (`exec_run` runs non-interactively with pipes). Get the display of full-screen programs (`vi`, `top` and so on) with `screen: true` of `job_read`.
- Encoding detection checks only BOMs, the byte distribution of UTF-16 and UTF-8 validity; it does not tell non-UTF-8 encodings such as Shift_JIS and EUC-JP apart. For files in an encoding different from the target's default, give `charset` explicitly.
- Rarely, a short non-UTF-8 byte sequence happens to be valid UTF-8 and is garbled. In that case, give `output_encoding` or `charset` explicitly.
- `net_list_listeners`, `svc_list`, `svc_control`, `log_query` and `disk_list` support only Linux and Windows (service and log operations on Linux assume systemd).
- Restrictions by startup options (policy) are checks on tool arguments, not OS permission boundaries (see [Limits of the restrictions](#limits-of-the-restrictions)).
- Based on the requirements of Go 1.26, the supported operating systems are Windows 10 / Windows Server 2016 or later (`pty` needs Windows 10 1809 / Windows Server 2019 or later), Linux kernel 3.2 or later, and macOS 12 or later (`bridge` only).

## Client proxy

[HTTP proxy through the client](client-proxy.md) describes startup, restrictions, proxy_status, environment precedence, and limits.
