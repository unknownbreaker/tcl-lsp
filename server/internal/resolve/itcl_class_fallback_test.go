package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
)

// A relative class name in `inherit` or `itcl::body` names the class in the
// current namespace, else the global one; no parent and no `namespace path` entry
// is searched (tclsh 8.6 and 9.0, Itcl 4.3;
// research/experiments/08_itcl_class_fallback.tcl).

func TestICFInheritFallsBackToGlobalBase(t *testing.T) {
	src := `itcl::class ::Base { method bm {} {} }
namespace eval ns {
  itcl::class X { inherit Base; method hi {} { bm } }
}
`
	if got := icbDef(t, src, "bm } }"); !slices.Equal(got, []int{1}) {
		t.Errorf("bm in ::ns::X -> lines %v, want [1] (::Base::bm)", got)
	}
}

func TestICFInheritPrefersTheLocalBase(t *testing.T) {
	src := `itcl::class ::Base { method bm {} {} }
namespace eval ns {
  itcl::class Base { method bm {} {} }
  itcl::class X { inherit Base; method hi {} { bm } }
}
`
	if got := icbDef(t, src, "bm } }"); !slices.Equal(got, []int{3}) {
		t.Errorf("bm in ::ns::X -> lines %v, want [3] (::ns::Base::bm)", got)
	}
}

func TestICFInheritRelativeQualifiedFallsBackToGlobal(t *testing.T) {
	src := `itcl::class ::sub::B { method bm {} {} }
namespace eval z {
  itcl::class Z { inherit sub::B; method hi {} { bm } }
}
`
	if got := icbDef(t, src, "bm } }"); !slices.Equal(got, []int{1}) {
		t.Errorf("bm in ::z::Z -> lines %v, want [1] (::sub::B::bm)", got)
	}
}

// Neither a parent namespace nor the namespace path is searched: Tcl rejects the
// inherit, so the call answers nothing.
func TestICFInheritSearchesNoParentOrPath(t *testing.T) {
	src := `itcl::class ::p::B { method bm {} {} }
itcl::class ::lib::C { method cm {} {} }
namespace eval p::q {
  itcl::class Q { inherit B; method hi {} { bm } }
}
namespace eval w {
  namespace path ::lib
  itcl::class W { inherit C; method hi {} { cm } }
}
`
	if got := icbDef(t, src, "bm } }"); got != nil {
		t.Errorf("bm in ::p::q::Q -> lines %v, want nothing", got)
	}
	if got := icbDef(t, src, "cm } }"); got != nil {
		t.Errorf("cm in ::w::W -> lines %v, want nothing", got)
	}
}

// The body of `itcl::body G::run` in ::other belongs to ::G: its bare calls find
// G's methods and ::G's procs, not a same-named global proc, and the receiver
// call `$g run` reaches the body.
func TestICFBodyInOtherNamespaceNamesGlobalClass(t *testing.T) {
	src := `proc ::helper {} {}
proc ::m2 {} {}
itcl::class ::G {
  method run {}
  method m2 {} {}
}
proc ::G::helper {} {}
namespace eval other {
  itcl::body G::run {} { helper; m2 }
}
proc f {} {
  set g [::G #auto]
  $g run x
}
`
	if got := icbDef(t, src, "helper; m2"); !slices.Equal(got, []int{7}) {
		t.Errorf("helper in G::run -> lines %v, want [7] (::G::helper)", got)
	}
	if got := iicMethodDef(src, "m2 }"); !slices.Equal(got, []int{5}) {
		t.Errorf("m2 in G::run -> lines %v, want [5] (method ::G::m2)", got)
	}
	if got := iicMethodDef(src, "run x"); !slices.Equal(got, []int{4, 9}) {
		t.Errorf("$g run -> lines %v, want [4 9]", got)
	}
}

func TestICFBodyPrefersTheLocalClass(t *testing.T) {
	src := `itcl::class ::G { method run {} }
proc ::G::helper {} {}
namespace eval other {
  itcl::class G { method run {} }
  proc G::helper {} {}
  itcl::body G::run {} { helper }
}
`
	if got := icbDef(t, src, "helper }"); !slices.Equal(got, []int{5}) {
		t.Errorf("helper in other::G::run -> lines %v, want [5] (::other::G::helper)", got)
	}
}

// With neither class declared, the body keeps the local name, as before.
func TestICFBodyOfUndeclaredClassStaysLocal(t *testing.T) {
	src := `proc ::other::G::helper {} {}
namespace eval other {
  itcl::body G::run {} { helper }
}
`
	if got := icbDef(t, src, "helper }"); !slices.Equal(got, []int{1}) {
		t.Errorf("helper -> lines %v, want [1] (::other::G::helper)", got)
	}
}

// The class lives in another file, and the answer follows it as files come and go.
func TestICFBodyFollowsTheClassAcrossFiles(t *testing.T) {
	cls := "itcl::class ::G { method run {} }\nproc ::G::helper {} {}\n"
	local := "namespace eval other { itcl::class G { method run {} } }\nproc ::other::G::helper {} {}\n"
	body := "namespace eval other {\n  itcl::body G::run {} { helper }\n}\n"
	ix := index.New()
	ix.IndexFile("cls.tcl", cls)
	ix.IndexFile("body.tcl", body)
	r := New(ix)
	at := strings.Index(body, "helper }")
	defFiles := func() []string {
		var out []string
		for _, l := range r.Definition("body.tcl", body, at) {
			out = append(out, l.File)
		}
		return out
	}
	if got := defFiles(); !slices.Equal(got, []string{"cls.tcl"}) {
		t.Fatalf("helper -> %v, want [cls.tcl] (::G::helper)", got)
	}
	ix.IndexFile("local.tcl", local)
	if got := defFiles(); !slices.Equal(got, []string{"local.tcl"}) {
		t.Errorf("with ::other::G declared, helper -> %v, want [local.tcl]", got)
	}
	ix.RemoveFile("local.tcl")
	if got := defFiles(); !slices.Equal(got, []string{"cls.tcl"}) {
		t.Errorf("after removing local.tcl, helper -> %v, want [cls.tcl]", got)
	}
	// The body's method is a member of ::G while body.tcl is indexed, and only then.
	if ci := ix.Class("::G"); ci == nil || len(ci.Methods["run"]) != 2 {
		t.Errorf("::G run sites = %v, want the declaration and the body", ci)
	}
	ix.RemoveFile("body.tcl")
	if ci := ix.Class("::G"); ci == nil || len(ci.Methods["run"]) != 1 {
		t.Errorf("after removing body.tcl, ::G run sites = %v, want only the declaration", ci)
	}
}

// Find-references' declarations of the class-body method include the body.
func TestICFReferencesFromDeclarationReachBody(t *testing.T) {
	src := `itcl::class ::G { method run {} }
namespace eval other {
  itcl::body G::run {} { return 1 }
}
proc f {} {
  set g [::G #auto]
  $g run x
}
`
	r := resolverOverGuardFile(src)
	at := strings.Index(src, "run {}")
	got := startLinesOf(src, guardFile, r.Declarations(guardFile, src, at))
	if !slices.Equal(got, []int{1, 3}) {
		t.Errorf("declarations of run -> lines %v, want [1 3]", got)
	}
}

// A proc defined in such a body is created in the class namespace, which is a
// workspace fact the parser does not know, so it defines nothing.
func TestICFProcInFallbackBodyDefinesNothing(t *testing.T) {
	src := `itcl::class ::G { method run {} }
namespace eval other {
  itcl::body G::run {} { proc inner {} {} }
}
`
	for _, n := range icbProcDefs(src) {
		if strings.HasSuffix(n, "inner") {
			t.Errorf("proc def %q, want none", n)
		}
	}
}

// A call to that proc, from the body or as an absolute name elsewhere, may be it,
// so it answers nothing rather than a same-named global proc (Tcl runs
// ::G::inner here).
func TestICFCallToProcInFallbackBodyIsSilent(t *testing.T) {
	src := `itcl::class ::G { method run {} {} }
proc ::inner {} {}
namespace eval other {
  itcl::body G::run {} {
    proc inner {} {}
    inner
  }
}
proc ::user {} { ::G::inner; ::inner }
`
	if got := icbDef(t, src, "inner\n  }"); got != nil {
		t.Errorf("inner in the body -> lines %v, want silent", got)
	}
	if got := icbDef(t, src, "G::inner;"); got != nil {
		t.Errorf("::G::inner -> lines %v, want silent", got)
	}
	if got := icbDef(t, src, "inner }"); !slices.Equal(got, []int{2}) {
		t.Errorf("::inner -> lines %v, want [2]", got)
	}
}

// An `inherit` in a class whose namespace is unknown (an `itcl::class` a wrapper
// defines in a method) names no base, not even through the global fallback.
func TestICFInheritInUnknownNamespaceClassIsSilent(t *testing.T) {
	src := `itcl::class ::Base {
  variable bv
  method bm {} {}
}
itcl::class ::ns::K {
  method mk {} {
    uplevel 1 {
      itcl::class X {
        inherit Base
        method m {} { return $bv }
        method m2 {} { $this bm }
      }
    }
  }
}
`
	if got := icbDef(t, src, "bv }"); got != nil {
		t.Errorf("$bv -> lines %v, want silent", got)
	}
	if got := icbDef(t, src, "bm }"); got != nil {
		t.Errorf("$this bm -> lines %v, want silent", got)
	}
}

// A template runs in ::request, so `itcl::body G::run` there names ::G.
func TestICFBodyInTemplateNamesGlobalClass(t *testing.T) {
	const cls = "itcl::class ::G { method run {} }\nproc ::G::helper {} {}\nproc ::helper {} {}\n"
	const page = "<? itcl::body G::run {} { helper } ?>\n"
	ix := index.New()
	ix.IndexFile("/w/g.tcl", cls)
	ix.IndexFile("/w/p.rvt", page)
	r := New(ix)
	got := r.Definition("/w/p.rvt", page, strings.Index(page, "helper }"))
	if len(got) != 1 || got[0].File != "/w/g.tcl" || lineOfOffset(cls, got[0].NameStart) != 2 {
		t.Errorf("helper in the template's G::run -> %v, want ::G::helper", got)
	}
}

// In a substituted namespace (`namespace eval $ns`) the local class may be
// declared there, so neither `inherit Base` nor `itcl::body G::run` falls back.
func TestICFSubstitutedNamespaceDoesNotFallBack(t *testing.T) {
	src := `itcl::class ::Base { method bm {} {} }
itcl::class ::G { method run {} {}; method m2 {} {} }
namespace eval $ns {
  itcl::class X { inherit Base; method hi {} { bm } }
  itcl::body G::run {} { m2 }
}
`
	if got := icbDef(t, src, "bm } }"); got != nil {
		t.Errorf("bm in $ns::X -> lines %v, want silent", got)
	}
	if got := icbDef(t, src, "m2 }\n}"); got != nil {
		t.Errorf("m2 in a $ns body -> lines %v, want silent", got)
	}
}
