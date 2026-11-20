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

// The other modeled builtins run by their head's value too (tclsh 8.6 and 9.0):
// a catch or switch script binds, foreach, lmap and dict loops carry bindings
// across their back edge, and break ends the pass, in .tcl and .rvt (#126).
func TestModeledHeadsThroughResolver(t *testing.T) {
	for _, h := range []string{`"catch"`, "{*}{catch}", "::catch"} {
		runControlHead(t, h, "  "+h+" {set x [helper]}\n  puts $x\n", "$x\n}", []string{"x 0", "x [helper]"})
	}
	for _, h := range []string{`{switch}`, "::switch"} {
		runControlHead(t, h, "  "+h+" a {a {set x [helper]}}\n  puts $x\n", "$x\n}", []string{"x 0", "x [helper]"})
	}
	for _, h := range []string{`"foreach"`, "::lmap"} {
		runControlHead(t, h, "  "+h+" v {1 2} {\n    puts $x\n    set x [helper]\n  }\n", "$x\n    set", []string{"x 0", "x [helper]"})
	}
	for _, h := range []string{`{dict}`, "::dict"} {
		runControlHead(t, h, "  "+h+" for {k v} {a 1} {\n    puts $x\n    set x [helper]\n  }\n", "$x\n    set", []string{"x 0", "x [helper]"})
	}
	for _, h := range []string{`"break"`, "{*}{continue}"} {
		runControlHead(t, h, "  while {[helper]} {\n    "+h+"\n    set x 1\n  }\n  puts $x\n", "$x\n}", []string{"x 0"})
	}
	for _, h := range []string{`"eval"`, "::time"} {
		runControlHead(t, h, "  "+h+" {set x [helper]}\n  puts $x\n", "$x\n}", []string{"x 0", "x [helper]"})
	}
	// return ends the path, so the set before it never reaches past the loop.
	runControlHead(t, `"return"`, "  while {[helper]} {\n    set x 1\n    \"return\"\n  }\n  puts $x\n", "$x\n}", []string{"x 0"})
	// timerate traps the break in its script, as catch does.
	runControlHead(t, "{timerate}", "  while {[helper]} {\n    {timerate} {set x 1; break}\n    set x 2\n  }\n  puts $x\n", "$x\n}", []string{"x 0", "x 2"})
	// timerate reruns its script, so a use in it sees an earlier pass's binding.
	runControlHead(t, "::timerate", "  ::timerate {\n    puts $x\n    set x [helper]\n  }\n", "$x\n    set", []string{"x 0", "x [helper]"})
	// catch traps the break in its script, so the loop runs on to set x 2.
	for _, h := range []string{`"catch"`, "catch", "::try"} {
		trap := ""
		if h == "::try" {
			trap = " on break {} {}"
		}
		runControlHead(t, h, "  while {[helper]} {\n    "+h+" {set x 1; break}"+trap+"\n    set x 2\n  }\n  puts $x\n", "$x\n}", []string{"x 0", "x 2"})
	}
}

// A `::`-qualified catch, foreach or break names the global builtin, not a
// namespace's own proc of the same name (tclsh 8.6 and 9.0), so neither
// goto-definition nor find-references names ns::catch.
func TestQualifiedModeledHeadIsNotNamespaceProc(t *testing.T) {
	src := "namespace eval ns {\n  proc catch {a} {}\n  proc foreach {a b c} {}\n  proc break {} {}\n  proc helper {} {\n    ::catch {}\n    ::foreach v {} {}\n    while 1 {::break}\n    {*}::catch {}\n  }\n}\n"
	ix := index.New()
	ix.IndexFile("a.tcl", src)
	r := New(ix)
	for _, m := range []string{"::catch {}\n    ::", "::foreach v", "::break}", "{*}::catch"} {
		i := strings.Index(src, m)
		if i < 0 {
			t.Fatalf("mark %q not in source", m)
		}
		off := i + strings.Index(m, "::") + 2
		if locs := r.Definition("a.tcl", src, off); len(locs) != 0 {
			t.Errorf("goto-def on %q = %+v, want none", m, locs)
		}
	}
	for _, m := range []string{"catch {a", "foreach {a", "break {}"} {
		i := strings.Index(src, m)
		if i < 0 {
			t.Fatalf("mark %q not in source", m)
		}
		if locs := r.References("a.tcl", src, i); len(locs) != 0 {
			t.Errorf("find-references of %q = %+v, want none", m, locs)
		}
	}
}
