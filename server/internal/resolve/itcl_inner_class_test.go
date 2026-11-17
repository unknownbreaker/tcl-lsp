package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Where an itcl::class defined inside a method is created, and where its methods
// look up from (tclsh 8.6 and 9.0, Itcl 4.3;
// research/experiments/08_itcl_inner_class.tcl).

// iicClassDefs returns the FQ names of every class def in src.
func iicClassDefs(src string) []string {
	var out []string
	for _, d := range tcl.FileDefs(src) {
		if d.Kind == tcl.DefClass {
			out = append(out, d.Name)
		}
	}
	return out
}

func TestIICUnqualifiedClassInMethodIsInClassNamespace(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class K {
    method m {} { itcl::class Inner {} }
    method c {} { if {1} { ::itcl::class Inner2 {} } }
    method q {} { itcl::class ::abs::Inner9 {} }
    constructor {} { itcl::class Inner6 {} }
    proc cp {} { itcl::class Inner7 {} }
    method later
  }
}
itcl::body ::ns::K::later {} { catch { itcl::class Inner8 {} } }
itcl::class Top {}
`
	got := iicClassDefs(src)
	want := []string{"::ns::K", "::ns::K::Inner", "::ns::K::Inner2", "::abs::Inner9",
		"::ns::K::Inner6", "::ns::K::Inner7", "::ns::K::Inner8", "::Top"}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("class defs = %v, want %v", got, want)
	}
}

func TestIICClassInWrapperBodyDefinesNothing(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class K {
    method w {} { after 0 {itcl::class Inner3 {}}; uplevel 1 {itcl::class Inner4 {}} }
  }
}
`
	got := iicClassDefs(src)
	if !slices.Equal(got, []string{"::ns::K"}) {
		t.Errorf("class defs = %v, want only ::ns::K", got)
	}
}

func TestIICBareCallInMethodFindsInnerClass(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class Inner {}
  itcl::class K {
    method m {} { itcl::class Inner {} }
    method who {} { return [Inner #auto] }
  }
}
`
	if got := icbDef(t, src, "Inner #auto"); !slices.Equal(got, []int{4}) {
		t.Errorf("Inner #auto -> lines %v, want [4] (::ns::K::Inner)", got)
	}
}

func TestIICInnerMethodLooksUpFromItsClassNamespace(t *testing.T) {
	src := `proc ::h {} {}
namespace eval ::ns {
  proc g {} {}
  proc h {} {}
  itcl::class K {
    method m {} { itcl::class Inner { method hi {} { g; h } } }
  }
}
namespace eval ::ns::K::Inner { proc g {} {} }
`
	if got := icbDef(t, src, "g; h"); !slices.Equal(got, []int{9}) {
		t.Errorf("g -> lines %v, want [9] (::ns::K::Inner::g)", got)
	}
	if got := icbDef(t, src, "h }"); !slices.Equal(got, []int{1}) {
		t.Errorf("h -> lines %v, want [1] (::h, never ::ns::h)", got)
	}
}

// A class an `uplevel 1` body defines lives in the caller's namespace, so its
// methods' lookups and its own methods are not certain.
func TestIICWrappedClassMethodsAreSilentWhenUncertain(t *testing.T) {
	src := `proc ::g {} {}
proc ::hi2 {} {}
namespace eval ::ns {
  itcl::class K {
    method w {} { uplevel 1 { itcl::class Inner4 { method hi {} { g; hi2 }; method hi2 {} {} } } }
  }
}
namespace eval ::ns::Inner4 { proc g {} {} }
`
	if got := icbDef(t, src, "g; hi2"); got != nil {
		t.Errorf("g in a wrapped class -> lines %v, want silent", got)
	}
	// Not icbDef: CommandRefKind skips the intra-class check, so it colors hi2 as
	// ::hi2 (a known wrong answer on main, docs/BACKLOG.md).
	if got := iicMethodDef(src, "hi2 }"); got != nil {
		t.Errorf("hi2 in a wrapped class -> lines %v, want silent", got)
	}
}

// iicMethodDef resolves the bare method call at the first occurrence of mark. It
// skips icbDef's CommandRefKind check: semantic tokens do not resolve intra-class
// calls (docs/BACKLOG.md).
func iicMethodDef(src, mark string) []int {
	r := resolverOverGuardFile(src)
	return startLinesOf(src, guardFile, r.Definition(guardFile, src, strings.Index(src, mark)))
}

// `inherit` names resolve in the namespace that holds the class: ::ns::K for a
// class defined in K's method, ::ns::sub for `itcl::class sub::X` in ::ns, never
// the lexical ::ns (tclsh 8.6 and 9.0, Itcl 4.3; 08_itcl_inner_class.tcl).
func TestIICInheritResolvesInTheClassParentNamespace(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class Base { method bm {} {} }
  itcl::class K {
    method m {} { itcl::class Inner { inherit Base; method hi {} { bm } } }
  }
  itcl::class K::Base { method bm {} {} }
}
`
	if got := iicMethodDef(src, "bm } } }"); !slices.Equal(got, []int{6}) {
		t.Errorf("bm in Inner -> lines %v, want [6] (::ns::K::Base::bm)", got)
	}
}

// Tcl rejects `inherit B2` here (not found in context ::ns::K), so it answers nothing.
func TestIICInheritOfALexicalOnlyBaseIsSilent(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class B2 { method bm {} {} }
  itcl::class K {
    method m {} { itcl::class Inner2 { inherit B2; method hi {} { bm } } }
  }
}
`
	if got := iicMethodDef(src, "bm } } }"); got != nil {
		t.Errorf("bm in Inner2 -> lines %v, want silent", got)
	}
}

func TestIICInheritOfAQualifiedClassUsesItsOwnNamespace(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class B { method bm {} {} }
  itcl::class sub::B { method bm {} {} }
  itcl::class sub::X { inherit B; method hi {} { bm } }
}
`
	if got := iicMethodDef(src, "bm } }"); !slices.Equal(got, []int{3}) {
		t.Errorf("bm in sub::X -> lines %v, want [3] (::ns::sub::B::bm)", got)
	}
}

// A class a wrapper defines in a method is not ::ns::Inner4 (Tcl creates it in
// the caller's namespace), so `inherit Inner4` in ::ns does not reach its members.
func TestIICWrappedClassMembersAreNotUnderTheLexicalName(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class Inner4 { method hi {} {} }
  itcl::class K {
    method w {} { uplevel 1 { itcl::class Inner4 { method hi {} {} } } }
  }
  itcl::class D { inherit Inner4; method x {} { hi } }
}
`
	if got := iicMethodDef(src, "hi } }\n}"); !slices.Equal(got, []int{2}) {
		t.Errorf("hi in D -> lines %v, want [2] (::ns::Inner4::hi only)", got)
	}
}

// In a wrapped class's method a bare call may be an inherited method of a base
// whose namespace is unknown, so it is silent; its own ivars still resolve.
func TestIICWrappedClassBareCallsSilentIvarsResolve(t *testing.T) {
	src := `proc ::bm {} {}
namespace eval ::ns {
  itcl::class K {
    method w {} { uplevel 1 { itcl::class Inner4 { inherit B; variable v; method hi {} { bm; ::bm; set v } } } }
  }
}
`
	if got := iicMethodDef(src, "bm; ::bm"); got != nil {
		t.Errorf("bare bm -> lines %v, want silent", got)
	}
	if got := icbDef(t, src, "::bm; set"); !slices.Equal(got, []int{1}) {
		t.Errorf("::bm -> lines %v, want [1]", got)
	}
	if got := iicMethodDef(src, "v } } }"); !slices.Equal(got, []int{4}) {
		t.Errorf("ivar v -> lines %v, want [4]", got)
	}
}
