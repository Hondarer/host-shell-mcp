# Editing files with fs_edit

`fs_edit` replaces exact text in an existing file and writes it back in the same encoding, BOM and
line endings. Nothing is written unless every edit succeeds.

## Single edit

```json
{"path": "/srv/app/config.yml", "old_string": "  port: 8080\n", "new_string": "  port: 9090\n"}
```

## Several edits in one call

```json
{"path": "src/main.c", "edits": [
  {"old_string": "#include <stdio.h>\n", "new_string": "#include <stdio.h>\n#include <string.h>\n"},
  {"old_string": "MAX_LEN 64", "new_string": "MAX_LEN 256", "replace_all": true}
]}
```

Edits apply in order; each sees the result of the previous one.

## Rules

- `old_string` must match the file **exactly**, including indentation (tabs vs spaces) and
  trailing spaces. Copy it from `fs_read` output, without the `line_numbers` prefix.
- It must occur **exactly once**, unless `replace_all: true`. If it occurs several times the error
  lists the lines; add surrounding lines to make it unique.
- If it is not found, the error says so and, when a block differing only in whitespace exists,
  gives its line. Re-read that part of the file and retry.
- You may write line endings as `\n` even when the file uses CRLF: they are matched and written as
  CRLF automatically.
- `new_string` may be empty to delete text.
- `dry_run: true` validates and returns the snippet without writing.

## Result

- `replacements`: number of replaced occurrences; `lines`: where they start in the new file.
- `snippet`: the edited regions with two lines of context and line numbers. Check it instead of
  re-reading the file.
- `sha256`, `size`, `charset`, `bom`, `newline` of the written file.

## When not to use fs_edit

- New files or complete rewrites: `fs_write`.
- Binary files: `fs_write` with `encoding: "base64"` and `offset`.
- Files over 32 MiB: `fs_write` with `offset`.
