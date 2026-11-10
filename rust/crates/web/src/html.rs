//! HTML 壓成排版區塊(對應 Go 版 `html.go`)。
//!
//! Go 版用 golang.org/x/net/html 的 Tokenizer(只切 token,不建樹),這裡用
//! html5ever 的 tokenizer,兩者都照 WHATWG 的 tokenization 規格。只用 tokenizer
//! 時有幾件事是樹建構器原本會做、得由這一層自己補的,補法照 Go 版的行為:
//!
//! - 遇到 script、style、title、textarea 這類元素的開始標籤,要切到 raw text /
//!   RCDATA 狀態。Go 的 tokenizer 自己切(它把 noscript 當 raw text,等於
//!   「腳本開著」),html5ever 的 tokenizer 等 sink 回 `RawData` 才切。
//! - html5ever 一段文字會分好幾個 token 送;Go 是「兩個標籤之間的文字」一次給。
//!   空白摺疊是逐個文字 token 做的,切點不同結果就不同,所以這裡先接起來,
//!   遇到非文字的 token 才交出去。parse error 不算 token,不會把文字切斷。
//! - Go 的 tokenizer 不丟開頭的 BOM,這裡也不丟。

use std::cell::RefCell;

use html5ever::tendril::StrTendril;
use html5ever::tokenizer::states::RawKind;
use html5ever::tokenizer::{
    BufferQueue, Tag, TagKind, Token, TokenSink, TokenSinkResult, Tokenizer, TokenizerOpts,
};
use wincv_i18n::t;
use wincv_markdown::{Block, Kind, Span, Style};

use crate::gostr;
use crate::gourl;

/// 把一份 HTML 拆成排版用的區塊,回傳標題與區塊。
///
/// 產出的是 markdown 那一套區塊,不是 DOM:這一層做的就是「丟掉」。
/// 樣式表、腳本、版面用的巢狀容器全部不留,剩下標題、段落、清單、
/// 引言、預格式化、表格、連結與圖片。畫面只有字元格點,那些丟掉的
/// 東西本來也畫不出來。
///
/// `base` 用來把相對位址接成絕對位址(照 Go 的 net/url 規則)。傳 `None`,
/// 或 `base` 本身解不開的話,相對位址原樣留著。`src` 是原始位元組:
/// 不合法的 UTF-8 照 Go 的規則處理(見 `gostr`)。
pub fn parse_html(base: Option<&str>, src: &[u8]) -> (String, Vec<Block>) {
    let base = base.and_then(|b| gourl::parse(&gostr::to_bytes(b)).ok());
    parse_html_url(base, src)
}

pub(crate) fn parse_html_url(base: Option<gourl::Url>, src: &[u8]) -> (String, Vec<Block>) {
    let sink = Sink {
        p: RefCell::new(Parser::new(base)),
        pending: RefCell::new(String::new()),
    };
    let opts = TokenizerOpts {
        discard_bom: false,
        ..Default::default()
    };
    let tok = Tokenizer::new(sink, opts);
    let input = BufferQueue::default();
    input.push_back(StrTendril::from(gostr::from_bytes(src)));
    let _ = tok.feed(&input);
    tok.end();
    let p = tok.sink.p.into_inner();
    let blocks = p.out.into_iter().map(finish_block).collect();
    (gostr::finish(&p.title), blocks)
}

fn finish_block(mut b: Block) -> Block {
    for sp in &mut b.spans {
        sp.text = gostr::finish(&sp.text);
        sp.href = gostr::finish(&sp.href);
    }
    for ln in &mut b.lines {
        *ln = gostr::finish(ln);
    }
    b.src = gostr::finish(&b.src);
    b.alt = gostr::finish(&b.alt);
    b
}

struct Sink {
    p: RefCell<Parser>,
    /// 還沒交出去的文字(見檔頭的說明)。
    pending: RefCell<String>,
}

impl Sink {
    fn flush_text(&self) {
        let s = std::mem::take(&mut *self.pending.borrow_mut());
        if !s.is_empty() {
            self.p.borrow_mut().text(&s);
        }
    }
}

impl TokenSink for Sink {
    type Handle = ();

    fn process_token(&self, token: Token, _line: u64) -> TokenSinkResult<()> {
        match token {
            Token::CharacterTokens(s) => {
                self.pending.borrow_mut().push_str(&s);
                TokenSinkResult::Continue
            }
            Token::NullCharacterToken => {
                // Go 的 tokenizer 在一般文字裡原樣留著 NUL。
                self.pending.borrow_mut().push('\0');
                TokenSinkResult::Continue
            }
            Token::ParseError(_) => TokenSinkResult::Continue,
            Token::TagToken(tag) => {
                self.flush_text();
                self.tag(tag)
            }
            Token::CommentToken(_) | Token::DoctypeToken(_) => {
                self.flush_text();
                TokenSinkResult::Continue
            }
            Token::EOFToken => {
                self.flush_text();
                self.p.borrow_mut().flush();
                TokenSinkResult::Continue
            }
        }
    }
}

impl Sink {
    fn tag(&self, tag: Tag) -> TokenSinkResult<()> {
        let name: &str = &tag.name;
        let mut p = self.p.borrow_mut();
        match tag.kind {
            TagKind::EndTag => {
                p.end(name);
                TokenSinkResult::Continue
            }
            TagKind::StartTag => {
                let attrs: Vec<(String, String)> = tag
                    .attrs
                    .iter()
                    .map(|a| (a.name.local.to_string(), a.value.to_string()))
                    .collect();
                p.start(name, &attrs);
                // 自閉合的標籤不會有結束標籤,立刻收尾。
                // br/hr/img 本來就沒有內容,不必等。
                if tag.self_closing && !void_element(name) {
                    p.end(name);
                }
                // Go 的 tokenizer 不管自閉合與否,看到這幾個開始標籤就切狀態。
                match name {
                    "iframe" | "noembed" | "noframes" | "noscript" | "style" | "xmp" => {
                        TokenSinkResult::RawData(RawKind::Rawtext)
                    }
                    "script" => TokenSinkResult::RawData(RawKind::ScriptData),
                    "textarea" | "title" => TokenSinkResult::RawData(RawKind::Rcdata),
                    "plaintext" => TokenSinkResult::Plaintext,
                    _ => TokenSinkResult::Continue,
                }
            }
        }
    }
}

/// 規格上就沒有結束標籤的元素。start 已經處理完它們,
/// 再送一次 end 會把樣式堆疊弄亂。
fn void_element(a: &str) -> bool {
    matches!(a, "br" | "hr" | "img" | "input" | "meta" | "link")
}

/// 整個子樹都不要的元素。
///
/// script/style 的內容不是給人看的;svg/canvas 畫不出來;
/// iframe/object 是另一份文件,這個客戶端不會去取;
/// select/textarea 是表單控制項,沒有互動就只剩雜訊。
fn skipped(a: &str) -> bool {
    matches!(
        a,
        "script"
            | "style"
            | "noscript"
            | "svg"
            | "canvas"
            | "iframe"
            | "object"
            | "select"
            | "textarea"
            | "template"
            | "video"
            | "audio"
    )
}

/// 「到這裡要換一段」的元素。
fn block_level(a: &str) -> bool {
    matches!(
        a,
        "p" | "div"
            | "section"
            | "article"
            | "header"
            | "footer"
            | "nav"
            | "aside"
            | "main"
            | "form"
            | "fieldset"
            | "figure"
            | "figcaption"
            | "address"
            | "details"
            | "summary"
            | "dl"
            | "dt"
            | "dd"
            | "tr"
            | "caption"
            | "center"
    )
}

#[derive(Clone, Copy)]
struct ListState {
    ordered: bool,
    n: i64,
}

/// 一路往前掃的狀態機。
///
/// 不建 DOM 樹的理由:要的東西是線性的(一段接一段),而 HTML 的樹狀
/// 結構在這裡只有兩個用處,決定樣式範圍與決定何時換段,兩個都可以
/// 用堆疊處理。不建樹就不必擔心巢狀錯亂的頁面把記憶體吃光。
///
/// 這裡的字串都是 `gostr` 的形式(壞位元組帶著記號),輸出前才換成 U+FFFD。
struct Parser {
    base: Option<gourl::Url>,

    title: String,
    out: Vec<Block>,

    // 正在累積的段落。
    cur: Vec<Span>,
    kind: Kind,
    /// 標題階層,或清單縮排層數。
    level: i32,
    /// 清單項目的符號。
    marker: String,
    ordered: bool,
    num: i64,

    style: Style,
    href: String,

    /// 大於 0 表示正在一個「整個丟掉」的子樹裡(script / style / …)。
    skip: i32,
    /// 大於 0 表示在預格式化區塊裡,空白要原樣保留。
    in_pre: i32,
    pre: String,

    /// 清單的巢狀狀態。每進一層 ul/ol 推一個。
    lists: Vec<ListState>,
    /// 正在 `<title>` 裡面。
    in_title: bool,
}

impl Parser {
    fn new(base: Option<gourl::Url>) -> Parser {
        Parser {
            base,
            title: String::new(),
            out: Vec::new(),
            cur: Vec::new(),
            kind: Kind::Para,
            level: 0,
            marker: String::new(),
            ordered: false,
            num: 0,
            style: Style::default(),
            href: String::new(),
            skip: 0,
            in_pre: 0,
            pre: String::new(),
            lists: Vec::new(),
            in_title: false,
        }
    }

    fn start(&mut self, name: &str, attrs: &[(String, String)]) {
        if self.skip > 0 {
            if skipped(name) {
                self.skip += 1;
            }
            return;
        }
        if skipped(name) {
            self.skip = 1;
            return;
        }
        match name {
            "title" => self.in_title = true,
            // 換行但不換段:br 在段落中間很常見,拆成兩段會多出一個空行。
            "br" => self.text("\n"),
            "hr" => {
                self.flush();
                self.out.push(Block {
                    kind: Kind::Rule,
                    ..Default::default()
                });
            }
            "h1" | "h2" | "h3" | "h4" | "h5" | "h6" => {
                self.flush();
                self.kind = Kind::Heading;
                self.level = (name.as_bytes()[1] - b'0') as i32;
            }
            "pre" => {
                self.flush();
                self.in_pre += 1;
                self.pre.clear();
            }
            "blockquote" => {
                self.flush();
                self.kind = Kind::Quote;
            }
            "ul" | "ol" => {
                self.flush();
                self.lists.push(ListState {
                    ordered: name == "ol",
                    n: 0,
                });
            }
            "li" => {
                self.flush();
                self.kind = Kind::List;
                self.level = self.lists.len() as i32;
                if self.level < 1 {
                    // 沒有 ul/ol 就冒出來的 li。頁面壞掉不是不看它的理由。
                    self.level = 1;
                }
                if let Some(l) = self.lists.last_mut() {
                    l.n += 1;
                    self.ordered = l.ordered;
                    self.num = l.n;
                }
            }
            "td" | "th" => {
                // 表格不另外排版,同一列的格子之間插一個分隔就好。
                // 保住行內的連結比排出格線重要:連結是這個畫面唯一的導覽方式。
                if !self.cur.is_empty() {
                    self.add_text("  ");
                }
            }
            "a" => {
                let h = attr(attrs, "href");
                if !h.is_empty() {
                    self.href = self.resolve(h);
                    self.style |= Style::LINK;
                }
            }
            "b" | "strong" => self.style |= Style::BOLD,
            "i" | "em" | "cite" => self.style |= Style::ITALIC,
            "code" | "kbd" | "samp" | "tt" => self.style |= Style::MONO,
            "s" | "del" | "strike" => self.style |= Style::STRIKE,
            "img" => {
                let (src, alt) = (
                    attr(attrs, "src").to_string(),
                    attr(attrs, "alt").to_string(),
                );
                self.image(&src, &alt);
            }
            _ => {
                if block_level(name) {
                    self.flush();
                }
            }
        }
    }

    fn end(&mut self, name: &str) {
        if self.skip > 0 {
            if skipped(name) {
                self.skip -= 1;
            }
            return;
        }
        match name {
            "title" => self.in_title = false,
            "pre" => {
                if self.in_pre > 0 {
                    self.in_pre -= 1;
                    if self.in_pre == 0 {
                        self.flush_pre();
                    }
                }
            }
            "ul" | "ol" => {
                self.flush();
                self.lists.pop();
            }
            "h1" | "h2" | "h3" | "h4" | "h5" | "h6" | "li" | "blockquote" => self.flush(),
            "a" => {
                self.href.clear();
                self.style = Style(self.style.0 & !Style::LINK.0);
            }
            "b" | "strong" => self.style = Style(self.style.0 & !Style::BOLD.0),
            "i" | "em" | "cite" => self.style = Style(self.style.0 & !Style::ITALIC.0),
            "code" | "kbd" | "samp" | "tt" => self.style = Style(self.style.0 & !Style::MONO.0),
            "s" | "del" | "strike" => self.style = Style(self.style.0 & !Style::STRIKE.0),
            _ => {
                if block_level(name) {
                    self.flush();
                }
            }
        }
    }

    fn text(&mut self, s: &str) {
        if self.skip > 0 {
            return;
        }
        if self.in_title {
            self.title.push_str(s);
            return;
        }
        if self.in_pre > 0 {
            self.pre.push_str(s);
            return;
        }
        let s = collapse(s);
        if s.is_empty() {
            return;
        }
        self.add_text(&s);
    }

    /// 把一段文字接到目前的段落上。
    ///
    /// 樣式相同就併進上一個 span:每個文字節點各開一個 span 會讓
    /// 排版時的折行點多到不合理,而且同樣式的相鄰片段本來就是同一段字。
    fn add_text(&mut self, s: &str) {
        // 段落開頭的空白是上一個標籤留下來的,不是內容。
        if self.cur.is_empty() && s.trim().is_empty() {
            return;
        }
        if let Some(last) = self.cur.last_mut() {
            if last.style == self.style && last.href == self.href {
                last.text.push_str(s);
                return;
            }
        }
        self.cur.push(Span {
            text: s.to_string(),
            style: self.style,
            href: self.href.clone(),
        });
    }

    /// 產生一個圖片區塊。
    ///
    /// 圖片自己成一段,不留在段落中間:排版引擎是以「一張圖佔幾列」在算
    /// 後面內容的行號的,夾在文字裡沒有位置可以放。
    fn image(&mut self, src: &str, alt: &str) {
        if src.is_empty() {
            return;
        }
        self.flush();
        let alt = if alt.is_empty() {
            t("圖片")
        } else {
            alt.to_string()
        };
        let src = self.resolve(src);
        self.out.push(Block {
            kind: Kind::Image,
            src,
            alt,
            ..Default::default()
        });
    }

    /// 把累積中的段落收成一個區塊。
    fn flush(&mut self) {
        let mut cur = std::mem::take(&mut self.cur);
        let (kind, level) = (self.kind, self.level);
        let (marker, ordered, num) = (std::mem::take(&mut self.marker), self.ordered, self.num);
        self.kind = Kind::Para;
        self.level = 0;
        self.ordered = false;
        self.num = 0;
        if cur.is_empty() {
            return;
        }
        // 整段都是空白的話不算一段。
        if cur.iter().all(|sp| sp.text.trim().is_empty()) {
            return;
        }
        // 段尾的空白會在排版時變成一個看得見的縮排。
        let last = cur.len() - 1;
        cur[last].text = cur[last]
            .text
            .trim_end_matches([' ', '\t', '\n'])
            .to_string();
        cur[0].text = cur[0]
            .text
            .trim_start_matches([' ', '\t', '\n'])
            .to_string();

        let mut b = Block {
            kind,
            level,
            spans: cur,
            marker,
            ordered,
            num,
            ..Default::default()
        };
        if b.kind == Kind::Heading && (b.level < 1 || b.level > 6) {
            b.level = 1;
        }
        self.out.push(b);
    }

    /// 把 `<pre>` 的內容收成一個原樣區塊。
    fn flush_pre(&mut self) {
        let s = std::mem::take(&mut self.pre).replace("\r\n", "\n");
        let s = s.trim_matches('\n');
        if s.trim().is_empty() {
            return;
        }
        self.out.push(Block {
            kind: Kind::Pre,
            lines: s.split('\n').map(str::to_string).collect(),
            ..Default::default()
        });
    }

    /// 把相對位址接成絕對位址。
    fn resolve(&self, r: &str) -> String {
        let r = r.trim();
        let Some(base) = &self.base else {
            return r.to_string();
        };
        if r.is_empty() {
            return r.to_string();
        }
        match gourl::parse(&gostr::to_bytes(r)) {
            Ok(u) => gostr::from_bytes(&base.resolve_reference(&u).string()),
            Err(_) => r.to_string(),
        }
    }
}

fn attr<'a>(attrs: &'a [(String, String)], name: &str) -> &'a str {
    // 屬性名稱 tokenizer 已經轉成小寫;Go 版用 EqualFold 比,結果相同。
    attrs
        .iter()
        .find(|(k, _)| k == name)
        .map(|(_, v)| v.as_str())
        .unwrap_or("")
}

/// 把連續的空白(含換行)壓成一個空格。
///
/// HTML 的原始碼裡到處是為了可讀性而換的行,原樣送進排版引擎的話
/// 每一個縮排都會變成內容的一部分。`<pre>` 不走這裡。
///
/// Go 以字元走訪這段字串,壞位元組在這裡變成 U+FFFD。
fn collapse(s: &str) -> String {
    let mut b = String::with_capacity(s.len());
    let mut space = false;
    for r in s.chars() {
        match r {
            ' ' | '\t' | '\n' | '\r' | '\x0c' | '\x0b' | '\u{a0}' => space = true,
            _ => {
                // 開頭的空白也留一個:「</b> 文字」的那個空格是有意義的,
                // 段落真正的前導空白由 flush 那邊統一修掉。
                if space {
                    b.push(' ');
                }
                space = false;
                b.push(if gostr::raw_byte(r).is_some() {
                    '\u{FFFD}'
                } else {
                    r
                });
            }
        }
    }
    if space && !b.is_empty() {
        b.push(' ');
    }
    b
}
