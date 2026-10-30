# Files

## fs_read

| Mode | Arguments | Next chunk |
|---|---|---|
| Text lines (source code, configs) | `line_start` (1-based), `line_count` (default 2000), optional `line_numbers: true` | `next_line` |
| Text byte range (large logs) | `offset`, `length` | `next_offset` |
| Binary byte range | `encoding: "base64"`, `offset`, `length` | `next_offset` |

- Without `line_start`, text is read from byte `offset` (default 0) up to the output limit.
- `eof: true` indicates end of file. `truncated: true` indicates the output limit cut the requested range; continue from `next_line` or `next_offset`.
- `line_numbers: true` prefixes each line with its number and a tab (`     12\t...`), like `cat -n`. The prefix is not part of the file; do not include it in `fs_edit` or `fs_write`.
- The result reports `charset`, `bom`, and `newline`. Byte-range reads never split a multibyte character; any trailing partial character is left for `next_offset`.
- To read the tail of a large log: call `fs_stat` for `size`, then call `fs_read` with `offset: size - 65536`.

## fs_write

- `mode`: `overwrite` (default), `append`, or `create` (fails if the file already exists).
- `offset`: writes at a byte offset without truncating (for resuming transfers or patching binary data). Cannot be combined with `append`.
- `make_dirs: true` creates missing parent directories. `perm` (e.g. `"0755"`) sets permissions on new files on Linux.
- By default, an existing file is truncated and rewritten in place, preserving owner, permissions, and links. `atomic: true` (also supported in `fs_edit`) writes a temporary file and renames it over the target so running services never observe a partially written file. It preserves permissions and owner (failing if ownership cannot be preserved), follows symlinks, and drops ACLs, extended attributes, and hard links. Supported only with `mode: "overwrite"` and no `offset`.
- `encoding: "base64"` writes raw bytes. Otherwise `content` is text and can be converted using `charset`, `newline` (`lf` or `crlf`), and `bom`. See topic `encoding`.
- Large files: send the first chunk with `mode: "overwrite"` and subsequent chunks with `mode: "append"` or `offset`, then verify the SHA-256 with `fs_manifest`.

## fs_edit

Replaces exact text in an existing file. See topic `edit`.

## Directories and file operations

| Tool | Behavior |
|---|---|
| `fs_list` | Lists one directory (non-recursive). Use `search_files` for recursive listings |
| `fs_stat` | Returns type, size, mode, and modification time without following a final symlink |
| `fs_mkdir` | Creates the directory and parents. Existing directories succeed with `created: false` |
| `fs_move` | Renames or moves to the full path `destination`. `overwrite` replaces a file, never a directory. Copies then deletes across file systems |
| `fs_copy` | Copies a file, or a directory with `recursive: true`. Recreates symlinks without following them. `overwrite` replaces files and merges directories |
| `fs_remove` | Deletes a file or directory (`recursive: true` required for non-empty directories). Refuses file system roots and the home directory. `missing_ok` ignores missing paths |

Prefer these tools over running `rm -rf`, `Remove-Item`, `mv`, or `copy` via `exec_run`; their behavior is identical on Linux and Windows.

## Comparing directory trees: fs_manifest

Returns the relative path, size, and SHA-256 of every file under a directory (filtered by `include`). Compare the manifest with local files to transfer only files that differ.
