# FlightAware corpus probe harness

The tool behind `research/08-flightaware-corpus-probe.md`. It drives a real `tcl-lsp`
binary over stdio across 10 public Tcl repos and classifies every answer against an
independent Tcl word parser (the "oracle"), not against the server's own output.

**Python 3.12, stdlib only, and not part of the shipped plugin.** Nothing here is
installed, packaged, or used by the Lua/Vim clients or the Go server. It lives under
`research/` as the evidence and the before/after gate for server changes.

No corpus code is executed. The corpus is only read as text.

## Run it

```bash
H=research/experiments/08_corpus_probe
$H/fetch_corpus.sh /tmp/corpus                         # 10 repos, each fetched by full SHA
go -C server build -o /tmp/tcl-lsp ./cmd/tcl-lsp
python3 -I $H/probe.py --binary /tmp/tcl-lsp --corpus /tmp/corpus --results $H/results
python3 -I $H/probe.py --binary /tmp/tcl-lsp --corpus /tmp/corpus --results $H/results tsp turtles   # a subset
```

Full run: about 3.5 minutes on `main@26f8ab2`, of which yajl-tcl is about 105 s. That
is the slowdown described in research/08, so expect it to fall after the position fix.

```bash
python3 -I $H/compare.py $H/baseline/counts.json $H/results      # per-repo deltas; exit 1 on any
python3 -I $H/counts.py $H/results /tmp/counts.json              # write counts; keep out.json outside results/
python3 -I $H/summarize.py --results $H/results                  # build results/SUMMARY.md
python3 -I $H/inspect_rows.py --results $H/results <repo> <section> [class|field=value] [n] [offset]
python3 -I $H/oracle_smoke.py /tmp/corpus [file-suffix]          # oracle only, no server
```

`compare.py` compares only the repos present in the results directory, and reports a
repo that left a `<repo>.FAILED.txt`. It compares counts, not individual rows or timings.

## The baseline

`baseline/SUMMARY.md` and `baseline/counts.json` come from a binary built from
`main@26f8ab2`, over the commits pinned in `fetch_corpus.sh`. `baseline/SUMMARY.md` is a
verbatim copy, so its "see HARNESS-NOTES.md" line means this README.

**The baseline is reproducible.** A full rerun matched the first run exactly: every
count and every classified row. Timings do vary, which is why `counts.json` leaves them
out.

## How a run works

Each repo gets one server process, started with `rootUri` set to the repo and
`capabilities.window.workDoneProgress=false`. Every probe for that repo goes through
that one process, which then gets `shutdown`/`exit`. Server→client requests
(`client/registerCapability`) are answered with `null`.

- Every request has a 10 s timeout. A timeout or a dead server is recorded in
  `<repo>.json:timeouts`, and the server is restarted (re-initialized and re-indexed, with
  open docs re-sent) for the remaining probes.
- When `documentSymbol` or `semanticTokens/full` times out, it is retried once with a 240 s
  timeout to measure the real latency (`files_flagged[].retry_long`).
- Index time: the server indexes synchronously after its initialize reply, so `index_ms` is
  the time from the initialize reply to the reply to an unknown-method "ping" sent right
  after `initialized`.
- Positions: the server uses UTF-16 `character` (`position.go`). `posutil.py` reproduces
  that, counting an invalid UTF-8 byte as one unit, which matches Go's `RuneError` handling.

Files: `lspclient.py` (client), `tclparse.py` (independent Tcl word parser), `roles.py`
(which braced word is a script, expr, data or callback), `oracle.py` + `oracle_util.py`
(definitions, call sites, `$var` uses, proc bindings), `probe_core.py` (repo tables,
plausible-namespace rules), `phases.py` (A/B/C/D/G), `phases2.py` (E/F/H), `probe.py`
(driver), `summarize.py`, `inspect_rows.py`, `counts.py`, `compare.py`, `fetch_corpus.sh`.

## Caps applied

| what | cap | effect in this run |
|---|---|---|
| C call probes per (file, name) | 20 | `call_probes_capped` per repo (TclProDebug and speedtables hit it) |
| C call probes per file | 600 | included in the above |
| D `$obj` probes per file | 300 | `obj_usage.capped` |
| E var probes per (proc, name) | 3 | `var_probes_capped` per repo |
| E var probes per proc | 60 | included in the above |
| F references: distinct OK-resolved defs per repo | 300 (evenly spaced sample) | `refs_stats.defs_checked` |
| F call hierarchy per repo | first 30 of those defs | |
| H test files per repo | 2 (most unique library calls); ≤30 probes, ≤2 per name, ≤5 library defs for refs | |
| selectionRange per file | 5 positions (offset 0, middle, first 3 def names) | |
| VAR_OK rows stored | counts only, plus a 10-row sample | |

## Classes (beyond the obvious)

- `OK_SILENT`: a switch-arm pattern word with an empty goto-definition result. This is
  correct silence.
- `DATA_CONTEXT_RESOLVED`: a "call" the server resolved inside a brace the oracle treats as
  data. It is informational, not wrong: in this corpus most of these are `subst {...}`
  script templates that are evaluated later.
- `VAR_UNBOUND_EMPTY`: an empty result for a `$x` that has no textual binding in the
  enclosing proc (Itcl/TclOO ivars, dynamic `upvar`, undeclared globals). Silence is probably
  correct.
- `VAR_UNBOUND_RESOLVED`: the same kind of use, but the server resolved it (mostly Itcl
  ivars).
- SUSPECT_WRONG reasons for calls:
  - `switch-pattern-resolved-as-call`: a pattern word of a switch arm-list was resolved as a
    command.
  - `command-resolved-to-non-callable:<form>`: a command name resolved to a variable
    definition.
  - `name-mismatch`
  - `namespace-unreachable`: had 0 rows in this run.
- VAR_SUSPECT_WRONG reasons:
  - `outside-proc-unlinked`: the binding is outside the enclosing proc, with no
    `global`/`variable`/`upvar` link for that name, and it is not an ivar of the enclosing
    class.
  - `binding-after-use-no-loop`: an unqualified, unlinked local resolved to a binding that
    comes later in the source, with no enclosing loop.
  - `name-mismatch`

## Oracle design and limitations (what triage must keep in mind)

- The oracle is a real Tcl word parser, not line regex. It handles braces, quotes, `[...]`,
  `$var(...)`, comments only at command start, backslash-newline, and lenient `{a}b` words.
  It works in bytes (latin-1 decode) so offsets match the server.
- It recurses into every braced word. `roles.py` decides each brace's role:
  - known scripts: if/while/for/foreach/catch/try/switch/eval/uplevel/time/dict
    for|with|update, tcltest `-body/-setup/-cleanup` and the old-style body, namespace eval,
    proc, Itcl/snit/TclOO bodies;
  - expressions: `expr`, if/while/for conditions;
  - callbacks: `-command`/`-*command`/`-server`, after, fileevent, chan event, trace, bind,
    namespace code;
  - `unknown-trailing`: the last brace of an unknown command, which the server also treats as
    a script (tagged `conf=heuristic`);
  - data: braces of data builtins (`set`/`list`/`dict`/`subst`/…), non-trailing braces of
    unknown commands, foreach lists, and test `-result` (tagged `conf=data`).
- Every probe and def carries a `conf` value (script / heuristic / data) and a context chain.
  Filter on `conf=script` for the high-signal set. CANDIDATE_MISS rows with `conf=data` (434)
  have a high false-positive rate; many are option tables, DSL tables or test arguments.
- Known false-positive sources:
  - **Callback scripts** (`bind`, `after`, `-command`) actually run in the global namespace.
    The oracle uses the lexical namespace for its plausible-namespace check. This only affects
    reachability tags (`n_reachable`), not classification.
  - **Plausible-namespace rules**: current ns, global, `namespace import` patterns,
    `namespace path` entries, and class ancestors (Itcl `inherit`, TclOO `superclass`). Not
    modeled: `namespace unknown`, ensembles, `interp alias` targets, runtime renames. A
    `namespace-unreachable` SUSPECT_WRONG would need checking against these, but this run had
    none.
  - **Known names** = oracle callable defs ∪ `workspace/symbol` Function/Method entries. A
    probe exists only where a word in command position matches a known short name. Methods
    count only inside their own class chain, or when the call is qualified. Builtin-named user
    procs (`update`, `lassign`, `frame`, …) produce `builtin-name`-tagged probes. Real
    compatibility shims like `proc lassign` exist, so those rows are not wrong per se.
  - **`$obj method` probes**: the oracle does not type receivers, except for a
    `typed-from:<head>` tag when `set var [Head ...]` appears in the same proc. CANDIDATE_MISS
    there just means "exactly one method of that name exists in the workspace", which is not
    evidence that the receiver has that class.
  - **Var binding detection** is per proc body and textual. Bindings created by
    `uplevel`/`eval`/`dict with` keys/`trace`/`vwait` are not seen, so they appear as
    VAR_UNBOUND_EMPTY, not as a miss. `foreach`/`lassign` targets inside data braces are
    skipped.
  - `promoted-link-assign` and `qualified-set-in-proc` mirror the server's promotion rules
    (global, `variable`, `upvar #0` or a qualified target), so they are matched against
    documentSymbol.
  - **Def-miss matching** means: same line, and the last name segment equals the symbol name.
    `namespace eval` is matched against namespace nodes by FQ name. The server only creates a
    namespace node when that namespace has a child symbol, so empty-namespace misses (26) are
    expected.
  - **`.rvt` files**: text outside `<? ?>` is blanked. There are only 4 `.rvt` files
    (speedtables).
  - Switch patterns are only recorded for the braced arm-list form. Inline
    `switch $x a {..} b {..}` patterns are not probed.
  - **G (semantic tokens)** uses the first `semanticTokens/full` result. For yajl-tcl
    `airports.tcl` that request timed out, so its probes have `sem=null`.
- The spot-check below shows high-signal categories are mostly true positives. CANDIDATE_*
  counts are still upper bounds: confirm each root cause by reading the source.

## Spot-check log (rows read against corpus source) and residual false-positive rate

| # | category | row | verdict |
|---|---|---|---|
| 1 | CANDIDATE_MISS (relative-qualified) | prometheus-tcl/client_api.tcl:37 `prom::_new_metric_family` called from `::prom::counter`; def `::prom::_new_metric_family` | real: the server tries only `::prom::counter::prom::_new_metric_family`, with no global fallback for relative qualified names |
| 2 | CANDIDATE_MISS (relative-qualified) | TclProDebug/coverage.tcl:484 `gui::getCurrentState` from `::coverage` | real (same root cause; 2057 of 2910 misses) |
| 3 | VAR_SUSPECT_WRONG outside-proc | prometheus-tcl/counter.tcl:42 `$_value` in a TclOO method → `set _value` in gauge.tcl and histogram.tcl | real: TclOO method bodies are walked at namespace frame, so method locals become `::prom::_value` |
| 4 | VAR_SUSPECT_WRONG outside-proc | tsp/bench/langbench/cat.tcl:5 `$file` (a `tsp::proc` param) → `set file` in handcoded/cat-ext.tcl | real: the `tsp::proc` body is walked at namespace frame |
| 5 | VAR_SUSPECT_WRONG binding-after-use | tsp/lib/tclparser-1.4.1/tools/genStubs.tcl:279 `$args` (bound by `regexp` at 269) → `set args` at 291 | real: `regexp` vars are not modeled, so the server falls back to the earliest binding, which comes after the use |
| 6 | VAR_SUSPECT_WRONG (first run) | TclProDebug nub.tcl `$DbgNub(...)` after `global DbgNub` | **oracle false positive, fixed** (linked/qualified names exempted). Rate before the fix: ~85% in TclProDebug |
| 7 | SUSPECT_WRONG switch-pattern | piaware-gui/grid.tcl:476 `button_wired {` (pattern of `switch $button {...}`) → proc in indicators.tcl | real |
| 8 | SUSPECT_WRONG switch-pattern | TclProDebug/menu.tcl:816 `stop {` → `::stop` in demos/tutorials/pulse.tcl | real, and it crosses files |
| 9 | REFS_SUSPECT switch-pattern | speedtables/gentable.tcl:3549 `boolean {` listed as a reference of proc `boolean` | real |
| 10 | SUSPECT_WRONG cmd→variable | speedtables/ctables/tests/pgtcl_ctable.tcl:3 `key id` (CTable DSL) → `set key` in corruption-test.tcl | real: commands and variables share one FQ table |
| 11 | SUSPECT_WRONG cmd→variable | tclrmq/Channel.tcl:122 `::rmq::debug "..."` → proc `::rmq::debug` AND `set ::rmq::debug $debug` (Connection.tcl:152) | real |
| 12 | symbols_not_in_oracle | prometheus-tcl/counter.tcl:21 `set _value 0.0` reported as Variable `::prom::_value` | real server false-positive symbol (same root cause as #3) |
| 13 | CANDIDATE_VAR_MISS | TclProDebug/lib/cmdline/cmdline.tcl:120 `$argsList` inside switch arm `"--" {...}` | real: arms whose pattern is quoted are never walked (`scriptBodies` needs a bare head) |
| 14 | CANDIDATE_VAR_MISS in-expr | TclProDebug/demos/fac/fac.tcl:19 `if {$n <= 1}` | real: `$vars` inside braced expr/conditions are not references (3051 of the misses are tagged `in-expr`) |
| 15 | H test files | turtles/tests/functional/persistence_ev.test:13 `::turtles::persistence::ev::start` in `-setup {}` | real: even after didOpen, tcltest `-setup/-body/-cleanup` and old-style bodies are not walked (only the last braced word is) |
| 16 | def-miss (first run) | tcl-jira-api `promoted-link-assign` after a dynamic `upvar $v result` | **oracle false positive, fixed** (only static links promote) |
| 17 | known-name table (first run) | `proc-variable-link` forms counted as callables | **oracle bug, fixed** (exact form set) |
| 17b | symbols_not_in_oracle (first runs) | TclProDebug/demos/hiq/hiqState.tcl:52 `set board($col,$row) 1` after `global board` | **oracle bug, fixed** (subscripted targets with `$` in the index were not recognized as bindings/defs); TclProDebug "server FP symbols" dropped 116 -> 5 |
| 18 | REFS_MISSING | piaware/fa_adept_client.tcl:105 `logger` intra-class Itcl method call; goto-def resolves it, references omits it | real asymmetry (goto-def has the `methodInClass` path, references does not) |
| 19 | obj CANDIDATE_MISS | piaware/fa_piaware_config.tcl:420 `$metadata key` (`metadata` is an Itcl ivar) | correct silence, since the receiver is untyped. The class is only a candidate; see the caveat above |
| 20 | VAR_SUSPECT_WRONG name-mismatch | TclProDebug/lib/tcldebugger/nub.tcl:1674 `$ns` | **oracle false positive**: `upvar #0 DbgNub(scope) ns` at line 1669 is a static alias, so chasing to `::DbgNub` is correct, if noisy at ~160 targets |

Residual false-positive rate observed after the fixes. Row 20 shows the ~0% for
VAR_SUSPECT_WRONG was too low: those samples were small, so treat every rate here as a
rough guide.

| category | false-positive rate |
|---|---|
| SUSPECT_WRONG calls | ~0% (5/5 checked) |
| VAR_SUSPECT_WRONG | ~0% (3/3 checked), before row 20 |
| REFS_SUSPECT switch-pattern / text-mismatch | ~0% (2/2) |
| CANDIDATE_MISS `conf=script` | low (3/3 real) |
| CANDIDATE_MISS `conf=data` | high (not spot-checked; treat as noise) |
| obj CANDIDATE_MISS | high as "server should resolve" (untyped receivers) |
| `namespace export` def-misses | 100% by design (exports are not definitions) |
| `namespace eval` def-misses | mostly expected (empty namespaces get no node) |
| REFS_SUSPECT `not-oracle-command-position` | mixed: `info exists sel::selectCursor(...)` hits are refs of a *namespace variable* that shares a proc's name (another face of cmd/var collision) |

## Known gaps in the harness itself

- E probes only uses inside oracle-recognized proc/method/lambda bodies (including
  `tsp::proc` and Itcl/TclOO methods). **Namespace-level `$x` is not probed**, so a fix that
  loses those answers will not show up in these numbers.
- F checks only call sites that goto-def already resolved (OK rows) against `references`.
  Misses in both directions are invisible to it.
- `references` is requested with `includeDeclaration: false`.
- `semanticTokens` for the probe-time check comes from the phase-A request. Files are not
  re-requested after the H-phase didOpen.
- The yajl-tcl run includes 4 restarts (2 phase-A timeouts plus 2 `$::airport(...)` var probes
  that timed out), each re-indexing that repo.
- Result JSON keeps symbol counts and samples, not every symbol name.
