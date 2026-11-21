package checkcmd

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run is Run with captured output.
func run(args ...string) (stdout, stderr string, status int) {
	var o, e bytes.Buffer
	status = Run(args, &o, &e)
	return o.String(), e.String(), status
}

// Every fixture under testdata/fixtures is checked on its own, and its output
// and exit status must match testdata/golden/<fixture>.out. CHECK_UPDATE=1
// rewrites the goldens.
func TestFixturesGolden(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/fixtures/*")
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	update := os.Getenv("CHECK_UPDATE") == "1"
	for _, f := range fixtures {
		out, errOut, status := run(f)
		if errOut != "" {
			t.Errorf("%s: stderr %q", f, errOut)
		}
		got := fmt.Sprintf("%sexit %d\n", out, status)
		golden := filepath.Join("testdata/golden", filepath.Base(f)+".out")
		if update {
			if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("%s: %v (run with CHECK_UPDATE=1)", f, err)
		}
		if got != string(want) {
			t.Errorf("%s: output differs from %s:\n got:\n%s\nwant:\n%s", f, golden, got, want)
		}
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A directory is walked for .tcl and .rvt files in lexical order, skipping
// .git and other extensions.
func TestWalksDirectories(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "b.tcl"), "set b $b\n")
	write(t, filepath.Join(dir, "sub", "a.rvt"), "<? set a $a ?>\n")
	write(t, filepath.Join(dir, "a.tcl"), "set a $a\n")
	write(t, filepath.Join(dir, "notes.txt"), "set n $n\n")
	write(t, filepath.Join(dir, "lib.tm"), "set m $m\n")
	write(t, filepath.Join(dir, ".git", "hook.tcl"), "set g $g\n")

	out, errOut, status := run(dir)
	want := strings.Join([]string{
		filepath.Join(dir, "a.tcl") + ":1:1: warning self-assignment assigns a its own value",
		filepath.Join(dir, "b.tcl") + ":1:1: warning self-assignment assigns b its own value",
		filepath.Join(dir, "sub", "a.rvt") + ":1:4: warning self-assignment assigns a its own value",
	}, "\n") + "\n"
	if out != want || errOut != "" || status != exitWarnings {
		t.Fatalf("got status %d stdout:\n%s\nstderr: %q\nwant status 1 stdout:\n%s", status, out, errOut, want)
	}
}

// A file named directly is checked whatever its extension, once even when a
// directory argument also covers it.
func TestNamedFilesAndDuplicates(t *testing.T) {
	dir := t.TempDir()
	tm := filepath.Join(dir, "lib.tm")
	a := filepath.Join(dir, "a.tcl")
	write(t, tm, "set m $m\n")
	write(t, a, "set a $a\n")
	out, _, status := run(tm, dir, a)
	want := tm + ":1:1: warning self-assignment assigns m its own value\n" +
		a + ":1:1: warning self-assignment assigns a its own value\n"
	if out != want || status != exitWarnings {
		t.Fatalf("got status %d:\n%s\nwant:\n%s", status, out, want)
	}
}

// A builtin one file redefines is silenced in every file of the run, whichever
// comes first.
func TestRedefinitionInAnotherFileSilences(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.tcl"), "set x $x\n")
	write(t, filepath.Join(dir, "z.tcl"), "namespace eval ::u { proc set {args} {} }\n")
	if out, _, status := run(dir); out != "" || status != exitClean {
		t.Fatalf("got status %d stdout %q, want 0 and nothing", status, out)
	}
	write(t, filepath.Join(dir, "z.tcl"), "itcl::class K { method set {args} {} }\n")
	if out, _, status := run(dir); out != "" || status != exitClean {
		t.Fatalf("method named set: got status %d stdout %q, want 0 and nothing", status, out)
	}
}

func TestCleanRunExitsZero(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.tcl"), "set x 1\n")
	if out, errOut, status := run(dir); out != "" || errOut != "" || status != exitClean {
		t.Fatalf("got status %d stdout %q stderr %q", status, out, errOut)
	}
}

func TestUsageErrors(t *testing.T) {
	if _, errOut, status := run(); status != exitUsage || !strings.Contains(errOut, "usage") {
		t.Errorf("no args: status %d stderr %q", status, errOut)
	}
	if _, errOut, status := run("--"); status != exitUsage || !strings.Contains(errOut, "usage") {
		t.Errorf("only --: status %d stderr %q", status, errOut)
	}
	dir := t.TempDir()
	a := filepath.Join(dir, "a.tcl")
	write(t, a, "set a $a\n")
	missing := filepath.Join(dir, "missing.tcl")
	out, errOut, status := run(missing, a)
	if status != exitUsage || !strings.Contains(errOut, "missing.tcl") {
		t.Errorf("missing path: status %d stderr %q, want 2 naming it", status, errOut)
	}
	if !strings.Contains(out, "assigns a its own value") {
		t.Errorf("missing path: the readable file was not checked: %q", out)
	}
}

// Columns count bytes, as Vim's quickfix %c does.
func TestColumnsCountBytes(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.tcl")
	write(t, a, "puts \"é\"; set x $x\n")
	out, _, _ := run(a)
	if want := a + ":1:12: warning"; !strings.HasPrefix(out, want) {
		t.Fatalf("got %q, want prefix %q", out, want)
	}
}
