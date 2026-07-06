package lsp

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeEnvFile writes a .tcl-lsp.env into root with the given lines.
func writeEnvFile(t *testing.T, root string, lines string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, ".tcl-lsp.env"), []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
}

// An external package file listed as index_file is indexed for real: goto-def
// from a workspace call site jumps INTO the package source outside the root.
func TestServerEnvironmentIndexesExternalFile(t *testing.T) {
	root := t.TempDir()
	extDir := t.TempDir() // outside the workspace root
	extFile := filepath.Join(extDir, "helpers.tcl")
	if err := os.WriteFile(extFile, []byte("proc exthelper {} {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "caller.tcl"), []byte("exthelper\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEnvFile(t, root, "index_file\t"+extFile+"\n")

	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{RootURI: pathToURI(root)}))
	in.Write(frame(t, "textDocument/definition", 2, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(filepath.Join(root, "caller.tcl"))},
		Position:     Position{Line: 0, Character: 0},
	}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")
	var locs []Location
	_ = json.Unmarshal(resp.Result, &locs)
	if len(locs) != 1 || locs[0].URI != pathToURI(extFile) {
		t.Fatalf("goto-def should jump into the external package file %s; got %#v", extFile, locs)
	}
}

// A declared command (C extension / runtime-generated: name only, no source)
// colors as a function in semantic tokens, but goto-def stays SILENT -- there
// is no location, and the server never invents one.
func TestServerEnvironmentDeclaredCommand(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "caller.tcl"), []byte("fa_c_cmd\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEnvFile(t, root, "command\t::fa_c_cmd\nbuiltin\t::puts\n")

	callerURI := pathToURI(filepath.Join(root, "caller.tcl"))
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{RootURI: pathToURI(root)}))
	in.Write(frame(t, "textDocument/semanticTokens/full", 2, SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: callerURI}}))
	in.Write(frame(t, "textDocument/definition", 3, TextDocumentPositionParams{
		TextDocument: TextDocumentIdentifier{URI: callerURI},
		Position:     Position{Line: 0, Character: 0},
	}))
	in.Write(frame(t, "exit", nil, nil))
	msgs := runServer(t, in.Bytes())

	var st SemanticTokens
	_ = json.Unmarshal(responseByID(msgs, "2").Result, &st)
	toks := decodeSemanticTokens(st.Data)
	found := false
	for _, tk := range toks {
		if tk.line == 0 && tk.char == 0 && tk.typ == stFunction {
			found = true
		}
	}
	if !found {
		t.Fatalf("declared command call should color as function; toks=%#v", toks)
	}

	var locs []Location
	_ = json.Unmarshal(responseByID(msgs, "3").Result, &locs)
	if len(locs) != 0 {
		t.Fatalf("goto-def on a declared (location-less) command must stay silent; got %#v", locs)
	}
}

// Entries whose paths don't exist on this machine (artifact extracted
// elsewhere) are skipped without error, and the rest of the env still loads.
func TestServerEnvironmentSkipsMissingPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "caller.tcl"), []byte("declared_one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeEnvFile(t, root, "index_file\t/no/such/machine/path.tcl\ncommand\t::declared_one\n")

	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{RootURI: pathToURI(root)}))
	in.Write(frame(t, "textDocument/semanticTokens/full", 2, SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: pathToURI(filepath.Join(root, "caller.tcl"))}}))
	in.Write(frame(t, "exit", nil, nil))
	var st SemanticTokens
	_ = json.Unmarshal(responseByID(runServer(t, in.Bytes()), "2").Result, &st)
	if len(st.Data) == 0 {
		t.Fatalf("declared command should still load despite a missing index_file path")
	}
}
