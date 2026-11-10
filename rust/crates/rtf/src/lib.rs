//! 讀 Rich Text Format(對應 Go 版 `internal/rtf`)。
//!
//! RTF 是一種標記語言而不是容器格式:一份 .rtf 就是一長串
//! `{` 群組、`\控制字` 與純文字。難的地方不在語法而在**文字怎麼變回字**:
//!
//!   - 一個位元組是什麼字,取決於當時選到的字型是哪一個字集。
//!     同一份檔案裡可以有 Big5 的段落與 cp1252 的段落。
//!   - `\uN` 帶著一個 Unicode 字碼,後面跟著給舊版讀取器看的替代寫法,
//!     那幾個位元組要跳過:跳幾個由 `\ucN` 決定。不跳的話每個中文字
//!     後面都會多出兩個問號。
//!   - 清單的項目符號不在段落裡,而在一個 `\listtext` 群組裡,那是
//!     「不懂清單的讀取器請直接印這個」的備援。拿它當符號用,就不必
//!     解整張清單表。
//!
//! 圖片是內嵌的十六進位文字,取出來就能直接解碼。

mod cp_tables;
mod words;

#[cfg(test)]
mod tests;

use std::collections::HashMap;
use std::path::Path;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Span, Style};

/// 檔案大小上限。
pub const MAX_BYTES: u64 = 64 << 20;

/// 一份解析完的 RTF。
pub struct Doc {
    blocks: Vec<Block>,
    imgs: HashMap<String, Vec<u8>>,
}

/// Go 的 `*PathError` 長相:「stat 檔名: no such file or directory」。
fn go_path_error(op: &str, name: &str, e: &std::io::Error) -> String {
    let mut msg = e.to_string();
    if let Some(i) = msg.find(" (os error ") {
        msg.truncate(i);
    }
    let mut cs = msg.chars();
    let msg: String = match cs.next() {
        Some(c) => c.to_lowercase().chain(cs).collect(),
        None => msg,
    };
    format!("{op} {name}: {msg}")
}

impl Doc {
    /// 讀一份 .rtf。
    pub fn open(name: impl AsRef<Path>) -> Result<Doc, String> {
        let p = name.as_ref();
        let shown = p.to_string_lossy();
        let st = std::fs::metadata(p).map_err(|e| go_path_error("stat", &shown, &e))?;
        if st.len() > MAX_BYTES {
            return Err(tr!("這份 RTF 太大(%d 位元組)", st.len()));
        }
        let b = std::fs::read(p).map_err(|e| go_path_error("open", &shown, &e))?;
        Doc::parse(&b)
    }

    /// 解析一份 RTF。
    pub fn parse(src: &[u8]) -> Result<Doc, String> {
        let src = if src.starts_with(&[0xEF, 0xBB, 0xBF]) { &src[3..] } else { src };
        let head = &src[..src.len().min(16)];
        let start = head.iter().position(|c| !matches!(c, b' ' | b'\r' | b'\n' | b'\t')).unwrap_or(head.len());
        if !head[start..].starts_with(b"{\\rt") {
            return Err(tr!("這不是 RTF 檔"));
        }
        let mut p = Parser::new(src);
        p.run();
        p.finish();
        let mut d = Doc { blocks: p.out, imgs: p.imgs };
        if d.blocks.is_empty() {
            d.blocks = vec![Block {
                kind: Kind::Para,
                spans: vec![Span { text: tr!("(這份文件沒有可以顯示的內容)"), ..Default::default() }],
                ..Default::default()
            }];
        }
        Ok(d)
    }

    /// 整份文件的區塊。
    pub fn blocks(&self) -> Vec<Block> {
        self.blocks.clone()
    }

    /// 取一張內嵌的圖。
    pub fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        self.imgs.get(r).cloned().ok_or_else(|| tr!("這份文件裡沒有 %s", r))
    }

    pub fn close(&self) {}
}

// --- 解析 ---

#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub(crate) enum Dest {
    #[default]
    Body,
    /// 整個群組丟掉
    Skip,
    FontTbl,
    StyleSheet,
    Pict,
    ListText,
    FldInst,
    Footnote,
}

#[derive(Clone, Debug, Default)]
pub(crate) struct State {
    pub dest: Dest,
    /// 目前的字碼頁
    pub cp: i64,
    /// \uN 之後要跳過幾個替代字元
    pub uc: i64,
    pub style: Style,
    pub href: String,
    /// \outlinelevel + 1,0 表示沒有指定。
    pub outline: i64,
    // 段落層級的屬性。放在 state 裡是因為它們同樣受群組範圍限制。
    pub style_num: i64,
    pub in_table: bool,
}

impl State {
    fn initial() -> State {
        State { cp: 1252, uc: 1, ..Default::default() }
    }
}

pub(crate) struct Parser<'a> {
    src: &'a [u8],
    i: usize,

    pub(crate) cur: State,
    stack: Vec<State>,

    pub(crate) out: Vec<Block>,
    pub(crate) spans: Vec<Span>,

    /// 還沒解碼的位元組
    raw: Vec<u8>,
    pub(crate) raw_cp: i64,

    /// \uN 之後還要跳過幾個字元
    pub(crate) skip: i64,

    // 字型表與樣式表。
    pub(crate) font_cp: HashMap<i64, i64>,
    pub(crate) cur_font: i64,
    pub(crate) styles: HashMap<i64, String>,
    style_buf: String,
    pub(crate) style_cur: i64,

    // 清單:目前段落的項目符號(來自 \listtext 或 \pntext)。
    pub(crate) list_mark: String,
    pub(crate) list_level: i64,
    pub(crate) in_list: bool,

    // 表格。
    rows: Vec<Vec<String>>,
    row: Vec<String>,

    // 欄位。
    pub(crate) instr: String,

    // 圖片。
    pub(crate) pic_hex: Vec<u8>,
    pub(crate) pic_bin: Vec<u8>,
    pub(crate) pic_kind: String,
    pic_n: i64,
    imgs: HashMap<String, Vec<u8>>,

    // 註腳。
    pub(crate) foot_n: i64,
    footnotes: Vec<Block>,
    sink_out: Vec<Vec<Block>>,
    sink_spans: Vec<Vec<Span>>,

    pub(crate) high_surrogate: i32,
}

fn is_alpha(c: u8) -> bool {
    c.is_ascii_alphabetic()
}

fn hex_val(c: u8) -> u8 {
    match c {
        b'0'..=b'9' => c - b'0',
        b'a'..=b'f' => c - b'a' + 10,
        b'A'..=b'F' => c - b'A' + 10,
        _ => 0,
    }
}

impl<'a> Parser<'a> {
    fn new(src: &'a [u8]) -> Parser<'a> {
        Parser {
            src,
            i: 0,
            cur: State::initial(),
            stack: Vec::new(),
            out: Vec::new(),
            spans: Vec::new(),
            raw: Vec::new(),
            raw_cp: 0,
            skip: 0,
            font_cp: HashMap::new(),
            cur_font: 0,
            styles: HashMap::new(),
            style_buf: String::new(),
            style_cur: 0,
            list_mark: String::new(),
            list_level: 0,
            in_list: false,
            rows: Vec::new(),
            row: Vec::new(),
            instr: String::new(),
            pic_hex: Vec::new(),
            pic_bin: Vec::new(),
            pic_kind: String::new(),
            pic_n: 0,
            imgs: HashMap::new(),
            foot_n: 0,
            footnotes: Vec::new(),
            sink_out: Vec::new(),
            sink_spans: Vec::new(),
            high_surrogate: 0,
        }
    }

    fn run(&mut self) {
        while self.i < self.src.len() {
            let c = self.src[self.i];
            match c {
                b'{' => {
                    self.i += 1;
                    self.flush_raw();
                    // 群組繼承目前的狀態,離開時整個還原:RTF 的所有格式
                    // 設定都受群組範圍限制,這是它唯一的作用域機制。
                    self.stack.push(self.cur.clone());
                }
                b'}' => {
                    self.i += 1;
                    self.flush_raw();
                    self.close_group();
                }
                b'\\' => {
                    self.i += 1;
                    self.control();
                }
                b'\r' | b'\n' => {
                    // RTF 原始碼裡的換行沒有意義,換行一律由 \par 表示。
                    self.i += 1;
                }
                _ => {
                    self.i += 1;
                    if self.skip > 0 {
                        self.skip -= 1;
                        continue;
                    }
                    self.put_byte(c);
                }
            }
        }
    }

    fn close_group(&mut self) {
        let popped = std::mem::replace(&mut self.cur, State::initial());
        if let Some(s) = self.stack.pop() {
            self.cur = s;
        }
        // 樣式表的每一筆是一個群組,而且它們的目的地跟外層一樣,
        // 所以要在比對目的地之前先收:比對會把它們全部濾掉。
        if popped.dest == Dest::StyleSheet {
            self.end_style_entry();
        }
        if popped.dest == self.cur.dest {
            return;
        }
        // 一個特殊目的地的群組結束了,把它累積的東西收掉。
        match popped.dest {
            Dest::Pict => self.end_pict(),
            Dest::ListText => {
                let got = self.pop_sink();
                self.list_mark = blocks_text(&got).trim().to_string();
            }
            Dest::FldInst => {
                // 這時候 cur 已經還原成 field 群組本身,所以網址設在它身上,
                // 接下來的 fldrslt 群組會繼承到。
                let h = words::field_hyperlink(&self.instr);
                if !h.is_empty() {
                    self.cur.href = h;
                }
                self.instr.clear();
            }
            Dest::StyleSheet => self.end_style_entry(),
            Dest::Footnote => {
                let got = self.pop_sink();
                self.footnotes.extend(got);
            }
            _ => {}
        }
    }

    /// 讀一個控制字或控制符號。
    fn control(&mut self) {
        if self.i >= self.src.len() {
            return;
        }
        let c = self.src[self.i];
        // 控制符號:反斜線後面接一個非字母。
        if !is_alpha(c) {
            self.i += 1;
            match c {
                b'\'' => self.hex_byte(),
                b'\\' | b'{' | b'}' => {
                    if self.skip > 0 {
                        self.skip -= 1;
                        return;
                    }
                    self.put_byte(c);
                }
                b'*' => {
                    // \* 表示「不認得後面那個目的地就整組丟掉」。
                    self.flush_raw();
                    self.mark_optional();
                }
                b'~' => self.put_byte(b' '),
                b'_' | b'-' => {
                    // 不斷行連字號與選擇性連字號:畫面上就是一個連字號或什麼都沒有。
                }
                b'\r' | b'\n' => self.end_para(),
                _ => {}
            }
            return;
        }
        // 控制字。
        let start = self.i;
        while self.i < self.src.len() && is_alpha(self.src[self.i]) {
            self.i += 1;
        }
        let word = String::from_utf8_lossy(&self.src[start..self.i]).into_owned();
        let (mut num, mut has_num, mut neg) = (0i64, false, false);
        if self.i < self.src.len() && self.src[self.i] == b'-' {
            neg = true;
            self.i += 1;
        }
        let ns = self.i;
        while self.i < self.src.len() && self.src[self.i].is_ascii_digit() {
            self.i += 1;
        }
        if self.i > ns {
            // Go 的 strconv.Atoi 溢位時回傳最大值(錯誤被忽略)。
            num = std::str::from_utf8(&self.src[ns..self.i]).ok().and_then(|s| s.parse().ok()).unwrap_or(i64::MAX);
            has_num = true;
            if neg {
                num = -num;
            }
        }
        // 控制字後面的一個空白是分隔符,不是內容。
        if self.i < self.src.len() && self.src[self.i] == b' ' {
            self.i += 1;
        }
        // [雷] `\'hh` 不能觸發清空。Big5 的一個字是連續兩個 `\'hh`,
        // 中間清空就會拿半個字去解碼,解出來是兩個問號。
        self.flush_raw();
        self.word(&word, num, has_num);
    }

    fn hex_byte(&mut self) {
        if self.i + 1 >= self.src.len() {
            return;
        }
        let v = hex_val(self.src[self.i]) * 16 + hex_val(self.src[self.i + 1]);
        self.i += 2;
        if self.skip > 0 {
            self.skip -= 1;
            return;
        }
        if self.cur.dest == Dest::Pict {
            self.pic_hex.push(self.src[self.i - 2]);
            self.pic_hex.push(self.src[self.i - 1]);
            return;
        }
        self.put_byte(v);
    }

    /// 處理 `\*`:看下一個控制字是不是我們認得的目的地,不認得就整組丟掉。
    fn mark_optional(&mut self) {
        let mut j = self.i;
        if j < self.src.len() && self.src[j] == b'\\' {
            j += 1;
            let s = j;
            while j < self.src.len() && is_alpha(self.src[j]) {
                j += 1;
            }
            if matches!(&self.src[s..j], b"fldinst" | b"listtext" | b"pntext" | b"shppict" | b"footnote") {
                return; // 這幾個要進去
            }
        }
        self.cur.dest = Dest::Skip;
    }

    // --- 文字 ---

    /// 把一個原始位元組放進待解碼的緩衝區。
    ///
    /// 不是逐個位元組解碼,因為 Big5、GBK、Shift_JIS 的一個字是兩個位元組,
    /// 而它們在 RTF 裡是兩個各自獨立的 `\'hh`。要湊齊了才解得出字。
    pub(crate) fn put_byte(&mut self, c: u8) {
        if self.cur.dest == Dest::Pict {
            if c.is_ascii_hexdigit() {
                self.pic_hex.push(c);
            }
            return;
        }
        if self.raw_cp != self.cur.cp {
            self.flush_raw();
            self.raw_cp = self.cur.cp;
        }
        self.raw.push(c);
    }

    /// 把待解碼的位元組解成字。
    pub(crate) fn flush_raw(&mut self) {
        if self.raw.is_empty() {
            return;
        }
        let b = std::mem::take(&mut self.raw);
        let s = words::decode(&b, self.raw_cp);
        self.emit_text(&s);
    }

    pub(crate) fn emit_text(&mut self, s: &str) {
        if s.is_empty() {
            return;
        }
        match self.cur.dest {
            Dest::Skip | Dest::FontTbl | Dest::Pict => return,
            Dest::StyleSheet => {
                self.style_buf.push_str(s);
                return;
            }
            Dest::FldInst => {
                self.instr.push_str(s);
                return;
            }
            _ => {}
        }
        // 同樣式的相鄰片段併起來:一個中文字常常被拆成好幾個 `\'hh`,
        // 不併的話一句話會變成幾十個片段,排版那一層要多做很多次工。
        if let Some(last) = self.spans.last_mut() {
            if last.style == self.cur.style && last.href == self.cur.href {
                last.text.push_str(s);
                return;
            }
        }
        let mut sp = Span { text: s.to_string(), style: self.cur.style, ..Default::default() };
        if !self.cur.href.is_empty() {
            sp.style |= Style::LINK;
            sp.href = self.cur.href.clone();
        }
        self.spans.push(sp);
    }

    // --- 區塊 ---

    pub(crate) fn end_para(&mut self) {
        self.flush_raw();
        if self.cur.in_table {
            // 儲存格裡的換行:內容之後會被壓成一行,用空白接起來。
            self.emit_text(" ");
            return;
        }
        if self.spans.is_empty() {
            return;
        }
        let spans = std::mem::take(&mut self.spans);
        let lvl = self.heading_level();
        if lvl > 0 {
            self.emit_block(Block { kind: Kind::Heading, level: lvl as i32, spans, ..Default::default() });
        } else if self.in_list || !self.list_mark.is_empty() {
            let b = Block {
                kind: Kind::List,
                level: (self.list_level + 1) as i32,
                spans,
                marker: safe_marker(&self.list_mark),
                ..Default::default()
            };
            self.emit_block(b);
        } else {
            self.emit_block(Block { kind: Kind::Para, spans, ..Default::default() });
        }
        self.list_mark.clear();
    }

    /// 目前段落的樣式是不是標題。
    fn heading_level(&self) -> i64 {
        if self.cur.outline > 0 {
            return self.cur.outline.clamp(1, 6);
        }
        let name = self.styles.get(&self.cur.style_num).map(String::as_str).unwrap_or("");
        heading_by_name(name)
    }

    /// 送出一個區塊。表格要先收尾:表格是靠一列一列累積出來的,
    /// 中間插進別的區塊表示那個表格已經結束了。
    pub(crate) fn emit_block(&mut self, b: Block) {
        self.flush_table();
        self.out.push(b);
    }

    pub(crate) fn end_cell(&mut self) {
        self.flush_raw();
        let t = flatten(&self.spans).trim().to_string();
        self.row.push(t);
        self.spans.clear();
    }

    pub(crate) fn end_row(&mut self) {
        self.flush_raw();
        if !self.spans.is_empty() {
            self.end_cell();
        }
        if !self.row.is_empty() {
            self.rows.push(std::mem::take(&mut self.row));
        }
        // 一列結束就離開表格狀態。照規格是要等 \pard 才重設,而真實的
        // 檔案也都會寫:但少了那一句的話,表格後面的整篇內文會被
        // 當成同一個儲存格吃掉,而畫面上只會看到表格變長,不像出錯。
        self.cur.in_table = false;
    }

    fn flush_table(&mut self) {
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

    // --- 巢狀輸出 ---

    /// 把產出接到一個新的緩衝區。註腳與清單符號的內容要另外收,
    /// 不能混進正文。
    pub(crate) fn push_sink(&mut self) {
        self.sink_out.push(std::mem::take(&mut self.out));
        self.sink_spans.push(std::mem::take(&mut self.spans));
    }

    fn pop_sink(&mut self) -> Vec<Block> {
        self.flush_raw();
        if !self.spans.is_empty() {
            let spans = std::mem::take(&mut self.spans);
            self.out.push(Block { kind: Kind::Para, spans, ..Default::default() });
        }
        let got = std::mem::take(&mut self.out);
        // 推進去與拿出來的次數在 Go 版也不一定成對(見報告),
        // 沒有可以還原的就留在原地,不跟著出錯。
        if let (Some(o), Some(s)) = (self.sink_out.pop(), self.sink_spans.pop()) {
            self.out = o;
            self.spans = s;
        }
        got
    }

    // --- 圖片 ---

    fn end_pict(&mut self) {
        let mut data = std::mem::take(&mut self.pic_bin);
        if data.is_empty() {
            if let Some(b) = hex_decode(&self.pic_hex) {
                data = b;
            }
        }
        self.pic_hex.clear();
        let kind = std::mem::take(&mut self.pic_kind);
        if data.is_empty() {
            return;
        }
        // 只交出畫得出來的格式。WMF / EMF 是向量指令,這裡沒有解譯器,
        // 交出去只會變成一個解不開的檔案。
        if kind != "png" && kind != "jpg" {
            let text = tr!("(內嵌圖片:") + &kind_name(&kind) + &tr!(",無法顯示)");
            self.emit_block(Block {
                kind: Kind::Para,
                spans: vec![Span { text, style: Style::ITALIC, ..Default::default() }],
                ..Default::default()
            });
            return;
        }
        self.pic_n += 1;
        let r = format!("{}.{}", self.pic_n, kind);
        self.imgs.insert(r.clone(), data);
        self.emit_block(Block { kind: Kind::Image, src: r.clone(), alt: r, ..Default::default() });
    }

    // --- 樣式表 ---

    fn end_style_entry(&mut self) {
        let t = self.style_buf.trim();
        let name = t.strip_suffix(';').unwrap_or(t).trim().to_string();
        self.style_buf.clear();
        if !name.is_empty() {
            self.styles.insert(self.style_cur, name);
        }
    }

    // --- 收尾 ---

    fn finish(&mut self) {
        self.flush_raw();
        self.end_row();
        if !self.spans.is_empty() {
            self.end_para();
        }
        self.flush_table();
        if !self.footnotes.is_empty() {
            self.out.push(Block { kind: Kind::Rule, ..Default::default() });
            self.out.push(Block {
                kind: Kind::Heading,
                level: 2,
                spans: vec![Span { text: tr!("註腳"), ..Default::default() }],
                ..Default::default()
            });
            let f = std::mem::take(&mut self.footnotes);
            self.out.extend(f);
        }
    }
}

/// Go 的 `hex.DecodeString`,只在整段都合法時回傳(Go 版遇到錯誤就整段不要)。
fn hex_decode(s: &[u8]) -> Option<Vec<u8>> {
    if s.len() % 2 != 0 || !s.iter().all(|c| c.is_ascii_hexdigit()) {
        return None;
    }
    Some(s.chunks(2).map(|p| hex_val(p[0]) * 16 + hex_val(p[1])).collect())
}

fn kind_name(k: &str) -> String {
    match k {
        "wmf" => "WMF".to_string(),
        "emf" => "EMF".to_string(),
        "bmp" => "DIB".to_string(),
        _ => tr!("未知格式"),
    }
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn go_lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

pub(crate) fn heading_by_name(name: &str) -> i64 {
    let s = go_lower(name.trim());
    for pre in [
        "heading ".to_string(),
        "heading".to_string(),
        tr!("標題 "),
        tr!("標題"),
        tr!("标题 "),
        tr!("标题"),
    ] {
        if let Some(rest) = s.strip_prefix(pre.as_str()) {
            let rest = rest.trim().as_bytes();
            if rest.len() == 1 && (b'1'..=b'9').contains(&rest[0]) {
                return ((rest[0] - b'0') as i64).clamp(1, 6);
            }
        }
    }
    0
}

/// 擋掉畫不出來的項目符號。
///
/// 舊式清單的符號常常是 Symbol 或 Wingdings 字型裡的一個位元組,
/// 照字碼頁解出來會是 · 或落在私人使用區。畫不出來的一律交回預設符號。
fn safe_marker(s: &str) -> String {
    let t = s.trim();
    if t.is_empty() {
        return String::new();
    }
    let r: Vec<char> = t.chars().collect();
    if r.len() == 1 {
        let c = r[0] as u32;
        if (0xE000..=0xF8FF).contains(&c) || c < 0x20 {
            return String::new();
        }
        if c == 0xB7 {
            // 中間點:Symbol 字型的項目符號解出來的樣子
            return "•".to_string();
        }
    }
    if r.len() > 8 {
        return String::new();
    }
    t.to_string()
}

/// 把一疊區塊壓成一行字。
fn blocks_text(bs: &[Block]) -> String {
    bs.iter()
        .map(|b| flatten(&b.spans).trim().to_string())
        .filter(|t| !t.is_empty())
        .collect::<Vec<_>>()
        .join(" ")
}

fn flatten(spans: &[Span]) -> String {
    spans.iter().map(|s| s.text.as_str()).collect()
}
