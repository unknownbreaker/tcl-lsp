package tcl

import (
	"strings"
	"testing"
	"time"
)

// nestedSubstSource returns `set r [list a [list a ... $x ...]]` nested n deep
// inside a proc that binds x, and the offsets of the `$x` inside it and the one
// after it.
func nestedSubstSource(n int) (src string, inner, after int) {
	src = "proc p {} {\n set x 1\n set r " + strings.Repeat("[list a ", n) + "$x" + strings.Repeat("]", n) + "\n puts $x\n}\n"
	return src, strings.Index(src, "$x") + 1, strings.LastIndex(src, "$x") + 1
}

// TestNestedSubstScalesLinearly bounds FileRefs (Parse + CommandRefs) and
// ReachingAt over a command substitution nested 20,000 deep (#84). Each level used
// to re-scan the span below it: FileRefs took about 34 s and ReachingAt over 10 s.
// The bound is loose enough for a loaded machine.
func TestNestedSubstScalesLinearly(t *testing.T) {
	const depth = 20000
	src, inner, after := nestedSubstSource(depth)

	start := time.Now()
	refs := FileRefs(src)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("FileRefs took %v, want well under 2s", took)
	}
	var lists int
	var xs []int
	for _, cr := range refs {
		r := cr.Ref
		switch {
		case r.Kind == RefCommand && r.Name == "list":
			lists++
		case r.Kind == RefVariable && r.Name == "x":
			xs = append(xs, r.Start)
		}
	}
	if lists != depth {
		t.Errorf("FileRefs found %d `list` calls, want %d", lists, depth)
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
}

// TestNestedSubstObjMethodScalesLinearly bounds the `$obj method` finder on a
// `[$obj m]` call at the bottom of the same 20,000-deep nesting.
func TestNestedSubstObjMethodScalesLinearly(t *testing.T) {
	const depth = 20000
	src := "proc p {obj} {\n set r " + strings.Repeat("[list a ", depth) + "[$obj m]" + strings.Repeat("]", depth) + "\n}\n"
	off := strings.Index(src, "$obj m") + len("$obj ")
	start := time.Now()
	ro, mn, ok := ObjMethodAt(src, off)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("ObjMethodAt took %v, want well under 2s", took)
	}
	if want := strings.Index(src, "$obj m") + 1; !ok || ro != want || mn != "m" {
		t.Errorf("ObjMethodAt = %d, %q, %v; want %d, \"m\", true", ro, mn, ok, want)
	}
}
