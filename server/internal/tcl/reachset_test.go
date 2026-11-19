package tcl

import (
	"fmt"
	"math/rand"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// refSet is the flat map reachSet replaced, with the join it had: the reference
// the persistent set must agree with.
type refSet map[string][]Definition

func refJoin(sets []refSet) refSet {
	out := refSet{}
	seen := map[string]map[defKey]bool{}
	for _, s := range sets {
		for name, defs := range s {
			if seen[name] == nil {
				seen[name] = map[defKey]bool{}
			}
			for _, d := range defs {
				k := defKey{d.NameStart, d.NameEnd}
				if !seen[name][k] {
					seen[name][k] = true
					out[name] = append(out[name], d)
				}
			}
		}
	}
	return out
}

func toRef(s reachSet) refSet {
	out := refSet{}
	s.each(func(name string, defs defList) { out[name] = defs.slice() })
	return out
}

func refEqual(a, b refSet) bool {
	if len(a) != len(b) {
		return false
	}
	for name, da := range a {
		db, ok := b[name]
		if !ok || len(da) != len(db) {
			return false
		}
		keys := map[defKey]bool{}
		for _, d := range da {
			keys[defKey{d.NameStart, d.NameEnd}] = true
		}
		for _, d := range db {
			if !keys[defKey{d.NameStart, d.NameEnd}] {
				return false
			}
		}
	}
	return true
}

// TestReachSetMatchesFlatMap drives the persistent set and the flat map with the
// same random binds, appends and joins, and checks that every set holds exactly the
// reference bindings in the same order per name, and that reachEqual agrees. Few
// names force hash collisions in a leaf, and many force leaves down the trie.
func TestReachSetMatchesFlatMap(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	for _, names := range []int{3, 40, 2000} {
		var sets []reachSet
		var refs []refSet
		sets, refs = append(sets, reachSet{}), append(refs, refSet{})
		for step := 0; step < 3000; step++ {
			i := rng.Intn(len(sets))
			s, r := sets[i], refs[i]
			name := fmt.Sprintf("v%d", rng.Intn(names))
			// Namespace varies so one site is sometimes carried as two values, as a
			// try handler's variable is; the first a join sees must win.
			d := Definition{Name: name, Namespace: []string{"", "::"}[rng.Intn(2)], NameStart: rng.Intn(50), NameEnd: 0}
			d.NameEnd = d.NameStart + 1
			switch rng.Intn(4) {
			case 0: // straight-line bind
				nr := refSet{}
				for k, v := range r {
					nr[k] = v
				}
				nr[name] = []Definition{d}
				sets, refs = append(sets, s.with(name, defsOf(d))), append(refs, nr)
			case 1: // may-write
				nr := refSet{}
				for k, v := range r {
					nr[k] = v
				}
				nr[name] = refJoin([]refSet{{name: r[name]}, {name: {d}}})[name]
				sets, refs = append(sets, s.withAdded(d)), append(refs, nr)
			default: // join with one to three others
				pick := []int{i}
				for k := rng.Intn(3); k >= 0; k-- {
					pick = append(pick, rng.Intn(len(sets)))
				}
				var js []reachSet
				var jr []refSet
				for _, p := range pick {
					js, jr = append(js, sets[p]), append(jr, refs[p])
				}
				sets, refs = append(sets, joinSets(js)), append(refs, refJoin(jr))
			}
			last := len(sets) - 1
			if got, want := toRef(sets[last]), refs[last]; !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
				t.Fatalf("names=%d step %d: set %v, want %v", names, step, got, want)
			}
			j := rng.Intn(len(sets))
			if got, want := reachEqual(sets[last], sets[j]), refEqual(refs[last], refs[j]); got != want {
				t.Fatalf("names=%d step %d: reachEqual = %v, want %v", names, step, got, want)
			}
		}
		// No operation wrote into a set it was given.
		for i := range sets {
			if got := toRef(sets[i]); !reflect.DeepEqual(got, refs[i]) && !(len(got) == 0 && len(refs[i]) == 0) {
				t.Fatalf("names=%d: set %d changed after later operations: %v, want %v", names, i, got, refs[i])
			}
		}
	}
}

// deepExprShapes are generated sources whose reaching analysis must stay close
// to linear in n: deep ternaries and && chains, plain and with a binding in every
// operand, and straight-line binds, each inside a loop so the fixpoint runs.
var deepExprShapes = map[string]func(n int) string{
	"ternary": func(n int) string { return strings.Repeat("$c ? $x : ", n) + "0" },
	"and":     func(n int) string { return strings.Repeat("$c && ", n) + "$x" },
	"ternary binding": func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "$c ? [set v%d 1] : ", i)
		}
		return b.String() + "$x"
	},
	"and binding": func(n int) string {
		var b strings.Builder
		for i := 0; i < n; i++ {
			fmt.Fprintf(&b, "[set v%d 1] && ", i)
		}
		return b.String() + "$x"
	},
}

func deepExprSource(shape string, n int) (string, int) {
	src := "proc p {c} {\n set x 1\n while {" + deepExprShapes[shape](n) + "} { puts $x }\n}\n"
	return src, strings.LastIndex(src, "$x") + 1
}

// sameNameTernarySource sets x in every arm of a deep ternary, so n+1 bindings
// of the one name reach the body's use.
func sameNameTernarySource(n int) (string, int) {
	src := "proc p {c} {\n set x 1\n while {" + strings.Repeat("$c ? [set x 2] : ", n) + "0} { puts $x }\n}\n"
	return src, strings.LastIndex(src, "$x") + 1
}

// sameNameSwitchSource sets x in every arm of a switch inside a loop; the use
// after it sees every arm and the fall-through.
func sameNameSwitchSource(n int) (string, int) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  a%d {set x %d}\n", i, i)
	}
	src := "proc p {c} {\n set x 1\n while {$c} {\n switch $c {\n" + b.String() + " }\n puts $x }\n}\n"
	return src, strings.LastIndex(src, "$x") + 1
}

func straightSource(n int) (string, int) {
	var b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, " set v%d 1\n", i)
	}
	src := "proc p {c} {\n set x 1\n while {$c} {\n" + b.String() + " puts $x }\n}\n"
	return src, strings.LastIndex(src, "$x") + 1
}

// nestedLoopSource nests levels while loops, each holding perLevel bindings: of x
// when sameName, else of a name of their own. With after, a loop's bindings follow
// its inner loop, so each outer pass reaches the inner loop with new bindings. The
// use of x is in the innermost loop, above its bindings.
func nestedLoopSource(levels, perLevel int, sameName, after bool) (string, int) {
	binds := func(level int) string {
		var b strings.Builder
		for i := 0; i < perLevel; i++ {
			if sameName {
				fmt.Fprintf(&b, " set x %d\n", level*perLevel+i)
			} else {
				fmt.Fprintf(&b, " set v%d_%d 1\n", level, i)
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString("proc p {c} {\n set x 1\n")
	for level := 0; level < levels; level++ {
		b.WriteString(" while {$c} {\n")
		if !after {
			b.WriteString(binds(level))
		}
	}
	b.WriteString(" puts $x\n")
	for level := levels - 1; level >= 0; level-- {
		if after {
			b.WriteString(binds(level))
		}
		b.WriteString(" }\n")
	}
	b.WriteString("}\n")
	src := b.String()
	return src, strings.Index(src, "$x") + 1
}

// reachingCost runs ReachingAt once and returns the bytes it allocated and the
// time it took. Bytes are deterministic, and the flat map grew them by a full
// copy of the set per binding, so they show the scaling where time is noise.
func reachingCost(t *testing.T, src string, off, want int) (uint64, time.Duration) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	defs, ok := reachingWithin(t, src, off)
	took := time.Since(start)
	runtime.ReadMemStats(&after)
	if !ok || len(defs) != want {
		t.Fatalf("ReachingAt = %d bindings, %v; want %d", len(defs), ok, want)
	}
	return after.TotalAlloc - before.TotalAlloc, took
}

// TestReachingNestedForeachAndWrapper pins 20 levels of foreach alternating with
// an unknown command's body, which took over 20 s when each outer pass re-ran the
// inner loops. Each level re-collects the bodies below it, so the cost still grows
// with depth times size (BACKLOG "Cost at pathological depth").
func TestReachingNestedForeachAndWrapper(t *testing.T) {
	var b strings.Builder
	b.WriteString("proc p {l} {\n set x 1\n")
	for level := 0; level < 20; level++ {
		fmt.Fprintf(&b, " foreach y $l {\n wrap {\n set x %d\n", level)
	}
	b.WriteString(" puts $x\n")
	b.WriteString(strings.Repeat(" }\n }\n", 20))
	b.WriteString("}\n")
	src := b.String()
	start := time.Now()
	defs, ok := reachingWithin(t, src, strings.Index(src, "$x")+1)
	if took := time.Since(start); took > 2*time.Second {
		t.Errorf("20 levels took %v, want well under 2s", took)
	}
	site := strings.Index(src, "set x 19") + 4
	if !ok || len(defs) != 1 || defs[0].NameStart != site {
		t.Errorf("ReachingAt = %+v, %v; want the binding at %d", defs, ok, site)
	}
}

// reachingWithin runs ReachingAt and fails the test if it takes over 10 seconds, so
// a superlinear regression fails here rather than at the go test timeout.
func reachingWithin(t *testing.T, src string, off int) ([]Definition, bool) {
	t.Helper()
	type result struct {
		defs []Definition
		ok   bool
	}
	done := make(chan result, 1)
	go func() {
		defs, ok := ReachingAt(src, off)
		done <- result{defs, ok}
	}()
	select {
	case r := <-done:
		return r.defs, r.ok
	case <-time.After(10 * time.Second):
		t.Fatalf("ReachingAt took over 10s")
		return nil, false
	}
}

// TestReachingScalesLinearlyInDepth checks the ratio between depths 1000 and 4000
// rather than wall time. Linear allocates about 4x as much (4.0x to 5.1x when
// written); the flat map these sets replaced allocated 16x, with 59 GB for the
// ternary binding shape at 4000. The wall-time bound is loose.
func TestReachingScalesLinearlyInDepth(t *testing.T) {
	type source struct {
		gen  func(n int) (string, int)
		uses func(n int) int // bindings that reach the use
	}
	one := func(int) int { return 1 }
	all := func(n int) int { return n + 1 }
	sources := map[string]source{
		"straight-line binds": {straightSource, one},
		"same-name ternary":   {sameNameTernarySource, all},
		"same-name switch":    {sameNameSwitchSource, all},
		// Bindings above the inner loop: depth grows with n, 50 bindings a level.
		"nested loops":            {func(n int) (string, int) { return nestedLoopSource(n/50, 50, false, false) }, one},
		"nested loops, same name": {func(n int) (string, int) { return nestedLoopSource(n/50, 50, true, false) }, one},
		// Bindings below the inner loop cost a pass per enclosing loop, so the
		// depth stays 10 and the loops widen. TestReachingNestedLoopsPastBudget
		// covers depth growing with n.
		"nested loops, binds after inner": {func(n int) (string, int) { return nestedLoopSource(10, n/10, false, true) }, one},
		// Each loop's last binding reaches the use around a back edge, and so does
		// the binding above the loops.
		"nested loops, same name after inner": {func(n int) (string, int) { return nestedLoopSource(10, n/10, true, true) }, func(int) int { return 11 }},
	}
	for shape := range deepExprShapes {
		sources[shape] = source{func(n int) (string, int) { return deepExprSource(shape, n) }, one}
	}
	var names []string
	for name := range sources {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		s := sources[name]
		smallSrc, smallOff := s.gen(1000)
		largeSrc, largeOff := s.gen(4000)
		small, _ := reachingCost(t, smallSrc, smallOff, s.uses(1000))
		large, took := reachingCost(t, largeSrc, largeOff, s.uses(4000))
		if ratio := float64(large) / float64(small); ratio > 8 {
			t.Errorf("%s: depth 1000 allocated %d bytes, 4000 allocated %d (%.1fx), want about 4x", name, small, large, ratio)
		}
		if took > 2*time.Second {
			t.Errorf("%s: depth 4000 took %v, want well under 2s", name, took)
		}
	}
}

// TestReachingNestedLoopsPastBudget pins the work budget: with bindings below each
// inner loop, every outer pass sends new bindings through the loops it holds, so
// the cost grows with the depth. At 80 levels that passes the budget, and
// ReachingAt answers nothing, at no more than a few times the cost of answering
// the same bindings 10 levels deep.
func TestReachingNestedLoopsPastBudget(t *testing.T) {
	for _, sameName := range []bool{false, true} {
		answered, off := nestedLoopSource(10, 400, sameName, true)
		want := 1
		if sameName {
			want = 11
		}
		shallow, _ := reachingCost(t, answered, off, want)
		deepSrc, deepOff := nestedLoopSource(80, 50, sameName, true)
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		start := time.Now()
		defs, ok := reachingWithin(t, deepSrc, deepOff)
		took := time.Since(start)
		runtime.ReadMemStats(&after)
		if ok || defs != nil {
			t.Errorf("sameName=%v: 80 levels: ReachingAt = %d bindings, %v; want nothing, false", sameName, len(defs), ok)
		}
		deep := after.TotalAlloc - before.TotalAlloc
		if ratio := float64(deep) / float64(shallow); ratio > 4 {
			t.Errorf("sameName=%v: 80 levels allocated %d bytes, 10 levels %d (%.1fx), want at most 4x", sameName, deep, shallow, ratio)
		}
		if took > 2*time.Second {
			t.Errorf("sameName=%v: 80 levels took %v, want well under 2s", sameName, took)
		}
	}
}

func BenchmarkReachingDeepExpr(b *testing.B) {
	for _, shape := range []string{"ternary", "and", "ternary binding", "and binding"} {
		for _, n := range []int{1000, 2000, 4000} {
			src, off := deepExprSource(shape, n)
			b.Run(fmt.Sprintf("%s/%d", shape, n), func(b *testing.B) {
				for range b.N {
					ReachingAt(src, off)
				}
			})
		}
	}
}

// TestReachingKeepsFirstCopyOfASite pins a shape where one binding site reaches
// as two Definition values: tryEntries binds the handler variable with Namespace
// "", localBindings with "::", and the continue carries the first onto the back
// edge. The flat map's join kept the first value it saw.
func TestReachingKeepsFirstCopyOfASite(t *testing.T) {
	src := "proc p {l} {\n" +
		"    foreach x $l {\n" +
		"        try {} trap {X} {v5} - on break {} { continue }\n" +
		"    }\n" +
		"    puts $v5\n" +
		"}\n"
	defs, ok := ReachingAt(src, strings.LastIndex(src, "$v5")+1)
	site := strings.Index(src, "{v5}") + 1
	want := []Definition{{Kind: DefLocal, Name: "v5", Namespace: "::", NameStart: site, NameEnd: site + 2}}
	if !ok || !reflect.DeepEqual(defs, want) {
		t.Errorf("ReachingAt = %+v, %v; want %+v, true", defs, ok, want)
	}
}

// naiveSites renders a set as name -> sorted site list, for a reference equality.
// A name bound to no sites is left out, as get cannot tell it from an absent one.
func naiveSites(s reachSet) string {
	var parts []string
	s.each(func(name string, d defList) {
		if d.len() == 0 {
			return
		}
		var sites []string
		for _, x := range d.slice() {
			sites = append(sites, fmt.Sprintf("%d:%d", x.NameStart, x.NameEnd))
		}
		sort.Strings(sites)
		parts = append(parts, name+"="+strings.Join(sites, ","))
	})
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

// TestReachEqualMatchesNaive builds pairs of sets from shared ancestors by random
// with/withAdded/joinSets steps, some binding a name to no sites, and checks
// reachEqual against a naive comparison in both argument orders. Sets that share
// subtrees exercise nodesEqual's skipping, and leaves against inner nodes its
// fallback.
func TestReachEqualMatchesNaive(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	nameCount := []int{3, 20, 200}
	for trial := 0; trial < 50000; trial++ {
		nn := nameCount[trial%len(nameCount)]
		name := func() string { return fmt.Sprintf("v%d", r.Intn(nn)) }
		def := func(n string) Definition {
			s := r.Intn(6)
			return Definition{Kind: DefLocal, Name: n, NameStart: s, NameEnd: s + 1}
		}
		step := func(s reachSet) reachSet {
			switch r.Intn(4) {
			case 0:
				n := name()
				return s.with(n, defsOf(def(n)))
			case 1:
				n := name()
				return s.withAdded(def(n))
			case 2:
				n := name()
				return s.with(n, defList{})
			default:
				o := reachSet{}
				for i := r.Intn(5); i > 0; i-- {
					o = o.withAdded(def(name()))
				}
				if r.Intn(2) == 0 {
					return joinSets([]reachSet{s, o})
				}
				return joinSets([]reachSet{o, s})
			}
		}
		base := reachSet{}
		for i := r.Intn(nn); i > 0; i-- {
			base = step(base)
		}
		a, b := base, base
		for i := r.Intn(4); i > 0; i-- {
			a = step(a)
		}
		for i := r.Intn(4); i > 0; i-- {
			b = step(b)
		}
		if r.Intn(3) == 0 {
			b = joinSets([]reachSet{a, b})
		}
		want := naiveSites(a) == naiveSites(b)
		if got := reachEqual(a, b); got != want {
			t.Fatalf("trial %d: reachEqual(a,b)=%v naive=%v\na=%s\nb=%s", trial, got, want, naiveSites(a), naiveSites(b))
		}
		if got := reachEqual(b, a); got != want {
			t.Fatalf("trial %d: reachEqual(b,a)=%v naive=%v", trial, got, want)
		}
	}
}
