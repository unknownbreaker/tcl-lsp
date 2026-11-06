"""Reduce harness results to the per-repo counts the before/after gate compares.

Usage: python3 -I counts.py <results-dir> <out.json>

Write <out.json> outside <results-dir>: every *.json in there is read as a repo result."""
import glob
import json
import os
import sys
from collections import Counter


def repo_counts(result):
    """The deterministic counts of one <repo>.json. Timings are left out on purpose."""
    wrong = Counter(r.get('reason') for r in result['call_probes'] + result['obj_probes']
                    if r['class'] == 'SUSPECT_WRONG')
    wrong.update(r.get('reason') for r in result['var_probes'] if r['class'] == 'VAR_SUSPECT_WRONG')
    return {
        'call_counts': result['call_counts'],
        'obj_counts': result['obj_counts'],
        'var_counts': result['var_counts'],
        'wrong_answers_by_reason': dict(wrong),
        'sem_mismatch_counts': result['sem_mismatch_counts'],
        'refs_suspect_by_why': dict(Counter(x['why'] for x in result['refs_suspect'])),
        'refs_missing': len(result['refs_missing']),
        'def_misses': len(result['def_misses']),
        'symbols_not_in_oracle': result['symbols_not_in_oracle']['count'],
        'workspace_symbols': result['workspace_symbols']['count'],
    }


def load_counts(results_dir):
    """Map repo name -> counts for every repo result in results_dir."""
    out = {}
    for path in sorted(glob.glob(os.path.join(results_dir, '*.json'))):
        with open(path) as f:
            result = json.load(f)
        out[result['repo']] = repo_counts(result)
    return out


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    results_dir, out_path = sys.argv[1], sys.argv[2]
    counts = load_counts(results_dir)
    if not counts:
        sys.exit('no <repo>.json files in %s' % results_dir)
    with open(out_path, 'w') as f:
        json.dump(counts, f, indent=1, sort_keys=True)
        f.write('\n')
    print('wrote %s: %d repos' % (out_path, len(counts)))


if __name__ == '__main__':
    main()
