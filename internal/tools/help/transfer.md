# Transferring files

## With host-shell bridge (recommended)

When the MCP server is registered via `host-shell bridge` on the local machine, two transfer tools are available:

- `transfer_push`: copies a local file or directory to the remote host.
- `transfer_pull`: copies a remote file or directory to the local machine.

```json
{"local_path": "dist/app.tar.gz", "remote_path": "/opt/app/releases/"}
{"local_path": "src", "remote_path": "/srv/app/src", "overwrite": true, "exclude": [".git", "node_modules"]}
{"local_path": "logs", "remote_path": "/var/log/app"}
```

- File data streams directly between `bridge` and `host-shell` in 1 MiB chunks without passing through the model context. Each destination file is written to a temporary path, verified by SHA-256, and renamed into place.
- Files with matching SHA-256 hashes are skipped (`unchanged`), so repeated calls transfer only modified files. Existing files that differ are skipped and reported as `exists` unless `overwrite: true`. `dry_run: true` previews actions without transferring bytes.
- Up to `parallel` files (default 4, max 16) transfer concurrently while preserving result order.
- Executable permissions are preserved between Linux hosts. Empty directories and symbolic links are skipped.
- If `transfer_push` and `transfer_pull` are not listed, the server was registered without `host-shell bridge`.

## Without bridge

Transfer small files through `fs_read` and `fs_write` (contents pass through the model context):

- Text files: `fs_read` then `fs_write`.
- Binary files: read chunks with `fs_read` (`encoding: "base64"`, `offset`, `length`), write chunks with `fs_write` (`encoding: "base64"`, `offset`), and verify SHA-256 hashes with `fs_manifest`.
- Remote downloads: if the remote host can reach the URL directly, download it on the host with `net_http` (`save_to`).
