//! A Rust port of the tcl-lsp scanner and command/word parser, and only those
//! (research/09). It mirrors `server/internal/tcl/scanner.go`, `parser.go` and
//! the span helpers they call with a nil memo (`scriptclose.go`,
//! `scriptends.go`, `bracesHideBrackets` in `exprtree.go`), so it reads the same
//! words: `compare.sh` checks that byte for byte. Words borrow slices of the
//! source; nothing is copied. Offsets are byte offsets, as in Go.
//!
//! Like the Go code it recurses on nested braces and brackets, so a very deep
//! input needs a large stack (the binaries run on a 1 GiB thread).

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum TokenKind {
    Newline,
    Semicolon,
    Comment,
    Word,
}

/// A lexical unit and its byte range `[start, end)`. The end of input is the
/// end of the iteration, where Go emits `KindEOF`.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Token {
    pub kind: TokenKind,
    pub start: usize,
    pub end: usize,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WordKind {
    Bare,
    Braced,
    Quoted,
}

/// One word of a command: its raw text, borrowed from the source, and range.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Word<'a> {
    pub kind: WordKind,
    pub text: &'a [u8],
    pub start: usize,
    pub end: usize,
}

pub type Command<'a> = Vec<Word<'a>>;

/// Groups the tokens into commands, as written: comments dropped, newline and
/// semicolon separating, empty commands omitted. This is Go's `Parse` without
/// its head rewrite (`bareModeledHead`), which is analysis, not parsing.
pub fn parse(src: &[u8]) -> Vec<Command<'_>> {
    let mut cmds = Vec::new();
    let mut cur: Command = Vec::new();
    for tk in Scanner::new(src) {
        match tk.kind {
            TokenKind::Word => {
                let kind = match src[tk.start] {
                    b'{' => WordKind::Braced,
                    b'"' => WordKind::Quoted,
                    _ => WordKind::Bare,
                };
                cur.push(Word {
                    kind,
                    text: &src[tk.start..tk.end],
                    start: tk.start,
                    end: tk.end,
                });
            }
            TokenKind::Newline | TokenKind::Semicolon => {
                if !cur.is_empty() {
                    cmds.push(std::mem::take(&mut cur));
                }
            }
            TokenKind::Comment => {}
        }
    }
    if !cur.is_empty() {
        cmds.push(cur);
    }
    cmds
}

/// The tokens of src, as Go's `Scan` returns them (without the EOF token).
pub fn scan(src: &[u8]) -> Vec<Token> {
    Scanner::new(src).collect()
}

pub struct Scanner<'a> {
    src: &'a [u8],
    pos: usize,
    at_command_start: bool,
}

impl<'a> Scanner<'a> {
    pub fn new(src: &'a [u8]) -> Self {
        Scanner {
            src,
            pos: 0,
            at_command_start: true,
        }
    }

    fn scan_quoted(&mut self) {
        let s = self.src;
        self.pos += 1;
        while self.pos < s.len() {
            match s[self.pos] {
                b'\\' if self.pos + 1 < s.len() => {
                    self.pos += 2;
                    continue;
                }
                b'"' => {
                    self.pos += 1;
                    return;
                }
                b'[' => {
                    self.scan_bracket();
                    continue;
                }
                _ => {}
            }
            self.pos += 1;
        }
    }

    fn scan_comment(&mut self) -> Token {
        let s = self.src;
        let start = self.pos;
        while self.pos < s.len() && s[self.pos] != b'\n' {
            if s[self.pos] == b'\\' && self.pos + 1 < s.len() {
                self.pos += 2;
                continue;
            }
            self.pos += 1;
        }
        Token {
            kind: TokenKind::Comment,
            start,
            end: self.pos,
        }
    }

    fn scan_bare(&mut self) {
        let s = self.src;
        while self.pos < s.len() {
            match s[self.pos] {
                b'\\' if self.pos + 1 < s.len() => self.pos += 2,
                b'[' => self.scan_bracket(),
                b' ' | b'\t' | b'\r' | b'\n' | b';' => return,
                _ => self.pos += 1,
            }
        }
    }

    fn scan_bracket(&mut self) {
        let s = self.src;
        if let Some(end) = script_close(s, self.pos + 1) {
            self.pos = end + 1;
        } else if let Some(end) = trusted_depth_span(s, self.pos) {
            self.pos = end;
        } else {
            self.pos = s.len();
        }
    }
}

impl Iterator for Scanner<'_> {
    type Item = Token;

    fn next(&mut self) -> Option<Token> {
        let s = self.src;
        while self.pos < s.len() {
            let c = s[self.pos];
            match c {
                b' ' | b'\t' | b'\r' => self.pos += 1,
                b'\\' if self.pos + 1 < s.len() && s[self.pos + 1] == b'\n' => self.pos += 2,
                b'\n' | b';' => {
                    let start = self.pos;
                    self.pos += 1;
                    self.at_command_start = true;
                    let kind = if c == b'\n' {
                        TokenKind::Newline
                    } else {
                        TokenKind::Semicolon
                    };
                    return Some(Token {
                        kind,
                        start,
                        end: start + 1,
                    });
                }
                b'#' if self.at_command_start => return Some(self.scan_comment()),
                _ => {
                    let start = self.pos;
                    match c {
                        b'{' => self.pos = brace_span(s, start).0,
                        b'"' => self.scan_quoted(),
                        _ => self.scan_bare(),
                    }
                    self.at_command_start = false;
                    return Some(Token {
                        kind: TokenKind::Word,
                        start,
                        end: self.pos,
                    });
                }
            }
        }
        None
    }
}

fn is_list_space(c: u8) -> bool {
    c == b' ' || (b'\t'..=b'\r').contains(&c)
}

fn is_name_byte(c: u8) -> bool {
    c == b'_' || c.is_ascii_alphanumeric()
}

/// A [script] span's braces by depth count: the net count of `{` over `}`, and
/// the lowest running count.
#[derive(Clone, Copy, Default)]
struct BraceCounts {
    net: i32,
    low: i32,
}

/// Go's `scanDepth`: the depth count of the [script] span at the `[` at i. Returns
/// the counts, the offset just past its `]` (or len), and whether it closes.
fn scan_depth(s: &[u8], i: usize) -> (BraceCounts, usize, bool) {
    let (mut net, mut low) = (0i32, 0i32);
    let mut j = i + 1;
    while j < s.len() {
        match s[j] {
            b'\\' if j + 1 < s.len() => j += 2,
            b'[' => {
                let (inner, inner_end, ok) = scan_depth(s, j);
                if !ok {
                    return (BraceCounts::default(), s.len(), false);
                }
                low = low.min(net + inner.low);
                net += inner.net;
                j = inner_end;
            }
            b']' => return (BraceCounts { net, low }, j + 1, true),
            b'{' => {
                net += 1;
                j += 1;
            }
            b'}' => {
                net -= 1;
                low = low.min(net);
                j += 1;
            }
            _ => j += 1,
        }
    }
    (BraceCounts::default(), s.len(), false)
}

/// Go's `trustedDepthSpan`: the end of the depth-count span at the `[` at i when
/// it can be trusted.
fn trusted_depth_span(s: &[u8], i: usize) -> Option<usize> {
    let (_, end, closed) = scan_depth(s, i);
    if !closed {
        if end == i + 1 || s[end - 1] != b']' || braces_hide_brackets(&s[i + 1..end - 1]) {
            return None;
        }
        return Some(end);
    }
    if braces_hide_brackets(&s[i + 1..end - 1]) {
        None
    } else {
        Some(end)
    }
}

fn closed_trusted_span(s: &[u8], i: usize) -> Option<usize> {
    if !scan_depth(s, i).2 {
        return None;
    }
    trusted_depth_span(s, i)
}

/// Go's `braceSpan`: the offset just past the {braced} span at i, and whether it
/// closes. Backslash-aware; unterminated runs to len.
fn brace_span(s: &[u8], i: usize) -> (usize, bool) {
    let mut j = i + 1;
    while j < s.len() {
        match s[j] {
            b'\\' if j + 1 < s.len() => j += 2,
            b'{' => {
                let (e, ok) = brace_span(s, j);
                if !ok {
                    return (s.len(), false);
                }
                j = e;
            }
            b'}' => return (j + 1, true),
            _ => j += 1,
        }
    }
    (s.len(), false)
}

/// Go's `scriptClose`: the offset of the `]` closing the command substitution
/// whose interior starts at i, read as Tcl reads it, or None.
fn script_close(s: &[u8], mut i: usize) -> Option<usize> {
    let (mut at_word, mut at_cmd) = (true, true);
    while i < s.len() {
        let c = s[i];
        let (was_word, was_cmd) = (at_word, at_cmd);
        at_word = false;
        at_cmd = false;
        if c == b'\\' {
            if i + 1 < s.len() && s[i + 1] == b'\n' {
                at_word = true;
                at_cmd = was_cmd;
            }
            i += 2;
            continue;
        } else if c == b';' || c == b'\n' {
            at_word = true;
            at_cmd = true;
        } else if is_list_space(c) {
            at_word = true;
            at_cmd = was_cmd;
        } else if c == b'#' && was_cmd {
            i = comment_end(s, i);
            at_word = true;
            at_cmd = true;
            continue;
        } else if c == b']' {
            return Some(i);
        } else if c == b'[' {
            i = script_close(s, i + 1)? + 1;
            continue;
        } else if c == b'{' && was_word && is_expand_prefix(s, i) {
            i += 3;
            at_word = true;
            continue;
        } else if c == b'{' && was_word {
            let (j, ok) = brace_span(s, i);
            if !ok {
                return None;
            }
            i = j;
            continue;
        } else if c == b'"' && was_word {
            i = quote_close(s, i + 1)? + 1;
            continue;
        } else if c == b'$' {
            i = var_end(s, i)?;
            continue;
        }
        i += 1;
    }
    None
}

fn is_expand_prefix(s: &[u8], i: usize) -> bool {
    if !s[i..].starts_with(b"{*}") || i + 3 >= s.len() {
        return false;
    }
    match s[i + 3] {
        c if is_list_space(c) => false,
        b';' | b']' => false,
        b'\\' => i + 4 >= s.len() || s[i + 4] != b'\n',
        _ => true,
    }
}

fn comment_end(s: &[u8], mut i: usize) -> usize {
    while i < s.len() && s[i] != b'\n' {
        if s[i] == b'\\' {
            i += 1;
        }
        i += 1;
    }
    i.min(s.len())
}

fn quote_close(s: &[u8], mut i: usize) -> Option<usize> {
    while i < s.len() {
        match s[i] {
            b'\\' => i += 2,
            b'"' => return Some(i),
            b'[' => i = script_close(s, i + 1)? + 1,
            b'$' => i = var_end(s, i)?,
            _ => i += 1,
        }
    }
    None
}

fn var_end(s: &[u8], i: usize) -> Option<usize> {
    let mut j = i + 1;
    if j < s.len() && s[j] == b'{' {
        for (k, &c) in s.iter().enumerate().skip(j + 1) {
            match c {
                b'}' => return Some(k + 1),
                b'{' | b'\\' => return None,
                _ => {}
            }
        }
        return None;
    }
    j = name_end(s, j);
    if j >= s.len() || s[j] != b'(' {
        return Some(j.max(i + 1));
    }
    j += 1;
    while j < s.len() {
        match s[j] {
            b'\\' => j += 2,
            b')' => return Some(j + 1),
            b'[' => j = script_close(s, j + 1)? + 1,
            b'$' => j = var_end(s, j)?,
            _ => j += 1,
        }
    }
    None
}

fn name_end(s: &[u8], mut i: usize) -> usize {
    while i < s.len() {
        if is_name_byte(s[i]) {
            i += 1;
        } else if s[i] == b':' && i + 1 < s.len() && s[i + 1] == b':' {
            while i < s.len() && s[i] == b':' {
                i += 1;
            }
        } else {
            break;
        }
    }
    i
}

fn array_index_at(s: &[u8], i: usize) -> usize {
    let j = name_end(s, i);
    if j < s.len() && s[j] == b'(' {
        j
    } else {
        0
    }
}

/// Go's `jumpBraced`: moves *i to the `]` of the [script] span at *i when it
/// closes, adjusting *braces by the span's braces.
fn jump_braced(s: &[u8], i: &mut usize, braces: &mut i32) -> bool {
    let (e, end, closed) = scan_depth(s, *i);
    if !closed {
        return false;
    }
    if *braces + e.low <= 0 {
        *braces = 0;
    } else {
        *braces += e.net;
    }
    *i = end - 1;
    true
}

/// Go's `bracesHideBrackets`: whether something in a [script] interior may hide
/// its `]` from the depth count.
fn braces_hide_brackets(script: &[u8]) -> bool {
    let n = script.len();
    let (mut braces, mut brackets) = (0i32, 0i32);
    let mut quoted = false;
    let mut indexes: Vec<i32> = Vec::new();
    let (mut word_start, mut cmd_start) = (true, true);
    let mut i = 0;
    while i < n {
        let c = script[i];
        let (at_word, at_cmd) = (word_start, cmd_start);
        word_start = false;
        cmd_start = false;
        let bare = braces == 0 && !quoted;
        let in_index = braces == 0 && indexes.last() == Some(&0);
        if c == b'\\' {
            if i + 1 < n && script[i + 1] == b'\n' && bare {
                word_start = true;
                cmd_start = at_cmd;
            }
            i += 1;
        } else if in_index && c != b'$' && c != b'[' && c != b']' && c != b')' {
        } else if bare && (c == b';' || c == b'\n') {
            word_start = true;
            cmd_start = true;
        } else if bare && is_list_space(c) {
            word_start = true;
            cmd_start = at_cmd;
        } else if bare && c == b'$' && i + 1 < n && script[i + 1] == b'{' {
            let rest = &script[i + 2..];
            match rest.iter().position(|&b| b == b'}') {
                Some(end) if !rest[..end].iter().any(|b| b"[]{\\".contains(b)) => i += 2 + end,
                _ => return true,
            }
        } else if braces == 0 && c == b'$' && array_index_at(script, i + 1) > 0 {
            i = array_index_at(script, i + 1);
            indexes.push(0);
        } else if bare && c == b'[' && indexes.is_empty() {
            if let Some(end) = closed_trusted_span(script, i) {
                i = end - 1;
            } else {
                word_start = true;
                cmd_start = true;
            }
        } else if bare && c == b'[' {
            *indexes.last_mut().unwrap() += 1;
            word_start = true;
            cmd_start = true;
        } else if bare && c == b']' && !indexes.is_empty() {
            let last = indexes.last_mut().unwrap();
            if *last == 0 {
                return true;
            }
            *last -= 1;
        } else if in_index && c == b')' {
            indexes.pop();
        } else if bare && c == b'#' && at_cmd {
            return true;
        } else if c == b'"' && braces == 0 {
            if quoted {
                if brackets != 0 {
                    return true;
                }
                quoted = false;
            } else if at_word {
                quoted = true;
            }
        } else if bare && c == b'{' && at_word && is_expand_prefix(script, i) {
            i += 2;
            word_start = true;
        } else if c == b'{' && !quoted && (braces > 0 || at_word) {
            braces += 1;
        } else if c == b'}' && !quoted && braces > 0 {
            braces -= 1;
            if braces == 0 && brackets != 0 {
                return true;
            }
        } else if braces > 0 && c == b'[' && jump_braced(script, &mut i, &mut braces) {
            if braces <= 0 {
                return true;
            }
        } else if (braces > 0 || quoted) && c == b'[' {
            if quoted {
                if let Some(last) = indexes.last_mut() {
                    *last += 1;
                }
            }
            brackets += 1;
        } else if (braces > 0 || quoted) && c == b']' {
            if quoted && in_index {
                return true;
            }
            if quoted {
                if let Some(last) = indexes.last_mut() {
                    *last -= 1;
                }
            }
            brackets -= 1;
            if brackets < 0 {
                return true;
            }
        }
        i += 1;
    }
    braces != 0 || quoted || !indexes.is_empty()
}

/// Renders src's commands in the shared dump format (research/09): one command
/// per line, each an array of {"kind","text","start","end"} objects.
pub fn dump(src: &[u8]) -> Vec<u8> {
    let cmds = parse(src);
    if cmds.is_empty() {
        return b"[]\n".to_vec();
    }
    let mut b = Vec::with_capacity(src.len() * 4);
    b.extend_from_slice(b"[\n");
    for (i, cmd) in cmds.iter().enumerate() {
        if i > 0 {
            b.extend_from_slice(b",\n");
        }
        b.push(b'[');
        for (j, w) in cmd.iter().enumerate() {
            if j > 0 {
                b.push(b',');
            }
            let kind = match w.kind {
                WordKind::Bare => "bare",
                WordKind::Braced => "braced",
                WordKind::Quoted => "quoted",
            };
            b.extend_from_slice(format!(r#"{{"kind":"{kind}","text":"#).as_bytes());
            write_dump_string(&mut b, w.text);
            b.extend_from_slice(format!(r#","start":{},"end":{}}}"#, w.start, w.end).as_bytes());
        }
        b.push(b']');
    }
    b.extend_from_slice(b"\n]\n");
    b
}

/// A JSON string: `"` and `\` escaped, bytes below 0x20 and 0x7f as \u00xx, valid
/// UTF-8 as is, and each byte outside a valid UTF-8 sequence as \u00xx.
fn write_dump_string(b: &mut Vec<u8>, mut s: &[u8]) {
    b.push(b'"');
    loop {
        let (valid, invalid) = match std::str::from_utf8(s) {
            Ok(_) => (s.len(), 0),
            Err(e) => (
                e.valid_up_to(),
                e.error_len().unwrap_or(s.len() - e.valid_up_to()),
            ),
        };
        for &c in &s[..valid] {
            match c {
                b'"' | b'\\' => b.extend_from_slice(&[b'\\', c]),
                c if c < 0x20 || c == 0x7f => b.extend_from_slice(format!("\\u{c:04x}").as_bytes()),
                c => b.push(c),
            }
        }
        for &c in &s[valid..valid + invalid] {
            b.extend_from_slice(format!("\\u{c:04x}").as_bytes());
        }
        s = &s[valid + invalid..];
        if s.is_empty() {
            break;
        }
    }
    b.push(b'"');
}

/// Every regular *.tcl file under root, sorted by its path relative to root
/// (as bytes, matching Go's string order). Symlinks inside root are not followed.
pub fn load_corpus(root: &std::path::Path) -> std::io::Result<Vec<(String, Vec<u8>)>> {
    let root = std::fs::canonicalize(root)?;
    if !root.is_dir() {
        return Err(std::io::Error::new(
            std::io::ErrorKind::NotADirectory,
            "corpus is not a directory",
        ));
    }
    let mut files = Vec::new();
    let mut stack = vec![root.clone()];
    while let Some(dir) = stack.pop() {
        for entry in std::fs::read_dir(&dir)? {
            let entry = entry?;
            let ft = entry.file_type()?;
            let path = entry.path();
            if ft.is_dir() {
                stack.push(path);
            } else if ft.is_file() && entry.file_name().as_encoded_bytes().ends_with(b".tcl") {
                let rel = path
                    .strip_prefix(&root)
                    .unwrap()
                    .to_string_lossy()
                    .into_owned();
                files.push((rel, std::fs::read(&path)?));
            }
        }
    }
    files.sort_by(|a, b| a.0.as_bytes().cmp(b.0.as_bytes()));
    Ok(files)
}

/// The corpus root: $TCL_CORPUS, else the Homebrew Tcl library.
pub fn corpus_root() -> std::path::PathBuf {
    std::env::var_os("TCL_CORPUS")
        .filter(|v| !v.is_empty())
        .map(Into::into)
        .unwrap_or_else(|| "/opt/homebrew/opt/tcl-tk/lib".into())
}

/// Runs f on a thread with a 1 GiB stack, for deeply nested input.
pub fn with_big_stack<T: Send + 'static>(f: impl FnOnce() -> T + Send + 'static) -> T {
    std::thread::Builder::new()
        .stack_size(1 << 30)
        .spawn(f)
        .unwrap()
        .join()
        .unwrap()
}
