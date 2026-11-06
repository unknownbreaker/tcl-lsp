"""Independent, lenient Tcl script parser (Dodekalogue word rules) for the harness.

Operates on a str decoded as latin-1 so every index is a BYTE offset into the file.
Produces commands -> words; bare/quoted words carry their [command substitutions]
already parsed (subs). Braced words are left unparsed; the walker decides whether
a braced word is a script, an expression, or data."""

WS = ' \t\r\f\v'


class Word:
    __slots__ = ('kind', 'start', 'end', 'subs')

    def __init__(self, kind, start, end, subs):
        self.kind = kind      # 'bare' | 'braced' | 'quoted'
        self.start = start
        self.end = end
        self.subs = subs      # list of (inner_start, inner_end, [Cmd])


class Cmd:
    __slots__ = ('words', 'start', 'end')

    def __init__(self, words, start, end):
        self.words = words
        self.start = start
        self.end = end


def match_brace(s, i, n):
    """s[i] == '{'. Return index just past the matching '}' (or n)."""
    depth = 0
    while i < n:
        c = s[i]
        if c == '\\':
            i += 2
            continue
        if c == '{':
            depth += 1
        elif c == '}':
            depth -= 1
            if depth == 0:
                return i + 1
        i += 1
    return n


def _var(s, i, n, subs):
    """s[i] == '$'. Skip a variable reference; returns index after it."""
    j = i + 1
    if j < n and s[j] == '{':
        k = s.find('}', j)
        return n if k < 0 else k + 1
    while j < n:
        c = s[j]
        if c.isalnum() or c == '_':
            j += 1
        elif c == ':' and j + 1 < n and s[j + 1] == ':':
            j += 2
            while j < n and s[j] == ':':
                j += 1
        else:
            break
    if j < n and s[j] == '(' and j > i + 1:
        j += 1
        while j < n and s[j] != ')':
            c = s[j]
            if c == '\\':
                j += 2
            elif c == '[':
                cmds, k = parse_script(s, j + 1, n, True)
                subs.append((j + 1, k, cmds))
                j = k + 1
            elif c == '$':
                j = _var(s, j, n, subs)
            else:
                j += 1
        j += 1
    return j


def scan_bare(s, i, n, bracket):
    subs = []
    while i < n:
        c = s[i]
        if c in WS or c == '\n' or c == ';':
            break
        if bracket and c == ']':
            break
        if c == '\\':
            if i + 1 < n and s[i + 1] == '\n':
                break
            i += 2
            continue
        if c == '[':
            cmds, k = parse_script(s, i + 1, n, True)
            subs.append((i + 1, k, cmds))
            i = k + 1
            continue
        if c == '$':
            i = _var(s, i, n, subs)
            continue
        i += 1
    return min(i, n), subs


def scan_quoted(s, i, n):
    """i is just past the opening quote. Returns index past closing quote."""
    subs = []
    while i < n:
        c = s[i]
        if c == '\\':
            i += 2
            continue
        if c == '"':
            return i + 1, subs
        if c == '[':
            cmds, k = parse_script(s, i + 1, n, True)
            subs.append((i + 1, k, cmds))
            i = k + 1
            continue
        if c == '$':
            i = _var(s, i, n, subs)
            continue
        i += 1
    return n, subs


def _is_sep(s, j, n, bracket):
    if j >= n:
        return True
    c = s[j]
    return c in WS or c == '\n' or c == ';' or (bracket and c == ']') or (c == '\\' and j + 1 < n and s[j + 1] == '\n')


def parse_word(s, i, n, bracket):
    c = s[i]
    if c == '{':
        j = match_brace(s, i, n)
        if _is_sep(s, j, n, bracket):
            return Word('braced', i, j, None), j
        j2, subs = scan_bare(s, j, n, bracket)     # lenient: `{a}b`
        return Word('bare', i, j2, subs), j2
    if c == '"':
        j, subs = scan_quoted(s, i + 1, n)
        if not _is_sep(s, j, n, bracket):
            j2, subs2 = scan_bare(s, j, n, bracket)
            return Word('bare', i, j2, subs + subs2), j2
        return Word('quoted', i, j, subs), j
    j, subs = scan_bare(s, i, n, bracket)
    if j == i:  # defensive: never stall
        j = i + 1
    return Word('bare', i, j, subs), j


def parse_script(s, i, n, bracket=False):
    """Parse s[i:n]. In bracket mode stop at the unmatched ']' and return its index."""
    cmds = []
    while i < n:
        c = s[i]
        if c in WS or c == '\n' or c == ';':
            i += 1
            continue
        if c == '\\' and i + 1 < n and s[i + 1] == '\n':
            i += 2
            continue
        if bracket and c == ']':
            return cmds, i
        if c == '#':
            while i < n and s[i] != '\n':
                i += 2 if s[i] == '\\' else 1
            continue
        words = []
        cstart = i
        while i < n:
            c = s[i]
            if c in WS:
                i += 1
                continue
            if c == '\\' and i + 1 < n and s[i + 1] == '\n':
                i += 2
                continue
            if c == '\n' or c == ';' or (bracket and c == ']'):
                break
            w, i = parse_word(s, i, n, bracket)
            words.append(w)
        if words:
            cmds.append(Cmd(words, cstart, words[-1].end))
    return cmds, min(i, n)


def parse_list(s, i, n):
    """Split s[i:n] as a Tcl list (newlines are plain whitespace, no comments)."""
    out = []
    while i < n:
        c = s[i]
        if c in WS or c == '\n':
            i += 1
            continue
        if c == '{':
            j = match_brace(s, i, n)
            out.append(Word('braced', i, j, None))
            i = j
        elif c == '"':
            j, subs = scan_quoted(s, i + 1, n)
            out.append(Word('quoted', i, j, subs))
            i = j
        else:
            j = i
            while j < n and s[j] not in WS and s[j] != '\n':
                j += 2 if s[j] == '\\' else 1
            out.append(Word('bare', i, min(j, n), []))
            i = j
    return out


def text(s, w):
    return s[w.start:w.end]


def inner(w):
    """(start, end) of a braced/quoted word's interior."""
    if w.kind in ('braced', 'quoted'):
        return w.start + 1, max(w.start + 1, w.end - 1)
    return w.start, w.end


def literal(s, w):
    """Static text of a word, or None if it involves substitution."""
    if w.kind == 'braced':
        a, b = inner(w)
        return s[a:b]
    t = s[w.start:w.end]
    if w.kind == 'quoted':
        t = t[1:-1]
    if '$' in t or '[' in t or '\\' in t:
        return None
    return t
