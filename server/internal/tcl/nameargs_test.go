package tcl

import (
	"strings"
	"testing"
)

// varRefAt returns true if refs contains a RefVariable with the given name whose
// Start equals the offset of needle in src.
func varRefAt(src string, refs []ContextRef, name, needle string) bool {
	off := strings.Index(src, needle)
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && r.Ref.Name == name && r.Ref.Start == off {
			return true
		}
	}
	return false
}

// The reported shape: an info-exists guard on a global array element inside an
// if-condition. The BASE name becomes a variable reference (covering only the
// base, not the subscript); the dynamic subscript's $var is still its own ref.
func TestNameArgInfoExistsGuard(t *testing.T) {
	src := "proc check {stuff} {\n  if {[info exists ::something($stuff)]} {\n    puts yes\n  }\n}\n"
	refs := FileRefs(src)
	if !varRefAt(src, refs, "::something", "::something") {
		t.Errorf("info exists base name not a var ref: %#v", refs)
	}
	if !varRefAt(src, refs, "stuff", "$stuff") { // $-refs anchor at the '$'
		t.Errorf("dynamic subscript $stuff lost its own ref")
	}
}

// unset: every non-flag argument is a variable name; option words are skipped.
func TestNameArgUnset(t *testing.T) {
	src := "unset -nocomplain -- ::a b(k)\n"
	refs := FileRefs(src)
	if !varRefAt(src, refs, "::a", "::a") {
		t.Errorf("unset ::a not a var ref: %#v", refs)
	}
	if !varRefAt(src, refs, "b", "b(k)") {
		t.Errorf("unset array base b not a var ref: %#v", refs)
	}
	for _, r := range refs {
		if r.Ref.Kind == RefVariable && strings.HasPrefix(r.Ref.Name, "-") {
			t.Errorf("option word wrongly treated as a var name: %#v", r.Ref)
		}
	}
}

// array read-subcommands reference their name argument; `array set` does NOT
// (it already emits a Definition; a ref too would double-list the site).
func TestNameArgArraySubcommands(t *testing.T) {
	for _, sub := range []string{"exists", "get", "names", "size", "unset"} {
		src := "array " + sub + " ::cfg\n"
		if !varRefAt(src, FileRefs(src), "::cfg", "::cfg") {
			t.Errorf("array %s ::cfg not a var ref", sub)
		}
	}
	src := "array set ::cfg {a 1}\n"
	for _, r := range FileRefs(src) {
		if r.Ref.Kind == RefVariable && r.Ref.Name == "::cfg" {
			t.Errorf("array set target must stay a definition, not also a ref: %#v", r.Ref)
		}
	}
}

// Dynamic names stay out of reach: no reference is invented for $name or
// substituted words in name position.
func TestNameArgDynamicSkipped(t *testing.T) {
	for _, src := range []string{
		"info exists $dyn\n",
		"unset $dyn\n",
		"array get $dyn\n",
		"info exists [pick]\n",
	} {
		for _, r := range FileRefs(src) {
			if r.Ref.Kind == RefVariable && strings.Contains(r.Ref.Name, "dyn") && r.Ref.Start == strings.Index(src, "$dyn") {
				// $dyn itself is a legit $-ref; what must NOT exist is a bareword-style
				// name ref invented for the whole word. Covered by the checks below.
				continue
			}
		}
		// `info exists [pick]` must not produce a variable named "[pick]" etc.
		for _, r := range FileRefs(src) {
			if r.Ref.Kind == RefVariable && (strings.ContainsAny(r.Ref.Name, "$[")) {
				t.Errorf("%q: invented ref for dynamic name: %#v", src, r.Ref)
			}
		}
	}
}

// A user proc merely named like the builtins must not trigger name-arg refs
// beyond the defined shapes: `info` without `exists`, `array` with an unknown
// subcommand.
func TestNameArgOnlyDefinedShapes(t *testing.T) {
	for _, src := range []string{
		"info level ::x\n",
		"array whatever ::x\n",
	} {
		for _, r := range FileRefs(src) {
			if r.Ref.Kind == RefVariable && r.Ref.Name == "::x" {
				t.Errorf("%q: ::x wrongly treated as a var name", src)
			}
		}
	}
}
