package checkcmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every case in the tclsh transcript (testdata/tclsh/unreachable_code.out) gets
// the finding its row names, run as the same proc body the transcript ran: one
// unreachable-code hint for flagged, none for exempt. A flagged row must show,
// in every Tcl version, that the command after the exit never ran. So the
// evidence and the Check cannot drift apart.
func TestUnreachableCodeTranscript(t *testing.T) {
	f, err := os.Open("testdata/tclsh/unreachable_code.out")
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
		body := strings.TrimSpace(line[9:77])
		if flagged && !strings.Contains(line[77:], "ran=0") {
			t.Errorf("flagged %q: the transcript shows the dead command ran: %q", body, line)
		}
		path := filepath.Join(dir, "c.tcl")
		write(t, path, "proc p {} {set d {k v}; "+body+"}\n")
		out, _, _ := run(path)
		got := strings.Count(out, " hint unreachable-code ")
		if flagged && got != 1 || !flagged && got != 0 {
			t.Errorf("%s %q: got %q", kind, body, out)
		}
	}
	if rows == 0 {
		t.Fatal("no transcript rows")
	}
}

// A redefined exit, or a redefined builtin whose body holds the dead code,
// silences the hint.
func TestUnreachableCodeShadowed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"proc p {} { return; a }\n", 1},
		{"proc return args {}\nproc p {} { return; a }\n", 0},
		{"namespace eval ns { proc error args {} }\nproc p {} { error x; a }\n", 0},
		{"rename break {}\nforeach v {1} { break; a }\n", 0},
		{"proc foreach args {}\nforeach v {1} { continue; a }\n", 0},
		{"proc if args {}\nproc p {} { if 1 { return; a } }\n", 0},
		{"proc if args {}\nproc p {} { return; if 1 { a } }\n", 1},
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, tc.src)
		out, _, _ := run(path)
		if got := strings.Count(out, " unreachable-code "); got != tc.want {
			t.Errorf("%q: got %d findings, want %d:\n%s", tc.src, got, tc.want, out)
		}
	}
}
