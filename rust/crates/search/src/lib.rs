//! 「尋找 檔名/字串/註解」(原版主畫面的 W,對應 Go 版 `internal/search`)。
//!
//! 內容搜尋一定要經過編碼判讀:直接拿 UTF-8 的關鍵字去比對 Big5 檔案的位元組,
//! 中文永遠搜不到,而且**不會報錯**,只是安靜地找不到。

use std::ffi::OsString;
use std::io;
use std::path::{Path, PathBuf};

use wincv_textenc::{decode, detect, Enc};

/// 搜尋目標。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum Kind {
    #[default]
    Name,
    Content,
    Comment,
}

/// 一筆結果。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Hit {
    /// 檔案所在的目錄。
    pub dir: PathBuf,
    pub name: OsString,
    /// 內容搜尋時的行號(1 起算),其餘為 0。
    pub line: usize,
    /// 內容搜尋時命中的那一行(已轉成 UTF-8),註解搜尋時是註解。
    pub text: String,
}

impl Hit {
    pub fn path(&self) -> PathBuf {
        self.dir.join(&self.name)
    }
}

/// 搜尋設定。
#[derive(Clone, Copy, Debug, Default)]
pub struct Options {
    pub kind: Kind,
    pub recursive: bool,
    /// 上限,達到就停。0 表示用 DEFAULT_MAX_HITS。
    pub max_hits: usize,
    /// 內容搜尋時單檔的大小上限,超過就跳過。0 表示用 DEFAULT_MAX_FILE_SIZE。
    pub max_file_size: u64,
}

pub const DEFAULT_MAX_HITS: usize = 2000;
pub const DEFAULT_MAX_FILE_SIZE: u64 = 8 << 20;

/// 註解檔的檔名。原版可以在設定裡改,預設是 dir.doc。
pub const COMMENT_FILE: &str = "dir.doc";

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

fn shown(n: &OsString) -> String {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        wincv_textenc::utf8_lossy_go(n.as_bytes())
    }
    #[cfg(not(unix))]
    {
        n.to_string_lossy().into_owned()
    }
}

/// 依名稱排序的目錄內容(Go 的 os.ReadDir / filepath.WalkDir 都是這個順序)。
fn read_dir_sorted(dir: &Path) -> io::Result<Vec<(OsString, bool)>> {
    let mut v: Vec<(OsString, bool)> = std::fs::read_dir(dir)?
        .flatten()
        .map(|e| (e.file_name(), e.file_type().map(|t| t.is_dir()).unwrap_or(false)))
        .collect();
    v.sort_by(|a, b| a.0.cmp(&b.0));
    Ok(v)
}

/// 執行搜尋。`pattern` 不分大小寫。
pub fn run(root: &Path, pattern: &str, mut o: Options) -> io::Result<Vec<Hit>> {
    if pattern.is_empty() {
        return Ok(Vec::new());
    }
    if o.max_hits == 0 {
        o.max_hits = DEFAULT_MAX_HITS;
    }
    if o.max_file_size == 0 {
        o.max_file_size = DEFAULT_MAX_FILE_SIZE;
    }
    let low = lower(pattern);
    let mut hits = Vec::new();
    if o.kind == Kind::Comment {
        if o.recursive {
            walk(root, &mut |d, _, is_dir| {
                if is_dir {
                    comment_hits(&mut hits, d, &low);
                }
                true
            });
        } else {
            comment_hits(&mut hits, root, &low);
        }
        return Ok(hits);
    }
    let visit = |dir: &Path, name: &OsString, hits: &mut Vec<Hit>| match o.kind {
        Kind::Name => {
            if lower(&shown(name)).contains(&low) {
                hits.push(Hit { dir: dir.into(), name: name.clone(), line: 0, text: String::new() });
            }
        }
        _ => content_hits(hits, dir, name, &low, &o),
    };
    if !o.recursive {
        for (name, is_dir) in read_dir_sorted(root)? {
            if hits.len() >= o.max_hits {
                break;
            }
            if !is_dir {
                visit(root, &name, &mut hits);
            }
        }
        return Ok(hits);
    }
    walk(root, &mut |dir, name, is_dir| {
        if hits.len() >= o.max_hits {
            return false;
        }
        if let (false, Some(name)) = (is_dir, name) {
            visit(dir, name, &mut hits);
        }
        true
    });
    Ok(hits)
}

/// 依名稱順序遞迴走訪(與 filepath.WalkDir 相同)。`f(目錄, 檔名, 是否目錄)`:
/// 對目錄本身呼叫時檔名是 None;回 false 就整個停下。讀不到的目錄跳過。
fn walk(root: &Path, f: &mut dyn FnMut(&Path, Option<&OsString>, bool) -> bool) -> bool {
    if !f(root, None, true) {
        return false;
    }
    let Ok(es) = read_dir_sorted(root) else { return true };
    for (name, is_dir) in es {
        if is_dir {
            if !walk(&root.join(&name), f) {
                return false;
            }
        } else if !f(root, Some(&name), false) {
            return false;
        }
    }
    true
}

fn content_hits(hits: &mut Vec<Hit>, dir: &Path, name: &OsString, low: &str, o: &Options) {
    let p = dir.join(name);
    let Ok(md) = std::fs::metadata(&p) else { return };
    if md.len() > o.max_file_size || md.len() == 0 {
        return;
    }
    let Ok(data) = std::fs::read(&p) else { return };
    let enc = detect(&data);
    if enc == Enc::Binary {
        return;
    }
    // 先整份轉成 UTF-8 再比對。少了這一步,Big5 檔案裡的中文永遠搜不到。
    let text = decode(&data, enc);
    if !lower(&text).contains(low) {
        return;
    }
    let body = text.strip_suffix('\n').unwrap_or(&text);
    for (i, line) in body.split('\n').enumerate() {
        let line = line.strip_suffix('\r').unwrap_or(line);
        if lower(line).contains(low) {
            hits.push(Hit { dir: dir.into(), name: name.clone(), line: i + 1, text: line.trim().into() });
            if hits.len() >= o.max_hits {
                break;
            }
        }
    }
}

/// 在註解檔裡找。格式是 `<檔名><空白><註解>`。
fn comment_hits(hits: &mut Vec<Hit>, dir: &Path, low: &str) {
    let Ok(data) = std::fs::read(dir.join(COMMENT_FILE)) else { return };
    let text = decode(&data, detect(&data)).replace("\r\n", "\n");
    for line in text.split('\n') {
        let Some(i) = line.find([' ', '\t']).filter(|&i| i > 0) else { continue };
        let note = line[i..].trim();
        if lower(note).contains(low) {
            hits.push(Hit { dir: dir.into(), name: line[..i].into(), line: 0, text: note.into() });
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tmp(tag: &str) -> PathBuf {
        let d = std::env::temp_dir().join(format!("wincv-search-{tag}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&d);
        std::fs::create_dir_all(d.join("sub")).unwrap();
        std::fs::write(d.join("Readme.TXT"), "hello\nWorld here\n").unwrap();
        std::fs::write(d.join("sub/readme2.txt"), "nothing\n").unwrap();
        std::fs::write(d.join("sub/big5.txt"), b"\xa4\xa4\xa4\xe5\xc0\xc9\xae\xd7\r\nsecond\r\n").unwrap();
        std::fs::write(d.join("bin.dat"), [0u8, 1, 2, 3, b'w', b'o', b'r', b'l', b'd', 0, 0, 0]).unwrap();
        d
    }

    fn names(h: &[Hit]) -> Vec<String> {
        h.iter().map(|h| h.name.to_string_lossy().into_owned()).collect()
    }

    #[test]
    fn by_name() {
        let d = tmp("name");
        let flat = run(&d, "README", Options::default()).unwrap();
        assert_eq!(names(&flat), ["Readme.TXT"]);
        let deep = run(&d, "readme", Options { recursive: true, ..Default::default() }).unwrap();
        assert_eq!(names(&deep), ["Readme.TXT", "readme2.txt"]);
    }

    #[test]
    fn by_content_skips_binary() {
        let d = tmp("content");
        let h = run(&d, "WORLD", Options { kind: Kind::Content, ..Default::default() }).unwrap();
        assert_eq!(h.len(), 1);
        assert_eq!((h[0].line, h[0].text.as_str()), (2, "World here"));
    }

    /// Big5 檔案要先轉成 UTF-8 才搜得到中文。
    #[test]
    fn by_content_big5() {
        let d = tmp("big5");
        let h = run(&d, "檔案", Options { kind: Kind::Content, recursive: true, ..Default::default() }).unwrap();
        assert_eq!(names(&h), ["big5.txt"]);
        assert_eq!(h[0].text, "中文檔案");
    }

    #[test]
    fn by_comment() {
        let d = tmp("comment");
        std::fs::write(d.join("sub").join(COMMENT_FILE), "big5.txt 重要的檔案\nx 別的\n").unwrap();
        let h = run(&d, "重要", Options { kind: Kind::Comment, recursive: true, ..Default::default() }).unwrap();
        assert_eq!(names(&h), ["big5.txt"]);
        assert!(run(&d, "重要", Options { kind: Kind::Comment, ..Default::default() }).unwrap().is_empty());
    }

    #[test]
    fn max_hits() {
        let d = tmp("max");
        let h = run(&d, "e", Options { recursive: true, max_hits: 1, ..Default::default() }).unwrap();
        assert_eq!(h.len(), 1);
    }
}
