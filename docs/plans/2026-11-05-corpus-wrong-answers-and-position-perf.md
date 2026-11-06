# Corpus Wrong Answers and Position Slowdown Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the four kinds of wrong answer and the position slowdown that the FlightAware corpus probe found, without adding features.

**Architecture:** A per-request line index replaces the rescan in `LSPPosition`. `scriptBodies` learns `switch`. The resolver filters lookups by command or variable side. Reaching-defs records uses from a precise view and the fallback stops returning later bindings. A new `FrameOpaque` marks bodies of unknown commands, where the server stays silent about bare variables.

**Tech Stack:** Go (stdlib only) under `server/`; the vendored Python harness in `research/experiments/08_corpus_probe/` is the before/after gate.

**Spec:** `research/08-flightaware-corpus-probe.md`

## Global Constraints

- Bugs only; no new features. The project rule: report only what is certain, stay silent otherwise. Out-of-scope features stay out (see `CLAUDE.md`).
- Go stdlib only. Baseline: `go -C server test ./...` passes on all 8 packages at `main@26f8ab2`.
- `CLAUDE.md` workflow: TDD; `adversarial-tester` adds edge cases before implementation on anything over 50 changed lines; `pr-review-toolkit:code-simplifier` when the finished diff exceeds 50 changed lines; implementers run only the test file they are working on while iterating, then the full suite once before reporting. Also `go test -race ./...` and `go vet ./...` at the end.
- One server per repo in harness runs.
- Work on a branch, not `main`. Its first commit is the prerequisites, which are uncommitted today: `research/08-flightaware-corpus-probe.md`, `research/experiments/08_corpus_probe/`, `research/experiments/08_relative_qualified.tcl`, this plan, and the edits to `research/README.md`, `research/02-namespace-resolution.md`, `docs/BACKLOG.md`. Leave `.claude/settings.json` out; its change predates this work.
- `git commit -- <paths>` silently skips untracked files, so every commit step runs `git add <paths>` first. Without it, new test files are lost from the commit.
- Release: patch `v0.6.3`. Bump `lua/tcl-lsp/version.lua` in the same change; after merge, `make -C server publish VERSION=v0.6.3` from main (`docs/RELEASING.md`).

## Review Focus

1. A `proc` and a variable with the same name: find-references and highlight from either side return only that side. Task 3.
2. A half-typed `switch` while editing: at every byte prefix, no panic, no pattern reported as a call, and the rest of the file still resolves. Task 2.
3. A live unsaved buffer, CRLF, and non-UTF-8 text through the position index: positions follow the latest edit and match the old scan exactly. Task 1.
4. `namespace eval` or `itcl::class` nested inside an unknown wrapper: they reset the frame, so their variables and members still resolve. Task 5.
5. `.rvt` switch arms spanning `<? ?>` tags: bodies walk, patterns stay silent, and outer and inner folds survive. Task 2.

## Where this plan departs from the first design, and why

Each of these was run against a throwaway prototype (a scratch copy of `server/`, not part of this repo); the spec has the results.

- **Task 2, odd word count:** returning nil for every odd count made calls in sibling arms go silent while a new arm's pattern was being typed; they resolve on `main@26f8ab2`. Only a braced last word now means the pairing is lost. An unbraced last word is dropped. Corpus results are identical either way.
- **Task 4, guard:** `NameStart > offset`, not `>=`. With `>=`, a cursor on the start of a binding loses its self-answer, and `TestDefinitionProcLocalJumpsToReachingDefNotEarliestBinding` fails.
- **Task 4, loops:** `recordUses` must union across visits. It keeps the first visit today, so `while {1} { puts $x; set x 1 }` gets an empty reaching set and resolves only through the fallback the guard now closes. The same change fixes `set x 0; while {1} { puts $x; set x 1 }`, which returns only `set x 0` on `main@26f8ab2`.
- **Task 4, `finally`:** starts from `in` plus the main body's **and the handlers'** bindings. A handler may have run, and leaving its binding out drops a real reaching definition.
- **Task 3:** also filters the `Origin` chases at `resolve.go:94,123`. On `main@26f8ab2`, a `global test` link next to `proc test` returns both.
- **Task 5, reaching:** `enclosingProcOrScope` must accept `FrameOpaque`. Otherwise `set o [::C #auto]; $o m` inside a wrapper or a TclOO method stops resolving; it resolves on `main@26f8ab2`.
- **Task 5, `substRefs`:** also drops variable refs from a `FrameClass` body. `if [catch {itcl::class X { method m {} { puts $a } }}] {}` leaks `$a` to `::a` otherwise.
- **Task 1:** the workspace-symbol map is built before the parallel fan-out, because filling it lazily there would be a concurrent map write.
- **Task 1, test:** the equivalence reference is a copy of the old scan. `LSPPosition` will share the new code, so comparing against it would prove nothing.
- **Task 6:** adds `symbol_names.py`, because result JSON keeps symbol counts and samples, not every name, so the required symbol diff had no tool.

### Departures made during implementation

The tasks below are kept as written. These rulings changed them, so **what shipped is what `research/08-flightaware-corpus-probe.md` "Fix design" describes, not the task text**. In particular, `enclosingFrameHeads` and "keep only a qualified name" did not ship.

- **Task 2, inline `switch`:** `switch $k proc - method { puts 1 }` made a proc named `-` through `decoratedProcDef`, so that function refuses a `switch` head. The plan did not list this.
- **Task 4, loop variables:** rebound on every iteration, or the union across visits adds a wrong answer in `foreach`.
- **Task 4, `for`:** `start` runs once, then the body before `next`. The old order killed back-edge bindings and ran `next` first.
- **Task 4, `tryParts`:** steps by clause, so a variable list named `finally` does not start a clause.
- **Task 4, `finally`:** sees the main body's and handlers' bindings but not its own, overriding "all gens". Confirmed against Tcl.
- **Task 4, empty answers:** an empty reaching answer is honored for non-array uses, and a cursor on a binding answers itself. The guard alone left sibling-branch and self-binding answers in place.
- **Task 4, unknown bodies:** their bindings stay out of reach sets and count only when nothing else reaches. An earlier ruling made them conservative and was replaced: it answered both lines for `set x 0; puts {set x 1}; puts $x`, and an exclusion list can never be complete.
- **Task 4, loop tests:** braced `while` and `for` tests are recorded at the loop header, which `binding-after-use-no-loop` cannot see (see the Task 6 result).
- **Task 4, Itcl parameters:** method parameters are locals, in a separate commit. A parameter-shadowed ivar was a wrong answer.
- **Task 4, `array set`:** it adds to what reaches rather than killing it. `array set c {a 1}; array set c {b 2}; array names c` used to drop the first line.
- **Final review, `switch` scaling:** each arm's bindings are collected into one set. Rebuilding the set per arm made goto-def quadratic in arms: 10.1 s at 4,000 arms, against 48 ms on `main@26f8ab2`.
- **Final review, uses in unknown bodies:** in a proc, the analyzer walks the one unknown body that holds the use, on its own copy of the set. `set x 1; set x 2; with { puts $x }` used to answer the killed `set`. The body's bindings still stay out of the outer reach set, and the corpus gate did not move.
- **Task 5, `eval`:** keeps the enclosing frame only in its one-word form `eval {script}`, overriding the task's list.
- **Task 5, `uplevel`:** removed from the task's list. Its brace runs in another frame, so `namespace eval ::n { uplevel #0 { set x 1 } }` would define `::n::x`.
- **Task 5, relative names:** at `FrameOpaque` only absolute names count as definitions or uses, overriding "keep only a qualified name". A relative name depends on a frame the server cannot know, and tclsh errors on the shape.
- **Task 5, the frame rule:** `bodiesRunHere` replaces `enclosingFrameHeads`, so the analyzer and the index agree on which bodies run here.
- **Task 5, bare calls:** bare command calls in unknown bodies keep resolving against the lexical namespace, though TclOO methods run in the object's namespace. A wrapper cannot be told apart from a method, and silencing would lose every call in wrappers. `research/08` states the limit.

---

### Task 1: Position index

**Files:**
- Modify: `server/internal/lsp/position.go:27-47`
- Modify: `server/internal/lsp/symbols.go:34-59,70,331-335,351-379`
- Modify: `server/internal/lsp/semantictokens.go:55-60`
- Modify: `server/internal/lsp/selectionrange.go:16-27,33,75-78`
- Modify: `server/internal/lsp/documenthighlight.go:13-26`
- Modify: `server/internal/lsp/folding.go:12-19`
- Modify: `server/internal/lsp/callhierarchy.go:79-81,113,116-157,235-285`
- Modify: `server/internal/lsp/server.go:410-425`
- Test: `server/internal/lsp/position_index_test.go` (new)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces, in `position.go`:

```go
type lineIndex struct{ /* src, line starts */ }
func newLineIndex(src string) *lineIndex
func (ix *lineIndex) lineChar(offset int) (line, character int) // same contract as LSPPosition
func (ix *lineIndex) position(offset int) Position

type lineIndexes map[string]*lineIndex // per-request only; never stored on Server
func (c lineIndexes) forFile(file string, sourceOf func(string) string) *lineIndex
```

- Must not change, because tests call them: `LSPPosition` (`position_test.go`, `callhierarchy_test.go`), `buildDocumentSymbols(defs, src, hoist)`, `buildFoldingRanges(folds, src)`, `buildWorkspaceSymbols(entries, query, sourceOf)`.
- Free to change, no test calls them: `leafSymbol`, `buildSelectionRange`, `callItem`. Pass them a `*lineIndex`.
- Remove `offsetToPosition`. Nothing in tests uses it.

- [ ] **Step 1: Write the benchmark and run it on unchanged code**

`BenchmarkDocumentSymbolLargeFile` over the generated file from Step 2. The baseline binary was never profiled, so this is the confirmation of the cause.

Run: `go -C server test ./internal/lsp/ -run '^$' -bench DocumentSymbolLargeFile -benchtime 1x`
Expected: several seconds or more per operation. Record it for the PR.

- [ ] **Step 2: Write the failing tests**

```go
// scanPosition is today's LSPPosition loop, copied verbatim. LSPPosition shares the new
// code after this task, so it cannot serve as the reference.
func TestLineIndexMatchesScan(t *testing.T) {
	fixtures := []string{"", "\n", "a", "a\n", "\n\n", "set x 1\nputs $x", "x😀y\né!\n",
		"a\r\nb\r\n", "\xff\xfe\nz\xc3", "😀", "no newline at end"}
	for _, src := range fixtures {
		ix := newLineIndex(src)
		for off := -1; off <= len(src)+2; off++ { // past-end clamp, mid-rune, negative
			wantL, wantC := scanPosition(src, off)
			if l, c := ix.lineChar(off); l != wantL || c != wantC { t.Errorf(/* src, off */) }
		}
	}
}

// 30,000 lines of `set ::a(k) {v}`. NewServer(nil) works: these paths never touch conn.
func TestLargeFileRequestsStayFast(t *testing.T) {
	// s.setDoc big.tcl and use.tcl ("puts $::a($k)\n"), then time each call:
	// buildDocumentSymbols(source.Defs(...), src, false), s.semanticTokens(...),
	// s.handleDefinition(...) on `$::a` from use.tcl.
	// Each: elapsed < 2*time.Second, and the result is non-empty.
}

// Review Focus 3. Via frame/runServer: didOpen, documentSymbol id 2, didChange that
// prepends two blank lines, documentSymbol id 3.
func TestPositionsFollowLiveEdits(t *testing.T) {
	// id 3's SelectionRange.Start.Line == id 2's + 2
}
```

The prototype measured 29 ms, 37 ms and 6 ms for those three calls, and 122 ms, 191 ms and 39 ms under `-race`, so the 2 s bound is safe in both modes.

- [ ] **Step 3: Run to verify they fail**

Run: `go -C server test ./internal/lsp/ -run 'TestLineIndexMatchesScan|TestLargeFileRequestsStayFast|TestPositionsFollowLiveEdits'`
Expected: build failure, `newLineIndex` undefined.

- [ ] **Step 3a: Spawn `adversarial-tester`**

- [ ] **Step 4: Implement `lineIndex`**

Line starts from one pass over `src`; lookup with `sort.SearchInts(starts, offset+1) - 1`. A negative result returns `(0, 0)`, matching the old loop for a negative offset. Clamp past-end first. Count UTF-16 from the line start with the same decode loop. `LSPPosition` becomes `newLineIndex(src).lineChar(offset)`.

- [ ] **Step 5: Replace every caller**

One `*lineIndex` per source per request in `semanticTokens`, `selectionRanges`, `documentHighlight`, `buildFoldingRanges`, `buildDocumentSymbols`. `lineIndexes` per request in `toLocations`, `incomingCalls`, `outgoingCalls`.

`buildWorkspaceSymbols` builds its `lineIndexes` for every entry's file **before** `par.Map`; the workers only read it. Update its comment at `symbols.go:351-355`.

- [ ] **Step 6: Run tests**

Run: `go -C server test ./internal/lsp/` then `go -C server test -race ./internal/lsp/`
Expected: PASS, with no existing test edited.

- [ ] **Step 7: Confirm on the real file**

Run: `python3 -I research/experiments/08_corpus_probe/probe.py --binary <built> --corpus <dir> --results <dir> yajl-tcl`, then `compare.py` and `inspect_rows.py --results <dir> yajl-tcl slowest_files`.
Expected: `airports.tcl` documentSymbol and semanticTokens under 2 s each, 0 timeouts, 0 restarts. The only count deltas are `var_counts.TIMEOUT 2 -> 0` and `VAR_OK 72 -> 74`. If either is still over 2 s, profile with pprof and report. Do not guess.

- [ ] **Step 8: Commit**

```bash
git add server/internal/lsp
git commit -m "perf(lsp): index line starts per request instead of rescanning" -- server/internal/lsp
```

### Task 2: Switch arms

**Files:**
- Modify: `server/internal/tcl/bodies.go:204-251` (the `switch` case, new helpers, and drop the switch clause from the comment at `:242-243`)
- Modify: `server/internal/tcl/reaching.go:238-249,256-264` (remove the inline workaround)
- Modify: `server/internal/tcl/fold.go:19-34`
- Modify: `server/internal/tcl/context_test.go:166-167` (comment only: "handled transitively" is no longer true)
- Test: `server/internal/tcl/switch_test.go` (new), `server/internal/resolve/switch_test.go` (new), `server/internal/source/switch_folds_test.go` (new)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces, in `bodies.go`:

```go
func switchArgs(w []Word) []Word          // words after options and subject; nil if none
func switchArmList(w []Word) (Word, bool) // the single braced arm-list word, when w is that form
func switchBodies(w []Word) []Word        // every braced arm body, in source order
```

Options: `-exact -glob -regexp -nocase` take no argument; `-matchvar V` and `-indexvar V` take one; `--` ends them. Any other word ends them and is the subject. `switchBodies` flattens `Parse(inner)` words into one list, each shifted by `list.Start + 1`, so `bracedInner(body, base)` still works. Pairs are (pattern, body); a non-braced body, including `-`, is skipped. An odd count with a braced last word has lost its pairing and returns nil. An unbraced last word is a pattern still being typed: drop it, and the complete pairs still walk.

- [ ] **Step 1: Write the failing tests**

`countCommandRefs`, `defsNamed` and `foldAt` already exist in package `tcl`.

```go
func TestSwitchBodiesWalked(t *testing.T) {
	cases := []struct{ name, src string; helpers int }{
		{"bare", "switch $x {\n stop { helper }\n}", 1},
		{"quoted pattern", "switch $x {\n \"--\" { helper }\n}", 1},
		{"braced pattern", "switch $x {\n {a b} { helper }\n}", 1},
		{"glob pattern", "switch -glob $x {\n a* { helper }\n}", 1},
		{"same line", "switch $x { a { helper } b { helper } }", 2},
		{"inline", "switch $x a { helper } b { helper }", 2},
		{"options", "switch -regexp -nocase -matchvar m -indexvar i -- $x {\n a { helper }\n}", 1},
		{"fall-through", "switch $x {\n a -\n b { helper }\n}", 1},
		{"pattern still being typed", "switch $x {\n a { helper }\n b\n}", 1},
		{"odd, braced last: pairing lost", "switch $x {\n a { helper }\n { puts 1 }\n}", 0},
		{"crlf", "switch $x {\r\n a { helper }\r\n}\r\n", 1},
		{"comment line", "switch $x {\n # note\n a { helper }\n}", 1},
		{"in a bracket", "set r [switch $x { a { helper } }]", 1},
		// isCmd's accepted limitation: a user proc named switch is read as switch.
		{"user proc, even", "switch a b { helper }", 1},
		{"user proc, odd", "switch a b c { helper }", 0},
	}
	// countCommandRefs(FileRefs(src), "helper") == helpers; for "stop", always 0
}

func TestSwitchProcDashArmMakesNoSymbol(t *testing.T) {
	// "switch $k {\n  proc -\n  method { puts 1 }\n}": len(FileDefs(src)) == 0
}

func TestSwitchDefsInsideArms(t *testing.T) {
	// "switch $x {\n a { proc g {} {} }\n}": defsNamed(FileDefs(src), "::g") has 1
}

func TestFileFolds_SwitchOuterAndInner(t *testing.T) {
	// "switch $x {\n  a {\n    puts 1\n  }\n}\n": foldAt true for the arm-list brace and the arm body
}

// Review Focus 2.
func TestSwitchHalfTypedAtEveryPrefix(t *testing.T) {
	// full := "proc ok {} {}\nswitch -glob -- $x {\n  stop { helper }\n  \"q\" -\n  {b c} { set y 1 }\n}\n"
	// For every prefix past the proc line: no panic in FileRefs, FileDefs, FileFolds, ReachingAt;
	// countCommandRefs(refs, "stop") == 0; defsNamed(defs, "::ok") has 1.
}
```

In `resolve/switch_test.go`:

```go
func TestSwitchPatternIsNotAReference(t *testing.T) {
	// with `proc stop {} {}`: Definition on `stop {` is nil; References of ::stop excludes it
}
func TestSwitchQuotedArmVariableResolvesToParam(t *testing.T) {
	// "proc f {y} {\n  switch $k {\n    \"a\" { puts $y }\n  }\n}": $y -> the param
}
// Review Focus 5. Shape from rvt/testdata/corpus/control_flow_spanning.rvt.
func TestSwitchArmsSpanRVTTags(t *testing.T) {
	// "<?\nproc helper {} {}\nswitch $x {\n  a { ?>\n<li>one</li>\n<? helper\n  }\n  b { ?>\n<li>two</li>\n<? puts 2\n  }\n}\n?>\n"
	// helper in the arm resolves (1 location); `a` is nil
}
```

`source/switch_folds_test.go`: `source.Folds("p.rvt", src)` on that same source has the arm-list fold and the first arm fold, with `src[f.Open] == '{'` and `src[f.Close] == '}'` on every fold.

- [ ] **Step 2: Run to verify they fail**

Run: `go -C server test ./internal/tcl/ ./internal/resolve/ ./internal/source/ -run 'Switch'`
Expected: FAIL on quoted, braced, same-line, inline and pattern-silence cases.

- [ ] **Step 3: Spawn `adversarial-tester`**

Brief it with this task and the file list. It adds edge cases to the three test files before implementation.

- [ ] **Step 4: Implement**

Add `case "switch": return switchBodies(w)` to `scriptBodies`. Remove the workaround from `analyzeConservative`, since `scriptBodies` now returns every arm. In `FileFolds`, before the `childBodies` loop, append the arm list's fold when `switchArmList(c.Words)` matches, computing it with `bracedInner` exactly as `childBodies` does, so an unterminated brace behaves the same.

- [ ] **Step 5: Run tests**

Run: `go -C server test ./internal/tcl/ ./internal/resolve/ ./internal/source/`
Expected: PASS, including the unchanged `TestFileRefsSwitchArms`, `TestReachingSwitchArms`, `TestReachingSwitchInlineArms`.

- [ ] **Step 6: Commit**

```bash
git add server/internal/tcl server/internal/resolve server/internal/source
git commit -m "fix(tcl): parse switch arms as pattern/body pairs, not commands" -- server/internal/tcl server/internal/resolve server/internal/source
```

### Task 3: Separate commands from variables

**Files:**
- Modify: `server/internal/resolve/resolve.go:94,123,265-269,416-425,459,514-518,532-535,549-571,579-590,618-629`
- Test: `server/internal/resolve/sides_test.go` (new), `server/internal/lsp/sides_test.go` (new)

**Interfaces:**
- Consumes: nothing from other tasks.
- Produces, in `resolve.go` (all unexported):

```go
type nameSide int
const ( sideCommand nameSide = iota; sideVariable )
func sideOfRef(k tcl.RefKind) nameSide
func sideOfDef(k tcl.DefKind) nameSide // DefNamespaceVar, DefIvar, DefLocal, DefGlobalLink: variable; else command
func (r *Resolver) lookupSide(name, file string, side nameSide) []index.Location // lookupScoped, filtered
func (r *Resolver) targetFQ(file, src string, offset int) (string, nameSide)     // signature change
```

Kind sets, read from `storeUnit` (`index.go:124-132`): `defsByName` only ever holds `DefProc`, `DefNamespaceVar` and `DefClass`. `DefMethod` and `DefIvar` go to the class table and never appear in `Lookup`. So through `Lookup`, the command side is `DefProc` + `DefClass` and the variable side is `DefNamespaceVar`. `Index.Lookup` stays unchanged.

Sites that change:
- `Definition` (`:265-269`), `refFQ` (`:618-629`): `lookupSide` with the ref's side.
- `CommandRefKind` (`:532-535`): `sideCommand`.
- `localDefinition`'s `Origin` chases (`:94`, `:123`): `sideVariable`.
- `targetFQ` (`:579-590`): the side comes from `sideOfDef` on a definition, or `sideOfRef` on a reference.
- `References` (`:416-425`, `:459`), `Declarations` (`:514-518`), `FileHighlights` (`:549-571`): use the side for `lookupSide`. Keep a ref only when `sideOfRef(ref.Ref.Kind) == side`. `FileHighlights` also filters its in-file declarations with `sideOfDef`.

`callhierarchy.go` needs no edit: it goes through `References`, and through `Definition` with an `isCallable` filter, and `methodCallSites` already requires `RefCommand`. Pin that with the lsp test below.

- [ ] **Step 1: Write the failing tests**

Each builds `ix := index.New(); ix.IndexFile(...); r := New(ix)`.

```go
func TestCommandRefIgnoresSameNamedVariable(t *testing.T) {
	// "proc ::rmq::debug {m} {}\nset ::rmq::debug 1\n::rmq::debug hello":
	// Definition on the call -> 1 location, Kind DefProc
}
func TestVariableRefIgnoresSameNamedProc(t *testing.T) {
	// same source plus "puts $::rmq::debug": Definition -> 1 location, Kind DefNamespaceVar
}
func TestGlobalLinkChaseIgnoresSameNamedProc(t *testing.T) {
	// "proc test {} {}\nset test 1\nproc f {} {\n  global test\n  puts $test\n}":
	// Definition on $test -> only `set test`
}
// Review Focus 1, both directions.
func TestReferencesOfProcExcludeVariableUses(t *testing.T) {
	// "proc test {} {}\nset test 1\ntest\nputs $test": from `proc test`, only the `test` call
}
func TestReferencesOfVariableExcludeProcCalls(t *testing.T) {
	// same source: from `set test`, only `$test`
}
func TestDeclarationsHonorSide(t *testing.T)    { /* from the call: proc site only; from $test: set site only */ }
func TestFileHighlightsHonorSide(t *testing.T)  { /* from each side: that side's refs + declaration only */ }
func TestCommandRefKindIgnoresVariable(t *testing.T) {
	// "set ::a::f 1\nproc ::a::f {} {}\n::a::f": the variable is indexed first; kind == DefProc, ok
}
func TestBareCommandWithOnlyVariableStaysSilent(t *testing.T) {
	// "set key 1\nkey id": Definition on `key id` is nil
}
func TestCommandFallsThroughVariableToOuterProc(t *testing.T) {
	// "proc debug {} {}\nnamespace eval ::n {\n  variable debug 1\n  debug\n}": `debug` -> ::debug
}
func TestClassHeadStillResolves(t *testing.T) {
	// "itcl::class ::Foo {}\nset o [::Foo #auto]": the class name in the bracket -> the class
}
```

In `lsp/sides_test.go`, via `frame`/`runServer`/`decodeSemanticTokens`:

```go
func TestSemanticTokenForCallIsNotVariable(t *testing.T) { /* the call's token typ == stFunction */ }
func TestIncomingCallsExcludeVariableUses(t *testing.T)  { /* $test does not appear in FromRanges */ }
```

- [ ] **Step 2: Run to verify they fail**

Run: `go -C server test ./internal/resolve/ ./internal/lsp/ -run 'Sides|Ignores|Exclude|HonorSide|StaysSilent|FallsThrough|ClassHead|NotVariable'`
Expected: FAIL on the collision cases. `TestClassHeadStillResolves` passes already and is a guard.

- [ ] **Step 3: Spawn `adversarial-tester`**

- [ ] **Step 4: Implement the sites listed above**

- [ ] **Step 5: Run tests**

Run: `go -C server test ./internal/resolve/ ./internal/lsp/` then `go -C server test -race ./internal/resolve/`
Expected: PASS. `warmNamespaces` is untouched, so the parallel scan stays read-only.

- [ ] **Step 6: Commit**

```bash
git add server/internal/resolve server/internal/lsp
git commit -m "fix(resolve): keep commands and variables on separate sides of lookup" -- server/internal/resolve server/internal/lsp
```

### Task 4: Bindings that cannot reach

**Files:**
- Modify: `server/internal/resolve/resolve.go:76-82,107-121` (guard and comments)
- Modify: `server/internal/tcl/reaching.go:221-250,326-333,409-426` and the comment at `:226-231`
- Modify: `server/internal/tcl/bodies.go:332-356` (`tryParts`; `tryBodies` built on it)
- Modify: `docs/BACKLOG.md:47-49,58-63` (the "graceful" wording, items 2 and 3)
- Test: `server/internal/tcl/reaching_test.go`, `server/internal/resolve/reaching_guard_test.go` (new)

**Interfaces:**
- Consumes: `switchBodies` via `scriptBodies` (Task 2). The workaround is already gone.
- Produces, in `bodies.go`:

```go
func tryParts(w []Word) (main *Word, handlers []Word, finally *Word)
```

`main` is nil for `try $script …`, so a handler is never mistaken for the main body. `tryBodies` keeps its order: main, then handlers and finally in source order.

The rules:
- **Fallback (`localDefinition`):** skip a binding when `d.NameStart > offset`. Strictly greater: a cursor on a binding still answers itself. All three paths into the fallback get the same guard, and this is the decision for each:
  - no reaching def: a use before its only binding goes nil, then `ivarInClass`;
  - array element: `$a(1)` before `set a(1)` is nil. A loop-carried array use also goes silent, accepted because arrays bypass reaching;
  - oversized proc (over 200,000 bytes): same; a loop-carried use there also goes silent, accepted.
- **`recordUses` and the `isRMW` branch:** remove the first-visit-wins guards. Union `in[name]` into `a.answer` with `appendDedup` on every visit, and set `a.found`. Reaching sets only grow across loop iterations, so the last visit is the fixpoint.
- **`analyzeConservative`:** bindings and the exit set stay exactly as they are. Only the entry view changes:
  - `catch` body and each `switch` arm: `in`;
  - `try` main body: `in`;
  - `on`/`trap` handler: `in` joined with the main body's bindings (`collectBindings`);
  - `finally`: `in` joined with all gens, which is today's `bodyView`.

- [ ] **Step 1: Write the failing tests**

In `resolve/reaching_guard_test.go`, add a helper `defLinesAt(t, src, mark string) []int`: index `src` as `a.tcl`, call `Definition` at `strings.LastIndex(src, mark)+1`, return sorted 1-based lines.

```go
func TestDefinitionBindingsThatCannotReach(t *testing.T) {
	inTry := "proc in_try {x} {\n    try {\n        puts $x\n        set x 2\n    } on error {} {\n    }\n}"
	inCatch := "proc in_catch {y} {\n    catch {\n        puts $y\n        set y 2\n    }\n}"
	afterCatch := "proc after_all {} {\n    set z 0\n    catch { set z 1 }\n    puts $z\n}"
	cases := []struct{ name, src, mark string; want []int }{
		{"try, before set", inTry, "$x", []int{1}},                // was [1,4]
		{"catch, before set", inCatch, "$y", []int{1}},            // was [1,4]
		{"control, after catch", afterCatch, "$z", []int{2, 3}},   // must stay
		{"regexp, then later set", "proc f {} {\n  regexp {a} $s word\n  puts $word\n  set word 2\n}", "$word", nil},
		{"itcl ivar over later set", "itcl::class C {\n  protected variable n 0\n  method m {} {\n    puts $n\n    set n 1\n  }\n}", "$n", []int{2}},
		{"loop, no prior", "proc f {} {\n  while {1} {\n    puts $x\n    set x 1\n  }\n}", "$x", []int{4}},
		{"loop, prior", "proc f {} {\n  set x 0\n  while {1} {\n    puts $x\n    set x 1\n  }\n}", "$x", []int{2, 5}},
		{"loop, in catch", "proc f {} {\n  while {1} {\n    catch {\n      puts $x\n      set x 1\n    }\n  }\n}", "$x", []int{5}},
		{"foreach carried", "proc f {l} {\n  foreach i $l {\n    if {$i} { puts $prev }\n    set prev $i\n  }\n}", "$prev", []int{4}},
		{"handler sees body", "proc f {} {\n  try {\n    set x 1\n  } on error {} {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"finally sees body", "proc f {} {\n  try {\n    set x 1\n  } finally {\n    puts $x\n  }\n}", "$x", []int{3}},
		{"finally sees handler", "proc f {} {\n  set x 0\n  try {\n    set a 1\n  } on error {} {\n    set x 2\n  } finally {\n    puts $x\n  }\n}", "$x", []int{2, 6}},
		{"arm, own later set", "proc f {k} {\n  set x 0\n  switch $k {\n    a { puts $x\n      set x 1 }\n  }\n}", "$x", []int{2}},
		{"arm, other arm's set", "proc f {k} {\n  set x 0\n  switch $k {\n    a { set x 1 }\n    b { puts $x }\n  }\n}", "$x", []int{2}},
		{"global before use", "set ::x 1\nproc f {} {\n  global x\n  puts $x\n}", "$x", []int{1}},
		{"array, before set", "proc f {} {\n  puts $a(1)\n  set a(1) 2\n}", "$a", nil},
		{"array, after set", "proc f {} {\n  set a(1) 2\n  puts $a(1)\n}", "$a", []int{2}},
		{"inside a wrapper, before set", "proc f {} {\n  with { puts $x }\n  set x 1\n}", "$x", nil},
	}
}

func TestDefinitionOversizedProcGuard(t *testing.T) {
	// over 200 KB of filler, then `puts $x` then `set x 1`: nil.
	// With `set x 1` before `puts $x`: the binding.
}
func TestDefinitionCursorOnBindingAnswersItself(t *testing.T) {
	// the start byte of `x` in "set x 1", and in a first "incr x": 1 location at that offset.
	// TestDefinitionProcLocalJumpsToReachingDefNotEarliestBinding also pins this; leave it unedited.
}
```

In `tcl/reaching_test.go`, using `reachAtMarker`:

```go
func TestReachingUseInsideLoopSeesBackEdge(t *testing.T) { /* `set y $x` inside the loop: 2 defs */ }
func TestReachingTryWithUnbracedBody(t *testing.T) {
	// "proc f {} {\n  set x 1\n  try $script on error {} {\n    puts $x\n  }\n}": 1 def
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go -C server test ./internal/resolve/ ./internal/tcl/ -run 'CannotReach|OversizedProcGuard|CursorOnBinding|SeesBackEdge|UnbracedBody'`
Expected: FAIL on try, catch, regexp, Itcl, array and arm cases.

- [ ] **Step 3: Spawn `adversarial-tester`**

- [ ] **Step 4: Implement the rules above, then update the comments**

Rewrite `reaching.go:226-231` and `resolve.go:76-82,107-109` to describe the new behaviour. In `docs/BACKLOG.md`: drop "graceful" for the fallback, remove item 2 (a `$x` in an arm now gets a precise answer), and correct item 3 (a `try … on e` name now goes silent unless an earlier binding exists).

- [ ] **Step 5: Run tests**

Run: `go -C server test ./internal/tcl/ ./internal/resolve/ ./internal/source/`
Expected: PASS. `ClassOf` also reads `ReachingAt`, so the `classof` tests in `tcl` and `source` are part of this gate.

- [ ] **Step 6: Commit**

```bash
git add server/internal/tcl server/internal/resolve docs/BACKLOG.md
git commit -m "fix(reaching): stop answering with bindings that cannot reach the use" -- server/internal/tcl server/internal/resolve docs/BACKLOG.md
```

### Task 5: Opaque frame for unknown bodies

**Files:**
- Modify: `server/internal/tcl/context.go:10-14`
- Modify: `server/internal/tcl/bodies.go:71-75,144-151,240-245` (default branch, doc comments)
- Modify: `server/internal/tcl/defs.go:109,236-256`
- Modify: `server/internal/tcl/refs.go:215-228`
- Modify: `server/internal/tcl/reaching.go:37-40,58-60`
- Test: `server/internal/tcl/opaque_test.go` (new), `server/internal/resolve/opaque_test.go` (new)

**Interfaces:**
- Consumes: the `switch` case from Task 2 (a pass-through head) and the side-aware resolver from Task 3.
- Produces, in `context.go` and `bodies.go`:

```go
FrameOpaque // appended last in the FrameKind iota, so existing values do not move

// Heads whose script runs in the enclosing frame.
var enclosingFrameHeads = map[string]bool{ "if", "while", "for", "foreach", "lmap", "dict",
	"catch", "try", "switch", "eval", "uplevel", "time", "timerate" }
```

The rules:
- **`childBodies` default branch:** a body gets `FrameOpaque` when `frame == FrameNamespace` and `w[0]` is not in `enclosingFrameHeads`. Otherwise it keeps `frame`, so an `if` inside an opaque body stays opaque. `namespace eval`, `proc` and `itcl::class` keep resetting the frame through their own cases. `FrameProc` and `FrameClass` behaviour is unchanged.
- **`emitDefs` at `FrameOpaque`:** `variable NAME` (`defs.go:109`) needs a guard, since it emits regardless of frame: keep only a qualified name. `emitVarAssign` (`defs.go:236-256`) needs a guard too, because its second branch would otherwise emit a `DefLocal`: return after the qualified check. Procs still define.
- **`substRefs` (`refs.go:226-228`):** drop `RefVariable` results when `b.Frame == FrameProc || b.Frame == FrameClass`, and keep command refs (`TestSubstScriptBody_ProcInsideSubst` and `TestSubstScriptBody_ItclClassInsideSubst` pin them). Do not drop for other frames: the neutral frame there would lose `$y` in `proc f {y} { set r [with_lock { puts $y }] }`.
- **`enclosingProcOrScope` (`reaching.go:58-60`):** accept `b.Frame == FrameOpaque` next to `FrameNamespace`, and return `b.Frame`. The analyzer only special-cases `FrameProc`, so this is safe, and it keeps `$o m` typed inside a wrapper.

Checked by reading, and no change needed:
- `variableCandidates` (`resolve.go:370`) returns nil for a bare name at any frame but `FrameNamespace`.
- `localAt` (`resolve.go:32-49`) needs a local binding or `FrameProc`.
- `objmethod.go` only threads the frame into `childBodies`, whose gate is `frame == FrameClass`.
- `classof.go:57` and `collectBindings` (`reaching.go:280`) hard-code `FrameProc`.
- `FileFolds` gets the same bodies, since the frame does not change which bodies `scriptBodies` returns.

**Known and accepted:**
- TclOO same-class answers go silent (spec, Decisions).
- A bare `$x` in `set r [with_lock { puts $x }]` at namespace level still resolves to the namespace variable, because bracket refs are tagged by the enclosing command.

**Stop rule:** if an existing test pins a namespace variable through an unknown wrapper, stop and report it rather than editing the test. The prototype found none.

- [ ] **Step 1: Write the failing tests**

```go
// tcl/opaque_test.go, using defsNamed
func TestOpaqueFrameDefs(t *testing.T) {
	cases := []struct{ src, name string; want int }{
		{"oo::class create X { method m {} { set x 1 } }", "::x", 0},
		{"oo::class create X { method m {} { set x 1 } }", "x", 0},   // no fake local either
		{"oo::class create X { variable v }", "::v", 0},
		{"tsp::proc f {a} { set b 1 }", "::b", 0},
		{"tsp::proc f {a} { set b 1 }", "b", 0},
		{"with { if {1} { set x 1 } }", "::x", 0},                    // if inside opaque stays opaque
		{"namespace eval ::n { with { set x 1 } }", "::n::x", 0},
		{"if {1} { set x 1 }", "::x", 1},
		{"eval { set x 1 }", "::x", 1},
		{"foreach i $l { set x 1 }", "::x", 1},
		{"namespace eval ::n { if {1} { set x 1 } }", "::n::x", 1},
		{"with { set ::x 1 }", "::x", 1},
		{"with { variable ::y }", "::y", 1},
		{"with { proc g {} {} }", "::g", 1},
		// Review Focus 4.
		{"with { namespace eval ::n { set v 1 } }", "::n::v", 1},
	}
}
```

```go
// resolve/opaque_test.go, using defLinesAt from Task 4
func TestOpaqueFrameResolution(t *testing.T) {
	cases := []struct{ name, src, mark string; want []int }{
		{"TclOO local", "oo::class create X {\n  method m {} {\n    set x 1\n    puts $x\n  }\n}", "$x", nil},
		{"tsp::proc param vs global", "set file 1\ntsp::proc f {file} {\n  puts $file\n}", "$file", nil},
		{"no crossing classes", "oo::class create A {\n  method m {} {\n    set _value 0\n  }\n}\noo::class create B {\n  method n {} {\n    return $_value\n  }\n}", "$_value", nil},
		{"bare in wrapper", "set x 1\nwith {\n  puts $x\n}", "$x", nil},
		{"qualified in wrapper", "set ::x 1\nwith {\n  puts $::x\n}", "$::x", []int{1}},
		{"namespace-level if", "set x 1\nif {1} {\n  puts $x\n}", "$x", []int{1}},
		{"wrapper inside a proc", "proc f {a} {\n  with {\n    puts $a\n  }\n}", "$a", []int{1}},
		{"proc in bracket, global set", "set a 1\nif [catch {proc g {a} {puts $a}}] {}", "$a", nil},
		{"class in bracket, global set", "set a 1\nif [catch {itcl::class X { method m {} { puts $a } }}] {}", "$a", nil},
		{"wrapper in bracket in proc", "proc f {y} {\n  set r [with_lock { puts $y }]\n}", "$y", []int{1}},
		// Review Focus 4.
		{"namespace eval in wrapper", "with {\n  namespace eval ::n {\n    set v 1\n    puts $v\n  }\n}", "$v", []int{3}},
		{"proc in wrapper", "with {\n  proc g {a} {\n    puts $a\n  }\n}", "$a", []int{2}},
		{"itcl::class in wrapper", "with {\n  itcl::class C {\n    variable v\n    method m {} {\n      puts $v\n    }\n  }\n}", "$v", []int{3}},
	}
}
func TestObjMethodTypedInsideOpaqueBodies(t *testing.T) {
	// `$o m` resolves to ::C's method (1 location) in each, with `set o [::C #auto]` just before it:
	// inside `with { … }` at top level; in a TclOO method; in `with { … }` in a .rvt page.
	// Use a fresh index per case: one index holding ::C in two files correctly returns 2 locations.
}
```

Existing tests that must stay green unedited: `TestFileDefsInControlFlowBodies`, `TestFileRefsScriptBlockArgument` (`context_test.go:142`, calls inside unknown bodies), and the `TestSubstScriptBody_*` family.

- [ ] **Step 2: Run to verify they fail**

Run: `go -C server test ./internal/tcl/ ./internal/resolve/ -run 'Opaque'`
Expected: FAIL (`FrameOpaque` is not referenced by the tests, so they compile and fail on the TclOO, `tsp::proc` and bracket cases).

- [ ] **Step 3: Spawn `adversarial-tester`**

- [ ] **Step 4: Implement the rules above**

Rewrite the comments at `bodies.go:71-75`, `:144-145` and `:240-245`: a trailing brace is walked for calls, but its frame is unknown. Update the frame list in `reaching.go:37-40`.

- [ ] **Step 5: Run tests**

Run: `go -C server test ./internal/tcl/ ./internal/resolve/ ./internal/source/ ./internal/lsp/`
Expected: PASS with no existing test edited.

- [ ] **Step 6: Commit**

```bash
git add server/internal/tcl server/internal/resolve
git commit -m "fix(tcl): stop treating unknown command bodies as namespace frames" -- server/internal/tcl server/internal/resolve
```

### Task 6: Gate, docs, release

**Files:**
- Create: `research/experiments/08_corpus_probe/symbol_names.py`
- Modify: `research/experiments/08_corpus_probe/README.md`, `research/07-realworld-itcl-survey.md:81-92`, `CHANGELOG.md`, `README.md:299`, `lua/tcl-lsp/version.lua`

**Interfaces:**
- Consumes: Tasks 1-5.
- Produces: `symbol_names.py --binary PATH --corpus DIR --out DIR [repo ...]`. It reuses `lspclient.py`, sends `workspace/symbol` with an empty query, and writes sorted `file:line kind name` lines to `<out>/<repo>.txt`, one server per repo. Plain `diff -r` between two outputs is the symbol diff.

- [ ] **Step 1: Full suite on the final head**

First run `pr-review-toolkit:code-simplifier` over the branch diff and a whole-branch review, with no open Critical or Important findings.

Run: `go -C server test ./...`, `go -C server test -race ./...`, `go -C server vet ./...`
Expected: all pass, 8 packages.

- [ ] **Step 2: Harness before/after**

Run: `probe.py --binary <new> --corpus <dir> --results research/experiments/08_corpus_probe/results`, then `compare.py research/experiments/08_corpus_probe/baseline/counts.json research/experiments/08_corpus_probe/results`.

Check against this table. The Prototype column is what a throwaway prototype of Tasks 1-5 produced; it is a guide, not a target.

| Metric | Baseline | Target | Prototype | Shipped |
|---|---|---|---|---|
| `switch-pattern-resolved-as-call` | 106 | 0 | 0 | 0 |
| switch patterns colored as calls | 106 | 0 | 0 | 0 |
| refs `switch-pattern` | 18 | 0 | 0 | 0 |
| `command-resolved-to-non-callable:*` | 56 | 0 | 0 | 0 |
| refs `text-mismatch` | 6 | 0 | 0 | 0 |
| `binding-after-use-no-loop` | 83 | 0, or each row read and justified | 0 | 15, each read: right answers in braced loop tests |
| `outside-proc-unlinked` | 709 | 0 | 0 | 0 |
| symbols not in oracle | 887 | each remaining row read | 3 | 3, the same rows |
| airports.tcl documentSymbol / semanticTokens | 40.6 s / 20.2 s | under 2 s each | 118 ms slowest; nothing else in the repo over 200 ms | 273 ms / 83 ms at worst, on a heavily loaded machine; nothing else in the repo over 3 ms |
| test2.tcl goto-def timeouts | 2 | 0 | 0 | 0 |

**Shipped result.** `CANDIDATE_VAR_MISS` 4,510 → 4,709, `def_misses` 524 → 649, call `OK` 4,464 → 4,595, `VAR_OK` 15,514 → 15,936, with `VAR_OK` falling only in prometheus-tcl (415 → 398) and tclrmq (510 → 449). The 15 rows are 11 in TclProDebug and 4 in speedtables. The prototype had 0 because it did not record braced loop tests at the loop header; the harness does not tag those tests as in-loop, so this is not a regression. `research/08` has every row, the symbol diff and the costs.

The 3 prototype symbols, already read:
- `TclProDebug/lib/tcldebugger/instrument.tcl:620` (2): `lassign` writing through `variable` links; the symbols look right.
- `speedtables/ctable_server/ctable_client.tcl:372` (1): `scanQuoted` has no bracket awareness, so the `"` inside `[join $codeList "\n"]` at line 368 flips quoting. Pre-existing and newly visible because this quoted arm is now walked. Not in this plan.

Expected to move the "wrong" way, and fine:
- `CANDIDATE_VAR_MISS` rises, because wrong answers become silent (prototype 4,510 → 5,019).
- `def_misses` rises (prototype 524 → 644): `oo-variable` 73, `ns-set` 46, `ns-array-set` 8, `namespace eval` 4, `proc-variable-link` 4. The first and last two are TclOO bodies and namespaces left with no symbols. The `ns-*` rows are bodies of unknown commands, oracle `conf=heuristic`, except one `bind` callback.

Must be explained if it moves. What the prototype showed:
- **Call `OK` falling outside switch-pattern rows.** 3 rows in tsp: `bench/langbench/proc.tcl:23`, `:31`, `handcoded/cat-ext.tcl:98`. Those baseline "OK"s were themselves wrong: `f` and `d` resolved to `set f`/`set d` in other files (W3). `OK` otherwise rises, 4,464 → 4,595, from arms now walked.
- **`VAR_OK` falling in plain procs.** Falls only in the TclOO repos, prometheus-tcl 415 → 398 and tclrmq 510 → 449. Total rises, 15,514 → 15,641. The harness stores counts only here, so a fall elsewhere needs a sampled read.
- **`workspace_symbols.count` outside tclrmq, tsp, prometheus-tcl.** TclProDebug −18 (16 wrapper variables, 2 `-`), speedtables −31, turtles −4 (wrapper variables), piaware +11 and piaware-gui +1 (`set ::x` in quoted arms now found).
- **Others.** `refs_missing` piaware 153 → 158 and `resolved-but-uncolored` +5 follow from more resolved calls being checked (misses 4 and 9). 2 call rows named `-` disappear, because `-` is no longer a known name.

Read every remaining row in the wrong-answer categories and justify it in the PR. `name-mismatch` stays at 3: all `$ns` in `nub.tcl:1674-1676`, an oracle false positive (spot-check row 20).

- [ ] **Step 3: Symbol diff, since namespace-level `$x` is unprobed**

Build `main@26f8ab2` in a temporary worktree and run `symbol_names.py` with both binaries. Run `diff -r` per repo, read a sample from each repo with changes, and put the per-repo counts and the sample in the PR.
Expected: removals are TclOO, `tsp::proc` and wrapper-body variables; additions are arm-body definitions. Anything else is a bug to explain.

Internal FlightAware code is off limits (spec, Decisions): do not read it, list it, or run anything against it, and do not name internal repos in this public repo. Task 5's cost on page-level `.rvt` code therefore stays unmeasured, so say that in the PR.

- [ ] **Step 4: Update docs**

- `research/07`: under Remaining gaps, TclOO bodies are now silent rather than walked as namespace code, with a pointer to `research/08`.
- `README.md:299`: add that TclOO names stay silent.
- `CHANGELOG.md`: it is empty today. Add a `v0.6.3` entry covering the fixes.
- `research/experiments/08_corpus_probe/README.md`: add `symbol_names.py`, and the new rerun cost.
- Check `CLAUDE.md` for anything these changes contradict.

- [ ] **Step 5: Bump the version**

`lua/tcl-lsp/version.lua` to `return "v0.6.3"`.

- [ ] **Step 6: Commit**

```bash
git add research docs README.md CHANGELOG.md lua/tcl-lsp/version.lua
git commit -m "docs: record corpus gate results and bump to v0.6.3" -- research docs README.md CHANGELOG.md lua/tcl-lsp/version.lua
```

- [ ] **Step 7: After merge, publish from main**

Run: `make -C server publish VERSION=v0.6.3`
Expected: GitHub Release with four targets and `SHA256SUMS`; `version.lua` rewrite is a no-op.

---

## Self-review

- **Spec coverage:** W1 Task 5; W2 Task 2; W3 Task 3; W4 and the `recordUses` finding Task 4; the slowdown Task 1; the gate Task 6. The misses are out of scope on purpose and listed in `docs/BACKLOG.md`.
- **Review Focus:** each of the five lines has a test in its owning task, marked in the test code.
- **Consistency:** `defLinesAt` is defined in Task 4 and used in Task 5. `FrameOpaque`, `switchArgs`, `switchArmList`, `switchBodies`, `tryParts`, `lineIndex`, `lineIndexes`, `nameSide` and `lookupSide` are each defined once.
- **Line references:** checked against `main@26f8ab2`. Task 4's and Task 5's `reaching.go` references predate Task 2's removal of about 20 lines; find the function by name.
