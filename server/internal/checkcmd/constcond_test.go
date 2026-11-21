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
