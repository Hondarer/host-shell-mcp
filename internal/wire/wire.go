// Package wire holds the names and payloads that host-shell bridge and the target's tools agree
// on, so the bridge does not depend on the tool implementations.
package wire

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"os"
)

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

// HashFile returns the size and hex SHA-256 of the file at path, the digest ManifestEntry.SHA256
// carries, so both ends compare files the same way.
func HashFile(path string) (size int64, sum string, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	if size, err = io.Copy(h, f); err != nil {
		return 0, "", err
	}
	return size, hex.EncodeToString(h.Sum(nil)), nil
}
