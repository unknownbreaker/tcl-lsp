package checkcmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every condition in the tclsh transcript (testdata/tclsh/constant_condition.out)
// gets the finding its row names: a warning for flagged, a hint for hint, and
// nothing for exempt. So the evidence and the Check cannot drift apart.
func TestConstantConditionTranscript(t *testing.T) {
	f, err := os.Open("testdata/tclsh/constant_condition.out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	want := map[string]string{"flagged": "warning", "hint": "hint", "exempt": ""}
	dir := t.TempDir()
	rows := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		kind := strings.TrimSpace(line[:min(9, len(line))])
		sev, ok := want[kind]
		if !ok {
			continue
		}
		rows++
		cond := strings.TrimSpace(line[9:49])
		path := filepath.Join(dir, "c.tcl")
		write(t, path, "if {"+cond+"} {}\n")
		out, _, _ := run(path)
		got := ""
		if out != "" {
			got = strings.Fields(strings.SplitN(out, ": ", 2)[1])[0]
		}
		if got != sev || strings.Count(out, "\n") > 1 {
			t.Errorf("%s %q: got %q, want severity %q", kind, cond, out, sev)
		}
	}
	if rows == 0 {
		t.Fatal("no transcript rows")
	}
}

// Tcl rejects each of these commands before it evaluates the condition ("extra
// characters after close-brace" or "after close-quote", in 8.6 and 9.0), since
// two of its words abut. The scanner splits the abutting text into a word of its
// own, which must not pass for an else body. Each source is checked on its own,
// so a stray quote or brace cannot hide the next case.
func TestConstantConditionAbuttingWordsSilent(t *testing.T) {
	dir := t.TempDir()
	for _, src := range []string{
		"if {1 == 1} {x}]\n",
		"if {1 == 1} {x}}\n",
		"if {1 == 1}x {x}\n",
		"if {1 == 1}{x}\n",
		"if {1 == 1} {x}{y}\n",
		"if {1 == 1}\"x\"\n",
		"if \"1 == 1\"{x}\n",
		"if {0} {x}{y}\n",
		"if {$a} {} elseif {1 == 1}{x}\n",
		"while {1 < 2}{x}\n",
		"for {} {1 < 2} {}{} {x}\n",
	} {
		path := filepath.Join(dir, "a.tcl")
		write(t, path, src)
		if out, _, _ := run(path); out != "" {
			t.Errorf("%q: want no finding, got %q", src, out)
		}
	}
}
