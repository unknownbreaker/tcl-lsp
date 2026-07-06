BINARY := tcl-lsp
PKG := ./cmd/tcl-lsp
DIST := dist
BINDIR := $(HOME)/.local/bin
# Strip symbol/debug info from published binaries (smaller downloads); trimpath
# drops local filesystem paths for reproducibility.
RELEASE_FLAGS := -trimpath -ldflags "-s -w"

.PHONY: build install watch dist release publish test vet clean

build:
	go build -o $(BINARY) $(PKG)

# Build, then copy the binary to BINDIR (default ~/.local/bin) -- where the
# shipped editor configs (editors/nvim, editors/vim) expect to find it.
# Override the destination with e.g. `make install BINDIR=/usr/local/bin`.
install: build
	mkdir -p $(BINDIR)
	cp $(BINARY) $(BINDIR)/$(BINARY)
	@echo "installed $(BINARY) to $(BINDIR)/$(BINARY)"

# Rebuild + reinstall on every change to a server .go file (run in a spare
# terminal for live testing). Uses watchexec, else entr; needs one installed
# (brew install watchexec).
# NOTE: a rebuild does NOT update an already-running LSP -- restart it in the
# editor afterwards (`:LspRestart` in Neovim) to load the new binary.
watch:
	@if command -v watchexec >/dev/null 2>&1; then \
		watchexec -w . -e go -- $(MAKE) install; \
	elif command -v entr >/dev/null 2>&1; then \
		fd -e go . | entr -r $(MAKE) install; \
	else \
		echo "no file watcher found -- install one: brew install watchexec"; \
		exit 1; \
	fi

test:
	go test ./...

vet:
	go vet ./...

# Cross-compiled release binaries (no cgo, so this just works).
dist:
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=arm64 go build -o $(DIST)/$(BINARY)-darwin-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin  GOARCH=amd64 go build -o $(DIST)/$(BINARY)-darwin-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64 go build -o $(DIST)/$(BINARY)-linux-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64 go build -o $(DIST)/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o $(DIST)/$(BINARY)-windows-amd64.exe $(PKG)

# release: the stripped, per-platform binaries the plugin downloads, plus a
# SHA256SUMS the client verifies against. Asset names are tcl-lsp-<os>-<arch>,
# matching lua/tcl-lsp/download.lua. Publish these with `make publish`.
release:
	rm -rf $(DIST)
	mkdir -p $(DIST)
	CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build $(RELEASE_FLAGS) -o $(DIST)/$(BINARY)-darwin-arm64 $(PKG)
	CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build $(RELEASE_FLAGS) -o $(DIST)/$(BINARY)-darwin-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux  GOARCH=amd64 go build $(RELEASE_FLAGS) -o $(DIST)/$(BINARY)-linux-amd64 $(PKG)
	CGO_ENABLED=0 GOOS=linux  GOARCH=arm64 go build $(RELEASE_FLAGS) -o $(DIST)/$(BINARY)-linux-arm64 $(PKG)
	cd $(DIST) && { sha256sum $(BINARY)-* 2>/dev/null || shasum -a 256 $(BINARY)-*; } > SHA256SUMS
	@echo "built $(DIST)/ — publish with:  make publish VERSION=vX.Y.Z"

# publish: cut a GitHub Release from the built artifacts and pin the plugin to it.
# Requires `gh` (logged in). Bumps lua/tcl-lsp/version.lua; commit + push after.
publish:
	@test -n "$(VERSION)" || { echo "usage: make publish VERSION=vX.Y.Z"; exit 1; }
	$(MAKE) release
	gh release create $(VERSION) $(DIST)/* --title $(VERSION) --generate-notes
	sed -i.bak 's/^return ".*"/return "$(VERSION)"/' ../lua/tcl-lsp/version.lua && rm -f ../lua/tcl-lsp/version.lua.bak
	@echo "published $(VERSION) and bumped lua/tcl-lsp/version.lua — commit + push it."

clean:
	rm -f $(BINARY)
	rm -rf $(DIST)
