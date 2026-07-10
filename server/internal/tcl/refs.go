package tcl

import "strings"

// RefKind classifies a reference by its syntactic position.
type RefKind int

const (
	RefCommand  RefKind = iota // command-position name (a command being invoked)
	RefVariable                // a $-substituted variable
)

// Reference is one classified identifier occurrence with an absolute byte range.
type Reference struct {
	Kind  RefKind
	Name  string
	Start int
	End   int
}

// CommandRefs returns the references in a single command: the command-position
// name (when the first word is a literal name) plus the variable references in
// every word. Offsets are absolute when the command's word offsets are absolute
// (as produced by Parse on source text). References nested inside
// [command substitution] spans are included via substRefs.
func CommandRefs(c Command) []Reference {
	var refs []Reference
	// Braced words this command evaluates as expressions (expr args, if/while/for
	// conditions). Tcl evaluates [command substitutions] inside them, so they are
	// scanned for embedded calls -- keyed by start offset, which is unique per word.
	exprWord := map[int]bool{}
	for _, e := range exprBodies(c.Words) {
		exprWord[e.Start] = true
	}
	for idx, w := range c.Words {
		if idx == 0 && isLiteralName(w) {
			refs = append(refs, Reference{Kind: RefCommand, Name: w.Text, Start: w.Start, End: w.End})
			continue
		}
		if w.Kind == WordBraced && exprWord[w.Start] {
			// Scan only the bracket spans so embedded calls are found while bare
			// operands and $vars stay non-references (braces suppress those).
			inner, innerBase := bracedInner(w, 0)
			refs = append(refs, exprBracketRefs(inner, innerBase)...)
			continue
		}
		refs = append(refs, wordRefs(w)...)
	}
	refs = append(refs, nameArgRefs(c)...)
	return refs
}

// arrayNameSubcmds are the `array` subcommands whose first argument is defined
// by the command's contract to be an array VARIABLE NAME that is read (or, for
// unset, destroyed) -- not written-and-defined. `array set` is deliberately
// absent: its target already emits a Definition, and emitting a reference too
// would double-list the site in find-references (declarations are added
// separately via includeDeclaration).
var arrayNameSubcmds = map[string]bool{
	"anymore": true, "donesearch": true, "exists": true, "get": true,
	"names": true, "nextelement": true, "size": true, "startsearch": true,
	"statistics": true, "unset": true,
}

// nameArgRefs emits variable references for bareword VARIABLE-NAME arguments of
// commands whose contract defines those positions as variable names:
//
//	info exists NAME
//	unset ?-nocomplain? ?--? NAME ?NAME ...?
//	array <read-subcmd> NAME
//
// These names are passed as data (no `$`), so the ordinary scanners never see
// them -- `if {[info exists ::cfg($k)]}` was invisible to goto-def and
// find-references. Restricting to this fixed command list keeps the never-wrong
// bet: the position IS a variable name by the command's contract, not by
// inference (subject to the same accepted lightweight-parsing caveat as every
// literal-head match here: a user proc shadowing `info`/`unset`/`array` would
// be misread -- see isCmd's note). For an array element the reference covers
// only the base name (the subscript may be dynamic and is scanned separately
// for its own $refs); dynamic names ($n, [expr]) are skipped by arrayBaseName.
func nameArgRefs(c Command) []Reference {
	w := c.Words
	if len(w) == 0 || w[0].Kind != WordBare {
		return nil
	}
	var nameWords []Word
	switch w[0].Text {
	case "info":
		if len(w) >= 3 && w[1].Kind == WordBare && w[1].Text == "exists" {
			nameWords = w[2:3]
		}
	case "unset":
		// Per unset(n), flags (-nocomplain, --) form a LEADING run only, and
		// `--` terminates flag parsing -- after it, every word (even one
		// starting with '-') is a variable name: `unset -- -foo` unsets -foo.
		args := w[1:]
		for len(args) > 0 && args[0].Kind == WordBare && strings.HasPrefix(args[0].Text, "-") {
			terminator := args[0].Text == "--"
			args = args[1:]
			if terminator {
				break
			}
		}
		nameWords = args
	case "array":
		if len(w) >= 3 && w[1].Kind == WordBare && arrayNameSubcmds[w[1].Text] {
			nameWords = w[2:3]
		}
	}
	var out []Reference
	for _, nw := range nameWords {
		if name, s, e, ok := arrayBaseName(nw); ok {
			out = append(out, Reference{Kind: RefVariable, Name: name, Start: s, End: e})
		}
	}
	return out
}

// exprBracketRefs scans an expr's braced argument for [command substitution]
// spans only, recursing into each via substRefs. Unlike scanRefs it ignores
// bare $vars and operands, which are not substituted inside an expr brace.
func exprBracketRefs(text string, base int) []Reference {
	var refs []Reference
	i := 0
	for i < len(text) {
		switch c := text[i]; {
		case c == '\\' && i+1 < len(text):
			i += 2
		case c == '[':
			end := skipBracketSpan(text, i) // index just past the matching ']'
			innerEnd := end
			if end > i+1 && text[end-1] == ']' {
				innerEnd = end - 1
			}
			refs = append(refs, substRefs(text[i+1:innerEnd], base+i+1)...)
			i = end
		default:
			i++
		}
	}
	return refs
}

// isLiteralName reports whether a word is a static command name: a bareword with
// no substitution ($ or [). Dynamic heads ($cmd, [get]) are not command names.
func isLiteralName(w Word) bool {
	if w.Kind != WordBare || w.Text == "" {
		return false
	}
	for i := 0; i < len(w.Text); i++ {
		if w.Text[i] == '$' || w.Text[i] == '[' {
			return false
		}
	}
	return true
}

// wordRefs scans one word for variable references and command substitutions.
// Braced words undergo no substitution and yield none. Bare and quoted words are
// scanned for $var refs and [cmd] spans; bracket interiors are recursed into via
// substRefs.
func wordRefs(w Word) []Reference {
	if w.Kind == WordBraced {
		return nil
	}
	return scanRefs(w.Text, w.Start)
}

// scanRefs differs from scanVarRefs (varref.go): it descends into [cmd] spans
// (via substRefs) rather than skipping them, and also reports command-position
// names found inside those spans.
func scanRefs(text string, base int) []Reference {
	var refs []Reference
	i := 0
	for i < len(text) {
		c := text[i]
		switch {
		case c == '\\' && i+1 < len(text):
			i += 2
		case c == '$':
			ref, next, ok := parseVarRef(text, i, base)
			if ok {
				refs = append(refs, Reference{Kind: RefVariable, Name: ref.Name, Start: ref.Start, End: ref.End})
			}
			i = next
		case c == '[':
			end := skipBracketSpan(text, i) // index just past the matching ']'
			innerEnd := end
			// Strip the closing ']' if present; on unterminated input
			// skipBracketSpan returns len(text) and there is no ']' to remove.
			if end > i+1 && text[end-1] == ']' {
				innerEnd = end - 1
			}
			refs = append(refs, substRefs(text[i+1:innerEnd], base+i+1)...)
			i = end
		default:
			i++
		}
	}
	return refs
}

// substRefs extracts references from the interior of a [command substitution].
// innerBase is the absolute offset of the interior's first byte. The interior is
// itself a script, so it is parsed and each command recursed into; offsets are
// shifted from interior-relative to absolute.
func substRefs(inner string, innerBase int) []Reference {
	var refs []Reference
	for _, c := range Parse(inner) {
		for _, r := range CommandRefs(c) {
			r.Start += innerBase
			r.End += innerBase
			refs = append(refs, r)
		}
		// CommandRefs scans a command's words and the [substitutions] within them,
		// but does NOT enter a command's braced SCRIPT body (catch/foreach/if/eval/…
		// bodies) -- those are descended by walkAll via childBodies. When such a body
		// sits inside a [substitution], though, walkAll never reaches it (it only
		// descends top-level command bodies), so calls and vars inside e.g.
		// `[catch {… myproc …} err]` would be invisible. Descend them here, mirroring
		// walkAll. childBodies and CommandRefs partition a command's words (script
		// bodies vs. expr/substituted words), so there is no double-counting. The
		// neutral namespace frame only locates the body spans; the resulting
		// context-free References are tagged by the enclosing command at the walkAll
		// call site, as every other substitution ref already is.
		for _, b := range childBodies(c, innerBase, "::", FrameNamespace, 0, "") {
			refs = append(refs, substRefs(b.Inner, b.Base)...)
		}
	}
	return refs
}
