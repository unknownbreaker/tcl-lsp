package tcl_test

// The parse dump and the corpus benchmarks (research/09). The dump is the
// language-neutral form of what the scanner and command/word parser read: a
// second implementation (research/experiments/09_rust_parse) must print the same
// bytes for every input before its timing counts. The benchmarks keep the Go
// baseline reproducible and catch a parser change that regresses throughput.
//
// Environment:
//   TCL_CORPUS     corpus root, walked for *.tcl (default: the Homebrew Tcl
//                  library). The corpus tests and benchmarks skip when it is missing.
//   PARSEDUMP_OUT  when set, TestParseDumpWrite writes <rel>.json there for every
//                  corpus file.
//   PARSEDUMP_UPDATE=1  rewrites the golden dumps in testdata/parsedump.

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

const defaultCorpus = "/opt/homebrew/opt/tcl-tk/lib"

// sourceCommands groups Scan's tokens into commands as Parse does, minus
// Parse's head rewrite (bareModeledHead): each word is the word as written.
// TestSourceCommandsMatchParse keeps the two in step.
func sourceCommands(src string) [][]tcl.Word {
	var cmds [][]tcl.Word
	var cur []tcl.Word
	for _, tk := range tcl.Scan(src) {
		switch tk.Kind {
		case tcl.KindWord:
			k := tcl.WordBare
			if tk.Text != "" {
				switch tk.Text[0] {
				case '{':
					k = tcl.WordBraced
				case '"':
					k = tcl.WordQuoted
				}
			}
			cur = append(cur, tcl.Word{Kind: k, Text: tk.Text, Start: tk.Start, End: tk.End})
		case tcl.KindNewline, tcl.KindSemicolon, tcl.KindEOF:
			if len(cur) > 0 {
				cmds = append(cmds, cur)
				cur = nil
			}
		}
	}
	return cmds
}

var wordKindNames = map[tcl.WordKind]string{
	tcl.WordBare: "bare", tcl.WordBraced: "braced", tcl.WordQuoted: "quoted",
}

// parseDump renders src's commands in the dump format (research/09): one
// command per line, each an array of {"kind","text","start","end"} objects,
// with offsets as UTF-8 byte offsets.
func parseDump(src string) []byte {
	cmds := sourceCommands(src)
	if len(cmds) == 0 {
		return []byte("[]\n")
	}
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, cmd := range cmds {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteByte('[')
		for j, w := range cmd {
			if j > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `{"kind":"%s","text":`, wordKindNames[w.Kind])
			writeDumpString(&b, w.Text)
			fmt.Fprintf(&b, `,"start":%d,"end":%d}`, w.Start, w.End)
		}
		b.WriteByte(']')
	}
	b.WriteString("\n]\n")
	return b.Bytes()
}

// writeDumpString writes s as a JSON string: `"` and `\` escaped, every byte
// below 0x20 or equal to 0x7f as \u00xx, valid UTF-8 as is, and each byte that is
// not part of a valid UTF-8 sequence as \u00xx.
func writeDumpString(b *bytes.Buffer, s string) {
	b.WriteByte('"')
	for i := 0; i < len(s); {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
			i++
		case c < 0x20 || c == 0x7f:
			fmt.Fprintf(b, `\u%04x`, c)
			i++
		case c < utf8.RuneSelf:
			b.WriteByte(c)
			i++
		default:
			r, n := utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && n == 1 {
				fmt.Fprintf(b, `\u%04x`, c)
			} else {
				b.WriteString(s[i : i+n])
			}
			i += n
		}
	}
	b.WriteByte('"')
}

type corpusFile struct{ rel, src string }

// loadCorpus reads every regular *.tcl file under root into memory, sorted by
// path relative to root. Symlinks inside root are not followed.
func loadCorpus(tb testing.TB, root string) []corpusFile {
	tb.Helper()
	dir, err := filepath.EvalSymlinks(root)
	if err != nil {
		tb.Skipf("no corpus at %s: %v", root, err)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		tb.Fatalf("corpus %s is not a directory", root)
	}
	var files []corpusFile
	err = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".tcl") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		files = append(files, corpusFile{rel: filepath.ToSlash(rel), src: string(b)})
		return nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	if len(files) == 0 {
		tb.Skipf("no .tcl files under %s", root)
	}
	slices.SortFunc(files, func(a, b corpusFile) int { return strings.Compare(a.rel, b.rel) })
	return files
}

func corpusRoot() string {
	if r := os.Getenv("TCL_CORPUS"); r != "" {
		return r
	}
	return defaultCorpus
}

// The golden dumps in testdata/parsedump pin the format and the parser's reading
// of hostile input: unterminated braces, quotes and brackets, continuations,
// comment edges, and `]` hidden in quotes, braces, indexes and comments.
func TestParseDumpGolden(t *testing.T) {
	files := loadCorpus(t, "testdata/parsedump")
	update := os.Getenv("PARSEDUMP_UPDATE") == "1"
	for _, f := range files {
		golden := filepath.Join("testdata/parsedump", strings.TrimSuffix(f.rel, ".tcl")+".json")
		got := parseDump(f.src)
		if update {
			if err := os.WriteFile(golden, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s: %v (run with PARSEDUMP_UPDATE=1)", f.rel, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: dump differs from %s:\n got: %s\nwant: %s", f.rel, golden, got, want)
		}
	}
}

// Parse differs from sourceCommands only in its head rewrite: the head may be
// respelled bare, keeping its span, and a `{*}` that expands it dropped.
// Everything else, and the commands themselves, must match.
func TestSourceCommandsMatchParse(t *testing.T) {
	check := func(t *testing.T, files []corpusFile) {
		for _, f := range files {
			src, parsed := sourceCommands(f.src), tcl.Parse(f.src)
			if len(src) != len(parsed) {
				t.Fatalf("%s: %d source commands, Parse gave %d", f.rel, len(src), len(parsed))
			}
			for i := range src {
				sw, pw := src[i], parsed[i].Words
				d := len(sw) - len(pw)
				dropOK := d == 0 || d == 1 && sw[0].Kind == tcl.WordBraced && sw[0].Text == "{*}" &&
					len(sw) > 1 && sw[0].End == sw[1].Start
				headOK := d >= 0 && d < len(sw) && sw[d].Start == pw[0].Start && sw[d].End == pw[0].End &&
					(sw[d] == pw[0] || pw[0].Kind == tcl.WordBare)
				if !dropOK || !headOK || !slices.Equal(sw[d+1:], pw[1:]) {
					t.Fatalf("%s command %d: source words %+v, Parse words %+v", f.rel, i, sw, pw)
				}
			}
		}
	}
	t.Run("golden", func(t *testing.T) { check(t, loadCorpus(t, "testdata/parsedump")) })
	t.Run("corpus", func(t *testing.T) { check(t, loadCorpus(t, corpusRoot())) })
}

// TestParseDumpWrite writes the dump of every corpus file to $PARSEDUMP_OUT, for
// research/experiments/09_rust_parse/compare.sh.
func TestParseDumpWrite(t *testing.T) {
	out := os.Getenv("PARSEDUMP_OUT")
	if out == "" {
		t.Skip("PARSEDUMP_OUT not set")
	}
	for _, f := range loadCorpus(t, corpusRoot()) {
		p := filepath.Join(out, f.rel+".json")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, parseDump(f.src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// BenchmarkCorpus runs each stage over the whole corpus ("all") and over its
// largest file ("largest"), reporting MB/s and allocations per pass:
//
//	scan   tcl.Scan
//	words  tcl.Scan grouped into commands as written (the work the dump and the
//	       Rust port do)
//	parse  tcl.Parse (words plus the head rewrite)
//	index  source.IndexUnit, full per-file indexing
//
// One pass per count, so the median of the counts is comparable with the Rust
// port's median pass (research/09 "Method"):
//
//	go test -C server -run '^$' -bench Corpus -benchmem -benchtime 1x -count 20 ./internal/tcl
func BenchmarkCorpus(b *testing.B) {
	files := loadCorpus(b, corpusRoot())
	largest := files[0]
	for _, f := range files {
		if len(f.src) > len(largest.src) {
			largest = f
		}
	}
	sets := []struct {
		name  string
		files []corpusFile
	}{{"all", files}, {"largest", []corpusFile{largest}}}
	stages := []struct {
		name string
		run  func(corpusFile)
	}{
		{"scan", func(f corpusFile) { _ = tcl.Scan(f.src) }},
		{"words", func(f corpusFile) { _ = sourceCommands(f.src) }},
		{"parse", func(f corpusFile) { _ = tcl.Parse(f.src) }},
		{"index", func(f corpusFile) { _ = source.IndexUnit(f.rel, f.src) }},
	}
	for _, st := range stages {
		for _, set := range sets {
			var n int64
			for _, f := range set.files {
				n += int64(len(f.src))
			}
			b.Run(st.name+"/"+set.name, func(b *testing.B) {
				b.SetBytes(n)
				b.ReportAllocs()
				for range b.N {
					for _, f := range set.files {
						st.run(f)
					}
				}
			})
		}
	}
}
