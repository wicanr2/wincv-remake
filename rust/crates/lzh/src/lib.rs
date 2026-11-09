//! LHA / LZH 壓縮檔(對應 Go 版 `internal/archive/lzh`)。
//!
//! 演算法是 LHA 原始碼(huf.c / decode.c)那一套:LZSS 滑動視窗加
//! 每區塊自帶碼長表的靜態 Huffman。支援 -lh0-、-lhd-、-lz4-、
//! -lh4-、-lh5-、-lh6-、-lh7-;-lh1- 與 -lz5- 用的是另一套,還沒做,
//! 遇到時明確回報不支援。

mod header;
mod huffman;

pub use header::{list, Entry, MTime};
pub use huffman::Bits;

use std::fmt;
use std::io::Read;

/// 解壓縮或標頭錯誤。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error(pub String);

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for Error {}

fn dic_bits(method: &[u8]) -> Option<u32> {
    match method {
        b"-lh4-" => Some(12),
        b"-lh5-" => Some(13),
        b"-lh6-" => Some(15),
        b"-lh7-" => Some(16),
        _ => None,
    }
}

/// 這個壓縮法解得開嗎。
pub fn supported(method: &[u8]) -> bool {
    matches!(method, b"-lh0-" | b"-lhd-" | b"-lz4-") || dic_bits(method).is_some()
}

/// 解一個成員。`src` 從壓縮資料的起點開始。
pub fn decode<R: Read>(src: R, method: &[u8], orig_size: u64) -> Result<Vec<u8>, Error> {
    match method {
        b"-lh0-" | b"-lz4-" => {
            // 不壓縮。檔案比標頭說的短時,有多少給多少(與 Go 版相同)。
            let mut out = Vec::with_capacity(orig_size as usize);
            src.take(orig_size).read_to_end(&mut out).map_err(|e| Error(e.to_string()))?;
            out.resize(orig_size as usize, 0);
            Ok(out)
        }
        b"-lhd-" => Ok(Vec::new()),
        _ => match dic_bits(method) {
            Some(bits) => decode_bits(src, bits, orig_size),
            None => Err(Error(format!("還不支援 {}", String::from_utf8_lossy(method)))),
        },
    }
}

/// 以指定的字典位數解 -lh4- 到 -lh7- 的資料流。
pub fn decode_bits<R: Read>(src: R, bits: u32, orig_size: u64) -> Result<Vec<u8>, Error> {
    if orig_size > 1 << 32 {
        return Err(Error(format!("原始大小不合理: {orig_size}")));
    }
    // lh6(15 位)與 lh7(16 位)的距離位數欄都是 5,不是只有 lh7。
    let pbit = if bits >= 15 { 5 } else { 4 };
    let mut h = huffman::Huffman::new(bits as usize + 1, pbit);
    let mut b = huffman::BitReader::new(src);
    let want = orig_size as usize;
    let mut out = Vec::with_capacity(want);
    while out.len() < want {
        let c = h.decode_c(&mut b).map_err(|e| {
            Error(format!("第 {} 區塊,已輸出 {}/{}: {}", h.blocks, out.len(), want, e.0))
        })?;
        if c <= 0xFF {
            out.push(c as u8);
            continue;
        }
        let length = c - (256 - huffman::THRESHOLD);
        let dist = h.decode_p(&mut b)?;
        let Some(start) = out.len().checked_sub(dist + 1) else {
            return Err(Error(format!("距離超出視窗({},目前只有 {} 個位元組)", dist + 1, out.len())));
        };
        for i in 0..length {
            if out.len() >= want {
                break;
            }
            out.push(out[start + i]);
        }
    }
    Ok(out)
}
