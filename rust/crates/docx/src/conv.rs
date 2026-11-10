//! document.xml 轉成區塊(對應 Go 版 `conv.go`)。

use std::collections::HashMap;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_ooxml::{self as ooxml, gopath, Decoder, Error, StartElement};

use crate::{atoi_def, go_lower, on_off, Doc};

/// 一次轉換的狀態。
///
/// 產出直接累積在 out 裡而不是層層回傳:一個段落可能同時產生文字區塊、
/// 圖片區塊與文字方塊的內容,層層回傳會讓每一層都要處理「我這一層可能
/// 生出好幾種東西」。要把產出接到別的地方(表格儲存格)時用 capture 換掉。
pub(crate) struct Conv<'a> {
    d: &'a Doc,
    pub out: Vec<Block>,

    /// 各串清單的計數器,鍵是 numId,索引是層級。
    counters: HashMap<String, Vec<i64>>,

    // 欄位狀態。Word 的超連結有兩種寫法,這是舊的那一種:
    // 用 fldChar 標出起訖,網址寫在中間的 instrText 裡。
    field_instr: String,
    field_href: String,
    in_field: i64,

    /// 依出現順序記下引用到的註腳編號
    notes_seen: Vec<String>,
    end_seen: Vec<String>,
    img_n: i64,
}

#[derive(Clone, Debug, Default)]
struct PProps {
    style_id: String,
    num_id: String,
    ilvl: i64,
    center: bool,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Brk {
    None,
    Line,
    Page,
}

type R = Result<(), Error>;

impl<'a> Conv<'a> {
    pub fn new(d: &'a Doc) -> Conv<'a> {
        Conv {
            d,
            out: Vec::new(),
            counters: HashMap::new(),
            field_instr: String::new(),
            field_href: String::new(),
            in_field: 0,
            notes_seen: Vec::new(),
            end_seen: Vec::new(),
            img_n: 0,
        }
    }

    /// 把 f 產出的區塊接走,不進主輸出。
    fn capture(&mut self, f: impl FnOnce(&mut Self)) -> Vec<Block> {
        let saved = std::mem::take(&mut self.out);
        f(self);
        std::mem::replace(&mut self.out, saved)
    }

    fn emit(&mut self, b: Block) {
        self.out.push(b);
    }

    /// 走一段「區塊層級」的內容:文件本體、表格儲存格、文字方塊都是這個形狀。
    pub fn body(&mut self, dec: &mut Decoder, owner: &str) -> R {
        ooxml::each(dec, |d, se| match se.name.local.as_str() {
            "p" => self.para(d, owner).map(|_| true),
            "tbl" => self.table(d, owner).map(|_| true),
            // 結構化文件標籤。目錄、封面欄位、內容控制項都包在這裡面,
            // 不進去的話那些內容整段消失。
            "sdt" | "sdtContent" => self.body(d, owner).map(|_| true),
            "AlternateContent" => self.alternate(d, &mut |c, d| c.body(d, owner)).map(|_| true),
            // 修訂:插入的內容是要留的,刪除的(w:del)整段跳過。
            "ins" | "txbxContent" => self.body(d, owner).map(|_| true),
            _ => Ok(false),
        })
    }

    /// 處理 mc:AlternateContent。
    ///
    /// [雷] Choice 與 Fallback 是**同一段內容的兩個版本**,新舊版 Word 各讀一種。
    /// 兩個都收會讓文字方塊的內容出現兩次,而兩次都是對的字:看起來像
    /// 文件本身有重複,不像解析錯誤。只取第一個分支(Choice 依規格排在
    /// Fallback 前面,所以第一個就是最好的那一個)。
    fn alternate(&mut self, dec: &mut Decoder, f: &mut dyn FnMut(&mut Self, &mut Decoder) -> R) -> R {
        let mut used = false;
        ooxml::each(dec, |d, se| {
            if used {
                return Ok(false);
            }
            match se.name.local.as_str() {
                "Choice" | "Fallback" => {
                    used = true;
                    f(self, d).map(|_| true)
                }
                _ => Ok(false),
            }
        })
    }

    /// 走一個段落。
    fn para(&mut self, dec: &mut Decoder, owner: &str) -> R {
        let mut pr = PProps::default();
        let mut spans: Vec<Span> = Vec::new();
        // 圖片、文字方塊,排在段落之後
        let mut after: Vec<Block> = Vec::new();

        self.field_href.clear();
        self.in_field = 0;
        self.field_instr.clear();

        let err = ooxml::each(dec, |d, se| {
            match se.name.local.as_str() {
                "pPr" => return self.p_props(d, &mut pr).map(|_| true),
                "r" => {
                    let (s, extra, brk) = self.run(d, owner, Style::default());
                    spans.extend(s);
                    after.extend(extra);
                    // 段落中間出現分頁或手動換行時要切開:一個區塊畫不出換行,只能拆成兩個。
                    match brk {
                        Brk::Page => {
                            if !spans.is_empty() {
                                self.emit_para(&pr, std::mem::take(&mut spans));
                            }
                            self.emit(Block { kind: Kind::Rule, ..Default::default() });
                        }
                        Brk::Line => {
                            if !spans.is_empty() {
                                self.emit_para(&pr, std::mem::take(&mut spans));
                            }
                        }
                        Brk::None => {}
                    }
                    return Ok(true);
                }
                "hyperlink" => {
                    let href = self.hyperlink_href(owner, se);
                    return ooxml::each(d, |d, h| {
                        if h.name.local != "r" {
                            return Ok(false);
                        }
                        let (mut s, extra, _) = self.run(d, owner, Style::default());
                        if !href.is_empty() {
                            for sp in &mut s {
                                sp.style |= Style::LINK;
                                sp.href = href.clone();
                            }
                        }
                        spans.extend(s);
                        after.extend(extra);
                        Ok(true)
                    })
                    .map(|_| true);
                }
                "ins" | "smartTag" | "sdt" | "sdtContent" | "fldSimple" | "bdo" | "dir" => {
                    // 這些是包在段落裡的容器,裡面還是 run。fldSimple 另外
                    // 帶著欄位指令,超連結的新寫法就是它。
                    if se.name.local == "fldSimple" {
                        let h = field_hyperlink(&ooxml::attr(se, "instr"));
                        if !h.is_empty() {
                            self.field_href = h;
                        }
                    }
                    let err = ooxml::each(d, |d, h| {
                        if h.name.local != "r" {
                            return Ok(false);
                        }
                        let (s, extra, _) = self.run(d, owner, Style::default());
                        spans.extend(s);
                        after.extend(extra);
                        Ok(true)
                    });
                    self.field_href.clear();
                    return err.map(|_| true);
                }
                "AlternateContent" => {
                    return self
                        .alternate(d, &mut |c, d| {
                            let got = c.capture(|c| {
                                let _ = c.body(d, owner);
                            });
                            after.extend(got);
                            Ok(())
                        })
                        .map(|_| true);
                }
                "del" => return Ok(false), // 修訂刪掉的內容不顯示
                _ => {}
            }
            Ok(false)
        });

        // 空段落也要留一個空區塊嗎?不要:Word 的空段落是排版用的間距,
        // 而區塊之間本來就有間距,兩個疊起來會讓文件變得很鬆散。
        if !spans.is_empty() {
            self.emit_para(&pr, spans);
        }
        self.out.extend(after);
        err
    }

    /// 決定一個段落要變成哪一種區塊。
    fn emit_para(&mut self, pr: &PProps, spans: Vec<Span>) {
        let lvl = self.d.heading_level(&pr.style_id);
        if lvl > 0 {
            self.emit(Block { kind: Kind::Heading, level: lvl as i32, spans, ..Default::default() });
            return;
        }
        let mut num_id = pr.num_id.clone();
        if num_id.is_empty() {
            if let Some(st) = self.d.styles.get(&pr.style_id) {
                num_id = st.num_id.clone();
            }
        }
        let spans = if !num_id.is_empty() {
            match self.list_block(&num_id, pr.ilvl, spans) {
                Ok(b) => {
                    self.emit(b);
                    return;
                }
                Err(spans) => spans,
            }
        } else {
            spans
        };
        let name = self.d.styles.get(&pr.style_id).map(|s| s.name.as_str()).unwrap_or("");
        if is_quote_style(name) {
            self.emit(Block { kind: Kind::Quote, spans, ..Default::default() });
            return;
        }
        self.emit(Block { kind: Kind::Para, spans, ..Default::default() });
    }

    /// 把一個段落變成清單項目,並推進計數器。不是清單時把 spans 交回。
    fn list_block(&mut self, num_id: &str, ilvl: i64, spans: Vec<Span>) -> Result<Block, Vec<Span>> {
        let level = (ilvl + 1) as i32;
        // 編號定義找不到時仍然當成清單:段落自己已經說了它屬於某一串,
        // 那個事實比「查不到格式」更可靠。
        let Some(n) = self.d.nums.get(num_id) else {
            return Ok(Block { kind: Kind::List, level, spans, ..Default::default() });
        };
        let Some(lv) = n.lvl.get(&ilvl) else {
            return Ok(Block { kind: Kind::List, level, spans, ..Default::default() });
        };
        if lv.format == "none" {
            return Err(spans);
        }
        let mut b = Block { kind: Kind::List, level, spans, ordered: lv.ordered, ..Default::default() };
        if lv.ordered {
            let start = lv.start;
            b.num = self.next_num(num_id, ilvl, start);
        } else {
            let m = bullet_marker(&lv.text);
            if !m.is_empty() {
                b.marker = m;
            }
        }
        Ok(b)
    }

    /// 推進某一層的編號,並把更深的層歸零。
    ///
    /// 歸零是編號清單的定義:1.1、1.2 之後回到第二個「1.」,下一層要從
    /// 2.1 重新開始而不是 2.3。
    fn next_num(&mut self, num_id: &str, ilvl: i64, start: i64) -> i64 {
        let cnt = self.counters.entry(num_id.to_string()).or_default();
        let ilvl = ilvl as usize;
        while cnt.len() <= ilvl {
            cnt.push(0);
        }
        if cnt[ilvl] == 0 {
            cnt[ilvl] = start;
        } else {
            cnt[ilvl] = cnt[ilvl].wrapping_add(1);
        }
        for c in cnt.iter_mut().skip(ilvl + 1) {
            *c = 0;
        }
        cnt[ilvl]
    }

    fn p_props(&mut self, dec: &mut Decoder, pr: &mut PProps) -> R {
        ooxml::each(dec, |d, se| {
            match se.name.local.as_str() {
                "pStyle" => pr.style_id = ooxml::attr(se, "val"),
                "jc" => pr.center = ooxml::attr(se, "val") == "center",
                "numPr" => {
                    return ooxml::each(d, |_, n| {
                        match n.name.local.as_str() {
                            "numId" => pr.num_id = ooxml::attr(n, "val"),
                            "ilvl" => pr.ilvl = atoi_def(&ooxml::attr(n, "val"), 0).clamp(0, 8),
                            _ => {}
                        }
                        Ok(false)
                    })
                    .map(|_| true);
                }
                _ => {}
            }
            Ok(false)
        })
    }

    /// 走一個文字段,回傳它產生的行內內容、要排在段落後面的區塊,
    /// 以及它結尾有沒有換行或分頁。
    fn run(&mut self, dec: &mut Decoder, owner: &str, base: Style) -> (Vec<Span>, Vec<Block>, Brk) {
        let mut style = base;
        let mut spans: Vec<Span> = Vec::new();
        let mut extra: Vec<Block> = Vec::new();
        let mut brk = Brk::None;

        fn add(spans: &mut Vec<Span>, style: Style, href: &str, text: String) {
            if text.is_empty() {
                return;
            }
            let mut sp = Span { text, style, ..Default::default() };
            if !href.is_empty() {
                sp.style |= Style::LINK;
                sp.href = href.to_string();
            }
            spans.push(sp);
        }

        let _ = ooxml::each(dec, |d, se| {
            match se.name.local.as_str() {
                "rPr" => {
                    return ooxml::each(d, |_, p| {
                        match p.name.local.as_str() {
                            "b" | "bCs" => {
                                if on_off(p) {
                                    style |= Style::BOLD;
                                }
                            }
                            "i" | "iCs" => {
                                if on_off(p) {
                                    style |= Style::ITALIC;
                                }
                            }
                            "strike" | "dstrike" => {
                                if on_off(p) {
                                    style |= Style::STRIKE;
                                }
                            }
                            _ => {}
                        }
                        Ok(false)
                    })
                    .map(|_| true);
                }
                "t" => {
                    let t = ooxml::text(d);
                    add(&mut spans, style, &self.field_href, t);
                    return Ok(true);
                }
                "tab" => add(&mut spans, style, &self.field_href, "    ".to_string()),
                "noBreakHyphen" => add(&mut spans, style, &self.field_href, "-".to_string()),
                "br" => {
                    brk = if ooxml::xml::equal_fold(&ooxml::attr(se, "type"), "page") { Brk::Page } else { Brk::Line };
                }
                "cr" => brk = Brk::Line,
                "sym" => add(&mut spans, style, &self.field_href, sym_char(&ooxml::attr(se, "char"))),
                "drawing" | "pict" | "object" => {
                    let mut alt = String::new();
                    let got = self.capture(|c| {
                        let _ = c.drawing(d, owner, &mut alt);
                    });
                    extra.extend(got);
                    return Ok(true);
                }
                "footnoteReference" => {
                    self.notes_seen.push(ooxml::attr(se, "id"));
                    let t = tr!("[註 %d]", self.notes_seen.len());
                    add(&mut spans, style, &self.field_href, t);
                }
                "endnoteReference" => {
                    self.end_seen.push(ooxml::attr(se, "id"));
                    let t = tr!("[尾註 %d]", self.end_seen.len());
                    add(&mut spans, style, &self.field_href, t);
                }
                "instrText" => {
                    let t = ooxml::text(d);
                    self.field_instr.push_str(&t);
                    return Ok(true);
                }
                "fldChar" => match ooxml::attr(se, "fldCharType").as_str() {
                    "begin" => {
                        self.in_field += 1;
                        self.field_instr.clear();
                    }
                    "separate" => {
                        let h = field_hyperlink(&self.field_instr);
                        if !h.is_empty() {
                            self.field_href = h;
                        }
                        self.field_instr.clear();
                    }
                    "end" => {
                        if self.in_field > 0 {
                            self.in_field -= 1;
                        }
                        self.field_href.clear();
                    }
                    _ => {}
                },
                "AlternateContent" => {
                    return self
                        .alternate(d, &mut |c, d| {
                            let mut alt = String::new();
                            let got = c.capture(|c| {
                                let _ = c.drawing(d, owner, &mut alt);
                            });
                            extra.extend(got);
                            Ok(())
                        })
                        .map(|_| true);
                }
                _ => {}
            }
            Ok(false)
        });
        (spans, extra, brk)
    }

    /// 查超連結的目標。
    ///
    /// 包內的目標(指向文件裡的書籤)回空字串:畫面沒有錨點可以跳,
    /// 給一個按了沒反應的連結比不給更糟。
    fn hyperlink_href(&self, owner: &str, se: &StartElement) -> String {
        let id = ooxml::rel_id(se);
        if id.is_empty() {
            return String::new();
        }
        let rels = self.d.pkg.rels(owner);
        match rels.get(&id) {
            Some(r) if r.external => r.target.clone(),
            _ => String::new(),
        }
    }

    /// 在一段繪圖標記裡找圖片與文字方塊。
    ///
    /// 用「一路往下找」而不是照 DrawingML 的結構走:同一張圖可以包在
    /// inline、anchor、群組、圖表的備援裡,結構有十幾種組合,而要的東西
    /// 只有兩樣:blip 的關聯編號,與文字方塊的內容。
    fn drawing(&mut self, dec: &mut Decoder, owner: &str, alt: &mut String) -> R {
        ooxml::each(dec, |d, se| match se.name.local.as_str() {
            "docPr" | "cNvPr" => {
                // 替代文字與 blip 分屬繪圖樹的不同層,所以整棵樹共用同一個
                // 變數:每一層自己留一份的話,遞迴下去就看不到上一層讀到的名字。
                let descr = ooxml::attr(se, "descr");
                if !descr.is_empty() {
                    *alt = descr;
                } else {
                    let name = ooxml::attr(se, "name");
                    if !name.is_empty() && alt.is_empty() {
                        *alt = name;
                    }
                }
                Ok(false)
            }
            "blip" | "imagedata" => {
                let id = ooxml::rel_id(se);
                if !id.is_empty() {
                    let a = alt.clone();
                    self.add_image(owner, &id, &a);
                }
                Ok(false)
            }
            "txbxContent" => self.body(d, owner).map(|_| true),
            "AlternateContent" => self.alternate(d, &mut |c, d| c.drawing(d, owner, alt)).map(|_| true),
            _ => self.drawing(d, owner, alt).map(|_| true),
        })
    }

    /// 記下一張圖並產生一個圖片區塊。
    fn add_image(&mut self, owner: &str, rel_id: &str, alt: &str) {
        let part = self.d.pkg.rel_target(owner, rel_id);
        if part.is_empty() || !self.d.pkg.has(&part) {
            return;
        }
        self.img_n += 1;
        let base = gopath::base(&part);
        let r = format!("{}-{}", self.img_n, base);
        self.d.imgs.borrow_mut().insert(r.clone(), part);
        let alt = if alt.is_empty() { base } else { alt.to_string() };
        self.emit(Block { kind: Kind::Image, src: r, alt, ..Default::default() });
    }

    /// 走一個表格。
    ///
    /// 儲存格壓成純文字:markdown 那一層的表格只有字串,沒有行內樣式。
    /// 儲存格裡的圖片改排在表格後面:表格的格子畫不下一張圖,
    /// 而丟掉圖比挪位置糟。
    fn table(&mut self, dec: &mut Decoder, owner: &str) -> R {
        let mut rows: Vec<Vec<String>> = Vec::new();
        let mut after: Vec<Block> = Vec::new();
        let err = ooxml::each(dec, |d, se| {
            if se.name.local != "tr" {
                return Ok(false);
            }
            let mut cells: Vec<String> = Vec::new();
            let err = ooxml::each(d, |d, tc| {
                if tc.name.local != "tc" {
                    return Ok(false);
                }
                let mut span = 1;
                let mut blocks: Vec<Block> = Vec::new();
                let err = ooxml::each(d, |d, inn| {
                    if inn.name.local == "tcPr" {
                        return ooxml::each(d, |_, p| {
                            if p.name.local == "gridSpan" {
                                span = atoi_def(&ooxml::attr(p, "val"), 1).clamp(1, 64);
                            }
                            Ok(false)
                        })
                        .map(|_| true);
                    }
                    let local = inn.name.local.clone();
                    let got = self.capture(|c| match local.as_str() {
                        "p" => {
                            let _ = c.para(d, owner);
                        }
                        "tbl" => {
                            let _ = c.table(d, owner);
                        }
                        "sdt" | "sdtContent" => {
                            let _ = c.body(d, owner);
                        }
                        _ => {
                            let _ = d.skip();
                        }
                    });
                    let (text, imgs) = split_images(got);
                    blocks.push(Block { kind: Kind::Para, spans: vec![Span { text, ..Default::default() }], ..Default::default() });
                    after.extend(imgs);
                    Ok(true)
                });
                cells.push(blocks_text(&blocks));
                // 跨欄:後面補空格子,不然同一列的欄位會整排錯開。
                for _ in 1..span {
                    cells.push(String::new());
                }
                err.map(|_| true)
            });
            rows.push(cells);
            err.map(|_| true)
        });
        if !rows.is_empty() {
            self.emit(Block { kind: Kind::Table, rows: normalize_rows(rows), ..Default::default() });
        }
        self.out.extend(after);
        err
    }

    /// 把引用到的註腳排在文件最後。
    pub fn notes(&mut self) {
        let seen = std::mem::take(&mut self.notes_seen);
        self.notes_from("/footnotes", "footnote", &seen, &tr!("註腳"));
        let seen = std::mem::take(&mut self.end_seen);
        self.notes_from("/endnotes", "endnote", &seen, &tr!("尾註"));
    }

    fn notes_from(&mut self, rel_type: &str, elem: &str, seen: &[String], title: &str) {
        if seen.is_empty() {
            return;
        }
        let part = self.d.rel_part(rel_type);
        if part.is_empty() {
            return;
        }
        let Ok(b) = self.d.pkg.bytes(&part) else { return };
        let mut texts: HashMap<String, Vec<Block>> = HashMap::new();
        let mut dec = ooxml::new_decoder(b);
        if ooxml::root(&mut dec).is_err() {
            return;
        }
        let _ = ooxml::each(&mut dec, |d, se| {
            if se.name.local != elem {
                return Ok(false);
            }
            let id = ooxml::attr(se, "id");
            // separator 與 continuationSeparator 是那條分隔線,不是註腳內容。
            let t = ooxml::attr(se, "type");
            if !t.is_empty() && t != "normal" {
                return Ok(false);
            }
            let got = self.capture(|c| {
                let _ = c.body(d, &part);
            });
            texts.insert(id, got);
            Ok(true)
        });

        self.emit(Block { kind: Kind::Rule, ..Default::default() });
        self.emit(Block {
            kind: Kind::Heading,
            level: 2,
            spans: vec![Span { text: title.to_string(), ..Default::default() }],
            ..Default::default()
        });
        for (i, id) in seen.iter().enumerate() {
            let body = texts.get(id).map(|b| blocks_text(b)).unwrap_or_default();
            if body.is_empty() {
                continue;
            }
            self.emit(Block {
                kind: Kind::List,
                ordered: true,
                num: i as i64 + 1,
                level: 1,
                spans: vec![Span { text: body, ..Default::default() }],
                ..Default::default()
            });
        }
    }
}

fn is_quote_style(name: &str) -> bool {
    let s = go_lower(name);
    s.contains("quote") || s.contains(&tr!("引文")) || s.contains(&tr!("引言"))
}

/// 挑一個畫得出來的項目符號。
///
/// Word 的預設項目符號是 Wingdings 的字碼(0xF0B7 之類),那些字碼在
/// Unicode 裡是私人使用區:直接畫會是一個缺字方框。畫不出來的一律
/// 交回預設符號。
fn bullet_marker(text: &str) -> String {
    let t = text.trim();
    if t.is_empty() {
        return String::new();
    }
    let mut cs = t.chars();
    let (Some(c), None) = (cs.next(), cs.next()) else { return String::new() };
    let c = c as u32;
    if (0xE000..=0xF8FF).contains(&c) || c < 0x20 {
        return String::new();
    }
    t.to_string()
}

/// 把 w:sym 的字碼換成畫得出來的字。
fn sym_char(hex: &str) -> String {
    let mut n: i64 = 0;
    for r in go_lower(hex.trim()).chars() {
        let v = match r {
            '0'..='9' => r as i64 - '0' as i64,
            'a'..='f' => r as i64 - 'a' as i64 + 10,
            _ => return String::new(),
        };
        n = n.wrapping_mul(16).wrapping_add(v);
    }
    // Wingdings 的字碼落在私人使用區。最常見的那幾個換成對應的符號,
    // 其餘的畫不出來,寧可不畫。
    match n {
        0xF0B7 | 0xB7 | 0x2022 => return "•".to_string(),
        0xF0A7 | 0xA7 => return "▪".to_string(),
        0xF0FC => return "✓".to_string(),
        _ => {}
    }
    if (0xE000..=0xF8FF).contains(&n) || n < 0x20 {
        return String::new();
    }
    // Go 的 string(rune(n)):int 截成 int32,不合法的碼位給 U+FFFD。
    let r = n as i32;
    if r < 0 {
        return "\u{FFFD}".to_string();
    }
    char::from_u32(r as u32).unwrap_or('\u{FFFD}').to_string()
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
    // 沒有引號時取第一個空白之前的部分,並丟掉 \l \o 這類旗標。
    for f in rest.split_whitespace() {
        if !f.starts_with('\\') {
            return f.to_string();
        }
    }
    String::new()
}

/// 把每一列補到同樣的欄數。
fn normalize_rows(mut rows: Vec<Vec<String>>) -> Vec<Vec<String>> {
    let w = rows.iter().map(|r| r.len()).max().unwrap_or(0);
    for r in &mut rows {
        r.resize(w, String::new());
    }
    rows
}

fn split_images(bs: Vec<Block>) -> (String, Vec<Block>) {
    let (imgs, keep): (Vec<Block>, Vec<Block>) = bs.into_iter().partition(|b| b.kind == Kind::Image);
    (blocks_text(&keep), imgs)
}

/// 把一疊區塊壓成一行字。
fn blocks_text(bs: &[Block]) -> String {
    let mut parts = Vec::new();
    for b in bs {
        let mut sb = String::new();
        for s in &b.spans {
            sb.push_str(&s.text);
        }
        for l in &b.lines {
            sb.push_str(l);
        }
        for r in &b.rows {
            sb.push_str(&r.join(" "));
        }
        let t = sb.trim();
        if !t.is_empty() {
            parts.push(t.to_string());
        }
    }
    parts.join(" ")
}
