//! Microsoft Cabinet(對應 Go 版 `internal/archive/cab`)。
//!
//! 支援不壓縮與 MSZIP。LZX 與 Quantum 還沒做,遇到會明確回報,不會拿錯的
//! 解法硬解出垃圾。
//!
//! CAB 的結構有兩層:CFFOLDER 是壓縮單位,CFFILE 只是「在第幾個 folder 的
//! 第幾個位元組起、多長」。所以要先把整個 folder 解開,再照 offset 切檔案。

use std::time::SystemTime;

use wincv_i18n::{t, tr};

const COMP_NONE: u16 = 0;
const COMP_MSZIP: u16 = 1;
const COMP_QUANTUM: u16 = 2;
const COMP_LZX: u16 = 3;

fn comp_name(c: u16) -> String {
    match c & 0x0F {
        COMP_NONE => t("不壓縮"),
        COMP_MSZIP => "MSZIP".into(),
        COMP_QUANTUM => "Quantum".into(),
        COMP_LZX => "LZX".into(),
        _ => tr!("未知(%d)", c),
    }
}

/// 壓縮檔裡的一筆。
pub struct File {
    pub name: Vec<u8>,
    pub size: i64,
    pub mod_time: Option<SystemTime>,
    pub is_dir: bool,
    pub data: Vec<u8>,
}

struct Folder {
    data_offset: usize,
    blocks: u16,
    comp: u16,
}

fn le16(b: &[u8]) -> u16 {
    u16::from_le_bytes([b[0], b[1]])
}

fn le32(b: &[u8]) -> u32 {
    u32::from_le_bytes([b[0], b[1], b[2], b[3]])
}

/// 讀出整份 cabinet。folder 一定要整份解開才能切出檔案,所以內容全部留在記憶體裡。
pub fn read(data: &[u8]) -> Result<Vec<File>, String> {
    if data.len() < 36 || &data[0..4] != b"MSCF" {
        return Err(t("不是 CAB 檔(magic 不對)"));
    }
    let n_folders = le16(&data[26..]) as usize;
    let n_files = le16(&data[28..]) as usize;
    let flags = le16(&data[30..]);
    if flags & 0x0004 != 0 {
        // 有 RESERVE 欄位時,每個 folder / block 的表頭多幾個位元組,位置全部要
        // 偏移。還沒遇到過真檔,先誠實回報而不是算錯。
        return Err(t("這個 CAB 帶 RESERVE 欄位,還沒支援"));
    }
    if flags & 0x0003 != 0 {
        return Err(t("這是多片 cabinet 的一部分,還沒支援"));
    }
    let file_offset = le32(&data[16..]) as usize;

    let mut folders = Vec::with_capacity(n_folders);
    let mut at = 36;
    for _ in 0..n_folders {
        if at + 8 > data.len() {
            return Err(t("folder 表被截斷"));
        }
        folders.push(Folder {
            data_offset: le32(&data[at..]) as usize,
            blocks: le16(&data[at + 4..]),
            comp: le16(&data[at + 6..]),
        });
        at += 8;
    }
    let mut outs = Vec::with_capacity(n_folders);
    for (i, f) in folders.iter().enumerate() {
        outs.push(decode_folder(data, f).map_err(|e| format!("folder {i}({}): {e}", comp_name(f.comp)))?);
    }

    let mut files = Vec::with_capacity(n_files);
    at = file_offset;
    for _ in 0..n_files {
        if at + 16 > data.len() {
            return Err(t("檔案表被截斷"));
        }
        let size = le32(&data[at..]) as usize;
        let off = le32(&data[at + 4..]) as usize;
        let fidx = le16(&data[at + 8..]) as usize;
        let date = le16(&data[at + 10..]) as u32;
        let tm = le16(&data[at + 12..]) as u32;
        let attrs = le16(&data[at + 14..]);
        at += 16;
        let end = data[at..].iter().position(|&c| c == 0).map_or(data.len(), |i| at + i);
        // 0x80 表示檔名是 UTF-8,否則是該系統的 ANSI 編碼。CAB 的目錄分隔字元是 \,統一成 /。
        let name: Vec<u8> = data[at..end].iter().map(|&c| if c == b'\\' { b'/' } else { c }).collect();
        at = end + 1;
        let is_dir = attrs & 0x10 != 0;
        let mut body = Vec::new();
        if fidx < outs.len() && !is_dir {
            let out = &outs[fidx];
            if off + size > out.len() {
                return Err(tr!(
                    "%s: folder 解出來只有 %d 個位元組,取不到 [%d,%d)",
                    wincv_textenc::utf8_lossy_go(&name),
                    out.len(),
                    off,
                    off + size
                ));
            }
            body = out[off..off + size].to_vec();
        }
        files.push(File { name, size: size as i64, mod_time: crate::dos_time(date << 16 | tm), is_dir, data: body });
    }
    Ok(files)
}

fn decode_folder(data: &[u8], f: &Folder) -> Result<Vec<u8>, String> {
    let comp = f.comp & 0x0F;
    if comp != COMP_NONE && comp != COMP_MSZIP {
        return Err(tr!("還不支援 %s", comp_name(f.comp)));
    }
    let mut at = f.data_offset;
    let mut out: Vec<u8> = Vec::new();
    for b in 0..f.blocks as usize {
        if at + 8 > data.len() {
            return Err(tr!("第 %d 個區塊的表頭被截斷", b));
        }
        let n_comp = le16(&data[at + 4..]) as usize;
        let n_raw = le16(&data[at + 6..]) as usize;
        at += 8;
        if at + n_comp > data.len() {
            return Err(tr!("第 %d 個區塊的資料被截斷", b));
        }
        let blk = &data[at..at + n_comp];
        at += n_comp;
        if comp == COMP_NONE {
            out.extend_from_slice(blk);
            continue;
        }
        if blk.len() < 2 || &blk[..2] != b"CK" {
            return Err(tr!("第 %d 個區塊少了 MSZIP 的 CK 標記", b));
        }
        // [雷] 每個區塊是獨立的 deflate 串流,但 LZ77 的視窗**接續前一個區塊的
        // 輸出**。不把前 32 KB 當字典餵進去,前 32 KB 會正常,之後才開始壞,
        // 小檔案測不出來。
        let dict = &out[out.len().saturating_sub(32768)..];
        let (raw, err) = inflate_with_dict(&blk[2..], dict, n_raw);
        if let Some(e) = err {
            if raw.len() != n_raw {
                return Err(tr!("第 %d 個區塊解不開: %w", b, e));
            }
        }
        out.extend_from_slice(&raw);
    }
    Ok(out)
}

/// 以 `dict` 當預設字典解一段 raw deflate。出錯時也交回已解出的部分。
fn inflate_with_dict(input: &[u8], dict: &[u8], hint: usize) -> (Vec<u8>, Option<String>) {
    use flate2::{Decompress, FlushDecompress, Status};
    let mut d = Decompress::new(false);
    let mut raw = Vec::with_capacity(hint.max(1024));
    if !dict.is_empty() {
        if let Err(e) = d.set_dictionary(dict) {
            return (raw, Some(e.to_string()));
        }
    }
    loop {
        if raw.len() == raw.capacity() {
            raw.reserve(raw.capacity().max(1024));
        }
        let before = (d.total_in(), raw.len());
        let pos = d.total_in() as usize;
        match d.decompress_vec(&input[pos..], &mut raw, FlushDecompress::Finish) {
            Ok(Status::StreamEnd) => return (raw, None),
            Ok(_) => {
                if (d.total_in(), raw.len()) == before && raw.len() < raw.capacity() {
                    return (raw, Some("unexpected EOF".into()));
                }
            }
            Err(e) => return (raw, Some(e.to_string())),
        }
    }
}
