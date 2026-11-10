//! Go 的 `strconv.ParseFloat`、`strconv.FormatFloat(f, 'f', -1, 64)` 與 `time`
//! 裡用得到的那一小部分,照 Go 的行為寫。

/// Go 的 `strconv.ParseFloat(s, 64)`:回傳 None 表示 Go 會回報錯誤。
///
/// 文法照 Go 的 `readFloat` 逐行搬(與 Rust 的 `str::parse::<f64>` 不同的地方):
///   - 數字之間可以有底線(`1_000`),規則是 Go 的 `underscoreOK`。
///   - 十六進位浮點數(`0x1p-2`),一定要有 `p` 指數。
///   - 超出範圍(`1e400`)是錯誤,不是無限大。
///   - `NaN` 不能帶正負號,`Inf` / `Infinity` 可以;大小寫不拘。
///
/// 十進位的值交給 Rust 的 parse 算:兩邊都是「最接近的那一個浮點數」,結果相同。
pub fn parse_float(s: &str) -> Option<f64> {
    if let Some((f, n)) = special(s) {
        return (n == s.len()).then_some(f);
    }
    let r = read_float(s)?;
    if r.n != s.len() {
        return None;
    }
    if r.hex {
        return atof_hex(r.mantissa, r.exp, r.neg, r.trunc);
    }
    let clean: String = s[..r.n].chars().filter(|&c| c != '_').collect();
    let f: f64 = clean.parse().ok()?;
    if f.is_infinite() {
        return None; // Go 的 ErrRange
    }
    Some(f)
}

fn lower(c: u8) -> u8 {
    c | 0x20
}

fn common_prefix_len_ignore_case(s: &[u8], prefix: &[u8]) -> usize {
    let n = prefix.len().min(s.len());
    for i in 0..n {
        let mut c = s[i];
        if c.is_ascii_uppercase() {
            c += b'a' - b'A';
        }
        if c != prefix[i] {
            return i;
        }
    }
    n
}

/// Go 的 `special`:開頭的 NaN / Inf / Infinity。回傳值與吃掉的長度。
fn special(s: &str) -> Option<(f64, usize)> {
    let b = s.as_bytes();
    if b.is_empty() {
        return None;
    }
    let (sign, nsign, rest) = match b[0] {
        b'+' => (1.0, 1, &b[1..]),
        b'-' => (-1.0, 1, &b[1..]),
        b'i' | b'I' => (1.0, 0, b),
        b'n' | b'N' => {
            return (common_prefix_len_ignore_case(b, b"nan") == 3).then_some((f64::NAN, 3));
        }
        _ => return None,
    };
    let mut n = common_prefix_len_ignore_case(rest, b"infinity");
    if 3 < n && n < 8 {
        n = 3;
    }
    if n == 3 || n == 8 {
        return Some((sign * f64::INFINITY, nsign + n));
    }
    None
}

struct ReadFloat {
    mantissa: u64,
    exp: i64,
    neg: bool,
    trunc: bool,
    hex: bool,
    n: usize,
}

/// Go 的 `readFloat`。
fn read_float(s: &str) -> Option<ReadFloat> {
    let b = s.as_bytes();
    let mut i = 0;
    let mut underscores = false;
    let mut neg = false;
    if i >= b.len() {
        return None;
    }
    if b[i] == b'+' {
        i += 1;
    } else if b[i] == b'-' {
        neg = true;
        i += 1;
    }
    let mut base: u64 = 10;
    let mut max_mant_digits = 19;
    let mut exp_char = b'e';
    let mut hex = false;
    if i + 2 < b.len() && b[i] == b'0' && lower(b[i + 1]) == b'x' {
        base = 16;
        max_mant_digits = 16;
        i += 2;
        exp_char = b'p';
        hex = true;
    }
    let (mut sawdot, mut sawdigits) = (false, false);
    let (mut nd, mut nd_mant, mut dp) = (0i64, 0i64, 0i64);
    let mut mantissa: u64 = 0;
    let mut trunc = false;
    while i < b.len() {
        let c = b[i];
        if c == b'_' {
            underscores = true;
        } else if c == b'.' {
            if sawdot {
                break;
            }
            sawdot = true;
            dp = nd;
        } else if c.is_ascii_digit() {
            sawdigits = true;
            if c == b'0' && nd == 0 {
                // 開頭的零不算
                dp -= 1;
                i += 1;
                continue;
            }
            nd += 1;
            if nd_mant < max_mant_digits {
                mantissa = mantissa.wrapping_mul(base).wrapping_add((c - b'0') as u64);
                nd_mant += 1;
            } else if c != b'0' {
                trunc = true;
            }
        } else if base == 16 && (b'a'..=b'f').contains(&lower(c)) {
            sawdigits = true;
            nd += 1;
            if nd_mant < max_mant_digits {
                mantissa = mantissa.wrapping_mul(16).wrapping_add((lower(c) - b'a' + 10) as u64);
                nd_mant += 1;
            } else {
                trunc = true;
            }
        } else {
            break;
        }
        i += 1;
    }
    if !sawdigits {
        return None;
    }
    if !sawdot {
        dp = nd;
    }
    if base == 16 {
        dp *= 4;
        nd_mant *= 4;
    }
    if i < b.len() && lower(b[i]) == exp_char {
        i += 1;
        if i >= b.len() {
            return None;
        }
        let mut esign = 1;
        if b[i] == b'+' {
            i += 1;
        } else if b[i] == b'-' {
            i += 1;
            esign = -1;
        }
        if i >= b.len() || !b[i].is_ascii_digit() {
            return None;
        }
        let mut e = 0i64;
        while i < b.len() && (b[i].is_ascii_digit() || b[i] == b'_') {
            if b[i] == b'_' {
                underscores = true;
            } else if e < 10000 {
                e = e * 10 + (b[i] - b'0') as i64;
            }
            i += 1;
        }
        dp += e * esign;
    } else if base == 16 {
        // 十六進位一定要有指數。
        return None;
    }
    let mut exp = 0;
    if mantissa != 0 {
        exp = dp - nd_mant;
    }
    if underscores && !underscore_ok(&b[..i]) {
        return None;
    }
    Some(ReadFloat { mantissa, exp, neg, trunc, hex, n: i })
}

/// Go 的 `underscoreOK`:底線只能夾在數字之間,或接在進位前綴之後。
fn underscore_ok(s: &[u8]) -> bool {
    let mut saw = b'^';
    let mut i = 0;
    let s = if !s.is_empty() && (s[0] == b'-' || s[0] == b'+') { &s[1..] } else { s };
    let mut hex = false;
    if s.len() >= 2 && s[0] == b'0' && matches!(lower(s[1]), b'b' | b'o' | b'x') {
        i = 2;
        saw = b'0';
        hex = lower(s[1]) == b'x';
    }
    while i < s.len() {
        let c = s[i];
        i += 1;
        if c.is_ascii_digit() || hex && (b'a'..=b'f').contains(&lower(c)) {
            saw = b'0';
            continue;
        }
        if c == b'_' {
            if saw != b'0' {
                return false;
            }
            saw = b'_';
            continue;
        }
        if saw == b'_' {
            return false;
        }
        saw = b'!';
    }
    saw != b'_'
}

/// Go 的 `atofHex`(float64)。超出範圍回 None(Go 的 ErrRange)。
fn atof_hex(mut mantissa: u64, mut exp: i64, neg: bool, trunc: bool) -> Option<f64> {
    const MANTBITS: u32 = 52;
    const EXPBITS: u32 = 11;
    const BIAS: i64 = -1023;
    let max_exp = (1i64 << EXPBITS) + BIAS - 2;
    let min_exp = BIAS + 1;
    exp += MANTBITS as i64;
    while mantissa != 0 && mantissa >> (MANTBITS + 2) == 0 {
        mantissa <<= 1;
        exp -= 1;
    }
    if trunc {
        mantissa |= 1;
    }
    while mantissa >> (1 + MANTBITS + 2) != 0 {
        mantissa = (mantissa >> 1) | (mantissa & 1);
        exp += 1;
    }
    while mantissa > 1 && exp < min_exp - 2 {
        mantissa = (mantissa >> 1) | (mantissa & 1);
        exp += 1;
    }
    let mut round = mantissa & 3;
    mantissa >>= 2;
    round |= mantissa & 1;
    exp += 2;
    if round == 3 {
        mantissa += 1;
        if mantissa == 1 << (1 + MANTBITS) {
            mantissa >>= 1;
            exp += 1;
        }
    }
    if mantissa >> MANTBITS == 0 {
        exp = BIAS;
    }
    if exp > max_exp {
        return None;
    }
    let mut bits = mantissa & ((1u64 << MANTBITS) - 1);
    bits |= (((exp - BIAS) as u64) & ((1u64 << EXPBITS) - 1)) << MANTBITS;
    if neg {
        bits |= 1u64 << MANTBITS << EXPBITS;
    }
    Some(f64::from_bits(bits))
}

/// Go 的 `strconv.FormatFloat(f, 'f', -1, 64)`:最短而能還原的十進位,不用指數。
///
/// Rust 的 `{}` 對有限值給的正是同一串數字(兩邊都是「最短且最接近」的那一個);
/// 不同的只有無限大與 NaN 的寫法。
pub fn format_float(f: f64) -> String {
    if f.is_nan() {
        return "NaN".to_string();
    }
    if f.is_infinite() {
        return if f > 0.0 { "+Inf".to_string() } else { "-Inf".to_string() };
    }
    format!("{f}")
}

const NS_PER_DAY: i64 = 86_400 * 1_000_000_000;

/// UTC 的一個時刻:1970-01-01 起的天數,加上當天的奈秒數。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Civil {
    days: i64,
    ns: i64,
}

impl Civil {
    /// 某一天的午夜。
    pub fn new(y: i64, m: i64, d: i64) -> Civil {
        Civil { days: days_from_civil(y, m, d), ns: 0 }
    }

    /// Go 的 `t.AddDate(0, 0, n)`:在 UTC 裡就是加 n 天。
    pub fn add_days(self, n: i64) -> Civil {
        Civil { days: self.days.wrapping_add(n), ns: self.ns }
    }

    /// Go 的 `t.Add(time.Duration(ns))`。
    pub fn add_nanos(self, ns: i64) -> Civil {
        let total = self.ns as i128 + ns as i128;
        let d = total.div_euclid(NS_PER_DAY as i128) as i64;
        let r = total.rem_euclid(NS_PER_DAY as i128) as i64;
        Civil { days: self.days.wrapping_add(d), ns: r }
    }

    /// Go 的 `t.Format("2006-01-02")`。
    pub fn format_date(&self) -> String {
        let (y, m, d) = civil_from_days(self.days);
        format!("{y:04}-{m:02}-{d:02}")
    }

    /// Go 的 `t.Format("15:04:05")`:小數秒直接捨去。
    pub fn format_time(&self) -> String {
        let s = self.ns / 1_000_000_000;
        format!("{:02}:{:02}:{:02}", s / 3600, s / 60 % 60, s % 60)
    }
}

/// 公曆日期換成 1970-01-01 起的天數(Howard Hinnant 的 days_from_civil)。
fn days_from_civil(y: i64, m: i64, d: i64) -> i64 {
    let y = if m <= 2 { y - 1 } else { y };
    let era = y.div_euclid(400);
    let yoe = y - era * 400;
    let mp = (m + 9) % 12;
    let doy = (153 * mp + 2) / 5 + d - 1;
    let doe = yoe * 365 + yoe / 4 - yoe / 100 + doy;
    era * 146097 + doe - 719468
}

fn civil_from_days(z: i64) -> (i64, i64, i64) {
    let z = z + 719468;
    let era = z.div_euclid(146097);
    let doe = z - era * 146097;
    let yoe = (doe - doe / 1460 + doe / 36524 - doe / 146096) / 365;
    let y = yoe + era * 400;
    let doy = doe - (365 * yoe + yoe / 4 - yoe / 100);
    let mp = (5 * doy + 2) / 153;
    let d = doy - (153 * mp + 2) / 5 + 1;
    let m = if mp < 10 { mp + 3 } else { mp - 9 };
    (if m <= 2 { y + 1 } else { y }, m, d)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn dates() {
        assert_eq!(Civil::new(1899, 12, 30).add_days(45000).format_date(), "2023-03-15");
        assert_eq!(Civil::new(1904, 1, 1).add_days(45000).format_date(), "2027-03-16");
        assert_eq!(Civil::new(1899, 12, 30).add_days(2958465).format_date(), "9999-12-31");
        assert_eq!(Civil::new(2000, 1, 1).add_nanos(NS_PER_DAY / 2).format_time(), "12:00:00");
    }

    #[test]
    fn floats() {
        assert_eq!(format_float(3.5), "3.5");
        assert_eq!(format_float(45000.0), "45000");
        assert_eq!(format_float(1e21), "1000000000000000000000");
        assert_eq!(format_float(-0.0), "-0");
        assert_eq!(format_float(f64::INFINITY), "+Inf");
        assert_eq!(parse_float("1e400"), None);
        assert_eq!(parse_float("+nan"), None);
        assert_eq!(parse_float("Inf"), Some(f64::INFINITY));
        assert_eq!(parse_float("-infinity"), Some(f64::NEG_INFINITY));
        assert_eq!(parse_float("infin"), None);
        assert_eq!(parse_float("1_000"), Some(1000.0));
        assert_eq!(parse_float("1__0"), None);
        assert_eq!(parse_float("_1"), None);
        assert_eq!(parse_float("1e1_0"), Some(1e10));
        assert_eq!(parse_float("0x1p3"), Some(8.0));
        assert_eq!(parse_float("0x_1.8p1"), Some(3.0));
        assert_eq!(parse_float("0x1"), None);
        assert_eq!(parse_float("-0x1p-1074"), Some(-5e-324));
        assert_eq!(parse_float("0x1p1024"), None);
    }
}
