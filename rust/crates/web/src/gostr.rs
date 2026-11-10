//! 在 Rust 的 `String` 裡帶著 Go 字串的「壞位元組」。
//!
//! Go 的字串是位元組序列:頁面宣告 UTF-8 或 Latin-1 但內容其實是別的編碼時,
//! Go 版把原始位元組一路帶到底,遇到「以字元走訪」的地方(摺疊空白)才變成
//! U+FFFD,遇到網址逃脫時則照原始位元組寫成 `%XX`。`String` 裝不下不合法的
//! UTF-8,所以 tokenizer 之前先把每個壞位元組換成一個私用區字元
//! (U+10FE80 起,依位元組值排),網址那一層再換回原始位元組,
//! 輸出前一律換成 U+FFFD。這樣兩邊在每一步看到的東西都相同。
//!
//! 代價是輸入裡本來就有 U+10FE80–U+10FEFF 的話會被當成壞位元組。
//! 那是第 16 平面私用區的最後一段,實際的網頁不會用到。

const BASE: u32 = 0x10FE00;

/// 這個字元代表的原始位元組。
pub(crate) fn raw_byte(c: char) -> Option<u8> {
    let v = c as u32;
    if (BASE + 0x80..=BASE + 0xFF).contains(&v) {
        Some((v - BASE) as u8)
    } else {
        None
    }
}

/// 位元組轉成帶壞位元組記號的字串。合法的 UTF-8 原樣,壞的位元組一個換一個記號
/// (與 Go 的 range 一樣,一次只吃一個位元組)。
pub(crate) fn from_bytes(b: &[u8]) -> String {
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
                out.push(char::from_u32(BASE + bad[0] as u32).unwrap_or('\u{FFFD}'));
                rest = &bad[1..];
            }
        }
    }
}

/// 還原成 Go 那一份位元組。
pub(crate) fn to_bytes(s: &str) -> Vec<u8> {
    let mut out = Vec::with_capacity(s.len());
    let mut tmp = [0u8; 4];
    for c in s.chars() {
        match raw_byte(c) {
            Some(b) => out.push(b),
            None => out.extend_from_slice(c.encode_utf8(&mut tmp).as_bytes()),
        }
    }
    out
}

/// 輸出用:壞位元組的記號換成 U+FFFD(Go 的排版引擎以字元走訪時看到的就是它)。
pub(crate) fn finish(s: &str) -> String {
    if !s.chars().any(|c| raw_byte(c).is_some()) {
        return s.to_string();
    }
    s.chars()
        .map(|c| if raw_byte(c).is_some() { '\u{FFFD}' } else { c })
        .collect()
}
