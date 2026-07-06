# CLAUDE.md

Guidance for Claude Code when working in this repository.

## Project Overview

tcl-lsp is a Language Server Protocol implementation for TCL/RVT, integrated
with Neovim and classic Vim. (The repo was historically named `tcl-lsp.nvim`;
it now ships clients for both editors — the Go server is editor-agnostic.)

**This is a deliberate restart (v2).** A previous implementation (313 commits) grew
too broad — many features, accumulating performance regressions that became
intractable. That work is preserved on the `v1` branch (its tip) and at the
`archive-v1` tag (an earlier checkpoint in the same history); the `main` branch
now holds v2. Recover any piece with `git checkout v1 -- <path>`.

## Current Phase: navigation + structure features, parallelized, prebuilt-distributed

The server implements a coherent, deliberately-bounded feature set, all obeying one
rule: **report only what can be derived with certainty from structure and the
workspace index, and stay silent otherwise — never assert something that can be
wrong.** Everything below works for both `.tcl` and `.rvt`, with cross-file
`.rvt` ⇄ `.tcl` resolution.

Shipped:
- **Navigation** — goto-definition, find-references, document + workspace symbols
  (source-ordered), call hierarchy (incoming/outgoing, procs and methods).
- **Structure** — code folding, document highlight, selection range, semantic
  tokens. Structural / index-backed; they degrade to silence, never to a wrong
  answer (e.g. semantic tokens colors only defs, `$`-vars, and calls that resolve
  to user procs/methods — builtins fall back to syntax highlighting).
- **Itcl OO** — classes, methods, ivars, inheritance, `$obj method` receiver typing.
- **Reaching-definitions** for proc-locals (a `$x` jumps to the assignment(s) that
  actually reach it), run only when needed, off the goto-def hot path.

**Performance:** one parse per file feeds all four analyses; the initial workspace
index and the workspace read-scans (find-references, incoming call hierarchy,
workspace symbols) run in parallel across GOMAXPROCS *while the single-goroutine
dispatch loop is blocked on the request*, so the index needs no locking. The one
lazily-mutated cache (`nsCache`) is pre-warmed single-threaded before a parallel
reference scan. Race-detector-tested (`go test -race`).

**Distribution:** consumers download a prebuilt, SHA-256-verified server binary
from a GitHub Release — both the Neovim plugin (`lua/tcl-lsp/download.lua`) and the
classic-Vim client (`autoload/tcl_lsp.vim`), sharing one pinned version
(`lua/tcl-lsp/version.lua`). Source build is a fallback. Cut releases with
`make -C server publish VERSION=vX.Y.Z` (see `docs/RELEASING.md`). The Go server
lives under `server/` (stdio/JSON-RPC).

**Still out of scope, by design — do not propose or scaffold:** hover, completion,
signature help, rename, formatting, diagnostics, code actions, inlay hints. They
need inferred types (which dynamic, interpreted TCL can't give a static server) or
make assertions that can be *wrong* — the exact bet v2 refuses, and the reason v1
grew intractable. The one conceivable future addition is *conservative, opt-in,
off-by-default* undefined-proc diagnostics, which would need a written design
first. Research lives in `research/`; designs/plans in `docs/`.

## Working Agreements

- Research output lives in `research/` as Markdown (create it when starting).
- Plans live in `docs/plans/` once research is mapped.
- Keep changes small and verified; resist re-expanding scope.

## Recovering v1

```bash
git log v1 --oneline              # browse the old history (full v1 tip)
git checkout v1 -- tcl/           # pull a specific path back as reference
git checkout v1                   # the full old tree lives on this branch
git checkout archive-v1           # ...or an earlier v1 checkpoint (tag)
```
