//! 倚天中文系統的原生點陣字庫,當作全形字的來源(對應 Go 版 `internal/eten`)。
//!
//! 檔案是裸格式:每列 (W+7)/8 個位元組、MSB 在左、由上而下,
//! 每字 stride = rowBytes × H。
//!
//! | 檔案 | 內容 | 尺寸 | 字數 |
//! |---|---|---|---|
//! | STDFONT.15 | 漢字區 | 16×15 | 13094 |
//! | SPCFONT.15 | 全形符號 | 16×15 | 408 |
//! | SPCFSUPP.15 | 符號補充 C6A1–C8FE | 16×15 | 365 |
//!
//! STDFONT 從 A440「一」起,不含 A140–A3BF 的全形標點,所以一定要一起載
//! SPCFONT。

pub mod cjk24;
mod etunpack;

pub use etunpack::unpack as unpack_etunpack;

use std::collections::HashMap;
use std::path::{Path, PathBuf};
use std::sync::{LazyLock, Mutex};

use encoding_rs::BIG5;
use wincv_fnt::Glyph;

/// `.15` 那一組字庫的點陣尺寸。
pub const NATIVE_W: i32 = 16;
pub const NATIVE_H: i32 = 15;
/// `.24` 那一組字庫的點陣尺寸。
pub const NATIVE24_W: i32 = 24;
pub const NATIVE24_H: i32 = 24;

/// 把 Big5 雙位元組換成線性序號。低位元組有兩段(0x40–0x7E 與
/// 0xA1–0xFE),所以不是單純相減。
pub const fn raw_index(hi: u8, lo: u8) -> i32 {
    let off = if lo >= 0x7F { lo as i32 - 0x62 } else { lo as i32 - 0x40 };
    (hi as i32 - 0xA1) * 157 + off
}

const LAST_SPC: i32 = raw_index(0xA3, 0xBF);
const BASE_A440: i32 = raw_index(0xA4, 0x40);
const LAST_COMMON: i32 = raw_index(0xC6, 0x7E);
const BASE_C940: i32 = raw_index(0xC9, 0x40);
const BASE_C6A1: i32 = raw_index(0xC6, 0xA1);
const LAST_C8FE: i32 = raw_index(0xC8, 0xFE);
const N_COMMON: i32 = 5401;

/// 補充區每個碼位在 SPCFSUPP 裡的字模序號,-1 是 Big5 沒有定義的洞。
///
/// 洞在哪不寫死:問 Big5 解碼器,解得開的才佔一個字模序號。
/// 算出來的總數(365)本身就是驗證,與 SPCFSUPP.15 的字模數相符。
struct Supp {
    index: Vec<i32>,
    count: usize,
}

static SUPP: LazyLock<Supp> = LazyLock::new(|| {
    let n = (LAST_C8FE - BASE_C6A1 + 1) as usize;
    let mut index = vec![-1; n];
    let mut next = 0;
    for hi in 0xC6u8..=0xC8 {
        for lo in 0x40u8..=0xFE {
            if lo > 0x7E && lo < 0xA1 {
                continue;
            }
            let code = (hi as u32) << 8 | lo as u32;
            if !(0xC6A1..=0xC8FE).contains(&code) {
                continue;
            }
            let pair = [hi, lo];
            let (s, had_errors) = BIG5.decode_without_bom_handling(&pair);
            let mut it = s.chars();
            let one = matches!((it.next(), it.next()), (Some(c), None) if c != '\u{FFFD}');
            if had_errors || !one {
                continue;
            }
            index[(raw_index(hi, lo) - BASE_C6A1) as usize] = next;
            next += 1;
        }
    }
    Supp { index, count: next as usize }
});

/// 一組同尺寸的倚天字庫。
pub struct Font {
    std: Vec<u8>,
    spc: Option<Vec<u8>>,
    supp: Option<Vec<u8>>,
    pub w: i32,
    pub h: i32,
    stride: usize,
    /// 符號補充區為什麼沒用上。空字串表示用上了,或本來就沒給。
    pub supp_note: String,
    b5: Mutex<HashMap<char, Option<[u8; 2]>>>,
}

/// 載入錯誤。
#[derive(Debug)]
pub struct Error(pub String);

impl std::fmt::Display for Error {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for Error {}

/// 由 SPCFONT 的路徑推出 SPCFSUPP 的路徑(同目錄、同副檔名)。
fn supp_path_for(spc: &Path) -> PathBuf {
    let ext = spc.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_default();
    let name = if ext.is_empty() { "SPCFSUPP".to_string() } else { format!("SPCFSUPP.{ext}") };
    spc.with_file_name(name)
}

impl Font {
    /// 讀入漢字區與符號區。符號補充區在 spc 的同目錄找,找不到當那一區缺字。
    pub fn load(std_path: &Path, spc_path: Option<&Path>, w: i32, h: i32) -> Result<Font, Error> {
        let std = std::fs::read(std_path).map_err(|e| Error(format!("讀 STDFONT: {e}")))?;
        let (mut spc, mut supp) = (None, None);
        if let Some(p) = spc_path {
            spc = Some(std::fs::read(p).map_err(|e| Error(format!("讀 SPCFONT: {e}")))?);
            supp = std::fs::read(supp_path_for(p)).ok();
        }
        Font::load_bytes(std, spc, supp, w, h)
    }

    /// 讀 24×24 漢字區與符號區。`std_path` 可為 ETUNPACK V1.00 壓縮檔(STD.24M/K/L/R/B/S)
    /// 或已解開的裸字模(國喬相容的裸字模也能用同一個介面)。`spc_path` 是裸格式
    /// SPCFONT.24,補充區自動找同目錄的 SPCFSUPP.24。
    pub fn load24(std_path: &Path, spc_path: Option<&Path>) -> Result<Font, Error> {
        let mut std = std::fs::read(std_path).map_err(|e| Error(wincv_i18n::tr!("讀 24 點漢字區: %w", e.to_string())))?;
        if std.starts_with(etunpack::MAGIC) {
            std = etunpack::unpack(&std).map_err(Error)?;
        }
        let (mut spc, mut supp) = (None, None);
        if let Some(p) = spc_path {
            spc = Some(std::fs::read(p).map_err(|e| Error(wincv_i18n::tr!("讀 24 點符號區: %w", e.to_string())))?);
            supp = std::fs::read(supp_path_for(p)).ok();
        }
        Font::load_bytes(std, spc, supp, NATIVE24_W, NATIVE24_H)
    }

    /// 與 [`Font::load`] 相同,但字庫直接給位元組(內嵌字型用)。
    pub fn load_bytes(std: Vec<u8>, spc: Option<Vec<u8>>, supp: Option<Vec<u8>>, w: i32, h: i32) -> Result<Font, Error> {
        let stride = ((w + 7) / 8 * h) as usize;
        if stride == 0 || std.len() % stride != 0 {
            return Err(Error(format!("STDFONT 大小 {} 不是 stride {} 的整數倍", std.len(), stride)));
        }
        let (supp, supp_note) = supp_layout(supp.as_deref().unwrap_or(&[]), stride);
        Ok(Font { std, spc, supp, w, h, stride, supp_note, b5: Mutex::new(HashMap::new()) })
    }

    fn big5_bytes(&self, r: char) -> Option<[u8; 2]> {
        // U+FF5E 與 U+301C 的歧義:Big5 A1E3 在倚天是全形波浪號。
        if r == '～' {
            return Some([0xA1, 0xE3]);
        }
        let mut cache = self.b5.lock().unwrap_or_else(|e| e.into_inner());
        *cache.entry(r).or_insert_with(|| {
            let mut buf = [0u8; 4];
            let (out, _, had_errors) = BIG5.encode(r.encode_utf8(&mut buf));
            (!had_errors && out.len() == 2).then(|| [out[0], out[1]])
        })
    }

    /// 取一個全形字模。Big5 沒有這個字、或字庫沒有那一區,回 None。
    pub fn glyph(&self, r: char) -> Option<Glyph> {
        let [hi, lo] = self.big5_bytes(r)?;
        if !(0xA1..=0xF9).contains(&hi) {
            return None;
        }
        let raw = raw_index(hi, lo);
        let (data, idx): (&[u8], i32) = if raw <= LAST_SPC {
            (self.spc.as_deref()?, raw)
        } else if raw < BASE_A440 {
            return None; // 符號區與漢字區之間的空隙
        } else if raw <= LAST_COMMON {
            (&self.std, raw - BASE_A440)
        } else if raw < BASE_C940 {
            let supp = self.supp.as_deref()?;
            let i = raw - BASE_C6A1;
            let idx = *SUPP.index.get(usize::try_from(i).ok()?)?;
            if idx < 0 {
                return None; // 這個碼位本來就沒有字
            }
            (supp, idx)
        } else {
            (&self.std, N_COMMON + (raw - BASE_C940))
        };
        if idx < 0 {
            return None;
        }
        let off = idx as usize * self.stride;
        let b = data.get(off..off + self.stride)?;
        Some(decode(b, self.w, self.h))
    }
}

/// 認出 SPCFSUPP 是哪一種排法,一律換成「擠掉洞」的排法。
///
/// 倚天 3.53 存 365 個字模(只存有定義的碼位);倚天 2000 存 408 個
/// (一碼位一格)。408 格那種,洞的 43 格必須全空才接受,否則整區不用。
/// 認不出來不是錯誤,原因放在第二個回傳值。
fn supp_layout(supp: &[u8], stride: usize) -> (Option<Vec<u8>>, String) {
    if supp.is_empty() {
        return (None, String::new());
    }
    let s = &*SUPP;
    let n = supp.len() / stride;
    let slots = s.index.len();
    if supp.len() % stride != 0 || (n != s.count && n != slots) {
        return (None, format!("SPCFSUPP 有 {n} 個字模,不是已知的 {} 或 {slots} 格排法,C6A1-C8FE 改用後備字型", s.count));
    }
    if n == s.count {
        return (Some(supp.to_vec()), String::new());
    }
    let mut packed = Vec::with_capacity(s.count * stride);
    for (i, &idx) in s.index.iter().enumerate() {
        let g = &supp[i * stride..(i + 1) * stride];
        if idx >= 0 {
            packed.extend_from_slice(g);
        } else if g.iter().any(|&b| b != 0) {
            return (None, "SPCFSUPP 在 Big5 未定義的碼位上有字形,排法無法確認,C6A1-C8FE 改用後備字型".into());
        }
    }
    (Some(packed), String::new())
}

fn decode(b: &[u8], w: i32, h: i32) -> Glyph {
    let row_bytes = ((w + 7) / 8) as usize;
    let mut g = Glyph::blank(w, h);
    for y in 0..h as usize {
        for x in 0..w as usize {
            if b[y * row_bytes + x / 8] & (0x80 >> (x % 8)) != 0 {
                g.bits[y * w as usize + x] = true;
            }
        }
    }
    g
}

#[cfg(test)]
mod tests {
    use super::*;

    const DIR: &str = "../../../original/eten";

    fn load15() -> Option<Font> {
        let std = Path::new(DIR).join("STDFONT.15");
        if !std.exists() {
            eprintln!("略過:沒有 {}", std.display());
            return None;
        }
        Some(Font::load(&std, Some(&Path::new(DIR).join("SPCFONT.15")), NATIVE_W, NATIVE_H).unwrap())
    }

    #[test]
    fn supp_region_hole_count() {
        assert_eq!(SUPP.count, 365);
        assert_eq!(SUPP.index.len(), 408);
        assert_eq!(SUPP.index.iter().filter(|&&v| v < 0).count(), 43);
    }

    #[test]
    fn supp_hole_positions() {
        let is_hole = |hi: u8, lo: u8| SUPP.index[(raw_index(hi, lo) - BASE_C6A1) as usize] < 0;
        for lo in 0xA5..=0xCC {
            assert!(is_hole(0xC8, lo), "C8{lo:02X} 應該是洞");
        }
        for lo in 0xF2..=0xF4 {
            assert!(is_hole(0xC8, lo), "C8{lo:02X} 應該是洞");
        }
        for (hi, lo) in [(0xC8, 0xA4), (0xC8, 0xCD), (0xC8, 0xF1), (0xC8, 0xF5)] {
            assert!(!is_hole(hi, lo), "{hi:02X}{lo:02X} 不該是洞");
        }
    }

    #[test]
    fn supp_linear_408() {
        let stride = 72;
        let mut packed = vec![0u8; SUPP.count * stride];
        for i in 0..SUPP.count {
            packed[i * stride] = (i % 250 + 1) as u8;
            packed[i * stride + 1] = (i / 250) as u8;
        }
        let mut linear = vec![0u8; SUPP.index.len() * stride];
        for (slot, &idx) in SUPP.index.iter().enumerate() {
            if idx >= 0 {
                let idx = idx as usize;
                linear[slot * stride..(slot + 1) * stride].copy_from_slice(&packed[idx * stride..(idx + 1) * stride]);
            }
        }
        let (got, note) = supp_layout(&linear, stride);
        assert!(note.is_empty());
        assert_eq!(got.as_deref(), Some(&packed[..]));
        let hole = SUPP.index.iter().position(|&v| v < 0).unwrap();
        linear[hole * stride] = 0xFF;
        let (got, note) = supp_layout(&linear, stride);
        assert!(got.is_none() && !note.is_empty());
        let f = Font::load_bytes(vec![0; stride], None, Some(linear), NATIVE24_W, NATIVE24_H).unwrap();
        assert!(!f.supp_note.is_empty(), "補充區認不出來不該讓整套字型載入失敗");
        let (got, note) = supp_layout(&vec![0u8; 100 * stride], stride);
        assert!(got.is_none() && !note.is_empty());
    }

    #[test]
    fn punctuation_from_spc() {
        let Some(f) = load15() else { return };
        for r in "，。！？「」（）《》".chars() {
            let g = f.glyph(r).unwrap_or_else(|| panic!("{r} 缺字"));
            assert!(g.bits.iter().any(|&b| b), "{r} 是空白字模");
        }
    }

    #[test]
    fn supp_glyph_not_shifted() {
        let Some(f) = load15() else { return };
        if f.supp.is_none() {
            return;
        }
        let a = f.glyph('ゃ').expect("「ゃ」取不到字模");
        if let Some(b) = f.glyph('や') {
            assert_ne!(a, b, "「ゃ」與「や」拿到同一個字模,索引偏了");
        }
    }
}
