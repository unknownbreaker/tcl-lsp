package resolve

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// itclNSSrc mirrors research/experiments/08_itcl_class_namespace.tcl. A method, a
// class proc and an itcl::body run in the class namespace (::ns::K), so a call
// there tries ::ns::K first, then the class namespace's path and imports, then
// global. The enclosing ::ns and its path are never searched (tclsh 8.6.18 with
// Itcl 4.3.7 and 9.0.3 with Itcl 4.3.5 agree).
const itclNSSrc = `namespace eval ::util { proc f {} {} }
proc ::g {} {}
namespace eval ::lib { proc lp {} {} }
namespace eval ::src { namespace export *; proc imp {} {} }
namespace eval ::ns {
  namespace eval util { proc f {} {} }
  proc g {} {}
  proc lex {} {}
  namespace eval nlib { proc pp {} {} }
  namespace path ::ns::nlib
  itcl::class K {
    method qualified {} { return [util::f] }
    method bare {} { return [g] }
    method lexical {} { return [lex] }
    method lexicalPath {} { return [pp] }
    method classNs {} { return [kh] }
    method classPath {} { return [lp] }
    method classImport {} { return [imp] }
    method wrapped {} { after 0 {g} }
    method later
    proc classProc {} { return [g] }
    constructor {} { g }
  }
  proc K::kh {} {}
  namespace eval K { namespace path ::lib; namespace import ::src::imp }
  proc plain {} { g }
}
itcl::body ::ns::K::later {} { return [g] }
namespace eval ::other {
  itcl::body ::ns::K::later2 {} { return [g] }
}
itcl::class ::Base {}
proc ::Base::bh {} {}
itcl::class ::D { inherit ::Base; method baseNs {} { return [bh] } }
`

// lineOf returns the 1-based line of the first occurrence of mark in src.
func lineOf(t *testing.T, src, mark string) int {
	t.Helper()
	i := strings.Index(src, mark)
	if i < 0 {
		t.Fatalf("test bug: mark %q not found", mark)
	}
	return lineOfOffset(src, i)
}

// Bug hunted: a method body resolving calls against the namespace that lexically
// holds the class (answering ::ns::g, ::ns::util::f, ::ns::lex), and a fix that
// drops the class namespace's own path, imports or definitions, or leaks into a
// plain proc of the enclosing namespace.
func TestItclMethodCallsResolveInClassNamespace(t *testing.T) {
	src := itclNSSrc
	globalG := lineOf(t, src, "proc ::g")
	cases := []struct {
		name, at string
		want     []int
	}{
		{"qualified skips the lexical namespace", "util::f] }", []int{lineOf(t, src, "namespace eval ::util")}},
		{"bare falls back to global", "g] }\n    method lexical", []int{globalG}},
		{"lexical namespace is not searched", "lex] }", nil},
		{"lexical namespace path is not searched", "pp] }", nil},
		{"class namespace first", "kh] }", []int{lineOf(t, src, "proc K::kh")}},
		{"class namespace path", "lp] }", []int{lineOf(t, src, "proc lp")}},
		{"class namespace import", "imp] }", []int{lineOf(t, src, "proc imp")}},
		{"wrapped body in a method", "g} }", []int{globalG}},
		{"class proc", "g] }\n    constructor", []int{globalG}},
		{"constructor", "g }\n  }", []int{globalG}},
		{"itcl::body", "g] }\nnamespace eval ::other", []int{globalG}},
		{"itcl::body inside another namespace", "g] }\n}\nitcl::class ::Base", []int{globalG}},
		{"base class namespace is not searched", "bh] }", nil},
		{"plain proc keeps its namespace", "g }\n}", []int{lineOf(t, src, "proc g {}")}},
	}
	resolver := resolverOverGuardFile(src)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := strings.Index(src, c.at)
			if at < 0 {
				t.Fatalf("test bug: mark %q not found", c.at)
			}
			if got := startLinesOf(src, guardFile, resolver.Definition(guardFile, src, at)); !slices.Equal(got, c.want) {
				t.Errorf("definition lines at %q = %v, want %v", c.at, got, c.want)
			}
		})
	}
}

// References, highlights and semantic tokens share the lookup, so they agree with
// goto-definition. Bug hunted: the class namespace used on one path only, which
// lists a method's `g` as a reference to ::ns::g, or colors it as a call to it.
func TestItclClassNamespaceOtherPaths(t *testing.T) {
	src := itclNSSrc
	resolver := resolverOverGuardFile(src)

	nsG := strings.Index(src, "proc g {}") + len("proc ")
	if got := startLinesOf(src, guardFile, resolver.References(guardFile, src, nsG)); !slices.Equal(got, []int{lineOf(t, src, "proc plain")}) {
		t.Errorf("references to ::ns::g = %v, want only the plain proc's call", got)
	}
	globalG := strings.Index(src, "proc ::g") + len("proc ::")
	refs := startLinesOf(src, guardFile, resolver.References(guardFile, src, globalG))
	for _, mark := range []string{"method bare", "method wrapped", "proc classProc", "constructor", "itcl::body ::ns::K::later {}", "later2"} {
		if !slices.Contains(refs, lineOf(t, src, mark)) {
			t.Errorf("references to ::g = %v, missing the call on the %q line", refs, mark)
		}
	}

	lex := strings.Index(src, "lex] }")
	if got := startLinesOf(src, guardFile, resolver.FileHighlights(guardFile, src, lex)); !slices.Equal(got, []int{lineOf(t, src, "method lexical")}) {
		t.Errorf("highlights from the unresolvable lex call = %v, want only the call, not ::ns::lex", got)
	}

	for _, c := range []struct {
		name string
		ok   bool
	}{{"lex", false}, {"kh", true}, {"util::f", true}} {
		ref := &tcl.ContextRef{Ref: tcl.Reference{Kind: tcl.RefCommand, Name: c.name}, Namespace: "::ns", Frame: tcl.FrameProc, Class: "::ns::K", InClassNS: true}
		if _, ok := resolver.CommandRefKind(ref, guardFile); ok != c.ok {
			t.Errorf("CommandRefKind(%s in a ::ns::K method) resolves = %v, want %v", c.name, ok, c.ok)
		}
	}
}

// itclNestedSrc has a ::ns::K::g, which a body running in the class namespace
// finds before ::g, and a ::other::g and ::x::g for bodies that run there.
const itclNestedSrc = `proc ::g {} {}
namespace eval ::other { proc g {} {} }
namespace eval ::x { proc g {} {} }
namespace eval ::ns {
  proc g {} {}
  itcl::class K {
    method inner {} { proc inner {} { return [g] } }
    method qualified {} { proc ::other::p {} { return [g] } }
    method nsEvalProc {} { namespace eval ::x { proc p {} { return [g] } } }
    method nsEval {} { namespace eval ::x { g } }
    method deferred {} { after 0 { set r [g] } }
    method global {} { uplevel #0 { g } }
  }
  proc K::g {} {}
}
`

// A body defined inside a method runs where Tcl creates it: an unqualified proc in
// the class namespace, a qualified proc or a `namespace eval` in the namespace it
// names (tclsh 8.6 and 9.0, Itcl 4.3). A wrapper's body (`after`, `uplevel #0`) may
// run globally, so with ::ns::K::g and ::g both defined it is silent. Bug hunted
// (found by the adversarial tester): the class namespace leaking into every body
// nested in a method, which answered ::ns::K::g for all six.
func TestItclBodiesNestedInMethods(t *testing.T) {
	src := itclNestedSrc
	cases := []struct {
		name, at string
		want     []int
	}{
		{"unqualified proc runs in the class namespace", "g] } }\n    method qualified", []int{lineOf(t, src, "proc K::g")}},
		{"qualified proc runs in its namespace", "g] } }\n    method nsEvalProc", []int{lineOf(t, src, "namespace eval ::other")}},
		{"proc in namespace eval", "g] } } }", []int{lineOf(t, src, "namespace eval ::x")}},
		{"namespace eval", "g } }\n    method deferred", []int{lineOf(t, src, "namespace eval ::x")}},
		{"after body is silent when lookups disagree", "g] } }\n    method global", nil},
		{"uplevel #0 body is silent when lookups disagree", "g } }\n  }", nil},
	}
	resolver := resolverOverGuardFile(src)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := strings.Index(src, c.at)
			if at < 0 {
				t.Fatalf("test bug: mark %q not found", c.at)
			}
			if got := startLinesOf(src, guardFile, resolver.Definition(guardFile, src, at)); !slices.Equal(got, c.want) {
				t.Errorf("definition lines at %q = %v, want %v", c.at, got, c.want)
			}
			for _, ref := range source.Refs(guardFile, src) {
				if ref.Ref.Start == at {
					if _, ok := resolver.CommandRefKind(&ref, guardFile); ok != (c.want != nil) {
						t.Errorf("CommandRefKind at %q resolves = %v, want %v", c.at, ok, c.want != nil)
					}
				}
			}
		})
	}
}

// Find-references scans files in parallel, so the class namespaces a method's
// calls look up from must be warmed first. Here nothing else names them. Bug
// hunted (found by the adversarial tester): warming only the lexical namespace,
// which under -race is a concurrent map write in the index's namespace cache.
func TestItclClassNamespaceWarmedForParallelReferences(t *testing.T) {
	ix := index.New()
	for i := 0; i < 64; i++ {
		ix.IndexFile(fmt.Sprintf("/w/f%02d.tcl", i), fmt.Sprintf(
			"namespace eval ::n%d {\n    itcl::class C%d { method m {} { f } }\n}\n", i, i))
	}
	lib := "proc ::f {} {}\n"
	ix.IndexFile("/w/lib.tcl", lib)
	if refs := New(ix).References("/w/lib.tcl", lib, strings.Index(lib, "::f")+2); len(refs) != 64 {
		t.Errorf("references to ::f = %d, want 64, one per method", len(refs))
	}
}

// itclBodySrc mirrors research/experiments/08_itcl_class_body.tcl. A class body's
// own commands run in ::itcl::parser, whose path is empty, so `[g]` there tries
// ::itcl::parser::g, then ::g; neither the enclosing ::ns nor the class namespace
// ::ns::K is searched (tclsh 8.6.18 with Itcl 4.3.7 and 9.0.3 with Itcl 4.3.5).
const itclBodySrc = `proc ::g {} {}
namespace eval ::util { proc f {} {} }
namespace eval ::ns {
  proc g {} {}
  proc h {} {}
  namespace eval util { proc f {} {} }
  itcl::class K {
    common c [g]
    common u [util::f]
    common n [h]
    common k [kh]
    variable v [g]
    private common p [g]
  }
  proc K::kh {} {}
  proc K::g {} {}
}
`

// Bug hunted: a class body's commands looked up from the enclosing ::ns (answering
// ::ns::g, ::ns::util::f and ::ns::h), or from the class namespace (::ns::K::g,
// ::ns::K::kh), neither of which Tcl searches there.
func TestItclClassBodyCallsResolveInParserNamespace(t *testing.T) {
	src := itclBodySrc
	globalG := []int{lineOf(t, src, "proc ::g")}
	cases := []struct {
		name, at string
		want     []int
	}{
		{"common falls back to global", "g]\n    common u", globalG},
		{"qualified skips the lexical namespace", "util::f]", []int{lineOf(t, src, "namespace eval ::util")}},
		{"lexical namespace is not searched", "h]\n", nil},
		{"class namespace is not searched", "kh]\n", nil},
		{"variable initializer", "g]\n    private", globalG},
		{"access-modified common", "g]\n  }", globalG},
	}
	resolver := resolverOverGuardFile(src)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := strings.Index(src, c.at)
			if at < 0 {
				t.Fatalf("test bug: mark %q not found", c.at)
			}
			if got := startLinesOf(src, guardFile, resolver.Definition(guardFile, src, at)); !slices.Equal(got, c.want) {
				t.Errorf("definition lines at %q = %v, want %v", c.at, got, c.want)
			}
			for _, ref := range source.Refs(guardFile, src) {
				if ref.Ref.Start == at {
					if _, ok := resolver.CommandRefKind(&ref, guardFile); ok != (c.want != nil) {
						t.Errorf("CommandRefKind at %q resolves = %v, want %v", c.at, ok, c.want != nil)
					}
				}
			}
		})
	}
	nsG := strings.Index(src, "proc g {}") + len("proc ")
	if got := startLinesOf(src, guardFile, resolver.References(guardFile, src, nsG)); got != nil {
		t.Errorf("references to ::ns::g = %v, want none", got)
	}
}

// itclInnerSrc mirrors research/experiments/08_itcl_class_body.tcl. An unqualified
// proc defined in a method, constructor, class proc or itcl::body is created in the
// class namespace (::ns::K::inner). One defined in a wrapper's body is created
// where the wrapper runs it (`after`: ::, `uplevel 1`: the caller's namespace).
const itclInnerSrc = `namespace eval ::ns {
  proc inner {} {}
  itcl::class K {
    method m {} { proc inner {} {}; inner }
    proc cp {} { proc inner4 {} {}; inner4 }
    constructor {} { proc inner5 {} {} }
    method w {} { after 0 {proc inner2 {} {}}; uplevel 1 {proc inner3 {} {}} }
    method later
  }
  proc caller {} { K::inner; inner; K::inner4; K::inner5; K::inner6; inner2; inner3 }
}
itcl::body ::ns::K::later {} { proc inner6 {} {} }
proc ::top {} { ::ns::K::inner; ::inner2; ::ns::inner3 }
`

// Bug hunted: a proc defined in a method indexed in the namespace that lexically
// holds the class (::ns::inner), so `inner` from ::ns answered it and `K::inner`
// was silent; and a proc in a wrapper's body indexed at a namespace Tcl may not use.
func TestItclProcDefinedInMethodLivesInClassNamespace(t *testing.T) {
	src := itclInnerSrc
	nsInner := []int{lineOf(t, src, "proc inner {} {}\n")}
	kInner := []int{lineOf(t, src, "method m")}
	cases := []struct {
		name, at string
		want     []int
	}{
		{"call in the method", "inner }\n", kInner},
		{"class proc's nested proc", "inner4 }\n", []int{lineOf(t, src, "proc cp")}},
		{"qualified from the enclosing namespace", "K::inner;", kInner},
		{"bare from the enclosing namespace", "inner; K::inner4", nsInner},
		{"class proc's from outside", "K::inner4;", []int{lineOf(t, src, "proc cp")}},
		{"constructor's", "K::inner5;", []int{lineOf(t, src, "constructor")}},
		{"itcl::body's", "K::inner6;", []int{lineOf(t, src, "itcl::body")}},
		{"after body's is not indexed", "inner2;", nil},
		{"uplevel body's is not indexed", "inner3 }", nil},
		{"absolute class name", "::ns::K::inner;", kInner},
		{"absolute after target", "::inner2;", nil},
		{"absolute uplevel target", "::ns::inner3 }", nil},
	}
	resolver := resolverOverGuardFile(src)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			at := strings.Index(src, c.at)
			if at < 0 {
				t.Fatalf("test bug: mark %q not found", c.at)
			}
			if got := startLinesOf(src, guardFile, resolver.Definition(guardFile, src, at)); !slices.Equal(got, c.want) {
				t.Errorf("definition lines at %q = %v, want %v", c.at, got, c.want)
			}
		})
	}
	def := strings.Index(src, "proc inner {} {};") + len("proc ")
	if got, want := startLinesOf(src, guardFile, resolver.References(guardFile, src, def)), []int{lineOf(t, src, "method m"), lineOf(t, src, "proc caller"), lineOf(t, src, "proc ::top")}; !slices.Equal(got, want) {
		t.Errorf("references to ::ns::K::inner = %v, want %v", got, want)
	}
	defs := tcl.FileDefs(src)
	for _, d := range defs {
		if d.Kind == tcl.DefProc && strings.HasSuffix(d.Name, "inner") && d.Name != "::ns::inner" && d.Name != "::ns::K::inner" {
			t.Errorf("unexpected proc def %s", d.Name)
		}
		if d.Kind == tcl.DefProc && (strings.HasSuffix(d.Name, "inner2") || strings.HasSuffix(d.Name, "inner3")) {
			t.Errorf("proc in a wrapper's body indexed as %s, want no def", d.Name)
		}
	}
}
