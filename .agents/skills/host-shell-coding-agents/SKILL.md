---
name: host-shell-coding-agents
description: Prepare deployment and validation of CLI coding agents such as Copilot, agy, Claude Code, Codex, or Grok on a host-shell target, including code-server terminal use through the client HTTP proxy. Use for remote CLI deployment or validation, not ordinary local CLI use or code-server installation alone.
---

# CLI coding agents on host-shell targets

This is an initial workflow scaffold, not a verified installer. No CLI archives or provider-specific installation scripts are bundled. Local version checks have passed. Copilot CLI 1.0.83 has also been deployed to an isolated Linux x86_64 container with code-server 4.144.0, and the user confirmed successful device code login by entering the CLI-displayed code in a separate browser. Conversational responses were observed in a user screenshot, but built-in Web Fetch failed on target-side DNS. Long-term authentication behavior and shutdown remain unverified; see the deployment guide for evidence and limits. Report these stages separately.

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

If registration options are missing, show the required changes; do not modify registration or reconnect automatically. Do not broaden destination rules beyond authorized scope. Use `proxy_status` to distinguish login, token refresh, inference, and optional download destinations. If `proxy_rules_update` is exposed, apply user-authorized changes without reconnecting by submitting both complete lists and the observed `rules_revision`; otherwise show the required startup-option change. Runtime rule updates require a bridge started with `--proxy-allow-rule-updates`, last only for that session, and do not disconnect existing tunnels. A denied destination (HTTP 403 or CONNECT 403) is not a DNS failure. The HTTP proxy resolves hostnames on the client route; it does not relay target OS DNS queries. Separate direct target lookup failures from applications that resolve names before using the proxy. Copilot 1.0.83 built-in Web Fetch was observed failing with `WebFetchBlockedUrlError: failed to lookup address information` in the network-none container, while curl reached the proxy and was denied for an unlisted destination. Do not claim an allowlist change alone fixes built-in Web Fetch. A proxy-compatible curl request to a user-authorized destination is a separate fallback. For Linux/glibc applications that call `getaddrinfo` before using the proxy, see [remote-dns](../../../docs/en/remote-dns.md): enable `--remote-dns` on both ends before starting code-server. Check `sys_get_info.remote_dns`; the target needs `cc` and glibc headers or a compatible adapter supplied with `--remote-dns-library`. The feature injects `LD_PRELOAD` and `HOST_SHELL_DNS_SOCKET` into executed commands and inheriting children, and cleans its socket and library on disconnect. It permits arbitrary client-side DNS lookups independently of HTTP proxy destination rules. VS Code sanitizes `LD_PRELOAD` in some extension/agent hosts, including the observed bundled Copilot SDK path; do not claim all VS Code processes are covered or disable sanitization automatically. Static binaries and Node.js `dns.resolve*` are outside its scope.

Have the user complete a supported remote authentication flow. Record login success separately from long-term authentication maintenance; do not leave a successful login marked incomplete merely because expiry handling has not been tested. Device authorization still needs target-to-authentication-service traffic. Browser loopback callbacks may require a separate local forward. Keep keys, tokens, and credential files out of tool arguments, conversation output, and validation logs. Do not copy a local home directory or credential store.

## Validate incrementally

1. Run `--version` and `--help` through `exec_run`; use `job_start` with `pty: true` for terminal checks. Bound unattended commands with a timeout.
2. In the actual code-server terminal, check rendering, input, Ctrl-C, resizing, and copying outside the browser. A host-shell PTY version check does not validate browser terminal behavior. If copying works in an ordinary terminal but Copilot shows `Copied` without updating the clipboard, the user-confirmed preferred workaround on Copilot 1.0.83 / code-server 4.144.0 / Edge is to keep mouse support enabled and run `Terminal: Select All` followed by `Terminal: Copy Selection` from the Command Palette, then trim the pasted text (the whole terminal is copied). This preserves normal wheel scrolling. Shift+drag with Ctrl+Shift+C does not work while mouse support is enabled; an earlier note claiming it did was wrong. `--no-mouse` also restored copying, but the user reported broken wheel scrolling and other usability issues; treat it as a fallback, not the default. Exporting with `/share` and copying from the editor was also confirmed to work and suits partial or long copies. Verify an actual paste rather than relying on the notification. See the guide for upstream reports; do not apply this workaround to unrelated clipboard failures automatically.
3. After user authentication, run a small inference request in a dedicated test workspace. Claim proxy-only operation only after verifying direct target egress is blocked and observing the bridge route.
4. When requested, check a small file edit and command execution in that workspace with normal CLI approval handling.
5. For long-term validation, check authentication maintenance across expiry when applicable; do not assume every authentication method uses refresh tokens. When relevant, check multiple MCP connections under the same target user. Record shared credentials, history, and helpers instead of assuming isolation.
6. In a disposable validation session, record PIDs and listeners, close that session, and inspect shutdown through another connection. Do not disconnect unrelated sessions or kill by executable name alone. Unix descendants that leave their process group or session may survive.

If a stage fails, retain previous passing evidence and report the failed stage and cause. Do not disable CLI sandboxes or approvals merely to obtain a pass. Files, credentials, and history persist after disconnect; remove them only within the requested scope.

## Report

Record route, OS/CPU/libc, host-shell and code-server versions, CLI source/version/hash, destination, authentication method without secrets, allowed destinations, passed stages, and failures. Clearly identify pending remote verification. Add provider-specific recipes after observing them work on the stated platform.
