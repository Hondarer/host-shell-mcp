# Searching

## Glob syntax (search_files pattern, search_grep include, fs_manifest include)

- Without a slash, the pattern matches the **base name**: `*.go`, `*.{yml,yaml}`, `Makefile`.
- With a slash, it matches the **path relative to `path`**: `src/**/*.ts`, `**/test/*.py`, `cmd/*/main.go`.
- `*` and `?` stay within one path element, `**` matches any number of directories (including none),
  `[abc]` is a character class, `{a,b}` is an alternative.
- On Windows, matching is case-insensitive and `\` is accepted as a separator.
- `exclude_dirs` names directories to skip anywhere in the tree. The default is `.git`, `.hg`, `.svn`.
  A list you pass replaces the default, so include those too, e.g. `[".git", "node_modules", "dist"]`.

## search_files

Recursive file and directory search by glob. `type` limits to `file` or `dir`.
`sort: "mtime"` returns the most recently modified first (good for "which logs changed").

## search_grep

Regular expressions use Go RE2 syntax: no lookahead/lookbehind and no backreferences.
Set `literal: true` to search for a plain string without escaping. `ignore_case` for case-insensitive.

| output_mode | Returns |
|---|---|
| `content` (default) | `matches`: path, line number, line text; with `context` (or `before` / `after`) also surrounding lines |
| `files` | `files`: only the paths of files that contain a match (fast; good first step) |
| `count` | `files`: path and number of matching lines |

- `max_matches` (default 200) caps lines in content mode and files in the other modes; `truncated`
  tells you the cap was hit.
- Binary files (NUL bytes) and files larger than `max_file_bytes` (default 10 MiB) are skipped.
- Lines are decoded per line (see topic `encoding`), so Japanese patterns match Shift_JIS or
  EUC-JP files; `charset` on a match says it was converted.
- Matching is per line. To find something spanning lines, search for one line and read around it
  with `fs_read`.

Typical sequence: `output_mode: "files"` to find candidates, then `content` with `context: 3` in
the interesting file, then `fs_read` with `line_start` for the full picture.
