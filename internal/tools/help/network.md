# Network

All network tools execute **on the remote host**: `localhost` refers to the remote host, and any destination reachable from the host is accessible without port forwarding.

## net_request_http

```json
{"url": "http://localhost:8080/health"}
{"url": "https://api.internal/v1/items", "method": "POST",
 "headers": {"Content-Type": "application/json", "Authorization": "Bearer ..."},
 "body": "{\"name\": \"x\"}"}
{"url": "http://localhost/v1.43/containers/json", "unix_socket": "/var/run/docker.sock"}
{"url": "https://example.com/big.tar.gz", "save_to": "/tmp/big.tar.gz"}
```

- Returns `status`, `headers`, and `body`. Text responses (JSON, HTML, etc.) are returned as UTF-8 text and binary responses as base64 (`response_encoding` overrides detection). Responses exceeding the output limit are truncated (`body_truncated: true`); use `save_to` to stream responses of any size to a remote file, then inspect it with `fs_read` or `search_grep`.
- Follows up to 10 redirects unless `no_redirects: true`. `insecure: true` accepts self-signed or unverified certificates; `ca_cert`, `client_cert`, and `client_key` specify PEM files on the remote host.
- Honors `HTTP_PROXY`, `HTTPS_PROXY`, and `NO_PROXY` on the remote host; `proxy: "none"` bypasses proxies.
- `body_file` streams a remote file as the request body.
- For HTTPS requests, `tls` reports the server certificate and verification status.

## net_exchange

Connects to a TCP (or TLS) endpoint, optionally sends data, and reads until `until` matches, `idle_ms` of silence elapses (default 1000), the peer closes the connection, or `max_bytes` is reached.

```json
{"port": 6379, "send": "PING\r\n", "until": "\\r\\n"}
{"host": "mail.internal", "port": 25}
{"host": "db.internal", "port": 5432, "tls": true, "insecure": true}
```

## net_check

Probes multiple targets in parallel: `host:port` (DNS + TCP connect), `host` (DNS only), or URLs.

```json
{"targets": ["db.internal:5432", "https://example.com", "cache:6379", "registry.npmjs.org"], "tls": true}
```

Each entry returns `addresses`, `dns_ms`, `connected`, `connect_ms`, certificate details with `days_left` and `verified` (when `tls: true` or for `https://` URLs), and `error` (connection refused, timeout, unknown host, etc.).

## net_list_listeners, net_list_interfaces

`net_list_listeners` lists listening TCP and UDP sockets with `pid` and process name (filtered by `port`, `protocol`, or `process`). On Linux, processes owned by other users appear only when host-shell runs as root.
`net_list_interfaces` lists network interfaces with IP addresses, MAC address, MTU, and flags.

## Client proxy

With target --client-proxy and bridge --proxy --proxy-allow-host HOST[:PORT], HTTP proxy aware programs can use the client network over existing SSH stdio. Both peers must enable the feature. HTTP and CONNECT are supported without TLS interception. Ordinary exec_run/job_start inherit the relay URL; per-call env and explicit net_request_http proxy/none take precedence, NO_PROXY remains effective, and sudo follows its own environment rules. Use bridge proxy_status to inspect denied host/port and reasons; this host's audit log records each proxied destination and result (proxy_connect). sys_get_info reports only the credential URL file path; never paste its contents. Destination rules constrain requested host names, not resolved IP ranges.

## Port forwarding

The bridge can forward TCP ports over the same SSH stdio session when SSH port forwarding is unavailable. Listeners are loopback only (127.0.0.1 or ::1) and do not authenticate connections, like ssh -L/-R/-D.

- Local forward (bridge --forward-local, or forward_open direction local): listens on the agent's machine and connects from this host, for example to show a dev server running here in the user's browser.
- Remote forward (bridge --forward-remote, or forward_open direction remote): listens on this host and connects from the agent's machine directly, without proxies. Programs here connect to the address listed in sys_get_info remote_forwards.
- SOCKS5 (bridge --forward-socks, or forward_open direction socks): CONNECT only, no authentication; use socks5h:// so this host resolves names.

This host must permit the direction with --allow-forward-local (local forwards and SOCKS5) or --allow-forward-remote; sys_get_info reports forward_local and forward_remote. --allow-forward-remote-port may limit the ports remote forwards listen on; sys_get_info lists them under restrictions, port 0 picks a free port within them, and other ports fail with "listen port not allowed by target". Destinations connected from this host follow --allow-host; --read-only and --tools do not restrict forwarding. forward_open and forward_close exist only when the bridge has --forward-allow-local/--forward-allow-remote rules. Use bridge forward_status to list forwards and inspect denied or failed connections. Sessions carry at most 64 connections (shared with the client proxy) and 16 forwards.
