package tcl

import (
	"strings"
	"testing"
)

// promotedFor returns the Origin of the promoted DefNamespaceVar whose NameStart
// equals the byte offset of needle in src (the assignment site), or "" if none.
func promotedFor(t *testing.T, src, needle string) string {
	t.Helper()
	off := strings.Index(src, needle)
	if off < 0 {
		t.Fatalf("needle %q not found", needle)
	}
	for _, d := range FileDefs(src) {
		if d.Kind == DefNamespaceVar && d.NameStart == off {
			return d.Name
		}
	}
	return ""
}

// promoteLinkedAssignments must attribute each assignment to the NEAREST
// PRECEDING link for its name, matching TCL's dynamic last-write-wins link
// semantics. Regression guard: an earlier version used the scope-wide EARLIEST
// link, so when an alias name was rebound mid-proc (`global cfg` then
// `upvar #0 other cfg`, or vice versa), assignments after the rebind were
// attributed to the stale first link's origin.
func TestPromoteLinkedAssignments_StaleOriginAfterRebind_GlobalThenUpvar(t *testing.T) {
	src := "proc p {} {\n" +
		"  global cfg\n" + // cfg -> ::cfg
		"  upvar #0 other cfg\n" + // cfg REBOUND -> ::other
		"  set cfg 1\n" + // must attribute to ::other (the live binding), not ::cfg
		"}\n"
	got := promotedFor(t, src, "cfg 1")
	if got != "::other" {
		t.Errorf("promoted origin = %q, want %q (rebind via upvar #0 after global was ignored)", got, "::other")
	}
}

// Symmetric case: upvar #0 first, then global reusing the same alias name.
// Assignments after the global should attribute to ::cfg, not the earlier
// upvar's ::other.
func TestPromoteLinkedAssignments_StaleOriginAfterRebind_UpvarThenGlobal(t *testing.T) {
	src := "proc p {} {\n" +
		"  upvar #0 other cfg\n" + // cfg -> ::other
		"  global cfg\n" + // cfg REBOUND -> ::cfg
		"  set cfg 1\n" + // must attribute to ::cfg (the live binding), not ::other
		"}\n"
	got := promotedFor(t, src, "cfg 1")
	if got != "::cfg" {
		t.Errorf("promoted origin = %q, want %q (rebind via global after upvar #0 was ignored)", got, "::cfg")
	}
}

// `variable cfg` inside a namespace proc links the local name to the namespace
// variable exactly as `global` links to ::cfg -- so a subsequent `set cfg`
// (which writes ::app::cfg in real TCL) gets a promoted workspace definition at
// the assignment site, symmetric with the `global`+`set` init-proc idiom.
func TestPromoteLinkedAssignments_VariableLinkAssignmentPromoted(t *testing.T) {
	src := "namespace eval ::app {\n" +
		"  proc init {} {\n" +
		"    variable cfg\n" +
		"    set cfg 1\n" + // the actual write site
		"  }\n" +
		"}\n"
	got := promotedFor(t, src, "cfg 1")
	if got != "::app::cfg" {
		t.Errorf("promoted origin = %q, want %q (variable-link assignment should promote like global-link)", got, "::app::cfg")
	}
}
