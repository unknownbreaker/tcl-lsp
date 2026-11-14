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

Known limitations of the reaching-definitions feature (proc-local goto-def).

1. **Array element uses bypass reaching.** A variable `Reference` carries an
   `Element` flag (`$a(key)`, `${a(key)}`, `info exists a(key)`), and the resolver
   sends element sites to the first-binding fallback because reaching tracks only
   the base name and cannot tell `a(x)` from `a(y)`. So an element use answers with
   the earliest binding at or before it, and a use whose only binding is below it,
   even one a loop carries around, stays silent. A fix would track subscripts in
   reaching, at least literal ones. `${a(key)}` is flagged but always silent: its
   reference name keeps the subscript, so no binding of `a` matches it.

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

## Corpus-fix residuals (v0.6.3)

**Status:** deferred. **Area:** `server/internal/tcl/`, `server/internal/lsp/`.

The larger known limits, including the two parser limits, are in
`research/08-flightaware-corpus-probe.md` ("Known limits that remain"). These are the smaller
ones, each rare. "Same on `main@26f8ab2`" means the fixes did not cause it.

- **Switch, still a wrong answer.** Abbreviated or quoted options read a pattern as a call:
  `switch -reg $x {stop {...}}`, `switch "-glob" $x {stop {...}}`. Tcl's rule is that the last two
  words are never options. `::switch` is unhandled, so a pattern reads as a call and
  `::switch $k proc - method {...}` makes `::-`. Same on `main@26f8ab2`.
- **Switch, now silent.** `switch {*}$opts $x {...}` and `switch -exact {...}` find no arms, and a `;`
  in a bare pattern shifts the pairing. Their arms' calls resolved on `main@26f8ab2`. Also, `#* { helper }`
  at line start in an arm list parses as a comment, so its body is silent, where Tcl treats it as a pattern.
  That one is silent on `main@26f8ab2` too.
- **Reaching, still a wrong answer.** `for {} {1} {set x 1} {puts $x; break}` runs `next` when the
  body cannot complete. `set x 0; eval {set y 1;} {set x 1}; puts $x` answers `set x 0`.
  `puts {set x 1}; puts $x`, with no other binding, answers the text in the `puts`. The last two are
  the same on `main@26f8ab2`. So is `switch $k { a {set x 1; return} default {set x 2} }; puts $x`, which
  answers both lines though Tcl says the first arm cannot reach: a `switch` exit joins every arm.
- **Skipped bindings ignore control flow.** A wrapper's binding answers a use nothing else reaches, wherever
  the wrapper sits: `if {$c} { with {set x 1} } else { puts $x }` answers the sibling, and so does a wrapper in a
  branch that returned. Same on `main@26f8ab2`.
- **Reaching, incomplete.** `set x 0; time {set x 1} 5; puts $x` omits the `time` body, and
  `set a(1) 1; set a(2) 2; array names a` answers only the last `set`. Same on `main@26f8ab2`.
- **Silent, not wrong.** `foo destructor {set x 1}`: tail matching reads an unknown command's word as
  a member keyword. `$a` in a bracketed proc or class body loses its semantic token.
- **Cost.** `workspace/symbol` builds line indexes for every file even on a narrow query.
  `incomingCalls` fetches source twice per site and re-parses once per site. `refFQ` calls
  `lookupSide` only for a length check, allocating per candidate on the workspace scan; unmeasured.
  A same-variable `switch` in a loop stays quadratic in its arms (about 1 s at 2,000, 3 s at 4,000).
- **Cost at pathological depth, new in v0.6.3.** Goto-def on a use inside nested unknown bodies re-collects each
  sub-body per level (1,000 nested wrappers took about 3.4 s), and alternating `foreach` and a wrapper grows about 2.3x per
  level (16 levels about 12.6 s, 20 levels over 20 s, on a loaded machine). It only runs when the cursor is in that
  body, and hand-written code does not nest this deep, but one such request blocks the dispatch loop. A cap on the
  walk's depth would bound it.
