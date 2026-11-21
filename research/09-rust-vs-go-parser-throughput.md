# Rust vs Go parser throughput

**Date:** 2026-11-21 · **Issue:** #80 · **Code:** `main@d647306`

## Why this exists

A Rust port of the server was being weighed on a guess: 1.5–3× for a hand-written
parser. A port means rewriting about 6,700 lines of server code and 16,600 lines of
tests. This experiment replaces the guess with a measurement. It ports only the
scanner and the command/word parser, proves that the port parses exactly as Go does,
and times both on the same corpus.

**Answer:** Rust parses about 2.1× faster than Go's `tcl.Parse` on the whole corpus
and about 2.0× faster on the largest file (medians; per-round ratios ranged from
1.1× to 4.3× and from 0.8× to 3.9×). Parsing is at most about 45% of indexing, so the
same port would make indexing about 1.3× faster. A typical edit would save about
0.02 ms, which is invisible beside Neovim's 150 ms `didChange` debounce.
**Recommendation: don't port.** Leave the parser as it is. If a large file's latency
ever matters, optimize the Go first (see "Recommendation").

## How to read the evidence

- **Measured:** timed on the corpus below, with the machine load stated.
- **Profile:** a Go CPU profile, taken under load, so its percentages are distorted.
- **Estimate:** arithmetic on the two above (Amdahl's law), not a measurement.

**Every timing here was taken on a saturated machine.** Other workloads kept the CPU
at 0.3% idle on 10 cores. The load average was 57 when the timed rounds started and
167 when they ended. No quiet window came: an earlier 40-minute wait for a load
below 20 timed out. A round's median moved by up to 3× from one round to the next,
and so did the Go/Rust ratio: per-round ratios for the headline pair ran from 1.1×
to 4.3×. Go and Rust alternated round by round, which keeps a slow stretch from
landing on one side only, but it doesn't make the ratio exact. Read each ratio as
"about 2×", not to two decimals. Absolute MB/s are low and noisy. The issue's Go
baseline (268 MB/s) came from a different, throwaway harness, so it isn't comparable
either.

The conclusion doesn't need a tighter ratio. Even at the top of the per-round range
(4.3×), indexing would get about 1.5× faster and a typical edit would save about
0.03 ms (see "Implication: editor latency").

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
  `bench` binary runs `scan` and `words` with a counting allocator.
- **Rounds:** 15, interleaved: Go `main`, then the Go allocation experiment, then
  Rust. In each round, each stage ran 20 single passes on each side, and the round's
  figure is the median pass. Go ran with `-benchtime 1x -count 20`, so each count is
  one pass. Rust ran one warm-up pass and then 20 timed passes. The tables give the
  median of the 15 round medians. After the rounds, Go `index` ran 5 times
  (`-count 5`).
- **The headline pair** is Rust `words` against Go `parse`. `tcl.Parse` is what the
  server runs, and #80 scoped the comparison to the scanner and `Parse`. The head
  rewrite it adds over `words` is too small to measure here: under this load Go's
  `words` median even came out below its `parse` median on the whole corpus. Treat
  stage-to-stage differences under about 15% as noise.
- **Equivalence:** a timing counts only if both sides parse identically.
  `compare.sh` checks the golden dumps, then a seeded fuzz set (every prefix of every
  fixture plus 4,000 random files), then the corpus. The Go and Rust dumps were
  byte-identical on all of them: 9 goldens, 4,713 fuzz files, 887 corpus files,
  406 files from research 08's public corpus, and about 140k hostile inputs from an
  adversarial tester. A planted Rust bug made `compare.sh` fail, naming the file,
  command and word.

The run commands and the port's limits are in the
[crate README](experiments/09_rust_parse/README.md). The dump format is below.

## Dump format

Both sides print this for any Tcl source. It is meant to become the project's
language-neutral golden corpus: a future parser, such as an expression parser, can
be checked against the fixtures the same way.

One JSON document per source file. Each command is on its own line, as an array of
its words in order. Each word is `{"kind":…,"text":…,"start":…,"end":…}`, keys in
that order, with no other whitespace:

```
[
[{"kind":"bare","text":"set","start":0,"end":3},{"kind":"braced","text":"{a b}","start":4,"end":9}],
[{"kind":"quoted","text":"\"x\"","start":10,"end":13}]
]
```

- `kind` is `bare`, `braced` or `quoted`, from the word's first byte.
- `text` is the word's raw source text, as written: Parse's head rewrite
  (`bareModeledHead`) is analysis, not parsing, and isn't applied. `"` and `\` are
  escaped. Bytes below 0x20, 0x7f, and each byte outside a valid UTF-8 sequence are
  written as `\u00xx`. Valid UTF-8 is written unescaped. So a JSON decoder reads
  the invalid byte 0xff and the character U+00FF the same way. Compare dumps as
  bytes, where the two differ (`ÿ` against `ÿ`); a decoded dump is lossy.
- `start`/`end` are a half-open UTF-8 byte range into the source.
- Comments and empty commands are omitted. A file with no commands is `[]`.

The Go writer is `parseDump` in `server/internal/tcl/parsedump_test.go`, and
`TestSourceCommandsMatchParse` checks that its grouping matches `tcl.Parse`. The
golden fixtures are in `server/internal/tcl/testdata/parsedump/`, stored byte-exact
(`.gitattributes`: `-text`).

## Observed output

**Measured.** Median MB/s over 15 rounds (1 MB = 10⁶ bytes), with allocations per
pass:

| Stage | Go median | Rust median | Ratio (medians) | Ratio (per-round median, range) | Go allocs / bytes | Rust allocs / bytes |
| --- | ---: | ---: | ---: | --- | ---: | ---: |
| scan, all | 526 | 725 | 1.38× | 1.33× (0.72–2.41) | 7,157 / 24.4 MB | 5,513 / 14.3 MB |
| words, all | 297 | 718 | 2.42× | 2.18× (1.17–4.37) | 60,831 / 31.0 MB | 21,313 / 4.6 MB |
| scan, largest | 582 | 910 | 1.56× | 1.58× (0.98–2.44) | 8 / 12 KB | 6 / 6 KB |
| words, largest | 576 | 943 | 1.64× | 1.51× (0.81–2.02) | 30 / 14 KB | 9 / 1.6 KB |

**The headline pair:** Go `parse` ran at a median of 339 MB/s on the whole corpus
and 464 MB/s on the largest file. Against it, Rust `words` is 2.12× (per-round
median 1.96×, range 1.06–4.26) and 2.03× (per-round median 1.77×, range 0.79–3.94).

**Best single passes.** Rust reached 1,016 MB/s on the whole corpus and 1,055 on the
largest file. Go `parse` reached 602 and 749. Both are single passes, so this is a
like-for-like pair: 1.69× and 1.41×. It is lower than the median ratio, so Go may
lose more to the load than Rust does (its garbage collector needs CPU time the
machine didn't have). On a quiet machine, the ratio may be nearer the best-pass
figures than the medians.

**An earlier run is superseded.** A first run, under a load of 350–770, compared
Go's *mean* of 20 passes (`-benchtime 20x`) with Rust's *median* of 20. Slow
passes pull a mean up, so under that load the comparison favored Rust, and it read
2.6×. Every figure here comes from the matched rerun.

**Why the largest file gains less.** `filetypes.tcl` has only 6 top-level commands.
Its parse is mostly one long braced word, a byte loop in both languages, and both
sides allocate almost nothing.

**Where the whole-corpus gain comes from: allocation.** Go makes 2.9× the
allocations and 6.7× the bytes that Rust does. Neither side copies text: Go's
`Word.Text` is a substring of the source, just as Rust's `text` borrows a slice of
it. The gap has two causes:

- **Go builds the whole token list first.** `tcl.Parse` calls `Scan`, which returns
  a `[]Token` for the file (40 bytes a token), and only then groups it into
  commands. That list is 24.4 of the 31.0 MB that Go's `words` allocates per corpus
  pass. Rust's `parse` pulls tokens from the scanner one at a time and never builds
  the list.
- **Go grows each command from nothing.** Each command's `[]Word` starts nil and is
  appended to, so it is allocated at 1, 2 and 4 words: 3 allocations for a 4-word
  command. Rust's first `push` reserves room for 4 words, so most commands take one
  allocation. That accounts for most of Go's 53,674 grouping allocations (`words`
  less `scan`, about 3.3 for each of the 16,396 commands), against the 21,313 of
  Rust's whole `words` pass.

**Indexing.** **Measured:** `index/all` ran at a median of 8.3 MB/s under this load,
against the issue's 42 MB/s, so only its allocation counts are usable: 2,187,857
allocations and 571 MB per corpus pass, and 443,075 allocations and 112 MB for the
1.3 MB largest file.

**Profile:** a CPU profile of `index/all`, taken in the first run under a load of
350–770. `parseWith` (scanner and parser, called again for every nested body)
accounts for 20% of samples and 45% of `FileAll`. The profile is distorted:
`runtime.madvise` takes 23% of all samples, from the kernel's memory pressure, not
the code. Two other costs stand out: `globalHead`'s `strings.ContainsAny` at about
11% and `growslice` at about 10%.

## Go allocation experiment

**Measured.** A patch to `parseWith`, never committed: count the words and
separators in the token list first, then allocate one flat `[]Word` for the file and
an exactly sized `[]Command`, and slice each command's words out of the flat array.
It is about a dozen lines.

| | Go `main` | Flat words | Change |
| --- | ---: | ---: | ---: |
| parse, all: median MB/s | 339 | 394 | 1.16× (per-round median 1.07×, range 0.71–2.09) |
| parse, all: allocs / bytes per pass | 60,846 / 31.3 MB | 8,931 / 32.0 MB | −85% / +2% |
| parse, largest: median MB/s | 464 | 478 | 1.03× (noise) |

It removes 85% of the allocations but none of the bytes: the token list is still
built, and the flat array holds every word. The speedup is within the noise. The
first run, under heavier load, put it at 1.37×. Rust `words` is 1.8× the flat-words
Go on the whole corpus (718 against 394 MB/s).

**A pitfall to record:** a first version also gave the scanner's token slice a
capacity hint of `len(src)/8`. That broke `TestReachingScalesLinearlyInDepth`.
Nested bodies are re-parsed, and each re-parse preallocated against its whole span,
so a depth-n nest did O(n²) work. Any capacity hint in the scanner has to be safe
under re-parse.

## Implication: editor latency

**Estimate.** Porting only the parser speeds up only the parser's share of
indexing. With that share `p` and a parser speedup `s`, indexing takes
`(1 − p) + p/s` of today's time. Here `p` is at most 0.45 (the profile, taken
under load). `s` is Rust `words` against Go `parse`: 2.1 on the whole corpus,
which stands for typical files and the cold index, and 1.8–2.0 on the largest file
(per-round and overall median). The baselines are the issue's Go figures.

| Case | Today (Go) | Rust parser (estimate) | Saved | Flat-words Go (estimate) |
| --- | ---: | ---: | ---: | ---: |
| Typical edit, p50 | 0.09 ms | 0.069 ms | 0.021 ms | 0.084 ms |
| p99 edit | 8.3 ms | 6.3 ms | 2.0 ms | 7.8 ms |
| Worst file (1.3 MB) | 85 ms | 66–68 ms | 17–19 ms | about 85 ms |
| Cold index, whole corpus | 240 ms | 183 ms | 57 ms | 225 ms |

At the top of the per-round range (`s` = 4.3, and 3.9 on the largest file), the
typical edit would save 0.031 ms, the worst file 29 ms and the cold index 83 ms.

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

**Don't port.** Leave the parser as it is. The measured gain (about 2× on parsing,
about 1.3× at most on indexing) is real and sits inside the 1.5–3× guess, but it is
invisible on a typical edit. A port would buy 17–19 ms on the worst file at the cost
of rewriting the server and its tests.

If a large file's latency ever becomes a complaint, optimize the Go instead, in this
order. Each step is small and testable against the golden dumps:

1. **Don't build the token list** (not measured): let `parseWith` take tokens from
   the scanner as they are found, as the Rust port does. The list is 24.4 of the
   31.0 MB that parsing allocates per corpus pass. Mind the re-parse pitfall above.
2. **`globalHead`'s `strings.ContainsAny`:** about 11% of indexing in the profile.
3. **Indexing's allocations:** 571 MB for 11.5 MB of source. Profile it on a quiet
   machine before choosing a target.

The flat `[]Word` patch alone isn't worth it: it cut the allocations but not the
time, within this noise.

Before acting on any of these, rerun `BenchmarkCorpus` and the profile on a quiet
machine. Every percentage above was taken under load.

## What stays in the repo

- `server/internal/tcl/parsedump_test.go`: the dump, its golden test, a guard that the
  dump's grouping matches `tcl.Parse`, and `BenchmarkCorpus`, which is the
  throughput regression guard. All of it skips without a corpus.
- `server/internal/tcl/testdata/parsedump/`: hostile fixtures and their goldens, the
  start of a language-neutral golden corpus.
- `research/experiments/09_rust_parse/`: the throwaway Rust port and `compare.sh`.

## Found along the way

- **Go stack overflow (pre-existing), #154.** 1 MB of unterminated `[` (also
  `"[`, `[x `, `[{` repeated) kills the Go process with `fatal error: stack
  overflow`, at Go's 1 GB goroutine stack limit. The scanner recurses (`depthScan`
  → `spanMemo`), and the LSP server would die on such a file too. The Rust port, on a
  1 GiB thread stack, survives it. Balanced input of the same size passes.
- **Backslash-newline in a bare word (pre-existing), #155.** In
  `set a b\<newline>  c`, the word `b` spans `b\<newline>`, while Tcl (8.6 and 9.0)
  treats the continuation as a word separator. Word counts are right. So
  `proc foo\<newline> {} {…}` defines `foo` in Tcl, but the server indexes a proc
  named `foo\<newline>`: workspace symbols show a name Tcl never defines, and a call
  to `foo` doesn't resolve.
