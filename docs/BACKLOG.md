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

## Corpus-probe misses (relative qualified commands, callbacks, ...)

**Status:** deferred, ranked. **Area:** resolver, reference walker, index.

The FlightAware corpus probe found ten kinds of missing answers, ranked in
`research/08-flightaware-corpus-probe.md` ("Misses"). None are in the current fix
plan (`docs/plans/2026-11-05-corpus-wrong-answers-and-position-perf.md`), which
covers wrong answers and the position slowdown only. Rows there are harness counts
and upper bounds; confirm a root cause by reproducing it before picking one up. Items 1, 2, 3 and 4 are fixed;
the notes there say which version.

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
- **Reaching, still a wrong answer.** `set x 0; eval {set y 1;} {set x 1}; puts $x` answers `set x 0`.
  `puts {set x 1}; puts $x`, with no other binding, answers the text in the `puts`. Both are
  the same on `main@26f8ab2`. (A `for` whose body cannot complete no longer runs `next`, since v0.6.7.) So is `switch $k { a {set x 1; return} default {set x 2} }; puts $x`, which
  answers both lines though Tcl says the first arm cannot reach: a `switch` exit joins every arm.
- **Reaching, after a conditional bracket.** A bracket after `&&`, `||` or `?` joins its bindings
  with the set before it, which is right after the whole expression but over-includes inside it.
  `$c || [set x 1] && [puts $x]` and `if {$c && [set x 1]} { puts $x }` both answer `set x 0`
  too, though Tcl reaches the use only through `set x 1`. A bare `$x` after such a bracket in the
  same expression is silent (v0.6.7); a use inside a later bracket or the `if` body is not.
  The scan also over-reads what is conditional: a `?` that a paren already closed
  (`expr {($c ? 1 : 2) + [set x 1]}`) and an operator or `"` inside an array index (`$a(?)`) both
  make a later bracket conditional, so `return $x` also answers `set x 0`. Same on v0.6.6.
- **Itcl method bodies resolve calls in the lexical namespace.** A method runs in its class
  namespace, so from `itcl::class K` inside `namespace eval ::ns`, Tcl calls `util::f` as
  `::ns::K::util::f`, then `::util::f`, and a bare `g` defined in `::ns` is `invalid command name`
  (tclsh 8.6, Itcl 4.3.7). The server tries `::ns` first, so it answers `::ns::util::f` and
  `::ns::g`. Before v0.6.8 the qualified call was answered the same way. v0.6.8 adds the global
  fallback, but the class-namespace candidate is still never tried.
- **Itcl class names miss Tcl's global fallback.** `itcl::body G::run` inside `namespace eval other`,
  or in a template (`::request`), names class `::other::G`, and `inherit Base` in a class inside
  `namespace eval ns` names `::ns::Base`, where Tcl finds the global `::G` or `::Base` (tclsh 8.6 and
  9.0, Itcl 4.3.7). Calls in those methods then miss the class's methods and answer a same-named proc.
  Same on v0.6.8.
- **Itcl method calls Tcl rejects.** A bare call to a base class's `private` method, to an instance
  method from a class `proc`, or from a nested `proc` inside a method errors in Tcl, yet resolves to
  the method and is listed as its reference. Same on v0.6.8 for goto-definition.
- **`dict with`/`dict update` bodies in a method are silent for method-named calls.** Since v0.6.9 a
  call in a custom command's body inside a method answers nothing when the class has a method of
  that name. `dict with` and `dict update` run their body in place but are not in `bodiesRunHere`, so
  a bare `m` there answers nothing for goto-definition and find-references. Adding them would fix it.
- **regexp in a condition joins earlier bindings.** regexp's match variables are possible writes,
  so `set m {}; if {[regexp {(a)} $s -> m]} { puts $m }` answers both lines, though inside the
  body only the regexp reaches. The analysis is not path-sensitive, as with any `if`. Since v0.6.10.
- **Quoted or braced variable names bind nothing.** `set {v} 1`, `catch {foo} "v"`, `gets $f {v}` and
  `regexp {a} $s "v"` all write `v`, but only a bare name binds, so a later `$v` answers just the
  earlier bindings. Likewise a regexp whose substituted leading word is followed by a substituted
  operand (`regexp $opt {a} $s v`, `regsub $opt {a} $s {b} v`), a dynamic variable name
  (`regexp {(a)} $s v $n`), the same variable named twice (`regexp {(a)(b)} ab v v`), regsub's
  9.0-only `-command`, and any `{*}` form bind nothing, though Tcl may write `v`. Since v0.6.10.
  Since v0.6.11 the same holds for `scan`, including distinct elements of one array
  (`scan $s {%d %d} a(1) a(2)`).
- **Backslash-escaped variable names bind their raw text.** `set a\x62 new` and `scan $s %d a\x62`
  write `ab` (tclsh 8.6 and 9.0), but the binding is named `a\x62`, so a later `$ab` answers only
  the earlier `set ab old`, which `set` overwrote. This is a wrong answer, but only for an unusual spelling.
- **Goto-def repeats a result-var location after `global`.** With `global a` and then
  `scan $s %d a` (also `regexp`, `binary scan`) in a proc, `$a` lists the scan's location twice.
  The set of locations is right; only the dedupe is missing.
- **`::request` declarations from a `.tcl` file.** Since v0.6.8 a template's `::request` takes only
  that page's `namespace path` and imports. A `.tcl` file that writes them into `::request`
  (outside the model, research/05 V2) is ignored from pages, so if a page sources it, a call can
  fall through to a global target. A `.tcl` caller in `::request` still merges every page's.
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
