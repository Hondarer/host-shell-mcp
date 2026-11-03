---
name: host-shell-vscode-remote-ssh
description: Connect the user's VS Code (Remote - SSH extension) to a Linux host-shell target, including closed networks without internet access, through a user-mode sshd on the target's loopback and host-shell port forwarding. Places the VS Code Server for the exact client commit offline, pins the agent host to it, and stops the server left by earlier sessions. Use for requests such as "open target in VS Code Remote - SSH", "update the VS Code Server on target after a VS Code update", or "remove the VS Code Server from target".
---

# VS Code Remote - SSH to a host-shell target

The target runs a second, user-mode `sshd` that listens on `127.0.0.1:2222` only. `host-shell` forwards a local port on the client to it, and VS Code Remote - SSH connects to that local port with a dedicated key. The VS Code CLI and server for the client's exact commit are fetched on the client, verified by SHA-256, and placed on the target, so the target needs no internet access.  
This skill operates through the `host-shell` MCP tools of a session started with `host-shell bridge`. To deploy `host-shell` itself, use the `host-shell-connect` skill. The target may also be a Linux container reached via `podman exec -i` or a similar command.

The user documentation is in `docs/en/vscode-remote-ssh.md` (the Japanese version `docs/ja/vscode-remote-ssh.md` is authoritative); read its verified scope before reporting what works.

Below, `$SKILL` refers to the directory containing this `SKILL.md` (`.agents/skills/host-shell-vscode-remote-ssh` in the repository), `<kit>` to `<home>/.local/share/host-shell-vscode-remote-ssh/kit` on the target, `NAME` to a short name for the target chosen with the user (letters, digits, `.`, `_`, `-`), and `LISTEN_PORT` to the local port VS Code connects to (default `42222`; one per target).

## When this applies

- The inner `sshd` gives the client a shell and port forwarding that bypass the system `sshd` policy and every `host-shell` restriction (`--read-only`, `--allow-command`, `--allow-path`, ...), and its actions are not in the `host-shell` audit log. Use this skill only where the target's administrator permits it. Before the first setup, explain this and proceed only with the user's explicit approval; repeat it when `restrictions` in `sys_get_info` is non-empty.
- Linux targets with glibc only (`x64` and `arm64`; arm64 is unverified). The target needs `/usr/sbin/sshd` and `ssh-keygen` (OpenSSH server package) but no running system `sshd` and no root.
- The machine running VS Code needs OpenSSH (`ssh`, `ssh-keygen`) and the Remote - SSH extension. It may differ from the machine running the agent and `bridge` (for example, VS Code on Windows and the agent in WSL); run the `ssh-key` script on the VS Code machine (from WSL: `powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$(wslpath -w "$SKILL/scripts/client/ssh-key.ps1")" ...`).

## Folder layout

| Path | Contents |
|---|---|
| `scripts/client/fetch.sh`, `scripts/client/fetch.ps1` | Client: fetch the CLI and server of one commit from the VS Code update service and verify them (`packages/<COMMIT>-<ARCH>/`) |
| `scripts/client/ssh-key.sh`, `scripts/client/ssh-key.ps1` | VS Code machine: create the dedicated key and known_hosts, and print the `~/.ssh/config` block |
| `scripts/linux/place.sh` | Target: verify and place one version, with the `code-<C>` wrapper that pins the agent host; `--prune` removes other versions |
| `scripts/linux/authorize.sh` | Target: register the client public key and print the inner `sshd` host key |
| `scripts/linux/start.sh` | Target: stop the server left by earlier sessions, regenerate the `sshd` configuration, and run `sshd` in the foreground (via `job_start`) |
| `scripts/linux/cleanup.sh` | Target: stop the VS Code Server of this user's `~/.vscode-server` and remove its temporary files; `--purge` also removes the server and this skill's data |
| `scripts/linux/status.sh` | Target: list placed versions and running servers, and warn when a server is not pinned to its client commit |
| `tests/scripts.test.sh` | Offline tests of the scripts; run them in a disposable container because `cleanup.sh` stops processes |
| `packages/` | Distributions fetched by `fetch` (not tracked in Git) |

Table: Folder layout

## Rules

- Do not edit the user's `~/.ssh/config`, VS Code settings, or `host-shell` registration command. Show the required lines and let the user apply them.
- Keep the inner `sshd` on `127.0.0.1`; only `start.sh` writes its configuration.
- Never print or paste the relay proxy URL (`HTTPS_PROXY`); it carries a session token. Never print private keys.
- Do not reconnect VS Code on the user's behalf; ask the user to connect, reload, or close windows.

## Procedure

### 1. Check prerequisites

1. Call `sys_get_info`. Stop unless `os` is `linux`; map `arch` `amd64` to `x64` and `arm64` to `arm64` (`ARCH`).
2. Check with `exec_run` `argv: ["sh", "-c", "test -x /usr/sbin/sshd && command -v ssh-keygen && ldd --version | head -n 1"]`. If `sshd` is missing, ask the user to have the OpenSSH server package installed (no service needs to run). Musl-based targets are not supported.
3. Verify that `forward_local` is `true` and that `bridge` publishes `forward_open` and `transfer_push`. Otherwise stop and ask the user to add these options and reconnect:
   - target: `--allow-forward-local` (and allow `127.0.0.1:2222` if the target uses `--allow-host`)
   - bridge: `--forward-allow-local '127.0.0.1:2222'`

   ```text
   host-shell bridge --forward-allow-local '127.0.0.1:2222' -- ssh -T target /path/to/host-shell --mcp --allow-forward-local
   ```

4. Ask the user for the VS Code commit: the second line of `code --version` on the machine running VS Code (40 hex digits). If the agent runs on that machine, run it yourself.

### 2. Fetch on the client

```sh
"$SKILL"/scripts/client/fetch.sh COMMIT ARCH          # Linux and macOS clients (curl, sha256sum or shasum)
```

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File "$SKILL\scripts\client\fetch.ps1" COMMIT ARCH   # Windows clients
```

The result is `$SKILL/packages/COMMIT-ARCH/` (about 226 MiB for x64). A folder that already verifies is kept.

### 3. Send and place

1. Check free space: `fs_stat` on `<home>` and read `available_bytes`. Placing needs about 1 GiB (240 MiB for the kit and 768 MiB for the extracted version). If less is free, stop and tell the user how much is free and needed.
2. `transfer_push` with `overwrite: true`:
   - `$SKILL/scripts/linux` -> `<kit>/scripts/linux`
   - `$SKILL/packages/COMMIT-ARCH` -> `<kit>/packages/COMMIT-ARCH`
3. `exec_run` with `timeout_sec: 600`: `argv: ["sh", "<kit>/scripts/linux/place.sh", "<kit>/packages/COMMIT-ARCH"]`. It verifies the SHA-256 values, checks the server commit, writes `~/.vscode-server/code-COMMIT` (wrapper), `~/.vscode-server/host-shell-cli/code-COMMIT` (CLI) and `~/.vscode-server/cli/servers/Stable-COMMIT/`, and puts the version first in `lru.json`. A version whose server is already placed keeps its server folder (only the wrapper and CLI are rewritten), so rerunning it is safe while that server runs. Placing a new version while an older client is connected does not affect the running server.
4. Remove `<kit>/packages/COMMIT-ARCH` with `fs_remove` (`recursive: true`) to free the space; the client keeps its copy.

### 4. Register the key (first time per target and VS Code machine)

1. On the VS Code machine: `ssh-key.sh key NAME` (Windows: `ssh-key.ps1 key NAME`). It creates `~/.ssh/host-shell-vscode-NAME_ed25519` when absent and prints the public key line.
2. `exec_run`: `argv: ["sh", "<kit>/scripts/linux/authorize.sh", "<public key line>"]`. It prints `host key: ssh-ed25519 ...`.
3. On the VS Code machine: `ssh-key.sh trust NAME LISTEN_PORT <target user> "<host key line>"` (Windows: `ssh-key.ps1 trust ...`). It writes `~/.ssh/host-shell-vscode-NAME_known_hosts` and prints a `Host host-shell-vscode-NAME` block.
4. Show the block to the user and ask them to add it to `~/.ssh/config` on the VS Code machine. Optionally they can add `"remote.SSH.remotePlatform": {"host-shell-vscode-NAME": "linux"}` to VS Code settings to skip the platform prompt.

`authorize.sh` without arguments prints the host key again. Rerun step 3 when `LISTEN_PORT` changes.

### 5. Start in every host-shell session

1. `job_start`: `argv: ["sh", "<kit>/scripts/linux/start.sh"]`. It stops the VS Code Server left by an earlier session (a running server keeps the relay proxy URL of the session it started in), regenerates the `sshd` configuration, and runs `sshd` in the foreground.
2. `job_read` with `wait_sec: 30` and `until: "Server listening on 127.0.0.1 port"`. If the job exits, show its output (for example, `Address already in use` when another `sshd` holds port 2222).
3. `forward_open` with `direction: "local"`, `listen_port: LISTEN_PORT`, `host: "127.0.0.1"`, `port: 2222`. If `LISTEN_PORT` is in use on the client, pick another, rerun step 4.3, and ask the user to update the block.
4. Ask the user to connect to `host-shell-vscode-NAME` in VS Code ("Remote-SSH: Connect to Host..."). If a window was connected before, VS Code shows "Cannot reconnect. Please reload the window."; the user chooses "Reload Window".
5. After the user connects, run `exec_run` `argv: ["sh", "<kit>/scripts/linux/status.sh"]`. Exit status 0 with `running server: Stable-COMMIT` means the client's version runs pinned. Report any `WARNING:` lines; they mean a server was downloaded or runs a commit other than its client's.
6. On the first connection after placing a fresh `~/.vscode-server` through the proxy, the server installs the client's language pack after the extension host started, so extension strings stay in English. If the user's VS Code is not in English, restart the `start.sh` job (`job_stop`, `job_start`) and ask the user to reload the window once.

### 6. Client access through the relay proxy (optional)

The VS Code Server, Copilot Chat in VS Code, and terminals reach the network only through the client relay proxy. Start the target with `--client-proxy` and the bridge with `--proxy` and `--proxy-allow-host` rules before step 5; `start.sh` passes the proxy to SSH sessions with `SetEnv` and sets `NO_PROXY=127.0.0.1,localhost,::1`. Rules used with this skill:

- Copilot: `github.com:443`, `api.github.com:443`, `*.githubcopilot.com:443`, `copilot-proxy.githubusercontent.com:443`
- Extensions from the Marketplace on the target: `marketplace.visualstudio.com:443`, `*.gallerycdn.vsassets.io:443`, `*.gallery.vsassets.io:443`, `main.vscode-cdn.net:443`

Placed versions are pinned by their wrapper, so the server does not contact `update.code.visualstudio.com`. Without the proxy, VS Code installs extensions on the target by downloading them on the client and relaying them, and Copilot does not work. DNS name resolution on the target (remote-dns) is not used with this skill; tools that resolve names on the target themselves (such as Copilot CLI's web fetch) are expected to fail (unverified).

### 7. After a VS Code update

1. Get the new commit (step 1.4) and run steps 2 and 3 for it. The old client may stay connected.
2. The user updates VS Code. VS Code applies a downloaded update only after all its windows close.
3. Run step 5 (`job_stop` the running `start.sh` job first if it is still running in this session, then `job_start` again) and let the user reconnect. `status.sh` must show the new commit.
4. After the user confirms it works, run `cleanup.sh`, then `exec_run` `argv: ["sh", "<kit>/scripts/linux/place.sh", "--prune", "NEWCOMMIT"]` to delete the other versions, and let the user reconnect.

Extensions and the language pack registration are kept across versions. When the user updates a language pack, the new translation is used after the server restarts (`cleanup.sh` and reconnect).

### 8. Remote - SSH downloads the server itself (fallback)

When the client cannot fetch distributions but the target may use the relay proxy, Remote - SSH can download the server through it. Add `update.code.visualstudio.com:443` and `vscode.download.prss.microsoft.com:443` to the bridge rules, and start with `argv: ["sh", "<kit>/scripts/linux/start.sh", "--commit", "COMMIT"]` so that the agent host does not run the latest stable server instead of the client's commit. Restart with the new commit after every client update. `status.sh` reports the pin from the `sshd` configuration.

### 9. End and remove

- End of a session: run `exec_run` `argv: ["sh", "<kit>/scripts/linux/cleanup.sh"]` before the session ends. The VS Code Server, its agent host, terminals, and programs started in them (such as Copilot CLI) otherwise keep running after `host-shell` exits. Then `forward_close` and `job_stop` (both also stop when the MCP session ends).
- Remove: `cleanup.sh --purge` deletes `~/.vscode-server` (placed versions, extensions, user data) and `<home>/.local/share/host-shell-vscode-remote-ssh` (kit, `sshd` configuration and keys). `~/.copilot` is kept because Copilot CLI shares it. On the VS Code machine, the user removes the `Host host-shell-vscode-NAME` block and `~/.ssh/host-shell-vscode-NAME_ed25519`, `.pub`, and `_known_hosts`.

## Limitations

- The inner `sshd` has no `Subsystem sftp`, so `sftp` and scp in SFTP mode are refused. Legacy scp (`scp -O`) runs through the shell and cannot be blocked while the `sshd` offers a shell.
- The agent host pin relies on the internal environment variable `VSCODE_CLI_INITIAL_AH_VERSION`. If a VS Code release stops honoring it, `status.sh` warns that a server runs another commit or has no CLI.
- `cleanup.sh` matches processes whose command line starts with this user's `~/.vscode-server` (and terminal shells started from it), and removes `code-*` and `vscode-*` entries owned by the user in `TMPDIR` (default `/tmp`) only when none of those processes remain. It leaves `/tmp/mcp-<random>/mcp.sock` (Copilot Chat's socket for Copilot CLI integration) alone because the name does not identify VS Code; do not delete such directories without asking the user.
- Use of the VS Code Server is subject to the license terms of the VS Code Server and Remote - SSH.
