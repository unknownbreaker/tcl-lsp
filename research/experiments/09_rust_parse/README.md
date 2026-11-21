# 09: Rust port of the scanner and parser

A throwaway prototype for [research/09](../../09-rust-vs-go-parser-throughput.md).
It answers one question: how much faster is the scanner and command/word parser in
Rust? Nothing else is ported.

- `src/lib.rs`: the port. `parse(&[u8])` returns commands of words that borrow
  slices of the source. It mirrors `server/internal/tcl/scanner.go`, `parser.go`,
  and the span helpers they call with a nil memo (`scriptclose.go`,
  `scriptends.go`, `bracesHideBrackets`), minus Parse's head rewrite
  (`bareModeledHead`), which is analysis, not parsing.
- `src/bin/parsedump.rs`: writes the shared parse dump for every `*.tcl` file
  under a directory.
- `src/bin/bench.rs`: throughput and allocations per corpus pass.
- `compare.sh`: proves both sides parse identically. It fails on the first file
  whose dumps differ, naming the file, command and word. When either dump tool
  crashes, it names the first file left without a dump and shows the tool's
  output. Its dumps and logs go in a `$TMPDIR/parsedump.*` directory. The dumps
  hold the corpus source, so the directory is deleted on success and kept on
  failure; `KEEP_DUMPS=1` keeps it either way.

No dependencies: the dump is hand-written (its string escaping must match Go's
byte for byte), and the benchmark is a timed loop with a counting allocator.

## Run

```sh
# Equivalence: golden fixtures, seeded fuzz (every fixture prefix and 4,000
# random files), then the corpus. ROOT defaults to $TCL_CORPUS, else
# /opt/homebrew/opt/tcl-tk/lib; it may be relative or a symlink. A missing ROOT,
# or one with no .tcl files, skips the corpus step; a ROOT that is a file is an
# error.
./compare.sh [ROOT]

# Throughput. Rust: median of $BENCH_PASSES passes (default 20, at least 1).
cargo run --release --bin bench
# Go: the same stages, plus full indexing. One pass per count, so the median of
# the counts matches Rust's median pass (research/09 "Method").
go test -C ../../../server -run '^$' -bench 'Corpus/(scan|words|parse)/' -benchmem \
  -benchtime 1x -count 20 ./internal/tcl
go test -C ../../../server -run '^$' -bench 'Corpus/index/' -benchmem -count 5 ./internal/tcl
```

Both benchmarks read `TCL_CORPUS` and load every file into memory before timing.
To measure private code, point `TCL_CORPUS` at it and record only the aggregate
numbers. Never commit the dumps or a file list.

## Dump format

The shared dump format is specified in
[research/09](../../09-rust-vs-go-parser-throughput.md#dump-format). The Go side
is `server/internal/tcl/parsedump_test.go`, with its golden fixtures in
`server/internal/tcl/testdata/parsedump/`.

## Limits

- Like the Go code, the port recurses on nested braces and brackets, so the
  binaries run on a 1 GiB thread stack. A production port would need an explicit
  stack. Go's goroutine stack also stops at 1 GB, and 1 MB of unterminated `[`
  overflows it (`fatal error: stack overflow`), while the Rust port survives it.
  That is a pre-existing Go bug outside this experiment (#154), so
  `compare.sh` reports such a file as a Go crash.
- The port has no `scriptEnds` memo. Go's `Parse` passes a nil memo too, so the
  two do the same work. Indexing uses the memo for deeply nested shapes, and this
  prototype doesn't model that.
- The fuzz generator uses the system `awk`'s seeded `rand()`. It reproduces on one
  machine, but another `awk` may generate different files.
