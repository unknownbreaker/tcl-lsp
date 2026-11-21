package lsp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// attackSession runs the server over a scripted list of client messages, to EOF, and
// returns every message the server wrote.
func attackSession(t *testing.T, msgs []*Message) []*Message {
	t.Helper()
	var in bytes.Buffer
	wc := NewConn(nil, &in)
	for _, m := range msgs {
		if err := wc.Write(m); err != nil {
			t.Fatal(err)
		}
	}
	var out bytes.Buffer
	srv := NewServer(NewConn(&in, &out))
	if err := srv.Run(); err != nil {
		t.Fatal(err)
	}
	rc := NewConn(&out, io.Discard)
	var got []*Message
	for {
		m, err := rc.Read()
		if err != nil {
			break
		}
		got = append(got, m)
	}
	return got
}

func attackReq(id int, method string, params any) *Message {
	raw, _ := json.Marshal(params)
	m := &Message{Method: method, Params: raw}
	if id > 0 {
		m.ID = json.RawMessage(fmt.Sprint(id))
	}
	return m
}

func attackInit(rootURI string, opts map[string]any) *Message {
	p := map[string]any{"rootUri": rootURI, "capabilities": map[string]any{}}
	if opts != nil {
		p["initializationOptions"] = opts
	}
	return attackReq(1, "initialize", p)
}

func attackOpen(uri, text string) *Message {
	return attackReq(0, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "text": text, "languageId": "tcl", "version": 1}})
}

func attackChange(uri, text string) *Message {
	return attackReq(0, "textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": 2},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

func attackClose(uri string) *Message {
	return attackReq(0, "textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
}

// attackPublishes returns the publishDiagnostics notifications, in order.
func attackPublishes(t *testing.T, got []*Message) []PublishDiagnosticsParams {
	t.Helper()
	var out []PublishDiagnosticsParams
	for _, m := range got {
		if m.Method != "textDocument/publishDiagnostics" {
			continue
		}
		var p PublishDiagnosticsParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

// lastFor returns the last publish for uri and whether there was one.
func lastFor(ps []PublishDiagnosticsParams, uri string) (PublishDiagnosticsParams, bool) {
	for i := len(ps) - 1; i >= 0; i-- {
		if ps[i].URI == uri {
			return ps[i], true
		}
	}
	return PublishDiagnosticsParams{}, false
}

// A command redefined by another open document makes an earlier finding wrong:
// `set` now calls user code. The client must not be left showing it.
func TestStaleDiagnosticAfterSetRedefinedElsewhere(t *testing.T) {
	root := t.TempDir()
	a := pathToURI(filepath.Join(root, "a.tcl"))
	b := pathToURI(filepath.Join(root, "b.tcl"))
	got := attackSession(t, []*Message{
		attackInit(pathToURI(root), nil),
		attackReq(0, "initialized", map[string]any{}),
		attackOpen(a, "set x $x\n"),
		attackOpen(b, "rename set _set\nproc set {name args} { _set ::log $name }\n"),
		attackReq(0, "exit", nil),
	})
	ps := attackPublishes(t, got)
	first, ok := lastFor(ps[:1], a)
	if !ok || len(first.Diagnostics) != 1 {
		t.Fatalf("setup: want one finding published for a.tcl first, got %+v", ps)
	}
	last, _ := lastFor(ps, a)
	if len(last.Diagnostics) != 0 {
		t.Errorf("a.tcl still shows %d diagnostic(s) after b.tcl redefined set: %+v", len(last.Diagnostics), last.Diagnostics)
	}
}

// Same, through an on-disk file the client reports via didChangeWatchedFiles.
func TestStaleDiagnosticAfterWatchedFileRedefinesSet(t *testing.T) {
	root := t.TempDir()
	a := pathToURI(filepath.Join(root, "a.tcl"))
	bPath := filepath.Join(root, "b.tcl")
	if err := os.WriteFile(bPath, []byte("proc set {name args} {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := attackSession(t, []*Message{
		attackInit(pathToURI(t.TempDir()), nil), // b.tcl not in this root: arrives by watch
		attackOpen(a, "set x $x\n"),
		attackReq(0, "workspace/didChangeWatchedFiles", map[string]any{"changes": []map[string]any{{"uri": pathToURI(bPath), "type": FileChangeCreated}}}),
		attackReq(0, "exit", nil),
	})
	ps := attackPublishes(t, got)
	last, ok := lastFor(ps, a)
	if !ok {
		t.Fatal("setup: nothing published for a.tcl")
	}
	if len(last.Diagnostics) != 0 {
		t.Errorf("a.tcl still shows %d diagnostic(s) after a watched file redefined set", len(last.Diagnostics))
	}
}

// .rvt ranges are in template coordinates, characters in UTF-16 code units.
func TestRVTRangeUTF16(t *testing.T) {
	root := t.TempDir()
	u := pathToURI(filepath.Join(root, "p.rvt"))
	text := "<p>é😀</p>\n<b>ü</b><? puts 1; set x $x ?>\n"
	got := attackSession(t, []*Message{
		attackInit(pathToURI(root), nil),
		attackOpen(u, text),
		attackReq(0, "exit", nil),
	})
	last, ok := lastFor(attackPublishes(t, got), u)
	if !ok || len(last.Diagnostics) != 1 {
		t.Fatalf("want one diagnostic, got %+v", last)
	}
	// line 1: `<b>ü</b><? puts 1; ` = 3+1+4+3+8 = 19 UTF-16 units before `set`.
	r := last.Diagnostics[0].Range
	if r.Start.Line != 1 || r.Start.Character != 19 || r.End.Line != 1 || r.End.Character != 27 {
		t.Errorf("range = %+v, want 1:19-1:27", r)
	}
}

// Master switch off: nothing on open, change or close.
func TestMasterSwitchOffAllPaths(t *testing.T) {
	root := t.TempDir()
	u := pathToURI(filepath.Join(root, "a.tcl"))
	r := pathToURI(filepath.Join(root, "a.rvt"))
	got := attackSession(t, []*Message{
		attackInit(pathToURI(root), map[string]any{"diagnostics": false, "extraIndexPaths": "not-a-list"}),
		attackOpen(u, "set x $x\n"),
		attackChange(u, "set y $y\n"),
		attackOpen(r, "<? set x $x ?>"),
		attackClose(u),
		attackClose(r),
		attackReq(0, "exit", nil),
	})
	if ps := attackPublishes(t, got); len(ps) != 0 {
		t.Errorf("master switch off, yet %d publish(es): %+v", len(ps), ps)
	}
}

// Change clears, close clears, reopen republishes.
func TestChangeCloseReopen(t *testing.T) {
	root := t.TempDir()
	u := pathToURI(filepath.Join(root, "a.tcl"))
	got := attackSession(t, []*Message{
		attackInit(pathToURI(root), nil),
		attackOpen(u, "set x $x\n"),
		attackChange(u, "set x $y\n"),
		attackChange(u, "set x $x\nset y $y\n"),
		attackClose(u),
		attackOpen(u, "set z $z\n"),
		attackReq(0, "exit", nil),
	})
	ps := attackPublishes(t, got)
	var counts []int
	for _, p := range ps {
		counts = append(counts, len(p.Diagnostics))
	}
	if fmt.Sprint(counts) != "[1 0 2 0 1]" {
		t.Errorf("publish counts = %v, want [1 0 2 0 1]", counts)
	}
}

// Per-change latency: a document with some findings in a large workspace. The
// finding filter must not scan the whole workspace per finding.
func TestChangeLatencyLargeWorkspace(t *testing.T) {
	if testing.Short() {
		t.Skip("perf")
	}
	root := t.TempDir()
	for f := 0; f < 200; f++ {
		var b strings.Builder
		fmt.Fprintf(&b, "namespace eval ns%d {\n", f)
		for i := 0; i < 500; i++ {
			fmt.Fprintf(&b, "  variable v%d 1\n  proc p%d {} { return 1 }\n", i, i)
		}
		b.WriteString("}\n")
		if err := os.WriteFile(filepath.Join(root, fmt.Sprintf("f%d.tcl", f)), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var doc strings.Builder
	for i := 0; i < 200; i++ {
		fmt.Fprintf(&doc, "set v%d $v%d\n", i, i)
	}
	u := pathToURI(filepath.Join(root, "open.tcl"))
	var in bytes.Buffer
	wc := NewConn(nil, &in)
	_ = wc.Write(attackInit(pathToURI(root), nil))
	srv := NewServer(NewConn(&in, io.Discard))
	for {
		m, err := srv.conn.Read()
		if err != nil {
			break
		}
		srv.dispatch(m)
	}
	srv.conn = NewConn(nil, io.Discard)
	clean := strings.ReplaceAll(doc.String(), "$v", "$w")
	timeIt := func(text string) time.Duration {
		start := time.Now()
		for i := 0; i < 5; i++ {
			srv.setDoc(u, text)
		}
		return time.Since(start) / 5
	}
	base := timeIt(clean)
	withFindings := timeIt(doc.String())
	t.Logf("per change: no findings %v, 200 findings %v", base, withFindings)
	if withFindings > 3*base+20*time.Millisecond {
		t.Errorf("per-change latency with 200 findings is %v vs %v without: the filter scales with workspace size × findings", withFindings, base)
	}
}
