"""Phases E (variable probes), F (references symmetry + call hierarchy), H (skipped files)."""
import os
from collections import Counter, defaultdict
from lspclient import path_to_uri, uri_to_path
from oracle_util import last_seg
from probe_core import is_method
from phases import td, classify

CAP_VAR_PER_PROC = 60
CAP_VAR_PER_NAME = 3
CAP_REFS = 300
CAP_CH = 30


def phase_e(repo, cl, out):
    rows, counts, ok_sample = [], Counter(), []
    tagc = defaultdict(Counter)
    capped = 0
    for p in repo.files:
        li = repo.li[p]
        o = repo.orc[p]
        per_proc, per_name = Counter(), Counter()
        for v in o.varuses:
            pr = o.procs.get(v['proc'])
            if pr is None:
                continue
            key = (v['proc'], v['name'])
            if per_proc[v['proc']] >= CAP_VAR_PER_PROC or per_name[key] >= CAP_VAR_PER_NAME:
                capped += 1
                continue
            per_proc[v['proc']] += 1
            per_name[key] += 1
            name = v['name']
            qualified = '::' in name
            pos = li.lsp(v['start'] + max(0, (v['end'] - v['start']) // 2))
            res, st, ms = cl.safe_request('textDocument/definition', {'textDocument': td(p), 'position': pos})
            forms = sorted(pr['bindings'].get(name, ())) if not qualified else []
            tags = list(v['tags']) + (['qualified'] if qualified else []) + (['array-elem'] if v['array'] else []) \
                + (['in-loop'] if v['loop'] else []) + (['in-class-method'] if pr['cls'] and pr['kind'] == 'method' else [])
            row = {'file': repo.rel(p), 'line': pos['line'] + 1, 'col': pos['character'], 'name': name,
                   'proc': pr['fq'], 'binding_forms': forms, 'tags': tags, 'text': li.line_text(pos['line']),
                   'ms': round(ms, 1)}
            if st != 'ok':
                cls = st.upper()
            elif res:
                tg = [repo.target_info(l) for l in res]
                row['targets'] = [{'file': repo.rel(t['file']), 'line': t['line'] + 1, 'text': t['text'],
                                   'form': t['def']['form'] if t['def'] else None} for t in tg[:4]]
                cls, reason = 'VAR_OK', None
                for t in tg:
                    if t['text'] is None or last_seg(t['text']) != last_seg(name):
                        cls, reason = 'VAR_SUSPECT_WRONG', 'name-mismatch'
                        break
                    inside = t['file'] == p and pr['start'] <= t['start'] < pr['end']
                    if not inside:
                        dform = t['def']['form'] if t['def'] else ''
                        ivar = pr['cls'] and ('variable' in dform or 'common' in dform)
                        if not (qualified or name in pr['links'] or ivar):
                            cls, reason = 'VAR_SUSPECT_WRONG', 'outside-proc-unlinked'
                            break
                    elif t['start'] > v['start'] and not v['loop'] and not qualified \
                            and name not in pr['links']:
                        cls, reason = 'VAR_SUSPECT_WRONG', 'binding-after-use-no-loop'
                        break
                if reason:
                    row['reason'] = reason
                if cls == 'VAR_OK' and not forms and not qualified:
                    cls = 'VAR_UNBOUND_RESOLVED'
            else:
                cls = 'CANDIDATE_VAR_MISS' if forms else 'VAR_UNBOUND_EMPTY'
            row['class'] = cls
            counts[cls] += 1
            if cls == 'CANDIDATE_VAR_MISS':
                for f in forms:
                    tagc['forms'][f] += 1
                for t in tags:
                    tagc['tags'][t] += 1
            if cls == 'VAR_OK':
                if len(ok_sample) < 10:
                    ok_sample.append(row)
            elif cls != 'VAR_UNBOUND_EMPTY' or len(rows) < 20000:
                rows.append(row)
    out['var_counts'] = dict(counts)
    out['var_miss_by_form'] = dict(tagc['forms'])
    out['var_miss_by_tag'] = dict(tagc['tags'])
    out['var_probes_capped'] = capped
    out['var_ok_sample'] = ok_sample
    return rows


def phase_f(repo, cl, out, call_rows):
    groups = defaultdict(list)
    for r in call_rows:
        if r.get('_target'):
            groups[r['_target']].append(r)
    keys = sorted(groups)
    if len(keys) > CAP_REFS:
        step = len(keys) / CAP_REFS
        keys = [keys[int(i * step)] for i in range(CAP_REFS)]
    datapos, allpos, patpos = set(), set(), set()
    for p in repo.files:
        li = repo.li[p]
        for c in repo.orc[p].cmdpos:
            k = (p,) + li.pos(c['start'])
            if c['pk'] == 'switch-pattern':
                patpos.add(k)
                continue
            allpos.add(k)
            if c['conf'] == 'data':
                datapos.add(k)
    missing, suspect, stats = [], [], Counter()
    for k in keys:
        fpath, line, ch = k
        res, st, ms = cl.safe_request('textDocument/references', {
            'textDocument': {'uri': path_to_uri(fpath)}, 'position': {'line': line, 'character': ch + 1},
            'context': {'includeDeclaration': False}})
        stats['requests'] += 1
        if st != 'ok':
            stats[st] += 1
            continue
        refs = []
        for l in res or []:
            rp = uri_to_path(l['uri'])
            rs, re_ = l['range']['start'], l['range']['end']
            refs.append((rp, rs['line'], rs['character'], re_['line'], re_['character']))
        stats['refs_returned'] += len(refs)
        short = groups[k][0]['name']
        short = last_seg(short)
        for r in groups[k]:
            wp, (l0, c0), (l1, c1) = r['_word']
            if not any(x[0] == wp and (x[1], x[2]) <= (l0, c0) and (x[3], x[4]) >= (l1, c1) for x in refs):
                missing.append({'def': '%s:%d' % (repo.rel(fpath), line + 1), 'name': short,
                                'call': '%s:%d' % (r['file'], r['line']), 'pk': r['pk'], 'text': r['text']})
        for x in refs:
            li = repo.li.get(x[0])
            if li is None:
                continue
            txt = li.text(li.off(x[1], x[2]), li.off(x[3], x[4]))
            why = None
            if last_seg(txt) != short:
                why = 'text-mismatch'
            elif (x[0], x[1], x[2]) in patpos:
                why = 'switch-pattern'
            elif (x[0], x[1], x[2]) in datapos:
                why = 'data-context'
            elif (x[0], x[1], x[2]) not in allpos:
                stats['ref_not_at_oracle_cmd_pos'] += 1
                why = 'not-oracle-command-position'
            if why:
                suspect.append({'def': '%s:%d' % (repo.rel(fpath), line + 1), 'name': short, 'why': why,
                                'ref': '%s:%d' % (repo.rel(x[0]), x[1] + 1), 'text': li.line_text(x[1])})
    # call hierarchy robustness
    ch_stats = Counter()
    for k in keys[:CAP_CH]:
        fpath, line, ch = k
        items, st, ms = cl.safe_request('textDocument/prepareCallHierarchy', {
            'textDocument': {'uri': path_to_uri(fpath)}, 'position': {'line': line, 'character': ch + 1}})
        ch_stats['prepare_' + st] += 1
        if st != 'ok' or not items:
            ch_stats['prepare_empty'] += 1
            continue
        for meth in ('callHierarchy/incomingCalls', 'callHierarchy/outgoingCalls'):
            res, st, ms = cl.safe_request(meth, {'item': items[0]})
            ch_stats[meth.split('/')[1] + '_' + st] += 1
            ch_stats[meth.split('/')[1] + '_items'] += len(res or []) if st == 'ok' else 0
    out['refs_stats'] = dict(stats, defs_checked=len(keys))
    out['refs_missing'] = missing
    out['refs_suspect'] = suspect
    out['call_hierarchy'] = dict(ch_stats)


def phase_h(repo, cl, out):
    out['skipped_files'] = repo.skipped
    tests = [os.path.join(repo.root, s['file']) for s in repo.skipped if s['why'] == '.test']
    if not tests:
        return
    scored = []
    for t in tests:
        raw = open(t, 'rb').read()
        text = raw.decode('utf-8', 'replace')
        repo.add_file(t, text.encode('utf-8'))
        uniq = []
        for c in repo.orc[t].cmdpos:
            if c['pk'] == 'class-member':
                continue
            ds = [d for d in repo.candidates(c['name']) if not is_method(d['form']) and d['file'] != t]
            if len({(d['file'], d['start']) for d in ds}) == 1:
                uniq.append((c, ds[0]))
        scored.append((len({c['name'] for c, _ in uniq}), t, text, uniq))
    scored.sort(key=lambda x: -x[0])
    exp = []
    for score, t, text, uniq in scored[:2]:
        if not score:
            continue
        li = repo.li[t]
        libdefs = {}
        for c, d in uniq:
            libdefs.setdefault(d['fq'], d)
        libdefs = list(libdefs.values())[:5]

        def refs_hit(d):
            res, st, _ = cl.safe_request('textDocument/references', {
                'textDocument': {'uri': path_to_uri(d['file'])},
                'position': repo.li[d['file']].lsp(d['start'] + 1), 'context': {'includeDeclaration': False}})
            return st, sum(1 for l in (res or []) if uri_to_path(l['uri']) == t)
        before = {d['fq']: refs_hit(d) for d in libdefs}
        cl.did_open(t, text)
        probes = Counter()
        seen = Counter()
        samples = []
        for c, d in uniq:
            if seen[c['name']] >= 2 or sum(seen.values()) >= 30:
                continue
            seen[c['name']] += 1
            pos = li.lsp(c['start'] + len(c['name']) // 2)
            res, st, _ = cl.safe_request('textDocument/definition', {'textDocument': td(t), 'position': pos})
            cls, det = classify(repo, c, res, st)
            where = 'in-test-body' if c['test'] else ('conf-' + c['conf'])
            probes[cls + '/' + where] += 1
            if len(samples) < 6:
                samples.append({'line': pos['line'] + 1, 'name': c['name'], 'class': cls, 'pk': c['pk'],
                                'where': where, 'chain': c['chain'][-2:], 'text': li.line_text(pos['line'])})
        after = {d['fq']: refs_hit(d) for d in libdefs}
        exp.append({'test_file': repo.rel(t), 'lib_defs': [d['fq'] for d in libdefs],
                    'refs_from_test_before_open': {k: v[1] for k, v in before.items()},
                    'probes_after_open': dict(probes), 'probe_samples': samples,
                    'refs_from_test_after_open': {k: v[1] for k, v in after.items()}})
    out['test_file_experiment'] = exp
