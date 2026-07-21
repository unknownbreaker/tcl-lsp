package tcl

// ReachingAt returns the local-variable bindings that may reach the use at byte
// offset useOff in src, as Definition values (Kind DefLocal/DefGlobalLink, with
// NameStart/NameEnd on the binding's name token and Origin for links). ok is
// false when useOff is not on a proc-local variable use or no in-proc binding
// reaches it. Intraprocedural and may-reach (over-approximate, never drops a real
// reaching def). Later tasks add control-flow precision; this task handles a
// straight-line proc body. Also supports namespace-level variables (used by RVT
// templates).
func ReachingAt(src string, useOff int) (defs []Definition, ok bool) {
	inner, innerBase, argsWord, argsBase, ns, frame, found := enclosingProcOrScope(Parse(src), 0, "::", FrameNamespace, 0, useOff)
	if !found {
		return nil, false
	}
	const maxReachingBytes = 200000
	if len(inner) > maxReachingBytes {
		return nil, false // oversized: caller falls back to first-binding
	}
	a := &analyzer{useOff: useOff, ns: ns, frame: frame}
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
	return a.answer, true
}

// enclosingProcOrScope returns the interior text and absolute base of the innermost
// scope (proc or namespace) containing useOff, plus the proc's args Word and base
// for interpreting it (empty/zero for namespace scopes). Also returns the scope's
// namespace and frame kind (FrameProc or FrameNamespace).
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
			// a scope-introducing frame (proc or namespace).
			if b.Frame == FrameProc && b.Scope == b.Base {
				// Recover the args word from the proc command.
				aw, ab := procArgsWord(c, base)
				return b.Inner, b.Base, aw, ab, b.NS, FrameProc, true
			}
			if b.Frame == FrameNamespace && b.Scope == 0 {
				return b.Inner, b.Base, Word{}, base, b.NS, FrameNamespace, true
			}
		}
	}
	return "", 0, Word{}, 0, "::", FrameNamespace, false
}

// procArgsWord returns the args Word and base for the proc command c (or a
// decorated proc). Returns a zero Word when the command is not a proc.
func procArgsWord(c Command, base int) (Word, int) {
	w := c.Words
	if isCmd(w, "proc") && len(w) >= 4 {
		return w[2], base
	}
	if _, args, _, ok := decoratedProcDef(w); ok {
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
	answer    []Definition
	found     bool
	loopStack []*frameAcc
	ret       reachSet
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

// analyzeLoop iterates the loop body to a fixpoint so a value assigned in one
// iteration reaches later iterations' uses, then joins with the entry set (the
// loop may run zero times). Loop variables (foreach/lmap/dict-for) are gen'd at
// body entry. `for`'s start/next scripts are folded into the iterated block; for
// may-reach soundness, treating start as possibly-repeated only over-approximates.
// break/continue exits are accumulated in a per-loop frameAcc pushed onto loopStack.
func (a *analyzer) analyzeLoop(c Command, base int, in reachSet) (reachSet, bool) {
	acc := &frameAcc{brk: reachSet{}, cont: reachSet{}}
	a.loopStack = append(a.loopStack, acc)
	defer func() { a.loopStack = a.loopStack[:len(a.loopStack)-1] }()

	bodyEntry := in.clone()
	for _, d := range localBindings(c, base, a.ns, a.frame) {
		bodyEntry[d.Name] = []Definition{d}
	}
	bodies := scriptBodies(c.Words)
	cur := bodyEntry
	normalEnd := reachSet{}
	for iter := 0; iter < maxLoopIters; iter++ {
		next := cur.clone()
		live := true
		for _, b := range bodies {
			inner, ibase := bracedInner(b, base)
			next, live = a.seq(Parse(inner), ibase, next)
			if !live {
				break
			}
		}
		if live {
			normalEnd = next
		} else {
			normalEnd = reachSet{}
		}
		backEdge := joinAll([]reachSet{normalEnd, acc.cont})
		merged := joinAll([]reachSet{cur, backEdge})
		if reachEqual(merged, cur) {
			break
		}
		cur = merged
	}
	// cur is the fixpoint set at the loop header: it already subsumes `in`,
	// `normalEnd`, and all `acc.cont` back-edge contributions, so it covers
	// every path that may reach post-loop — including iterations that ended
	// via `continue`. acc.brk adds paths that exited via `break`.
	exit := joinAll([]reachSet{cur, acc.brk})
	return exit, true
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
// modeling: catch, try, and switch are alternatives or may-abort constructs
// where precise arm/exception modeling is deferred.
func conservativeHead(c Command) bool {
	return isCmd(c.Words, "catch") || isCmd(c.Words, "try") || isCmd(c.Words, "switch")
}

// analyzeConservative over-approximates: the bodies may or may not run and may
// exit at any point, so the result joins the entry set with every binding
// generated anywhere in the bodies (and the construct's own bindings, e.g. a
// catch result variable). Sound for may-reach; never drops a real reaching def.
//
// Known graceful-degradation limits (no code change needed — these fall back to
// first-binding at the resolver): (a) a $x use directly inside a switch arm body
// is found via the conservative bodyView pass, but the reaching answer comes from
// the over-approximated union rather than a precise reaching set; (b) try...on/trap
// handler var-list names (the varList argument to on/trap) are not modeled as
// bindings and are not tracked by reaching-defs.
func (a *analyzer) analyzeConservative(c Command, base int, in reachSet) (reachSet, bool) {
	gens := reachSet{}
	for _, b := range scriptBodies(c.Words) {
		inner, ibase := bracedInner(b, base)
		a.collectBindings(Parse(inner), ibase, gens)
	}
	// For inline-form switch (`switch ?opts? string pat body pat body ...`),
	// scriptBodies returns only the last braced word (the default arm, by the
	// trailing-brace heuristic). Collect bindings and record uses from EVERY
	// braced word in c.Words[2:] so no arm is silently dropped.
	if isCmd(c.Words, "switch") {
		for _, w := range c.Words[2:] {
			if w.Kind == WordBraced {
				inner, ibase := bracedInner(w, base)
				a.collectBindings(Parse(inner), ibase, gens)
			}
		}
	}
	bodyView := joinAll([]reachSet{in, gens})
	// Record any use at useOff inside the bodies against the conservative view.
	for _, b := range scriptBodies(c.Words) {
		inner, ibase := bracedInner(b, base)
		a.seq(Parse(inner), ibase, bodyView.clone())
	}
	// For inline switch, also record uses inside each arm body.
	if isCmd(c.Words, "switch") {
		for _, w := range c.Words[2:] {
			if w.Kind == WordBraced {
				inner, ibase := bracedInner(w, base)
				a.seq(Parse(inner), ibase, bodyView.clone())
			}
		}
	}
	out := bodyView.clone()
	for _, d := range localBindings(c, base, a.ns, a.frame) { // catch resultVar / optionsVar
		out[d.Name] = appendDedup(out[d.Name], d)
	}
	return out, true
}

// collectBindings unions every local binding made anywhere in cmds (recursing
// all child script bodies) into `into`. Bindings inherit the analyzer's scope
// context (a.ns/a.frame): the whole analyzed body is treated as one frame.
func (a *analyzer) collectBindings(cmds []Command, base int, into reachSet) {
	for _, c := range cmds {
		for _, d := range localBindings(c, base, a.ns, a.frame) {
			into[d.Name] = appendDedup(into[d.Name], d)
		}
		for _, b := range childBodies(c, base, "::", FrameProc, base, "") {
			a.collectBindings(Parse(b.Inner), b.Base, into)
		}
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

// command applies one command: record any variable use at useOff against the
// incoming set, then apply this command's bindings (kill prior, gen new).
// Returns (out set, live). live is false for non-local exits (return/break/continue).
func (a *analyzer) command(c Command, base int, in reachSet) (reachSet, bool) {
	a.recordUses(c, base, in)
	switch {
	case isCmd(c.Words, "if"):
		return a.analyzeIf(c, base, in)
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
	binds := localBindings(c, base, a.ns, a.frame)
	if isRMW(c) && !a.found {
		for _, d := range binds {
			if a.useOff >= d.NameStart && a.useOff < d.NameEnd {
				a.answer = append([]Definition(nil), in[d.Name]...)
				a.found = true
			}
		}
	}
	if len(binds) == 0 {
		return in, true
	}
	out := in.clone()
	for _, d := range binds {
		out[d.Name] = []Definition{d} // straight-line: reassign kills+gens
	}
	return out, true
}

// analyzeIf threads the reaching set through each branch body of an if command
// and joins only the live-exiting branches. When there is no else clause, the
// fall-through (no branch taken) path is always live and added to the join.
// Returns (joined set, anyLive): anyLive is true when at least one path is live.
func (a *analyzer) analyzeIf(c Command, base int, in reachSet) (reachSet, bool) {
	var liveOuts []reachSet
	anyLive := false
	for _, b := range ifBodies(c.Words) {
		inner, ibase := bracedInner(b, base)
		out, live := a.seq(Parse(inner), ibase, in.clone())
		if live {
			liveOuts = append(liveOuts, out)
			anyLive = true
		}
	}
	if !hasElse(c.Words) {
		liveOuts = append(liveOuts, in) // no branch taken: fall-through path
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

// recordUses snapshots the reaching set for the variable used at useOff (a
// $-substituted RefVariable in this command's own words).
func (a *analyzer) recordUses(c Command, base int, in reachSet) {
	if a.found {
		return
	}
	for _, r := range CommandRefs(c) {
		if r.Kind != RefVariable {
			continue
		}
		s, e := r.Start+base, r.End+base
		if a.useOff >= s && a.useOff < e {
			a.answer = append([]Definition(nil), in[r.Name]...)
			a.found = true
			return
		}
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
// exercising BOTH paths.
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

// isRMW reports whether command c is a read-modify-write command
// (incr, append, or lappend).
func isRMW(c Command) bool {
	return isCmd(c.Words, "incr") || isCmd(c.Words, "append") || isCmd(c.Words, "lappend")
}
