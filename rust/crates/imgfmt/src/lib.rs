//! 圖檔解碼(對應 Go 版 `internal/imgfmt`)。
//!
//! JPEG、PNG、GIF、BMP、TIFF、WebP 交給 `image` crate(依 magic number 認格式);
//! PCX、TGA、PNM、ICO、Koala 是從 Go 版搬過來的自寫解碼器。格式支援狀態見
//! [`FORMATS`],它是進度的唯一真相。

mod ico;
mod koa;
mod pcx;
mod pnm;
mod tga;
mod tiff_fallback;

pub use wincv_render::Image;

use wincv_i18n::tr;

/// 一種圖檔格式的支援狀態。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Format {
    pub ext: &'static [&'static str],
    pub name: &'static str,
    pub supported: bool,
    /// 實作方式;沒支援時寫原因。存中文原文,顯示時才過語系目錄。
    pub note: &'static str,
}

/// 原版支援的全部格式與 Rust 版目前的狀態。副檔名與名稱和 Go 版的
/// `imgfmt.Formats` 逐列相同(`tests::formats_match_go` 盯著)。
pub const FORMATS: &[Format] = &[
    Format { ext: &[".jpg", ".jpeg"], name: "JPEG", supported: true, note: "image crate(zune-jpeg)" },
    Format { ext: &[".png"], name: "PNG", supported: true, note: "image crate" },
    Format { ext: &[".gif"], name: "GIF", supported: true, note: "image crate" },
    Format { ext: &[".bmp", ".dib", ".rle"], name: "BMP", supported: true, note: "image crate" },
    Format { ext: &[".tif", ".tiff"], name: "TIFF", supported: true, note: "image crate" },
    Format { ext: &[".webp"], name: "WebP", supported: true, note: "image crate(原版沒有,順手支援)" },
    Format { ext: &[".pcx"], name: "PCX", supported: true, note: "自寫" },
    Format { ext: &[".tga"], name: "TGA", supported: true, note: "自寫" },
    Format { ext: &[".pbm", ".pgm", ".ppm", ".pnm"], name: "PNM", supported: true, note: "自寫" },
    Format { ext: &[".ico"], name: "ICO", supported: true, note: "自寫(挑最大的那張)" },
    Format { ext: &[".koa"], name: "Koala", supported: true, note: "自寫,C64 多色點陣" },
    // SVG 的 <text> 要用向量字型的後備鏈(ttf / vecfont),那幾個模組還沒搬。
    Format { ext: &[".svg"], name: "SVG", supported: false, note: "等字型模組搬到 Rust 版" },
    // Photo CD 是 Huffman 編碼的 YCC,還帶多組解析度,而且找不到可以產生測試檔的
    // 工具。沒有 oracle 的解碼器不寫。
    Format { ext: &[".pcd"], name: "PhotoCD", supported: false, note: "Kodak Photo CD,造不出測試資料" },
];

/// Go 的 path.Ext:最後一段裡最後一個點之後(含點)。
fn ext_of(name: &str) -> String {
    let base = name.rsplit(['/', std::path::MAIN_SEPARATOR]).next().unwrap_or(name);
    base.rfind('.').map_or(String::new(), |i| base[i..].to_lowercase())
}

/// 依副檔名判斷。
pub fn detect_format(name: &str) -> Option<&'static Format> {
    let ext = ext_of(name);
    FORMATS.iter().find(|f| f.ext.contains(&ext.as_str()))
}

/// 這個檔名看起來是不是圖檔(不論支不支援)。
pub fn is_image(name: &str) -> bool {
    detect_format(name).is_some()
}

/// 解一張圖,回傳圖與格式名。先看副檔名選自寫的解碼器,其餘交給 `image` crate
/// (依 magic number 認格式,副檔名取錯也解得開)。
pub fn decode(name: &str, data: &[u8]) -> Result<(Image, &'static str), String> {
    let f = detect_format(name);
    if let Some(f) = f.filter(|f| !f.supported) {
        let base = name.rsplit(['/', std::path::MAIN_SEPARATOR]).next().unwrap_or(name);
        return Err(tr!("%s: %s 格式還沒實作(%s)", base, f.name, wincv_i18n::t(f.note)));
    }
    let own = match f.map(|f| f.name) {
        Some("PCX") => pcx::decode(data).map(|i| (i, "PCX")),
        Some("TGA") => tga::decode(data).map(|i| (i, "TGA")),
        Some("PNM") => pnm::decode(data).map(|i| (i, "PNM")),
        Some("ICO") => ico::decode(data).map(|i| (i, "ICO")),
        Some("Koala") => koa::decode(data).map(|i| (i, "KOA")),
        _ => return decode_magic(data),
    };
    // 副檔名寫錯的檔案很常見(.ico 其實是 PNG、.gif 其實是 JPEG):自寫的解碼器
    // 認不得時,再依 magic number 試一次。都不行才報自寫解碼器的錯。
    own.or_else(|e| decode_magic(data).map_err(|_| e))
}

/// 依 magic number 解 JPEG、PNG、GIF、BMP、TIFF、WebP。
pub fn decode_magic(data: &[u8]) -> Result<(Image, &'static str), String> {
    use image::ImageFormat as F;
    let fmt = image::guess_format(data).map_err(|e| e.to_string())?;
    let kind = match fmt {
        F::Jpeg => "JPEG",
        F::Png => "PNG",
        F::Gif => "GIF",
        F::Bmp => "BMP",
        F::Tiff => "TIFF",
        F::WebP => "WEBP",
        other => return Err(format!("{other:?}: 不支援的圖檔格式")),
    };
    // image crate 解不開的 TIFF(調色盤、CCITT Group 3)交給備援解碼器。
    if fmt == F::Tiff && tiff_fallback::handles(data) {
        return tiff_fallback::decode(data).map(|i| (i, kind));
    }
    let img = image::load_from_memory_with_format(data, fmt).map_err(|e| e.to_string())?.into_rgba8();
    let (w, h) = img.dimensions();
    Ok((Image { w, h, pix: img.into_raw() }, kind))
}

fn le16(b: &[u8], o: usize) -> u32 {
    u16::from_le_bytes([b[o], b[o + 1]]) as u32
}

fn le32(b: &[u8], o: usize) -> u32 {
    u32::from_le_bytes([b[o], b[o + 1], b[o + 2], b[o + 3]])
}

#[cfg(test)]
mod tests;
