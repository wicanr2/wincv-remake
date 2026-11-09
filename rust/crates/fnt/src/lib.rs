//! 原版隨附的 NE .FON(FNT 2.0)點陣字型(對應 Go 版 `internal/fnt`)。

use std::fmt;

/// 一個字模。`bits` 是點陣;`alpha` 有值時是覆蓋率(向量字型反鋸齒用),
/// 優先於 `bits`。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Glyph {
    pub w: i32,
    pub h: i32,
    pub bits: Vec<bool>,
    pub alpha: Option<Vec<u8>>,
}

impl Glyph {
    /// 空白字模。
    pub fn blank(w: i32, h: i32) -> Glyph {
        Glyph { w, h, bits: vec![false; (w * h).max(0) as usize], alpha: None }
    }

    /// (x, y) 的覆蓋率,0–255。超出範圍是 0。
    pub fn cov(&self, x: i32, y: i32) -> u8 {
        if x < 0 || y < 0 || x >= self.w || y >= self.h {
            return 0;
        }
        let i = (y * self.w + x) as usize;
        match &self.alpha {
            Some(a) => a[i],
            None if self.bits[i] => 255,
            None => 0,
        }
    }

    /// (x, y) 有沒有點。超出範圍是 false。
    pub fn at(&self, x: i32, y: i32) -> bool {
        if x < 0 || y < 0 || x >= self.w || y >= self.h {
            return false;
        }
        self.bits[(y * self.w + x) as usize]
    }
}

/// 解析錯誤。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error(pub &'static str);

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.0)
    }
}

impl std::error::Error for Error {}

/// 一份 .FON 裡的第一個字型。
#[derive(Clone, Debug)]
pub struct Font {
    pub face: String,
    /// 0 表示變寬。
    pub pix_width: i32,
    pub pix_height: i32,
    pub ascent: i32,
    pub first: u8,
    pub last: u8,
    widths: Vec<i32>,
    offsets: Vec<i32>,
    data: Vec<u8>,
    base: usize,
}

fn u16_at(d: &[u8], o: usize) -> Option<usize> {
    d.get(o..o + 2).map(|b| u16::from_le_bytes([b[0], b[1]]) as usize)
}

fn u32_at(d: &[u8], o: usize) -> Option<usize> {
    d.get(o..o + 4).map(|b| u32::from_le_bytes([b[0], b[1], b[2], b[3]]) as usize)
}

/// 從 NE 的 resource table 找出所有 RT_FONT 的位置。
fn ne_font_resources(d: &[u8]) -> Result<Vec<usize>, Error> {
    if d.len() < 0x40 || d[0] != b'M' || d[1] != b'Z' {
        return Err(Error("不是 MZ 檔"));
    }
    let ne = u16_at(d, 0x3C).ok_or(Error("不是 MZ 檔"))?;
    if ne + 0x26 > d.len() || d[ne] != b'N' || d[ne + 1] != b'E' {
        return Err(Error("找不到 NE header"));
    }
    let rt = ne + u16_at(d, ne + 0x24).ok_or(Error("找不到 NE header"))?;
    if rt + 2 > d.len() {
        return Err(Error("resource table 超出檔案"));
    }
    let shift = u16_at(d, rt).unwrap_or(0) as u32;
    let mut p = rt + 2;
    let mut out = Vec::new();
    while p + 8 <= d.len() {
        let tid = u16_at(d, p).unwrap_or(0);
        if tid == 0 {
            break;
        }
        let cnt = u16_at(d, p + 2).unwrap_or(0);
        p += 8;
        let mut i = 0;
        while i < cnt && p + 12 <= d.len() {
            let off = u16_at(d, p).unwrap_or(0).checked_shl(shift).unwrap_or(0);
            if tid & 0x7FFF == 8 {
                out.push(off);
            }
            p += 12;
            i += 1;
        }
    }
    if out.is_empty() {
        return Err(Error("沒有 RT_FONT resource"));
    }
    Ok(out)
}

impl Font {
    /// 解析 .FON,取第一個字型。
    pub fn parse(d: Vec<u8>) -> Result<Font, Error> {
        let offs = ne_font_resources(&d)?;
        Font::parse_at(d, offs[0])
    }

    fn parse_at(d: Vec<u8>, base: usize) -> Result<Font, Error> {
        if base + 0x76 > d.len() {
            return Err(Error("FNT header 超出檔案"));
        }
        let rd16 = |o: usize| u16_at(&d, base + o).unwrap_or(0) as i32;
        let ascent = rd16(0x4A);
        let pix_width = rd16(0x56);
        let pix_height = rd16(0x58);
        let first = d[base + 0x5F];
        let last = d[base + 0x60];
        let face_off = base + u32_at(&d, base + 0x69).unwrap_or(usize::MAX - base);
        if face_off >= d.len() {
            return Err(Error("dfFace 超出檔案"));
        }
        let end = d[face_off..].iter().position(|&b| b == 0).map_or(d.len(), |p| face_off + p);
        let face = String::from_utf8_lossy(&d[face_off..end]).into_owned();
        let n = last as usize + 2 - first as usize;
        if base + 0x76 + n * 4 > d.len() {
            return Err(Error("dfCharTable 超出檔案"));
        }
        let mut widths = Vec::with_capacity(n);
        let mut offsets = Vec::with_capacity(n);
        for i in 0..n {
            widths.push(u16_at(&d, base + 0x76 + i * 4).unwrap_or(0) as i32);
            offsets.push(u16_at(&d, base + 0x76 + i * 4 + 2).unwrap_or(0) as i32);
        }
        Ok(Font { face, pix_width, pix_height, ascent, first, last, widths, offsets, data: d, base })
    }

    /// 定寬字型的格子大小。
    pub fn size(&self) -> (i32, i32) {
        (self.pix_width, self.pix_height)
    }

    /// 取一個字模。不在字型範圍內或寬度是 0 回 None。
    ///
    /// FNT 2.0 的點陣是「直條」存放:每 8 個像素寬一條,一條由上而下
    /// 存 h 個位元組。
    pub fn glyph(&self, code: u8) -> Option<Glyph> {
        if code < self.first || code > self.last {
            return None;
        }
        let i = (code - self.first) as usize;
        let (w, off) = (self.widths[i], self.offsets[i]);
        let h = self.pix_height;
        if w == 0 || h == 0 {
            return None;
        }
        let mut g = Glyph::blank(w, h);
        let cols = (w + 7) / 8;
        for c in 0..cols {
            let col_base = self.base + off as usize + (c * h) as usize;
            for y in 0..h {
                let Some(&b) = self.data.get(col_base + y as usize) else {
                    return Some(g);
                };
                for bit in 0..8 {
                    let x = c * 8 + bit;
                    if x >= w {
                        break;
                    }
                    if b & (0x80 >> bit) != 0 {
                        g.bits[(y * w + x) as usize] = true;
                    }
                }
            }
        }
        Some(g)
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn rejects_non_mz() {
        assert_eq!(Font::parse(vec![0; 0x80]).unwrap_err(), Error("不是 MZ 檔"));
    }

    /// 有原版素材時,對 cvga.fon 驗格子大小(8×15)與一個字模。
    #[test]
    fn cvga_when_available() {
        let Ok(d) = std::fs::read("../../../original/app/cvga.fon") else {
            eprintln!("略過:沒有 original/app/cvga.fon");
            return;
        };
        let f = Font::parse(d).unwrap();
        assert_eq!(f.size(), (8, 15));
        let a = f.glyph(b'A').unwrap();
        assert_eq!((a.w, a.h), (8, 15));
        assert!(a.bits.iter().any(|&b| b));
    }
}
