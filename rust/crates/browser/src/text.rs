//! 依顯示格數(全形算兩格)處理字串:寬度、補空白、截斷、千分位。

use wincv_cell::is_wide;

fn cw(r: char) -> i32 {
    if is_wide(r) { 2 } else { 1 }
}

/// 顯示格數。
pub fn width(s: &str) -> i32 {
    s.chars().map(cw).sum()
}

/// 右邊補空白到 w 格。
pub fn pad(s: &str, w: i32) -> String {
    let d = w - width(s);
    if d > 0 { format!("{s}{}", " ".repeat(d as usize)) } else { s.to_string() }
}

/// 左邊補空白到 w 格。
pub fn lpad(s: &str, w: i32) -> String {
    let d = w - width(s);
    if d > 0 { format!("{}{s}", " ".repeat(d as usize)) } else { s.to_string() }
}

/// 截到不超過 w 格;放不下的全形字整個不要。
pub fn truncate(s: &str, w: i32) -> &str {
    let mut n = 0;
    for (i, r) in s.char_indices() {
        if n + cw(r) > w {
            return &s[..i];
        }
        n += cw(r);
    }
    s
}

/// 取尾巴不超過 w 格的部分(路徑太長時留最後幾層)。
pub fn tail_of(s: &str, w: i32) -> &str {
    let mut n = 0;
    let mut start = s.len();
    for (i, r) in s.char_indices().rev() {
        if n + cw(r) > w {
            break;
        }
        n += cw(r);
        start = i;
    }
    &s[start..]
}

/// 千分位逗號。
pub fn comma(v: i64) -> String {
    let digits = v.unsigned_abs().to_string();
    let mut out = String::with_capacity(digits.len() + digits.len() / 3 + 1);
    if v < 0 {
        out.push('-');
    }
    for (i, c) in digits.chars().enumerate() {
        if i > 0 && (digits.len() - i) % 3 == 0 {
            out.push(',');
        }
        out.push(c);
    }
    out
}
