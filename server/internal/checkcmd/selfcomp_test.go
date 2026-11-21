package checkcmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every condition in the tclsh transcript (testdata/tclsh/self_comparison.out)
// gets the finding its row names: one self-comparison warning for flagged, and
// nothing for exempt. So the evidence and the Check cannot drift apart.
func TestSelfComparisonTranscript(t *testing.T) {
	f, err := os.Open("testdata/tclsh/self_comparison.out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]bool{"flagged": true, "exempt": false}
	dir := t.TempDir()
	rows := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		kind := strings.TrimSpace(line[:min(9, len(line))])
		flagged, ok := want[kind]
		if !ok {
			continue
		}
		rows++
		cond := strings.TrimSpace(line[9:49])
		path := filepath.Join(dir, "c.tcl")
		write(t, path, "if {"+cond+"} {}\n")
		out, _, _ := run(path)
		got := strings.Count(out, " warning self-comparison ")
		if flagged && (got != 1 || strings.Count(out, "\n") != 1) || !flagged && out != "" {
			t.Errorf("%s %q: got %q", kind, cond, out)
		}
	}
	if rows == 0 {
		t.Fatal("no transcript rows")
	}
}

// A redefined string, or a command named after the subcommand in any namespace
// (`::tcl::string::equal` is what `string equal` runs), silences the command
// form; the expression form relies only on the command that evaluates it.
func TestSelfComparisonShadowed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct{ src, want string }{
		{"proc ::tcl::string::equal args {}\nstring equal $x $x\n", ""},
		{"proc compare args {}\nstring compare $x $x\n", ""},
		{"proc compare args {}\nstring equal $x $x\n", "string equal"},
		{"rename string {}\nstring equal $x $x\nif {$x eq $x} {}\n", "if"},
		{"proc expr args {}\nexpr {$x eq $x}\nif {$x eq $x} {}\n", "if"},
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, tc.src)
		out, _, _ := run(path)
		if tc.want == "" && out != "" || tc.want != "" && strings.Count(out, "\n") != 1 {
			t.Errorf("%q: got %q, want one finding on %q", tc.src, out, tc.want)
		}
	}
}
