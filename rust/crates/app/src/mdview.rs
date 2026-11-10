//! markdown 檢視模式(對應 Go 版 `mdview.go`)。

use std::io::Read;
use std::path::PathBuf;
use std::sync::Arc;

use wincv_cell::{Color, Screen};
use wincv_i18n::{t, tr};
use wincv_keys::{Code, Key};
use wincv_markdown::{Block, Line, Pic};
use wincv_render::{Image, Overlay};

use crate::{cell_width, App, Mode};

/// markdown 內嵌圖片的大小上限。文件裡順手引到一個 200 MB 的 TIFF 不是沒有的事,而看
/// 文件的人要的是「這裡有張圖」。超過就只顯示 alt 與原因。
pub const MAX_MD_IMAGE_BYTES: u64 = 32 << 20;

/// markdown 檢視模式的狀態。
#[derive(Default)]
pub(crate) struct MdView {
    pub name: String,
    pub blocks: Vec<Block>,
    pub lines: Option<Vec<Line>>,
    pub top: i32,
    /// 上次排版用的欄數,寬度變了要重排。
    pub cols: i32,
    pub dir: PathBuf,
}

/// 這個檔名是不是 markdown。
pub fn is_markdown(name: &str) -> bool {
    let base = name.rsplit('/').next().unwrap_or(name);
    let ext = base.rfind('.').map(|i| base[i..].to_lowercase()).unwrap_or_default();
    matches!(ext.as_str(), ".md" | ".markdown" | ".mdown" | ".mkd")
}

/// Go 的 path.Clean(只處理 / 分隔的路徑)。
fn clean_slash(p: &str) -> String {
    let rooted = p.starts_with('/');
    let mut out: Vec<&str> = Vec::new();
    for seg in p.split('/') {
        match seg {
            "" | "." => {}
            ".." => {
                if out.last().is_some_and(|&s| s != "..") {
                    out.pop();
                } else if !rooted {
                    out.push("..");
                }
            }
            s => out.push(s),
        }
    }
    let s = out.join("/");
    match (rooted, s.is_empty()) {
        (true, _) => format!("/{s}"),
        (false, true) => ".".into(),
        (false, false) => s,
    }
}

impl App {
    /// 用 markdown 模式開一份文件。
    pub(crate) fn open_markdown(&mut self, name: &str, data: &[u8]) {
        let enc = wincv_textenc::detect(data);
        self.md = MdView {
            name: name.to_string(),
            blocks: wincv_markdown::parse(&wincv_textenc::decode(data, enc)),
            dir: self.browser.dir.clone(),
            ..Default::default()
        };
        self.mode = Mode::Markdown;
        self.recall_pos();
    }

    /// 讀取 markdown 裡引到的圖。
    ///
    /// 只認相對於文件所在目錄的路徑,而且要求整理過的路徑仍在該目錄之下:一份來路不明
    /// 的 .md 寫 `![](../../../etc/shadow)` 不該讓看文件的人把那個檔案讀進記憶體。
    ///
    /// 讀檔走目前列表的檔案系統,所以壓縮檔裡的 .md 也看得到同一個壓縮檔裡的圖
    /// (Go 版走的是真實檔案系統,壓縮檔裡的圖一律「找不到」)。
    pub(crate) fn md_image(&self, src: &str) -> Result<Arc<Image>, String> {
        if src.is_empty() {
            return Err(t("沒有路徑"));
        }
        // 遠端圖片不抓:開一份 .md 不該變成連外行為,而那份 .md 可能是從壓縮檔或別人
        // 給的目錄裡來的。
        let lower = src.to_lowercase();
        if ["http://", "https://", "//", "data:"].iter().any(|p| lower.starts_with(p)) {
            return Err(t("遠端圖片不下載"));
        }
        let clean = clean_slash(&src.replace('\\', "/"));
        if clean.starts_with('/') || clean.starts_with("../") || clean == ".." {
            return Err(t("只能引用文件所在目錄底下的圖"));
        }
        let full = match self.archive() {
            Some(af) => {
                let mut inner = af.inner(&self.md.dir);
                if !inner.is_empty() {
                    inner.push(b'/');
                }
                inner.extend_from_slice(clean.as_bytes());
                af.path(&inner)
            }
            None => self.md.dir.join(&clean),
        };
        let mut rc = self.browser.fs.open(&full).map_err(|_| t("找不到"))?;
        // 上限用讀的位元組數把關,不靠檔案大小:壓縮檔成員宣告的大小與實際解出來的
        // 大小未必一致。
        let mut data = Vec::new();
        (&mut rc).take(MAX_MD_IMAGE_BYTES + 1).read_to_end(&mut data).map_err(|e| e.to_string())?;
        if data.len() as u64 > MAX_MD_IMAGE_BYTES {
            return Err(tr!("超過 %s", comma(MAX_MD_IMAGE_BYTES as i64)));
        }
        let base = clean.rsplit('/').next().unwrap_or(&clean);
        wincv_imgfmt::decode(base, &data).map(|(img, _)| Arc::new(img))
    }

    /// 需要時重新排版。欄數沒變就沿用上次的結果:排版要解圖檔,每幀重做會讓捲動變成
    /// 幻燈片。
    fn md_layout(&mut self, cols: i32) {
        if cols == self.md.cols && self.md.lines.is_some() {
            return;
        }
        let load = |src: &str| self.md_image(src);
        let (lines, _) = wincv_markdown::layout(
            &self.md.blocks,
            cols,
            self.cell_w,
            self.cell_h,
            Some(&load),
            wincv_markdown::default_theme(),
        );
        self.md.cols = cols;
        self.md.lines = Some(lines);
    }

    fn md_lines(&self) -> &[Line] {
        self.md.lines.as_deref().unwrap_or(&[])
    }

    pub(crate) fn draw_markdown(&mut self, s: &mut Screen) -> Vec<Overlay> {
        let th = wincv_markdown::default_theme();
        s.clear(th.text, th.bg);
        let rows = (s.rows - 1).max(1);
        self.md_layout(s.cols);
        self.clamp_md(rows);
        let mut ovs = Vec::new();
        let top = self.md.top;
        for i in 0..rows {
            let idx = (top + i) as usize;
            let Some(ln) = self.md_lines().get(idx) else { break };
            if let Some(p) = &ln.pic {
                // 一張圖只在它的第一列排一次 overlay,而且要考慮圖被捲出畫面上緣的情況:
                // 那時 overlay 的矩形要往上超出,由光柵器自己裁掉。
                if ln.pic_row == 0 || idx as i32 == top {
                    if let Some(img) = &p.img {
                        let y = i - ln.pic_row;
                        ovs.push(Overlay {
                            img: img.clone(),
                            x: 0,
                            y: y * self.cell_h,
                            w: p.cols * self.cell_w,
                            h: p.rows * self.cell_h,
                            fit: true,
                            pan_x: 0,
                            pan_y: 0,
                            zoom: 0.0,
                        });
                    }
                }
                continue;
            }
            let mut x = 0;
            for pc in &ln.pieces {
                if x >= s.cols {
                    break;
                }
                let n = s.print(x, i, &pc.text, pc.fg, pc.bg);
                if pc.under {
                    s.underline(x, i, n, true);
                }
                x += n;
            }
        }
        self.rows = rows;
        self.draw_md_status(s);
        ovs
    }

    fn draw_md_status(&self, s: &mut Screen) {
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', Color::LT_GRAY2, Color::BLUE);
        let n = self.md_lines().len() as i32;
        let pct = if n > 0 { ((self.md.top + self.rows) * 100 / n).min(100) } else { 100 };
        // 名字過一次 t:一般檔名查不到目錄會原樣回來,而「使用說明」是內嵌文件的識別字,
        // 顯示時要翻。
        let left = tr!("%s  %d 列  %d%%", t(&self.md.name), n, pct);
        s.print(0, y, &left, Color::LT_GRAY2, Color::BLUE);
        let right = t("Enter 看圖  Esc 離開");
        let x = s.cols - cell_width(&right);
        if x > cell_width(&left) {
            s.print(x, y, &right, Color::GRAY, Color::BLUE);
        }
    }

    fn clamp_md(&mut self, rows: i32) {
        let max = (self.md_lines().len() as i32 - rows).max(0);
        self.md.top = self.md.top.min(max).max(0);
    }

    pub(crate) fn markdown_key(&mut self, k: Key) -> bool {
        let rows = self.rows.max(1);
        match k.code {
            Code::Up => self.md.top -= 1,
            Code::Down => self.md.top += 1,
            Code::PgUp => self.md.top -= rows,
            Code::PgDn => self.md.top += rows,
            Code::Home => self.md.top = 0,
            Code::End => self.md.top = self.md_lines().len() as i32,
            // 畫面上第一張圖用看圖模式全螢幕打開。
            Code::Enter => return self.open_first_visible_pic(),
            Code::Esc => {
                self.mode = Mode::Browser;
                self.md = MdView::default();
                return true;
            }
            _ => return false,
        }
        self.clamp_md(rows);
        true
    }

    /// 把目前畫面上第一張圖丟給看圖模式。
    fn open_first_visible_pic(&mut self) -> bool {
        let pic: Option<Arc<Pic>> = (0..self.rows)
            .map_while(|i| self.md_lines().get((self.md.top + i) as usize))
            .find_map(|ln| ln.pic.clone().filter(|p| p.img.is_some()));
        let Some(p) = pic else {
            self.message = t("這一頁沒有可以放大的圖");
            return true;
        };
        let name = p.src.rsplit('/').next().unwrap_or(&p.src).to_string();
        let img = (**p.img.as_ref().unwrap()).clone();
        self.image = Some(wincv_imgview::Model::from_image(&name, "", img, 0));
        self.mode = Mode::Image;
        self.md_return = true;
        true
    }
}

/// 千分位。
pub(crate) fn comma(v: i64) -> String {
    let s = v.to_string();
    let n = s.len();
    let mut out = String::new();
    for (i, c) in s.chars().enumerate() {
        if i > 0 && (n - i) % 3 == 0 {
            out.push(',');
        }
        out.push(c);
    }
    out
}
