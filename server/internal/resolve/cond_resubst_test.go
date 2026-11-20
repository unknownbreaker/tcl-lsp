package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// An unbraced if, while or for condition is substituted again when it is
// evaluated, so an escaped bracket or a substituted value may form a script that
// binds (tclsh 8.6 and 9.0). Goto-def stays silent in the body and after the
// command, while a use read in the condition word keeps its answer, and a braced
// condition keeps every answer, in .tcl and .rvt.
func TestCondResubstThroughResolver(t *testing.T) {
	for _, shape := range []struct {
		name, cmd string
		silent    bool
	}{
		{"if escaped brackets", `if "\[set x 1\] && $x" {puts $x}`, true},
		{"if value", `if $s {puts $x}`, true},
		{"elseif value", `if 0 {} elseif "$x && $s" {puts $x}`, true},
		{"while value", `while "$x && $s" {puts $x}`, true},
		{"for value", `for {} "$x && $s" {} {puts $x}`, true},
		{"braced if", `if {$x && $s} {puts $x}`, false},
		{"braced while", `while {$x && $s} {puts $x}`, false},
	} {
		body := "proc f {s} {\n" +
			"  set x 0\n" +
			"  " + shape.cmd + "\n" +
			"  puts $x\n" +
			"}\n"
		for _, c := range []struct{ name, file, src string }{
			{"tcl proc", "a.tcl", body},
			{"proc in an rvt page", "p.rvt", "<h1>x</h1>\n<?\n" + body + "?>\n"},
		} {
			t.Run(shape.name+"/"+c.name, func(t *testing.T) {
				ix := index.New()
				ix.IndexFile(c.file, c.src)
				r := New(ix)
				at := func(mark string) int {
					off := strings.Index(c.src, mark)
					if off < 0 {
						t.Fatalf("mark %q not in source", mark)
					}
					return off + 1 // the name, past '$'
				}
				bind := at("x 0") - 1
				answers := func(where string, off int) {
					t.Helper()
					if locs := r.Definition(c.file, c.src, off); len(locs) != 1 || locs[0].NameStart != bind {
						t.Errorf("Definition %s = %v, want the set at %d", where, locs, bind)
					}
				}
				cmd := strings.Index(c.src, shape.cmd)
				if i := strings.Index(shape.cmd, "$x"); i >= 0 && i < strings.Index(shape.cmd, "puts") {
					answers("in the condition", cmd+i+1)
				}
				for _, use := range []struct{ where, mark string }{
					{"in the body", "$x}"},
					{"after the command", "$x\n}"},
				} {
					if shape.silent {
						if locs := r.Definition(c.file, c.src, at(use.mark)); len(locs) != 0 {
							t.Errorf("Definition %s = %v, want silent", use.where, locs)
						}
					} else {
						answers(use.where, at(use.mark))
					}
				}
			})
		}
	}
}
