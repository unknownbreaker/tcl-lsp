package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// ickKind returns CommandRefKind for the command ref starting at the first
// occurrence of mark, and fails if CommandRefKind and Definition disagree on
// whether the call resolves.
func ickKind(t *testing.T, src, mark string) (tcl.DefKind, bool) {
	t.Helper()
	at := strings.Index(src, mark)
	if at < 0 {
		t.Fatalf("test bug: mark %q not found", mark)
	}
	r := resolverOverGuardFile(src)
	def := r.Definition(guardFile, src, at)
	for _, ref := range source.Refs(guardFile, src) {
		if ref.Ref.Start == at && ref.Ref.Kind == tcl.RefCommand {
			kind, ok := r.CommandRefKind(&ref, guardFile)
			if ok != (len(def) > 0) {
				t.Errorf("CommandRefKind at %q resolves=%v but Definition=%v", mark, ok, def)
			}
			if ok && len(def) > 0 && kind != def[0].Kind {
				t.Errorf("CommandRefKind at %q kind=%v but Definition kind=%v", mark, kind, def[0].Kind)
			}
			return kind, ok
		}
	}
	t.Fatalf("test bug: no command ref at %q", mark)
	return 0, false
}

// tclsh 8.6 and 9.0 (Itcl 4.3): a bare `m` in a method runs the method even when
// a global proc ::m exists; `after 0 {m}` runs ::m at global level, so
// goto-definition is silent there and the token must be too.
const ickShadowSrc = `proc ::m {} {}
namespace eval ::ns {
  itcl::class K {
    method a {} { m }
    method w {} { after 0 {m} }
    method m {} {}
  }
}
`

func TestCommandRefKindBareMethodShadowedByGlobalProc(t *testing.T) {
	kind, ok := ickKind(t, ickShadowSrc, "m }\n    method w")
	if !ok || kind != tcl.DefMethod {
		t.Errorf("bare m -> (%v, %v), want (DefMethod, true)", kind, ok)
	}
}

func TestCommandRefKindWrappedMethodCallIsSilent(t *testing.T) {
	if kind, ok := ickKind(t, ickShadowSrc, "m} }"); ok {
		t.Errorf("after 0 {m} -> (%v, true), want silent", kind)
	}
}

// A bare call to an inherited method with no same-named proc is a method too.
func TestCommandRefKindInheritedBareMethod(t *testing.T) {
	src := `itcl::class B { method m {} {} }
itcl::class D {
  inherit B
  method a {} { m }
}
`
	kind, ok := ickKind(t, src, "m }")
	if !ok || kind != tcl.DefMethod {
		t.Errorf("bare inherited m -> (%v, %v), want (DefMethod, true)", kind, ok)
	}
}

// A bare call in a method with no such method still resolves as a command.
func TestCommandRefKindBareProcInMethod(t *testing.T) {
	src := `proc ::g {} {}
itcl::class K { method a {} { g } }
`
	kind, ok := ickKind(t, src, "g }")
	if !ok || kind != tcl.DefProc {
		t.Errorf("bare g -> (%v, %v), want (DefProc, true)", kind, ok)
	}
}

// tclsh 8.6 and 9.0 (Itcl 4.3): a qualified proc defined in a method, and a
// `namespace eval` there, run outside the class namespace, so a bare `m` in
// them runs ::m, not the method. itcl::body and constructor bodies stay in it.
const ickOutsideSrc = `proc ::m {} {}
namespace eval ::x {}
itcl::class K {
  method m {} {}
  method a {} { proc ::p {} { m }; ::p }
  method b {} { namespace eval ::x {m} }
  method c {}
  constructor {} { m }
}
itcl::body K::c {} { m }
`

func TestCommandRefKindQualifiedProcInMethodIsNotIntraClass(t *testing.T) {
	for _, mark := range []string{"m }; ::p", "m} }"} {
		if kind, ok := ickKind(t, ickOutsideSrc, mark); !ok || kind != tcl.DefProc {
			t.Errorf("%q -> (%v, %v), want (DefProc, true) for ::m", mark, kind, ok)
		}
		at := strings.Index(ickOutsideSrc, mark)
		if got := startLinesOf(ickOutsideSrc, guardFile, resolverOverGuardFile(ickOutsideSrc).Definition(guardFile, ickOutsideSrc, at)); len(got) != 1 || got[0] != 1 {
			t.Errorf("%q -> lines %v, want [1] (::m)", mark, got)
		}
	}
}

func TestCommandRefKindBodyAndConstructorAreIntraClass(t *testing.T) {
	ctor := strings.Index(ickOutsideSrc, "constructor {} { m }") + len("constructor {} { ")
	body := strings.LastIndex(ickOutsideSrc, "{ m }") + 2
	r := resolverOverGuardFile(ickOutsideSrc)
	for name, at := range map[string]int{"constructor": ctor, "itcl::body": body} {
		if got := startLinesOf(ickOutsideSrc, guardFile, r.Definition(guardFile, ickOutsideSrc, at)); len(got) != 1 || got[0] != 4 {
			t.Errorf("%s m -> lines %v, want [4] (method m)", name, got)
		}
		for _, ref := range source.Refs(guardFile, ickOutsideSrc) {
			if ref.Ref.Start == at {
				if kind, ok := r.CommandRefKind(&ref, guardFile); !ok || kind != tcl.DefMethod {
					t.Errorf("%s m -> (%v, %v), want (DefMethod, true)", name, kind, ok)
				}
			}
		}
	}
}
