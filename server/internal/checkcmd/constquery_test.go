package checkcmd

import (
	"bufio"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/check"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// transcriptBlocks splits a tclsh transcript into its per-version blocks, keyed
// by the patchlevel on each `== Tcl` line, and fails unless it holds exactly
// one 8.6 and one 9.0 run.
func transcriptBlocks(t *testing.T, path string) (v86, v90 []string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	blocks := map[string][]string{}
	var cur string
	sc := bufio.NewScanner(f)
	sc.Buffer(nil, 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if v, ok := strings.CutPrefix(line, "== Tcl "); ok {
			cur = v[:3]
			if _, dup := blocks[cur]; dup {
				t.Fatalf("two Tcl %s runs in %s", cur, path)
			}
			blocks[cur] = []string{}
			continue
		}
		if cur == "" {
			t.Fatalf("%s: line before any `== Tcl` header: %q", path, line)
		}
		blocks[cur] = append(blocks[cur], line)
	}
	if len(blocks) != 2 || blocks["8.6"] == nil || blocks["9.0"] == nil {
		t.Fatalf("%s: want one Tcl 8.6 and one Tcl 9.0 run, got %d runs", path, len(blocks))
	}
	return blocks["8.6"], blocks["9.0"]
}

// Every command in the tclsh transcript (testdata/tclsh/constant_query.out)
// gets the finding its row names: one constant-query warning for flagged, and
// none for exempt. A flagged row is pure and gives the same result in both
// versions, and so does every fuzz line, so the evidence and the Check cannot
// drift apart.
func TestConstantQueryTranscript(t *testing.T) {
	v86, v90 := transcriptBlocks(t, "testdata/tclsh/constant_query.out")
	if len(v86) != len(v90) {
		t.Fatalf("8.6 has %d lines, 9.0 has %d", len(v86), len(v90))
	}
	dir := t.TempDir()
	rows, fuzzed := 0, 0
	for i, line := range v86 {
		cols := strings.Split(line, "\t")
		switch cols[0] {
		case "fuzz":
			fuzzed++
			if line != v90[i] {
				t.Errorf("fuzz lines differ by version:\n8.6 %s\n9.0 %s", line, v90[i])
			}
			checkFuzzLine(t, cols)
			continue
		case "flagged", "exempt":
		default:
			t.Fatalf("unknown row %q", line)
		}
		if len(cols) != 4 {
			t.Fatalf("row %q: want 4 columns", line)
		}
		rows++
		kind, cmd := cols[0], cols[1]
		if kind == "flagged" && (cols[2] != "pure" || line != v90[i]) {
			t.Errorf("flagged %q is not pure with one result in both versions:\n8.6 %s\n9.0 %s", cmd, line, v90[i])
		}
		path := filepath.Join(dir, "c.tcl")
		write(t, path, cmd+"\n")
		out, _, _ := run(path)
		got := strings.Count(out, " warning constant-query ")
		if want := map[string]int{"flagged": 1, "exempt": 0}[kind]; got != want || strings.Count(out, "\n") != want {
			t.Errorf("%s %q: got %q", kind, cmd, out)
		}
	}
	if rows == 0 || fuzzed != len(fuzzCases) {
		t.Fatalf("got %d rows and %d fuzz lines, want rows and %d fuzz lines", rows, fuzzed, len(fuzzCases))
	}
}

// fuzzCase mirrors one fuzz line of constant_query.tcl: the words it
// enumerates, and the commands it runs on each. The Check must flag all of a
// word's commands or none, and flag them for exactly the words the Tcl rule
// accepts.
type fuzzCase struct {
	alpha []string
	n     int
	cmds  func(v string) []string
}

var cqClasses = []string{"alnum", "alpha", "ascii", "control", "digit", "graph", "lower", "print", "punct", "space", "upper", "wordchar", "xdigit"}

var fuzzCases = map[string]fuzzCase{
	"classes": {asciiAlpha(), 1, func(v string) []string {
		var out []string
		for _, cls := range cqClasses {
			out = append(out, "string is "+cls+" "+tq(v), "string is "+cls+" -strict "+tq(v))
		}
		return out
	}},
	"integer": {[]string{"0", "1", "8", "9", "x", "o", "b", "d", "e", "_", "+", "-", " ", "a"}, 4, func(v string) []string {
		return []string{"string is integer " + tq(v), "string is integer -strict " + tq(v)}
	}},
	"index": {[]string{"0", "1", "9", "e", "n", "d", "-", "+", "x", "_", " "}, 4, func(v string) []string {
		return []string{"lindex {a b c d e f g h i j k} " + tq(v), "string index abcdefghijk " + tq(v)}
	}},
	"match": {[]string{"a", "b", "*", "?", "[", "]", "-", `\`, "^", "!"}, 3, func(v string) []string {
		var out []string
		for _, s := range enumWords([]string{"a", "b", "-", "]", `\`}, 2) {
			out = append(out, "string match "+tq(v)+" "+tq(s))
		}
		return out
	}},
	"text": {[]string{"a", "b", " ", "-", `\`}, 3, func(v string) []string {
		out := []string{"string index " + tq(v) + " end"}
		for _, s := range enumWords([]string{"a", "b"}, 2) {
			for _, sub := range []string{"first", "last", "equal", "compare"} {
				out = append(out, "string "+sub+" "+tq(s)+" "+tq(v))
			}
		}
		return out
	}},
	"list": {[]string{"a", "b", " ", ";", "#", "$", "[", "]"}, 4, func(v string) []string {
		return []string{"lindex " + tq(v) + " 0", "lindex " + tq(v) + " end"}
	}},
}

func asciiAlpha() []string {
	var out []string
	for c := byte(0x20); c < 0x7f; c++ {
		out = append(out, string(c))
	}
	return out
}

// enumWords returns every word over alpha up to n letters, shortest first, in
// the order constant_query.tcl's words proc does.
func enumWords(alpha []string, n int) []string {
	out, cur := []string{""}, []string{""}
	for range n {
		var next []string
		for _, w := range cur {
			for _, c := range alpha {
				next = append(next, w+c)
			}
		}
		out = append(out, next...)
		cur = next
	}
	return out
}

// tq spells v as a quoted Tcl word with no substitution.
func tq(v string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		if strings.IndexByte(`\$[]"`, v[i]) >= 0 {
			b.WriteByte('\\')
		}
		b.WriteByte(v[i])
	}
	b.WriteByte('"')
	return b.String()
}

// constantQueries counts the constant-query findings on src, in process.
func constantQueries(src string) int {
	var col check.Collector
	tcl.VisitCommands(src, tcl.Visit{Command: col.Visit})
	n := 0
	for _, f := range col.Findings() {
		if f.Check == "constant-query" {
			n++
		}
	}
	return n
}

// checkFuzzLine checks that the Check flags exactly the words the fuzz line
// counted: `fuzz name alphabet n count crc(words) crc(results)`.
func checkFuzzLine(t *testing.T, cols []string) {
	t.Helper()
	if len(cols) != 7 {
		t.Fatalf("fuzz line %q: want 7 columns", cols)
	}
	fc, ok := fuzzCases[cols[1]]
	if !ok {
		t.Fatalf("unknown fuzz line %q", cols[1])
	}
	if fmt.Sprint(fc.n) != cols[3] {
		t.Fatalf("fuzz %s: length %s, want %d", cols[1], cols[3], fc.n)
	}
	var in strings.Builder
	k := 0
	for _, v := range enumWords(fc.alpha, fc.n) {
		cmds := fc.cmds(v)
		got := constantQueries(strings.Join(cmds, "\n") + "\n")
		if got != 0 && got != len(cmds) {
			t.Errorf("fuzz %s %q: flagged %d of %d commands", cols[1], v, got, len(cmds))
		}
		if got == len(cmds) {
			k++
			in.WriteString(v + "\n")
		}
	}
	if gotK, gotCRC := fmt.Sprint(k), fmt.Sprint(crc32.ChecksumIEEE([]byte(in.String()))); gotK != cols[4] || gotCRC != cols[5] {
		t.Errorf("fuzz %s: the Check flags %s words (crc %s), Tcl's rule accepts %s (crc %s)", cols[1], gotK, gotCRC, cols[4], cols[5])
	}
}

// A redefined command, or a command named after the subcommand in any
// namespace (`::tcl::string::index` is what `string index` runs), silences
// the finding; a namespace's own `string` does too.
func TestConstantQueryShadowed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"proc string args {}\nstring index abc 0\nlindex {a b} 0\n", 1},
		{"namespace eval ns { proc string args {} }\nstring index abc 0\n", 0},
		{"proc ::tcl::string::index args {}\nstring index abc 0\nstring is alpha a\n", 1},
		{"proc is args {}\nstring is alpha a\nstring index abc 0\n", 1},
		{"rename lindex {}\nlindex {a b} 0\nstring index abc 0\n", 1},
		{"interp alias {} lindex {} myindex\nlindex {a b} 0\n", 0},
		{"proc for args {}\nproc p {} { string index abc 0 }\n", 1},
		{"proc proc args {}\nproc p {} { string index abc 0 }\n", 0},
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, tc.src)
		out, _, _ := run(path)
		if got := strings.Count(out, " constant-query "); got != tc.want {
			t.Errorf("%q: got %d findings, want %d:\n%s", tc.src, got, tc.want, out)
		}
	}
}

// The message names the query, and the finding starts at the command's head.
func TestConstantQueryMessage(t *testing.T) {
	dir := t.TempDir()
	for src, want := range map[string]string{
		"string index abc 0\n":          "1:1: warning constant-query string index has only literal arguments, so it always returns the same result",
		"set n [::string is alpha a]\n": "1:8: warning constant-query string is has only literal arguments, so it always returns the same result",
		"puts [lindex {a b} 0]\n":       "1:7: warning constant-query lindex has only literal arguments, so it always returns the same result",
	} {
		path := filepath.Join(dir, "m.tcl")
		write(t, path, src)
		out, _, _ := run(path)
		if out != path+":"+want+"\n" {
			t.Errorf("%q: got %q, want %q", src, out, want)
		}
	}
}

// Touching words are a Tcl parse error ("extra characters after close-quote"
// or "close-brace" on 8.6 and 9.0), so nothing runs and nothing is flagged.
func TestConstantQueryTouchingWordsSilent(t *testing.T) {
	for _, src := range []string{
		"string equal \"a\"\"b\"\n",
		"string equal {a}{b}\n",
		"lindex {a b}{0}\n",
	} {
		if got := constantQueries(src); got != 0 {
			t.Errorf("%q: got %d findings, want 0", src, got)
		}
	}
}
