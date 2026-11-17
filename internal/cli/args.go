// Package cli implements host-shell call and host-shell tools, which use the tools of a
// running host-shell session (package session) from the shell.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
)

// schema is the part of a tool's JSON input schema that argument conversion uses.
type schema struct {
	Type       any                `json:"type"` // a type name or a list of them
	Items      *schema            `json:"items"`
	Properties map[string]*schema `json:"properties"`
	Required   []string           `json:"required"`
}

func parseSchema(v any) (*schema, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var s schema
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// types returns the JSON types the schema allows, without "null".
func (s *schema) types() []string {
	var out []string
	switch t := s.Type.(type) {
	case string:
		out = []string{t}
	case []any:
		for _, v := range t {
			if name, ok := v.(string); ok {
				out = append(out, name)
			}
		}
	}
	return slices.DeleteFunc(out, func(t string) bool { return t == "null" })
}

// is reports whether the schema allows exactly one type besides null, and it is name.
func (s *schema) is(name string) bool {
	t := s.types()
	return len(t) == 1 && t[0] == name
}

// stringList reports whether the schema is an array of strings, whose values KEY=VALUE builds
// one element at a time.
func (s *schema) stringList() bool {
	return s.is("array") && s.Items != nil && s.Items.is("string")
}

// keys returns the property names, sorted.
func (s *schema) keys() []string {
	keys := make([]string, 0, len(s.Properties))
	for k := range s.Properties {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// buildArgs combines the --json object (base, may be nil) and KEY=VALUE pairs into the
// arguments of a call. Pairs replace keys of base. readFile reads KEY=@FILE.
func buildArgs(s *schema, base map[string]any, pairs []string, readFile func(string) ([]byte, error)) (map[string]any, error) {
	args := map[string]any{}
	for k, v := range base {
		args[k] = v
	}
	seen := map[string]bool{}
	for _, pair := range pairs {
		key, raw, ok := strings.Cut(pair, "=")
		if !ok || key == "" {
			return nil, fmt.Errorf("argument %q is not KEY=VALUE", pair)
		}
		prop, ok := s.Properties[key]
		if !ok {
			return nil, fmt.Errorf("unknown argument %q; the tool takes: %s", key, strings.Join(s.keys(), ", "))
		}
		fromFile := false
		switch {
		case strings.HasPrefix(raw, "@@"):
			raw = raw[1:]
		case strings.HasPrefix(raw, "@"):
			b, err := readFile(raw[1:])
			if err != nil {
				return nil, fmt.Errorf("argument %s: %w", key, err)
			}
			raw, fromFile = string(b), true
		}
		if prop.stringList() {
			if !seen[key] {
				args[key] = []any{}
			}
			seen[key] = true
			args[key] = append(args[key].([]any), raw)
			continue
		}
		if seen[key] {
			return nil, fmt.Errorf("argument %s is given more than once", key)
		}
		seen[key] = true
		v, err := convert(prop, raw, fromFile)
		if err != nil {
			return nil, fmt.Errorf("argument %s: %w", key, err)
		}
		args[key] = v
	}
	return args, nil
}

// convert turns the text of one value into the type the schema asks for.
func convert(s *schema, raw string, fromFile bool) (any, error) {
	switch {
	case s.is("string"):
		return raw, nil
	case s.is("integer"):
		n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not an integer", raw)
		}
		return n, nil
	case s.is("number"):
		n, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a number", raw)
		}
		return n, nil
	case s.is("boolean"):
		b, err := strconv.ParseBool(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("%q is not true or false", raw)
		}
		return b, nil
	}
	// Arrays, objects and values the schema does not pin to one type are JSON. A value that is
	// not JSON stays a string unless the schema requires a structure.
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err == nil {
		return v, nil
	}
	if s.is("array") || s.is("object") {
		what := "the value"
		if fromFile {
			what = "the file"
		}
		return nil, fmt.Errorf("%s is not JSON (%s expected)", what, s.types()[0])
	}
	return raw, nil
}

// readArgsFile reads the --json object from a file or, for "-", from stdin.
func readArgsFile(name string, stdin func() ([]byte, error)) (map[string]any, error) {
	var b []byte
	var err error
	if name == "-" {
		b, err = stdin()
	} else {
		b, err = os.ReadFile(name)
	}
	if err != nil {
		return nil, err
	}
	var args map[string]any
	if err := json.Unmarshal(b, &args); err != nil {
		return nil, fmt.Errorf("--json %s: not a JSON object: %w", name, err)
	}
	return args, nil
}
