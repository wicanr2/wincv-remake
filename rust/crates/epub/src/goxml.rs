//! Go 1.22 `encoding/xml` 的 Decoder 與 Unmarshal 中這一包用得到的部分。
//!
//! 為什麼不用現成的 XML crate:Go 版先用嚴格模式解,失敗了再用寬鬆模式
//! (`Strict = false`、`HTMLAutoClose`、`HTMLEntity`)**解進同一個結構**。
//! 嚴格模式失敗之前已經填進去的欄位不會清掉,寬鬆模式再往後接,
//! 所以壞掉的 OPF 會出現重複的章節;寬鬆模式自動關閉 `<meta>` 這類元素、
//! 錯配的結束標籤會往外一路關掉。這些行為決定了一本壞書讀得出幾章,
//! 而沒有一個 Rust 的 XML crate 是這樣做的,所以照 Go 的原始碼逐段搬:
//! tokenizer(rawToken)、元素堆疊(含命名空間宣告佔的位置)、錯誤訊息與行號,
//! 以及 Unmarshal 依欄位路徑(`a>b`)配對、遇錯回滾切片的規則。
//!
//! 命名空間只保留「堆疊上佔一格」這件事。欄位一律以 local name 配對,
//! Go 的 translate 只改 Space,不影響這裡的任何結果。

use crate::xmltables::{FIRST, HTML_ENTITY, SECOND};

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct Name {
    pub space: String,
    pub local: String,
}

#[derive(Clone, Debug)]
pub(crate) struct Attr {
    pub name: Name,
    pub value: String,
}

#[derive(Clone, Debug)]
pub(crate) struct Start {
    pub name: Name,
    pub attr: Vec<Attr>,
}

#[derive(Clone, Debug)]
enum Tok {
    Start(Start),
    End(Name),
    CharData(Vec<u8>),
    Other,
}

/// 解碼錯誤。訊息與 Go 的 `error.Error()` 相同。
#[derive(Clone, Debug, PartialEq, Eq)]
pub(crate) enum XmlError {
    Eof,
    Syntax { line: usize, msg: String },
    Other(String),
}

impl std::fmt::Display for XmlError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            XmlError::Eof => f.write_str("EOF"),
            XmlError::Syntax { line, msg } => write!(f, "XML syntax error on line {line}: {msg}"),
            XmlError::Other(s) => f.write_str(s),
        }
    }
}

type Res<T> = Result<T, XmlError>;

#[derive(Clone, Copy, PartialEq, Eq)]
enum StkKind {
    Start,
    Ns,
}

struct StkEntry {
    kind: StkKind,
    name: Name,
}

const HTML_AUTO_CLOSE: &[&str] = &[
    "basefont", "br", "area", "link", "img", "param", "hr", "input", "col", "frame", "isindex",
    "base", "meta",
];

/// Go 的 `Decoder.CharsetReader`:收宣告的編碼名稱與宣告之後的位元組,
/// 回解碼成 UTF-8 的位元組。
pub(crate) type CharsetReader<'a> = &'a dyn Fn(&str, &[u8]) -> Vec<u8>;

struct Decoder<'a> {
    /// 還沒讀的位元組從 `pos` 起。CharsetReader 換掉讀取來源時,換成解碼過的那一份。
    data: std::borrow::Cow<'a, [u8]>,
    pos: usize,
    charset: Option<CharsetReader<'a>>,
    strict: bool,
    err: Option<XmlError>,
    next_byte: Option<u8>,
    line: usize,
    buf: Vec<u8>,
    stk: Vec<StkEntry>,
    need_close: bool,
    to_close: Name,
    next_token: Option<Tok>,
}

impl<'a> Decoder<'a> {
    fn new(data: &'a [u8], strict: bool, charset: Option<CharsetReader<'a>>) -> Decoder<'a> {
        Decoder {
            data: std::borrow::Cow::Borrowed(data),
            pos: 0,
            charset,
            strict,
            err: None,
            next_byte: None,
            line: 1,
            buf: Vec::new(),
            stk: Vec::new(),
            need_close: false,
            to_close: Name::default(),
            next_token: None,
        }
    }

    fn syntax_error(&self, msg: impl Into<String>) -> XmlError {
        XmlError::Syntax {
            line: self.line,
            msg: msg.into(),
        }
    }

    fn fail<T>(&mut self, e: XmlError) -> Res<T> {
        self.err = Some(e.clone());
        Err(e)
    }

    fn cur_err(&self) -> XmlError {
        self.err.clone().unwrap_or(XmlError::Eof)
    }

    // ---- 位元組層 -------------------------------------------------------

    fn getc(&mut self) -> Option<u8> {
        if self.err.is_some() {
            return None;
        }
        let b = match self.next_byte.take() {
            Some(b) => b,
            None => {
                if self.pos >= self.data.len() {
                    self.err = Some(XmlError::Eof);
                    return None;
                }
                self.pos += 1;
                self.data[self.pos - 1]
            }
        };
        if b == b'\n' {
            self.line += 1;
        }
        Some(b)
    }

    fn mustgetc(&mut self) -> Option<u8> {
        let b = self.getc();
        if b.is_none() && self.err == Some(XmlError::Eof) {
            self.err = Some(self.syntax_error("unexpected EOF"));
        }
        b
    }

    fn ungetc(&mut self, b: u8) {
        if b == b'\n' {
            self.line -= 1;
        }
        self.next_byte = Some(b);
    }

    fn space(&mut self) {
        while let Some(b) = self.getc() {
            if !matches!(b, b' ' | b'\r' | b'\n' | b'\t') {
                self.ungetc(b);
                return;
            }
        }
    }

    // ---- token 層 -------------------------------------------------------

    fn token(&mut self) -> Res<Tok> {
        let mut t = match self.next_token.take() {
            Some(t) => t,
            None => match self.raw_token() {
                Ok(t) => t,
                Err(e) => {
                    if e == XmlError::Eof && !self.stk.is_empty() {
                        return Err(self.syntax_error("unexpected EOF"));
                    }
                    return Err(e);
                }
            },
        };
        if !self.strict {
            if let Some(t1) = self.auto_close(&t) {
                self.next_token = Some(t);
                t = t1;
            }
        }
        match &mut t {
            Tok::Start(s) => {
                for a in &s.attr {
                    if a.name.space == "xmlns" {
                        self.stk.push(StkEntry {
                            kind: StkKind::Ns,
                            name: Name::default(),
                        });
                    }
                    if a.name.space.is_empty() && a.name.local == "xmlns" {
                        self.stk.push(StkEntry {
                            kind: StkKind::Ns,
                            name: Name::default(),
                        });
                    }
                }
                self.stk.push(StkEntry {
                    kind: StkKind::Start,
                    name: s.name.clone(),
                });
            }
            Tok::End(name) => {
                if !self.pop_element(name) {
                    return Err(self.cur_err());
                }
            }
            _ => {}
        }
        Ok(t)
    }

    fn pop_element(&mut self, name: &mut Name) -> bool {
        let s = self.stk.pop();
        let Some(s) = s.filter(|s| s.kind == StkKind::Start) else {
            self.err = Some(self.syntax_error(format!("unexpected end element </{}>", name.local)));
            return false;
        };
        if s.name.local != name.local {
            if !self.strict {
                self.need_close = true;
                self.to_close = name.clone();
                *name = s.name;
                return true;
            }
            self.err = Some(self.syntax_error(format!(
                "element <{}> closed by </{}>",
                s.name.local, name.local
            )));
            return false;
        }
        if s.name.space != name.space {
            self.err = Some(self.syntax_error(format!(
                "element <{}> in space {} closed by </{}> in space {}",
                s.name.local, s.name.space, name.local, name.space
            )));
            return false;
        }
        while self.stk.last().is_some_and(|e| e.kind != StkKind::Start) {
            self.stk.pop();
        }
        true
    }

    fn auto_close(&self, t: &Tok) -> Option<Tok> {
        let top = self.stk.last().filter(|e| e.kind == StkKind::Start)?;
        for s in HTML_AUTO_CLOSE {
            if s.eq_ignore_ascii_case(&top.name.local) || go_equal_fold(s, &top.name.local) {
                match t {
                    Tok::End(n) if go_equal_fold(&n.local, &top.name.local) => {}
                    _ => return Some(Tok::End(top.name.clone())),
                }
                break;
            }
        }
        None
    }

    fn raw_token(&mut self) -> Res<Tok> {
        if let Some(e) = &self.err {
            return Err(e.clone());
        }
        if self.need_close {
            self.need_close = false;
            return Ok(Tok::End(self.to_close.clone()));
        }
        let Some(b) = self.getc() else {
            return Err(self.cur_err());
        };
        if b != b'<' {
            self.ungetc(b);
            return match self.text(None, false) {
                Some(d) => Ok(Tok::CharData(d)),
                None => Err(self.cur_err()),
            };
        }
        let Some(b) = self.mustgetc() else {
            return Err(self.cur_err());
        };
        match b {
            b'/' => {
                let Some(name) = self.nsname() else {
                    if self.err.is_none() {
                        let e = self.syntax_error("expected element name after </");
                        return self.fail(e);
                    }
                    return Err(self.cur_err());
                };
                self.space();
                let Some(b) = self.mustgetc() else {
                    return Err(self.cur_err());
                };
                if b != b'>' {
                    let e = self
                        .syntax_error(format!("invalid characters between </{} and >", name.local));
                    return self.fail(e);
                }
                return Ok(Tok::End(name));
            }
            b'?' => {
                let Some(target) = self.name() else {
                    if self.err.is_none() {
                        let e = self.syntax_error("expected target name after <?");
                        return self.fail(e);
                    }
                    return Err(self.cur_err());
                };
                self.space();
                self.buf.clear();
                let mut b0 = 0u8;
                loop {
                    let Some(b) = self.mustgetc() else {
                        return Err(self.cur_err());
                    };
                    self.buf.push(b);
                    if b0 == b'?' && b == b'>' {
                        break;
                    }
                    b0 = b;
                }
                let data = self.buf[..self.buf.len() - 2].to_vec();
                if target == "xml" {
                    let ver = proc_inst("version", &data);
                    if !ver.is_empty() && ver != b"1.0" {
                        let e = XmlError::Other(format!(
                            "xml: unsupported version {}; only version 1.0 is supported",
                            go_quote(&ver)
                        ));
                        return self.fail(e);
                    }
                    let enc = proc_inst("encoding", &data);
                    if !enc.is_empty() && !go_equal_fold(&String::from_utf8_lossy(&enc), "utf-8") {
                        let Some(cr) = self.charset else {
                            let e = XmlError::Other(format!(
                                "xml: encoding {} declared but Decoder.CharsetReader is nil",
                                go_quote(&enc)
                            ));
                            return self.fail(e);
                        };
                        // Go 的 switchToReader:宣告之後的位元組改從 CharsetReader 讀。
                        // 這時 next_byte 一定是空的(上面讀到 ?> 就停,沒有退回位元組)。
                        let rest = cr(&String::from_utf8_lossy(&enc), &self.data[self.pos..]);
                        self.data = std::borrow::Cow::Owned(rest);
                        self.pos = 0;
                    }
                }
                return Ok(Tok::Other);
            }
            b'!' => {
                let Some(b) = self.mustgetc() else {
                    return Err(self.cur_err());
                };
                match b {
                    b'-' => {
                        let Some(b) = self.mustgetc() else {
                            return Err(self.cur_err());
                        };
                        if b != b'-' {
                            let e = self.syntax_error("invalid sequence <!- not part of <!--");
                            return self.fail(e);
                        }
                        let (mut b0, mut b1) = (0u8, 0u8);
                        loop {
                            let Some(b) = self.mustgetc() else {
                                return Err(self.cur_err());
                            };
                            if b0 == b'-' && b1 == b'-' {
                                if b != b'>' {
                                    let e = self.syntax_error(
                                        "invalid sequence \"--\" not allowed in comments",
                                    );
                                    return self.fail(e);
                                }
                                break;
                            }
                            b0 = b1;
                            b1 = b;
                        }
                        return Ok(Tok::Other);
                    }
                    b'[' => {
                        for &want in b"CDATA[" {
                            let Some(b) = self.mustgetc() else {
                                return Err(self.cur_err());
                            };
                            if b != want {
                                let e = self.syntax_error("invalid <![ sequence");
                                return self.fail(e);
                            }
                        }
                        return match self.text(None, true) {
                            Some(d) => Ok(Tok::CharData(d)),
                            None => Err(self.cur_err()),
                        };
                    }
                    _ => {}
                }
                // 其他 <! 是 DOCTYPE 之類的指令:照 Go 數引號與巢狀的角括號,
                // 中間的 <!-- --> 註解整段跳過。
                let mut inquote = 0u8;
                let mut depth = 0i32;
                // 第一個位元組(<! 之後那一個)只記下來,不進下面的判斷,與 Go 相同。
                let mut pending: Option<u8> = None;
                loop {
                    let b = match pending.take() {
                        // goto HandleB:不檢查結束條件。
                        Some(p) => p,
                        None => {
                            let Some(nb) = self.mustgetc() else {
                                return Err(self.cur_err());
                            };
                            if inquote == 0 && nb == b'>' && depth == 0 {
                                break;
                            }
                            nb
                        }
                    };
                    // HandleB:
                    if b == inquote {
                        inquote = 0;
                    } else if inquote != 0 {
                    } else if b == b'\'' || b == b'"' {
                        inquote = b;
                    } else if b == b'>' {
                        depth -= 1;
                    } else if b == b'<' {
                        let mut matched = true;
                        for &want in b"!--" {
                            let Some(nb) = self.mustgetc() else {
                                return Err(self.cur_err());
                            };
                            if nb != want {
                                depth += 1;
                                pending = Some(nb);
                                matched = false;
                                break;
                            }
                        }
                        if !matched {
                            continue;
                        }
                        let (mut b0, mut b1) = (0u8, 0u8);
                        loop {
                            let Some(nb) = self.mustgetc() else {
                                return Err(self.cur_err());
                            };
                            if b0 == b'-' && b1 == b'-' && nb == b'>' {
                                break;
                            }
                            b0 = b1;
                            b1 = nb;
                        }
                    }
                }
                return Ok(Tok::Other);
            }
            _ => {}
        }

        self.ungetc(b);
        let Some(name) = self.nsname() else {
            if self.err.is_none() {
                let e = self.syntax_error("expected element name after <");
                return self.fail(e);
            }
            return Err(self.cur_err());
        };
        let mut attr = Vec::new();
        let mut empty = false;
        loop {
            self.space();
            let Some(b) = self.mustgetc() else {
                return Err(self.cur_err());
            };
            if b == b'/' {
                empty = true;
                let Some(b) = self.mustgetc() else {
                    return Err(self.cur_err());
                };
                if b != b'>' {
                    let e = self.syntax_error("expected /> in element");
                    return self.fail(e);
                }
                break;
            }
            if b == b'>' {
                break;
            }
            self.ungetc(b);
            let Some(an) = self.nsname() else {
                if self.err.is_none() {
                    let e = self.syntax_error("expected attribute name in element");
                    return self.fail(e);
                }
                return Err(self.cur_err());
            };
            self.space();
            let Some(b) = self.mustgetc() else {
                return Err(self.cur_err());
            };
            let value = if b != b'=' {
                if self.strict {
                    let e = self.syntax_error("attribute name without = in element");
                    return self.fail(e);
                }
                self.ungetc(b);
                an.local.clone()
            } else {
                self.space();
                match self.attrval() {
                    Some(d) => String::from_utf8_lossy(&d).into_owned(),
                    None => return Err(self.cur_err()),
                }
            };
            attr.push(Attr { name: an, value });
        }
        if empty {
            self.need_close = true;
            self.to_close = name.clone();
        }
        Ok(Tok::Start(Start { name, attr }))
    }

    fn attrval(&mut self) -> Option<Vec<u8>> {
        let b = self.mustgetc()?;
        if b == b'"' || b == b'\'' {
            return self.text(Some(b), false);
        }
        if self.strict {
            self.err = Some(self.syntax_error("unquoted or missing attribute value in element"));
            return None;
        }
        self.ungetc(b);
        self.buf.clear();
        loop {
            let b = self.mustgetc()?;
            if b.is_ascii_alphanumeric() || b == b'_' || b == b':' || b == b'-' {
                self.buf.push(b);
            } else {
                self.ungetc(b);
                break;
            }
        }
        Some(self.buf.clone())
    }

    /// 文字或屬性值。quote 為 None 表示讀到 `<` 為止。實體在這裡展開,
    /// 結果要是合法的 UTF-8 而且每個字元都在 XML 的字元範圍內,否則是錯誤。
    fn text(&mut self, quote: Option<u8>, cdata: bool) -> Option<Vec<u8>> {
        let (mut b0, mut b1) = (0u8, 0u8);
        let mut trunc = 0;
        self.buf.clear();
        loop {
            let Some(b) = self.getc() else {
                if cdata {
                    if self.err == Some(XmlError::Eof) {
                        self.err = Some(self.syntax_error("unexpected EOF in CDATA section"));
                    }
                    return None;
                }
                break;
            };
            if b0 == b']' && b1 == b']' && b == b'>' {
                if cdata {
                    trunc = 2;
                    break;
                }
                self.err = Some(self.syntax_error("unescaped ]]> not in CDATA section"));
                return None;
            }
            if b == b'<' && !cdata {
                if quote.is_some() {
                    self.err = Some(self.syntax_error("unescaped < inside quoted string"));
                    return None;
                }
                self.ungetc(b'<');
                break;
            }
            if quote == Some(b) {
                break;
            }
            if b == b'&' && !cdata {
                let before = self.buf.len();
                self.buf.push(b'&');
                let mut text: Option<String> = None;
                let mut b = self.mustgetc()?;
                if b == b'#' {
                    self.buf.push(b);
                    b = self.mustgetc()?;
                    let mut base = 10;
                    if b == b'x' {
                        base = 16;
                        self.buf.push(b);
                        b = self.mustgetc()?;
                    }
                    let start = self.buf.len();
                    while b.is_ascii_digit() || base == 16 && b.is_ascii_hexdigit() {
                        self.buf.push(b);
                        b = self.mustgetc()?;
                    }
                    if b != b';' {
                        self.ungetc(b);
                    } else {
                        let s = std::str::from_utf8(&self.buf[start..])
                            .unwrap_or("")
                            .to_string();
                        self.buf.push(b';');
                        // Go 的 strconv.ParseUint:空字串或溢位都是錯誤。
                        if let Ok(n) = u64::from_str_radix(&s, base) {
                            if n <= 0x10FFFF && !s.is_empty() {
                                // string(rune(n)):代理區換成 U+FFFD。
                                text = Some(
                                    char::from_u32(n as u32).unwrap_or('\u{FFFD}').to_string(),
                                );
                            }
                        }
                    }
                } else {
                    self.ungetc(b);
                    if !self.read_name() && self.err.is_some() {
                        return None;
                    }
                    let b = self.mustgetc()?;
                    if b != b';' {
                        self.ungetc(b);
                    } else {
                        let name = self.buf[before + 1..].to_vec();
                        self.buf.push(b';');
                        if is_name(&name) {
                            let s = String::from_utf8_lossy(&name).into_owned();
                            text = match s.as_str() {
                                "lt" => Some("<".into()),
                                "gt" => Some(">".into()),
                                "amp" => Some("&".into()),
                                "apos" => Some("'".into()),
                                "quot" => Some("\"".into()),
                                _ if !self.strict => html_entity(&s).map(|c| c.to_string()),
                                _ => None,
                            };
                        }
                    }
                }
                if let Some(t) = text {
                    self.buf.truncate(before);
                    self.buf.extend_from_slice(t.as_bytes());
                    b0 = 0;
                    b1 = 0;
                    continue;
                }
                if !self.strict {
                    b0 = 0;
                    b1 = 0;
                    continue;
                }
                let mut ent = String::from_utf8_lossy(&self.buf[before..]).into_owned();
                if !ent.ends_with(';') {
                    ent.push_str(" (no semicolon)");
                }
                self.err = Some(self.syntax_error(format!("invalid character entity {ent}")));
                return None;
            }
            // 沒有逃脫的 \r 與 \r\n 改寫成 \n。
            if b == b'\r' {
                self.buf.push(b'\n');
            } else if b1 == b'\r' && b == b'\n' {
                // \r 已經寫成 \n 了。
            } else {
                self.buf.push(b);
            }
            b0 = b1;
            b1 = b;
        }
        let n = self.buf.len() - trunc;
        let data = self.buf[..n].to_vec();
        // 逐字元檢查:不合法的 UTF-8 或不在 XML 字元範圍內都是錯誤。
        // Go 由前往後逐字元走,先遇到哪一種問題就報哪一種。
        let (good, bad) = match std::str::from_utf8(&data) {
            Ok(s) => (s, false),
            Err(e) => (
                std::str::from_utf8(&data[..e.valid_up_to()]).unwrap_or(""),
                true,
            ),
        };
        if let Some(c) = good.chars().find(|&c| !in_char_range(c as u32)) {
            self.err =
                Some(self.syntax_error(format!("illegal character code U+{:04X}", c as u32)));
            return None;
        }
        if bad {
            self.err = Some(self.syntax_error("invalid UTF-8"));
            return None;
        }
        Some(data)
    }

    fn nsname(&mut self) -> Option<Name> {
        let s = self.name()?;
        if s.matches(':').count() > 1 {
            return None;
        }
        match s.split_once(':') {
            Some((space, local)) if !space.is_empty() && !local.is_empty() => Some(Name {
                space: space.to_string(),
                local: local.to_string(),
            }),
            _ => Some(Name {
                space: String::new(),
                local: s,
            }),
        }
    }

    fn name(&mut self) -> Option<String> {
        self.buf.clear();
        if !self.read_name() {
            return None;
        }
        if !is_name(&self.buf) {
            let msg = format!("invalid XML name: {}", String::from_utf8_lossy(&self.buf));
            self.err = Some(self.syntax_error(msg));
            return None;
        }
        Some(String::from_utf8_lossy(&self.buf).into_owned())
    }

    /// 讀一串名稱位元組到 buf(接在後面)。ASCII 只收名稱字元,非 ASCII 一律收,
    /// 是不是合法名稱留給 is_name 判斷。
    fn read_name(&mut self) -> bool {
        let Some(b) = self.mustgetc() else {
            return false;
        };
        if b < 0x80 && !is_name_byte(b) {
            self.ungetc(b);
            return false;
        }
        self.buf.push(b);
        loop {
            let Some(b) = self.mustgetc() else {
                return false;
            };
            if b < 0x80 && !is_name_byte(b) {
                self.ungetc(b);
                break;
            }
            self.buf.push(b);
        }
        true
    }

    fn skip(&mut self) -> Res<()> {
        let mut depth = 0i64;
        loop {
            match self.token()? {
                Tok::Start(_) => depth += 1,
                Tok::End(_) => {
                    if depth == 0 {
                        return Ok(());
                    }
                    depth -= 1;
                }
                _ => {}
            }
        }
    }
}

fn is_name_byte(c: u8) -> bool {
    c.is_ascii_alphanumeric() || c == b'_' || c == b':' || c == b'.' || c == b'-'
}

fn in_table(t: &[(u32, u32, u32)], r: u32) -> bool {
    t.iter()
        .any(|&(lo, hi, st)| lo <= r && r <= hi && (r - lo) % st == 0)
}

fn is_name(s: &[u8]) -> bool {
    let Ok(s) = std::str::from_utf8(s) else {
        // Go 逐字元解,遇到壞位元組就回 false;整串不合法的情形結果相同。
        return false;
    };
    let mut it = s.chars();
    let Some(c) = it.next() else { return false };
    if !in_table(FIRST, c as u32) {
        return false;
    }
    it.all(|c| in_table(FIRST, c as u32) || in_table(SECOND, c as u32))
}

fn in_char_range(r: u32) -> bool {
    r == 0x09
        || r == 0x0A
        || r == 0x0D
        || (0x20..=0xD7FF).contains(&r)
        || (0xE000..=0xFFFD).contains(&r)
        || (0x10000..=0x10FFFF).contains(&r)
}

fn html_entity(name: &str) -> Option<char> {
    HTML_ENTITY
        .binary_search_by(|(k, _)| (*k).cmp(name))
        .ok()
        .map(|i| HTML_ENTITY[i].1)
}

/// Go 的 procInst:找 `param=` 之後用引號括起來的值。
fn proc_inst(param: &str, s: &[u8]) -> Vec<u8> {
    let pat = format!("{param}=");
    let pat = pat.as_bytes();
    let Some(i) = s.windows(pat.len()).position(|w| w == pat) else {
        return Vec::new();
    };
    let v = &s[i + pat.len()..];
    if v.is_empty() || (v[0] != b'\'' && v[0] != b'"') {
        return Vec::new();
    }
    match v[1..].iter().position(|&c| c == v[0]) {
        Some(j) => v[1..1 + j].to_vec(),
        None => Vec::new(),
    }
}

/// Go 的 strconv.Quote(只處理這裡會出現的情形:ASCII 與一般的 UTF-8)。
fn go_quote(s: &[u8]) -> String {
    let mut o = String::from("\"");
    let text = String::from_utf8_lossy(s);
    for c in text.chars() {
        match c {
            '"' => o.push_str("\\\""),
            '\\' => o.push_str("\\\\"),
            '\n' => o.push_str("\\n"),
            '\r' => o.push_str("\\r"),
            '\t' => o.push_str("\\t"),
            c if (c as u32) < 0x20 || c == '\u{7f}' => o.push_str(&format!("\\x{:02x}", c as u32)),
            c => o.push(c),
        }
    }
    o.push('"');
    o
}

/// Go 的 strings.EqualFold(Unicode 的簡單大小寫摺疊)。
pub(crate) fn go_equal_fold(a: &str, b: &str) -> bool {
    let mut x = a.chars();
    let mut y = b.chars();
    loop {
        match (x.next(), y.next()) {
            (None, None) => return true,
            (Some(p), Some(q)) => {
                if p != q && fold(p) != fold(q) {
                    return false;
                }
            }
            _ => return false,
        }
    }
}

/// 一個字元摺疊後的代表:取小寫,K(U+212A)與 ſ(U+017F)併到 k、s。
fn fold(c: char) -> char {
    match c {
        '\u{212A}' => 'k',
        '\u{017F}' => 's',
        _ => {
            let mut l = c.to_lowercase();
            match (l.next(), l.next()) {
                (Some(x), None) => x,
                _ => c,
            }
        }
    }
}

// ---- Unmarshal ----------------------------------------------------------

/// 一個元素欄位的路徑:Go 的 struct tag `a>b>name`。
pub(crate) struct Field {
    pub parents: &'static [&'static str],
    pub name: &'static str,
}

/// 可以被 Unmarshal 的結構:屬性欄位與元素欄位(依宣告順序)。
pub(crate) trait Target: Default {
    const FIELDS: &'static [Field];
    /// 一個屬性:local name 相同的欄位都設成這個值(Go 不看命名空間)。
    fn attr(&mut self, local: &str, value: &str);
    /// 把以 `start` 開頭的元素解進第 `i` 個元素欄位。
    fn field(&mut self, i: usize, d: &mut Dec, start: &Start) -> Result<(), XmlError>;
}

/// 給 Target 實作用的 Decoder 包裝(不讓外面碰到內部狀態)。
pub(crate) struct Dec<'a, 'b>(&'b mut Decoder<'a>);

/// Go 的切片欄位。解到一半失敗時 Go 用 `SetLen(n)` 回滾,但底層陣列那一格
/// 還留著填到一半的值,下一次 append 會**沿用**它(沒出現的屬性保留舊值)。
#[derive(Clone, Debug, Default)]
pub(crate) struct GoSlice<T> {
    pub v: Vec<T>,
    stale: Option<T>,
}

impl<T: Default> GoSlice<T> {
    fn push_with(&mut self, f: impl FnOnce(&mut T) -> Res<()>) -> Res<()> {
        let mut item = self.stale.take().unwrap_or_default();
        match f(&mut item) {
            Ok(()) => {
                self.v.push(item);
                Ok(())
            }
            Err(e) => {
                self.stale = Some(item);
                Err(e)
            }
        }
    }
}

impl Dec<'_, '_> {
    /// 字串欄位:只收這個元素自己的文字,子元素整個跳過。
    pub(crate) fn string(&mut self, out: &mut String) -> Res<()> {
        let mut data = Vec::new();
        loop {
            match self.0.token()? {
                Tok::Start(_) => self.0.skip()?,
                Tok::End(_) => break,
                Tok::CharData(c) => data.extend_from_slice(&c),
                Tok::Other => {}
            }
        }
        *out = String::from_utf8_lossy(&data).into_owned();
        Ok(())
    }

    pub(crate) fn strings(&mut self, out: &mut GoSlice<String>) -> Res<()> {
        out.push_with(|s| self.string(s))
    }

    pub(crate) fn object<T: Target>(&mut self, out: &mut T, start: &Start) -> Res<()> {
        unmarshal_struct(self.0, out, start)
    }

    pub(crate) fn objects<T: Target>(&mut self, out: &mut GoSlice<T>, start: &Start) -> Res<()> {
        let d = &mut *self.0;
        out.push_with(|v| unmarshal_struct(d, v, start))
    }
}

fn unmarshal_struct<T: Target>(d: &mut Decoder, v: &mut T, start: &Start) -> Res<()> {
    for a in &start.attr {
        v.attr(&a.name.local, &a.value);
    }
    loop {
        match d.token()? {
            Tok::Start(s) => {
                if !unmarshal_path(d, v, &[], &s)? {
                    d.skip()?;
                }
            }
            Tok::End(_) => return Ok(()),
            _ => {}
        }
    }
}

fn unmarshal_path<T: Target>(
    d: &mut Decoder,
    v: &mut T,
    parents: &[&'static str],
    start: &Start,
) -> Res<bool> {
    let mut recurse: Option<&'static [&'static str]> = None;
    'fields: for (i, f) in T::FIELDS.iter().enumerate() {
        if f.parents.len() < parents.len() {
            continue;
        }
        for (j, p) in parents.iter().enumerate() {
            if *p != f.parents[j] {
                continue 'fields;
            }
        }
        if f.parents.len() == parents.len() && f.name == start.name.local {
            v.field(i, &mut Dec(d), start)?;
            return Ok(true);
        }
        if f.parents.len() > parents.len() && f.parents[parents.len()] == start.name.local {
            recurse = Some(&f.parents[..parents.len() + 1]);
            break;
        }
    }
    let Some(parents) = recurse else {
        return Ok(false);
    };
    loop {
        match d.token()? {
            Tok::Start(s) => {
                if !unmarshal_path(d, v, parents, &s)? {
                    d.skip()?;
                }
            }
            Tok::End(_) => return Ok(true),
            _ => {}
        }
    }
}

/// Go 的 `xml.NewDecoder(data).Decode(v)`(嚴格或寬鬆模式),解進既有的 `v`。
/// 沒有 CharsetReader 的 Unmarshal:宣告 UTF-8 以外的編碼一律失敗(Go 的預設)。
#[cfg(test)]
pub(crate) fn decode<T: Target>(data: &[u8], v: &mut T, strict: bool) -> Res<()> {
    decode_with(data, v, strict, None)
}

/// Unmarshal,宣告 UTF-8 以外的編碼時交給 `charset` 解碼(Go 的 `Decoder.CharsetReader`)。
pub(crate) fn decode_with<T: Target>(
    data: &[u8],
    v: &mut T,
    strict: bool,
    charset: Option<CharsetReader<'_>>,
) -> Res<()> {
    let mut d = Decoder::new(data, strict, charset);
    let start = loop {
        if let Tok::Start(s) = d.token()? {
            break s;
        }
    };
    unmarshal_struct(&mut d, v, &start)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Default, Debug)]
    struct Item {
        id: String,
        props: String,
    }
    impl Target for Item {
        const FIELDS: &'static [Field] = &[];
        fn attr(&mut self, local: &str, value: &str) {
            match local {
                "id" => self.id = value.into(),
                "properties" => self.props = value.into(),
                _ => {}
            }
        }
        fn field(&mut self, _: usize, _: &mut Dec, _: &Start) -> Res<()> {
            Ok(())
        }
    }
    #[derive(Default, Debug)]
    struct Pkg {
        titles: GoSlice<String>,
        items: GoSlice<Item>,
    }
    impl Target for Pkg {
        const FIELDS: &'static [Field] = &[
            Field {
                parents: &["metadata"],
                name: "title",
            },
            Field {
                parents: &["manifest"],
                name: "item",
            },
        ];
        fn attr(&mut self, _: &str, _: &str) {}
        fn field(&mut self, i: usize, d: &mut Dec, start: &Start) -> Res<()> {
            match i {
                0 => d.strings(&mut self.titles),
                _ => d.objects(&mut self.items, start),
            }
        }
    }

    #[test]
    fn strict_errors_carry_go_messages() {
        let mut p = Pkg::default();
        let e = decode(
            b"<p>\n<metadata><title>a &nbsp; b</title></metadata></p>",
            &mut p,
            true,
        )
        .unwrap_err();
        assert_eq!(
            e.to_string(),
            "XML syntax error on line 2: invalid character entity &nbsp;"
        );
        let e = decode(
            b"<?xml version=\"1.0\" encoding=\"GBK\"?><p/>",
            &mut p,
            true,
        )
        .unwrap_err();
        assert_eq!(
            e.to_string(),
            "xml: encoding \"GBK\" declared but Decoder.CharsetReader is nil"
        );
        let e = decode(b"<p><a></b></p>", &mut p, true).unwrap_err();
        assert_eq!(
            e.to_string(),
            "XML syntax error on line 1: element <a> closed by </b>"
        );
        let e = decode(b"<p><a>", &mut p, true).unwrap_err();
        assert_eq!(e.to_string(), "XML syntax error on line 1: unexpected EOF");
    }

    // 寬鬆模式解進同一個結構:嚴格模式失敗前填好的會留著,之後再往後接。
    #[test]
    fn lenient_retry_appends() {
        let src = b"<p><manifest><item id=\"a\"/></manifest><metadata><title>&nbsp;x</title></metadata></p>";
        let mut p = Pkg::default();
        assert!(decode(src, &mut p, true).is_err());
        decode(src, &mut p, false).unwrap();
        let ids: Vec<&str> = p.items.v.iter().map(|i| i.id.as_str()).collect();
        assert_eq!(ids, ["a", "a"]);
        assert_eq!(p.titles.v, ["\u{a0}x"]);
    }

    // 寬鬆模式會自動關閉 <meta>,後面那個 </meta> 於是把外層一路關掉。
    #[test]
    fn lenient_meta_autoclose_cascades() {
        let src = b"<p><metadata><meta>2020</meta><title>t</title></metadata><manifest><item id=\"a\"/></manifest></p>";
        let mut p = Pkg::default();
        let r = decode(src, &mut p, false);
        assert!(
            p.titles.v.is_empty(),
            "title 不該被讀到:{:?} {r:?}",
            p.titles.v
        );
    }

    // 回滾的那一格會被下一次 append 沿用:沒出現的屬性留著舊值。
    #[test]
    fn rolled_back_slot_is_reused() {
        let mut p = Pkg::default();
        let _ = decode(
            b"<p><manifest><item id=\"a\" properties=\"nav\"><x>&bad;</x></item></manifest></p>",
            &mut p,
            true,
        );
        assert!(p.items.v.is_empty());
        decode(
            b"<p><manifest><item id=\"b\"/></manifest></p>",
            &mut p,
            true,
        )
        .unwrap();
        assert_eq!(
            (p.items.v[0].id.as_str(), p.items.v[0].props.as_str()),
            ("b", "nav")
        );
    }
}
