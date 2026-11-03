# Client-side name resolution (remote-dns)

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/remote-dns.md](../ja/remote-dns.md)) is the single source of truth (SSOT) and takes precedence if they differ.

Relay Linux/glibc target processes' `getaddrinfo` calls over the existing MCP connection to the connecting machine. This optional feature needs no root access or changes to `/etc/resolv.conf`, `/etc/hosts`, or service configuration. It is disabled by default.

```mermaid
flowchart LR
    Process["Target: exec_run / job_start<br/>code-server and inheriting children"] --> Hook["LD_PRELOAD<br/>getaddrinfo"]
    Hook --> Socket["Per-session Unix socket<br/>owner-only access"]
    Socket --> Channel["Existing MCP / SSH connection<br/>dedicated DNS channel"]
    Channel --> Resolver["Client: Go standard resolver"]
    Resolver --> DNS["Client DNS / hosts"]
```

## Use cases

| Use case | Features |
|---|---|
| The target has no DNS and a program resolves names itself before connecting (for example, Copilot CLI's built-in Web Fetch fails with `failed to lookup address information`) | remote-dns |
| Web access from a target with neither DNS nor outbound connectivity | remote-dns and the [client HTTP proxy](client-proxy.md) |
| Client-side resolution also in code-server extension and agent hosts (such as the in-editor Copilot Chat) | The above plus the [code-server patched-copy mode](#extension-and-agent-hosts-in-code-server) |

Table: Use cases

Proxy-aware programs (such as curl) pass the destination hostname to the proxy, which resolves it on the client side. remote-dns is not needed for proxy traffic alone.

## Enable

Pass `--remote-dns` on both the client (`bridge`) and the target. The target needs the name resolution adapter (a shared library).

| Adapter source | Target options | Requirement |
|---|---|---|
| Placed by the host-shell-connect skill's `deploy.sh` (recommended) | `--remote-dns --remote-dns-library <printed path>` | The adapter exists in the skill's `bin/` (included in release archives) |
| `dist/libhostshell_dns-linux-<arch>.so` from a release archive, transferred manually | `--remote-dns --remote-dns-library <absolute path>` | Matches the target CPU |
| Built on the target | `--remote-dns` only | `cc` and glibc development headers on the target |

Table: Adapter sources

### With the skill

When `deploy.sh` places the binary on a glibc Linux target, it also places the matching adapter beside it as `libhostshell_dns.so`. It checks that the target can actually `LD_PRELOAD` it and prints `dns adapter deployed: <absolute path>`. The skill presents a registration command with that path as `--remote-dns-library`. Users do not need to prepare the adapter.

```bash
host-shell bridge --remote-dns -- \
  ssh -T target /tmp/host-shell-1000/host-shell --mcp --remote-dns \
    --remote-dns-library /tmp/host-shell-1000/libhostshell_dns.so
```

### Manual preparation

Release archives include `libhostshell_dns-linux-amd64.so` and `libhostshell_dns-linux-arm64.so` in `dist/` and in the host-shell-connect skill's `bin/`. CI builds them on AlmaLinux 8 (glibc 2.28) and checks that they require only glibc 2.17 symbols or older. Transfer the one matching the target CPU and pass its absolute path to `--remote-dns-library`.

To build from source, run `make dns-lib` on a system with the same CPU architecture and a glibc version no newer than the target's.

Without `--remote-dns-library`, the target compiles the adapter from the embedded C source with `cc` at session startup.

### Behavior when enabled

- `exec_run` and `job_start` receive `LD_PRELOAD`, `HOST_SHELL_DNS_LIBRARY`, and `HOST_SHELL_DNS_SOCKET` automatically. Existing preload libraries are preserved. Policy `--unset-env` filtering and explicit call `env` overrides apply afterwards.
- Inspect `remote_dns`, `remote_dns_library`, and `remote_dns_socket` in `sys_get_info`.
- The adapter and owner-only socket live in a private per-session temporary directory removed on disconnect. The original passed with `--remote-dns-library` is retained.
- A target started with `--remote-dns` without a bridge stops with `bridge must enable --remote-dns`.

## Combine with the HTTP proxy

Name resolution does not create a network route. For web access from targets without outbound connectivity, also enable the [client HTTP proxy](client-proxy.md):

```bash
host-shell bridge --remote-dns --proxy \
  --proxy-allow-host api.github.com:443 -- \
  ssh -T target /path/to/host-shell --mcp --remote-dns --client-proxy
```

Start code-server with `job_start` in this session. Existing processes keep their environment. No Python service, second MCP session, or remote port forward is required.

DNS queries are not filtered by HTTP proxy allow or deny rules. `--remote-dns` permits arbitrary client-side resolution. HTTP/HTTPS destinations remain restricted by proxy rules; resolving a name does not bypass a 403 denial.

## Extension and agent hosts in code-server

VS Code removes `LD_PRELOAD` when starting extension and agent hosts. Setting the adapter on the code-server parent does not reach the in-editor Copilot Chat or the bundled Copilot SDK (`copilot-runtime`). The Linux code-server skill's `start.sh` offers an explicitly selected patched-copy mode. Normal startup uses the original distribution.

### Procedure

1. Start `host-shell` at both ends with `--remote-dns`. For web access, also enable the client HTTP proxy and the required destination rules.
2. Install code-server on the target (if code-server is already installed with an older `<kit>/scripts/linux`, transfer the skill's `scripts/linux` to update it; the installed code-server distribution can be reused as is).
3. Start `start.sh` with `job_start` and set `HOST_SHELL_CODE_SERVER_REMOTE_DNS: "1"` in `env`. `<kit>` is the absolute path of the deployed kit.

   ```json
   {
     "argv": ["bash", "<kit>/scripts/linux/start.sh"],
     "env": {
       "HOST_SHELL_CODE_SERVER_REMOTE_DNS": "1",
       "CODE_SERVER_WORKSPACE": "/absolute/path/to/workspace"
     }
   }
   ```

`start.sh` uses normal user data. To check alongside an existing instance, run the patched copy's `bin/code-server` directly with a separate port, `--user-data-dir`, and `--session-socket`, and pass the same activation variable. Do not share a running user-data directory.

Stop with `job_stop` for the job and `forward_close` for the forward. To return to the normal distribution, omit `HOST_SHELL_CODE_SERVER_REMOTE_DNS` or set it to `0` on the next `start.sh` launch. Existing processes keep their environment. The patched copy persists after the session and is removed by normal code-server uninstallation.

### How the patched copy works

The first launch creates an independent `code-server-4.144.0-remote-dns-v1` copy beside the original. Reserve about 750 MiB. Free space is checked before patching, and the copy is moved into its final directory name only after JavaScript syntax checks pass. The original distribution and running sessions are not modified. Restarts verify the manifest and patched hashes rather than recreating the copy. Any modification is rejected rather than repaired.

Supported inputs are official code-server 4.144.0 / VS Code 1.144.0 Linux amd64 and arm64 distributions. Patching requires matching the SHA-256 hashes of the three target files and the four target strings to be patched. Unknown versions, modified files, and duplicate patches are rejected.

```mermaid
flowchart LR
    Env["Create child environment"] --> Sanitize["Existing VS Code sanitization"]
    Sanitize --> Restore["Explicit mode only<br/>restore session DNS adapter"]
    Restore --> Fork["Start extension host / IPC child"]
    Fork --> Load["Dynamic loader maps the adapter"]
```

The patch is inserted immediately before child launches in `out/server-main.js`, `out/vs/workbench/api/node/extensionHostProcess.js`, and `out/vs/platform/agentHost/node/agentHostMain.js`. It retains removal of `NODE_OPTIONS` and other variables and never restores the original `LD_PRELOAD` wholesale. It uses inherited `HOST_SHELL_DNS_LIBRARY` and `HOST_SHELL_DNS_SOCKET` and sets only the same session's adapter. Values from another session, unsafe ownership or permissions, symbolic links, and stale sockets after disconnect are rejected.

Browser Web extensions, custom DNS APIs, and extension-specific launch paths that remove the variable themselves are not covered.

## Diagnostics

Check name resolution, environment inheritance, and HTTP proxy destination rules separately.

| Symptom or check | Interpretation |
|---|---|
| `getaddrinfo` errors (`failed to lookup address information`, `ENOTFOUND`, `EAI_AGAIN`) | Name resolution failed. Check `sys_get_info.remote_dns` and that `LD_PRELOAD` reaches the process |
| HTTP 403, `CONNECT tunnel failed, response 403` | A proxy destination denial, not a DNS failure. Check the reason with `proxy_status` |
| Ambiguous messages such as `a transport-level failure (DNS, connection, or TLS handshake)` | Do not conclude DNS failure from the message alone; check the actual denial reason with `proxy_status` |
| Whether a process loaded the adapter | Check the PID's environment and whether `/proc/<pid>/maps` contains `libhostshell_dns` |
| Queries passing through the hook | Setting `HOST_SHELL_DNS_TRACE=1` at startup logs host names and address families to standard error. In code-server patched-copy mode, entries from `copilot-runtime` appear in the code-server output as `[CLI subprocess] host-shell-dns: getaddrinfo <host> family=<n>` (with the copy relayed by the agent host, one query produces two lines). Entries carry no time, so match them to an action by comparing the output before and after it |

Table: Diagnostic checks

Use the same allowed URL (for example `https://api.github.com/zen`) to separate HTTP 403 from resolution failures.

## Verified scope

"OL10 container" in the table is an Oracle Linux 10.2 / x86_64 / glibc 2.39 Podman container (`--network none`, no external route) reached over SSH.

| Area | Verified behavior | Method and environment |
|---|---|---|
| Resolution relay | IPv4/IPv6, canonical names, service names, numeric IPs, localhost, NXDOMAIN, `AI_NUMERICHOST`, relay-unavailable errors, 24 concurrent lookups, child inheritance | Automated tests, OL10 container |
| `exec_run` and `job_start` processes | Name resolution succeeds | OL10 container |
| With the HTTP proxy | HTTPS to an allowed destination succeeds; an unlisted destination resolves and is denied with 403 | OL10 container |
| Disconnect cleanup | The temporary directory and running jobs are reclaimed | Automated tests, OL10 container |
| Building on the target | Compiling and loading the embedded source | OL10 container |
| Prebuilt adapters | Loading and hook behavior succeed for amd64 and arm64 on glibc 2.28 and a newer glibc | CI (AlmaLinux 8 container and GitHub runners) |
| Adapter placement by `deploy.sh` | Placement, hash comparison on update, load check, musl exclusion, removal of unloadable adapters | Podman containers (Oracle Linux 10, Oracle Linux 8, Alpine) |
| Invalid adapters | Adapters with a wrong format, architecture, or version are rejected | Automated tests |
| Copilot CLI (terminal) | Built-in Web Fetch fails to resolve without remote-dns and succeeds with it | OL10 container, Copilot CLI 1.0.83 |
| Node.js | `dns.lookup()` succeeds; `dns.resolve4()` is out of scope (ECONNREFUSED) | OL10 container |
| code-server patched-copy mode | Original files retained, modified files rejected, cleanup after syntax errors, unchanged behavior when disabled, two-session isolation, rejection of unsafe permissions, links, and stale sockets | Automated tests |
| Patched-copy mode in operation | Adapter loaded into the extension host, agent host, and bundled `copilot-runtime`; resolution in a Chat-launched Node child; terminal Copilot CLI Web Fetch; in-editor Copilot Chat responses | OL10 container, code-server 4.144.0 |
| In-editor Copilot Chat, Copilot harness (Copilot SDK) | When the built-in `web_fetch` runs, `copilot-runtime`, a child of the agent host, resolves the destination through the hook, and HTTPS succeeds through the proxy. Each fetch adds exactly one trace entry | OL10 container, code-server 4.144.0, UI driven from Chrome for Testing 148 |
| In-editor Copilot Chat, Local harness | code-server provides no web page fetch tool (the `web` group contains only `githubRepo` and `githubTextSearch`). Extension host traffic goes through the HTTP proxy, and no queries through the hook are observed | Same as above |

Table: Verified scope

Unverified:

- Name resolution by Copilot SDK internals other than `web_fetch`, and by extensions in the extension host directly
- Patched-copy mode on arm64 (archive inspection only)
- Browser Web extensions

The validation history is in [261102-remote-dns-validation.md](../prompts/past/261102-remote-dns-validation.md) and [261103-copilot-chat-remote-dns-validation.md](../prompts/past/261103-copilot-chat-remote-dns-validation.md) (Japanese only).

Development validation commands (Linux, Node.js required):

```bash
node --test .agents/skills/host-shell-code-server/tests/remote-dns.test.mjs
go test ./internal/remotedns
```

## Scope and limitations

- Targets require Linux/glibc; the bridge supports host-shell's supported operating systems. musl (such as Alpine), static binaries, privileged execution ignoring `LD_PRELOAD`, custom DNS implementations, Node.js `dns.resolve*`, reverse lookups, and `gethostbyname` are out of scope.
- Numeric IPs, `localhost`, and `AI_NUMERICHOST` use target libc. Other target-specific hosts entries are replaced by client resolution.
- The client uses Go's standard resolver, not every OS libc/NSS behavior. It supports IPv4/IPv6, canonical names, and service names, but is not a complete libc replacement.
- Each query is limited to about 3 seconds. Unavailable relays fail closed rather than falling back to target DNS. There is no cache. The socket is available to the same user.
