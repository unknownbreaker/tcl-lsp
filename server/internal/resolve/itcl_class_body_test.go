package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// icbProcDefs returns the FQ names of every proc def in src.
func icbProcDefs(src string) []string {
	var out []string
	for _, d := range tcl.FileDefs(src) {
		if d.Kind == tcl.DefProc {
			out = append(out, d.Name)
		}
	}
	return out
}

// icbDef resolves the call whose text starts at the first occurrence of mark and
// returns the def lines; CommandRefKind must agree with Definition.
func icbDef(t *testing.T, src, mark string) []int {
	t.Helper()
	at := strings.Index(src, mark)
	if at < 0 {
		t.Fatalf("test bug: mark %q not found", mark)
	}
	r := resolverOverGuardFile(src)
	got := startLinesOf(src, guardFile, r.Definition(guardFile, src, at))
	for _, ref := range source.Refs(guardFile, src) {
		if ref.Ref.Start == at {
			if _, ok := r.CommandRefKind(&ref, guardFile); ok != (got != nil) {
				t.Errorf("CommandRefKind at %q resolves=%v but Definition=%v", mark, ok, got)
			}
		}
	}
	return got
}

// Bug hunted: `after 0 { proc outer {} { proc inner2 {} {} } }` in a method. The
// outer proc is created by the wrapper (no def), but the proc nested in outer's
// body is a new proc scope, so it lost the wrapped flag and got indexed as
// ::ns::K::inner2 although Tcl creates it in :: when outer runs.
func TestICBProcNestedInWrappedProcDefinesNothing(t *testing.T) {
	src := `namespace eval ::ns {
  itcl::class K {
    method w {} { after 0 { proc outer {} { proc inner2 {} {} } } }
  }
}
`
	for _, n := range icbProcDefs(src) {
		if strings.HasSuffix(n, "inner2") || strings.HasSuffix(n, "outer") {
			t.Errorf("proc created by a wrapper indexed as %s, want no def", n)
		}
	}
}

// Bug hunted: a proc nested in a proc nested in a method is created in the class
// namespace and its body runs there, so `g` inside resolves ::ns::K::g first.
func TestICBDeeplyNestedProcInMethod(t *testing.T) {
	src := `namespace eval ::ns {
  proc g {} {}
  itcl::class K {
    method m {} { proc a {} { proc b {} { proc c {} {}; c; g } } }
  }
}
proc ::ns::K::g {} {}
proc ::top {} { ::ns::K::a; ::ns::K::b; ::ns::K::c }
`
	want := []string{"::ns::K::a", "::ns::K::b", "::ns::K::c"}
	var got []string
	for _, n := range icbProcDefs(src) {
		if !strings.HasSuffix(n, "::g") && n != "::top" {
			got = append(got, n)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("proc defs = %v, want %v", got, want)
	}
	if l := icbDef(t, src, "g } } }"); !slices.Equal(l, []int{lineOf(t, src, "proc ::ns::K::g")}) {
		t.Errorf("g in deeply nested proc = %v, want ::ns::K::g", l)
	}
}

// Bug hunted: `public variable v 1 { g }` -- the configure body runs like a
// method (class namespace), not in ::itcl::parser. Looked up from the parser
// namespace it resolves ::g although the class namespace has its own g first.
func TestICBConfigBodyOfVariableRunsInClassNamespace(t *testing.T) {
	src := `proc ::g {} {}
namespace eval ::ns {
  itcl::class K {
    public variable v 1 { g }
  }
  proc K::g {} {}
}
`
	if l := icbDef(t, src, "g }"); l != nil && !slices.Equal(l, []int{lineOf(t, src, "proc K::g")}) {
		t.Errorf("config body g = %v, want silent or ::ns::K::g (not ::g)", l)
	}
}

// Bug hunted: control-flow wrapped around a class-body command.
func TestICBClassBodyControlFlow(t *testing.T) {
	src := `proc ::g {} {}
namespace eval ::ns {
  proc g {} {}
  itcl::class K {
    if {1} { common c [g] }
    foreach x {a} { common d [g] }
  }
}
`
	want := []int{lineOf(t, src, "proc ::g")}
	if l := icbDef(t, src, "g] }\n    foreach"); !slices.Equal(l, want) {
		t.Errorf("if-wrapped = %v want %v", l, want)
	}
	if l := icbDef(t, src, "g] }\n  }"); !slices.Equal(l, want) {
		t.Errorf("foreach-wrapped = %v want %v", l, want)
	}
}

// Bug hunted: the ::itcl::class spelling and a class nested in a namespace eval
// inside a method.
func TestICBClassSpellingsAndNesting(t *testing.T) {
	src := `proc ::g {} {}
namespace eval ::ns {
  proc g {} {}
  ::itcl::class A { common c [g]
  }
  itcl::class K {
    method m {} {
      namespace eval ::x {
        itcl::class Inner { common c [g]
        }
        proc g {} {}
      }
    }
  }
}
`
	want := []int{lineOf(t, src, "proc ::g")}
	if l := icbDef(t, src, "g]\n  }"); !slices.Equal(l, want) {
		t.Errorf("::itcl::class body = %v want %v", l, want)
	}
	if l := icbDef(t, src, "g]\n        }"); !slices.Equal(l, want) {
		t.Errorf("nested class body = %v want %v", l, want)
	}
}

// Bug hunted: .rvt pages wrap code in `namespace eval ::request`; a class body
// there must also run in ::itcl::parser (so `g` skips ::request::g and finds ::g
// from another file), and a method's proc lives in ::request::K, callable from a
// .tcl file as ::request::K::inner.
func TestICBRvtClassBody(t *testing.T) {
	rvt := "<?\nproc g {} {}\nitcl::class K {\n  common c [g]\n  method m {} { proc inner {} {} }\n}\n?>\n"
	tclSrc := "proc ::g {} {}\nproc ::top {} { ::request::K::inner }\n"
	ix := index.New()
	ix.IndexFile("page.rvt", rvt)
	ix.IndexFile("lib.tcl", tclSrc)
	r := New(ix)
	at := strings.Index(rvt, "g]")
	locs := r.Definition("page.rvt", rvt, at)
	if len(locs) != 1 || locs[0].File != "lib.tcl" {
		t.Errorf("class-body g in .rvt = %+v, want only ::g in lib.tcl", locs)
	}
	at = strings.Index(tclSrc, "::request::K::inner")
	locs = r.Definition("lib.tcl", tclSrc, at)
	if len(locs) != 1 || locs[0].File != "page.rvt" || lineOfOffset(rvt, locs[0].NameStart) != 5 {
		t.Errorf("::request::K::inner = %+v, want the method's proc in page.rvt line 5", locs)
	}
}

// Bug hunted: which procs get indexed for assorted bodies in a method.
func TestICBMethodBodyProcDefShapes(t *testing.T) {
	cases := []struct {
		name, body string
		want       []string
	}{
		{"decorated proc", `DECOR proc d {} {}`, []string{"::ns::K::d"}},
		{"decorated proc in wrapper", `after 0 {DECOR proc d {} {}}`, nil},
		{"custom command trailing brace", `mycmd -x {proc d {} {}}`, nil},
		{"wrapper in if in wrapper", `after 0 { if {1} { proc d {} {} } }`, nil},
		{"if in method", `if {1} { proc d {} {} } else { proc e {} {} }`, []string{"::ns::K::d", "::ns::K::e"}},
		{"catch and foreach", `catch { proc d {} {} }; foreach x {1} { proc e {} {} }`, []string{"::ns::K::d", "::ns::K::e"}},
		{"namespace eval absolute", `namespace eval ::x { proc d {} {} }`, []string{"::x::d"}},
		{"qualified absolute", `proc ::x::d {} {}`, []string{"::x::d"}},
		{"global-qualified", `proc ::d {} {}`, []string{"::d"}},
		{"uplevel", `uplevel 1 { proc d {} {} }`, nil},
		{"wrapped namespace eval absolute", `after 0 { namespace eval ::x { proc d {} {} } }`, []string{"::x::d"}},
		{"proc in proc in namespace eval", `namespace eval ::x { proc o {} { proc d {} {} } }`, []string{"::x::o", "::x::d"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			src := "namespace eval ::ns {\n itcl::class K {\n  method m {} {" + c.body + "}\n }\n}\n"
			if got := icbProcDefs(src); !slices.Equal(got, c.want) {
				t.Errorf("proc defs = %v, want %v", got, c.want)
			}
		})
	}
}

// Bug hunted: a call in a wrapper's body inside a method (`after 0 {g}`) runs in
// ::, not the class namespace, so with ::ns::K::g defined it must not answer it.
// Same for a call in the body of a proc that the wrapper creates.
func TestICBWrappedCallsDoNotUseClassNamespace(t *testing.T) {
	src := `proc ::g {} {}
namespace eval ::ns {
  itcl::class K {
    method w {} { after 0 {g}; after 0 {proc outer {} { g }} }
  }
  proc K::g {} {}
}
`
	kg := lineOf(t, src, "proc K::g")
	for _, mark := range []string{"g}; after", "g }}"} {
		if l := icbDef(t, src, mark); slices.Contains(l, kg) {
			t.Errorf("call at %q resolved to ::ns::K::g (line %d), but it runs in ::", mark, kg)
		}
	}
}

// Bug hunted: highlight and references from a class-body call disagree with
// goto-definition (the ::ns::g proc must not be highlighted or referenced).
func TestICBClassBodyHighlightAndReferencesAgree(t *testing.T) {
	src := `proc ::g {} {}
namespace eval ::ns {
  proc g {} {}
  itcl::class K {
    common c [g]
  }
  proc use {} { g }
}
`
	r := resolverOverGuardFile(src)
	at := strings.Index(src, "g]")
	if got := startLinesOf(src, guardFile, r.FileHighlights(guardFile, src, at)); !slices.Equal(got, []int{lineOf(t, src, "proc ::g"), lineOf(t, src, "common c")}) {
		t.Errorf("highlights of class-body g = %v, want ::g def and the call only", got)
	}
	globalG := strings.Index(src, "proc ::g") + len("proc ::")
	if got := startLinesOf(src, guardFile, r.References(guardFile, src, globalG)); !slices.Equal(got, []int{lineOf(t, src, "common c")}) {
		t.Errorf("references to ::g = %v, want the class-body call only", got)
	}
}
