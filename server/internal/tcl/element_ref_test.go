package tcl

import "testing"

// A variable reference says whether it names an array element, so the resolver
// can read the flag instead of re-scanning source bytes after the name.
func TestReferenceElementFlag(t *testing.T) {
	cases := []struct {
		src, name string
		element   bool
	}{
		{"puts $s", "s", false},
		{"puts $a(1)", "a", true},
		{"puts $a($i,$j)", "a", true},
		{"puts $ns::a(k)", "ns::a", true},
		{"puts $::g(k)", "::g", true},
		{"puts ${b}", "b", false},
		{"puts ${b(x)}", "b(x)", true}, // Tcl: ${b(x)} is element x of array b
		{"puts \"$q(x)\"", "q", true},
		{"puts [list $n(1)]", "n", true},
		{"info exists e(x)", "e", true},
		{"info exists e", "e", false},
		{"unset -nocomplain u(1) v", "u", true},
		{"unset -nocomplain u(1) v", "v", false},
		{"array get w", "w", false},
		{"puts $a::(k)", "a::", true}, // Tcl: element k of namespace array ::a::
		{"puts $a::-x", "a::", false}, // Tcl: a trailing :: belongs to the name
		{"puts $a::", "a::", false},
	}
	for _, c := range cases {
		t.Run(c.src+"/"+c.name, func(t *testing.T) {
			found := false
			for _, cmd := range Parse(c.src) {
				for _, r := range CommandRefs(cmd) {
					if r.Kind != RefVariable || r.Name != c.name {
						continue
					}
					found = true
					if r.Element != c.element {
						t.Errorf("Element = %v, want %v (ref %+v)", r.Element, c.element, r)
					}
				}
			}
			if !found {
				t.Fatalf("no variable reference named %q in %q", c.name, c.src)
			}
		})
	}
}

// Tcl names an array element only when the name ends in `)`: `a(k)b` and `c(` are
// scalars (tclsh 8.6 and 9.0 list them in [info locals]), so neither binds or
// references the base `a`/`c`.
func TestElementNameNeedsTrailingParen(t *testing.T) {
	for _, src := range []string{"info exists a(k)b\n", "unset c(\n"} {
		for _, r := range CommandRefs(Parse(src)[0]) {
			if r.Kind == RefVariable {
				t.Errorf("%q: ref %+v, want none (scalar name with a paren)", src, r)
			}
		}
	}
	for _, d := range FileDefs("proc f {} {\n  set a(k)b 1\n  set c( 2\n  incr d(x\n}\n") {
		if d.Kind == DefLocal {
			t.Errorf("local binding %+v, want none (scalar names with a paren)", d)
		}
	}
	if v, _, _ := parseVarRef("${a(k)b}", 0, 0); v.Element {
		t.Errorf("${a(k)b} flagged Element, want scalar")
	}
}
