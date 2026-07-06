// Package resolve maps a cursor position to definition sites using the workspace
// index and TCL's name-resolution rules.
package resolve

import (
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/index"
	"github.com/unknownbreaker/tcl-lsp/internal/par"
	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Resolver resolves symbols to definitions against a workspace index.
type Resolver struct {
	ix *index.Index
}

// New returns a Resolver over the given index.
func New(ix *index.Index) *Resolver {
	return &Resolver{ix: ix}
}

// isLocalBinding reports whether a definition kind binds a proc-local name.
func isLocalBinding(k tcl.DefKind) bool {
	return k == tcl.DefLocal || k == tcl.DefGlobalLink
}

// localAt reports whether offset sits on a proc-local symbol, returning its
// bare name and scope. Definition name-ranges (local bindings) are checked
// first, then FrameProc variable references ($x uses).
func (r *Resolver) localAt(file, src string, offset int) (name string, scope int, ok bool) {
	for _, d := range source.Defs(file, src) {
		if isLocalBinding(d.Kind) && offset >= d.NameStart && offset < d.NameEnd {
			return d.Name, d.Scope, true
		}
	}
	for _, ref := range source.Refs(file, src) {
		if ref.Ref.Kind == tcl.RefVariable && ref.Frame == tcl.FrameProc &&
			offset >= ref.Ref.Start && offset < ref.Ref.End {
			return ref.Ref.Name, ref.Scope, true
		}
	}
	return "", 0, false
}

// isNameByte reports whether b is a valid TCL variable name byte ([A-Za-z0-9_]).
func isNameByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// isArrayVarAt reports whether the variable reference at offset in src is an
// array element access (e.g. $arr(key)). The reaching analysis does not track
// array subscripts, so array accesses fall back to first-binding resolution.
func isArrayVarAt(src string, offset int) bool {
	i := offset
	// Skip over the name characters (the $ was already consumed; offset is on
	// the first name byte or somewhere within).
	for i < len(src) && isNameByte(src[i]) {
		i++
	}
	// Handle qualified names (:: segments).
	for i+1 < len(src) && src[i] == ':' && src[i+1] == ':' {
		i += 2
		for i < len(src) && isNameByte(src[i]) {
			i++
		}
	}
	return i < len(src) && src[i] == '('
}

// localDefinition returns the definition site(s) for the proc-local (name,
// scope). It first tries context-sensitive reaching definitions for the use at
// offset: each reaching binding is origin-chased when it is a global/upvar link
// (preserving prior behavior). Falls back to the earliest binding when reaching
// analysis is unavailable (oversized proc, array element access, or a use with
// no in-proc reaching def such as a bare global/upvar link before any local
// assignment).
func (r *Resolver) localDefinition(file, src, name string, scope, offset int) []index.Location {
	// Precise reaching definitions for the use at offset. Each reaching binding
	// origin-chases when it is a global/upvar link (preserving prior behavior).
	// Array element accesses (e.g. $arr(key)) are excluded: the reaching engine
	// tracks the base name without subscripts, so it cannot distinguish arr(a)
	// from arr(b); first-binding gives more useful results for arrays.
	if !isArrayVarAt(src, offset) {
		if defs, ok := source.Reaching(file, src, offset); ok && len(defs) > 0 {
			var out []index.Location
			for _, d := range defs {
				if d.Origin != "" {
					if locs := r.lookupScoped(d.Origin, file); len(locs) > 0 {
						out = append(out, locs...)
						continue
					}
				}
				out = append(out, index.Location{File: file, Name: d.Name, Kind: tcl.DefLocal, NameStart: d.NameStart, NameEnd: d.NameEnd})
			}
			if len(out) > 0 {
				return out
			}
		}
	}

	// Fallback: earliest binding (used when reaching-defs is unavailable —
	// oversized proc, array element access, or a use with no in-proc reaching
	// def such as a bare global/upvar link before any local assignment).
	firstStart, firstEnd, firstOrigin, have := 0, 0, "", false
	for _, d := range source.Defs(file, src) {
		if !isLocalBinding(d.Kind) || d.Name != name || d.Scope != scope {
			continue
		}
		if !have || d.NameStart < firstStart {
			firstStart, firstEnd, firstOrigin, have = d.NameStart, d.NameEnd, d.Origin, true
		}
	}
	if !have {
		return nil
	}
	if firstOrigin != "" {
		if locs := r.lookupScoped(firstOrigin, file); len(locs) > 0 {
			return locs
		}
	}
	return []index.Location{{File: file, Name: name, Kind: tcl.DefLocal, NameStart: firstStart, NameEnd: firstEnd}}
}

// methodInClass looks up method name in the class identified by classFQ,
// walking the inheritance chain depth-first (first match wins). Returns the
// method's definition sites, or nil if not found. A visited set guards against
// inheritance cycles.
func (r *Resolver) methodInClass(classFQ, name string) []index.Location {
	return r.methodInClassSeen(classFQ, name, map[string]bool{})
}

func (r *Resolver) methodInClassSeen(classFQ, name string, seen map[string]bool) []index.Location {
	if seen[classFQ] {
		return nil
	}
	seen[classFQ] = true
	ci := r.ix.Class(classFQ)
	if ci == nil {
		return nil
	}
	if locs := ci.Methods[name]; len(locs) > 0 {
		return locs
	}
	// Walk bases depth-first; first match wins.
	for _, base := range ci.Inherit {
		if locs := r.methodInClassSeen(base, name, seen); len(locs) > 0 {
			return locs
		}
	}
	return nil
}

// ivarInClass looks up instance variable name in the class identified by
// classFQ, walking the inheritance chain depth-first (first match wins).
// Returns the ivar's declaration sites, or nil if not found. A visited set
// guards against inheritance cycles.
func (r *Resolver) ivarInClass(classFQ, name string) []index.Location {
	return r.ivarInClassSeen(classFQ, name, map[string]bool{})
}

func (r *Resolver) ivarInClassSeen(classFQ, name string, seen map[string]bool) []index.Location {
	if seen[classFQ] {
		return nil
	}
	seen[classFQ] = true
	ci := r.ix.Class(classFQ)
	if ci == nil {
		return nil
	}
	if locs := ci.Ivars[name]; len(locs) > 0 {
		return locs
	}
	// Walk bases depth-first; first match wins.
	for _, base := range ci.Inherit {
		if locs := r.ivarInClassSeen(base, name, seen); len(locs) > 0 {
			return locs
		}
	}
	return nil
}

// Definition returns the definition site(s) for the symbol at byte offset in
// src. file is the path of the calling document; page-local (::request::*)
// symbols are scoped to file only. Returns nil if there is no symbol at the
// offset or it resolves to nothing. Candidates are tried in TCL precedence
// order (current namespace, then global); the first candidate that resolves
// wins (a bare name is NOT unioned across namespaces).
func (r *Resolver) Definition(file, src string, offset int) []index.Location {
	if name, scope, ok := r.localAt(file, src, offset); ok {
		if locs := r.localDefinition(file, src, name, scope, offset); len(locs) > 0 {
			return locs
		}
		// Proc-local resolution found nothing. If the use is inside a class method
		// body, fall back to the class's instance variables (MRO walk). Proc-locals
		// take precedence: this branch is reached only when local resolution fails.
		// Ivar fallback relies on localDefinition returning nil (not a synthetic
		// DefLocal) for a bare ivar use with no local binding; if that ever
		// changes, ivar resolution silently breaks.
		if ref := refAt(file, src, offset); ref != nil && ref.Frame == tcl.FrameProc && ref.Class != "" {
			if locs := r.ivarInClass(ref.Class, name); len(locs) > 0 {
				return locs
			}
		}
		return nil
	}
	// $obj method shape: a command whose head is a lone $var and whose second
	// word (the method name) is at the cursor. Fires regardless of frame — in
	// .rvt pages the $obj method call runs at namespace scope, not proc scope.
	if receiverOff, methodName, ok := source.ObjMethodAt(file, src, offset); ok {
		classes := source.ClassOf(file, src, receiverOff)
		// `$this method`: the receiver is the implicit self, which ClassOf cannot
		// type (there is no `set this [...]` instantiation). Inside a method body,
		// resolve the method on the enclosing class directly (and its bases). This
		// is the dominant intra-class call form in real Itcl code.
		if len(classes) == 0 {
			if rref := refAt(file, src, receiverOff); rref != nil &&
				rref.Ref.Name == "this" && rref.Frame == tcl.FrameProc && rref.Class != "" {
				classes = []string{rref.Class}
			}
		}
		if len(classes) > 0 {
			type locKey struct {
				File      string
				NameStart int
				NameEnd   int
			}
			seen := map[locKey]bool{}
			var out []index.Location
			for _, cls := range classes {
				for _, loc := range r.methodInClass(cls, methodName) {
					key := locKey{loc.File, loc.NameStart, loc.NameEnd}
					if !seen[key] {
						seen[key] = true
						out = append(out, loc)
					}
				}
			}
			if len(out) > 0 {
				return out
			}
		}
		// ClassOf returned nothing (unknown receiver) or method not found.
		// Return nil: no wrong jump.
		return nil
	}

	ref := refAt(file, src, offset)
	if ref == nil {
		return nil
	}
	// Intra-class method resolution: a bare command call (or $this method) inside
	// a class method body resolves to a method on the current class (or its bases).
	// This takes precedence over the global command path but comes after proc-locals.
	if ref.Ref.Kind == tcl.RefCommand && ref.Frame == tcl.FrameProc && ref.Class != "" {
		if locs := r.methodInClass(ref.Class, ref.Ref.Name); len(locs) > 0 {
			return locs
		}
	}
	for _, name := range r.candidates(ref) {
		if locs := r.lookupScoped(name, file); len(locs) > 0 {
			return locs
		}
	}
	return nil
}

// refAt returns the innermost reference whose byte range contains offset, parsing
// src through the source seam so .rvt content is extracted to its stitched script
// and reported in source coordinates.
func refAt(file, src string, offset int) *tcl.ContextRef {
	refs := source.Refs(file, src)
	var best *tcl.ContextRef
	for i := range refs {
		rg := refs[i].Ref
		if offset >= rg.Start && offset < rg.End {
			if best == nil || (rg.End-rg.Start) < (best.Ref.End-best.Ref.Start) {
				best = &refs[i]
			}
		}
	}
	return best
}

// candidates returns the fully-qualified names to look up for a reference.
func (r *Resolver) candidates(ref *tcl.ContextRef) []string {
	name := ref.Ref.Name
	ns := ref.Namespace
	if ref.Ref.Kind == tcl.RefCommand {
		return r.commandCandidates(name, ns)
	}
	return variableCandidates(name, ns, ref.Frame)
}

// commandCandidates returns the FQ command names to try, in TCL precedence
// order: current namespace, imported names, namespace-path entries, then global.
// A qualified name resolves directly.
func (r *Resolver) commandCandidates(name, ns string) []string {
	if isQualified(name) {
		return []string{qualify(name, ns)}
	}
	cands := []string{qualify(name, ns)} // current namespace

	path, imports := r.ix.Namespace(ns)
	// Imported commands behave like commands in the current namespace. A glob
	// import (last == "*") adds srcNs::name even when name is not actually
	// exported by srcNs; Lookup filters non-existent names, so this only
	// over-matches when srcNs::name happens to exist (export patterns are not
	// yet modeled — research OQ6).
	for _, imp := range imports {
		if srcNs, last, ok := splitLastSegment(imp); ok && (last == name || last == "*") {
			if srcNs == "::" {
				cands = append(cands, "::"+name)
			} else {
				cands = append(cands, srcNs+"::"+name)
			}
		}
	}
	// namespace path entries (already fully qualified).
	for _, p := range path {
		cands = append(cands, p+"::"+name)
	}
	// global fallback.
	if ns != "::" {
		cands = append(cands, "::"+name)
	}
	return dedup(cands)
}

// splitLastSegment splits a qualified name into (namespace, lastSegment), e.g.
// "::p::pub" -> ("::p", "pub") and "::pub" -> ("::", "pub"). Returns ok=false
// when there is no "::" separator.
func splitLastSegment(qname string) (nsPart, last string, ok bool) {
	i := strings.LastIndex(qname, "::")
	if i < 0 {
		return "", "", false
	}
	nsPart = qname[:i]
	if nsPart == "" {
		nsPart = "::"
	}
	return nsPart, qname[i+2:], true
}

func dedup(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := in[:0]
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// variableCandidates: a qualified variable resolves directly; a bare variable at
// namespace-eval top level is the current namespace's own variable. A bare
// variable inside a proc body is local-only and not resolvable via the workspace
// index (frame-local resolution is a later plan) — returns nil.
func variableCandidates(name, ns string, frame tcl.FrameKind) []string {
	if isQualified(name) {
		return []string{qualify(name, ns)}
	}
	if frame == tcl.FrameNamespace {
		if ns == "::" {
			return []string{"::" + name}
		}
		return []string{ns + "::" + name}
	}
	return nil // bare proc-local — deferred
}

// localReferences returns every occurrence of the proc-local (name, scope) in the
// current file: binding sites and $-use sites. Locals never cross files, so only
// the current document is scanned. Results are deduped by byte range.
func (r *Resolver) localReferences(file, src, name string, scope int) []index.Location {
	seen := map[[2]int]bool{}
	var out []index.Location
	add := func(start, end int) {
		key := [2]int{start, end}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, index.Location{File: file, Name: name, Kind: tcl.DefLocal, NameStart: start, NameEnd: end})
	}
	for _, d := range source.Defs(file, src) {
		if isLocalBinding(d.Kind) && d.Name == name && d.Scope == scope {
			add(d.NameStart, d.NameEnd)
		}
	}
	for _, ref := range source.Refs(file, src) {
		if ref.Ref.Kind == tcl.RefVariable && ref.Frame == tcl.FrameProc &&
			ref.Ref.Name == name && ref.Scope == scope {
			add(ref.Ref.Start, ref.Ref.End)
		}
	}
	return out
}

// References returns all workspace references to the symbol at byte offset in
// src. The current file is parsed from the live src; other files use the
// reference sites precomputed at index time, so a request does not re-parse the
// whole workspace. Only command and namespace/qualified-variable references are
// matched; bare proc-locals and bareword variable-name arguments are not.
func (r *Resolver) References(file, src string, offset int) []index.Location {
	if name, scope, ok := r.localAt(file, src, offset); ok {
		return r.localReferences(file, src, name, scope)
	}
	target := r.targetFQ(file, src, offset)
	if target == "" {
		return nil
	}
	// targetKind carries the target's DEFINITION kind for the protocol layer's
	// use; it is the zero value (DefProc) when the target has no definition in
	// the index (an undefined symbol). A reference site itself has no def-kind.
	var targetKind tcl.DefKind
	if defs := r.lookupScoped(target, file); len(defs) > 0 {
		targetKind = defs[0].Kind
	}

	// A page-local (::request) symbol has references only within its own page, so
	// scanning other files would risk matching an identically-named page-local
	// helper elsewhere (their primary candidate name collides).
	pageLocal := source.IsRVT(file) && strings.HasPrefix(target, "::request::")

	// Build the per-file work list: the current file from live source, the rest
	// from precomputed refs. The current file is first so results stay in the same
	// order as the old sequential scan.
	work := []refScan{{file: file, refs: source.Refs(file, src)}}
	if !pageLocal {
		for _, f := range r.ix.Files() {
			if f == file {
				continue
			}
			work = append(work, refScan{file: f, refs: r.ix.FileRefs(f)})
		}
	}

	// Pre-warm the namespace cache single-threaded. Resolving a command ref calls
	// index.Namespace(ref.Namespace), which WRITES the memoized nsCache on a miss;
	// under the parallel scan below that would be a concurrent map write (a Go
	// fatal panic, not merely a race). Warming every referenced namespace first
	// makes the scan touch nsCache read-only.
	r.warmNamespaces(work)

	// Resolve each file's refs in parallel -- index access is read-only after the
	// warm-up, and this runs while the dispatch loop is blocked on the request.
	// Results concatenate in work order, identical to the sequential scan.
	parts := par.Map(work, func(w refScan) []index.Location {
		var out []index.Location
		for i := range w.refs {
			if r.refFQ(&w.refs[i], w.file) == target {
				out = append(out, index.Location{
					File: w.file, Name: target, Kind: targetKind,
					NameStart: w.refs[i].Ref.Start, NameEnd: w.refs[i].Ref.End,
				})
			}
		}
		return out
	})

	var out []index.Location
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// refScan pairs a file with its reference sites for a workspace scan.
type refScan struct {
	file string
	refs []tcl.ContextRef
}

// warmNamespaces populates the index's namespace cache for every namespace
// referenced by work, single-threaded. Command resolution only ever queries
// index.Namespace(ref.Namespace), so warming that exact set guarantees the
// subsequent parallel scan finds every entry already cached (read-only).
func (r *Resolver) warmNamespaces(work []refScan) {
	seen := map[string]bool{}
	for _, w := range work {
		for i := range w.refs {
			ns := w.refs[i].Namespace
			if !seen[ns] {
				seen[ns] = true
				r.ix.Namespace(ns)
			}
		}
	}
}

// Declarations returns the definition site(s) of the symbol at offset -- its
// declaration(s), for find-references with includeDeclaration. Unlike Definition
// it resolves from a usage OR from the definition itself (goto-definition is a
// no-op when already at the definition), so `gr` with the cursor on the proc
// name still yields the declaration. Returns nil for an undefined symbol.
func (r *Resolver) Declarations(file, src string, offset int) []index.Location {
	if name, scope, ok := r.localAt(file, src, offset); ok {
		var out []index.Location
		for _, d := range source.Defs(file, src) {
			if isLocalBinding(d.Kind) && d.Name == name && d.Scope == scope {
				out = append(out, index.Location{File: file, Name: name, Kind: tcl.DefLocal, NameStart: d.NameStart, NameEnd: d.NameEnd})
			}
		}
		return out
	}
	target := r.targetFQ(file, src, offset)
	if target == "" {
		return nil
	}
	return r.lookupScoped(target, file)
}

// CommandRefKind returns the definition kind a command reference resolves to (by
// TCL command precedence) and whether it resolves to a known symbol: an indexed
// definition, or a command DECLARED by the environment file (extracted from a
// live tclsh -- C extension commands and runtime-generated procs, which have no
// indexable location but certainly exist). Semantic tokens use it to color only
// calls that resolve, leaving core builtins to fall back to syntax highlighting.
// Method-MRO resolution ($obj method) is out of scope here -- those stay
// uncolored.
func (r *Resolver) CommandRefKind(ref *tcl.ContextRef, file string) (tcl.DefKind, bool) {
	if ref.Ref.Kind != tcl.RefCommand {
		return 0, false
	}
	for _, name := range r.commandCandidates(ref.Ref.Name, ref.Namespace) {
		if locs := r.lookupScoped(name, file); len(locs) > 0 {
			return locs[0].Kind, true
		}
		if r.ix.DeclaredCommand(name) {
			return tcl.DefProc, true // existence only; no location to jump to
		}
	}
	return 0, false
}

// FileHighlights returns every occurrence of the symbol at offset WITHIN file --
// its reference sites and its in-file declaration(s) -- for document highlight.
// Unlike References it never scans other files, so it stays cheap at cursor-move
// frequency (and needs no namespace pre-warm: a single file resolves
// single-threaded). Proc-locals return their in-file bindings and uses.
func (r *Resolver) FileHighlights(file, src string, offset int) []index.Location {
	if name, scope, ok := r.localAt(file, src, offset); ok {
		return r.localReferences(file, src, name, scope) // already in-file only
	}
	target := r.targetFQ(file, src, offset)
	if target == "" {
		return nil
	}
	var targetKind tcl.DefKind
	if defs := r.lookupScoped(target, file); len(defs) > 0 {
		targetKind = defs[0].Kind
	}

	var out []index.Location
	// In-file reference sites (command / qualified-variable uses).
	refs := source.Refs(file, src)
	for i := range refs {
		if r.refFQ(&refs[i], file) == target {
			out = append(out, index.Location{File: file, Name: target, Kind: targetKind, NameStart: refs[i].Ref.Start, NameEnd: refs[i].Ref.End})
		}
	}
	// In-file declaration site(s), from live source so an unsaved edit is honored.
	for _, d := range source.Defs(file, src) {
		if d.Name == target && !isLocalBinding(d.Kind) {
			out = append(out, index.Location{File: file, Name: target, Kind: d.Kind, NameStart: d.NameStart, NameEnd: d.NameEnd})
		}
	}
	return out
}

// targetFQ returns the fully-qualified name of the symbol at offset: a definition
// name-range it falls within, else the reference there resolved to its FQ name.
// file selects the document (so .rvt is extracted) and scopes page-local lookups.
// Returns "" if there is no resolvable symbol.
func (r *Resolver) targetFQ(file, src string, offset int) string {
	for _, d := range source.Defs(file, src) {
		if (d.Kind == tcl.DefProc || d.Kind == tcl.DefNamespaceVar || d.Kind == tcl.DefClass) &&
			offset >= d.NameStart && offset < d.NameEnd {
			return d.Name
		}
	}
	if ref := refAt(file, src, offset); ref != nil {
		return r.refFQ(ref, file)
	}
	return ""
}

// lookupScoped returns the definition sites of a fully-qualified name. A
// page-local name (under ::request) resolves only to definitions in file, because
// the per-request namespace is recreated per page and not shared across templates.
// All other names resolve workspace-wide.
func (r *Resolver) lookupScoped(name, file string) []index.Location {
	locs := r.ix.Lookup(name)
	// Page-locality applies only when resolving from within a template: the
	// ::request namespace is recreated per request and not shared across .rvt
	// pages. A .tcl file that writes into ::request (unusual — outside the
	// supported model, see research/05-rvt-scope.md V2) resolves workspace-wide.
	if !source.IsRVT(file) || !strings.HasPrefix(name, "::request::") {
		return locs
	}
	var kept []index.Location
	for _, l := range locs {
		if l.File == file {
			kept = append(kept, l)
		}
	}
	return kept
}

// refFQ resolves a reference to the fully-qualified name it binds to, using the
// same first-match precedence as goto-definition. file is the document the ref
// lives in (used for page-local scoping in lookups). If no candidate is defined,
// the primary candidate is used so undefined references still group together.
func (r *Resolver) refFQ(ref *tcl.ContextRef, file string) string {
	cands := r.candidates(ref)
	for _, name := range cands {
		if len(r.lookupScoped(name, file)) > 0 {
			return name
		}
	}
	if len(cands) > 0 {
		return cands[0]
	}
	return ""
}

func isQualified(name string) bool { return strings.Contains(name, "::") }

// qualify resolves name against ns: a leading "::" is absolute; otherwise the
// name is qualified into ns.
func qualify(name, ns string) string {
	if strings.HasPrefix(name, "::") {
		return name
	}
	if ns == "::" {
		return "::" + name
	}
	return ns + "::" + name
}
