package index

import "testing"

// DefinesCommand follows each file's definitions through re-indexing and
// removal, counting a name once per file however many files define it.
func TestDefinesCommandTracksFiles(t *testing.T) {
	ix := New()
	ix.IndexFile("/a.tcl", "namespace eval ::u { proc set {args} {} }\n")
	ix.IndexFile("/b.tcl", "rename myset set\n")
	if !ix.DefinesCommand("set") {
		t.Fatal("set defined in two files: want true")
	}
	ix.IndexFile("/a.tcl", "proc other {} {}\n")
	if !ix.DefinesCommand("set") || !ix.DefinesCommand("other") {
		t.Fatal("after re-indexing a.tcl, b.tcl still creates set and a.tcl defines other")
	}
	ix.RemoveFile("/b.tcl")
	if ix.DefinesCommand("set") {
		t.Fatal("no file defines set any more: want false")
	}
	ix.IndexFile("/c.tcl", "itcl::class K { method get {} {}; constructor {} {} }\n")
	if !ix.DefinesCommand("get") || !ix.DefinesCommand("K") || ix.DefinesCommand("constructor") {
		t.Fatal("a method and its class are commands; a constructor is not")
	}
}
