"""Minimal synchronous LSP client over stdio for the tcl-lsp probe harness.

One request in flight at a time. A reader thread frames messages into a queue;
server->client requests are answered with null. Timeouts and dead servers are
reported to the caller (which records them and restarts)."""
import json
import os
import queue
import subprocess
import threading
import time
import urllib.parse


def path_to_uri(path):
    return 'file://' + urllib.parse.quote(path)


def uri_to_path(uri):
    return urllib.parse.unquote(urllib.parse.urlparse(uri).path)


class ServerDead(Exception):
    pass


class Timeout(Exception):
    pass


class LSPClient:
    def __init__(self, binary, root, stderr_path):
        self.binary = binary
        self.root = root
        self.stderr_path = stderr_path
        self.proc = None
        self.next_id = 0
        self.q = None
        self.stats = {}          # method -> list of latency ms
        self.errors = {}         # method -> count of error responses
        self.timeouts = []       # (method, params summary)
        self.restarts = 0
        self.server_requests = {}
        self.index_ms = None
        self.open_docs = {}      # uri -> text (re-sent on restart)

    # ---- process management -------------------------------------------
    def start(self):
        errf = open(self.stderr_path, 'ab')
        errf.write(('\n=== server start %s ===\n' % time.strftime('%H:%M:%S')).encode())
        errf.flush()
        self.proc = subprocess.Popen([self.binary], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                     stderr=errf, cwd=os.path.dirname(self.binary))
        self.q = queue.Queue()
        threading.Thread(target=self._reader, args=(self.proc.stdout, self.q), daemon=True).start()
        t0 = time.monotonic()
        self.request('initialize', {
            'processId': os.getpid(),
            'rootUri': path_to_uri(self.root),
            'rootPath': self.root,
            'capabilities': {'window': {'workDoneProgress': False},
                             'textDocument': {'documentSymbol': {'hierarchicalDocumentSymbolSupport': True}}},
            'initializationOptions': {},
        }, timeout=60)
        t_init = time.monotonic()
        self.notify('initialized', {})
        # The server indexes synchronously after replying to initialize, so the
        # first request after `initialized` is answered only once indexing is done.
        self.request('$/harness/ping', {}, timeout=300)
        self.index_ms = (time.monotonic() - t_init) * 1000.0
        self.init_ms = (t_init - t0) * 1000.0
        for uri, text in self.open_docs.items():
            self.notify('textDocument/didOpen', {'textDocument': {'uri': uri, 'languageId': 'tcl', 'version': 1, 'text': text}})

    def restart(self):
        self.kill()
        self.restarts += 1
        self.start()

    def kill(self):
        if self.proc and self.proc.poll() is None:
            try:
                self.proc.kill()
            except Exception:
                pass
            self.proc.wait()

    def stop(self):
        try:
            self.request('shutdown', None, timeout=10)
            self.notify('exit', None)
            self.proc.wait(timeout=5)
        except Exception:
            self.kill()
        return self.proc.returncode

    @staticmethod
    def _reader(stream, q):
        try:
            while True:
                length = None
                while True:
                    line = stream.readline()
                    if not line:
                        q.put(None)
                        return
                    line = line.strip()
                    if not line:
                        break
                    if line.lower().startswith(b'content-length:'):
                        length = int(line.split(b':', 1)[1])
                body = stream.read(length)
                if len(body) < length:
                    q.put(None)
                    return
                q.put(json.loads(body))
        except Exception as exc:  # malformed frame -> treat as death
            q.put({'__reader_error__': repr(exc)})
            q.put(None)

    def _send(self, msg):
        data = json.dumps(msg).encode('utf-8')
        try:
            self.proc.stdin.write(b'Content-Length: %d\r\n\r\n' % len(data) + data)
            self.proc.stdin.flush()
        except (BrokenPipeError, OSError):
            raise ServerDead('write failed')

    def notify(self, method, params):
        msg = {'jsonrpc': '2.0', 'method': method}
        if params is not None:
            msg['params'] = params
        self._send(msg)

    def request(self, method, params, timeout=10.0):
        """Returns (result, error, latency_ms). Raises Timeout / ServerDead."""
        self.next_id += 1
        rid = self.next_id
        msg = {'jsonrpc': '2.0', 'id': rid, 'method': method}
        if params is not None:
            msg['params'] = params
        t0 = time.monotonic()
        self._send(msg)
        deadline = t0 + timeout
        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                self.timeouts.append({'method': method, 'params': _short(params)})
                raise Timeout(method)
            try:
                m = self.q.get(timeout=remaining)
            except queue.Empty:
                continue
            if m is None:
                raise ServerDead(method)
            if '__reader_error__' in m:
                continue
            if 'method' in m and 'id' in m:   # server -> client request
                self.server_requests[m['method']] = self.server_requests.get(m['method'], 0) + 1
                self._send({'jsonrpc': '2.0', 'id': m['id'], 'result': None})
                continue
            if 'method' in m:                 # notification
                continue
            if m.get('id') != rid:
                continue                      # stale response
            ms = (time.monotonic() - t0) * 1000.0
            self.stats.setdefault(method, []).append(ms)
            if 'error' in m and m['error'] is not None:
                self.errors[method] = self.errors.get(method, 0) + 1
                return None, m['error'], ms
            return m.get('result'), None, ms

    def safe_request(self, method, params, timeout=10.0):
        """request() that records timeouts/deaths, restarts, and returns
        (result, status, ms) where status in ok|error|timeout|dead."""
        try:
            res, err, ms = self.request(method, params, timeout)
            return (err if err else res), ('error' if err else 'ok'), ms
        except Timeout:
            self.restart()
            return None, 'timeout', timeout * 1000.0
        except ServerDead:
            self.timeouts.append({'method': method, 'params': _short(params), 'dead': True})
            self.restart()
            return None, 'dead', 0.0

    def did_open(self, path, text):
        uri = path_to_uri(path)
        self.open_docs[uri] = text
        self.notify('textDocument/didOpen', {'textDocument': {'uri': uri, 'languageId': 'tcl', 'version': 1, 'text': text}})


def _short(params):
    try:
        s = json.dumps(params)
    except Exception:
        s = repr(params)
    return s[:300]
