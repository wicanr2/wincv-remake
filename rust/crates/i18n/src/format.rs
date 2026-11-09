//! Go 格式動詞的執行期展開。只做語系 key 裡用得到的那幾種:
//! `%d %s %v %w %q %c %x %X %f %g %%`,加上旗標 `-` `0` `+` 空白、寬度、精度。

use std::fmt::Write;

/// 格式化的參數。
#[derive(Clone, Debug, PartialEq)]
pub enum Arg {
    Str(String),
    Int(i64),
    Uint(u64),
    Float(f64),
    Char(char),
}

macro_rules! from_int {
    ($($t:ty),*) => { $(impl From<$t> for Arg { fn from(v: $t) -> Arg { Arg::Int(v as i64) } })* };
}
from_int!(i8, i16, i32, i64, isize);

macro_rules! from_uint {
    ($($t:ty),*) => { $(impl From<$t> for Arg { fn from(v: $t) -> Arg { Arg::Uint(v as u64) } })* };
}
from_uint!(u8, u16, u32, u64, usize);

impl From<f64> for Arg {
    fn from(v: f64) -> Arg {
        Arg::Float(v)
    }
}
impl From<f32> for Arg {
    fn from(v: f32) -> Arg {
        Arg::Float(v as f64)
    }
}
impl From<char> for Arg {
    fn from(v: char) -> Arg {
        Arg::Char(v)
    }
}
impl From<&str> for Arg {
    fn from(v: &str) -> Arg {
        Arg::Str(v.to_string())
    }
}
impl From<String> for Arg {
    fn from(v: String) -> Arg {
        Arg::Str(v)
    }
}
impl From<&String> for Arg {
    fn from(v: &String) -> Arg {
        Arg::Str(v.clone())
    }
}

/// 字串裡的格式動詞(不含 `%%`),依出現順序。測試用。
#[cfg(test)]
pub(crate) fn verbs(s: &str) -> Vec<String> {
    let mut out = Vec::new();
    let b: Vec<char> = s.chars().collect();
    let mut i = 0;
    while i < b.len() {
        if b[i] != '%' {
            i += 1;
            continue;
        }
        let start = i;
        i += 1;
        while i < b.len() && "-+# 0123456789.".contains(b[i]) {
            i += 1;
        }
        if i < b.len() {
            if b[i] != '%' {
                out.push(b[start..=i].iter().collect());
            }
            i += 1;
        }
    }
    out
}

/// Go 的 strconv.Quote:雙引號包起來,控制字元與無法顯示的字元跳脫。
fn quote(s: &str) -> String {
    let mut o = String::from("\"");
    for c in s.chars() {
        match c {
            '"' => o.push_str("\\\""),
            '\\' => o.push_str("\\\\"),
            '\n' => o.push_str("\\n"),
            '\t' => o.push_str("\\t"),
            '\r' => o.push_str("\\r"),
            '\x07' => o.push_str("\\a"),
            '\x08' => o.push_str("\\b"),
            '\x0C' => o.push_str("\\f"),
            '\x0B' => o.push_str("\\v"),
            c if (c as u32) < 0x20 || c as u32 == 0x7F => {
                let _ = write!(o, "\\x{:02x}", c as u32);
            }
            c if c.is_control() => {
                let _ = write!(o, "\\u{:04x}", c as u32);
            }
            c => o.push(c),
        }
    }
    o.push('"');
    o
}

/// Go 的 %g(精度未指定):最短表示,指數小於 -4 或不小於 21 時用科學記號。
fn go_g(v: f64) -> String {
    if v == 0.0 || !v.is_finite() {
        return format!("{v}");
    }
    let exp = v.abs().log10().floor() as i32;
    if (-4..21).contains(&exp) {
        return format!("{v}");
    }
    let s = format!("{v:e}");
    let (m, e) = s.split_once('e').unwrap_or((&s, "0"));
    let e: i32 = e.parse().unwrap_or(0);
    format!("{m}e{}{:02}", if e < 0 { '-' } else { '+' }, e.abs())
}

struct Spec {
    minus: bool,
    zero: bool,
    plus: bool,
    space: bool,
    width: Option<usize>,
    prec: Option<usize>,
}

fn pad(o: &mut String, body: String, sp: &Spec, numeric: bool) {
    let n = body.chars().count();
    let w = sp.width.unwrap_or(0);
    if n >= w {
        o.push_str(&body);
    } else if sp.minus {
        o.push_str(&body);
        o.extend(std::iter::repeat_n(' ', w - n));
    } else if sp.zero && numeric {
        // 補零放在正負號之後。
        let (sign, digits) = match body.strip_prefix(['-', '+']) {
            Some(rest) => (&body[..1], rest),
            None => ("", body.as_str()),
        };
        o.push_str(sign);
        o.extend(std::iter::repeat_n('0', w - n));
        o.push_str(digits);
    } else {
        o.extend(std::iter::repeat_n(' ', w - n));
        o.push_str(&body);
    }
}

fn int_body(v: i128, sp: &Spec) -> String {
    let mut s = v.to_string();
    if v >= 0 {
        if sp.plus {
            s.insert(0, '+');
        } else if sp.space {
            s.insert(0, ' ');
        }
    }
    s
}

/// 依 Go 的規則展開格式字串。參數不夠時寫 `%!d(MISSING)`,多出來的寫
/// `%!(EXTRA ...)`,與 Go 相同 —— 錯在哪裡看得到,而不是安靜地吞掉。
pub fn sprintf(fmt: &str, args: &[Arg]) -> String {
    let mut o = String::with_capacity(fmt.len() + 16);
    let mut it = fmt.chars().peekable();
    let mut ai = 0;
    while let Some(c) = it.next() {
        if c != '%' {
            o.push(c);
            continue;
        }
        let mut sp = Spec { minus: false, zero: false, plus: false, space: false, width: None, prec: None };
        while let Some(&f) = it.peek() {
            match f {
                '-' => sp.minus = true,
                '0' => sp.zero = true,
                '+' => sp.plus = true,
                ' ' => sp.space = true,
                '#' => {}
                _ => break,
            }
            it.next();
        }
        let num = |it: &mut std::iter::Peekable<std::str::Chars>| {
            let mut n: Option<usize> = None;
            while let Some(d) = it.peek().and_then(|c| c.to_digit(10)) {
                n = Some(n.unwrap_or(0) * 10 + d as usize);
                it.next();
            }
            n
        };
        sp.width = num(&mut it);
        if it.peek() == Some(&'.') {
            it.next();
            sp.prec = Some(num(&mut it).unwrap_or(0));
        }
        let Some(verb) = it.next() else {
            o.push_str("%!(NOVERB)");
            break;
        };
        if verb == '%' {
            o.push('%');
            continue;
        }
        let Some(arg) = args.get(ai) else {
            let _ = write!(o, "%!{verb}(MISSING)");
            continue;
        };
        ai += 1;
        match (verb, arg) {
            ('d' | 'v', Arg::Int(v)) => pad(&mut o, int_body(*v as i128, &sp), &sp, true),
            ('d' | 'v', Arg::Uint(v)) => pad(&mut o, int_body(*v as i128, &sp), &sp, true),
            ('x', Arg::Int(v)) => pad(&mut o, if *v < 0 { format!("-{:x}", v.unsigned_abs()) } else { format!("{v:x}") }, &sp, true),
            ('X', Arg::Int(v)) => pad(&mut o, if *v < 0 { format!("-{:X}", v.unsigned_abs()) } else { format!("{v:X}") }, &sp, true),
            ('x', Arg::Uint(v)) => pad(&mut o, format!("{v:x}"), &sp, true),
            ('X', Arg::Uint(v)) => pad(&mut o, format!("{v:X}"), &sp, true),
            ('c', Arg::Int(v)) => pad(&mut o, char::from_u32(*v as u32).unwrap_or('\u{FFFD}').to_string(), &sp, false),
            ('c', Arg::Uint(v)) => pad(&mut o, char::from_u32(*v as u32).unwrap_or('\u{FFFD}').to_string(), &sp, false),
            ('c', Arg::Char(ch)) => pad(&mut o, ch.to_string(), &sp, false),
            // Go 的 rune 是整數:%v、%d 印碼位。
            ('d' | 'v', Arg::Char(ch)) => pad(&mut o, int_body(*ch as i128, &sp), &sp, true),
            ('f', Arg::Float(v)) => pad(&mut o, format!("{:.*}", sp.prec.unwrap_or(6), v), &sp, true),
            ('g' | 'v', Arg::Float(v)) => pad(&mut o, go_g(*v), &sp, true),
            ('s' | 'v' | 'w', Arg::Str(s)) => {
                let body = match sp.prec {
                    Some(p) => s.chars().take(p).collect(),
                    None => s.clone(),
                };
                pad(&mut o, body, &sp, false)
            }
            ('q', Arg::Str(s)) => pad(&mut o, quote(s), &sp, false),
            ('q', Arg::Char(ch)) => pad(&mut o, format!("'{ch}'"), &sp, false),
            ('s' | 'w', Arg::Int(v)) => pad(&mut o, format!("%!{verb}(int={v})"), &sp, false),
            _ => {
                let _ = write!(o, "%!{verb}({arg:?})");
            }
        }
    }
    if ai < args.len() {
        o.push_str("%!(EXTRA ");
        let rest: Vec<String> = args[ai..].iter().map(|a| format!("{a:?}")).collect();
        o.push_str(&rest.join(", "));
        o.push(')');
    }
    o
}

#[cfg(test)]
mod tests {
    use super::*;

    fn s(f: &str, a: &[Arg]) -> String {
        sprintf(f, a)
    }

    /// 期望值是 Go fmt.Sprintf 對同一組輸入的實際輸出(Go 1.22.12,
    /// 以 .cache/gofmtcheck 的小程式跑過,2026-11-09)。
    #[test]
    fn matches_go() {
        assert_eq!(s("%d 個", &[3.into()]), "3 個");
        assert_eq!(s("%2d|%-12s|", &[7.into(), "black".into()]), " 7|black       |");
        assert_eq!(s("%08X", &[0xBEEFu32.into()]), "0000BEEF");
        assert_eq!(s("%02X%02X", &[0xA4u8.into(), 0x40u8.into()]), "A440");
        assert_eq!(s("%04X", &[0x1Fu32.into()]), "001F");
        assert_eq!(s("%.1f×", &[1.25f64.into()]), "1.2×");
        assert_eq!(s("%.1f", &[1.35f64.into()]), "1.4");
        assert_eq!(s("%q", &["a\"b\n中".into()]), "\"a\\\"b\\n中\"");
        assert_eq!(s("%c", &['中'.into()]), "中");
        assert_eq!(s("100%%", &[]), "100%");
        assert_eq!(s("%g", &[0.5f64.into()]), "0.5");
        assert_eq!(s("%g", &[1e21f64.into()]), "1e+21");
        assert_eq!(s("%v / %w", &["x".into(), "err".into()]), "x / err");
        assert_eq!(s("%d %d", &[1.into()]), "1 %!d(MISSING)");
    }
}
