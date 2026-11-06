# FlightAware corpus probe: wrong answers, a slowdown, and misses

**Date:** 2026-11-05

## Why this exists

The project bet is that the server reports only what is certain and stays silent
otherwise. Every suite so far tests shapes someone thought to write down. Research 07
broke that loop for Itcl idioms by vendoring excerpts; this probe goes further and
asks the running server real questions across whole repos, then checks the answers
against something other than the server. It found a server that is robust, wrong in
four specific ways, and slow on one generated file.

## How to read the evidence

Every finding carries one of these tags. They are not interchangeable.

- **Reproduced:** run directly against the server (or `tclsh`), and the output was read.
- **Harness count:** produced by the harness's classifier. **Every row count below is an
  upper bound.** A row is a candidate from a lenient oracle, not a confirmed bug.
- **Code reading:** read in the source and not run.

## Method

The user chose "active public Tcl repos". No internal FlightAware code was read, so
nothing here describes it.

- Server: `main@26f8ab2`.
- Client: a stdio LSP client, one server process per repo.
- Oracle: an independent Tcl word parser. It never reads the server's output to decide
  what a correct answer looks like.
- Rerun: the harness is vendored in `experiments/08_corpus_probe/` (its README has the
  caps, the oracle's limits, and the spot-check log). **The baseline is reproducible:** a
  full rerun matched the first run exactly, on every count and every classified row.

## Corpus

410 files, 127,510 lines. yajl-tcl is 30,343 of those, nearly all one data file
(`playpen/airports.tcl`). Full SHAs are in `experiments/08_corpus_probe/fetch_corpus.sh`.

| Repo | Commit | Files | Lines |
| --- | --- | ---: | ---: |
| `flightaware/piaware` | `8480286` | 37 | 9,288 |
| `flightaware/piaware-gui` | `a12dbc1` | 10 | 3,526 |
| `flightaware/prometheus-tcl` | `4ecc609` | 24 | 3,568 |
| `flightaware/speedtables` | `0fe25e1` | 150 | 22,774 |
| `flightaware/tcl-jira-api` | `2dd4771` | 1 | 869 |
| `flightaware/TclProDebug` | `fb1dc77` | 70 | 37,033 |
| `flightaware/tclrmq` | `15318be` | 27 | 3,910 |
| `flightaware/tsp` | `dad8752` | 63 | 13,444 |
| `flightaware/turtles` | `ca942b3` | 19 | 2,755 |
| `flightaware/yajl-tcl` | `fa5da41` | 9 | 30,343 |

## What held up

- 34,870 requests, zero error responses (**harness count**).
- Zero panic/recovered/error lines in all 10 stderr logs (**reproduced** by grep).
- Worst index time 24 ms (**harness count**).
- The only timeouts were 4, all in yajl-tcl (see Slowdown). The restarts were initiated by
  the harness after those timeouts; they were not crashes.

## Wrong answers

### W1. Bodies of unknown commands are walked at namespace level

**Cause.** `bodies.go:146-150` passes the enclosing `frame` to the default branch, and
`scriptBodies`' default (`bodies.go:240-248`) treats any trailing brace as a script. The chain:
`oo::class create X {…}` → `method m {} {…}` (the frame is not FrameClass, so default again) →
`set x` → `emitVarAssign` (`defs.go:237`) → `DefNamespaceVar ::ns::x`. Uses then resolve through
`variableCandidates` (`resolve.go:370`).

**Reproduced:**
- `prometheus-tcl/gauge.tcl:79` `$_value` → `counter.tcl:14,21,35` and `gauge.tcl:14`: another
  class's variable. `counter.tcl:42` returns the same list: three right, plus `gauge.tcl:14`,
  which is a `variable` declaration, not a `set`.
- `tsp/bench/langbench/cat.tcl:5` `$file` (a `tsp::proc` param) →
  `bench/langbench/handcoded/cat-ext.tcl:205` only.
- Synthetic `tsp::proc`: `$file` jumps to a global `set file`, and local `f` appears as a fake
  outline symbol.
- `tclrmq/package/Channel.tcl:93` `$active` → six lines in the same class, all defensible, since
  the class declares `variable active`.
- Bracket variant: `speedtables/ctable_server/tests/test_trigger.tcl:19` `$tmp` →
  `ctables/tests/tsv-tests.tcl:152…`. The proc sits inside `c_test eval {…}` inside
  `if [catch {…}]`. `substRefs` (`refs.go:216-226`) tags everything by the enclosing command, so
  the proc's locals become namespace-frame refs.

**Harness count:**

| | Rows | By repo |
| --- | ---: | --- |
| All `outside-proc-unlinked` | 709 | tclrmq 438, tsp 201, prometheus-tcl 67, speedtables 3 (the bracket variant) |
| Target in another file | 309 | tsp 196, tclrmq 94, prometheus-tcl 16, speedtables 3 |
| Same file, a declared TclOO `variable` among the targets (likely right or partly right) | 180 | |
| Same file, no such declaration | 220 | |

887 outline symbols the oracle cannot find: tsp 470, tclrmq 367, prometheus-tcl 45,
TclProDebug 5. Those 5 have other causes. 3 are the switch `-` arm (W2). 2 are a `lassign` at
`TclProDebug/lib/tcldebugger/instrument.tcl:620`; read while writing this up, that `lassign`
writes through `variable suppress` and `variable result` links declared at the top of the proc,
so the symbols look correct and this looks like an oracle gap (**code reading**).

### W2. Switch arms are parsed as commands

**Cause.** It is the stated design: "switch arms are handled transitively as each parsed arm is
itself such a command" (`bodies.go:240-243`).

**Reproduced:** a `stop { puts hi }` arm jumps to `proc stop`, and a `proc -` fall-through arm
produces an outline symbol named `-`.

**Harness count:** 106 call rows (TclProDebug, piaware-gui, mostly speedtables `gentable.tcl`),
106 colored as calls, 18 in references. Examples: `TclProDebug/lib/tcldebugger/menu.tcl:816`
`stop {` → `demos/tutorials/pulse.tcl:49`; `piaware-gui/grid.tcl:476`.

The same root cause also loses answers. A quoted-pattern arm (`"--" {…}`) is never walked,
because its head is not bare (**reproduced**). A braced pattern is skipped the same way, and only
the last body survives when arms share a line (**code reading**). `reaching.go:238-264` already
carries a workaround for the inline form.

### W3. Commands and variables share one name table

**Cause.** `defsByName` (`index.go:44`) and `Lookup` (`index.go:315`), used from `Definition`
(`resolve.go:265-269`), `refFQ` (`resolve.go:618-629`), `targetFQ` (`resolve.go:579`) and
`CommandRefKind` (`resolve.go:533`). Tcl keeps these in separate namespaces (`research/02`), so
merging them is never right.

**Reproduced:** `::rmq::debug "hello"` alongside `set ::rmq::debug 1` returns both the proc and
the variable. A `global test` link alongside `proc test` returns both as well (run while writing
this up, through the resolver on `main@26f8ab2`).

**Harness count:** 56 rows by target form (qualified-set-in-proc 45, ns-set 8, proc-variable-link
2, ns-variable 1). 6 reference rows where the refs of proc `test` include `$test`
(`speedtables/ctables/tests/stapireturn.tcl:35`).

### W4. Goto-def can land on a binding that cannot reach the use

**(a) The earliest-binding fallback** (`resolve.go:107-127`) has no position check.

**Reproduced:**
- Synthetic `regexp … word; puts $word; set word 2` jumps to the later `set`.
- `piaware/package/fa_adept_client.tcl:83` `$nextHostIndex` → `:84`, while the class declares
  `protected variable nextHostIndex 0` at `:51`. The right answer is `:51`, and returning nil
  already reaches `ivarInClass` (`resolve.go:205`), so guarding yields the correct target here,
  not just silence.
- `piaware-gui/fa_tailer.tcl:53` → `:57`.

**(b) The conservative join** (`reaching.go:250-254`): uses inside try/catch/switch bodies see
bindings from anywhere in those bodies.

**Reproduced:**
- `$x` in `try` before `set x` → `[1, 4]`.
- `$y` in `catch` before `set y` → `[8, 11]`.
- Control: `$z` after a `catch` → `[15, 16]`, which is correct and must stay.
- Real: `piaware-gui/radio.tcl:10` `$value` → `5, 30` (line 30 is a later `foreach` in the same
  `try`).

**Harness count:** 83 rows, excluding uses in loops. 74 have all targets after the use (speedtables
47, TclProDebug 17, piaware 5, piaware-gui 3, tsp 2). 9 are mixed: tclrmq 7 are really W1,
piaware-gui 1, and speedtables 1 (`stapi/display/display.tcl:2331`, not read, may be a false
positive). `docs/BACKLOG.md` currently calls this fallback "graceful".

**Related, found while checking the fix (reproduced through the resolver on `main@26f8ab2`).**
`recordUses` (`reaching.go:411-426`) keeps the set from the *first* visit to a use, so the loop
fixpoint never reaches an answer inside a loop. `set x 0; while {1} { puts $x; set x 1 }` returns
only `set x 0`, not both bindings. With no prior binding the answer is empty, and only the
earliest-binding fallback happens to rescue it. The existing `TestReachingLoopCarried` probes the
use *after* the loop, so it does not see this.

## Slowdown

**Cause.** `LSPPosition` (`position.go:30`) rescans from byte 0 on every call.
`semantictokens.go:59-60` calls it twice per token; `symbols.go:35-36,43-44` up to four times per
symbol.

**Reproduced,** on `yajl-tcl/playpen/airports.tcl` (1.35 MB, about 30k lines): documentSymbol
40.6 s, semanticTokens 20.2 s, foldingRange 11 ms. The 2:1 ratio fits the 4-vs-2 call counts.
**The baseline binary was not profiled,** so the plan confirms this with a benchmark.

**Harness count:**
- Goto-def on `$::airport($ap)` in `playpen/test2.tcl` timed out at 10 s twice. This is a different
  file: about 30k locations through `toLocations` (`server.go:413-415`, **code reading**).
- workspace/symbol took 4.2 s on that repo.
- Next slowest in the corpus: `speedtables/ctables/gentable.tcl` semanticTokens, 374 ms.

**Impact.** The dispatch loop is single-threaded, so every other request waits. Whether the two
requests stack depends on what the client sends. This is one generated data file out of 410.

## Misses (ranked; none are in the fix plan)

1. **Relative qualified commands.** `resolve.go:304-306` returns only the current-relative name.
   **Reproduced** in tclsh 8.6.18 and 9.0.3 (`experiments/08_relative_qualified.tcl`): `prom::helper`
   from `::other` falls back to `::prom::helper`, and the current-relative target wins when both
   exist. **Harness count:** 2,064 rows, of which TclProDebug is 2,001, prometheus-tcl 61, turtles 2.
   Probes were capped, so this is undercounted, and it is concentrated in one repo. `research/02`
   F-C only tested a target that exists (now corrected there).
2. **`$var` in braced expressions.** **Reproduced:** `if {$n <= 1}` and `expr {$n * 2}` → nothing.
   `refs.go:41-42` and `:120-122` say braces suppress these, which is wrong for Tcl: `expr`
   substitutes them itself. **Harness count:** 3,054 rows tagged `in-expr`, of 4,510 variable misses.
3. **Unrecognised bindings.** **Reproduced:** `gets`, `binary scan`, `try … on`, `regexp`.
   **Harness count,** where it is the only binding form: gets 87, regexp 77, binary scan 40, try-on
   23. These overlap with item 2, so do not add them up.
4. **Find-references omits intra-class Itcl calls.** **Reproduced** synthetically. `Definition` has
   `methodInClass` (`resolve.go:260-264`) and `refFQ` does not. **Harness count:** 336 rows (speedtables
   176, piaware 153, piaware-gui 4, tsp 3), sampled at up to 300 defs per repo; "mostly" Itcl, not all
   checked.
5. **Test files.** `.test` is never indexed (`index.go:83`): 44 files, plus 10 shebang scripts.
   **Reproduced:** even when opened, tcltest `-setup`/`-body` and old-style bodies do not resolve. A
   braced `-result {x}` as last word is walked as code (**code reading**).
6. **`dict with` / `dict update` bodies are not walked** (`bodies.go:216-220`, **code reading**, not
   run; **harness count:** 46 rows tagged `dict-with`).
7. **Callbacks.** **Reproduced:** `after idle animate`, `-command animate`, `trace … animate` → nothing.
   `bind . <Key> {animate}` **does** resolve via the trailing-brace rule. **Harness count:** 77 rows.
   These scripts run in the global namespace, so lexical resolution is not safe.
8. **Unmodeled definitions** (**harness count**). TclOO method 176, `oo::class create` 15, `tsp::proc`
   50, `::proc` 5, `interp alias` 3. TclOO is deliberately deferred (`research/07`, `BACKLOG`).
9. **Semantic tokens leave resolved intra-class calls uncolored** (**harness count:** 301 rows,
   speedtables and piaware). `resolve.go:526-527` documents `$obj method` staying uncolored; bare
   intra-class calls go uncolored too.
10. **Relative qualified variables.** **Reproduced:** the server is silent, **8.6 falls back, and 9.0
    errors.** The target is 8.6 (`research/README.md`), so this is a real miss on 8.6, and silence is the
    safe choice while the versions differ.

`$obj method` misses (412 on `$var`, 95 on `my`) mostly mean an untyped receiver. Silence is correct
by design; they are not bugs.

## Not actually bugs

- `$ns` at `TclProDebug/lib/tcldebugger/nub.tcl:1674` (spot-check row 20): `upvar #0 DbgNub(scope) ns`
  at line 1669 is a static alias, so chasing to `::DbgNub` is correct, if noisy at about 160 targets.
  The other two `name-mismatch` rows are lines 1675 and 1676 of the same string, same variable.
- `namespace export` (126) and empty `namespace eval` (26) def misses are expected.
- `DATA_CONTEXT_RESOLVED` rows are mostly `subst` templates that do run later.
- `CANDIDATE_MISS conf=data` (434) is noise.

## Limits of the evidence

- **Row counts are upper bounds,** and some are also capped. Caps are 20 call probes per file+name, 60
  var probes per proc, and 300 defs for refs; capped counts hit TclProDebug, speedtables, tsp, tclrmq.
- **Namespace-level `$x` is not probed at all,** so a fix that loses those answers will not show up in
  harness numbers.
- Only 4 `.rvt` files.
- The refs check only sees call sites goto-def already resolved.
- The harness reported about 0% false positives on wrong-answer rows from a 5-row and a 3-row sample. The
  `$ns` row shows that estimate was too low.
- Rows with mixed targets count as wrong even when most targets are right.
- Public repos only. No internal FlightAware code was read.

## Fix design

The plan is `docs/plans/2026-11-05-corpus-wrong-answers-and-position-perf.md`. In order:

1. **Position index.** A per-request line-start table with a `sort.SearchInts` lookup, then a UTF-16 count
   from the line start. Exact behaviour match with `LSPPosition`. Multi-file paths use a per-request map
   keyed by file, never cached across requests, because live documents change. Workspace symbols convert
   in parallel, so that map is built single-threaded first and only read by the workers.
2. **Switch.** `scriptBodies` gets a `switch` case: parse options and subject; one braced word left is an
   arm list (its parsed words flattened and shifted to `w`'s coordinates), otherwise inline pairs. Only a
   braced body is returned, `-` is fall-through, and patterns are never walked. An odd word count with a
   braced last word returns nil; an unbraced last word is a pattern still being typed and is dropped, so
   the other arms keep resolving mid-edit, as they do on `main@26f8ab2`. The reaching workaround goes away. `FileFolds` keeps the outer arm-list fold through a small helper.
3. **Separate commands from variables.** Index keeps one table. Resolver lookups filter by side: commands
   accept `DefProc` and `DefClass`, variables accept `DefNamespaceVar` (`storeUnit` puts only those three
   kinds in `defsByName`). `targetFQ` also returns the side. That includes the `Origin` chases in
   `localDefinition` (`resolve.go:94,123`), or a `global test` link still returns `proc test`.
4. **Bindings that cannot reach.** The fallback ignores bindings *strictly after* the cursor
   (`NameStart > offset`), so that a cursor on a binding still answers itself, which
   `TestDefinitionProcLocalJumpsToReachingDefNotEarliestBinding` pins. `recordUses` unions across visits,
   so loop-carried uses keep resolving now that the fallback no longer rescues them. In
   `analyzeConservative`, uses are recorded from a precise view: the main body and each switch arm start
   from `in`; `on`/`trap` start from `in` plus the main body's bindings; `finally` also sees the handlers'
   bindings, since a handler may have run. The exit set stays conservative.
5. **Opaque frame for unknown bodies.** A new `FrameOpaque`: a trailing-brace body of a command that is not
   known to run its script in the enclosing frame. No unqualified variable defs and no bare `$x` answers
   there; qualified names, procs and command calls still work, and `proc`, `namespace eval` and
   `itcl::class` reset the frame as before. `FrameProc` and `FrameClass` are unchanged.
   `enclosingProcOrScope` accepts the opaque frame, so `set o [::C #auto]; $o m` inside a wrapper keeps
   working. `substRefs` drops variable refs from a proc or class body nested in a bracket.
6. **Gate and release.** Harness before/after, every remaining row read, patch release `v0.6.3`.

**Checked against a throwaway prototype.** Designs 2 to 5 were applied to a scratch copy of `server/`, not
this tree, with design 1 on its hot paths only (documentSymbol, semanticTokens, workspace/symbol,
`toLocations`). All 8 existing test packages stayed green with no test edited, so no existing test pins a namespace
variable through an unknown wrapper. These checks were repeated independently of the prototype's author: the existing
tests, the full `-race` suite, and the corpus run. The corpus run gave these counts, which are **harness counts for
the prototype, not for shipped code**:

| Metric | Baseline | Prototype |
| --- | ---: | ---: |
| `switch-pattern-resolved-as-call` | 106 | 0 |
| switch patterns colored as calls | 106 | 0 |
| refs `switch-pattern` | 18 | 0 |
| `command-resolved-to-non-callable:*` | 56 | 0 |
| refs `text-mismatch` | 6 | 0 |
| `binding-after-use-no-loop` | 83 | 0 |
| `outside-proc-unlinked` | 709 | 0 |
| symbols not in oracle | 887 | 3 |
| yajl-tcl timeouts / restarts | 4 / 4 | 0 / 0 |
| yajl-tcl run, wall clock | 106.8 s | 1.8 s |
| `CANDIDATE_VAR_MISS` (expected to rise) | 4,510 | 5,019 |
| `def_misses` (expected to rise) | 524 | 644 |

The plan's Task 6 explains these movements row by row.

**What this costs.** Silence replaces some answers that were right:
- TclOO same-class answers (see Decisions).
- A namespace-level variable assigned or read inside a custom wrapper that really does `uplevel`. The rule
  cannot tell those apart from a TclOO method. 54 `ns-set`/`ns-array-set` definitions leave the outline in
  the prototype (speedtables 34, TclProDebug 16, turtles 4). 53 are oracle `conf=heuristic` and 1 is a `bind`
  callback. The harness cannot measure the matching `$x` uses, since it does not probe them. `.rvt` page code
  runs at namespace level, so wrappers in pages are the most exposed. This corpus has only 4 `.rvt` files, so that cost is unmeasured (see Decisions).
- A known residual: a bare `$x` in `set r [with_lock { puts $x }]` at namespace level still resolves to the
  namespace variable, because references inside a bracket are tagged by the enclosing command, and dropping
  them would lose `$y` in `proc f {y} { set r [with_lock { puts $y }] }`.

## Decisions (made by the user, 2026-11-05)

1. **TclOO goes silent for now, rather than being modeled.** The W1 fix also silences the roughly 180 defensible
   same-class answers in prometheus-tcl and tclrmq. TclOO support, modeled the way Itcl is, would need its own design,
   since FlightAware's own repos use it.
2. **Internal code stays off limits, so the opaque-frame cost on it is unmeasured.** Nothing here measures how much
   page-level `.rvt` code the fix silences there. The public corpus has only 4 `.rvt` files and cannot answer that.
3. **The Python harness stays under `research/experiments/`.** `research/` had no Python until now. It is stdlib-only,
   not part of the shipped plugin, and it is the plan's before/after gate.
