"""Repo loading, oracle aggregation, name tables and classification helpers."""
import os
import re
from collections import defaultdict
from oracle import FileOracle
from oracle_util import qualify, last_seg
from posutil import LineIndex
from lspclient import uri_to_path

CALLABLE_PREFIX = ('proc', '::proc', 'decorated-proc', 'proc-like:', 'itcl-method', 'itcl-proc',
                   'itcl::body', 'snit-method', 'snit-typemethod', 'snit-proc', 'oo-method',
                   'oo-classmethod', 'oo::define-method', 'interp alias', 'coroutine', 'ws-symbol')
METHOD_PREFIX = ('itcl-method', 'itcl-proc', 'itcl::body', 'snit-method', 'snit-typemethod',
                 'oo-method', 'oo-classmethod', 'oo::define-method', 'ws-method')
SKIP_EXT = ('.test', '.tm', '.itcl', '.tk', '.in')
SHEBANG = re.compile(rb'^#!.*\b(tclsh|wish|expect)|exec\s+(tclsh|wish)', re.M)


CALLABLE_EXACT = {'proc', '::proc', 'decorated-proc', 'itcl::body', 'interp alias', 'coroutine',
                  'ws-symbol', 'ws-method', 'oo::define-method', 'oo::define-classmethod'}


def _base_form(form):
    return form.split('(')[0]   # strip itcl access modifier suffix, e.g. itcl-method(protected)


def is_callable(form):
    f = _base_form(form)
    return f in CALLABLE_EXACT or f.startswith('proc-like:') or f in (
        'itcl-method', 'itcl-proc', 'snit-method', 'snit-typemethod', 'snit-proc', 'oo-method', 'oo-classmethod')


def is_method(form):
    f = _base_form(form)
    return f in ('itcl-method', 'itcl-proc', 'itcl::body', 'snit-method', 'snit-typemethod', 'oo-method',
                 'oo-classmethod', 'oo::define-method', 'oo::define-classmethod', 'ws-method')


class Repo:
    def __init__(self, name, root):
        self.name, self.root = name, root
        self.files, self.skipped = [], []
        for dp, dn, fn in os.walk(root):
            dn[:] = sorted(d for d in dn if d != '.git')
            for f in sorted(fn):
                p = os.path.join(dp, f)
                if f.endswith('.tcl') or f.endswith('.rvt'):
                    self.files.append(p)
                elif f.endswith(SKIP_EXT):
                    self.skipped.append({'file': os.path.relpath(p, root), 'why': os.path.splitext(f)[1]})
                elif '.' not in f:
                    try:
                        head = open(p, 'rb').read(600)
                    except OSError:
                        continue
                    if b'\0' not in head and SHEBANG.search(head):
                        self.skipped.append({'file': os.path.relpath(p, root), 'why': 'shebang-no-ext'})
        self.data = {p: open(p, 'rb').read() for p in self.files}
        blob = b'\n'.join(self.data.values())
        self.itcl_imported = bool(re.search(rb'namespace\s+import\s+(-force\s+)?:*itcl::\*', blob))
        self.li = {p: LineIndex(d) for p, d in self.data.items()}
        self.loc = sum(len(li.starts) for li in self.li.values())
        self.orc = {p: FileOracle(p, self.data[p], self.itcl_imported).run() for p in self.files}
        self.build_tables()

    def add_file(self, path, data):
        """Load a non-indexed file (e.g. .test) for oracle use."""
        self.data[path] = data
        self.li[path] = LineIndex(data)
        self.orc[path] = FileOracle(path, data, self.itcl_imported).run()

    def rel(self, p):
        return os.path.relpath(p, self.root)

    def build_tables(self):
        self.defs = []
        self.inherit = defaultdict(list)
        self.imports = defaultdict(list)
        self.paths = defaultdict(list)
        for p in self.files:
            o = self.orc[p]
            for d in o.defs:
                d = dict(d)
                d['file'] = p
                d['line'] = self.li[p].pos(d['start'])[0]
                self.defs.append(d)
            for k, v in o.inherit.items():
                self.inherit[k].extend(v)
            for ns, pat in o.imports:
                self.imports[ns].append(pat)
            for ns, pth in o.paths:
                self.paths[ns].append(pth)
        self.reindex()

    def reindex(self):
        self.by_short = defaultdict(list)
        self.by_loc = defaultdict(list)
        seen = set()
        for d in self.defs:
            self.by_loc[(d['file'], d['line'])].append(d)
            if d['name'] and is_callable(d['form']):
                k = (d['file'], d['start'])
                if k not in seen:
                    seen.add(k)
                    self.by_short[d['name']].append(d)
        self.method_names = {n for n, ds in self.by_short.items() if any(is_method(d['form']) for d in ds)}

    def add_ws_symbols(self, syms):
        """Merge workspace/symbol Function/Method entries the oracle lacks."""
        added = []
        for si in syms or []:
            if si.get('kind') not in (6, 12):
                continue
            p = uri_to_path(si['location']['uri'])
            if p not in self.li:
                continue
            r = si['location']['range']
            line = r['start']['line']
            name = si['name']
            if any(d['name'] == name for d in self.by_loc.get((p, line), [])):
                continue
            cont = si.get('containerName') or '::'
            fq = ('::' + name) if cont == '::' else cont + '::' + name
            start = self.li[p].off(line, r['start']['character'])
            d = {'form': 'ws-method' if si['kind'] == 6 else 'ws-symbol', 'name': name, 'fq': fq,
                 'start': start, 'end': start + len(name), 'file': p, 'line': line, 'chain': [],
                 'ns': cont, 'cls': cont if si['kind'] == 6 else None, 'conf': 'script', 'models': True}
            self.defs.append(d)
            added.append(d)
        if added:
            self.reindex()
        return len(added)

    # ------------------------------------------------------------------
    def ancestors(self, cls, seen=None):
        seen = seen or set()
        if not cls or cls in seen:
            return seen
        seen.add(cls)
        for b in self.inherit.get(cls, []):
            self.ancestors(b, seen)
        return seen

    def plausible(self, name, ns, cls):
        """Fully-qualified names Tcl could resolve `name` to from ns (plus class ctx)."""
        if name.startswith('::'):
            return {name}
        out = {qualify(name, ns), '::' + name}
        if '::' not in name:
            for pat in self.imports.get(ns, []):
                src, last = pat.rsplit('::', 1) if '::' in pat.lstrip(':') else ('', pat.lstrip(':'))
                if last == name or last == '*':
                    out.add((src or '') + '::' + name)
            for pth in self.paths.get(ns, []):
                out.add(pth + '::' + name)
        if cls:
            for c in self.ancestors(cls):
                out.add(c + '::' + name)
        return out

    def candidates(self, name, cls=None):
        """Callable defs a command name could mean. Methods count only when the call
        site is inside a class whose chain defines them (bare intra-class call)."""
        seg = last_seg(name)
        anc = self.ancestors(cls) if cls else set()
        ds = self.by_short.get(seg, [])
        if '::' in name:
            tail = name if name.startswith('::') else '::' + name
            return [d for d in ds if d['fq'] and (d['fq'] == tail or d['fq'].endswith(tail))]
        return [d for d in ds if not is_method(d['form']) or d.get('cls') in anc]

    def is_probe_name(self, name, cls):
        return bool(self.candidates(name, cls)) or ('::' in name and last_seg(name) in self.by_short)

    def method_defs(self, seg):
        return [d for d in self.by_short.get(seg, []) if is_method(d['form'])]

    def target_info(self, loc):
        p = uri_to_path(loc['uri'])
        r = loc['range']
        li = self.li.get(p)
        if li is None:
            return {'file': p, 'line': r['start']['line'], 'text': None, 'def': None, 'foreign': True}
        a = li.off(r['start']['line'], r['start']['character'])
        b = li.off(r['end']['line'], r['end']['character'])
        txt = li.text(a, b)
        dd = None
        for d in self.by_loc.get((p, r['start']['line']), []):
            if d['name'] and last_seg(txt) == d['name']:
                dd = d
                break
        return {'file': p, 'line': r['start']['line'], 'char': r['start']['character'], 'start': a,
                'text': txt, 'def': dd}
