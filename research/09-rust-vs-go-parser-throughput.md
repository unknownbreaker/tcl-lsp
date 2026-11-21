# Rust vs Go parser throughput

**Date:** 2026-11-21 · **Issue:** #80 · **Code:** `main@d647306`

## Why this exists

A Rust port of the server was being weighed on a guess: 1.5–3× for a hand-written
parser. A port means rewriting about 6,700 lines of server code and 16,600 lines of
tests. This experiment replaces the guess with a measurement. It ports only the
scanner and the command/word parser, proves that the port parses exactly as Go does,
and times both on the same corpus.

**Answer:** Rust parses about 2–2.6× faster than Go on the whole corpus and about
1.5–2.2× faster on the largest file. Parsing is at most about 45% of indexing, so the
same port would make indexing about 1.2–1.4× faster. A typical edit would save about
0.025 ms, which is invisible beside Neovim's 150 ms `didChange` debounce.
**Recommendation: don't port.** Leave the parser as it is. If a large file's latency
ever matters, optimize the Go first (see "Recommendation").

## How to read the evidence

- **Measured:** timed on the corpus below, with the machine load stated.
- **Profile:** a Go CPU profile, taken under load, so its percentages are distorted.
- **Estimate:** arithmetic on the two above (Amdahl's law), not a measurement.

**Every timing here was taken on a saturated machine.** Other workloads held the load
average at 350–770 on 10 cores, with 0% idle CPU, through both sessions. No quiet
window came: a 40-minute wait for load below 20 timed out. A single 20-pass round
varied up to 10× from the next, so the numbers below are medians over 15 interleaved
rounds, and the **ratios** carry the result: Go and Rust alternated round by round,
so they ran under the same load. Absolute MB/s are low and noisy; the issue's quiet
Go baseline (268 MB/s for scanner plus `Parse`) is a better absolute figure.

## Method

- **Machine:** Apple M1 Max, 10 cores, 64 GB, macOS 27.0.1. **Toolchains:** go1.26.4,
  rustc 1.97.1, both optimized (Go's defaults; Cargo `--release`).
- **Corpus:** Homebrew's Tcl script library (`/opt/homebrew/opt/tcl-tk/lib`): 887
  `.tcl` files, 11,457,776 bytes; at the top level, 16,396 commands and about 67k
  words. The largest file is `tcllib2.0/fumagic/filetypes.tcl` (1,320,164 bytes).
  Both sides load every file into memory before timing.
- **Go:** `BenchmarkCorpus` in `server/internal/tcl/parsedump_test.go`. Stages:
  `scan` (`tcl.Scan`), `words` (scan grouped into commands as written, the same work
  as the Rust port), `parse` (`tcl.Parse`, which adds the head rewrite) and `index`
  (`source.IndexUnit`). Each runs over the whole corpus (`all`) and its largest file
  (`largest`).
- **Rust:** `research/experiments/09_rust_parse/`, a crate with no dependencies. Its
  `bench` binary runs `scan` and `words` with a counting allocator. Each round is one
  warm-up and 20 timed passes, and it reports the median pass.
- **Rounds:** 15, interleaved: Go `main` (`-benchtime 20x`), then the Go allocation
  experiment, then Rust. After the rounds, Go `index` ran 5 times (`-count 5`).
  Load: 597 at the start, 768 at the end.
- **Equivalence:** a timing counts only if both sides parse identically.
  `compare.sh` checks the golden dumps, then a seeded fuzz set (every prefix of every
  fixture plus 4,000 random files), then the corpus. The Go and Rust dumps were
  byte-identical on all of them: 9 goldens, 4,713 fuzz files, 887 corpus files,
  406 files from research 08's public corpus, and about 140k hostile inputs from an
  adversarial tester. A planted Rust bug made `compare.sh` fail, naming the file,
  command and word.

The dump format, the run commands and the port's limits are in the
[crate README](experiments/09_rust_parse/README.md#dump-format).

## Observed output

**Measured.** Median MB/s over 15 rounds (1 MB = 10⁶ bytes), with allocations per
pass:

| Stage | Go median | Rust median | Ratio (medians) | Ratio (per-round median, range) | Go allocs / bytes | Rust allocs / bytes |
| --- | ---: | ---: | ---: | --- | ---: | ---: |
| scan, all | 306 | 762 | 2.49× | 2.67× (0.30–8.5) | 7,160 / 24.4 MB | 5,513 / 14.3 MB |
| words, all | 181 | 473 | 2.62× | 2.54× (0.79–7.9) | 60,833 / 31.0 MB | 21,313 / 4.6 MB |
| scan, largest | 653 | 999 | 1.53× | 1.52× (0.84–11.8) | 8 / 12 KB | 6 / 6 KB |
| words, largest | 453 | 1,008 | 2.23× | 2.21× (0.69–13.5) | 30 / 14 KB | 9 / 1.6 KB |

Go `parse` (words plus the head rewrite) ran at a median of 179 MB/s on the whole
corpus and 553 MB/s on the largest file. Against it, Rust `words` is 2.64× and 1.82×.

The best single passes tell the same story: Rust reached about 1,000 MB/s on both
sets, and Go's best 20-pass round reached 438 (all) and 703 (largest). The Rust best
is a single pass and the Go best is a 20-pass mean, so best-versus-best (2.3× and
1.5×) understates Go's noise and isn't a like-for-like ratio. Quote the medians.

**Why the largest file gains less.** `filetypes.tcl` has only 6 top-level commands.
Its parse is one long braced word, a byte loop in both languages, and both allocate
almost nothing. Most of the whole-corpus gain comes from allocation: Go makes 2.9×
the allocations and 6.7× the bytes that Rust does, because Go builds a `[]Word` per
command and copies tokens into them, while Rust borrows slices of the source.

**Indexing.** **Measured:** `index/all` ran at a median of 4 MB/s under this load,
against the issue's quiet 42 MB/s, so only its allocation counts are usable:
2,187,874 allocations and 571 MB per corpus pass, and 443,072 allocations and
112 MB for the 1.3 MB largest file.

**Profile:** a CPU profile of `index/all`, taken under load. `parseWith` (scanner and
parser, called again for every nested body) accounts for 20% of samples and 45% of
`FileAll`. The profile is distorted: `runtime.madvise` takes 23% of all samples,
from the kernel's memory pressure, not the code. Two other costs stand out:
`globalHead`'s `strings.ContainsAny` at about 11% and `growslice` at about 10%.

## Go allocation experiment

**Measured.** A patch to `parseWith`, never committed: count the words and
separators in the token list first, then allocate one flat `[]Word` for the file and
an exactly sized `[]Command`, and slice each command's words out of the flat array.

| | Go `main` | Flat words | Change |
| --- | ---: | ---: | ---: |
| parse, all: median MB/s | 179 | 246 | 1.37× |
| parse, all: allocs per pass | 60,846 | 8,932 | −85% |
| parse, largest: median MB/s | 553 | 528 | 0.95× (noise) |

It is about a dozen lines. Rust `words` is still 1.9× the flat-words Go on the
whole corpus (473 against 246 MB/s), so the patch closes about a quarter of the
MB/s gap.

**A pitfall to record:** a first version also gave the scanner's token slice a
capacity hint of `len(src)/8`. That broke `TestReachingScalesLinearlyInDepth`.
Nested bodies are re-parsed, and each re-parse preallocated against its whole span,
so a depth-n nest did O(n²) work. Any capacity hint in the scanner has to be safe
under re-parse.

## Implication: editor latency

**Estimate.** Porting only the parser speeds up only the parser's share of
indexing. With that share `p` and a parser speedup `s`, indexing takes
`(1 − p) + p/s` of today's time. Here `p` is at most 0.45 (the profile, taken
under load), `s` is 2.6 for typical files and 1.5–2.2 for the largest. The
baselines are the issue's quiet Go figures.

| Case | Today (Go) | Rust parser (estimate) | Saved | Flat-words Go (estimate) |
| --- | ---: | ---: | ---: | ---: |
| Typical edit, p50 | 0.09 ms | 0.065 ms | 0.025 ms | 0.079 ms |
| p99 edit | 8.3 ms | 6.0 ms | 2.3 ms | 7.3 ms |
| Worst file (1.3 MB) | 85 ms | 64–72 ms | 13–21 ms | about 85 ms |
| Cold index, whole corpus | 240 ms | 174 ms | 66 ms | 211 ms |

What a user waits for is mostly not parsing. Neovim batches `didChange` for 150 ms by
default (`debounce_text_changes`). Each answer then crosses JSON-RPC, and the
client redraws semantic tokens and highlights. A typical edit's 0.09 ms is under
0.1% of that path. The worst file and the cold index are the only cases where the
saving is a noticeable fraction. A cold index happens once per session, and it
already runs across all cores.

The estimate is an upper bound on the gain. `p` = 0.45 comes from a profile in which
allocation was inflated by memory pressure. A full port would also speed up the
indexer, which this experiment didn't measure, but that is the 6,700-line rewrite the
experiment was meant to price.

## Recommendation

**Don't port.** Leave the parser as it is: the measured gain (about 2.6× on parsing,
about 1.4× at most on indexing) is real, but it matches the 1.5–3× guess and is
invisible on a typical edit. A port would buy 13–21 ms on the worst file at the cost
of rewriting the server and its tests.

If a large file's latency ever becomes a complaint, optimize the Go instead, in this
order. Each step is small and testable against the golden dumps:

1. The flat `[]Word` in `parseWith`: 1.37× on parsing, 85% fewer parse allocations.
2. `globalHead`'s `strings.ContainsAny`: about 11% of indexing in the profile.
3. Indexing's allocations: 571 MB for 11.5 MB of source. Profile it on a quiet
   machine before choosing a target.

Before acting on any of these, rerun `BenchmarkCorpus` and the profile on a quiet
machine. Every percentage above was taken under load.

## What stays in the repo

- `server/internal/tcl/parsedump_test.go`: the dump, its golden test, a guard that the
  dump's grouping matches `tcl.Parse`, and `BenchmarkCorpus`, which is the
  throughput regression guard. All of it skips without a corpus.
- `server/internal/tcl/testdata/parsedump/`: hostile fixtures and their goldens, the
  start of a language-neutral golden corpus. A future parser, such as an expression
  parser, can be checked the same way.
- `research/experiments/09_rust_parse/`: the throwaway Rust port and `compare.sh`.

## Found along the way

- **Go stack overflow (pre-existing).** 1 MB of unterminated `[` (also `"[`, `[x `,
  `[{` repeated) kills the Go process with `fatal error: stack overflow`, at Go's 1 GB
  goroutine stack limit. The scanner recurses (`depthScan` → `spanMemo`), and the
  LSP server would die on such a file too. The Rust port, on a 1 GiB thread stack,
  survives it. Balanced input of the same size passes. Filed as a `proposed` issue.
- **Backslash-newline in a bare word (pre-existing).** In `set a b\<newline>  c`,
  the word `b` spans `b\<newline>`, while Tcl (8.6 and 9.0) treats the continuation
  as a word separator. Word counts are right. So `proc foo\<newline> {} {…}`
  defines `foo` in Tcl, but the server sees a name with a backslash and stays
  silent: a miss, not a wrong answer. Filed as a `proposed` issue.
