# Backlog

Deferred work that is understood but intentionally not done yet. Each item notes
why it's deferred and the rough shape of the fix, so picking it up later is cheap.

## coc.nvim auto-build / rebuild-on-update

**Status:** deferred. **Editor:** Vim/Neovim via coc.nvim.

vim-lsp users get auto-build + rebuild-when-stale (the bundled
`editors/vim/tcl-lsp.vim` runs `autoload/tcl_lsp.vim#ensure_built` before
registering). coc users don't, because coc's integration is **declarative JSON**
(`coc-settings.json`) — there's no seam where our code runs before coc launches
the server, so we can't call the build step. coc users currently build by hand
(`make -C server build`) and rebuild after pulling.

Two ways to add it when wanted:
1. **Wrapper command (lightweight).** Ship `editors/vim/tcl-lsp-coc.sh` that runs
   `make -C <repo>/server build` (a no-op when fresh) then `exec`s the binary, and
   point coc's `command` at it. Caveats: no Windows `sh`; runs `make` on every
   server start (cheap when up to date); the wrapper must locate the repo.
2. **coc extension (`coc-tcl-lsp`).** A Node/TS package with an `activate()` hook
   that builds, then registers the language client. Full parity, but a whole
   package to publish and maintain.

Lean toward (1) if coc demand appears; (2) only if a coc extension is wanted for
other reasons.

## vim-lsp live on-disk re-indexing (external changes)

**Status:** deferred / likely won't-fix. **Editor:** Vim via vim-lsp.

The server registers for `workspace/didChangeWatchedFiles`, but vim-lsp implements
neither `client/registerCapability` nor file watching (confirmed: both absent from
its source), so files changed on disk **while not open** aren't re-indexed until
opened. Open-buffer edits/saves/opens DO re-index live (via `didChange`/`didOpen`),
so only external changes to unopened files are missed. coc handles this natively.

A fix would require Vimscript-side mtime polling (classic Vim has no native fs
watch) plus a way to push notifications through vim-lsp — hacky and low value.
Recommend leaving it; document the limitation (done, see `editors/README.md`).

## Reaching-defs follow-ups (from the final whole-branch review)

**Status:** deferred. **Area:** `server/internal/tcl/reaching.go`, `varref.go`, `resolve.go`.

The reaching-definitions feature (proc-local goto-def) shipped with two known,
non-blocking limitations, both noted in code comments:

1. **Array-ness detection is duplicated/low-layer.** `resolve.go` has its own
   `isArrayVarAt`/`isNameByte` byte-scan to detect `$arr(key)` uses (so they
   bypass reaching, which doesn't track subscripts). This re-derives info the
   lexer discarded: `parseVarRef` (`varref.go`) drops the `(subscript)` and emits
   only the base name. Cleaner: add a `hasSubscript`/array flag to the use-side
   `Reference` so the resolver reads it instead of re-scanning, removing the
   duplicated `isNameByte`. This is layering debt: an element use answers with the
   earliest binding at or before it, so a use whose only binding is below it, even
   one a loop carries around, stays silent.
2. **`try ... on e {varList} {body}` handler var-list names** aren't emitted as
   local bindings by `localBindings` or `emitDefs`, so `$e` there goes silent unless
   an earlier binding of the name exists, which is then the answer.

The OO (`$obj method`) type-tracking this engine was built to enable has since
**shipped** — Itcl classes, methods, ivars, inheritance, and the `$obj method`
receiver call all resolve (see the README's "Itcl OO support"). **TclOO
(`oo::class`)** remains the deferred OO dialect.

## Corpus-probe misses (relative qualified commands, `$var` in expressions, ...)

**Status:** deferred, ranked. **Area:** resolver, reference walker, index.

The FlightAware corpus probe found ten kinds of missing answers, ranked in
`research/08-flightaware-corpus-probe.md` ("Misses"). None are in the current fix
plan (`docs/plans/2026-11-05-corpus-wrong-answers-and-position-perf.md`), which
covers wrong answers and the position slowdown only. Rows there are harness counts
and upper bounds; confirm a root cause by reproducing it before picking one up.
