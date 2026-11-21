# CLAUDE.md

tcl-lsp: a Language Server for TCL/RVT. The Go server (`server/`, stdio JSON-RPC)
is editor-agnostic. It ships with a Neovim client (`lua/tcl-lsp/`) and a classic-Vim
client (`autoload/tcl_lsp.vim`). This repo is public, so tracked files, issues and
PRs must never name internal FlightAware code.

`main` holds v2, a deliberate restart. v1 grew too broad and its performance
regressions became intractable. It lives on the `v1` branch and the `archive-v1`
tag; pull back any path with `git checkout v1 -- <path>`.

## The rule: certain or silent

Every feature reports only what can be derived with certainty from structure and
the workspace index, and stays **silent** otherwise. A missing answer is fine; a
wrong answer is a bug. Everything works for `.tcl` and `.rvt`, including
cross-file `.rvt` ⇄ `.tcl` resolution.

Shipped:
- **Navigation:** goto-definition, find-references, document and workspace symbols
  (source-ordered), call hierarchy (procs and methods).
- **Structure:** folding, document highlight, selection range, semantic tokens.
- **Itcl OO:** classes, methods, ivars, inheritance, `$obj method` receiver typing.
- **Reaching-definitions** for proc-locals: `$x` jumps to the assignments that
  reach it. It runs only when needed, off the goto-def hot path.
- **External packages:** `extra_index_paths` (initializationOptions.extraIndexPaths)
  statically indexes external source dirs (the `$tcl_library`, package checkouts).
  It is read-only, executes nothing, and skips missing paths. This is the only
  way the server reaches outside the workspace, and adding another needs a written
  design first. Color and jumps mean "there is source": C builtins,
  runtime-generated procs and dynamic call sites (`$cmd`, `eval`) stay out of reach.
- **Certain-only diagnostics:** built-in Checks (`server/internal/check`, design in
  #93, user doc `docs/CHECKS.md`) flag code that is valid but cannot mean
  anything sensible. A Check ships only if it can never fire on correct code, sees
  only commands certain to run as Tcl (`tcl.CommandVisitor`), and stays silent
  when a builtin it assumes is redefined in the workspace. The editor gets them as
  publishDiagnostics; CI runs `tcl-lsp check <paths>`. No custom rules.

The feature set is closed beyond the Checks #93 lists. Hover, completion,
signature help, rename, formatting, code actions and inlay hints all need
inferred types or make assertions that can be wrong, and that is the bet v2
refuses. Any other diagnostic, such as opt-in undefined-proc diagnostics, needs a
written design first.

## Verify

- Server: `make test-go`, plus `go -C server test -race ./...` and `go -C server vet ./...`.
- Lua client: `make test-lua` (plenary, headless).
- Tcl semantics: run `tclsh` 8.6 and 9.0. Tcl is the oracle, not a test, a
  prototype or a research note.
- Real code: the corpus gate in `research/experiments/08_corpus_probe/` (see its README).

## Releases

A server change ships only with a release, so the prebuilt binary that clients
download always matches merged code. In the same change, bump
`lua/tcl-lsp/version.lua` (patch for fixes, perf and internals; minor for
user-visible features). After the merge, run
`make -C server publish VERSION=vX.Y.Z` from `main`. Changes that touch only
docs, clients or tests skip the release. Steps are in `docs/RELEASING.md`.

## Where things live

- `research/`: findings as Markdown, numbered. Read the highest-numbered relevant
  one before changing resolution behavior.
- `.claude/rules/`: path-scoped conventions (server concurrency invariants, Lua, Tcl).
- `docs/plans/`: implementation plans, written once the research is mapped.
- `docs/BACKLOG.md`: deferred work and known wrong answers.
- Issue tracker: GitHub Issues on `unknownbreaker/tcl-lsp` via `gh`. See
  `docs/agents/issue-tracker.md`, and `docs/agents/triage-labels.md` for labels.
- Domain docs: root `GLOSSARY.md` and `docs/adr/`, created lazily. See
  `docs/agents/domain.md`.

Keep changes small and verified, and keep scope where it is.
