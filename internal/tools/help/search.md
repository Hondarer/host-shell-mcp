# Searching

## Glob syntax (search_find pattern, search_grep include, fs_manifest include)

- Patterns without `/` match the **base name**: `*.go`, `*.{yml,yaml}`, `Makefile`.
- Patterns with `/` match the **path relative to `path`**: `src/**/*.ts`, `**/test/*.py`, `cmd/*/main.go`.
- `*` and `?` match within a single path segment, `**` matches zero or more directories, `[abc]` matches a character class, and `{a,b}` matches alternatives.
- On Windows, matching is case-insensitive and accepts `\` as a path separator.
- `exclude_dirs` skips matching directory names anywhere in the tree (default: `[".git", ".hg", ".svn"]`). Passing `exclude_dirs` replaces the default list; include `.git` if it should remain excluded (e.g. `[".git", "node_modules", "dist"]`).
- `search_find` and `search_grep` honor `.gitignore` files in the searched tree, parent repositories, and `.git/info/exclude`. Pass `gitignore: false` to include ignored files (build outputs, `node_modules`, logs).

## search_find

Recursively finds files and directories matching a glob. `type` filters by `file` or `dir`. `sort: "mtime"` orders results newest first.

## search_grep

Patterns use Go RE2 regular expression syntax (no lookahead, lookbehind, or backreferences). Set `literal: true` for exact substring matching without escaping, and `ignore_case: true` for case-insensitive matching.

| output_mode | Returns |
|---|---|
| `content` (default) | `matches`: path, line number, and line text, plus surrounding lines when `context`, `before`, or `after` is set |
| `files` | `files`: paths of matching files only (fastest for initial discovery) |
| `count` | `files`: path and matching line count per file |

- `max_matches` (default 200) caps matching lines in `content` mode and matching files in `files` or `count` mode; `truncated: true` indicates the cap was reached.
- Binary files (containing NUL bytes) and files larger than `max_file_bytes` (default 10 MiB) are skipped.
- Lines are decoded individually (see topic `encoding`), allowing UTF-8 patterns to match Shift_JIS or EUC-JP files; `charset` on a match reports the source encoding when converted.
- By default, patterns match within a single line. With `multiline: true`, the pattern runs against the entire file, `.` matches newlines, `^` and `$` match line boundaries, and each match returns `line`, `end_line`, and the matched text. `multiline: true` skips files over 32 MiB (`skipped_large`).

Recommended workflow: run `output_mode: "files"` to locate candidate files, then `output_mode: "content"` with `context: 3` on relevant files, then `fs_read` with `line_start` for surrounding code.
