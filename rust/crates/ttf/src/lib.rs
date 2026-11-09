//! 把 TrueType / OpenType 字型光柵化成點陣字模(對應 Go 版 `internal/ttf`)。
//!
//! 倚天字庫是用 Big5 索引的,Big5 沒有的字就沒有字模:简体字、韓文、希臘文、
//! 大部分符號都畫不出來,而且是安靜地空白。這裡補一條後備:系統上的字型渲染成與
//! 點陣字同尺寸的字模。預設以覆蓋率 0x80 為門檻切成 1-bit(整個 UI 是點陣風格);
//! `aa` 開著時另外帶覆蓋率。
//!
//! 光柵化的每一步都照 Go 版做:外框座標的取整(TrueType 隱含中點在縮放前以整數
//! 計算、縮放時四捨五入到 26.6 定點)、曲線切分、22.9 定點數的覆蓋率累加
//! (見 [`gofix`])。所以同一份字型在兩邊畫出來的字模逐位元相同。

mod glyf;
pub mod gofix;
mod paths;

pub use paths::{candidates, font_dirs, missing_hint};

use std::collections::HashMap;
use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, Mutex};

use wincv_cell::{is_wide, CP437};
use wincv_fnt::Glyph;
use wincv_i18n::tr;
use wincv_render::{CjkSource, HalfSource};

/// 一個後備字型,依 `is_wide` 決定每個字佔一格或兩格。
pub struct Font {
    data: Arc<[u8]>,
    /// 字級(26.6 定點的 ppem)。
    ppem: i32,
    half_w: i32,
    full_w: i32,
    h: i32,
    ascent: i32,
    /// 開著時字模帶覆蓋率(`Glyph::alpha`)。要在第一次取字之前設好:字模有快取。
    aa: AtomicBool,
    cache: Mutex<HashMap<char, Option<Glyph>>>,
}

/// Go 的 sfnt `scale`:四捨五入(遠離零)後除以 units per em。
fn scale(x: i64, upem: i64) -> i32 {
    let x = if x >= 0 { x + upem / 2 } else { x - upem / 2 };
    (x / upem) as i32
}

fn parse(data: &[u8]) -> Result<ttf_parser::Face<'_>, String> {
    ttf_parser::Face::parse(data, 0).map_err(|e| tr!("認不得的字型: %w", e.to_string()))
}

/// 外框的一段,座標是 26.6 定點、Y 軸向下(與 Go 版的 sfnt 相同)。
///
/// `Z` 是一個輪廓的結尾,意思是「回到這個輪廓的起點」。光柵器把它當成一條直線;
/// 要列出線段的地方(`wincv-vecfont`)只在筆不在起點時補那一條線,與 Go 版 sfnt
/// 產生的線段相同。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Seg {
    M(i32, i32),
    L(i32, i32),
    Q(i32, i32, i32, i32),
    C(i32, i32, i32, i32, i32, i32),
    Z,
}

/// 一個字形的外框,字級 `ppem`(26.6 定點)。座標的量化照 Go 版 sfnt 的 `LoadGlyph`。
///
/// TrueType 外框走 [`glyf`] 的移植(複合字形的中點與位移順序要跟 Go 版一樣),
/// CFF 外框走 ttf-parser。字形資料有問題時回空的外框,與 Go 版回錯誤時一樣畫不出東西。
pub fn outline(face: &ttf_parser::Face<'_>, gid: ttf_parser::GlyphId, ppem: i32) -> Vec<Seg> {
    let upem = face.units_per_em() as i64;
    if let Some(g) = glyf::Glyf::new(face) {
        // Go 版:x 乘 ppem 再縮放,y 另外翻轉。乘法在 int32 裡做(Go 的 fixed.Int26_6)。
        let sc = |p: (i32, i32)| {
            (scale(p.0.wrapping_mul(ppem) as i64, upem), -scale(p.1.wrapping_mul(ppem) as i64, upem))
        };
        return g
            .load(gid)
            .unwrap_or_default()
            .into_iter()
            .map(|s| {
                let (a, b) = (sc(s.p[0]), sc(s.p[1]));
                match s.op {
                    b'm' => Seg::M(a.0, a.1),
                    b'l' => Seg::L(a.0, a.1),
                    _ => Seg::Q(a.0, a.1, b.0, b.1),
                }
            })
            .collect();
    }
    let mut c = Collect { upem, ppem: ppem as i64, segs: Vec::new() };
    face.outline_glyph(gid, &mut c);
    c.segs
}

/// 前進寬度(26.6 定點,不做 hinting),照 Go 版 sfnt 的 `GlyphAdvance`。
pub fn advance(face: &ttf_parser::Face<'_>, gid: ttf_parser::GlyphId, ppem: i32) -> Option<i32> {
    let units = face.glyph_hor_advance(gid)?;
    Some(scale(units as i64 * ppem as i64, face.units_per_em() as i64))
}

/// 把 ttf-parser 的 CFF 外框換成 Go 版 sfnt 會得到的座標:先向零截斷成整數字型單位,
/// 再照 Go 的 `scale` 縮放。TrueType 外框不走這裡(見 [`glyf`])。
struct Collect {
    upem: i64,
    ppem: i64,
    segs: Vec<Seg>,
}

impl Collect {
    fn p(&self, x: f32, y: f32) -> (i32, i32) {
        let (xi, yi) = (x.trunc() as i64, y.trunc() as i64);
        (scale(xi * self.ppem, self.upem), -scale(yi * self.ppem, self.upem))
    }
}

impl ttf_parser::OutlineBuilder for Collect {
    fn move_to(&mut self, x: f32, y: f32) {
        let (a, b) = self.p(x, y);
        self.segs.push(Seg::M(a, b));
    }
    fn line_to(&mut self, x: f32, y: f32) {
        let (a, b) = self.p(x, y);
        self.segs.push(Seg::L(a, b));
    }
    fn quad_to(&mut self, x1: f32, y1: f32, x: f32, y: f32) {
        let (a, b) = self.p(x1, y1);
        let (c, d) = self.p(x, y);
        self.segs.push(Seg::Q(a, b, c, d));
    }
    fn curve_to(&mut self, x1: f32, y1: f32, x2: f32, y2: f32, x: f32, y: f32) {
        let (a, b) = self.p(x1, y1);
        let (c, d) = self.p(x2, y2);
        let (e, f) = self.p(x, y);
        self.segs.push(Seg::C(a, b, c, d, e, f));
    }
    fn close(&mut self) {
        self.segs.push(Seg::Z);
    }
}

impl Font {
    /// 建一份字型。`fit_half` 為真時把字級縮到「半形字塞得進 half_w」。
    ///
    /// 字級用**格高**當 em(DPI 72),一個字佔多寬是字型自己決定的。DejaVu Sans Mono
    /// 在 15 px em 下一格是 9 px,塞進 8 px 的格子右邊就被切掉 1 px:m 少一豎會看成 n。
    /// 只有拿來當半形來源時才縮;當全形來源時格子有兩倍寬,本來就塞得下。
    fn build(data: Arc<[u8]>, half_w: i32, full_w: i32, h: i32, fit_half: bool) -> Result<Font, String> {
        let face = parse(&data)?;
        let upem = face.units_per_em() as i64;
        let mut size = h as f64;
        let mut ppem = (0.5 + size * 64.0) as i32;
        if fit_half {
            // 量 M 的前進寬度(HintingFull:進位到整數像素)。三次就收斂。
            for _ in 0..3 {
                let Some(units) = face.glyph_index('M').filter(|g| g.0 != 0).and_then(|g| face.glyph_hor_advance(g)) else {
                    break;
                };
                let adv = (scale(units as i64 * ppem as i64, upem) + 32) & !63;
                let ceil = (adv + 63) >> 6;
                if ceil <= half_w || ceil <= 0 {
                    break;
                }
                size = size * half_w as f64 / ceil as f64;
                ppem = (0.5 + size * 64.0) as i32;
            }
        }
        // 基線不能用字型的 ascent:TrueType 的 ascent 含行距,CJK 字型尤其誇張。這裡用
        // CJK 慣用的比例,字身頂端對齊格頂,基線在 0.88 em 處;縮過字級的話垂直置中。
        let ascent = (((h as f64 - size) as i32) / 2 + (size * 0.88 + 0.5) as i32).min(h);
        drop(face);
        Ok(Font {
            data,
            ppem,
            half_w,
            full_w,
            h,
            ascent,
            aa: AtomicBool::new(false),
            cache: Mutex::new(HashMap::new()),
        })
    }

    /// 讀入字型並建立指定尺寸的字模來源。
    pub fn load(path: &Path, half_w: i32, full_w: i32, h: i32) -> Result<Font, String> {
        let data = std::fs::read(path).map_err(|e| format!("{}: {e}", path.display()))?;
        Font::build(data.into(), half_w, full_w, h, false).map_err(|e| format!("{}: {e}", path.display()))
    }

    /// 從記憶體裡的一份字型建立(打包進執行檔的字型用)。
    pub fn load_bytes(data: Arc<[u8]>, half_w: i32, full_w: i32, h: i32) -> Result<Font, String> {
        Font::build(data, half_w, full_w, h, false)
    }

    /// 反鋸齒開關。
    pub fn set_aa(&self, on: bool) {
        self.aa.store(on, Ordering::Relaxed);
    }

    /// 取一個字的字模。字型裡沒有就回 None。
    pub fn glyph(&self, r: char) -> Option<Glyph> {
        let mut cache = self.cache.lock().unwrap_or_else(|e| e.into_inner());
        cache.entry(r).or_insert_with(|| self.render(r)).clone()
    }

    fn render(&self, r: char) -> Option<Glyph> {
        let face = parse(&self.data).ok()?;
        let gid = face.glyph_index(r).filter(|g| g.0 != 0)?;
        let w = if is_wide(r) { self.full_w } else { self.half_w };
        let segs = outline(&face, gid, self.ppem);

        // Go 版 opentype.Face.Glyph:以 (0, ascent) 為原點,外框的包圍盒(含控制點)
        // 取整成像素矩形,再把座標平移進光柵器。
        let dot_y = self.ascent << 6;
        let (mut x0, mut y0, mut x1, mut y1) = (i32::MAX, i32::MAX, i32::MIN, i32::MIN);
        let mut grow = |x: i32, y: i32| {
            x0 = x0.min(x);
            x1 = x1.max(x);
            y0 = y0.min(y);
            y1 = y1.max(y);
        };
        for s in &segs {
            match *s {
                Seg::Z => {}
                Seg::M(a, b) | Seg::L(a, b) => grow(a, b),
                Seg::Q(a, b, cc, d) => {
                    grow(a, b);
                    grow(cc, d);
                }
                Seg::C(a, b, cc, d, e, f) => {
                    grow(a, b);
                    grow(cc, d);
                    grow(e, f);
                }
            }
        }
        if segs.is_empty() {
            (x0, y0, x1, y1) = (0, 0, 0, 0);
        }
        let (dr_x0, dr_y0) = (x0 >> 6, (y0 + dot_y) >> 6);
        let (dr_x1, dr_y1) = ((x1 + 63) >> 6, (y1 + dot_y + 63) >> 6);
        let (mw, mh) = (dr_x1 - dr_x0, dr_y1 - dr_y0);
        if mw < 0 || mh < 0 {
            return None;
        }
        let (bias_x, bias_y) = (-(dr_x0 << 6), dot_y - (dr_y0 << 6));
        let f = |v: i32, b: i32| (v + b) as f32 / 64.0;
        let mut ras = gofix::Raster::new(mw, mh);
        for s in &segs {
            match *s {
                // 回到起點。長度為零的線段對覆蓋率沒有影響,所以不必判斷是否已經在起點。
                Seg::Z => ras.close_path(),
                Seg::M(a, b) => ras.move_to(f(a, bias_x), f(b, bias_y)),
                Seg::L(a, b) => ras.line_to(f(a, bias_x), f(b, bias_y)),
                Seg::Q(a, b, cc, d) => ras.quad_to(f(a, bias_x), f(b, bias_y), f(cc, bias_x), f(d, bias_y)),
                Seg::C(a, b, cc, d, e, g) => {
                    ras.cube_to(f(a, bias_x), f(b, bias_y), f(cc, bias_x), f(d, bias_y), f(e, bias_x), f(g, bias_y))
                }
            }
        }
        let mask = ras.accumulate();

        let aa = self.aa.load(Ordering::Relaxed);
        let mut g = Glyph { w, h: self.h, bits: vec![false; (w * self.h) as usize], alpha: aa.then(|| vec![0; (w * self.h) as usize]) };
        let mut ink = false;
        for y in 0..self.h {
            let sy = y - dr_y0;
            if sy < 0 || sy >= mh {
                continue;
            }
            for x in 0..w {
                let sx = x - dr_x0;
                if sx < 0 || sx >= mw {
                    continue;
                }
                let a = mask[(sy * mw + sx) as usize];
                let i = (y * w + x) as usize;
                if let Some(al) = g.alpha.as_mut() {
                    al[i] = a;
                }
                // 一半以上不透明才算有筆畫。門檻放低會讓細筆畫糊掉,放高會讓整個字變瘦。
                if a >= 0x80 {
                    g.bits[i] = true;
                }
                ink |= a > 0;
            }
        }
        ink.then_some(g)
    }
}

impl CjkSource for Font {
    fn glyph(&self, r: char) -> Option<Glyph> {
        Font::glyph(self, r)
    }
}

/// 依序試多個字型,誰有這個字就用誰。一個字型很難涵蓋全部:Noto CJK 有漢字與
/// 韓文但缺帶聲調的希臘字母,DejaVu 剛好相反。
#[derive(Clone, Default)]
pub struct Chain(pub Vec<Arc<Font>>);

impl Chain {
    /// 整條鏈的反鋸齒。
    pub fn set_aa(&self, on: bool) {
        self.0.iter().for_each(|f| f.set_aa(on));
    }
}

impl CjkSource for Chain {
    fn glyph(&self, r: char) -> Option<Glyph> {
        self.0.iter().find_map(|f| f.glyph(r))
    }
}

/// 載入的結果:用到的字型、它們的路徑、「檔案在、但載不起來」的原因。
///
/// **不要把第三項丟掉**:字型在那裡卻用不了,和根本沒裝是完全不同的兩件事,而兩者
/// 的症狀都只是「中文變成空框」。
pub type Loaded<T> = (T, Vec<String>, Vec<String>);

/// 依序試候選字型(寫死清單加目錄掃描,去重),`want` 決定要不要試這一個,
/// `take` 決定試到第幾個就停(0 表示全部)。
fn try_candidates(half_w: i32, full_w: i32, h: i32, want: impl Fn(&str) -> bool, take: usize) -> Loaded<Vec<Arc<Font>>> {
    let (mut fonts, mut used, mut errs) = (Vec::new(), Vec::new(), Vec::new());
    let mut seen = std::collections::HashSet::new();
    for p in candidates() {
        if !seen.insert(p.clone()) || !want(&p) || !Path::new(&p).exists() {
            continue; // 沒裝,不算錯
        }
        match Font::load(Path::new(&p), half_w, full_w, h) {
            Ok(f) => {
                fonts.push(Arc::new(f));
                used.push(p);
                if take > 0 && fonts.len() >= take {
                    break;
                }
            }
            Err(e) => errs.push(e),
        }
    }
    (fonts, used, errs)
}

/// 把所有讀得起來的候選字型串成一條。
pub fn load_chain(half_w: i32, full_w: i32, h: i32) -> Loaded<Chain> {
    let (fonts, used, errs) = try_candidates(half_w, full_w, h, |_| true, 0);
    (Chain(fonts), used, errs)
}

/// 依序試候選字型,回第一個成功的。
pub fn find_and_load(half_w: i32, full_w: i32, h: i32) -> Loaded<Option<Arc<Font>>> {
    let (mut fonts, used, errs) = try_candidates(half_w, full_w, h, |_| true, 1);
    (fonts.pop(), used, errs)
}

/// 等寬字型的檔名特徵。
fn looks_mono(p: &str) -> bool {
    let base = Path::new(p).file_name().map(|b| b.to_string_lossy().to_lowercase()).unwrap_or_default();
    base.replace('-', "").contains("mono") || ["consola", "menlo", "courier", "cutive"].iter().any(|k| base.contains(k))
}

/// 先在等寬字型裡找,找不到才退回一般的候選。
///
/// 半形字模一格只有 8 像素寬。比例字型的 m 與 i 差三倍寬,硬塞進同一格會把 m 擠成
/// 一團、i 旁邊空一大片。等寬字型本來就是照固定寬度設計的。
pub fn find_mono(half_w: i32, full_w: i32, h: i32) -> Loaded<Option<Arc<Font>>> {
    let (mut fonts, used, mut errs) = try_candidates(half_w, full_w, h, looks_mono, 1);
    if let Some(f) = fonts.pop() {
        return (Some(f), used, errs);
    }
    let (f, used, more) = find_and_load(half_w, full_w, h);
    errs.extend(more);
    (f, used, errs)
}

/// 用一份 TrueType 現場產出半形字模,實作 [`HalfSource`]。
///
/// 原版的 cvga.fon 是第三方檔案,公開產物不能帶;沒有半形字模的話整個畫面一個字都
/// 畫不出來,所以拿系統字型頂上。字碼走 CP437(與原版的 .FON 相同),上層不知道換過來源。
pub struct HalfFont {
    f: Arc<Font>,
    w: i32,
    h: i32,
}

impl HalfFont {
    /// 包一份已經載好的字型。字型的 half_w 必須等於 w:寬度不一致的話格點會歪掉,
    /// 而症狀是「字距怪怪的」,不像設定錯誤。
    pub fn new(f: Arc<Font>, w: i32, h: i32) -> HalfFont {
        HalfFont { f, w, h }
    }

    /// 讀一份字型當半形來源,字級縮到一個字塞得進 w 像素寬。
    pub fn load_fit(path: &Path, w: i32, h: i32) -> Result<HalfFont, String> {
        let data = std::fs::read(path).map_err(|e| format!("{}: {e}", path.display()))?;
        HalfFont::load_fit_bytes(data.into(), w, h).map_err(|e| format!("{}: {e}", path.display()))
    }

    pub fn load_fit_bytes(data: Arc<[u8]>, w: i32, h: i32) -> Result<HalfFont, String> {
        Ok(HalfFont::new(Arc::new(Font::build(data, w, w * 2, h, true)?), w, h))
    }

    /// 從候選字型裡挑一份能用的(不縮字級)。
    pub fn load_any(w: i32, h: i32) -> Loaded<Option<HalfFont>> {
        let (f, used, errs) = find_and_load(w, w * 2, h);
        (f.map(|f| HalfFont::new(f, w, h)), used, errs)
    }

    /// 找一份等寬字型當半形來源,字級縮到塞得下。
    pub fn find_mono(w: i32, h: i32) -> Loaded<Option<HalfFont>> {
        let (f, used, mut errs) = find_mono(w, w * 2, h);
        let Some(path) = f.and(used.last().cloned()) else { return (None, used, errs) };
        match HalfFont::load_fit(Path::new(&path), w, h) {
            Ok(hf) => (Some(hf), used, errs),
            Err(e) => {
                errs.push(e);
                (None, used, errs)
            }
        }
    }
}

impl HalfSource for HalfFont {
    fn glyph(&self, code: u8) -> Option<Glyph> {
        let r = CP437[code as usize];
        if r == '\0' {
            return None;
        }
        self.f.glyph(r)
    }
    fn size(&self) -> (i32, i32) {
        (self.w, self.h)
    }
}

#[cfg(test)]
mod tests;
