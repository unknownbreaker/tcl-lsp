"""Print a few rows of one category from <results>/<repo>.json.
Usage: python3 -I inspect_rows.py --results DIR <repo> <section> [class-or-field=value] [n] [offset]
section: call_probes | obj_probes | var_probes | def_misses | refs_missing | refs_suspect | <top-level key>"""
import argparse
import json
import os
import sys
from collections import Counter

ap = argparse.ArgumentParser(description='Print rows from one harness result file.')
ap.add_argument('--results', required=True, help='directory holding <repo>.json')
ap.add_argument('repo')
ap.add_argument('section')
ap.add_argument('filter', nargs='?', default='', help='class name, or field=value')
ap.add_argument('n', nargs='?', type=int, default=8)
ap.add_argument('offset', nargs='?', type=int, default=0)
args = ap.parse_args()
flt, n, off = args.filter, args.n, args.offset
d = json.load(open(os.path.join(args.results, args.repo + '.json')))
rows = d.get(args.section)
if not isinstance(rows, list):
    print(json.dumps(rows, indent=1)[:4000])
    sys.exit()
if flt:
    k, v = flt.split('=', 1) if '=' in flt else ('class', flt)
    rows = [r for r in rows if str(r.get(k)) == v or (isinstance(r.get(k), list) and v in r.get(k))]
print('matching rows:', len(rows))
if rows and 'tags' in rows[0]:
    c = Counter(t for r in rows for t in r['tags'][:1])
    print('first-tag counts:', c.most_common(8))
for r in rows[off:off + n]:
    r = {k: v for k, v in r.items() if k not in ('ms',)}
    print(json.dumps(r)[:600])
