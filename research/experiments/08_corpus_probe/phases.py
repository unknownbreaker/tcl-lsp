"""Phases A (robustness/perf), B (def coverage), C/D (call + $obj probes), G (sem tokens)."""
from collections import Counter, defaultdict
from lspclient import path_to_uri, uri_to_path
from oracle_util import last_seg
from probe_core import is_method, is_callable

CAP_PER_FILE_NAME = 20
CAP_PER_FILE = 600
CAP_OBJ_PER_FILE = 300
BUILTINS = set('''after append apply array binary break catch cd chan clock close concat continue dict
encoding eof error eval exec exit expr fblocked fconfigure fcopy file fileevent flush for foreach format
gets glob global if incr info interp join lappend lassign lindex linsert list llength lmap load lrange
lrepeat lreplace lreverse lsearch lset lsort namespace open package pid proc puts pwd read regexp regsub
rename return scan seek set socket source split string subst switch tailcall tell throw time trace try
unload unset update uplevel upvar variable vwait while yield zlib bind destroy wm winfo pack grid place
label button entry frame toplevel canvas text menu scrollbar my next self'''.split())


def td(p):
    return {'uri': path_to_uri(p)}


def phase_a(repo, cl, out):
    per_file, sem = [], {}
    docsyms = {}
    for p in repo.files:
        li = repo.li[p]
        row = {'file': repo.rel(p), 'ms': {}, 'status': {}}
        res, st, ms = cl.safe_request('textDocument/documentSymbol', {'textDocument': td(p)})
        row['ms']['documentSymbol'], row['status']['documentSymbol'] = round(ms, 1), st
        docsyms[p] = res if st == 'ok' else None
        res, st, ms = cl.safe_request('textDocument/foldingRange', {'textDocument': td(p)})
        row['ms']['foldingRange'], row['status']['foldingRange'] = round(ms, 1), st
        row['folds'] = len(res or []) if st == 'ok' else None
        res, st, ms = cl.safe_request('textDocument/semanticTokens/full', {'textDocument': td(p)})
        row['ms']['semanticTokens'], row['status']['semanticTokens'] = round(ms, 1), st
        toks = defaultdict(list)
        if st == 'ok' and res:
            data = res.get('data') or []
            line = char = 0
            for i in range(0, len(data) - 4, 5):
                dl, dc, ln, ty = data[i], data[i + 1], data[i + 2], data[i + 3]
                if dl:
                    line, char = line + dl, dc
                else:
                    char += dc
                toks[line].append((char, ln, ty))
        sem[p] = toks
        defs = repo.orc[p].defs
        offs = [0, len(repo.data[p]) // 2] + [d['start'] + 1 for d in defs[:3]]
        positions = [li.lsp(o) for o in offs]
        res, st, ms = cl.safe_request('textDocument/selectionRange', {'textDocument': td(p), 'positions': positions})
        row['ms']['selectionRange'], row['status']['selectionRange'] = round(ms, 1), st
        for meth, key in (('textDocument/documentSymbol', 'documentSymbol'),
                          ('textDocument/semanticTokens/full', 'semanticTokens')):
            if row['status'][key] == 'timeout':   # measure the real latency once, generously
                res2, st2, ms2 = cl.safe_request(meth, {'textDocument': td(p)}, timeout=240)
                row.setdefault('retry_long', {})[key] = {'status': st2, 'ms': round(ms2, 1)}
                if key == 'documentSymbol' and st2 == 'ok':
                    docsyms[p] = res2
        row['slow'] = [k for k, v in row['ms'].items() if v > 250]
        per_file.append(row)
    out['files_perf'] = per_file
    bad = [r for r in per_file if r['slow'] or any(v != 'ok' for v in r['status'].values())]
    out['files_flagged'] = bad
    worst = sorted(per_file, key=lambda r: -max(r['ms'].values()))[:5]
    out['slowest_files'] = [{'file': r['file'], 'max_ms': max(r['ms'].values()),
                             'method': max(r['ms'], key=r['ms'].get)} for r in worst]
    return docsyms, sem


def flatten(syms, path=()):
    for s in syms or []:
        sel = s.get('selectionRange') or s.get('range')
        yield {'name': s['name'], 'kind': s['kind'], 'line': sel['start']['line'],
               'char': sel['start']['character'], 'path': '/'.join(path)}
        yield from flatten(s.get('children'), path + (s['name'],))


def phase_b(repo, docsyms, out):
    misses, extra, extra_n = [], [], 0
    extra_kinds = Counter()
    form_total, form_miss = Counter(), Counter()
    for p in repo.files:
        syms = list(flatten(docsyms.get(p) or []))
        ns_nodes = {s['name'] for s in syms if s['kind'] == 3}
        by_line = defaultdict(list)
        for s in syms:
            by_line[s['line']].append(s)
        matched = set()
        li = repo.li[p]
        for d in repo.orc[p].defs:
            form = d['form']
            form_total[form] += 1
            line = li.pos(d['start'])[0]
            ok = False
            if form == 'namespace eval':
                ok = d['fq'] in ns_nodes
            elif d['name']:
                for s in by_line.get(line, []):
                    if last_seg(s['name']) == d['name'] or s['name'].lstrip(':') == d['name']:
                        ok = True
                        matched.add((s['line'], s['char']))
            else:
                ok = None   # dynamic / nameless forms: counted only
            if ok is False:
                form_miss[form] += 1
                tags = list(d['chain'])
                if d['conf'] != 'script':
                    tags.append('conf:' + d['conf'])
                if form == 'itcl::body' and not any(x['form'].startswith(('itcl::class', 'class(')) and x['fq'] == d['cls'] for x in repo.orc[p].defs):
                    tags.append('class-defined-in-other-file')
                misses.append({'file': repo.rel(p), 'line': line + 1, 'form': form, 'name': d['name'],
                               'fq': d['fq'], 'models': d['models'], 'indent': li.pos(d['cmd_start'])[1],
                               'tags': tags, 'text': li.line_text(line)})
        for s in syms:
            if s['kind'] == 3 or (s['line'], s['char']) in matched:
                continue
            extra_n += 1
            off = li.off(s['line'], s['char'])
            inproc = [pr for pr in repo.orc[p].procs.values() if pr['start'] <= off < pr['end']]
            where = ('inside-%s-body' % inproc[-1]['kind']) if inproc else 'outside-oracle-procs'
            if inproc and inproc[-1]['cls']:
                where += '-of-class'
            extra_kinds[where] += 1
            if len(extra) < 20 or (where != 'outside-oracle-procs' and len(extra) < 40):
                extra.append({'file': repo.rel(p), 'line': s['line'] + 1, 'name': s['name'], 'kind': s['kind'],
                              'container': s['path'], 'where': where, 'text': li.line_text(s['line'])})
    out['def_forms_total'] = dict(form_total)
    out['def_forms_missed'] = dict(form_miss)
    out['def_misses'] = misses
    out['symbols_not_in_oracle'] = {'count': extra_n, 'by_where': dict(extra_kinds), 'sample': extra}


def _mid_offset(name, start):
    seg = last_seg(name)
    return start + len(name) - len(seg) + max(0, len(seg) // 2)


def classify(repo, probe, res, st, is_obj=False):
    """Return (class, detail) for a goto-definition probe on a call/method name."""
    if st != 'ok':
        return st.upper(), {}
    name = probe['name']
    seg = last_seg(name)
    locs = res if isinstance(res, list) else ([res] if res else [])
    if locs:
        tg = [repo.target_info(l if 'uri' in l else {'uri': l['targetUri'], 'range': l['targetSelectionRange']})
              for l in locs]
        det = {'targets': [{'file': repo.rel(t['file']) if not t.get('foreign') else t['file'],
                            'line': t['line'] + 1, 'text': t['text'],
                            'fq': t['def']['fq'] if t['def'] else None,
                            'form': t['def']['form'] if t['def'] else None} for t in tg[:5]],
               'n_targets': len(tg)}
        if any(t['text'] is None or last_seg(t['text']) != seg for t in tg):
            return 'SUSPECT_WRONG', dict(det, reason='name-mismatch')
        varforms = [t['def']['form'] for t in tg if t['def'] and not is_callable(t['def']['form'])
                    and t['def']['form'] not in ('itcl::class', 'class(itcl-imported)', 'oo::class create')]
        if varforms:
            return 'SUSPECT_WRONG', dict(det, reason='command-resolved-to-non-callable:' + varforms[0])
        if probe.get('pk') == 'switch-pattern':
            return 'SUSPECT_WRONG', dict(det, reason='switch-pattern-resolved-as-call')
        if probe.get('conf') == 'data' and probe.get('pk') == 'command-start':
            return 'DATA_CONTEXT_RESOLVED', det
        if is_obj:
            if probe.get('recv') == 'this' and probe.get('cls'):
                anc = repo.ancestors(probe['cls'])
                bad = [t for t in tg if t['def'] and t['def'].get('cls') and t['def']['cls'] not in anc]
                if bad:
                    return 'SUSPECT_WRONG', dict(det, reason='this-method-outside-class-chain')
            return 'OK', det
        pl = repo.plausible(name, probe['ns'], probe.get('cls'))
        bad = []
        for t in tg:
            d = t['def']
            if not d or not d['fq']:
                continue
            if d['fq'] in pl:
                continue
            if is_method(d['form']) and probe.get('cls') and d.get('cls') in repo.ancestors(probe['cls']):
                continue
            bad.append(d['fq'])
        if bad:
            return 'SUSPECT_WRONG', dict(det, reason='namespace-unreachable', resolved_fq=bad[:3],
                                         plausible=sorted(pl)[:6])
        return 'OK', det
    if probe.get('pk') == 'switch-pattern':
        return 'OK_SILENT', {}
    cands = repo.method_defs(seg) if is_obj else repo.candidates(name, probe.get('cls'))
    keys = {(d['file'], d['start']) for d in cands}
    det = {'n_defs': len(keys), 'def_forms': sorted({d['form'] for d in cands})[:5]}
    if not is_obj:
        pl = repo.plausible(name, probe['ns'], probe.get('cls'))
        reach = [d for d in cands if d['fq'] in pl or (is_method(d['form']) and probe.get('cls')
                                                         and d.get('cls') in repo.ancestors(probe['cls']))]
        det['n_reachable'] = len({(d['file'], d['start']) for d in reach})
        det['defs'] = [{'file': repo.rel(d['file']), 'line': d['line'] + 1, 'fq': d['fq']} for d in cands[:3]]
    if not keys:
        return 'EXTERNAL', det
    if len(keys) == 1:
        return 'CANDIDATE_MISS', det
    return 'AMBIGUOUS_SILENT', det


def phase_c(repo, cl, out, sem):
    rows, caps = [], Counter()
    known = set(repo.by_short)
    for p in repo.files:
        li = repo.li[p]
        per_name, n_file = Counter(), 0
        for c in repo.orc[p].cmdpos:
            if c['pk'] == 'class-member' or last_seg(c['name']) not in known \
                    or not repo.is_probe_name(c['name'], c['cls']):
                continue
            if per_name[c['name']] >= CAP_PER_FILE_NAME or n_file >= CAP_PER_FILE:
                caps['capped'] += 1
                continue
            per_name[c['name']] += 1
            n_file += 1
            pos = li.lsp(_mid_offset(c['name'], c['start']))
            res, st, ms = cl.safe_request('textDocument/definition', {'textDocument': td(p), 'position': pos})
            cls, det = classify(repo, c, res, st)
            line = pos['line']
            tags = [c['pk']] + c['chain'][-3:]
            if c['test']:
                tags.append('inside-test-body')
            if c['frame'] == 'proc' and c.get('cls'):
                tags.append('inside-class-method')
            if c['name'] in BUILTINS:
                tags.append('builtin-name')
            if '::' in c['name'] and not c['name'].startswith('::'):
                tags.append('relative-qualified' + ('' if c['ns'] == '::' else '-from-nonglobal-ns'))
            row = {'file': repo.rel(p), 'line': line + 1, 'col': pos['character'], 'name': c['name'],
                   'class': cls, 'pk': c['pk'], 'conf': c['conf'], 'ns': c['ns'], 'cls': c['cls'],
                   'tags': tags, 'text': li.line_text(line), 'ms': round(ms, 1), **det}
            # G: semantic token at the probe position
            tok = None
            for (ch, ln, ty) in sem.get(p, {}).get(line, []):
                if ch <= pos['character'] < ch + ln:
                    tok = ty
            row['sem'] = tok
            if c['pk'] == 'switch-pattern' and tok in (2, 3):
                row['sem_mismatch'] = 'switch-pattern-colored-as-call'
            if c['pk'] in ('command-start', 'bracket'):
                if cls == 'OK' and tok not in (2, 3):
                    row['sem_mismatch'] = 'resolved-but-uncolored' if tok is None else 'resolved-colored-type-%d' % tok
                elif cls in ('CANDIDATE_MISS', 'AMBIGUOUS_SILENT', 'EXTERNAL') and tok in (2, 3):
                    row['sem_mismatch'] = 'unresolved-but-colored'
            row['_target'] = None
            if cls == 'OK':
                t = res[0]
                row['_target'] = (uri_to_path(t['uri']), t['range']['start']['line'], t['range']['start']['character'])
                row['_word'] = (p, li.pos(c['start']), li.pos(c['end']))
            rows.append(row)
    out['call_probes_capped'] = caps['capped']
    return rows


def phase_d(repo, cl, out):
    rows, usage = [], Counter()
    for p in repo.files:
        li = repo.li[p]
        o = repo.orc[p]
        n = 0
        for c in o.objcalls:
            usage['recv:' + ('this' if c['recv'] == 'this' else 'self' if c['recv'] == 'self'
                             else 'my' if c['recv'] == 'my' else 'var')] += 1
            if c['name'] not in repo.method_names:
                continue
            if n >= CAP_OBJ_PER_FILE:
                usage['capped'] += 1
                continue
            n += 1
            pos = li.lsp(c['start'] + len(c['name']) // 2)
            res, st, ms = cl.safe_request('textDocument/definition', {'textDocument': td(p), 'position': pos})
            cls, det = classify(repo, c, res, st, is_obj=True)
            typed = None
            if c['proc'] and c['recv'] in o.procs[c['proc']]['typed']:
                typed = o.procs[c['proc']]['typed'][c['recv']]
            elif c['recv'] in o.file_typed:
                typed = o.file_typed[c['recv']]
            tags = ['recv:' + c['recv'] if c['recv'] in ('this', 'self', 'my') else 'recv:$var'] + c['chain'][-2:]
            if typed:
                known_cls = any(d['form'].startswith(('itcl::class', 'class(', 'oo::class', 'snit::'))
                                and d['name'] == last_seg(typed) for d in repo.defs)
                tags.append('typed-from:%s%s' % (typed, '(known-class)' if known_cls else ''))
            rows.append({'file': repo.rel(p), 'line': pos['line'] + 1, 'name': c['name'], 'recv': c['recv'],
                         'class': cls, 'tags': tags, 'cls': c['cls'], 'text': li.line_text(pos['line']),
                         'ms': round(ms, 1), **det})
    out['obj_usage'] = dict(usage)
    return rows
