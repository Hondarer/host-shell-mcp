VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hondarer/host-shell-mcp/internal/version.Version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

# Remote targets (host-shell --mcp) run on Linux and Windows; local bridge mode (host-shell bridge) also supports macOS.
TARGETS := linux-amd64 linux-arm64 windows-amd64 windows-arm64 darwin-amd64 darwin-arm64

# Bundled binaries for standalone operation of the host-shell-connect skill
SKILL_DIR := .agents/skills/host-shell-connect
SKILL_BIN := $(SKILL_DIR)/bin

# Output directory for release archives
RELEASE_DIR := dist/release

.PHONY: all build dist skill-bin release test vet clean $(TARGETS)

all: vet test build

# Build for the local host OS/architecture
build:
	$(GOBUILD) -o dist/host-shell$(if $(filter windows,$(shell go env GOOS)),.exe) ./cmd/host-shell

# Cross-compile all target binaries and populate the skill bin/ directory
dist: $(TARGETS)
	$(MAKE) skill-bin

skill-bin:
	mkdir -p $(SKILL_BIN)
	cp dist/host-shell-*-* $(SKILL_BIN)/
	echo $(VERSION) > $(SKILL_BIN)/VERSION

$(TARGETS):
	GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) \
		$(GOBUILD) -o dist/host-shell-$@$(if $(findstring windows,$@),.exe) ./cmd/host-shell

# Package release archives (zip and tar.gz) matching GitHub source archive layout
# (root directory host-shell-mcp-<version without leading v>) with prebuilt binaries in dist/ and $(SKILL_BIN)/.
ARCHIVE := host-shell-mcp-$(patsubst v%,%,$(VERSION))
STAGE := $(RELEASE_DIR)/stage

release: dist
	rm -rf $(RELEASE_DIR)
	mkdir -p $(STAGE)/$(ARCHIVE)/dist
	git archive HEAD | tar -x -C $(STAGE)/$(ARCHIVE)
	cp dist/host-shell-*-* $(STAGE)/$(ARCHIVE)/dist/
	cp -R $(SKILL_BIN) $(STAGE)/$(ARCHIVE)/$(SKILL_DIR)/
	tar -C $(STAGE) -czf $(RELEASE_DIR)/$(ARCHIVE)-bin.tar.gz $(ARCHIVE)
	cd $(STAGE) && zip -qr ../$(ARCHIVE)-bin.zip $(ARCHIVE)
	rm -rf $(STAGE)

test:
	go test ./...

vet:
	go vet ./...
	GOOS=windows go vet ./...
	GOOS=darwin go vet ./...

clean:
	rm -rf dist $(SKILL_BIN)
