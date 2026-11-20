package tcl

import (
	"strings"
	"testing"
)

// #100: a `$obj method` call inside a braced expression (an if/while/for
// condition, an expr argument) is found where Tcl runs it, as in a bracket
// script. An expression span whose `]` may be hidden stays silent.
func TestObjMethodAtInBracedExpressions(t *testing.T) {
	for _, c := range []struct {
		name, src, mark string
		want            bool
	}{
		{"bracket script", "set r [list [$o m]]", "m]", true},
		{"expr argument", "set r [expr {1+[$o m]}]", "m]", true},
		{"if condition", "if {[$o m]} {}", "m]", true},
		{"elseif condition", "if {0} {} elseif {[$o m]} {}", "m]", true},
		{"while condition", "while {[$o m]} {}", "m]", true},
		{"for test", "for {set i 0} {[$o m]} {incr i} {}", "m]", true},
		{"after short circuit", "if {$a && [$o m]} {}", "m]", true},
		{"ternary arm", "set r [expr {$a ? [$o m] : 0}]", "m]", true},
		{"nested in expr script", "if {[f [$o m]]} {}", "m]", true},
		{"in a proc body", "proc p {} {\n set o [C #auto]\n if {[$o m]} {}\n}", "m]", true},
		{"in a method body", "itcl::class C {\n method r {} {\n  while {[$this m]} {}\n }\n}", "m]", true},
		// Tcl 8.6 and 9.0 run m in both: a braced or quoted operand is a literal.
		{"bracket in a braced operand", "expr {{[}+[$o m]}", "m]", true},
		{"bracket in a quoted operand", "expr {\"]\"+[$o m]}", "m]", true},
		// A `]` in a script's quotes may end the script for the depth count, so
		// the expression is distrusted from there on: silent, never wrong.
		{"distrusted script before", "if {[string length \"]\"] && [$o m]} {}", "m]", false},
		{"distrusted script around", "if {[list \"]\" [$o m]]} {}", "m]", false},
		// Several expr words mean what their join means (Tcl 8.6 and 9.0).
		{"multi-word expr joined into a literal", "expr \"{\" {[$o m]} \"}\"", "m]", false},
		{"multi-word expr syntax error", "expr {1} {[$o m]}", "m]", false},
		{"multi-word expr unbraced script", "expr 1 + [$o m]", "m]", true},
		{"braced literal operand", "expr {{[$o m]}}", "m]", false},
		{"braced if body is not an expression", "if {1} {list {[$o m]}}", "m]", false},
		{"non-expression braced word", "list {[$o m]}", "m]", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			off := strings.Index(c.src, c.mark)
			if off < 0 {
				t.Fatalf("mark %q not in source", c.mark)
			}
			_, name, ok := ObjMethodAt(c.src, off)
			if ok != c.want || (ok && name != "m") {
				t.Errorf("ObjMethodAt(%q at %q) = %q, %v; want found=%v", c.src, c.mark, name, ok, c.want)
			}
		})
	}
}
