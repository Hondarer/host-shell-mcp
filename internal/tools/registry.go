package tools

// RegReadInput is the input of reg_read.
type RegReadInput struct {
	Key   string  `json:"key" jsonschema:"registry key, e.g. HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion or HKCU\\Environment"`
	Value *string `json:"value,omitempty" jsonschema:"read only this value (empty string for the default value). Omit to list subkeys and all values"`
	View  string  `json:"view,omitempty" jsonschema:"64 or 32 to select the registry view on 64-bit Windows (default: native)"`
}

// RegValue is a registry value.
type RegValue struct {
	Name     string `json:"name" jsonschema:"value name (empty for the default value)"`
	Type     string `json:"type" jsonschema:"REG_SZ, REG_EXPAND_SZ, REG_MULTI_SZ, REG_DWORD, REG_QWORD, REG_BINARY, ..."`
	Data     any    `json:"data" jsonschema:"string, array of strings (REG_MULTI_SZ), number (REG_DWORD, REG_QWORD) or base64 (binary)"`
	Expanded string `json:"expanded,omitempty" jsonschema:"REG_EXPAND_SZ with environment variables expanded"`
}

// RegReadOutput is the output of reg_read.
type RegReadOutput struct {
	Key     string     `json:"key"`
	Subkeys []string   `json:"subkeys,omitempty"`
	Values  []RegValue `json:"values"`
}

// RegWriteInput is the input of reg_write.
type RegWriteInput struct {
	Key   string  `json:"key" jsonschema:"registry key; created if missing"`
	Value *string `json:"value,omitempty" jsonschema:"value name (empty string for the default value). Omit to only create the key"`
	Type  string  `json:"type,omitempty" jsonschema:"REG_SZ (default), REG_EXPAND_SZ, REG_MULTI_SZ, REG_DWORD, REG_QWORD or REG_BINARY"`
	Data  any     `json:"data,omitempty" jsonschema:"string; array of strings for REG_MULTI_SZ; number for REG_DWORD/REG_QWORD; base64 string for REG_BINARY"`
	View  string  `json:"view,omitempty" jsonschema:"64 or 32 registry view"`
}

// RegWriteOutput is the output of reg_write.
type RegWriteOutput struct {
	Key        string    `json:"key"`
	CreatedKey bool      `json:"created_key,omitempty"`
	Value      *RegValue `json:"value,omitempty" jsonschema:"the value as stored"`
}

// RegDeleteInput is the input of reg_delete.
type RegDeleteInput struct {
	Key       string  `json:"key" jsonschema:"registry key"`
	Value     *string `json:"value,omitempty" jsonschema:"delete this value (empty string for the default value) instead of the key"`
	Recursive bool    `json:"recursive,omitempty" jsonschema:"delete the key with all its subkeys (without it, a key with subkeys is not deleted)"`
	View      string  `json:"view,omitempty" jsonschema:"64 or 32 registry view"`
}

// RegDeleteOutput is the output of reg_delete.
type RegDeleteOutput struct {
	Deleted string `json:"deleted"`
}
