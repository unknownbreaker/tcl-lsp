package resolve

import (
	"slices"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// A relative qualified command name (`a::b`, no leading `::`) resolves like a bare
// one, minus imports: relative to the current namespace first, then to each
// `namespace path` entry in order, then to the global namespace. tclsh 8.6.18 and
// 9.0.3 agree on every case below (research/experiments/08_relative_qualified.tcl
// and research/08, miss 1). Bug hunted: a qualified name that resolves only
// relative to the current namespace, a fallback that wins over the current-relative
// target, a search of parent namespaces, and a fallback for an absolute name.
func TestRelativeQualifiedCommands(t *testing.T) {
	const prom = "namespace eval ::prom {\n  proc helper {} {}\n}\n"
	cases := []struct {
		name, src, mark string
		want            []int
	}{
		{"falls back to global", prom + "namespace eval ::other {\n  proc caller {} {\n    prom::helper\n  }\n}", "prom::helper", []int{2}},
		{"falls back at namespace frame", prom + "namespace eval ::other {\n  prom::helper\n}", "prom::helper", []int{2}},
		{"falls back from a deep namespace", prom + "namespace eval ::a::b::c {\n  proc f {} {\n    prom::helper\n  }\n}", "prom::helper", []int{2}},
		{"falls back in a bracket", prom + "namespace eval ::other {\n  proc f {} {\n    set v [prom::helper]\n  }\n}", "prom::helper", []int{2}},
		{"current namespace wins", prom + "namespace eval ::other::prom {\n  proc helper {} {}\n}\nnamespace eval ::other {\n  proc caller {} {\n    prom::helper\n  }\n}", "prom::helper", []int{5}},
		{"current namespace wins over path", "namespace eval ::lib::x {\n  proc p {} {}\n}\nnamespace eval ::other::x {\n  proc p {} {}\n}\nnamespace eval ::other {\n  namespace path ::lib\n  proc c {} {\n    x::p\n  }\n}", "x::p", []int{5}},
		{"path wins over global", "namespace eval ::x {\n  proc p {} {}\n}\nnamespace eval ::lib::x {\n  proc p {} {}\n}\nnamespace eval ::other {\n  namespace path ::lib\n  proc c {} {\n    x::p\n  }\n}", "x::p", []int{5}},
		{"path order", "namespace eval ::lib2::y {\n  proc p {} {}\n}\nnamespace eval ::lib3::y {\n  proc p {} {}\n}\nnamespace eval ::o {\n  namespace path {::lib3 ::lib2}\n  proc c {} {\n    y::p\n  }\n}", "y::p", []int{5}},
		{"multi-segment through path", "namespace eval ::lib::m::n {\n  proc p {} {}\n}\nnamespace eval ::o {\n  namespace path ::lib\n  proc c {} {\n    m::n::p\n  }\n}", "m::n::p", []int{2}},
		// Tcl never searches a parent namespace: `c::p` from ::a::b is an error.
		{"parent not searched", "namespace eval ::a::c {\n  proc p {} {}\n}\nnamespace eval ::a::b {\n  proc f {} {\n    c::p\n  }\n}", "c::p", nil},
		// Imports bring in commands, not namespaces: `x::p` is an error.
		{"imports not searched", "namespace eval ::src::x {\n  proc p {} {}\n}\nnamespace eval ::src {\n  namespace export *\n  proc q {} {}\n}\nnamespace eval ::o {\n  namespace import ::src::*\n  proc c {} {\n    x::p\n  }\n}", "x::p", nil},
		// An absolute name means only itself.
		{"absolute has no fallback", prom + "namespace eval ::other {\n  proc caller {} {\n    ::other::prom::helper\n  }\n}", "::other::prom::helper", nil},
		{"undefined stays silent", prom + "namespace eval ::other {\n  proc caller {} {\n    prom::nope\n  }\n}", "prom::nope", nil},
		// Miss 10 stays silent: 8.6 falls back for variables and 9.0 errors.
		{"relative qualified variable stays silent", "namespace eval ::cfg {\n  variable level 3\n}\nnamespace eval ::user {\n  proc readit {} {\n    return $cfg::level\n  }\n}", "$cfg::level", nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := defLinesAt(t, c.src, c.mark); !slices.Equal(got, c.want) {
				t.Errorf("definition lines of %s = %v, want %v\nsource:\n%s", c.mark, got, c.want, c.src)
			}
		})
	}
}

// References, highlights and semantic tokens share the lookup with goto-definition,
// so the fallback reaches them too. Bug hunted: a fallback added to one path only, and
// a relative call grouped with the global proc when a current-relative one shadows it.
func TestRelativeQualifiedCommandsOtherPaths(t *testing.T) {
	src := "namespace eval ::prom {\n  proc helper {} {}\n}\n" +
		"namespace eval ::other {\n  proc caller {} {\n    prom::helper\n  }\n}\n" +
		"namespace eval ::shadow::prom {\n  proc helper {} {}\n}\n" +
		"namespace eval ::shadow {\n  proc caller {} {\n    prom::helper\n  }\n}\n"
	resolver := resolverOverGuardFile(src)
	defOffset := strings.Index(src, "helper {}")

	if got := startLinesOf(src, guardFile, resolver.References(guardFile, src, defOffset)); !slices.Equal(got, []int{6}) {
		t.Errorf("references to ::prom::helper = %v, want [6], without the shadowed call on line 14", got)
	}
	globalUse := strings.Index(src, "prom::helper\n  }")
	if got := startLinesOf(src, guardFile, resolver.FileHighlights(guardFile, src, globalUse)); !slices.Equal(got, []int{2, 6}) {
		t.Errorf("highlights from the falling-back call = %v, want [2 6]", got)
	}
	shadowUse := strings.LastIndex(src, "prom::helper")
	if got := startLinesOf(src, guardFile, resolver.FileHighlights(guardFile, src, shadowUse)); !slices.Equal(got, []int{10, 14}) {
		t.Errorf("highlights from the shadowed call = %v, want [10 14]", got)
	}

	ref := &tcl.ContextRef{Ref: tcl.Reference{Kind: tcl.RefCommand, Name: "prom::helper"}, Namespace: "::other", Frame: tcl.FrameProc}
	if kind, ok := resolver.CommandRefKind(ref, guardFile); !ok || kind != tcl.DefProc {
		t.Errorf("CommandRefKind(prom::helper in ::other) = %v, %v; want a proc", kind, ok)
	}
}

// A page's ::request namespace is recreated for each page, so a `namespace path` or
// `namespace import` one .rvt page declares there steers only that page's calls, as
// lookupScoped already does for page-local definitions. Bug hunted: path and import
// entries merged across pages, which made page b jump to the target page a's path
// chose. Found by the adversarial tester for miss 1; bare names leaked the same way.
func TestPageNamespaceDeclsStayInTheirPage(t *testing.T) {
	const lib = "namespace eval ::lib::util {\n  proc f {} {}\n}\nnamespace eval ::util {\n  proc f {} {}\n}\n" +
		"namespace eval ::lib {\n  proc helper {} {}\n}\nnamespace eval ::src {\n  namespace export *\n  proc imported {} {}\n}\n"
	const a = "<? namespace path ::lib ?>\n<? namespace import ::src::* ?>\n<? util::f ?>\n<? helper ?>\n<? imported ?>\n"
	const b = "<? util::f ?>\n<? helper ?>\n<? imported ?>\n"
	ix := index.New()
	for _, f := range []struct{ name, src string }{{"/w/lib.tcl", lib}, {"/w/a.rvt", a}, {"/w/b.rvt", b}} {
		ix.IndexFile(f.name, f.src)
	}
	resolver := New(ix)
	answer := func(file, src, mark string) []int {
		var lines []int
		for _, l := range resolver.Definition(file, src, strings.Index(src, mark)+1) {
			if l.File != "/w/lib.tcl" {
				t.Fatalf("definition of %s in %s landed in %s", mark, file, l.File)
			}
			lines = append(lines, lineOfOffset(lib, l.NameStart))
		}
		return lines
	}
	for _, c := range []struct {
		file, src, mark string
		want            []int
	}{
		{"/w/a.rvt", a, "util::f", []int{2}},
		{"/w/a.rvt", a, "helper", []int{8}},
		{"/w/a.rvt", a, "imported ", []int{12}},
		{"/w/b.rvt", b, "util::f", []int{5}},
		{"/w/b.rvt", b, "helper", nil},
		{"/w/b.rvt", b, "imported", nil},
	} {
		if got := answer(c.file, c.src, c.mark); !slices.Equal(got, c.want) {
			t.Errorf("definition of %s in %s = lib.tcl lines %v, want %v", c.mark, c.file, got, c.want)
		}
	}
	for _, c := range []struct{ mark, wantFile string }{{"proc f", "/w/a.rvt"}, {"proc helper", "/w/a.rvt"}} {
		for _, l := range resolver.References("/w/lib.tcl", lib, strings.Index(lib, c.mark)+5) {
			if l.File != c.wantFile {
				t.Errorf("references of %s include %s, want only %s", c.mark, l.File, c.wantFile)
			}
		}
	}
}
