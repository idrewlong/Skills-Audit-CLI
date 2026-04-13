BINARY     := skill-mgr
MODULE     := github.com/idrewlong/skill-mgr
CMD        := ./cmd/skill-mgr
VERSION    := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS    := -ldflags "-X main.version=$(VERSION) -s -w"
INSTALL_DIR := $(HOME)/.local/bin

.PHONY: all build install uninstall test lint clean release snapshot

all: build

## build: compile binary for current platform
build:
	go build $(LDFLAGS) -o $(BINARY) $(CMD)
	@echo "Built: ./$(BINARY)"

## install: install to ~/.local/bin (or /usr/local/bin with sudo)
install: build
	@mkdir -p $(INSTALL_DIR)
	cp $(BINARY) $(INSTALL_DIR)/$(BINARY)
	@echo "Installed to $(INSTALL_DIR)/$(BINARY)"
	@echo "Make sure $(INSTALL_DIR) is in your PATH"

## uninstall: remove from install dir
uninstall:
	rm -f $(INSTALL_DIR)/$(BINARY)
	@echo "Removed $(INSTALL_DIR)/$(BINARY)"

## test: run all tests
test:
	go test ./... -v

## lint: run go vet
lint:
	go vet ./...

## clean: remove build artifacts
clean:
	rm -f $(BINARY)
	rm -rf dist/

## release: build for all platforms via goreleaser
release:
	goreleaser release --clean

## snapshot: local goreleaser snapshot (no publish)
snapshot:
	goreleaser release --snapshot --clean

## build-all: cross-compile for common platforms
build-all:
	GOOS=darwin  GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)_darwin_amd64  $(CMD)
	GOOS=darwin  GOARCH=arm64  go build $(LDFLAGS) -o dist/$(BINARY)_darwin_arm64  $(CMD)
	GOOS=linux   GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)_linux_amd64   $(CMD)
	GOOS=linux   GOARCH=arm64  go build $(LDFLAGS) -o dist/$(BINARY)_linux_arm64   $(CMD)
	GOOS=windows GOARCH=amd64  go build $(LDFLAGS) -o dist/$(BINARY)_windows_amd64.exe $(CMD)
	@echo "Cross-compiled binaries in dist/"

help:
	@grep -E '^## ' $(MAKEFILE_LIST) | sed 's/## /  /'
