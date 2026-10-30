# Character encodings and line endings

Everything you send and receive is UTF-8. host-shell converts on the host.

## Values for output_encoding, stdin_encoding and charset

| Value | Meaning |
|---|---|
| `auto` | Default for reading. Uses a BOM if present, or UTF-16 detected from the zero bytes of ASCII characters; keeps UTF-8 when the bytes are valid UTF-8, otherwise uses the host default. Decided per line for command output and search |
| `utf-8` | No conversion |
| `system` | The host default: `console_encoding` for commands, `file_encoding` for files (see `sys_info`) |
| a name | `shift_jis` (`cp932`, `sjis`), `euc-jp`, `iso-2022-jp`, `gbk`, `gb18030`, `big5`, `euc-kr`, `windows-1252`, `iso-8859-1`, `utf-16le`, `utf-16be`, `cp437`, `65001` ... |

## Commands

- `output_encoding` (default `auto`) decodes stdout and stderr. When conversion happened,
  `stdout_encoding` / `stderr_encoding` (jobs: `status.output_encoding`) name the source encoding.
- `stdin_encoding` (default `utf-8`) converts `stdin` and `job_write` input.

## Files

- `fs_read` and `search_grep` default to `auto` and report `charset`. `fs_read` also reports `bom`
  and `newline` (`lf`, `crlf`, `cr`, `mixed`).
- `fs_edit` detects the encoding the same way and writes back in the same encoding, BOM and line endings.
- `fs_write` writes UTF-8 unless `charset` is given. To rewrite a file in its original format, pass
  the `charset`, `bom` and `newline` that `fs_read` reported. Characters the target encoding cannot
  represent (emoji in Shift_JIS ...) cause an error and nothing is written.
- `newline: "crlf"` / `"lf"` converts the line endings of `content` before writing.
- UTF-16 without a BOM is detected when it contains ASCII characters or line breaks; for short text made only of
  CJK characters, pass `charset` (e.g. `utf-16le`).

Base64 content is never converted.
