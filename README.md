# tcl-lsp

A focused Language Server for **TCL** and **RVT** (Apache Rivet templates).
One self-contained Go binary, with clients for **Neovim** and **classic Vim**.

Scope is deliberately tight: a few features done well — scope-correct,
cross-file, `.rvt`-aware, Itcl-aware — rather than a broad, shallow set.

## Features

| LSP feature                  |    |
| ---------------------------- | :-: |
| Go to definition             | ✅ |
| Find references              | ✅ |
| Document / workspace symbols | ✅ |
| Call hierarchy               | ✅ |
| Code folding                 | ✅ |
| Document highlight           | ✅ |
| Selection range              | ✅ |
| Semantic tokens              | ✅ |
| Itcl ([incr Tcl]) OO         | ✅ |

**The one rule** everything follows:

- Report only what is derivable with certainty from structure and the index.
- Stay silent otherwise — never assert something that can be wrong.
- Example: semantic tokens color only calls that resolve to indexed source;
  builtins keep normal syntax highlighting instead of being mis-colored.

**Out of scope by design** (see [Why a v2 reset](#why-a-v2-reset)):

- hover, completion, signature help
- rename, formatting, code actions, inlay hints
- diagnostics

**Beyond regex/ctags:**

- **Cross-file & `.rvt`-aware** — resolution flows `.rvt` ⇄ `.tcl`.
- **Reaching-definitions** — `$x` jumps to the assignment(s) that actually
  reach it, through loops and conditionals.
- **Scope-correct** — namespaces, `namespace path`/`import`,
  `global`/`upvar`/`variable` link-chasing, arrays.
- **Itcl OO** — classes, methods, ivars, inheritance, `$obj method` receiver
  calls. See [Itcl OO support](#itcl-oo-support).

## Quick start

No toolchain needed for either editor:

- On first load, the client downloads the prebuilt server binary for your
  OS/arch from a GitHub Release (needs `curl` or `wget`).
- The download is SHA-256-verified, then cached — every later launch is
  instant and offline.
- Prebuilt targets: macOS (arm64/amd64), Linux (amd64/arm64). Anything else
  falls back to building from source (`go` + `make`).

### Neovim (≥ 0.11, lazy.nvim / LazyVim)

1. Copy the spec below to `~/.config/nvim/lua/plugins/tcl-lsp.lua`.
2. Adjust `extra_index_paths` to your machine (see comments).
3. Restart Neovim and open any `.tcl` or `.rvt` file.
4. Verify: put the cursor on a proc call and press `gd`.

```lua
{
  "unknownbreaker/tcl-lsp",
  ft = { "tcl", "rvt" },
  init = function()
    vim.filetype.add({ extension = { tcl = "tcl", rvt = "rvt" } }) -- map .rvt before load
  end,
  opts = {
    -- LSP code folding for tcl/rvt buffers (other filetypes untouched).
    folding = true,

    -- External TCL sources to index alongside your project. Entries that
    -- don't exist on a machine are skipped, so one config is safe to share.
    extra_index_paths = {
      "/opt/homebrew/opt/tcl-tk/lib", -- Tcl's script library + tcllib (macOS/Homebrew)
      -- "/usr/share/tcltk",          -- common Linux equivalent
      -- "/opt/fa/tcl-lib",           -- your company packages / other TCL repos
    },
  },
}
```

> Find your Tcl library path with: `echo 'puts $tcl_library' | tclsh`

A fully-commented spec (plus a dev/local-clone variant) lives at
[`editors/nvim/tcl-lsp.lua`](editors/nvim/tcl-lsp.lua).

### Classic Vim (vim-lsp)

1. Install [vim-lsp](https://github.com/prabirshrestha/vim-lsp) (+ async.vim).
2. Clone this repo somewhere, e.g. `~/tools/tcl-lsp`.
3. Add to your vimrc (paths before the `source` line):

```vim
let g:tcl_lsp_extra_index_paths = ['/opt/homebrew/opt/tcl-tk/lib', '/opt/fa/tcl-lib']
source ~/tools/tcl-lsp/editors/vim/tcl-lsp.vim
```

4. Restart Vim. Use `:LspDefinition` / `:LspReferences` (or your vim-lsp maps).

### Other setups

- **packer**, **vim-plug**, **coc.nvim**: see
  [`editors/README.md`](editors/README.md).
- Maintainers cutting releases: see [`docs/RELEASING.md`](docs/RELEASING.md).

## What you get

With the quick-start config, using LazyVim's standard keys as examples:

| Key | Does | Example |
| --- | --- | --- |
| `gd` | goto definition, cross-file and cross-language | `render_header` in a `.rvt` jumps to its `proc` in a `.tcl` |
| `gd` | …including into indexed *library* code | `parray` jumps into Tcl's own `parray.tcl` |
| `gd` on `$x` | jumps to the assignment that actually *reaches* it | not just the first `set x` in the file |
| `grr` | find references, including `.rvt` call sites | see caveat below |
| `<leader>ci` / `<leader>co` | incoming / outgoing call hierarchy | works for procs and Itcl methods |
| `za` | fold the body under the cursor | procs, namespaces, classes, `if`/`foreach`, `.rvt` `<? ?>` regions |
| `<leader>ss` | project-wide symbol search | procs, namespace vars, classes, methods |
| `gO` | file outline, in source order | outline panels (`aerial.nvim`) pick it up too |

**How to read the colors** (semantic tokens):

- A **colored** call = resolves to indexed source = `gd` will work on it.
- A **plain** call = C-implemented (`puts`, `string`), runtime-generated, or
  dynamic — nothing to jump to, by the nature of the thing.

**Honest caveats:**

- Treat find-references results as *"at least these."* Dynamically-built calls
  (`$cmd args`, `eval $s`) are invisible to any static tool, in any language.
- Before deleting or renaming a proc, back the LSP up with a plain `grep`.
- Call hierarchy traces bare and qualified calls; explicit `$obj method` edges
  aren't traced yet.
- Folding is curated: script bodies only — not arg lists, expressions, or data
  braces.

## Configuration reference

Pass options via lazy.nvim's `opts`, or call `require("tcl-lsp").setup({ … })`
directly. Everything is optional; defaults shown.

```lua
require("tcl-lsp").setup({
  filetypes    = { "tcl", "rvt" },           -- buffers the server attaches to
  root_markers = { ".git", "pkgIndex.tcl" }, -- project root (order = priority; .git first)
  cmd          = nil,                        -- override the server binary; nil = downloaded
  auto_build   = true,                       -- source-build fallback if download unavailable

  -- Buffer-local keymaps, set on attach (tcl/rvt buffers only; never clobber
  -- your other maps). Default: none. The two forms mix freely.
  keymaps = {
    -- named action -> key (the plugin owns the function)
    definition      = "gd",
    references      = "grr",
    document_symbol = "gO",
    incoming_calls  = "<leader>ci",
    outgoing_calls  = "<leader>co",
    -- also: declaration, type_definition, workspace_symbol, hover
    -- (set any action to false to leave it unbound)
  },
  keys = {
    -- lazy.nvim-style escape hatch for arbitrary maps / your own functions:
    -- { "<leader>cx", function() ... end, desc = "...", mode = "n" },
  },

  folding = false, -- true: enable LSP code folding for tcl/rvt

  -- External TCL sources indexed read-only at startup (see next section).
  extra_index_paths = {}, -- e.g. { "/opt/fa/tcl-lib", vim.fn.expand("~/Repos/fa-tcl") }
})
```

Notes:

- Each `keymaps` entry carries a `desc`, so **which-key** lists it
  automatically.
- Leave `keymaps` unset and your editor's existing LSP maps (LazyVim's
  `gd`/`grr`/`<leader>ss`…) keep working.
- Prefer wiring folding yourself instead of `folding = true`? Set
  `vim.wo.foldexpr = "v:lua.vim.lsp.foldexpr()"` (+ `foldmethod=expr`) in your
  own `LspAttach`.
- Classic Vim options: `g:tcl_lsp_cmd`, `g:tcl_lsp_auto_build`,
  `g:tcl_lsp_extra_index_paths` — documented in
  [`editors/vim/tcl-lsp.vim`](editors/vim/tcl-lsp.vim).

## External packages & the Tcl library

`extra_index_paths` is how navigation reaches beyond the repo you have open —
the Tcl script library, `package require`d libraries, company package
checkouts, any other TCL repo.

**Mechanics:**

- **Static and read-only.** Each path (a directory's whole subtree, or a single
  file) is parsed at startup exactly like workspace files. `.tcl`, `.rvt`, and
  `.tm` (Tcl module) files are indexed.
- **Symlinks are followed** (with cycle protection) — point it at a Nix profile
  (`~/.nix-profile`) or a Homebrew `opt/` path and the whole symlink forest is
  indexed; jump targets keep the stable profile path, not the store path.
- **Nothing executed, nothing written, nothing stale** — the real sources are
  re-read every start.
- **Safe to share one config.** Missing paths are skipped silently; a team
  snippet can list macOS and Linux locations side by side.
- **Lives in editor config, never in a project repo** — no tool artifacts in
  company repositories.

**Out of reach by design** (no source text exists):

- C-implemented commands — `set`, `puts`, `string`, C extensions.
- Procs *generated at runtime* by package code.
- Dynamic *call sites* (`$cmd`, `eval`) — a fundamental boundary for any
  static tool, not a missing feature.

That boundary is what makes the coloring meaningful: **colored = there is
source you can jump to.**

## Indexing feedback

- On first connect, the server indexes the whole project (parallelized; a few
  seconds on a big repo).
- Progress is reported via LSP work-done progress: an `Indexing TCL workspace`
  spinner with a live file count, then `Indexed N files`.
- Statuslines that render LSP progress show it out of the box: `fidget.nvim` /
  `noice.nvim` (both LazyVim defaults), coc's `coc#status()`.
- Without one of those, you just see a brief startup pause while the first
  request waits on the index.

## Itcl OO support

The dominant Rivet/speedtables idiom — `itcl::class`, `[::C #auto]`,
`$obj method` — resolves end-to-end:

- **Classes** appear in outlines with their members.
- **Methods and ivars** resolve — inline and external `itcl::body`, with or
  without `public`/`protected`/`private` modifiers (the usual real-world form).
- **`inherit` chains** resolve through the class hierarchy.
- **`$obj method` is receiver-typed** — `set d [::STDisplay #auto]; $d field`
  types `$d` and resolves `field` on its class, including inherited members,
  as a statement or bracketed (`[$obj method]`).

**Graceful boundaries** (returns nothing rather than jumping wrong):

- **TclOO** (`oo::class`) is not supported — Itcl only.
- Receivers with no locally-known class (a parameter, a factory return, a
  cross-method ivar) stay unresolved.
- Simple `inherit`-order MRO — no C3, dynamic dispatch, or mixins.
- `public { … }` protection *blocks* aren't parsed — declare members
  individually.

Pinned against verbatim real Itcl/Rivet code (`flightaware/speedtables`,
`mxmanghi/rivetweb`, `apache/tcl-rivet`); see
[`research/07-realworld-itcl-survey.md`](research/07-realworld-itcl-survey.md).

## Why a v2 reset

- v1 (313 commits) tried to do too much and accumulated performance
  regressions that became impossible to untangle.
- v2 inverts the approach: understand TCL's scope rules first, write them
  down, then build the minimum that works.
- Heavier analysis (reaching-definitions dataflow) runs only when needed, off
  the goto-def hot path.
- Research lives in `research/`; designs and plans in `docs/`; deferred work
  in [`docs/BACKLOG.md`](docs/BACKLOG.md).

## Recovering the old prototype

The full v1 history is on the `v1` branch (its tip) and the `archive-v1` tag
(an earlier checkpoint):

```bash
git checkout v1 -- <path>     # pull a v1 file back as reference
git checkout v1               # the full old tree
git checkout archive-v1       # ...or an earlier checkpoint (tag)
```
