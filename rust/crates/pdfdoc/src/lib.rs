//! 從 PDF 取出文字與圖片、讀書籤、算頁面大小、畫整頁(對應 Go 版 `internal/pdfdoc`)。
//!
//! 做的是「取出」不是「還原版面」。PDF 描述的是「把這個字放在這個座標」,
//! 沒有段落、沒有欄、沒有閱讀順序,所以這裡把散落的字重新組回列、把列分回欄。
//!
//! Go 版的底層是自寫的 `internal/pdf`(物件層用 pdfcpu)。這裡不搬那一層:
//! 物件層、內容串流直譯、字型(含 ToUnicode)與光柵化都交給 hayro。
//! 排列(`layout`)逐行照 Go 版搬。
//!
//! 與 Go 版相同的已知限制:沒有嵌入字型、也不是那 14 個核心字型的 PDF,
//! 詞間空白不一定還原得出來。

mod images;
mod layout;
mod outline;
mod text;

use std::collections::{BTreeMap, BTreeSet, HashMap};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};

use hayro::hayro_interpret::font::FontQuery;
use hayro::hayro_interpret::util::TransformExt;
use hayro::hayro_interpret::{Context, InterpreterCache, InterpreterSettings, InterpreterWarning, RenderContent, interpret_page};
use hayro::hayro_syntax::Pdf;
use hayro::kurbo::{Affine, Rect};
use hayro::vello_cpu::color::palette::css::WHITE;
use hayro::{RenderCache, RenderSettings};
use wincv_i18n::tr;

pub use layout::{SPACE_RATIO, layout};
pub use outline::{MAX_BOOKMARKS, text_string};
pub use text::MAX_TEXTS;

/// 單張圖的上限(位元組)。
pub const MAX_IMAGE_BYTES: usize = 32 << 20;

/// 預設的解析度:螢幕的慣例值,一頁 A4 大約 794×1123。
pub const DEFAULT_DPI: f64 = 96.0;
/// 預設的像素上限(約 4000×4000)。
pub const DEFAULT_MAX_PIXELS: usize = 16 << 20;

/// 頁面上的一個字(對應 Go 版 `pdf.Text`)。
#[derive(Clone, Debug, PartialEq)]
pub struct Text {
    /// 字的原點,PDF 使用者空間(由左下往右上長)。
    pub x: f64,
    pub y: f64,
    /// 這個字佔多寬(前進量在橫軸上的投影)。
    pub w: f64,
    /// 換算過各層變換之後的實際字級。
    pub size: f64,
    pub s: String,
}

/// 一頁上的一列文字。
#[derive(Clone, Debug, PartialEq)]
pub struct Line {
    /// 列在頁面上的縱座標(PDF 座標,由下往上長)。
    pub y: f64,
    /// 這一列從左邊算起空了多少個字寬。
    pub indent: i32,
    pub text: String,
}

/// 書籤裡的一筆。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Bookmark {
    pub title: String,
    /// 0 表示指不到頁碼。
    pub page: usize,
    pub level: i32,
}

/// 畫一頁的設定。
#[derive(Clone, Copy, Debug, Default)]
pub struct RenderOptions {
    /// 解析度。0 表示用 [`DEFAULT_DPI`]。
    pub dpi: f64,
    /// 輸出圖的像素數上限,0 表示用 [`DEFAULT_MAX_PIXELS`]。超過的話自動降解析度。
    pub max_pixels: usize,
}

/// 畫好的一頁。
#[derive(Clone, Debug)]
pub struct Rendered {
    /// 非預乘的 RGBA,白底。
    pub img: wincv_render::Image,
    /// 實際用的解析度(可能因為像素上限而低於要求的值)。
    pub dpi: f64,
    /// 沒有嵌入、改用替代字型畫的字型。畫面看起來正常,但字形不是原檔那一套。
    pub substituted: Vec<String>,
    /// 載入失敗、那些字沒有畫上去的字型。
    pub missing: Vec<String>,
}

/// 一頁的圖:名稱(含副檔名)→ 內容。
type PageImages = Arc<BTreeMap<String, Vec<u8>>>;

/// 一份打開著的 PDF。
pub struct Doc {
    pub path: PathBuf,
    pub pages: usize,
    pdf: Pdf,
    imgs: Mutex<HashMap<usize, PageImages>>,
}

impl std::fmt::Debug for Doc {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.debug_struct("Doc").field("path", &self.path).field("pages", &self.pages).finish()
    }
}

/// 打開一份 PDF。
pub fn open(path: impl AsRef<Path>) -> Result<Doc, String> {
    let path = path.as_ref();
    let data = std::fs::read(path).map_err(|e| tr!("打不開:%w", e.to_string()))?;
    from_bytes(path, data)
}

/// 從記憶體裡的內容打開。`path` 只用來記名字。
pub fn from_bytes(path: impl AsRef<Path>, data: Vec<u8>) -> Result<Doc, String> {
    // 空密碼:大部分「加密」的 PDF 只是設了權限,使用者密碼是空的。
    let pdf = guarded(|| Pdf::new(data)).ok_or_else(|| tr!("這份 PDF 解不開:%w", "panic"))?.map_err(|e| {
        // 細節沿用 hayro 的錯誤名稱(Decryption(PasswordProtected)、Invalid),不另立譯文。
        tr!("這份 PDF 解不開:%w", format!("{e:?}"))
    })?;
    let pages = pdf.pages().len();
    if pages < 1 {
        return Err(tr!("這份 PDF 沒有頁面"));
    }
    Ok(Doc { path: path.as_ref().to_path_buf(), pages, pdf, imgs: Mutex::new(HashMap::new()) })
}

/// hayro 對損壞的檔案偶爾會 panic。開發與測試的建置在這裡接住;
/// 發行版是 `panic = "abort"`,接不住,只能靠 hayro 自己的防護。
fn guarded<T>(f: impl FnOnce() -> T) -> Option<T> {
    std::panic::catch_unwind(std::panic::AssertUnwindSafe(f)).ok()
}

impl Doc {
    fn check(&self, page: usize) -> Result<(), String> {
        if page < 1 || page > self.pages {
            return Err(tr!("沒有第 %d 頁", page as i64));
        }
        Ok(())
    }

    /// 這份 PDF 的物件層,給需要更多細節的呼叫端。
    pub fn pdf(&self) -> &Pdf {
        &self.pdf
    }

    /// 一頁上所有的字,依內容串流的順序。頁碼從 1 起算。
    pub fn texts(&self, page: usize) -> Result<Vec<Text>, String> {
        self.check(page)?;
        let p = &self.pdf.pages()[page - 1];
        let settings = InterpreterSettings { render_content: RenderContent::PAGE, ..Default::default() };
        let cache = InterpreterCache::new();
        let mb = p.media_box();
        let mut ctx = Context::new(Affine::IDENTITY, Rect::new(mb.x0, mb.y0, mb.x1, mb.y1), &cache, self.pdf.xref(), settings);
        let mut dev = text::TextDevice::default();
        guarded(|| interpret_page(p, &mut ctx, &mut dev)).ok_or_else(|| tr!("第 %d 頁讀不出來:%w", page as i64, "panic"))?;
        Ok(dev.out)
    }

    /// 取一頁的文字,依閱讀順序排好。頁碼從 1 起算。
    pub fn text(&self, page: usize) -> Result<Vec<Line>, String> {
        let ts = self.texts(page)?;
        let (x0, _, x1, _) = self.media_box(page);
        Ok(layout(&ts, x0, x1))
    }

    /// MediaBox 的四個邊。沒有或不合理的 MediaBox 照 A4 算,與 Go 版相同。
    fn media_box(&self, page: usize) -> (f64, f64, f64, f64) {
        let mb = self.pdf.pages()[page - 1].media_box();
        if mb.x1 <= mb.x0 || mb.y1 <= mb.y0 { (0.0, 0.0, 595.0, 842.0) } else { (mb.x0, mb.y0, mb.x1, mb.y1) }
    }

    /// 一頁的尺寸,單位是 point(1/72 吋)。與 Go 版相同取 MediaBox、不轉向。
    ///
    /// 給「這一頁放大到 N 倍會有多少像素」用,那個判斷要在真的配置記憶體
    /// 之前做完。hayro 畫的是 CropBox(與 MediaBox 的交集),不會比這個大。
    pub fn page_size(&self, page: usize) -> Result<(f64, f64), String> {
        self.check(page)?;
        let (x0, y0, x1, y1) = self.media_box(page);
        Ok((x1 - x0, y1 - y0))
    }

    /// 讀 PDF 自己帶的目錄。沒有書籤的檔案回空的。
    pub fn outline(&self) -> Vec<Bookmark> {
        guarded(|| outline::outline(&self.pdf)).unwrap_or_default()
    }

    /// 把一頁畫成點陣圖。
    pub fn render(&self, page: usize, dpi: f64) -> Result<Rendered, String> {
        self.render_with(page, RenderOptions { dpi, max_pixels: 0 })
    }

    /// 依設定把一頁畫成點陣圖。
    pub fn render_with(&self, page: usize, opt: RenderOptions) -> Result<Rendered, String> {
        self.check(page)?;
        let p = &self.pdf.pages()[page - 1];
        let mut dpi = if opt.dpi > 0.0 { opt.dpi } else { DEFAULT_DPI };
        let max_px = if opt.max_pixels > 0 { opt.max_pixels } else { DEFAULT_MAX_PIXELS };
        let (pw, ph) = p.render_dimensions();
        let (pw, ph) = (pw as f64, ph as f64);
        if pw <= 0.0 || ph <= 0.0 {
            return Err(tr!("這一頁沒有尺寸"));
        }
        let mut s = dpi / 72.0;
        let mut capped = false;
        if pw * ph * s * s > max_px as f64 {
            s = (max_px as f64 / (pw * ph)).sqrt();
            capped = true;
        }
        // 畫布的邊長是 u16。
        let longest = pw.max(ph);
        if longest * s > u16::MAX as f64 {
            s = (u16::MAX as f64 - 1.0) / longest;
            capped = true;
        }
        if capped {
            dpi = s * 72.0;
        }
        // 沒有壓上限時往上取整,免得最右邊那一列內容被切掉;壓上限時往下取整,
        // 不然兩個維度各進位一格就會超過上限。與 Go 版相同。
        let round = if capped { f64::floor } else { f64::ceil };
        let (w, h) = (round(pw * s), round(ph * s));
        if w < 1.0 || h < 1.0 {
            return Err(tr!("這一頁太小"));
        }
        let (w, h) = (w as u16, h as u16);

        let subst = Arc::new(Mutex::new(BTreeSet::new()));
        let missing = Arc::new(Mutex::new(BTreeSet::new()));
        let mut settings = InterpreterSettings::default();
        let base = settings.font_resolver.clone();
        let sub2 = subst.clone();
        settings.font_resolver = Arc::new(move |q: &FontQuery| {
            let name = match q {
                FontQuery::Standard(s) => s.postscript_name().to_string(),
                FontQuery::Fallback(f) => f
                    .post_script_name
                    .clone()
                    .or_else(|| f.font_name.clone())
                    .unwrap_or_else(|| wincv_i18n::t("(未具名字型)")),
            };
            sub2.lock().unwrap_or_else(|e| e.into_inner()).insert(name);
            base(q)
        });
        let miss2 = missing.clone();
        settings.warning_sink = Arc::new(move |w| {
            if let InterpreterWarning::UnsupportedFont = w {
                miss2.lock().unwrap_or_else(|e| e.into_inner()).insert(wincv_i18n::t("(未具名字型)"));
            }
        });

        let pix = guarded(|| {
            let cache = RenderCache::new();
            let mut ctx = hayro::vello_cpu::RenderContext::new(w, h);
            let transform = Affine::scale(s) * p.initial_transform(true).to_kurbo();
            hayro::render_into(p, &cache, &settings, &RenderSettings::default(), &mut ctx, transform);
            ctx.flush();
            let mut pixmap = hayro::vello_cpu::Pixmap::new(w, h);
            ctx.render_with(
                &mut pixmap,
                &mut hayro::vello_cpu::Resources::default(),
                hayro::vello_cpu::RasterizerSettings {
                    target_init: hayro::vello_cpu::TargetInit::Clear(WHITE),
                    ..Default::default()
                },
            );
            pixmap
        })
        .ok_or_else(|| tr!("這一頁畫不出來(%v)", "panic"))?;
        // 底色是不透明的白,每個像素的 alpha 都是 255,預乘與非預乘相同。
        let img = wincv_render::Image { w: w as u32, h: h as u32, pix: pix.data_as_u8_slice().to_vec() };
        let take = |m: &Arc<Mutex<BTreeSet<String>>>| m.lock().unwrap_or_else(|e| e.into_inner()).iter().cloned().collect();
        Ok(Rendered { img, dpi, substituted: take(&subst), missing: take(&missing) })
    }

    /// 一頁上有哪些圖,依名稱排序。
    pub fn image_names(&self, page: usize) -> Result<Vec<String>, String> {
        Ok(self.page_images(page)?.keys().cloned().collect())
    }

    /// 取一頁上的一張圖。回傳的是可以直接解碼的 png 或 jpg。
    pub fn image(&self, page: usize, name: &str) -> Result<Vec<u8>, String> {
        self.page_images(page)?.get(name).cloned().ok_or_else(|| tr!("第 %d 頁沒有 %s", page as i64, name))
    }

    /// 抽一頁的圖,結果留著。按頁抽:一份幾百頁的技術文件圖片加起來可以是
    /// 幾百 MB,而使用者一次只看一頁。
    fn page_images(&self, page: usize) -> Result<PageImages, String> {
        self.check(page)?;
        let mut cache = self.imgs.lock().unwrap_or_else(|e| e.into_inner());
        if let Some(m) = cache.get(&page) {
            return Ok(m.clone());
        }
        let p = &self.pdf.pages()[page - 1];
        let m = guarded(|| images::page_images(p, self.pdf.xref()))
            .ok_or_else(|| tr!("第 %d 頁的圖抽不出來(%v)", page as i64, "panic"))?;
        let m = Arc::new(m);
        cache.insert(page, m.clone());
        Ok(m)
    }

    /// 關閉。資源在 drop 時釋放,這裡只為了與 Go 版的介面對應。
    pub fn close(self) {}
}
