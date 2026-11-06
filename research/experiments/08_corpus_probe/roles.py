"""Classify each word of a Tcl command: which braced words are scripts / exprs /
data, and which bare words are callback command names. Independent of the server.

brace_roles(s, words, head, frame) -> (roles, callbacks)
  roles:     {word_index: (role, tag)}   role in script|loop|expr|data|params|
             proc-body|ns-body|class-body|method-body|snit-body|oo-body|
             callback|test-body|unknown-trailing|unknown-other|lambda|switch-arms
  callbacks: {word_index: kind}           a word holding a command (prefix) name
"""
import re
from tclparse import literal

CB_OPT = re.compile(r'^-(\w*command|server|validatecommand|invalidcommand)$')
DATA_HEADS = {'set', 'list', 'lappend', 'lset', 'linsert', 'lreplace', 'concat', 'array', 'append',
              'incr', 'string', 'global', 'variable', 'common', 'return', 'regexp', 'regsub', 'format',
              'puts', 'lsearch', 'lsort', 'split', 'join', 'binary', 'encoding', 'unset', 'package',
              'source', 'info', 'error', 'throw', 'subst', 'lindex', 'lrange', 'llength', 'scan',
              'upvar', 'dict', 'namespace', 'lassign', 'lmap', 'expr', 'clock', 'file', 'open',
              'interp', 'trace', 'after', 'bind', 'fileevent', 'chan', 'event', 'msgcat::mc', 'mc'}
CLASS_HEADS = ('itcl::class', '::itcl::class')
SNIT_HEADS = ('snit::type', '::snit::type', 'snit::widget', '::snit::widget',
              'snit::widgetadaptor', '::snit::widgetadaptor')
TEST_HEADS = ('test', 'tcltest::test', '::tcltest::test')


def _b(words, i):
    return 0 <= i < len(words) and words[i].kind == 'braced'


def _lit(s, words, i):
    if 0 <= i < len(words) and words[i].kind == 'bare':
        return literal(s, words[i])
    return None


def brace_roles(s, words, head, frame, itcl_imported=False):
    R, CB = {}, {}
    n = len(words)
    last = n - 1
    h = head or ''
    if frame in ('class', 'snit', 'oo'):
        return member_roles(s, words, h, frame)
    if h in ('proc', '::proc') and n >= 4:
        R[2] = ('params', 'proc-params')
        R[3] = ('proc-body', 'proc')
        return R, CB
    if h in ('namespace', '::namespace') and n >= 2:
        sub = _lit(s, words, 1)
        if sub == 'eval' and n >= 4:
            R[last] = ('ns-body', 'namespace-eval')
        elif sub == 'inscope' and n >= 4:
            R[3] = ('script', 'namespace-inscope')
        elif sub == 'code' and n == 3:
            if _b(words, 2):
                R[2] = ('callback', 'namespace-code')
            else:
                CB[2] = 'namespace-code'
        return R, CB
    if h in CLASS_HEADS or (h == 'class' and itcl_imported):
        if n >= 3 and _b(words, last):
            R[last] = ('class-body', 'itcl-class')
        return R, CB
    if h in ('itcl::body', '::itcl::body') and n >= 4:
        R[2] = ('params', 'itcl-body-params')
        R[3] = ('method-body', 'itcl-body')
        return R, CB
    if h in ('itcl::configbody', '::itcl::configbody') and n >= 3:
        R[last] = ('method-body', 'itcl-configbody')
        return R, CB
    if h in SNIT_HEADS and n >= 3:
        R[last] = ('snit-body', 'snit')
        return R, CB
    if h in ('oo::class', '::oo::class') and n >= 4 and _lit(s, words, 1) == 'create':
        R[last] = ('oo-body', 'oo-class')
        return R, CB
    if h in ('oo::define', '::oo::define', 'oo::objdefine', '::oo::objdefine') and n >= 3:
        if n == 3:
            R[2] = ('oo-body', h.lstrip(':'))
        else:
            sub = _lit(s, words, 2)
            if sub in ('method', 'classmethod') and n >= 6:
                R[4] = ('params', 'oo-params')
                R[5] = ('method-body', 'oo-method')
            elif sub == 'constructor' and n >= 5:
                R[3] = ('params', 'oo-params')
                R[4] = ('method-body', 'oo-ctor')
            elif sub == 'destructor' and n >= 4:
                R[3] = ('method-body', 'oo-dtor')
        return R, CB
    if h == 'if':
        i = 1
        while i < n:
            if _b(words, i):
                R[i] = ('expr', 'if-cond')
            i += 1
            if _lit(s, words, i) == 'then':
                i += 1
            if _b(words, i):
                R[i] = ('script', 'if-body')
            i += 1
            t = _lit(s, words, i)
            if t == 'elseif':
                i += 1
                continue
            if t == 'else':
                i += 1
            if _b(words, i):
                R[i] = ('script', 'else-body')
            break
        return R, CB
    if h == 'while' and n >= 3:
        if _b(words, 1):
            R[1] = ('expr', 'while-cond')
        if _b(words, 2):
            R[2] = ('loop', 'while-body')
        return R, CB
    if h == 'for' and n >= 5:
        for i, tg in ((1, 'for-start'), (3, 'for-next')):
            if _b(words, i):
                R[i] = ('loop', tg)
        if _b(words, 2):
            R[2] = ('expr', 'for-cond')
        if _b(words, 4):
            R[4] = ('loop', 'for-body')
        return R, CB
    if h in ('foreach', 'lmap') and n >= 4:
        for i in range(1, last):
            if _b(words, i):
                R[i] = ('data', h + '-list')
        if _b(words, last):
            R[last] = ('loop', h + '-body')
        return R, CB
    if h == 'dict' and n >= 3:
        sub = _lit(s, words, 1)
        if sub in ('for', 'map') and n >= 5:
            if _b(words, 2):
                R[2] = ('data', 'dict-varlist')
            if _b(words, last):
                R[last] = ('loop', 'dict-' + sub)
        elif sub in ('with', 'update') and _b(words, last):
            R[last] = ('script', 'dict-' + sub)
        return R, CB
    if h == 'catch' and n >= 2:
        if _b(words, 1):
            R[1] = ('script', 'catch-body')
        return R, CB
    if h == 'try' and n >= 2:
        if _b(words, 1):
            R[1] = ('script', 'try-body')
        i = 2
        while i < n:
            t = _lit(s, words, i)
            if t in ('on', 'trap') and i + 3 < n:
                if _b(words, i + 3):
                    R[i + 3] = ('script', 'try-handler')
                i += 4
            elif t == 'finally' and i + 1 < n:
                if _b(words, i + 1):
                    R[i + 1] = ('script', 'try-finally')
                i += 2
            else:
                i += 1
        return R, CB
    if h == 'switch' and n >= 3:
        i = 1
        while i < n:
            t = _lit(s, words, i)
            if t is None or not t.startswith('-'):
                break
            if t == '--':
                i += 1
                break
            i += 2 if t in ('-matchvar', '-indexvar') else 1
        rest = list(range(i + 1, n))
        if len(rest) == 1 and _b(words, rest[0]):
            R[rest[0]] = ('switch-arms', 'switch')
        else:
            for k in rest[1::2]:
                if _b(words, k):
                    R[k] = ('script', 'switch-arm')
        return R, CB
    if h in ('eval', 'time'):
        for i in range(1, n if h == 'eval' else 2):
            if _b(words, i):
                R[i] = ('script', h + '-body')
        return R, CB
    if h == 'uplevel' and n >= 2:
        t = _lit(s, words, 1)
        start = 2 if (t and (t.isdigit() or t.startswith('#'))) else 1
        for i in range(start, n):
            if _b(words, i):
                R[i] = ('script', 'uplevel-body')
        return R, CB
    if h == 'apply' and n >= 2 and _b(words, 1):
        R[1] = ('lambda', 'apply')
        return R, CB
    if h == 'expr':
        for i in range(1, n):
            if _b(words, i):
                R[i] = ('expr', 'expr')
        return R, CB
    if h == 'after' and n >= 3:
        t = _lit(s, words, 1)
        if t in ('cancel', 'info'):
            return R, CB
        k = 2
        if k < n:
            _cb(s, words, k, 'after', R, CB)
        return R, CB
    if h in ('fileevent',) and n >= 4:
        _cb(s, words, 3, 'fileevent', R, CB)
        return R, CB
    if h == 'chan' and n >= 5 and _lit(s, words, 1) == 'event':
        _cb(s, words, 4, 'chan-event', R, CB)
        return R, CB
    if h == 'trace' and n >= 5:
        _cb(s, words, last, 'trace', R, CB)
        return R, CB
    if h == 'bind' and n >= 4:
        _cb(s, words, 3, 'bind', R, CB)
        return R, CB
    if h == 'interp' and n >= 5 and _lit(s, words, 1) == 'alias':
        CB[4] = 'interp-alias-target'
        return R, CB
    if h == 'coroutine' and n >= 3:
        CB[2] = 'coroutine-target'
        return R, CB
    if h in TEST_HEADS and n >= 4:
        opt = any((_lit(s, words, i) or '').startswith('-') for i in range(3, n))
        if opt:
            i = 3
            while i + 1 < n:
                t = _lit(s, words, i) or ''
                if t in ('-body', '-setup', '-cleanup') and _b(words, i + 1):
                    R[i + 1] = ('test-body', 'test' + t)
                elif _b(words, i + 1):
                    R[i + 1] = ('data', 'test' + t)
                i += 2
        else:
            bi = 3 if n == 5 else 4 if n >= 6 else None
            if bi is not None and _b(words, bi):
                R[bi] = ('test-body', 'test-body')
            for i in range(3, n):
                if i != bi and _b(words, i):
                    R[i] = ('data', 'test-arg')
        return R, CB
    # option-value callbacks (Tk -command, socket -server, lsort -command, ...)
    for i in range(1, n - 1):
        t = _lit(s, words, i)
        if t and CB_OPT.match(t):
            _cb(s, words, i + 1, 'opt' + t, R, CB)
    data_head = h in DATA_HEADS
    for i in range(1, n):
        if i in R or not _b(words, i):
            continue
        if data_head:
            R[i] = ('data', 'data:' + h)
        elif i == last:
            R[i] = ('unknown-trailing', 'unknown:' + h)
        else:
            R[i] = ('unknown-other', 'unknown-arg:' + h)
    return R, CB


def _cb(s, words, i, kind, R, CB):
    if i >= len(words):
        return
    if words[i].kind == 'braced':
        R[i] = ('callback', 'cb:' + kind)
    elif literal(s, words[i]) is not None and words[i].kind == 'bare':
        CB[i] = kind
    else:
        CB[i] = '~' + kind   # substituted word, e.g. [list name ...]: walk with ctx.callback


def member_words(s, words):
    if len(words) >= 2 and literal(s, words[0]) in ('public', 'protected', 'private'):
        return 1
    return 0


def member_roles(s, words, h, frame):
    """Roles inside itcl / snit / TclOO class-definition bodies."""
    R, CB = {}, {}
    off = member_words(s, words) if frame == 'class' else 0
    w = words[off:]
    n = len(w)
    k = literal(s, w[0]) if n else None
    if frame == 'oo' and k == 'self' and n >= 2:
        if n == 2 and w[1].kind == 'braced':
            R[off + 1] = ('oo-body', 'oo-self')
            return R, CB
        off += 1
        w = words[off:]
        n = len(w)
        k = literal(s, w[0]) if n else None
    def body(i, tag):
        if 0 <= i < n and w[i].kind == 'braced':
            R[off + i] = ('method-body', tag)
    if k in ('method', 'proc', 'typemethod', 'classmethod') and n >= 4:
        R[off + 2] = ('params', k + '-params')
        body(3, frame + '-' + k)
    elif k == 'constructor':
        if n >= 3:
            R[off + 1] = ('params', 'ctor-params')
        if n >= 4 and w[2].kind == 'braced':
            R[off + 2] = ('method-body', 'ctor-init')
        body(n - 1, frame + '-constructor')
    elif k in ('destructor', 'typeconstructor') and n >= 2:
        body(n - 1, frame + '-' + k)
    elif k in ('onconfigure',) and n >= 4:
        body(3, 'snit-onconfigure')
    elif k in ('oncget',) and n >= 3:
        body(2, 'snit-oncget')
    elif k == 'variable' and frame == 'class' and n >= 4:
        body(3, 'itcl-configbody')
    else:
        for i in range(1, n):
            if w[i].kind == 'braced':
                R[off + i] = ('data', 'member-data:%s' % k)
    return R, CB
