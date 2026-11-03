---
name: host-shell-coding-agents
description: Prepare deployment and validation of CLI coding agents such as Copilot, agy, Claude Code, Codex, or Grok on a host-shell target, including code-server terminal use through the client HTTP proxy. Use for remote CLI deployment or validation, not ordinary local CLI use or code-server installation alone.
---

# CLI coding agents on host-shell targets

This is an initial workflow scaffold, not a verified installer. No CLI archives or provider-specific installation scripts are bundled. Only Copilot CLI has been validated on a target (an isolated Linux x86_64 container with code-server); other CLIs have only local version checks. See the guide's verified scope for verified and unverified stages, and report stages separately.

The target needs an SSH stdio route (or container exec), and the client needs access to the authentication and AI services. Direct target internet access is optional. Do not change target service configuration for this workflow.

## References and scope

Read [the deployment guide](../../../docs/en/coding-agents.md) for routing, authentication, and validation stages. The [Japanese version](../../../docs/ja/coding-agents.md) is authoritative. Use `host-shell-connect` if the connection itself needs preparation and `host-shell-code-server` if the browser editor needs installation or startup. code-server is optional: coding agents can also run in a regular SSH terminal.

Read the chosen CLI's official documentation for the actual distribution and version. Names such as `agy` and `grok` do not establish a vendor; confirm the source when unknown.

## Prepare deployment

Establish the selected CLI, distribution source, version, target route, destination directory, and terminal from the request and connected tools. Ask only for missing information that blocks the next stage; do not guess an SSH destination.

Use `sys_get_info` and `fs_stat` to check target OS, CPU, home directory, restrictions, proxy availability, and free space. Check libc and runtime requirements. Do not assume code-server's Node.js can run another CLI. Explain that CLI commands and file edits bypass host-shell's MCP tool restrictions and approvals. Follow the code-server skill's restrictions procedure when using that skill; do not weaken existing restrictions.

Fetch target-compatible artifacts on the client. Record source, version, size, and SHA-256, and verify official checksums or signatures when available. Resolve symbolic links and include companion files. npm entry points need their runtime and target-compatible dependencies.

Use `transfer_push` into a dedicated user-owned directory, for example `<home>/.local/share/host-shell-coding-agents/<CLI>/<version>/` on Linux. Verify hashes on the target. Initially use absolute paths rather than changing PATH, shell startup files, or existing installations.

This scaffold supplies no fixed download URLs or installation commands. If provenance or dependencies cannot be established, report what is missing before deploying that artifact. Do not substitute another CLI or execute an unverified installer.

## Route traffic and authenticate

For targets without direct egress, use the [client HTTP proxy](../../../docs/en/client-proxy.md) with explicit destinations needed by the selected CLI and authentication method. Enable it before starting code-server so terminal children inherit proxy variables. Check `NO_PROXY` and CLI-specific authenticated proxy support without printing credential-bearing URLs.

If registration options are missing, show the required changes; do not modify registration or reconnect automatically. Do not broaden destination rules beyond authorized scope. Use `proxy_status` to distinguish login, token refresh, inference, and optional download destinations. If `proxy_rules_update` is exposed, apply user-authorized changes without reconnecting by submitting both complete lists and the observed `rules_revision`; otherwise show the required startup-option change. Runtime rule updates require a bridge started with `--proxy-allow-rule-updates`, last only for that session, and do not disconnect existing tunnels. A denied destination (HTTP 403 or CONNECT 403) is not a DNS failure. The HTTP proxy resolves hostnames on the client route; it does not relay target OS DNS queries.

Built-in tools that resolve names on the target before connecting, such as Copilot CLI's built-in Web Fetch (`WebFetchBlockedUrlError: failed to lookup address information`), fail on targets without DNS; do not claim an allowlist change alone fixes them. For Linux/glibc applications that call `getaddrinfo` before using the proxy, see [remote-dns](../../../docs/en/remote-dns.md): enable `--remote-dns` on both ends before starting code-server and check `sys_get_info.remote_dns`. The `host-shell-connect` skill deploys the CI-built adapter beside the binary and gives the `--remote-dns-library` path (otherwise the target needs `cc` and glibc headers). The feature permits arbitrary client-side DNS lookups independently of HTTP proxy destination rules. VS Code removes `LD_PRELOAD` in extension and agent hosts, including the bundled Copilot SDK; for user-requested coverage there, use the code-server skill's explicit patched-copy mode (`HOST_SHELL_CODE_SERVER_REMOTE_DNS=1`). Do not remove the sanitizer or restore arbitrary preload libraries. Static binaries and Node.js `dns.resolve*` are outside its scope. A proxy-compatible curl request to a user-authorized destination is a separate fallback.

Copilot Chat BYOK model discovery also needs user-authorized `main.vscode-cdn.net:443` for `https://main.vscode-cdn.net/extensions/copilotChat.json`; GitHub/Copilot API rules alone do not cover it. Use the session rule-update procedure when exposed, and record `--proxy-allow-host main.vscode-cdn.net:443` for future starts. Distinguish direct catalog retrieval from the extension's own retry, and do not add unrelated telemetry or experimentation hosts to address the BYOK model-list update warning. The guide lists the verified Copilot destinations.

Have the user complete a supported remote authentication flow. For Copilot CLI, use `/login` → `GitHub.com` → `Sign in with a device code`; the browser option returns to the target's localhost. If it then reports `System vault not available` and asks to store the token in plain text, choose `1. Yes` on disposable validation targets (declining cancels sign-in), report that the token is in `~/.copilot/config.json` (mode 600), and ask the user first on shared or long-lived targets. Record login success separately from long-term authentication maintenance; do not leave a successful login marked incomplete merely because expiry handling has not been tested. Device authorization still needs target-to-authentication-service traffic. Browser loopback callbacks may require a separate local forward. Keep keys, tokens, and credential files out of tool arguments, conversation output, and validation logs. Do not copy a local home directory or credential store.

## Validate incrementally

1. Run `--version` and `--help` through `exec_run`; use `job_start` with `pty: true` for terminal checks. Bound unattended commands with a timeout.
2. In the actual code-server terminal, check rendering, input, Ctrl-C, resizing, and copying outside the browser. A host-shell PTY version check does not validate browser terminal behavior. If copying works in an ordinary terminal but Copilot shows `Copied` without updating the clipboard, keep mouse support enabled and run `Terminal: Select All` followed by `Terminal: Copy Selection` from the Command Palette, then trim the pasted text. In Copilot CLI this copies only the area currently on screen (scroll with the wheel first to copy another part); an ordinary shell copies its scrollback too. Shift+drag with Ctrl+Shift+C does not work while mouse support is enabled. `/share` export suits partial copies and output longer than the screen. `--no-mouse` also restores copying but degrades wheel scrolling; treat it as a fallback. Verify an actual paste rather than relying on the notification, and do not apply this workaround to unrelated clipboard failures.
3. After user authentication, run a small inference request in a dedicated test workspace. Claim proxy-only operation only after verifying direct target egress is blocked and observing the bridge route.
4. When requested, check a small file edit and command execution in that workspace with normal CLI approval handling.
5. For long-term validation, check authentication maintenance across expiry when applicable; do not assume every authentication method uses refresh tokens. When relevant, check multiple MCP connections under the same target user. Record shared credentials, history, and helpers instead of assuming isolation.
6. In a disposable validation session, record PIDs and listeners, close that session, and inspect shutdown through another connection. Do not disconnect unrelated sessions or kill by executable name alone. An interactive CLI in a code-server terminal exits with the terminal even though the terminal shell has its own session; descendants detached from the terminal (for example `nohup` or `setsid`) may survive. In containers whose PID 1 does not reap children (such as `sleep infinity`), exited processes remain as `<defunct>` entries; do not report them as running.

If a stage fails, retain evidence for previously verified stages and report the failed stage and cause. Do not disable CLI sandboxes or approvals merely to obtain a pass. Files, credentials, and history persist after disconnect; remove them only within the requested scope.

## Report

Record route, OS/CPU/libc, host-shell and code-server versions, CLI source/version/hash, destination, authentication method without secrets, allowed destinations, verified stages, and failures. Clearly identify pending remote verification. Add provider-specific recipes after observing them work on the stated platform. When updating documents, follow `AGENTS.md`: put results in the guide's verified scope and the history in `docs/prompts/past/`.
