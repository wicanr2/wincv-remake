//! SVG 的第三方對照:Go 版、Rust 版各自與 Chromium 畫出來的結果差多少。
//!
//! ```text
//! svgcmp <目錄>
//! ```
//!
//! 目錄裡要有 `list.txt`(檔案清單)、`go.tsv` 與 `rust.tsv`(兩邊 imgsum 的輸出)、
//! `go/<i>.rgba`、`rust/<i>.rgba`,以及 `chrome/<i>.png`(Chromium 以 Rust 版的尺寸、
//! 黑底截的圖,可以比圖大,左上角對齊)。三者都疊在黑底上比,差值取 RGB 三個通道。
//!
//! stdout 每列「序號<TAB>Go 平均差<TAB>Go 最大差<TAB>Rust 平均差<TAB>Rust 最大差<TAB>檔名」,
//! 某一邊沒有結果寫「-」;最後印總結。

use std::collections::HashMap;

fn sizes(path: &str) -> HashMap<usize, (u32, u32)> {
    let s = std::fs::read_to_string(path).unwrap_or_default();
    s.lines()
        .filter_map(|l| {
            let f: Vec<&str> = l.split('\t').collect();
            (f.len() >= 4 && f[1] != "ERR").then(|| Some((f[0].parse().ok()?, (f[2].parse().ok()?, f[3].parse().ok()?))))?
        })
        .collect()
}

/// 非預乘的 RGBA 疊到黑底上。
fn on_black(pix: &[u8]) -> Vec<u8> {
    pix.chunks_exact(4).flat_map(|p| [0, 1, 2].map(|c| ((p[c] as u32 * p[3] as u32 + 127) / 255) as u8)).collect()
}

fn read_png(path: &str) -> Option<(u32, u32, Vec<u8>)> {
    let dec = png::Decoder::new(std::io::BufReader::new(std::fs::File::open(path).ok()?));
    let mut r = dec.read_info().ok()?;
    let mut buf = vec![0; r.output_buffer_size()];
    let info = r.next_frame(&mut buf).ok()?;
    let (w, h) = (info.width, info.height);
    let ch = match info.color_type {
        png::ColorType::Rgba => 4,
        png::ColorType::Rgb => 3,
        _ => return None,
    };
    let rgb = buf[..(w * h) as usize * ch].chunks_exact(ch).flat_map(|p| [p[0], p[1], p[2]]).collect();
    Some((w, h, rgb))
}

/// 平均差與最大差。`refimg` 是 Chromium 的截圖(寬 rw),比左上角 w×h 那一塊。
fn diff(a: &[u8], w: u32, h: u32, refimg: &[u8], rw: u32) -> (f64, u8) {
    let (mut sum, mut max) = (0u64, 0u8);
    for y in 0..h {
        for x in 0..w {
            for c in 0..3 {
                let i = ((y * w + x) * 3 + c) as usize;
                let j = ((y * rw + x) * 3 + c) as usize;
                let d = a[i].abs_diff(refimg[j]);
                sum += d as u64;
                max = max.max(d);
            }
        }
    }
    (sum as f64 / (w as f64 * h as f64 * 3.0), max)
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [dir] = args.as_slice() else {
        eprintln!("用法: svgcmp <目錄>");
        std::process::exit(2);
    };
    let list: Vec<String> = std::fs::read_to_string(format!("{dir}/list.txt")).unwrap_or_default().lines().map(String::from).collect();
    let (go, rust) = (sizes(&format!("{dir}/go.tsv")), sizes(&format!("{dir}/rust.tsv")));
    let (mut n, mut go_err, mut rust_err, mut both) = (0, 0, 0, 0);
    let (mut gm, mut rm, mut gx, mut rx, mut closer, mut same_size) = (0f64, 0f64, 0f64, 0f64, 0, 0);
    for (i, name) in list.iter().enumerate() {
        n += 1;
        let Some(&(w, h)) = rust.get(&i) else {
            rust_err += 1;
            println!("{i}\t-\t-\t-\t-\t{name}");
            continue;
        };
        let Some((cw, ch, chrome)) = read_png(&format!("{dir}/chrome/{i}.png")).filter(|(cw, ch, _)| *cw >= w && *ch >= h) else {
            println!("{i}\t-\t-\t-\t-\t{name}\t(沒有 Chromium 截圖)");
            continue;
        };
        let _ = ch;
        let load = |side: &str| std::fs::read(format!("{dir}/{side}/{i}.rgba")).ok().map(|p| on_black(&p));
        let Some(r) = load("rust").filter(|p| p.len() == (w * h * 3) as usize) else { continue };
        let (rmean, rmax) = diff(&r, w, h, &chrome, cw);
        let g = go.get(&i).filter(|s| **s == (w, h)).and_then(|_| load("go"));
        match g {
            Some(g) => {
                let (gmean, gmax) = diff(&g, w, h, &chrome, cw);
                println!("{i}\t{gmean:.3}\t{gmax}\t{rmean:.3}\t{rmax}\t{name}");
                both += 1;
                same_size += 1;
                (gm, rm, gx, rx) = (gm + gmean, rm + rmean, gx + gmax as f64, rx + rmax as f64);
                if rmean < gmean {
                    closer += 1;
                }
            }
            None => {
                if !go.contains_key(&i) {
                    go_err += 1;
                }
                println!("{i}\t-\t-\t{rmean:.3}\t{rmax}\t{name}");
            }
        }
    }
    let k = both.max(1) as f64;
    eprintln!(
        "{n} 個 SVG;Go 解不開 {go_err}、Rust 解不開 {rust_err};兩邊尺寸相同且有截圖的 {same_size} 個:\n\
         與 Chromium 的平均差 Go {:.3} / Rust {:.3};平均最大差 Go {:.1} / Rust {:.1};Rust 比較接近的 {closer} 個",
        gm / k,
        rm / k,
        gx / k,
        rx / k
    );
}
