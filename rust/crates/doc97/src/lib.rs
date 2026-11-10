//! 讀 Word 97–2003 的 .doc(對應 Go 版 `internal/doc97`)。
//!
//! 外殼是 OLE2 複合文件(`wincv-cfb`),裡面主要是三條串流:
//!
//! ```text
//! WordDocument   FIB(檔案資訊區塊)+ 正文的位元組 + 屬性頁
//! 0Table/1Table  各種對照表;用哪一條由 FIB 裡的一個位元決定
//! Data           圖片等大塊資料
//! ```
//!
//! 正文不是一段連續的文字。它被切成「片段」(piece),每一段各自說明
//! 自己在哪裡、是單位元組還是 UTF-16:這是 Word 為了讓編輯時不必
//! 搬動整份文件而設計的。所以取文字的第一步是走 piece table,
//! 把片段接回一條連續的字元序列。
//!
//! 兩件事讓「看起來對」與「真的對」差很多:
//!
//!   - **表格的邊界不在文字裡。** 儲存格結尾與列結尾用的是同一個字元
//!     (0x07),差別記在段落屬性的一個位元上。只看文字的話,分不出
//!     一列有幾格,拼出來的表格會是自洽但錯的。
//!   - **標題不是靠字體大小認出來的。** 段落屬性帶著樣式編號,樣式表裡
//!     記著那個樣式的識別碼;識別碼 1 到 9 就是標題一到標題九,而且
//!     與介面語言無關:中文版 Word 的「標題 1」識別碼一樣是 1。
//!
//! 沒有做的:圖片(內容在 Data 串流裡,是另一套 PICF 結構)、
//! 加密的檔案、Word 6/95(FIB 的版面不同,遇到會明說)。

mod blocks;
mod cp1252;
mod gosort;
mod lists;
mod props;

#[cfg(test)]
mod tests;

use std::collections::HashMap;
use std::path::Path;

use wincv_cfb::File as CfbFile;
use wincv_i18n::tr;

use lists::LstInfo;
use props::{CharRun, ParaProp};

/// 一份打開著的 .doc。
pub struct Doc {
    /// 整份文件的字元,索引就是 CP(character position)。
    /// 各種對照表都用 CP 或 FC 指位置,所以先攤平成一條序列,
    /// 後面的工作全部變成查表。
    chars: Vec<char>,
    /// fcs[i] 是 chars[i] 在 WordDocument 串流裡的位元組位置。
    /// 屬性表用的是 FC 不是 CP,兩者之間隔著 piece table。
    fcs: Vec<i32>,

    /// 正文有幾個字元;之後是註腳、頁首頁尾等。
    ccp_text: i64,
    ccp_ftn: i64,

    paras: Vec<ParaProp>,
    runs: Vec<CharRun>,
    /// istd → sti(樣式識別碼)
    stis: Vec<i64>,

    /// 「清單編號方式」表,lfo 把段落上的 ilfo 對到那張表。見 lists.rs。
    lists: HashMap<u32, LstInfo>,
    lfo: Vec<u32>,
}

pub(crate) fn le16(b: &[u8], off: usize) -> u16 {
    u16::from_le_bytes([b[off], b[off + 1]])
}

pub(crate) fn le32(b: &[u8], off: usize) -> u32 {
    u32::from_le_bytes([b[off], b[off + 1], b[off + 2], b[off + 3]])
}

impl Doc {
    /// 打開一份 .doc。
    pub fn open(name: impl AsRef<Path>) -> Result<Doc, String> {
        let f = CfbFile::open(name)?;
        Doc::new(&f)
    }

    /// 從一個已經打開的複合文件建 Doc。
    pub fn new(f: &CfbFile) -> Result<Doc, String> {
        let wd = f.stream("WordDocument").map_err(|_| tr!("這不是 Word 文件(沒有 WordDocument 串流)"))?;
        if wd.len() < 0x200 {
            return Err(tr!("WordDocument 串流太短"));
        }
        let magic = le16(&wd, 0);
        if magic != 0xA5EC {
            return Err(tr!("這不是 Word 文件(識別碼 0x%04X)", magic));
        }
        let n_fib = le16(&wd, 0x02);
        let flags = le16(&wd, 0x0A);
        if flags & 0x0100 != 0 {
            return Err(tr!("這份文件有密碼保護"));
        }
        if n_fib < 193 {
            // Word 6/95 的 FIB 版面不同,對照表的位置也不一樣。
            // 照 Word 97 的位置去讀會拿到別的欄位,而那些數字都是
            // 合法的偏移量:解出來是垃圾而不是錯誤。
            return Err(tr!("這是 Word 6/95 的格式(nFib=%d),尚未支援", n_fib));
        }
        let table_name = if flags & 0x0200 != 0 { "1Table" } else { "0Table" };
        let tbl = f.stream(table_name).map_err(|_| tr!("找不到 %s 串流", table_name))?;

        let mut d = Doc {
            chars: Vec::new(),
            fcs: Vec::new(),
            ccp_text: le32(&wd, 0x4C) as i32 as i64,
            ccp_ftn: le32(&wd, 0x50) as i32 as i64,
            paras: Vec::new(),
            runs: Vec::new(),
            stis: Vec::new(),
            lists: HashMap::new(),
            lfo: Vec::new(),
        };
        d.read_text(&wd, &tbl)?;
        d.read_styles(&wd, &tbl);
        d.read_para_props(&wd, &tbl);
        d.read_char_runs(&wd, &tbl);
        d.read_lists(&wd, &tbl);
        Ok(d)
    }

    pub fn close(&self) {}

    /// 走 piece table,把正文攤平成一條字元序列。
    fn read_text(&mut self, wd: &[u8], tbl: &[u8]) -> Result<(), String> {
        let (fc_clx, lcb_clx) = fib_fc(wd, FIB_CLX);
        if fc_clx < 0 || lcb_clx <= 0 || fc_clx + lcb_clx > tbl.len() as i64 {
            return Err(tr!("piece table 的位置不合理(fc=%d lcb=%d)", fc_clx, lcb_clx));
        }
        let pieces = parse_clx(&tbl[fc_clx as usize..(fc_clx + lcb_clx) as usize])?;
        let wlen = wd.len() as i64;
        for p in pieces {
            let mut n = p.cp_end - p.cp_start;
            if n <= 0 {
                continue;
            }
            if p.wide {
                if p.fc + 2 * n > wlen {
                    n = (wlen - p.fc) / 2;
                }
                if n <= 0 {
                    continue;
                }
                let base = p.fc as usize;
                let u: Vec<u16> = (0..n as usize).map(|i| le16(wd, base + 2 * i)).collect();
                // 位置照「解出來的第幾個字」算,與 Go 的 range utf16.Decode(u) 相同:
                // 代理對併成一個字之後,後面的位置會少算兩個位元組。
                for (i, r) in char::decode_utf16(u).map(|r| r.unwrap_or('\u{FFFD}')).enumerate() {
                    self.chars.push(r);
                    self.fcs.push((p.fc + 2 * i as i64) as i32);
                }
                continue;
            }
            if p.fc + n > wlen {
                n = wlen - p.fc;
            }
            if n <= 0 {
                continue;
            }
            // [雷] 單位元組的片段一律是 cp1252,不是文件本身的語系字碼頁。
            // Word 只在整段文字都塞得進 cp1252 時才用這種寫法,所以
            // 中文的內容必定是 UTF-16 的片段:拿 Big5 去解這一段
            // 會把好好的英文變成亂碼。
            for i in 0..n {
                let c = wd[(p.fc + i) as usize];
                let r = if c < 0x80 { c as char } else { cp1252::CP1252[c as usize - 0x80] };
                self.chars.push(r);
                self.fcs.push((p.fc + i) as i32);
            }
        }
        if self.chars.is_empty() {
            return Err(tr!("這份文件沒有文字"));
        }
        if self.ccp_text < 0 || self.ccp_text > self.chars.len() as i64 {
            self.ccp_text = self.chars.len() as i64;
        }
        Ok(())
    }
}

/// 取 FibRgFcLcb 裡第 i 組的 fc 與 lcb。
///
/// 這個陣列從 0x9A 開始,每組 8 個位元組(4 位元組位置 + 4 位元組長度)。
/// 位置固定在這裡是因為它前面的三段(FibBase、FibRgW97、FibRgLw97)
/// 都是定長的。
pub(crate) fn fib_fc(wd: &[u8], i: usize) -> (i64, i64) {
    let off = 0x9A + i * 8;
    if off + 8 > wd.len() {
        return (0, 0);
    }
    (le32(wd, off) as i32 as i64, le32(wd, off + 4) as i32 as i64)
}

// FibRgFcLcb97 裡用得到的欄位編號。
pub(crate) const FIB_STSHF: usize = 1;
pub(crate) const FIB_PLCF_BTE_CHP: usize = 12;
pub(crate) const FIB_PLCF_BTE_PAP: usize = 13;
const FIB_CLX: usize = 33;

/// piece table 裡的一段。
struct Piece {
    cp_start: i64,
    cp_end: i64,
    /// 在 WordDocument 串流裡的位元組位置
    fc: i64,
    wide: bool,
}

/// 解 piece table。
///
/// CLX 是「幾筆 Prc(直接寫在這裡的屬性)之後接一筆 Pcdt(片段表)」。
/// Prc 用不到,但它們的長度必須算對才走得到 Pcdt。
fn parse_clx(clx: &[u8]) -> Result<Vec<Piece>, String> {
    let mut i: i64 = 0;
    let n = clx.len() as i64;
    while i < n {
        match clx[i as usize] {
            1 => {
                // Prc
                if i + 3 > n {
                    return Err(tr!("piece table 被截斷"));
                }
                let cb = le16(clx, i as usize + 1) as i16 as i64;
                // Go 版在 cb <= -3 時會原地打轉或讀到負的索引(見報告);
                // 這裡當成截斷,不跟著卡住。
                if 3 + cb <= 0 {
                    return Err(tr!("piece table 被截斷"));
                }
                i += 3 + cb;
            }
            2 => {
                // Pcdt
                if i + 5 > n {
                    return Err(tr!("piece table 被截斷"));
                }
                let mut lcb = le32(clx, i as usize + 1) as i64;
                if i + 5 + lcb > n {
                    lcb = n - i - 5;
                }
                let s = (i + 5) as usize;
                return parse_plc_pcd(&clx[s..s + lcb as usize]);
            }
            other => return Err(tr!("piece table 裡有看不懂的項目(0x%02X)", other)),
        }
    }
    Err(tr!("piece table 裡沒有片段表"))
}

/// 解片段表:n+1 個 CP 之後接 n 個 8 位元組的描述。
fn parse_plc_pcd(b: &[u8]) -> Result<Vec<Piece>, String> {
    if b.len() < 16 {
        return Err(tr!("片段表太短"));
    }
    let n = (b.len() - 4) / 12;
    if n == 0 {
        return Err(tr!("片段表是空的"));
    }
    let cps: Vec<i64> = (0..=n).map(|i| le32(b, i * 4) as i32 as i64).collect();
    let base = 4 * (n + 1);
    let mut out = Vec::with_capacity(n);
    for i in 0..n {
        let off = base + i * 8;
        let fc_raw = le32(b, off + 2);
        // [雷] 第 30 個位元是「這一段是單位元組」的旗標,而位置本身
        // 要除以二才是真正的位元組偏移量。不處理的話會從檔案裡
        // 完全不相干的地方開始讀,而讀出來的東西看起來像亂碼,
        // 不像位置算錯。
        let (fc, wide) = if fc_raw & 0x40000000 != 0 {
            ((fc_raw & 0x3FFFFFFF) as i64 / 2, false)
        } else {
            ((fc_raw & 0x3FFFFFFF) as i64, true)
        };
        out.push(Piece { cp_start: cps[i], cp_end: cps[i + 1], fc, wide });
    }
    Ok(out)
}

/// Go 的 `sort.Search`:回傳第一個讓 f 為真的位置。
///
/// 照抄它的二分法:資料不保證依 f 單調(屬性表依起點排序,f 看的卻是終點),
/// 換一種二分法會停在別的位置。
pub(crate) fn go_search(n: usize, f: impl Fn(usize) -> bool) -> usize {
    let (mut i, mut j) = (0usize, n);
    while i < j {
        let h = (i + j) >> 1;
        if !f(h) {
            i = h + 1;
        } else {
            j = h;
        }
    }
    i
}
