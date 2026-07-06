package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// extra_index_paths (initializationOptions.extraIndexPaths): external dirs are
// statically indexed at startup -- goto-def jumps into them -- with missing
// paths skipped (a shared editor config may name dirs some machines lack).
func TestServerExtraIndexPaths(t *testing.T) {
	root := t.TempDir()
	extDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(extDir, "pkg.tcl"), []byte("proc extpkg_helper {} {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "caller.tcl"), []byte("extpkg_helper\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{
		RootURI: pathToURI(root),
		InitializationOptions: InitializationOptions{
			ExtraIndexPaths: []string{extDir, "/no/such/dir/on/this/machine"},
		},
	}))
	in.Write(frame(t, "textDocument/definition", 2, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(filepath.Join(root, "caller.tcl"))},
		Position:     Position{Line: 0, Character: 0},
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")
	var locs []Location
	_ = json.Unmarshal(resp.Result, &locs)
	if len(locs) != 1 || locs[0].URI != pathToURI(filepath.Join(extDir, "pkg.tcl")) {
		t.Fatalf("goto-def should jump into the extra-indexed dir; got %#v", locs)
	}
}

// A single FILE (not a dir) as an extra index path is indexed by itself.
func TestServerExtraIndexPathsSingleFile(t *testing.T) {
	root := t.TempDir()
	extDir := t.TempDir()
	extFile := filepath.Join(extDir, "solo.tcl")
	if err := os.WriteFile(extFile, []byte("proc solo_helper {} {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "caller.tcl"), []byte("solo_helper\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{
		RootURI:               pathToURI(root),
		InitializationOptions: InitializationOptions{ExtraIndexPaths: []string{extFile}},
	}))
	in.Write(frame(t, "textDocument/definition", 2, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(filepath.Join(root, "caller.tcl"))},
		Position:     Position{Line: 0, Character: 0},
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")
	var locs []Location
	_ = json.Unmarshal(resp.Result, &locs)
	if len(locs) != 1 || locs[0].URI != pathToURI(extFile) {
		t.Fatalf("goto-def should jump into the extra-indexed file; got %#v", locs)
	}
}
