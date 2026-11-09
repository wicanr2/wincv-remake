//! ARJ 壓縮檔(對應 Go 版 `internal/archive/arj`)。
//!
//! 方法 0 是不壓縮,方法 1-3 用的是與 LHA -lh5- 同一套編碼(unarj 本來就是從
//! LHA 衍生的),所以直接借 `wincv-lzh` 的解碼器。方法 4 是另一套
//! (LZ77 + 固定編碼),寫在 [`decode_m4`]。

use std::time::SystemTime;

use wincv_i18n::{t, tr};

/// 決定距離表的大小:np = DIC_BITS+1、pbit = 5。
///
/// [雷] ARJ 的方法 1-3 **不是** -lh5- 的參數。Huffman 的部分一模一樣,但
/// NP = MAXDICBIT+1 = 17、PBIT = 5,而 -lh5- 是 14 / 4。照 lh5 去解,碼長表
/// 一開始就對不上。`decode_bits` 以 16 位呼叫剛好給出 np=17、pbit=5
/// (參考 arj 3.10 defines.h 的 NP / PBIT;字典是 DICSIZ=26624)。
const DIC_BITS: u32 = 16;
/// 最短的比對長度。LHA 與 ARJ 都是 3。
const THRESHOLD: usize = 3;
/// 方法 4 的視窗大小。參考實作(arj 3.10 decode.c)寫死 32768,與方法 1-3 不同。
const FDIC_SIZ: usize = 32768;

const MAGIC0: u8 = 0x60;
const MAGIC1: u8 = 0xEA;
const FLAG_GARBLED: u8 = 0x01; // 有密碼
const FLAG_EXT_FILE: u8 = 0x08; // 跨片的續檔
const FLAG_PATH_SYM: u8 = 0x10; // 路徑用 / 而不是 \
const TYPE_DIR: u8 = 3;

/// 壓縮檔裡的一筆。
pub struct File {
    pub name: Vec<u8>,
    pub size: i64,
    pub mod_time: Option<SystemTime>,
    pub is_dir: bool,
    pub method: u8,
    pub data: Vec<u8>,
}

struct Header {
    flags: u8,
    method: u8,
    file_type: u8,
    mod_time: Option<SystemTime>,
    comp_size: usize,
    orig_size: i64,
    name: Vec<u8>,
}

fn le16(b: &[u8]) -> usize {
    u16::from_le_bytes([b[0], b[1]]) as usize
}

fn le32(b: &[u8]) -> u32 {
    u32::from_le_bytes([b[0], b[1], b[2], b[3]])
}

/// 讀出整份壓縮檔。
pub fn read(data: &[u8]) -> Result<Vec<File>, String> {
    let (mut at, _) = read_block(data, 0)?; // 主標頭
    let mut out = Vec::new();
    while at < data.len() {
        let (next, h) = read_block(data, at)?;
        let Some(h) = h else { break }; // 長度 0 的標頭 = 檔尾
        let name = wincv_textenc::utf8_lossy_go(&h.name);
        if h.flags & FLAG_GARBLED != 0 {
            return Err(tr!("%s: 有密碼,還不支援", name));
        }
        if h.flags & FLAG_EXT_FILE != 0 {
            return Err(tr!("%s: 是跨片壓縮檔的續檔,還不支援", name));
        }
        let is_dir = h.file_type == TYPE_DIR;
        let mut body = Vec::new();
        if !is_dir {
            let end = next + h.comp_size;
            if end > data.len() {
                return Err(tr!("%s: 資料被截斷", name));
            }
            body = decode(&data[next..end], h.method, h.orig_size).map_err(|e| format!("{name}: {e}"))?;
        }
        out.push(File { name: h.name, size: h.orig_size, mod_time: h.mod_time, is_dir, method: h.method, data: body });
        at = next + h.comp_size;
    }
    Ok(out)
}

/// 讀一個標頭區塊,回傳資料的起點。
///
/// 版面:magic(2) + 基本標頭長度(2) + 基本標頭 + CRC(4) + 一連串擴充標頭
/// (各自是 長度(2) + 內容 + CRC(4),長度 0 表示結束)。
fn read_block(data: &[u8], at: usize) -> Result<(usize, Option<Header>), String> {
    if at + 4 > data.len() {
        return Err(tr!("在 %d 讀不到標頭", at));
    }
    if data[at] != MAGIC0 || data[at + 1] != MAGIC1 {
        return Err(tr!("在 %d 找不到 ARJ 的標記", at));
    }
    let basic = le16(&data[at + 2..]);
    if basic == 0 {
        return Ok((at + 4, None)); // 檔尾
    }
    if basic > 2600 || at + 4 + basic + 4 > data.len() {
        return Err(tr!("標頭長度 %d 不合理", basic));
    }
    let b = &data[at + 4..at + 4 + basic];
    if b.len() < 30 {
        return Err(tr!("標頭太短(%d)", b.len()));
    }
    let first_hdr = b[0] as usize;
    // 檔名與註解接在固定欄位後面,各自以 0 結尾。first_hdr 隨版本不同
    // (通常 30 或 34),所以要照欄位值走不能寫死。
    let p = if first_hdr < 30 || first_hdr > b.len() { 30 } else { first_hdr };
    let end = b[p..].iter().position(|&c| c == 0).map_or(b.len(), |i| p + i);
    let flags = b[4];
    let mut name: Vec<u8> = b[p..end]
        .iter()
        .map(|&c| match c {
            b'/' if flags & FLAG_PATH_SYM != 0 => b'/',
            b'\\' => b'/',
            c => c,
        })
        .collect();
    if name.first() == Some(&b'/') {
        name.remove(0);
    }
    let h = Header {
        flags,
        method: b[5],
        file_type: b[6],
        mod_time: crate::dos_time(le32(&b[8..])),
        comp_size: le32(&b[12..]) as usize,
        orig_size: le32(&b[16..]) as i64,
        name,
    };
    let mut next = at + 4 + basic + 4; // 基本標頭 + 它的 CRC
    loop {
        if next + 2 > data.len() {
            return Err(t("擴充標頭被截斷"));
        }
        let n = le16(&data[next..]);
        next += 2;
        if n == 0 {
            break;
        }
        next += n + 4; // 內容 + CRC
        if next > data.len() {
            return Err(t("擴充標頭被截斷"));
        }
    }
    Ok((next, Some(h)))
}

fn decode(body: &[u8], method: u8, orig_size: i64) -> Result<Vec<u8>, String> {
    let orig = orig_size.max(0) as usize;
    match method {
        0 => {
            if body.len() < orig {
                return Err(tr!("不壓縮的資料只有 %d 個位元組,期望 %d", body.len(), orig));
            }
            Ok(body[..orig].to_vec())
        }
        1..=3 => wincv_lzh::decode_bits(body, DIC_BITS, orig as u64).map_err(|e| t(&e.0)),
        4 => decode_m4(body, orig),
        _ => Err(tr!("還不支援壓縮方法 %d", method)),
    }
}

/// 解方法 4。沒有 Huffman:長度與距離都用「前綴的 1 的個數決定欄位寬度」這種
/// 固定編碼。長度欄位 0-7 位,距離欄位 9-13 位。
fn decode_m4(body: &[u8], orig: usize) -> Result<Vec<u8>, String> {
    let mut b = wincv_lzh::Bits::new(body);
    let mut out = Vec::with_capacity(orig);
    while out.len() < orig {
        // 數前面有幾個 1(最多 7 個),再讀同樣寬度的欄位。第一個位元就是 0 的話
        // 結果是 0,代表「接下來是一個原樣的位元組」。
        let (mut plus, mut pwr, mut width) = (0usize, 1usize, 0u32);
        while width < 7 {
            if b.get(1) == 0 {
                break;
            }
            plus += pwr;
            pwr <<= 1;
            width += 1;
        }
        let c = if width != 0 { b.get(width) as usize } else { 0 } + plus;
        if c == 0 {
            out.push(b.get(8) as u8);
            continue;
        }
        let length = c - 1 + THRESHOLD;
        // 距離:同樣的做法,但寬度從 9 起跳、最多 13。
        let (mut plus, mut pwr, mut width) = (0usize, 1usize << 9, 9u32);
        while width < 13 {
            if b.get(1) == 0 {
                break;
            }
            plus += pwr;
            pwr <<= 1;
            width += 1;
        }
        let dist = b.get(width) as usize + plus;
        let Some(start) = out.len().checked_sub(dist + 1) else {
            return Err(tr!("距離 %d 超出目前的 %d 個位元組", dist + 1, out.len()));
        };
        if dist >= FDIC_SIZ {
            return Err(tr!("距離 %d 超過視窗 %d", dist + 1, FDIC_SIZ));
        }
        for i in 0..length {
            if out.len() >= orig {
                break;
            }
            out.push(out[start + i]);
        }
    }
    Ok(out)
}
