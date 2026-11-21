package tcl

import (
	"strings"
	"testing"
)

// AppendIfBodies returns an if command's bodies in order, the else body last
// with or without its keyword, and nothing for a command Tcl rejects.
func TestAppendIfBodies(t *testing.T) {
	for _, c := range []struct{ src, want string }{
		{`if {$x} {a}`, `{a}`},
		{`if {$x} then {a} else {b}`, `{a}|{b}`},
		{`if {$x} {a} {b}`, `{a}|{b}`},
		{`if {$x} {a} elseif {$y} then {b} else {c}`, `{a}|{b}|{c}`},
		{`if {$x} {a} elseif {$y} {b}`, `{a}|{b}`},
		{`::if {$x} {a} else {b}`, `{a}|{b}`},
		{`if {$x} {a} else`, ``},
		{`if {$x} {a} else {b} c`, ``},
		{`if {$x}{a}`, ``},
		{`while {$x} {a}`, ``},
		{`if {*}$args`, ``},
	} {
		cmds := Parse(c.src)
		if len(cmds) != 1 {
			t.Fatalf("%q: %d commands", c.src, len(cmds))
		}
		var got []string
		for _, w := range AppendIfBodies(nil, cmds[0].Words) {
			got = append(got, w.Text)
		}
		if g := strings.Join(got, "|"); g != c.want {
			t.Errorf("AppendIfBodies(%q) = %q, want %q", c.src, g, c.want)
		}
	}
}
