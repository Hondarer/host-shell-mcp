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

When the target is started with `--client-proxy` and `bridge` is started with `--proxy` and `--proxy-allow-host HOST[:PORT]`, proxy-aware HTTP/HTTPS programs on this host can reach external networks through the client over the existing SSH stdio stream. Both sides must enable the feature. HTTP requests and `CONNECT` tunnels are supported without TLS interception. Ordinary `exec_run` and `job_start` calls inherit `HTTP_PROXY` and `HTTPS_PROXY` automatically; per-call `env` settings and explicit `proxy` / `"none"` arguments in `net_request_http` take precedence, `NO_PROXY` remains effective, and `sudo` follows its own environment preservation rules. Use the bridge's `proxy_status` tool to inspect denied hosts, ports, and failure reasons; this host's audit log also records each proxied destination and result (`proxy_connect`). `sys_get_info` reports only the credential URL file path (`proxy_url_file`); never print or paste its contents. Destination rules match requested host names rather than resolved IP addresses.

## Port forwarding

When SSH port forwarding is unavailable, `bridge` can forward TCP ports over the same SSH stdio session. Listeners bind only to loopback (`127.0.0.1` or `::1`) and do not authenticate local connections, similar to `ssh -L` / `-R` / `-D`.

- Local forward (`bridge --forward-local`, or `forward_open` with `direction: "local"`): listens on the client machine and connects from this host (for example, to open a web server running on this host in the user's local browser).
- Remote forward (`bridge --forward-remote`, or `forward_open` with `direction: "remote"`): listens on this host and connects directly from the client machine without using proxies. Programs on this host connect to the loopback address listed under `remote_forwards` in `sys_get_info`.
- SOCKS5 (`bridge --forward-socks`, or `forward_open` with `direction: "socks"`): supports `CONNECT` without authentication; use `socks5h://` so name resolution happens on this host.

This host must permit each direction with `--allow-forward-local` (for local forwards and SOCKS5) or `--allow-forward-remote`; `sys_get_info` reports `forward_local` and `forward_remote`. `--allow-forward-remote-port` can restrict which ports remote forwards may bind on this host (`sys_get_info` lists allowed ports under `restrictions`, port `0` selects a free port within the allowed set, and disallowed ports fail with `listen port not allowed by target`). Outbound connections from this host respect `--allow-host`, whereas `--read-only` and `--tools` do not restrict forwarding. `forward_open` and `forward_close` are published only when `bridge` has `--forward-allow-local` or `--forward-allow-remote` rules. Use `forward_status` on the bridge to list active forwards and inspect denied or failed connections. A session supports up to 64 concurrent streams (shared with the client proxy) and 16 active forwards.
