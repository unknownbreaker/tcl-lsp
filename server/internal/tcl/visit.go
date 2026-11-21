package tcl

import "strings"

// The command visitor serves the Checks (package check). It rides the walk that
// indexes a file, so an open document is parsed once per change for both. It sees
// only commands certain to run as Tcl: the top level, the bodies of proc,
// namespace eval, Itcl members and itcl::body, the bodies of the control-flow
// builtins whose scripts run where they stand (bodiesRunHere), and the [scripts]
// substituted into any of those commands' words. A custom command's braced word
// may be data or deferred code, so neither it nor anything inside it is visited,
// and an Itcl class body's own commands declare members rather than run. Each
// visited command comes with the builtins whose bodies the walk trusted to reach
// it, since a redefined `foreach` makes its body data (Visit.Command).

// Visit configures the command visitor.
type Visit struct {
	// Command receives one command certain to run as Tcl. base is the absolute
	// offset of the text the command's word offsets are relative to. assumes
	// names the builtins whose bodies the walk entered to reach it, by their last
	// name segment, outermost first: the command runs only if none of them is
	// redefined. The slice is shared; copy it to keep it.
	Command func(c Command, base int, assumes []string)
	// Data, when set, reports a command that is not run although it sits in a
	// body that is: its words are arguments to another command (a `.rvt`
	// `<?= ?>` block's first command, which Rivet runs as `puts -nonewline`).
	// Its [scripts] are still substituted, so they are visited; it and its
	// bodies are not.
	Data func(c Command, base int) bool
}

// VisitCommands calls v for every command of src certain to run as Tcl.
func VisitCommands(src string, v Visit) {
	walkFile(src, collectors{visit: &v})
}

// FileAllVisit is FileAll that also calls v for every command of src certain to
// run as Tcl, from the same parse. A nil v visits nothing.
func FileAllVisit(src string, v *Visit) FileIndex {
	fi := FileIndex{
		Namespaces: map[string]*NamespaceInfo{},
		Classes:    map[string][]string{},
	}
	walkFile(src, collectors{
		defs:    &fi.Defs,
		refs:    &fi.Refs,
		ns:      fi.Namespaces,
		classes: fi.Classes,
		maybe:   &fi.MaybeCommands,
		created: &fi.Created,
		visit:   v,
	})
	fi.Defs = promoteLinkedAssignments(fi.Defs)
	return fi
}

// visitCommand visits cmd, a command of a body the visitor trusts, and its
// substituted scripts, and returns the visitor and assumed builtins its own bodies
// get: none unless they certainly run as scripts. last says cmd is its script's
// last command, the only one whose last word may be unterminated. An unterminated
// word means Tcl rejects the command, so neither it nor anything in it runs.
func visitCommand(cmd Command, base int, last bool, ns string, frame FrameKind, scope int, class string, wrapped, inClassNS bool, c collectors) (*Visit, []string) {
	if last && !wordComplete(cmd.Words[len(cmd.Words)-1]) {
		return nil, nil
	}
	if frame == FrameClass {
		// An Itcl class body declares members; its commands are not Tcl
		// commands, but its member bodies are scripts.
		if runsAsScript(cmd, frame) {
			return c.visit, assume(c.assumes, cmd)
		}
		return nil, nil
	}
	data := c.visit.Data != nil && c.visit.Data(cmd, base)
	if !data {
		c.visit.Command(cmd, base, c.assumes)
	}
	visitSubst(cmd, base, ns, frame, scope, class, wrapped, inClassNS, c)
	if data || !runsAsScript(cmd, frame) {
		return nil, nil
	}
	return c.visit, assume(c.assumes, cmd)
}

// assume returns assumes plus the builtins cmd's head names: its last name
// segment, and the member keyword after an Itcl access modifier.
func assume(assumes []string, cmd Command) []string {
	out := append(assumes[:len(assumes):len(assumes)], NameTail(cmd.Words[0].Text))
	if m := memberWords(cmd.Words); len(m) < len(cmd.Words) {
		out = append(out, m[0].Text)
	}
	return out
}

// runsAsScript reports whether the bodies childBodies finds for cmd, a command in
// frame, are scripts Tcl runs, when every builtin cmd names means what Tcl says:
// a proc, a namespace eval of one script, an Itcl class, member or itcl::body,
// each with exactly its words, or a builtin whose scripts run here. A decorated
// proc's body is a script only if its macro defines one, which nothing here can
// know, and `namespace eval ns a b` joins its words into a script of its own.
func runsAsScript(cmd Command, frame FrameKind) bool {
	w := cmd.Words
	if frame == FrameClass {
		w = memberWords(w)
		switch {
		case isCmd(w, "method"), isCmd(w, "proc"):
			return len(w) == 4
		case isCmd(w, "constructor"):
			return len(w) == 3 || len(w) == 4
		case isCmd(w, "destructor"):
			return len(w) == 2
		}
	}
	switch {
	case isCmd(w, "namespace") && len(w) >= 2 && w[1].Text == "eval":
		return len(w) == 4
	case isCmd(w, "proc"), isCmd(w, "itcl::body"), isCmd(w, "::itcl::body"):
		return len(w) == 4
	case isItclClassCmd(w):
		return len(w) == 3
	}
	return bodiesRunHere(cmd) && controlArity(w)
}

// controlArity reports whether w, a builtin whose scripts run here
// (bodiesRunHere), has words Tcl accepts. Tcl rejects any other shape with
// "wrong # args" before a body runs (tclsh 8.6 and 9.0, testdata/tclsh/arity.tcl
// in package checkcmd). An expanded word hides the count, so it fails. timerate's
// options are not read, so it fails too.
func controlArity(w []Word) bool {
	if hasExpansion(w) {
		return false
	}
	n := len(w)
	switch w[0].Text {
	case "if":
		return ifArity(w)
	case "while":
		return n == 3
	case "for":
		return n == 5
	case "foreach", "lmap":
		return n >= 4 && n%2 == 0
	case "catch":
		return n >= 2 && n <= 4
	case "try":
		return tryArity(w)
	case "switch":
		return switchArity(w)
	case "time":
		return n == 2 || n == 3
	case "dict":
		switch keyword(w[1]) {
		case "for", "map":
			return n == 5
		case "with":
			return n >= 4
		case "update":
			return isDictUpdate(w)
		}
		return false
	case "eval", "uplevel":
		return true // conservativeHead and upLevelHere admit one exact shape each
	}
	return false
}

// keyword returns a word's literal value, or "" when it has none.
func keyword(w Word) string {
	v, _ := literalValue(w)
	return v
}

// ifArity reports whether an if command's words are `if cond ?then? body
// (elseif cond ?then? body)* ?else? ?body?`, with keywords read by value.
func ifArity(w []Word) bool {
	i := 1
	for {
		if i >= len(w) {
			return false // no condition
		}
		i++
		if i < len(w) && keyword(w[i]) == "then" {
			i++
		}
		if i >= len(w) {
			return false // no body
		}
		i++
		if i == len(w) {
			return true
		}
		switch keyword(w[i]) {
		case "elseif":
			i++
			continue
		case "else":
			i++
		}
		return i == len(w)-1 // exactly one else body
	}
}

// tryArity reports whether a try command's words are `try body (on|trap code
// varList body)* ?finally body?`, with the last handler's body not `-`.
func tryArity(w []Word) bool {
	lastBody := -1
	for i := 2; i < len(w); {
		switch keyword(w[i]) {
		case "on", "trap":
			if i+3 >= len(w) {
				return false
			}
			lastBody = i + 3
			i += 4
		case "finally":
			return i+2 == len(w)
		default:
			return false
		}
	}
	return lastBody < 0 || !isFallThrough(w[lastBody])
}

// switchArity reports whether a switch command's arms, inline or as one list,
// pair every pattern with a body and end with a body other than `-`.
func switchArity(w []Word) bool {
	arms := switchArgs(w)
	if len(arms) == 1 {
		if arms[0].Kind != WordBraced {
			return false
		}
		elems, ok := switchArmListElements(arms[0])
		if !ok {
			return false
		}
		arms = elems
	}
	return len(arms) >= 2 && len(arms)%2 == 0 && !isFallThrough(arms[len(arms)-1])
}

// visitSubst visits the commands of every [script] substituted into cmd's words,
// and everything those commands run, with the visit collector alone: the other
// collectors read substitutions themselves (commandRefs). A bare or quoted word
// is substituted whatever cmd is, so its scripts assume only what cmd itself
// does. A braced expression word is substituted by cmd when it evaluates it, so
// its scripts also assume cmd's head, and an if, while or for that Tcl rejects
// for its word count evaluates nothing.
func visitSubst(cmd Command, base int, ns string, frame FrameKind, scope int, class string, wrapped, inClassNS bool, c collectors) {
	if !anyBracket(cmd.Words) {
		return // no word holds a [script]: the common case, kept free of allocation
	}
	v := *c.visit
	v.Data = nil // a substituted script's commands all run
	words := collectors{visit: &v, assumes: c.assumes, ends: c.ends}
	exprs := collectors{visit: &v, assumes: assume(c.assumes, cmd), ends: c.ends}
	walk := func(sub collectors) scriptFunc {
		return func(inner string, innerBase int, _ bool) {
			walkAll(parseWith(inner, c.ends), innerBase, ns, frame, scope, class, wrapped, inClassNS, sub)
		}
	}
	onWord, onExpr := walk(words), walk(exprs)
	evaluates := !isControlName(cmd.Words[0].Text) || controlArity(cmd.Words)
	exprWords := exprWordStarts(cmd)
	for idx, w := range cmd.Words {
		onScript := onWord
		if w.Kind == WordBraced {
			if !evaluates {
				continue
			}
			onScript = onExpr
		}
		walkWordSubst(cmd, exprWords, idx, base, flowAlways, c.ends, func(VarRef) {}, onScript)
	}
}

// anyBracket reports whether any of w holds a `[`, which every substituted script
// starts with.
func anyBracket(w []Word) bool {
	for _, x := range w {
		if strings.IndexByte(x.Text, '[') >= 0 {
			return true
		}
	}
	return false
}

// wordComplete reports whether w ends where Tcl ends it rather than where the
// text ran out: a braced word at its matching close brace, a quoted word at its
// closing quote, and every [script] in a bare or quoted word at its `]`. The
// scanner reads an unterminated word to the end of its script, so only a
// script's last word can fail this.
func wordComplete(w Word) bool {
	t := w.Text
	if strings.HasPrefix(t, "{*}") && len(t) > 3 {
		return wordComplete(wordFromToken(Token{Kind: KindWord, Text: t[3:]}))
	}
	switch w.Kind {
	case WordBraced:
		return bracesClose(t)
	case WordQuoted:
		if len(t) < 2 || t[len(t)-1] != '"' || trailingBackslashes(t[:len(t)-1])%2 == 1 {
			return false
		}
	}
	return walkSubst(t, 0, false, flowAlways, &scriptEnds{}, func(VarRef) {}, func(string, int, bool) {}) != flowDistrusted
}

// bracesClose reports whether the brace opening t closes at t's last byte,
// counting nesting and skipping backslash-escaped bytes as Tcl does.
func bracesClose(t string) bool {
	depth := 0
	for i := 0; i < len(t); i++ {
		switch t[i] {
		case '\\':
			i++
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i == len(t)-1
			}
		}
	}
	return false
}

// trailingBackslashes counts the backslashes that end s.
func trailingBackslashes(s string) int {
	n := 0
	for n < len(s) && s[len(s)-1-n] == '\\' {
		n++
	}
	return n
}

// NameTail returns a command name's last segment: `::a::b` and `a::b` give `b`.
func NameTail(name string) string {
	if i := strings.LastIndex(name, "::"); i >= 0 {
		return name[i+2:]
	}
	return name
}

// collectCreated appends the tails of the command names cmd, run in namespace
// ns, may create, and those of the commands in its substituted scripts that name
// a creating command.
func collectCreated(cmd Command, base int, ns string, ends *scriptEnds, out *[]string) {
	*out = append(*out, createdNames(cmd.Words, ns)...)
	for _, w := range cmd.Words {
		if w.Kind != WordBraced && strings.IndexByte(w.Text, '[') >= 0 && mayNameCreator(w.Text) {
			sub := collectors{created: out, ends: ends}
			walkSubst(w.Text, base+w.Start, false, flowAlways, ends, func(VarRef) {}, func(inner string, innerBase int, _ bool) {
				walkAll(parseWith(inner, ends), innerBase, ns, FrameNamespace, 0, "", false, false, sub)
			})
		}
	}
}

// creators are the words that may start or select a command-creating command.
var creators = []string{"proc", "rename", "alias", "create", "ensemble"}

func mayNameCreator(text string) bool {
	for _, k := range creators {
		if strings.Contains(text, k) {
			return true
		}
	}
	return false
}

// createdNames returns the tails of the literal command names w may create,
// redefine or rename away: the word after any `proc` word (a plain, member or
// decorated proc), both names of `rename OLD NEW`, the alias of `interp alias
// PATH NAME ...`, the object of `CMD create NAME` (TclOO and Itcl), and the
// ensemble of `namespace ensemble configure NAME ...` (its -map may send a
// subcommand to user code) or `namespace ensemble create ?-command NAME?`, which
// with no -command is named after ns, the namespace it runs in. A name braced,
// quoted or bare counts; one with a substitution or a backslash is unknown and
// gives nothing. Reading too many words only silences more.
func createdNames(w []Word, ns string) []string {
	var out []string
	val := func(x Word) string {
		v, _ := literalValue(x)
		return v
	}
	add := func(name Word) {
		if v := val(name); v != "" {
			out = append(out, NameTail(v))
		}
	}
	for k := 0; k+1 < len(w); k++ {
		if NameTail(val(w[k])) == "proc" {
			add(w[k+1])
		}
	}
	switch head := NameTail(val(w[0])); {
	case head == "rename" && len(w) == 3:
		add(w[1])
		add(w[2])
	case head == "interp" && len(w) >= 4 && val(w[1]) == "alias":
		add(w[3])
	case head == "namespace" && len(w) >= 3 && val(w[1]) == "ensemble":
		switch val(w[2]) {
		case "configure":
			if len(w) >= 4 {
				add(w[3])
			}
		case "create":
			named := false
			for k := 3; k+1 < len(w); k++ {
				if val(w[k]) == "-command" {
					add(w[k+1])
					named = true
				}
			}
			if t := NameTail(ns); !named && t != "" {
				out = append(out, t)
			}
		}
	}
	if len(w) >= 3 && val(w[1]) == "create" {
		add(w[2])
	}
	return out
}
