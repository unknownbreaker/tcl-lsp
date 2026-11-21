package checkcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// attackRun writes files (name -> content) into a fresh dir and runs check on it.
func attackRun(t *testing.T, files map[string]string, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(args) == 0 {
		args = []string{dir}
	} else {
		for i, a := range args {
			if !filepath.IsAbs(a) {
				args[i] = filepath.Join(dir, a)
			}
		}
	}
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return out.String(), errb.String(), code
}

// Each source makes `set x $x` call user code, or not run as a command at all
// (tclsh 8.6 and 9.0). A finding is
// wrong for every one.
func TestNoFalsePositives(t *testing.T) {
	cases := map[string]string{
		// set redefined by forms the index does not count as defining `set`.
		"proc braced name": "rename set _s\nproc {set} {a b} {_s ::log $a}\nset x $x\n",
		"proc quoted name": "rename set _s\nproc \"set\" {a b} {_s ::log $a}\nset x $x\n",
		"rename to set":    "rename set _s\nproc myset {a b} {_s ::log $a}\nrename myset set\nset x $x\n",
		"interp alias":     "rename set _s\nproc myset {a b} {_s ::log $a}\ninterp alias {} set {} myset\nset x $x\n",
		// The body's command is user code: its braced word is data.
		"redefined foreach": "rename foreach _f\nproc foreach {args} {return $args}\nforeach i {1 2} {set x $x}\n",
		"redefined catch":   "rename catch _c\nproc catch {args} {return $args}\ncatch {set x $x}\n",
		// namespace eval joins its script words: the last word is not a script.
		"namespace eval joined list": "set x 1\nnamespace eval foo {list} {set x $x}\n",
		"namespace eval joined cmd":  "proc bar {args} {return $args}\nnamespace eval foo bar {set x $x}\n",
		// Malformed: proc with 5 words is a wrong # args error, never a body.
		"proc wrong arg count": "proc p a b {set x $x}\n",
	}
	for name, src := range cases {
		t.Run(name, func(t *testing.T) {
			out, _, code := attackRun(t, map[string]string{"a.tcl": src})
			if out != "" || code != 0 {
				t.Errorf("source:\n%s\ngot exit %d, output:\n%s", src, code, out)
			}
		})
	}
}

// The same file named twice, spelled differently, is still one file.
func TestSameFileTwoSpellings(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.tcl")
	if err := os.WriteFile(p, []byte("set x $x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wd, _ := os.Getwd()
	defer os.Chdir(wd)
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run([]string{"a.tcl", p, dir}, &out, &errb)
	if n := strings.Count(out.String(), "self-assignment"); n != 1 || code != 1 {
		t.Errorf("want 1 finding and exit 1, got %d findings, exit %d:\n%s", n, code, out.String())
	}
}

// Output for a file with many findings must not be quadratic in file size.
func TestManyFindingsScale(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	var b strings.Builder
	for i := 0; i < 20000; i++ {
		fmt.Fprintf(&b, "set x%d $x%d\n", i, i)
	}
	start := time.Now()
	out, _, code := attackRun(t, map[string]string{"many.tcl": b.String()})
	el := time.Since(start)
	if code != 1 || strings.Count(out, "\n") != 20000 {
		t.Fatalf("exit %d, %d lines", code, strings.Count(out, "\n"))
	}
	t.Logf("20000 findings in %v", el)
	if el > 3*time.Second {
		t.Errorf("checking a 20000-line file took %v", el)
	}
}

// Exit codes: a missing path is 2 even when another path warned; the warning is
// still printed.
func TestExitCodes(t *testing.T) {
	out, errOut, code := attackRun(t, map[string]string{"a.tcl": "set x $x\n"}, "a.tcl", "missing.tcl")
	if code != 2 || !strings.Contains(out, "self-assignment") || errOut == "" {
		t.Errorf("exit %d out %q err %q", code, out, errOut)
	}
	var o, e bytes.Buffer
	if c := Run([]string{"--"}, &o, &e); c != 2 {
		t.Errorf("`check --` exit %d, want 2", c)
	}
	// A file named directly with another extension is checked; in a directory it is not.
	out, _, code = attackRun(t, map[string]string{"a.txt": "set x $x\n", ".git/b.tcl": "set y $y\n", "d.tcl/c.tcl": "set z $z\n"})
	if code != 1 || strings.Contains(out, "a.txt") || strings.Contains(out, "b.tcl") || !strings.Contains(out, "c.tcl") {
		t.Errorf("dir walk: exit %d out %q", code, out)
	}
}

// docs/CHECKS.md: "an unterminated word, brace or bracket gives no finding".
// tclsh 8.6 and 9.0 never run the [set] in an unterminated quoted word.
func TestUnterminatedQuoteSilent(t *testing.T) {
	src := "puts \"[set x $x]\n"
	out, _, code := attackRun(t, map[string]string{"a.tcl": src})
	if out != "" || code != 0 {
		t.Errorf("unterminated quote: exit %d, output:\n%s", code, out)
	}
}
