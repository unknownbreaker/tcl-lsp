"""Byte offset <-> LSP (line, UTF-16 character) conversion matching the server's
Go implementation (invalid UTF-8 bytes count as one U+FFFD = 1 UTF-16 unit)."""
import bisect


def _u16len(bs):
    if bs.isascii():
        return len(bs)
    n = 0
    for ch in bs.decode('utf-8', 'surrogateescape'):
        o = ord(ch)
        n += 2 if o > 0xFFFF else 1
    return n


class LineIndex:
    def __init__(self, data):
        self.data = data
        self.starts = [0]
        i = data.find(b'\n')
        while i >= 0:
            self.starts.append(i + 1)
            i = data.find(b'\n', i + 1)

    def pos(self, off):
        off = max(0, min(off, len(self.data)))
        line = bisect.bisect_right(self.starts, off) - 1
        return line, _u16len(self.data[self.starts[line]:off])

    def off(self, line, char):
        if line >= len(self.starts):
            return len(self.data)
        st = self.starts[line]
        end = self.starts[line + 1] - 1 if line + 1 < len(self.starts) else len(self.data)
        seg = self.data[st:end]
        if seg.isascii():
            return st + min(char, len(seg))
        u = 0
        txt = seg.decode('utf-8', 'surrogateescape')
        b = 0
        for ch in txt:
            if u >= char:
                break
            o = ord(ch)
            u += 2 if o > 0xFFFF else 1
            b += 1 if 0xDC80 <= o <= 0xDCFF else len(ch.encode('utf-8'))
        return st + b

    def line_text(self, line):
        if line >= len(self.starts):
            return ''
        st = self.starts[line]
        end = self.starts[line + 1] - 1 if line + 1 < len(self.starts) else len(self.data)
        return self.data[st:end].decode('utf-8', 'replace').strip()[:200]

    def text(self, a, b):
        return self.data[a:b].decode('utf-8', 'replace')

    def lsp(self, off):
        ln, ch = self.pos(off)
        return {'line': ln, 'character': ch}
