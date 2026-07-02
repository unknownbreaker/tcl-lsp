package lsp

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestServerAdvertisesSemanticTokens(t *testing.T) {
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "1")
	var res InitializeResult
	_ = json.Unmarshal(resp.Result, &res)
	stp := res.Capabilities.SemanticTokensProvider
	if stp == nil || !stp.Full || len(stp.Legend.TokenTypes) == 0 {
		t.Fatalf("semantic tokens capability not advertised: %#v", res.Capabilities.SemanticTokensProvider)
	}
}

// decodedToken is an absolute-position semantic token, decoded from the relative
// wire encoding.
type decodedToken struct{ line, char, length, typ int }

func decodeSemanticTokens(data []uint) []decodedToken {
	var out []decodedToken
	line, char := 0, 0
	for i := 0; i+4 < len(data); i += 5 {
		dl, dc := int(data[i]), int(data[i+1])
		if dl == 0 {
			char += dc
		} else {
			line += dl
			char = dc
		}
		out = append(out, decodedToken{line, char, int(data[i+2]), int(data[i+3])})
	}
	return out
}

// Itcl definitions map to their token types: class->class, method->method,
// ivar->property, namespace var->variable. Pins the legend index mapping.
func TestServerSemanticTokensItclKinds(t *testing.T) {
	src := "itcl::class ::Widget {\n  method draw {} {}\n  variable count 0\n}\nnamespace eval ::app { variable total 0 }\n"
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///m.tcl", Text: src}}))
	in.Write(frame(t, "textDocument/semanticTokens/full", 2, SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///m.tcl"}}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")

	var st SemanticTokens
	_ = json.Unmarshal(resp.Result, &st)
	kinds := map[int]bool{}
	for _, tk := range decodeSemanticTokens(st.Data) {
		kinds[tk.typ] = true
	}
	for _, want := range []struct {
		typ  int
		name string
	}{{stClass, "class"}, {stMethod, "method"}, {stProperty, "property (ivar)"}, {stVariable, "variable (ns var)"}} {
		if !kinds[want.typ] {
			t.Fatalf("no %s token emitted; kinds=%v", want.name, kinds)
		}
	}
}

// A proc definition, its parameter, a $var use, and a call site are colored;
// builtins (puts) are not. Verifies the relative encoding round-trips.
func TestServerSemanticTokens(t *testing.T) {
	src := "proc greet {p} {\n  puts $p\n}\ngreet hi\n"
	var in bytes.Buffer
	in.Write(frame(t, "initialize", 1, InitializeParams{}))
	in.Write(frame(t, "textDocument/didOpen", nil, DidOpenParams{
		TextDocument: TextDocumentItem{URI: "file:///m.tcl", Text: src}}))
	in.Write(frame(t, "textDocument/semanticTokens/full", 2, SemanticTokensParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///m.tcl"}}))
	in.Write(frame(t, "exit", nil, nil))
	resp := responseByID(runServer(t, in.Bytes()), "2")

	var st SemanticTokens
	_ = json.Unmarshal(resp.Result, &st)
	if len(st.Data)%5 != 0 {
		t.Fatalf("token data length %d is not a multiple of 5", len(st.Data))
	}
	toks := decodeSemanticTokens(st.Data)

	// The definition name and the call site are both functions.
	var funcs, vars int
	var haveDef, haveCall bool
	for _, tk := range toks {
		switch tk.typ {
		case stFunction:
			funcs++
			if tk.line == 0 && tk.char == 5 {
				haveDef = true // `greet` in `proc greet`
			}
			if tk.line == 3 && tk.char == 0 {
				haveCall = true // `greet` call
			}
		case stVariable:
			vars++
		}
	}
	if funcs != 2 || !haveDef || !haveCall {
		t.Fatalf("function tokens: got %d (def=%v call=%v), want the def and the call; toks=%#v", funcs, haveDef, haveCall, toks)
	}
	if vars < 1 {
		t.Fatalf("expected at least one variable token (the $p use); toks=%#v", toks)
	}
	// The builtin `puts` must NOT be colored.
	for _, tk := range toks {
		if tk.line == 1 && tk.char == 2 { // `puts`
			t.Fatalf("builtin puts should not be colored; toks=%#v", toks)
		}
	}
}
