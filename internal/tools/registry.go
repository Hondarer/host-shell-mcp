package tools

// RegReadInput is the input of reg_read.
type RegReadInput struct {
	Key   string  `json:"key" jsonschema:"registry key, e.g. HKLM\\SOFTWARE\\Microsoft\\Windows NT\\CurrentVersion or HKCU\\Environment"`
	Value *string `json:"value,omitempty" jsonschema:"value name to read (empty string for default value; omit to list subkeys and all values)"`
	View  string  `json:"view,omitempty" jsonschema:"64 or 32 WOW64 registry view (default: native)"`
}

// RegValue is a registry value.
type RegValue struct {
	Name     string `json:"name" jsonschema:"value name (empty for default value)"`
	Type     string `json:"type" jsonschema:"REG_SZ, REG_EXPAND_SZ, REG_MULTI_SZ, REG_DWORD, REG_QWORD, REG_BINARY, ..."`
	Data     any    `json:"data" jsonschema:"string, string array (REG_MULTI_SZ), number (REG_DWORD, REG_QWORD) or base64 (binary)"`
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
	Key   string  `json:"key" jsonschema:"registry key (created if missing)"`
	Value *string `json:"value,omitempty" jsonschema:"value name (empty string for default value; omit to only create the key)"`
	Type  string  `json:"type,omitempty" jsonschema:"REG_SZ (default), REG_EXPAND_SZ, REG_MULTI_SZ, REG_DWORD, REG_QWORD or REG_BINARY"`
	Data  any     `json:"data,omitempty" jsonschema:"string, string array (REG_MULTI_SZ), number (REG_DWORD, REG_QWORD) or base64 string (REG_BINARY)"`
	View  string  `json:"view,omitempty" jsonschema:"64 or 32 registry view"`
}

// RegWriteOutput is the output of reg_write.
type RegWriteOutput struct {
	Key        string    `json:"key"`
	CreatedKey bool      `json:"created_key,omitempty"`
	Value      *RegValue `json:"value,omitempty" jsonschema:"stored value"`
}

// RegDeleteInput is the input of reg_delete.
type RegDeleteInput struct {
	Key       string  `json:"key" jsonschema:"registry key"`
	Value     *string `json:"value,omitempty" jsonschema:"value name to delete (empty string for default value; omit to delete the key)"`
	Recursive bool    `json:"recursive,omitempty" jsonschema:"delete the key and all its subkeys"`
	View      string  `json:"view,omitempty" jsonschema:"64 or 32 registry view"`
}

// RegDeleteOutput is the output of reg_delete.
type RegDeleteOutput struct {
	Deleted string `json:"deleted"`
}
