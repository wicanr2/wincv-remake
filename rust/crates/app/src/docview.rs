//! Office 文件(對應 Go 版 `docview.go`)。
//!
//! 與書、PDF 同一個作法:一份文件就是「幾段內容加上一份清單」,借瀏覽模式的連結導覽。

use std::path::Path;

use wincv_i18n::t;
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_render::Image;

use crate::bookview::go_atoi;
use crate::browse::BrowseView;
use crate::{App, Mode};

/// Office 文件內位址的前綴。
pub(crate) const DOC_SCHEME: &str = "doc:";

/// 組出文件內的位址。part 從 1 起算,0 表示分段清單。
pub(crate) fn doc_url(path: &str, part: i32) -> String {
    if part < 1 {
        return format!("{DOC_SCHEME}{path}");
    }
    format!("{DOC_SCHEME}{path}#{part}")
}

/// 拆開文件內位址:(路徑, 段)。
pub(crate) fn parse_doc_url(raw: &str) -> Option<(String, i32)> {
    let rest = raw.strip_prefix(DOC_SCHEME)?;
    if let Some(i) = rest.rfind('#') {
        if let Ok(n) = go_atoi(&rest[i + 1..]) {
            return Some((rest[..i].to_string(), n));
        }
    }
    Some((rest.to_string(), 0))
}

/// 這個檔名是不是 Word / PowerPoint / Excel 文件。
pub(crate) fn is_office_doc(name: &str) -> bool {
    wincv_officedoc::is(name)
}

fn doc_img_ref(r: &str) -> String {
    format!("{DOC_SCHEME}img/{r}")
}

fn parse_doc_img_ref(src: &str) -> Option<&str> {
    src.strip_prefix("doc:img/")
}

/// 把區塊裡的圖片參照加上前綴。解析那一層產生的是文件內部的名字(它不知道自己會被誰
/// 用),而排版那一層要靠前綴分辨「這張圖該去哪裡拿」。
fn doc_image_refs(mut bs: Vec<Block>) -> Vec<Block> {
    for b in &mut bs {
        if b.kind == Kind::Image && !b.src.starts_with(DOC_SCHEME) {
            b.src = doc_img_ref(&b.src);
        }
    }
    bs
}

fn base_name(path: &str) -> String {
    Path::new(path).file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_else(|| path.to_string())
}

fn link(text: String, href: String) -> Span {
    Span { text, style: Style::LINK, href }
}

fn list(sp: Span) -> Block {
    Block { kind: Kind::List, marker: "  ".into(), spans: vec![sp], ..Default::default() }
}

/// 排出分段清單。
fn doc_toc(d: &wincv_officedoc::Doc, path: &str, name: &str) -> Vec<Block> {
    let title = if d.title.is_empty() { name.to_string() } else { d.title.clone() };
    let mut out = vec![Block { kind: Kind::Heading, level: 1, spans: vec![Span { text: title, ..Default::default() }], ..Default::default() }];
    for (i, p) in d.parts.iter().enumerate() {
        out.push(list(link(format!("{}. {}", i + 1, p.title), doc_url(path, i as i32 + 1))));
    }
    out
}

/// 排出頁尾的導覽。只有一段的文件不需要:那時候上一段、下一段、清單三個連結都指向
/// 同一個地方。
fn doc_nav(d: &wincv_officedoc::Doc, path: &str, part: i32) -> Vec<Block> {
    let n = d.parts.len() as i32;
    if n <= 1 {
        return Vec::new();
    }
    let word = d.kind.part_word();
    let mut links = Vec::new();
    if part > 1 {
        links.push(link(t("← 上一") + &word, doc_url(path, part - 1)));
    }
    links.push(link(word.clone() + &t("清單"), doc_url(path, 0)));
    if part < n {
        links.push(link(t("下一") + &word + " →", doc_url(path, part + 1)));
    }
    let mut out = vec![Block { kind: Kind::Rule, ..Default::default() }];
    out.extend(links.into_iter().map(list));
    out
}

impl App {
    /// 用瀏覽模式打開一份 Office 文件,停在第一段。
    pub(crate) fn open_office(&mut self, name: &str) -> bool {
        let full = self.browser.dir.join(self.os_name(name));
        self.bv = BrowseView::default();
        self.mode = Mode::Browse;
        self.browse_fetch(&doc_url(&full.to_string_lossy(), 1), false);
        true
    }

    /// 把文件內位址變成畫面。同步做,理由與書、PDF 相同:那是本機檔案,為它多開一條
    /// 非同步的路只會多一種狀態要處理。
    pub(crate) fn show_office(&mut self, raw: &str) {
        let Some((path, part)) = parse_doc_url(raw) else { return };
        if let Err(e) = self.load_office(&path) {
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

        let d = self.office.as_ref().expect("load_office 成功後一定有文件");
        let name = base_name(&path);
        let n = d.parts.len() as i32;
        if part < 1 || part > n {
            self.bv.title = name.clone();
            self.bv.blocks = doc_toc(d, &path, &name);
            return;
        }
        self.bv.title = if n > 1 { format!("{name} — {} {part}/{n}", d.kind.part_word()) } else { name };
        let mut blocks = doc_image_refs(d.blocks(part as usize - 1));
        blocks.extend(doc_nav(d, &path, part));
        self.bv.blocks = blocks;
    }

    fn load_office(&mut self, path: &str) -> Result<(), String> {
        if self.office.is_some() && self.office_path == path {
            return Ok(());
        }
        self.close_office();
        self.office = Some(wincv_officedoc::open(path)?);
        self.office_path = path.to_string();
        Ok(())
    }

    /// 關掉開著的文件。離開瀏覽模式時呼叫。
    pub(crate) fn close_office(&mut self) {
        if let Some(d) = self.office.take() {
            d.close();
        }
        self.office_path.clear();
    }

    /// 讀文件裡的一張圖。
    pub(crate) fn office_image(&self, src: &str) -> Result<Image, String> {
        let (Some(r), Some(d)) = (parse_doc_img_ref(src), &self.office) else {
            return Err(t("不是這份文件的圖"));
        };
        let data = d.image(r)?;
        wincv_imgfmt::decode(r, &data).map(|(m, _)| m)
    }
}
