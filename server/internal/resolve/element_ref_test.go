package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// An element use answers the earliest binding wherever the cursor sits on it, in
// .tcl and .rvt alike: the flag comes from the reference, not from bytes after the
// cursor. Reaching on the base name would answer the second set instead.
func TestGotoDefArrayElementSameAnswerOnDollarAndName(t *testing.T) {
	body := "  set a(1) 1\n  set a(1) 2\n  puts $a(1)\n"
	wrap := map[string]string{
		"a.tcl": "proc f {} {\n" + body + "}\n",
		"p.rvt": "<h1>x</h1>\n<?\nproc f {} {\n" + body + "}\n?>\n",
	}
	for file, src := range wrap {
		ix := index.New()
		ix.IndexFile(file, src)
		r := New(ix)
		use := strings.Index(src, "$a")
		want := strings.Index(src, "a(1) 1")
		for _, off := range []int{use, use + 1} {
			locs := r.Definition(file, src, off)
			if len(locs) != 1 || locs[0].NameStart != want {
				t.Errorf("%s: goto-def at %d = %#v, want one location at %d", file, off, locs, want)
			}
		}
	}
}

// Element sites other than $a(k) take the same path. A name argument answers the
// earliest binding, like $a(1); reaching would answer the second set.
// $a::(k) is element k of the namespace array ::a:: (tclsh 8.6 and 9.0), so it
// must not answer the local a. A name with a paren that is not last (a(k)b, c()
// is a scalar of its own, never a binding or use of the base.
func TestGotoDefOtherElementSites(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"info exists element", "proc f {} {\n  set a(1) 1\n  set a(1) 2\n  info exists a(1)\n}", " a(1)\n}", []int{2}},
		{"qualified with trailing colons, on $", "proc f {} {\n  set a 1\n  set a 2\n  puts $a::(k)\n}", " $a::", nil},
		{"qualified with trailing colons, on name", "proc f {} {\n  set a 1\n  set a 2\n  puts $a::(k)\n}", "$a::", nil},
		{"qualified with trailing colons, scalar", "proc f {} {\n  set a 1\n  puts $a::\n}", "$a::", nil},
		{"name argument with paren not last", "proc f {} {\n  set a 1\n  info exists a(k)b\n}", " a(k)b", nil},
		{"set of an unclosed paren is not a binding of the base", "proc f {} {\n  set a 1\n  set a( 2\n  puts $a\n}", "$a\n", []int{2}},
	})
}

// A cursor on an element binding that also reads (incr, append, lappend) takes the
// element path too, not reaching. Reaching tracks only the base name, so inside the
// loop it would answer the incr itself, carried around from the previous iteration.
func TestDefinitionArrayElementReadModifyWriteSite(t *testing.T) {
	runDefinitionCases(t, []definitionCase{
		{"incr of an element in a loop", "proc f {} {\n  set n(a) 0\n  while {1} {\n    incr n(b)\n  }\n}", " n(b)", []int{2}},
		{"lappend of an element in a loop", "proc f {} {\n  set q(a) {}\n  foreach i {1 2} {\n    lappend q(b) $i\n  }\n}", " q(b)", []int{2}},
	})
}
