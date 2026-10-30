# Files

## fs_read

Three ways to read:

| Goal | Arguments | Continue with |
|---|---|---|
| Lines of a text file (source code, configs) | `line_start` (1-based), `line_count` (default 2000), optional `line_numbers: true` | `next_line` |
| A byte range of a text file (huge logs) | `offset`, `length` | `next_offset` |
| A byte range of a binary file | `encoding: "base64"`, `offset`, `length` | `next_offset` |

- Without `line_start`, text is read from byte `offset` (default 0) up to the output limit.
- `eof: true` means the end of the file was reached. `truncated: true` means the output limit
  cut the requested range; continue from `next_line` / `next_offset`.
- `line_numbers: true` prefixes each line with its number and a tab (`     12\t...`), like `cat -n`.
  The prefix is not part of the file: never copy it into `fs_edit` or `fs_write`.
- The result reports `charset`, `bom` and `newline` of the file. A range never splits a character;
  a partial character at the end is left for the next read.
- To read the end of a big log: `fs_stat` for the size, then `fs_read` with `offset: size - 65536`.

## fs_write

- `mode`: `overwrite` (default), `append`, or `create` (fails if the file exists; use it to avoid
  clobbering).
- `offset`: write at a byte position without truncating (resume an interrupted transfer, patch
  binary data). Not with `append`.
- `make_dirs: true` creates missing parent directories. `perm` (e.g. `"0755"`) applies to new files on Linux.
- `encoding: "base64"` writes binary content. Otherwise `content` is text and may be converted with
  `charset`, `newline` (`lf`/`crlf`) and `bom`. See topic `encoding`.
- Large files: split into chunks of the size you can send, first chunk with `mode: "overwrite"`,
  the rest with `mode: "append"` (or `offset`). Check the result with `fs_manifest` (SHA-256).

## fs_edit

Exact string replacement in an existing file. See topic `edit`.

## Directories and file operations

| Tool | Notes |
|---|---|
| `fs_list` | One directory, not recursive. Use `search_files` for recursive listings |
| `fs_stat` | Type, size, mode, modification time. Does not follow a final symlink |
| `fs_mkdir` | Creates parents too. An existing directory is not an error (`created: false`) |
| `fs_move` | Rename or move. `destination` is the full new path. `overwrite` replaces a file, never a directory. Works across file systems (copies then deletes) |
| `fs_copy` | Files, or directories with `recursive: true`. Symlinks are recreated, not followed. `overwrite` replaces files and merges directories |
| `fs_remove` | `recursive: true` is required for a non-empty directory. Refuses file system roots and the home directory. `missing_ok` ignores a missing path |

These tools behave the same on Linux and Windows; prefer them over `rm -rf`, `Remove-Item`,
`mv`, `copy` through `exec_run`.

## Comparing trees: fs_manifest

Returns relative path, size and SHA-256 of every file under a directory (filtered by `include`).
Compare with your local copy and read or write only the files that differ.
