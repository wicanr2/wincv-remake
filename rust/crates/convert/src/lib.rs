//! 原版 Ctrl-O 那一組轉換功能(對應 Go 版 `internal/convert`)。
//!
//! 對照 file_id.diz 列出的項目:日文碼、繁體碼、簡體碼文字檔互轉;UNIX 與 PC
//! 換行轉換;批次更改檔名(大小寫、轉碼);去除 HTML 與 ANSI 彩色控制碼。

use std::sync::LazyLock;

use regex::Regex;
use wincv_textenc::{decode, encode, Enc};

/// 換行樣式。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum Eol {
    Unix,
    Pc,
    Mac,
}

fn replace(b: &[u8], from: &[u8], to: &[u8]) -> Vec<u8> {
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i..].starts_with(from) {
            out.extend_from_slice(to);
            i += from.len();
        } else {
            out.push(b[i]);
            i += 1;
        }
    }
    out
}

fn count(b: &[u8], pat: &[u8]) -> usize {
    b.windows(pat.len()).filter(|w| *w == pat).count()
}

/// 換掉換行樣式。先全部正規化成 `\n` 再換,混用兩種換行的檔案也會被統一:
/// 原版就是這樣才修得好壞掉的檔。
pub fn to_eol(b: &[u8], e: Eol) -> Vec<u8> {
    let s = replace(&replace(b, b"\r\n", b"\n"), b"\r", b"\n");
    match e {
        Eol::Pc => replace(&s, b"\n", b"\r\n"),
        Eol::Mac => replace(&s, b"\n", b"\r"),
        Eol::Unix => s,
    }
}

/// 目前的換行樣式。混用時回傳出現最多的那一種。
pub fn detect_eol(b: &[u8]) -> Eol {
    let crlf = count(b, b"\r\n");
    let lf = count(b, b"\n") - crlf;
    let cr = count(b, b"\r") - crlf;
    if crlf >= lf && crlf >= cr {
        Eol::Pc
    } else if cr > lf {
        Eol::Mac
    } else {
        Eol::Unix
    }
}

/// 把位元組從一種編碼轉成另一種。中間一律經過 UTF-8:直接做 Big5→GBK 的位元組
/// 對映表要維護一張大表,而且遇到某一邊沒有的字時沒有統一的處理方式。
///
/// 目標編碼放不下的字回錯誤,訊息指出是哪一個字。
pub fn recode(b: &[u8], from: Enc, to: Enc) -> Result<Vec<u8>, String> {
    if from == to {
        return Ok(b.to_vec());
    }
    let s = decode(b, from);
    encode(&s, to).ok_or_else(|| {
        let bad = s.chars().find(|c| encode(c.encode_utf8(&mut [0; 4]), to).is_none()).unwrap_or('?');
        if to == Enc::Big5 {
            wincv_i18n::tr!("%q 無法以 Big5 表示", bad.to_string())
        } else {
            format!("{bad:?} 無法以 {} 表示", to.name())
        }
    })
}

/// 跟 recode 一樣,但目標編碼放不下的字用 `repl` 代替,不會整份失敗。轉繁簡時
/// 一定會遇到對不過去的字,停在第一個轉不了的字對使用者沒有幫助。
pub fn recode_lossy(b: &[u8], from: Enc, to: Enc, repl: &str) -> Vec<u8> {
    if from == to {
        return b.to_vec();
    }
    let s = decode(b, from);
    if let Some(all) = encode(&s, to) {
        return all;
    }
    let mut out = Vec::with_capacity(s.len());
    for c in s.chars() {
        match encode(c.encode_utf8(&mut [0; 4]), to) {
            Some(e) => out.extend(e),
            None => out.extend_from_slice(repl.as_bytes()),
        }
    }
    out
}

macro_rules! re {
    ($name:ident, $pat:expr) => {
        static $name: LazyLock<Regex> = LazyLock::new(|| Regex::new($pat).unwrap());
    };
}
re!(ANSI, r"\x1b\[[0-9;?]*[a-zA-Z]");
re!(HTML_TAG, r"(?s)<[^>]*>");
re!(SCRIPT, r"(?is)<script\b[^>]*>.*?</\s*script\s*>");
re!(STYLE, r"(?is)<style\b[^>]*>.*?</\s*style\s*>");
re!(BR, r"(?i)<br\s*/?>");
re!(P_END, r"(?i)</p\s*>");
re!(SPACE, r"[ \t]+");
re!(BLANK, r"\n{3,}");

/// 去掉 ANSI 控制碼。
pub fn strip_ansi(s: &str) -> String {
    ANSI.replace_all(s, "").into_owned()
}

/// 把 HTML 變成純文字。
///
/// 順序有意義:先整段拿掉 script/style(它們的**內容**也不要),再拆標籤,最後
/// 還原實體。反過來做會把 script 裡的程式碼留在正文。
pub fn strip_html(s: &str) -> String {
    let s = SCRIPT.replace_all(s, "");
    let s = STYLE.replace_all(&s, "");
    let s = BR.replace_all(&s, "\n");
    let s = P_END.replace_all(&s, "\n\n");
    let s = HTML_TAG.replace_all(&s, "");
    let s = unescape_entities(&s);
    let s = SPACE.replace_all(&s, " ");
    let s = BLANK.replace_all(&s, "\n\n");
    s.trim().to_string()
}

fn unescape_entities(s: &str) -> String {
    // &amp; 要最後換:先換的話 "&amp;lt;" 會變成 "<",但它原本表示的是字面上的 "&lt;"。
    let mut s = s.to_string();
    for (k, v) in [("&lt;", "<"), ("&gt;", ">"), ("&quot;", "\""), ("&apos;", "'"), ("&nbsp;", " "), ("&#39;", "'"), ("&#34;", "\"")] {
        s = s.replace(k, v);
    }
    s.replace("&amp;", "&")
}

/// 批次改名的大小寫模式。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum NameCase {
    Lower,
    Upper,
    /// 首字母大寫,副檔名小寫。
    Title,
}

/// Go 的 unicode.ToLower / ToUpper:一個字對一個字,不會變長(ß 不會變成 SS)。
fn map1(s: &str, upper: bool) -> String {
    s.chars()
        .map(|c| if upper { c.to_uppercase().next() } else { c.to_lowercase().next() }.unwrap_or(c))
        .collect()
}

/// 依模式改一個檔名的大小寫。副檔名與主檔名分開處理。
///
/// 首字母大寫以**字元**為單位:Go 版取第一個位元組,首字是中文或 é 時會把檔名
/// 弄壞。
pub fn rename_case(name: &str, c: NameCase) -> String {
    let (base, ext) = match name.rfind('.') {
        Some(i) if i > 0 => name.split_at(i),
        _ => (name, ""),
    };
    match c {
        NameCase::Upper => map1(base, true) + &map1(ext, true),
        NameCase::Lower => map1(base, false) + &map1(ext, false),
        NameCase::Title => {
            let low = map1(base, false);
            let mut it = low.chars();
            let head = it.next().map(|f| map1(&f.to_string(), true)).unwrap_or_default();
            head + it.as_str() + &map1(ext, false)
        }
    }
}

/// 連續編號的新檔名(原版的「連續編號改名」)。`pattern` 裡的 `#` 換成編號,
/// `#` 的個數就是補零的位數。
pub fn numbered_rename(pattern: &str, n: u64) -> String {
    let Some(i) = pattern.find('#') else { return pattern.to_string() };
    let j = pattern[i..].find(|c| c != '#').map_or(pattern.len(), |k| i + k);
    format!("{}{:0w$}{}", &pattern[..i], n, &pattern[j..], w = j - i)
}

#[cfg(test)]
mod tests;
