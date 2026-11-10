//! EPUB 電子書(對應 Go 版 `bookview.go`)。
//!
//! 借瀏覽模式的殼:一本書就是「一份目錄加上一堆用連結串起來的章節」,與 gopher 選單是
//! 同一個形狀。連結導覽、上一頁、圖片、排版全部共用,這一層只要把「章節」翻成「位址」。

use std::path::Path;

use wincv_i18n::{t, tr};
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_render::Image;

use crate::browse::{img_name_of, BrowseView};
use crate::{App, Mode};

/// 書內位址的前綴。
pub(crate) const EPUB_SCHEME: &str = "epub:";

/// 組出書內位址。chapter 為負數表示目錄那一頁。
pub(crate) fn book_url(path: &str, chapter: i32) -> String {
    if chapter < 0 {
        return format!("{EPUB_SCHEME}{path}");
    }
    format!("{EPUB_SCHEME}{path}#{chapter}")
}

/// 拆開書內位址:(路徑, 章節)。章節 -1 表示目錄。
pub(crate) fn parse_book_url(raw: &str) -> Option<(String, i32)> {
    let mut rest = raw.strip_prefix(EPUB_SCHEME)?;
    let mut chapter = -1;
    // 從**最後**一個 # 切:書的路徑本身可能含 #。Go 的 Atoi 吃前置的 + 與 -,
    // i32 的 parse 也一樣。
    if let Some(i) = rest.rfind('#') {
        if let Ok(n) = go_atoi(&rest[i + 1..]) {
            (chapter, rest) = (n, &rest[..i]);
        }
    }
    Some((rest.to_string(), chapter))
}

/// Go 的 strconv.Atoi:可帶一個正負號,其餘都要是十進位數字。Go 的 int 是 64 位元,
/// 超出 i32 的章節號在後面一定找不到那一節,一律當解不開。
fn go_atoi(s: &str) -> Result<i32, ()> {
    let d = s.strip_prefix(['+', '-']).unwrap_or(s);
    if d.is_empty() || !d.bytes().all(|b| b.is_ascii_digit()) {
        return Err(());
    }
    s.parse::<i64>().ok().and_then(|n| i32::try_from(n).ok()).ok_or(())
}

/// 這個檔名是不是這一包看得懂的書。
pub(crate) fn is_book(name: &str) -> bool {
    Path::new(name).extension().is_some_and(|e| e.eq_ignore_ascii_case("epub"))
}

fn link(text: String, href: String) -> Span {
    Span { text, style: Style::LINK, href }
}

/// 排出目錄那一頁。
fn book_toc(b: &wincv_epub::Book, path: &str) -> Vec<Block> {
    let mut out = vec![Block { kind: Kind::Heading, level: 1, spans: vec![Span { text: b.title.clone(), ..Default::default() }], ..Default::default() }];
    if !b.author.is_empty() {
        out.push(Block { kind: Kind::Para, spans: vec![Span { text: b.author.clone(), style: Style::ITALIC, ..Default::default() }], ..Default::default() });
    }
    for (i, c) in b.chapters.iter().enumerate() {
        out.push(Block { kind: Kind::List, marker: format!("{:3}. ", i + 1), spans: vec![link(c.title.clone(), book_url(path, i as i32))], ..Default::default() });
    }
    out
}

/// 章節末尾的上下一節。
fn book_nav(b: &wincv_epub::Book, path: &str, cur: i32) -> Vec<Block> {
    let mut links = Vec::new();
    if cur > 0 {
        links.push(link(t("← 上一節"), book_url(path, cur - 1)));
    }
    links.push(link(t("目錄"), book_url(path, -1)));
    if ((cur + 1) as usize) < b.chapters.len() {
        links.push(link(t("下一節 →"), book_url(path, cur + 1)));
    }
    let mut out = vec![Block { kind: Kind::Rule, ..Default::default() }];
    // 一列一個連結:游標是以列為單位停的,擠在同一段裡就只選得到第一個。
    for sp in links {
        out.push(Block { kind: Kind::List, marker: "  ".into(), spans: vec![sp], ..Default::default() });
    }
    out
}

impl App {
    /// 用瀏覽模式打開一本書,停在目錄那一頁。
    pub(crate) fn open_book(&mut self, name: &str) -> bool {
        let full = self.browser.dir.join(self.os_name(name));
        self.bv = BrowseView::default();
        self.mode = Mode::Browse;
        self.browse_fetch(&book_url(&full.to_string_lossy(), -1), false);
        true
    }

    /// 把書內位址變成畫面。同步做:讀的是本機的 zip,不像網路那樣可能等上幾十秒。
    pub(crate) fn show_book(&mut self, raw: &str) {
        let Some((path, chapter)) = parse_book_url(raw) else { return };
        if let Err(e) = self.load_book(&path) {
            self.bv.status = e;
            return;
        }
        let bv = &mut self.bv;
        bv.url = raw.to_string();
        bv.status.clear();
        (bv.top, bv.cur, bv.cols) = (0, -1, 0);
        bv.lines = None;
        bv.imgs.clear();
        bv.want.clear();
        let book = self.book.as_ref().expect("load_book 成功後一定有書");
        if chapter < 0 {
            self.bv.title = book.title.clone();
            self.bv.blocks = book_toc(book, &path);
            return;
        }
        // Go 版的負數以外的越界會在 Blocks 裡回錯誤;這裡照樣交給 blocks 判斷。
        match book.blocks(chapter as usize) {
            Ok(mut blocks) => {
                self.bv.title = book.chapters[chapter as usize].title.clone();
                // 前後接一列導覽,不然讀完一節之後只能按 Backspace 回目錄再點下一節。
                blocks.extend(book_nav(book, &path, chapter));
                self.bv.blocks = blocks;
            }
            Err(e) => self.bv.status = e,
        }
    }

    /// 需要時打開書。同一本書不重開:換一節就重解一次 zip 會讓翻頁變成等待。
    fn load_book(&mut self, path: &str) -> Result<(), String> {
        if self.book.is_some() && self.book_path == path {
            return Ok(());
        }
        self.close_book();
        let b = wincv_epub::open(path).map_err(|e| tr!("開不了這本書:%w", e))?;
        self.book = Some(b);
        self.book_path = path.to_string();
        Ok(())
    }

    /// 關掉開著的書。離開瀏覽模式時呼叫。
    pub(crate) fn close_book(&mut self) {
        self.book = None;
        self.book_path.clear();
    }

    /// 讀書裡的一張圖。
    pub(crate) fn book_image(&self, src: &str) -> Result<Image, String> {
        let Some(b) = &self.book else { return Err(t("沒有開著的書")) };
        let data = b.image(src)?;
        wincv_imgfmt::decode(img_name_of(src), &data).map(|(m, _)| m)
    }
}
