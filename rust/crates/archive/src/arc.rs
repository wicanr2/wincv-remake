//! ARC / PAK 壓縮檔(對應 Go 版 `internal/archive/arc`)。
//!
//! 1980 年代的格式,沒有整體標頭:一個檔案就是一串「每筆各自帶標頭」的記錄,
//! 方法為 0 的那一筆代表結束。
//!
//! | 方法 | 內容 |
//! |---|---|
//! | 1, 2 | 不壓縮(1 是舊版,標頭少一個欄位) |
//! | 3 | packed:0x90 當跳脫的 RLE |
//! | 5, 6 | crunched:固定 12 位的 LZW(6 再加一層 RLE) |
//! | 8 | Crunched:9-13 位的動態 LZW + RLE(ARC 5.x 的預設) |
//! | 9 | Squashed:9-13 位的動態 LZW,不加 RLE |
//!
//! 方法 4(squeezed,CP/M SQ 的 Huffman)與 7 還沒做。造不出測試資料的東西
//! 不寫進來:沒驗過的解碼器比明講不支援更糟。

use std::time::SystemTime;

use wincv_i18n::{t, tr};

/// 壓縮檔裡的一筆。
pub struct File {
    pub name: Vec<u8>,
    pub size: i64,
    pub mod_time: Option<SystemTime>,
    pub method: u8,
    pub data: Vec<u8>,
}

fn le16(b: &[u8]) -> u32 {
    u16::from_le_bytes([b[0], b[1]]) as u32
}

fn le32(b: &[u8]) -> u32 {
    u32::from_le_bytes([b[0], b[1], b[2], b[3]])
}

/// 讀出整份壓縮檔。
pub fn read(data: &[u8]) -> Result<Vec<File>, String> {
    // 自解壓縮檔在第一個 0x1A 之前最多有 3 個位元組。
    let mut at = 0;
    while at < 4 && at < data.len() && data[at] != 0x1A {
        at += 1;
    }
    if at >= data.len() || data[at] != 0x1A {
        return Err(t("不是 ARC 檔(找不到起始標記)"));
    }
    let mut out = Vec::new();
    loop {
        if at + 2 > data.len() || data[at] != 0x1A {
            if out.is_empty() {
                return Err(tr!("在 %d 讀不到記錄標頭", at));
            }
            return Ok(out);
        }
        let method = data[at + 1] & 0x7F;
        let spark_extra = data[at + 1] >> 7 != 0;
        if method == 0 {
            return Ok(out); // 檔尾
        }
        at += 2;
        // 方法 1 的標頭少了「原始大小」那個欄位。
        let fixed = if method == 1 { 13 + 4 + 2 + 2 + 2 } else { 13 + 4 + 2 + 2 + 2 + 4 };
        if at + fixed > data.len() {
            return Err(t("標頭被截斷"));
        }
        let raw = &data[at..at + fixed];
        at += fixed;
        let n = raw[..13].iter().position(|&c| c == 0).unwrap_or(13);
        // 檔名的最高位是旗標位,不是字元的一部分。
        let name: Vec<u8> = raw[..n].iter().map(|&c| (c & 0x7F).to_ascii_lowercase()).collect();
        let b = &raw[13..];
        let comp_size = le32(b) as usize;
        let (date, tm) = (le16(&b[4..]), le16(&b[6..]));
        let orig_size = if method != 1 { le32(&b[10..]) as usize } else { comp_size };
        if spark_extra {
            at += 12; // Spark 變體多帶 12 個位元組
        }
        let shown = wincv_textenc::utf8_lossy_go(&name);
        if at + comp_size > data.len() {
            return Err(tr!("%s: 資料被截斷", shown));
        }
        let body = &data[at..at + comp_size];
        at += comp_size;
        let content = decode(body, method, orig_size).map_err(|e| tr!("%s(方法 %d): %w", shown, method, e))?;
        out.push(File { name, size: orig_size as i64, mod_time: crate::dos_time(date << 16 | tm), method, data: content });
    }
}

fn decode(body: &[u8], method: u8, orig: usize) -> Result<Vec<u8>, String> {
    match method {
        1 | 2 => {
            let mut out = vec![0u8; orig];
            let n = body.len().min(orig);
            out[..n].copy_from_slice(&body[..n]);
            Ok(out)
        }
        3 => {
            let mut w = Rle::new(orig);
            for &c in body {
                w.write(c);
            }
            Ok(w.out)
        }
        5 => lzw(body, 0, false, orig),
        6 => lzw(body, 0, true, orig),
        8 => lzw(body, 12, true, orig),
        9 => lzw(body, 13, false, orig),
        127 => lzw(body, 16, false, orig),
        _ => Err(tr!("還不支援方法 %d", method)),
    }
}

/// ARC 的 RLE 解碼器。
///
/// 0x90 是跳脫字元:`0x90 00` 是一個原樣的 0x90,`0x90 n`(n>0)表示前一個
/// 位元組總共出現 n 次(已經吐出的那一個算在內,所以再補 n-1 個)。
/// 重複之後**不更新 last**:連續兩組 0x90 都是指同一個位元組。
struct Rle {
    out: Vec<u8>,
    last: u8,
    repeating: bool,
    limit: usize,
}

impl Rle {
    fn new(limit: usize) -> Rle {
        Rle { out: Vec::with_capacity(limit), last: 0, repeating: false, limit }
    }

    fn raw(&mut self, c: u8) {
        if self.out.len() < self.limit {
            self.out.push(c);
        }
    }

    fn write(&mut self, c: u8) {
        if self.repeating {
            if c == 0 {
                self.raw(0x90);
            } else {
                for _ in 1..c {
                    self.raw(self.last);
                }
            }
            self.repeating = false;
            return;
        }
        if c == 0x90 {
            self.repeating = true;
            return;
        }
        self.raw(c);
        self.last = c;
    }
}

const UNUSED: i32 = -1;
const REAL_MAX_STR: usize = 65536;

/// ARC 的 LZW 字典。字串用「指標 + 尾字元」串成鏈:ptr 指向前綴的編號,
/// chr 是最後一個字元,ptr1st 是這條鏈的第一個字元(直接記下來,免得每次都要走到底)。
struct Dict {
    ptr: Vec<i32>,
    chr: Vec<i32>,
    ptr1st: Vec<i32>,
    last: i32,
    max_str: i32,
}

impl Dict {
    fn new(max_str: i32, oldver: bool) -> Dict {
        let mut d = Dict {
            ptr: vec![UNUSED; REAL_MAX_STR],
            chr: vec![UNUSED; REAL_MAX_STR],
            ptr1st: vec![UNUSED; REAL_MAX_STR],
            last: -1,
            max_str,
        };
        if !oldver {
            for i in 0..256 {
                d.chr[i] = i as i32;
            }
            d.last = 255;
        }
        d
    }

    fn add(&mut self, old_code: i32, chr: i32) {
        self.last += 1;
        if self.last & self.max_str != 0 {
            // 溢位:停在最後一格,不再長
            self.last = self.max_str - 1;
            return;
        }
        let idx = self.last as usize;
        self.chr[idx] = chr;
        if old_code >= self.max_str {
            return;
        }
        self.ptr[idx] = old_code;
        self.ptr1st[idx] = if self.ptr[old_code as usize] == UNUSED {
            old_code // 指到根,那個根自己就是第一個字元
        } else {
            self.ptr1st[old_code as usize]
        };
    }

    fn first_char(&self, mut code: i32) -> i32 {
        if self.ptr[code as usize] != UNUSED {
            code = self.ptr1st[code as usize];
        }
        self.chr[code as usize]
    }
}

/// 讀變動寬度的碼。
///
/// [雷] 位元是 **LSB 優先**(和 LHA / ARJ 的 MSB 優先相反),而且換寬度時要
/// 「重新對齊」:編碼器每 8 個碼算一組,換寬度前會把那一組補滿,解碼端得照樣
/// 把剩下的碼讀掉丟棄。
struct Codes<'a> {
    input: &'a [u8],
    pos: usize,
    bitbox: u32,
    bits_left: u32,
    code_ofs: u32,
    max_str: i32,
}

impl Codes<'_> {
    fn read(&mut self, num_bits: u32) -> Option<i32> {
        let (mut filled, mut code) = (0u32, 0u32);
        while filled < num_bits {
            if self.bits_left == 0 {
                let &b = self.input.get(self.pos)?;
                self.bitbox = b as u32;
                self.pos += 1;
                self.bits_left = 8;
            }
            let got = self.bits_left.min(num_bits - filled);
            code |= (self.bitbox & ((1 << got) - 1)) << filled;
            self.bitbox >>= got;
            self.bits_left -= got;
            filled += got;
        }
        let code = code as i32;
        if code > self.max_str - 1 {
            return None;
        }
        self.code_ofs = (self.code_ofs + 1) & 7;
        Some(code)
    }

    /// 把目前這一組(8 個碼)剩下的讀完丟棄。
    fn resync(&mut self, old: u32) {
        while self.code_ofs != 0 {
            if self.read(old).is_none() {
                return;
            }
        }
    }
}

/// 解 ARC 的 LZW。`max_bits` 為 0 表示固定 12 位的舊版。
fn lzw(input: &[u8], max_bits: u32, use_rle: bool, orig: usize) -> Result<Vec<u8>, String> {
    let oldver = max_bits == 0;
    let (mut csize, mut max_str) = if oldver { (12u32, 4096i32) } else { (9, 1i32 << max_bits) };
    let orgcsize = csize;
    let mut r = Codes { input, pos: 0, bitbox: 0, bits_left: 0, code_ofs: 0, max_str };
    if max_bits == 12 {
        r.pos += 1; // 方法 8 的第一個位元組是「最大碼寬」,永遠是 12
    }
    if max_bits == 16 && r.pos < input.len() {
        // compress 型(Spark)可以自己改上限
        max_str = 1i32.checked_shl(input[r.pos] as u32).unwrap_or(0).clamp(0, REAL_MAX_STR as i32);
        r.pos += 1;
        r.max_str = max_str;
    }
    let mut st = Dict::new(max_str, oldver);
    let mut w = Rle::new(orig);
    let mut buf: Vec<i32> = Vec::with_capacity(REAL_MAX_STR);
    let emit = |w: &mut Rle, c: i32| if use_rle { w.write(c as u8) } else { w.raw(c as u8) };
    // 把一條鏈倒著吐出來。
    let output = |w: &mut Rle, st: &Dict, buf: &mut Vec<i32>, mut code: i32| {
        buf.clear();
        while st.ptr[code as usize] != UNUSED && (buf.len() as i32) < max_str {
            buf.push(st.chr[code as usize]);
            code = st.ptr[code as usize];
        }
        emit(w, st.chr[code as usize]);
        for &c in buf.iter().rev() {
            emit(w, c);
        }
    };

    let (mut old_code, mut k) = (0i32, 0i32);
    let mut first = true;
    loop {
        let Some(mut new_code) = r.read(csize) else { break };
        let mut noadd = false;
        if first {
            k = new_code;
            first = false;
            noadd = oldver;
        }
        if new_code == 256 && !oldver {
            // [雷] 這個碼**不清空字典**,只是把碼寬降回去:新字串會直接蓋掉
            // 舊的格子。當成 GIF 那種 clear 來處理會從這裡開始整份錯掉。
            st.last = 255;
            r.resync(csize);
            csize = orgcsize;
            match r.read(csize) {
                Some(c) => new_code = c,
                None => break,
            }
        }
        let known = if oldver { st.chr[new_code as usize] != UNUSED } else { new_code <= st.last };
        if known {
            output(&mut w, &st, &mut buf, new_code);
            k = st.first_char(new_code);
        } else {
            // KwKwK:編碼器用了它剛建、這邊還沒建的字串
            output(&mut w, &st, &mut buf, old_code);
            emit(&mut w, k);
        }
        if st.last != max_str - 1 && !noadd {
            st.add(old_code, k);
            if st.last != max_str - 1 && st.last == (1 << csize) - 1 {
                csize += 1;
                r.resync(csize - 1);
            }
        }
        old_code = new_code;
    }
    if w.out.len() != orig {
        return Err(tr!("解出 %d 個位元組,期望 %d", w.out.len(), orig));
    }
    Ok(w.out)
}
