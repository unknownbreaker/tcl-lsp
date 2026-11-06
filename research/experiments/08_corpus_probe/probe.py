"""End-to-end LSP probe harness for tcl-lsp over the FlightAware corpus.

Usage: python3 -I probe.py --binary PATH --corpus DIR --results DIR [repo ...]
(default: every repo directory in DIR). Writes <results>/<repo>.json and
<repo>.stderr.log; prints one line per repo."""
import argparse
import json
import os
import re
import sys
import time
from collections import Counter

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
sys.setrecursionlimit(10000)
from lspclient import LSPClient  # noqa: E402
from probe_core import Repo  # noqa: E402
from phases import phase_a, phase_b, phase_c, phase_d  # noqa: E402
from phases2 import phase_e, phase_f, phase_h  # noqa: E402


def pct(xs, q):
    if not xs:
        return None
    xs = sorted(xs)
    return round(xs[min(len(xs) - 1, int(q * len(xs)))], 1)


def run_repo(name, binary, corpus, results):
    t0 = time.time()
    root = os.path.realpath(os.path.join(corpus, name))
    repo = Repo(name, root)
    out = {'repo': name, 'files': len(repo.files), 'loc': repo.loc, 'itcl_imported': repo.itcl_imported}
    err_path = os.path.join(results, name + '.stderr.log')
    open(err_path, 'w').close()
    cl = LSPClient(binary, root, err_path)
    cl.start()
    out['index_ms'] = round(cl.index_ms, 1)
    out['initialize_ms'] = round(cl.init_ms, 1)
    syms, st, ms = cl.safe_request('workspace/symbol', {'query': ''}, timeout=60)
    syms = syms if st == 'ok' else []
    out['workspace_symbols'] = {'count': len(syms or []), 'ms': round(ms, 1),
                                'files_with_symbols': len({s['location']['uri'] for s in syms or []}),
                                'by_kind': dict(Counter(s['kind'] for s in syms or []))}
    out['ws_symbols_added_to_known'] = repo.add_ws_symbols(syms)
    usage = Counter()
    for o in repo.orc.values():
        usage.update(o.usage)
    out['oo_usage'] = dict(usage)
    out['def_shaped_heads'] = dict(sum((o.shape_heads for o in repo.orc.values()), Counter()).most_common(15))

    docsyms, sem = phase_a(repo, cl, out)
    phase_b(repo, docsyms, out)
    calls = phase_c(repo, cl, out, sem)
    objs = phase_d(repo, cl, out)
    vars_ = phase_e(repo, cl, out)
    phase_f(repo, cl, out, calls)
    phase_h(repo, cl, out)

    for r in calls:
        r.pop('_target', None)
        r.pop('_word', None)
    out['call_probes'] = calls
    out['call_counts'] = dict(Counter(r['class'] for r in calls))
    out['sem_mismatch_counts'] = dict(Counter(r['sem_mismatch'] for r in calls if r.get('sem_mismatch')))
    out['obj_probes'] = objs
    out['obj_counts'] = dict(Counter(r['class'] for r in objs))
    out['var_probes'] = vars_
    cl.stop()
    out['restarts'] = cl.restarts
    out['timeouts'] = cl.timeouts
    out['server_requests_seen'] = cl.server_requests
    out['latency'] = {m: {'n': len(v), 'p50': pct(v, .5), 'p95': pct(v, .95), 'max': round(max(v), 1)}
                      for m, v in cl.stats.items()}
    out['error_responses'] = cl.errors
    log = open(err_path, 'rb').read().decode('utf-8', 'replace')
    flagged = [ln for ln in log.splitlines() if re.search(r'panic|recovered|error|fatal', ln, re.I)]
    out['stderr_flagged'] = flagged[:50]
    out['stderr_flagged_count'] = len(flagged)
    out['wall_s'] = round(time.time() - t0, 1)
    with open(os.path.join(results, name + '.json'), 'w') as f:
        json.dump(out, f, indent=1)
    vc = out['var_counts']
    print('%-15s files=%d loc=%d idx=%.0fms calls=%s obj=%s varmiss=%d varsus=%d defmiss=%d restarts=%d %ss' % (
        name, out['files'], out['loc'], out['index_ms'], out['call_counts'], out['obj_counts'],
        vc.get('CANDIDATE_VAR_MISS', 0), vc.get('VAR_SUSPECT_WRONG', 0), len(out['def_misses']),
        cl.restarts, out['wall_s']), flush=True)


def parse_args():
    ap = argparse.ArgumentParser(description='Probe tcl-lsp over a corpus of Tcl repos.')
    ap.add_argument('--binary', required=True, help='path to the tcl-lsp server binary')
    ap.add_argument('--corpus', required=True, help='directory holding one checkout per repo')
    ap.add_argument('--results', required=True, help='directory to write <repo>.json into')
    ap.add_argument('repos', nargs='*', help='repo names (default: every directory in --corpus)')
    return ap.parse_args()


def main():
    args = parse_args()
    binary = os.path.abspath(args.binary)  # the client runs the server with cwd set to its directory
    corpus = os.path.abspath(args.corpus)
    results = os.path.abspath(args.results)
    os.makedirs(results, exist_ok=True)
    names = args.repos or [d for d in sorted(os.listdir(corpus)) if os.path.isdir(os.path.join(corpus, d))]
    for n in names:
        try:
            run_repo(n, binary, corpus, results)
        except Exception as exc:  # record and continue with the next repo
            import traceback
            with open(os.path.join(results, n + '.FAILED.txt'), 'w') as f:
                traceback.print_exc(file=f)
            print('%-15s FAILED: %r' % (n, exc), flush=True)


if __name__ == '__main__':
    main()
