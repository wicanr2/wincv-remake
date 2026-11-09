//! Netpbm 家族:P1 PBM ASCII、P2 PGM ASCII、P3 PPM ASCII、P4 PBM binary、P5 PGM binary、
//! P6 PPM binary。PBM 的 1 是**黑**、0 是白,跟直覺相反,弄錯會整張反相。

use wincv_i18n::{t, tr};

use crate::Image;

pub fn decode(d: &[u8]) -> Result<Image, String> {
    if d.len() < 2 || d[0] != b'P' {
        return Err(t("不是 PNM"));
    }
    let kind = d[1];
    if !(b'1'..=b'6').contains(&kind) {
        return Err(tr!("不認得的 PNM 類型 P%c", kind as char));
    }
    let mut p = 2;
    let next = |p: &mut usize| -> Result<i64, String> {
        while *p < d.len() {
            match d[*p] {
                b'#' => {
                    // 註解到行尾
                    while *p < d.len() && d[*p] != b'\n' {
                        *p += 1;
                    }
                }
                b' ' | b'\t' | b'\n' | b'\r' => *p += 1,
                _ => break,
            }
        }
        if *p >= d.len() {
            return Err(t("PNM 檔頭不完整"));
        }
        let start = *p;
        let mut v: i64 = 0;
        while *p < d.len() && d[*p].is_ascii_digit() {
            v = v.wrapping_mul(10).wrapping_add((d[*p] - b'0') as i64);
            *p += 1;
        }
        if *p == start {
            return Err(t("PNM 檔頭讀不到數字"));
        }
        Ok(v)
    };
    let w = next(&mut p)?;
    let h = next(&mut p)?;
    let maxv = if kind != b'1' && kind != b'4' { next(&mut p)? } else { 1 };
    if w <= 0 || h <= 0 || maxv <= 0 || w > 1 << 16 || h > 1 << 16 {
        return Err(tr!("PNM 尺寸不合理 %dx%d maxv=%d", w, h, maxv));
    }
    let (w, h) = (w as usize, h as usize);
    // binary 格式在 maxval 之後**剛好一個** whitespace,然後就是資料。
    if kind >= b'4' && p < d.len() {
        p += 1;
    }
    let mut img = Image::new(w as u32, h as u32);
    let scale = |v: i64| if maxv == 255 { v as u8 } else { (v * 255 / maxv) as u8 };
    let gray = |c: u8| [c, c, c, 0xFF];
    match kind {
        b'1'..=b'3' => {
            // ASCII。資料不足就畫到哪算哪。
            'rows: for y in 0..h {
                for x in 0..w {
                    let c = match kind {
                        b'1' => match next(&mut p) {
                            Ok(v) => gray(if v == 1 { 0 } else { 255 }),
                            Err(_) => break 'rows,
                        },
                        b'2' => match next(&mut p) {
                            Ok(v) => gray(scale(v)),
                            Err(_) => break 'rows,
                        },
                        _ => {
                            let Ok(r) = next(&mut p) else { break 'rows };
                            let g = next(&mut p).unwrap_or(0);
                            let b = next(&mut p).unwrap_or(0);
                            [scale(r), scale(g), scale(b), 0xFF]
                        }
                    };
                    img.set(x as u32, y as u32, c);
                }
            }
        }
        b'4' => {
            // PBM binary:每列補齊到 byte 邊界
            let row = w.div_ceil(8);
            for y in 0..h {
                for x in 0..w {
                    let bit = d.get(p + y * row + x / 8).map_or(0, |b| b & (0x80 >> (x % 8)));
                    img.set(x as u32, y as u32, gray(if bit != 0 { 0 } else { 255 }));
                }
            }
        }
        _ => {
            // PGM / PPM binary
            let nch = if kind == b'6' { 3 } else { 1 };
            let bpc = if maxv > 255 { 2 } else { 1 };
            let read = |o: usize| -> i64 {
                if bpc == 2 {
                    d.get(o..o + 2).map_or(0, |b| u16::from_be_bytes([b[0], b[1]]) as i64)
                } else {
                    d.get(o).map_or(0, |&b| b as i64)
                }
            };
            let stride = w * nch * bpc;
            for y in 0..h {
                for x in 0..w {
                    let o = p + y * stride + x * nch * bpc;
                    let c = if nch == 1 {
                        gray(scale(read(o)))
                    } else {
                        [scale(read(o)), scale(read(o + bpc)), scale(read(o + 2 * bpc)), 0xFF]
                    };
                    img.set(x as u32, y as u32, c);
                }
            }
        }
    }
    Ok(img)
}
