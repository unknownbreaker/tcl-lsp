package tcl

import "testing"

// TestScriptEndsTruncation pins the memo's length guards (scriptends.go): an end
// found in a longer text is "none" for a prefix that stops before it, and a
// failure in a short text does not answer a longer one. No caller reaches these
// today, since nested texts are interiors of the scripts that found their ends,
// so only this test notices if the guards go.
func TestScriptEndsTruncation(t *testing.T) {
	full := "[list [set x 1]] tail"
	m := &scriptEnds{}
	if e, ok := m.scriptClose(full, 1); !ok || e != 15 {
		t.Fatalf("full: got %d %v, want 15 true", e, ok)
	}
	if _, ok := m.scriptClose(full[:10], 1); ok {
		t.Errorf("a prefix that stops before both `]` reused the cached end")
	}
	if e, ok := m.scriptClose(full[:16], 1); !ok || e != 15 {
		t.Errorf("prefix holding the `]`: got %d %v, want 15 true", e, ok)
	}

	m = &scriptEnds{}
	if _, ok := m.scriptClose(full[:10], 1); ok {
		t.Fatalf("short text: closed, want none")
	}
	if e, ok := m.scriptClose(full, 1); !ok || e != 15 {
		t.Errorf("longer text after a cached short failure: got %d %v, want 15 true", e, ok)
	}

	// `{*}` at the end of a truncated text is the literal word `*`; in the
	// longer text it is the expansion prefix.
	s := "[list {*}[x]] z"
	want, wantOK := scriptClose(s, 1)
	m = &scriptEnds{}
	m.scriptClose(s[:9], 1)
	if e, ok := m.scriptClose(s, 1); e != want || ok != wantOK {
		t.Errorf("{*} boundary: got %d %v, want %d %v", e, ok, want, wantOK)
	}
}
