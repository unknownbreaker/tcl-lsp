package tcl

import (
	"reflect"
	"testing"
)

func TestFileClassesInherit(t *testing.T) {
	src := "itcl::class ::Derived { inherit ::Base }"
	m := FileClasses(src)
	got, ok := m["::Derived"]
	if !ok {
		t.Fatalf("::Derived not in FileClasses result: %#v", m)
	}
	if !reflect.DeepEqual(got, []string{"::Base"}) {
		t.Fatalf("FileClasses[::Derived] = %#v, want [::Base]", got)
	}
}

func TestFileClassesNoInherit(t *testing.T) {
	src := "itcl::class ::Standalone { method run {} {} }"
	m := FileClasses(src)
	if len(m) != 0 {
		t.Fatalf("expected empty map for class with no inherit, got %#v", m)
	}
}

func TestFileClassesMultipleBases(t *testing.T) {
	src := "itcl::class ::Child { inherit ::A ::B }"
	m := FileClasses(src)
	got := m["::Child"]
	if !reflect.DeepEqual(got, []string{"::A", "::B"}) {
		t.Fatalf("FileClasses[::Child] = %#v, want [::A ::B]", got)
	}
}

func TestFileClassesRelativeName(t *testing.T) {
	// A bare base name outside :: names ::ns::Base, else ::Base: which one is a
	// workspace fact, so FileClasses keeps the fallback key the index resolves.
	src := "namespace eval ::ns { itcl::class Child { inherit Base } }"
	m := FileClasses(src)
	got := m["::ns::Child"]
	if !reflect.DeepEqual(got, []string{classRefKey("Base", "::ns")}) {
		t.Fatalf("FileClasses[::ns::Child] = %#v, want the fallback key for Base in ::ns", got)
	}
	if local, global, ok := FallbackClass(got[0]); !ok || local != "::ns::Base" || global != "::Base" {
		t.Fatalf("FallbackClass(%q) = %q, %q, %v", got[0], local, global, ok)
	}
}

// A relative class name outside the global namespace is a fallback key: the local
// class, else the global one (research/experiments/08_itcl_class_fallback.tcl).
func TestClassRefKeyFallback(t *testing.T) {
	declared := func(set ...string) func(string) bool {
		return func(c string) bool {
			for _, s := range set {
				if s == c {
					return true
				}
			}
			return false
		}
	}
	for _, tc := range []struct {
		name, ns string
		decl     []string
		want     string
	}{
		{"G", "::", nil, "::G"},
		{"::G", "::ns", nil, "::G"},
		{"G", "::ns", nil, "::ns::G"},
		{"G", "::ns", []string{"::G"}, "::G"},
		{"G", "::ns", []string{"::G", "::ns::G"}, "::ns::G"},
		{"sub::B", "::z", []string{"::sub::B"}, "::sub::B"},
		{"B", "::p::q", []string{"::p::B"}, "::p::q::B"},
	} {
		key := classRefKey(tc.name, tc.ns)
		if got := CanonicalClass(key, declared(tc.decl...)); got != tc.want {
			t.Errorf("%s in %s with %v -> %s, want %s", tc.name, tc.ns, tc.decl, got, tc.want)
		}
		if IsUnknownClass(key) {
			t.Errorf("IsUnknownClass(%q) = true", key)
		}
	}
}
