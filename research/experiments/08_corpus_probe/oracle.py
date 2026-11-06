"""Independent definition / call-site / variable oracle over one Tcl file.

FileOracle(path, data_bytes, itcl_imported).run() fills:
  defs, cmdpos, objcalls, varuses, procs, imports, paths, inherit, shape_heads, usage
All offsets are byte offsets."""
import re
from collections import Counter
from tclparse import parse_script, parse_list, literal, inner
from roles import brace_roles, member_words, CLASS_HEADS, SNIT_HEADS, TEST_HEADS
from oracle_util import (worse, qualify, last_seg, base_name, scan_subst, list_names,
                         bindings_of)

PROCLIKE = re.compile(r'(^|::|_)proc$', re.I)
OBJHEAD = re.compile(r'^\$(\{[^}]+\}|[A-Za-z0-9_:]+)$')


class Ctx:
    __slots__ = ('ns', 'frame', 'proc', 'cls', 'chain', 'conf', 'loop', 'test', 'bracket', 'callback')

    def __init__(self, **kw):
        for k in self.__slots__:
            setattr(self, k, kw.get(k))

    def w(self, **kw):
        c = Ctx(**{k: getattr(self, k) for k in self.__slots__})
        for k, v in kw.items():
            setattr(c, k, v)
        return c


def rvt_mask(b):
    """Blank everything outside <? ?> (keeping newlines) so offsets are preserved."""
    out = bytearray(10 if ch == 10 else 32 for ch in b)
    i = 0
    while True:
        a = b.find(b'<?', i)
        if a < 0:
            break
        e = b.find(b'?>', a + 2)
        e = len(b) if e < 0 else e
        st = a + 2
        if st < len(b) and b[st:st + 1] == b'=':
            st += 1
        out[st:e] = b[st:e]
        i = e + 2
    return bytes(out)


class FileOracle:
    def __init__(self, path, data, itcl_imported=False):
        self.path = path
        if path.endswith('.rvt'):
            data = rvt_mask(data)
        self.s = data.decode('latin-1')
        self.itcl = itcl_imported
        self.defs, self.cmdpos, self.objcalls, self.varuses = [], [], [], []
        self.procs = {}
        self.imports, self.paths, self.inherit = [], [], {}
        self.shape_heads = Counter()
        self.usage = Counter()
        self.file_typed = {}

    def run(self):
        ns0 = '::request' if self.path.endswith('.rvt') else '::'
        cmds, _ = parse_script(self.s, 0, len(self.s))
        self.walk(cmds, Ctx(ns=ns0, frame='ns', proc=None, cls=None, chain=(), conf='script',
                            loop=False, test=False, bracket=False, callback=False))
        return self

    # ------------------------------------------------------------------
    def add_def(self, form, name, fq, start, end, ctx, models, cmd, cls=None):
        self.defs.append({'form': form, 'name': name, 'fq': fq, 'start': start, 'end': end,
                          'cmd_start': cmd.start, 'chain': list(ctx.chain[-4:]), 'ns': ctx.ns,
                          'frame': ctx.frame, 'conf': ctx.conf, 'models': models,
                          'cls': cls if cls is not None else ctx.cls})

    def new_proc(self, name, fq, cmd, ctx, params_word, cls, kind):
        pid = len(self.procs) + 1
        p = {'id': pid, 'name': name, 'fq': fq, 'kind': kind, 'start': cmd.start, 'end': cmd.end,
             'cls': cls, 'bindings': {}, 'links': set(), 'static_links': set(), 'typed': {}, 'ns': ctx.ns}
        self.procs[pid] = p
        if params_word is not None:
            for nm, _, _ in list_names(self.s, params_word):
                p['bindings'].setdefault(nm, set()).add('param')
        return pid

    def walk(self, cmds, ctx):
        for c in cmds:
            try:
                self.command(c, ctx)
            except RecursionError:
                pass

    def walk_body(self, w, ctx):
        a, b = inner(w)
        cmds, _ = parse_script(self.s, a, b)
        self.walk(cmds, ctx)

    # ------------------------------------------------------------------
    def command(self, c, ctx):
        s, words = self.s, c.words
        h = literal(s, words[0]) if words[0].kind == 'bare' else None
        n = len(words)
        frame = ctx.frame
        # --- command-position name -------------------------------------
        if h:
            pk = 'bracket' if ctx.bracket else 'command-start'
            if frame in ('class', 'snit', 'oo'):
                pk = 'class-member'
            self.cmdpos.append(self._pos(h, words[0], pk, ctx))
            if h in ('list',) and n >= 2 and words[1].kind == 'bare' and literal(s, words[1]):
                self.cmdpos.append(self._pos(literal(s, words[1]), words[1],
                                             'callback-list' if ctx.callback else 'list-arg', ctx))
            if h == 'my' and n >= 2 and literal(s, words[1]):
                self.objcalls.append(self._obj('my', words[1], ctx))
        m = OBJHEAD.match(s[words[0].start:words[0].end]) if words[0].kind == 'bare' else None
        if m and n >= 2 and words[1].kind == 'bare' and literal(s, words[1]):
            self.objcalls.append(self._obj(m.group(1).strip('{}'), words[1], ctx))
        # --- definitions / bindings ---------------------------------------
        skip = None
        if frame in ('class', 'snit', 'oo'):
            self.member_defs(c, h, ctx)
        else:
            skip = self.defs_of(c, h, ctx)
            if ctx.proc and ctx.conf != 'data' and h:
                p = self.procs[ctx.proc]
                for nm, form in bindings_of(s, words, h):
                    p['bindings'].setdefault(nm, set()).add(form)
                    if form in ('global', 'upvar', 'upvar-static', 'variable', 'namespace upvar'):
                        p['links'].add(nm)
                    if form in ('global', 'upvar-static', 'variable'):
                        p['static_links'].add(nm)
                if h == 'set' and n == 3 and words[2].kind == 'bare':
                    t = s[words[2].start:words[2].end]
                    mm = re.match(r'^\[\s*([A-Za-z0-9_:]+)', t)
                    if mm:
                        p['typed'][base_name(literal(s, words[1]) or '') or ''] = mm.group(1)
            elif h == 'set' and n == 3 and words[2].kind == 'bare':
                mm = re.match(r'^\[\s*([A-Za-z0-9_:]+)', s[words[2].start:words[2].end])
                if mm and literal(s, words[1]):
                    self.file_typed[literal(s, words[1])] = mm.group(1)
        # --- words ------------------------------------------------------------
        roles, cbs = brace_roles(s, words, h, frame, self.itcl)
        for i, w in enumerate(words):
            if skip and i in skip:
                continue
            if i in cbs:
                kind = cbs[i]
                if not kind.startswith('~'):
                    t = literal(s, w)
                    if t and w.kind == 'bare':
                        self.cmdpos.append(self._pos(t, w, 'callback:' + kind, ctx))
                        continue
                kind = kind.lstrip('~')
                self.scan_word(w, ctx.w(callback=True, chain=ctx.chain + ('cb:' + kind,)))
                continue
            if w.kind != 'braced':
                self.scan_word(w, ctx)
                continue
            role, tag = roles.get(i, ('unknown-other', 'unknown-arg:%s' % h))
            self.braced(c, h, i, w, role, tag, ctx)

    def _pos(self, name, w, pk, ctx):
        return {'name': name, 'start': w.start, 'end': w.end, 'pk': pk, 'chain': list(ctx.chain[-4:]),
                'ns': ctx.ns, 'cls': ctx.cls, 'proc': ctx.proc, 'conf': ctx.conf,
                'test': bool(ctx.test), 'frame': ctx.frame}

    def _obj(self, recv, w, ctx):
        d = self._pos(literal(self.s, w), w, 'objmethod', ctx)
        d['recv'] = recv
        return d

    def scan_word(self, w, ctx):
        a, b = (w.start + 1, w.end - 1) if w.kind == 'quoted' else (w.start, w.end)
        tag = 'in-quoted' if w.kind == 'quoted' else None
        self._scan(a, b, ctx, False, tag)

    def _scan(self, a, b, ctx, expr_mode, tag):
        def on_var(name, st, en):
            if ctx.proc and ctx.conf != 'data':
                chain = list(ctx.chain[-3:])
                tags = [t for t in (tag, 'in-expr' if expr_mode else None,
                                    'in-bracket' if ctx.bracket else None) if t]
                self.varuses.append({'name': name, 'start': st, 'end': en, 'proc': ctx.proc,
                                     'tags': tags, 'chain': chain, 'loop': bool(ctx.loop),
                                     'array': st < len(self.s) and en < len(self.s) and self.s[en:en + 1] == '('})

        def on_br(cmds):
            self.walk(cmds, ctx.w(bracket=True))
        scan_subst(self.s, a, b, on_var, on_br, expr_mode)

    # ------------------------------------------------------------------
    def braced(self, c, h, i, w, role, tag, ctx):
        s, words = self.s, c.words
        ch = ctx.chain + (tag,)
        if role in ('script', 'test-body', 'callback'):
            self.walk_body(w, ctx.w(chain=ch, bracket=False, test=ctx.test or role == 'test-body',
                                    callback=role == 'callback'))
        elif role == 'loop':
            self.walk_body(w, ctx.w(chain=ch, bracket=False, loop=True))
        elif role == 'unknown-trailing':
            self.walk_body(w, ctx.w(chain=ch, bracket=False, conf=worse(ctx.conf, 'heuristic')))
        elif role in ('data', 'unknown-other'):
            self.walk_body(w, ctx.w(chain=ch, bracket=False, conf='data'))
        elif role == 'expr':
            a, b = inner(w)
            self._scan(a, b, ctx.w(chain=ch, bracket=True), True, None)
        elif role == 'switch-arms':
            a, b = inner(w)
            items = parse_list(s, a, b)
            for k in range(0, len(items) - 1, 2):
                pat = items[k]
                t = literal(s, pat)
                if pat.kind == 'bare' and t and t != 'default':
                    self.cmdpos.append(self._pos(t, pat, 'switch-pattern', ctx.w(chain=ch)))
            for k in range(1, len(items), 2):
                if items[k].kind == 'braced':
                    self.walk_body(items[k], ctx.w(chain=ch + ('switch-arm',), bracket=False))
        elif role == 'proc-body':
            name = literal(s, words[1]) or '?'
            fq = qualify(name, ctx.ns)
            ns = ctx.ns if '::' not in name else (fq.rsplit('::', 1)[0] or '::')
            pid = self.new_proc(name, fq, c, ctx, words[2], ctx.cls, 'proc')
            self.walk_body(w, ctx.w(ns=ns, frame='proc', proc=pid, chain=ch[:-1] + ('proc:' + fq,),
                                    loop=False, bracket=False))
        elif role == 'ns-body':
            nm = literal(s, words[2]) or '?'
            ns = qualify(nm, ctx.ns)
            self.walk_body(w, ctx.w(ns=ns, frame='ns', proc=None, chain=ch[:-1] + ('ns:' + ns,),
                                    loop=False, bracket=False))
        elif role in ('class-body', 'snit-body', 'oo-body'):
            fr = {'class-body': 'class', 'snit-body': 'snit', 'oo-body': 'oo'}[role]
            if tag == 'oo-self':
                cls = ctx.cls
            else:
                idx = 2 if h in ('oo::class', '::oo::class') else 1
                cls = qualify(literal(s, words[idx]) or '?', ctx.ns)
            self.walk_body(w, ctx.w(frame=fr, cls=cls, proc=None, chain=ch[:-1] + (fr + ':' + cls,),
                                    bracket=False))
        elif role == 'method-body':
            cls, name, pw = ctx.cls, tag, None
            if h in ('itcl::body', '::itcl::body', 'itcl::configbody', '::itcl::configbody'):
                full = literal(s, words[1]) or '?'
                cls = qualify(full.rsplit('::', 1)[0], ctx.ns) if '::' in full.lstrip(':') else ctx.cls
                name = last_seg(full)
                pw = words[2] if n_(words) >= 4 else None
            elif h in ('oo::define', '::oo::define', 'oo::objdefine', '::oo::objdefine'):
                cls = qualify(literal(s, words[1]) or '?', ctx.ns)
                name = literal(s, words[3]) if len(words) >= 6 else literal(s, words[2])
                pw = words[i - 1] if words[i - 1].kind == 'braced' else None
            else:
                off = member_words(s, words) if ctx.frame == 'class' else 0
                mw = words[off:]
                k = literal(s, mw[0])
                if k == 'self' and len(mw) > 1:
                    mw = mw[1:]
                    k = literal(s, mw[0])
                if k in ('method', 'proc', 'typemethod', 'classmethod'):
                    name = literal(s, mw[1]) or '?'
                    pw = mw[2]
                elif k == 'constructor':
                    name, pw = 'constructor', mw[1]
                else:
                    name = k or '?'
            pid = self.new_proc(name, (cls or '?') + '::' + name, c, ctx, pw, cls, 'method')
            self.walk_body(w, ctx.w(frame='proc', proc=pid, cls=cls, loop=False, bracket=False,
                                    chain=ch[:-1] + ('method:%s::%s' % (cls, name),)))
        elif role == 'lambda':
            a, b = inner(w)
            items = parse_list(s, a, b)
            if len(items) >= 2 and items[1].kind == 'braced':
                pid = self.new_proc('<lambda>', '<lambda>', c, ctx, items[0], ctx.cls, 'lambda')
                ns = ctx.ns
                self.walk_body(items[1], ctx.w(frame='proc', proc=pid, ns=ns, loop=False, bracket=False,
                                               chain=ch))
        # params: nothing to walk

    # ------------------------------------------------------------------
    def defs_of(self, c, h, ctx):
        s, words = self.s, c.words
        n = len(words)
        if not h:
            return
        lit = lambda i: literal(s, words[i]) if i < n and words[i].kind == 'bare' else None
        if h in ('proc', '::proc') and n >= 2:
            nm = lit(1)
            if nm:
                self.add_def(h, last_seg(nm), qualify(nm, ctx.ns), words[1].start, words[1].end, ctx,
                             h == 'proc' and n >= 4, c)
            else:
                self.add_def(h + '-dynamic', None, None, words[1].start, words[1].end, ctx, False, c)
            if n == 4 and words[3].kind == 'braced' and '\n' in s[words[3].start:words[3].end]:
                pass
            return
        if n >= 4 and PROCLIKE.search(h) and lit(1) and words[n - 1].kind == 'braced':
            self.add_def('proc-like:' + h, last_seg(lit(1)), qualify(lit(1), ctx.ns),
                         words[1].start, words[1].end, ctx, False, c)
            pid = self.new_proc(lit(1), qualify(lit(1), ctx.ns), c, ctx, words[2], ctx.cls, 'proc-like')
            self.walk_body(words[n - 1], ctx.w(frame='proc', proc=pid, chain=ctx.chain + ('proclike:' + h,),
                                               loop=False, bracket=False))
            return {n - 1, 2}
        for i in range(1, n - 3):
            if lit(i) == 'proc' and lit(i + 1) and words[i + 3].kind == 'braced':
                self.add_def('decorated-proc', last_seg(lit(i + 1)), qualify(lit(i + 1), ctx.ns),
                             words[i + 1].start, words[i + 1].end, ctx, True, c)
                break
        if h in ('namespace', '::namespace') and n >= 3:
            sub = lit(1)
            if sub == 'eval' and lit(2):
                self.add_def('namespace eval', last_seg(lit(2)), qualify(lit(2), ctx.ns),
                             words[2].start, words[2].end, ctx, True, c)
            elif sub == 'ensemble' and lit(2) == 'create':
                self.add_def('namespace ensemble create', last_seg(ctx.ns), ctx.ns, words[0].start,
                             words[0].end, ctx, False, c)
            elif sub == 'export':
                for i in range(2, n):
                    t = lit(i)
                    if t and t != '-clear':
                        self.add_def('namespace export', t, qualify(t, ctx.ns), words[i].start,
                                     words[i].end, ctx, False, c)
            elif sub == 'import':
                for i in range(2, n):
                    t = lit(i)
                    if t and not t.startswith('-'):
                        self.imports.append((ctx.ns, qualify(t, ctx.ns)))
            elif sub == 'path':
                for i in range(2, n):
                    if words[i].kind == 'braced':
                        a, b = inner(words[i])
                        for e in parse_list(s, a, b):
                            self.paths.append((ctx.ns, qualify(s[e.start:e.end], ctx.ns)))
                    elif lit(i):
                        self.paths.append((ctx.ns, qualify(lit(i), ctx.ns)))
            return
        if (h in CLASS_HEADS or (h == 'class' and self.itcl)) and n >= 3 and lit(1):
            self.usage['itcl-class'] += 1
            self.add_def('itcl::class' if h in CLASS_HEADS else 'class(itcl-imported)', last_seg(lit(1)),
                         qualify(lit(1), ctx.ns), words[1].start, words[1].end, ctx, h in CLASS_HEADS, c,
                         cls=qualify(lit(1), ctx.ns))
            return
        if h in ('itcl::body', '::itcl::body', 'itcl::configbody', '::itcl::configbody') and lit(1):
            full = lit(1)
            seg = last_seg(full)
            cls = qualify(full.rsplit('::', 1)[0], ctx.ns) if '::' in full.lstrip(':') else None
            st = words[1].start + len(full) - len(seg)
            self.add_def(h.lstrip(':'), seg, (cls or '?') + '::' + seg, st, words[1].end, ctx,
                         'body' in h, c, cls=cls)
            return
        if h in SNIT_HEADS and lit(1):
            self.usage['snit'] += 1
            self.add_def(h.lstrip(':'), last_seg(lit(1)), qualify(lit(1), ctx.ns), words[1].start,
                         words[1].end, ctx, False, c, cls=qualify(lit(1), ctx.ns))
            return
        if h in ('oo::class', '::oo::class') and n >= 3 and lit(1) == 'create' and lit(2):
            self.usage['tcloo'] += 1
            self.add_def('oo::class create', last_seg(lit(2)), qualify(lit(2), ctx.ns), words[2].start,
                         words[2].end, ctx, False, c, cls=qualify(lit(2), ctx.ns))
            return
        if h in ('oo::define', '::oo::define', 'oo::objdefine', '::oo::objdefine') and n >= 3:
            self.usage['tcloo'] += 1
            cls = qualify(lit(1) or '?', ctx.ns)
            if n >= 4 and lit(2) in ('method', 'classmethod', 'constructor', 'destructor'):
                nm = lit(3) if lit(2) in ('method', 'classmethod') else lit(2)
                wi = 3 if lit(2) in ('method', 'classmethod') else 2
                self.add_def('oo::define-' + lit(2), nm, cls + '::' + (nm or '?'), words[wi].start,
                             words[wi].end, ctx, False, c, cls=cls)
            elif lit(2) == 'superclass':
                self.inherit.setdefault(cls, []).extend(qualify(lit(i), ctx.ns) for i in range(3, n) if lit(i))
            else:
                self.add_def(h.lstrip(':'), last_seg(lit(1) or '?'), cls, words[1].start, words[1].end,
                             ctx, False, c, cls=cls)
            return
        if h == 'interp' and n >= 5 and lit(1) == 'alias' and lit(3) is not None:
            nm = lit(3)
            if nm:
                self.add_def('interp alias', last_seg(nm), qualify(nm, ctx.ns), words[3].start,
                             words[3].end, ctx, False, c)
            return
        if h == 'coroutine' and n >= 3 and lit(1):
            self.add_def('coroutine', last_seg(lit(1)), qualify(lit(1), ctx.ns), words[1].start,
                         words[1].end, ctx, False, c)
            return
        if h == 'apply':
            self.add_def('apply', None, None, words[0].start, words[0].end, ctx, False, c)
            return
        # variables
        if h == 'variable' and n >= 2:
            for k, i in enumerate(range(1, n, 2) if ctx.frame == 'ns' or n > 2 else [1]):
                nm = lit(i)
                if nm and base_name(nm):
                    form = ('ns-variable' if ctx.frame == 'ns' else 'proc-variable-link')
                    if k > 0:
                        form += '-multi'
                    self.add_def(form, last_seg(nm), qualify(nm, ctx.ns), words[i].start,
                                 words[i].start + len(base_name(nm)), ctx, True, c)
            return
        tgt = None
        if h == 'set' and n >= 2:
            tgt = 1
        elif h == 'array' and n >= 3 and lit(1) == 'set':
            tgt = 2
        elif h in ('incr', 'append', 'lappend') and n >= 2 and ctx.frame == 'proc':
            tgt = 1
        if tgt is not None:
            raw = s[words[tgt].start:words[tgt].end] if tgt < n and words[tgt].kind == 'bare' else None
            nm = base_name(raw.split('(', 1)[0]) if raw and '(' in raw else base_name(lit(tgt))
            if not nm:
                return
            if ctx.frame == 'ns' and ctx.conf != 'data':
                self.add_def('ns-' + h if h != 'array' else 'ns-array-set', last_seg(nm),
                             qualify(nm, ctx.ns), words[tgt].start, words[tgt].start + len(nm), ctx, True, c)
            elif ctx.frame == 'proc' and '::' in nm:
                self.add_def('qualified-set-in-proc', last_seg(nm), qualify(nm, ctx.ns), words[tgt].start,
                             words[tgt].start + len(nm), ctx, True, c)
            elif ctx.frame == 'proc' and ctx.proc and nm in self.procs[ctx.proc]['static_links']:
                self.add_def('promoted-link-assign', nm, None, words[tgt].start,
                             words[tgt].start + len(nm), ctx, True, c)
            return
        # recurring definition-shaped commands (discovery histogram)
        if ctx.frame == 'ns' and n == 4 and lit(1) and words[3].kind == 'braced' \
                and '\n' in s[words[3].start:words[3].end] and h not in ('proc', 'namespace', 'if', 'foreach', 'while', 'switch', 'dict', 'catch', 'try'):
            self.shape_heads[h] += 1

    def member_defs(self, c, h, ctx):
        s, words = self.s, c.words
        if not h:
            return
        off = member_words(s, words) if ctx.frame == 'class' else 0
        mod = literal(s, words[0]) if off else None
        w = words[off:]
        n = len(w)
        k = literal(s, w[0]) if w[0].kind == 'bare' else None
        if ctx.frame == 'oo' and k == 'self' and n >= 3:
            w, n = w[1:], n - 1
            k = literal(s, w[0])
        pre = {'class': 'itcl', 'snit': 'snit', 'oo': 'oo'}[ctx.frame]
        models = ctx.frame == 'class'
        if k in ('constructor', 'destructor', 'typeconstructor'):
            self.add_def('%s-%s' % (pre, k), k, ctx.cls + '::' + k, w[0].start, w[0].end, ctx, models, c)
        elif k in ('method', 'proc', 'typemethod', 'classmethod', 'variable', 'common', 'typevariable',
                   'option', 'component', 'forward', 'onconfigure', 'oncget', 'delegate') and n >= 2:
            nm = literal(s, w[1])
            if k == 'delegate' and n >= 3:
                nm = literal(s, w[2])
                wi = 2
            else:
                wi = 1
            if k == 'variable' and ctx.frame == 'oo':
                for i in range(1, n):
                    t = literal(s, w[i])
                    if t:
                        self.add_def('oo-variable', t, ctx.cls + '::' + t, w[i].start, w[i].end, ctx, False, c)
                return
            if nm:
                form = '%s-%s' % (pre, k) + ('(%s)' % mod if mod else '')
                if k == 'option' and nm.startswith('{'):
                    nm = nm.strip('{}').split()[0]
                self.add_def(form, nm, ctx.cls + '::' + nm, w[wi].start, w[wi].end, ctx,
                             models and k in ('method', 'proc', 'variable', 'common'), c)
        elif k in ('inherit', 'superclass'):
            for i in range(1, n):
                t = literal(s, w[i])
                if t:
                    self.inherit.setdefault(ctx.cls, []).append(qualify(t, ctx.ns))


def n_(words):
    return len(words)
