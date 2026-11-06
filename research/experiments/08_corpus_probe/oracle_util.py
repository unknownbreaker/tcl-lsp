"""Helpers for the oracle walker: variable scanning and proc-local binding forms."""
from tclparse import parse_script, parse_list, match_brace, literal, inner

CONF_RANK = {'script': 0, 'heuristic': 1, 'data': 2}


def worse(a, b):
    return a if CONF_RANK[a] >= CONF_RANK[b] else b


def qualify(name, ns):
    if name.startswith('::'):
        return name
    return '::' + name if ns == '::' else ns + '::' + name


def last_seg(name):
    i = name.rfind('::')
    return name if i < 0 else name[i + 2:]


def ns_of(fq):
    i = fq.rfind('::')
    return '::' if i <= 0 else fq[:i]


def base_name(t):
    """Variable base name of a target like arr(x); None if not a plain name."""
    if t is None or t == '':
        return None
    p = t.find('(')
    if p == 0:
        return None
    b = t if p < 0 else t[:p]
    if any(c in b for c in ' \t\n{}"$[]'):
        return None
    return b


def is_name_char(c):
    return c.isalnum() or c == '_'


def scan_subst(s, a, b, on_var, on_bracket, expr_mode=False):
    """Scan s[a:b] as substitutable text. on_var(name, start, end, in_index) for
    each $ref (start/end span the name without '$'); on_bracket(cmds) for each
    [script]. In expr_mode, {...} spans are literal and skipped."""
    i = a
    while i < b:
        c = s[i]
        if c == '\\':
            i += 2
            continue
        if c == '{' and expr_mode:
            i = match_brace(s, i, b)
            continue
        if c == '[':
            cmds, k = parse_script(s, i + 1, b, True)
            on_bracket(cmds)
            i = k + 1
            continue
        if c == '$':
            j = i + 1
            if j < b and s[j] == '{':
                k = s.find('}', j, b)
                if k < 0:
                    return
                on_var(s[j + 1:k], j + 1, k)
                i = k + 1
                continue
            while j < b:
                if is_name_char(s[j]):
                    j += 1
                elif s[j] == ':' and j + 1 < b and s[j + 1] == ':':
                    j += 2
                else:
                    break
            if j == i + 1:
                i += 1
                continue
            on_var(s[i + 1:j], i + 1, j)
            if j < b and s[j] == '(':
                # scan the index for nested refs, up to the matching ')'
                depth, k = 0, j
                while k < b:
                    if s[k] == '\\':
                        k += 2
                        continue
                    if s[k] == '[':
                        _, kk = parse_script(s, k + 1, b, True)
                        k = kk + 1
                        continue
                    if s[k] == '(':
                        depth += 1
                    elif s[k] == ')':
                        depth -= 1
                        if depth == 0:
                            break
                    k += 1
                scan_subst(s, j + 1, min(k, b), on_var, on_bracket, False)
                i = k + 1
            else:
                i = j
            continue
        i += 1


def list_names(s, w):
    """Names in a varlist word ({a b} or a): list of (name, start, end)."""
    if w.kind == 'braced':
        a, b = inner(w)
        out = []
        for e in parse_list(s, a, b):
            if e.kind == 'braced':        # {name default} in a params list
                ea, eb = inner(e)
                sub = parse_list(s, ea, eb)
                if sub:
                    out.append((s[sub[0].start:sub[0].end], sub[0].start, sub[0].end))
            elif e.kind == 'bare':
                out.append((s[e.start:e.end], e.start, e.end))
        return out
    t = literal(s, w)
    if w.kind == 'bare' and t:
        return [(t, w.start, w.end)]
    return []


def _opts(s, words, i):
    while i < len(words):
        t = literal(s, words[i])
        if t is None or not t.startswith('-'):
            break
        i += 1
        if t == '--':
            break
    return i


def bindings_of(s, words, h):
    """Proc-local binding targets of one command: list of (name, form)."""
    n = len(words)
    out = []

    def tgt(i, form):
        if 0 <= i < n and words[i].kind == 'bare':
            raw = s[words[i].start:words[i].end]
            b = base_name(raw.split('(', 1)[0]) if '(' in raw else base_name(literal(s, words[i]))
            if b:
                out.append((b, form))

    if h in ('set', 'incr', 'append', 'lappend', 'lset') and n >= 2:
        tgt(1, h)
    elif h == 'variable':
        for i in range(1, n, 2):
            tgt(i, 'variable')
    elif h == 'global':
        for i in range(1, n):
            tgt(i, 'global')
    elif h == 'upvar' and n >= 3:
        t = literal(s, words[1]) or ''
        st = 2 if (t.isdigit() or t.startswith('#')) else 1
        for i in range(st + 1, n, 2):
            other = literal(s, words[i - 1]) or ''
            static = (st == 2 and t == '#0' and base_name(other)) or other.startswith('::')
            tgt(i, 'upvar-static' if static else 'upvar')
    elif h in ('foreach', 'lmap') and n >= 4:
        for i in range(1, n - 1, 2):
            for nm, _, _ in list_names(s, words[i]):
                out.append((base_name(nm) or nm, h))
    elif h == 'lassign':
        for i in range(2, n):
            tgt(i, 'lassign')
    elif h == 'dict' and n >= 3:
        sub = literal(s, words[1])
        if sub in ('set', 'unset', 'lappend', 'incr', 'append', 'update', 'with'):
            tgt(2, 'dict ' + sub)
        if sub in ('for', 'map'):
            for nm, _, _ in list_names(s, words[2]):
                out.append((nm, 'dict ' + sub))
        if sub == 'update':
            for i in range(4, n - 1, 2):
                tgt(i, 'dict update')
    elif h == 'gets' and n >= 3:
        tgt(2, 'gets')
    elif h == 'chan' and n >= 4 and literal(s, words[1]) == 'gets':
        tgt(3, 'gets')
    elif h == 'regexp':
        i = _opts(s, words, 1)
        for k in range(i + 2, n):
            tgt(k, 'regexp')
    elif h == 'regsub':
        i = _opts(s, words, 1)
        tgt(i + 3, 'regsub')
    elif h == 'scan':
        for k in range(3, n):
            tgt(k, 'scan')
    elif h == 'binary' and n >= 5 and literal(s, words[1]) == 'scan':
        for k in range(4, n):
            tgt(k, 'binary scan')
    elif h == 'array' and n >= 3 and literal(s, words[1]) == 'set':
        tgt(2, 'array set')
    elif h == 'catch':
        tgt(2, 'catch')
        tgt(3, 'catch')
    elif h == 'try':
        i = 2
        while i < n:
            t = literal(s, words[i])
            if t in ('on', 'trap') and i + 2 < n:
                for nm, _, _ in list_names(s, words[i + 2]):
                    out.append((nm, 'try-on'))
                i += 4
            else:
                i += 1
    elif h == 'namespace' and n >= 5 and literal(s, words[1]) == 'upvar':
        for k in range(4, n, 2):
            tgt(k, 'namespace upvar')
    elif h == 'info' and n >= 4 and literal(s, words[1]) in ('exists',):
        pass
    elif h in ('vwait',):
        pass
    elif h == 'trace':
        pass
    elif h in ('open', 'socket'):
        pass
    return out
