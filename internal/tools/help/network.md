# Network

All network tools execute **on the remote host**: `localhost` refers to the remote host, and any destination reachable from the host is accessible without port forwarding.

## net_http

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

## net_tcp

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

## net_listen, net_interfaces

`net_listen` lists listening TCP and UDP sockets with `pid` and process name (filtered by `port`, `protocol`, or `process`). On Linux, processes owned by other users appear only when host-shell runs as root.
`net_interfaces` lists network interfaces with IP addresses, MAC address, MTU, and flags.
