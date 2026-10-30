//go:build !windows

package tools

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// レジストリは Windows だけにあります。ツールは Windows でだけ登録します。

var errNoRegistry = errors.New("the registry exists only on Windows")

func (d *Deps) regRead(context.Context, *mcp.CallToolRequest, RegReadInput) (*mcp.CallToolResult, RegReadOutput, error) {
	return nil, RegReadOutput{}, errNoRegistry
}

func (d *Deps) regWrite(context.Context, *mcp.CallToolRequest, RegWriteInput) (*mcp.CallToolResult, RegWriteOutput, error) {
	return nil, RegWriteOutput{}, errNoRegistry
}

func (d *Deps) regDelete(context.Context, *mcp.CallToolRequest, RegDeleteInput) (*mcp.CallToolResult, RegDeleteOutput, error) {
	return nil, RegDeleteOutput{}, errNoRegistry
}
