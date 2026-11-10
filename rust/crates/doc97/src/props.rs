//! 樣式表、段落屬性頁與字元屬性頁(對應 Go 版 `props.go`)。

use crate::{fib_fc, go_search, gosort, le16, le32, Doc, FIB_PLCF_BTE_CHP, FIB_PLCF_BTE_PAP, FIB_STSHF};

/// 一個段落的屬性,連同它在串流裡的位元組範圍。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) struct ParaProp {
    pub fc_start: i32,
    pub fc_end: i32,
    pub istd: i64,
    pub in_table: bool,
    /// 這一段是「列結尾」的標記,不是儲存格
    pub ttp: bool,
    /// 不為 0 表示屬於某一串清單
    pub ilfo: i64,
    pub ilvl: i64,
    /// \outlinelevel + 1,0 表示沒有指定
    pub outline: i64,
}

impl ParaProp {
    pub fn none() -> ParaProp {
        ParaProp { fc_start: 0, fc_end: 0, istd: -1, in_table: false, ttp: false, ilfo: 0, ilvl: 0, outline: 0 }
    }
}

/// 一段字元格式相同的範圍。
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub(crate) struct CharRun {
    pub fc_start: i32,
    pub fc_end: i32,
    pub bold: bool,
    pub italic: bool,
    pub strike: bool,
}

// 用得到的段落與字元 sprm。
const SPRM_P_ILVL: u16 = 0x260A;
const SPRM_P_ILFO: u16 = 0x460B;
const SPRM_P_F_IN_TBL: u16 = 0x2416;
const SPRM_P_F_TTP: u16 = 0x2417;
const SPRM_P_OUT_LVL: u16 = 0x2640;
const SPRM_C_F_BOLD: u16 = 0x0835;
const SPRM_C_F_ITALIC: u16 = 0x0836;
const SPRM_C_F_STRIKE: u16 = 0x0837;

impl Doc {
    /// 讀樣式表,取出每個樣式的識別碼。
    ///
    /// 識別碼(sti)才是「這是不是標題」的真值:它與介面語言無關,
    /// 中文版的「標題 1」與英文版的 "heading 1" 識別碼都是 1。
    /// 用名字比對會在每一種語言版本上各壞一次。
    pub(crate) fn read_styles(&mut self, wd: &[u8], tbl: &[u8]) {
        let (fc, lcb) = fib_fc(wd, FIB_STSHF);
        if fc < 0 || lcb < 4 || fc + lcb > tbl.len() as i64 {
            return;
        }
        let st = &tbl[fc as usize..(fc + lcb) as usize];
        let cb_stshi = le16(st, 0) as usize;
        if cb_stshi < 4 || 2 + cb_stshi > st.len() {
            return;
        }
        let cstd = le16(st, 2) as usize;
        if cstd > 4096 {
            return;
        }
        self.stis = vec![-1; cstd];
        let mut p = 2 + cb_stshi;
        let mut i = 0;
        while i < cstd && p + 2 <= st.len() {
            let cb_std = le16(st, p) as usize;
            p += 2;
            // cbStd 小於 2 的 STD 連識別碼都放不下:不論在表中間還是表尾,
            // 都當成不知道,不去讀下一筆的長度欄位或表外。
            if cb_std < 2 || p + cb_std > st.len() {
                p += cb_std;
                i += 1;
                continue;
            }
            // STD 的第一個 16 位元裡,低 12 位是樣式識別碼。
            self.stis[i] = (le16(st, p) & 0x0FFF) as i64;
            p += cb_std;
            i += 1;
        }
    }

    /// 某個樣式編號是第幾階標題,0 表示不是標題。
    pub(crate) fn heading_of(&self, istd: i64) -> i64 {
        if istd < 0 || istd >= self.stis.len() as i64 {
            return 0;
        }
        let sti = self.stis[istd as usize];
        if (1..=9).contains(&sti) {
            return sti.min(6);
        }
        0
    }

    /// 走段落屬性頁。
    pub(crate) fn read_para_props(&mut self, wd: &[u8], tbl: &[u8]) {
        for page in fkp_pages(wd, tbl, FIB_PLCF_BTE_PAP) {
            let crun = page[511] as usize;
            if crun == 0 || 4 * (crun + 1) + 13 * crun > 511 {
                continue;
            }
            for j in 0..crun {
                let mut p = ParaProp {
                    fc_start: le32(page, j * 4) as i32,
                    fc_end: le32(page, (j + 1) * 4) as i32,
                    ..ParaProp::none()
                };
                // 每一筆的第一個位元組是 PAPX 在這一頁裡的「字組」偏移量;
                // 0 表示這一段沒有自己的屬性,整段沿用樣式的預設值。
                let word_off = page[4 * (crun + 1) + j * 13] as usize;
                if word_off != 0 {
                    if let Some(g) = papx_at(page, word_off * 2) {
                        if g.len() >= 2 {
                            p.istd = le16(g, 0) as i64;
                            apply_para_sprms(&mut p, &g[2..]);
                        }
                    }
                }
                self.paras.push(p);
            }
        }
        gosort::slice(&mut self.paras, |a, b| a.fc_start < b.fc_start);
    }

    /// 走字元屬性頁。
    pub(crate) fn read_char_runs(&mut self, wd: &[u8], tbl: &[u8]) {
        for page in fkp_pages(wd, tbl, FIB_PLCF_BTE_CHP) {
            let crun = page[511] as usize;
            if crun == 0 || 4 * (crun + 1) + crun > 511 {
                continue;
            }
            for j in 0..crun {
                let mut r = CharRun {
                    fc_start: le32(page, j * 4) as i32,
                    fc_end: le32(page, (j + 1) * 4) as i32,
                    ..CharRun::default()
                };
                // 字元屬性頁的索引只有一個位元組,而且長度的寫法也比較簡單:
                // 它跟段落屬性頁長得像但不一樣,混用會讀到偏移一格的資料。
                let off = page[4 * (crun + 1) + j] as usize * 2;
                if off > 0 && off < page.len() {
                    let cb = page[off] as usize;
                    if off + 1 + cb <= page.len() {
                        apply_char_sprms(&mut r, &page[off + 1..off + 1 + cb]);
                    }
                }
                self.runs.push(r);
            }
        }
        gosort::slice(&mut self.runs, |a, b| a.fc_start < b.fc_start);
    }

    /// 涵蓋某個位元組位置的段落屬性。
    pub(crate) fn para_at(&self, fc: i32) -> ParaProp {
        let i = go_search(self.paras.len(), |i| self.paras[i].fc_end > fc);
        if i < self.paras.len() && self.paras[i].fc_start <= fc {
            return self.paras[i];
        }
        ParaProp::none()
    }

    /// 涵蓋某個位元組位置的字元屬性。
    pub(crate) fn run_at(&self, fc: i32) -> CharRun {
        let i = go_search(self.runs.len(), |i| self.runs[i].fc_end > fc);
        if i < self.runs.len() && self.runs[i].fc_start <= fc {
            return self.runs[i];
        }
        CharRun::default()
    }
}

/// 把一個 PLC 拆開:前面是 n+1 個位置,後面是 n 筆固定大小的資料。
pub(crate) fn plc_data(b: &[u8], size: usize) -> (Vec<i32>, Vec<&[u8]>) {
    if b.len() < 4 + size {
        return (Vec::new(), Vec::new());
    }
    let n = (b.len() - 4) / (4 + size);
    if n == 0 {
        return (Vec::new(), Vec::new());
    }
    let pos = (0..=n).map(|i| le32(b, i * 4) as i32).collect();
    let base = 4 * (n + 1);
    let data = (0..n).map(|i| &b[base + i * size..base + (i + 1) * size]).collect();
    (pos, data)
}

/// 找出屬性頁的頁號。
///
/// 屬性不是集中放在一張表裡,而是散在 WordDocument 串流的 512 位元組
/// 「頁」上,由這個 PLC 指出哪幾頁。這個設計是為了讓編輯時只要改一頁。
fn fkp_pages<'a>(wd: &'a [u8], tbl: &[u8], idx: usize) -> Vec<&'a [u8]> {
    let (fc, lcb) = fib_fc(wd, idx);
    if fc < 0 || lcb <= 0 || fc + lcb > tbl.len() as i64 {
        return Vec::new();
    }
    let (_, data) = plc_data(&tbl[fc as usize..(fc + lcb) as usize], 4);
    let mut out = Vec::new();
    for d in data {
        let pn = (le32(d, 0) & 0x003FFFFF) as usize;
        let off = pn * 512;
        if off + 512 > wd.len() {
            continue;
        }
        out.push(&wd[off..off + 512]);
    }
    out
}

/// 取出一頁裡某個位置的段落屬性串。
///
/// 長度有兩種寫法:第一個位元組不為 0 時它就是長度(以字組計,再扣掉
/// 自己那半個);為 0 時真正的長度在下一個位元組。第二種是後來為了
/// 放得下比較長的屬性才加的。
fn papx_at(page: &[u8], off: usize) -> Option<&[u8]> {
    if off >= page.len() {
        return None;
    }
    let cb = page[off] as usize;
    if cb == 0 {
        if off + 1 >= page.len() {
            return None;
        }
        let n = page[off + 1] as usize * 2;
        if off + 2 + n > page.len() {
            return None;
        }
        return Some(&page[off + 2..off + 2 + n]);
    }
    let n = cb * 2 - 1;
    if off + 1 + n > page.len() {
        return None;
    }
    Some(&page[off + 1..off + 1 + n])
}

fn apply_para_sprms(p: &mut ParaProp, g: &[u8]) {
    walk_sprms(g, |op, operand| match op {
        SPRM_P_F_IN_TBL => p.in_table = !operand.is_empty() && operand[0] != 0,
        SPRM_P_F_TTP => p.ttp = !operand.is_empty() && operand[0] != 0,
        SPRM_P_ILVL => {
            if !operand.is_empty() {
                p.ilvl = operand[0] as i64;
            }
        }
        SPRM_P_ILFO => {
            if operand.len() >= 2 {
                p.ilfo = le16(operand, 0) as i64;
            }
        }
        SPRM_P_OUT_LVL => {
            if !operand.is_empty() && operand[0] <= 8 {
                p.outline = operand[0] as i64 + 1;
            }
        }
        _ => {}
    });
}

fn apply_char_sprms(r: &mut CharRun, g: &[u8]) {
    walk_sprms(g, |op, operand| {
        if operand.is_empty() {
            return;
        }
        // 這幾個是開關型的:0 關、1 開,128 表示沿用樣式、129 表示反轉。
        // 後兩種要真正算出來得先解樣式的預設值,這裡只認明確的開與關。
        let on = operand[0] == 1;
        match op {
            SPRM_C_F_BOLD => r.bold = on,
            SPRM_C_F_ITALIC => r.italic = on,
            SPRM_C_F_STRIKE => r.strike = on,
            _ => {}
        }
    });
}

/// 走一串屬性。
///
/// 每一筆的長度藏在操作碼本身:第 13–15 位是「操作元大小」的編碼。
/// 算錯一筆的長度,後面整串就全部錯位,而錯位之後解出來的仍然是
/// 合法的操作碼:所以會得到一份自洽但完全錯誤的屬性。
fn walk_sprms(g: &[u8], mut f: impl FnMut(u16, &[u8])) {
    let mut i = 0;
    while i + 2 <= g.len() {
        let op = le16(g, i);
        i += 2;
        let n = match (op >> 13) & 7 {
            0 | 1 => 1,
            2 | 4 | 5 => 2,
            3 => 4,
            7 => 3,
            _ => {
                // 6:長度寫在下一個位元組。
                if i >= g.len() {
                    return;
                }
                let mut n = g[i] as usize;
                if n == 255 {
                    // 只有表格定義用這種寫法:長度改成兩個位元組。
                    if i + 3 > g.len() {
                        return;
                    }
                    n = le16(g, i + 1) as usize + 2;
                }
                i += 1;
                n
            }
        };
        if i + n > g.len() {
            return;
        }
        f(op, &g[i..i + n]);
        i += n;
    }
}
