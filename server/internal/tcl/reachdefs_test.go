package tcl

import (
	"fmt"
	"math/rand"
	"testing"
)

// refUnion is the eager union defList must agree with: x, then y's sites not in x.
func refUnion(x, y []Definition) []Definition {
	out := append([]Definition(nil), x...)
	seen := map[defKey]bool{}
	for _, d := range x {
		seen[keyOf(d)] = true
	}
	for _, d := range y {
		if !seen[keyOf(d)] {
			seen[keyOf(d)] = true
			out = append(out, d)
		}
	}
	return out
}

// collidingKeys returns want distinct sites, in pairs whose keyHash is equal.
func collidingKeys(want int) []defKey {
	byHash := map[uint32]defKey{}
	var out []defKey
	for s := 0; len(out) < want; s++ {
		for e := s + 1; e < s+3 && len(out) < want; e++ {
			k := defKey{s, e}
			if o, ok := byHash[keyHash(k)]; ok {
				out = append(out, o, k)
			} else {
				byHash[keyHash(k)] = k
			}
		}
	}
	return out
}

// TestDefListMatchesReference drives unionDefs with random unions and appends
// over sites that include full keyHash collisions and the same site under two
// Namespace spellings, and checks order, first-copy-wins, len and keySubset
// against the eager union.
func TestDefListMatchesReference(t *testing.T) {
	keys := collidingKeys(16)
	for i := 0; i < 64; i++ {
		keys = append(keys, defKey{i * 7, i*7 + 1})
	}
	rng := rand.New(rand.NewSource(7))
	type pair struct {
		l   defList
		ref []Definition
	}
	mk := func() Definition {
		k := keys[rng.Intn(len(keys))]
		return Definition{Name: "x", NameStart: k.start, NameEnd: k.end, Namespace: []string{"", "::"}[rng.Intn(2)]}
	}
	var pool []pair
	for i := 0; i < 20; i++ {
		d := mk()
		pool = append(pool, pair{defsOf(d), []Definition{d}})
	}
	keysOf := func(l defList) *keyNode {
		if l.n == nil {
			return nil
		}
		return l.n.keys
	}
	for step := 0; step < 20000; step++ {
		a, b := pool[rng.Intn(len(pool))], pool[rng.Intn(len(pool))]
		if rng.Intn(4) == 0 {
			d := mk()
			b = pair{defsOf(d), []Definition{d}}
		}
		p := pair{unionDefs(a.l, b.l), refUnion(a.ref, b.ref)}
		if got := p.l.slice(); fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", p.ref) || p.l.len() != len(p.ref) {
			t.Fatalf("step %d: unionDefs = %+v (len %d), want %+v", step, got, p.l.len(), p.ref)
		}
		c := pool[rng.Intn(len(pool))]
		in := map[defKey]bool{}
		for _, d := range c.ref {
			in[keyOf(d)] = true
		}
		want := true
		for _, d := range p.ref {
			want = want && in[keyOf(d)]
		}
		if got := keySubset(keysOf(p.l), keysOf(c.l), 0); got != want {
			t.Fatalf("step %d: keySubset = %v, want %v", step, got, want)
		}
		pool = append(pool, p)
		if len(pool) > 200 {
			pool = pool[len(pool)-200:]
		}
	}
}
