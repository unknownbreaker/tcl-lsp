package checkcmd

import (
	"path/filepath"
	"strings"
	"testing"
)

// A file with a self-assignment warning and an unreachable-code hint.
const togglesSrc = "set x $x\nproc p {} {\n    return\n    puts a\n}\n"

// --disable omits the named Checks' findings, and the exit status ignores them.
func TestDisableOmitsCheck(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.tcl")
	write(t, a, togglesSrc)
	hint := a + ":4:5: hint unreachable-code code after return never runs\n"
	warn := a + ":1:1: warning self-assignment assigns x its own value\n"
	for _, tc := range []struct {
		name   string
		args   []string
		out    string
		status int
	}{
		{"none disabled", []string{a}, warn + hint, exitWarnings},
		{"warning disabled", []string{"--disable", "self-assignment", a}, hint, exitClean},
		{"equals form", []string{"--disable=self-assignment", a}, hint, exitClean},
		{"hint disabled", []string{"--disable", "unreachable-code", a}, warn, exitWarnings},
		{"comma list", []string{"--disable", "self-assignment,unreachable-code", a}, "", exitClean},
		{"spaces in list", []string{"--disable", " self-assignment , unreachable-code ", a}, "", exitClean},
		{"repeated", []string{"--disable", "self-assignment", "--disable=unreachable-code", a}, "", exitClean},
		{"unknown id ignored", []string{"--disable", "no-such-check", a}, warn + hint, exitWarnings},
		{"unknown beside known", []string{"--disable", "no-such-check,self-assignment", a}, hint, exitClean},
		{"empty value", []string{"--disable=", a}, warn + hint, exitWarnings},
		{"before --", []string{"--disable", "self-assignment", "--", a}, hint, exitClean},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, status := run(tc.args...)
			if out != tc.out || errOut != "" || status != tc.status {
				t.Fatalf("got status %d stdout:\n%s\nstderr %q\nwant status %d stdout:\n%s", status, out, errOut, tc.status, tc.out)
			}
		})
	}
}

// --disable with no value, or with no path after it, is a usage error.
func TestDisableUsageErrors(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.tcl")
	write(t, a, togglesSrc)
	for _, args := range [][]string{
		{"--disable"},
		{"--disable", "self-assignment"},
		{"--disable", "self-assignment", "--"},
	} {
		if out, errOut, status := run(args...); status != exitUsage || out != "" || !strings.Contains(errOut, "usage") {
			t.Errorf("%q: status %d stdout %q stderr %q, want 2 and usage", args, status, out, errOut)
		}
	}
}

// After --, and after the first path, --disable is a path like any other.
func TestDisableAfterPathsIsAPath(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.tcl")
	write(t, a, togglesSrc)
	for _, args := range [][]string{
		{"--", "--disable", a},
		{a, "--disable", "self-assignment"},
	} {
		out, errOut, status := run(args...)
		if status != exitUsage || !strings.Contains(errOut, "--disable") || !strings.Contains(out, "self-assignment") {
			t.Errorf("%q: status %d stdout %q stderr %q, want 2, --disable unreadable, the file still checked", args, status, out, errOut)
		}
	}
}
