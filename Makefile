VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X github.com/hondarer/host-shell-mcp/internal/version.Version=$(VERSION)
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)"

# Targets (host-shell --mcp) are Linux and Windows. The local side (host-shell bridge) also runs on macOS.
TARGETS := linux-amd64 linux-arm64 windows-amd64 windows-arm64 darwin-amd64 darwin-arm64

# Copies of the release executables, so the host-shell-connect skill works on its own
SKILL_DIR := .agents/skills/host-shell-connect
SKILL_BIN := $(SKILL_DIR)/bin

# Files to attach to a Release
RELEASE_DIR := dist/release

.PHONY: all build dist skill-bin release test vet clean $(TARGETS)

all: vet test build

# Build for the development machine
build:
	$(GOBUILD) -o dist/host-shell$(if $(filter windows,$(shell go env GOOS)),.exe) ./cmd/host-shell

# Build one executable per OS and architecture for distribution, and copy them into the skill
dist: $(TARGETS)
	$(MAKE) skill-bin

skill-bin:
	mkdir -p $(SKILL_BIN)
	cp dist/host-shell-*-* $(SKILL_BIN)/
	echo $(VERSION) > $(SKILL_BIN)/VERSION

$(TARGETS):
	GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) \
		$(GOBUILD) -o dist/host-shell-$@$(if $(findstring windows,$@),.exe) ./cmd/host-shell

# Build the files to attach to a Release: the source tree laid out like GitHub's own source archives
# (top directory host-shell-mcp-<version without a leading v>), plus dist/ and the skill's bin/, as zip and tar.gz.
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
