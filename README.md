# tcl-lsp

A focused Language Server for **TCL** and **RVT** (Apache Rivet templates), for
Neovim and Vim. One self-contained Go binary; the Neovim and Vim clients drive it.

Scope is deliberately tight — a few features done well (scope-correct, cross-file,
`.rvt`-aware, Itcl-aware) rather than a broad, shallow set.

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

Not supported, out of scope by design (see [Why a v2 reset](#why-a-v2-reset)):
hover, completion, signature help, rename, formatting, diagnostics, code actions,
inlay hints.

The supported set stays within one rule: it only reports what it can derive with
certainty from structure and the index, and stays silent otherwise — so it never
asserts something wrong. (Semantic tokens colors definitions, `$`-variables, and
calls that resolve to your procs/methods; builtins fall back to syntax
highlighting rather than being mis-colored.)

What the supported features bring that a regex/ctags tool can't:

- **Cross-file & `.rvt`-aware** — resolution flows `.rvt` ⇄ `.tcl`.
- **Reaching-definitions** — `$x` jumps to the assignment(s) that actually reach
  it (through loops, conditionals, `break`/`return`), not just the first binding.
- **Scope-correct** — namespaces, `namespace path`/`import`, `global`/`upvar`/
  `variable` link-chasing, and arrays.
- **Itcl OO** — classes, methods, ivars, inheritance, and the `$obj method`
  receiver call. See [Itcl OO support](#itcl-oo-support).

## Install (Neovim ≥ 0.11)

No toolchain required. On first load the plugin downloads the prebuilt server
binary for your OS/arch from a GitHub Release (needs `curl` or `wget`), verifies
its SHA-256, and caches it under `stdpath("cache")/tcl-lsp/` — every launch after
is instant. `go` + `make` are only a fallback (unsupported platform, offline, or
local development).

```lua
-- lazy.nvim
{
  "unknownbreaker/tcl-lsp",
  ft = { "tcl", "rvt" },
  init = function()
    vim.filetype.add({ extension = { tcl = "tcl", rvt = "rvt" } }) -- map .rvt before load
  end,
  opts = {}, -- calls require("tcl-lsp").setup(opts) — see Configuration below
}
```

Supported prebuilt targets: macOS (arm64/amd64) and Linux (amd64/arm64). Other
platforms fall back to a source build. Maintainers: see
[`docs/RELEASING.md`](docs/RELEASING.md) for cutting a release.

A fully-commented spec (and a dev/local-clone variant) lives at
[`editors/nvim/tcl-lsp.lua`](editors/nvim/tcl-lsp.lua). For **packer**,
**vim-plug**, and **Vim** (vim-lsp / coc.nvim), see
[`editors/README.md`](editors/README.md).

## Configuration

Pass options to `setup()` — through lazy.nvim's `opts`, or by calling
`require("tcl-lsp").setup({ … })` directly. Everything is optional; defaults shown.

```lua
require("tcl-lsp").setup({
  filetypes    = { "tcl", "rvt" },           -- buffers the server attaches to
  root_markers = { ".git", "pkgIndex.tcl" }, -- project root (order = priority; .git first)
  cmd          = nil,                        -- override the server binary; nil = bundled
  auto_build   = true,                       -- build the bundled server when missing/stale

  -- Keymaps, set buffer-local on attach (only in tcl/rvt buffers; they never
  -- clobber your other maps). Default: none. The two forms mix freely.
  keymaps = {
    -- a named action -> the key that triggers it (the plugin owns the function)
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

  folding = false, -- true: enable LSP code folding for tcl/rvt (see Usage)

  -- External TCL sources (company package checkouts, tcllib dirs) indexed
  -- read-only at startup, so goto-def/references/tokens reach into them.
  -- Static: nothing executed, nothing written, missing paths skipped.
  extra_index_paths = {}, -- e.g. { "/opt/fa/tcl-lib", vim.fn.expand("~/Repos/fa-tcl") }
})
```

Each `keymaps` entry carries a `desc`, so which-key (if installed) lists it
automatically — no which-key config needed. Leave `keymaps` unset and your
editor's existing LSP maps (LazyVim's `gd`/`grr`/`<leader>ss`…) keep working.

## Usage

Open a `.tcl`/`.rvt` file; the server attaches and indexes the project on first
connect. Use the keymaps you configured above, or your editor's defaults:

- **Go-to-definition / references** — `gd` / `grr` (LazyVim defaults). Vim:
  `:LspDefinition` / `:LspReferences`.
- **Symbols** — a document outline (procs, namespace vars, Itcl classes + members;
  `.rvt` page symbols hoisted to the top) and a project-wide name search. LazyVim:
  `<leader>ss` / `<leader>sS`. An outline panel (`aerial.nvim`) or breadcrumbs
  (`nvim-navic`) pick it up automatically.
- **Call hierarchy** — incoming/outgoing calls for procs and methods, across files
  and `.rvt`. The built-ins fill the quickfix list; `lspsaga.nvim`
  (`:Lspsaga incoming_calls`) gives a drill-down tree. Traces bare and qualified
  calls; explicit `$obj method` edges aren't traced yet.
- **Code folding** — fold ranges for proc/method/namespace/class and control-flow
  bodies, plus the TCL inside `.rvt` `<? ?>` blocks (folds where tree-sitter
  struggles with the mixed HTML/TCL). Curated: it folds script bodies only, not
  arg lists, expressions, or data braces. Not on by default — set `folding = true`
  in `setup()` and the plugin wires `foldmethod`/`foldexpr` window-local for
  tcl/rvt buffers (new splits included), leaving every other filetype's folding
  alone. You keep control of `foldlevel`/`foldcolumn`/etc. Prefer to wire it
  yourself? Point Neovim 0.11+'s LSP foldexpr at our server in your own
  `LspAttach`: `vim.wo.foldexpr = "v:lua.vim.lsp.foldexpr()"`.

**Indexing feedback.** On first connect the server indexes the whole project (a few
seconds on a big repo), reported via LSP work-done progress: an `Indexing TCL
workspace` spinner with a live file count, then `Indexed N files`. Statuslines that
render LSP progress (`fidget.nvim` / `noice.nvim`, both default in LazyVim; coc's
`coc#status()`) show it; others just see a brief startup pause while goto-def and
references wait on the index.

## External packages

The index only sees your workspace by default. To make goto-def, references, and
semantic tokens reach into `package require`d libraries, point the server at
their sources — one line of editor config, shared via your dotfiles, never a
file in any project repo:

```lua
require("tcl-lsp").setup({
  extra_index_paths = { "/opt/fa/tcl-lib", vim.fn.expand("~/Repos/fa-tcl") },
})
```

(Vim: `let g:tcl_lsp_extra_index_paths = ['/opt/fa/tcl-lib']`.) The paths are
indexed **statically and read-only** at startup: nothing is executed, nothing is
written, missing paths are skipped — safe to share one config across machines.
This covers script packages, the common case, with real jump targets.

**Power tool: environment extraction.** Two things have no source text to index:
procs *generated at runtime* by package init code, and C-extension commands. For
those, `tools/extract.tcl` introspects a **live tclsh** — run it deliberately
(it executes package init code; never automatic), with the same tclsh your code
runs under:

```sh
tclsh tools/extract.tcl -global fa_utils json sqlite3
```

`-global` writes an artifact to `~/.config/tcl-lsp/environment.env` (respects
`$XDG_CONFIG_HOME`) that the server reads for every workspace: sourced package
files get indexed, and sourceless commands are declared by name (semantic tokens
color their calls; goto-def stays silent rather than jumping somewhere wrong).
Re-run it when packages change; a per-workspace `.tcl-lsp.env` (stdout mode)
overrides the global file for teams that deliberately commit one.

Dynamic *call sites* (`$cmd`, `eval`) remain out of reach under any mechanism —
that boundary is fundamental, not a missing feature.

## Itcl OO support

The dominant Rivet/speedtables idiom — `itcl::class`, `[::C #auto]`, `$obj method`
— resolves end-to-end:

- **Classes**, **methods/ivars** (inline and external `itcl::body`, with or without
  `public`/`protected`/`private` modifiers — the usual real-world form), and
  **`inherit`** chains all resolve and appear in the symbol outlines.
- **`$obj method`** is receiver-typed: `set d [::STDisplay #auto]; $d field` types
  `$d` and resolves `field` on its class (including inherited), as a statement or
  bracketed (`[$obj method]`).

Graceful boundaries (returns nothing rather than jump wrong): **TclOO**
(`oo::class`) is not supported — Itcl only; receivers with no locally-known class
(a parameter, a factory return, a cross-method ivar) stay unresolved; simple
`inherit`-order MRO, no C3 / dynamic dispatch / mixins; `public { … }` protection
*blocks* aren't parsed (declare members individually).

Pinned against verbatim real Itcl/Rivet code (`flightaware/speedtables`,
`mxmanghi/rivetweb`, `apache/tcl-rivet`); see
[`research/07-realworld-itcl-survey.md`](research/07-realworld-itcl-survey.md).

## Why a v2 reset

v1 (313 commits) tried to do too much and accumulated performance regressions that
were impossible to untangle. v2 inverts the approach: understand TCL's scope rules
first, write them down, then build the minimum that works — which is why heavier
analysis (the reaching-definitions dataflow) runs only when needed, off the
goto-def hot path. Research lives in `research/`, designs and plans in `docs/`, and
deferred work (e.g. TclOO) in [`docs/BACKLOG.md`](docs/BACKLOG.md).

## Recovering the old prototype

The full v1 history is on the `v1` branch (its tip) and the `archive-v1` tag (an
earlier checkpoint):

```bash
git checkout v1 -- <path>     # pull a v1 file back as reference
git checkout v1               # the full old tree
git checkout archive-v1       # ...or an earlier checkpoint (tag)
```
