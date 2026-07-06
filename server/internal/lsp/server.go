package lsp

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/unknownbreaker/tcl-lsp/internal/envfile"
	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/resolve"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
)

// Server is a single-goroutine LSP server: it reads and dispatches messages
// sequentially, so the index/resolver need no locking.
type Server struct {
	conn   *Conn
	ix     *index.Index
	res    *resolve.Resolver
	docs   map[string]string // uri -> live text
	nextID int               // id for server-initiated requests (registerCapability)
}

// isIndexable reports whether a path is a workspace file the index tracks.
func isIndexable(path string) bool {
	return strings.HasSuffix(path, ".tcl") || strings.HasSuffix(path, ".rvt")
}

// NewServer builds a server over conn with an empty index.
func NewServer(conn *Conn) *Server {
	ix := index.New()
	return &Server{conn: conn, ix: ix, res: resolve.New(ix), docs: map[string]string{}}
}

// Run reads and dispatches messages until exit or the stream ends.
func (s *Server) Run() error {
	for {
		m, err := s.conn.Read()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if s.dispatch(m) {
			return nil
		}
	}
}

// dispatch handles one message; returns true to stop the server (exit).
func (s *Server) dispatch(m *Message) (stop bool) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic in dispatch(%s): %v", m.Method, r)
			if len(m.ID) > 0 {
				s.reply(m.ID, nil) // avoid leaving a request unanswered
			}
		}
	}()
	// A message with no method but an id is a RESPONSE to a server-initiated
	// request (e.g. the client's reply to registerCapability). It is not ours to
	// answer; ignore it so we don't reply to a reply.
	if m.Method == "" {
		return false
	}
	switch m.Method {
	case "initialize":
		var p InitializeParams
		_ = json.Unmarshal(m.Params, &p)
		root := p.RootPath
		if root == "" && p.RootURI != "" {
			root = uriToPath(p.RootURI)
		}
		// Reply first so the client finishes initializing, THEN index — wrapped in
		// $/progress so the (possibly multi-second) workspace index shows an
		// "indexing…/ready" indicator. The single-goroutine loop processes no
		// feature request until indexWorkspace returns, so the index is still ready
		// before the first goto-def/references/etc.
		s.reply(m.ID, InitializeResult{Capabilities: ServerCapabilities{
			TextDocumentSync: 1, DefinitionProvider: true, ReferencesProvider: true,
			DocumentSymbolProvider: true, WorkspaceSymbolProvider: true,
			CallHierarchyProvider: true, FoldingRangeProvider: true,
			DocumentHighlightProvider: true, SelectionRangeProvider: true,
			SemanticTokensProvider: &SemanticTokensOptions{
				Legend: SemanticTokensLegend{TokenTypes: semanticTokenTypes, TokenModifiers: []string{}},
				Full:   true,
			},
		}})
		s.indexWorkspace(root, p.Capabilities.Window.WorkDoneProgress)
	case "initialized":
		// Per the spec, dynamic registration happens after initialized. Register
		// for file watching so the client reports on-disk .tcl/.rvt changes; this
		// keeps the index fresh for files that are never opened in the editor.
		s.registerFileWatchers()
	case "workspace/didChangeWatchedFiles":
		var p DidChangeWatchedFilesParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			log.Printf("didChangeWatchedFiles: bad params: %v", err)
			break
		}
		s.applyWatchedChanges(p.Changes)
	case "shutdown":
		s.reply(m.ID, nil)
	case "textDocument/didOpen":
		var p DidOpenParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			log.Printf("didOpen: bad params: %v", err)
			break
		}
		s.setDoc(p.TextDocument.URI, p.TextDocument.Text)
	case "textDocument/didChange":
		var p DidChangeParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			log.Printf("didChange: bad params: %v", err)
			break
		}
		if n := len(p.ContentChanges); n > 0 {
			s.setDoc(p.TextDocument.URI, p.ContentChanges[n-1].Text)
		}
	case "textDocument/didClose":
		var p DidCloseParams
		if err := json.Unmarshal(m.Params, &p); err != nil {
			log.Printf("didClose: bad params: %v", err)
			break
		}
		delete(s.docs, p.TextDocument.URI)
	case "textDocument/definition":
		var p TextDocumentPositionParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.handleDefinition(p))
	case "textDocument/references":
		var p ReferenceParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.handleReferences(p))
	case "textDocument/documentHighlight":
		var p DocumentHighlightParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.documentHighlight(p))
	case "textDocument/selectionRange":
		var p SelectionRangeParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.selectionRanges(p))
	case "textDocument/semanticTokens/full":
		var p SemanticTokensParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.semanticTokens(p))
	case "textDocument/documentSymbol":
		var p DocumentSymbolParams
		_ = json.Unmarshal(m.Params, &p)
		path := uriToPath(p.TextDocument.URI)
		src := s.sourceOf(path)
		s.reply(m.ID, buildDocumentSymbols(source.Defs(path, src), src, source.IsRVT(path)))
	case "workspace/symbol":
		var p WorkspaceSymbolParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, buildWorkspaceSymbols(s.ix.AllSymbols(), p.Query, s.sourceOf))
	case "textDocument/foldingRange":
		var p FoldingRangeParams
		_ = json.Unmarshal(m.Params, &p)
		path := uriToPath(p.TextDocument.URI)
		src := s.sourceOf(path)
		s.reply(m.ID, buildFoldingRanges(source.Folds(path, src), src))
	case "textDocument/prepareCallHierarchy":
		var p CallHierarchyPrepareParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.prepareCallHierarchy(p))
	case "callHierarchy/incomingCalls":
		var p CallHierarchyIncomingCallsParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.incomingCalls(p))
	case "callHierarchy/outgoingCalls":
		var p CallHierarchyOutgoingCallsParams
		_ = json.Unmarshal(m.Params, &p)
		s.reply(m.ID, s.outgoingCalls(p))
	case "exit":
		return true
	default:
		// Unknown request: reply null so the client does not hang. Unknown
		// notifications (no id) are ignored. Document-sync and feature methods
		// are added in the next task.
		if len(m.ID) > 0 {
			s.reply(m.ID, nil)
		}
	}
	return false
}

// reply writes a JSON-RPC response for the given id. A nil result is sent as
// JSON null. A marshal failure is logged and reported as an internal error so
// the client does not hang.
func (s *Server) reply(id json.RawMessage, result any) {
	raw := json.RawMessage("null")
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			log.Printf("reply: marshal error for id %s: %v", string(id), err)
			_ = s.conn.Write(&Message{ID: id, Error: &ResponseError{Code: -32603, Message: "internal error"}})
			return
		}
		raw = b
	}
	_ = s.conn.Write(&Message{ID: id, Result: raw})
}

// setDoc stores a document's live text and re-indexes it.
func (s *Server) setDoc(uri, text string) {
	s.docs[uri] = text
	s.ix.IndexFile(uriToPath(uri), text)
}

// indexWorkspace builds the workspace index for root, reporting it via $/progress
// when the client supports work-done progress. It runs synchronously inside the
// initialize handler (after the reply), so by the time the loop reads the first
// feature request the index is complete.
func (s *Server) indexWorkspace(root string, progress bool) {
	if root == "" {
		return
	}
	const token = "tcl-lsp/index"
	var onFile func(int)
	if progress {
		s.progressCreate(token)
		s.progressBegin(token, "Indexing TCL workspace")
		// Report the running count, throttled to ~20/sec (50ms) so the number
		// animates briskly without flooding the client (a counter is unreadable
		// faster than that anyway). Always report the first file so feedback is
		// immediate regardless of how fast indexing runs.
		var last time.Time
		onFile = func(n int) {
			now := time.Now()
			if n == 1 || now.Sub(last) >= 50*time.Millisecond {
				last = now
				s.progressReport(token, fmt.Sprintf("%d files", n))
			}
		}
	}
	// Best-effort: IndexDir continues past unreadable entries and returns an
	// aggregated error, which we log (stderr) rather than fail on.
	if err := s.ix.IndexDirProgress(root, onFile); err != nil {
		log.Printf("workspace index (%s): %v", root, err)
	}
	s.loadEnvironment(root)
	if progress {
		s.progressEnd(token, fmt.Sprintf("Indexed %d files", len(s.ix.Files())))
	}
}

// loadEnvironment reads the optional .tcl-lsp.env artifact at the workspace root
// (produced offline by tools/extract.tcl from a live tclsh) and folds it into
// the index: external package source files are indexed like workspace files
// (real definitions, goto-def jumps into them), and package-provided commands
// with no indexable source (C extensions, runtime-generated procs) are declared
// by name. Paths that don't exist on this machine are skipped silently -- the
// artifact may have been extracted on a different host, and a missing entry
// just means less reach, never an error.
func (s *Server) loadEnvironment(root string) {
	env, ok, err := envfile.Load(filepath.Join(root, ".tcl-lsp.env"))
	if err != nil {
		log.Printf("environment file: %v", err)
		return
	}
	if !ok {
		return
	}
	indexed := 0
	for _, p := range env.IndexFiles {
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			continue // extracted on another machine, or the package moved
		}
		s.ix.IndexFile(p, string(b))
		indexed++
	}
	s.ix.DeclareCommands(env.Commands)
	if indexed > 0 || len(env.Commands) > 0 {
		log.Printf("environment: indexed %d package files, declared %d commands", indexed, len(env.Commands))
	}
}

// progressCreate registers a progress token with the client (a server-initiated
// request). Its reply is ignored by the response guard in dispatch. Sent before
// the $/progress notifications; the client processes messages in order, so the
// token is registered by the time it handles the `begin`.
func (s *Server) progressCreate(token string) {
	s.nextID++
	id, _ := json.Marshal(s.nextID)
	params, _ := json.Marshal(WorkDoneProgressCreateParams{Token: token})
	if err := s.conn.Write(&Message{ID: id, Method: "window/workDoneProgress/create", Params: params}); err != nil {
		log.Printf("progress create: %v", err)
	}
}

func (s *Server) progressNotify(token string, value any) {
	params, _ := json.Marshal(ProgressParams{Token: token, Value: value})
	if err := s.conn.Write(&Message{Method: "$/progress", Params: params}); err != nil {
		log.Printf("progress: %v", err)
	}
}

func (s *Server) progressBegin(token, title string) {
	s.progressNotify(token, WorkDoneProgressBegin{Kind: "begin", Title: title})
}

func (s *Server) progressReport(token, message string) {
	s.progressNotify(token, WorkDoneProgressReport{Kind: "report", Message: message})
}

func (s *Server) progressEnd(token, message string) {
	s.progressNotify(token, WorkDoneProgressEnd{Kind: "end", Message: message})
}

// registerFileWatchers asks the client to watch all workspace .tcl/.rvt files
// and report changes via workspace/didChangeWatchedFiles. The client's reply is
// ignored (handled by the response guard in dispatch).
func (s *Server) registerFileWatchers() {
	s.nextID++
	id, _ := json.Marshal(s.nextID)
	params, _ := json.Marshal(RegistrationParams{Registrations: []Registration{{
		ID:     "watch-tcl-rvt",
		Method: "workspace/didChangeWatchedFiles",
		RegisterOptions: DidChangeWatchedFilesRegistrationOptions{Watchers: []FileSystemWatcher{
			{GlobPattern: "**/*.tcl"},
			{GlobPattern: "**/*.rvt"},
		}},
	}}})
	if err := s.conn.Write(&Message{ID: id, Method: "client/registerCapability", Params: params}); err != nil {
		log.Printf("registerCapability: %v", err)
	}
}

// applyWatchedChanges re-indexes created/changed files and drops deleted ones.
// An open document's live text always wins, so changes to open files are skipped
// (didChange already keeps them fresh, and the on-disk copy may be stale).
func (s *Server) applyWatchedChanges(changes []FileEvent) {
	for _, ch := range changes {
		path := uriToPath(ch.URI)
		if !isIndexable(path) {
			continue
		}
		switch ch.Type {
		case FileChangeCreated, FileChangeChanged:
			if _, open := s.docs[pathToURI(path)]; open {
				continue // live text is authoritative for open docs
			}
			b, err := os.ReadFile(path)
			if err != nil {
				log.Printf("watched file %s: %v", path, err)
				continue
			}
			s.ix.IndexFile(path, string(b))
		case FileChangeDeleted:
			s.ix.RemoveFile(path)
		}
	}
}

// sourceOf returns the best-available source for a path: the live document if
// open, else the indexed copy.
func (s *Server) sourceOf(path string) string {
	if t, ok := s.docs[pathToURI(path)]; ok {
		return t
	}
	return s.ix.Source(path)
}

func (s *Server) handleDefinition(p TextDocumentPositionParams) []Location {
	path := uriToPath(p.TextDocument.URI)
	src := s.sourceOf(path)
	off := ByteOffset(src, p.Position.Line, p.Position.Character)
	return s.toLocations(s.res.Definition(path, src, off))
}

func (s *Server) handleReferences(p ReferenceParams) []Location {
	path := uriToPath(p.TextDocument.URI)
	src := s.sourceOf(path)
	off := ByteOffset(src, p.Position.Line, p.Position.Character)
	locs := s.res.References(path, src, off)
	// includeDeclaration (true by default in most clients): prepend the symbol's
	// declaration site(s), which are not themselves invocation references.
	// Declarations (not Definition) resolves even when the cursor is on the
	// definition, so `gr` on the proc name still lists it.
	if p.Context.IncludeDeclaration {
		locs = mergeLocations(s.res.Declarations(path, src, off), locs)
	}
	return s.toLocations(locs)
}

// mergeLocations concatenates two location lists, declarations first, dropping
// any whose file and name-range already appear (a declaration is never also an
// invocation site, but this keeps the result free of accidental duplicates).
func mergeLocations(decls, refs []index.Location) []index.Location {
	type key struct {
		file       string
		start, end int
	}
	seen := make(map[key]bool, len(decls)+len(refs))
	out := make([]index.Location, 0, len(decls)+len(refs))
	for _, group := range [][]index.Location{decls, refs} {
		for _, l := range group {
			k := key{l.File, l.NameStart, l.NameEnd}
			if seen[k] {
				continue
			}
			seen[k] = true
			out = append(out, l)
		}
	}
	return out
}

// toLocations converts resolver locations (byte ranges) to LSP locations.
func (s *Server) toLocations(locs []index.Location) []Location {
	var out []Location
	for _, l := range locs {
		src := s.sourceOf(l.File)
		sl, sc := LSPPosition(src, l.NameStart)
		el, ec := LSPPosition(src, l.NameEnd)
		out = append(out, Location{
			URI: pathToURI(l.File),
			Range: Range{
				Start: Position{Line: sl, Character: sc},
				End:   Position{Line: el, Character: ec},
			},
		})
	}
	return out
}
