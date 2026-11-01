# Port forwarding over SSH stdio

> [!NOTE]
> This is an English translation. The Japanese version ([../ja/port-forward.md](../ja/port-forward.md)) is the master and takes precedence if they differ.

Where SSH port forwarding (`-L`, `-R`, `-D`) is unavailable, host-shell forwards TCP ports using only the standard input/output of the existing SSH session. It shares the session with the [HTTP proxy through the client](client-proxy.md). The feature is disabled by default.

## Kinds of forwards

| Kind | Listens on | Connects from | ssh equivalent | Example use |
| --- | --- | --- | --- | --- |
| Local forward | Client (bridge) | Target | `-L` | View a dev server started on the target in the local browser |
| Remote forward | Target | Client (bridge) | `-R` | Let target applications use a database or Git server reachable from the client |
| SOCKS5 | Client (bridge) | Target (destination per connection) | `-D` | Browse the target's internal network from the local browser |

Table: Kinds of forwards

As an example of a local forward, [Injecting code-server](code-server.md) describes installing code-server on a target and opening it in the local browser.

```mermaid
flowchart LR
    subgraph client ["Client"]
        app1["Local application"]
        bridge["host-shell bridge"]
        dst2["Destination seen from the client"]
    end
    subgraph target ["Target"]
        remote["host-shell --mcp"]
        dst1["Destination seen from the target"]
        app2["Target application"]
    end
    app1 -->|"local forward, SOCKS5"| bridge
    bridge <-->|"SSH stdio"| remote
    remote --> dst1
    app2 -->|"remote forward"| remote
    bridge --> dst2
```

Only TCP is forwarded; Unix domain sockets and UDP are not. Targets support Linux/Windows; bridges support Linux/Windows/macOS.

## Startup

The bridge's forward options request directions and the target's options permit them.

    host-shell bridge \
      --forward-local 8080:localhost:3000 \
      --forward-remote 15432:db.client.example:5432 \
      --forward-socks 1080 --forward-allow-local '*.internal.example' \
      -- ssh -T target /path/to/host-shell --mcp --allow-forward-local --allow-forward-remote

### Bridge options

| Option | Value | Description |
| --- | --- | --- |
| `--forward-local` | `[BIND:]PORT:HOST:HOSTPORT` | Persistent local forward: listens on local PORT and connects from the target to HOST:HOSTPORT |
| `--forward-remote` | `[BIND:]PORT:HOST:HOSTPORT` | Persistent remote forward: listens on the target's PORT and connects from the client to HOST:HOSTPORT |
| `--forward-socks` | `[BIND:]PORT` | Persistent SOCKS5 listening on local PORT |
| `--forward-allow-local` | `HOST[:PORT]` | Destination rule for local forwards created by forward_open and for each SOCKS5 connection |
| `--forward-allow-remote` | `HOST[:PORT]` | Destination rule for remote forwards created by forward_open |

Table: Bridge forward options (all repeatable)

- BIND is `127.0.0.1` (default) or `[::1]` only. Connections from other hosts are not accepted.
- PORT 0 picks a free port; forward_status reports the actual port.
- Bracket an IPv6 HOST, as in `[2001:db8::1]`.
- Rules use the same host name glob and optional port as `--proxy-allow-host` (no CIDR). They constrain requested names, not resolved IP ranges.
- The port is also matched as a glob. For example, `'127.0.0.1:80[0-7]?'` matches 8000-8079. Quote it so that the shell does not expand it.
- Persistent local and remote forwards are the user's own permission and are not matched against rules.
- `--forward-socks` requires at least one `--forward-allow-local`. To allow every destination, state `--forward-allow-local '*'` explicitly.
- If any persistent forward cannot listen (for example, the port is in use), startup fails, including listeners on the target.

### Target options

| Option | Description |
| --- | --- |
| `--allow-forward-local` | Permits local forwards and SOCKS5 (connections from the target). Takes no value |
| `--allow-forward-remote` | Permits remote forwards (listeners on the target's loopback). Takes no value |
| `--allow-forward-remote-port PORT[-PORT]` | Limits the ports remote forwards listen on to the given ranges (like ssh `PermitListen`). Can be repeated or comma-separated. Requires `--allow-forward-remote` |

Table: Target permission options

- Destinations connected from the target follow `--allow-host`. Without it, the target does not restrict destinations.
- Ports the target listens on are not restricted unless `--allow-forward-remote-port` is given. With it, a port outside the ranges is refused (a persistent remote forward fails at startup; forward_open returns an error). A request for port 0 (automatic) takes the first free port, trying the ranges in the order given and each from its lowest port, and fails if none is free. sys_get_info reports the ranges under `restrictions`.

  ```sh
  host-shell --mcp --allow-forward-remote --allow-forward-remote-port 15432,20000-20099
  ```

- `--read-only` and `--tools` do not restrict forwarding. With a permission option, a local forward can write to a service on a `--read-only` target (such as a database). If `--read-only` is a strict boundary, do not give the permission options.
- The permission options are independent of `--client-proxy`.

### Matching requests and permissions

| Bridge option (request) | Target option required (permission) |
| --- | --- |
| `--forward-local`, `--forward-socks`, `--forward-allow-local` | `--allow-forward-local` |
| `--forward-remote`, `--forward-allow-remote` | `--allow-forward-remote` |
| `--proxy` | `--client-proxy` (required on both ends) |

Table: Requests and permissions

- A request without permission fails startup, and the error names the missing option.
- A permission without a request leaves the feature disabled and starts normally, so administrators can keep permission options in `authorized_keys` `command=`.
- When the bridge requests nothing, the session is ordinary MCP.
- Different host-shell versions on the two ends also fail startup; use the same version on both ends.

## Tools (bridge)

| Tool | Exposed when | Description |
| --- | --- | --- |
| forward_status | Any persistent forward or rule is given | Forwards, rules, and recent connection results |
| forward_open | At least one rule is given | Creates a forward |
| forward_close | At least one rule is given | Closes a forward created by forward_open and its connections |

Table: Forward tools

forward_open takes direction (`local`, `remote`, `socks`), listen_address (`127.0.0.1` or `::1`, default `127.0.0.1`), listen_port (default 0), and host and port (required for `local` and `remote`, not allowed for `socks`). It returns forward_id and the actual listen address and port.

    {"direction": "local", "host": "localhost", "port": 3000}

- Destinations must match `--forward-allow-local` for `local` and `socks`, or `--forward-allow-remote` for `remote`. A direction without rules cannot be created.
- forward_close cannot close persistent forwards.

forward_status takes after_seq and limit (1 to 100, default 20), as proxy_status does. It returns state, forwards (forward_id, direction, persistent, listen_address, listen_port, host, port, active_connections), allow_local, allow_remote, active_connections, events, next_seq, and truncated.

events carry seq, time, forward_id, connection_id, direction, host, port, status (allowed/denied/failed), and reason; the latest 100 are kept. reason is one of these fixed phrases.

| reason | Meaning |
| --- | --- |
| `connection established` | Connected |
| `destination not allowed` | Does not match the bridge rules |
| `destination not allowed by target` | Does not match the target's `--allow-host` |
| `connection limit reached` | The concurrent connection limit was reached |
| `forward closed` | The forward was closed |
| `unsupported SOCKS request` | The SOCKS request is not supported |
| `connection refused`, `host unreachable`, `connection timeout`, `connection failed` | Connecting failed |

Table: forward_status reasons

The same records go to the bridge's stderr. Forwarded data is never recorded.

## SOCKS5

- Only SOCKS5 CONNECT is supported. BIND, UDP ASSOCIATE, and SOCKS4/4a receive error replies.
- Addresses may be IPv4, IPv6, or domain names. The target resolves domain names, so use `socks5h://` with curl.

        curl --proxy socks5h://127.0.0.1:1080 http://intranet.internal.example/

- There is no authentication (method 0x00 only).
- Replies are 0x02 for denials, 0x04 for name resolution failures and unreachable hosts, 0x05 for refused connections, and 0x06 when the 30 second establishment wait expires.

## Security

- Both sides listen on loopback only. Listeners do not authenticate connections, so processes of other users on the same host can connect, as with ssh `-L`, `-R`, and `-D`.
- The feature does not use sshd forwarding, so sshd `no-port-forwarding` or `AllowTcpForwarding no` cannot stop it. Target administrators permit it with the permission options, and forbid it by leaving them out of `command=`.
- For remote forwards, the bridge connects to destinations directly without HTTP_PROXY/HTTPS_PROXY.
- The target's audit log records remote forward listeners starting, stopping and being refused (`forward_listen`, `forward_unlisten`) and connections for local forwards and SOCKS5 (`forward_connect`: direction, destination, result).
- sys_get_info returns the permitted directions (`forward_local`, `forward_remote`) and the remote forwards listening on the target (`remote_forwards`).

## Limits and shutdown

- A session carries at most 64 concurrent connections, shared with the HTTP proxy.
- A session has at most 16 forwards (listeners), persistent and forward_open combined.
- Connecting waits up to 30 seconds. Forwarded connections have no idle timeout; they last until either end closes, forward_close, or the session ends.
- Half-closes are passed to the other end.
- When the session disconnects, all listeners and connections on both sides close. There is no automatic reconnection.
