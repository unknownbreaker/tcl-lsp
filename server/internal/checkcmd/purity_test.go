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

// purityTranscript is a purity-table Check's tclsh evidence
// (testdata/tclsh/<name>.out, made by testdata/tclsh/<name>.tcl).
type purityTranscript struct {
	path     string
	finding  string // " <severity> <check-id> ", as tcl-lsp check prints it
	allow    string // the finding of another Check an exempt row may get, or ""
	fuzzList map[string]fuzzCase
}

// verify checks that every command in the transcript gets the finding its row
// names: one finding for flagged, and none for exempt. A flagged row is pure
// and gives the same result in both versions, and so does every fuzz line, so
// the evidence and the Check cannot drift apart.
func (p purityTranscript) verify(t *testing.T) {
	v86, v90 := transcriptBlocks(t, p.path)
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
			p.checkFuzzLine(t, cols)
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
		got := strings.Count(out, p.finding)
		others := strings.Count(out, "\n") - got
		if p.allow != "" {
			others -= strings.Count(out, p.allow)
		}
		if want := map[string]int{"flagged": 1, "exempt": 0}[kind]; got != want || others != 0 {
			t.Errorf("%s %q: got %q", kind, cmd, out)
		}
	}
	if rows == 0 || fuzzed != len(p.fuzzList) {
		t.Fatalf("got %d rows and %d fuzz lines, want rows and %d fuzz lines", rows, fuzzed, len(p.fuzzList))
	}
}

// fuzzCase mirrors one fuzz line of a purity transcript: the words it
// enumerates, and the commands it runs on each. The Check must flag all of a
// word's commands or none, and flag them for exactly the words the Tcl rule
// accepts.
type fuzzCase struct {
	alpha []string
	n     int
	cmds  func(v string) []string
}

// checkFuzzLine checks that the Check flags exactly the words the fuzz line
// counted: `fuzz name alphabet n count crc(words) crc(results)`.
func (p purityTranscript) checkFuzzLine(t *testing.T, cols []string) {
	t.Helper()
	if len(cols) != 7 {
		t.Fatalf("fuzz line %q: want 7 columns", cols)
	}
	fc, ok := p.fuzzList[cols[1]]
	if !ok {
		t.Fatalf("unknown fuzz line %q", cols[1])
	}
	if fmt.Sprint(fc.n) != cols[3] {
		t.Fatalf("fuzz %s: length %s, want %d", cols[1], cols[3], fc.n)
	}
	id := strings.Fields(p.finding)[1]
	var in strings.Builder
	k := 0
	for _, v := range enumWords(fc.alpha, fc.n) {
		cmds := fc.cmds(v)
		got := countFindings(strings.Join(cmds, "\n")+"\n", id)
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

func asciiAlpha() []string {
	var out []string
	for c := byte(0x20); c < 0x7f; c++ {
		out = append(out, string(c))
	}
	return out
}

// enumWords returns every word over alpha up to n letters, shortest first, in
// the order the transcripts' words proc (testdata/tclsh/purity.tcl) does.
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

// countFindings counts the findings of Check id on src, in process.
func countFindings(src, id string) int {
	var col check.Collector
	tcl.VisitCommands(src, tcl.Visit{Command: col.Visit})
	n := 0
	for _, f := range col.Findings() {
		if f.Check == id {
			n++
		}
	}
	return n
}
