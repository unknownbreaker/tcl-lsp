package index

import "testing"

// Re-indexing a file after the `global` line is deleted must drop the
// promoted ::cfg definition -- RemoveFile walks ix.fileDefs[path], which is
// populated from unit.Defs (post-promotion), so this should already hold; this
// is a regression guard for that bookkeeping path specifically for promoted
// defs (a def whose FullStart/FullEnd are both zero and whose Scope is
// nonzero, unlike ordinary DefNamespaceVar entries).
func TestPromotedDef_RemovedOnReindexAfterGlobalLineDeleted(t *testing.T) {
	ix := New()
	withGlobal := "proc init {} {\n  global cfg\n  set cfg 1\n}\n"
	ix.IndexFile("f.tcl", withGlobal)

	if locs := ix.Lookup("::cfg"); len(locs) == 0 {
		t.Fatalf("setup: expected ::cfg to be indexed before edit, got none")
	}

	withoutGlobal := "proc init {} {\n  set cfg 1\n}\n" // global link removed
	ix.IndexFile("f.tcl", withoutGlobal)

	if locs := ix.Lookup("::cfg"); len(locs) != 0 {
		t.Errorf("BUG: ::cfg definition survived after the `global cfg` line was deleted: %#v", locs)
	}
}

// Same, but via RemoveFile directly (no re-index).
func TestPromotedDef_RemovedOnRemoveFile(t *testing.T) {
	ix := New()
	ix.IndexFile("f.tcl", "proc init {} {\n  global cfg\n  set cfg 1\n}\n")
	if locs := ix.Lookup("::cfg"); len(locs) == 0 {
		t.Fatalf("setup: expected ::cfg to be indexed, got none")
	}
	ix.RemoveFile("f.tcl")
	if locs := ix.Lookup("::cfg"); len(locs) != 0 {
		t.Errorf("BUG: ::cfg definition survived RemoveFile: %#v", locs)
	}
}
