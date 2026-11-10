//! 把區塊排成固定寬度的列(對應 Go 版 `layout.go`)。

use std::sync::Arc;

use wincv_cell::Color;
use wincv_i18n::t as tr;
use wincv_render::Image;

use crate::wrap::{clip, expand_tabs, itoa, pad_to, width, wrap_spans};
use crate::{Block, Kind, Span, Style};

/// 水平線(Go 版 `cell.HLine`)。
const H_LINE: char = '─';
/// 垂直線(Go 版 `cell.VLine`)。
const V_LINE: char = '│';

/// 排版後一列上的一段文字。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Piece {
    pub text: String,
    pub fg: Color,
    pub bg: Color,
    pub under: bool,
    /// 連結目標,只有 Link 樣式才有。
    ///
    /// 排版之前 Span 就帶著它了,但排版之後如果不一起帶下來,
    /// 畫面上看得到底線、卻沒有東西可以點:上層拿不到目標。
    pub href: String,
}

/// 一張要畫在格點上的圖。
///
/// `cols` / `rows` 是它佔幾格;實際的像素貼圖由 render 那一層做,
/// 這裡只決定格位,markdown 這一包不碰像素。
#[derive(Clone, Debug, Default)]
pub struct Pic {
    pub src: String,
    pub alt: String,
    pub img: Option<Arc<Image>>,
    pub err: String,
    pub cols: i32,
    pub rows: i32,
    /// 在整份文件裡從第幾列開始。
    pub row: i32,
}

/// 排版後的一列。
#[derive(Clone, Debug, Default)]
pub struct Line {
    pub pieces: Vec<Piece>,
    /// 不為 None 表示這一列被一張圖佔著,與 `layout` 回傳的圖是同一個
    /// `Arc`(可以用 `Arc::ptr_eq` 比)。`pic_row` 是圖的第幾列。
    pub pic: Option<Arc<Pic>>,
    pub pic_row: i32,
}

/// 讀取圖片。傳 None 表示不載圖,圖片只顯示 alt。
///
/// 做成函式參數而不是直接讀檔:markdown 可能來自壓縮檔內部,
/// 那時候「相對路徑」要對到壓縮檔裡的成員,不是磁碟上的檔案。
///
/// **「哪些位址可以載」是載入器的政策,不是排版引擎的。** 看一份 .md
/// 不該變成連外行為,但使用者自己輸入網址進去的瀏覽模式就該載:
/// 同一個排版引擎服務兩種來源,把政策寫死在這裡會讓其中一種一定是錯的。
/// 回傳錯誤的話會原樣顯示在 alt 旁邊,所以訊息要寫給人看。
pub type Loader<'a> = &'a dyn Fn(&str) -> Result<Arc<Image>, String>;

/// 各種元素的顏色。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Theme {
    pub text: Color,
    pub heading: Color,
    pub code: Color,
    pub code_bg: Color,
    pub quote: Color,
    pub link: Color,
    pub marker: Color,
    pub rule: Color,
    pub table_head: Color,
    pub bg: Color,
}

/// 沿用 image 裡那 29 個具名顏色。
///
/// 挑色的原則跟 keyword_*.cfg 一樣:同一類東西同一個色相,
/// 靠明度分階層。標題最亮,正文次之,裝飾性的最暗。
///
/// 但「最暗」有下限:分隔線用 dkgray(#404040)畫在黑底上等於看不見,
/// 而看不見的分隔線與沒有分隔線是同一回事。改用 gray(#808080)。
pub fn default_theme() -> Theme {
    Theme {
        text: Color::LT_GRAY,
        heading: Color::LT_CYAN,
        code: Color::LT_GREEN,
        code_bg: Color::BLACK,
        quote: Color::GRAY,
        link: Color::LT_BLUE,
        marker: Color::LT_YELLOW,
        rule: Color::GRAY,
        table_head: Color::WHITE,
        bg: Color::BLACK,
    }
}

impl Default for Theme {
    fn default() -> Theme {
        default_theme()
    }
}

/// 一張圖最多佔幾列。
///
/// 沒有上限的話,一張直式圖會把整個畫面吃掉,而使用者要的多半是
/// 「文件裡有這張圖」而不是「這張圖填滿螢幕」。要看大圖按 Enter
/// 進看圖模式。
pub const MAX_PIC_ROWS: i32 = 24;

/// `strings.Repeat`:次數為負時 Go 會 panic,這裡照做。
fn rep(s: &str, n: i32) -> String {
    let n = usize::try_from(n).expect("strings: negative Repeat count");
    s.repeat(n)
}

/// 把區塊排成固定寬度的列。
///
/// `load` 為 None 時圖片只留 alt 文字。`cell_w` / `cell_h` 是格子的像素大小,
/// 用來把圖片的像素尺寸換算成格數:換算要在這裡做,因為
/// 「一張圖佔幾列」會影響後面所有內容的行號。
pub fn layout(blocks: &[Block], cols: i32, cell_w: i32, cell_h: i32, load: Option<Loader>, t: Theme) -> (Vec<Line>, Vec<Arc<Pic>>) {
    let cols = cols.max(8);
    let mut out: Vec<Line> = Vec::new();
    let mut pics: Vec<Arc<Pic>> = Vec::new();
    let line = |pieces: Vec<Piece>| Line { pieces, ..Default::default() };
    let piece = |text: String, fg: Color| Piece { text, fg, ..Default::default() };

    for (bi, b) in blocks.iter().enumerate() {
        if bi > 0 && b.kind != Kind::List && b.kind != Kind::Pre {
            // 清單項目之間不空行,其餘區塊之間空一行。
            //
            // Pre 也不空行:那是「呼叫端已經排好版」的意思,
            // 排版引擎再自己加空白就是在猜它的意圖。
            //
            // (這一層已經排除 b 是 List,所以「前後都是 List」的例外在 Go 版
            // 裡永遠不成立;照抄判斷式,行為相同。)
            let prev = &blocks[bi - 1];
            if !(prev.kind == Kind::List && b.kind == Kind::List) {
                out.push(Line::default());
            }
        }
        match b.kind {
            Kind::Heading => {
                let fg = t.heading;
                let pre = rep("#", b.level) + " ";
                for (i, ln) in wrap_spans(&b.spans, cols - pre.len() as i32).iter().enumerate() {
                    let mut p = if i > 0 { vec![piece(" ".repeat(pre.len()), t.text)] } else { vec![piece(pre.clone(), t.marker)] };
                    p.extend(styled(ln, fg, &t));
                    out.push(line(p));
                }
                // 一級與二級標題底下加一條線,層級用線條表示而不是靠顏色。
                if b.level <= 2 {
                    out.push(line(vec![piece(rep(&H_LINE.to_string(), cols), t.rule)]));
                }
            }
            Kind::Para => {
                for ln in wrap_spans(&b.spans, cols) {
                    out.push(line(styled(&ln, t.text, &t)));
                }
            }
            Kind::Quote => {
                for ln in wrap_spans(&b.spans, cols - 2) {
                    let mut p = vec![piece(format!("{V_LINE} "), t.rule)];
                    p.extend(styled(&ln, t.quote, &t));
                    out.push(line(p));
                }
            }
            Kind::List => {
                let ind = rep("  ", b.level);
                let mut mark = ind.clone() + "• ";
                if !b.marker.is_empty() {
                    // 符號與內文之間一定要有空白。標記是各種格式各自帶來的
                    // (RTF 帶「1.」、docx 帶「•」),誰有沒有留尾巴那個空格
                    // 並不一致,沒補的話會變成「1.內文」黏在一起。
                    mark = ind + &b.marker;
                    if !mark.ends_with(' ') {
                        mark.push(' ');
                    }
                } else if b.ordered {
                    mark = ind + &itoa(b.num) + ". ";
                }
                // Go 版用字元數(len([]rune(mark)))當記號的寬度,不是顯示格數:
                // 記號裡有全形字時,折行寬度與續行縮排都少算。照抄。
                let mark_n = mark.chars().count() as i32;
                let pad = rep(" ", mark_n);
                for (i, ln) in wrap_spans(&b.spans, cols - mark_n).iter().enumerate() {
                    let (m, fg) = if i > 0 { (pad.clone(), t.text) } else { (mark.clone(), t.marker) };
                    let mut p = vec![piece(m, fg)];
                    p.extend(styled(ln, t.text, &t));
                    out.push(line(p));
                }
            }
            Kind::Pre => {
                // 與 Code 的差別:不縮排、不套底色、用正文顏色。
                //
                // 給「本來就排好版、不能重新斷行」的內容用(gopher 的文字檔
                // 常常是 70 欄的 ASCII art)。markdown 的解析器不會產生這種區塊,
                // 它只從 layout 這一側被用到。
                //
                // 裁切照 Go 版以字元數計,不是顯示格數。
                for ln in &b.lines {
                    let ln = truncate_runes(expand_tabs(ln), cols);
                    out.push(line(vec![piece(ln, t.text)]));
                }
            }
            Kind::Code => {
                for ln in &b.lines {
                    let ln = truncate_runes(expand_tabs(ln), cols - 2);
                    out.push(line(vec![Piece { text: "  ".to_string() + &ln, fg: t.code, bg: t.code_bg, ..Default::default() }]));
                }
            }
            Kind::Rule => {
                out.push(line(vec![piece(rep(&H_LINE.to_string(), cols), t.rule)]));
            }
            Kind::Table => {
                out.extend(table(b, cols, &t));
            }
            Kind::Image => {
                let mut pic = load_pic(&b.src, &b.alt, cols, cell_w, cell_h, load);
                pic.row = out.len() as i32;
                let pic = Arc::new(pic);
                pics.push(pic.clone());
                for r in 0..pic.rows {
                    out.push(Line { pic: Some(pic.clone()), pic_row: r, ..Default::default() });
                }
                if pic.img.is_none() {
                    // 載不到就把 alt 與原因寫出來。留白的話使用者只會覺得
                    // 「這裡本來就沒東西」,而事實是「有圖但畫不出來」。
                    let mut txt = tr("[圖] ") + &pic.alt;
                    if !pic.err.is_empty() {
                        txt += " —— ";
                        txt += &pic.err;
                    }
                    out.push(line(vec![piece(clip(&txt, cols), t.quote)]));
                }
            }
        }
    }
    (out, pics)
}

/// `string([]rune(s)[:n])`,只在字元數超過 n 時才切。
fn truncate_runes(s: String, n: i32) -> String {
    if s.chars().count() as i32 > n {
        s.chars().take(n.max(0) as usize).collect()
    } else {
        s
    }
}

fn load_pic(src: &str, alt: &str, cols: i32, cell_w: i32, cell_h: i32, load: Option<Loader>) -> Pic {
    let mut p = Pic { src: src.to_string(), alt: alt.to_string(), ..Default::default() };
    match load {
        None => p.err = tr("沒有載圖器"),
        Some(load) => match load(src) {
            Ok(img) => p.img = Some(img),
            Err(e) => p.err = e,
        },
    }
    let Some(img) = p.img.as_ref() else {
        return p;
    };
    let (dx, dy) = (i64::from(img.w), i64::from(img.h));
    if dx <= 0 || dy <= 0 || cell_w <= 0 || cell_h <= 0 {
        p.img = None;
        p.err = tr("尺寸不合理");
        return p;
    }
    // 先照原尺寸換算成格數,再縮到畫面容得下。Go 的 int 是 64 位元,這裡也用 i64 算。
    let (cw, ch, cols) = (i64::from(cell_w), i64::from(cell_h), i64::from(cols));
    let mut c = (dx + cw - 1) / cw;
    let mut r = (dy + ch - 1) / ch;
    if c > cols {
        r = r * cols / c;
        c = cols;
    }
    let max = i64::from(MAX_PIC_ROWS);
    if r > max {
        c = c * max / r;
        r = max;
    }
    p.cols = c.max(1) as i32;
    p.rows = r.max(1) as i32;
    p
}

fn table(b: &Block, cols: i32, t: &Theme) -> Vec<Line> {
    if b.rows.is_empty() {
        return Vec::new();
    }
    let n = b.rows.iter().map(|r| r.len()).max().unwrap_or(0);
    let mut w = vec![0i64; n];
    for r in &b.rows {
        for (i, c) in r.iter().enumerate() {
            let v = i64::from(width(c));
            if v > w[i] {
                w[i] = v;
            }
        }
    }
    // 太寬就等比例壓,不要讓表格把畫面撐爆。
    //
    // 每一欄的寬度都是 0(整張表都是空格子)而欄數又多到 1+3n 超過 cols 時,
    // 分母是 0。Go 版在這裡會 panic(integer divide by zero),Rust 的整數除法
    // 同樣 panic,行為相同。
    let n64 = n as i64;
    let total: i64 = 1 + w.iter().map(|v| v + 3).sum::<i64>();
    let cols64 = i64::from(cols);
    if total > cols64 {
        for v in w.iter_mut() {
            *v = *v * (cols64 - 1 - 3 * n64) / (total - 1 - 3 * n64);
            if *v < 1 {
                *v = 1;
            }
        }
    }
    let mut out = Vec::new();
    for (ri, r) in b.rows.iter().enumerate() {
        let fg = if ri == 0 { t.table_head } else { t.text };
        let mut ps = Vec::new();
        for i in 0..n {
            let wi = w[i] as i32;
            let s = if i < r.len() { clip(&r[i], wi) } else { String::new() };
            let align = b.aligns.get(i).copied().unwrap_or(b'l');
            ps.push(Piece { text: " ".to_string() + &pad_to(&s, wi, align) + " ", fg, ..Default::default() });
            if i + 1 < n {
                ps.push(Piece { text: V_LINE.to_string(), fg: t.rule, ..Default::default() });
            }
        }
        out.push(Line { pieces: ps, ..Default::default() });
        if ri == 0 {
            let mut sep = String::new();
            for (i, &wi) in w.iter().enumerate() {
                sep.push_str(&rep(&H_LINE.to_string(), (wi + 2) as i32));
                if i + 1 < n {
                    sep.push(H_LINE);
                }
            }
            out.push(Line { pieces: vec![Piece { text: sep, fg: t.rule, ..Default::default() }], ..Default::default() });
        }
    }
    out
}

/// 把一列的片段轉成畫得出來的 Piece。
///
/// 格點畫面沒有字重也沒有斜體(只有一種字模),所以粗體與斜體只能
/// 用顏色表示,這是格點的先天限制,不是偷懶。粗體用亮一階的顏色,
/// 斜體與刪除線用暗一階,讀者仍然分得出層次。
fn styled(sp: &[Span], base: Color, t: &Theme) -> Vec<Piece> {
    sp.iter()
        .map(|s| {
            let (mut fg, mut bg, mut under) = (base, t.bg, false);
            if s.style.has(Style::MONO) {
                (fg, bg) = (t.code, t.code_bg);
            } else if s.style.has(Style::LINK) {
                (fg, under) = (t.link, true);
            } else if s.style.has(Style::BOLD) {
                fg = Color::WHITE;
            } else if s.style.has(Style::STRIKE) {
                fg = t.rule;
            } else if s.style.has(Style::ITALIC) {
                fg = t.quote;
            }
            Piece { text: s.text.clone(), fg, bg, under, href: s.href.clone() }
        })
        .collect()
}
