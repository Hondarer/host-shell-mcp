package tools

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func registerNet(r *Registry, d *Deps) {
	add(r, groupNet, &mcp.Tool{
		Name:  "net_request_http",
		Title: "HTTP request",
		Description: "Send an HTTP(S) request from the remote host and return status, headers and body (for localhost services, internal APIs, health checks, or metrics without port forwarding). " +
			"Supports any method, headers, body (text, base64 or body_file), unix_socket (e.g. Docker), save_to for large downloads, and TLS options (insecure, ca_cert, client_cert/client_key).",
		Annotations: network(false),
	}, d.netRequestHTTP)
	add(r, groupNet, &mcp.Tool{
		Name:  "net_exchange",
		Title: "TCP exchange",
		Description: "Open a TCP (optionally TLS) or Unix socket connection from the remote host, send optional data, and read the response " +
			"(for Redis, SMTP/FTP banners, memcached, or raw protocol checks). " +
			"Stops reading on until (regex), idle_ms of silence, peer close, or max_bytes.",
		Annotations: network(false),
	}, d.netExchange)
	add(r, groupNet, &mcp.Tool{
		Name:  "net_check",
		Title: "Check reachability",
		Description: "Check DNS resolution, TCP connectivity, and optional TLS certificates in parallel from the remote host, returning timings and certificate details. " +
			"Targets accept host:port, host (DNS only) or URLs.",
		Annotations: network(true),
	}, d.netCheck)
	add(r, groupNet, &mcp.Tool{
		Name:        "net_list_listeners",
		Title:       "Listening ports",
		Description: "List listening TCP and UDP sockets on the remote host with owning pid and process name (other users' processes may require root). Filter by port, protocol or process.",
		Annotations: readOnly(),
	}, d.netListListeners)
	add(r, groupNet, &mcp.Tool{
		Name:        "net_list_interfaces",
		Title:       "Network interfaces",
		Description: "List network interfaces on the remote host with IP addresses, MAC address, MTU and flags.",
		Annotations: readOnly(),
	}, d.netListInterfaces)
}
