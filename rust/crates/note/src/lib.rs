//! 讀寫檔案註解(對應 Go 版 `internal/note`)。
//!
//! 原版把註解放在每個目錄底下的 dir.doc,一行一筆,格式是 `<檔名><空白><註解>`。
//! 這個格式沒有跳脫,所以檔名裡有空白時只能靠「第一段空白之前」切,和原版一樣。

use std::collections::HashMap;
use std::io;
use std::path::Path;

use wincv_textenc::{decode, detect, Enc};

/// 註解檔的檔名。原版可在設定裡改。
pub const FILE: &str = "dir.doc";

/// 一個目錄的註解表。
#[derive(Clone, Debug)]
pub struct Notes {
    m: HashMap<String, String>,
    /// 原檔的編碼,寫回去時要維持,否則原版讀到亂碼。
    enc: Enc,
}

impl Default for Notes {
    fn default() -> Notes {
        Notes { m: HashMap::new(), enc: Enc::Utf8 }
    }
}

impl Notes {
    /// 某個檔案的註解,沒有就回空字串。
    pub fn get(&self, name: &str) -> &str {
        self.m.get(name).map_or("", |s| s.as_str())
    }

    /// 設定註解。`text` 是空白表示刪掉這一筆。
    pub fn set(&mut self, name: &str, text: &str) {
        if text.trim().is_empty() {
            self.m.remove(name);
        } else {
            self.m.insert(name.into(), text.into());
        }
    }

    pub fn raw(&self) -> &HashMap<String, String> {
        &self.m
    }

    pub fn len(&self) -> usize {
        self.m.len()
    }

    pub fn is_empty(&self) -> bool {
        self.m.is_empty()
    }

    /// 讀一個目錄的註解。檔案不存在不算錯,回一個空的表。
    pub fn load(dir: &Path) -> io::Result<Notes> {
        let mut s = Notes::default();
        let data = match std::fs::read(dir.join(FILE)) {
            Ok(d) => d,
            Err(e) if e.kind() == io::ErrorKind::NotFound => return Ok(s),
            Err(e) => return Err(e),
        };
        let enc = detect(&data);
        if enc != Enc::Binary {
            s.enc = enc;
        }
        let text = decode(&data, s.enc).replace("\r\n", "\n");
        for line in text.split('\n') {
            if line.is_empty() || line.starts_with('#') {
                continue;
            }
            let Some(i) = line.find([' ', '\t']).filter(|&i| i > 0) else { continue };
            let v = line[i..].trim();
            if !v.is_empty() {
                s.m.insert(line[..i].into(), v.into());
            }
        }
        Ok(s)
    }

    /// 把註解寫回目錄。沒有任何一筆時就把檔案刪掉,不要留一個空檔。
    pub fn save(&self, dir: &Path) -> io::Result<()> {
        let p = dir.join(FILE);
        if self.m.is_empty() {
            return match std::fs::remove_file(&p) {
                Err(e) if e.kind() != io::ErrorKind::NotFound => Err(e),
                _ => Ok(()),
            };
        }
        let mut names: Vec<&String> = self.m.keys().collect();
        names.sort();
        let mut sb = String::new();
        for n in names {
            sb.push_str(n);
            sb.push(' ');
            sb.push_str(&self.m[n]);
            sb.push('\n');
        }
        // 維持原檔的編碼:原版是 Big5,寫成 UTF-8 會讓原版讀到亂碼。
        std::fs::write(p, wincv_convert::recode_lossy(sb.as_bytes(), Enc::Utf8, self.enc, "?"))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tmp(tag: &str) -> std::path::PathBuf {
        let d = std::env::temp_dir().join(format!("wincv-note-{tag}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&d);
        std::fs::create_dir_all(&d).unwrap();
        d
    }

    #[test]
    fn round_trip() {
        let d = tmp("rt");
        let mut s = Notes::load(&d).unwrap();
        assert!(s.is_empty());
        s.set("a.txt", "第一個檔");
        s.set("b.zip", "壓縮檔");
        s.save(&d).unwrap();
        let s2 = Notes::load(&d).unwrap();
        assert_eq!((s2.get("a.txt"), s2.get("b.zip"), s2.len()), ("第一個檔", "壓縮檔", 2));
    }

    #[test]
    fn save_removes_empty_file() {
        let d = tmp("empty");
        let mut s = Notes::default();
        s.set("a", "x");
        s.save(&d).unwrap();
        s.set("a", "  ");
        s.save(&d).unwrap();
        assert!(!d.join(FILE).exists());
    }

    /// 原檔是 Big5 就寫回 Big5,原版才讀得懂。
    #[test]
    fn keeps_big5_encoding() {
        let d = tmp("big5");
        std::fs::write(d.join(FILE), b"a.txt \xa4\xa4\xa4\xe5\xb5\xf9\xb8\xd1\r\n").unwrap();
        let mut s = Notes::load(&d).unwrap();
        assert_eq!(s.get("a.txt"), "中文註解");
        s.set("b.txt", "新的");
        s.save(&d).unwrap();
        let raw = std::fs::read(d.join(FILE)).unwrap();
        assert_eq!(detect(&raw), Enc::Big5);
        assert!(raw.windows(4).any(|w| w == b"\xa4\xa4\xa4\xe5"));
    }

    /// 檔名與註解以第一段空白分開,註解裡的空白保留。
    #[test]
    fn first_space_splits() {
        let d = tmp("split");
        std::fs::write(d.join(FILE), "a.txt  hello world\n# comment\nnoval\n").unwrap();
        let s = Notes::load(&d).unwrap();
        assert_eq!(s.get("a.txt"), "hello world");
        assert_eq!(s.len(), 1);
    }
}
