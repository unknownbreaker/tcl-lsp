# tcl-lsp — editor setup

A Language Server for TCL/RVT providing **goto-definition** and
**goto-references** across a workspace. Both `.tcl` and `.rvt` (Rivet)
files are supported. Proc-local goto-definition is precise (reaching-definitions:
a `$x` jumps to the assignment(s) that actually reach it). Works in Neovim and
classic Vim.

## 1. Build the binary

**Most users can skip this** — both the Neovim plugin and the bundled Vim config
download a prebuilt server binary automatically (see step 2). You need this
section only for a manual install, an unsupported platform, or building from
source for development.

Requires Go 1.23+ and `make`.

```sh
cd server
make build            # produces ./tcl-lsp for your platform
# or cross-compile every platform into ./dist:
make dist             # ./dist/tcl-lsp-{linux,darwin}-{amd64,arm64}, windows-amd64.exe
```

Install it on your PATH, e.g.:

```sh
mkdir -p ~/.local/bin
cp server/tcl-lsp ~/.local/bin/tcl-lsp
```

**No Go on the target machine?** Cross-compile elsewhere with `make dist`, then
copy the matching static binary over (`uname -m`: `x86_64` → `-linux-amd64`,
`aarch64` → `-linux-arm64`):

```sh
scp server/dist/tcl-lsp-linux-amd64  <host>:~/.local/bin/tcl-lsp
ssh <host> chmod +x ~/.local/bin/tcl-lsp
```

## 2. Configure your editor

### Neovim / LazyVim (0.11+, lazy.nvim)

Copy `editors/nvim/tcl-lsp.lua` to `~/.config/nvim/lua/plugins/tcl-lsp.lua` and
restart Neovim. That's the whole install. lazy.nvim clones the repo and, via the
spec's `opts`, calls `require("tcl-lsp").setup(opts)`; the plugin then **downloads
the prebuilt server binary** for your OS/arch from a GitHub Release (needs `curl`
or `wget`), verifies its SHA-256, caches it under `stdpath("cache")/tcl-lsp/`, and
wires it into Neovim's native LSP. No toolchain, no PATH setup, no binary path to
maintain — and every launch after the first is instant (cache hit). `go` + `make`
are only a fallback (unsupported platform, offline, or building from source).

The spec loads on the `tcl`/`rvt` filetypes (`ft = { "tcl", "rvt" }`) and exposes
a documented `opts` table — `filetypes`, `root_markers`, `cmd` (override the
binary), `auto_build` — all optional, defaults shown inline. Edit those to
customize; leave `opts = {}` for defaults.

Open a `.tcl` file and use `gd` (goto-definition) and your references keymap
(LazyVim: `grr`; stock Neovim: `:lua vim.lsp.buf.references()`).

When a plugin update bumps the pinned server version, the next load downloads the
new binary; run `:LspRestart` afterward to swap the running process. (Developing
the server yourself? See Mode B below — build locally and `:LspRestart`.)

> **Developing the LSP itself?** The file ships a commented **Mode B** spec that
> points at your local working clone (`dir = …`) instead of a lazy-managed one —
> swap to it so your edits + `make watch` / `:TclLspRebuild` drive the server.
>
> The spec is standalone (Neovim 0.11 native `vim.lsp.config`/`vim.lsp.enable`)
> on purpose: merging into LazyVim's `nvim-lspconfig` spec does not reliably run
> the setup, so the server would never start. Confirm attachment with
> `:checkhealth lsp` (look for `tcl_lsp`).

### Vim

Vim has no built-in LSP client, so pick one — **vim-lsp** (lightweight, pure
Vimscript) or **coc.nvim** (heavier, needs Node, but supports the full LSP
surface and also runs on Neovim). Either way:

- **The server binary.** The bundled **vim-lsp** config downloads it for you (see
  below). For **coc**, get it first (step 1, or download a release asset) — put
  `tcl-lsp` on your PATH, or point the config at the absolute binary path.
- **`.rvt` detection** ships in `ftdetect/rvt.vim` (loaded automatically when the
  repo is on your plugin runtimepath; it also benefits non-lazy Neovim). If you
  are not installing the repo as a plugin, add to your vimrc:
  `autocmd BufRead,BufNewFile *.rvt setfiletype rvt`.

**vim-lsp** — install `vim-lsp` (+ `async.vim`), then `source` the bundled config
from your vimrc:

```vim
source /path/to/tcl-lsp/editors/vim/tcl-lsp.vim
```

It registers the server for `tcl`/`rvt`, finds the project root **`.git`-first**
(see the note below), and — like the Neovim plugin — **downloads the prebuilt
server binary** for your platform, verifies its SHA-256, and caches it under
`~/.cache/tcl-lsp/` (needs `curl` or `wget`; `go` + `make` are only a fallback).
Point `g:tcl_lsp_cmd` at a specific binary to skip the download, or set
`g:tcl_lsp_auto_build = 0` to disable the source-build fallback. Use
`:LspDefinition` and `:LspReferences`.

**coc.nvim** — merge `editors/vim/coc-settings.json` into your coc config
(`:CocConfig`). coc supports `workspace/didChangeWatchedFiles`, so on-disk changes
re-index live (the same behavior Neovim gets). coc just runs the binary, so build
it yourself (step 1) and rebuild after pulling new server code.

> **Root detection matters — same fragmentation trap as Neovim.** Cross-file
> resolution (`.tcl` definitions ↔ `.rvt` call sites) only works when one server
> indexes the whole project. The bundled vim-lsp config resolves the root to the
> nearest `.git`, falling back to `pkgIndex.tcl` only when there is no `.git`.
> The coc example uses `"rootPatterns": [".git"]` for the same reason — coc roots
> at the *nearest* matching dir, so adding `pkgIndex.tcl` there would let a
> per-package dir win and hide references. If you do not use git, swap in
> `pkgIndex.tcl`.

**Caveats vs. Neovim.**
- **Auto-download on update:** the bundled **vim-lsp** config fetches the pinned
  prebuilt binary (via `autoload/tcl_lsp.vim`, the Vimscript port of the download
  logic); **coc** does not — install the binary yourself and update it after a
  new release.
- **Live on-disk re-indexing** (`didChangeWatchedFiles`) works under **coc** but
  **not vim-lsp**; with vim-lsp the server still indexes at startup and on open,
  so you only miss files changed on disk without being opened.

## 3. Try it

Open `examples/main.tcl`, put the cursor on `::math::square` and goto-definition
— it should jump to `examples/math.tcl`. Goto-references on `square` (in
`math.tcl`) should list the call in `main.tcl`.

For `.rvt` templates, open `examples/page.rvt`, put the cursor on `render_title`
in the `<?= [render_title $title] ?>` line and goto-definition — it should jump to
the `proc render_title` definition earlier in the same file.

## Verify the server itself

```sh
cd server
go test ./...          # unit + end-to-end (the cmd/tcl-lsp smoke test builds and drives the binary)
```

## Known limitations

- Array-element locals (`set arr(i)` / `$arr(i)`) are not yet resolved (a
  high-priority follow-up; see
  `docs/superpowers/specs/2026-06-22-proc-local-variable-resolution-design.md`).
- `namespace path` command search, `source` include-following, and `::rivet::`
  built-in command resolution are deferred (see
  `docs/plans/2026-06-08-phase-b-rvt-design.md` §9).

Proc-local variables (params, `set`/`incr`/`append`/`lappend`,
`foreach`/`lmap`/`lassign`/`dict for` targets, `upvar`/`global`/`variable`
links) now resolve within their enclosing proc.
