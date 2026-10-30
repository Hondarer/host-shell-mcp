package tools

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hondarer/host-shell-mcp/internal/audit"
)

// pathArgs is implemented by input types that report read and write paths for policy checks.
type pathArgs interface {
	PathArgs() (read, write []string)
}

// commandArgs is implemented by input types that report invoked programs and command text for policy checks.
type commandArgs interface {
	CommandArgs(defaultShell string) (programs []string, text string)
}

// checkPolicy validates that the tool input satisfies configured path and command policies.
func (d *Deps) checkPolicy(in any) error {
	if pa, ok := in.(pathArgs); ok {
		read, write := pa.PathArgs()
		for _, p := range read {
			if err := d.Policy.CheckPath(p, false); err != nil {
				return err
			}
		}
		for _, p := range write {
			if err := d.Policy.CheckPath(p, true); err != nil {
				return err
			}
		}
	}
	if ca, ok := in.(commandArgs); ok {
		programs, text := ca.CommandArgs(d.DefaultShell)
		if err := d.Policy.CheckCommand(programs, text); err != nil {
			return err
		}
	}
	return nil
}

// confirmID identifies the elicitation confirmation request.
const confirmID = "confirm"

// confirm requests user confirmation via MCP elicitation (SEP-2322).
//
// On the initial call it returns a non-nil CallToolResult carrying the elicitation prompt and an HMAC state
// bound to the tool name and arguments. On retry with a matching state and accepted response, it returns nil, nil.
// If the client does not support elicitation, it returns an error.
func (d *Deps) confirm(req *mcp.CallToolRequest, name string, in any) (ask *mcp.CallToolResult, err error) {
	if p := req.Session.InitializeParams(); p == nil || p.Capabilities == nil || p.Capabilities.Elicitation == nil {
		return nil, fmt.Errorf("policy: %s requires confirmation by the user, but the client cannot ask the user (no elicitation support)", name)
	}
	// Verify that the confirmation response matches the HMAC state for these exact arguments.
	state := d.confirmState(name, req.Params.Arguments)
	if resp, ok := req.Params.InputResponses[confirmID]; ok && hmac.Equal([]byte(req.Params.RequestState), []byte(state)) {
		r, ok := resp.(*mcp.ElicitResult)
		switch {
		case !ok:
			return nil, fmt.Errorf("policy: unexpected confirmation response %T", resp)
		case r.Action != "accept":
			return nil, fmt.Errorf("policy: the user did not allow %s (%s)", name, r.Action)
		}
		if v, ok := r.Content["approve"].(bool); ok && !v {
			return nil, fmt.Errorf("policy: the user did not allow %s", name)
		}
		return nil, nil
	}

	args := in
	if r, ok := in.(audit.Redactor); ok {
		args = r.AuditArgs()
	}
	b, _ := json.Marshal(args)
	s := string(b)
	if len(s) > 1000 {
		s = s[:1000]
		for !utf8.ValidString(s) {
			s = s[:len(s)-1]
		}
		s += "..."
	}
	host, _ := os.Hostname()
	return &mcp.CallToolResult{
		InputRequests: mcp.InputRequestMap{confirmID: &mcp.ElicitParams{
			Message: fmt.Sprintf("host-shell on %s: allow %s with %s?", host, name, s),
			RequestedSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"approve": map[string]any{"type": "boolean", "title": "Allow this operation", "default": true},
				},
				"required": []string{"approve"},
			},
		}},
		RequestState: state,
	}, nil
}

// confirmState signs the tool name and arguments. The key is created each time the server starts.
func (d *Deps) confirmState(name string, args []byte) string {
	d.keyOnce.Do(func() {
		d.confirmKey = make([]byte, 32)
		rand.Read(d.confirmKey)
	})
	m := hmac.New(sha256.New, d.confirmKey)
	m.Write([]byte(name))
	m.Write([]byte{0})
	m.Write(args)
	return hex.EncodeToString(m.Sum(nil))
}

// redactOutput applies redaction to every string in the output.
func redactOutput[T any](d *Deps, res *mcp.CallToolResult, out T) (*mcp.CallToolResult, T) {
	if res != nil {
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				tc.Text = d.Policy.RedactString(tc.Text)
			}
		}
	}
	v := reflect.New(reflect.TypeOf(&out).Elem()).Elem()
	v.Set(reflect.ValueOf(&out).Elem())
	redactValue(d, v)
	return res, v.Interface().(T)
}

func redactValue(d *Deps, v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(d.Policy.RedactString(v.String()))
		}
	case reflect.Pointer, reflect.Interface:
		if !v.IsNil() {
			e := v.Elem()
			if v.Kind() == reflect.Interface {
				// The value inside an interface cannot be modified in place, so modify a copy and put it back.
				c := reflect.New(e.Type()).Elem()
				c.Set(e)
				redactValue(d, c)
				if v.CanSet() {
					v.Set(c)
				}
				return
			}
			redactValue(d, e)
		}
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				redactValue(d, v.Field(i))
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < v.Len(); i++ {
			redactValue(d, v.Index(i))
		}
	case reflect.Map:
		for _, k := range v.MapKeys() {
			e := v.MapIndex(k)
			c := reflect.New(e.Type()).Elem()
			c.Set(e)
			redactValue(d, c)
			v.SetMapIndex(k, c)
		}
	}
}

// The paths each tool input reads and writes.

func (in FsListInput) PathArgs() (read, write []string) {
	return []string{orDefault(in.Path, ".")}, nil
}
func (in FsStatInput) PathArgs() (read, write []string)  { return []string{in.Path}, nil }
func (in FsReadInput) PathArgs() (read, write []string)  { return []string{in.Path}, nil }
func (in FsEditInput) PathArgs() (read, write []string)  { return nil, []string{in.Path} }
func (in FsWriteInput) PathArgs() (read, write []string) { return nil, []string{in.Path} }
func (in FsMkdirInput) PathArgs() (read, write []string) { return nil, []string{in.Path} }
func (in FsMoveInput) PathArgs() (read, write []string) {
	return nil, []string{in.Source, in.Destination}
}
func (in FsCopyInput) PathArgs() (read, write []string) {
	return []string{in.Source}, []string{in.Destination}
}
func (in FsRemoveInput) PathArgs() (read, write []string) { return nil, []string{in.Path} }
func (in SearchGrepInput) PathArgs() (read, write []string) {
	return []string{orDefault(in.Path, ".")}, nil
}
func (in SearchFilesInput) PathArgs() (read, write []string) {
	return []string{orDefault(in.Path, ".")}, nil
}
func (in FsManifestInput) PathArgs() (read, write []string) {
	return []string{orDefault(in.Path, ".")}, nil
}
