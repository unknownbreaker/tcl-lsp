package lsp

import "net/url"

// Position is an LSP position (UTF-16 character units).
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is an LSP range.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// Location is an LSP location.
type Location struct {
	URI   string `json:"uri"`
	Range Range  `json:"range"`
}

// TextDocumentIdentifier references a document by URI.
type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

// TextDocumentItem is a document with its full text.
type TextDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

// DidOpenParams is textDocument/didOpen.
type DidOpenParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

// TextDocumentContentChangeEvent is one change (full-document sync: Text is the
// whole new content).
type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

// DidChangeParams is textDocument/didChange.
type DidChangeParams struct {
	TextDocument   TextDocumentIdentifier           `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

// DidCloseParams is textDocument/didClose.
type DidCloseParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// TextDocumentPositionParams is a position in a document (definition/references).
type TextDocumentPositionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// SymbolKind is the kind of a symbol (LSP SymbolKind).
type SymbolKind int

// SymbolKind constants from the LSP spec.
const (
	SymKindFile      SymbolKind = 1
	SymKindNamespace SymbolKind = 3
	SymKindClass     SymbolKind = 5
	SymKindMethod    SymbolKind = 6
	SymKindField     SymbolKind = 8
	SymKindFunction  SymbolKind = 12
	SymKindVariable  SymbolKind = 13
)

// DocumentSymbol is a symbol in a document (part of the document symbols response).
type DocumentSymbol struct {
	Name           string           `json:"name"`
	Kind           SymbolKind       `json:"kind"`
	Range          Range            `json:"range"`
	SelectionRange Range            `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

// SymbolInformation is a symbol in the workspace (part of the workspace symbols response).
type SymbolInformation struct {
	Name          string     `json:"name"`
	Kind          SymbolKind `json:"kind"`
	Location      Location   `json:"location"`
	ContainerName string     `json:"containerName,omitempty"`
}

// DocumentSymbolParams is a document/documentSymbol request.
type DocumentSymbolParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// WorkspaceSymbolParams is a workspace/symbol request.
type WorkspaceSymbolParams struct {
	Query string `json:"query"`
}

// FoldingRangeParams is a textDocument/foldingRange request.
type FoldingRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// FoldingRange is one foldable region, reported by line. We emit only the line
// fields (no kind / character columns), so the editor folds whole-line block
// regions — the common case for braced TCL bodies.
type FoldingRange struct {
	StartLine int `json:"startLine"`
	EndLine   int `json:"endLine"`
}

// DocumentHighlightParams is a textDocument/documentHighlight request.
type DocumentHighlightParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// DocumentHighlight is one occurrence of the symbol under the cursor, within the
// current document. Kind is omitted: we do not distinguish read vs write uses
// (a $x could be either), so every occurrence is a plain highlight.
type DocumentHighlight struct {
	Range Range `json:"range"`
}

// SelectionRangeParams is a textDocument/selectionRange request: a set of cursor
// positions to expand.
type SelectionRangeParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Positions    []Position             `json:"positions"`
}

// SelectionRange is one level of the expand-selection hierarchy: a range plus the
// next-larger enclosing range (nil at the document root). The editor grows the
// selection by walking Parent outward.
type SelectionRange struct {
	Range  Range           `json:"range"`
	Parent *SelectionRange `json:"parent,omitempty"`
}

// SemanticTokensLegend names the token types/modifiers by index; the encoded
// data references them positionally.
type SemanticTokensLegend struct {
	TokenTypes     []string `json:"tokenTypes"`
	TokenModifiers []string `json:"tokenModifiers"`
}

// SemanticTokensOptions is the semanticTokensProvider capability (an object, not
// a bool): it carries the legend and advertises full-document support.
type SemanticTokensOptions struct {
	Legend SemanticTokensLegend `json:"legend"`
	Full   bool                 `json:"full"`
}

// SemanticTokensParams is a textDocument/semanticTokens/full request.
type SemanticTokensParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

// SemanticTokens is the response: a flat array of 5-tuples (deltaLine,
// deltaStartChar, length, tokenType, tokenModifiers), relative-encoded per spec.
type SemanticTokens struct {
	Data []uint `json:"data"`
}

// CallHierarchyItem identifies a callable in the call hierarchy (a proc or
// method, or a file for top-level/page-level call sites). The SelectionRange (the
// name token) doubles as the re-resolution anchor for incoming/outgoing calls.
type CallHierarchyItem struct {
	Name           string     `json:"name"`
	Kind           SymbolKind `json:"kind"`
	Detail         string     `json:"detail,omitempty"` // fully-qualified name / container
	URI            string     `json:"uri"`
	Range          Range      `json:"range"`
	SelectionRange Range      `json:"selectionRange"`
}

// CallHierarchyPrepareParams is a textDocument/prepareCallHierarchy request.
type CallHierarchyPrepareParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

// CallHierarchyIncomingCallsParams is a callHierarchy/incomingCalls request.
type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyOutgoingCallsParams is a callHierarchy/outgoingCalls request.
type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

// CallHierarchyIncomingCall is one caller of the prepared item, with the call
// sites (within the caller) that reach it.
type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []Range           `json:"fromRanges"`
}

// CallHierarchyOutgoingCall is one callee of the prepared item, with the call
// sites (within the item's body) that reach it.
type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []Range           `json:"fromRanges"`
}

// ReferenceContext carries the references request's options.
type ReferenceContext struct {
	// IncludeDeclaration asks the server to include the symbol's declaration
	// site(s) alongside its usages. Clients (Neovim included) send true by default.
	IncludeDeclaration bool `json:"includeDeclaration"`
}

// ReferenceParams is a references request: a position plus its context.
type ReferenceParams struct {
	TextDocumentPositionParams
	Context ReferenceContext `json:"context"`
}

// InitializeParams is the subset of initialize we use.
type InitializeParams struct {
	RootURI      string             `json:"rootUri"`
	RootPath     string             `json:"rootPath"`
	Capabilities ClientCapabilities `json:"capabilities"`
}

// ClientCapabilities is the subset of client capabilities we read.
// Window.WorkDoneProgress gates whether the initial workspace index is reported
// via $/progress (a client that doesn't support it just gets no feedback).
type ClientCapabilities struct {
	Window struct {
		WorkDoneProgress bool `json:"workDoneProgress"`
	} `json:"window"`
}

// WorkDoneProgressCreateParams is the window/workDoneProgress/create request: the
// server asks the client to register a progress token before reporting on it.
type WorkDoneProgressCreateParams struct {
	Token string `json:"token"`
}

// ProgressParams is the $/progress notification: a token plus a begin/end value.
type ProgressParams struct {
	Token string `json:"token"`
	Value any    `json:"value"`
}

// WorkDoneProgressBegin starts a progress run (a titled spinner).
type WorkDoneProgressBegin struct {
	Kind  string `json:"kind"` // always "begin"
	Title string `json:"title"`
}

// WorkDoneProgressReport updates an in-progress run with a new message (here, the
// running file count).
type WorkDoneProgressReport struct {
	Kind    string `json:"kind"` // always "report"
	Message string `json:"message,omitempty"`
}

// WorkDoneProgressEnd ends a progress run, optionally with a final message.
type WorkDoneProgressEnd struct {
	Kind    string `json:"kind"` // always "end"
	Message string `json:"message,omitempty"`
}

// InitializeResult advertises server capabilities.
type InitializeResult struct {
	Capabilities ServerCapabilities `json:"capabilities"`
}

// ServerCapabilities is the subset we advertise.
type ServerCapabilities struct {
	TextDocumentSync          int                    `json:"textDocumentSync"` // 1 = full sync
	DefinitionProvider        bool                   `json:"definitionProvider"`
	ReferencesProvider        bool                   `json:"referencesProvider"`
	DocumentSymbolProvider    bool                   `json:"documentSymbolProvider"`
	WorkspaceSymbolProvider   bool                   `json:"workspaceSymbolProvider"`
	CallHierarchyProvider     bool                   `json:"callHierarchyProvider"`
	FoldingRangeProvider      bool                   `json:"foldingRangeProvider"`
	DocumentHighlightProvider bool                   `json:"documentHighlightProvider"`
	SelectionRangeProvider    bool                   `json:"selectionRangeProvider"`
	SemanticTokensProvider    *SemanticTokensOptions `json:"semanticTokensProvider,omitempty"`
}

// Dynamic capability registration (server -> client). After `initialized` the
// server registers for file watching so the client reports on-disk .tcl/.rvt
// changes via workspace/didChangeWatchedFiles, keeping the index fresh for files
// that are never opened in the editor.

// RegistrationParams is the client/registerCapability request payload.
type RegistrationParams struct {
	Registrations []Registration `json:"registrations"`
}

// Registration is one capability registration. RegisterOptions is method-specific.
type Registration struct {
	ID              string `json:"id"`
	Method          string `json:"method"`
	RegisterOptions any    `json:"registerOptions,omitempty"`
}

// DidChangeWatchedFilesRegistrationOptions lists the glob patterns to watch.
type DidChangeWatchedFilesRegistrationOptions struct {
	Watchers []FileSystemWatcher `json:"watchers"`
}

// FileSystemWatcher watches paths matching GlobPattern. Kind defaults (on the
// client) to create|change|delete when omitted.
type FileSystemWatcher struct {
	GlobPattern string `json:"globPattern"`
}

// FileChange* are workspace/didChangeWatchedFiles change kinds.
const (
	FileChangeCreated = 1
	FileChangeChanged = 2
	FileChangeDeleted = 3
)

// DidChangeWatchedFilesParams is the workspace/didChangeWatchedFiles notification.
type DidChangeWatchedFilesParams struct {
	Changes []FileEvent `json:"changes"`
}

// FileEvent is one watched-file change: a URI and a FileChange* type.
type FileEvent struct {
	URI  string `json:"uri"`
	Type int    `json:"type"`
}

// uriToPath converts a file:// URI to a filesystem path. A string that is not a
// file URI is returned unchanged (best effort).
func uriToPath(uri string) string {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" {
		return uri
	}
	return u.Path
}

// pathToURI converts a filesystem path to a file:// URI.
func pathToURI(path string) string {
	u := url.URL{Scheme: "file", Path: path}
	return u.String()
}
