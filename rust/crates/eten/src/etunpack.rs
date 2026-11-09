//! 倚天 ETUNPACK V1.00 的 24×24 漢字區(對應 Go 版 `eten.UnpackETUNPACK`)。
//!
//! 格式證據:ETEN 3.53 的 FILES/ETUNPACK.EXE 與 STD.24M,輸入輸出分別是 657096 與
//! 942768 bytes。資料以三棵靜態 Huffman 樹壓縮(依輸出位元組的序號輪流用),解碼後
//! 再做列間 XOR 還原。只接受完整且自洽的單一字庫資料,不把截斷資料靜默當成缺字。

use std::collections::HashMap;

use wincv_i18n::{t, tr};

use crate::{NATIVE24_H, NATIVE24_W};

pub const MAGIC: &[u8] = b"ETUNPACK V1.00";
const GLYPHS: usize = 13094;
const STRIDE: usize = (NATIVE24_W / 8 * NATIVE24_H) as usize;

struct Bits<'a> {
    data: &'a [u8],
    bit: usize,
}

impl Bits<'_> {
    fn read(&mut self, n: u32) -> Result<u32, String> {
        if self.bit + n as usize > self.data.len() * 8 {
            return Err(t("碼流不足"));
        }
        let mut v = 0u32;
        for _ in 0..n {
            v = v << 1 | ((self.data[self.bit / 8] >> (7 - self.bit % 8)) & 1) as u32;
            self.bit += 1;
        }
        Ok(v)
    }
}

/// 一棵 Huffman 樹:(碼長, 碼字) → 符號。
struct Tree {
    codes: HashMap<(u8, u32), u8>,
    max: u8,
}

fn read_tree(r: &mut Bits) -> Result<Tree, String> {
    let mut t = Tree { codes: HashMap::with_capacity(256), max: 0 };
    for symbol in 0..=255u8 {
        let n = r.read(5)? as u8;
        if n == 0 {
            return Err(wincv_i18n::t("碼長不可為零"));
        }
        let bits = r.read(n as u32)?;
        if t.codes.insert((n, bits), symbol).is_some() {
            return Err(wincv_i18n::t("重複碼字"));
        }
        t.max = t.max.max(n);
    }
    for &(al, ab) in t.codes.keys() {
        for &(bl, bb) in t.codes.keys() {
            if (al, ab) != (bl, bb) && al <= bl && bb >> (bl - al) == ab {
                return Err(wincv_i18n::t("碼字不是前綴自由"));
            }
        }
    }
    Ok(t)
}

impl Tree {
    fn decode(&self, r: &mut Bits) -> Result<u8, String> {
        let mut bits = 0u32;
        for n in 1..=self.max {
            bits = bits << 1 | r.read(1)?;
            if let Some(&v) = self.codes.get(&(n, bits)) {
                return Ok(v);
            }
        }
        Err(t("找不到對應碼字"))
    }
}

fn le32(b: &[u8], o: usize) -> usize {
    u32::from_le_bytes([b[o], b[o + 1], b[o + 2], b[o + 3]]) as usize
}

/// 解開一份 ETUNPACK 壓縮的 24×24 漢字區。
pub fn unpack(input: &[u8]) -> Result<Vec<u8>, String> {
    if input.len() < 0x40 || !input.starts_with(MAGIC) {
        return Err(t("不是 ETUNPACK V1.00 字型"));
    }
    let (dir, stream) = (le32(input, 0x10), le32(input, 0x14));
    if dir + 0x14 > input.len() || stream >= input.len() || dir >= stream {
        return Err(t("ETUNPACK 目錄或碼流位移無效"));
    }
    let (start, end) = (input[dir + 0x12] as usize, input[dir + 0x13] as usize);
    // i+3 必須仍在同一個 24×24 字模內;end=24 會越過最後一列。
    if start > end || end >= NATIVE24_H as usize {
        return Err(tr!("ETUNPACK XOR 列範圍無效: %d..%d", start, end));
    }
    let mut r = Bits { data: input, bit: stream * 8 };
    let mut trees = Vec::with_capacity(3);
    for i in 0..3 {
        trees.push(read_tree(&mut r).map_err(|e| tr!("ETUNPACK Huffman 樹 %d 無效: %w", i, e))?);
    }
    let mut out = vec![0u8; GLYPHS * STRIDE];
    for (i, b) in out.iter_mut().enumerate() {
        *b = trees[i % 3].decode(&mut r).map_err(|e| tr!("ETUNPACK 在輸出 byte %d 截斷或碼字無效: %w", i, e))?;
    }
    for g in out.chunks_exact_mut(STRIDE) {
        for i in start * 3..end * 3 {
            g[i + 3] ^= g[i];
        }
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 產生固定 8 位元 Huffman 碼的合法 ETUNPACK 流(不含任何第三方字形資料)。
    fn build(want: &[u8], start: usize, end: usize) -> Vec<u8> {
        let mut packed = want.to_vec();
        for g in packed.chunks_exact_mut(STRIDE) {
            for i in (start * 3..end * 3).rev() {
                g[i + 3] ^= g[i];
            }
        }
        let mut bits: Vec<u8> = Vec::new();
        let mut n = 0usize;
        let mut write = |v: u32, w: u32| {
            for shift in (0..w).rev() {
                if n % 8 == 0 {
                    bits.push(0);
                }
                if v & (1 << shift) != 0 {
                    *bits.last_mut().unwrap() |= 0x80 >> (n % 8);
                }
                n += 1;
            }
        };
        for _ in 0..3 {
            for s in 0..256 {
                write(8, 5);
                write(s, 8);
            }
        }
        for &b in &packed {
            write(b as u32, 8);
        }
        let mut out = vec![0u8; 0x40];
        out[..MAGIC.len()].copy_from_slice(MAGIC);
        out[0x10..0x14].copy_from_slice(&0x20u32.to_le_bytes());
        out[0x14..0x18].copy_from_slice(&0x40u32.to_le_bytes());
        out[0x24..0x2e].copy_from_slice(b"stdfont.24");
        out[0x32] = start as u8;
        out[0x33] = end as u8;
        out.extend(bits);
        out
    }

    #[test]
    fn round_trip() {
        let want: Vec<u8> = (0..GLYPHS * STRIDE).map(|i| (i * 37 + i / 17) as u8).collect();
        assert_eq!(unpack(&build(&want, 0, 22)).unwrap(), want);
    }

    #[test]
    fn rejects_invalid_input() {
        assert!(unpack(MAGIC).is_err());
        let mut bad = build(&vec![0; GLYPHS * STRIDE], 0, 22);
        bad[0] = b'X';
        assert!(unpack(&bad).is_err());
    }

    /// 倚天 3.53 的 STD.24M(外部字型不入版控,有才跑)。雜湊與 Go 版的收據相同。
    #[test]
    fn eten353_receipt() {
        use sha2::{Digest, Sha256};
        let p = std::path::Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../original/eten/STD.24M");
        let Ok(input) = std::fs::read(&p) else { return };
        let hex = |b: &[u8]| b.iter().map(|x| format!("{x:02x}")).collect::<String>();
        if hex(&Sha256::digest(&input)) != "347ae2655807fc250a18673e6634a363dfba7feee6c3355b886a0810d2d9c030" {
            return; // 不是驗證過的那一份
        }
        let out = unpack(&input).unwrap();
        assert_eq!(hex(&Sha256::digest(&out)), "65aafa2a59baa73e835dbfebeeaf6c99ae85db52b3bc8e184cbc1d3f7bd364e6");
    }
}
