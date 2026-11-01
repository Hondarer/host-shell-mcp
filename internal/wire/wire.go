// Package wire holds the names and payloads that host-shell bridge and the target's tools agree
// on, so the bridge does not depend on the tool implementations.
package wire

// BridgeClientName is the client name host-shell bridge connects with.
const BridgeClientName = "host-shell-bridge"

// TransferChunkBytes is the size of each read or write when the bridge transfers files.
const TransferChunkBytes = 1 << 20

// ManifestEntry is the path and hash of one file.
type ManifestEntry struct {
	Path   string `json:"path" jsonschema:"path relative to root, with forward slashes"`
	Size   int64  `json:"size"`
	Mode   string `json:"mode" jsonschema:"permission bits in octal, e.g. 0644"`
	SHA256 string `json:"sha256"`
}

// FsManifestOutput is the output of fs_manifest.
type FsManifestOutput struct {
	Root      string          `json:"root"`
	Files     []ManifestEntry `json:"files"`
	Skipped   int             `json:"skipped,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
}
