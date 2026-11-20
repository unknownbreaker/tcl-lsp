package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// Tcl runs if, while and for for any head whose value names them (tclsh 8.6 and
// 9.0), so goto-definition after their bodies names the bindings in them, a loop
// carries bindings across its back edge, and a call in a condition is a reference,
// in .tcl and .rvt (#117).
func TestControlHeadsThroughResolver(t *testing.T) {
	for _, h := range []string{`"if"`, "{*}{if}", "::if"} {
		runControlHead(t, h, "  "+h+" {[helper]} {set x 1}\n  puts $x\n", "$x\n}", []string{"x 0", "x 1"})
	}
	for _, h := range []string{`"while"`, "::while"} {
		runControlHead(t, h, "  "+h+" {[helper]} {\n    puts $x\n    set x 1\n  }\n", "$x\n    set", []string{"x 0", "x 1"})
	}
	for _, h := range []string{`{for}`, "::for"} {
		runControlHead(t, h, "  "+h+" {} {[helper]} {} {\n    puts $x\n    set x 1\n  }\n", "$x\n    set", []string{"x 0", "x 1"})
	}
}

func runControlHead(t *testing.T, h, tail, use string, want []string) {
	t.Helper()
	body := "proc helper {} { return 0 }\n" +
		"proc f {} {\n" +
		"  set x 0\n" +
		tail +
		"}\n"
	for _, c := range []struct{ name, file, src string }{
		{"tcl proc", "a.tcl", body},
		{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
	} {
		t.Run(h+"/"+c.name, func(t *testing.T) {
			ix := index.New()
			ix.IndexFile(c.file, c.src)
			r := New(ix)
			at := func(mark string) int {
				off := strings.Index(c.src, mark)
				if off < 0 {
					t.Fatalf("mark %q not in source", mark)
				}
				return off
			}
			locs := r.Definition(c.file, c.src, at(use)+1)
			got := map[int]bool{}
			for _, l := range locs {
				got[l.NameStart] = true
			}
			if len(locs) != len(want) {
				t.Errorf("Definition at %q = %v, want %d bindings", use, locs, len(want))
			}
			for _, m := range want {
				if !got[at(m)] {
					t.Errorf("Definition at %q = %v, missing the set at %d (%q)", use, locs, at(m), m)
				}
			}
			call := at("helper]")
			found := false
			for _, l := range r.References(c.file, c.src, at("helper")) {
				if l.NameStart == call {
					found = true
				}
			}
			if !found {
				t.Errorf("References to helper miss the call at %d in the condition", call)
			}
		})
	}
}

// A `::`-qualified head names the global builtin, not a namespace's own proc of
// the same name: in ns, `::while 0 {}` runs the builtin (tclsh 8.6 and 9.0). The
// analysis reads the head as bare `while`, but its call reference keeps the
// written name, so neither goto-definition nor find-references names ns::while.
func TestQualifiedControlHeadIsNotNamespaceProc(t *testing.T) {
	src := "namespace eval ns {\n  proc while {a b} {}\n  proc for {a b c d} {}\n  proc if {a b} {}\n  proc helper {} {\n    ::while 0 {}\n    ::for {} 0 {} {}\n    ::if 0 {}\n    {*}::while 0 {}\n  }\n}\n"
	ix := index.New()
	ix.IndexFile("a.tcl", src)
	r := New(ix)
	for _, m := range []string{"::while 0", "::for {}", "::if 0", "{*}::while"} {
		i := strings.Index(src, m)
		if i < 0 {
			t.Fatalf("mark %q not in source", m)
		}
		off := i + strings.Index(m, "::") + 2
		if locs := r.Definition("a.tcl", src, off); len(locs) != 0 {
			t.Errorf("goto-def on %q = %+v, want none", m, locs)
		}
	}
	for _, m := range []string{"while {a", "for {a", "if {a"} {
		i := strings.Index(src, m)
		if i < 0 {
			t.Fatalf("mark %q not in source", m)
		}
		if locs := r.References("a.tcl", src, i); len(locs) != 0 {
			t.Errorf("find-references of %q = %+v, want none", m, locs)
		}
	}
}
