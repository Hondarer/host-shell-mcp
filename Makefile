VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hondarer/host-shell-mcp/internal/version.Version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

TARGETS := linux-amd64 linux-arm64 windows-amd64 windows-arm64

# host-shell-connect skill が単独で使えるよう、配布用の実行ファイルを複製する場所
SKILL_DIR := .agents/skills/host-shell-connect
SKILL_BIN := $(SKILL_DIR)/bin

# Release に添付するファイルを置く場所
RELEASE_DIR := dist/release

.PHONY: all build dist skill-bin release test vet clean $(TARGETS)

all: vet test build

# 開発機向けのビルド
build:
	$(GOBUILD) -o dist/host-shell$(if $(filter windows,$(shell go env GOOS)),.exe) ./cmd/host-shell

# 配布用に OS とアーキテクチャごとに 1 ファイルずつ作り、skill にも複製する
dist: $(TARGETS)
	$(MAKE) skill-bin

skill-bin:
	mkdir -p $(SKILL_BIN)
	cp dist/host-shell-*-* $(SKILL_BIN)/
	echo $(VERSION) > $(SKILL_BIN)/VERSION

$(TARGETS):
	GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) \
		$(GOBUILD) -o dist/host-shell-$@$(if $(findstring windows,$@),.exe) ./cmd/host-shell

# Release に添付するファイルを作る。skill のアーカイブ (bin/ を含む)、実行ファイル、SHA256SUMS
release: dist
	rm -rf $(RELEASE_DIR)
	mkdir -p $(RELEASE_DIR)
	tar -C $(dir $(SKILL_DIR)) -czf $(RELEASE_DIR)/host-shell-connect-$(VERSION).tar.gz $(notdir $(SKILL_DIR))
	cd $(dir $(SKILL_DIR)) && zip -qr $(CURDIR)/$(RELEASE_DIR)/host-shell-connect-$(VERSION).zip $(notdir $(SKILL_DIR))
	cp dist/host-shell-*-* $(RELEASE_DIR)/
	cd $(RELEASE_DIR) && sha256sum * > SHA256SUMS

test:
	go test ./...

vet:
	go vet ./...
	GOOS=windows go vet ./...

clean:
	rm -rf dist $(SKILL_BIN)
