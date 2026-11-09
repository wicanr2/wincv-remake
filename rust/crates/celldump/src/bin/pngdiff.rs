//! 逐像素比對兩張 PNG,差異以 cell 格點回報(格子大小由參數給)。
//!
//! ```text
//! pngdiff a.png b.png [cellW cellH]
//! ```
//!
//! 結束碼:0 完全相同,1 有差異,2 讀檔或尺寸問題。

use std::collections::BTreeSet;
use std::process::exit;

fn load(p: &str) -> (u32, u32, Vec<u8>) {
    let f = std::fs::File::open(p).unwrap_or_else(|e| {
        eprintln!("{p}: {e}");
        exit(2)
    });
    let mut dec = png::Decoder::new(std::io::BufReader::new(f));
    // 調色盤、灰階、少於 8 位元一律展開成 8 位元的 RGB(A)。
    dec.set_transformations(png::Transformations::EXPAND | png::Transformations::STRIP_16);
    let mut rd = dec.read_info().unwrap_or_else(|e| {
        eprintln!("{p}: {e}");
        exit(2)
    });
    let mut buf = vec![0; rd.output_buffer_size()];
    let info = rd.next_frame(&mut buf).unwrap_or_else(|e| {
        eprintln!("{p}: {e}");
        exit(2)
    });
    buf.truncate(info.buffer_size());
    let rgba = match info.color_type {
        png::ColorType::Rgba => buf,
        png::ColorType::Rgb => buf.chunks(3).flat_map(|p| [p[0], p[1], p[2], 0xFF]).collect(),
        png::ColorType::GrayscaleAlpha => buf.chunks(2).flat_map(|p| [p[0], p[0], p[0], p[1]]).collect(),
        png::ColorType::Grayscale => buf.iter().flat_map(|&g| [g, g, g, 0xFF]).collect(),
        png::ColorType::Indexed => unreachable!("EXPAND 之後不會是調色盤"),
    };
    (info.width, info.height, rgba)
}

fn main() {
    let a: Vec<String> = std::env::args().skip(1).collect();
    if a.len() != 2 && a.len() != 4 {
        eprintln!("用法: pngdiff a.png b.png [cellW cellH]");
        exit(2);
    }
    let (cw, ch): (u32, u32) = if a.len() == 4 {
        (a[2].parse().unwrap_or(8), a[3].parse().unwrap_or(16))
    } else {
        (8, 16)
    };
    let (w1, h1, p1) = load(&a[0]);
    let (w2, h2, p2) = load(&a[1]);
    if (w1, h1) != (w2, h2) {
        println!("尺寸不同: {w1}x{h1} vs {w2}x{h2}");
        exit(2);
    }
    let mut diff = 0u64;
    let mut cells = BTreeSet::new();
    for y in 0..h1 {
        for x in 0..w1 {
            let o = ((y * w1 + x) * 4) as usize;
            if p1[o..o + 4] != p2[o..o + 4] {
                diff += 1;
                cells.insert((y / ch, x / cw));
            }
        }
    }
    if diff == 0 {
        println!("相同: {w1}x{h1},{} 個像素", w1 as u64 * h1 as u64);
        return;
    }
    println!("不同: {diff} 個像素,{} 格", cells.len());
    for (row, col) in cells.iter().take(40) {
        println!("  第 {row} 列第 {col} 欄");
    }
    exit(1);
}
