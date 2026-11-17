package resolve

import (
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// ickKind returns CommandRefKind for the command ref starting at the first
// occurrence of mark; see ickAt.
func ickKind(t *testing.T, src, mark string) (tcl.DefKind, bool) {
	t.Helper()
	at := strings.Index(src, mark)
	if at < 0 {
		t.Fatalf("test bug: mark %q not found", mark)
	}
	kind, ok, _ := ickAt(t, src, at)
	return kind, ok
}

// ickAt returns CommandRefKind and Definition's start lines for the command ref
// starting at byte offset at, and fails if they disagree on whether the call
// resolves or on its kind.
func ickAt(t *testing.T, src string, at int) (tcl.DefKind, bool, []int) {
	t.Helper()
	r := resolverOverGuardFile(src)
	def := r.Definition(guardFile, src, at)
	for _, ref := range source.Refs(guardFile, src) {
		if ref.Ref.Start == at && ref.Ref.Kind == tcl.RefCommand {
			kind, ok := r.CommandRefKind(&ref, guardFile)
			if ok != (len(def) > 0) {
				t.Errorf("CommandRefKind at %d resolves=%v but Definition=%v", at, ok, def)
			}
			if ok && len(def) > 0 && kind != def[0].Kind {
				t.Errorf("CommandRefKind at %d kind=%v but Definition kind=%v", at, kind, def[0].Kind)
			}
			return kind, ok, startLinesOf(src, guardFile, def)
		}
	}
	t.Fatalf("test bug: no command ref at %d", at)
	return 0, false, nil
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

// tclsh 8.6 and 9.0 (Itcl 4.3): a bare `m` runs the method only where its
// commands run in the class namespace: a method, constructor or itcl::body, and a
// proc or `namespace eval` inside a method that names the class namespace (there
// Tcl rejects it without an object, a known wrong answer, docs/BACKLOG.md). One
// naming another namespace runs ::m. A substituted proc name may be either.
const ickNSSrc = `proc ::m {} {}
namespace eval ::x {}
itcl::class K {
  method m {} {}
  method a {} { proc ::p {} {m-a} }
  method b {} { namespace eval ::x {m-b} }
  method c {}
  constructor {} {m-ctor}
  method i {} { proc ::K::r {} {m-i} }
  method j {} { proc ::K::sub::r {} {m-j} }
  method k {} { proc [namespace current]::s {} {m-k} }
  method n {} { namespace eval ::K {m-n} }
}
itcl::body K::c {} {m-body}
`

func TestCommandRefKindFollowsTheClassNamespace(t *testing.T) {
	// Each call is `m`; the -suffix only marks it and is not part of the name.
	src := ickNSSrc
	for _, tc := range []struct {
		mark string
		ok   bool
		kind tcl.DefKind
		line int
	}{
		{"m-a", true, tcl.DefProc, 1},
		{"m-b", true, tcl.DefProc, 1},
		{"m-j", true, tcl.DefProc, 1},
		{"m-ctor", true, tcl.DefMethod, 4},
		{"m-body", true, tcl.DefMethod, 4},
		{"m-i", true, tcl.DefMethod, 4},
		{"m-n", true, tcl.DefMethod, 4},
		{"m-k", false, 0, 0},
	} {
		at := strings.Index(src, tc.mark)
		clean := strings.ReplaceAll(src, tc.mark, "m"+strings.Repeat(" ", len(tc.mark)-1))
		kind, ok, lines := ickAt(t, clean, at)
		if ok != tc.ok || kind != tc.kind || (tc.ok && (len(lines) != 1 || lines[0] != tc.line)) {
			t.Errorf("%s -> (%v, %v, lines %v), want (%v, %v, line %d)", tc.mark, kind, ok, lines, tc.kind, tc.ok, tc.line)
		}
	}
}
