package lsp

import (
	"bytes"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
)

// codes returns the Check ids of a publish's diagnostics, sorted.
func codes(p PublishDiagnosticsParams) []string {
	out := []string{}
	for _, d := range p.Diagnostics {
		out = append(out, d.Code)
	}
	sort.Strings(out)
	return out
}

// A document with one self-assignment, one self-comparison and one
// unreachable-code finding.
const togglesDoc = "set x $x\nif {$y eq $y} {}\nproc p {} {\n    return\n    puts a\n}\n"

// Turning one Check off stops only that Check's diagnostics; an id set true,
// or one no Check has, changes nothing.
func TestDiagnosticsOneCheckOff(t *testing.T) {
	for _, tc := range []struct {
		name   string
		checks map[string]bool
		want   []string
	}{
		{"none set", nil, []string{"self-assignment", "self-comparison", "unreachable-code"}},
		{"one off", map[string]bool{"self-assignment": false}, []string{"self-comparison", "unreachable-code"}},
		{"hint off", map[string]bool{"unreachable-code": false}, []string{"self-assignment", "self-comparison"}},
		{"on is the default", map[string]bool{"self-assignment": true}, []string{"self-assignment", "self-comparison", "unreachable-code"}},
		{"unknown ignored", map[string]bool{"no-such-check": false}, []string{"self-assignment", "self-comparison", "unreachable-code"}},
		{"two off", map[string]bool{"self-assignment": false, "self-comparison": false, "no-such-check": true}, []string{"unreachable-code"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var in bytes.Buffer
			in.Write(frame(t, "initialize", 1, InitializeParams{InitializationOptions: InitializationOptions{Checks: tc.checks}}))
			open(t, &in, "file:///a.tcl", togglesDoc)
			in.Write(frame(t, "exit", nil, nil))
			pubs := published(t, runServer(t, in.Bytes()))
			if len(pubs) != 1 {
				t.Fatalf("got %d publishes, want 1: %+v", len(pubs), pubs)
			}
			if got := codes(pubs[0]); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// A document whose only findings come from an off Check publishes an empty
// list, and a redefinition elsewhere that changes only those findings
// republishes nothing.
func TestDiagnosticsOffCheckNeverPublishes(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{InitializationOptions: InitializationOptions{
		Checks: map[string]bool{"self-assignment": false}}}))
	open(t, &in, "file:///a.tcl", "set x $x\n")
	open(t, &in, "file:///lib.tcl", "proc set {args} {}\n")
	in.Write(frame(t, "exit", nil, nil))
	pubs := published(t, runServer(t, in.Bytes()))
	var forA int
	for _, p := range pubs {
		if len(p.Diagnostics) != 0 {
			t.Errorf("%s: got %+v, want none", p.URI, p.Diagnostics)
		}
		if p.URI == "file:///a.tcl" {
			forA++
		}
	}
	if forA != 1 {
		t.Errorf("a.tcl published %d times, want once (on open)", forA)
	}
}

// The master switch off wins over a Check set on.
func TestDiagnosticsMasterOffBeatsCheckOn(t *testing.T) {
	off := false
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{InitializationOptions: InitializationOptions{
		Diagnostics: &off, Checks: map[string]bool{"self-assignment": true}}}))
	open(t, &in, "file:///a.tcl", "set x $x\n")
	in.Write(frame(t, "exit", nil, nil))
	if pubs := published(t, runServer(t, in.Bytes())); len(pubs) != 0 {
		t.Fatalf("got %+v, want no publishes", pubs)
	}
}

// The checks option decodes from the JSON a client sends.
func TestChecksOptionDecodes(t *testing.T) {
	for _, tc := range []struct {
		json string
		want map[string]bool
	}{
		{`{}`, nil},
		{`{"checks": {}}`, map[string]bool{}},
		{`{"checks": {"self-assignment": false, "unreachable-code": true}}`,
			map[string]bool{"self-assignment": false, "unreachable-code": true}},
	} {
		var o InitializationOptions
		if err := json.Unmarshal([]byte(tc.json), &o); err != nil {
			t.Fatalf("%s: %v", tc.json, err)
		}
		if !reflect.DeepEqual(map[string]bool(o.Checks), tc.want) {
			t.Errorf("%s: Checks = %v, want %v", tc.json, o.Checks, tc.want)
		}
	}
}

// A malformed checks value, or an entry that is not a JSON boolean, leaves the
// other options intact and the Checks it names on.
func TestChecksOptionMalformed(t *testing.T) {
	for _, raw := range []string{
		`{"diagnostics": true, "checks": ["self-assignment"]}`,
		`{"diagnostics": true, "checks": {"self-assignment": 0}}`,
		`{"diagnostics": true, "checks": "self-assignment"}`,
		`{"diagnostics": true, "checks": {"self-assignment": "false"}}`,
		`{"diagnostics": true, "checks": {"self-assignment": null}}`,
	} {
		var in bytes.Buffer
		in.Write(frame(t, "initialize", 1, json.RawMessage(`{"initializationOptions": `+raw+`}`)))
		open(t, &in, "file:///a.tcl", "set x $x\n")
		in.Write(frame(t, "exit", nil, nil))
		pubs := published(t, runServer(t, in.Bytes()))
		if len(pubs) != 1 || len(pubs[0].Diagnostics) != 1 {
			t.Errorf("%s: got %+v, want the self-assignment finding", raw, pubs)
		}
	}
}

// A malformed checks value is no decoding error: options after it still decode.
func TestChecksOptionMalformedKeepsLaterOptions(t *testing.T) {
	for _, checks := range []string{`[1, 2]`, `"self-assignment"`, `7`, `{"a": [1]}`} {
		var o InitializationOptions
		raw := `{"checks": ` + checks + `, "diagnostics": false, "extraIndexPaths": ["/a"]}`
		if err := json.Unmarshal([]byte(raw), &o); err != nil {
			t.Errorf("%s: %v, want no error", raw, err)
		}
		if o.Diagnostics == nil || *o.Diagnostics || !reflect.DeepEqual(o.ExtraIndexPaths, []string{"/a"}) {
			t.Errorf("%s: got diagnostics %v, extraIndexPaths %v; want false and [/a]", raw, o.Diagnostics, o.ExtraIndexPaths)
		}
	}
}

// Toggles apply to .rvt templates as to .tcl files.
func TestDiagnosticsCheckOffInRvt(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{InitializationOptions: InitializationOptions{
		Checks: map[string]bool{"self-assignment": false}}}))
	open(t, &in, "file:///page.rvt", "<? set x $x ?>\n<? if {$y eq $y} {} ?>\n")
	in.Write(frame(t, "exit", nil, nil))
	pubs := published(t, runServer(t, in.Bytes()))
	if len(pubs) != 1 {
		t.Fatalf("got %d publishes, want 1: %+v", len(pubs), pubs)
	}
	if got, want := codes(pubs[0]), []string{"self-comparison"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}
