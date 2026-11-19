package tcl

import (
	"strings"
	"testing"
)

// A `$obj method` call is found only where Tcl runs it: in a command word's
// [script], which ends where Tcl ends it, and not in text a `]` hidden in braces
// or quotes would have cut loose from the script.
func TestObjMethodAtHiddenBracket(t *testing.T) {
	for _, c := range []struct {
		name, src, mark string
		want            bool
	}{
		{"a braced literal after a hidden bracket", "puts \"[list {]} {[$o m]}]\"", "m]", false},
		{"a call after a hidden bracket", "puts \"[list {]} [$o m]]\"", "m]", true},
		{"a call in a balanced script", "puts \"[list [$o m]]\"", "m]", true},
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
