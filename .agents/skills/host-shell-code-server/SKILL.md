---
name: host-shell-code-server
description: Inject code-server into a host-shell target (including closed networks without internet access), start it on the target's loopback, and open it in the user's browser through host-shell port forwarding. On the first install it also injects default settings and extensions. Use for requests such as "put code-server on target", "open VS Code in the browser for target", or "remove code-server from target".
---

# Injecting code-server into a host-shell target

Install code-server on the target using archives fetched on the client, start it on `127.0.0.1` on the target (binding to the first free port in `8000`–`8079`), and forward a local port to it.  
The target needs no internet access; every file is verified by SHA-256 on the client and again on the target.  
This skill operates through the `host-shell` MCP tools of a session started with `host-shell bridge`. To deploy `host-shell` itself, use the `host-shell-connect` skill. The target may also be a Linux container reached via `podman exec -i` or a similar command (`docs/en/container.md`).

Below, `$SKILL` refers to the directory containing this `SKILL.md` (`.agents/skills/host-shell-code-server` in the repository). The user documentation is in `docs/en/code-server.md`.

## Folder layout

| Path | Contents |
|---|---|
| `version.env` | code-server version, bundled VS Code version, target port range (`8000`–`8079`), and SHA-256 of each release archive |
| `defaults/User/settings.json`, `defaults/Machine/settings.json` | Default settings installed on first setup |
| `defaults/extensions.txt` | Default extensions (`publisher.name` or `publisher.name@version`, one per line) |
| `packages/` | Release archives and `extensions/<TYPE>/` (VSIX files and `resolved-extensions.txt`) fetched by `download` (not tracked in Git) |
| `scripts/linux/download.sh`, `scripts/windows/download.ps1` | Client: fetch and verify the release archive and default extensions for target `TYPE` |
| `scripts/linux/install.sh`, `scripts/windows/install.ps1` | Target: perform offline installation, generate the configuration, and apply defaults on first install |
| `scripts/linux/start.sh`, `scripts/windows/start.ps1` | Target: run code-server in the foreground on the first free port in `127.0.0.1:8000-8079` (via `job_start`) |
| `scripts/linux/uninstall.sh`, `scripts/windows/uninstall.ps1` | Target: remove code-server (`--purge` / `-Purge` also removes settings, data, extensions, and the kit) |

Table: Folder layout

`TYPE` is `linux-amd64`, `linux-arm64`, or `windows-amd64`. Other targets (such as Windows arm64) are not supported. Linux arm64 has not been verified on physical hardware.

## Rules

- Never read, print, or paste the code-server password. `install` does not print it; copy the configuration file to the client with `transfer_pull` and tell the user its local path.
- Do not modify the user's registration command or reconnect on your own; show the user the required changes.
- Keep code-server bound to `127.0.0.1` so it is reachable only through the port forward.

## Procedure

### 1. Check prerequisites

1. Call `sys_get_info` and map `os`/`arch` to `TYPE` (`linux`/`amd64` -> `linux-amd64`, `linux`/`arm64` -> `linux-arm64`, `windows`/`amd64` -> `windows-amd64`). Stop and inform the user if the target is unsupported.
2. Verify that `forward_local` is `true` and that `bridge` publishes `forward_open`. If either is missing, stop and ask the user to add the following options to their registration command and reconnect (quote the glob so the shell does not expand it):
   - target: `--allow-forward-local` (and allow `127.0.0.1:80[0-7]?` if the target uses `--allow-host`)
   - bridge: `--forward-allow-local '127.0.0.1:80[0-7]?'` (ports 8000–8079)

   ```text
   host-shell bridge --forward-allow-local '127.0.0.1:80[0-7]?' -- ssh -T target /path/to/host-shell --mcp --allow-forward-local
   ```

3. The installation kit is transferred with `transfer_push`. If `bridge` does not publish `transfer_push` (for example, when started with `--no-transfer`; note that `bridge` refuses to start against a `--read-only` target unless `--no-transfer` is set), stop and inform the user.
4. If `restrictions` in `sys_get_info` is non-empty, explain that code-server's built-in terminal and file access bypass `host-shell` restrictions (`--read-only`, `--allow-command`, `--allow-path`, etc.) and are not recorded in the `host-shell` audit log. Proceed only after receiving the user's explicit approval.
5. On Linux targets, code-server requires glibc and a writable home directory; `install.sh` checks both before extracting. Musl-based distributions such as Alpine containers are not supported.
6. Ask the user which directory code-server should open (default: the target user's home directory, `home` in `sys_get_info`).

### 2. Fetch on the client

Run on the client (the machine running the agent and `bridge`):

```sh
"$SKILL"/scripts/linux/download.sh TYPE          # Linux and macOS clients (requires curl and python3)
```

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "$SKILL\scripts\windows\download.ps1" TYPE   # Windows clients
```

Already verified files are skipped quickly. Report any skipped extensions (warnings and `# skipped` lines in `packages/extensions/TYPE/resolved-extensions.txt`) to the user.

### 3. Send the kit

The kit directory on the target is `<home>/.local/share/host-shell-code-server/kit` on Linux and `<LOCALAPPDATA>\host-shell-code-server\kit` on Windows (retrieve `LOCALAPPDATA` with `exec_run` `echo $env:LOCALAPPDATA`). Use `transfer_push` with `overwrite: true` for each item:

| `local_path` | `remote_path` |
|---|---|
| `$SKILL/version.env` | `<kit>/version.env` |
| `$SKILL/defaults` | `<kit>/defaults` |
| `$SKILL/scripts/linux` and `$SKILL/scripts/lib` (Linux targets) or `$SKILL/scripts/windows` (Windows targets) | `<kit>/scripts/linux`, `<kit>/scripts/lib`, or `<kit>/scripts/windows` |
| `$SKILL/packages/code-server-<version>-TYPE.tar.gz` | `<kit>/packages/` |
| `$SKILL/packages/extensions/TYPE` | `<kit>/packages/extensions/TYPE` |

Table: Files to send

The release archive is roughly 210–230 MB and the default extensions are roughly 140 MB; let the user know the initial transfer takes some time. Unchanged files are skipped on subsequent runs.

### 4. Install

Run with `exec_run` and `timeout_sec: 900`:

- Linux: `argv: ["bash", "<kit>/scripts/linux/install.sh"]`
- Windows: `argv: ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "<kit>\\scripts\\windows\\install.ps1"]`

The script verifies and extracts the archive, creates the configuration file with a newly generated password (never printed), and on the first install copies default settings (only for files that do not yet exist) and installs default extensions (while the marker file is absent). Relay any errors—such as missing utilities (`tar`, `gzip`, `sha256sum`, `od`), missing glibc, or an unwritable `HOME`—directly to the user.

### 5. Copy the configuration to the client

Use `transfer_pull` to copy `<home>/.config/host-shell-code-server/config.yaml` (Windows: `<USERPROFILE>\.config\host-shell-code-server\config.yaml`) to `~/.config/host-shell-code-server/<target name>/config.yaml` on the client, and run `chmod 600` on it on Linux and macOS clients. Do not read the file contents.

### 6. Start and forward

1. Start code-server with `job_start` and `env: {"CODE_SERVER_WORKSPACE": "<folder>"}`:
   - Linux: `argv: ["bash", "<kit>/scripts/linux/start.sh"]`
   - Windows: `argv: ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "<kit>\\scripts\\windows\\start.ps1"]`
2. Wait with `job_read` (`wait_sec: 60`, `until: "HTTP server listening on"`) and extract the target port from the `HTTP server listening on http://127.0.0.1:<port>/` line. `start` selects the first free port in `8000`–`8079` (set `CODE_SERVER_PORT` in `env` to pin a specific port).
   - If the job exits early because another process claimed the port (`EADDRINUSE`), run `start` again.
   - If it exits with `no free port` or any other error, show its output to the user.
3. Call `forward_open` with `direction: "local"`, `host: "127.0.0.1"`, `port: <port>`, and `listen_port` omitted.
   - If `bridge` rejects the destination (for example, when registered with `127.0.0.1:8000` while port 8000 was already in use), stop code-server with `job_stop` and ask the user to update the rule to `--forward-allow-local '127.0.0.1:80[0-7]?'` (along with the target's `--allow-host`, if used) and reconnect.
4. Report the local URL `http://127.0.0.1:<listen_port>/` and the local path of the copied `config.yaml` (its `password:` line contains the login password).

Browser login sessions are scoped to the local port, so the user may need to log in again in a later session.

### 7. Extensions

- User-supplied `.vsix` file: upload it to the kit directory with `transfer_push`, then run `install` with `--extension <kit path>` (Windows: `-Extension`).
- Marketplace extension fetched via the client: see "Extensions" in `docs/en/code-server.md`.

### 8. Stop and remove

- Stop: call `forward_close` and `job_stop` (both also stop automatically when the MCP session ends).
- Remove: after stopping code-server with `job_stop`, run `uninstall.sh` (Windows: `uninstall.ps1`) via `exec_run`. Pass `--purge` (`-Purge`) only if the user also wants settings, user data, extensions, and the kit directory removed.

To reapply the initial defaults, delete `User/settings.json`, `Machine/settings.json`, or the `.host-shell-code-server-defaults` marker file in the user data directory, and run `install` again.
