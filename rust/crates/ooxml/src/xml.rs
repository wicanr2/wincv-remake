//! Go `encoding/xml` 的 `Decoder` 逐 token 那一層,逐行移植(Go 1.22.12)。
//!
//! 為什麼不用現成的 XML crate(例如 quick-xml):Go 版是用
//! `Strict = false`、`Entity = HTMLEntity` 開的解碼器,而非嚴格模式改變的是
//! 壞檔的**結構**,不只是容錯程度:
//!
//!   - 對不上的結束標記不報錯,而是改成關閉堆疊頂端的元素,
//!     並把原本那個結束標記留到下一次再處理,一路往上關。
//!   - 不認得的實體原樣留在文字裡;沒有引號的屬性值、沒有值的屬性都收。
//!
//! 這些行為決定了上層走訪壞檔時「段落在哪裡結束」。換一個解析器會在這些
//! 地方得到另一種結構,所以照抄 Go 的狀態機。
//!
//! `AutoClose`(`br`、`col`、`hr` 這些名字不分命名空間、一打開就視為關閉)
//! 也照搬了,但 `new_decoder` 不開它,理由見該函式的說明。
//!
//! 沒有移植的:`CharsetReader`(Go 版沒有設,宣告非 UTF-8 編碼的檔案一律
//! 是錯誤)、`RawToken`、`Unmarshal` 用的 stkEOF、輸出的 escape。

use std::collections::HashMap;
use std::fmt;

use crate::xml_tables::{FIRST, HTML_ENTITY, SECOND};

/// 名稱。`space` 在 `token()` 回傳時已經換成命名空間網址(認得的話)。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Name {
    pub space: String,
    pub local: String,
}

/// 屬性。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Attr {
    pub name: Name,
    pub value: String,
}

/// 開始標記。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct StartElement {
    pub name: Name,
    pub attr: Vec<Attr>,
}

/// 一個 token。Go 的 `CharData` 一定是合法的 UTF-8(不合法的在解碼時就報錯),
/// 所以這裡用 `String`。
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Token {
    Start(StartElement),
    End(Name),
    CharData(String),
    Comment(Vec<u8>),
    ProcInst { target: String, inst: Vec<u8> },
    Directive(Vec<u8>),
}

/// 解碼錯誤。`Eof` 對應 Go 的 `io.EOF`:只有「所有元素都關好之後讀到結尾」才是它,
/// 截斷的檔案是 `Syntax("unexpected EOF")`。
#[derive(Clone, Debug, PartialEq, Eq)]
pub enum Error {
    Eof,
    Syntax { msg: String, line: i64 },
    Other(String),
}

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Error::Eof => f.write_str("EOF"),
            Error::Syntax { msg, line } => write!(f, "XML syntax error on line {line}: {msg}"),
            Error::Other(m) => f.write_str(m),
        }
    }
}

/// Go 的 `xml.HTMLAutoClose`。
pub const HTML_AUTO_CLOSE: &[&str] =
    &["basefont", "br", "area", "link", "img", "param", "hr", "input", "col", "frame", "isindex", "base", "meta"];

const XML_URL: &str = "http://www.w3.org/XML/1998/namespace";
const XMLNS_PREFIX: &str = "xmlns";
const XML_PREFIX: &str = "xml";

#[derive(Clone, Copy, PartialEq, Eq)]
enum StkKind {
    Start,
    Ns,
}

struct Stk {
    kind: StkKind,
    name: Name,
    ok: bool,
}

/// 解碼器。對應 Go 的 `*xml.Decoder`(讀的是一段已經在記憶體裡的位元組)。
pub struct Decoder {
    /// 對應 Go 的 `Strict`。
    pub strict: bool,
    /// 對應 Go 的 `AutoClose`。
    pub auto_close: &'static [&'static str],
    /// 對應 Go 的 `Entity = HTMLEntity`。
    pub html_entity: bool,
    /// 對應 Go 的 `DefaultSpace`。
    pub default_space: String,

    src: Vec<u8>,
    pos: usize,
    buf: Vec<u8>,
    stk: Vec<Stk>,
    need_close: bool,
    to_close: Name,
    next_token: Option<Token>,
    next_byte: Option<u8>,
    ns: HashMap<String, String>,
    err: Option<Error>,
    line: i64,
}

impl Decoder {
    /// 對應 Go 的 `xml.NewDecoder`:預設是嚴格模式。
    pub fn new(src: Vec<u8>) -> Decoder {
        Decoder {
            strict: true,
            auto_close: &[],
            html_entity: false,
            default_space: String::new(),
            src,
            pos: 0,
            buf: Vec::new(),
            stk: Vec::new(),
            need_close: false,
            to_close: Name::default(),
            next_token: None,
            next_byte: None,
            ns: HashMap::new(),
            err: None,
            line: 1,
        }
    }

    /// 下一個 token。語意與 Go 的 `Decoder.Token` 相同。
    pub fn token(&mut self) -> Result<Token, Error> {
        let mut t = match self.next_token.take() {
            Some(t) => t,
            None => match self.raw_token() {
                Ok(t) => t,
                Err(e) => {
                    if e == Error::Eof && !self.stk.is_empty() {
                        return Err(self.syntax_error("unexpected EOF"));
                    }
                    return Err(e);
                }
            },
        };
        if !self.strict {
            if let Some(t1) = self.auto_close_tok(&t) {
                self.next_token = Some(t);
                t = t1;
            }
        }
        match t {
            Token::Start(mut t1) => {
                // 命名空間的宣告要先處理:它同時作用在元素名與其他屬性名上。
                for a in &t1.attr {
                    if a.name.space == XMLNS_PREFIX {
                        let old = self.ns.get(&a.name.local).cloned();
                        self.push_ns(&a.name.local, old);
                        self.ns.insert(a.name.local.clone(), a.value.clone());
                    }
                    if a.name.space.is_empty() && a.name.local == XMLNS_PREFIX {
                        let old = self.ns.get("").cloned();
                        self.push_ns("", old);
                        self.ns.insert(String::new(), a.value.clone());
                    }
                }
                self.stk.push(Stk { kind: StkKind::Start, name: t1.name.clone(), ok: false });
                self.translate(&mut t1.name, true);
                for i in 0..t1.attr.len() {
                    let mut n = std::mem::take(&mut t1.attr[i].name);
                    self.translate(&mut n, false);
                    t1.attr[i].name = n;
                }
                Ok(Token::Start(t1))
            }
            Token::End(mut name) => {
                if !self.pop_element(&mut name) {
                    return Err(self.err.clone().unwrap_or(Error::Eof));
                }
                Ok(Token::End(name))
            }
            other => Ok(other),
        }
    }

    /// 跳過目前元素剩下的部分,停在它的結束標記之後。對應 Go 的 `Decoder.Skip`。
    pub fn skip(&mut self) -> Result<(), Error> {
        let mut depth: i64 = 0;
        loop {
            match self.token()? {
                Token::Start(_) => depth += 1,
                Token::End(_) => {
                    if depth == 0 {
                        return Ok(());
                    }
                    depth -= 1;
                }
                _ => {}
            }
        }
    }

    fn translate(&self, n: &mut Name, is_element: bool) {
        if n.space == XMLNS_PREFIX {
            return;
        } else if n.space.is_empty() && !is_element {
            return;
        } else if n.space == XML_PREFIX {
            n.space = XML_URL.to_string();
        } else if n.space.is_empty() && n.local == XMLNS_PREFIX {
            return;
        }
        if let Some(v) = self.ns.get(&n.space) {
            n.space = v.clone();
        } else if n.space.is_empty() {
            n.space = self.default_space.clone();
        }
    }

    fn push_ns(&mut self, local: &str, old: Option<String>) {
        let ok = old.is_some();
        self.stk.push(Stk {
            kind: StkKind::Ns,
            name: Name { space: old.unwrap_or_default(), local: local.to_string() },
            ok,
        });
    }

    fn syntax_error(&self, msg: &str) -> Error {
        Error::Syntax { msg: msg.to_string(), line: self.line }
    }

    /// 結束一個元素。名字要對得上堆疊頂端;非嚴格模式下對不上時,改成關閉
    /// 堆疊頂端那一個,原本的結束標記留到下一次(`need_close`)。
    fn pop_element(&mut self, t: &mut Name) -> bool {
        let s = self.stk.pop();
        let s = match s {
            Some(s) if s.kind == StkKind::Start => s,
            _ => {
                self.err = Some(self.syntax_error(&format!("unexpected end element </{}>", t.local)));
                return false;
            }
        };
        if s.name.local != t.local {
            if !self.strict {
                self.need_close = true;
                self.to_close = t.clone();
                *t = s.name;
                return true;
            }
            self.err = Some(self.syntax_error(&format!("element <{}> closed by </{}>", s.name.local, t.local)));
            return false;
        }
        if s.name.space != t.space {
            self.err = Some(self.syntax_error(&format!(
                "element <{}> in space {} closed by </{}> in space {}",
                s.name.local, s.name.space, t.local, t.space
            )));
            return false;
        }
        self.translate(t, true);
        // 把這個元素帶進來的命名空間宣告還原。
        while let Some(top) = self.stk.last() {
            if top.kind == StkKind::Start {
                break;
            }
            let s = self.stk.pop().unwrap();
            if s.ok {
                self.ns.insert(s.name.local, s.name.space);
            } else {
                self.ns.remove(&s.name.local);
            }
        }
        true
    }

    /// 堆疊頂端是自動關閉的元素,而 t 不是它的結束標記時,補一個結束標記。
    fn auto_close_tok(&self, t: &Token) -> Option<Token> {
        let top = self.stk.last()?;
        if top.kind != StkKind::Start {
            return None;
        }
        for s in self.auto_close {
            if equal_fold(s, &top.name.local) {
                match t {
                    Token::End(n) if equal_fold(&n.local, &top.name.local) => return None,
                    _ => return Some(Token::End(top.name.clone())),
                }
            }
        }
        None
    }

    fn raw_token(&mut self) -> Result<Token, Error> {
        if let Some(e) = &self.err {
            return Err(e.clone());
        }
        if self.need_close {
            // 上一個元素是自己關閉的(`<x/>`),先前只回了開始那一半。
            self.need_close = false;
            return Ok(Token::End(self.to_close.clone()));
        }

        let Some(b) = self.getc() else { return Err(self.cur_err()) };

        if b != b'<' {
            // 文字。
            self.ungetc(b);
            return match self.text(-1, false) {
                Some(data) => Ok(Token::CharData(data)),
                None => Err(self.cur_err()),
            };
        }

        let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
        match b {
            b'/' => {
                // </:結束標記
                let Some(name) = self.nsname() else {
                    if self.err.is_none() {
                        self.err = Some(self.syntax_error("expected element name after </"));
                    }
                    return Err(self.cur_err());
                };
                self.space();
                let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                if b != b'>' {
                    self.err = Some(self.syntax_error(&format!("invalid characters between </{} and >", name.local)));
                    return Err(self.cur_err());
                }
                Ok(Token::End(name))
            }
            b'?' => {
                // <?:處理指令
                let Some(target) = self.name() else {
                    if self.err.is_none() {
                        self.err = Some(self.syntax_error("expected target name after <?"));
                    }
                    return Err(self.cur_err());
                };
                self.space();
                self.buf.clear();
                let mut b0 = 0u8;
                loop {
                    let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                    self.buf.push(b);
                    if b0 == b'?' && b == b'>' {
                        break;
                    }
                    b0 = b;
                }
                let n = self.buf.len() - 2;
                let data = self.buf[..n].to_vec();
                if target == "xml" {
                    let content = String::from_utf8_lossy(&data).into_owned();
                    let ver = proc_inst("version", &content);
                    if !ver.is_empty() && ver != "1.0" {
                        self.err = Some(Error::Other(format!(
                            "xml: unsupported version {}; only version 1.0 is supported",
                            go_quote(&ver)
                        )));
                        return Err(self.cur_err());
                    }
                    let enc = proc_inst("encoding", &content);
                    if !enc.is_empty() && enc != "utf-8" && enc != "UTF-8" && !equal_fold(&enc, "utf-8") {
                        // Go 版沒有設 CharsetReader:宣告其他編碼的檔案一律是錯誤。
                        self.err = Some(Error::Other(format!(
                            "xml: encoding {} declared but Decoder.CharsetReader is nil",
                            go_quote(&enc)
                        )));
                        return Err(self.cur_err());
                    }
                }
                Ok(Token::ProcInst { target, inst: data })
            }
            b'!' => {
                // <!:註解、CDATA 或宣告
                let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                match b {
                    b'-' => {
                        let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                        if b != b'-' {
                            self.err = Some(self.syntax_error("invalid sequence <!- not part of <!--"));
                            return Err(self.cur_err());
                        }
                        self.buf.clear();
                        let (mut b0, mut b1) = (0u8, 0u8);
                        loop {
                            let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                            self.buf.push(b);
                            if b0 == b'-' && b1 == b'-' {
                                if b != b'>' {
                                    self.err = Some(self.syntax_error("invalid sequence \"--\" not allowed in comments"));
                                    return Err(self.cur_err());
                                }
                                break;
                            }
                            b0 = b1;
                            b1 = b;
                        }
                        let n = self.buf.len() - 3;
                        return Ok(Token::Comment(self.buf[..n].to_vec()));
                    }
                    b'[' => {
                        for &want in b"CDATA[" {
                            let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                            if b != want {
                                self.err = Some(self.syntax_error("invalid <![ sequence"));
                                return Err(self.cur_err());
                            }
                        }
                        return match self.text(-1, true) {
                            Some(data) => Ok(Token::CharData(data)),
                            None => Err(self.cur_err()),
                        };
                    }
                    _ => {}
                }
                // 宣告(<!DOCTYPE ...> 之類)。引號裡的角括號不算巢狀。
                self.buf.clear();
                self.buf.push(b);
                let mut inquote = 0u8;
                let mut depth = 0i64;
                loop {
                    let Some(mut b) = self.mustgetc() else { return Err(self.cur_err()) };
                    if inquote == 0 && b == b'>' && depth == 0 {
                        break;
                    }
                    // Go 的 `HandleB:` 標籤:看到 `<` 但不是 `<!--` 時,拿讀到的那個位元組重來一次。
                    'handle: loop {
                        self.buf.push(b);
                        if b == inquote {
                            inquote = 0;
                        } else if inquote != 0 {
                            // 引號裡,不做事
                        } else if b == b'\'' || b == b'"' {
                            inquote = b;
                        } else if b == b'>' && inquote == 0 {
                            depth -= 1;
                        } else if b == b'<' && inquote == 0 {
                            let s = b"!--";
                            for i in 0..s.len() {
                                let Some(nb) = self.mustgetc() else { return Err(self.cur_err()) };
                                b = nb;
                                if b != s[i] {
                                    for &c in &s[..i] {
                                        self.buf.push(c);
                                    }
                                    depth += 1;
                                    continue 'handle;
                                }
                            }
                            // 拿掉上面寫進去的 <。
                            self.buf.pop();
                            let (mut b0, mut b1) = (0u8, 0u8);
                            loop {
                                let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                                if b0 == b'-' && b1 == b'-' && b == b'>' {
                                    break;
                                }
                                b0 = b1;
                                b1 = b;
                            }
                            // 註解換成一個空白,讓被它隔開的兩段不會黏在一起。
                            self.buf.push(b' ');
                        }
                        break;
                    }
                }
                Ok(Token::Directive(self.buf.clone()))
            }
            _ => {
                // 開始標記,例如 <a href="foo">
                self.ungetc(b);
                let Some(name) = self.nsname() else {
                    if self.err.is_none() {
                        self.err = Some(self.syntax_error("expected element name after <"));
                    }
                    return Err(self.cur_err());
                };
                let mut attr = Vec::new();
                let mut empty = false;
                loop {
                    self.space();
                    let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                    if b == b'/' {
                        empty = true;
                        let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                        if b != b'>' {
                            self.err = Some(self.syntax_error("expected /> in element"));
                            return Err(self.cur_err());
                        }
                        break;
                    }
                    if b == b'>' {
                        break;
                    }
                    self.ungetc(b);
                    let Some(aname) = self.nsname() else {
                        if self.err.is_none() {
                            self.err = Some(self.syntax_error("expected attribute name in element"));
                        }
                        return Err(self.cur_err());
                    };
                    self.space();
                    let Some(b) = self.mustgetc() else { return Err(self.cur_err()) };
                    let value = if b != b'=' {
                        if self.strict {
                            self.err = Some(self.syntax_error("attribute name without = in element"));
                            return Err(self.cur_err());
                        }
                        self.ungetc(b);
                        aname.local.clone()
                    } else {
                        self.space();
                        match self.attrval() {
                            Some(v) => v,
                            None => return Err(self.cur_err()),
                        }
                    };
                    attr.push(Attr { name: aname, value });
                }
                if empty {
                    self.need_close = true;
                    self.to_close = name.clone();
                }
                Ok(Token::Start(StartElement { name, attr }))
            }
        }
    }

    fn cur_err(&self) -> Error {
        self.err.clone().unwrap_or(Error::Eof)
    }

    fn attrval(&mut self) -> Option<String> {
        let b = self.mustgetc()?;
        if b == b'"' || b == b'\'' {
            return self.text(b as i32, false);
        }
        if self.strict {
            self.err = Some(self.syntax_error("unquoted or missing attribute value in element"));
            return None;
        }
        // 非嚴格模式:沒有引號的屬性值。
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
        Some(String::from_utf8_lossy(&self.buf).into_owned())
    }

    fn space(&mut self) {
        while let Some(b) = self.getc() {
            match b {
                b' ' | b'\r' | b'\n' | b'\t' => {}
                _ => {
                    self.ungetc(b);
                    return;
                }
            }
        }
    }

    fn getc(&mut self) -> Option<u8> {
        if self.err.is_some() {
            return None;
        }
        let b = match self.next_byte.take() {
            Some(b) => b,
            None => {
                if self.pos >= self.src.len() {
                    self.err = Some(Error::Eof);
                    return None;
                }
                let b = self.src[self.pos];
                self.pos += 1;
                b
            }
        };
        if b == b'\n' {
            self.line += 1;
        }
        Some(b)
    }

    fn mustgetc(&mut self) -> Option<u8> {
        let b = self.getc();
        if b.is_none() && self.err == Some(Error::Eof) {
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

    /// 讀一段文字。quote >= 0 表示在引號裡;cdata 表示在 `<![CDATA[` 裡。
    /// 失敗時回 None,錯誤留在 self.err。
    fn text(&mut self, quote: i32, cdata: bool) -> Option<String> {
        let (mut b0, mut b1) = (0u8, 0u8);
        let mut trunc = 0;
        self.buf.clear();
        loop {
            let Some(b) = self.getc() else {
                if cdata {
                    if self.err == Some(Error::Eof) {
                        self.err = Some(self.syntax_error("unexpected EOF in CDATA section"));
                    }
                    return None;
                }
                break;
            };

            // <![CDATA[ 以 ]]> 結束;一般文字裡出現 ]]> 是錯誤。
            if b0 == b']' && b1 == b']' && b == b'>' {
                if cdata {
                    trunc = 2;
                    break;
                }
                self.err = Some(self.syntax_error("unescaped ]]> not in CDATA section"));
                return None;
            }

            if b == b'<' && !cdata {
                if quote >= 0 {
                    self.err = Some(self.syntax_error("unescaped < inside quoted string"));
                    return None;
                }
                self.ungetc(b'<');
                break;
            }
            if quote >= 0 && b as i32 == quote {
                break;
            }
            if b == b'&' && !cdata {
                // 實體參照,讀到分號為止。
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
                    while b.is_ascii_digit() || base == 16 && (b'a'..=b'f').contains(&b) || base == 16 && (b'A'..=b'F').contains(&b) {
                        self.buf.push(b);
                        b = self.mustgetc()?;
                    }
                    if b != b';' {
                        self.ungetc(b);
                    } else {
                        let s = std::str::from_utf8(&self.buf[start..]).unwrap_or("").to_string();
                        self.buf.push(b';');
                        if let Ok(n) = u64::from_str_radix(&s, base) {
                            if n <= 0x10FFFF {
                                // Go 的 string(rune(n)):代理區的碼位變成 U+FFFD。
                                text = Some(char::from_u32(n as u32).unwrap_or('\u{FFFD}').to_string());
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
                            let s = std::str::from_utf8(&name).unwrap_or("");
                            text = match s {
                                "lt" => Some("<".to_string()),
                                "gt" => Some(">".to_string()),
                                "amp" => Some("&".to_string()),
                                "apos" => Some("'".to_string()),
                                "quot" => Some("\"".to_string()),
                                _ if self.html_entity => html_entity(s).map(|v| v.to_string()),
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
                    // 不認得的實體原樣留下。
                    b0 = 0;
                    b1 = 0;
                    continue;
                }
                let mut ent = String::from_utf8_lossy(&self.buf[before..]).into_owned();
                if !ent.ends_with(';') {
                    ent.push_str(" (no semicolon)");
                }
                self.err = Some(self.syntax_error(&format!("invalid character entity {ent}")));
                return None;
            }

            // 沒有跳脫的 \r 與 \r\n 一律改成 \n。
            if b == b'\r' {
                self.buf.push(b'\n');
            } else if b1 == b'\r' && b == b'\n' {
                // \r 已經寫成 \n 了
            } else {
                self.buf.push(b);
            }
            b0 = b1;
            b1 = b;
        }
        let n = self.buf.len() - trunc;
        let data = &self.buf[..n];

        // 逐字檢查:不合法的 UTF-8 與 XML 不允許的字元都是錯誤。
        let (valid, bad) = match std::str::from_utf8(data) {
            Ok(s) => (s, false),
            Err(e) => (std::str::from_utf8(&data[..e.valid_up_to()]).unwrap_or(""), true),
        };
        for r in valid.chars() {
            if !is_in_character_range(r) {
                self.err = Some(self.syntax_error(&format!("illegal character code U+{:04X}", r as u32)));
                return None;
            }
        }
        if bad {
            self.err = Some(self.syntax_error("invalid UTF-8"));
            return None;
        }
        Some(valid.to_string())
    }

    /// 帶命名空間前綴的名稱。
    fn nsname(&mut self) -> Option<Name> {
        let s = self.name()?;
        if s.matches(':').count() > 1 {
            return None;
        }
        match s.split_once(':') {
            Some((space, local)) if !space.is_empty() && !local.is_empty() => {
                Some(Name { space: space.to_string(), local: local.to_string() })
            }
            _ => Some(Name { space: String::new(), local: s }),
        }
    }

    /// 名稱。名稱不在這裡時不設錯誤(除非讀到結尾),讓呼叫端給比較好的訊息。
    fn name(&mut self) -> Option<String> {
        self.buf.clear();
        if !self.read_name() {
            return None;
        }
        if !is_name(&self.buf) {
            let s = String::from_utf8_lossy(&self.buf).into_owned();
            self.err = Some(self.syntax_error(&format!("invalid XML name: {s}")));
            return None;
        }
        Some(String::from_utf8_lossy(&self.buf).into_owned())
    }

    /// 讀一個名稱接到 buf 後面。名稱在任何「不能出現在名稱裡的單位元組字元」處結束;
    /// 多位元組字元一律收,由呼叫端檢查。
    fn read_name(&mut self) -> bool {
        let Some(b) = self.mustgetc() else { return false };
        if b < 0x80 && !is_name_byte(b) {
            self.ungetc(b);
            return false;
        }
        self.buf.push(b);
        loop {
            let Some(b) = self.mustgetc() else { return false };
            if b < 0x80 && !is_name_byte(b) {
                self.ungetc(b);
                break;
            }
            self.buf.push(b);
        }
        true
    }
}

fn is_in_character_range(r: char) -> bool {
    let r = r as u32;
    r == 0x09
        || r == 0x0A
        || r == 0x0D
        || (0x20..=0xD7FF).contains(&r)
        || (0xE000..=0xFFFD).contains(&r)
        || (0x10000..=0x10FFFF).contains(&r)
}

fn is_name_byte(c: u8) -> bool {
    c.is_ascii_alphanumeric() || c == b'_' || c == b':' || c == b'.' || c == b'-'
}

fn in_table(t: &[(u32, u32, u32)], c: char) -> bool {
    let c = c as u32;
    t.iter().any(|&(lo, hi, stride)| c >= lo && c <= hi && (c - lo) % stride == 0)
}

fn is_name(s: &[u8]) -> bool {
    let Ok(s) = std::str::from_utf8(s) else {
        // Go 逐字解碼,遇到壞位元組就回 false;整段不合法時結果相同。
        return false;
    };
    let mut cs = s.chars();
    let Some(c) = cs.next() else { return false };
    if !in_table(FIRST, c) {
        return false;
    }
    cs.all(|c| in_table(FIRST, c) || in_table(SECOND, c))
}

fn html_entity(name: &str) -> Option<&'static str> {
    HTML_ENTITY.binary_search_by(|(k, _)| (*k).cmp(name)).ok().map(|i| HTML_ENTITY[i].1)
}

/// Go 的 `procInst`:從 `param="..."` 取出值,找不到回空字串。
fn proc_inst(param: &str, s: &str) -> String {
    let param = format!("{param}=");
    let Some((_, v)) = s.split_once(&param) else { return String::new() };
    if v.is_empty() {
        return String::new();
    }
    let q = v.as_bytes()[0];
    if q != b'\'' && q != b'"' {
        return String::new();
    }
    match v[1..].split_once(q as char) {
        Some((u, _)) => u.to_string(),
        None => String::new(),
    }
}

/// 近似 Go 的 `%q`,只用在錯誤訊息裡。
fn go_quote(s: &str) -> String {
    format!("{s:?}")
}

/// Go 的 `strings.EqualFold`:逐字元做簡單大小寫摺疊後比較。
pub fn equal_fold(a: &str, b: &str) -> bool {
    let (mut x, mut y) = (a.chars(), b.chars());
    loop {
        match (x.next(), y.next()) {
            (None, None) => return true,
            (Some(c), Some(d)) => {
                if c == d {
                    continue;
                }
                let lc = |c: char| c.to_lowercase().next().unwrap_or(c);
                let uc = |c: char| {
                    let mut u = c.to_uppercase();
                    match (u.next(), u.next()) {
                        (Some(v), None) => v,
                        _ => c,
                    }
                };
                if lc(c) != lc(d) && uc(c) != uc(d) {
                    return false;
                }
            }
            _ => return false,
        }
    }
}
