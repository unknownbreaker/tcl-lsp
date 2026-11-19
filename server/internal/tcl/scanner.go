package tcl

// Kind enumerates the token categories the scanner emits.
type Kind int

const (
	KindEOF       Kind = iota // end of input
	KindNewline               // "\n" — a command separator
	KindSemicolon             // ";"  — a command separator
	KindComment               // "# ..." to end of line (only at command start)
	KindWord                  // one word: bare, {braced}, or "quoted" (raw text)
)

// Token is a lexical unit with its raw source text and byte range [Start,End).
type Token struct {
	Kind  Kind
	Text  string
	Start int
	End   int
}

// Scan tokenizes src into a slice of tokens always terminated by KindEOF.
func Scan(src string) []Token {
	return scanWith(src, nil)
}

// scanWith is Scan, finding script ends through ends (scriptends.go).
func scanWith(src string, ends *scriptEnds) []Token {
	s := &scanner{src: src, atCommandStart: true, ends: ends}
	return s.scan()
}

type scanner struct {
	src            string
	pos            int
	atCommandStart bool
	toks           []Token
	ends           *scriptEnds
}

func (s *scanner) emit(k Kind, start, end int) {
	s.toks = append(s.toks, Token{Kind: k, Text: s.src[start:end], Start: start, End: end})
}

func (s *scanner) scan() []Token {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == ' ' || c == '\t' || c == '\r':
			s.pos++ // '\r' (incl. the CR of a CRLF) is inter-word whitespace
		case c == '\\' && s.pos+1 < len(s.src) && s.src[s.pos+1] == '\n':
			s.pos += 2 // line continuation acts as whitespace
		case c == '\n':
			s.emit(KindNewline, s.pos, s.pos+1)
			s.pos++
			s.atCommandStart = true
		case c == ';':
			s.emit(KindSemicolon, s.pos, s.pos+1)
			s.pos++
			s.atCommandStart = true
		case c == '#' && s.atCommandStart:
			s.scanComment()
		default:
			s.scanWord()
			s.atCommandStart = false
		}
	}
	s.emit(KindEOF, s.pos, s.pos)
	return s.toks
}

func (s *scanner) scanWord() {
	start := s.pos
	switch s.src[s.pos] {
	case '{':
		s.scanBraced()
	case '"':
		s.scanQuoted()
	default:
		s.scanBare()
	}
	s.emit(KindWord, start, s.pos)
}

// scanQuoted advances over a "quoted" word to the next unescaped quote outside
// its [command substitutions]. Tolerant of unterminated input.
func (s *scanner) scanQuoted() {
	s.pos++ // opening quote
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\\' && s.pos+1 < len(s.src):
			s.pos += 2
			continue
		case c == '"':
			s.pos++ // closing quote
			return
		case c == '[':
			s.scanBracket() // a quote inside the script does not end the word
			continue
		}
		s.pos++
	}
}

// scanBraced advances over a {braced} word, honoring nesting. Backslash escapes
// the next byte so \{ and \} do not change depth (matches Tcl's rule for locating
// the matching close brace; verified on tclsh 8.6: `set a {\}}` -> value "\}",
// length 2). Tolerant of unterminated input. Nested spans are found once
// (scriptends.go).
func (s *scanner) scanBraced() {
	s.pos = s.ends.skipBraceSpan(s.src, s.pos)
}

// scanComment advances over a comment from '#' to (but not including) newline.
// A backslash-newline continues the comment onto the next physical line.
func (s *scanner) scanComment() {
	start := s.pos
	for s.pos < len(s.src) && s.src[s.pos] != '\n' {
		if s.src[s.pos] == '\\' && s.pos+1 < len(s.src) {
			s.pos += 2
			continue
		}
		s.pos++
	}
	s.emit(KindComment, start, s.pos)
}

// scanBare advances past a bareword, stopping at unescaped word terminators.
func (s *scanner) scanBare() {
	for s.pos < len(s.src) {
		c := s.src[s.pos]
		switch {
		case c == '\\' && s.pos+1 < len(s.src):
			s.pos += 2
		case c == '[':
			s.scanBracket()
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == ';':
			return // '\r' terminates a bareword (CRLF parses like LF)
		default:
			s.pos++
		}
	}
}

// scanBracket advances over a [command substitution] span, backslash-aware. It
// ends the span where Tcl ends it (scriptClose), which knows the quotes, braces,
// indexes and comments that hide a `]`. When that is unknown, the depth count
// ends it if that span is trusted (trustedDepthSpan); otherwise the
// span runs to the end of the input, so no text after a misread `]` is read as
// words or commands.
func (s *scanner) scanBracket() {
	if end, ok := s.ends.scriptClose(s.src, s.pos+1); ok {
		s.pos = end + 1
		return
	}
	if end, ok := s.ends.trustedDepthSpan(s.src, s.pos); ok {
		s.pos = end
		return
	}
	s.pos = len(s.src)
}
