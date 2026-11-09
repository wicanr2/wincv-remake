//! Windows .ico,挑面積最大的那一張。
//!
//! 目錄項的 width/height 是 1 byte,0 表示 256:照字面讀會把 256x256 的圖當成 0x0
//! 而挑到小的那張。圖片本體可能是 PNG(Vista 之後)或 BMP。BMP 的高度是**兩倍**,
//! 因為底下接著 AND 遮罩;而且沒有 BITMAPFILEHEADER。

use wincv_i18n::{t, tr};

use crate::{le16, le32, Image};

pub fn decode(d: &[u8]) -> Result<Image, String> {
    if d.len() < 6 || le16(d, 0) != 0 || le16(d, 2) != 1 {
        return Err(t("不是 ICO"));
    }
    let n = le16(d, 4) as usize;
    if n == 0 {
        return Err(t("ICO 沒有任何圖"));
    }
    let mut best: Option<(usize, usize)> = None;
    for i in 0..n {
        let o = 6 + i * 16;
        if o + 16 > d.len() {
            break;
        }
        let w = if d[o] == 0 { 256 } else { d[o] as usize };
        let h = if d[o + 1] == 0 { 256 } else { d[o + 1] as usize };
        if best.is_none_or(|(_, a)| w * h > a) {
            best = Some((i, w * h));
        }
    }
    let Some((best, _)) = best else { return Err(t("ICO 目錄壞了")) };
    let o = 6 + best * 16;
    let (size, off) = (le32(d, o + 8) as usize, le32(d, o + 12) as usize);
    if off.checked_add(size).is_none_or(|e| e > d.len()) {
        return Err(t("ICO 圖片資料超出檔案"));
    }
    let body = &d[off..off + size];
    if body.len() > 8 && body[..8] == [0x89, b'P', b'N', b'G', 0x0D, 0x0A, 0x1A, 0x0A] {
        return crate::decode_magic(body).map(|(i, _)| i);
    }
    decode_bmp(body)
}

fn decode_bmp(b: &[u8]) -> Result<Image, String> {
    if b.len() < 40 {
        return Err(t("ICO 內的 BMP 檔頭不完整"));
    }
    let w = le32(b, 4) as i32 as i64;
    let h2 = le32(b, 8) as i32 as i64;
    let bpp = le16(b, 14) as usize;
    let h = h2 / 2; // 上半是圖、下半是 AND 遮罩
    if w <= 0 || h <= 0 || w > 1 << 16 || h > 1 << 16 {
        return Err(tr!("ICO 內的 BMP 尺寸不合理 %dx%d", w, h));
    }
    let (w, h) = (w as usize, h as usize);
    let mut hdr = le32(b, 0) as usize;
    if hdr < 40 || hdr > b.len() {
        hdr = 40;
    }
    let mut n_colors = le32(b, 32) as usize;
    if n_colors == 0 && bpp <= 8 {
        n_colors = 1 << bpp;
    }
    let pal_off = hdr;
    let pal: Vec<[u8; 4]> = (0..n_colors)
        .take_while(|i| pal_off + i * 4 + 3 < b.len())
        .map(|i| {
            let p = &b[pal_off + i * 4..];
            [p[2], p[1], p[0], 0xFF]
        })
        .collect();
    let data_off = pal_off + n_colors * 4;
    let row_bytes = (w * bpp).div_ceil(32) * 4; // 每列補齊到 4 bytes
    let mask_row = w.div_ceil(32) * 4;
    let mask_off = data_off + row_bytes * h;
    let byte = |o: usize| b.get(o).copied();
    let mut img = Image::new(w as u32, h as u32);
    for y in 0..h {
        let sy = h - 1 - y; // BMP 由下往上
        let row = data_off + sy * row_bytes;
        for x in 0..w {
            let pal_at = |v: Option<u8>| v.and_then(|v| pal.get(v as usize).copied()).unwrap_or([0; 4]);
            let mut c = match bpp {
                32 => b.get(row + x * 4..row + x * 4 + 4).map_or([0; 4], |p| [p[2], p[1], p[0], p[3]]),
                24 => b.get(row + x * 3..row + x * 3 + 3).map_or([0; 4], |p| [p[2], p[1], p[0], 0xFF]),
                8 => pal_at(byte(row + x)),
                4 => pal_at(byte(row + x / 2).map(|v| if x % 2 == 1 { v & 0x0F } else { v >> 4 })),
                1 => pal_at(byte(row + x / 8).map(|v| (v & (0x80 >> (x % 8)) != 0) as u8)),
                _ => return Err(tr!("ICO: 還沒支援 %d bpp", bpp)),
            };
            // 32bpp 自帶 alpha,其餘看 AND 遮罩:1 表示透明。
            if bpp != 32 {
                let masked = byte(mask_off + sy * mask_row + x / 8).is_some_and(|m| m & (0x80 >> (x % 8)) != 0);
                c[3] = if masked { 0 } else { 0xFF };
            }
            img.set(x as u32, y as u32, c);
        }
    }
    Ok(img)
}
