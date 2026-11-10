//! 字寬、裁切與折行(對應 Go 版 `wrap.go`)。

use wincv_cell::is_wide;

use crate::{Span, Style};

/// 字串佔幾格(全形算兩格)。
///
/// Go 版在 markdown 裡自己寫了一份,算法與 `cell.Width` 相同:只看
/// [`is_wide`],沒有零寬字元的特例,每個字至少一格。Rust 版的 cell 沒有
/// 對應 `cell.Width` 的函式,所以留在這裡。
pub(crate) fn width(s: &str) -> i32 {
    s.chars().map(char_width).sum()
}

fn char_width(r: char) -> i32 {
    if is_wide(r) { 2 } else { 1 }
}

pub(crate) fn clip(s: &str, w: i32) -> String {
    if width(s) <= w {
        return s.to_string();
    }
    let mut b = String::new();
    let mut n = 0;
    for r in s.chars() {
        let rw = char_width(r);
        if n + rw > w {
            break;
        }
        b.push(r);
        n += rw;
    }
    b
}

pub(crate) fn pad_to(s: &str, w: i32, align: u8) -> String {
    let d = w - width(s);
    if d <= 0 {
        return s.to_string();
    }
    let d = d as usize;
    match align {
        b'r' => " ".repeat(d) + s,
        b'c' => {
            let l = d / 2;
            " ".repeat(l) + s + &" ".repeat(d - l)
        }
        _ => s.to_string() + &" ".repeat(d),
    }
}

/// 把 tab 換成空白。
///
/// 定位點照 Go 版以**位元組長度**算(`4 - b.Len()%4`),不是顯示格數:
/// tab 前面有中文時,停點會跟畫面上的欄位對不齊。要與 Go 逐位元相同,照抄。
pub(crate) fn expand_tabs(s: &str) -> String {
    let mut b = String::new();
    for r in s.chars() {
        if r == '\t' {
            let n = 4 - b.len() % 4;
            b.push_str(&" ".repeat(n));
        } else {
            b.push(r);
        }
    }
    b
}

/// Go 版的 itoa:負數回空字串(迴圈一次都不跑)。
pub(crate) fn itoa(n: i64) -> String {
    if n == 0 {
        return "0".to_string();
    }
    if n < 0 {
        return String::new();
    }
    n.to_string()
}

/// 把帶樣式的片段折成固定寬度的列。
///
/// 折行要在**片段之間**與**片段內部**都能發生,而且樣式要跟著走:
/// 一個粗體片段被折成兩列時,兩列都要是粗體。所以不能先把文字接起來
/// 折完再套樣式。
///
/// 中文不靠空白斷字:CJK 之間任何位置都能折。只按空白折的話,
/// 一整段沒有空白的中文會變成一列超長的字被硬切在畫面邊緣。
pub(crate) fn wrap_spans(spans: &[Span], cols: i32) -> Vec<Vec<Span>> {
    let cols = cols.max(2);
    let mut out: Vec<Vec<Span>> = Vec::new();
    let mut cur: Vec<Span> = Vec::new();
    let mut used = 0i32;

    fn push(out: &mut Vec<Vec<Span>>, cur: &mut Vec<Span>, used: &mut i32) {
        if !cur.is_empty() {
            out.push(std::mem::take(cur));
            *used = 0;
        }
    }
    // 相鄰且同樣式的片段要併起來。不併的話每個中文字都是一個獨立片段
    // (CJK 是逐字切的折行單位),一段千字的中文會產生上千個片段,
    // 後面每一層都得為此付代價。
    fn add(cur: &mut Vec<Span>, used: &mut i32, text: &str, st: Style, href: &str) {
        if text.is_empty() {
            return;
        }
        match cur.last_mut() {
            Some(l) if l.style == st && l.href == href => l.text.push_str(text),
            _ => cur.push(Span { text: text.to_string(), style: st, href: href.to_string() }),
        }
        *used += width(text);
    }

    for sp in spans {
        for word in split_words(&sp.text) {
            let mut word: &str = &word;
            let w = width(word);
            // 行首的空白丟掉,不然折行處會多一格縮排。
            if used == 0 && word.trim().is_empty() {
                continue;
            }
            if used + w > cols && used > 0 {
                push(&mut out, &mut cur, &mut used);
                if word.trim().is_empty() {
                    continue;
                }
            }
            // 單一「字」就超過整列寬度(超長網址、無空白的長字串):
            // 硬切,不然它會永遠放不進去而卡住。
            while width(word) > cols {
                let head = clip(word, cols - used);
                if head.is_empty() {
                    push(&mut out, &mut cur, &mut used);
                    continue;
                }
                add(&mut cur, &mut used, &head, sp.style, &sp.href);
                word = &word[head.len()..];
                push(&mut out, &mut cur, &mut used);
            }
            add(&mut cur, &mut used, word, sp.style, &sp.href);
        }
    }
    push(&mut out, &mut cur, &mut used);
    if out.is_empty() {
        out.push(Vec::new());
    }
    out
}

/// 把一段文字切成可以折行的單位。
///
/// 空白後面接著下一個詞算一個單位(空白留在詞前面,折行時整個丟掉);
/// CJK 每個字自己一個單位。
fn split_words(s: &str) -> Vec<String> {
    let mut out = Vec::new();
    let mut b = String::new();
    for r in s.chars() {
        if is_wide(r) {
            if !b.is_empty() {
                out.push(std::mem::take(&mut b));
            }
            out.push(r.to_string());
            continue;
        }
        if r == ' ' || r == '\t' {
            if !b.is_empty() {
                out.push(std::mem::take(&mut b));
            }
            b.push(' ');
            continue;
        }
        b.push(r);
    }
    if !b.is_empty() {
        out.push(b);
    }
    out
}
