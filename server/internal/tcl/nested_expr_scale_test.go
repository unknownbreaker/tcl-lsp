package tcl

import (
	"strings"
	"testing"
	"time"
)

// nestedExprSource returns `set r [expr {1+[expr {1+ ... $x ...}]}]` nested n
// deep inside a proc that binds x, and the offsets of the `$x` inside it and the
// one after it.
func nestedExprSource(n int) (src string, inner, after int) {
	src = "proc p {} {\n set x 1\n set r " + strings.Repeat("[expr {1+", n) + "$x" + strings.Repeat("}]", n) + "\n puts $x\n}\n"
	return src, strings.Index(src, "$x") + 1, strings.LastIndex(src, "$x") + 1
}

// TestNestedExprScalesLinearly bounds FileRefs and ReachingAt over a command
// substitution nested 8,000 deep through braced expressions (#95). The bound is
// loose enough for a loaded machine.
func TestNestedExprScalesLinearly(t *testing.T) {
	const depth = 8000
	src, inner, after := nestedExprSource(depth)

	start := time.Now()
	refs := FileRefs(src)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("FileRefs took %v, want well under 2s", took)
	}
	var exprs int
	var xs []int
	for _, cr := range refs {
		r := cr.Ref
		switch {
		case r.Kind == RefCommand && r.Name == "expr":
			exprs++
		case r.Kind == RefVariable && r.Name == "x":
			xs = append(xs, r.Start)
		}
	}
	if exprs != depth {
		t.Errorf("FileRefs found %d `expr` calls, want %d", exprs, depth)
	}
	if len(xs) != 2 || xs[0] != inner-1 || xs[1] != after-1 {
		t.Errorf("FileRefs found $x at %v, want [%d %d]", xs, inner-1, after-1)
	}

	for _, off := range []int{inner, after} {
		start = time.Now()
		defs, ok := reachingWithin(t, src, off)
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("ReachingAt(%d) took %v, want well under 2s", off, took)
		}
		if !ok || len(defs) != 1 {
			t.Errorf("ReachingAt(%d) = %v, %v; want the one `set x 1`", off, defs, ok)
		}
	}

	// A `$obj method` call at the bottom of the same nesting is found (#100).
	objSrc := "proc p {} {\n set o [C #auto]\n set r " + strings.Repeat("[expr {1+", depth) + "[$o m]" + strings.Repeat("}]", depth) + "\n}\n"
	start = time.Now()
	_, name, ok := ObjMethodAt(objSrc, strings.Index(objSrc, "$o m")+3)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("ObjMethodAt took %v, want well under 2s", took)
	}
	if !ok || name != "m" {
		t.Errorf("ObjMethodAt = %q, %v; want \"m\", true", name, ok)
	}
}

// TestNestedIfScalesLinearly bounds FileRefs over `if {1} {list ...}` nested
// 8,000 deep in a bracket script, the condition shape of #95. Before #95 this
// took well over 2s.
func TestNestedIfScalesLinearly(t *testing.T) {
	const depth = 8000
	src := "proc p {} {\n set x 1\n set r " + strings.Repeat("[if {[string length 1]} {list ", depth) + "$x" + strings.Repeat("}]", depth) + "\n puts $x\n}\n"
	start := time.Now()
	refs := FileRefs(src)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("FileRefs took %v, want well under 2s", took)
	}
	var ifs int
	for _, cr := range refs {
		if cr.Ref.Kind == RefCommand && cr.Ref.Name == "if" {
			ifs++
		}
	}
	if ifs != depth {
		t.Errorf("FileRefs found %d `if` calls, want %d", ifs, depth)
	}
}

// TestNestedExprShapesScaleLinearly bounds FileRefs, ReachingAt and ClassOf on
// other shapes of scripts nested in expressions (#95): lazy operators, an array
// index, a quoted word, an if condition, a literal `[` in a brace group, and an
// expression nested in a command's word. Each source stays under the reaching
// size cap, so ReachingAt answers. The literal `[` throws the depth count off, so
// that span is distrusted and reaching stays silent, as it did before.
func TestNestedExprShapesScaleLinearly(t *testing.T) {
	const depth = 4000
	for _, sh := range []struct {
		name, open, close string
		silent            bool
	}{
		{"and", "[expr {1 && [", "]}]", false},
		{"ternary", "[expr {1 ? [", "] : 0}]", false},
		{"array index", "[expr {$a([", "])}]", false},
		{"quoted", "[expr {\"[", "]\"}]", false},
		{"if condition", "[if {[string length ", "]} {list 1}]", false},
		{"while condition", "[while {[string length ", "]} {break}]", false},
		{"brace literal", "[expr {{[}+", "}]", true},
		{"command word", "[list [expr {1+", "}]]", false},
	} {
		t.Run(sh.name, func(t *testing.T) {
			src := "proc p {} {\n set x 1\n set r " + strings.Repeat(sh.open, depth) + "$x" + strings.Repeat(sh.close, depth) + "\n puts $x\n}\n"
			after := strings.LastIndex(src, "$x") + 1
			start := time.Now()
			FileRefs(src)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("FileRefs took %v, want well under 2s", took)
			}
			start = time.Now()
			defs, ok := reachingWithin(t, src, after)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("ReachingAt took %v, want well under 2s", took)
			}
			if sh.silent {
				if !ok || len(defs) != 0 {
					t.Errorf("ReachingAt = %v, %v; want silence (none, true)", defs, ok)
				}
			} else if !ok || len(defs) != 1 {
				t.Errorf("ReachingAt = %v, %v; want the one `set x 1`", defs, ok)
			}
			start = time.Now()
			ClassOf(src, after)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("ClassOf took %v, want well under 2s", took)
			}
		})
	}
}
