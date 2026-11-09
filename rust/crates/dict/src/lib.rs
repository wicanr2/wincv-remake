//! 英漢字典與 KK 音標查詢(對應 Go 版 `internal/dict`)。
//!
//! 原版隨附四份資料,格式各不相同(分隔字元不一樣):
//!
//! | 檔案 | 內容 | 分隔 |
//! |---|---|---|
//! | eng.txt.dat | 英文單字 → 中文解釋 | 0x01 |
//! | kk.txt.dat | 英文單字 → KK 音標 | 0x01 |
//! | chi.txt.dat | 國字 → 發音與解釋 | 空白 |
//! | origin-verb.txt.dat | 變化形 → 原形 | `*` |
//!
//! chi.txt 的分隔字元有 image 內的字串佐證:「國字及解釋間須以空白隔開」。原版另有
//! .idx 索引檔,這裡不用:最大的一份 .dat 也只有 5.6 MB,載進來自己建索引更單純。
//! .idx 的已知結構記在 docs/formats/dict-dat.md。

mod kk;

pub use kk::kk_to_ipa;

use std::collections::HashMap;
use std::io;
use std::path::Path;

use wincv_textenc::{decode, Enc};

/// 一筆查詢結果。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Entry {
    pub word: String,
    /// 中文解釋。
    pub trans: String,
    /// KK 音標(已轉成 IPA)。
    pub kk: String,
    /// 原形(查到的是變化形時才有)。
    pub base: String,
}

/// 載入好的字典。
#[derive(Default)]
pub struct Dict {
    eng: HashMap<String, String>,
    kk: HashMap<String, String>,
    chi: HashMap<String, String>,
    /// 變化形 → 原形。
    verb: HashMap<String, String>,
    /// 排序過的英文詞,供前綴查詢。
    words: Vec<String>,
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

/// 逐行拆 `<key><sep><value>`。資料是 Big5。同一個詞出現多次就接起來,不要後面的
/// 蓋掉前面的。
fn parse_into(raw: &[u8], sep: u8, into: &mut HashMap<String, String>) {
    for line in raw.split(|&c| c == b'\n') {
        let n = line.iter().rposition(|&c| c != b'\r').map_or(0, |i| i + 1);
        let line = &line[..n];
        let Some(i) = line.iter().position(|&c| c == sep).filter(|&i| i > 0) else { continue };
        let key = decode(&line[..i], Enc::Big5);
        let val = decode(&line[i + 1..], Enc::Big5);
        into.entry(key).and_modify(|v| {
            v.push_str("; ");
            v.push_str(&val);
        }).or_insert(val);
    }
}

impl Dict {
    /// 從目錄載入四份資料。缺哪一份就少哪一種查詢,不會整個失敗:使用者可能只留了
    /// 其中幾份。
    pub fn load(dir: &Path) -> io::Result<Dict> {
        let mut d = Dict::default();
        let mut loaded = 0;
        for (file, sep) in [("eng.txt.dat", 0x01), ("kk.txt.dat", 0x01), ("chi.txt.dat", b' '), ("origin-verb.txt.dat", b'*')] {
            let Ok(raw) = std::fs::read(dir.join(file)) else { continue };
            let into = match file {
                "eng.txt.dat" => &mut d.eng,
                "kk.txt.dat" => &mut d.kk,
                "chi.txt.dat" => &mut d.chi,
                _ => &mut d.verb,
            };
            parse_into(&raw, sep, into);
            loaded += 1;
        }
        if loaded == 0 {
            return Err(io::Error::other(wincv_i18n::tr!("%s 底下找不到任何字典資料", dir.display().to_string())));
        }
        d.words = d.eng.keys().cloned().collect();
        d.words.sort();
        Ok(d)
    }

    /// 由記憶體裡的資料建字典(測試用)。
    pub fn from_parts(eng: &[u8], kk: &[u8], chi: &[u8], verb: &[u8]) -> Dict {
        let mut d = Dict::default();
        parse_into(eng, 0x01, &mut d.eng);
        parse_into(kk, 0x01, &mut d.kk);
        parse_into(chi, b' ', &mut d.chi);
        parse_into(verb, b'*', &mut d.verb);
        d.words = d.eng.keys().cloned().collect();
        d.words.sort();
        d
    }

    /// 查一個詞。查不到就試小寫,再試把變化形換成原形:查 "went" 要找得到 "go",
    /// 否則字典對真實文章沒什麼用。
    pub fn lookup(&self, word: &str) -> Option<Entry> {
        let w = word.trim();
        if w.is_empty() {
            return None;
        }
        if let Some(e) = self.lookup_exact(w) {
            return Some(e);
        }
        let low = lower(w);
        if low != w {
            if let Some(e) = self.lookup_exact(&low) {
                return Some(e);
            }
        }
        for k in [w, low.as_str()] {
            if let Some(base) = self.verb.get(k) {
                return Some(match self.lookup_exact(base) {
                    Some(e) => Entry { base: base.clone(), ..e },
                    None => Entry { word: k.into(), base: base.clone(), ..Default::default() },
                });
            }
        }
        // 中文:查國字解釋
        self.chi.get(w).map(|t| Entry { word: w.into(), trans: t.clone(), ..Default::default() })
    }

    fn lookup_exact(&self, w: &str) -> Option<Entry> {
        let (t, k) = (self.eng.get(w), self.kk.get(w));
        if t.is_none() && k.is_none() {
            return None;
        }
        Some(Entry {
            word: w.into(),
            trans: t.cloned().unwrap_or_default(),
            kk: kk_to_ipa(k.map_or("", |s| s.as_str())),
            base: String::new(),
        })
    }

    /// 以 p 開頭的前 n 個詞,供輸入時的候選清單。
    pub fn prefix(&self, p: &str, n: usize) -> Vec<String> {
        let p = lower(p);
        let i = self.words.partition_point(|w| w.as_str() < p.as_str());
        self.words[i..].iter().take_while(|w| w.starts_with(&p)).take(n).cloned().collect()
    }

    /// 各份資料的筆數:(eng, kk, chi, verb)。
    pub fn len(&self) -> (usize, usize, usize, usize) {
        (self.eng.len(), self.kk.len(), self.chi.len(), self.verb.len())
    }
}

#[cfg(test)]
mod tests;
