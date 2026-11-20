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
	// srcHead is the head as written (with any `{*}` before it), set only when
	// controlHead spelled it bare. A call reference names what the source names:
	// `::while` is the global builtin even in a namespace with its own `while`.
	srcHead []Word
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
			words, src := controlHead(cur)
			cmds = append(cmds, Command{Words: words, srcHead: src})
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

// controlHead returns w with a literal if, while or for head spelled bare. Tcl names
// a command by its word's value, so `"if"`, `{while}`, `::for` and an expanded
// single literal (`{*}{if}`, `{*}::while`) run the same builtin as the bare word
// (#117). Rewriting the head here decides it once, so reaching, references, folding
// and every word-index check see one shape. The bare word keeps the head's span,
// and a `{*}` before it is dropped: expanding one literal word is that word. src
// is the head as written when it was rewritten, else nil (Command.srcHead).
func controlHead(w []Word) (words, src []Word) {
	name, h, ok := globalHead(w)
	if !ok || name != "if" && name != "while" && name != "for" {
		return w, nil
	}
	if h == 0 && w[0].Kind == WordBare && w[0].Text == name {
		return w, nil
	}
	head := Word{Kind: WordBare, Text: name, Start: w[h].Start, End: w[h].End}
	return append([]Word{head}, w[h+1:]...), w[:h+1]
}

// sourceHead returns c's first word as written, before controlHead.
func (c Command) sourceHead() Word {
	if len(c.srcHead) > 0 {
		return c.srcHead[0]
	}
	return c.Words[0]
}
