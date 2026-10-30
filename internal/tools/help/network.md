# Network

All network tools run **on the remote host**: `localhost` is the remote host, and anything the host
can reach is reachable. This replaces port forwarding.

## net_http

```json
{"url": "http://localhost:8080/health"}
{"url": "https://api.internal/v1/items", "method": "POST",
 "headers": {"Content-Type": "application/json", "Authorization": "Bearer ..."},
 "body": "{\"name\": \"x\"}"}
{"url": "http://localhost/v1.43/containers/json", "unix_socket": "/var/run/docker.sock"}
{"url": "https://example.com/big.tar.gz", "save_to": "/tmp/big.tar.gz"}
```

- Returns `status`, `headers` and `body`. Text bodies (JSON, HTML, ...) come as UTF-8 text, binary as
  base64 (`response_encoding` to force). Bodies longer than the limit are cut (`body_truncated`);
  use `save_to` to store any size in a remote file, then `fs_read` or `search_grep` it.
- Redirects are followed (up to 10) unless `no_redirects`. `insecure` accepts self-signed
  certificates; `ca_cert`, `client_cert` and `client_key` are PEM files on the remote host.
- The host's `HTTP_PROXY`/`HTTPS_PROXY`/`NO_PROXY` apply; `proxy: "none"` connects directly.
- `body_file` sends a remote file as the body (uploads).
- For HTTPS, `tls` shows the certificate and whether it verifies.

## net_tcp

Connects, optionally sends, and reads until `until` matches, `idle_ms` of silence (default 1000),
the peer closes, or `max_bytes`.

```json
{"port": 6379, "send": "PING\r\n", "until": "\\r\\n"}
{"host": "mail.internal", "port": 25}
{"host": "db.internal", "port": 5432, "tls": true, "insecure": true}
```

## net_check

Many targets in parallel: `host:port` (DNS + connect), `host` (DNS only) or URLs.

```json
{"targets": ["db.internal:5432", "https://example.com", "cache:6379", "registry.npmjs.org"], "tls": true}
```

Each result has `addresses`, `dns_ms`, `connected`, `connect_ms`, the certificate with `days_left`
and `verified` (with `tls` or https URLs), and `error` (refused, timeout, no such host, ...).

## net_listen, net_interfaces

`net_listen` lists listening TCP/UDP sockets with pid and process (filter by `port`, `protocol`,
`process`). On Linux, processes of other users show only when host-shell runs as root.
`net_interfaces` lists interfaces with addresses, MAC, MTU and flags.
