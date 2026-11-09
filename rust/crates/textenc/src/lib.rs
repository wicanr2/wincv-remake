//! 文字編碼的判讀與解碼(對應 Go 版 `internal/textenc`)。
//!
//! 判讀規則與門檻和 Go 版逐條相同;解碼器改用 encoding_rs(WHATWG),
//! 與 Go 版 x/text 的差異由 tools/textenc-parity.sh 對照。

use encoding_rs::{Encoding, BIG5, EUC_KR, GBK, SHIFT_JIS, UTF_16BE, UTF_16LE};

/// 編碼。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Enc {
    Unknown,
    Ascii,
    Utf8,
    Utf16Le,
    Utf16Be,
    Big5,
    Gbk,
    ShiftJis,
    EucKr,
    /// 不是文字。
    Binary,
}

impl Enc {
    /// 顯示用的名字。「未知」「二進位」是繁中原文,顯示時再過 i18n。
    pub fn name(self) -> &'static str {
        match self {
            Enc::Unknown => "未知",
            Enc::Ascii => "ASCII",
            Enc::Utf8 => "UTF-8",
            Enc::Utf16Le => "UTF-16LE",
            Enc::Utf16Be => "UTF-16BE",
            Enc::Big5 => "Big5",
            Enc::Gbk => "GBK",
            Enc::ShiftJis => "Shift_JIS",
            Enc::EucKr => "EUC-KR",
            Enc::Binary => "二進位",
        }
    }

    fn encoding(self) -> Option<&'static Encoding> {
        match self {
            Enc::Big5 => Some(BIG5),
            Enc::Gbk => Some(GBK),
            Enc::ShiftJis => Some(SHIFT_JIS),
            Enc::EucKr => Some(EUC_KR),
            Enc::Utf16Le => Some(UTF_16LE),
            Enc::Utf16Be => Some(UTF_16BE),
            _ => None,
        }
    }
}

/// 照 Go 的規則把位元組當 UTF-8:不合法的位元組一個換一個 U+FFFD。
///
/// Go 的字串可以裝不合法的 UTF-8,range 時每個壞位元組各算一個
/// RuneError。`String::from_utf8_lossy` 會把一整段壞位元組併成一個,
/// 畫面上的格數就對不上,所以自己做。
pub fn utf8_lossy_go(b: &[u8]) -> String {
    let mut out = String::with_capacity(b.len());
    let mut rest = b;
    loop {
        match std::str::from_utf8(rest) {
            Ok(s) => {
                out.push_str(s);
                return out;
            }
            Err(e) => {
                let (good, bad) = rest.split_at(e.valid_up_to());
                out.push_str(std::str::from_utf8(good).unwrap_or_default());
                out.push('\u{FFFD}');
                rest = &bad[1..];
            }
        }
    }
}

/// 用指定的編碼解碼。BOM 去掉;壞掉的位元組換成 U+FFFD,不會中途停下。
pub fn decode(b: &[u8], e: Enc) -> String {
    let b = match e {
        Enc::Utf16Le if b.starts_with(&[0xFF, 0xFE]) => &b[2..],
        Enc::Utf16Be if b.starts_with(&[0xFE, 0xFF]) => &b[2..],
        Enc::Utf8 if b.starts_with(&[0xEF, 0xBB, 0xBF]) => &b[3..],
        _ => b,
    };
    match e.encoding() {
        Some(enc) => enc.decode_without_bom_handling(b).0.into_owned(),
        None => utf8_lossy_go(b),
    }
}

/// UTF-8 的「字元起點」:不是 10xxxxxx 的位元組。
fn rune_start(c: u8) -> bool {
    c & 0xC0 != 0x80
}

/// 猜編碼。只看前 64 KiB,切點不落在 UTF-8 字元中間。
pub fn detect(b: &[u8]) -> Enc {
    if b.is_empty() {
        return Enc::Ascii;
    }
    if b.starts_with(&[0xEF, 0xBB, 0xBF]) {
        return Enc::Utf8;
    }
    if b.starts_with(&[0xFF, 0xFE]) {
        return Enc::Utf16Le;
    }
    if b.starts_with(&[0xFE, 0xFF]) {
        return Enc::Utf16Be;
    }
    const LIMIT: usize = 64 * 1024;
    let mut sample = b;
    if sample.len() > LIMIT {
        let mut end = LIMIT;
        while end < b.len() && end < LIMIT + 3 && !rune_start(b[end]) {
            end += 1;
        }
        sample = &b[..end];
    }
    let u16 = detect_utf16_no_bom(sample);
    if u16 != Enc::Unknown {
        return u16;
    }
    if is_binary(sample) {
        return Enc::Binary;
    }
    if sample.is_ascii() {
        return Enc::Ascii;
    }
    if std::str::from_utf8(sample).is_ok() {
        return Enc::Utf8; // 不是純 ASCII,所以一定有多位元組字元
    }
    let mut best = (Enc::Unknown, -1.0f64);
    for e in [Enc::Big5, Enc::Gbk, Enc::ShiftJis, Enc::EucKr] {
        let s = score(sample, e);
        if s > best.1 {
            best = (e, s);
        }
    }
    if best.1 <= 0.0 { Enc::Binary } else { best.0 }
}

/// NUL 超過 2%,或控制字元(不含 Tab、換行、FF、VT、ESC)超過 5%。
/// ESC 不算:BBS 簽名檔的 ANSI 色碼就是 ESC 開頭。
fn is_binary(b: &[u8]) -> bool {
    let (mut ctrl, mut nul) = (0usize, 0usize);
    for &c in b {
        if c == 0 {
            nul += 1;
        }
        if c < 0x20 && !matches!(c, b'\t' | b'\n' | b'\r' | 0x0C | 0x0B | 0x1B) {
            ctrl += 1;
        }
    }
    if nul * 100 > b.len() * 2 {
        return true;
    }
    ctrl * 100 > b.len() * 5
}

/// 沒有 BOM 的 UTF-16:靠奇偶位置上 0x00 的比例判斷。
fn detect_utf16_no_bom(b: &[u8]) -> Enc {
    if b.len() < 16 {
        return Enc::Unknown;
    }
    let n = b.len() & !1;
    let (mut even, mut odd) = (0usize, 0usize);
    for i in (0..n).step_by(2) {
        even += (b[i] == 0) as usize;
        odd += (b[i + 1] == 0) as usize;
    }
    let half = n / 2;
    if odd * 100 > half * 60 && even * 100 < half * 10 {
        return Enc::Utf16Le;
    }
    if even * 100 > half * 60 && odd * 100 < half * 10 {
        return Enc::Utf16Be;
    }
    Enc::Unknown
}

fn score(b: &[u8], e: Enc) -> f64 {
    let (mut ok, mut bad, mut dbl) = (0usize, 0usize, 0usize);
    let mut i = 0;
    while i < b.len() {
        let c = b[i];
        if c < 0x80 {
            i += 1;
            continue;
        }
        if i + 1 >= b.len() {
            bad += 1;
            break;
        }
        if valid_double(c, b[i + 1], e) {
            ok += 1;
            dbl += 1;
            i += 2;
        } else {
            bad += 1;
            i += 1;
        }
    }
    if dbl == 0 {
        return 0.0;
    }
    ok as f64 / (ok + bad) as f64 + common_bonus(b, e)
}

/// 這兩個位元組在該編碼裡是不是一個合法的雙位元組字。
pub fn valid_double(hi: u8, lo: u8, e: Enc) -> bool {
    match e {
        Enc::Big5 => (0xA1..=0xF9).contains(&hi) && ((0x40..=0x7E).contains(&lo) || (0xA1..=0xFE).contains(&lo)),
        Enc::Gbk => (0x81..=0xFE).contains(&hi) && ((0x40..=0x7E).contains(&lo) || (0x80..=0xFE).contains(&lo)),
        Enc::ShiftJis => {
            ((0x81..=0x9F).contains(&hi) || (0xE0..=0xEF).contains(&hi))
                && ((0x40..=0x7E).contains(&lo) || (0x80..=0xFC).contains(&lo))
        }
        Enc::EucKr => (0xA1..=0xFE).contains(&hi) && (0xA1..=0xFE).contains(&lo),
        _ => false,
    }
}

/// 常用字的加分:合法性分數相同時(Big5 與 GBK 的範圍大量重疊),
/// 靠「的、一、中」這類高頻字分出勝負。最多加 0.5。
fn common_bonus(b: &[u8], e: Enc) -> f64 {
    let pats: &[[u8; 2]] = match e {
        Enc::Big5 => &[[0xAA, 0xBA], [0xA4, 0x40], [0xA4, 0xA4], [0xA4, 0x48], [0xAC, 0xB0], [0xA1, 0x41], [0xA1, 0x43]],
        Enc::Gbk => &[[0xB5, 0xC4], [0xD2, 0xBB], [0xD6, 0xD0], [0xC8, 0xCB], [0xA3, 0xAC], [0xA1, 0xA3]],
        Enc::ShiftJis => &[[0x82, 0xCC], [0x82, 0xC8], [0x83, 0x8A], [0x81, 0x41], [0x81, 0x42]],
        Enc::EucKr => &[[0xC0, 0xBB], [0xC0, 0xCC], [0xB4, 0xC2]],
        _ => &[],
    };
    let hits: usize = pats.iter().map(|p| count(b, p)).sum();
    if hits == 0 {
        return 0.0;
    }
    (hits as f64 / (b.len() / 64 + 1) as f64 * 0.1).min(0.5)
}

/// 不重疊的出現次數(與 Go 的 bytes.Count 相同)。
fn count(b: &[u8], p: &[u8; 2]) -> usize {
    let (mut n, mut i) = (0, 0);
    while i + 1 < b.len() {
        if b[i] == p[0] && b[i + 1] == p[1] {
            n += 1;
            i += 2;
        } else {
            i += 1;
        }
    }
    n
}

#[cfg(test)]
mod tests {
    use super::*;

    fn enc(e: Enc, s: &str) -> Vec<u8> {
        let (b, _, bad) = e.encoding().unwrap().encode(s);
        assert!(!bad, "{s} 編不成 {e:?}");
        b.into_owned()
    }

    const ZH_TW: &str = "這是一段繁體中文的測試文字,用來確認編碼判讀是否正確。中文的常用字很多,例如的、一、中、人、為。";

    #[test]
    fn detect_round_trip() {
        for e in [Enc::Big5, Enc::Gbk] {
            let b = enc(e, ZH_TW);
            assert_eq!(detect(&b), e);
            assert_eq!(decode(&b, e), ZH_TW);
        }
        let ja = "これは日本語のテキストです。リストの中の文字列を確認します。";
        let b = enc(Enc::ShiftJis, ja);
        assert_eq!(detect(&b), Enc::ShiftJis);
        assert_eq!(decode(&b, Enc::ShiftJis), ja);
    }

    #[test]
    fn utf8_and_ascii() {
        assert_eq!(detect(ZH_TW.as_bytes()), Enc::Utf8);
        assert_eq!(detect(b"\xEF\xBB\xBFhello"), Enc::Utf8);
        assert_eq!(detect(b"plain ascii text\nsecond line\n"), Enc::Ascii);
        assert_eq!(detect(b""), Enc::Ascii);
    }

    #[test]
    fn utf8_across_sample_boundary() {
        let mut b = vec![b'a'; 64 * 1024 - 1];
        b.extend_from_slice("中文說明\n".as_bytes());
        assert_eq!(detect(&b), Enc::Utf8);
        assert_eq!(decode(&b, detect(&b)).as_bytes(), &b[..]);
    }

    #[test]
    fn utf16() {
        let le = [0xFF, 0xFE, b'h', 0, b'i', 0];
        assert_eq!(detect(&le), Enc::Utf16Le);
        assert_eq!(decode(&le, Enc::Utf16Le), "hi");
        let no_bom: Vec<u8> = "hello world, this is utf16 without bom".bytes().flat_map(|c| [c, 0]).collect();
        assert_eq!(detect(&no_bom), Enc::Utf16Le);
    }

    #[test]
    fn ansi_escape_is_text() {
        let b = enc(Enc::Big5, "\x1b[1;31m紅色\x1b[0m 一般\n\x1b[44m藍底\x1b[0m\n");
        assert_eq!(detect(&b), Enc::Big5);
    }

    #[test]
    fn binary() {
        let b: Vec<u8> = (0..512).map(|i| (i % 7) as u8).collect();
        assert_eq!(detect(&b), Enc::Binary);
    }

    #[test]
    fn lossy_keeps_good_parts() {
        let mut b = enc(Enc::Big5, "前面正常");
        b.extend_from_slice(&[0xFF, 0xFF]);
        b.extend_from_slice(&enc(Enc::Big5, "後面也正常"));
        let s = decode(&b, Enc::Big5);
        assert!(s.contains("前面正常") && s.contains("後面也正常") && s.contains('\u{FFFD}'), "{s}");
    }

    #[test]
    fn go_style_utf8_replacement_is_per_byte() {
        assert_eq!(utf8_lossy_go(b"a\xFF\xFEb"), "a\u{FFFD}\u{FFFD}b");
    }

    #[test]
    fn real_original_files() {
        for name in ["WinCV.txt", "whatsnew.txt", "file_id.diz"] {
            let Ok(b) = std::fs::read(format!("../../../original/app/{name}")) else {
                eprintln!("略過:沒有 original/app/{name}");
                return;
            };
            assert_eq!(detect(&b), Enc::Big5, "{name}");
            assert!(decode(&b, Enc::Big5).contains("WinCV"), "{name}");
        }
    }
}
