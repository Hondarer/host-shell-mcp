// Package version はビルド時に埋め込むバージョン情報を保持します。
package version

// Version はリリース ビルドで -ldflags "-X github.com/hondarer/host-shell-mcp/internal/version.Version=..." により上書きします。
var Version = "dev"
