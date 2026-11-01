# HTTP proxy through the client

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/client-proxy.md](../ja/client-proxy.md)) is the single source of truth (SSOT) and takes precedence if they differ.

When a target cannot access external services directly, its HTTP proxy aware applications can use the client bridge's network. Traffic shares the existing SSH standard input/output session; SSH port forwarding is unnecessary. The feature is disabled by default. For other TCP traffic or the opposite direction, use [port forwarding](port-forward.md).

The feature does not use sshd forwarding, so sshd `no-port-forwarding` or `AllowTcpForwarding no` cannot stop it. It is available only when the target is started with `--client-proxy`.

## Startup

Explicitly enable both peers and provide destination rules on the bridge.

    host-shell bridge --proxy --proxy-allow-host 'packages.example.com:443' -- \
      ssh -T target /path/to/host-shell --mcp --client-proxy

Use --proxy and repeatable --proxy-allow-host on the bridge, and --client-proxy on the target. At least one rule is required. One-sided enablement, incompatible versions, or invalid rules fail startup. --no-transfer can be used independently.

    Target application → target 127.0.0.1 HTTP proxy
      → SSH stdio (JSON/Base64) → bridge → external server

HTTP forwarding and CONNECT are supported. TLS is neither decrypted nor re-signed. Targets support Linux/Windows; bridges support Linux/Windows/macOS.

## Routing and restrictions

Rules match host globs with an optional port: packages.example.com:443 or *.example.com:443. CIDR and Unix socket rules are rejected. Rules constrain requested names, not resolved IP addresses. Private addresses are not categorically blocked.

The bridge uses the client's startup HTTP_PROXY/HTTPS_PROXY/NO_PROXY settings, including lowercase variants, and supports upstream proxies. Target environment settings are not copied to the client. Failures do not trigger automatic direct fallback or retries.

Ordinary target exec_run/job_start processes replace inherited HTTP_PROXY/HTTPS_PROXY and lowercase variants with the relay URL, retaining NO_PROXY. --unset-env applies after injection; per-call env applies last. Windows variable names are case insensitive.

Explicit net_request_http proxy/none takes precedence. When omitted, session routing and NO_PROXY apply. Unix sockets connect directly. exec_sudo follows sudo environment rules; automatic propagation is not guaranteed.

This feature does not isolate all OS networking. CONNECT carries TCP streams to allowed ports, so allow only required destinations and ports. Existing target --allow-host restrictions still apply to network tools.

## Authentication and diagnostics

The listener binds only to an ephemeral IPv4 loopback port and requires a session-specific proxy token. Other same-user processes can read the URL from the proxy_url_file path reported by sys_get_info. Do not paste its contents into conversations or ordinary logs.

The URL file has user-only permissions and is removed on normal termination or disconnection. Startup fails if permissions cannot be secured. Tokens left after forced termination cannot access subsequent sessions. Session secrets are masked in ordinary tool results, audit records, and progress.

Use the bridge's read-only proxy_status tool to inspect denied destinations and reasons.

The target's audit log records:

- `proxy_connect`: one record per HTTP request or CONNECT, with the kind (`http`, `connect`), destination host and port, result (`allowed`, `denied`, `failed`) and reason. URL paths and queries, headers, bodies, and credentials are not recorded. The result is that of opening the relay to the bridge, so an HTTP request whose upstream connection later fails on the bridge (HTTP 502) is still `allowed`.
- `proxy_auth_failed`: the number of proxy authentication failures (HTTP 407). The first failure is recorded at once; later failures are counted and recorded once a minute, and any still counted when the session ends. Destinations of unauthenticated requests are not recorded.

    {"after_seq": 0, "limit": 20}

Inputs are after_seq (nonnegative, default 0) and limit (1–100, default 20). Outputs are enabled, state, allow_hosts, active_connections, events, next_seq, and truncated.

Events include seq, time, connection_id, host, port, status (allowed/denied/failed), reason, and count for aggregated events. The latest 100 events are retained and returned in ascending sequence. Pass next_seq as after_seq to continue. truncated indicates lost history or unreturned events. Metadata is also written to stderr. Diagnostics exclude bodies, URL paths and queries, and credentials.

For a denial, inspect host, port, and reason, then add only the required destination and restart. The agent need not expand its own permissions.

## Limits and shutdown

The session supports 64 connections (shared with [port forwarding](port-forward.md)), 16 KiB of decoded data per frame, and a 64 KiB directional window. MCP and relay control take priority over data, with bounded queues. Connection and HTTP header timeouts are 30 seconds; inactivity is 5 minutes; shutdown waits up to 5 seconds.

HTTP errors normally use 403 for denied destinations, 407 for authentication, 503 for capacity, 502 for connection failures, and 504 for establishment timeouts. Failures after successful CONNECT close the connection. Session disconnection closes the listener and all relay connections.


MCP --tools/--read-only settings do not govern all proxy traffic. The proxy route is controlled by explicit enablement on both peers and the bridge destination rules.
