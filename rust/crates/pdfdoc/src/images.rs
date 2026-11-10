//! 一頁上的圖片。
//!
//! Go 版交給 pdfcpu 的 `ExtractImagesRaw`:列出頁面資源(含巢狀表單)裡的每個
//! 影像物件,JPEG(非 CMYK)原樣交出,其餘依色彩空間轉成 png 或 tif,名稱是
//! 資源名稱加副檔名。這裡的差別有兩處,都是為了貼近 poppler 的 `pdfimages`:
//!
//! - 只收**真的畫在這一頁**的影像(直譯一次內容串流,看 `draw_image`)。
//!   共用資源字典的檔案,pdfcpu 會把整份文件的圖列在每一頁上。
//! - 不是原樣 JPEG 的一律由 hayro 解碼(套用色彩空間、ICC、Decode 陣列與
//!   SMask 的透明度)再存成 png。pdfcpu 對 CMYK 交 tif、對 JPX 交原始碼流,
//!   而且不套 SMask。
//!
//! 名稱沿用 Go 版的規則:頁面資源(依名稱排序、深度優先)裡第一個指到這個
//! 物件的資源名稱。內嵌影像(BI…EI)沒有資源名稱,與 Go 版一樣不收。

use std::collections::{BTreeMap, HashMap, HashSet};

use hayro::hayro_interpret::font::GlyphRun;
use hayro::hayro_interpret::{
    BlendMode, ClipPath, Context, Device, DrawMode, DrawProps, Image, ImageData, ImageDrawProps, InterpreterCache,
    InterpreterSettings, LumaData, RenderContent, SoftMask, interpret_page,
};
use hayro::hayro_syntax::Filter;
use hayro::hayro_syntax::object::{Dict, MaybeRef, Name, ObjRef, Stream};
use hayro::hayro_syntax::page::Page;
use hayro::kurbo::{Affine, BezPath, Rect};

use crate::MAX_IMAGE_BYTES;

/// 解碼前的像素數上限。一張 2 億像素的掃描圖解成 RGBA 要 800 MB。
pub const MAX_IMAGE_PIXELS: u64 = 64 << 20;

/// 資源巢狀的上限(表單裡的表單)。
const MAX_FORM_DEPTH: usize = 16;

/// 物件編號 → 資源名稱,由頁面資源走出來。
fn resource_names(page: &Page<'_>) -> HashMap<u32, String> {
    let mut out = HashMap::new();
    let mut seen = HashSet::new();
    walk_xobjects(&page.resources().x_objects, &mut out, &mut seen, 0);
    out
}

fn walk_xobjects(xobjs: &Dict<'_>, out: &mut HashMap<u32, String>, seen: &mut HashSet<u32>, depth: usize) {
    if depth > MAX_FORM_DEPTH {
        return;
    }
    // entries() 依鍵排序,名稱的選擇是決定性的。
    for (name, v) in xobjs.entries() {
        // 與 pdfcpu 相同:只看間接參照。
        let MaybeRef::Ref(r) = v else { continue };
        let num = r.obj_number;
        if !seen.insert(num) {
            continue;
        }
        let Some(st) = xobjs.get::<Stream<'_>>(name.as_ref()) else { continue };
        match st.dict().get::<Name<'_>>(b"Subtype").as_deref() {
            Some(b"Image") => {
                out.entry(num).or_insert_with(|| name.as_str().to_string());
            }
            Some(b"Form") => {
                if let Some(res) = st.dict().get::<Dict<'_>>(b"Resources") {
                    if let Some(x) = res.get::<Dict<'_>>(b"XObject") {
                        walk_xobjects(&x, out, seen, depth + 1);
                    }
                }
            }
            _ => {}
        }
    }
}

/// 取一頁上的圖:名稱(含副檔名)→ 可以直接解碼的 png 或 jpg。
pub(crate) fn page_images(page: &Page<'_>, xref: &hayro::hayro_syntax::xref::XRef) -> BTreeMap<String, Vec<u8>> {
    let names = resource_names(page);
    let mut dev = ImageDevice { names, done: HashSet::new(), out: BTreeMap::new() };
    if dev.names.is_empty() {
        return dev.out;
    }
    let settings = InterpreterSettings { render_content: RenderContent::PAGE, ..Default::default() };
    let cache = InterpreterCache::new();
    let mb = page.media_box();
    let mut ctx = Context::new(Affine::IDENTITY, Rect::new(mb.x0, mb.y0, mb.x1, mb.y1), &cache, xref, settings);
    interpret_page(page, &mut ctx, &mut dev);
    dev.out
}

struct ImageDevice {
    names: HashMap<u32, String>,
    done: HashSet<u32>,
    out: BTreeMap<String, Vec<u8>>,
}

impl ImageDevice {
    fn add(&mut self, num: u32, ext: &str, data: Vec<u8>) {
        if data.is_empty() || data.len() > MAX_IMAGE_BYTES {
            return;
        }
        let base = self.names.get(&num).cloned().unwrap_or_else(|| format!("obj{num}"));
        let mut key = format!("{base}.{ext}");
        // 不同的物件掛在同一個資源名稱下(兩個表單各有一個 Im0)。Go 版的
        // map 會讓後一張蓋掉前一張;這裡補上物件編號,兩張都留著。
        if self.out.contains_key(&key) {
            key = format!("{base}-obj{num}.{ext}");
        }
        self.out.insert(key, data);
    }
}

impl<'a> Device<'a> for ImageDevice {
    fn draw_path(&mut self, _: &BezPath, _: DrawProps<'a>, _: &DrawMode) {}
    fn push_clip_path(&mut self, _: &ClipPath) {}
    fn push_transparency_group(&mut self, _: f32, _: Option<SoftMask<'a>>, _: BlendMode) {}
    fn draw_glyph_run(&mut self, _: &GlyphRun<'_, 'a>, _: DrawProps<'a>, _: &DrawMode) {}
    fn pop_clip(&mut self) {}
    fn pop_transparency_group(&mut self) {}

    fn draw_image(&mut self, image: Image<'a, '_>, _: ImageDrawProps<'a>) {
        let st = match &image {
            Image::Raster(r) => r.stream(),
            Image::Stencil(s) => s.stream(),
        };
        let num = ObjRef::from(st.obj_id()).obj_number;
        if !self.names.contains_key(&num) || !self.done.insert(num) {
            return;
        }
        if (image.width() as u64) * (image.height() as u64) > MAX_IMAGE_PIXELS {
            return;
        }
        // JPEG 原樣交出(與 pdfcpu 相同,位元組一致);四色(CMYK)的除外,
        // 一般的解碼器把它畫成負片。
        if let Image::Raster(_) = &image {
            if st.filters().as_slice() == [Filter::DctDecode] {
                let raw = st.raw_data();
                if jpeg_components(&raw).is_some_and(|n| n != 4) {
                    self.add(num, "jpg", raw.into_owned());
                    return;
                }
            }
        }
        let png = match &image {
            Image::Raster(r) => {
                let mut png = None;
                r.with_rgba(|data, alpha| png = encode_raster(data, alpha), None);
                png
            }
            Image::Stencil(s) => {
                let mut png = None;
                // 遮罩的值是「這裡要不要上色」,存成白底黑字的灰階。
                s.with_stencil(
                    |m, _| {
                        let inv: Vec<u8> = m.data.iter().map(|v| 255 - v).collect();
                        png = encode_png(m.width, m.height, png::ColorType::Grayscale, &inv);
                    },
                    None,
                );
                png
            }
        };
        if let Some(b) = png {
            self.add(num, "png", b);
        }
    }
}

fn encode_raster(data: ImageData, alpha: Option<LumaData>) -> Option<Vec<u8>> {
    let (w, h) = (data.width(), data.height());
    // SMask 的尺寸可以與影像不同,那種情形照影像的格點取最近的點。
    let alpha = alpha.map(|a| {
        if a.width == w && a.height == h {
            a.data
        } else {
            let mut out = Vec::with_capacity(w as usize * h as usize);
            for y in 0..h {
                let sy = (y as u64 * a.height as u64 / h.max(1) as u64) as usize;
                for x in 0..w {
                    let sx = (x as u64 * a.width as u64 / w.max(1) as u64) as usize;
                    out.push(a.data.get(sy * a.width as usize + sx).copied().unwrap_or(255));
                }
            }
            out
        }
    });
    let alpha = alpha.filter(|a| a.iter().any(|&v| v != 255));
    match (data, alpha) {
        (ImageData::Rgb(d), None) => encode_png(w, h, png::ColorType::Rgb, &d.data),
        (ImageData::Luma(d), None) => encode_png(w, h, png::ColorType::Grayscale, &d.data),
        (ImageData::Rgb(d), Some(a)) => {
            let px: Vec<u8> = d.data.chunks_exact(3).zip(&a).flat_map(|(c, &al)| [c[0], c[1], c[2], al]).collect();
            encode_png(w, h, png::ColorType::Rgba, &px)
        }
        (ImageData::Luma(d), Some(a)) => {
            let px: Vec<u8> = d.data.iter().zip(&a).flat_map(|(&g, &al)| [g, al]).collect();
            encode_png(w, h, png::ColorType::GrayscaleAlpha, &px)
        }
    }
}

fn encode_png(w: u32, h: u32, ct: png::ColorType, px: &[u8]) -> Option<Vec<u8>> {
    if w == 0 || h == 0 {
        return None;
    }
    let mut buf = Vec::new();
    {
        let mut enc = png::Encoder::new(&mut buf, w, h);
        enc.set_color(ct);
        enc.set_depth(png::BitDepth::Eight);
        let mut wr = enc.write_header().ok()?;
        wr.write_image_data(px).ok()?;
    }
    Some(buf)
}

/// JPEG 的色彩分量數(SOF 區段的 Nf)。找不到 SOF 回 None。
fn jpeg_components(b: &[u8]) -> Option<u8> {
    if b.len() < 4 || b[0] != 0xFF || b[1] != 0xD8 {
        return None;
    }
    let mut i = 2;
    while i + 4 <= b.len() {
        if b[i] != 0xFF {
            i += 1;
            continue;
        }
        let m = b[i + 1];
        if m == 0xFF {
            i += 1;
            continue;
        }
        if m == 0xD8 || m == 0x01 || (0xD0..=0xD7).contains(&m) {
            i += 2;
            continue;
        }
        let len = u16::from_be_bytes([b[i + 2], b[i + 3]]) as usize;
        // SOF0–SOF15,扣掉 DHT(C4)、JPG(C8)、DAC(CC)。
        if (0xC0..=0xCF).contains(&m) && m != 0xC4 && m != 0xC8 && m != 0xCC {
            return b.get(i + 9).copied();
        }
        if m == 0xDA {
            return None;
        }
        i += 2 + len;
    }
    None
}
