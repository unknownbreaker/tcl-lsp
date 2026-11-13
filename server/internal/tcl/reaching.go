package tcl

// ReachingAt returns the local-variable bindings that may reach the use at byte
// offset useOff in src, as Definition values (Kind DefLocal/DefGlobalLink, with
// NameStart/NameEnd on the binding's name token and Origin for links). ok is false
// when the analyzer has no answer: the scope is oversized or useOff is not on a use
// it visited. ok with no defs means the analyzer visited the use and nothing reaches
// it. When nothing else reaches a visited use, a binding above it in an unknown
// command's body answers, because that body may or may not run here. Intraprocedural
// and may-reach. Also supports namespace-level variables (used by RVT templates).
func ReachingAt(src string, useOff int) (defs []Definition, ok bool) {
	inner, innerBase, argsWord, argsBase, ns, frame, found := enclosingProcOrScope(Parse(src), 0, "::", FrameNamespace, 0, useOff)
	if !found {
		return nil, false
	}
	const maxReachingBytes = 200000
	if len(inner) > maxReachingBytes {
		return nil, false // oversized: caller falls back to first-binding
	}
	a := &analyzer{useOff: useOff, ns: ns, frame: frame, skipped: reachSet{}}
	entry := reachSet{}
	// Seed entry set with proc params (namespace-level code has no params).
	if frame == FrameProc {
		var paramDefs []Definition
		emitProcParams(argsWord, argsBase, "::", innerBase, "", &paramDefs)
		for _, d := range paramDefs {
			entry[d.Name] = []Definition{d}
		}
	}
	a.seq(Parse(inner), innerBase, entry) //nolint:errcheck — result unused; a.found/a.answer carry the answer
	if !a.found {
		return nil, false
	}
	if len(a.answer) == 0 {
		return a.skippedAboveUse(), true
	}
	return a.answer, true
}

// skippedAboveUse returns the skipped bindings of the used name that start before
// the use.
func (a *analyzer) skippedAboveUse() []Definition {
	var above []Definition
	for _, d := range a.skipped[a.useName] {
		if d.NameStart < a.useOff {
			above = append(above, d)
		}
	}
	return above
}

// enclosingProcOrScope returns the interior text and absolute base of the innermost
// scope (proc, namespace or unknown command's body) containing useOff, plus the
// proc's args Word and base for interpreting it (empty/zero for the others). Also
// returns the scope's namespace and frame kind (FrameProc, FrameNamespace or
// FrameOpaque).
func enclosingProcOrScope(cmds []Command, base int, ns string, frame FrameKind, scope, useOff int) (inner string, innerBase int, argsWord Word, argsBase int, scopeNS string, frameKind FrameKind, found bool) {
	for _, c := range cmds {
		for _, b := range childBodies(c, base, ns, frame, scope, "") {
			if useOff < b.Base || useOff >= b.Base+len(b.Inner) {
				continue
			}
			// Try to find a deeper (more nested) scope first.
			if in2, base2, aw2, ab2, ns2, fk2, ok2 := enclosingProcOrScope(Parse(b.Inner), b.Base, b.NS, b.Frame, b.Scope, useOff); ok2 {
				return in2, base2, aw2, ab2, ns2, fk2, true
			}
			// This body is the innermost one containing useOff. Accept it if it's
			// a scope-introducing frame (proc or namespace), or a body of unknown frame,
			// so `$o m` stays typed inside a wrapper.
			if b.Frame == FrameProc && b.Scope == b.Base {
				// Recover the args word from the proc command.
				aw, ab := procArgsWord(c, base, frame)
				return b.Inner, b.Base, aw, ab, b.NS, FrameProc, true
			}
			if (b.Frame == FrameNamespace || b.Frame == FrameOpaque) && b.Scope == 0 {
				return b.Inner, b.Base, Word{}, base, b.NS, b.Frame, true
			}
		}
	}
	return "", 0, Word{}, 0, "::", FrameNamespace, false
}

// procArgsWord returns the args Word and base for the proc command c (a decorated
// proc, or an Itcl method, constructor or itcl::body), where frame is the frame c
// sits in. Returns a zero Word when the command has no parameters.
func procArgsWord(c Command, base int, frame FrameKind) (Word, int) {
	w := c.Words
	if isCmd(w, "proc") && len(w) >= 4 {
		return w[2], base
	}
	if _, args, _, ok := decoratedProcDef(w); ok {
		return args, base
	}
	if args, ok := methodArgs(w, frame); ok {
		return args, base
	}
	return Word{}, base
}

// reachSet maps a local variable name to the bindings that may currently reach
// this program point.
type reachSet map[string][]Definition

func (s reachSet) clone() reachSet {
	out := make(reachSet, len(s))
	for k, v := range s {
		cp := make([]Definition, len(v))
		copy(cp, v)
		out[k] = cp
	}
	return out
}

type frameAcc struct{ brk, cont reachSet }

type analyzer struct {
	useOff    int
	useName   string // the variable named at useOff, once found
	answer    []Definition
	found     bool
	loopStack []*frameAcc
	ret       reachSet
	// skipped holds bindings in the bodies of commands the analyzer passes over. Any
	// command may hold data or deferred code, so they never enter a reach set outside
	// their own body and never kill anything. They answer only a use nothing else
	// reaches.
	skipped reachSet
	// Enclosing scope of the whole analyzed body, threaded into localBindings so
	// `variable NAME` links carry the qualified Origin (proc frames only) --
	// mirroring emitDefs. See the localBindings doc comment.
	ns    string
	frame FrameKind
}

// seq threads the reaching set left-to-right through a command sequence.
// Returns (set at end, live). live is false when a non-local exit (return,
// break, continue) terminates the sequence early; callers treat a non-live
// path as contributing nothing to joins.
func (a *analyzer) seq(cmds []Command, base int, in reachSet) (reachSet, bool) {
	cur := in
	for _, c := range cmds {
		var live bool
		cur, live = a.command(c, base, cur)
		if !live {
			return cur, false
		}
	}
	return cur, true
}

const maxLoopIters = 200

// loopHead reports whether command c is a loop-introducing command
// (while, for, foreach, lmap, dict for, dict map).
func loopHead(c Command) bool {
	w := c.Words
	if len(w) == 0 || w[0].Kind != WordBare {
		return false
	}
	switch w[0].Text {
	case "while", "for", "foreach", "lmap":
		return true
	case "dict":
		return len(w) >= 2 && w[1].Kind == WordBare && (w[1].Text == "for" || w[1].Text == "map")
	}
	return false
}

// loopScripts splits a loop's scripts by role: `for start test next body` has all
// three, and other loops have only a body. scriptBodies cannot answer this, because
// it returns `for`'s scripts as start, next, body without saying which is which.
func loopScripts(w []Word) (start, body, next *Word) {
	if !isCmd(w, "for") {
		if bodies := scriptBodies(w); len(bodies) > 0 {
			body = &bodies[0]
		}
		return nil, body, nil
	}
	script := func(i int) *Word {
		if i < len(w) && w[i].Kind == WordBraced {
			return &w[i]
		}
		return nil
	}
	return script(1), script(4), script(3)
}

// loopTest returns the index of the braced test word of a while or for command, or
// -1. Its [substitutions] run at the loop header on every pass. Tcl substitutes every
// other word of a command once, before the command runs, including an unbraced test.
func loopTest(c Command) int {
	if !loopHead(c) {
		return -1
	}
	tests := exprBodies(c.Words)
	if len(tests) == 0 {
		return -1
	}
	for i, w := range c.Words {
		if w.Start == tests[0].Start {
			return i
		}
	}
	return -1
}

// runScript threads in through an optional braced script. A nil script does
// nothing and is live. A dead path contributes nothing to a later join.
func (a *analyzer) runScript(script *Word, base int, in reachSet) (reachSet, bool) {
	if script == nil {
		return in, true
	}
	inner, innerBase := bracedInner(*script, base)
	out, live := a.seq(Parse(inner), innerBase, in)
	if !live {
		return reachSet{}, false
	}
	return out, true
}

// analyzeLoop iterates the loop to a fixpoint so a value assigned in one iteration
// reaches later iterations' uses, then joins with the entry set (the loop may run
// zero times). `for` runs its start script once, before the fixpoint; each pass then
// evaluates the test, runs the body, then next, so `continue` still runs next. Loop
// variables (foreach/lmap/dict-for) are rebound at the top of every pass, so a
// reassignment below a use does not reach that use on the next iteration.
// break/continue exits are accumulated in a per-loop frameAcc pushed onto loopStack.
func (a *analyzer) analyzeLoop(c Command, base int, in reachSet) (reachSet, bool) {
	acc := &frameAcc{brk: reachSet{}, cont: reachSet{}}
	a.loopStack = append(a.loopStack, acc)
	defer func() { a.loopStack = a.loopStack[:len(a.loopStack)-1] }()

	start, body, next := loopScripts(c.Words)
	test := loopTest(c)
	loopVars := localBindings(c, base, a.ns, a.frame)
	cur, live := a.runScript(start, base, in.clone())
	if !live {
		return cur, false
	}
	// A test script that ends the path is rare; the loop treats it as live, which
	// only adds bindings.
	exprWords := exprWordStarts(c)
	afterTest := func(header reachSet) reachSet {
		if test < 0 {
			return header
		}
		live := true
		out, _ := a.substituteWord(c, exprWords, test, base, header, false, &live)
		return out
	}
	for iter := 0; iter < maxLoopIters; iter++ {
		passEntry := afterTest(cur).clone()
		for _, d := range loopVars {
			passEntry[d.Name] = []Definition{d}
		}
		normalEnd, _ := a.runScript(body, base, passEntry)
		backEdge, _ := a.runScript(next, base, joinAll([]reachSet{normalEnd, acc.cont}))
		merged := joinAll([]reachSet{cur, backEdge})
		if reachEqual(merged, cur) {
			break
		}
		cur = merged
	}
	// cur is the fixpoint set at the loop header: it subsumes the entry set and
	// every back-edge contribution, so after the test it covers every path that
	// may reach post-loop. acc.brk adds paths that exited via `break`.
	return joinAll([]reachSet{afterTest(cur), acc.brk}), true
}

// reachEqual reports whether two reachSets contain the same bindings (by
// variable name and NameStart/NameEnd identity).
func reachEqual(a, b reachSet) bool {
	if len(a) != len(b) {
		return false
	}
	for name, da := range a {
		db, ok := b[name]
		if !ok || len(da) != len(db) {
			return false
		}
		seen := map[[2]int]bool{}
		for _, d := range da {
			seen[[2]int{d.NameStart, d.NameEnd}] = true
		}
		for _, d := range db {
			if !seen[[2]int{d.NameStart, d.NameEnd}] {
				return false
			}
		}
	}
	return true
}

// conservativeHead reports whether command c requires conservative may-reach
// modeling: its scripts run in this frame, and they are alternatives or may abort, so
// precise exception modeling is deferred. `eval` counts only in its one-script form.
// With more words it concatenates them, and the resulting head decides what happens.
func conservativeHead(c Command) bool {
	w := c.Words
	if isCmd(w, "eval") {
		return len(w) == 2
	}
	return isCmd(w, "catch") || isCmd(w, "try") || isCmd(w, "switch") || isCmd(w, "time") || isCmd(w, "timerate")
}

// bodiesRunHere reports whether the scripts of command c are known to run in this
// frame. Any other command's braced word may be data or deferred code, and `uplevel`
// runs its script in another frame. childBodies asks this too, so indexed definitions
// and reach sets agree on which bodies run here.
func bodiesRunHere(c Command) bool {
	return isCmd(c.Words, "if") || conservativeHead(c) || loopHead(c)
}

// analyzedScope marks a body as still in the analyzed scope when asking childBodies
// where a body runs: a body that starts a new scope reports a different Scope.
const analyzedScope = -1

// declaresMember reports whether c declares an OO member. Its body runs when the
// member is called, not here, even though childBodies sees an ordinary trailing
// script outside an Itcl class body. The keyword may have words before it:
// `self method m {} {...}`, `oo::define C method m {} {...}`. In a command whose
// scripts are known to run here, those words are patterns or variable names.
func declaresMember(c Command) bool {
	w := c.Words
	if bodiesRunHere(c) {
		return false
	}
	if isCmd(w, "method") || isCmd(w, "constructor") || isCmd(w, "destructor") {
		return true
	}
	wordBeforeLast := func(count int, keyword string) bool {
		i := len(w) - count
		return i >= 0 && w[i].Kind == WordBare && w[i].Text == keyword
	}
	return wordBeforeLast(4, "method") || wordBeforeLast(3, "constructor") || wordBeforeLast(2, "destructor")
}

// sameScopeBodies returns the child bodies of c that are not a new scope. Bodies
// that start one (proc, decorated proc, namespace eval, itcl::class, itcl::body, OO
// members) do not run here, so their bindings cannot reach.
func (a *analyzer) sameScopeBodies(c Command, base int) []bodyScope {
	if declaresMember(c) {
		return nil
	}
	var bodies []bodyScope
	for _, b := range childBodies(c, base, a.ns, a.frame, analyzedScope, "") {
		if b.Scope == analyzedScope {
			bodies = append(bodies, b)
		}
	}
	return bodies
}

// conservativeBody is one body of a conservative construct with the set it sees on
// entry.
type conservativeBody struct {
	script Word
	entry  reachSet
}

// bindingsIn unions every binding made in the braced script by code known to run
// in this frame.
func (a *analyzer) bindingsIn(script Word, base int) reachSet {
	gens := reachSet{}
	inner, innerBase := bracedInner(script, base)
	a.collectBindings(Parse(inner), innerBase, gens, bodiesRunHere)
	return gens
}

// tryEntries gives each part of a try command its entry view: the main body sees
// what precedes the try; a handler also sees the main body's bindings, because an
// error can arrive at any point in it, but never a sibling handler's; finally sees
// the bindings of the main body and every handler, but not its own, which reach only
// through a loop's back edge.
func (a *analyzer) tryEntries(w []Word, base int, in reachSet) []conservativeBody {
	main, handlers, finally := tryParts(w)
	var bodies []conservativeBody
	handlerEntry := in
	if main != nil {
		bodies = append(bodies, conservativeBody{*main, in})
		handlerEntry = joinAll([]reachSet{in, a.bindingsIn(*main, base)})
	}
	finallyEntry := handlerEntry
	for _, handler := range handlers {
		bodies = append(bodies, conservativeBody{handler, handlerEntry})
		finallyEntry = joinAll([]reachSet{finallyEntry, a.bindingsIn(handler, base)})
	}
	if finally != nil {
		bodies = append(bodies, conservativeBody{*finally, finallyEntry})
	}
	return bodies
}

// analyzeConservative over-approximates the exit: the bodies may or may not run and
// may exit at any point, so the result joins the entry set with every binding
// generated anywhere in them. Entry is precise: a catch body or a switch arm sees
// only what precedes the command, and a try's parts see what tryEntries gives them.
// So a binding below a use, or in a sibling arm, reaches it only through a loop's
// back edge. Names in a try handler's variable list are not modeled as bindings.
func (a *analyzer) analyzeConservative(c Command, base int, in reachSet) (reachSet, bool) {
	bodyScripts := scriptBodies(c.Words)
	gens := reachSet{}
	for _, script := range bodyScripts {
		inner, innerBase := bracedInner(script, base)
		a.collectBindings(Parse(inner), innerBase, gens, bodiesRunHere)
	}
	var bodies []conservativeBody
	if isCmd(c.Words, "try") {
		bodies = a.tryEntries(c.Words, base, in)
	} else {
		for _, script := range bodyScripts {
			bodies = append(bodies, conservativeBody{script, in})
		}
	}
	for _, body := range bodies {
		a.runScript(&body.script, base, body.entry.clone())
	}
	out := joinAll([]reachSet{in, gens})
	for _, d := range localBindings(c, base, a.ns, a.frame) {
		out[d.Name] = appendDedup(out[d.Name], d)
	}
	return out, true
}

// collectBindings unions every local binding made in cmds into `into`, recursing the
// same-scope bodies of each command that follow accepts. Bindings inherit the
// analyzer's scope context (a.ns/a.frame): the whole analyzed body is treated as one
// frame.
func (a *analyzer) collectBindings(cmds []Command, base int, into reachSet, follow func(Command) bool) {
	for _, c := range cmds {
		a.collectSubstBindings(c, base, into, follow)
		for _, d := range localBindings(c, base, a.ns, a.frame) {
			into[d.Name] = appendDedup(into[d.Name], d)
		}
		if !follow(c) {
			continue
		}
		for _, b := range a.sameScopeBodies(c, base) {
			a.collectBindings(Parse(b.Inner), b.Base, into, follow)
		}
	}
}

// visitBodyHoldingUse walks the same-scope body of a passed-over command that holds
// the use, if any, entering with what reaches the command. Whether that body runs here
// is unknown, but the use is only meaningful if it does. The walk gets its own set and
// no enclosing loop, so the body's bindings do not leave it, even by break or continue.
func (a *analyzer) visitBodyHoldingUse(c Command, base int, in reachSet) {
	for _, b := range a.sameScopeBodies(c, base) {
		if a.useOff < b.Base || a.useOff >= b.Base+len(b.Inner) {
			continue
		}
		loops, returned := a.loopStack, a.ret
		a.loopStack = nil
		a.seq(Parse(b.Inner), b.Base, in.clone()) //nolint:errcheck — a.found/a.answer carry the answer
		a.loopStack, a.ret = loops, returned
	}
}

// recordSkipped records the bindings in the same-scope bodies of a command the
// analyzer passes over without descending.
func (a *analyzer) recordSkipped(c Command, base int) {
	everyBody := func(Command) bool { return true }
	for _, b := range a.sameScopeBodies(c, base) {
		a.collectBindings(Parse(b.Inner), b.Base, a.skipped, everyBody)
	}
}

// appendDedup appends d to list only if no existing element has the same
// NameStart/NameEnd range (identity by binding site).
func appendDedup(list []Definition, d Definition) []Definition {
	for _, x := range list {
		if x.NameStart == d.NameStart && x.NameEnd == d.NameEnd {
			return list
		}
	}
	return append(list, d)
}

// command applies one command: thread the set through its substitutions, recording
// any variable use at useOff, then apply this command's bindings (kill prior, gen
// new). Returns (out set, live). live is false for non-local exits
// (return/break/continue).
func (a *analyzer) command(c Command, base int, in reachSet) (reachSet, bool) {
	if isCmd(c.Words, "if") {
		return a.analyzeIf(c, base, in)
	}
	test := loopTest(c)
	in, live := a.substitute(c, base, in, func(idx int) bool { return idx == test })
	if !live {
		return in, false
	}
	switch {
	case conservativeHead(c):
		return a.analyzeConservative(c, base, in)
	case loopHead(c):
		return a.analyzeLoop(c, base, in)
	case isCmd(c.Words, "return"):
		a.ret = joinAll([]reachSet{nonNil(a.ret), in})
		return in, false
	case isCmd(c.Words, "break"):
		if n := len(a.loopStack); n > 0 {
			top := a.loopStack[n-1]
			top.brk = joinAll([]reachSet{nonNil(top.brk), in})
		}
		return in, false
	case isCmd(c.Words, "continue"):
		if n := len(a.loopStack); n > 0 {
			top := a.loopStack[n-1]
			top.cont = joinAll([]reachSet{nonNil(top.cont), in})
		}
		return in, false
	}
	a.visitBodyHoldingUse(c, base, in)
	a.recordSkipped(c, base)
	binds := localBindings(c, base, a.ns, a.frame)
	if isRMW(c) {
		for _, d := range binds {
			if a.useOff >= d.NameStart && a.useOff < d.NameEnd {
				a.recordAnswer(d.Name, in[d.Name])
			}
		}
	}
	if len(binds) == 0 {
		return in, true
	}
	out := in.clone()
	for _, d := range binds {
		if isArraySet(c.Words) {
			out[d.Name] = appendDedup(out[d.Name], d) // merges into the array
		} else {
			out[d.Name] = []Definition{d} // straight-line: reassign kills+gens
		}
	}
	return out, true
}

// analyzeIf threads the reaching set through each branch body of an if command
// and joins only the live-exiting branches. Every unbraced word is substituted
// before the if runs. A braced condition is evaluated only when no earlier branch
// was taken, so a body sees the conditions up to its own, and else sees them all.
// When there is no else clause, the fall-through (no branch taken) path is always
// live and added to the join. Returns (joined set, anyLive): anyLive is true when
// at least one path is live.
func (a *analyzer) analyzeIf(c Command, base int, in reachSet) (reachSet, bool) {
	w := c.Words
	braceCond := func(idx int) bool { return isIfCondition(w, idx) && w[idx].Kind == WordBraced }
	cur, live := a.substitute(c, base, in, braceCond)
	if !live {
		return cur, false
	}
	exprWords := exprWordStarts(c)
	var liveOuts []reachSet
	anyLive := false
	for i := 1; i < len(w); i++ {
		if braceCond(i) {
			cur, _ = a.substituteWord(c, exprWords, i, base, cur, false, &live)
			if !live {
				// No later condition or body runs, and neither does the fall-through.
				return joinAll(liveOuts), anyLive
			}
			continue
		}
		if w[i].Kind != WordBraced || isIfCondition(w, i) {
			continue // then, else, an unbraced body, or an unbraced condition
		}
		inner, ibase := bracedInner(w[i], base)
		out, bodyLive := a.seq(Parse(inner), ibase, cur.clone())
		if bodyLive {
			liveOuts = append(liveOuts, out)
			anyLive = true
		}
	}
	if !hasElse(c.Words) {
		liveOuts = append(liveOuts, cur) // no branch taken: fall-through path
		anyLive = true
	}
	return joinAll(liveOuts), anyLive
}

// hasElse reports whether the if command words include an else keyword.
func hasElse(w []Word) bool {
	for _, x := range w {
		if x.Kind == WordBare && x.Text == "else" {
			return true
		}
	}
	return false
}

// joinAll unions reaching sets per variable, deduplicating bindings by
// name-range (NameStart/NameEnd pair).
func joinAll(sets []reachSet) reachSet {
	out := reachSet{}
	type key struct{ s, e int }
	seen := map[string]map[key]bool{}
	for _, s := range sets {
		for name, defs := range s {
			if seen[name] == nil {
				seen[name] = map[key]bool{}
			}
			for _, d := range defs {
				k := key{d.NameStart, d.NameEnd}
				if seen[name][k] {
					continue
				}
				seen[name][k] = true
				out[name] = append(out[name], d)
			}
		}
	}
	return out
}

// nonNil returns s unchanged if non-nil, or an empty reachSet if s is nil.
// Used to safely merge an accumulator that has not been written yet.
func nonNil(s reachSet) reachSet {
	if s == nil {
		return reachSet{}
	}
	return s
}

// recordAnswer unions defs into the answer for the use of name at useOff. A use is
// visited once per loop pass, and reaching sets only grow across passes, so the union
// over every visit is the set at the fixpoint.
func (a *analyzer) recordAnswer(name string, defs []Definition) {
	a.found = true
	a.useName = name
	for _, d := range defs {
		a.answer = appendDedup(a.answer, d)
	}
}

// localBindings returns the variable bindings introduced by a single command c,
// applying the same rules as emitDefs. base is the absolute offset of the parse
// context that produced c's offsets; ns and frame are the analyzed scope's
// namespace and kind (the analyzer treats the whole scope as one frame).
//
// KEEP IN SYNC with emitDefs (defs.go): this is a second copy of the binding
// rules, projected to what reaching-defs needs. The `variable`-link Origin was
// once dropped here while defs.go carried it, producing a wrong goto-def jump
// (see variable_link_reaching_test.go in internal/resolve) -- any rule change
// in one file must be mirrored in the other, and pinned by a resolve-level test
// exercising BOTH paths. One difference is on purpose: at FrameOpaque emitDefs binds
// no bare name and no relative qualified name, but this still binds both, because
// that is how `$o m` is typed there.
func localBindings(c Command, base int, ns string, frame FrameKind) []Definition {
	var out []Definition
	w := c.Words
	if len(w) == 0 {
		return nil
	}

	// set x VAL  (proc-local scalar or array base)
	if isCmd(w, "set") && len(w) >= 2 {
		if name, s, e, ok := arrayBaseName(w[1]); ok {
			out = append(out, Definition{
				Kind: DefLocal, Name: name,
				NameStart: base + s, NameEnd: base + e,
			})
		}
	}

	// array set NAME data (binds array NAME; `command` merges it, unlike `set`)
	if isArraySet(w) {
		if name, s, e, ok := arrayBaseName(w[2]); ok {
			out = append(out, Definition{
				Kind: DefLocal, Name: name,
				NameStart: base + s, NameEnd: base + e,
			})
		}
	}

	// incr / append / lappend x ...
	if len(w) >= 2 {
		switch {
		case isCmd(w, "incr"), isCmd(w, "append"), isCmd(w, "lappend"):
			if name, s, e, ok := arrayBaseName(w[1]); ok {
				out = append(out, Definition{
					Kind: DefLocal, Name: name,
					NameStart: base + s, NameEnd: base + e,
				})
			}
		}
	}

	// variable NAME ?val? -- in a proc frame this LINKS the local name to the
	// namespace variable, so Origin carries the qualified target for the
	// resolver to chase (mirrors emitDefs' FrameProc case). In a namespace
	// frame the declaration IS the definition: no Origin.
	if isCmd(w, "variable") && len(w) >= 2 && isPlainName(w[1]) {
		d := Definition{
			Kind: DefLocal, Name: w[1].Text,
			NameStart: base + w[1].Start, NameEnd: base + w[1].End,
		}
		if frame == FrameProc {
			d.Origin = qualifyName(w[1].Text, ns)
		}
		out = append(out, d)
	}

	// global NAME ... (DefGlobalLink per name)
	if isCmd(w, "global") {
		for _, gw := range w[1:] {
			if isPlainName(gw) {
				out = append(out, Definition{
					Kind: DefGlobalLink, Name: gw.Text,
					NameStart: base + gw.Start, NameEnd: base + gw.End,
					Origin: globalOrigin(gw.Text),
				})
			}
		}
	}

	// upvar ?level? otherVar alias ?otherVar alias ...?
	if isCmd(w, "upvar") && len(w) >= 3 {
		args := w[1:]
		level := ""
		if len(args) > 0 && isUpvarLevel(args[0]) {
			level = args[0].Text
			args = args[1:]
		}
		for i := 1; i < len(args); i += 2 {
			alias := args[i]
			if isPlainName(alias) {
				out = append(out, Definition{
					Kind: DefLocal, Name: alias.Text,
					NameStart: base + alias.Start, NameEnd: base + alias.End,
					Origin: upvarOrigin(level, args[i-1]),
				})
			}
		}
	}

	// foreach / lmap / lassign / dict for|map — loop var lists
	emitLoopVarDefs(w, base, "::", 0, "", &out)

	return out
}

// isArraySet reports whether w is `array set NAME data`.
func isArraySet(w []Word) bool {
	return isCmd(w, "array") && len(w) >= 3 && w[1].Kind == WordBare && w[1].Text == "set"
}

// isRMW reports whether command c is a read-modify-write command
// (incr, append, or lappend).
func isRMW(c Command) bool {
	return isCmd(c.Words, "incr") || isCmd(c.Words, "append") || isCmd(c.Words, "lappend")
}
