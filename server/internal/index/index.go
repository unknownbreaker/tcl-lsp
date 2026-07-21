// Package index builds a workspace-wide, fully-qualified symbol table of TCL
// definitions across files, with incremental per-file updates.
package index

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/unknownbreaker/tcl-lsp/internal/source"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// Location is a single definition site.
type Location struct {
	File      string
	Name      string // fully-qualified name
	Kind      tcl.DefKind
	NameStart int
	NameEnd   int
}

// ClassInfo aggregates per-class member and inheritance information collected
// across all indexed files.
type ClassInfo struct {
	DefSites []Location            // all `itcl::class` declaration sites
	Methods  map[string][]Location // method name -> definition sites (inline + itcl::body)
	Ivars    map[string][]Location // ivar name -> declaration sites
	Inherit  []string              // fully-qualified base class names (deduplicated)
}

// Index holds workspace-visible definitions (procs and namespace variables)
// keyed by fully-qualified name, plus each file's source for later analysis.
// Index is not safe for concurrent use; the LSP server (protocol layer, a later
// plan) must serialize access (e.g. an RWMutex: shared for Lookup/Files/Source,
// exclusive for IndexFile/RemoveFile).
type Index struct {
	defsByName       map[string][]Location                    // FQ name -> all definition sites
	fileDefs         map[string][]string                      // file -> FQ names it defines (for removal)
	src              map[string]string                        // file -> source text
	fileNS           map[string]map[string]*tcl.NamespaceInfo // file -> (ns name -> decls)
	fileRefs         map[string][]tcl.ContextRef              // file -> precomputed reference sites
	nsCache          map[string]nsMerged                      // memoized merged path/imports per ns
	classes          map[string]*ClassInfo                    // classFQ -> aggregated class info
	fileClassKeys    map[string][]string                      // file -> class FQs it contributed to
	fileClassInherit map[string]map[string][]string           // file -> classFQ -> inherit edges from that file
}

// nsMerged is the merged namespace-path and import set for one namespace, cached
// so Namespace need not rescan every file on each call (it is queried once per
// unqualified command reference during a references scan — O(refs) calls).
type nsMerged struct {
	path    []string
	imports []string
}

// New returns an empty Index.
func New() *Index {
	return &Index{
		defsByName:       map[string][]Location{},
		fileDefs:         map[string][]string{},
		src:              map[string]string{},
		fileNS:           map[string]map[string]*tcl.NamespaceInfo{},
		fileRefs:         map[string][]tcl.ContextRef{},
		nsCache:          map[string]nsMerged{},
		classes:          map[string]*ClassInfo{},
		fileClassKeys:    map[string][]string{},
		fileClassInherit: map[string]map[string][]string{},
	}
}

// IsIndexable reports whether path is a source file the index tracks: .tcl or
// .rvt. This is THE suffix rule -- the workspace walk (IndexDirProgress), the
// LSP server's file-watcher filter, and the external-tree walk all call it, so
// a new extension is added in exactly one place. External trees additionally
// accept .tm (isExtraIndexable).
func IsIndexable(path string) bool {
	return strings.HasSuffix(path, ".tcl") || strings.HasSuffix(path, ".rvt")
}

// isExtraIndexable is IsIndexable plus .tm: Tcl modules ship alongside .tcl in
// external libraries (extra_index_paths) but are not workspace files.
func isExtraIndexable(path string) bool {
	return IsIndexable(path) || strings.HasSuffix(path, ".tm")
}

// IndexFile records the workspace-visible definitions in content under path. Locals
// and global links are skipped (resolved frame-locally, not via the workspace
// table).
func (ix *Index) IndexFile(path, content string) {
	ix.RemoveFile(path)
	// One parse produces all four analyses (defs, refs, namespaces, classes);
	// see source.IndexUnit. Calling source.Defs/Refs/Namespaces/Classes
	// separately would re-parse (and, for .rvt, re-Extract) the file four times.
	ix.storeUnit(path, content, source.IndexUnit(path, content))
}

// storeUnit inserts a parsed file's analyses into the index's shared maps. It is
// split from the (pure, expensive) source.IndexUnit parse so the parallel initial
// index can run the parse off the ix concurrently while these mutations -- the
// only shared state -- stay single-threaded. See IndexDirProgress.
func (ix *Index) storeUnit(path, content string, unit tcl.FileIndex) {
	ix.src[path] = content
	ix.fileNS[path] = unit.Namespaces
	// Precompute reference sites once here so a references request iterates
	// stored data instead of re-parsing every workspace file (the dominant cost
	// on large repos). Resolution stays request-time (it depends on cross-file
	// namespace state); only the parse is hoisted.
	if len(unit.Refs) > 0 {
		ix.fileRefs[path] = unit.Refs
	}

	// Track which class FQs this file touches (for RemoveFile bookkeeping).
	classKeysSeen := map[string]bool{}

	for _, d := range unit.Defs {
		loc := Location{File: path, Name: d.Name, Kind: d.Kind, NameStart: d.NameStart, NameEnd: d.NameEnd}
		switch d.Kind {
		case tcl.DefProc, tcl.DefNamespaceVar, tcl.DefClass:
			ix.defsByName[d.Name] = append(ix.defsByName[d.Name], loc)
			ix.fileDefs[path] = append(ix.fileDefs[path], d.Name)
			if d.Kind == tcl.DefClass {
				ci := ix.ensureClass(d.Name)
				ci.DefSites = append(ci.DefSites, loc)
				classKeysSeen[d.Name] = true
			}
		case tcl.DefMethod:
			if d.Class != "" {
				ci := ix.ensureClass(d.Class)
				ci.Methods[d.Name] = append(ci.Methods[d.Name], loc)
				classKeysSeen[d.Class] = true
			}
		case tcl.DefIvar:
			if d.Class != "" {
				ci := ix.ensureClass(d.Class)
				ci.Ivars[d.Name] = append(ci.Ivars[d.Name], loc)
				classKeysSeen[d.Class] = true
			}
		}
	}

	// Record inherit edges from FileClasses. fileClassInherit stores each
	// file's RAW assertions (every edge the file declares, deduped within the
	// file) -- not a delta against the merged view. The merged ci.Inherit is
	// DERIVED from all files' assertions (rebuildInherit), so an edge asserted
	// by two files survives removal of either one. (The old delta model
	// credited only the first-indexed file, and removing it dropped the edge
	// even though another file still declared it.)
	for classFQ, bases := range unit.Classes {
		ix.ensureClass(classFQ)
		classKeysSeen[classFQ] = true
		if ix.fileClassInherit[path] == nil {
			ix.fileClassInherit[path] = map[string][]string{}
		}
		seen := map[string]bool{}
		var asserted []string
		for _, base := range bases {
			if !seen[base] {
				seen[base] = true
				asserted = append(asserted, base)
			}
		}
		ix.fileClassInherit[path][classFQ] = asserted
		ix.rebuildInherit(classFQ)
	}

	// Record the set of class keys this file contributed.
	for fq := range classKeysSeen {
		ix.fileClassKeys[path] = append(ix.fileClassKeys[path], fq)
	}
}

// ensureClass returns the ClassInfo for fq, creating it if absent.
func (ix *Index) ensureClass(fq string) *ClassInfo {
	if ix.classes[fq] == nil {
		ix.classes[fq] = &ClassInfo{
			Methods: map[string][]Location{},
			Ivars:   map[string][]Location{},
		}
	}
	return ix.classes[fq]
}

// rebuildInherit recomputes fq's merged Inherit list from every indexed file's
// raw assertions. Files are visited in sorted order (deterministic across
// removals); within a file, edges keep declaration order; duplicates across
// files collapse to first occurrence. For the common single-file case this
// reduces to that file's declaration order.
func (ix *Index) rebuildInherit(fq string) {
	ci := ix.classes[fq]
	if ci == nil {
		return
	}
	var files []string
	for f, m := range ix.fileClassInherit {
		if len(m[fq]) > 0 {
			files = append(files, f)
		}
	}
	sort.Strings(files)
	seen := map[string]bool{}
	var merged []string
	for _, f := range files {
		for _, base := range ix.fileClassInherit[f][fq] {
			if !seen[base] {
				seen[base] = true
				merged = append(merged, base)
			}
		}
	}
	ci.Inherit = merged
}

// RemoveFile drops all definitions and stored source contributed by path.
func (ix *Index) RemoveFile(path string) {
	for _, name := range ix.fileDefs[path] {
		locs := ix.defsByName[name]
		// kept reuses locs' backing array; this is safe because Lookup returns
		// copies, so no external caller aliases this slice.
		kept := locs[:0]
		for _, l := range locs {
			if l.File != path {
				kept = append(kept, l)
			}
		}
		if len(kept) == 0 {
			delete(ix.defsByName, name)
		} else {
			ix.defsByName[name] = kept
		}
	}
	delete(ix.fileDefs, path)

	// Remove this file's class contributions (DefSites, Methods, Ivars, Inherit).
	// Drop the file's raw inherit assertions FIRST so rebuildInherit below
	// derives each class's merged Inherit from the remaining files only.
	delete(ix.fileClassInherit, path)
	for _, fq := range ix.fileClassKeys[path] {
		ci := ix.classes[fq]
		if ci == nil {
			continue
		}
		// Filter DefSites.
		ci.DefSites = filterLocs(ci.DefSites, path)
		// Filter Methods.
		for name, locs := range ci.Methods {
			filtered := filterLocs(locs, path)
			if len(filtered) == 0 {
				delete(ci.Methods, name)
			} else {
				ci.Methods[name] = filtered
			}
		}
		// Filter Ivars.
		for name, locs := range ci.Ivars {
			filtered := filterLocs(locs, path)
			if len(filtered) == 0 {
				delete(ci.Ivars, name)
			} else {
				ci.Ivars[name] = filtered
			}
		}
		// Re-derive the merged inherit list from the remaining files' assertions.
		ix.rebuildInherit(fq)
		// Drop class entry entirely when no contributions remain.
		if len(ci.DefSites) == 0 && len(ci.Methods) == 0 && len(ci.Ivars) == 0 && len(ci.Inherit) == 0 {
			delete(ix.classes, fq)
		}
	}
	delete(ix.fileClassKeys, path)

	delete(ix.src, path)
	delete(ix.fileNS, path)
	delete(ix.fileRefs, path)
	// Namespace data spans files, so any file change can alter a merged result;
	// drop the whole memo and let reads rebuild lazily. IndexFile calls RemoveFile
	// first, so this covers re-index too. Reads never happen mid-mutation (the
	// server serializes index access), so lazy rebuild is safe.
	clear(ix.nsCache)
}

// filterLocs returns locs with all entries from excludeFile removed.
// It reuses the slice's backing array (safe because Lookup returns copies).
func filterLocs(locs []Location, excludeFile string) []Location {
	kept := locs[:0]
	for _, l := range locs {
		if l.File != excludeFile {
			kept = append(kept, l)
		}
	}
	return kept
}

// Class returns the aggregated ClassInfo for a fully-qualified class name, or
// nil if the class has not been indexed.
func (ix *Index) Class(fq string) *ClassInfo {
	return ix.classes[fq]
}

// FileRefs returns the precomputed reference sites for path (nil if the file is
// not indexed or has no references). The returned slice is read-only; callers
// must not mutate it.
func (ix *Index) FileRefs(path string) []tcl.ContextRef {
	return ix.fileRefs[path]
}

// Lookup returns all definition sites for a fully-qualified name (nil if none).
// The returned slice is a copy; callers may retain it across Index mutations.
func (ix *Index) Lookup(name string) []Location {
	locs := ix.defsByName[name]
	if len(locs) == 0 {
		return nil
	}
	out := make([]Location, len(locs))
	copy(out, locs)
	return out
}

// Files returns the indexed file paths, sorted for deterministic iteration.
func (ix *Index) Files() []string {
	out := make([]string, 0, len(ix.src))
	for p := range ix.src {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Source returns the stored source for a file ("" if not indexed).
func (ix *Index) Source(path string) string {
	return ix.src[path]
}

// Namespace returns the merged command-search path and import source patterns
// declared for ns across the workspace, deduplicated and ordered by file then
// declaration order. Used for command resolution (namespace path / import).
// Variables are unaffected by these declarations.
// Note: this is a union approximation — in real TCL a later `namespace path`
// call replaces the earlier one, but we merge across files for static analysis.
// Namespace returns the merged namespace-path and import set declared for ns
// across all files, sorted by file for determinism. The result is memoized and
// invalidated on any index mutation; the returned slices are read-only and must
// not be mutated by callers.
func (ix *Index) Namespace(ns string) (path []string, imports []string) {
	if m, ok := ix.nsCache[ns]; ok {
		return m.path, m.imports
	}

	files := make([]string, 0, len(ix.fileNS))
	for f := range ix.fileNS {
		files = append(files, f)
	}
	sort.Strings(files)

	seenP, seenI := map[string]bool{}, map[string]bool{}
	for _, f := range files {
		info := ix.fileNS[f][ns]
		if info == nil {
			continue
		}
		for _, p := range info.Path {
			if !seenP[p] {
				seenP[p] = true
				path = append(path, p)
			}
		}
		for _, im := range info.Imports {
			if !seenI[im] {
				seenI[im] = true
				imports = append(imports, im)
			}
		}
	}
	ix.nsCache[ns] = nsMerged{path: path, imports: imports}
	return path, imports
}

// SymbolEntry is a flattened workspace symbol suitable for workspace/symbol responses.
type SymbolEntry struct {
	Name      string // simple name (last :: segment)
	Kind      tcl.DefKind
	File      string
	NameStart int
	NameEnd   int
	Container string // enclosing namespace FQ (procs/vars/classes) or class FQ (methods/ivars)
}

// splitFQName splits a fully-qualified TCL name into its simple (last segment)
// and container parts. Examples:
//
//	"::app::run" -> ("run", "::app")
//	"::render"   -> ("render", "::")
//	"::C"        -> ("C", "::")
func splitFQName(fq string) (simple, container string) {
	idx := strings.LastIndex(fq, "::")
	if idx < 0 {
		return fq, "::"
	}
	simple = fq[idx+2:]
	prefix := fq[:idx]
	if prefix == "" {
		container = "::"
	} else {
		container = prefix
	}
	return simple, container
}

// AllSymbols returns a flattened slice of every indexed symbol: procs,
// namespace vars, and classes (from defsByName), plus methods and ivars (from
// the classes table). The result is sorted by (File, NameStart) for
// deterministic output regardless of map-iteration order.
func (ix *Index) AllSymbols() []SymbolEntry {
	var out []SymbolEntry

	// Procs, namespace vars, and classes from defsByName.
	for _, locs := range ix.defsByName {
		for _, loc := range locs {
			switch loc.Kind {
			case tcl.DefProc, tcl.DefNamespaceVar, tcl.DefClass:
				simple, container := splitFQName(loc.Name)
				out = append(out, SymbolEntry{
					Name:      simple,
					Kind:      loc.Kind,
					File:      loc.File,
					NameStart: loc.NameStart,
					NameEnd:   loc.NameEnd,
					Container: container,
				})
			}
		}
	}

	// Methods and ivars from the classes table.
	for classFQ, ci := range ix.classes {
		for _, locs := range ci.Methods {
			for _, loc := range locs {
				out = append(out, SymbolEntry{
					Name:      loc.Name, // already bare
					Kind:      tcl.DefMethod,
					File:      loc.File,
					NameStart: loc.NameStart,
					NameEnd:   loc.NameEnd,
					Container: classFQ,
				})
			}
		}
		for _, locs := range ci.Ivars {
			for _, loc := range locs {
				out = append(out, SymbolEntry{
					Name:      loc.Name, // already bare
					Kind:      tcl.DefIvar,
					File:      loc.File,
					NameStart: loc.NameStart,
					NameEnd:   loc.NameEnd,
					Container: classFQ,
				})
			}
		}
	}

	// Sort by (File, NameStart) for deterministic output.
	sort.Slice(out, func(i, j int) bool {
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].NameStart < out[j].NameStart
	})

	return out
}

// IndexDir walks root and indexes every *.tcl and *.rvt file found (recursively). A
// per-entry read error is recorded and the walk continues, so one unreadable
// file or directory cannot truncate the whole workspace index. The `.git`
// directory is skipped. The returned error aggregates any failures (nil if none).
func (ix *Index) IndexDir(root string) error {
	return ix.IndexDirProgress(root, nil)
}

// IndexDirProgress is IndexDir with an optional per-file callback: progress(n) is
// invoked with the running count after each file is indexed (pass nil to skip).
// The callback runs synchronously in the walk, so a caller that reports it to a
// client should throttle (this layer does not).
func (ix *Index) IndexDirProgress(root string, progress func(indexed int)) error {
	var errs []error

	// Phase 1: collect the source files (sequential). The walk is cheap, and its
	// lexical order is what makes the parallel store below deterministic.
	var paths []string
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			errs = append(errs, err) // unreadable entry: record, keep walking
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return fs.SkipDir // never contains .tcl; skip the noise
			}
			return nil
		}
		if IsIndexable(p) {
			paths = append(paths, p)
		}
		return nil
	})
	if walkErr != nil {
		errs = append(errs, walkErr)
	}

	// Phase 2: read + parse in parallel -- the dominant cost, and independent per
	// file. source.IndexUnit (called in the workers) is pure, so this is race-free;
	// each results slot is written by exactly one worker and read only after
	// wg.Wait, so the slice needs no lock. This buffers all parse results in memory
	// before the store phase; freed once stored.
	type parsed struct {
		content string
		unit    tcl.FileIndex
		err     error
	}
	results := make([]parsed, len(paths))

	workers := runtime.GOMAXPROCS(0)
	if workers > len(paths) {
		workers = len(paths)
	}

	// Progress is driven by parse completion (the slow phase) and reported by a
	// single goroutine, so progress() -- which writes to the server's output -- is
	// never called from more than one goroutine. It counts completions in arrival
	// order; the caller only shows the running count, so order does not matter.
	var progressCh chan struct{}
	reporterDone := make(chan struct{})
	if progress != nil && len(paths) > 0 {
		progressCh = make(chan struct{}, workers)
		go func() {
			count := 0
			for range progressCh {
				count++
				progress(count)
			}
			close(reporterDone)
		}()
	} else {
		close(reporterDone)
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				b, err := os.ReadFile(paths[i])
				if err != nil {
					results[i] = parsed{err: fmt.Errorf("indexing %s: %w", paths[i], err)}
					continue // an unreadable file is not "indexed" -- do not count it
				}
				content := string(b)
				results[i] = parsed{content: content, unit: source.IndexUnit(paths[i], content)}
				if progressCh != nil {
					progressCh <- struct{}{} // count only successfully-parsed files
				}
			}
		}()
	}
	for i := range paths {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if progressCh != nil {
		close(progressCh)
	}
	<-reporterDone

	// Phase 3: store into the index serially, in walk order -- an identical result
	// to the old sequential index, so lookups (and their tests) are unchanged.
	for i, r := range results {
		if r.err != nil {
			errs = append(errs, r.err)
			continue
		}
		ix.RemoveFile(paths[i])
		ix.storeUnit(paths[i], r.content, r.unit)
	}
	return errors.Join(errs...)
}

// IndexExtraTree indexes an EXTERNAL source tree (extra_index_paths), FOLLOWING
// symlinks -- unlike the workspace walk. Nix profiles and Homebrew opt/ paths
// are symlink forests: the root, the package directories, and even individual
// files are links into a store, and filepath.WalkDir follows none of them, so
// the standard walk indexes almost nothing there. Cycles are guarded by
// tracking each directory's resolved identity. Files keep their TRAVERSAL path
// (the stable profile-relative name), not the resolved store path, so goto-def
// targets survive store churn. Also indexes .tm (Tcl modules), which external
// libraries ship alongside .tcl; the workspace walk is deliberately unchanged
// (not following links there avoids escaping the repo). Returns the number of
// files indexed.
//
// seen maps resolved identities (dirs AND files) already visited, for cycle and
// duplicate protection. Pass ONE map across all configured roots: overlapping
// roots (a profile and its subdir, two generation links into the same store)
// must still collapse each real file to a single goto-def location.
func (ix *Index) IndexExtraTree(root string, seen map[string]bool) (int, error) {
	indexed := 0
	var errs []error
	var walk func(path string)
	walk = func(path string) {
		fi, err := os.Stat(path) // follows symlinks
		if err != nil {
			errs = append(errs, err)
			return
		}
		if !fi.IsDir() {
			if isExtraIndexable(path) {
				// Dedup files by resolved identity too: two links to the same
				// store file must not yield two goto-def locations. The first
				// traversal path (ReadDir-sorted, so deterministic) wins.
				real, rerr := filepath.EvalSymlinks(path)
				if rerr != nil {
					errs = append(errs, rerr)
					return
				}
				if seen[real] {
					return
				}
				seen[real] = true
				b, rerr := os.ReadFile(path)
				if rerr != nil {
					errs = append(errs, rerr)
					return
				}
				ix.IndexFile(path, string(b))
				indexed++
			}
			return
		}
		real, err := filepath.EvalSymlinks(path)
		if err != nil {
			errs = append(errs, err)
			return
		}
		if seen[real] {
			return // cycle, or the same store dir reachable via two links
		}
		seen[real] = true
		entries, err := os.ReadDir(path)
		if err != nil {
			errs = append(errs, err)
			return
		}
		for _, e := range entries { // ReadDir sorts: deterministic order
			walk(filepath.Join(path, e.Name()))
		}
	}
	walk(root)
	return indexed, errors.Join(errs...)
}
