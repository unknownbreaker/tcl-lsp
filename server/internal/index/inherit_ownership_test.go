package index

// Regression: when TWO files assert the same inherit edge for a class, the
// edge must survive removal of either file as long as one asserting file
// remains. The old bookkeeping credited only the FIRST-indexed file with the
// edge ("newEdges"), so removing that file silently dropped the edge -- and
// with it method resolution through the base class -- even though the other
// file still declared it.

import "testing"

const classA = "itcl::class ::C {\n  inherit ::Base\n}\n"
const classB = "itcl::class ::C {\n  inherit ::Base\n}\n"
const baseSrc = "itcl::class ::Base {\n  method greet {} {}\n}\n"

func hasInherit(ci *ClassInfo, base string) bool {
	if ci == nil {
		return false
	}
	for _, b := range ci.Inherit {
		if b == base {
			return true
		}
	}
	return false
}

func TestInheritEdgeSurvivesFirstWriterRemoval(t *testing.T) {
	ix := New()
	ix.IndexFile("base.tcl", baseSrc)
	ix.IndexFile("a.tcl", classA)
	ix.IndexFile("b.tcl", classB)
	if !hasInherit(ix.Class("::C"), "::Base") {
		t.Fatal("precondition: ::C should inherit ::Base after indexing both files")
	}

	ix.RemoveFile("a.tcl") // the first writer -- b.tcl still asserts the edge
	if !hasInherit(ix.Class("::C"), "::Base") {
		t.Errorf("inherit edge ::C -> ::Base lost after removing a.tcl, though b.tcl still declares it: %#v",
			ix.Class("::C"))
	}

	ix.RemoveFile("b.tcl") // now no file asserts it
	if hasInherit(ix.Class("::C"), "::Base") {
		t.Errorf("inherit edge ::C -> ::Base survived removal of every asserting file: %#v",
			ix.Class("::C"))
	}
}

// Re-indexing a file with an UNCHANGED inherit declaration (the common
// didChange path: IndexFile calls RemoveFile then re-adds) must keep the edge.
func TestInheritEdgeSurvivesReindex(t *testing.T) {
	ix := New()
	ix.IndexFile("base.tcl", baseSrc)
	ix.IndexFile("a.tcl", classA)
	ix.IndexFile("b.tcl", classB)

	ix.IndexFile("a.tcl", classA) // remove+re-add under the hood
	if !hasInherit(ix.Class("::C"), "::Base") {
		t.Errorf("inherit edge lost across a no-op re-index of a.tcl: %#v", ix.Class("::C"))
	}

	// And an edit that REMOVES the inherit from a.tcl must still keep it,
	// because b.tcl asserts it.
	ix.IndexFile("a.tcl", "itcl::class ::C {\n  method extra {} {}\n}\n")
	if !hasInherit(ix.Class("::C"), "::Base") {
		t.Errorf("inherit edge lost after a.tcl dropped it, though b.tcl still declares it: %#v",
			ix.Class("::C"))
	}
}
