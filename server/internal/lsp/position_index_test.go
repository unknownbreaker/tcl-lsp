package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
)

// scanPosition is the original LSPPosition loop, copied verbatim. LSPPosition now
// shares the line index, so it cannot serve as the reference.
func scanPosition(src string, offset int) (line, character int) {
	if offset > len(src) {
		offset = len(src)
	}
	lineStart := 0
	for i := 0; i < offset; i++ {
		if src[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	for i := lineStart; i < offset; {
		r, size := utf8.DecodeRuneInString(src[i:])
		character += utf16.RuneLen(r)
		i += size
	}
	return line, character
}

func TestLineIndexMatchesScan(t *testing.T) {
	fixtures := []string{"", "\n", "a", "a\n", "\n\n", "set x 1\nputs $x", "x😀y\né!\n",
		"a\r\nb\r\n", "\xff\xfe\nz\xc3", "😀", "no newline at end"}
	for _, src := range fixtures {
		index := newLineIndex(src)
		for offset := -1; offset <= len(src)+2; offset++ { // past-end clamp, mid-rune, negative
			wantLine, wantCharacter := scanPosition(src, offset)
			line, character := index.lineChar(offset)
			if line != wantLine || character != wantCharacter {
				t.Errorf("lineChar(%q, %d) = (%d, %d), want (%d, %d)",
					src, offset, line, character, wantLine, wantCharacter)
			}
		}
	}
}

// TestLargeFileRequestsStayFast bounds each request a per-call rescan makes
// quadratic. NewServer(nil) is safe because these paths never touch the connection.
func TestLargeFileRequestsStayFast(t *testing.T) {
	const (
		maximumRequestTime = 2 * time.Second
		largeFileURI       = "file:///big.tcl"
		usingFileURI       = "file:///use.tcl"
	)
	largeFileSource := largeArrayFileSource()
	server := NewServer(nil)
	server.setDoc(largeFileURI, largeFileSource)
	server.setDoc(usingFileURI, "puts $::a($k)\n")

	// Each request returns its result size so a request that answers nothing
	// cannot pass the time bound by doing no work.
	requests := []struct {
		name       string
		resultSize func() int
	}{
		{"documentSymbol", func() int {
			definitions := source.Defs(uriToPath(largeFileURI), largeFileSource)
			return len(buildDocumentSymbols(definitions, largeFileSource, false))
		}},
		{"semanticTokens", func() int {
			params := SemanticTokensParams{TextDocument: TextDocumentIdentifier{URI: largeFileURI}}
			return len(server.semanticTokens(params).Data)
		}},
		{"definition", func() int {
			// Character 8 is the `a` of `$::a` in `puts $::a($k)`.
			return len(server.handleDefinition(TextDocumentPositionParams{
				TextDocument: TextDocumentIdentifier{URI: usingFileURI},
				Position:     Position{Line: 0, Character: 8},
			}))
		}},
	}
	for _, request := range requests {
		t.Run(request.name, func(t *testing.T) {
			start := time.Now()
			size := request.resultSize()
			elapsed := time.Since(start)
			if size == 0 {
				t.Errorf("empty result")
			}
			if elapsed >= maximumRequestTime {
				t.Errorf("took %v, want under %v", elapsed, maximumRequestTime)
			}
		})
	}
}

// TestPositionsFollowLiveEdits guards against a line index outliving its request: a
// stale one would keep reporting pre-edit lines after didChange.
func TestPositionsFollowLiveEdits(t *testing.T) {
	const (
		documentURI        = "file:///live.tcl"
		originalText       = "proc greet {} {}\n"
		prependedLineCount = 2
	)
	document := TextDocumentIdentifier{URI: documentURI}

	var input bytes.Buffer
	input.Write(frame(t, "initialize", 1, InitializeParams{}))
	input.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: documentURI, Text: originalText}}))
	input.Write(frame(t, "textDocument/documentSymbol", 2, DocumentSymbolParams{TextDocument: document}))
	input.Write(frame(t, "textDocument/didChange", nil, DidChangeParams{
		TextDocument:   document,
		ContentChanges: []TextDocumentContentChangeEvent{{Text: "\n\n" + originalText}},
	}))
	input.Write(frame(t, "textDocument/documentSymbol", 3, DocumentSymbolParams{TextDocument: document}))
	input.Write(frame(t, "exit", nil, nil))
	responses := runServer(t, input.Bytes())

	lineBeforeEdit := soleSymbolLine(t, responses, "2")
	lineAfterEdit := soleSymbolLine(t, responses, "3")
	if lineAfterEdit != lineBeforeEdit+prependedLineCount {
		t.Errorf("symbol line = %d after edit, want %d", lineAfterEdit, lineBeforeEdit+prependedLineCount)
	}
}

// soleSymbolLine returns the name line of the only symbol in a documentSymbol response.
func soleSymbolLine(t *testing.T, responses []*Message, id string) int {
	t.Helper()
	response := responseByID(responses, id)
	if response == nil {
		t.Fatalf("no documentSymbol response for id %s", id)
	}
	var symbols []DocumentSymbol
	if err := json.Unmarshal(response.Result, &symbols); err != nil {
		t.Fatalf("unmarshal documentSymbol %s: %v", id, err)
	}
	if len(symbols) != 1 {
		t.Fatalf("documentSymbol %s returned %d symbols, want 1", id, len(symbols))
	}
	return symbols[0].SelectionRange.Start.Line
}

// expectMatchesScan fails when lineChar or position disagrees with scanPosition.
func expectMatchesScan(t *testing.T, src string, index *lineIndex, offset int) {
	t.Helper()
	wantLine, wantCharacter := scanPosition(src, offset)
	line, character := index.lineChar(offset)
	if line != wantLine || character != wantCharacter {
		t.Errorf("lineChar(%.40q, %d) = (%d, %d), want (%d, %d)",
			src, offset, line, character, wantLine, wantCharacter)
	}
	want := Position{Line: wantLine, Character: wantCharacter}
	if got := index.position(offset); got != want {
		t.Errorf("position(%.40q, %d) = %+v, want %+v", src, offset, got, want)
	}
}

// position has no other direct test: a wrapper that swaps the fields, or returns
// byte columns instead of UTF-16 units, must be caught here.
func TestLineIndexPositionWrapsLineChar(t *testing.T) {
	src := "ab\n😀x\n\nend"
	index := newLineIndex(src)
	for offset := -2; offset <= len(src)+2; offset++ {
		expectMatchesScan(t, src, index, offset)
	}
	if got, want := index.position(len("ab\n😀x")), (Position{Line: 1, Character: 3}); got != want {
		t.Errorf("position after the emoji and x = %+v, want %+v", got, want)
	}
}

// An offset on a '\n' still belongs to the line it ends; one past it starts the next.
// Consecutive and trailing newlines are where duplicate or missing line starts show.
func TestLineIndexNewlineBoundaries(t *testing.T) {
	type position struct{ line, character int }
	tests := []struct {
		src  string
		want map[int]position
	}{
		{"ab\ncd", map[int]position{0: {0, 0}, 1: {0, 1}, 2: {0, 2}, 3: {1, 0}, 4: {1, 1}, 5: {1, 2}, 6: {1, 2}, 99: {1, 2}}},
		{"ab\n", map[int]position{2: {0, 2}, 3: {1, 0}, 4: {1, 0}}}, // trailing newline opens an empty last line
		{"\n\n\n", map[int]position{0: {0, 0}, 1: {1, 0}, 2: {2, 0}, 3: {3, 0}, 4: {3, 0}}},
		{"a\n\nb", map[int]position{0: {0, 0}, 1: {0, 1}, 2: {1, 0}, 3: {2, 0}, 4: {2, 1}}},
		{"abc", map[int]position{0: {0, 0}, 3: {0, 3}, 4: {0, 3}}}, // final line without a newline
	}
	for _, test := range tests {
		index := newLineIndex(test.src)
		for offset, want := range test.want {
			if line, character := index.lineChar(offset); line != want.line || character != want.character {
				t.Errorf("lineChar(%q, %d) = (%d, %d), want (%d, %d)",
					test.src, offset, line, character, want.line, want.character)
			}
			expectMatchesScan(t, test.src, index, offset)
		}
	}
}

// Only '\n' ends a line. An implementation built on bufio.ScanLines, strings.Split on
// "\r\n", or unicode line separators would shift every later line.
func TestLineIndexOnlyNewlineBreaksLines(t *testing.T) {
	src := "a\rb\r\rc d\u0085e\x0bf\x0cg\x00h\r\n\r\ni"
	index := newLineIndex(src)
	for offset := -1; offset <= len(src)+1; offset++ {
		expectMatchesScan(t, src, index, offset)
	}
	if line, _ := index.lineChar(strings.Index(src, "i")); line != 2 {
		t.Errorf("line of the final 'i' = %d, want 2 (only the two \\n bytes break lines)", line)
	}
}

// Astral runes count 2 UTF-16 units; a pair at the very start or end of a line, and
// offsets inside the 4 bytes, are where rune counting or byte counting diverge.
func TestLineIndexSurrogatePairsAtLineEdges(t *testing.T) {
	src := "😀\n😀x\nx😀"
	index := newLineIndex(src)
	type position struct{ line, character int }
	want := map[int]position{
		0: {0, 0}, 4: {0, 2}, 5: {1, 0}, 9: {1, 2}, 10: {1, 3},
		11: {2, 0}, 12: {2, 1}, 16: {2, 3}, 99: {2, 3},
	}
	for offset, expected := range want {
		if line, character := index.lineChar(offset); line != expected.line || character != expected.character {
			t.Errorf("lineChar(%d) = (%d, %d), want (%d, %d)", offset, line, character, expected.line, expected.character)
		}
	}
	for _, fixture := range []string{src, "😀😀\n😀😀", "é😀世\n世😀é\n", "\n😀\n", "😀\n\n😀"} {
		fixtureIndex := newLineIndex(fixture)
		for offset := -1; offset <= len(fixture)+1; offset++ { // includes every mid-rune offset
			expectMatchesScan(t, fixture, fixtureIndex, offset)
		}
	}
}

// Truncated and stray bytes right before a newline or at end of source must not be
// decoded across the line boundary or dropped from the count.
func TestLineIndexInvalidUTF8AtLineBoundaries(t *testing.T) {
	fixtures := []string{
		"\xc3\n\xc3\nz",
		"\xf0\x9f\x98\nx",            // 4-byte lead cut short by the newline
		"x\xf0\x9f\x98",              // same, cut short by end of source
		"\xf0\x9f\x98\x80\xf0\n\x80", // continuation byte opens the next line
		"\x80\x80\x80\n\xbf\xbf",
		"\xed\xa0\x80\n\xed\xb0\x80", // encoded surrogate halves are invalid UTF-8
		"\xff\n\xff\n\xff",
	}
	for _, fixture := range fixtures {
		index := newLineIndex(fixture)
		for offset := -1; offset <= len(fixture)+1; offset++ {
			expectMatchesScan(t, fixture, index, offset)
		}
	}
}

// Goto-definition on a local after a large switch must not be quadratic in the arms.
// Merging each arm's bindings into a rebuilt set took about 10 s at this size.
func TestLargeSwitchDefinitionStaysFast(t *testing.T) {
	const (
		maximumRequestTime = 2 * time.Second
		armCount           = 4000
		targetArm          = 2000
		uri                = "file:///switch.tcl"
		firstArmLine       = 2 // 0-based: after the proc and switch lines
	)
	var text strings.Builder
	text.WriteString("proc f {k} {\n  switch $k {\n")
	for arm := 0; arm < armCount; arm++ {
		fmt.Fprintf(&text, "    k%d { set v%d %d }\n", arm, arm, arm)
	}
	fmt.Fprintf(&text, "  }\n  puts $v%d\n}\n", targetArm)

	server := NewServer(nil)
	server.setDoc(uri, text.String())
	useLine := firstArmLine + armCount + 1
	start := time.Now()
	locations := server.handleDefinition(TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: uri},
		Position:     Position{Line: useLine, Character: len("  puts $v")},
	})
	elapsed := time.Since(start)
	if len(locations) != 1 || locations[0].Range.Start.Line != firstArmLine+targetArm {
		t.Errorf("locations = %+v, want one on line %d", locations, firstArmLine+targetArm)
	}
	if elapsed >= maximumRequestTime {
		t.Errorf("took %v, want under %v", elapsed, maximumRequestTime)
	}
}

// Offsets far outside the source's range must clamp, not overflow or index out of
// range. math.MaxInt + 1 wraps negative if the clamp comes after the search key.
func TestLineIndexExtremeOffsets(t *testing.T) {
	for _, src := range []string{"", "x", "a\nb😀\nc", "\n\n"} {
		index := newLineIndex(src)
		for _, offset := range []int{math.MinInt, math.MinInt + 1, -1000, math.MaxInt - 1, math.MaxInt} {
			expectMatchesScan(t, src, index, offset)
		}
	}
}

// Differential fuzz of the binary search against the scan: random mixes of newlines,
// empty lines, multi-byte, astral and invalid bytes, every offset. Queries then repeat
// in shuffled order on the same index, which catches a cursor or last-line cache that
// makes an answer depend on the previous query.
func TestLineIndexRandomSourcesMatchScan(t *testing.T) {
	pieces := []string{"a", "bc", "\n", "\n", "\n", "é", "世", "😀", "\xff", "\xf0\x9f", "\r", " ", "$x"}
	for seed := int64(1); seed <= 15; seed++ {
		random := rand.New(rand.NewSource(seed))
		var builder strings.Builder
		for pieceCount := random.Intn(400); pieceCount > 0; pieceCount-- {
			builder.WriteString(pieces[random.Intn(len(pieces))])
		}
		src := builder.String()
		index := newLineIndex(src)
		offsets := make([]int, 0, len(src)+4)
		for offset := -1; offset <= len(src)+2; offset++ {
			offsets = append(offsets, offset)
		}
		for pass := 0; pass < 2; pass++ {
			for _, offset := range offsets {
				expectMatchesScan(t, src, index, offset)
			}
			random.Shuffle(len(offsets), func(i, j int) { offsets[i], offsets[j] = offsets[j], offsets[i] })
		}
		if t.Failed() {
			t.Fatalf("seed %d diverged", seed)
		}
	}
}

// One enormous line: the column must count from the line start with the full UTF-16
// width, and a long run of lines must still resolve each offset to its own line.
func TestLineIndexVeryLongLineAndManyLines(t *testing.T) {
	longLine := strings.Repeat("é😀a", 100000) + "\nshort"
	longIndex := newLineIndex(longLine)
	for _, offset := range []int{0, 1, 2, 5, 6, 7, 99991, 123457, 700000 - 1, 700000, 700001, 700002, len(longLine), len(longLine) + 1} {
		expectMatchesScan(t, longLine, longIndex, offset)
	}
	if line, character := longIndex.lineChar(700001 + 3); line != 1 || character != 3 {
		t.Errorf("lineChar into the short line after a 700KB line = (%d, %d), want (1, 3)", line, character)
	}

	const lineCount = 100000
	manyLines := strings.Repeat("x\n", lineCount)
	manyIndex := newLineIndex(manyLines)
	for _, lineNumber := range []int{0, 1, 2, 4095, 4096, 65535, 65536, lineCount - 2, lineCount - 1} {
		if line, character := manyIndex.lineChar(2 * lineNumber); line != lineNumber || character != 0 {
			t.Errorf("lineChar(%d) = (%d, %d), want (%d, 0)", 2*lineNumber, line, character, lineNumber)
		}
		if line, character := manyIndex.lineChar(2*lineNumber + 1); line != lineNumber || character != 1 {
			t.Errorf("lineChar(%d) = (%d, %d), want (%d, 1)", 2*lineNumber+1, line, character, lineNumber)
		}
	}
	if line, character := manyIndex.lineChar(len(manyLines) + 5); line != lineCount || character != 0 {
		t.Errorf("past-end lineChar = (%d, %d), want (%d, 0)", line, character, lineCount)
	}
}

// buildWorkspaceSymbols workers share one index, so lineChar must be read-only.
// Meaningful under -race: a lazily built table or a remembered last line is a data race.
func TestLineIndexConcurrentReaders(t *testing.T) {
	src := strings.Repeat("proc p {} {}\nset x 😀\n\n", 200)
	index := newLineIndex(src)
	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func(worker int) {
			defer group.Done()
			for offset := worker; offset <= len(src)+1; offset += 8 {
				wantLine, wantCharacter := scanPosition(src, offset)
				if line, character := index.lineChar(offset); line != wantLine || character != wantCharacter {
					t.Errorf("concurrent lineChar(%d) = (%d, %d), want (%d, %d)",
						offset, line, character, wantLine, wantCharacter)
					return
				}
			}
		}(worker)
	}
	group.Wait()
}

// countingSources returns a sourceOf that serves the given files and counts calls.
func countingSources(files map[string]string) (func(string) string, *int64) {
	var calls int64
	return func(file string) string {
		atomic.AddInt64(&calls, 1)
		return files[file]
	}, &calls
}

// A request asks for the same file once per location; every ask must reuse one index
// and read the source once. Files with equal text or odd names must not share entries.
func TestLineIndexesForFileOneIndexPerFile(t *testing.T) {
	sourceOf, calls := countingSources(map[string]string{
		"a.tcl": "x\ny", "b.tcl": "x\ny", "": "named\nempty", "dir/ü 😀.tcl": "q\n\nr",
	})
	indexes := lineIndexes{}
	first := indexes.forFile("a.tcl", sourceOf)
	if first == nil {
		t.Fatal("forFile returned nil")
	}
	if again := indexes.forFile("a.tcl", sourceOf); again != first {
		t.Error("second forFile for the same file built a new index")
	}
	if got := atomic.LoadInt64(calls); got != 1 {
		t.Errorf("sourceOf called %d times for one file asked twice, want 1", got)
	}
	if other := indexes.forFile("b.tcl", sourceOf); other == first {
		t.Error("two different files with identical text share one index")
	}
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Errorf("sourceOf called %d times for two files, want 2", got)
	}
	for file, wantLine := range map[string]int{"": 1, "dir/ü 😀.tcl": 2} {
		if line, _ := indexes.forFile(file, sourceOf).lineChar(1 << 20); line != wantLine {
			t.Errorf("forFile(%q) last line = %d, want %d", file, line, wantLine)
		}
	}
}

// An unknown or empty file yields the zero source. It must still produce a usable index
// (never nil, which callers dereference) and must not be re-fetched on every ask.
func TestLineIndexesForFileEmptyAndUnknownSource(t *testing.T) {
	sourceOf, calls := countingSources(map[string]string{"empty.tcl": ""})
	indexes := lineIndexes{}
	for _, file := range []string{"empty.tcl", "missing.tcl"} {
		index := indexes.forFile(file, sourceOf)
		if index == nil {
			t.Fatalf("forFile(%q) = nil, want an index over empty source", file)
		}
		for _, offset := range []int{-1, 0, 1, 100} {
			if line, character := index.lineChar(offset); line != 0 || character != 0 {
				t.Errorf("forFile(%q).lineChar(%d) = (%d, %d), want (0, 0)", file, offset, line, character)
			}
		}
		if got := index.position(7); got != (Position{}) {
			t.Errorf("forFile(%q).position(7) = %+v, want zero Position", file, got)
		}
		if indexes.forFile(file, sourceOf) != index {
			t.Errorf("forFile(%q) built a second index for empty source", file)
		}
	}
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Errorf("sourceOf called %d times for two empty files each asked twice, want 2", got)
	}
}

// The cache is per request. A new lineIndexes must re-read the source, so an edit
// between requests is visible; any package-level or Server-level memo fails this.
func TestLineIndexesFreshInstanceSeesChangedSource(t *testing.T) {
	files := map[string]string{"edit.tcl": "proc a {} {}\n"}
	sourceOf, calls := countingSources(files)

	firstRequest := lineIndexes{}
	before, _ := firstRequest.forFile("edit.tcl", sourceOf).lineChar(len(files["edit.tcl"]))
	files["edit.tcl"] = "\n\nproc a {} {}\n"
	secondRequest := lineIndexes{}
	after, _ := secondRequest.forFile("edit.tcl", sourceOf).lineChar(len(files["edit.tcl"]))

	if before != 1 || after != 3 {
		t.Errorf("end-of-file line before/after edit = %d/%d, want 1/3", before, after)
	}
	if got := atomic.LoadInt64(calls); got != 2 {
		t.Errorf("sourceOf called %d times across two requests, want 2", got)
	}
}

// buildWorkspaceSymbols fills the cache before par.Map and workers only read it.
// A forFile hit must not write to the map (a race under -race) or call sourceOf.
func TestLineIndexesForFileHitIsReadOnly(t *testing.T) {
	files := map[string]string{"one.tcl": "a\nb", "two.tcl": "c\nd\ne", "three.tcl": ""}
	sourceOf, calls := countingSources(files)
	indexes := lineIndexes{}
	expected := map[string]*lineIndex{}
	for file := range files {
		expected[file] = indexes.forFile(file, sourceOf)
	}
	callsAfterWarmup := atomic.LoadInt64(calls)

	var group sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for round := 0; round < 200; round++ {
				for file, want := range expected {
					if indexes.forFile(file, sourceOf) != want {
						t.Errorf("forFile(%q) changed identity after warm-up", file)
						return
					}
				}
			}
		}()
	}
	group.Wait()
	if got := atomic.LoadInt64(calls); got != callsAfterWarmup {
		t.Errorf("sourceOf called %d more times on cache hits, want 0", got-callsAfterWarmup)
	}
}
