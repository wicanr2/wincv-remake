//! ACE 壓縮檔(WinACE),對應 Go 版 `internal/archive/ace`。
//!
//! 原版 WinCV 自己不解 ACE,是載入 WinACE 原廠的 unace.dll / unacev2.dll,
//! image 裡只有綁定層。Go 版照公開規格自己寫,這裡逐段搬過來:
//!
//! - 規格:Marcel Lemke,〈Technical information of the archiver ACE v1.2〉(1998)
//! - 參照:droe/acefile(BSD 2-Clause,純 Python)。多數拿來對答案,但
//!   [`ace_sort`] 是逐行移植的,著作權聲明見 repo 根目錄的 NOTICE
//! - 測資:droe/acefile-testdata
//!
//! 支援 stored、ACE 1.0 的 LZ77、ACE 2.0 blocked 模式底下的 LZ77 / LZ77_DELTA /
//! LZ77_EXE。SOUND 與 PIC 兩種模式還沒做,遇到會明確回報而不是解出垃圾。
//! 加密與跨片壓縮檔同樣先擋下。

use std::time::SystemTime;

use wincv_i18n::{t, tr};

// 壓縮法。
const COMP_STORED: u8 = 0;
const COMP_LZ77: u8 = 1; // ACE 1.0
const COMP_BLOCKED: u8 = 2; // ACE 2.0

// blocked 模式底下的子模式。
const MODE_LZ77: u32 = 0;
const MODE_LZ77_DELTA: u32 = 1; // 先重排位元組再取差值
const MODE_LZ77_EXE: u32 = 2; // 先把 x86 JMP/CALL 的目標改成相對值
const MODE_PIC: u32 = 7;

fn mode_name(m: u32) -> String {
    const NAMES: [&str; 8] = ["LZ77", "LZ77_DELTA", "LZ77_EXE", "SOUND_8", "SOUND_16", "SOUND_32A", "SOUND_32B", "PIC"];
    match NAMES.get(m as usize) {
        Some(n) => n.to_string(),
        None => tr!("未知(%d)", m),
    }
}

#[derive(Clone, Copy)]
struct Mode {
    mode: u32,
    delta_dist: usize,
    delta_len: usize,
    exe_mode: u32,
}

fn read_mode(b: &mut Bits) -> Result<Mode, String> {
    let mut m = Mode { mode: b.read(8)?, delta_dist: 0, delta_len: 0, exe_mode: 0 };
    match m.mode {
        MODE_LZ77_DELTA => {
            m.delta_dist = b.read(8)? as usize;
            m.delta_len = b.read(17)? as usize;
        }
        MODE_LZ77_EXE => m.exe_mode = b.read(8)?,
        _ => {}
    }
    Ok(m)
}

// 標頭型別與旗標。
const TYPE_MAIN: u8 = 0;
const TYPE_FILE32: u8 = 1;
const TYPE_FILE64: u8 = 3;
const FLAG_ADD_SIZE: u16 = 1 << 0;
const FLAG_64BIT: u16 = 1 << 2;
const FLAG_CONT_PREV: u16 = 1 << 12;
const FLAG_CONT_NEXT: u16 = 1 << 13;
const FLAG_PASSWORD: u16 = 1 << 14;

const MAGIC: &[u8] = b"**ACE**";

/// 壓縮檔裡的一筆。
pub struct File {
    pub name: Vec<u8>,
    pub size: i64,
    pub mod_time: Option<SystemTime>,
    pub is_dir: bool,
    pub crc32: u32,
    pub data: Vec<u8>,
    pub comp_type: u8,
}

/// ACE 用的 CRC-32:多項式與位元序都與標準相同,但**最後不做反相**。
fn ace_crc32(b: &[u8]) -> u32 {
    crc32fast::hash(b) ^ 0xFFFF_FFFF
}

fn le16(b: &[u8]) -> u16 {
    u16::from_le_bytes([b[0], b[1]])
}

fn le32(b: &[u8]) -> u32 {
    u32::from_le_bytes([b[0], b[1], b[2], b[3]])
}

fn le64(b: &[u8]) -> u64 {
    u64::from_le_bytes(b[..8].try_into().unwrap())
}

/// 讀出整份壓縮檔。
pub fn read(data: &[u8]) -> Result<Vec<File>, String> {
    let mut at = find_main(data)?;
    let mut lz = Lz77::new();
    let mut out = Vec::new();
    while at < data.len() {
        let (hdr, next) = match parse_header(data, at) {
            Ok(v) => v,
            Err(e) => {
                if !out.is_empty() {
                    return Ok(out); // 尾端可能有填補,已讀到的先給
                }
                return Err(e);
            }
        };
        if let Some(h) = hdr {
            let name = wincv_textenc::utf8_lossy_go(&h.name);
            out.push(member(&mut lz, data, h).map_err(|e| format!("{name}: {e}"))?);
        }
        if next <= at {
            return Err(t("標頭位置沒有前進,可能是壞檔"));
        }
        at = next;
    }
    Ok(out)
}

fn find_main(data: &[u8]) -> Result<usize, String> {
    if data.len() >= 14 && &data[7..14] == MAGIC {
        return Ok(0);
    }
    // 自解壓縮檔:主標頭藏在 stub 後面
    let limit = data.len().min(512 * 1024);
    let mut i = 8;
    while i + 7 <= limit {
        if &data[i..i + 7] == MAGIC {
            return Ok(i - 7);
        }
        i += 1;
    }
    Err(t("不是 ACE 檔(找不到 **ACE** 標記)"))
}

struct Header {
    flags: u16,
    pack_size: u64,
    orig_size: u64,
    mod_time: Option<SystemTime>,
    attribs: u32,
    crc32: u32,
    comp_type: u8,
    params: u16,
    name: Vec<u8>,
    data_at: usize,
}

/// 讀一個標頭區塊。回傳的標頭為 None 表示這一塊不是檔案(主標頭、修復記錄等),
/// 但仍然要照回傳的位置往下走。
fn parse_header(data: &[u8], at: usize) -> Result<(Option<Header>, usize), String> {
    let short = || t("標頭被截斷");
    if at + 4 > data.len() {
        return Err(short());
    }
    let hcrc = le16(&data[at..]);
    let hsize = le16(&data[at + 2..]) as usize;
    let body = at + 4;
    if body + hsize > data.len() {
        return Err(short());
    }
    let buf = &data[body..body + hsize];
    if (ace_crc32(buf) & 0xFFFF) as u16 != hcrc {
        return Err(t("標頭 CRC 不符"));
    }
    if buf.len() < 3 {
        return Err(t("標頭太短"));
    }
    let kind = buf[0];
    let flags = le16(&buf[1..]);
    let mut i = 3;
    let after = body + hsize;
    match kind {
        TYPE_MAIN => Ok((None, after)),
        TYPE_FILE32 | TYPE_FILE64 => {
            let (pack_size, orig_size);
            if flags & FLAG_64BIT != 0 {
                if i + 16 > buf.len() {
                    return Err(short());
                }
                (pack_size, orig_size) = (le64(&buf[i..]), le64(&buf[i + 8..]));
                i += 16;
            } else {
                if i + 8 > buf.len() {
                    return Err(short());
                }
                (pack_size, orig_size) = (le32(&buf[i..]) as u64, le32(&buf[i + 4..]) as u64);
                i += 8;
            }
            if i + 20 > buf.len() {
                return Err(short());
            }
            let fnsz = le16(&buf[i + 18..]) as usize;
            let h0 = i;
            i += 20;
            if i + fnsz > buf.len() {
                return Err(short());
            }
            // 路徑分隔字元統一成 /,去掉開頭的一個 /。
            let mut name: Vec<u8> = buf[i..i + fnsz].iter().map(|&c| if c == b'\\' { b'/' } else { c }).collect();
            if name.first() == Some(&b'/') {
                name.remove(0);
            }
            let h = Header {
                flags,
                pack_size,
                orig_size,
                mod_time: crate::dos_time(le32(&buf[h0..])),
                attribs: le32(&buf[h0 + 4..]),
                crc32: le32(&buf[h0 + 8..]),
                comp_type: buf[h0 + 12],
                params: le16(&buf[h0 + 14..]),
                name,
                data_at: after,
            };
            let next = after.checked_add(usize::try_from(pack_size).map_err(|_| short())?).ok_or_else(short)?;
            Ok((Some(h), next))
        }
        _ => {
            // 其他標頭(修復記錄等):照 addsize 跳過
            let mut addsz = 0u64;
            if flags & FLAG_ADD_SIZE != 0 {
                if flags & FLAG_64BIT != 0 && i + 8 <= buf.len() {
                    addsz = le64(&buf[i..]);
                } else if i + 4 <= buf.len() {
                    addsz = le32(&buf[i..]) as u64;
                }
            }
            let next = after.checked_add(usize::try_from(addsz).map_err(|_| short())?).ok_or_else(short)?;
            Ok((None, next))
        }
    }
}

/// 由標頭的 params 欄位算出這個成員要多大的字典。
fn dic_size(params: u16) -> usize {
    let bits = ((params & 0x0F) as u32 + MIN_DIC_BITS).min(MAX_DIC_BITS);
    1 << bits
}

fn member(lz: &mut Lz77, data: &[u8], h: Header) -> Result<File, String> {
    let mut f = File {
        name: h.name,
        size: h.orig_size as i64,
        mod_time: h.mod_time,
        is_dir: h.attribs & 0x10 != 0,
        crc32: h.crc32,
        data: Vec::new(),
        comp_type: h.comp_type,
    };
    if h.flags & FLAG_PASSWORD != 0 {
        return Err(t("有密碼,還不支援"));
    }
    if h.flags & (FLAG_CONT_PREV | FLAG_CONT_NEXT) != 0 {
        return Err(t("跨片壓縮檔,還不支援"));
    }
    if f.is_dir || h.orig_size == 0 {
        return Ok(f);
    }
    let end = h.data_at as u64 + h.pack_size;
    if end > data.len() as u64 {
        return Err(t("資料被截斷"));
    }
    let body = &data[h.data_at..end as usize];
    lz.set_dic_size(dic_size(h.params));
    let size = h.orig_size as usize;
    let out = match h.comp_type {
        COMP_STORED => {
            if body.len() < size {
                return Err(tr!("不壓縮的資料只有 %d 個位元組,期望 %d", body.len(), size));
            }
            lz.out.extend_from_slice(&body[..size]);
            body[..size].to_vec()
        }
        COMP_LZ77 => decode_lz77(lz, body, size)?,
        COMP_BLOCKED => decode_blocked(lz, body, size)?,
        c => return Err(tr!("還不支援壓縮法 %d", c)),
    };
    let got = ace_crc32(&out);
    if got != h.crc32 {
        return Err(tr!("CRC 不符(算出 %08X,標頭寫 %08X)", got, h.crc32));
    }
    f.data = out;
    Ok(f)
}

/// ACE 1.0 的路徑:整份就是一段 LZ77,沒有模式切換。
fn decode_lz77(lz: &mut Lz77, body: &[u8], size: usize) -> Result<Vec<u8>, String> {
    let mut b = Bits::new(body);
    lz.reinit();
    let mut out = Vec::with_capacity(size);
    while out.len() < size {
        let (chunk, next) = lz.read(&mut b, size - out.len())?;
        if next.is_some() {
            return Err(t("ACE 1.0 的 LZ77 不該出現模式切換碼"));
        }
        if chunk.is_empty() {
            return Err(t("沒有進展,可能是壞檔"));
        }
        out.extend_from_slice(&chunk);
    }
    Ok(out)
}

/// ACE 2.0 的路徑:資料切成一段一段,每段前面可以插入一個模式切換碼,
/// 換成別的前處理方式。
fn decode_blocked(lz: &mut Lz77, body: &[u8], size: usize) -> Result<Vec<u8>, String> {
    let mut b = Bits::new(body);
    lz.reinit();
    let mut out: Vec<u8> = Vec::with_capacity(size);
    let mut exe_leftover: Vec<u8> = Vec::new();
    let mut last_delta = 0u8;
    let mut mode = Mode { mode: MODE_LZ77, delta_dist: 0, delta_len: 0, exe_mode: 0 };
    let mut next: Option<Mode> = None;
    let no_progress = || t("沒有進展,可能是壞檔");
    while out.len() < size {
        if let Some(m) = next.take() {
            mode = m;
        }
        let chunk = match mode.mode {
            MODE_LZ77_DELTA => {
                let (chunk, nm) = read_delta(lz, &mut b, &mode, &mut last_delta)?;
                next = nm;
                if chunk.is_empty() && next.is_some() {
                    continue;
                }
                chunk
            }
            MODE_LZ77 | MODE_LZ77_EXE => {
                let mut buf = std::mem::take(&mut exe_leftover);
                let want = size as i64 - out.len() as i64 - buf.len() as i64;
                if want <= 0 {
                    return Err(no_progress());
                }
                let (c, nm) = lz.read(&mut b, want as usize)?;
                next = nm;
                buf.extend_from_slice(&c);
                if mode.mode == MODE_LZ77_EXE {
                    exe_leftover = patch_exe(&mut buf, out.len(), mode.exe_mode);
                }
                buf
            }
            m if m <= MODE_PIC => return Err(tr!("還不支援 %s 模式", mode_name(m))),
            m => return Err(tr!("認不得的模式 %d", m)),
        };
        if chunk.is_empty() && next.is_none() {
            return Err(no_progress());
        }
        out.extend_from_slice(&chunk);
    }
    Ok(out)
}

/// 解 LZ77_DELTA:先解出一整塊差值資料,累加還原成位元組值,再依 delta_dist
/// 把交錯存放的幾個平面重新編織回原本的順序。
fn read_delta(lz: &mut Lz77, b: &mut Bits, mode: &Mode, last: &mut u8) -> Result<(Vec<u8>, Option<Mode>), String> {
    let mut next = None;
    let mut delta: Vec<u8> = Vec::with_capacity(mode.delta_len);
    while delta.len() < mode.delta_len {
        let (chunk, nm) = lz.read(b, mode.delta_len - delta.len())?;
        delta.extend_from_slice(&chunk);
        if nm.is_some() {
            next = nm;
            if delta.is_empty() {
                return Ok((Vec::new(), next));
            }
            break;
        }
        if chunk.is_empty() {
            return Err(t("DELTA 沒有進展"));
        }
    }
    for d in delta.iter_mut() {
        *d = d.wrapping_add(*last);
        *last = *d;
    }
    if mode.delta_dist == 0 {
        return Err(t("DELTA 的 dist 是 0"));
    }
    let plane = mode.delta_len / mode.delta_dist;
    let mut out = Vec::with_capacity(delta.len());
    if plane > 0 {
        for pos in 0..plane {
            let mut p = 0;
            while p < mode.delta_len {
                if let Some(&c) = delta.get(p + pos) {
                    out.push(c);
                }
                p += plane;
            }
        }
    }
    Ok((out, next))
}

/// 還原 LZ77_EXE 的前處理:編碼時把 x86 的 CALL/JMP 目標改成相對於檔案位移
/// 的值,解碼時要減回去。
///
/// 就地處理 `buf`,回傳尾端最多 4 個位元組的殘留(從 `buf` 切掉):一條機器
/// 指令可能跨在兩個區塊的交界上,opcode 在這一段、位移在下一段。
/// 位移的寬度是格式定的(16 或 32 位元),一律用固定寬度的型別環繞。
fn patch_exe(buf: &mut Vec<u8>, file_pos: usize, exe_mode: u32) -> Vec<u8> {
    let mut i = 0;
    while i < buf.len() {
        if i + 4 >= buf.len() {
            break;
        }
        match buf[i] {
            0xE8 => {
                // CALL rel16/rel32
                let pos = (file_pos + i) as u32;
                if exe_mode == 0 {
                    let rel = u16::from_le_bytes([buf[i + 1], buf[i + 2]]).wrapping_sub(pos as u16);
                    buf[i + 1..i + 3].copy_from_slice(&rel.to_le_bytes());
                    i += 2;
                } else {
                    let rel = le32(&buf[i + 1..]).wrapping_sub(pos);
                    buf[i + 1..i + 5].copy_from_slice(&rel.to_le_bytes());
                    i += 4;
                }
            }
            0xE9 => {
                // JMP rel16
                let pos = (file_pos + i) as u16;
                let rel = u16::from_le_bytes([buf[i + 1], buf[i + 2]]).wrapping_sub(pos);
                buf[i + 1..i + 3].copy_from_slice(&rel.to_le_bytes());
                i += 2;
            }
            _ => {}
        }
        i += 1;
    }
    while i < buf.len() {
        if buf[i] == 0xE8 || buf[i] == 0xE9 {
            return buf.split_off(i);
        }
        i += 1;
    }
    Vec::new()
}

// ---- 位元讀取 ----

/// ACE 的位元讀取器。
///
/// [雷] 位元序很特別:資料先以**小端序**每 4 個位元組讀成一個 u32,再從那個
/// u32 裡**由高位往低位**取位元。當成單純的 MSB-first 位元流去讀,每 4 個
/// 位元組就會錯一次序。
struct Bits {
    words: Vec<u32>,
    pos: usize, // 以位元計
    nbits: usize,
}

/// 從一個 32 位元字裡取 `length` 個位元,`start` 是自最高位起算的位移。
fn get_bits(v: u32, start: u32, length: u32) -> u32 {
    if length == 0 {
        return 0;
    }
    let mask = (u32::MAX << (32 - length)) >> start;
    (v & mask) >> (32 - length - start)
}

impl Bits {
    fn new(data: &[u8]) -> Bits {
        // 長度補到 4 的倍數:規格要求壓縮資料以 32 位元為單位,最後不足的部分補 0。
        let words: Vec<u32> = data
            .chunks(4)
            .map(|c| {
                let mut b = [0u8; 4];
                b[..c.len()].copy_from_slice(c);
                u32::from_le_bytes(b)
            })
            .collect();
        let nbits = words.len() * 32;
        Bits { words, pos: 0, nbits }
    }

    fn word(&self, i: usize) -> u32 {
        self.words.get(i).copied().unwrap_or(0)
    }

    /// 看接下來的 n 個位元但不前進。允許看過檔尾最多 31 個位元,補 0:最後一個碼
    /// 可能跨過實體結尾,參考實作也是這樣處理。一次最多 31 位,ACE 用到的最寬
    /// 欄位是 LZ77_DELTA 的 17 位,所以跨字只會跨一次。
    fn peek(&self, n: u32) -> Result<u32, String> {
        if n == 0 {
            return Ok(0);
        }
        if n > 31 {
            return Err(tr!("一次讀 %d 位超過上限", n));
        }
        if self.pos + n as usize > self.nbits + 31 {
            return Err(t("位元流讀過頭"));
        }
        let idx = self.pos / 32;
        let off = (self.pos % 32) as u32;
        let got = n.min(32 - off);
        let mut res = get_bits(self.word(idx), off, got);
        if n > got {
            res <<= n - got;
            res += get_bits(self.word((self.pos + got as usize) / 32), 0, n - got);
        }
        Ok(res)
    }

    fn skip(&mut self, n: u32) {
        self.pos += n as usize;
    }

    fn read(&mut self, n: u32) -> Result<u32, String> {
        let v = self.peek(n)?;
        self.skip(n);
        Ok(v)
    }

    /// 讀一個已知位元寬的無號整數。最高位不寫進位元流,因為它一定是 1。
    fn read_known_width_uint(&mut self, bits: u32) -> Result<u32, String> {
        if bits < 2 {
            return Ok(bits);
        }
        Ok(self.read(bits - 1)? + (1 << (bits - 1)))
    }
}

// ---- Huffman ----

const WIDTH_WIDTH_BITS: u32 = 3;
const MAX_WIDTH_WIDTH: u32 = (1 << WIDTH_WIDTH_BITS) - 1;

/// 解碼用的 Huffman 表。不是走樹,而是「一次看 max_width 個位元直接查表」:
/// codes 的長度是 2^max_width,每個碼依它的實際寬度重複 2^(max_width-寬度) 次。
struct Tree {
    codes: Vec<usize>,
    widths: Vec<usize>,
    max_width: u32,
}

impl Tree {
    fn read_symbol(&self, b: &mut Bits) -> Result<usize, String> {
        let v = b.peek(self.max_width)? as usize;
        let Some(&sym) = self.codes.get(v) else {
            return Err(t("Huffman 查表越界"));
        };
        b.skip(self.widths[sym] as u32);
        Ok(sym)
    }
}

/// ACE 專用的排序:依 keys 由大到小,同時搬動 values。
///
/// [雷] **不能換成一般的排序。** 這個 quicksort 對相等的鍵是不穩定的,而且是
/// 以特定的方式不穩定;碼的指派順序直接取決於相等寬度的符號最後排成什麼次序。
/// 換成 `sort_by`(即使加上 tie-break)會得到一套自洽但與編碼器對不上的碼,
/// 解出來是垃圾而不是錯誤。這裡逐行照參考實作(droe/acefile,BSD 2-Clause)
/// 搬過來;依其授權條款,著作權聲明放在 repo 根目錄的 NOTICE。
fn ace_sort(keys: &mut [usize], values: &mut [usize]) {
    fn swap(keys: &mut [usize], values: &mut [usize], a: usize, b: usize) {
        keys.swap(a, b);
        values.swap(a, b);
    }
    fn sub(keys: &mut [usize], values: &mut [usize], left: isize, right: isize) {
        let (mut nl, mut nr) = (left, right);
        let m = keys[right as usize];
        loop {
            while keys[nl as usize] > m {
                nl += 1;
            }
            while keys[nr as usize] < m {
                nr -= 1;
            }
            if nl <= nr {
                swap(keys, values, nl as usize, nr as usize);
                nl += 1;
                nr -= 1;
            }
            if nl >= nr {
                break;
            }
        }
        if left < nr {
            if left < nr - 1 {
                sub(keys, values, left, nr);
            } else if keys[left as usize] < keys[nr as usize] {
                swap(keys, values, left as usize, nr as usize);
            }
        }
        if right > nl {
            if nl < right - 1 {
                sub(keys, values, nl, right);
            } else if keys[nl as usize] < keys[right as usize] {
                swap(keys, values, nl as usize, right as usize);
            }
        }
    }
    if !keys.is_empty() {
        sub(keys, values, 0, keys.len() as isize - 1);
    }
}

/// 由每個符號的碼長算出查表用的碼表。
fn make_tree(mut widths: Vec<usize>, max_width: u32) -> Result<Tree, String> {
    if widths.is_empty() {
        return Err(t("Huffman 查表越界"));
    }
    let mut syms: Vec<usize> = (0..widths.len()).collect();
    let mut sorted = widths.clone();
    ace_sort(&mut sorted, &mut syms);
    let mut used = sorted.iter().take_while(|&&w| w != 0).count();
    if used < 2 {
        widths[syms[0]] = 1;
        sorted[0] = 1;
        if used == 0 {
            used = 1;
        }
    }
    let max_codes = 1usize << max_width;
    let mut codes = Vec::with_capacity(max_codes);
    for i in (0..used).rev() {
        let w = sorted[i];
        if w as u32 > max_width {
            return Err(tr!("碼長 %d 超過上限 %d", w, max_width));
        }
        codes.extend(std::iter::repeat_n(syms[i], 1 << (max_width - w as u32)));
        if codes.len() > max_codes {
            return Err(tr!("碼表長度超過 %d", max_codes));
        }
    }
    Ok(Tree { codes, widths, max_width })
}

/// 從位元流讀一棵 Huffman 表。表本身也是壓縮的:先讀一棵「碼長的碼長表」,
/// 再用它解出各符號的碼長,而那串碼長還是差分編碼的(對 upper 取模)。
fn read_tree(b: &mut Bits, max_width: u32, num_codes: usize) -> Result<Tree, String> {
    let num_widths = (b.read(9)? as usize + 1).min(num_codes + 1);
    let lower = b.read(4)? as usize;
    let upper = b.read(4)? as usize;
    let mut ww = Vec::with_capacity(upper + 1);
    for _ in 0..=upper {
        ww.push(b.read(WIDTH_WIDTH_BITS)? as usize);
    }
    let wtree = make_tree(ww, MAX_WIDTH_WIDTH).map_err(|e| tr!("碼長表: %w", e))?;
    let mut widths = Vec::with_capacity(num_widths);
    while widths.len() < num_widths {
        let sym = wtree.read_symbol(b)?;
        if sym < upper {
            widths.push(sym);
            continue;
        }
        let len = (b.read(4)? as usize + 4).min(num_widths - widths.len());
        widths.extend(std::iter::repeat_n(0, len));
    }
    if upper > 0 {
        for i in 1..widths.len() {
            widths[i] = (widths[i] + widths[i - 1]) % upper;
        }
    }
    for w in widths.iter_mut() {
        if *w > 0 {
            *w += lower;
        }
    }
    make_tree(widths, max_width)
}

// ---- LZ77 ----
//
// 符號配置:
//   0..255   原樣的位元組
//   256..259 比對,距離取自最近用過的四個距離
//   260..282 比對,距離的位元寬由符號決定(0..22 位)
//   283      模式切換碼

const MAX_CODE_WIDTH: u32 = 11;
const MAX_DIST_AT_LEN2: usize = 255;
const MAX_DIST_AT_LEN3: usize = 8191;
const MIN_DIC_BITS: u32 = 10;
const MAX_DIC_BITS: u32 = 22;
const MIN_DIC_SIZE: usize = 1 << MIN_DIC_BITS;
const MAX_DIC_SIZE: usize = 1 << MAX_DIC_BITS;
const TYPE_CODE: usize = 260 + MAX_DIC_BITS as usize + 1;
const NUM_MAIN_CODES: usize = 260 + MAX_DIC_BITS as usize + 2;
const NUM_LEN_CODES: usize = 256 - 1;

/// 最近用過的四個距離。取用之後那個距離會被移到最新,所以連續回頭引用同一段時碼會很短。
#[derive(Default)]
struct DistHist([usize; 4]);

impl DistHist {
    fn append(&mut self, v: usize) {
        self.0.copy_within(1.., 0);
        self.0[3] = v;
    }

    fn retrieve(&mut self, off: usize) -> usize {
        let i = 3 - off;
        let v = self.0[i];
        self.0.copy_within(i + 1.., i);
        self.0[3] = v;
        v
    }
}

/// 讀一個個 Huffman 區塊。主碼與長度碼各有一棵表,區塊用完時再讀下一組表。
#[derive(Default)]
struct Syms {
    main: Option<Tree>,
    length: Option<Tree>,
    /// 與 Go 版一樣用有號數:區塊長度寫 0 時會減成負數,之後不再重讀表。
    to_read: i64,
}

impl Syms {
    fn read_main(&mut self, b: &mut Bits) -> Result<usize, String> {
        if self.to_read == 0 {
            self.main = Some(read_tree(b, MAX_CODE_WIDTH, NUM_MAIN_CODES).map_err(|e| tr!("主碼表: %w", e))?);
            self.length = Some(read_tree(b, MAX_CODE_WIDTH, NUM_LEN_CODES).map_err(|e| tr!("長度碼表: %w", e))?);
            self.to_read = b.read(15)? as i64;
        }
        self.to_read -= 1;
        self.main.as_ref().map_or_else(|| Err(t("Huffman 查表越界")), |m| m.read_symbol(b))
    }

    fn read_len(&self, b: &mut Bits) -> Result<usize, String> {
        self.length.as_ref().map_or_else(|| Err(t("Huffman 查表越界")), |m| m.read_symbol(b))
    }
}

/// LZ77 解碼器。字典就是輸出本身:同一個壓縮檔裡的成員共用一個字典
/// (solid 壓縮檔會跨成員回頭引用)。
struct Lz77 {
    /// 整個壓縮檔到目前為止的輸出,當成滑動視窗用。
    out: Vec<u8>,
    sym: Syms,
    dist: DistHist,
    dic_len: usize,
}

impl Lz77 {
    fn new() -> Lz77 {
        Lz77 { out: Vec::new(), sym: Syms::default(), dist: DistHist::default(), dic_len: MIN_DIC_SIZE }
    }

    fn set_dic_size(&mut self, n: usize) {
        self.dic_len = self.dic_len.max(n).min(MAX_DIC_SIZE);
    }

    /// 重設每個成員都要歸零的狀態。**字典不歸零**。
    fn reinit(&mut self) {
        self.sym = Syms::default();
        self.dist = DistHist::default();
    }

    /// 解出最多 `want` 個位元組,或直到遇到模式切換碼。回傳新產生的位元組,
    /// 以及模式切換碼(沒有就是 None)。
    fn read(&mut self, b: &mut Bits, want: usize) -> Result<(Vec<u8>, Option<Mode>), String> {
        let start = self.out.len();
        while self.out.len() - start < want {
            let sym = self.sym.read_main(b)?;
            if sym <= 255 {
                self.out.push(sym as u8);
            } else if sym < TYPE_CODE {
                let (mut dist, length);
                if sym <= 259 {
                    let n = self.sym.read_len(b)?;
                    let off = sym & 0x03;
                    dist = self.dist.retrieve(off);
                    length = if off > 1 { n + 3 } else { n + 2 };
                } else {
                    dist = b.read_known_width_uint((sym - 260) as u32)? as usize;
                    let n = self.sym.read_len(b)?;
                    self.dist.append(dist);
                    length = if dist <= MAX_DIST_AT_LEN2 {
                        n + 2
                    } else if dist <= MAX_DIST_AT_LEN3 {
                        n + 3
                    } else {
                        n + 4
                    };
                }
                dist += 1;
                if self.out.len() - start + length > want {
                    return Err(t("比對長度超出這一段要的位元組數"));
                }
                let Some(src) = self.out.len().checked_sub(dist) else {
                    return Err(tr!("比對距離 %d 超出視窗(目前 %d 個位元組)", dist, self.out.len()));
                };
                // 逐個位元組複製:來源與目的可能重疊
                for i in 0..length {
                    let c = self.out[src + i];
                    self.out.push(c);
                }
            } else if sym == TYPE_CODE {
                let m = read_mode(b)?;
                return Ok((self.out[start..].to_vec(), Some(m)));
            } else {
                return Err(tr!("主碼 %d 超出範圍", sym));
            }
        }
        Ok((self.out[start..].to_vec(), None))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// patch_exe 的位移是**固定寬度**的(16 或 32 位元),減法要環繞。
    #[test]
    fn patch_exe_wraps_at_fixed_width() {
        let mut buf = vec![0xE8, 0x10, 0x00, 0x00, 0x00, 0x90, 0x90, 0x90, 0x90];
        patch_exe(&mut buf, 0x20, 1);
        assert_eq!(buf, [0xE8, 0xF0, 0xFF, 0xFF, 0xFF, 0x90, 0x90, 0x90, 0x90]);
        let mut buf = vec![0xE8, 0x10, 0x00, 0x90, 0x90, 0x90];
        patch_exe(&mut buf, 0x20, 0);
        assert_eq!(buf, [0xE8, 0xF0, 0xFF, 0x90, 0x90, 0x90]);
        let mut buf = vec![0xE9, 0x05, 0x00, 0x90, 0x90, 0x90];
        patch_exe(&mut buf, 0x11, 0);
        assert_eq!(buf, [0xE9, 0xF4, 0xFF, 0x90, 0x90, 0x90]);
    }

    /// 一條指令可能跨在兩個區塊的交界上:尾端的殘留要留給下一段。
    #[test]
    fn patch_exe_keeps_tail_for_next_block() {
        let mut buf = vec![0x90, 0x90, 0xE8, 0x01];
        let tail = patch_exe(&mut buf, 0, 1);
        assert!(!tail.is_empty());
        assert_eq!(buf.len() + tail.len(), 4);
    }
}
