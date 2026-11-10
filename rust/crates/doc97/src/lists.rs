//! 清單的編號方式(對應 Go 版 `lists.go`)。
//!
//! 編號方式存在另外兩張表裡,不在段落屬性上。段落只帶兩個數字:
//! `ilfo`(屬於哪一串)與 `ilvl`(第幾層),要走到「這一層是編號還是項目
//! 符號」得繞一圈:
//!
//! ```text
//! ilfo → PlfLfo 的第 ilfo-1 筆 → lsid
//! lsid → PlfLst 裡同 lsid 的那一串 → 第 ilvl 層的 LVL → nfc
//! ```
//!
//! 不繞這一圈的話,所有清單都只能當成項目符號:而畫出來仍然是一份
//! 排版正常的清單,只是 1. 2. 3. 全變成了圓點。

use crate::{fib_fc, le16, le32, Doc};

// FibRgFcLcb97 裡這兩張表的欄位編號。
const FIB_PLF_LST: usize = 73;
const FIB_PLF_LFO: usize = 74;

// 「不編號」的兩種 nfc。其餘的(阿拉伯數字、大小寫羅馬數字、大小寫字母…)
// 都是編號清單。
const NFC_BULLET: i64 = 23;
const NFC_NONE: i64 = 255;

/// 一層清單的格式。
#[derive(Clone, Copy, Debug, Default)]
pub(crate) struct LvlInfo {
    pub nfc: i64,
    pub start: i64,
}

impl LvlInfo {
    pub fn ordered(&self) -> bool {
        self.nfc != NFC_BULLET && self.nfc != NFC_NONE
    }
}

/// 一串清單(LSTF + 它的各層 LVL)。
#[derive(Clone, Debug, Default)]
pub(crate) struct LstInfo {
    pub lvls: Vec<LvlInfo>,
}

impl LstInfo {
    /// 取第 ilvl 層。簡單清單只有一層,所有層都用它。
    fn level(&self, ilvl: i64) -> Option<LvlInfo> {
        if self.lvls.is_empty() {
            return None;
        }
        if ilvl < 0 || ilvl >= self.lvls.len() as i64 {
            return Some(self.lvls[0]);
        }
        Some(self.lvls[ilvl as usize])
    }
}

// 一筆 LSTF 的長度與一筆 LVLF 的長度。
const LSTF_SIZE: usize = 28;
const LVLF_SIZE: usize = 28;

impl Doc {
    /// 讀清單格式表。讀不到就整份當成沒有編號資訊,不是錯誤:
    /// 沒有清單的文件本來就沒有這兩張表。
    pub(crate) fn read_lists(&mut self, wd: &[u8], tbl: &[u8]) {
        self.lists.clear();
        self.read_plf_lst(wd, tbl);
        self.read_plf_lfo(wd, tbl);
    }

    /// 讀清單定義表。
    ///
    /// [雷] lcbPlfLst 只涵蓋「cLst + LSTF 陣列」,後面接著的 LVL 陣列**不算在
    /// 它裡面**。照 lcb 切下去的話,剛好切在第一組 LVL 之前:讀到的 LSTF 完全
    /// 正確、數量也對,只是每一串清單都是零層,於是所有編號清單都變成項目符號。
    /// 沒有任何一個地方會報錯,因為切出來的那一段本身是完好的。
    fn read_plf_lst(&mut self, wd: &[u8], tbl: &[u8]) {
        let (fc, lcb) = fib_fc(wd, FIB_PLF_LST);
        if fc < 0 || lcb < 2 || fc + lcb > tbl.len() as i64 {
            return;
        }
        // 只有 LSTF 陣列的長度由 lcb 決定;LVL 要一直往後讀到表尾。
        let b = &tbl[fc as usize..];
        let c_lst = le16(b, 0) as i16 as i64;
        if c_lst <= 0 || 2 + c_lst * LSTF_SIZE as i64 > lcb {
            return;
        }
        let c_lst = c_lst as usize;
        // 先把 LSTF 讀完,才知道後面跟著幾組 LVL:簡單清單一組,其餘九組。
        let heads: Vec<(u32, bool)> = (0..c_lst)
            .map(|i| {
                let r = &b[2 + i * LSTF_SIZE..];
                (le32(r, 0), r[26] & 1 != 0)
            })
            .collect();
        let mut p = 2 + c_lst * LSTF_SIZE;
        for (lsid, simple) in heads {
            let n = if simple { 1 } else { 9 };
            let mut info = LstInfo::default();
            for _ in 0..n {
                let Some((lv, next)) = read_lvl(b, p) else {
                    // 長度對不上就停手。後面每一組的位置都由前一組算出來,
                    // 錯一個位元組之後讀到的全是別的資料:而那些數字看起來
                    // 仍然像合法的 nfc。
                    if !info.lvls.is_empty() {
                        self.lists.insert(lsid, info);
                    }
                    return;
                };
                info.lvls.push(lv);
                p = next;
            }
            self.lists.insert(lsid, info);
        }
    }

    /// 讀「哪一個 ilfo 對到哪一串清單」。
    ///
    /// 只要 LFO 前面固定的 16 個位元組裡的 lsid;後面的 rgLfoData 是逐層的
    /// 覆寫,長度不定,這裡不解:覆寫改的多半是起始號碼,而編號或項目符號
    /// 這件事在原本那一串上就決定了。
    fn read_plf_lfo(&mut self, wd: &[u8], tbl: &[u8]) {
        let (fc, lcb) = fib_fc(wd, FIB_PLF_LFO);
        if fc < 0 || lcb < 4 || fc + lcb > tbl.len() as i64 {
            return;
        }
        let b = &tbl[fc as usize..(fc + lcb) as usize];
        let n = le32(b, 0) as usize;
        const LFO_SIZE: usize = 16;
        if n == 0 || 4 + n as u64 * LFO_SIZE as u64 > b.len() as u64 {
            return;
        }
        self.lfo = (0..n).map(|i| le32(b, 4 + i * LFO_SIZE)).collect();
    }

    /// 一個段落所屬的那一層清單格式。
    pub(crate) fn list_level(&self, ilfo: i64, ilvl: i64) -> Option<LvlInfo> {
        // ilfo 是 1 起算的索引。
        if ilfo < 1 || ilfo > self.lfo.len() as i64 {
            return None;
        }
        self.lists.get(&self.lfo[ilfo as usize - 1])?.level(ilvl)
    }
}

/// 讀一組 LVL:定長的 LVLF,後面接兩段長度自述的屬性,
/// 再接一個計數字串。回傳下一組的位置。
fn read_lvl(b: &[u8], p: usize) -> Option<(LvlInfo, usize)> {
    if p + LVLF_SIZE > b.len() {
        return None;
    }
    let f = &b[p..p + LVLF_SIZE];
    let lv = LvlInfo { start: le32(f, 0) as i32 as i64, nfc: f[4] as i64 };
    let (cb_chpx, cb_papx) = (f[24] as usize, f[25] as usize);
    let mut q = p + LVLF_SIZE + cb_papx + cb_chpx;
    if q + 2 > b.len() {
        return None;
    }
    // 結尾是編號的樣板字串(「%1.」這種),長度是字元數不是位元組數。
    let cch = le16(b, q) as usize;
    q += 2 + 2 * cch;
    if q > b.len() {
        return None;
    }
    Some((lv, q))
}
