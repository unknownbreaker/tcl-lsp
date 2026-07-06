# Releasing tcl-lsp

Consumers of this plugin download a prebuilt server binary from a GitHub Release
(see `lua/tcl-lsp/download.lua`); they never compile it. This doc is for the
maintainer cutting those releases.

## One command

From `server/`:

```bash
make publish VERSION=v0.2.0
```

That does everything:

1. `make release` — cross-compiles the four supported targets
   (`darwin-arm64`, `darwin-amd64`, `linux-amd64`, `linux-arm64`), stripped
   (`-s -w -trimpath`, ~2.6 MB each), into `server/dist/`, and writes
   `SHA256SUMS`.
2. `gh release create <VERSION> dist/* --generate-notes` — publishes the
   binaries and the checksum file as a GitHub Release. (Needs `gh` logged in.)
3. Rewrites `lua/tcl-lsp/version.lua` to the new tag.

Then **commit and push** the bumped `version.lua`:

```bash
git add lua/tcl-lsp/version.lua
git commit -m "release: v0.2.0"
git push
```

## Why the version bump matters

`version.lua` pins which release the plugin fetches, so the Lua client and the
server binary stay in lockstep. When a colleague runs `:Lazy update`, they pull
the new `version.lua`, and on next load the plugin downloads the matching binary
(caching it under `os.UserCacheDir()/tcl-lsp/<version>/`). Publishing the release
*without* bumping and pushing `version.lua` means nobody picks up the new binary;
bumping *without* publishing means the download 404s and the client falls back to
building from source.

## Asset naming (do not change casually)

The plugin builds the download URL from these exact names:

```
tcl-lsp-<os>-<arch>      os ∈ {darwin, linux}   arch ∈ {arm64, amd64}
SHA256SUMS
```

If you add a platform, update both `server/Makefile` (`release` target) and the
platform map in `lua/tcl-lsp/download.lua`.

## Building artifacts without publishing

`make release` alone builds `server/dist/` (gitignored) for inspection — handy
to eyeball sizes or test the download path against a local file.
