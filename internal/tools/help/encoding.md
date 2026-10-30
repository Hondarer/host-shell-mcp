# Character encodings and line endings

All MCP requests and responses use UTF-8; host-shell converts encodings on the remote host.

## Values for output_encoding, stdin_encoding, and charset

| Value | Behavior |
|---|---|
| `auto` | Default for reading. Uses a BOM when present, or UTF-16 detected from ASCII zero bytes; preserves valid UTF-8, and otherwise falls back to the host default. Evaluated per line for command output and search |
| `utf-8` | No conversion |
| `system` | Host default encoding: `console_encoding` for commands, `file_encoding` for files (see `sys_get_info`) |
| Encoding name | `shift_jis` (`cp932`, `sjis`), `euc-jp`, `iso-2022-jp`, `gbk`, `gb18030`, `big5`, `euc-kr`, `windows-1252`, `iso-8859-1`, `utf-16le`, `utf-16be`, `cp437`, `65001`, etc. |

## Commands

- `output_encoding` (default `auto`) decodes stdout and stderr. When conversion occurs, `stdout_encoding` and `stderr_encoding` (or `status.output_encoding` for jobs) report the detected source encoding.
- `stdin_encoding` encodes `stdin` and `job_write` input (default `utf-8`). On Windows, PowerShell runs in a UTF-8 console so it reads `utf-8` stdin intact; with another `stdin_encoding`, it runs in its own console and must read that code page. In the UTF-8 console, messages from PowerShell and the programs it runs may be in English.

## Files

- `fs_read` and `search_grep` default to `auto` and report `charset`. `fs_read` also reports `bom` and `newline` (`lf`, `crlf`, `cr`, `mixed`).
- `fs_edit` detects the file's encoding, BOM, and line endings the same way and preserves them when writing.
- `fs_write` writes UTF-8 unless `charset` is specified. To preserve an existing file's format during a full rewrite, pass the `charset`, `bom`, and `newline` returned by `fs_read`. If `content` contains characters unrepresentable in `charset` (such as emoji in Shift_JIS), `fs_write` returns an error without modifying the file.
- `newline: "crlf"` or `"lf"` normalizes line endings in `content` before writing.
- BOM-less UTF-16 is auto-detected when it contains ASCII characters or line breaks; for short BOM-less UTF-16 text containing only CJK characters, specify `charset` explicitly (`utf-16le` or `utf-16be`).

Base64 content is never converted.
