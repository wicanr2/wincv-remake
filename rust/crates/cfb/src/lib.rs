//! 讀 Compound File Binary(也就是 OLE2 複合文件)(對應 Go 版 `internal/cfb`)。
//!
//! 這是 Office 97–2003 那一代的容器格式:一個檔案裡放好幾條「串流」,
//! 用一張像 FAT 的配置表串起來。`.doc`、`.xls`、`.ppt` 的外殼都是它,
//! 裡面的內容各自不同。
//!
//! 三個容易解錯的地方:
//!
//!   - **磁區編號從 0 起算,但檔案開頭有 512 位元組的標頭。** 所以第 N 個
//!     磁區在 (N+1) × 磁區大小。忘記加一會讀到前一個磁區的內容,
//!     那是合法的位元組,不會出錯,只會解出垃圾。
//!   - **小串流走另一張表。** 小於門檻(通常 4096)的串流不佔完整磁區,
//!     它們擠在一條「迷你串流」裡,用迷你 FAT 串接。照大串流的方式讀
//!     會拿到整份檔案裡別的東西。
//!   - **鏈結可能成環。** 損壞的檔案會讓「跟著下一個磁區走」變成無窮迴圈。

use std::collections::{HashMap, HashSet};
use std::io;
use std::path::Path;

use wincv_i18n::tr;

#[cfg(test)]
mod tests;

/// 檔案大小上限。
pub const MAX_BYTES: u64 = 256 << 20;

const MAGIC: [u8; 8] = [0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1];

// 特殊的磁區編號。
const MAX_REG_SECT: u32 = 0xFFFFFFFA;

/// 一個打開著的複合文件。
pub struct File {
    data: Vec<u8>,

    sector_size: usize,
    mini_size: usize,
    mini_cutoff: u32,

    fat: Vec<u32>,
    mini_fat: Vec<u32>,
    mini: Vec<u8>,

    entries: Vec<Entry>,
    by_name: HashMap<String, usize>,
}

/// 目錄裡的一筆:一條串流或一個儲存區。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    pub name: String,
    /// 1 儲存區,2 串流,5 根目錄。
    pub typ: u8,
    pub start: u32,
    pub size: u64,
}

fn le16(b: &[u8], off: usize) -> u16 {
    u16::from_le_bytes([b[off], b[off + 1]])
}

fn le32(b: &[u8], off: usize) -> u32 {
    u32::from_le_bytes([b[off], b[off + 1], b[off + 2], b[off + 3]])
}

fn le64(b: &[u8], off: usize) -> u64 {
    let mut a = [0u8; 8];
    a.copy_from_slice(&b[off..off + 8]);
    u64::from_le_bytes(a)
}

/// Go 的 `*PathError` 長相:「stat 檔名: no such file or directory」。
///
/// Rust 的 io::Error 會在後面加「(os error 2)」,而且第一個字母大寫;
/// 兩邊的訊息要能並排比對,所以照 Go 的寫法重組。
pub fn go_path_error(op: &str, name: &str, e: &io::Error) -> String {
    let mut msg = e.to_string();
    if let Some(i) = msg.find(" (os error ") {
        msg.truncate(i);
    }
    let mut cs = msg.chars();
    let msg = match cs.next() {
        Some(c) => c.to_lowercase().chain(cs).collect(),
        None => msg,
    };
    format!("{op} {name}: {msg}")
}

impl File {
    /// 打開一個複合文件。
    pub fn open(name: impl AsRef<Path>) -> Result<File, String> {
        let p = name.as_ref();
        let shown = p.to_string_lossy();
        let st = std::fs::metadata(p).map_err(|e| go_path_error("stat", &shown, &e))?;
        if st.len() > MAX_BYTES {
            return Err(tr!("檔案太大(%d 位元組)", st.len()));
        }
        let b = std::fs::read(p).map_err(|e| go_path_error("open", &shown, &e))?;
        File::parse(b)
    }

    /// 解析一份記憶體裡的複合文件。
    pub fn parse(b: Vec<u8>) -> Result<File, String> {
        if b.len() < 512 {
            return Err(tr!("太短,不是複合文件"));
        }
        if b[..8] != MAGIC {
            return Err(tr!("不是複合文件(簽章不符)"));
        }
        let shift = le16(&b, 0x1E);
        let mini_shift = le16(&b, 0x20);
        if !(7..=20).contains(&shift) || mini_shift < 2 || mini_shift >= shift {
            return Err(tr!("磁區大小不合理(2^%d / 2^%d)", shift, mini_shift));
        }
        let mut mini_cutoff = le32(&b, 0x38);
        if mini_cutoff == 0 {
            mini_cutoff = 4096;
        }
        let mut f = File {
            data: b,
            sector_size: 1 << shift,
            mini_size: 1 << mini_shift,
            mini_cutoff,
            fat: Vec::new(),
            mini_fat: Vec::new(),
            mini: Vec::new(),
            entries: Vec::new(),
            by_name: HashMap::new(),
        };
        f.read_fat()?;
        f.read_dir()?;
        f.read_mini();
        Ok(f)
    }

    /// 取一個磁區的內容。
    fn sector(&self, n: u32) -> Option<&[u8]> {
        // 用 u64 算:磁區編號可以到 0xFFFFFFFA,32 位元平台上乘下去會溢位。
        let off = (n as u64 + 1) * self.sector_size as u64;
        if off + self.sector_size as u64 > self.data.len() as u64 {
            return None;
        }
        let off = off as usize;
        Some(&self.data[off..off + self.sector_size])
    }

    /// 把 FAT 讀成一張表。
    ///
    /// FAT 自己也存在磁區裡,而「哪些磁區是 FAT」記在 DIFAT。DIFAT 的前
    /// 109 筆在標頭裡,不夠的話再串到別的磁區去:一份大檔案的 FAT 本身
    /// 就要好幾百個磁區。
    fn read_fat(&mut self) -> Result<(), String> {
        let b = &self.data;
        let n_fat = le32(b, 0x2C) as usize;
        let dif_start = le32(b, 0x44);
        let n_dif = le32(b, 0x48) as usize;

        let mut fat_sects: Vec<u32> = Vec::new();
        for i in 0..109 {
            if fat_sects.len() >= n_fat {
                break;
            }
            let v = le32(b, 0x4C + i * 4);
            if v > MAX_REG_SECT {
                break;
            }
            fat_sects.push(v);
        }
        let mut next = dif_start;
        let per = self.sector_size / 4 - 1;
        let mut i = 0usize;
        while i < n_dif + 1 && next <= MAX_REG_SECT && fat_sects.len() < n_fat {
            let Some(s) = self.sector(next) else { break };
            let mut j = 0;
            while j < per && fat_sects.len() < n_fat {
                let v = le32(s, j * 4);
                j += 1;
                if v > MAX_REG_SECT {
                    continue;
                }
                fat_sects.push(v);
            }
            next = le32(s, per * 4);
            i += 1;
        }
        if fat_sects.is_empty() {
            return Err(tr!("找不到配置表"));
        }
        let mut fat = Vec::new();
        for sn in fat_sects {
            let Some(s) = self.sector(sn) else { continue };
            for j in (0..s.len().saturating_sub(3)).step_by(4) {
                fat.push(le32(s, j));
            }
        }
        self.fat = fat;
        Ok(())
    }

    fn read_dir(&mut self) -> Result<(), String> {
        let start = le32(&self.data, 0x30);
        let sects = chain_of(&self.fat, start, self.fat.len() + 1);
        if sects.is_empty() {
            return Err(tr!("找不到目錄"));
        }
        let per_sect = self.sector_size / 128;
        let mut entries = Vec::new();
        let mut by_name = HashMap::new();
        for sn in sects {
            let Some(s) = self.sector(sn) else { continue };
            for i in 0..per_sect {
                let Some(e) = parse_entry(&s[i * 128..(i + 1) * 128]) else { continue };
                let idx = entries.len();
                // 同名的以第一筆為準:.doc 的串流名在根儲存區裡是唯一的,
                // 而巢狀儲存區裡的同名項目不是我們要的那一條。
                by_name.entry(e.name.clone()).or_insert(idx);
                entries.push(e);
            }
        }
        if entries.is_empty() {
            return Err(tr!("目錄是空的"));
        }
        self.entries = entries;
        self.by_name = by_name;
        Ok(())
    }

    /// 讀出迷你串流與迷你配置表。
    fn read_mini(&mut self) {
        let mini_start = le32(&self.data, 0x3C);
        let n_mini = le32(&self.data, 0x40) as usize;
        let mut mini_fat = Vec::new();
        for sn in chain_of(&self.fat, mini_start, n_mini + 1) {
            let Some(s) = self.sector(sn) else { continue };
            for j in (0..s.len().saturating_sub(3)).step_by(4) {
                mini_fat.push(le32(s, j));
            }
        }
        self.mini_fat = mini_fat;
        // 根目錄那一筆的起始磁區指向迷你串流本身。
        if let Some(root) = self.entries.first() {
            if root.typ == 5 {
                let (start, size) = (root.start, root.size);
                self.mini = self.read_chain(start, size);
            }
        }
    }

    /// 沿著大串流的鏈讀出內容。
    fn read_chain(&self, start: u32, size: u64) -> Vec<u8> {
        let need = size.min(MAX_BYTES) as usize;
        let sects = chain_of(&self.fat, start, need / self.sector_size + 2);
        let mut out = Vec::with_capacity(need.min(self.data.len()));
        for sn in sects {
            let Some(s) = self.sector(sn) else { break };
            out.extend_from_slice(s);
            if out.len() >= need {
                break;
            }
        }
        out.truncate(need);
        out
    }

    /// 沿著迷你串流的鏈讀出內容。
    fn read_mini_chain(&self, start: u32, size: u64) -> Vec<u8> {
        let need = size as usize;
        // 容量不照 need 開:need 來自檔案,門檻欄位壞掉時可以大到幾 GB。
        let mut out = Vec::with_capacity(need.min(self.mini.len()));
        let mut seen = HashSet::new();
        let mut n = start;
        while n <= MAX_REG_SECT && out.len() < need {
            if n as usize >= self.mini_fat.len() || !seen.insert(n) {
                break;
            }
            let off = n as usize * self.mini_size;
            if off + self.mini_size > self.mini.len() {
                break;
            }
            out.extend_from_slice(&self.mini[off..off + self.mini_size]);
            n = self.mini_fat[n as usize];
        }
        out.truncate(need);
        out
    }

    /// 列出所有串流與儲存區的名字。
    pub fn names(&self) -> Vec<String> {
        self.entries.iter().map(|e| e.name.clone()).collect()
    }

    /// 目錄裡的每一筆。
    pub fn entries(&self) -> &[Entry] {
        &self.entries
    }

    /// 有沒有這條串流。
    pub fn has(&self, name: &str) -> bool {
        matches!(self.by_name.get(name), Some(&i) if self.entries[i].typ == 2)
    }

    /// 讀出一條串流。
    pub fn stream(&self, name: &str) -> Result<Vec<u8>, String> {
        let Some(&i) = self.by_name.get(name) else {
            return Err(tr!("沒有 %s 這條串流", printable(name)));
        };
        let e = &self.entries[i];
        if e.typ != 2 {
            return Err(tr!("%s 不是串流", printable(name)));
        }
        if e.size < self.mini_cutoff as u64 {
            return Ok(self.read_mini_chain(e.start, e.size));
        }
        Ok(self.read_chain(e.start, e.size))
    }
}

/// 沿著配置表把一條鏈上的磁區編號列出來。
fn chain_of(fat: &[u32], start: u32, limit: usize) -> Vec<u32> {
    let mut out = Vec::new();
    let mut seen = HashSet::new();
    let mut n = start;
    while n <= MAX_REG_SECT {
        if n as usize >= fat.len() || seen.contains(&n) || out.len() >= limit {
            break;
        }
        seen.insert(n);
        out.push(n);
        n = fat[n as usize];
    }
    out
}

fn parse_entry(b: &[u8]) -> Option<Entry> {
    let typ = b[0x42];
    if typ != 1 && typ != 2 && typ != 5 {
        return None;
    }
    let n = le16(b, 0x40) as usize;
    if !(2..=64).contains(&n) {
        return None;
    }
    // 名字是 UTF-16LE,長度含結尾的空字元。
    let mut u = Vec::with_capacity(n / 2);
    let mut i = 0;
    while i + 1 < n - 2 {
        u.push(le16(b, i));
        i += 2;
    }
    let name: String = char::decode_utf16(u).map(|r| r.unwrap_or('\u{FFFD}')).collect();
    Some(Entry { name, typ, start: le32(b, 0x74), size: le64(b, 0x78) })
}

/// 把名字裡的控制字元換掉。
///
/// Office 的串流名開頭常常是 0x01 到 0x05 這種控制字元(例如
/// `\x05SummaryInformation`),直接印出去會弄亂終端機。
pub fn printable(s: &str) -> String {
    let mut sb = String::with_capacity(s.len());
    for r in s.chars() {
        if (r as u32) < 0x20 {
            sb.push_str(&format!("\\x{:02x}", r as u32));
            continue;
        }
        sb.push(r);
    }
    sb
}
