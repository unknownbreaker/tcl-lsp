"""Smoke test: run the oracle over every .tcl/.rvt under a dir; print counts.
Usage: python3 -I oracle_smoke.py <dir> [file-to-dump]"""
import os
import sys
import time
from collections import Counter
sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from oracle import FileOracle  # noqa: E402

root = sys.argv[1]
dump = sys.argv[2] if len(sys.argv) > 2 else None
forms, pks, n, t0 = Counter(), Counter(), 0, time.time()
nv = nobj = 0
for dp, dn, fn in os.walk(root):
    dn[:] = [d for d in dn if d != '.git']
    for f in fn:
        if not (f.endswith('.tcl') or f.endswith('.rvt') or f.endswith('.test')):
            continue
        p = os.path.join(dp, f)
        o = FileOracle(p, open(p, 'rb').read(), True).run()
        n += 1
        for d in o.defs:
            forms[d['form']] += 1
        for c in o.cmdpos:
            pks[c['pk'].split(':')[0]] += 1
        nv += len(o.varuses)
        nobj += len(o.objcalls)
        if dump and p.endswith(dump):
            for d in o.defs[:40]:
                ln = o.s.count('\n', 0, d['start']) + 1
                print('DEF', ln, d['form'], d['fq'], d['conf'], d['chain'])
            for v in o.varuses[:15]:
                print('VAR', o.s.count('\n', 0, v['start']) + 1, v['name'], v['tags'], v['chain'])
print('files', n, 'secs %.1f' % (time.time() - t0), 'varuses', nv, 'objcalls', nobj)
print('forms', forms.most_common(40))
print('pks', pks.most_common())
