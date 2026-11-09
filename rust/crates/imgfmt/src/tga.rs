//! Truevision TGA。
//!
//! 檔頭 18 bytes:0 idLength、1 colorMapType、2 imageType(1 索引、2 RGB、3 灰階,
//! 9/10/11 是同前但 RLE)、5 colorMapLen(2)、7 colorMapDepth、12 width、14 height、
//! 16 pixelDepth、17 descriptor。
//!
//! descriptor 的 bit5 是「原點在上」。TGA 預設原點在**左下**,忘了處理就會整張
//! 上下顛倒,而顛倒的圖看起來「有解出來」,很容易漏掉。

use wincv_i18n::{t, tr};

use crate::{le16, Image};

pub fn decode(d: &[u8]) -> Result<Image, String> {
    if d.len() < 18 {
        return Err(t("不是 TGA:檔案太短"));
    }
    let id_len = d[0] as usize;
    let cm_type = d[1];
    let img_type = d[2];
    let cm_len = le16(d, 5) as usize;
    let cm_depth = d[7] as usize;
    let (w, h) = (le16(d, 12) as usize, le16(d, 14) as usize);
    let depth = d[16] as usize;
    let top_down = d[17] & 0x20 != 0;
    if w == 0 || h == 0 {
        return Err(tr!("TGA 尺寸不合理 %dx%d", w, h));
    }
    let mut p = 18 + id_len;
    let mut pal = Vec::new();
    if cm_type == 1 {
        let per = cm_depth.div_ceil(8);
        let need = cm_len * per;
        if p + need > d.len() {
            return Err(t("TGA 調色盤超出檔案"));
        }
        pal.extend((0..cm_len).map(|i| read_color(&d[p + i * per..], cm_depth)));
        p += need;
    }
    let per = depth.div_ceil(8);
    if per == 0 {
        return Err("TGA pixelDepth = 0".into());
    }
    let want = w * h * per;
    let mut pix = Vec::with_capacity(want);
    if (9..=11).contains(&img_type) {
        while p < d.len() && pix.len() < want {
            let c = d[p];
            p += 1;
            let n = (c & 0x7F) as usize + 1;
            if c & 0x80 != 0 {
                // 重複
                if p + per > d.len() {
                    break;
                }
                for _ in 0..n {
                    pix.extend_from_slice(&d[p..p + per]);
                }
                p += per;
            } else {
                // 原樣
                let need = (n * per).min(d.len() - p);
                pix.extend_from_slice(&d[p..p + need]);
                p += need;
            }
        }
    } else {
        let need = want.min(d.len().saturating_sub(p));
        pix.extend_from_slice(&d[p.min(d.len())..p.min(d.len()) + need]);
    }
    pix.resize(want.max(pix.len()), 0);

    let mut img = Image::new(w as u32, h as u32);
    for y in 0..h {
        let dy = if top_down { y } else { h - 1 - y };
        for x in 0..w {
            let o = (y * w + x) * per;
            let c = match img_type {
                1 | 9 => pal.get(pix[o] as usize).copied().unwrap_or([0; 4]),
                3 | 11 => [pix[o], pix[o], pix[o], 0xFF],
                _ => read_color(&pix[o..], depth),
            };
            img.set(x as u32, dy as u32, c);
        }
    }
    Ok(img)
}

/// 讀一個 TGA 像素。TGA 的位元組順序是 BGR(A)。
fn read_color(b: &[u8], depth: usize) -> [u8; 4] {
    match depth {
        15 | 16 if b.len() >= 2 => {
            let v = le16(b, 0);
            [(((v >> 10) & 0x1F) << 3) as u8, (((v >> 5) & 0x1F) << 3) as u8, ((v & 0x1F) << 3) as u8, 0xFF]
        }
        24 if b.len() >= 3 => [b[2], b[1], b[0], 0xFF],
        32 if b.len() >= 4 => [b[2], b[1], b[0], b[3]],
        _ => [0; 4],
    }
}
