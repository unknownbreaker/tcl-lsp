package resolve

import (
	"sort"
	"strings"
	"testing"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// research/08, miss 4: find-references must list a bare intra-class call to an
// Itcl method, the way goto-definition resolves it. In tclsh 8.6 and 9.0 (Itcl
// 4.3.7) a bare `logger` in K's method runs Base::logger, one in Over's method runs
// Over's override, and only the top-level call runs the global proc.

const methodRefsBase = `proc logger {args} {}
itcl::class Base {
    method logger {m} {}
}
`

const methodRefsUse = `itcl::class K {
    inherit Base
    method run {} { logger hi }
}
itcl::class Over {
    inherit Base
    method logger {m} {}
    method run {} { logger hi }
}
itcl::body Over::logger {m} { puts $m }
logger top
`

func methodRefsResolver() *Resolver {
	ix := index.New()
	ix.IndexFile("base.tcl", methodRefsBase)
	ix.IndexFile("use.tcl", methodRefsUse)
	return New(ix)
}

// msite is a (file, start) pair, for order-free comparison of locations.
type msite struct {
	file  string
	start int
}

func methodSitesOf(locs []index.Location) []msite {
	var out []msite
	for _, l := range locs {
		out = append(out, msite{l.File, l.NameStart})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].file != out[j].file {
			return out[i].file < out[j].file
		}
		return out[i].start < out[j].start
	})
	return out
}

func wantMethodSites(t *testing.T, what string, got []index.Location, want ...msite) {
	t.Helper()
	g := methodSitesOf(got)
	sort.Slice(want, func(i, j int) bool {
		if want[i].file != want[j].file {
			return want[i].file < want[j].file
		}
		return want[i].start < want[j].start
	})
	if len(g) != len(want) {
		t.Fatalf("%s = %v, want %v", what, g, want)
	}
	for i := range g {
		if g[i] != want[i] {
			t.Fatalf("%s = %v, want %v", what, g, want)
		}
	}
}

// nth returns the offset of the n-th (0-based) occurrence of sub in s.
func nthIndex(s, sub string, n int) int {
	off := -1
	for i := 0; i <= n; i++ {
		j := strings.Index(s[off+1:], sub)
		if j < 0 {
			return -1
		}
		off += 1 + j
	}
	return off
}

var (
	baseDef     = msite{"base.tcl", strings.Index(methodRefsBase, "logger {m}")}
	globalDef   = msite{"base.tcl", strings.Index(methodRefsBase, "logger {args}")}
	kCall       = msite{"use.tcl", nthIndex(methodRefsUse, "logger hi", 0)}
	overDef     = msite{"use.tcl", strings.Index(methodRefsUse, "logger {m}")}
	overCall    = msite{"use.tcl", nthIndex(methodRefsUse, "logger hi", 1)}
	overBodyDef = msite{"use.tcl", strings.Index(methodRefsUse, "logger {m} { puts")}
	topCall     = msite{"use.tcl", strings.Index(methodRefsUse, "logger top")}
)

func TestReferencesFromMethodDefinition(t *testing.T) {
	r := methodRefsResolver()
	wantMethodSites(t, "References(Base logger def)", r.References("base.tcl", methodRefsBase, baseDef.start), kCall)
	wantMethodSites(t, "References(Over logger def)", r.References("use.tcl", methodRefsUse, overDef.start), overCall)
	wantMethodSites(t, "References(itcl::body Over::logger)", r.References("use.tcl", methodRefsUse, overBodyDef.start), overCall)
}

func TestReferencesFromIntraClassCall(t *testing.T) {
	r := methodRefsResolver()
	wantMethodSites(t, "References(K call)", r.References("use.tcl", methodRefsUse, kCall.start), kCall)
	wantMethodSites(t, "Declarations(K call)", r.Declarations("use.tcl", methodRefsUse, kCall.start), baseDef)
	wantMethodSites(t, "Declarations(Over call)", r.Declarations("use.tcl", methodRefsUse, overCall.start), overDef, overBodyDef)
	for _, l := range r.References("use.tcl", methodRefsUse, kCall.start) {
		if l.Kind != tcl.DefMethod {
			t.Fatalf("reference kind = %v, want DefMethod", l.Kind)
		}
	}
}

// The global proc's references must not include calls that run a method.
func TestReferencesOfGlobalProcSkipMethodCalls(t *testing.T) {
	r := methodRefsResolver()
	wantMethodSites(t, "References(global logger def)", r.References("base.tcl", methodRefsBase, globalDef.start), topCall)
	wantMethodSites(t, "References(top call)", r.References("use.tcl", methodRefsUse, topCall.start), topCall)
	wantMethodSites(t, "Declarations(top call)", r.Declarations("use.tcl", methodRefsUse, topCall.start), globalDef)
}

func TestFileHighlightsOfMethod(t *testing.T) {
	r := methodRefsResolver()
	wantMethodSites(t, "FileHighlights(Over call)", r.FileHighlights("use.tcl", methodRefsUse, overCall.start), overDef, overCall, overBodyDef)
	wantMethodSites(t, "FileHighlights(K call)", r.FileHighlights("use.tcl", methodRefsUse, kCall.start), kCall)
	wantMethodSites(t, "FileHighlights(top call)", r.FileHighlights("use.tcl", methodRefsUse, topCall.start), topCall)
}

// A custom command's body inside a method may run in the method (a wrapper) or
// elsewhere: `after 0 {logger}` and `uplevel #0 {logger}` run the global proc in
// Tcl 8.6 and 9.0. Where the class has a method of that name, such a call answers
// nothing, so it is a reference of neither. A body that runs here (`if`, loops)
// stays the method's, even nested inside the wrapper's body it is not.
const wrappedSrc = `proc logger {args} {}
itcl::class K {
    method logger {m} {}
    method viaAfter {} { after 0 { logger after } }
    method viaUplevel {} { uplevel #0 { logger uplevel0 } }
    method viaWrap {} { with_lock { foreach x {1} { if 1 { logger nested } } } }
    method viaIf {} { if 1 { foreach x {1} { logger inif } } }
    method viaNs {} { namespace eval :: { logger nseval } }
    method direct {} { logger direct }
}
`

func TestWrappedIntraClassCallsAreSilent(t *testing.T) {
	ix := index.New()
	ix.IndexFile("k.tcl", wrappedSrc)
	r := New(ix)
	at := func(sub string) int { return strings.Index(wrappedSrc, sub) }
	method := msite{"k.tcl", at("logger {m}")}
	global := msite{"k.tcl", at("logger {args}")}

	wantMethodSites(t, "References(method)", r.References("k.tcl", wrappedSrc, method.start),
		msite{"k.tcl", at("logger inif")}, msite{"k.tcl", at("logger direct")})
	wantMethodSites(t, "References(global proc)", r.References("k.tcl", wrappedSrc, global.start),
		msite{"k.tcl", at("logger nseval")})
	for _, call := range []string{"logger after", "logger uplevel0", "logger nested"} {
		if d := r.Definition("k.tcl", wrappedSrc, at(call)); len(d) != 0 {
			t.Errorf("Definition(%q) = %v, want silence", call, d)
		}
		if refs := r.References("k.tcl", wrappedSrc, at(call)); len(refs) != 0 {
			t.Errorf("References(%q) = %v, want silence", call, refs)
		}
	}
}
