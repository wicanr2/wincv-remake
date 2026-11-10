//! 把字元序列排成區塊(對應 Go 版 `blocks.go`)。

use std::collections::HashMap;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Span, Style};

use crate::props::{CharRun, ParaProp};
use crate::Doc;

// 正文裡的特殊字元。它們不是文字,是結構標記。
const CH_PICTURE_INLINE: char = '\u{01}';
const CH_FOOTNOTE_REF: char = '\u{02}';
const CH_ANNOTATION_REF: char = '\u{05}';
const CH_CELL_MARK: char = '\u{07}';
const CH_DRAWN_OBJECT: char = '\u{08}';
const CH_TAB: char = '\u{09}';
const CH_LINE_BREAK: char = '\u{0B}';
const CH_PAGE_BREAK: char = '\u{0C}';
const CH_COLUMN_BREAK: char = '\u{0E}';
const CH_PARA_MARK: char = '\u{0D}';
const CH_FIELD_BEGIN: char = '\u{13}';
const CH_FIELD_SEP: char = '\u{14}';
const CH_FIELD_END: char = '\u{15}';
const CH_NO_BREAK_HYPHEN: char = '\u{1E}';
const CH_OPTIONAL_HYPH: char = '\u{1F}';

impl Doc {
    /// 排出整份文件。
    pub fn blocks(&self) -> Vec<Block> {
        let mut b = Builder { d: self, out: Vec::new(), rows: Vec::new(), row: Vec::new(), foot_n: 0, counters: HashMap::new() };
        b.paragraphs(0, self.ccp_text);
        b.flush_table();
        b.footnotes();
        if b.out.is_empty() {
            return vec![Block { kind: Kind::Para, spans: vec![span(tr!("(這份文件沒有可以顯示的內容)"))], ..Default::default() }];
        }
        b.out
    }
}

fn span(text: String) -> Span {
    Span { text, ..Default::default() }
}

struct Builder<'a> {
    d: &'a Doc,
    out: Vec<Block>,

    rows: Vec<Vec<String>>,
    row: Vec<String>,

    foot_n: i64,

    /// 各串清單每一層數到哪。key 是 ilfo。
    counters: HashMap<i64, Vec<i64>>,
}

impl Builder<'_> {
    /// 把一段 CP 範圍切成段落並排出來。
    fn paragraphs(&mut self, from: i64, to: i64) {
        let n = self.d.chars.len() as i64;
        let mut start = from;
        let mut i = from;
        while i < to && i < n {
            let c = self.d.chars[i as usize];
            if c == CH_PARA_MARK || c == CH_CELL_MARK {
                self.paragraph(start, i);
                start = i + 1;
            }
            i += 1;
        }
        if start < to && start < n {
            self.paragraph(start, to.min(n));
        }
    }

    /// 排出一個段落。end 指向段落標記本身,那個位置的屬性
    /// 才是這一段的屬性:Word 把段落屬性掛在結尾的標記上。
    fn paragraph(&mut self, start: i64, end: i64) {
        let mut prop = ParaProp::none();
        if end < self.d.fcs.len() as i64 {
            prop = self.d.para_at(self.d.fcs[end as usize]);
        }
        // 列結尾的標記本身沒有內容,它只是說「上面那幾格是同一列」。
        if prop.in_table && prop.ttp {
            self.end_row();
            return;
        }
        let spans = self.spans(start, end);
        if prop.in_table {
            self.row.push(flatten(&spans).trim().to_string());
            return;
        }
        self.flush_table();
        if spans.is_empty() {
            return;
        }
        let lvl = self.heading_level(&prop);
        if lvl > 0 {
            self.out.push(Block { kind: Kind::Heading, level: lvl as i32, spans, ..Default::default() });
        } else if prop.ilfo != 0 {
            let b = self.list_block(&prop, spans);
            self.out.push(b);
        } else {
            self.out.push(Block { kind: Kind::Para, spans, ..Default::default() });
        }
    }

    fn heading_level(&self, p: &ParaProp) -> i64 {
        let n = self.d.heading_of(p.istd);
        if n > 0 {
            return n;
        }
        if p.outline > 0 {
            return p.outline.clamp(1, 6);
        }
        0
    }

    /// 把一段字元換成帶樣式的行內片段。
    fn spans(&mut self, start: i64, end: i64) -> Vec<Span> {
        let mut out: Vec<Span> = Vec::new();
        let mut sb = String::new();
        let mut cur = CharRun::default();
        let mut href = String::new();
        let mut field = 0; // 0 一般,1 在欄位指令裡,2 在欄位結果裡
        let mut instr = String::new();

        let flush = |sb: &mut String, out: &mut Vec<Span>, cur: &CharRun, href: &str| {
            if sb.is_empty() {
                return;
            }
            let mut sp = Span { text: std::mem::take(sb), ..Default::default() };
            if cur.bold {
                sp.style |= Style::BOLD;
            }
            if cur.italic {
                sp.style |= Style::ITALIC;
            }
            if cur.strike {
                sp.style |= Style::STRIKE;
            }
            if !href.is_empty() {
                sp.style |= Style::LINK;
                sp.href = href.to_string();
            }
            out.push(sp);
        };

        let n = self.d.chars.len() as i64;
        let mut i = start;
        while i < end && i < n {
            let c = self.d.chars[i as usize];
            let idx = i;
            i += 1;
            match c {
                CH_FIELD_BEGIN => {
                    flush(&mut sb, &mut out, &cur, &href);
                    field = 1;
                    instr.clear();
                    continue;
                }
                CH_FIELD_SEP => {
                    field = 2;
                    let h = field_hyperlink(&instr);
                    if !h.is_empty() {
                        flush(&mut sb, &mut out, &cur, &href);
                        href = h;
                    }
                    continue;
                }
                CH_FIELD_END => {
                    flush(&mut sb, &mut out, &cur, &href);
                    field = 0;
                    href.clear();
                    continue;
                }
                _ => {}
            }
            if field == 1 {
                instr.push(c);
                continue;
            }

            let mut run = CharRun::default();
            if idx < self.d.fcs.len() as i64 {
                run = self.d.run_at(self.d.fcs[idx as usize]);
            }
            if run != cur {
                flush(&mut sb, &mut out, &cur, &href);
                cur = run;
            }

            match c {
                CH_TAB => sb.push_str("    "),
                CH_LINE_BREAK => {
                    // 段落內的手動換行。畫面上的區塊畫不出換行,拆成兩個區塊
                    // 看起來一樣。
                    flush(&mut sb, &mut out, &cur, &href);
                    if !out.is_empty() {
                        self.out.push(Block { kind: Kind::Para, spans: std::mem::take(&mut out), ..Default::default() });
                    }
                }
                CH_PAGE_BREAK | CH_COLUMN_BREAK => {
                    flush(&mut sb, &mut out, &cur, &href);
                    if !out.is_empty() {
                        self.out.push(Block { kind: Kind::Para, spans: std::mem::take(&mut out), ..Default::default() });
                    }
                    self.out.push(Block { kind: Kind::Rule, ..Default::default() });
                }
                CH_FOOTNOTE_REF => {
                    flush(&mut sb, &mut out, &cur, &href);
                    self.foot_n += 1;
                    out.push(span(tr!("[註 %d]", self.foot_n)));
                }
                CH_PICTURE_INLINE | CH_DRAWN_OBJECT => {
                    flush(&mut sb, &mut out, &cur, &href);
                    // 圖片的內容在 Data 串流裡,是另一套結構,這裡沒有解。
                    // 標出位置比什麼都不顯示誠實。
                    out.push(Span { text: tr!("(圖片)"), style: Style::ITALIC, ..Default::default() });
                }
                CH_NO_BREAK_HYPHEN => sb.push('-'),
                CH_OPTIONAL_HYPH | CH_ANNOTATION_REF | '\u{00}' | '\u{0A}' => {
                    // 選擇性連字號不顯示;註解標記與雜訊字元丟掉。
                }
                '\u{A0}' => sb.push(' '),
                _ => {
                    if c as u32 >= 0x20 || c == '\t' {
                        sb.push(c);
                    }
                }
            }
        }
        flush(&mut sb, &mut out, &cur, &href);
        out
    }

    fn end_row(&mut self) {
        if self.row.is_empty() {
            return;
        }
        self.rows.push(std::mem::take(&mut self.row));
    }

    fn flush_table(&mut self) {
        self.end_row();
        if self.rows.is_empty() {
            return;
        }
        let mut rows = std::mem::take(&mut self.rows);
        let w = rows.iter().map(|r| r.len()).max().unwrap_or(0);
        for r in &mut rows {
            r.resize(w, String::new());
        }
        self.out.push(Block { kind: Kind::Table, rows, ..Default::default() });
    }

    /// 把註腳排在文件最後。
    ///
    /// 註腳的文字接在正文後面,是同一條字元序列的下一段。每一則以
    /// 段落標記分隔,開頭是那個參照字元。
    fn footnotes(&mut self) {
        if self.foot_n == 0 || self.d.ccp_ftn <= 0 {
            return;
        }
        let chars = &self.d.chars;
        let from = self.d.ccp_text;
        let to = (self.d.ccp_text + self.d.ccp_ftn).min(chars.len() as i64);
        let mut texts = Vec::new();
        let mut start = from;
        let mut i = from;
        while i < to {
            let c = chars[i as usize];
            if c == CH_PARA_MARK || c == CH_CELL_MARK {
                let t = clean_text(&chars[start as usize..i as usize]);
                let t = t.trim();
                if !t.is_empty() {
                    texts.push(t.to_string());
                }
                start = i + 1;
            }
            i += 1;
        }
        if start < to {
            let t = clean_text(&chars[start as usize..to as usize]);
            let t = t.trim();
            if !t.is_empty() {
                texts.push(t.to_string());
            }
        }
        if texts.is_empty() {
            return;
        }
        self.out.push(Block { kind: Kind::Rule, ..Default::default() });
        self.out.push(Block { kind: Kind::Heading, level: 2, spans: vec![span(tr!("註腳"))], ..Default::default() });
        for (i, t) in texts.into_iter().enumerate() {
            self.out.push(Block {
                kind: Kind::List,
                ordered: true,
                num: i as i64 + 1,
                level: 1,
                spans: vec![span(t)],
                ..Default::default()
            });
        }
    }

    /// 排一段清單。編號或項目符號要查清單格式表才知道:
    /// 段落自己只說得出「屬於第幾串、第幾層」。
    fn list_block(&mut self, prop: &ParaProp, spans: Vec<Span>) -> Block {
        let mut blk = Block { kind: Kind::List, level: (prop.ilvl + 1).clamp(1, 9) as i32, spans, ..Default::default() };
        let Some(lv) = self.d.list_level(prop.ilfo, prop.ilvl).filter(|lv| lv.ordered()) else {
            // 查不到就當項目符號。段落自己已經說了它屬於某一串,
            // 那個事實比「查不到格式」更可靠:至少縮排與符號是對的。
            return blk;
        };
        blk.ordered = true;
        blk.num = self.next_num(prop.ilfo, prop.ilvl, lv.start);
        blk
    }

    /// 推進某一層的編號,並把更深的層歸零。
    ///
    /// 歸零是編號清單的定義:1.1、1.2 之後回到第二個「1.」,下一層要從
    /// 2.1 重新開始而不是 2.3。
    fn next_num(&mut self, ilfo: i64, ilvl: i64, start: i64) -> i64 {
        let cnt = self.counters.entry(ilfo).or_default();
        let ilvl = ilvl as usize;
        while cnt.len() <= ilvl {
            cnt.push(0);
        }
        if cnt[ilvl] == 0 {
            cnt[ilvl] = start.max(1);
        } else {
            cnt[ilvl] = cnt[ilvl].wrapping_add(1);
        }
        for c in cnt.iter_mut().skip(ilvl + 1) {
            *c = 0;
        }
        cnt[ilvl]
    }
}

/// 把一段字元裡的結構標記濾掉。
fn clean_text(rs: &[char]) -> String {
    let mut sb = String::new();
    for &c in rs {
        if c == CH_TAB {
            sb.push_str("    ");
        } else if c == '\u{A0}' {
            sb.push(' ');
        } else if c as u32 >= 0x20 {
            sb.push(c);
        }
    }
    sb
}

fn flatten(spans: &[Span]) -> String {
    spans.iter().map(|s| s.text.as_str()).collect()
}

/// Go 的 strings.ToUpper:逐字元的簡單對應(一個字元對一個字元)。
fn go_upper(s: &str) -> String {
    s.chars()
        .map(|c| {
            let mut u = c.to_uppercase();
            match (u.next(), u.next()) {
                (Some(v), None) => v,
                _ => c,
            }
        })
        .collect()
}

/// 從欄位指令裡挑出網址。
pub(crate) fn field_hyperlink(instr: &str) -> String {
    let s = instr.trim();
    if !go_upper(s).starts_with("HYPERLINK") {
        return String::new();
    }
    // Go 以位元組切 s[len("HYPERLINK"):];大寫前後長度不同的字(ı → I)
    // 會讓切點落在字中間,那時 Go 留下半個字,這裡換成 U+FFFD。
    let rest = String::from_utf8_lossy(&s.as_bytes()[9.min(s.len())..]).into_owned();
    let rest = rest.trim();
    if let Some(i) = rest.find('"') {
        let rest = &rest[i + 1..];
        return match rest.find('"') {
            Some(j) => rest[..j].to_string(),
            None => String::new(),
        };
    }
    for f in rest.split_whitespace() {
        if !f.starts_with('\\') {
            return f.to_string();
        }
    }
    String::new()
}
