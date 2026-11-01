---
name: host-shell-code-server
description: Inject code-server into a host-shell target (including closed networks without internet access), start it on the target's loopback, and open it in the user's browser through host-shell port forwarding. On the first install it also injects default settings and extensions. Use for requests such as "put code-server on target", "open VS Code in the browser for target", or "remove code-server from target".
---

# Injecting code-server into a host-shell target

Install code-server on the target from files fetched on the client, start it on `127.0.0.1` of the target, and forward a client port to it.  
The target needs no internet access; everything is verified by SHA-256 on the client and again on the target.  
This skill works through the host-shell MCP tools of a session started with `host-shell bridge`. To deploy host-shell itself, use the host-shell-connect skill.

Below, `$SKILL` refers to the directory containing this `SKILL.md` (`.agents/skills/host-shell-code-server` in the repository). The user documentation is docs/en/code-server.md.

## Folder layout

| Path | Contents |
|---|---|
| `version.env` | code-server version, its VS Code version, the target port (8000), and the SHA-256 of each release archive |
| `defaults/User/settings.json`, `defaults/Machine/settings.json` | Settings placed on the first install |
| `defaults/extensions.txt` | Default extensions (`publisher.name` or `publisher.name@version`, one per line) |
| `packages/` | Release archives and `extensions/<TYPE>/` (VSIX files and `resolved-extensions.txt`) fetched by `download` (not in Git) |
| `scripts/linux/download.sh`, `scripts/windows/download.ps1` | Client: fetch and verify the archive and default extensions for each target `TYPE` |
| `scripts/linux/install.sh`, `scripts/windows/install.ps1` | Target: install offline, create the configuration, inject defaults on the first install |
| `scripts/linux/start.sh`, `scripts/windows/start.ps1` | Target: run code-server in the foreground on `127.0.0.1:8000` (with `job_start`) |
| `scripts/linux/uninstall.sh`, `scripts/windows/uninstall.ps1` | Target: remove code-server (`--purge` / `-Purge` also removes settings, data, extensions and the kit) |

Table: Folder layout

`TYPE` is `linux-amd64`, `linux-arm64` or `windows-amd64`. Other targets (such as Windows arm64) are not supported. Linux arm64 has not been verified on a real machine.

## Rules

- Never read, print or paste the code-server password. `install` does not print it; copy the configuration file to the client with `transfer_pull` and give the user its path.
- Do not change the user's registration command or reconnect on your own; tell the user what to change.
- Keep code-server on `127.0.0.1`; it is reached only through the forward.

## Procedure

### 1. Check prerequisites

1. Call `sys_get_info`. Map `os`/`arch` to `TYPE` (`linux`/`amd64` -> `linux-amd64`, `linux`/`arm64` -> `linux-arm64`, `windows`/`amd64` -> `windows-amd64`). Stop and tell the user if there is no match.
2. Check that `forward_local` is `true` and that the bridge offers `forward_open`. If either is missing, stop and tell the user to add these options to the registration command and reconnect:
   - target: `--allow-forward-local` (and allow `127.0.0.1:8000` if the target uses `--allow-host`)
   - bridge: `--forward-allow-local 127.0.0.1:8000`

   ```text
   host-shell bridge --forward-allow-local 127.0.0.1:8000 -- ssh -T target /path/to/host-shell --mcp --allow-forward-local
   ```

3. The kit is sent with `transfer_push`. If the bridge does not offer it (bridge `--no-transfer`; a bridge refuses to start with a `--read-only` target unless `--no-transfer` is given), stop and tell the user.
4. If `restrictions` in `sys_get_info` is not empty, explain that code-server's terminal and file access bypass host-shell's restrictions (such as `--read-only`, `--allow-command`, `--allow-path`) and are not recorded in host-shell's audit log. Continue only with the user's explicit approval.
5. Ask the user which folder code-server should open (default: the home directory, `home` in `sys_get_info`).

### 2. Fetch on the client

Run on the client (the machine running the agent and the bridge):

```sh
"$SKILL"/scripts/linux/download.sh TYPE          # Linux and macOS clients (needs curl and python3)
```

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "$SKILL\scripts\windows\download.ps1" TYPE   # Windows clients
```

It is skipped quickly when the files are already verified. Report extensions it skipped (warnings, and `# skipped` lines in `packages/extensions/TYPE/resolved-extensions.txt`) to the user.

### 3. Send the kit

The kit folder on the target is `<home>/.local/share/host-shell-code-server/kit` on Linux and `<LOCALAPPDATA>\host-shell-code-server\kit` on Windows (get `LOCALAPPDATA` with `exec_run` `echo $env:LOCALAPPDATA`). Use `transfer_push` with `overwrite: true` for each of:

| `local_path` | `remote_path` |
|---|---|
| `$SKILL/version.env` | `<kit>/version.env` |
| `$SKILL/defaults` | `<kit>/defaults` |
| `$SKILL/scripts/linux` and `$SKILL/scripts/lib` (Linux targets) or `$SKILL/scripts/windows` (Windows targets) | `<kit>/scripts/linux`, `<kit>/scripts/lib`, or `<kit>/scripts/windows` |
| `$SKILL/packages/code-server-<version>-TYPE.tar.gz` | `<kit>/packages/` |
| `$SKILL/packages/extensions/TYPE` | `<kit>/packages/extensions/TYPE` |

Table: Files to send

The archive is about 210 to 230 MB and the default extensions about 140 MB; tell the user the transfer takes a while. Unchanged files are skipped on later runs.

### 4. Install

Run with `exec_run` and `timeout_sec: 900`:

- Linux: `argv: ["bash", "<kit>/scripts/linux/install.sh"]`
- Windows: `argv: ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "<kit>\\scripts\\windows\\install.ps1"]`

It verifies and extracts the archive, creates the configuration with a new password (never printed), and on the first install places the default settings (only files that do not exist) and installs the default extensions (only while the marker file is absent). Show errors, such as missing commands (`tar`, `gzip`, `sha256sum`, `od`), to the user as they are.

### 5. Copy the configuration to the client

Use `transfer_pull` from `<home>/.config/host-shell-code-server/config.yaml` (Windows: `<USERPROFILE>\.config\host-shell-code-server\config.yaml`) to `~/.config/host-shell-code-server/<target name>/config.yaml` on the client, then `chmod 600` it on Linux and macOS clients. Do not read the file.

### 6. Start and forward

1. Start with `job_start`, with `env: {"CODE_SERVER_WORKSPACE": "<folder>"}`:
   - Linux: `argv: ["bash", "<kit>/scripts/linux/start.sh"]`
   - Windows: `argv: ["powershell", "-NoProfile", "-ExecutionPolicy", "Bypass", "-File", "<kit>\\scripts\\windows\\start.ps1"]`
2. Wait with `job_read` (`wait_sec: 60`, `until: "HTTP server listening on"`). If the job ends, show its output to the user.
3. Call `forward_open` with `direction: "local"`, `host: "127.0.0.1"`, `port: 8000` and no `listen_port`.
4. Give the user `http://127.0.0.1:<listen_port>/` and the path of the copied configuration (its `password:` line is the login password).

The browser login is kept per port, so the user may need to log in again in a later session.

### 7. Extensions

- A `.vsix` the user provides: `transfer_push` it into the kit, then run `install` with `--extension <kit path>` (Windows: `-Extension`).
- From the marketplace through the client: see "Extensions" in docs/en/code-server.md.

### 8. Stop and remove

- Stop: `forward_close` and `job_stop`. Both also end with the MCP session.
- Remove: after `job_stop`, run `uninstall.sh` (Windows: `uninstall.ps1`) with `exec_run`. Add `--purge` (`-Purge`) only if the user wants settings, data, extensions and the kit removed too.

To apply the defaults again, delete `User/settings.json`, `Machine/settings.json` or the marker `.host-shell-code-server-defaults` in the user data folder, then run `install` again.
