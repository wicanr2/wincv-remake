//! ZSoft PCX。
//!
//! 檔頭 128 bytes:0x00 manufacturer 固定 0x0A、0x02 encoding(1 = RLE)、0x03 每個
//! plane 的位元數、0x04 xmin ymin xmax ymax(各 2 bytes,**含端點**)、0x41 plane 數、
//! 0x42 每個 plane 每列幾個 byte。256 色調色盤在檔尾:最後 769 bytes,第一個 byte 是 0x0C。

use wincv_i18n::{t, tr};

use crate::{le16, Image};

pub fn decode(d: &[u8]) -> Result<Image, String> {
    if d.len() < 128 || d[0] != 0x0A {
        return Err(t("不是 PCX"));
    }
    let enc = d[2];
    let bpp = d[3] as usize;
    let (xmin, ymin, xmax, ymax) = (le16(d, 4) as i64, le16(d, 6) as i64, le16(d, 8) as i64, le16(d, 10) as i64);
    let planes = d[0x41] as usize;
    let bpl = le16(d, 0x42) as usize;
    let (w, h) = (xmax - xmin + 1, ymax - ymin + 1);
    if w <= 0 || h <= 0 || w > 1 << 16 || h > 1 << 16 || planes == 0 || bpl == 0 {
        return Err(tr!("PCX 尺寸不合理: %dx%d planes=%d bpl=%d", w, h, planes, bpl));
    }
    let (w, h) = (w as usize, h as usize);
    let total = bpl * planes * h;
    let mut raw = Vec::with_capacity(total);
    let body = &d[128..];
    if enc == 1 {
        let mut i = 0;
        while i < body.len() && raw.len() < total {
            let b = body[i];
            i += 1;
            if b & 0xC0 == 0xC0 {
                let n = (b & 0x3F) as usize;
                let Some(&v) = body.get(i) else { break };
                i += 1;
                let k = n.min(total - raw.len());
                raw.extend(std::iter::repeat_n(v, k));
            } else {
                raw.push(b);
            }
        }
    } else {
        raw.extend_from_slice(&body[..total.min(body.len())]);
    }
    raw.resize(total, 0);

    // 調色盤:VGA 256 色在檔尾;16 色的在檔頭 0x10 起。
    let mut pal: Vec<[u8; 4]> = Vec::new();
    if bpp == 8 && planes == 1 {
        match d.len().checked_sub(769).filter(|&i| i > 0 && d[i] == 0x0C) {
            Some(i) => pal.extend((0..256).map(|c| [d[i + 1 + c * 3], d[i + 2 + c * 3], d[i + 3 + c * 3], 0xFF])),
            None => pal.extend((0..256).map(|c| [c as u8, c as u8, c as u8, 0xFF])),
        }
    } else {
        pal.extend((0..16).map(|c| [d[16 + c * 3], d[17 + c * 3], d[18 + c * 3], 0xFF]));
    }

    let mut img = Image::new(w as u32, h as u32);
    for y in 0..h {
        let line = &raw[y * bpl * planes..(y + 1) * bpl * planes];
        for x in 0..w {
            let c = match (planes, bpp) {
                (3, 8) => [line[x], line[bpl + x], line[2 * bpl + x], 0xFF],
                (1, 8) => pal[line[x] as usize],
                (_, 1) => {
                    // 每個 plane 一個位元,合起來是調色盤索引。
                    let idx = (0..planes).fold(0, |idx, p| if line[p * bpl + x / 8] & (0x80 >> (x % 8)) != 0 { idx | 1 << p } else { idx });
                    pal.get(idx).copied().unwrap_or([0; 4])
                }
                _ => return Err(tr!("PCX: 還沒支援 %d planes x %d bit", planes, bpp)),
            };
            img.set(x as u32, y as u32, c);
        }
    }
    Ok(img)
}
