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
and upper bounds; confirm a root cause by reproducing it before picking one up. Items 1, 2,
3 and 4 are fixed; the notes there say which version.

## Corpus-fix residuals (v0.6.3)

**Status:** deferred. **Area:** `server/internal/tcl/`, `server/internal/lsp/`.

The larger known limits, including the two parser limits, are in
`research/08-flightaware-corpus-probe.md` ("Known limits that remain"). These are the smaller
ones, each rare. "Same on `main@26f8ab2`" means the fixes did not cause it.

- **Switch, now silent.** Since v0.6.15 options follow Tcl's parser (abbreviations, quoted or braced
  options, never the last two words, `::switch`), and a switch whose options Tcl rejects, or whose
  subject a substituted word may displace, walks no arm. (A complete switch Tcl rejects for its
  arms, such as an odd inline count after `--`, still walks them, so a half-typed one keeps working.) So `switch {*}$opts $x {...}`, `switch $o $x {...}`,
  `switch -regexp $x a {...} b {...}` (`$x` may be `-matchvar`) and a single inline arm without `--`
  whose body is a two-word list (`switch $k "a" { puts $y }`) are silent, and so is a half-typed
  inline switch without `--` that reads as complete if its subject is an option. None of these
  occur in the corpus. Other `::`-qualified builtins (`::if`, `::foreach`) are still read as
  unknown commands.
- **Switch, a substituted last body.** A last body of `-` makes Tcl reject the switch, so its arms
  are silent, but only a literal `-` is seen: in `switch $x a { helper } b $body` the arms are
  walked though `$body` may be `-` at run time. (Arm lists are split as Tcl lists since v0.6.16.)
- **Switch, an odd arm count with an unbraced last word.** Tcl rejects it (`extra switch pattern
  with no body`), yet the complete pairs before it are walked, on purpose: it is a pattern still
  being typed, as in `switch $x {a { helper } b}`.
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
- **Itcl class names under the global fallback.** Since v0.6.21 a relative class name in
  `itcl::body` or `inherit` outside the global namespace names the local class when the workspace
  declares it, else the global one (`::G` for `itcl::body G::run` in `::other` or a template), as
  Itcl does. Tcl decides when the command runs, so a local class defined only later still wins here.
  An unqualified `proc` or `itcl::class` in such a body is created in a class namespace the parser
  cannot name, so it defines nothing, and a call that may reach it (`inner` in the body,
  `::G::inner`) answers nothing. One in a class whose namespace is unknown (an `itcl::class` a
  wrapper defines in a method) inherits no base.
- **Itcl method-body namespace neighbours.** Since v0.6.17 calls in a method body look up from the
  class namespace, then global, and since v0.6.18 class-body code (`common c [g]`) looks up from
  `::itcl::parser`, then global, and an unqualified `proc inner` in a method is indexed as
  `::ns::K::inner`, and since v0.6.19 so is an unqualified `itcl::class Inner` (`::ns::K::Inner`;
  one a wrapper defines there is not indexed). Some neighbours are still wrong (tclsh 8.6 and 9.0,
  Itcl 4.3). A substituted
  `namespace path` (`namespace path [list {*}[namespace path] ::lib]`, the form an Itcl class
  namespace needs, as a plain one replaces Itcl's own path) is recorded as one literal entry, so the
  class's path is silent. A wrapper's body in a method answers only when the class-namespace and
  global lookups agree, but `uplevel 1` runs in the caller's namespace, which neither may be.
  A relative `namespace eval sub {g}` or `proc sub::p` inside a method is qualified against the
  lexical namespace (`::ns::sub`), though Tcl puts it in `::ns::K::sub` when that exists and in the
  object's own namespace (`::oo::ObjN::sub`) otherwise, so its calls answer `::ns::sub::g`. In a
  wrapper's body there (`after`, `uplevel 1`) a relative `proc sub::d` is still indexed as `::ns::sub::d`,
  though Tcl creates it under the namespace the wrapper runs it in. A
  `variable v` in a method links `::ns::K::v` in Tcl, yet resolves against `::ns`. Same on v0.6.16.
  A class a wrapper defines in a method is not indexed under any name, so in its methods only
  absolute calls, `$this` calls, and its own ivars answer; bare calls are silent. Since v0.6.19.
- **Itcl method calls Tcl rejects.** A bare call to a base class's `private` method, to an instance
  method from a class `proc`, or from a nested `proc` inside a method (unqualified, or named
  into the class namespace: `proc ::K::r`, `namespace eval ::K`) errors in Tcl, yet resolves to
  the method and is listed as its reference. Same on v0.6.8 for goto-definition.
- **`dict with` keys bind nothing.** Since v0.6.14 `dict with` and `dict update` bodies run in place
  (before, they were not walked at all). `dict with` writes a variable per key of the dict, and the
  keys are runtime data, so `set x 0; dict with d { puts $x }` answers `set x 0`, as does a `$x` after
  the body, though a key `x` overwrites it. This is the same as other writes to names the analyzer
  cannot see (`set $n`). `dict update`'s variables bind when they are literal names (`v`, `{v}`,
  `"v"`); a substituted one (`$n`) or any `{*}` form binds nothing, and at namespace level they are
  not definitions, like `foreach` variables there.
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
  Itcl `variable`/`common` and `proc` names keep their raw text, so `proc a\x62` is not a target
  for `ab`. Variable names decode their escapes (`set a\x62` binds `ab`), and so do proc params.
- **Cost.** `workspace/symbol` builds line indexes for every file even on a narrow query.
  `incomingCalls` fetches source twice per site and re-parses once per site. `refFQ` calls
  `lookupSide` only for a length check, allocating per candidate on the workspace scan; unmeasured.
  A same-variable `switch` in a loop stays quadratic in its arms (about 1 s at 2,000, 3 s at 4,000).
- **Cost at pathological depth, new in v0.6.3.** Goto-def on a use inside nested unknown bodies re-collects each
  sub-body per level (1,000 nested wrappers took about 3.4 s), and alternating `foreach` and a wrapper grows about 2.3x per
  level (16 levels about 12.6 s, 20 levels over 20 s, on a loaded machine). It only runs when the cursor is in that
  body, and hand-written code does not nest this deep, but one such request blocks the dispatch loop. A cap on the
  walk's depth would bound it.
