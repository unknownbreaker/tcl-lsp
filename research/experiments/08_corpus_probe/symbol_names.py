"""Dump every workspace symbol per repo, so two binaries can be compared with plain diff.

Usage: python3 -I symbol_names.py --binary PATH --corpus DIR --out DIR [repo ...]
(default: every repo directory in DIR). Writes sorted `file:line kind name` lines to
<out>/<repo>.txt, one server process per repo. Lines are 1-based and files are relative to
the repo. `diff -r <out-before> <out-after>` is the symbol diff.

The harness's own results keep symbol counts and samples only, and it does not probe
namespace-level `$x`, so this is how a change to namespace-level symbols becomes visible."""
import argparse
import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
from lspclient import LSPClient, uri_to_path  # noqa: E402


def symbol_lines(symbols, root):
    """Format workspace/symbol results as sorted `file:line kind name` lines."""
    lines = []
    for symbol in symbols:
        location = symbol['location']
        relative_path = os.path.relpath(uri_to_path(location['uri']), root)
        line_number = location['range']['start']['line'] + 1
        lines.append('%s:%d %d %s' % (relative_path, line_number, symbol['kind'], symbol['name']))
    return sorted(lines)


def dump_repo(name, binary, corpus, out):
    """Index one repo with its own server and write its symbol lines to <out>/<name>.txt."""
    root = os.path.realpath(os.path.join(corpus, name))
    # A log in <out> carries timestamps, which would show up in `diff -r`.
    client = LSPClient(binary, root, os.devnull)
    client.start()
    try:
        symbols, error, _ = client.request('workspace/symbol', {'query': ''}, timeout=60)
        if error:
            raise RuntimeError('workspace/symbol failed for %s: %r' % (name, error))
    finally:
        client.stop()
    lines = symbol_lines(symbols or [], root)
    with open(os.path.join(out, name + '.txt'), 'w') as output:
        output.writelines(line + '\n' for line in lines)
    print('%-15s %d symbols' % (name, len(lines)), flush=True)


def parse_args():
    parser = argparse.ArgumentParser(description='Write each repo\'s workspace symbols as sorted lines.')
    parser.add_argument('--binary', required=True, help='path to the tcl-lsp server binary')
    parser.add_argument('--corpus', required=True, help='directory holding one checkout per repo')
    parser.add_argument('--out', required=True, help='directory to write <repo>.txt into')
    parser.add_argument('repos', nargs='*', help='repo names (default: every directory in --corpus)')
    return parser.parse_args()


def main():
    args = parse_args()
    binary = os.path.abspath(args.binary)  # the client runs the server with cwd set to its directory
    corpus = os.path.abspath(args.corpus)
    out = os.path.abspath(args.out)
    os.makedirs(out, exist_ok=True)
    names = args.repos or [d for d in sorted(os.listdir(corpus)) if os.path.isdir(os.path.join(corpus, d))]
    for name in names:
        dump_repo(name, binary, corpus, out)


if __name__ == '__main__':
    main()
