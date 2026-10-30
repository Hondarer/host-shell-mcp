# Editing files with fs_edit

`fs_edit` replaces exact text in an existing file and preserves its encoding, BOM, and line endings. All edits in a call are validated before writing; if any edit fails, the file is left unchanged.

## Single edit

```json
{"path": "/srv/app/config.yml", "old_string": "  port: 8080\n", "new_string": "  port: 9090\n"}
```

## Multiple edits in one call

```json
{"path": "src/main.c", "edits": [
  {"old_string": "#include <stdio.h>\n", "new_string": "#include <stdio.h>\n#include <string.h>\n"},
  {"old_string": "MAX_LEN 64", "new_string": "MAX_LEN 256", "replace_all": true}
]}
```

Edits apply sequentially; each operates on the result of the previous edit.

`atomic: true` writes to a temporary file and renames it over the target, as in `fs_write` (see topic `files`). Use it for files read by running processes.

## Rules

- `old_string` must match the file **exactly**, including indentation (tabs vs spaces) and trailing spaces. Copy it from `fs_read` output without the `line_numbers` prefix.
- `old_string` must occur **exactly once** unless `replace_all: true`. If it matches multiple locations, the error lists their line numbers; include surrounding context lines to disambiguate.
- If `old_string` is not found and a region differs only in whitespace, the error reports that line number. Re-read that section and retry.
- `\n` in `old_string` and `new_string` matches and writes CRLF automatically in CRLF files.
- Set `new_string` to `""` to delete `old_string`.
- `dry_run: true` validates the edits and returns `snippet` without modifying the file.

## Result

- `replacements`: count of replaced occurrences; `lines`: starting line numbers in the updated file.
- `snippet`: edited regions with two lines of context and line numbers, avoiding a follow-up `fs_read`.
- `sha256`, `size`, `charset`, `bom`, and `newline` of the written file.

## Alternatives to fs_edit

- New files or full rewrites: `fs_write`.
- Binary files: `fs_write` with `encoding: "base64"` and `offset`.
- Files over 32 MiB: `fs_write` with `offset`.
