package tools

import (
	"context"
	"net"
	"slices"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- net_list_listeners ----

// NetListListenersInput is the input of net_list_listeners.
type NetListListenersInput struct {
	Port     int    `json:"port,omitempty" jsonschema:"filter by port"`
	Protocol string `json:"protocol,omitempty" jsonschema:"tcp or udp (default both)"`
	Process  string `json:"process,omitempty" jsonschema:"case-insensitive process name substring"`
}

// Listener is a listening socket.
type Listener struct {
	Protocol string `json:"protocol" jsonschema:"tcp, tcp6, udp or udp6"`
	Address  string `json:"address"`
	Port     int    `json:"port"`
	PID      int    `json:"pid,omitempty"`
	Process  string `json:"process,omitempty"`
}

// NetListListenersOutput is the output of net_list_listeners.
type NetListListenersOutput struct {
	Listeners []Listener `json:"listeners"`
	Note      string     `json:"note,omitempty"`
}

func (d *Deps) netListListeners(ctx context.Context, _ *mcp.CallToolRequest, in NetListListenersInput) (*mcp.CallToolResult, NetListListenersOutput, error) {
	all, note, err := listeners(ctx)
	if err != nil {
		return nil, NetListListenersOutput{}, err
	}
	out := NetListListenersOutput{Listeners: []Listener{}, Note: note}
	for _, l := range all {
		switch {
		case in.Port > 0 && l.Port != in.Port:
		case in.Protocol != "" && !strings.HasPrefix(l.Protocol, strings.ToLower(in.Protocol)):
		case in.Process != "" && !strings.Contains(strings.ToLower(l.Process), strings.ToLower(in.Process)):
		default:
			out.Listeners = append(out.Listeners, l)
		}
	}
	slices.SortFunc(out.Listeners, func(a, b Listener) int {
		if a.Port != b.Port {
			return a.Port - b.Port
		}
		return strings.Compare(a.Protocol+a.Address, b.Protocol+b.Address)
	})
	return nil, out, nil
}

// ---- net_list_interfaces ----

// Interface is a network interface.
type Interface struct {
	Name      string   `json:"name"`
	Index     int      `json:"index"`
	MTU       int      `json:"mtu"`
	MAC       string   `json:"mac,omitempty"`
	Flags     string   `json:"flags"`
	Addresses []string `json:"addresses" jsonschema:"IP addresses with prefix length"`
}

// NetListInterfacesOutput is the output of net_list_interfaces.
type NetListInterfacesOutput struct {
	Interfaces []Interface `json:"interfaces"`
}

func (d *Deps) netListInterfaces(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, NetListInterfacesOutput, error) {
	ifs, err := net.Interfaces()
	if err != nil {
		return nil, NetListInterfacesOutput{}, err
	}
	out := NetListInterfacesOutput{Interfaces: []Interface{}}
	for _, i := range ifs {
		it := Interface{Name: i.Name, Index: i.Index, MTU: i.MTU, MAC: i.HardwareAddr.String(), Flags: i.Flags.String(), Addresses: []string{}}
		if addrs, err := i.Addrs(); err == nil {
			for _, a := range addrs {
				it.Addresses = append(it.Addresses, a.String())
			}
		}
		out.Interfaces = append(out.Interfaces, it)
	}
	return nil, out, nil
}
