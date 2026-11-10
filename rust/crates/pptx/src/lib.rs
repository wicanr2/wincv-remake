//! 讀 PowerPoint 的 .pptx(PresentationML)(對應 Go 版 `internal/pptx`)。
//!
//! 一份簡報在字元格點上就是「一張投影片一頁」:標題變成標題區塊,
//! 內文的條列保留階層,表格照表格畫,圖片貼在後面,備忘稿附在最後。
//!
//! 三件事決定了它跟 Word 的解析不一樣:
//!
//!   - **閱讀順序不是文件順序。** 投影片裡的圖形是照 z 順序存的,
//!     跟人的閱讀順序沒有關係。標題一律提到最前面,其餘依版面上的
//!     位置由上而下、由左而右:但只在每個圖形都寫明位置時才這樣排,
//!     少了位置資訊的圖形會全部擠到左上角,那比原本的順序更糟。
//!   - **文字可能不在投影片裡。** 佔位圖形的型別要回頭問版面配置,
//!     沒有型別就分不出標題與內文。
//!   - **備忘稿是另一個組件。** 靠關聯連過去。

#[cfg(test)]
mod tests;

use std::collections::HashMap;
use std::num::IntErrorKind;
use std::path::Path;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_ooxml::{self as ooxml, gopath, Decoder, Error, Package, StartElement};

/// 張數上限。
pub const MAX_SLIDES: usize = 5000;

/// 一份打開著的簡報。
pub struct Deck {
    pub title: String,
    pub slides: Vec<Slide>,

    pkg: Package,
    imgs: HashMap<String, String>,
    img_n: i64,
}

/// 一張投影片。
#[derive(Clone, Debug, Default)]
pub struct Slide {
    pub title: String,
    pub part: String,
    pub blocks: Vec<Block>,
}

/// 佔位圖形的角色。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
enum PhKind {
    #[default]
    None,
    Title,
    Body,
}

/// 投影片上的一個圖形,連同它排出來的區塊。
#[derive(Clone, Debug, Default)]
struct Shape {
    kind: PhKind,
    x: i64,
    y: i64,
    has_pos: bool,
    blocks: Vec<Block>,
    title: String,
    /// 這個圖形的文字壓成一行,paras 是它有幾段。
    /// 沒有佔位資訊的簡報要靠這兩個值認出哪一個是標題。
    plain: String,
    paras: usize,
}

type Layout = HashMap<String, PhKind>;

impl Deck {
    /// 打開一份 .pptx。
    pub fn open(name: impl AsRef<Path>) -> Result<Deck, String> {
        let p = Package::open(name)?;
        Deck::new(p)
    }

    /// 從一個已經打開的 OPC 包建 Deck。
    pub fn new(p: Package) -> Result<Deck, String> {
        let main = pres_part(&p);
        if main.is_empty() {
            p.close();
            return Err(tr!("這不是 PowerPoint 簡報(找不到 presentation.xml)"));
        }
        let mut d = Deck { title: core_title(&p), slides: Vec::new(), pkg: p, imgs: HashMap::new(), img_n: 0 };
        for (i, part) in slide_parts(&d.pkg, &main).into_iter().enumerate() {
            let (title, blocks) = d.slide(&part, i + 1);
            d.slides.push(Slide { title, part, blocks });
        }
        if d.slides.is_empty() {
            d.pkg.close();
            return Err(tr!("這份簡報沒有投影片"));
        }
        Ok(d)
    }

    pub fn close(&self) {
        self.pkg.close();
    }

    /// 取一張圖。
    pub fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        match self.imgs.get(r) {
            Some(part) => self.pkg.bytes(part),
            None => Err(tr!("這份簡報裡沒有 %s", r)),
        }
    }

    fn slide(&mut self, part: &str, num: usize) -> (String, Vec<Block>) {
        let layout = self.layout_of(part);
        let shapes = self.sp_tree_of(part, &layout);

        let mut title = String::new();
        let mut body: Vec<Shape> = Vec::new();
        for s in shapes {
            if s.kind == PhKind::Title && title.is_empty() && !s.title.is_empty() {
                title = s.title;
                continue;
            }
            body.push(s);
        }
        // 只有在每個圖形都寫明位置時才依位置排。少一個就整批照原順序:
        // 沒有位置的圖形會被當成 (0,0) 擠到最前面,那比 z 順序更難讀。
        let all_pos = body.iter().all(|s| s.has_pos);
        if all_pos && body.len() > 1 {
            body.sort_by(|a, b| (a.y, a.x).cmp(&(b.y, b.x)));
        }
        // 沒有佔位資訊的簡報(很多轉檔工具產出的都是)整張都是普通文字方塊。
        // 這時候取排在最前面、只有一段、又短的那一個當標題:那就是
        // 人看投影片時當成標題的東西。條件下得緊一點:猜錯的代價是
        // 一段內文被抬成標題,而漏猜只是少一個標題。
        if title.is_empty() && !body.is_empty() {
            let s = &body[0];
            if s.paras == 1 && !s.plain.is_empty() && s.plain.chars().count() <= 60 {
                title = s.plain.clone();
                body.remove(0);
            }
        }
        let mut rest = Vec::new();
        for s in body {
            rest.extend(s.blocks);
        }

        let head = if title.is_empty() { tr!("第 %d 張", num) } else { title.clone() };
        let mut out = vec![Block { kind: Kind::Heading, level: 1, spans: vec![span(head)], ..Default::default() }];
        out.extend(rest);
        out.extend(self.notes(part));
        if out.len() == 1 {
            out.push(Block { kind: Kind::Para, spans: vec![span(tr!("(這一張沒有文字)"))], ..Default::default() });
        }
        (title, out)
    }

    /// 找出投影片用的版面配置,並回傳「佔位編號 → 角色」的對照。
    ///
    /// 需要這一層轉接是因為投影片自己的佔位圖形常常只寫 idx 不寫 type,
    /// 型別留在版面配置裡。少了它就分不出標題與內文,整張投影片會變成
    /// 一堆沒有階層的段落。
    fn layout_of(&self, part: &str) -> Layout {
        let mut out = Layout::new();
        let Some(lay) = self.pkg.rels_by_type(part, "/slideLayout").into_iter().find(|t| self.pkg.has(t)) else {
            return out;
        };
        let Ok(b) = self.pkg.bytes(&lay) else { return out };
        let mut dec = ooxml::new_decoder(b);
        fn walk(d: &mut Decoder, out: &mut Layout) -> Result<(), Error> {
            ooxml::each(d, |d, se| {
                if se.name.local == "ph" {
                    let mut idx = ooxml::attr(se, "idx");
                    if idx.is_empty() {
                        idx = "0".to_string();
                    }
                    out.insert(idx, ph_of(&ooxml::attr(se, "type")));
                    return Ok(false);
                }
                walk(d, out).map(|_| true)
            })
        }
        let _ = walk(&mut dec, &mut out);
        out
    }

    /// 走一張投影片的圖形樹。
    fn sp_tree_of(&mut self, part: &str, layout: &Layout) -> Vec<Shape> {
        let Ok(b) = self.pkg.bytes(part) else { return Vec::new() };
        let mut dec = ooxml::new_decoder(b);
        match ooxml::root(&mut dec) {
            Ok(r) if r.name.local == "sld" || r.name.local == "notes" => {}
            _ => return Vec::new(),
        }
        let mut out = Vec::new();
        let _ = self.walk_tree(&mut dec, part, layout, &mut out);
        out
    }

    fn walk_tree(&mut self, dec: &mut Decoder, part: &str, layout: &Layout, out: &mut Vec<Shape>) -> Result<(), Error> {
        ooxml::each(dec, |d, se| match se.name.local.as_str() {
            "spTree" | "grpSp" | "cSld" => self.walk_tree(d, part, layout, out).map(|_| true),
            "sp" | "cxnSp" => {
                let s = self.shape_of(d, part, layout);
                if !s.blocks.is_empty() || !s.title.is_empty() {
                    out.push(s);
                }
                Ok(true)
            }
            "pic" => {
                let s = self.pic_of(d, part);
                if !s.blocks.is_empty() {
                    out.push(s);
                }
                Ok(true)
            }
            "graphicFrame" => {
                let s = self.frame_of(d, part);
                if !s.blocks.is_empty() {
                    out.push(s);
                }
                Ok(true)
            }
            "AlternateContent" => {
                let mut used = false;
                ooxml::each(d, |d, a| {
                    if used {
                        return Ok(false);
                    }
                    match a.name.local.as_str() {
                        "Choice" | "Fallback" => {
                            used = true;
                            self.walk_tree(d, part, layout, out).map(|_| true)
                        }
                        _ => Ok(false),
                    }
                })
                .map(|_| true)
            }
            _ => Ok(false),
        })
    }

    /// 走一個一般圖形:佔位角色、位置、文字。
    fn shape_of(&mut self, dec: &mut Decoder, part: &str, layout: &Layout) -> Shape {
        let mut s = Shape::default();
        let mut have_kind = false;
        let _ = ooxml::each(dec, |d, se| {
            match se.name.local.as_str() {
                "nvSpPr" | "nvCxnSpPr" | "nvPr" => {
                    return ooxml::each(d, |d, n| {
                        if n.name.local == "nvPr" {
                            return ooxml::each(d, |_, p| {
                                if p.name.local == "ph" {
                                    let typ = ooxml::attr(p, "type");
                                    s.kind = if !typ.is_empty() {
                                        ph_of(&typ)
                                    } else if let Some(&k) = layout.get(&ph_idx(p)) {
                                        k
                                    } else {
                                        PhKind::Body
                                    };
                                    have_kind = true;
                                }
                                Ok(false)
                            })
                            .map(|_| true);
                        }
                        Ok(false)
                    })
                    .map(|_| true);
                }
                "spPr" => {
                    return ooxml::each(d, |d, p| {
                        if p.name.local != "xfrm" {
                            return Ok(false);
                        }
                        ooxml::each(d, |_, x| {
                            if x.name.local == "off" {
                                s.x = atoi64(&ooxml::attr(x, "x"));
                                s.y = atoi64(&ooxml::attr(x, "y"));
                                s.has_pos = true;
                            }
                            Ok(false)
                        })
                        .map(|_| true)
                    })
                    .map(|_| true);
                }
                "txBody" | "txbxContent" => {
                    let (blocks, plain) = self.text_body(d, part, s.kind);
                    s.paras = blocks.len();
                    s.blocks.extend(blocks);
                    if s.kind == PhKind::Title {
                        s.title = plain.clone();
                    }
                    s.plain = plain;
                    return Ok(true);
                }
                _ => {}
            }
            Ok(false)
        });
        if !have_kind {
            s.kind = PhKind::None;
        }
        // 標題佔位的內容已經抽成 title,區塊不重複留一份。
        if s.kind == PhKind::Title && !s.title.is_empty() {
            s.blocks.clear();
        }
        s
    }

    /// 走一個文字方塊,回傳區塊與壓成一行的純文字。
    fn text_body(&mut self, dec: &mut Decoder, part: &str, kind: PhKind) -> (Vec<Block>, String) {
        let mut out = Vec::new();
        let mut plain = Vec::new();
        let _ = ooxml::each(dec, |d, se| {
            if se.name.local != "p" {
                return Ok(false);
            }
            let (blocks, text) = self.paragraph(d, part, kind);
            out.extend(blocks);
            if !text.is_empty() {
                plain.push(text);
            }
            Ok(true)
        });
        (out, plain.join(" "))
    }

    /// 走一個段落。
    fn paragraph(&mut self, dec: &mut Decoder, part: &str, kind: PhKind) -> (Vec<Block>, String) {
        let mut lvl = 0i64;
        let mut bullet = kind != PhKind::Title; // 內文預設有項目符號,標題沒有
        let mut marker = String::new();
        let mut ordered = false;
        let mut spans: Vec<Span> = Vec::new();
        let mut out: Vec<Block> = Vec::new();

        fn flush(out: &mut Vec<Block>, spans: &mut Vec<Span>, kind: PhKind, bullet: bool, ordered: bool, lvl: i64, marker: &str) {
            if spans.is_empty() {
                return;
            }
            let spans = std::mem::take(spans);
            if kind == PhKind::Title {
                out.push(Block { kind: Kind::Para, spans, ..Default::default() });
            } else if bullet {
                let mut b = Block { kind: Kind::List, level: (lvl + 1) as i32, spans, ordered, ..Default::default() };
                if !ordered && !marker.is_empty() {
                    b.marker = marker.to_string();
                }
                out.push(b);
            } else {
                out.push(Block { kind: Kind::Para, spans, ..Default::default() });
            }
        }

        let _ = ooxml::each(dec, |d, se| {
            match se.name.local.as_str() {
                "pPr" => {
                    lvl = atoi_def(&ooxml::attr(se, "lvl"), 0).clamp(0, 8);
                    return ooxml::each(d, |_, p| {
                        match p.name.local.as_str() {
                            "buNone" => bullet = false,
                            "buChar" => {
                                bullet = true;
                                marker = safe_marker(&ooxml::attr(p, "char"));
                            }
                            "buAutoNum" => {
                                bullet = true;
                                ordered = true;
                            }
                            _ => {}
                        }
                        Ok(false)
                    })
                    .map(|_| true);
                }
                // a:fld 是自動更新的欄位(頁碼、日期),裡面存著上次算出來
                // 的值。那個值就是簡報放映時看到的字,照收。
                "r" | "fld" => {
                    let s = self.run(d, part);
                    spans.extend(s);
                    return Ok(true);
                }
                "br" => flush(&mut out, &mut spans, kind, bullet, ordered, lvl, &marker),
                _ => {}
            }
            Ok(false)
        });
        flush(&mut out, &mut spans, kind, bullet, ordered, lvl, &marker);

        let mut sb = String::new();
        for b in &out {
            for s in &b.spans {
                sb.push_str(&s.text);
            }
        }
        let text = sb.trim().to_string();
        (out, text)
    }

    fn run(&mut self, dec: &mut Decoder, part: &str) -> Vec<Span> {
        let mut style = Style::default();
        let mut href = String::new();
        let mut text = String::new();
        let pkg = &self.pkg;
        let _ = ooxml::each(dec, |d, se| {
            match se.name.local.as_str() {
                "rPr" => {
                    if ooxml::attr(se, "b") == "1" {
                        style |= Style::BOLD;
                    }
                    if ooxml::attr(se, "i") == "1" {
                        style |= Style::ITALIC;
                    }
                    let strike = ooxml::attr(se, "strike");
                    if strike.starts_with("sng") || strike.starts_with("dbl") {
                        style |= Style::STRIKE;
                    }
                    return ooxml::each(d, |_, p| {
                        if p.name.local == "hlinkClick" {
                            let id = ooxml::rel_id(p);
                            if !id.is_empty() {
                                if let Some(r) = pkg.rels(part).get(&id) {
                                    if r.external {
                                        href = r.target.clone();
                                    }
                                }
                            }
                        }
                        Ok(false)
                    })
                    .map(|_| true);
                }
                "t" => {
                    text.push_str(&ooxml::text(d));
                    return Ok(true);
                }
                _ => {}
            }
            Ok(false)
        });
        if text.is_empty() {
            return Vec::new();
        }
        let mut sp = Span { text, style, ..Default::default() };
        if !href.is_empty() {
            sp.style |= Style::LINK;
            sp.href = href;
        }
        vec![sp]
    }

    /// 走一張圖。
    fn pic_of(&mut self, dec: &mut Decoder, part: &str) -> Shape {
        let mut s = Shape::default();
        let mut alt = String::new();
        let _ = self.walk_pic(dec, part, &mut s, &mut alt);
        s
    }

    fn walk_pic(&mut self, dec: &mut Decoder, part: &str, s: &mut Shape, alt: &mut String) -> Result<(), Error> {
        ooxml::each(dec, |d, se| match se.name.local.as_str() {
            "cNvPr" => {
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
            "off" => {
                s.x = atoi64(&ooxml::attr(se, "x"));
                s.y = atoi64(&ooxml::attr(se, "y"));
                s.has_pos = true;
                Ok(false)
            }
            "blip" => {
                let id = ooxml::rel_id(se);
                if !id.is_empty() {
                    if let Some(b) = self.image_block(part, &id, alt) {
                        s.blocks.push(b);
                    }
                }
                Ok(false)
            }
            _ => self.walk_pic(d, part, s, alt).map(|_| true),
        })
    }

    fn image_block(&mut self, owner: &str, rel_id: &str, alt: &str) -> Option<Block> {
        let target = self.pkg.rel_target(owner, rel_id);
        if target.is_empty() || !self.pkg.has(&target) {
            return None;
        }
        self.img_n += 1;
        let base = gopath::base(&target);
        let r = format!("{}-{}", self.img_n, base);
        self.imgs.insert(r.clone(), target);
        let alt = if alt.is_empty() { base } else { alt.to_string() };
        Some(Block { kind: Kind::Image, src: r, alt, ..Default::default() })
    }

    /// 走一個圖形框:表格與圖表都包在這裡面。
    fn frame_of(&mut self, dec: &mut Decoder, part: &str) -> Shape {
        let mut s = Shape::default();
        let _ = self.walk_frame(dec, part, &mut s);
        s
    }

    fn walk_frame(&mut self, dec: &mut Decoder, part: &str, s: &mut Shape) -> Result<(), Error> {
        ooxml::each(dec, |d, se| match se.name.local.as_str() {
            "off" => {
                s.x = atoi64(&ooxml::attr(se, "x"));
                s.y = atoi64(&ooxml::attr(se, "y"));
                s.has_pos = true;
                Ok(false)
            }
            "tbl" => {
                if let Some(b) = self.table(d, part) {
                    s.blocks.push(b);
                }
                Ok(true)
            }
            "chart" => {
                // 圖表的資料在另一個組件裡,畫成表格會是一份跟畫面
                // 對不起來的數字。只標出這裡有一張圖表。
                s.blocks.push(Block {
                    kind: Kind::Para,
                    spans: vec![Span { text: tr!("(圖表)"), style: Style::ITALIC, ..Default::default() }],
                    ..Default::default()
                });
                Ok(false)
            }
            _ => self.walk_frame(d, part, s).map(|_| true),
        })
    }

    fn table(&mut self, dec: &mut Decoder, part: &str) -> Option<Block> {
        let mut rows: Vec<Vec<String>> = Vec::new();
        let _ = ooxml::each(dec, |d, se| {
            if se.name.local != "tr" {
                return Ok(false);
            }
            let mut cells: Vec<String> = Vec::new();
            let err = ooxml::each(d, |d, tc| {
                if tc.name.local != "tc" {
                    return Ok(false);
                }
                let span = atoi_def(&ooxml::attr(tc, "gridSpan"), 1).clamp(1, 64);
                let mut text = String::new();
                let err = ooxml::each(d, |d, inn| {
                    if inn.name.local != "txBody" {
                        return Ok(false);
                    }
                    text = self.text_body(d, part, PhKind::Title).1;
                    Ok(true)
                });
                cells.push(text.trim().to_string());
                for _ in 1..span {
                    cells.push(String::new());
                }
                err.map(|_| true)
            });
            rows.push(cells);
            err.map(|_| true)
        });
        if rows.is_empty() {
            return None;
        }
        let w = rows.iter().map(|r| r.len()).max().unwrap_or(0);
        for r in &mut rows {
            r.resize(w, String::new());
        }
        Some(Block { kind: Kind::Table, rows, ..Default::default() })
    }

    /// 取備忘稿。
    fn notes(&mut self, part: &str) -> Vec<Block> {
        let Some(np) = self.pkg.rels_by_type(part, "/notesSlide").into_iter().find(|t| self.pkg.has(t)) else {
            return Vec::new();
        };
        let shapes = self.sp_tree_of(&np, &Layout::new());
        let mut body = Vec::new();
        for s in shapes {
            for b in s.blocks {
                // 備忘稿頁上那個「投影片編號」佔位只有一個數字,不是備忘稿。
                if b.kind == Kind::Image {
                    continue;
                }
                body.push(b);
            }
        }
        if body.is_empty() {
            return Vec::new();
        }
        let mut out = vec![
            Block { kind: Kind::Rule, ..Default::default() },
            Block { kind: Kind::Heading, level: 3, spans: vec![span(tr!("備忘稿"))], ..Default::default() },
        ];
        for b in body {
            // 備忘稿在畫面上用引言區塊,跟投影片本身的內容分開。
            out.push(Block { kind: Kind::Quote, spans: b.spans, ..Default::default() });
        }
        out
    }
}

fn span(text: String) -> Span {
    Span { text, ..Default::default() }
}

fn pres_part(p: &Package) -> String {
    if let Some(t) = p.rels_by_type("", "/officeDocument").into_iter().find(|t| p.has(t)) {
        return t;
    }
    if p.has("ppt/presentation.xml") {
        return "ppt/presentation.xml".to_string();
    }
    String::new()
}

fn core_title(p: &Package) -> String {
    for t in p.rels_by_type("", "/metadata/core-properties") {
        let Ok(b) = p.bytes(&t) else { continue };
        let mut dec = ooxml::new_decoder(b);
        let mut title = String::new();
        let _ = ooxml::each(&mut dec, |d, se| {
            if se.name.local != "coreProperties" {
                return Ok(false);
            }
            ooxml::each(d, |d, c| {
                if c.name.local == "title" {
                    title = ooxml::text(d).trim().to_string();
                    return Ok(true);
                }
                Ok(false)
            })
            .map(|_| true)
        });
        if !title.is_empty() {
            return title;
        }
    }
    String::new()
}

/// 依放映順序列出投影片組件。
///
/// [雷] 順序在 p:sldIdLst,不是關聯表也不是檔名。關聯表沒有順序,
/// 而檔名的數字是建立順序:搬動過投影片的簡報兩者就對不上,
/// 而且看起來完全正常,只是順序錯了。
fn slide_parts(p: &Package, main: &str) -> Vec<String> {
    let Ok(b) = p.bytes(main) else { return Vec::new() };
    let mut out = Vec::new();
    let mut dec = ooxml::new_decoder(b);
    match ooxml::root(&mut dec) {
        Ok(r) if r.name.local == "presentation" => {}
        _ => return Vec::new(),
    }
    let _ = ooxml::each(&mut dec, |d, se| {
        if se.name.local != "sldIdLst" {
            return Ok(false);
        }
        ooxml::each(d, |_, s| {
            if s.name.local != "sldId" || out.len() >= MAX_SLIDES {
                return Ok(false);
            }
            let t = p.rel_target(main, &ooxml::rel_id(s));
            if !t.is_empty() && p.has(&t) {
                out.push(t);
            }
            Ok(false)
        })
        .map(|_| true)
    });
    out
}

fn ph_of(typ: &str) -> PhKind {
    match typ {
        "title" | "ctrTitle" => PhKind::Title,
        // 沒寫型別時,規格的預設值是內文。
        _ => PhKind::Body,
    }
}

fn ph_idx(se: &StartElement) -> String {
    let v = ooxml::attr(se, "idx");
    if v.is_empty() { "0".to_string() } else { v }
}

/// Go 的 `n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)`:
/// 格式不對是 0,超出範圍是夾到上下限(錯誤被忽略,但值照樣回傳)。
fn atoi64(s: &str) -> i64 {
    match s.trim().parse::<i64>() {
        Ok(n) => n,
        Err(e) => match e.kind() {
            IntErrorKind::PosOverflow => i64::MAX,
            IntErrorKind::NegOverflow => i64::MIN,
            _ => 0,
        },
    }
}

/// 擋掉畫不出來的項目符號(Wingdings 落在私人使用區)。
fn safe_marker(s: &str) -> String {
    let mut cs = s.chars();
    match (cs.next(), cs.next()) {
        (Some(c), None) if !((0xE000..=0xF8FF).contains(&(c as u32)) || (c as u32) < 0x20) => s.to_string(),
        _ => String::new(),
    }
}

fn atoi_def(s: &str, def: i64) -> i64 {
    s.trim().parse::<i64>().unwrap_or(def)
}
