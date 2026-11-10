//! 印出一份 PDF 經 wincv-pdfdoc 取出的內容。輸出格式與 Go 端的 `tools/pdfsum` 相同,
//! 兩邊直接 diff;欄位說明見那支程式的開頭。
//!
//! ```text
//! tools/cargo.sh run --release -p wincv-pdfdoc --example pdfsum -- [-pages N] [-glyphs] [-shot 目錄] [-shotpages N] [-dpi 96] 檔案.pdf
//! ```

use std::panic::{AssertUnwindSafe, catch_unwind};
use std::process::exit;

use sha2::{Digest, Sha256};

fn esc(s: &str) -> String {
    let mut o = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '\\' => o.push_str("\\\\"),
            '\t' => o.push_str("\\t"),
            '\n' => o.push_str("\\n"),
            '\r' => o.push_str("\\r"),
            _ => o.push(c),
        }
    }
    o
}

fn f2(v: f64) -> String {
    format!("{v:.2}")
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// 執行一段,panic 時印成 error 列。
fn guard(section: &str, page: usize, f: impl FnOnce()) {
    if let Err(e) = catch_unwind(AssertUnwindSafe(f)) {
        let msg = e.downcast_ref::<String>().cloned().or_else(|| e.downcast_ref::<&str>().map(|s| s.to_string())).unwrap_or_default();
        if page > 0 {
            println!("error\t{section}\t{page}\tpanic: {}", esc(&msg));
        } else {
            println!("error\t{section}\tpanic: {}", esc(&msg));
        }
    }
}

/// 用 app 走的同一個解碼器(wincv-imgfmt)解圖,回「寬x高 像素sha256 平均RGBA」。
fn pixel_sum(name: &str, b: &[u8]) -> String {
    let Ok((m, _)) = wincv_imgfmt::decode(name, b) else { return "-\t-\t-".into() };
    let sum = Sha256::digest(&m.pix);
    let mut acc = [0f64; 4];
    for p in m.pix.chunks_exact(4) {
        for c in 0..4 {
            acc[c] += p[c] as f64;
        }
    }
    let px = ((m.pix.len() / 4) as f64).max(1.0);
    format!(
        "{}x{}\t{}\t{:.1},{:.1},{:.1},{:.1}",
        m.w,
        m.h,
        hex(&sum),
        acc[0] / px,
        acc[1] / px,
        acc[2] / px,
        acc[3] / px
    )
}

fn write_png(path: &std::path::Path, img: &wincv_render::Image) -> Result<(), String> {
    let f = std::fs::File::create(path).map_err(|e| e.to_string())?;
    let mut enc = png::Encoder::new(std::io::BufWriter::new(f), img.w, img.h);
    enc.set_color(png::ColorType::Rgba);
    enc.set_depth(png::BitDepth::Eight);
    let mut w = enc.write_header().map_err(|e| e.to_string())?;
    w.write_image_data(&img.pix).map_err(|e| e.to_string())
}

fn main() {
    let (mut max_pages, mut glyphs, mut shot, mut shot_n, mut dpi, mut input) = (0usize, false, None, 1usize, 96f64, None);
    let mut it = std::env::args().skip(1);
    let usage = || -> ! {
        eprintln!("用法: pdfsum [-pages N] [-glyphs] [-shot 目錄] [-shotpages N] [-dpi 96] 檔案.pdf");
        exit(2)
    };
    while let Some(k) = it.next() {
        match k.as_str() {
            "-pages" => max_pages = it.next().and_then(|v| v.parse().ok()).unwrap_or_else(|| usage()),
            "-glyphs" => glyphs = true,
            "-shot" => shot = Some(it.next().unwrap_or_else(|| usage())),
            "-shotpages" => shot_n = it.next().and_then(|v| v.parse().ok()).unwrap_or_else(|| usage()),
            "-dpi" => dpi = it.next().and_then(|v| v.parse().ok()).unwrap_or_else(|| usage()),
            _ if input.is_none() => input = Some(k),
            _ => usage(),
        }
    }
    let path = input.unwrap_or_else(|| usage());
    println!("file\t{}", esc(&path));
    let mut opened = None;
    guard("open", 0, || match wincv_pdfdoc::open(&path) {
        Ok(d) => opened = Some(d),
        Err(e) => println!("error\topen\t{}", esc(&e)),
    });
    let Some(d) = opened else { return };
    println!("pages\t{}", d.pages);
    let n = if max_pages > 0 { d.pages.min(max_pages) } else { d.pages };
    for p in 1..=n {
        guard("size", p, || match d.page_size(p) {
            Ok((w, h)) => println!("size\t{p}\t{}\t{}", f2(w), f2(h)),
            Err(e) => println!("error\tsize\t{p}\t{}", esc(&e)),
        });
    }
    guard("outline", 0, || {
        for b in d.outline() {
            println!("bookmark\t{}\t{}\t{}", b.level, b.page, esc(&b.title));
        }
    });
    for p in 1..=n {
        if glyphs {
            guard("glyphs", p, || match d.texts(p) {
                Ok(ts) => {
                    for t in ts {
                        println!("glyph\t{p}\t{}\t{}\t{}\t{}\t{}", f2(t.x), f2(t.y), f2(t.w), f2(t.size), esc(&t.s));
                    }
                }
                Err(e) => println!("error\tglyphs\t{p}\t{}", esc(&e)),
            });
        }
        guard("text", p, || match d.text(p) {
            Ok(lines) => {
                for l in lines {
                    println!("line\t{p}\t{}\t{}\t{}", f2(l.y), l.indent, esc(&l.text));
                }
            }
            Err(e) => println!("error\ttext\t{p}\t{}", esc(&e)),
        });
        guard("images", p, || match d.image_names(p) {
            Ok(names) => {
                for name in names {
                    match d.image(p, &name) {
                        Ok(b) => {
                            let sum = Sha256::digest(&b);
                            println!("image\t{p}\t{}\t{}\t{}\t{}", esc(&name), b.len(), hex(&sum), pixel_sum(&name, &b));
                        }
                        Err(e) => println!("error\timage\t{p}\t{}", esc(&e)),
                    }
                }
            }
            Err(e) => println!("error\timages\t{p}\t{}", esc(&e)),
        });
    }
    if let Some(dir) = shot {
        for p in 1..=n.min(shot_n) {
            guard("shot", p, || match d.render(p, dpi) {
                Ok(r) => {
                    let name = format!("{p}.png");
                    match write_png(&std::path::Path::new(&dir).join(&name), &r.img) {
                        Ok(()) => println!("shot\t{p}\t{}x{}\t{}\t{name}", r.img.w, r.img.h, f2(r.dpi)),
                        Err(e) => println!("error\tshot\t{p}\t{}", esc(&e)),
                    }
                }
                Err(e) => println!("error\tshot\t{p}\t{}", esc(&e)),
            });
        }
    }
}
