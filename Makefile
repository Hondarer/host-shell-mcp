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

# Prebuilt remote DNS adapters for Linux targets without a C compiler.
# Each one is built natively on an old glibc (CI uses AlmaLinux 8) so it loads on older targets too.
DNS_SRC := internal/remotedns/assets/preload.c
DNS_ARCHS := amd64 arm64
DNS_LIBS := $(foreach a,$(DNS_ARCHS),dist/libhostshell_dns-linux-$(a).so)
DNS_HOST_ARCH = $(patsubst x86_64,amd64,$(patsubst aarch64,arm64,$(shell uname -m)))
# Highest glibc symbol version an adapter may require (the aarch64 baseline is 2.17)
DNS_GLIBC_MAX := 2.17

.PHONY: all build dist skill-bin dns-lib release test vet clean $(TARGETS)

all: vet test build

# Build for the local host OS/architecture
build:
	$(GOBUILD) -o dist/host-shell$(if $(filter windows,$(shell go env GOOS)),.exe) ./cmd/host-shell

# Cross-compile all target binaries and populate the skill bin/ directory
dist: $(TARGETS)
	$(MAKE) skill-bin

skill-bin:
	mkdir -p $(SKILL_BIN)
	cp dist/host-shell-*-* $(wildcard dist/libhostshell_dns-linux-*.so) $(SKILL_BIN)/
	echo $(VERSION) > $(SKILL_BIN)/VERSION

$(TARGETS):
	GOOS=$(word 1,$(subst -, ,$@)) GOARCH=$(word 2,$(subst -, ,$@)) \
		$(GOBUILD) -o dist/host-shell-$@$(if $(findstring windows,$@),.exe) ./cmd/host-shell

# Build the remote DNS adapter for the host architecture (Linux only)
dns-lib: dist/libhostshell_dns-linux-$(DNS_HOST_ARCH).so

# Only the host architecture can be built here; release builds receive the others from CI.
# The symbol check rejects adapters that would not load on targets with an older glibc.
dist/libhostshell_dns-linux-%.so: $(DNS_SRC)
	@test "$*" = "$(DNS_HOST_ARCH)" || { echo "$@: build it on Linux/$* with 'make dns-lib'" >&2; exit 1; }
	mkdir -p dist
	$(CC) -Wall -Wextra -Werror -O2 -fPIC -shared -o $@ $< -ldl -pthread
	@v=$$(objdump -T $@ | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/^GLIBC_//' | sort -uV | tail -n 1); \
	if [ -z "$$v" ] || [ "$$(printf '%s\n%s\n' "$$v" "$(DNS_GLIBC_MAX)" | sort -V | tail -n 1)" != "$(DNS_GLIBC_MAX)" ]; then \
		echo "$@ requires GLIBC_$$v (limit $(DNS_GLIBC_MAX)); build on an older glibc" >&2; rm -f $@; exit 1; \
	fi; \
	echo "$@: highest glibc symbol version $$v"

# Package release archives (zip and tar.gz) matching GitHub source archive layout
# (root directory host-shell-mcp-<tag>, including a leading v) with prebuilt binaries and
# remote DNS adapters in dist/ and $(SKILL_BIN)/.
# Tag v20261031.1.0 produces host-shell-mcp-v20261031.1.0-bin.zip and .tar.gz.
ARCHIVE := host-shell-mcp-$(VERSION)
STAGE := $(RELEASE_DIR)/stage

release: $(DNS_LIBS) dist
	rm -rf $(RELEASE_DIR)
	mkdir -p $(STAGE)/$(ARCHIVE)/dist
	git archive HEAD | tar -x -C $(STAGE)/$(ARCHIVE)
	cp dist/host-shell-*-* $(DNS_LIBS) $(STAGE)/$(ARCHIVE)/dist/
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
