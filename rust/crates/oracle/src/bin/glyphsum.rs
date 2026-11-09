//! 用 `wincv-ttf` 把一份字型的一串字畫成字模(對應 Go 端 `tools/glyphsum`,輸出同樣格式)。
//!
//! ```text
//! glyphsum <字型> <半形寬> <全形寬> <高> <fit 0|1> <aa 0|1> < 字串
//! ```

use std::io::{Read, Write};
use std::path::Path;

use wincv_fnt::Glyph;
use wincv_render::HalfSource;

fn put(w: &mut impl Write, key: &str, g: Option<Glyph>) {
    let Some(g) = g else {
        let _ = writeln!(w, "{key}\t-");
        return;
    };
    let mut bits = vec![0u8; g.bits.len().div_ceil(8)];
    for (i, &b) in g.bits.iter().enumerate() {
        if b {
            bits[i / 8] |= 0x80 >> (i % 8);
        }
    }
    let hex = |b: &[u8]| b.iter().map(|x| format!("{x:02x}")).collect::<String>();
    let _ = writeln!(w, "{key}\t{}\t{}\t{}\t{}", g.w, g.h, hex(&bits), hex(g.alpha.as_deref().unwrap_or(&[])));
}

fn main() {
    let a: Vec<String> = std::env::args().skip(1).collect();
    if a.len() != 6 {
        eprintln!("用法: glyphsum <字型> <半形寬> <全形寬> <高> <fit 0|1> <aa 0|1> < 字串");
        std::process::exit(2);
    }
    let n = |i: usize| a[i].parse::<i32>().unwrap_or(0);
    let (hw, fw, h) = (n(1), n(2), n(3));
    let out = std::io::stdout();
    let mut w = std::io::BufWriter::new(out.lock());
    if a[4] == "1" {
        let hf = wincv_ttf::HalfFont::load_fit(Path::new(&a[0]), hw, h).unwrap_or_else(|e| {
            eprintln!("{e}");
            std::process::exit(1)
        });
        for c in 0..=255u8 {
            put(&mut w, &format!("C+{c:02X}"), hf.glyph(c));
        }
        return;
    }
    let f = wincv_ttf::Font::load(Path::new(&a[0]), hw, fw, h).unwrap_or_else(|e| {
        eprintln!("{e}");
        std::process::exit(1)
    });
    f.set_aa(a[5] == "1");
    let mut s = String::new();
    let _ = std::io::stdin().read_to_string(&mut s);
    for r in s.chars().filter(|&r| r != '\n') {
        put(&mut w, &format!("U+{:04X}", r as u32), f.glyph(r));
    }
}
