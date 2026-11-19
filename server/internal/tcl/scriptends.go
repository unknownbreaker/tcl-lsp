package tcl

import "unsafe"

// Script ends found once. Finding where a [script] ends reads every script nested
// in it, and each nested script is then parsed and walked in turn, which finds
// the ends inside it again: n levels of `[list a [list a ...]]` cost n² (#84).
// A scriptEnds remembers each end it finds, so a walk that threads one through
// the scanner and the substitution walk reads each byte a bounded number of times.
//
// An end is keyed by the address of the script's first interior byte, and a span
// (below) by the address of its opening `[` or `{`, not by an offset, so it can never answer for different text: Go strings are immutable,
// and the entry keeps the string it was found in alive, so that memory is not
// reused while the memo lives. A memo serves one walk on one goroutine and is
// dropped with it. A nil *scriptEnds remembers nothing; a zero one is ready.
//
// scriptClose(s, i) depends only on the bytes from i to the end of s, and a
// shorter s can only fail sooner: an end found at e answers any s that holds
// the byte at e, and is "none" for one that stops before it. A failure answers
// any s that stops no later than the one that failed.
type scriptEnds struct {
	ends   map[uintptr]scriptEnd
	depths map[uintptr]spanEnd // [script] spans by depth count, by their `[`
	braces map[uintptr]spanEnd // {braced} spans, by their `{`
}

type scriptEnd struct {
	keep  string  // the text the end was found in, held so its memory stays put
	at    uintptr // address of the closing `]`, or of the end of keep on failure
	found bool
}

// addr returns the address of s[i]; i < len(s).
func addr(s string, i int) uintptr {
	return uintptr(unsafe.Pointer(unsafe.StringData(s))) + uintptr(i)
}

// lookup returns the remembered answer for scriptClose(s, i), and false when
// there is none that applies.
func (m *scriptEnds) lookup(s string, i int) (end int, found, hit bool) {
	if m == nil || i >= len(s) {
		return 0, false, false
	}
	e, ok := m.ends[addr(s, i)]
	if !ok {
		return 0, false, false
	}
	start := addr(s, 0)
	limit := start + uintptr(len(s))
	switch {
	case e.found && e.at < limit:
		return int(e.at - start), true, true
	case e.found:
		return 0, false, true // s stops before the `]` that closes it
	case limit <= e.at:
		return 0, false, true // a longer text already failed
	}
	return 0, false, false
}

// store remembers the answer scriptClose(s, i) returned.
func (m *scriptEnds) store(s string, i, end int, found bool) {
	if m == nil || i >= len(s) {
		return
	}
	if m.ends == nil {
		m.ends = map[uintptr]scriptEnd{}
	}
	e := scriptEnd{keep: s, found: found}
	if found {
		e.at = addr(s, end)
	} else {
		e.at = addr(s, 0) + uintptr(len(s))
	}
	m.ends[addr(s, i)] = e
}

// Spans found once. The depth count of a [script] in an expression
// (skipBracketSpan), the trust check over that span (bracesHideBrackets) and a
// braced word's span (skipBraceSpan) each read the whole span, and so the spans
// nested in it, at every level: n levels of `[expr {1+[expr {1+ ...}]}]` cost n²
// (#95). Each is remembered by the address of its opening byte, as script ends
// are, and each jumps over the spans nested in it through the memo, so every
// span is read once per walk.
//
// A span's end depends only on the bytes from its opening to its closing byte,
// and its trust only on the bytes between them, so a span that closed answers any
// text that holds its closing byte, and is "open" for one that stops before it.
// A span that stays open answers any text that stops no later than the one it
// was found in. The trust of an open span depends on where the text stops, so it
// is not remembered.
type spanEnd struct {
	keep  string  // the text the span was found in, held so its memory stays put
	at    uintptr // address of the closing byte, or of the end of keep when open
	open  bool
	trust int8 // a [script] span's trust: 0 unknown, 1 trusted, -1 not
	// A [script] span's braces between its `[` and `]`: the net count of `{`
	// over `}`, and the lowest running count, as bracesHideBrackets counts them
	// inside braces. trust and these are unused for a {braced} span.
	braceNet, braceLow int
}

// spanLookup returns the remembered entry for the span opening at s[i] and, when
// it applies to s, whether the span closes in s, with end just past its closing
// byte, or len(s) when it does not close.
func spanLookup(tab map[uintptr]spanEnd, s string, i int) (e spanEnd, end int, closed, hit bool) {
	e, ok := tab[addr(s, i)]
	if !ok {
		return e, 0, false, false
	}
	start := addr(s, 0)
	limit := start + uintptr(len(s))
	switch {
	case !e.open && e.at < limit:
		return e, int(e.at-start) + 1, true, true
	case !e.open, limit <= e.at:
		return e, len(s), false, true // s stops before the span could close
	}
	return e, 0, false, false // a shorter text stayed open; s may close it
}

// spanMemo answers the span opening at s[i] from tab when an entry applies, and
// otherwise runs scan and remembers its answer: that the span closes at
// s[end-1], or stays open in s. A nil m runs scan alone.
func (m *scriptEnds) spanMemo(tab *map[uintptr]spanEnd, s string, i int, scan func() (spanEnd, int, bool)) (e spanEnd, end int, closed bool) {
	if m == nil {
		return scan()
	}
	if e, end, closed, hit := spanLookup(*tab, s, i); hit {
		return e, end, closed
	}
	e, end, closed = scan()
	if *tab == nil {
		*tab = map[uintptr]spanEnd{}
	}
	stored := e
	stored.keep = s
	if closed {
		stored.at = addr(s, end-1)
	} else {
		stored.open, stored.at = true, addr(s, 0)+uintptr(len(s))
	}
	(*tab)[addr(s, i)] = stored
	return e, end, closed
}

// depthSpan is skipBracketSpan(s, i) for the `[` at i, and whether the span
// closes in s.
func (m *scriptEnds) depthSpan(s string, i int) (end int, closed bool) {
	_, end, closed = m.depthScan(s, i)
	return end, closed
}

// depthScan is depthSpan, with the span's brace counts when it closes. A nested
// span is jumped over: the depth count returns to this level exactly where that
// span's own count closes.
func (m *scriptEnds) depthScan(s string, i int) (e spanEnd, end int, closed bool) {
	var tab *map[uintptr]spanEnd
	if m != nil {
		tab = &m.depths
	}
	return m.spanMemo(tab, s, i, func() (spanEnd, int, bool) { return m.scanDepth(s, i) })
}

// scanDepth is depthScan without the memo for the span at i itself.
func (m *scriptEnds) scanDepth(s string, i int) (e spanEnd, end int, closed bool) {
	net, low := 0, 0
	for j := i + 1; j < len(s); {
		switch c := s[j]; {
		case c == '\\' && j+1 < len(s):
			j += 2
		case c == '[':
			inner, innerEnd, ok := m.depthScan(s, j)
			if !ok {
				return spanEnd{}, len(s), false
			}
			low = min(low, net+inner.braceLow)
			net += inner.braceNet
			j = innerEnd
		case c == ']':
			return spanEnd{braceNet: net, braceLow: low}, j + 1, true
		case c == '{':
			net++
			j++
		case c == '}':
			net--
			low = min(low, net)
			j++
		default:
			j++
		}
	}
	return spanEnd{}, len(s), false
}

// trustedDepthSpan returns the offset just past the `]` the depth count of
// skipBracketSpan pairs with the `[` at i, and whether that span can be trusted:
// it closes and bracesHideBrackets finds nothing in it that may hide its `]`.
func (m *scriptEnds) trustedDepthSpan(text string, i int) (int, bool) {
	end, closed := m.depthSpan(text, i)
	if !closed {
		// Unterminated: the depth count ran to the end of text, which is
		// trusted only when it happens to stop on a `]`.
		if end == i+1 || text[end-1] != ']' || m.bracesHideBrackets(text[i+1:end-1]) {
			return end, false
		}
		return end, true
	}
	if m == nil {
		return end, !m.bracesHideBrackets(text[i+1 : end-1])
	}
	key := addr(text, i)
	e := m.depths[key]
	if e.trust == 0 {
		e.trust = 1
		if m.bracesHideBrackets(text[i+1 : end-1]) {
			e.trust = -1
		}
		m.depths[key] = e
	}
	return end, e.trust > 0
}

// closedTrustedSpan returns the end of the [script] span at i when it closes in
// s and is trusted.
func (m *scriptEnds) closedTrustedSpan(s string, i int) (int, bool) {
	if _, closed := m.depthSpan(s, i); !closed {
		return 0, false
	}
	return m.trustedDepthSpan(s, i)
}

// skipBraceSpan returns the index just past a balanced {..} span starting at i.
// Backslash-aware; tolerant of unterminated input (returns len(text)).
func (m *scriptEnds) skipBraceSpan(s string, i int) int {
	end, _ := m.braceSpan(s, i)
	return end
}

// braceSpan is skipBraceSpan for the `{` at i, and whether the span closes in
// s. A nested span is jumped over, as in depthSpan.
func (m *scriptEnds) braceSpan(s string, i int) (end int, closed bool) {
	var tab *map[uintptr]spanEnd
	if m != nil {
		tab = &m.braces
	}
	_, end, closed = m.spanMemo(tab, s, i, func() (spanEnd, int, bool) {
		end, closed := m.scanBrace(s, i)
		return spanEnd{}, end, closed
	})
	return end, closed
}

// scanBrace is braceSpan without the memo for the span at i itself.
func (m *scriptEnds) scanBrace(s string, i int) (end int, closed bool) {
	for j := i + 1; j < len(s); {
		switch c := s[j]; {
		case c == '\\' && j+1 < len(s):
			j += 2
		case c == '{':
			e, ok := m.braceSpan(s, j)
			if !ok {
				return len(s), false
			}
			j = e
		case c == '}':
			return j + 1, true
		default:
			j++
		}
	}
	return len(s), false
}
