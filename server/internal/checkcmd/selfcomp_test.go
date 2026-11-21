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

// Each finding stays on its one output line, and names its operand only when
// that is short and printable.
func TestSelfComparisonMessageNamesOnlyShortOperands(t *testing.T) {
	long := "\"$x" + strings.Repeat("a", 60) + "\""
	dir := t.TempDir()
	for src, want := range map[string]string{
		"if {\"$x\n\" eq \"$x\n\"} {}\n":           "compares a value with itself, so it is always true",
		"if {\"$x\t\" ne \"$x\t\"} {}\n":           "compares a value with itself, so it is always false",
		"if {" + long + " eq " + long + "} {}\n":   "compares a value with itself, so it is always true",
		"string equal " + long + " " + long + "\n": "compares a value with itself, so it always returns 1",
		"if {$x eq $x} {}\n":                       "compares $x with itself, so it is always true",
	} {
		path := filepath.Join(dir, "m.tcl")
		write(t, path, src)
		out, _, _ := run(path)
		if strings.Count(out, "\n") != 1 || !strings.HasSuffix(out, " self-comparison "+want+"\n") {
			t.Errorf("%q: got %q, want one line ending %q", src, out, want)
		}
	}
}

// Shapes a fix must not silence: precedence groups the left comparison first,
// and an expression index may hold spaces.
func TestSelfComparisonStillFlagged(t *testing.T) {
	dir := t.TempDir()
	for src, n := range map[string]int{
		"if {$x eq $x in $y} {}\n":             1, // ($x eq $x) in $y
		"if {$x < $x <= $y} {}\n":              1,
		"if {$y?$x eq $x:0} {}\n":              1,
		"if {$x eq$x} {}\n":                    1,
		"if {$x(a b) eq $x(a b)} {}\n":         1,
		"string equal $x(a) $x(a)\n":           1,
		"string equal $y(a$z(b)) $y(a$z(b))\n": 1,
		"if {$y < $x < $x} {}\n":               0, // ($y < $x) < $x
		"if {0 == $x eq $x} {}\n":              0,
		"if {$y ne $x ne $x} {}\n":             0,
	} {
		path := filepath.Join(dir, "f.tcl")
		write(t, path, src)
		out, _, _ := run(path)
		if got := strings.Count(out, " warning self-comparison "); got != n {
			t.Errorf("%q: got %d findings, want %d:\n%s", src, got, n, out)
		}
	}
}
