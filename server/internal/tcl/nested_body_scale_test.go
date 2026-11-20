package tcl

import (
	"strings"
	"testing"
	"time"
)

// nestedBodySource returns a proc that nests open n deep around a binding of o to
// an object of class ::C, closes each level with closer, then uses $o. It returns the
// offset of the use.
func nestedBodySource(n int, open, closer string) (src string, use int) {
	src = "proc p {} {\n set r " + strings.Repeat(open, n) + "[set o [::C #auto]]" + strings.Repeat(closer, n) + "\n $o m\n}\n"
	return src, strings.LastIndex(src, "$o") + 1
}

// TestNestedBodyScalesLinearly bounds ReachingAt and ClassOf after scripts nested
// through braced bodies (#96). Each level's bindings were collected again for every
// level above it, so 2,000 levels of `[catch {list [catch {...}]}]` took over 10 s.
// The bound is loose enough for a loaded machine. A switch still re-splits its arm
// list per level, so it is left out.
func TestNestedBodyScalesLinearly(t *testing.T) {
	const depth = 2000
	shapes := []struct{ name, open, close string }{
		{"catch", "[catch {list ", "}]"},
		{"try", "[try {list ", "} finally {}]"},
		{"catch body", "[catch {set r [list ", "]}]"},
	}
	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			src, use := nestedBodySource(depth, sh.open, sh.close)

			start := time.Now()
			defs, ok := reachingWithin(t, src, use)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("ReachingAt took %v, want well under 2s", took)
			}
			if want := strings.Index(src, "set o") + len("set "); !ok || len(defs) != 1 || defs[0].NameStart != want {
				t.Errorf("ReachingAt = %v, %v; want the one `set o` at %d", defs, ok, want)
			}

			start = time.Now()
			classes := ClassOf(src, use)
			if took := time.Since(start); took > 2*time.Second {
				t.Errorf("ClassOf took %v, want well under 2s", took)
			}
			if len(classes) != 1 || classes[0] != "::C" {
				t.Errorf("ClassOf = %v, want [::C]", classes)
			}
		})
	}
}

// TestCollectedBodyDistrustReplays pins that a remembered collection still
// distrusts. The outer catch collects the inner catch's body before the loop is
// analyzed. The loop's pass then reads that body again from the memo, and the walk
// itself stops at the continue, so only the replay sees the bracket script whose
// end cannot be trusted. It must still silence the earlier use in the loop, which a
// back edge may reach.
func TestCollectedBodyDistrustReplays(t *testing.T) {
	src := "proc f {c} {\n set x 0\n catch {\n  while {$c} {\n   puts $x\n   catch {continue; if {[string length \"]\"; set x 1]} {}}\n  }\n }\n}\n"
	off := strings.Index(src, "puts $x") + len("puts $")
	if defs, ok := ReachingAt(src, off); !ok || len(defs) != 0 {
		t.Errorf("ReachingAt = %v, %v; want silence (none, true)", defs, ok)
	}
}
