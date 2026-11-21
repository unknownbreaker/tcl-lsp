package checkcmd

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every command in the tclsh transcript (testdata/tclsh/duplicate_logic.out)
// gets the finding its row names: one duplicate-logic warning for flagged, and
// none for exempt. A flagged row must also agree with its reduction in every
// Tcl, so the evidence and the Check cannot drift apart.
func TestDuplicateLogicTranscript(t *testing.T) {
	f, err := os.Open("testdata/tclsh/duplicate_logic.out")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	dir := t.TempDir()
	rows := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		cols := strings.Split(sc.Text(), "\t")
		if len(cols) != 4 {
			continue
		}
		kind, verdict, cmd := cols[0], cols[1], cols[2]
		if kind != "flagged" && kind != "exempt" {
			t.Fatalf("unknown row kind %q", kind)
		}
		rows++
		if kind == "flagged" && verdict != "agree" {
			t.Errorf("flagged %q differs from its reduction in Tcl", cmd)
		}
		path := filepath.Join(dir, "c.tcl")
		write(t, path, cmd+"\n")
		out, _, _ := run(path)
		got := strings.Count(out, " warning duplicate-logic ")
		if want := map[string]int{"flagged": 1, "exempt": 0}[kind]; got != want {
			t.Errorf("%s %q: got %q", kind, cmd, out)
		}
	}
	if rows == 0 {
		t.Fatal("no transcript rows")
	}
}

// A redefined if silences the condition and body findings of an if; one in a
// while test relies on while, and one in expr {…} on expr.
func TestDuplicateLogicShadowed(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		src  string
		want int
	}{
		{"proc if args {}\nif {$x} {a} else {a}\nif {$x} {} elseif {$x} {}\nif {$x || $x} {}\n", 0},
		{"proc if args {}\nwhile {$x || $x} {}\nexpr {$x || $x}\n", 2},
		{"proc expr args {}\nexpr {$x || $x}\nwhile {$x || $x} {}\n", 1},
		{"proc while args {}\nwhile {$x || $x} {}\n", 0},
	} {
		path := filepath.Join(dir, "s.tcl")
		write(t, path, tc.src)
		out, _, _ := run(path)
		if got := strings.Count(out, " duplicate-logic "); got != tc.want {
			t.Errorf("%q: got %d findings, want %d:\n%s", tc.src, got, tc.want, out)
		}
	}
}
