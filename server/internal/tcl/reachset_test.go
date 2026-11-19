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

// reachingCost runs ReachingAt once and returns the bytes it allocated and the
// time it took. Bytes are deterministic, and the flat map grew them by a full
// copy of the set per binding, so they show the scaling where time is noise.
func reachingCost(t *testing.T, src string, off, want int) (uint64, time.Duration) {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	defs, ok := ReachingAt(src, off)
	took := time.Since(start)
	runtime.ReadMemStats(&after)
	if !ok || len(defs) != want {
		t.Fatalf("ReachingAt = %d bindings, %v; want %d", len(defs), ok, want)
	}
	return after.TotalAlloc - before.TotalAlloc, took
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
