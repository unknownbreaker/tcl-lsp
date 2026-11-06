"""Print per-repo count deltas between a baseline counts.json and a results directory.

Usage: python3 -I compare.py <baseline.json> <results-dir>

Only repos present in <results-dir> are compared, so a partial run is fine. Exits 0 with
NO DELTAS when every compared count matches, 1 when any differs or a repo failed, and 2
when there was nothing to compare."""
import glob
import json
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from counts import load_counts  # noqa: E402


def flatten(counts):
    """Turn one repo's nested counts into {'section.key' or 'section': value}."""
    flat = {}
    for section, value in counts.items():
        if isinstance(value, dict):
            for key, n in value.items():
                flat['%s.%s' % (section, key)] = n
        else:
            flat[section] = value
    return flat


def repo_deltas(before, after):
    """Yield (metric, before, after) for each metric whose count changed."""
    a, b = flatten(before), flatten(after)
    for metric in sorted(set(a) | set(b)):
        if a.get(metric, 0) != b.get(metric, 0):
            yield metric, a.get(metric, 0), b.get(metric, 0)


def main():
    if len(sys.argv) != 3:
        sys.exit(__doc__)
    baseline_path, results_dir = sys.argv[1], sys.argv[2]
    with open(baseline_path) as f:
        baseline = json.load(f)
    current = load_counts(results_dir)
    failed = sorted(os.path.basename(p)[:-len('.FAILED.txt')]
                    for p in glob.glob(os.path.join(results_dir, '*.FAILED.txt')))
    if not current and not failed:
        print('NOTHING TO COMPARE: no <repo>.json files in %s' % results_dir)
        return 2
    differences = len(failed)
    for repo in failed:
        print('%s FAILED: see %s.FAILED.txt' % (repo, repo))
    for repo in sorted(current):
        if repo not in baseline:
            print('%s: not in baseline' % repo)
            differences += 1
            continue
        for metric, old, new in repo_deltas(baseline[repo], current[repo]):
            print('%s %s: %s -> %s' % (repo, metric, old, new))
            differences += 1
    if differences:
        print('DELTAS: %d (%d of %d baseline repos compared)' % (differences, len(current), len(baseline)))
        return 1
    print('NO DELTAS (%d of %d baseline repos compared)' % (len(current), len(baseline)))
    return 0


if __name__ == '__main__':
    sys.exit(main())
