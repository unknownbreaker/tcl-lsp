# TCL/RVT Scope Semantics Research

The goal of this research is to **map TCL and RVT name-resolution behavior
precisely enough to design reliable goto-definition and goto-reference** before
writing any implementation. (See `../CLAUDE.md` for why the project was reset to
a research-first approach.)

## Method

- Every claim is **verified empirically** with `tclsh`, not asserted from memory.
- Experiment scripts live in `experiments/` and are runnable: `tclsh experiments/NN_name.tcl`.
- Findings documents quote the observed output and draw out the implication for a
  static resolver (an LSP cannot `eval`; it must replicate TCL's resolution rules
  by static analysis).

## Environment / target version

- **Target: TCL 8.6** (the Rivet/RVT baseline). Run all experiments with:
  `/opt/homebrew/opt/tcl-tk@8/bin/tclsh experiments/NN_name.tcl`
  (installed via `brew install tcl-tk@8`, version 8.6.18, keg-only).
- A 9.0.3 `tclsh` is also present (the default on PATH) and is used as a
  cross-check. Topic 01 was verified **identical** on 8.6.18 and 9.0.3.
- When a behavior differs between 8.6 and 9.0, the findings doc must call it out.

## Findings index

| # | Topic | Status |
|---|-------|--------|
| 01 | [Variable scope & resolution](01-variable-scope.md) | ✅ verified on 8.6 + 9.0 |
| 02 | [Namespace name resolution (commands vs variables)](02-namespace-resolution.md) | ✅ verified on 8.6 + 9.0 |
| 03 | [Proc / command definition & call-site resolution](03-proc-resolution.md) | ✅ verified on 8.6 + 9.0 |
| 04 | [`source` / `package` / multi-file resolution](04-source-multifile.md) | ✅ verified on 8.6 + 9.0 |
| 05 | [RVT (Rivet template) scope implications](05-rvt-scope.md) | ✅ scope sim-verified + Rivet specifics resolved from docs (only FlightAware-local items pending) |
| 06 | [Synthesis → resolver model, architecture, v2 scope](06-synthesis.md) | ✅ complete (bridge to planning) |
| 07 | [Real-world TCL/RVT idiom survey & Itcl gap report](07-realworld-itcl-survey.md) | ✅ gaps fixed; remaining gaps listed |
| 08 | [FlightAware corpus probe: wrong answers, slowdown, misses](08-flightaware-corpus-probe.md) | ✅ findings + fix design; harness in `experiments/08_corpus_probe/` |

**Research status:** core mapping complete and empirically grounded. Remaining
unknowns are FlightAware-local (Rivet version, a real `.rvt`, house conventions)
and a few cheap language experiments deferred to the build. Ready for the plan
phase (goto-definition + goto-reference).

## Why this order

goto-definition and goto-reference hinge most on **name resolution** — given a
symbol at a cursor, which declaration does it bind to? Variables (01) and
namespaces (02) are the foundation; the command-vs-variable asymmetry surfaced in
01 is the single biggest source of TCL's "wacky" reputation.
