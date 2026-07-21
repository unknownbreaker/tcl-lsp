// Package source produces definitions, references, and namespace declarations for
// a workspace file in SOURCE coordinates, dispatching on file type: .tcl is parsed
// directly; .rvt is extracted to a stitched ::request script (package rvt), parsed,
// and each offset translated back to .rvt coordinates. Both the index and the
// resolver use this seam so neither needs to know about templates.
package source

import (
	"strings"

	"github.com/unknownbreaker/tcl-lsp/internal/rvt"
	"github.com/unknownbreaker/tcl-lsp/internal/tcl"
)

// IsRVT reports whether path is a Rivet template, by extension.
func IsRVT(path string) bool { return strings.HasSuffix(path, ".rvt") }

// translateDefs maps definitions from stitched-script offsets back to .rvt
// source coordinates, dropping wrapper-synthetic entries (which map to -1).
// Shared by Defs and IndexUnit so the translation rule exists exactly once.
func translateDefs(doc rvt.Document, in []tcl.Definition) []tcl.Definition {
	var out []tcl.Definition
	for _, d := range in {
		s := doc.ToSource(d.NameStart)
		if s < 0 {
			continue
		}
		d.NameStart, d.NameEnd = s, s+(d.NameEnd-d.NameStart)
		if fs := doc.ToSource(d.FullStart); fs >= 0 {
			d.FullEnd = fs + (d.FullEnd - d.FullStart)
			d.FullStart = fs
		} else {
			d.FullStart, d.FullEnd = d.NameStart, d.NameEnd // fall back to name range
		}
		out = append(out, d)
	}
	return out
}

// translateRefs maps references from stitched-script offsets back to .rvt
// source coordinates, dropping wrapper-synthetic entries (see translateDefs).
func translateRefs(doc rvt.Document, in []tcl.ContextRef) []tcl.ContextRef {
	var out []tcl.ContextRef
	for _, r := range in {
		s := doc.ToSource(r.Ref.Start)
		if s < 0 {
			continue
		}
		r.Ref.Start, r.Ref.End = s, s+(r.Ref.End-r.Ref.Start)
		out = append(out, r)
	}
	return out
}

// Defs returns the definitions declared in content, in source coordinates. For
// .rvt, name ranges are translated from the stitched script back to the .rvt;
// wrapper-synthetic definitions (which map to -1) are dropped.
func Defs(path, content string) []tcl.Definition {
	if !IsRVT(path) {
		return tcl.FileDefs(content)
	}
	doc := rvt.Extract(content)
	return translateDefs(doc, tcl.FileDefs(doc.Script))
}

// IndexUnit returns all four per-file analyses (defs, refs, namespaces, classes)
// for content in SOURCE coordinates, from a SINGLE parse -- and, for .rvt, a
// single Extract. It is the workspace index's entry point (index.IndexFile);
// callers that need only one analysis use Defs/Refs/Namespaces/Classes. The .rvt
// branch translates def/ref offsets back to .rvt coordinates and drops
// wrapper-synthetic entries (which map to -1), exactly as Defs/Refs do;
// namespaces and classes carry names only, so they need no translation.
func IndexUnit(path, content string) tcl.FileIndex {
	if !IsRVT(path) {
		return tcl.FileAll(content)
	}
	doc := rvt.Extract(content)
	fi := tcl.FileAll(doc.Script)
	fi.Defs = translateDefs(doc, fi.Defs)
	fi.Refs = translateRefs(doc, fi.Refs)
	return fi
}

// Refs returns the contextual references in content, in source coordinates (see
// Defs). The synthetic `namespace eval ::request` wrapper produces a `namespace`
// command-ref at a wrapper offset that maps to -1; such refs are dropped.
func Refs(path, content string) []tcl.ContextRef {
	if !IsRVT(path) {
		return tcl.FileRefs(content)
	}
	doc := rvt.Extract(content)
	return translateRefs(doc, tcl.FileRefs(doc.Script))
}

// Namespaces returns per-namespace declarations for content. NamespaceInfo holds
// names only (no offsets), so no translation is required; for .rvt the stitched
// script is parsed directly.
func Namespaces(path, content string) map[string]*tcl.NamespaceInfo {
	if !IsRVT(path) {
		return tcl.FileNamespaces(content)
	}
	return tcl.FileNamespaces(rvt.Extract(content).Script)
}

// Classes returns per-class inherit edges for content (classFQ -> []baseFQ).
// Names only (no offsets), so no translation is required; for .rvt the stitched
// script is parsed directly.
func Classes(path, content string) map[string][]string {
	if !IsRVT(path) {
		return tcl.FileClasses(content)
	}
	return tcl.FileClasses(rvt.Extract(content).Script)
}

// Folds returns the foldable braced script bodies in content, in SOURCE
// coordinates. For .tcl the parsed offsets are already source offsets. For .rvt
// both endpoints are translated from the stitched script back to the .rvt; a fold
// is dropped if either endpoint maps outside a real region (-1) — which discards
// the synthetic ::request wrapper body. A fold whose endpoints land in different
// regions (a construct opened in one <? ?> block and closed in a later one)
// survives and spans the intervening markup.
func Folds(path, content string) []tcl.FoldRange {
	if !IsRVT(path) {
		return tcl.FileFolds(content)
	}
	doc := rvt.Extract(content)
	var out []tcl.FoldRange
	for _, f := range tcl.FileFolds(doc.Script) {
		open := doc.ToSource(f.Open)
		closing := doc.ToSource(f.Close)
		if open < 0 || closing < 0 {
			continue
		}
		out = append(out, tcl.FoldRange{Open: open, Close: closing})
	}
	return out
}

// Reaching returns the local bindings that may reach the variable use at byte
// offset in content, in SOURCE coordinates. For .rvt the offset is mapped into the
// stitched ::request script and each result range is translated back to the .rvt;
// ranges that map outside a real region are dropped. ok is false when there is no
// reaching set (caller falls back).
func Reaching(path, content string, offset int) ([]tcl.Definition, bool) {
	if !IsRVT(path) {
		return tcl.ReachingAt(content, offset)
	}
	doc := rvt.Extract(content)
	vOff, ok := doc.ToVirtual(offset)
	if !ok {
		return nil, false
	}
	defs, ok := tcl.ReachingAt(doc.Script, vOff)
	if !ok {
		return nil, false
	}
	var out []tcl.Definition
	for _, d := range defs {
		s := doc.ToSource(d.NameStart)
		if s < 0 {
			continue
		}
		d.NameStart, d.NameEnd = s, s+(d.NameEnd-d.NameStart)
		out = append(out, d)
	}
	return out, len(out) > 0
}

// ClassOf returns the class set for a receiver variable at offset in content,
// in SOURCE coordinates. For .rvt the offset is mapped into the stitched ::request
// script; no translation is needed on the result (class names are plain strings).
func ClassOf(path, content string, offset int) []string {
	if !IsRVT(path) {
		return tcl.ClassOf(content, offset)
	}
	doc := rvt.Extract(content)
	vOff, ok := doc.ToVirtual(offset)
	if !ok {
		return nil
	}
	return tcl.ClassOf(doc.Script, vOff)
}

// ObjMethodAt detects the "$var method" command shape at byte offset in
// content, in SOURCE coordinates. Returns (receiverSourceOff, methodName, true)
// when offset falls on the second word of a "$var method" command; the receiver
// offset points to the variable name byte (after '$') in source coordinates.
// For .rvt, offset is translated into the stitched script, the shape is
// detected there, and the receiver offset is translated back to source.
func ObjMethodAt(path, content string, offset int) (receiverOff int, methodName string, ok bool) {
	if !IsRVT(path) {
		return tcl.ObjMethodAt(content, offset)
	}
	doc := rvt.Extract(content)
	vOff, mapped := doc.ToVirtual(offset)
	if !mapped {
		return 0, "", false
	}
	vReceiverOff, mn, found := tcl.ObjMethodAt(doc.Script, vOff)
	if !found {
		return 0, "", false
	}
	// Translate the receiver offset from virtual (stitched-script) to source.
	srcReceiverOff := doc.ToSource(vReceiverOff)
	if srcReceiverOff < 0 {
		return 0, "", false
	}
	return srcReceiverOff, mn, true
}
