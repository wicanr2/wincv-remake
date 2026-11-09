//! SVG:usvg 解析、tiny-skia 光柵化(對應 Go 版的 `svg.go`、`svgtext.go`、`svgdraw.go`)。
//!
//! Go 版的 oksvg 只畫路徑,`<text>` 是另外自己排版、用 vecfont 的外框補畫上去的。
//! usvg 本身會排版文字(text-anchor、tspan、變換、字距、複雜文字的 shaping),
//! 所以這裡不另做一套。兩邊的光柵器不同(反鋸齒的算法不一樣),本來就不可能逐像素
//! 相同,驗收改用瀏覽器(Chromium)當第三方真值,見 `tools/svg-parity.sh`。
//!
//! 字型來源與點陣那一路相同:先內嵌的後備字型,再系統的候選字型
//! (`wincv_ttf::candidates`)。usvg 預設只認 Windows 的字型名稱(Times New Roman
//! 等),Linux 上沒有的話整段字不畫,所以泛用字族與常見的字型名稱要自己對應到
//! 實際有的字型,做法與 fontconfig 的別名相同。

use std::sync::{Arc, OnceLock};

use resvg::tiny_skia;
use resvg::usvg::{self, fontdb};
use wincv_i18n::tr;

use crate::Image;

/// 光柵化時的最長邊上限。
///
/// SVG 沒有固有的像素大小,可以無限放大。畫進格點畫面只需要幾百個像素,而 viewBox
/// 寫著幾萬單位的檔案並不少見,不設上限的話會為了一張圖配置好幾 GB。
pub const SVG_MAX_SIDE: f32 = 2048.0;

/// 泛用字族的偏好順序,取第一個裝了的。拉丁字型排前面:CJK 的字由後備機制逐字補上
/// (usvg 依字型資料庫的載入順序找,內嵌的 CJK 字型在最前面)。
const SANS: &[&str] = &["DejaVu Sans", "Liberation Sans", "Noto Sans", "Arial", "Helvetica", "Noto Sans CJK TC", "Noto Sans CJK JP"];
const SERIF: &[&str] = &["DejaVu Serif", "Liberation Serif", "Noto Serif", "Times New Roman", "Noto Serif CJK TC", "Noto Serif CJK JP"];
const MONO: &[&str] = &["DejaVu Sans Mono", "Liberation Mono", "Noto Sans Mono", "Noto Mono", "Consolas", "Courier New", "Menlo"];

/// 常見的字型名稱對應到泛用字族(同 fontconfig 的別名)。名稱本身裝了就用本尊。
fn alias(name: &str) -> Option<fontdb::Family<'static>> {
    let n = name.to_ascii_lowercase();
    let sans = ["arial", "helvetica", "verdana", "tahoma", "segoe ui", "calibri", "trebuchet ms", "open sans", "roboto", "system-ui"];
    let serif = ["times", "times new roman", "georgia", "cambria", "garamond", "book antiqua", "palatino"];
    let mono = ["courier", "courier new", "consolas", "monaco", "menlo", "lucida console", "ui-monospace"];
    if sans.contains(&n.as_str()) {
        Some(fontdb::Family::SansSerif)
    } else if serif.contains(&n.as_str()) {
        Some(fontdb::Family::Serif)
    } else if mono.contains(&n.as_str()) {
        Some(fontdb::Family::Monospace)
    } else {
        None
    }
}

/// 字型資料庫。整個程式只建一次:載入的是字型的表頭,字型本體在桌面平台用 mmap。
fn fontdb() -> Arc<fontdb::Database> {
    static DB: OnceLock<Arc<fontdb::Database>> = OnceLock::new();
    DB.get_or_init(|| {
        let mut db = fontdb::Database::new();
        for b in wincv_bundled::fallbacks() {
            db.load_font_source(fontdb::Source::Binary(Arc::new(b)));
        }
        let mut seen = std::collections::HashSet::new();
        for p in wincv_ttf::candidates() {
            if seen.insert(p.clone()) {
                let _ = db.load_font_file(&p);
            }
        }
        let has = |db: &fontdb::Database, name: &str| db.faces().any(|f| f.families.iter().any(|(n, _)| n == name));
        let first = db.faces().next().and_then(|f| f.families.first()).map(|(n, _)| n.clone());
        let pick = |db: &fontdb::Database, list: &[&str]| list.iter().find(|n| has(db, n)).map(|s| s.to_string());
        if let Some(sans) = pick(&db, SANS).or(first) {
            let serif = pick(&db, SERIF).unwrap_or_else(|| sans.clone());
            let mono = pick(&db, MONO).unwrap_or_else(|| sans.clone());
            db.set_serif_family(serif);
            db.set_monospace_family(mono);
            db.set_cursive_family(sans.clone());
            db.set_fantasy_family(sans.clone());
            db.set_sans_serif_family(sans);
        }
        Arc::new(db)
    })
    .clone()
}

/// 選字型:照 CSS 的字族清單逐一問,沒裝的常見名稱換成對應的泛用字族,最後補 serif
/// (與 usvg 預設的選法相同,只多了別名)。
fn select_font() -> usvg::FontSelectionFn<'static> {
    Box::new(|font, db| {
        let mut names: Vec<fontdb::Family<'_>> = Vec::new();
        for f in font.families() {
            names.push(match f {
                usvg::FontFamily::Serif => fontdb::Family::Serif,
                usvg::FontFamily::SansSerif => fontdb::Family::SansSerif,
                usvg::FontFamily::Cursive => fontdb::Family::Cursive,
                usvg::FontFamily::Fantasy => fontdb::Family::Fantasy,
                usvg::FontFamily::Monospace => fontdb::Family::Monospace,
                usvg::FontFamily::Named(s) => {
                    let installed = db.faces().any(|x| x.families.iter().any(|(n, _)| n == s));
                    match alias(s) {
                        Some(g) if !installed => g,
                        _ => fontdb::Family::Name(s),
                    }
                }
            });
        }
        names.push(fontdb::Family::Serif);
        let stretch = match font.stretch() {
            usvg::FontStretch::UltraCondensed => fontdb::Stretch::UltraCondensed,
            usvg::FontStretch::ExtraCondensed => fontdb::Stretch::ExtraCondensed,
            usvg::FontStretch::Condensed => fontdb::Stretch::Condensed,
            usvg::FontStretch::SemiCondensed => fontdb::Stretch::SemiCondensed,
            usvg::FontStretch::Normal => fontdb::Stretch::Normal,
            usvg::FontStretch::SemiExpanded => fontdb::Stretch::SemiExpanded,
            usvg::FontStretch::Expanded => fontdb::Stretch::Expanded,
            usvg::FontStretch::ExtraExpanded => fontdb::Stretch::ExtraExpanded,
            usvg::FontStretch::UltraExpanded => fontdb::Stretch::UltraExpanded,
        };
        let style = match font.style() {
            usvg::FontStyle::Normal => fontdb::Style::Normal,
            usvg::FontStyle::Italic => fontdb::Style::Italic,
            usvg::FontStyle::Oblique => fontdb::Style::Oblique,
        };
        db.query(&fontdb::Query { families: &names, weight: fontdb::Weight(font.weight()), stretch, style })
    })
}

/// 把 SVG 光柵化成點陣圖。尺寸取自 width、height,沒有就取 viewBox;等比例縮到
/// [`SVG_MAX_SIDE`] 之內。
///
/// 三者都沒有時照 usvg 的做法取原點到內容右下角的範圍(連內容都沒有是 100×100)。
/// 那種檔案在瀏覽器裡也是靠 CSS 決定大小,沒有「正確答案」可言;Go 版一律給 256×256。
pub fn decode(data: &[u8]) -> Result<Image, String> {
    let opt = usvg::Options {
        fontdb: fontdb(),
        font_resolver: usvg::FontResolver { select_font: select_font(), ..Default::default() },
        ..Default::default()
    };
    let tree = usvg::Tree::from_data(data, &opt).map_err(|e| tr!("SVG 解析失敗: %w", e.to_string()))?;
    let size = tree.size();
    let (mut w, mut h) = (size.width(), size.height());
    if w > SVG_MAX_SIDE || h > SVG_MAX_SIDE {
        let k = (SVG_MAX_SIDE / w).min(SVG_MAX_SIDE / h);
        (w, h) = (w * k, h * k);
    }
    let (iw, ih) = (((w + 0.5) as u32).max(1), ((h + 0.5) as u32).max(1));
    let mut pm = tiny_skia::Pixmap::new(iw, ih).ok_or_else(|| tr!("SVG 解析失敗: %w", format!("{iw}x{ih}")))?;
    let t = tiny_skia::Transform::from_scale(iw as f32 / size.width(), ih as f32 / size.height());
    resvg::render(&tree, t, &mut pm.as_mut());
    // tiny-skia 是預乘的 RGBA,Image 不是。
    let pix = pm
        .pixels()
        .iter()
        .flat_map(|p| {
            let c = p.demultiply();
            [c.red(), c.green(), c.blue(), c.alpha()]
        })
        .collect();
    Ok(Image { w: iw, h: ih, pix })
}
