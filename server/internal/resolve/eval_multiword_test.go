package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// eval with more than one word runs its joined words as a script in this frame
// (tclsh 8.6 and 9.0: `set x 0; eval set x 1; puts $x` prints 1). Goto-definition
// after it stays silent rather than name only `set x 0`, while one-word eval keeps
// both bindings, in a .tcl proc and an .rvt page proc (#132).
func TestMultiWordEvalThroughResolver(t *testing.T) {
	for _, c := range []struct {
		name, line string
		want       []string
	}{
		{"bare words", "eval set x 1", nil},
		{"braced words", "eval {set x} {1}", nil},
		{"qualified head", "::eval set x 1", nil},
		{"one word", "eval {set x 1}", []string{"x 0", "x 1"}},
	} {
		body := "proc f {} {\n  set x 0\n  " + c.line + "\n  puts $x\n}\n"
		for _, f := range []struct{ name, file, src string }{
			{"tcl proc", "a.tcl", body},
			{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
		} {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(f.file, f.src)
				at := func(mark string) int {
					off := strings.Index(f.src, mark)
					if off < 0 {
						t.Fatalf("mark %q not in source", mark)
					}
					return off
				}
				locs := New(ix).Definition(f.file, f.src, at("$x\n}")+1)
				if len(locs) != len(c.want) {
					t.Fatalf("Definition at $x = %v, want %d bindings %v", locs, len(c.want), c.want)
				}
				got := map[int]bool{}
				for _, l := range locs {
					got[l.NameStart] = true
				}
				for _, m := range c.want {
					if !got[at(m)] {
						t.Errorf("Definition at $x = %v, missing the set at %q", locs, m)
					}
				}
			})
		}
	}
}

// The braced words of a multi-word eval are part of its joined script, never a
// decorated proc's body or a scope of their own, so goto-definition on a use in one
// stays silent, in a .tcl file and an .rvt page, inside a proc and at page level
// (#140). tclsh 8.6 and 9.0: x is 0 at each puts, and no proc g exists.
func TestMultiWordEvalBracedWordThroughResolver(t *testing.T) {
	for _, c := range []struct{ name, code string }{
		{"decorator in a proc", "proc f {} {\n  set x 0\n  eval DECO proc g {} {set x 1; puts $x}\n}\n"},
		{"decorator at page level", "set x 0\neval DECO proc g {} {set x 1; puts $x}\n"},
		{"list in a proc", "proc f {} {\n  set x 0\n  eval list {set x 1; puts $x}\n}\n"},
		{"list at page level", "set x 0\neval list {set x 1; puts $x}\n"},
		{"list in a namespace", "namespace eval ns {\n  set x 0\n  eval list {set x 1; puts $x}\n}\n"},
	} {
		for _, f := range []struct{ name, file, src string }{
			{"tcl", "a.tcl", c.code},
			{"rvt", "p.rvt", "<h1>x</h1>\n<?\n" + c.code + "?>\n"},
		} {
			t.Run(c.name+"/"+f.name, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(f.file, f.src)
				use := strings.LastIndex(f.src, "$x}")
				if use < 0 {
					t.Fatalf("mark $x} not in source")
				}
				if locs := New(ix).Definition(f.file, f.src, use+1); len(locs) != 0 {
					t.Errorf("Definition at $x = %v, want silent\nsource:\n%s", locs, f.src)
				}
			})
		}
	}
}

// `eval DECO proc g {} {...}` defines no proc g, so a call to g resolves to nothing
// in either file kind (#140).
func TestMultiWordEvalDecoratorDefinesNoProcThroughResolver(t *testing.T) {
	for _, f := range []struct{ name, file, src string }{
		{"tcl", "a.tcl", "eval DECO proc g {} {set x 1}\ng\n"},
		{"rvt", "p.rvt", "<?\neval DECO proc g {} {set x 1}\n?>\n<? g ?>\n"},
	} {
		t.Run(f.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(f.file, f.src)
			call := strings.LastIndex(f.src, "g\n")
			if strings.HasSuffix(f.file, ".rvt") {
				call = strings.LastIndex(f.src, " g ") + 1
			}
			if locs := New(ix).Definition(f.file, f.src, call); len(locs) != 0 {
				t.Errorf("Definition at the call to g = %v, want none\nsource:\n%s", locs, f.src)
			}
		})
	}
}
