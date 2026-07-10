package tcl

import "strings"

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
	// Class is the fully-qualified itcl class name this definition belongs to,
	// or "" when not inside a class body.
	Class string
}

// FileDefs parses src and returns the definitions it declares, recursing into
// namespace eval and proc bodies.
func FileDefs(src string) []Definition {
	var out []Definition
	walkAll(Parse(src), 0, "::", FrameNamespace, 0, "", collectors{defs: &out})
	return promoteLinkedAssignments(out)
}

func emitDefs(c Command, base int, ns string, frame FrameKind, scope int, class string, out *[]Definition) {
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
		name := qualifyName(w[1].Text, ns)
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
	// A proc defined through a decorator macro (`CACHE_PROC proc name args body`).
	if name, _, _, ok := decoratedProcDef(w); ok {
		*out = append(*out, Definition{
			Kind:      DefProc,
			Name:      qualifyName(name.Text, ns),
			Namespace: ns,
			NameStart: base + name.Start,
			NameEnd:   base + name.End,
			FullStart: cmdStart,
			FullEnd:   cmdEnd,
			Scope:     scope,
			Class:     class,
		})
	}
	if isCmd(w, "variable") && len(w) >= 2 && isPlainName(w[1]) {
		*out = append(*out, Definition{
			Kind:      DefNamespaceVar,
			Name:      qualifyName(w[1].Text, ns),
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
				Kind: DefLocal, Name: w[1].Text, Namespace: ns,
				NameStart: base + w[1].Start, NameEnd: base + w[1].End, Scope: scope,
				Origin: qualifyName(w[1].Text, ns),
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
	if isCmd(w, "global") && frame == FrameProc {
		for _, gw := range w[1:] {
			if isPlainName(gw) {
				*out = append(*out, Definition{
					Kind: DefGlobalLink, Name: gw.Text, Namespace: ns,
					NameStart: base + gw.Start, NameEnd: base + gw.End, Scope: scope,
					Origin: globalOrigin(gw.Text),
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
			if isPlainName(alias) {
				*out = append(*out, Definition{
					Kind: DefLocal, Name: alias.Text, Namespace: ns,
					NameStart: base + alias.Start, NameEnd: base + alias.End, Scope: scope,
					Origin: upvarOrigin(level, args[i-1]),
					Class:  class,
				})
			}
		}
	}
	// w[0]=itcl::class  w[1]=ClassName  w[2]=class body (required, hence len >= 3)
	if (isCmd(w, "itcl::class") || isCmd(w, "::itcl::class")) && len(w) >= 3 && isPlainName(w[1]) {
		*out = append(*out, Definition{
			Kind:      DefClass,
			Name:      qualifyName(w[1].Text, ns),
			Namespace: ns,
			NameStart: base + w[1].Start,
			NameEnd:   base + w[1].End,
			FullStart: cmdStart,
			FullEnd:   cmdEnd,
			Scope:     scope,
			Class:     class,
		})
	}
	// w[0]=itcl::body  w[1]=::Class::method  w[2]=args  w[3]=body
	// External method body definitions: `itcl::body ::C::m {args} {body}`.
	// Split the qualified name on the last :: to get the class and method name.
	if (isCmd(w, "itcl::body") || isCmd(w, "::itcl::body")) && len(w) >= 2 && isPlainName(w[1]) {
		full := w[1].Text
		if i := strings.LastIndex(full, "::"); i > 0 {
			classFQ := qualifyName(full[:i], ns)
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

// emitVarAssign records a variable-assignment target (`set`, `array set`,
// `incr`/`append`/`lappend`). At namespace frame -- or for a QUALIFIED name in
// any frame -- it is a namespace-variable definition: `set ::x 1` inside a proc
// writes the global directly in TCL, so indexing it as a proc-local both hid
// the workspace definition and blocked cross-file `$::x` resolution. A bare
// name at proc frame stays a proc-local binding.
func emitVarAssign(name string, s, e, base int, ns string, frame FrameKind, scope int, class string, cmdStart, cmdEnd int, out *[]Definition) {
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
// ok is false when the target is not a usable bare name (empty, a leading `(`, or
// a base containing a substitution). The index after `(` may be anything.
func arrayBaseName(w Word) (name string, start, end int, ok bool) {
	if w.Kind != WordBare || w.Text == "" {
		return "", 0, 0, false
	}
	p := strings.IndexByte(w.Text, '(')
	if p < 0 {
		if !isPlainName(w) {
			return "", 0, 0, false
		}
		return w.Text, w.Start, w.End, true
	}
	baseText := w.Text[:p]
	if baseText == "" {
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
	inner, innerBase := argsWord, base
	text := inner.Text
	start := innerBase + inner.Start
	if inner.Kind == WordBraced && len(text) >= 2 {
		text = text[1 : len(text)-1]
		start = innerBase + inner.Start + 1
	}
	for _, p := range scanParams(text, start) {
		*out = append(*out, Definition{
			Kind: DefLocal, Name: p.Name, Namespace: ns,
			NameStart: p.Start, NameEnd: p.End, Scope: scope,
			Class: class,
		})
	}
}

type paramName struct {
	Name       string
	Start, End int
}

func scanParams(text string, base int) []paramName {
	var ps []paramName
	cmds := Parse(text)
	for _, c := range cmds {
		for _, word := range c.Words {
			name, s, e := paramFromWord(word, base)
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
func paramFromWord(w Word, base int) (string, int, int) {
	if w.Kind == WordBraced && len(w.Text) >= 2 {
		inner := w.Text[1 : len(w.Text)-1]
		innerBase := base + w.Start + 1
		for _, c := range Parse(inner) {
			if len(c.Words) > 0 {
				fw := c.Words[0]
				return fw.Text, innerBase + fw.Start, innerBase + fw.End
			}
		}
		return "", 0, 0
	}
	if w.Kind == WordBare && w.Text != "" {
		return w.Text, base + w.Start, base + w.End
	}
	return "", 0, 0
}

// emitLoopVarDefs emits DefLocal bindings for loop/destructuring target variables
// that introduce proc-locals: foreach/lmap var lists, lassign targets, and
// dict for/map key lists. Called only in FrameProc.
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
		// lassign list var ?var ...? -- targets are w[2:].
		for _, vw := range w[2:] {
			emitVarListNames(vw, base, ns, scope, class, out)
		}
	case "dict":
		// dict for {k v} dict body ; dict map {k v} dict body.
		if len(w) >= 5 && w[1].Kind == WordBare && (w[1].Text == "for" || w[1].Text == "map") {
			emitVarListNames(w[2], base, ns, scope, class, out)
		}
	}
}

// emitVarListNames emits a DefLocal for each plain name in a variable-list word: a
// brace list {a b} yields a and b; a bare word yields itself. Substituted/quoted
// specs are skipped.
func emitVarListNames(vw Word, base int, ns string, scope int, class string, out *[]Definition) {
	text := vw.Text
	start := base + vw.Start
	if vw.Kind == WordBraced && len(text) >= 2 {
		text = text[1 : len(text)-1]
		start = base + vw.Start + 1
	} else if vw.Kind != WordBare {
		return
	}
	for _, p := range scanParams(text, start) {
		*out = append(*out, Definition{
			Kind: DefLocal, Name: p.Name, Namespace: ns,
			NameStart: p.Start, NameEnd: p.End, Scope: scope,
			Class: class,
		})
	}
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
