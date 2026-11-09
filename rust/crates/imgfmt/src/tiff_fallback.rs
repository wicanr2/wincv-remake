//! TIFF 的備援解碼器:image crate(tiff 0.11)解不開的兩種情況。
//!
//! - 調色盤(photometric 3):tiff crate 在判斷顏色型別時就拒絕。1、2、4、8 位元,
//!   壓縮法 不壓縮 / LZW / Deflate / PackBits。
//! - CCITT Group 3(壓縮法 3):tiff crate 只有 Group 4。解碼交給 `fax` crate。
//!
//! 只讀第一個 IFD、strip 排列、單一 plane。對照真值是 Go 的 x/image/tiff 與它的
//! 測試資料(tools/img-parity.sh)。

use std::io::Read;

use crate::Image;

struct Ifd<'a> {
    d: &'a [u8],
    le: bool,
    tags: Vec<(u16, u16, u32, usize)>, // tag, 型別, 個數, 值或位移所在的位置
}

impl Ifd<'_> {
    fn u16_at(&self, o: usize) -> Option<u32> {
        let b = self.d.get(o..o + 2)?;
        Some(if self.le { u16::from_le_bytes([b[0], b[1]]) } else { u16::from_be_bytes([b[0], b[1]]) } as u32)
    }

    fn u32_at(&self, o: usize) -> Option<u32> {
        let b: [u8; 4] = self.d.get(o..o + 4)?.try_into().ok()?;
        Some(if self.le { u32::from_le_bytes(b) } else { u32::from_be_bytes(b) })
    }

    /// 一個標籤的全部值(SHORT 或 LONG)。
    fn vals(&self, tag: u16) -> Option<Vec<u32>> {
        let &(_, typ, n, at) = self.tags.iter().find(|t| t.0 == tag)?;
        let size = match typ {
            3 => 2,
            4 => 4,
            _ => return None,
        };
        let base = if size * n as usize <= 4 { at } else { self.u32_at(at)? as usize };
        (0..n as usize).map(|i| if size == 2 { self.u16_at(base + i * 2) } else { self.u32_at(base + i * 4) }).collect()
    }

    fn val(&self, tag: u16) -> Option<u32> {
        self.vals(tag)?.first().copied()
    }
}

fn parse(d: &[u8]) -> Option<Ifd<'_>> {
    let le = match d.get(..2)? {
        b"II" => true,
        b"MM" => false,
        _ => return None,
    };
    let mut f = Ifd { d, le, tags: Vec::new() };
    let off = f.u32_at(4)? as usize;
    let n = f.u16_at(off)? as usize;
    for k in 0..n {
        let e = off + 2 + k * 12;
        f.tags.push((f.u16_at(e)? as u16, f.u16_at(e + 2)? as u16, f.u32_at(e + 4)?, e + 8));
    }
    Some(f)
}

/// 這個 TIFF 是不是備援解碼器要接手的那兩種。
pub fn handles(d: &[u8]) -> bool {
    parse(d).is_some_and(|f| f.val(262) == Some(3) || f.val(259) == Some(3))
}

fn unpack_strip(f: &Ifd, comp: u32, raw: &[u8]) -> Result<Vec<u8>, String> {
    match comp {
        1 => Ok(raw.to_vec()),
        5 => weezl::decode::Decoder::with_tiff_size_switch(weezl::BitOrder::Msb, 8).decode(raw).map_err(|e| e.to_string()),
        8 | 32946 => {
            let mut out = Vec::new();
            flate2::read::ZlibDecoder::new(raw).read_to_end(&mut out).map_err(|e| e.to_string())?;
            Ok(out)
        }
        32773 => {
            // PackBits:n 在 0..=127 是接下來 n+1 個原樣位元組;-1..=-127 是下一個位元組重複 1-n 次。
            let mut out = Vec::new();
            let mut i = 0;
            while i < raw.len() {
                let n = raw[i] as i8;
                i += 1;
                if n >= 0 {
                    let k = (n as usize + 1).min(raw.len() - i);
                    out.extend_from_slice(&raw[i..i + k]);
                    i += k;
                } else if n != -128 {
                    let Some(&b) = raw.get(i) else { break };
                    out.extend(std::iter::repeat_n(b, 1 - n as isize as usize));
                    i += 1;
                }
            }
            Ok(out)
        }
        c => {
            let _ = f;
            Err(format!("TIFF: 不支援壓縮法 {c}"))
        }
    }
}

pub fn decode(d: &[u8]) -> Result<Image, String> {
    let f = parse(d).ok_or("TIFF: 檔頭壞了")?;
    let (w, h) = (f.val(256).ok_or("TIFF: 沒有寬度")?, f.val(257).ok_or("TIFF: 沒有高度")?);
    if w == 0 || h == 0 || w > 1 << 16 || h > 1 << 16 {
        return Err(format!("TIFF 尺寸不合理 {w}x{h}"));
    }
    let comp = f.val(259).unwrap_or(1);
    let photo = f.val(262).unwrap_or(1);
    let bps = f.val(258).unwrap_or(1);
    let rps = f.val(278).unwrap_or(h).min(h);
    let offs = f.vals(273).ok_or("TIFF: 沒有 strip")?;
    let counts = f.vals(279).ok_or("TIFF: 沒有 strip 長度")?;
    let strip = |i: usize| -> Option<&[u8]> {
        let (o, n) = (*offs.get(i)? as usize, *counts.get(i)? as usize);
        d.get(o..o.checked_add(n)?)
    };
    let mut img = Image::new(w, h);

    if comp == 3 {
        // CCITT Group 3。fax 的 White / Black 是紙上的顏色,與 photometric 無關。
        if bps != 1 {
            return Err(format!("TIFF: CCITT 要 1 位元,這張是 {bps}"));
        }
        let mut y = 0u32;
        for s in 0..offs.len() {
            let Some(raw) = strip(s) else { break };
            fax::decoder::decode_g3(raw.iter().copied(), |line| {
                if y < h {
                    for (x, c) in fax::decoder::pels(line, w as u16).enumerate() {
                        let v = if c == fax::Color::White { 255 } else { 0 };
                        img.set(x as u32, y, [v, v, v, 0xFF]);
                    }
                }
                y += 1;
            });
        }
        return Ok(img);
    }

    // 調色盤。ColorMap 是 3 × 2^bps 個 16 位元值:先全部的 R,再 G,再 B。
    if photo != 3 || !matches!(bps, 1 | 2 | 4 | 8) {
        return Err(format!("TIFF: 不支援 photometric {photo} / {bps} 位元"));
    }
    let cmap = f.vals(320).ok_or("TIFF: 調色盤圖沒有 ColorMap")?;
    let n = cmap.len() / 3;
    let pal: Vec<[u8; 4]> = (0..n).map(|i| [(cmap[i] >> 8) as u8, (cmap[n + i] >> 8) as u8, (cmap[2 * n + i] >> 8) as u8, 0xFF]).collect();
    let row_bytes = (w as usize * bps as usize).div_ceil(8);
    let mut y = 0u32;
    for s in 0..offs.len() {
        let Some(raw) = strip(s) else { break };
        let buf = unpack_strip(&f, comp, raw)?;
        for r in 0..rps {
            if y >= h {
                break;
            }
            let row = buf.get(r as usize * row_bytes..).unwrap_or(&[]);
            for x in 0..w as usize {
                let bit = x * bps as usize;
                let Some(&b) = row.get(bit / 8) else { break };
                let idx = (b >> (8 - bps as usize - bit % 8)) & ((1u16 << bps) - 1) as u8;
                // 調色盤外的索引畫成黑色,其餘照常顯示。Go 的 x/image/tiff 遇到這種
                // 檔案整張報錯(invalid color index)。
                img.set(x as u32, y, pal.get(idx as usize).copied().unwrap_or([0, 0, 0, 0xFF]));
            }
            y += 1;
        }
    }
    Ok(img)
}
