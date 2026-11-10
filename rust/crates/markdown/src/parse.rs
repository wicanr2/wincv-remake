//! 區塊層的解析(對應 Go 版 `parse.go`)。
//!
//! `strings.TrimSpace` 對應 `str::trim`:兩者都以 Unicode 的 White_Space 屬性
//! 判斷空白(Go 的 `unicode.IsSpace` 在 Latin-1 以外就是查 White_Space),
//! 所以全形空白 U+3000 兩邊都會被修掉。

use crate::inline::inline;
use crate::{Block, Kind};

/// 把 markdown 拆成區塊。
pub fn parse(src: &str) -> Vec<Block> {
    let joined = src.replace("\r\n", "\n");
    let lines: Vec<&str> = joined.split('\n').collect();
    let mut out = Vec::new();
    let mut i = 0usize;
    while i < lines.len() {
        let ln = lines[i];
        let trimmed = ln.trim();

        // 圍籬程式碼區塊。內容一律原樣保留,那正是它的用途。
        if let Some(fence) = fence_of(trimmed) {
            let lang = trimmed.trim_start_matches(['`', '~']).trim().to_string();
            let mut body = Vec::new();
            i += 1;
            while i < lines.len() {
                if lines[i].trim().starts_with(fence) {
                    break;
                }
                body.push(lines[i].to_string());
                i += 1;
            }
            out.push(Block { kind: Kind::Code, lang, lines: body, ..Default::default() });
            i += 1;
            continue;
        }

        if trimmed.is_empty() {
            i += 1;
            continue;
        }

        if is_rule(trimmed) {
            out.push(Block { kind: Kind::Rule, ..Default::default() });
            i += 1;
            continue;
        }

        // ATX 標題
        let n = heading_level(trimmed);
        if n > 0 {
            out.push(Block { kind: Kind::Heading, level: n, spans: inline(atx_text(&trimmed[n as usize..])), ..Default::default() });
            i += 1;
            continue;
        }

        // 表格:這一行有 |,而且下一行是分隔列
        if ln.contains('|') && i + 1 < lines.len() && is_table_sep(lines[i + 1]) {
            let mut b = Block { kind: Kind::Table, aligns: table_aligns(lines[i + 1]), ..Default::default() };
            b.rows.push(table_cells(ln));
            i += 2;
            while i < lines.len() {
                if !lines[i].contains('|') || lines[i].trim().is_empty() {
                    break;
                }
                b.rows.push(table_cells(lines[i]));
                i += 1;
            }
            out.push(b);
            continue;
        }

        // 引言:連續的 > 行合成一段
        if trimmed.starts_with('>') {
            let mut parts = Vec::new();
            while i < lines.len() {
                let t = lines[i].trim();
                if !t.starts_with('>') {
                    break;
                }
                parts.push(t.strip_prefix('>').unwrap_or(t).trim());
                i += 1;
            }
            out.push(Block { kind: Kind::Quote, spans: inline(&parts.join(" ")), ..Default::default() });
            continue;
        }

        // 清單
        if let Some(it) = list_item(ln) {
            out.push(Block {
                kind: Kind::List,
                level: indent_of(ln) / 2,
                ordered: it.ordered,
                num: it.num,
                spans: inline(it.rest),
                ..Default::default()
            });
            i += 1;
            continue;
        }

        // setext 標題:下一行整行是 === 或 ---
        //
        // 這一段要放在水平線之後、段落之前:`---` 單獨出現(前面是空行)
        // 是水平線,接在一行文字後面才是二級標題。判斷的時機決定了語意:
        // 這裡是在處理**那行文字**時往下看,所以走到這裡就表示前面有文字。
        //
        // 也要放在引言與清單之後:底線只能接在段落文字後面。放在前面的話
        // 「- 項目」下一行接 `===` 會被當成一級標題,`1) 項目` 接 `---`
        // 會被當成二級標題,而那其實是清單後面接著一條水平線。
        if i + 1 < lines.len() {
            let nx = lines[i + 1].trim();
            if nx.len() >= 2 && (all_of(nx, b'=') || all_of(nx, b'-')) {
                let lvl = if nx.as_bytes()[0] == b'-' { 2 } else { 1 };
                out.push(Block { kind: Kind::Heading, level: lvl, spans: inline(trimmed), ..Default::default() });
                i += 2;
                continue;
            }
        }

        // 只有一張圖片的段落:獨立成一個 Image 區塊,才能整塊配版面。
        if let Some((src, alt)) = lone_image(trimmed) {
            out.push(Block { kind: Kind::Image, src, alt, ..Default::default() });
            i += 1;
            continue;
        }

        // 段落:吃到空行或下一個區塊為止
        let mut parts: Vec<&str> = Vec::new();
        while i < lines.len() {
            let t = lines[i].trim();
            if t.is_empty() || is_rule(t) || heading_level(t) > 0 || t.starts_with('>') || fence_of(t).is_some() {
                break;
            }
            if list_item(lines[i]).is_some() && !parts.is_empty() {
                break;
            }
            parts.push(t);
            i += 1;
        }
        out.push(Block { kind: Kind::Para, spans: inline(&parts.join(" ")), ..Default::default() });
    }
    out
}

fn fence_of(s: &str) -> Option<&'static str> {
    ["```", "~~~"].into_iter().find(|f| s.starts_with(f))
}

fn heading_level(s: &str) -> i32 {
    let b = s.as_bytes();
    let mut n = 0;
    while n < b.len() && b[n] == b'#' {
        n += 1;
    }
    if n == 0 || n > 6 {
        return 0;
    }
    // `#foo` 不是標題,`# foo` 才是
    if n < b.len() && b[n] != b' ' {
        return 0;
    }
    n as i32
}

fn is_rule(s: &str) -> bool {
    let s = s.replace(' ', "");
    if s.len() < 3 {
        return false;
    }
    all_of(&s, b'-') || all_of(&s, b'*') || all_of(&s, b'_')
}

fn all_of(s: &str, c: u8) -> bool {
    !s.is_empty() && s.bytes().all(|b| b == c)
}

fn indent_of(s: &str) -> i32 {
    let mut n = 0;
    for b in s.bytes() {
        match b {
            b'\t' => n += 4,
            b' ' => n += 1,
            _ => break,
        }
    }
    n
}

/// 清單項目的拆解結果。Go 版另外回傳記號本身,但沒有人用,這裡省略。
struct ListItem<'a> {
    rest: &'a str,
    ordered: bool,
    num: i64,
}

/// 判斷是不是清單項目。
fn list_item(ln: &str) -> Option<ListItem<'_>> {
    let s = ln.trim_start_matches([' ', '\t']);
    let b = s.as_bytes();
    if b.len() >= 2 && matches!(b[0], b'-' | b'*' | b'+') && b[1] == b' ' {
        return Some(ListItem { rest: s[2..].trim(), ordered: false, num: 0 });
    }
    // 有序:數字 + . 或 )。數字最多 9 位(CommonMark 的上限):再長的序號沒有人會寫,
    // 而且會讓序號溢位,畫出一個與原文無關的數字。
    let mut n = 0;
    while n < b.len() && b[n].is_ascii_digit() {
        n += 1;
    }
    if n > 0 && n <= 9 && n + 1 < b.len() && (b[n] == b'.' || b[n] == b')') && b[n + 1] == b' ' {
        let num = b[..n].iter().fold(0i64, |a, &c| a * 10 + i64::from(c - b'0'));
        return Some(ListItem { rest: s[n + 2..].trim(), ordered: true, num });
    }
    None
}

/// 判斷整行是不是只有一張圖片,回傳 (src, alt)。
fn lone_image(s: &str) -> Option<(String, String)> {
    if !s.starts_with("![") {
        return None;
    }
    let mut sp = inline(s);
    if sp.len() != 1 || !sp[0].style.has(crate::Style::LINK) || sp[0].href.is_empty() {
        return None;
    }
    // inline 把圖片存成 Link 樣式 + Href,而 Text 是 alt。
    // 這裡要的是「整行只有這一張圖」,所以長度必須剛好 1。
    let s0 = sp.remove(0);
    Some((s0.href, s0.text))
}

fn is_table_sep(ln: &str) -> bool {
    let s = ln.trim();
    if !s.contains('-') || !s.contains('|') {
        return false;
    }
    s.chars().all(|c| matches!(c, '-' | '|' | ':' | ' '))
}

fn table_cells(ln: &str) -> Vec<String> {
    let s = ln.trim();
    let s = s.strip_prefix('|').unwrap_or(s);
    let s = s.strip_suffix('|').unwrap_or(s);
    // tab 在格子裡展開成空白:畫面上一個 tab 只佔一格,寬度會算錯。
    s.split('|').map(|c| crate::wrap::expand_tabs(c.trim())).collect()
}

/// ATX 標題的內文,去掉結尾的 # 串。
///
/// 照 CommonMark:結尾的 # 串前面要是空白(或整段都是 #)才算收尾記號。一律把結尾的 #
/// 剪掉的話,「# 標題 C#」會變成「標題 C」。
fn atx_text(s: &str) -> &str {
    let s = s.trim();
    let t = s.trim_end_matches('#');
    if t.is_empty() {
        return "";
    }
    if t.len() < s.len() && (t.ends_with(' ') || t.ends_with('\t')) {
        return t.trim();
    }
    s
}

fn table_aligns(ln: &str) -> Vec<u8> {
    table_cells(ln)
        .iter()
        .map(|c| {
            if c.starts_with(':') && c.ends_with(':') {
                b'c'
            } else if c.ends_with(':') {
                b'r'
            } else {
                b'l'
            }
        })
        .collect()
}
