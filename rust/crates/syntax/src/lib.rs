//! 讀 WinCV 的 keyword_*.cfg 語法上色設定(對應 Go 版 `internal/syntax`)。
//!
//! 檔案結構:
//!
//! ```text
//! <設定鍵><空白><值>      ← 檔頭,一行一個
//! =========[任意說明]      ← 分隔線
//! <關鍵字><空白><顏色名>   ← 之後每行一個
//! ```
//!
//! 顏色名是 WinCV 自己的具名顏色(見 `wincv-cell`)。`keyword.cfg` 是總表:
//! `<副檔名><空白><設定檔名>`,值為 `.` 表示沿用上一個。

use std::collections::HashMap;
use std::path::Path;
use std::sync::Arc;

use wincv_cell::{by_name, Color};
use wincv_textenc::{decode, Enc};

/// 一種語言的上色設定。
#[derive(Clone, Debug, Default)]
pub struct Config {
    pub name: String,
    pub quote_color: Option<Color>,
    pub number_color: Option<Color>,
    pub line_comment: String,
    /// 行註解只有在行首才算。
    pub only_begin_line: bool,
    pub keyword_delimiter: String,
    /// 跨行註解的起訖標記。
    ///
    /// **設定檔裡沒有這一項**,但 image 的符號表有 COMMENTSTATE、END-COMMENT$、
    /// "CHECK-END-COMMENT,表示原版有跨行註解的狀態機。推測它依語言寫死;這裡在
    /// line_comment 為 `//` 時預設 C 家族的 `/* */`。**還沒查證**(CLAUDE.md §9 的 A14)。
    pub block_start: String,
    pub block_end: String,
    /// 關鍵字 → 顏色。
    pub keywords: HashMap<String, Color>,
    /// 單一符號 → 顏色(keyword_txt.cfg 那種全是符號的設定)。
    pub symbols: HashMap<char, Color>,
}

/// 沒指定 KeywordDelimiter 時的斷詞字元。
const DEFAULT_DELIMITER: &str = " \t,;:?'({[]})+-*/%&~<>=!|.\"#^@\\`$";

/// 註解的顏色。設定檔沒有這一項,用灰色。
pub const COMMENT_COLOR: Color = Color::GRAY;

/// Go 的 bufio.Scanner 切行:去掉行尾的 `\n` 與一個 `\r`。
fn lines(data: &[u8]) -> impl Iterator<Item = &[u8]> {
    let data = data.strip_suffix(b"\n").unwrap_or(data);
    data.split(|&c| c == b'\n').map(|l| l.strip_suffix(b"\r").unwrap_or(l)).filter(move |_| !data.is_empty())
}

fn trim_right(b: &[u8]) -> &[u8] {
    let n = b.iter().rposition(|&c| !matches!(c, b' ' | b'\t' | b'\r')).map_or(0, |i| i + 1);
    &b[..n]
}

/// 以第一段連續空白切成鍵與值。
fn split_kv(line: &[u8]) -> Option<(&[u8], String)> {
    let i = line.iter().position(|&c| c == b' ' || c == b'\t')?;
    let v = String::from_utf8_lossy(&line[i..]).trim().to_string();
    if v.is_empty() {
        return None;
    }
    Some((&line[..i], v))
}

fn is_word_char(r: char) -> bool {
    r == '_' || r == '#' || r.is_ascii_alphanumeric()
}

impl Config {
    /// 讀一個 keyword_*.cfg。內容是 Big5。
    pub fn parse(name: &str, data: &[u8]) -> Config {
        let mut c = Config { name: name.into(), keyword_delimiter: DEFAULT_DELIMITER.into(), ..Default::default() };
        let mut in_header = true;
        for raw in lines(data) {
            let line = trim_right(raw);
            if line.is_empty() {
                continue;
            }
            if line.starts_with(b"====") {
                in_header = false;
                continue;
            }
            if in_header {
                c.apply_header(line);
                continue;
            }
            let Some((k, v)) = split_kv(line) else { continue };
            let Some(col) = by_name(&v) else { continue };
            let key = decode(k, Enc::Big5);
            let mut it = key.chars();
            if let (Some(r), None) = (it.next(), it.next()) {
                if !is_word_char(r) {
                    c.symbols.insert(r, col);
                    continue;
                }
            }
            c.keywords.insert(key, col);
        }
        if c.line_comment == "//" && c.block_start.is_empty() {
            c.block_start = "/*".into();
            c.block_end = "*/".into();
        }
        c
    }

    fn apply_header(&mut self, line: &[u8]) {
        let Some((k, v)) = split_kv(line) else {
            // LineCommentStart 可以是空值(keyword_txt.cfg 就是),表示沒有行註解。
            if String::from_utf8_lossy(line).trim().eq_ignore_ascii_case("LineCommentStart") {
                self.line_comment.clear();
            }
            return;
        };
        match String::from_utf8_lossy(k).to_lowercase().as_str() {
            "quotecolor" => {
                if let Some(c) = by_name(&v) {
                    self.quote_color = Some(c);
                }
            }
            "numbercolor" => {
                if let Some(c) = by_name(&v) {
                    self.number_color = Some(c);
                }
            }
            "linecommentstart" => self.line_comment = v,
            "onlybeginlinecomment" => self.only_begin_line = v.eq_ignore_ascii_case("true"),
            // 值用單引號或雙引號包住,裡面就是那些字元。
            "keyworddelimiter" => self.keyword_delimiter = format!("{} \t", v.trim_matches(|c| c == '\'' || c == '"')),
            _ => {}
        }
    }

    /// 把一行切成上色片段(不在跨行註解裡)。
    pub fn highlight(&self, line: &str) -> Vec<Token> {
        self.highlight_state(line, false).0
    }

    /// 把一行切成上色片段,並回傳這一行結束時是不是還在跨行註解裡。呼叫端要把
    /// 上一行的回傳值餵進來。
    ///
    /// 順序有意義:跨行註解 → 行註解 → 字串 → 數字 → 關鍵字 → 符號。先判註解,
    /// 否則註解裡的關鍵字會被上色;先判字串,否則字串裡的符號會被上色。
    pub fn highlight_state(&self, line: &str, mut in_comment: bool) -> (Vec<Token>, bool) {
        let r: Vec<char> = line.chars().collect();
        let mut out = Vec::new();
        let mut pos = 0;
        let comment = |s, e| Token { start: s, end: e, color: Some(COMMENT_COLOR) };
        while pos < r.len() {
            if in_comment {
                let Some(e) = index_from(&r, &self.block_end, pos) else {
                    out.push(comment(pos, r.len()));
                    return (out, true);
                };
                let end = e + self.block_end.chars().count();
                out.push(comment(pos, end));
                pos = end;
                in_comment = false;
                continue;
            }
            let line_at = if self.line_comment.is_empty() {
                None
            } else {
                comment_at(&r[pos..], &self.line_comment, self.only_begin_line && pos == 0).map(|i| pos + i)
            };
            let block_at = if self.block_start.is_empty() { None } else { index_from(&r, &self.block_start, pos) };
            match (line_at, block_at) {
                (Some(l), b) if b.is_none_or(|b| l < b) => {
                    if l > pos {
                        out.extend(self.highlight_range(&r, pos, l));
                    }
                    out.push(comment(l, r.len()));
                    return (out, false);
                }
                (_, Some(b)) => {
                    if b > pos {
                        out.extend(self.highlight_range(&r, pos, b));
                    }
                    pos = b + self.block_start.chars().count();
                    out.push(comment(b, pos));
                    in_comment = true;
                }
                _ => {
                    out.extend(self.highlight_range(&r, pos, r.len()));
                    return (out, false);
                }
            }
        }
        (out, in_comment)
    }

    fn highlight_range(&self, r: &[char], from: usize, to: usize) -> Vec<Token> {
        let mut out = Vec::new();
        let mut i = from;
        while i < to {
            let ch = r[i];
            // 字串
            if let Some(qc) = self.quote_color.filter(|_| ch == '"' || ch == '\'') {
                let mut j = i + 1;
                while j < to && r[j] != ch {
                    if r[j] == '\\' && j + 1 < to {
                        j += 1;
                    }
                    j += 1;
                }
                if j < to {
                    j += 1;
                }
                out.push(Token { start: i, end: j, color: Some(qc) });
                i = j;
                continue;
            }
            // 數字
            if let Some(nc) = self.number_color.filter(|_| ch.is_ascii_digit() && (i == from || !is_word_char(r[i - 1]))) {
                let mut j = i;
                while j < to && (r[j].is_ascii_hexdigit() || matches!(r[j], '.' | 'x' | 'X')) {
                    j += 1;
                }
                out.push(Token { start: i, end: j, color: Some(nc) });
                i = j;
                continue;
            }
            // 關鍵字
            if is_word_char(ch) {
                let mut j = i;
                while j < to && is_word_char(r[j]) {
                    j += 1;
                }
                let w: String = r[i..j].iter().collect();
                out.push(Token { start: i, end: j, color: self.lookup_keyword(&w) });
                i = j;
                continue;
            }
            // 單一符號
            if let Some(&col) = self.symbols.get(&ch) {
                out.push(Token { start: i, end: i + 1, color: Some(col) });
                i += 1;
                continue;
            }
            let mut j = i;
            while j < to && !is_word_char(r[j]) && r[j] != '"' && r[j] != '\'' && !self.symbols.contains_key(&r[j]) {
                j += 1;
            }
            if j == i {
                j += 1;
            }
            out.push(Token { start: i, end: j, color: None });
            i = j;
        }
        out
    }

    fn lookup_keyword(&self, w: &str) -> Option<Color> {
        // 不分大小寫的語言(Pascal、BASIC…)在設定檔裡通常只寫一種寫法。
        self.keywords.get(w).or_else(|| self.keywords.get(&w.to_lowercase())).copied()
    }
}

/// 從 `from` 開始找 `sub` 在 `r` 裡的字元位置。
fn index_from(r: &[char], sub: &str, from: usize) -> Option<usize> {
    let m: Vec<char> = sub.chars().collect();
    if m.is_empty() || r.len() < m.len() {
        return None;
    }
    (from..=r.len() - m.len()).find(|&i| r[i..i + m.len()] == m[..])
}

fn comment_at(r: &[char], marker: &str, only_begin: bool) -> Option<usize> {
    let m: Vec<char> = marker.chars().collect();
    if m.is_empty() || m.len() > r.len() {
        return None;
    }
    if only_begin {
        // 行首可以有空白
        let start = r.iter().position(|&c| c != ' ' && c != '\t').unwrap_or(r.len());
        return (start + m.len() <= r.len() && r[start..start + m.len()] == m[..]).then_some(start);
    }
    (0..=r.len() - m.len()).find(|&i| r[i..i + m.len()] == m[..])
}

/// 一行裡上色相同的一段。`color` 為 None 表示用預設前景色。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Token {
    /// 字元索引。
    pub start: usize,
    pub end: usize,
    pub color: Option<Color>,
}

/// 一整組語法設定,含副檔名對照。
#[derive(Clone, Debug, Default)]
pub struct Set {
    by_ext: HashMap<String, Arc<Config>>,
}

impl Set {
    /// 從一個目錄讀 keyword.cfg 與它指到的所有設定檔。
    pub fn load(dir: &Path) -> std::io::Result<Set> {
        let index = std::fs::read(dir.join("keyword.cfg"))?;
        let mut s = Set::default();
        let mut cache: HashMap<String, Arc<Config>> = HashMap::new();
        let mut last: Option<Arc<Config>> = None;
        for raw in lines(&index) {
            let line = String::from_utf8_lossy(raw);
            let line = line.trim();
            if line.is_empty() {
                continue;
            }
            let Some((ext, file)) = split_kv(line.as_bytes()) else { continue };
            let ext = String::from_utf8_lossy(ext).to_lowercase();
            // "." 表示沿用上一行指到的設定檔:keyword.cfg 用這招讓 .c .h .rc 共用 keyword_c.cfg。
            if file == "." {
                if let Some(l) = &last {
                    s.by_ext.insert(ext, l.clone());
                }
                continue;
            }
            let cfg = match cache.get(&file) {
                Some(c) => c.clone(),
                None => {
                    let Ok(data) = std::fs::read(dir.join(&file)) else { continue };
                    let c = Arc::new(Config::parse(&file, &data));
                    cache.insert(file.clone(), c.clone());
                    c
                }
            };
            s.by_ext.insert(ext, cfg.clone());
            last = Some(cfg);
        }
        Ok(s)
    }

    /// 依副檔名取設定(副檔名照 Go 的 filepath.Ext:最後一段裡最後一個點之後)。
    pub fn for_name(&self, filename: &str) -> Option<Arc<Config>> {
        let base = filename.rsplit(['/', std::path::MAIN_SEPARATOR]).next().unwrap_or(filename);
        let ext = base.rfind('.').map_or("", |i| &base[i + 1..]);
        self.by_ext.get(&ext.to_lowercase()).cloned()
    }

    /// 有設定的副檔名。
    pub fn exts(&self) -> Vec<String> {
        self.by_ext.keys().cloned().collect()
    }
}

#[cfg(test)]
mod tests;
