//! 用 hayro 把 PDF 的一頁畫成 PNG(第一階段評估用,對應 Go 端 `tools/pdfshot`)。
//!
//! ```text
//! pdfshot -o out.png -page 1 -dpi 96 in.pdf
//! ```
//!
//! 與 Go 版、LibreOffice 各畫一張,用 `tools/inkdiff` 比墨水分布。
//! 標準 14 字型用 hayro 的 `embed-fonts`(可散布的替代字型)。

use std::process::exit;
use std::time::Instant;

use hayro::hayro_interpret::InterpreterSettings;
use hayro::hayro_syntax::Pdf;
use hayro::vello_cpu::color::palette::css::WHITE;
use hayro::{render, PixmapSettings, RenderCache, RenderSettings};

fn die(msg: &str) -> ! {
    eprintln!("pdfshot: {msg}");
    exit(1)
}

fn main() {
    let (mut out, mut page, mut dpi, mut input) = ("page.png".to_string(), 1usize, 96f32, None);
    let mut it = std::env::args().skip(1);
    while let Some(k) = it.next() {
        match k.as_str() {
            "-o" => out = it.next().unwrap_or_else(|| die("-o 少了參數")),
            "-page" => page = it.next().and_then(|v| v.parse().ok()).unwrap_or_else(|| die("-page 要是正整數")),
            "-dpi" => dpi = it.next().and_then(|v| v.parse().ok()).unwrap_or_else(|| die("-dpi 要是數字")),
            _ => input = Some(k),
        }
    }
    let input = input.unwrap_or_else(|| die("用法: pdfshot -o out.png -page 1 -dpi 96 in.pdf"));
    let data = std::fs::read(&input).unwrap_or_else(|e| die(&format!("{input}: {e}")));
    let t0 = Instant::now();
    let pdf = Pdf::new(data).unwrap_or_else(|e| die(&format!("{input}: {e:?}")));
    let pages = pdf.pages();
    let p = pages.get(page.wrapping_sub(1)).unwrap_or_else(|| die(&format!("沒有第 {page} 頁(共 {} 頁)", pages.len())));
    let scale = dpi / 72.0;
    let pix = render(
        p,
        &RenderCache::new(),
        &InterpreterSettings::default(),
        &RenderSettings::default(),
        &PixmapSettings { x_scale: scale, y_scale: scale, bg_color: WHITE },
    );
    let (w, h) = (pix.width(), pix.height());
    let png = pix.into_png().unwrap_or_else(|e| die(&format!("{e:?}")));
    std::fs::write(&out, png).unwrap_or_else(|e| die(&format!("{out}: {e}")));
    eprintln!("{out}  {w}x{h}  {:?}", t0.elapsed());
}
