//! 行內樣式的掃描器(對應 Go 版 `inline.go`)。
//!
//! Go 版逐位元組走字串:跳脫 `\` 只複製下一個位元組,之後的續位元組再一個個
//! 照抄。所以這裡也在 `&[u8]` 上走,片段文字收成位元組再轉回 `String`。
//! 所有切點都落在 ASCII 記號上,輸入是合法 UTF-8 時輸出也一定是。

use crate::{Span, Style};

/// 把一行文字拆成帶樣式的片段。
///
/// 圖片與連結都存成 Link 樣式 + Href;圖片另外把 Text 設成 alt。
/// 兩者在格點畫面上的差別只有顏色,不值得為此多一個樣式位元。
///
/// 這是手寫的掃描器而不是正則:markdown 的強調是「配對」問題
/// (`**a* b**` 這種交錯要能收斂),正則寫得出來但看不懂。
pub(crate) fn inline(s: &str) -> Vec<Span> {
    inline_bytes(s.as_bytes())
}

fn inline_bytes(s: &[u8]) -> Vec<Span> {
    let mut out = Vec::new();
    let mut buf: Vec<u8> = Vec::new();
    let mut style = Style::default();

    fn flush(out: &mut Vec<Span>, buf: &mut Vec<u8>, style: Style) {
        if !buf.is_empty() {
            out.push(Span { text: to_string(std::mem::take(buf)), style, href: String::new() });
        }
    }

    let mut i = 0usize;
    while i < s.len() {
        // 跳脫:\* 就是一個星號
        if s[i] == b'\\' && i + 1 < s.len() {
            buf.push(s[i + 1]);
            i += 2;
            continue;
        }
        // 行內程式碼:內容原樣保留,不再解析
        if s[i] == b'`' {
            let mut n = 1;
            while i + n < s.len() && s[i + n] == b'`' {
                n += 1;
            }
            let mark = &s[i..i + n];
            if let Some(end) = find(&s[i + n..], mark) {
                flush(&mut out, &mut buf, style);
                out.push(Span {
                    text: to_string(s[i + n..i + n + end].to_vec()),
                    style: style | Style::MONO,
                    href: String::new(),
                });
                i += n + end + n;
                continue;
            }
        }
        // 圖片 ![alt](src) 與連結 [text](href)
        if s[i] == b'!'
            && i + 1 < s.len()
            && s[i + 1] == b'['
            && let Some((text, href, n)) = link_at(&s[i + 1..])
        {
            flush(&mut out, &mut buf, style);
            out.push(Span { text, style: style | Style::LINK, href });
            i += 1 + n;
            continue;
        }
        if s[i] == b'['
            && let Some((text, href, n)) = link_at(&s[i..])
        {
            flush(&mut out, &mut buf, style);
            out.push(Span { text, style: style | Style::LINK, href });
            i += n;
            continue;
        }
        // 強調
        if s[i] == b'*' || s[i] == b'_' {
            let c = s[i];
            let mut n = 1;
            while i + n < s.len() && s[i + n] == c {
                n += 1;
            }
            // 只認 * 與 **,更多的當文字
            if n <= 2 {
                let want = if n == 2 { Style::BOLD } else { Style::ITALIC };
                // 有沒有配對的收尾?沒有就當普通字元,不然
                // 一個落單的星號會讓後面整段都變成斜體。
                if style.has(want) || find(&s[i + n..], &vec![c; n]).is_some() {
                    flush(&mut out, &mut buf, style);
                    style ^= want;
                    i += n;
                    continue;
                }
            }
            buf.extend_from_slice(&s[i..i + n]);
            i += n;
            continue;
        }
        if s[i..].starts_with(b"~~") && (style.has(Style::STRIKE) || find(&s[i + 2..], b"~~").is_some()) {
            flush(&mut out, &mut buf, style);
            style ^= Style::STRIKE;
            i += 2;
            continue;
        }
        buf.push(s[i]);
        i += 1;
    }
    flush(&mut out, &mut buf, style);
    out
}

/// 解析 s 開頭的 [text](href),回傳 (text, href, 吃掉幾個位元組)。
///
/// 中括號要配對算:[![img](a)](b) 這種巢狀在 README 裡很常見,
/// 只找第一個 ] 會切在錯的地方。
fn link_at(s: &[u8]) -> Option<(String, String, usize)> {
    if s.is_empty() || s[0] != b'[' {
        return None;
    }
    let mut depth = 0i32;
    let mut i = 0usize;
    while i < s.len() {
        if s[i] == b'\\' {
            i += 2;
            continue;
        }
        if s[i] == b'[' {
            depth += 1;
        } else if s[i] == b']' {
            depth -= 1;
            if depth == 0 {
                break;
            }
        }
        i += 1;
    }
    if i >= s.len() || depth != 0 {
        return None;
    }
    let text_b = &s[1..i];
    let j = i + 1;
    if j >= s.len() || s[j] != b'(' {
        return None;
    }
    depth = 0;
    let mut k = j;
    while k < s.len() {
        if s[k] == b'(' {
            depth += 1;
        } else if s[k] == b')' {
            depth -= 1;
            if depth == 0 {
                break;
            }
        }
        k += 1;
    }
    if k >= s.len() {
        return None;
    }
    let mut href = trim_space(&s[j + 1..k]);
    // 連結標題:[x](url "標題"),標題丟掉,格點畫面沒地方放
    if let Some(sp) = href.iter().position(|&b| b == b' ' || b == b'\t') {
        href = &href[..sp];
    }
    // 巢狀圖片連結:[![alt](img)](url) 的 text 還是 markdown,
    // 取裡層的 alt 當文字。
    let mut text = to_string(text_b.to_vec());
    if text_b.starts_with(b"![") {
        let mut inner = inline_bytes(text_b);
        if inner.len() == 1 {
            text = inner.remove(0).text;
        }
    }
    Some((text, to_string(href.to_vec()), k + 1))
}

/// `strings.TrimSpace` 的位元組版。切點落在 ASCII 括號之間,
/// 轉回 `&str` 修完再換回位元組位置。
fn trim_space(b: &[u8]) -> &[u8] {
    match std::str::from_utf8(b) {
        Ok(s) => {
            let t = s.trim();
            let start = t.as_ptr() as usize - s.as_ptr() as usize;
            &b[start..start + t.len()]
        }
        // 不合法的 UTF-8 只修 ASCII 空白(與 Go 對不合法位元組的處理一致:
        // 它們解成 U+FFFD,不是空白)。
        Err(_) => b.trim_ascii(),
    }
}

fn find(h: &[u8], n: &[u8]) -> Option<usize> {
    if n.is_empty() {
        return Some(0);
    }
    if n.len() > h.len() {
        return None;
    }
    h.windows(n.len()).position(|w| w == n)
}

/// 收集到的位元組轉回字串。輸入是合法 UTF-8 時一定成功;
/// 不合法時退回 lossy,至少不 panic。
fn to_string(b: Vec<u8>) -> String {
    String::from_utf8(b).unwrap_or_else(|e| String::from_utf8_lossy(e.as_bytes()).into_owned())
}
