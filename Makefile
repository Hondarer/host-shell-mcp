VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hondarer/host-shell-mcp/internal/version.Version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

TARGETS := linux-amd64 linux-arm64 windows-amd64 windows-arm64

.PHONY: all build dist test vet clean $(TARGETS)

all: vet test build

# 開発機向けのビルド
build:
	$(GOBUILD) -o dist/host-shell$(if $(filter windows,$(shell go env GOOS)),.exe) ./cmd/host-shell

# 配布用に OS とアーキテクチャごとに 1 ファイルずつ作る
dist: $(TARGETS)

$(TARGETS):
	GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) \
		$(GOBUILD) -o dist/host-shell-$@$(if $(findstring windows,$@),.exe) ./cmd/host-shell

test:
	go test ./...

vet:
	go vet ./...
	GOOS=windows go vet ./...

clean:
	rm -rf dist
