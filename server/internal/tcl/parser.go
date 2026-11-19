package tcl

// WordKind classifies a word by its delimiter.
type WordKind int

const (
	WordBare   WordKind = iota // bareword (may contain $var and [cmd] spans)
	WordBraced                 // {braced} word — opaque literal at this layer
	WordQuoted                 // "quoted" word
)

// Word is one word of a command, with its raw text and byte range [Start,End).
type Word struct {
	Kind  WordKind
	Text  string
	Start int
	End   int
}

// Command is a single TCL command: an ordered list of words.
type Command struct {
	Words []Word
}

// Parse tokenizes src and groups the tokens into commands. Comments are
// discarded; newline and semicolon separate commands; empty commands (from
// blank lines or runs of separators) are omitted.
func Parse(src string) []Command {
	return parseWith(src, nil)
}

// parseWith is Parse, finding script ends through ends (scriptends.go).
func parseWith(src string, ends *scriptEnds) []Command {
	toks := scanWith(src, ends)
	var cmds []Command
	var cur []Word
	flush := func() {
		if len(cur) > 0 {
			cmds = append(cmds, Command{Words: cur})
			cur = nil
		}
	}
	for _, tk := range toks {
		switch tk.Kind {
		case KindWord:
			cur = append(cur, wordFromToken(tk))
		case KindNewline, KindSemicolon, KindEOF:
			flush()
		case KindComment:
			// not part of any command
		}
	}
	return cmds
}

// wordFromToken maps a scanner Token to a Word. Classifying by the first byte is
// safe because Scan guarantees a KindWord token starts with its delimiter ('{'
// or '"') for braced/quoted words, or a non-delimiter byte for bare words.
func wordFromToken(tk Token) Word {
	k := WordBare
	if len(tk.Text) > 0 {
		switch tk.Text[0] {
		case '{':
			k = WordBraced
		case '"':
			k = WordQuoted
		}
	}
	return Word{Kind: k, Text: tk.Text, Start: tk.Start, End: tk.End}
}
