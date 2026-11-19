package tcl

import (
	"slices"
	"strings"
)

// DefKind classifies a definition.
type DefKind int

const (
	DefProc         DefKind = iota // a proc (command) definition
	DefNamespaceVar                // a namespace variable (variable / qualified set / ns-top set)
	DefLocal                       // a proc-local variable (param, set, upvar alias)
	DefGlobalLink                  // a `global name` link to ::name
	DefClass                       // an itcl::class definition
	DefMethod                      // an itcl class method (method/constructor/destructor/proc inside a class body)
	DefIvar                        // an itcl instance/class variable (variable/common inside a class body)
)

// Definition is a declaration site. Name is fully qualified for proc, class, and
// namespace-variable kinds; for locals it is the bare local name. NameStart and
// NameEnd are the absolute byte range of the declared name token. FullStart and
// FullEnd are the byte range of the entire defining command (first word start to
// last word end), set for symbol kinds (DefProc, DefNamespaceVar, DefClass,
// DefMethod, DefIvar); locals leave them 0.
type Definition struct {
	Kind      DefKind
	Name      string
	Namespace string // the namespace the definition lives in ("::" for locals' enclosing)
	NameStart int
	NameEnd   int
	FullStart int // byte offset of the first word of the defining command
	FullEnd   int // byte offset past the last word of the defining command
	Scope     int
	// Origin is the fully-qualified variable a global/upvar link points at
	// (e.g. `global config` -> "::config"), or "" when there is none or it is
	// not statically known. Used by goto-definition to chase past the link.
	Origin string
	// Class is the fully-qualified itcl class name this definition belongs to, or
	// a class key (classRefKey, unknownClassKey; index.CanonicalClass resolves
	// it), or "" when not inside a class body.
	Class string
}

// FileDefs parses src and returns the definitions it declares, recursing into
// namespace eval and proc bodies.
func FileDefs(src string) []Definition {
	var out []Definition
	walkFile(src, collectors{defs: &out})
	return promoteLinkedAssignments(out)
}

func emitDefs(c Command, base int, ns string, frame FrameKind, scope int, class string, wrapped, inClassNS bool, out *[]Definition) {
	w := c.Words
	var cmdStart, cmdEnd int
	if len(w) > 0 {
		cmdStart = base + w[0].Start
		cmdEnd = base + w[len(w)-1].End
	}
	if frame == FrameClass {
		// Real Itcl declares members with an access modifier far more often than
		// not (`public method`, `protected method`, `private variable`,
		// `private common`, ...). Strip a leading modifier so the keyword that
		// follows is matched; FullStart/FullEnd still span the whole command
		// (starting at the modifier).
		mw := memberWords(w)
		switch {
		case len(mw) >= 1 && (isCmd(mw, "constructor") || isCmd(mw, "destructor")):
			// constructor/destructor have no name word; use the keyword itself as the name.
			*out = append(*out, Definition{Kind: DefMethod, Name: mw[0].Text, Class: class,
				Namespace: ns, NameStart: base + mw[0].Start, NameEnd: base + mw[0].End, Scope: scope,
				FullStart: cmdStart, FullEnd: cmdEnd})
		case len(mw) >= 2 && (isCmd(mw, "method") || isCmd(mw, "proc")) && isPlainName(mw[1]):
			*out = append(*out, Definition{Kind: DefMethod, Name: mw[1].Text, Class: class,
				Namespace: ns, NameStart: base + mw[1].Start, NameEnd: base + mw[1].End, Scope: scope,
				FullStart: cmdStart, FullEnd: cmdEnd})
		case len(mw) >= 2 && (isCmd(mw, "variable") || isCmd(mw, "common")) && isPlainName(mw[1]):
			*out = append(*out, Definition{Kind: DefIvar, Name: mw[1].Text, Class: class,
				Namespace: ns, NameStart: base + mw[1].Start, NameEnd: base + mw[1].End, Scope: scope,
				FullStart: cmdStart, FullEnd: cmdEnd})
		}
		return // class-body declarations handled; skip namespace/proc rules below
	}
	if isCmd(w, "proc") && len(w) >= 2 && isPlainName(w[1]) {
		if name, ok := nestedDefName(w[1].Text, ns, frame, class, wrapped, inClassNS); ok {
			*out = append(*out, Definition{
				Kind:      DefProc,
				Name:      name,
				Namespace: ns,
				NameStart: base + w[1].Start,
				NameEnd:   base + w[1].End,
				FullStart: cmdStart,
				FullEnd:   cmdEnd,
				Scope:     scope,
				Class:     class,
			})
		}
	}
	// A proc defined through a decorator macro (`CACHE_PROC proc name args body`).
	if nameWord, _, _, ok := decoratedProcDef(w); ok {
		if name, ok := nestedDefName(nameWord.Text, ns, frame, class, wrapped, inClassNS); ok {
			*out = append(*out, Definition{
				Kind:      DefProc,
				Name:      name,
				Namespace: ns,
				NameStart: base + nameWord.Start,
				NameEnd:   base + nameWord.End,
				FullStart: cmdStart,
				FullEnd:   cmdEnd,
				Scope:     scope,
				Class:     class,
			})
		}
	}
	// A bare `variable NAME` in a body of unknown frame may be a TclOO declaration.
	if vname, ok := variableDeclName(w); ok && !isUncertainName(vname, frame) {
		*out = append(*out, Definition{
			Kind:      DefNamespaceVar,
			Name:      qualifyName(vname, ns),
			Namespace: ns,
			NameStart: base + w[1].Start,
			NameEnd:   base + w[1].End,
			FullStart: cmdStart,
			FullEnd:   cmdEnd,
			Scope:     scope,
			Class:     class,
		})
		if frame == FrameProc {
			// Inside a proc, `variable cfg` LINKS the local name to the namespace
			// variable (like `global` links to ::cfg) -- Origin makes the link
			// chaseable and lets later assignments promote to the namespace var.
			*out = append(*out, Definition{
				Kind: DefLocal, Name: vname, Namespace: ns,
				NameStart: base + w[1].Start, NameEnd: base + w[1].End, Scope: scope,
				Origin: qualifyName(vname, ns),
				Class:  class,
			})
		}
	}
	if isCmd(w, "set") && len(w) >= 2 {
		if name, s, e, ok := arrayBaseName(w[1]); ok {
			emitVarAssign(name, s, e, base, ns, frame, scope, class, cmdStart, cmdEnd, out)
		}
	}
	// `array set NAME data` defines/assigns array NAME exactly like `set` -- the
	// dominant idiom for global config tables (`array set ::cfg {...}`).
	if isCmd(w, "array") && len(w) >= 3 && w[1].Kind == WordBare && w[1].Text == "set" {
		if name, s, e, ok := arrayBaseName(w[2]); ok {
			emitVarAssign(name, s, e, base, ns, frame, scope, class, cmdStart, cmdEnd, out)
		}
	}
	if frame == FrameProc && len(w) >= 2 {
		switch {
		case isCmd(w, "incr"), isCmd(w, "append"), isCmd(w, "lappend"):
			if name, s, e, ok := arrayBaseName(w[1]); ok {
				emitVarAssign(name, s, e, base, ns, frame, scope, class, cmdStart, cmdEnd, out)
			}
		}
	}
	if frame == FrameProc {
		resultWords, _ := resultVarWords(w)
		for _, words := range [][]Word{catchVarWords(w), resultWords} {
			for _, vw := range words {
				if name, s, e, ok := arrayBaseName(vw); ok {
					emitVarAssign(name, s, e, base, ns, frame, scope, class, cmdStart, cmdEnd, out)
				}
			}
		}
	}
	if isCmd(w, "global") && frame == FrameProc {
		for _, gw := range w[1:] {
			if gname, ok := plainVarName(gw); ok {
				*out = append(*out, Definition{
					Kind: DefGlobalLink, Name: gname, Namespace: ns,
					NameStart: base + gw.Start, NameEnd: base + gw.End, Scope: scope,
					Origin: globalOrigin(gname),
					Class:  class,
				})
			}
		}
	}
	if isCmd(w, "upvar") && frame == FrameProc && len(w) >= 3 {
		args := w[1:]
		// Optional leading level (e.g. 1 or #0); the rest are (otherVar, alias)
		// pairs. The alias names are static locals; a target is chaseable only
		// when qualified or reached via the #0 (global) frame -- see upvarOrigin.
		level := ""
		if len(args) > 0 && isUpvarLevel(args[0]) {
			level = args[0].Text
			args = args[1:]
		}
		for i := 1; i < len(args); i += 2 {
			alias := args[i]
			if aname, ok := plainVarName(alias); ok {
				*out = append(*out, Definition{
					Kind: DefLocal, Name: aname, Namespace: ns,
					NameStart: base + alias.Start, NameEnd: base + alias.End, Scope: scope,
					Origin: upvarOrigin(level, args[i-1]),
					Class:  class,
				})
			}
		}
	}
	// w[0]=itcl::class  w[1]=ClassName  w[2]=class body (required, hence len >= 3)
	if isItclClassCmd(w) && len(w) >= 3 && isPlainName(w[1]) {
		if name, ok := nestedDefName(w[1].Text, ns, frame, class, wrapped, inClassNS); ok {
			*out = append(*out, Definition{
				Kind:      DefClass,
				Name:      name,
				Namespace: ns,
				NameStart: base + w[1].Start,
				NameEnd:   base + w[1].End,
				FullStart: cmdStart,
				FullEnd:   cmdEnd,
				Scope:     scope,
				Class:     class,
			})
		}
	}
	// w[0]=itcl::body  w[1]=::Class::method  w[2]=args  w[3]=body
	// External method body definitions: `itcl::body ::C::m {args} {body}`.
	// Split the qualified name on the last :: to get the class and method name.
	if (isCmd(w, "itcl::body") || isCmd(w, "::itcl::body")) && len(w) >= 2 && isPlainName(w[1]) {
		full := w[1].Text
		if i := strings.LastIndex(full, "::"); i > 0 {
			classFQ := classRefKey(full[:i], ns)
			methodSeg := full[i+2:]
			if methodSeg != "" {
				segStart := w[1].Start + i + 2
				*out = append(*out, Definition{
					Kind:      DefMethod,
					Name:      methodSeg,
					Class:     classFQ,
					Namespace: ns,
					NameStart: base + segStart,
					NameEnd:   base + w[1].End,
					FullStart: cmdStart,
					FullEnd:   cmdEnd,
					Scope:     scope,
				})
			}
		}
	}
	if frame == FrameProc {
		emitLoopVarDefs(w, base, ns, scope, class, out)
	}
}

// isUncertainName reports whether a variable name defines nothing in frame. In a
// body of unknown frame only an absolute name is certain: a relative qualified name
// resolves from the namespace the body runs in, which is not known.
func isUncertainName(name string, frame FrameKind) bool {
	return frame == FrameOpaque && !strings.HasPrefix(name, "::")
}

// emitVarAssign records a variable-assignment target (`set`, `array set`,
// `incr`/`append`/`lappend`). At namespace frame -- or for a QUALIFIED name in
// any frame -- it is a namespace-variable definition: `set ::x 1` inside a proc
// writes the global directly in TCL, so indexing it as a proc-local both hid
// the workspace definition and blocked cross-file `$::x` resolution. A bare
// name at proc frame stays a proc-local binding. In a body of unknown frame only an
// absolute name defines anything: a bare name could be local or not.
func emitVarAssign(name string, s, e, base int, ns string, frame FrameKind, scope int, class string, cmdStart, cmdEnd int, out *[]Definition) {
	if isUncertainName(name, frame) {
		return
	}
	if frame == FrameNamespace || strings.Contains(name, "::") {
		*out = append(*out, Definition{
			Kind:      DefNamespaceVar,
			Name:      qualifyName(name, ns),
			Namespace: ns,
			NameStart: base + s,
			NameEnd:   base + e,
			FullStart: cmdStart,
			FullEnd:   cmdEnd,
			Scope:     scope,
			Class:     class,
		})
		return
	}
	*out = append(*out, Definition{
		Kind: DefLocal, Name: name, Namespace: ns,
		NameStart: base + s, NameEnd: base + e, Scope: scope,
		Class: class,
	})
}

// promoteLinkedAssignments appends a namespace-variable definition for each
// proc-local assignment that writes through an earlier same-scope link
// (`global cfg; set cfg 1`, `upvar #0 x y; set y 1`): executing the proc
// assigns the link's ORIGIN variable, so the assignment site is a
// workspace-visible definition of it -- the way many real codebases create
// their globals (an init proc). The DefLocal binding is kept, so proc-local
// resolution (reaching-defs, highlights) is unchanged; the promoted def only
// makes `$::cfg` elsewhere find the site. Promotions are inserted immediately
// after their local, keeping defs in ascending source order (the symbol
// builder relies on that ordering).
func promoteLinkedAssignments(defs []Definition) []Definition {
	type key struct {
		scope int
		name  string
	}
	type link struct {
		start  int
		origin string
	}
	links := map[key][]link{}
	for _, d := range defs {
		if d.Origin == "" || (d.Kind != DefGlobalLink && d.Kind != DefLocal) {
			continue
		}
		k := key{d.Scope, d.Name}
		links[k] = append(links[k], link{d.NameStart, d.Origin})
	}
	if len(links) == 0 {
		return defs
	}
	out := make([]Definition, 0, len(defs))
	for _, d := range defs {
		out = append(out, d)
		if d.Kind != DefLocal || d.Origin != "" {
			continue // promote only plain assignments, not the links themselves
		}
		// TCL links are last-write-wins for a local name: an assignment writes
		// through the NEAREST PRECEDING link, so a rebound alias (`global cfg`
		// then `upvar #0 other cfg`) attributes later sets to ::other, not ::cfg.
		bestStart, origin := -1, ""
		for _, l := range links[key{d.Scope, d.Name}] {
			if l.start < d.NameStart && l.start > bestStart {
				bestStart, origin = l.start, l.origin
			}
		}
		if origin == "" {
			continue
		}
		ns := "::"
		if i := strings.LastIndex(origin, "::"); i > 0 {
			ns = origin[:i]
		}
		out = append(out, Definition{
			Kind: DefNamespaceVar, Name: origin, Namespace: ns,
			NameStart: d.NameStart, NameEnd: d.NameEnd,
			Scope: d.Scope, Class: d.Class,
		})
	}
	return out
}

// isPlainName is currently equivalent to isLiteralName; kept as a distinct
// entry point in case definition-target rules diverge from command-name rules.
//
// isPlainName reports whether a word is a bareword usable as a declared name
// (no substitution). Used for proc/variable/set targets.
func isPlainName(w Word) bool {
	return isLiteralName(w)
}

// arrayBaseName returns the variable name a definition target word binds, with a
// byte range relative to the parse base (callers add `base`). For an array
// element target like `arr(i)` or `arr($i)` it returns the base name `arr` and a
// range covering just `arr`; for a plain scalar target it returns the whole word.
// ok is false when the target is not a usable bare name (empty, a leading `(`, a
// `(` without a final `)`, or a base containing a substitution). The index
// between the parens may be anything. Backslash escapes are decoded first, as Tcl
// does: `a\x62` binds `ab` and `a\(k)` binds array `a`.
func arrayBaseName(w Word) (name string, start, end int, ok bool) {
	if w.Kind != WordBare || w.Text == "" {
		return "", 0, 0, false
	}
	if strings.IndexByte(w.Text, '\\') >= 0 {
		return escapedArrayBaseName(w)
	}
	p := strings.IndexByte(w.Text, '(')
	if p < 0 {
		if !isPlainName(w) {
			return "", 0, 0, false
		}
		return w.Text, w.Start, w.End, true
	}
	baseText := w.Text[:p]
	// `a(k)b` and `a(` are scalars of their own, which bind nothing here.
	if !isElementName(w.Text) {
		return "", 0, 0, false
	}
	for i := 0; i < len(baseText); i++ {
		if baseText[i] == '$' || baseText[i] == '[' {
			return "", 0, 0, false
		}
	}
	return baseText, w.Start, w.Start + p, true
}

// globalOrigin returns the fully-qualified global variable a `global NAME` links
// to: the global namespace always, so a bare name is qualified with "::".
func globalOrigin(name string) string {
	if strings.HasPrefix(name, "::") {
		return name
	}
	return "::" + name
}

// upvarOrigin returns the fully-qualified origin an `upvar` alias points at, or ""
// when it is not statically resolvable. A qualified target (`::x`) is absolute; a
// bare target is the global variable of that name only when the level is "#0"
// (the global frame). Frame-relative levels (default/`1`/`#N>0`) name a variable
// in another call frame and are dynamic -> "".
func upvarOrigin(level string, target Word) string {
	base, _, _, ok := arrayBaseName(target)
	if !ok {
		return "" // dynamic/substituted target (e.g. $name)
	}
	if strings.HasPrefix(base, "::") {
		return base
	}
	if level == "#0" {
		return "::" + base
	}
	return ""
}

// isUpvarLevel reports whether a word is an upvar level argument (a number like
// "1" or a #-prefixed absolute level like "#0"), as opposed to a variable name.
func isUpvarLevel(w Word) bool {
	if w.Kind != WordBare || w.Text == "" {
		return false
	}
	if w.Text[0] == '#' {
		return true
	}
	for i := 0; i < len(w.Text); i++ {
		if w.Text[i] < '0' || w.Text[i] > '9' {
			return false
		}
	}
	return true
}

// emitProcParams emits a DefLocal for each parameter name in a proc args word.
func emitProcParams(argsWord Word, base int, ns string, scope int, class string, out *[]Definition) {
	if emitEscapedListName(argsWord, base, ns, scope, class, out) {
		return
	}
	inner, innerBase := argsWord, base
	text := inner.Text
	start := innerBase + inner.Start
	if inner.Kind == WordBraced && len(text) >= 2 {
		text = text[1 : len(text)-1]
		start = innerBase + inner.Start + 1
	}
	for _, p := range scanParams(text, start, false) {
		*out = append(*out, localDef(p, ns, scope, class))
	}
}

type paramName struct {
	Name       string
	Start, End int
}

func scanParams(text string, base int, varList bool) []paramName {
	var ps []paramName
	cmds := Parse(text)
	for _, c := range cmds {
		for _, word := range c.Words {
			name, s, e := paramFromWord(word, base, varList)
			if name != "" {
				ps = append(ps, paramName{Name: name, Start: s, End: e})
			}
		}
	}
	return ps
}

// paramFromWord extracts the parameter name and its absolute range from one
// args-list element: a bareword `name`, or a braced `{name default}` (first
// inner word). base is the absolute offset of the params text's first byte.
func paramFromWord(w Word, base int, varList bool) (string, int, int) {
	if w.Kind == WordBraced && len(w.Text) >= 2 {
		inner := w.Text[1 : len(w.Text)-1]
		innerBase := base + w.Start + 1
		if varList {
			// A braced var-list element is one name, undecoded: {a b} names `a b`.
			if inner == "" {
				return "", 0, 0
			}
			return inner, innerBase, innerBase + len(inner)
		}
		for _, c := range Parse(inner) {
			if len(c.Words) > 0 {
				fw := c.Words[0]
				if name, ok := plainVarName(fw); ok {
					return name, innerBase + fw.Start, innerBase + fw.End
				}
				if strings.IndexByte(fw.Text, '\\') >= 0 {
					return "", 0, 0
				}
				return fw.Text, innerBase + fw.Start, innerBase + fw.End
			}
		}
		return "", 0, 0
	}
	if name, ok := plainVarName(w); ok {
		return name, base + w.Start, base + w.End
	}
	if w.Kind == WordBare && w.Text != "" && strings.IndexByte(w.Text, '\\') < 0 {
		return w.Text, base + w.Start, base + w.End
	}
	return "", 0, 0
}

// emitLoopVarDefs emits DefLocal bindings for loop/destructuring target variables
// that introduce proc-locals: foreach/lmap var lists, lassign targets, dict for/map
// key lists, dict update variables, and try handler variable lists. Called only in
// FrameProc.
func emitLoopVarDefs(w []Word, base int, ns string, scope int, class string, out *[]Definition) {
	if len(w) == 0 || w[0].Kind != WordBare {
		return
	}
	switch w[0].Text {
	case "foreach", "lmap":
		// (varlist list)+ body -- varlists sit at odd indices before the body.
		for i := 1; i+1 < len(w); i += 2 {
			emitVarListNames(w[i], base, ns, scope, class, out)
		}
	case "lassign":
		// lassign list var ?var ...? -- each target is one variable name, not a list.
		for _, vw := range w[2:] {
			if name, s, e, ok := arrayBaseName(vw); ok {
				*out = append(*out, localDef(paramName{Name: name, Start: base + s, End: base + e}, ns, scope, class))
			}
		}
	case "dict":
		// dict for {k v} dict body ; dict map {k v} dict body.
		if len(w) >= 5 && w[1].Kind == WordBare && (w[1].Text == "for" || w[1].Text == "map") {
			emitVarListNames(w[2], base, ns, scope, class, out)
		}
		// dict update dictVar key varName ?key varName ...? body -- each varName is
		// written before the body runs, or unset when its key is missing.
		for _, vw := range dictUpdateVarWords(w) {
			if name, s, e, ok := arrayBaseName(unquotedLiteral(vw)); ok {
				*out = append(*out, localDef(paramName{Name: name, Start: base + s, End: base + e}, ns, scope, class))
			}
		}
	case "try":
		_, handlers, _ := tryParts(w)
		for _, h := range handlers {
			*out = append(*out, tryHandlerDefs(h, base, ns, scope, class)...)
		}
	}
}

// catchVarWords returns the words naming the variables catch assigns: the result,
// then the options. catch takes at most four words; with more it is an argument
// error and assigns nothing. With an expanded word the roles are unknown, so nothing
// is certain. When both words name one variable, compiled catch (in a proc, the only
// frame these bind in) writes the result last, so only the result word binds. With
// two words, both must be plain names, so a quoted `{m} m` cannot hide that they are
// one variable, and two different spellings of one variable, such as `m m(x)`, bind
// nothing: Tcl writes one and fails on the other.
// With an escape in either word (`a\x62 ab`, even `a\x62 a\x62`), tclsh 8.6 and
// 9.0 write the options last, so only the options word binds.
func catchVarWords(w []Word) []Word {
	if !isCmd(w, "catch") || len(w) < 3 || len(w) > 4 || hasExpansion(w) {
		return nil
	}
	if len(w) == 4 {
		r, _, _, rok := arrayBaseName(w[2])
		o, _, _, ook := arrayBaseName(w[3])
		switch {
		case !rok || !ook:
			return nil
		case w[2].Text == w[3].Text && !strings.ContainsRune(w[2].Text, '\\'):
			return w[2:3]
		case decodedEqual(w[2], w[3]):
			// tclsh 8.6 and 9.0: once a word has an escape, the options are
			// written last, even when the two words are spelled alike.
			return w[3:4]
		case r == o:
			return nil
		}
	}
	return w[2:]
}

// resultVarWords returns the words naming the variables gets, regsub, regexp,
// binary scan and scan write, and whether the write is certain. Behavior follows
// tclsh 8.6 and 9.0. `gets chan var` always writes var when it returns, including ""
// at end of file, and regsub always writes its variable, so they replace earlier
// bindings like set. regexp writes its match variables only on a match, binary scan
// stops writing at the end of its data, and scan writes only the variables of the
// conversions it performs, so theirs are possible writes that join earlier
// bindings, as is a regsub write when a substituted word may be an option. With an
// expanded word, or a variable word that is not a plain name, the roles are unknown
// and nothing binds. Two words naming one variable (`m m`, `m m(x)`) bind nothing
// either.
func resultVarWords(w []Word) (words []Word, certain bool) {
	if hasExpansion(w) {
		return nil, false
	}
	switch {
	case isCmd(w, "gets"):
		if len(w) == 3 {
			words, certain = w[2:], true
		}
	case isCmd(w, "regexp"):
		words = regexpVarWords(w)
	case isCmd(w, "regsub"):
		words, certain = regsubVarWord(w)
	case isCmd(w, "scan"):
		// scan string format ?varName ...?; with no varName it returns a list
		if len(w) >= 4 {
			words = w[3:]
		}
	case isCmd(w, "binary"):
		if len(w) >= 5 && isScanSubcommand(w[1]) {
			words = w[4:]
		}
	}
	seen := map[string]bool{}
	for _, vw := range words {
		name, _, _, ok := arrayBaseName(vw)
		if !ok || seen[name] {
			return nil, false
		}
		seen[name] = true
	}
	return words, certain
}

// isScanSubcommand reports whether w is binary's scan subcommand. binary is an
// ensemble, so any prefix of scan works: no other subcommand starts with s.
func isScanSubcommand(w Word) bool {
	sub, ok := literalValue(w)
	return ok && sub != "" && strings.HasPrefix("scan", sub)
}

// regexpOperands returns the words of a regexp or regsub command after its options,
// starting at the pattern, or nil when an option is wrong. Options must be spelled
// in full (Tcl rejects `-noc`); flags lists the ones that take no argument and
// change no role, and `-start` takes one. Tcl reads every leading word that starts
// with `-` as an option, even a substituted one (`regexp $re ...` with re set to
// -nocase). So a substituted word in option position is read as the pattern, and
// sure is false: if it is an option at run time, the roles shift right and every
// variable is one of the words returned here, but some of them may not be written.
func regexpOperands(w []Word, flags ...string) (ops []Word, sure bool) {
	for i := 1; i < len(w); i++ {
		v, ok := literalValue(w[i])
		switch {
		case !ok:
			return w[i:], false
		case !strings.HasPrefix(v, "-"):
			return w[i:], true
		case v == "--":
			return w[i+1:], true
		case v == "-start":
			i++ // the option's index argument, whatever it looks like
		case !slices.Contains(flags, v):
			return nil, false
		}
	}
	return nil, false
}

// regsubFlags are the options of regsub that change no word's role, and
// regexpFlags those of regexp.
var (
	regsubFlags = []string{"-all", "-nocase", "-expanded", "-line", "-linestop", "-lineanchor"}
	regexpFlags = append([]string{"-indices"}, regsubFlags...)
)

// regexpVarWords returns the match variable words of `regexp ?options? exp string
// ?matchVar? ?subMatchVar ...?`. -about and -inline take no variables, so they bind
// nothing.
func regexpVarWords(w []Word) []Word {
	ops, _ := regexpOperands(w, regexpFlags...)
	if len(ops) < 3 {
		return nil
	}
	return ops[2:]
}

// regsubVarWord returns the variable word of `regsub ?options? exp string subSpec
// varName`, which Tcl always writes, the input unchanged when nothing matches, so
// the write is certain unless a substituted word may be an option. More words are
// an argument error. -command, which only Tcl 9 has, binds nothing.
func regsubVarWord(w []Word) (words []Word, certain bool) {
	ops, sure := regexpOperands(w, regsubFlags...)
	if len(ops) != 4 {
		return nil, false
	}
	return ops[3:], sure
}

// literalValue returns the value of a word that undergoes no substitution: a bare
// or quoted word without `$`, `[` or a backslash, or any braced word.
func literalValue(w Word) (string, bool) {
	switch w.Kind {
	case WordBraced:
		if len(w.Text) >= 2 {
			return w.Text[1 : len(w.Text)-1], true
		}
	case WordBare, WordQuoted:
		if strings.ContainsAny(w.Text, "$[\\") {
			return "", false
		}
		if w.Kind == WordQuoted {
			if len(w.Text) < 2 {
				return "", false
			}
			return w.Text[1 : len(w.Text)-1], true
		}
		return w.Text, true
	}
	return "", false
}

// tryHandlerDefs returns the DefLocal bindings a try handler makes when it starts.
func tryHandlerDefs(h tryHandler, base int, ns string, scope int, class string) []Definition {
	var defs []Definition
	for _, p := range tryVarNames(h, base) {
		defs = append(defs, localDef(p, ns, scope, class))
	}
	return defs
}

// localDef is the DefLocal binding of one variable-list name.
func localDef(p paramName, ns string, scope int, class string) Definition {
	return Definition{
		Kind: DefLocal, Name: p.Name, Namespace: ns,
		NameStart: p.Start, NameEnd: p.End, Scope: scope,
		Class: class,
	}
}

// tryVarNames returns the names a try handler's variable list binds: its first two
// elements, the result and then the options; Tcl ignores the rest. When both
// elements are the same variable, Tcl writes the options last, so only the second
// binds; two spellings of one variable, such as `e e(x)`, bind nothing. Only a bare name or a braced list of whitespace-separated names is read.
// Any other list binds nothing here, because its elements are not certain, and
// neither does a qualified name. An array element binds its array, as `set` does.
// The list is split by hand, not with scanParams, which parses an args list as a
// script and keeps every element: here only two elements count, an element can be
// an array reference, and a repeated name binds once. Ranges are absolute: base is
// added.
func tryVarNames(h tryHandler, base int) []paramName {
	if h.vars == nil {
		return nil
	}
	vars := *h.vars
	text, start := vars.Text, base+vars.Start
	switch {
	case vars.Kind == WordBraced && len(text) >= 2:
		text, start = text[1:len(text)-1], start+1
	case vars.Kind == WordBare:
	default:
		return nil
	}
	if strings.ContainsAny(text, "{}[]$\\\";") {
		return nil
	}
	isListSpace := func(b byte) bool { return strings.IndexByte(" \t\n\r\v\f", b) >= 0 }
	var names []paramName
	var elems []string
	for i := 0; i < len(text) && len(elems) < 2; {
		if isListSpace(text[i]) {
			i++
			continue
		}
		j := i
		for j < len(text) && !isListSpace(text[j]) {
			j++
		}
		elem := text[i:j]
		elems = append(elems, elem)
		name := elem
		if p := strings.IndexByte(name, '('); p >= 0 {
			name = name[:p]
		}
		if name != "" && !strings.Contains(name, "::") {
			names = append(names, paramName{Name: name, Start: start + i, End: start + i + len(name)})
		}
		i = j
	}
	if len(names) == 2 && names[0].Name == names[1].Name {
		if elems[0] != elems[1] {
			return nil // one variable spelled two ways: Tcl writes one and fails on the other
		}
		names = names[1:]
	}
	return names
}

// emitVarListNames emits a DefLocal for each plain name in a variable-list word: a
// brace list {a b} yields a and b; a bare word yields itself. Substituted/quoted
// specs are skipped.
func emitVarListNames(vw Word, base int, ns string, scope int, class string, out *[]Definition) {
	if emitEscapedListName(vw, base, ns, scope, class, out) {
		return
	}
	text := vw.Text
	start := base + vw.Start
	if vw.Kind == WordBraced && len(text) >= 2 {
		text = text[1 : len(text)-1]
		start = base + vw.Start + 1
	} else if vw.Kind != WordBare {
		return
	}
	for _, p := range scanParams(text, start, true) {
		*out = append(*out, localDef(p, ns, scope, class))
	}
}

// fallbackNestedNames returns the commands an unqualified `proc NAME` (or a
// decorated one) or `itcl::class NAME` in an `itcl::body` whose class is a
// fallback key may create: NAME in the local class namespace or in the global
// one's. nestedDefName defines neither, so a lookup that reaches one answers
// nothing (index.MaybeCommand).
func fallbackNestedNames(c Command, frame FrameKind, class string, wrapped, inClassNS bool) []string {
	local, global, ok := FallbackClass(class)
	if !ok || frame != FrameProc || !inClassNS || wrapped {
		return nil
	}
	w := c.Words
	var name string
	if (isCmd(w, "proc") || isItclClassCmd(w)) && len(w) >= 2 && isPlainName(w[1]) {
		name = w[1].Text
	} else if nw, _, _, ok := decoratedProcDef(w); ok {
		name = nw.Text
	}
	if name == "" || strings.Contains(name, "::") {
		return nil
	}
	return []string{qualifyName(name, local), qualifyName(name, global)}
}

// nestedDefName returns the FQ name a `proc NAME` or `itcl::class NAME` command
// defines, or false when it is not certain. An unqualified one defined in a method,
// constructor, destructor, class proc or itcl::body is created in the class
// namespace (::ns::K::inner), not the one that lexically holds the class. In a
// wrapper's body there it is created wherever the wrapper runs it (`after`: ::,
// `uplevel 1`: the caller's namespace), so it defines nothing (tclsh 8.6 and 9.0,
// Itcl 4.3; research/experiments/08_itcl_class_body.tcl and
// 08_itcl_inner_class.tcl). Neither does one in an `itcl::body` whose class is a
// fallback key (classRefKey): which class namespace it names is a workspace fact.
func nestedDefName(name, ns string, frame FrameKind, class string, wrapped, inClassNS bool) (string, bool) {
	if frame != FrameProc || !inClassNS || class == "" || strings.Contains(name, "::") {
		return qualifyName(name, ns), true
	}
	if wrapped {
		return "", false
	}
	if _, _, ok := FallbackClass(class); ok {
		return "", false // the class namespace is a workspace fact, not known here
	}
	return qualifyName(name, class), true
}

// qualifyName resolves a command/variable name against the current namespace:
// a leading "::" is absolute, otherwise it is qualified into current.
func qualifyName(name, current string) string {
	if strings.HasPrefix(name, "::") {
		return name
	}
	if current == "::" {
		return "::" + name
	}
	return current + "::" + name
}
