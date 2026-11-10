//! PDF(對應 Go 版 `pdfview.go`)。
//!
//! 與書同一個作法:一份 PDF 就是「一份頁碼清單加上一頁一頁的內容」,借瀏覽模式的連結
//! 導覽。V 把這一頁整頁畫成圖,丟給看圖模式。

use std::path::Path;
use std::rc::Rc;

use wincv_i18n::{t, tr};
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_pdfdoc::Doc;
use wincv_render::Image;

use crate::bookview::go_atoi;
use crate::browse::BrowseView;
use crate::{App, Mode};

/// PDF 內位址的前綴。
pub(crate) const PDF_SCHEME: &str = "pdf:";

/// 「看整頁」時的解析度。150 比螢幕的 96 高一階:看圖模式可以放大,而放大一張 96 dpi
/// 的圖會糊掉。再高的話一頁 A4 就超過三千萬個像素,在手機上會吃掉記憶體。
pub const PAGE_IMAGE_DPI: f64 = 150.0;

/// 重畫一頁的像素上限。8 倍解析度的 A4 是一億三千萬個像素,一張 RGBA 就要 500 MB。
/// 超過就不重畫,退回點陣放大:那時使用者看到的是糊的,但畫面還在動。
pub const MAX_PAGE_IMAGE_PIXELS: f64 = (40 << 20) as f64;

pub(crate) fn pdf_url(path: &str, page: i32) -> String {
    if page < 1 {
        return format!("{PDF_SCHEME}{path}");
    }
    format!("{PDF_SCHEME}{path}#{page}")
}

/// 拆開 PDF 內位址:(路徑, 頁)。頁 0 表示頁碼清單。
pub(crate) fn parse_pdf_url(raw: &str) -> Option<(String, i32)> {
    let mut rest = raw.strip_prefix(PDF_SCHEME)?;
    let mut page = 0;
    if let Some(i) = rest.rfind('#') {
        if let Ok(n) = go_atoi(&rest[i + 1..]) {
            (page, rest) = (n, &rest[..i]);
        }
    }
    Some((rest.to_string(), page))
}

/// 這個檔名是不是 PDF。
pub(crate) fn is_pdf(name: &str) -> bool {
    Path::new(name).extension().is_some_and(|e| e.eq_ignore_ascii_case("pdf"))
}

/// 圖片在這一份 PDF 裡的參照。帶頁碼是必要的:不同頁可以有同名的 XObject,只用名字
/// 會取到別頁的圖。
pub(crate) fn pdf_img_ref(page: i32, name: &str) -> String {
    format!("{PDF_SCHEME}page/{page}/{name}")
}

pub(crate) fn parse_pdf_img_ref(r: &str) -> Option<(i32, String)> {
    let rest = r.strip_prefix("pdf:page/")?;
    let (num, name) = rest.split_once('/')?;
    Some((go_atoi(num).ok()?, name.to_string()))
}

fn base_name(path: &str) -> String {
    Path::new(path).file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_else(|| path.to_string())
}

fn para(text: String, style: Style) -> Block {
    Block { kind: Kind::Para, spans: vec![Span { text, style, ..Default::default() }], ..Default::default() }
}

fn link_item(text: String, href: String) -> Block {
    Block { kind: Kind::List, marker: "  ".into(), spans: vec![Span { text, style: Style::LINK, href }], ..Default::default() }
}

/// 頁碼轉成 pdfdoc 的頁碼(從 1 起算)。負數交給 pdfdoc 當成越界。
fn upage(page: i32) -> usize {
    if page < 1 { usize::MAX } else { page as usize }
}

/// 排出一頁。
fn pdf_page_blocks(d: &Doc, path: &str, page: i32) -> Vec<Block> {
    let mut out = Vec::new();
    match d.text(upage(page)) {
        Err(e) => out.push(para(e, Style::default())),
        Ok(lines) if !lines.is_empty() => {
            // 用 Pre:文字的位置是從座標算回來的,重新斷行會把好不容易對回去的縮排再
            // 弄亂一次。
            let texts = lines.iter().map(|ln| " ".repeat(ln.indent.clamp(0, 20) as usize) + &ln.text).collect();
            out.push(Block { kind: Kind::Pre, lines: texts, ..Default::default() });
        }
        Ok(_) => out.push(para(t("(這一頁沒有可以取出的文字)"), Style::default())),
    }
    // 圖片放在頁末,標明是這一頁的。不放回文字中間是因為位置資訊拿不到:PDF 把圖畫在
    // 哪裡是 content stream 裡的座標變換,而抽圖的那一層只交出點陣資料。
    if let Ok(names) = d.image_names(upage(page)) {
        if !names.is_empty() {
            out.push(Block { kind: Kind::Rule, ..Default::default() });
            out.push(para(tr!("本頁圖片(%d 張)", names.len()), Style::BOLD));
            for n in names {
                out.push(Block { kind: Kind::Image, src: pdf_img_ref(page, &n), alt: n, ..Default::default() });
            }
        }
    }
    out.extend(pdf_nav(d, path, page));
    out
}

/// 排出目錄。PDF 自己帶的書籤才是這份文件的章節結構;一排頁碼只是退路。兩者都給,
/// 書籤在前。
fn pdf_toc(d: &Doc, path: &str, name: &str) -> Vec<Block> {
    let mut out = vec![Block { kind: Kind::Heading, level: 1, spans: vec![Span { text: name.to_string(), ..Default::default() }], ..Default::default() }];
    let marks = d.outline();
    if !marks.is_empty() {
        for m in marks {
            let mut sp = Span { text: m.title, ..Default::default() };
            if m.page >= 1 && m.page <= d.pages {
                (sp.style, sp.href) = (Style::LINK, pdf_url(path, m.page as i32));
            }
            // 書籤可以巢狀很多層,而畫面的寬度有限。
            out.push(Block { kind: Kind::List, level: (m.level + 1).clamp(1, 4), marker: "  ".into(), spans: vec![sp], ..Default::default() });
        }
        out.push(Block { kind: Kind::Rule, ..Default::default() });
        out.push(Block { kind: Kind::Heading, level: 2, spans: vec![Span { text: t("頁碼"), ..Default::default() }], ..Default::default() });
    }
    for i in 1..=d.pages {
        out.push(link_item(tr!("第 %d 頁", i), pdf_url(path, i as i32)));
    }
    out
}

fn pdf_nav(d: &Doc, path: &str, page: i32) -> Vec<Block> {
    let mut out = vec![Block { kind: Kind::Rule, ..Default::default() }];
    if page > 1 {
        out.push(link_item(t("← 上一頁"), pdf_url(path, page - 1)));
    }
    out.push(link_item(t("頁碼清單"), pdf_url(path, 0)));
    if (page as i64) < d.pages as i64 {
        out.push(link_item(t("下一頁 →"), pdf_url(path, page + 1)));
    }
    out
}

/// 以指定倍率重畫一頁。
fn render_page(d: &Doc, page: i32, zoom: f64) -> Result<Image, String> {
    let dpi = PAGE_IMAGE_DPI * zoom;
    if let Ok((w, h)) = d.page_size(upage(page)) {
        if w / 72.0 * dpi * h / 72.0 * dpi > MAX_PAGE_IMAGE_PIXELS {
            return Err(tr!("這一頁放大到 %g 倍會超過記憶體上限", zoom));
        }
    }
    d.render(upage(page), dpi).map(|r| r.img)
}

impl App {
    /// 用瀏覽模式打開一份 PDF,停在第一頁。不從目錄開始:PDF 的「目錄」只是一排頁碼,
    /// 而使用者要的就是第一頁。頁末有導覽,要跳頁再回目錄。
    pub(crate) fn open_pdf(&mut self, name: &str) -> bool {
        let full = self.browser.dir.join(self.os_name(name));
        self.bv = BrowseView::default();
        self.mode = Mode::Browse;
        self.browse_fetch(&pdf_url(&full.to_string_lossy(), 1), false);
        true
    }

    /// 把 PDF 內位址變成畫面。同步做,理由與書相同。
    pub(crate) fn show_pdf(&mut self, raw: &str) {
        let Some((path, page)) = parse_pdf_url(raw) else { return };
        if let Err(e) = self.load_pdf(&path) {
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
        let d = self.pdf.clone().expect("load_pdf 成功後一定有 PDF");
        let name = base_name(&path);
        if page < 1 {
            self.bv.title = name.clone();
            self.bv.blocks = pdf_toc(&d, &path, &name);
            return;
        }
        self.bv.title = tr!("%s — 第 %d/%d 頁", name, page, d.pages);
        self.bv.blocks = pdf_page_blocks(&d, &path, page);
    }

    fn load_pdf(&mut self, path: &str) -> Result<(), String> {
        if self.pdf.is_some() && self.pdf_path == path {
            return Ok(());
        }
        self.close_pdf();
        self.pdf = Some(Rc::new(wincv_pdfdoc::open(path)?));
        self.pdf_path = path.to_string();
        Ok(())
    }

    pub(crate) fn close_pdf(&mut self) {
        self.pdf = None;
        self.pdf_path.clear();
    }

    /// 把目前這一頁畫成圖。取文字看的是內容,看整頁看的是版面:表格、圖表、公式、簽名,
    /// 那些東西的意義在位置上,抽成文字就沒了。
    pub(crate) fn show_pdf_page_image(&mut self) -> bool {
        let Some((path, page)) = parse_pdf_url(&self.bv.url).filter(|&(_, p)| p >= 1) else {
            self.bv.status = t("這裡沒有可以畫的頁面");
            return true;
        };
        if let Err(e) = self.load_pdf(&path) {
            self.bv.status = e;
            return true;
        }
        let d = self.pdf.clone().expect("load_pdf 成功後一定有 PDF");
        let r = match d.render(upage(page), PAGE_IMAGE_DPI) {
            Ok(r) => r,
            Err(e) => {
                self.bv.status = e;
                return true;
            }
        };
        let name = tr!("%s — 第 %d 頁", base_name(&path), page);
        let mut m = wincv_imgview::Model::from_image(&name, "PDF", r.img, 0);
        // PDF 是向量的:放大時用更高的解析度重畫,而不是把 150 dpi 的點陣圖拉大。差別在
        // 放大之後看不看得到更多東西:表格的細線、小字的註腳。
        m.rerender = Some(Box::new(move |zoom| render_page(&d, page, zoom)));
        self.image = Some(m);
        self.mode = Mode::Image;
        self.g_return = true;
        // 字形不是原檔那一套時要講出來:畫面看起來正常,差別在字形,不講的話使用者會
        // 以為看到的就是原樣。
        if !r.substituted.is_empty() {
            self.message = t("有字型畫不出來,改用系統字型:") + &r.substituted.join("、");
        }
        if !r.missing.is_empty() {
            self.message = t("有字型畫不出來,那些字沒有顯示:") + &r.missing.join("、");
        }
        true
    }

    /// 在整頁模式下換頁。看圖模式的「上一張 / 下一張」走的是目錄裡的圖檔清單,而 PDF 的
    /// 整頁圖不在那份清單上;在整頁模式下,「下一張」的自然意思就是下一頁。
    ///
    /// 換頁時 bv 的內容也一起換掉:Esc 退回瀏覽模式時看到的要是同一頁的文字。
    pub(crate) fn pdf_page_step(&mut self, d: i32) -> bool {
        let Some((path, page)) = parse_pdf_url(&self.bv.url).filter(|&(_, p)| p >= 1) else { return false };
        let Some(pages) = self.pdf.as_ref().map(|p| p.pages as i64) else { return false };
        let n = page + d;
        if n < 1 || n as i64 > pages {
            self.message = tr!("已經是第 %d 頁", page);
            return true;
        }
        // 倍率跟著人走:放大到 2 倍在讀表格的人翻頁之後還在讀表格。
        let (zoom, fit) = self.image.as_ref().map_or((1.0, true), |m| (m.zoom, m.fit));
        self.show_pdf(&pdf_url(&path, n));
        if !self.show_pdf_page_image() {
            return false;
        }
        if let Some(m) = self.image.as_mut().filter(|_| !fit) {
            m.set_zoom(zoom);
        }
        true
    }

    /// 現在看的是不是 PDF 的整頁圖。
    pub(crate) fn in_pdf_page(&self) -> bool {
        self.g_return && self.bv.url.starts_with(PDF_SCHEME)
    }

    /// 讀 PDF 裡的一張圖。
    pub(crate) fn pdf_image(&self, r: &str) -> Result<Image, String> {
        let (Some((page, name)), Some(d)) = (parse_pdf_img_ref(r), &self.pdf) else {
            return Err(t("不是這份 PDF 的圖"));
        };
        let data = d.image(upage(page), &name)?;
        wincv_imgfmt::decode(&name, &data).map(|(m, _)| m)
    }
}
