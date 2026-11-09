//! 回答一個問題:「這個字的外框長什麼樣」(對應 Go 版 `internal/vecfont`)。
//!
//! 點陣那一路(`wincv-fnt`、`wincv-eten`、`wincv-ttf`)產出的是固定格點的字模,
//! 格點畫面用那個就夠。但有些地方要在任意字級、任意位置畫字,SVG 裡的標題與
//! 座標軸刻度就是這樣,那時需要的是外框,不是字模。
//!
//! 字型來源與點陣那一路共用:先問內嵌的,再問系統的(`wincv_ttf::candidates`)。
//! 每個字逐一問下去,第一個畫得出來的就用,所以中英混排不必先決定字型。
//!
//! 座標的量化照 Go 版 sfnt(見 `wincv_ttf::outline`),所以同一份字型兩邊得到相同的
//! 外框。字型清單可能不同:Go 版的 sfnt 讀不了某些 `.ttc`(實測 uming、wqy-zenhei),
//! 這裡讀得了,於是前 [`MAX_FONTS`] 份可能是不同的組合。

use std::borrow::Cow;
use std::collections::HashMap;
use std::sync::{Mutex, OnceLock};

use wincv_ttf::Seg as RawSeg;

/// 外框座標的字身大小。外框的數值以此為單位,乘上「字級 / Em」就落到畫面上。
pub const EM: i32 = 1000;

/// 最多載幾份字型。
///
/// 一台機器上可以有上百份字型,全部載進來只為了找幾個罕用字並不划算。前幾份
/// (通常含一份 CJK)已經涵蓋絕大多數內容。
pub const MAX_FONTS: usize = 8;

/// 外框的一段。`op` 是 `b'm'` 移動、`b'l'` 直線、`b'q'` 二次貝茲、`b'c'` 三次貝茲;
/// `args` 依序放各控制點的 x、y。座標 Y 軸向下,與畫面同向。
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct Seg {
    pub op: u8,
    pub args: [f64; 6],
}

#[derive(Clone)]
struct Glyph {
    segs: Vec<Seg>,
    adv: f64,
    ok: bool,
}

/// 一組字型,對外像單一字型:問它一個字,它自己決定由誰畫。
#[derive(Default)]
pub struct Set {
    /// 內嵌的字型直接借用執行檔裡的那一份,不複製(CJK 字型一份就有十幾 MB)。
    fonts: OnceLock<Vec<Cow<'static, [u8]>>>,
    cache: Mutex<HashMap<char, Glyph>>,
}

/// 共用的那一組。字型只在第一次要用時才載入。
pub fn default_set() -> &'static Set {
    static STD: OnceLock<Set> = OnceLock::new();
    STD.get_or_init(Set::default)
}

/// 讀得起來的字型才收。`.ttc` 取第一份:裡面通常是同一套字的不同字重。
fn parses(b: &[u8]) -> bool {
    ttf_parser::Face::parse(b, 0).is_ok()
}

impl Set {
    /// 用指定的幾份字型組一組,不碰內嵌與系統字型。對照工具(`vecsum`)用它固定字型,
    /// 好與 Go 版逐段比對外框。讀不起來的字型略過。
    pub fn with_fonts(fonts: Vec<Cow<'static, [u8]>>) -> Set {
        let s = Set::default();
        let _ = s.fonts.set(fonts.into_iter().filter(|b| parses(b)).collect());
        s
    }

    fn fonts(&self) -> &[Cow<'static, [u8]>] {
        self.fonts.get_or_init(|| {
            let mut out = Vec::new();
            for b in wincv_bundled::fallbacks() {
                if out.len() >= MAX_FONTS {
                    return out;
                }
                if parses(b) {
                    out.push(Cow::Borrowed(b));
                }
            }
            let mut seen = std::collections::HashSet::new();
            for path in wincv_ttf::candidates() {
                if out.len() >= MAX_FONTS {
                    break;
                }
                if !seen.insert(path.clone()) {
                    continue;
                }
                if let Ok(b) = std::fs::read(&path) {
                    if parses(&b) {
                        out.push(Cow::Owned(b));
                    }
                }
            }
            out
        })
    }

    /// 有沒有任何字型可用。沒有的話畫字這件事整個做不到,呼叫端要能分辨
    /// 「這段沒有字」與「這台機器沒有字型」。
    pub fn ready(&self) -> bool {
        !self.fonts().is_empty()
    }

    /// 一個字的外框與前進寬度(單位同 [`EM`])。
    ///
    /// 第三項為假表示所有字型都沒有這個字。空白有前進寬度但沒有外框,那時外框是
    /// 空的而第三項為真。兩者要分開,不然空白會被當成缺字。
    pub fn glyph(&self, r: char) -> (Vec<Seg>, f64, bool) {
        let fonts = self.fonts();
        let mut cache = self.cache.lock().unwrap_or_else(|e| e.into_inner());
        let g = cache.entry(r).or_insert_with(|| lookup(fonts, r));
        (g.segs.clone(), g.adv, g.ok)
    }
}

fn lookup(fonts: &[Cow<'static, [u8]>], r: char) -> Glyph {
    let ppem = EM << 6;
    for b in fonts {
        let Ok(face) = ttf_parser::Face::parse(b, 0) else { continue };
        let Some(gid) = face.glyph_index(r).filter(|g| g.0 != 0) else { continue };
        let Some(adv) = wincv_ttf::advance(&face, gid, ppem) else { continue };
        // 彩色 emoji 之類的沒有單色外框,得到的是空的外框。前進寬度還是對的,
        // 讓後面的字排在正確位置上。
        return Glyph { segs: convert(&wincv_ttf::outline(&face, gid, ppem)), adv: from_fixed(adv), ok: true };
    }
    Glyph { segs: Vec::new(), adv: 0.0, ok: false }
}

fn from_fixed(v: i32) -> f64 {
    v as f64 / 64.0
}

/// 換成本模組的形式。輪廓結尾只在筆不在起點時補一條回起點的直線,與 Go 版 sfnt
/// 列出的線段相同。
fn convert(input: &[RawSeg]) -> Vec<Seg> {
    let mut out = Vec::with_capacity(input.len());
    let (mut first, mut pen) = ((0, 0), (0, 0));
    let mut push = |op: u8, pts: &[(i32, i32)]| {
        let mut args = [0.0; 6];
        for (i, &(x, y)) in pts.iter().enumerate() {
            args[i * 2] = from_fixed(x);
            args[i * 2 + 1] = from_fixed(y);
        }
        out.push(Seg { op, args });
    };
    for s in input {
        match *s {
            RawSeg::M(x, y) => {
                first = (x, y);
                pen = first;
                push(b'm', &[pen]);
            }
            RawSeg::L(x, y) => {
                pen = (x, y);
                push(b'l', &[pen]);
            }
            RawSeg::Q(a, b, x, y) => {
                pen = (x, y);
                push(b'q', &[(a, b), pen]);
            }
            RawSeg::C(a, b, c, d, x, y) => {
                pen = (x, y);
                push(b'c', &[(a, b), (c, d), pen]);
            }
            RawSeg::Z => {
                if pen != first {
                    pen = first;
                    push(b'l', &[pen]);
                }
            }
        }
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn glyph_outline() {
        let s = default_set();
        if !s.ready() {
            eprintln!("這台機器沒有可用的字型,跳過");
            return;
        }
        let (segs, adv, ok) = s.glyph('A');
        assert!(ok, "'A' 查不到字形");
        assert!(!segs.is_empty(), "'A' 沒有外框");
        assert!(adv > 0.0 && adv <= (EM * 2) as f64, "前進寬度 {adv} 不合理");
        assert_eq!(segs[0].op, b'm', "外框第一段應該是 moveto");
    }

    /// 空白有寬度但沒有形狀。兩者混為一談的話,空白會被當成缺字,整行的字距就錯了。
    #[test]
    fn space_has_width_but_no_outline() {
        let s = default_set();
        if !s.ready() {
            return;
        }
        let (segs, adv, ok) = s.glyph(' ');
        assert!(ok, "空白查不到");
        assert!(segs.is_empty(), "空白不該有外框,卻有 {} 段", segs.len());
        assert!(adv > 0.0, "空白的寬度是 {adv}");
    }

    /// 查兩次要得到同一份結果(有快取,不能第二次才走別條路)。
    #[test]
    fn cached() {
        let s = default_set();
        if !s.ready() {
            return;
        }
        assert_eq!(s.glyph('W'), s.glyph('W'));
    }

    /// 輪廓結尾:筆已在起點就不再補線,不在起點才補。
    #[test]
    fn close_adds_line_only_when_needed() {
        let raw = [RawSeg::M(0, 0), RawSeg::L(64, 0), RawSeg::L(64, 64), RawSeg::Z, RawSeg::M(0, 0), RawSeg::Q(64, 0, 0, 0), RawSeg::Z];
        let ops: Vec<u8> = convert(&raw).iter().map(|s| s.op).collect();
        assert_eq!(ops, b"mlllmq".to_vec());
    }
}
