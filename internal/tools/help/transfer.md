# Transferring files

## With host-shell bridge (recommended)

When host-shell is registered through `host-shell bridge` on your local machine, two more tools exist:

- `transfer_push`: local file or directory -> remote host.
- `transfer_pull`: remote file or directory -> local machine.

```json
{"local_path": "dist/app.tar.gz", "remote_path": "/opt/app/releases/"}
{"local_path": "src", "remote_path": "/srv/app/src", "overwrite": true, "exclude": [".git", "node_modules"]}
{"local_path": "logs", "remote_path": "/var/log/app"}
```

- The contents go directly between bridge and host-shell, in 1 MiB chunks, and never enter your
  context. Each file is written under a temporary name, checked by SHA-256 and then renamed.
- Identical files are skipped (`unchanged`), so repeating a push or pull syncs only the differences.
  Different existing files are reported as `exists` unless `overwrite` is set. `dry_run` previews.
- Executable bits are kept between Linux hosts. Empty directories and symbolic links are not copied.
- If these tools are missing, host-shell is registered without bridge.

## Without bridge

Move data through the tools themselves; it passes through your context, so keep it small:

- Text: `fs_read` then `fs_write`.
- Binary: `fs_read` with `encoding: "base64"` and `offset`/`length`, `fs_write` with `encoding: "base64"`
  and `offset` for each chunk, then compare `fs_manifest` hashes.
- Downloads the host can reach: `net_http` with `save_to`.
