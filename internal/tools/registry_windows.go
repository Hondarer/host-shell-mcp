package tools

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"math"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sys/windows/registry"
)

var regRoots = map[string]registry.Key{
	"HKLM": registry.LOCAL_MACHINE, "HKEY_LOCAL_MACHINE": registry.LOCAL_MACHINE,
	"HKCU": registry.CURRENT_USER, "HKEY_CURRENT_USER": registry.CURRENT_USER,
	"HKCR": registry.CLASSES_ROOT, "HKEY_CLASSES_ROOT": registry.CLASSES_ROOT,
	"HKU": registry.USERS, "HKEY_USERS": registry.USERS,
	"HKCC": registry.CURRENT_CONFIG, "HKEY_CURRENT_CONFIG": registry.CURRENT_CONFIG,
}

var regTypeNames = map[uint32]string{
	registry.NONE: "REG_NONE", registry.SZ: "REG_SZ", registry.EXPAND_SZ: "REG_EXPAND_SZ", registry.BINARY: "REG_BINARY",
	registry.DWORD: "REG_DWORD", registry.DWORD_BIG_ENDIAN: "REG_DWORD_BIG_ENDIAN", registry.LINK: "REG_LINK",
	registry.MULTI_SZ: "REG_MULTI_SZ", registry.RESOURCE_LIST: "REG_RESOURCE_LIST", registry.QWORD: "REG_QWORD",
	registry.FULL_RESOURCE_DESCRIPTOR: "REG_FULL_RESOURCE_DESCRIPTOR", registry.RESOURCE_REQUIREMENTS_LIST: "REG_RESOURCE_REQUIREMENTS_LIST",
}

// splitRegKey splits "HKLM\SOFTWARE\..." into the root key and the path.
func splitRegKey(key string) (registry.Key, string, error) {
	key = strings.Trim(strings.ReplaceAll(key, "/", `\`), `\`)
	root, rest, _ := strings.Cut(key, `\`)
	k, ok := regRoots[strings.ToUpper(strings.TrimSuffix(root, ":"))]
	if !ok {
		return 0, "", fmt.Errorf("unknown registry root %q (use HKLM, HKCU, HKCR, HKU or HKCC)", root)
	}
	return k, rest, nil
}

func regView(view string) (uint32, error) {
	switch view {
	case "":
		return 0, nil
	case "64":
		return registry.WOW64_64KEY, nil
	case "32":
		return registry.WOW64_32KEY, nil
	}
	return 0, fmt.Errorf("view must be 32 or 64: %q", view)
}

func openRegKey(key, view string, access uint32) (registry.Key, error) {
	root, path, err := splitRegKey(key)
	if err != nil {
		return 0, err
	}
	v, err := regView(view)
	if err != nil {
		return 0, err
	}
	k, err := registry.OpenKey(root, path, access|v)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", key, err)
	}
	return k, nil
}

func readRegValue(k registry.Key, name string) (RegValue, error) {
	_, typ, err := k.GetValue(name, nil)
	if err != nil {
		return RegValue{}, err
	}
	v := RegValue{Name: name, Type: regTypeNames[typ]}
	if v.Type == "" {
		v.Type = fmt.Sprintf("type %d", typ)
	}
	switch typ {
	case registry.SZ, registry.EXPAND_SZ:
		s, _, err := k.GetStringValue(name)
		if err != nil {
			return v, err
		}
		v.Data = s
		if typ == registry.EXPAND_SZ {
			v.Expanded, _ = registry.ExpandString(s)
		}
	case registry.MULTI_SZ:
		ss, _, err := k.GetStringsValue(name)
		if err != nil {
			return v, err
		}
		v.Data = ss
	case registry.DWORD, registry.QWORD:
		n, _, err := k.GetIntegerValue(name)
		if err != nil {
			return v, err
		}
		v.Data = n
	default:
		b, _, err := k.GetBinaryValue(name)
		if err != nil {
			return v, err
		}
		v.Data = base64.StdEncoding.EncodeToString(b)
	}
	return v, nil
}

func (d *Deps) regRead(_ context.Context, _ *mcp.CallToolRequest, in RegReadInput) (*mcp.CallToolResult, RegReadOutput, error) {
	k, err := openRegKey(in.Key, in.View, registry.READ)
	if err != nil {
		return nil, RegReadOutput{}, err
	}
	defer k.Close()
	out := RegReadOutput{Key: in.Key, Values: []RegValue{}}
	if in.Value != nil {
		v, err := readRegValue(k, *in.Value)
		if err != nil {
			return nil, RegReadOutput{}, fmt.Errorf("value %q: %w", *in.Value, err)
		}
		out.Values = append(out.Values, v)
		return nil, out, nil
	}
	if out.Subkeys, err = k.ReadSubKeyNames(-1); err != nil {
		return nil, RegReadOutput{}, err
	}
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return nil, RegReadOutput{}, err
	}
	for _, n := range names {
		v, err := readRegValue(k, n)
		if err != nil {
			continue
		}
		out.Values = append(out.Values, v)
	}
	return nil, out, nil
}

func (d *Deps) regWrite(_ context.Context, _ *mcp.CallToolRequest, in RegWriteInput) (*mcp.CallToolResult, RegWriteOutput, error) {
	root, path, err := splitRegKey(in.Key)
	if err != nil {
		return nil, RegWriteOutput{}, err
	}
	view, err := regView(in.View)
	if err != nil {
		return nil, RegWriteOutput{}, err
	}
	k, existed, err := registry.CreateKey(root, path, registry.ALL_ACCESS|view)
	if err != nil {
		return nil, RegWriteOutput{}, fmt.Errorf("create %s: %w", in.Key, err)
	}
	defer k.Close()
	out := RegWriteOutput{Key: in.Key, CreatedKey: !existed}
	if in.Value == nil {
		return nil, out, nil
	}
	name := *in.Value
	switch strings.ToUpper(orDefault(in.Type, "REG_SZ")) {
	case "REG_SZ", "REG_EXPAND_SZ":
		s, ok := in.Data.(string)
		if !ok {
			return nil, RegWriteOutput{}, errors.New("data must be a string")
		}
		if strings.EqualFold(in.Type, "REG_EXPAND_SZ") {
			err = k.SetExpandStringValue(name, s)
		} else {
			err = k.SetStringValue(name, s)
		}
	case "REG_MULTI_SZ":
		arr, ok := in.Data.([]any)
		if !ok {
			return nil, RegWriteOutput{}, errors.New("data must be an array of strings")
		}
		ss := make([]string, len(arr))
		for i, a := range arr {
			if ss[i], ok = a.(string); !ok {
				return nil, RegWriteOutput{}, errors.New("data must be an array of strings")
			}
		}
		err = k.SetStringsValue(name, ss)
	case "REG_DWORD", "REG_QWORD":
		f, ok := in.Data.(float64)
		if !ok || f < 0 || f != math.Trunc(f) {
			return nil, RegWriteOutput{}, errors.New("data must be a non-negative integer")
		}
		if strings.EqualFold(in.Type, "REG_DWORD") {
			if f > math.MaxUint32 {
				return nil, RegWriteOutput{}, errors.New("REG_DWORD data must fit in 32 bits")
			}
			err = k.SetDWordValue(name, uint32(f))
		} else {
			err = k.SetQWordValue(name, uint64(f))
		}
	case "REG_BINARY":
		s, ok := in.Data.(string)
		if !ok {
			return nil, RegWriteOutput{}, errors.New("data must be a base64 string")
		}
		b, derr := base64.StdEncoding.DecodeString(s)
		if derr != nil {
			return nil, RegWriteOutput{}, fmt.Errorf("data: %w", derr)
		}
		err = k.SetBinaryValue(name, b)
	default:
		return nil, RegWriteOutput{}, fmt.Errorf("unsupported type %q", in.Type)
	}
	if err != nil {
		return nil, RegWriteOutput{}, fmt.Errorf("set %q: %w", name, err)
	}
	v, err := readRegValue(k, name)
	if err == nil {
		out.Value = &v
	}
	return nil, out, nil
}

func (d *Deps) regDelete(_ context.Context, _ *mcp.CallToolRequest, in RegDeleteInput) (*mcp.CallToolResult, RegDeleteOutput, error) {
	if in.Value != nil {
		k, err := openRegKey(in.Key, in.View, registry.SET_VALUE)
		if err != nil {
			return nil, RegDeleteOutput{}, err
		}
		defer k.Close()
		if err := k.DeleteValue(*in.Value); err != nil {
			return nil, RegDeleteOutput{}, fmt.Errorf("delete value %q: %w", *in.Value, err)
		}
		return nil, RegDeleteOutput{Deleted: in.Key + `\` + *in.Value}, nil
	}
	root, path, err := splitRegKey(in.Key)
	if err != nil {
		return nil, RegDeleteOutput{}, err
	}
	if path == "" {
		return nil, RegDeleteOutput{}, errors.New("refusing to delete a registry root")
	}
	view, err := regView(in.View)
	if err != nil {
		return nil, RegDeleteOutput{}, err
	}
	if in.Recursive {
		err = deleteRegTree(root, path, view)
	} else {
		err = deleteRegKey(root, path, view)
	}
	if err != nil {
		return nil, RegDeleteOutput{}, err
	}
	return nil, RegDeleteOutput{Deleted: in.Key}, nil
}

func deleteRegKey(root registry.Key, path string, view uint32) error {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|view)
	if err != nil {
		return err
	}
	subs, _ := k.ReadSubKeyNames(1)
	k.Close()
	if len(subs) > 0 {
		return fmt.Errorf("%s has subkeys; set recursive to delete them too", path)
	}
	parent, name := "", path
	if i := strings.LastIndex(path, `\`); i >= 0 {
		parent, name = path[:i], path[i+1:]
	}
	pk, err := registry.OpenKey(root, parent, registry.ALL_ACCESS|view)
	if err != nil {
		return err
	}
	defer pk.Close()
	return registry.DeleteKey(pk, name)
}

func deleteRegTree(root registry.Key, path string, view uint32) error {
	k, err := registry.OpenKey(root, path, registry.ENUMERATE_SUB_KEYS|view)
	if err != nil {
		return err
	}
	subs, err := k.ReadSubKeyNames(-1)
	k.Close()
	if err != nil {
		return err
	}
	for _, s := range subs {
		if err := deleteRegTree(root, path+`\`+s, view); err != nil {
			return err
		}
	}
	return deleteRegKey(root, path, view)
}
