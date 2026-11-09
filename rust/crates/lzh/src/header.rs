//! LZH 的成員標頭,level 0、1、2。

use std::io::{Read, Seek, SeekFrom};

use crate::Error;

/// 修改時間。level 0/1 存 MS-DOS 格式,level 2 與 0x54 擴充標頭存 Unix 秒數。
/// 換成日曆時間要看時區,那是呼叫端的事,這裡保留原值。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum MTime {
    Dos(u32),
    Unix(u32),
}

/// 一個成員。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    /// 原始位元組(檔名編碼由上層判斷),目錄分隔字元已正規化成 `/`。
    pub name: Vec<u8>,
    /// 例如 `-lh5-`。
    pub method: Vec<u8>,
    pub packed: i64,
    pub original: i64,
    pub mtime: MTime,
    pub is_dir: bool,
    pub crc: u16,
    /// 壓縮資料在檔案裡的位移。
    pub offset: i64,
}

fn le16(b: &[u8]) -> u16 {
    u16::from_le_bytes([b[0], b[1]])
}

fn le32(b: &[u8]) -> u32 {
    u32::from_le_bytes([b[0], b[1], b[2], b[3]])
}

fn io_err(e: std::io::Error) -> Error {
    Error(e.to_string())
}

/// 讀滿 buf;讀不滿回 false(檔尾)。
fn read_full<R: Read>(r: &mut R, buf: &mut [u8]) -> bool {
    r.read_exact(buf).is_ok()
}

/// 列出所有成員。
pub fn list<R: Read + Seek>(r: &mut R) -> Result<Vec<Entry>, Error> {
    match list_partial(r) {
        (out, None) => Ok(out),
        (_, Some(e)) => Err(e),
    }
}

/// 與 [`list`] 相同,但出錯時也交回出錯前讀到的成員(Go 版 `lzh.List`
/// 的行為:壓縮檔尾端壞掉時,前面的成員仍然看得到)。
pub fn list_partial<R: Read + Seek>(r: &mut R) -> (Vec<Entry>, Option<Error>) {
    let mut out = Vec::new();
    let mut pos = match r.seek(SeekFrom::Start(0)) {
        Ok(p) => p as i64,
        Err(e) => return (out, Some(io_err(e))),
    };
    loop {
        let (e, next) = match read_header(r, pos) {
            Ok(Some(v)) => v,
            Ok(None) => return (out, None),
            Err(e) => return (out, Some(e)),
        };
        out.push(e);
        pos = next;
        if out.len() > 100_000 {
            return (out, Some(Error("成員數異常,可能不是 LZH 檔".into())));
        }
    }
}

fn seek<R: Seek>(r: &mut R, pos: i64) -> Result<(), Error> {
    let p = u64::try_from(pos).map_err(|_| Error(format!("位移是負的: {pos}")))?;
    r.seek(SeekFrom::Start(p)).map(|_| ()).map_err(io_err)
}

fn read_header<R: Read + Seek>(r: &mut R, pos: i64) -> Result<Option<(Entry, i64)>, Error> {
    seek(r, pos)?;
    let mut head = [0u8; 2];
    // 讀不到就當檔尾:尾端補 0 的檔很常見。
    if !read_full(r, &mut head) || head[0] == 0 {
        return Ok(None);
    }
    seek(r, pos)?;
    let mut buf = [0u8; 26];
    if !read_full(r, &mut buf) {
        return Ok(None);
    }
    match buf[20] {
        level @ (0 | 1) => read_header_lv01(r, pos, head[0] as usize, level).map(Some),
        2 => read_header_lv2(r, pos).map(Some),
        level => Err(Error(format!("還不支援 level {level} 的標頭"))),
    }
}

fn read_header_lv01<R: Read + Seek>(r: &mut R, pos: i64, hsize: usize, level: u8) -> Result<(Entry, i64), Error> {
    let mut full = vec![0u8; hsize + 2];
    seek(r, pos)?;
    r.read_exact(&mut full).map_err(|e| Error(format!("標頭不完整: {e}")))?;
    let mut e = Entry {
        name: Vec::new(),
        method: full[2..7].to_vec(),
        packed: le32(&full[7..11]) as i64,
        original: le32(&full[11..15]) as i64,
        mtime: MTime::Dos(le32(&full[15..19])),
        is_dir: false,
        crc: 0,
        offset: 0,
    };
    let name_len = full[21] as usize;
    if 22 + name_len + 2 > full.len() {
        return Err(Error("標頭裡的檔名長度不合理".into()));
    }
    e.name = normalize_name(&full[22..22 + name_len]);
    e.crc = le16(&full[22 + name_len..24 + name_len]);
    let mut data_at = pos + hsize as i64 + 2;
    if level == 1 {
        // [雷] 擴充標頭的長度欄位存在前一個標頭的尾巴,起點是 data_at-2。
        // 沒有擴充標頭的檔那兩個位元組剛好是 0,起點錯了也看起來正常。
        let mut at = data_at - 2;
        let mut dir = Vec::new();
        loop {
            seek(r, at)?;
            let mut sz = [0u8; 2];
            if !read_full(r, &mut sz) {
                break;
            }
            let n = le16(&sz) as usize;
            if n == 0 {
                at += 2;
                break;
            }
            if n < 3 {
                return Err(Error(format!("擴充標頭長度不合理: {n}")));
            }
            let mut ext = vec![0u8; n - 2];
            if !read_full(r, &mut ext) {
                break;
            }
            apply_ext(&mut e, &mut dir, &ext);
            e.packed -= n as i64;
            at += n as i64;
        }
        if !dir.is_empty() {
            let mut name = dir;
            name.extend_from_slice(&e.name);
            e.name = name;
        }
        data_at = at;
    }
    e.is_dir = e.method == b"-lhd-" || e.name.ends_with(b"/");
    e.offset = data_at;
    let next = data_at + e.packed;
    Ok((e, next))
}

fn read_header_lv2<R: Read + Seek>(r: &mut R, pos: i64) -> Result<(Entry, i64), Error> {
    seek(r, pos)?;
    let mut base = [0u8; 26];
    r.read_exact(&mut base).map_err(|e| Error(format!("標頭不完整: {e}")))?;
    let total = le16(&base[0..2]) as i64;
    let mut e = Entry {
        name: Vec::new(),
        method: base[2..7].to_vec(),
        packed: le32(&base[7..11]) as i64,
        original: le32(&base[11..15]) as i64,
        mtime: MTime::Unix(le32(&base[15..19])),
        is_dir: false,
        crc: le16(&base[21..23]),
        offset: 0,
    };
    // 擴充標頭從第 24 個位元組起(base[24..26] 是第一個的大小)。
    let mut at = pos + 24;
    let (mut dir, mut name) = (Vec::new(), Vec::new());
    loop {
        seek(r, at)?;
        let mut sz = [0u8; 2];
        if !read_full(r, &mut sz) {
            break;
        }
        let n = le16(&sz) as usize;
        if n < 3 {
            break;
        }
        let mut ext = vec![0u8; n - 2];
        if !read_full(r, &mut ext) {
            break;
        }
        if ext[0] == 0x01 {
            name = ext[1..].to_vec();
        } else {
            apply_ext(&mut e, &mut dir, &ext);
        }
        at += n as i64;
        if at > pos + total {
            break;
        }
    }
    if !name.is_empty() {
        e.name = normalize_name(&name);
    }
    if !dir.is_empty() {
        let mut full = dir;
        full.extend_from_slice(&e.name);
        e.name = full;
    }
    e.is_dir = e.method == b"-lhd-" || e.name.is_empty() || e.name.ends_with(b"/");
    e.offset = pos + total;
    let next = e.offset + e.packed;
    Ok((e, next))
}

fn apply_ext(e: &mut Entry, dir: &mut Vec<u8>, ext: &[u8]) {
    let Some(&kind) = ext.first() else { return };
    match kind {
        0x01 => e.name = normalize_name(&ext[1..]),
        // 目錄名。分隔字元是 0xFF,不是 / 也不是 \。
        0x02 => {
            let mut d: Vec<u8> = ext[1..].iter().map(|&b| if b == 0xFF { b'/' } else { b }).collect();
            if !d.is_empty() && !d.ends_with(b"/") {
                d.push(b'/');
            }
            *dir = d;
        }
        0x54 if ext.len() >= 5 => e.mtime = MTime::Unix(le32(&ext[1..5])),
        _ => {}
    }
}

/// 0xFF 與 `\` 換成 `/`,去掉一個開頭的 `/`。
fn normalize_name(s: &[u8]) -> Vec<u8> {
    let v: Vec<u8> = s.iter().map(|&b| if b == 0xFF || b == b'\\' { b'/' } else { b }).collect();
    match v.strip_prefix(b"/") {
        Some(rest) => rest.to_vec(),
        None => v,
    }
}
