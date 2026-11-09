//! 記住上次關掉程式時人在哪裡(對應 Go 版 `internal/session`)。
//!
//! 檔案格式與 Go 版相同(欄位名稱、省略零值、map 依 key 排序),
//! 換成 Rust 版之後讀得回 Go 版存的 session.json。
//!
//! 存的是「回到原處要知道的事」:目錄、游標停在哪個檔、開著哪份文件
//! 與捲到第幾列,加上畫面的尺寸與字級。標記、剪貼、搜尋結果、瀏覽歷史
//! 不存:那些是「這一次」的工作。

use std::collections::BTreeMap;
use std::io;
use std::path::{Path, PathBuf};

use serde::{Deserialize, Serialize};

fn is_zero_i32(v: &i32) -> bool {
    *v == 0
}
fn is_zero_f64(v: &f64) -> bool {
    *v == 0.0
}

/// 一份工作階段快照。每個欄位的零值都是「安全的預設」:舊版存的檔
/// 少了新欄位、新版讀到舊檔,兩邊都要能用。
#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(default)]
pub struct State {
    /// 最後所在的目錄。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub dir: String,
    /// 游標停在哪一個檔名(不存索引,目錄的內容會變)。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub cursor: String,
    /// 關掉時開著的畫面:"" / "viewer" / "hex" / "markdown" / "edit" / "image" / "browse"。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub mode: String,
    /// 那份文件的檔名(相對於 dir)。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub file: String,
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub top: i32,
    /// 文字檢視的游標(光棒)行號,與 top 分開存。
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub cur: i32,
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub cols: i32,
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub rows: i32,
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub zoom: i32,
    #[serde(skip_serializing_if = "is_zero_f64")]
    pub scale: f64,
    /// 選單用第幾級字;-1 是跟著內容。用 Option 分辨「沒存過」與「第一級」。
    #[serde(rename = "menuzoom", skip_serializing_if = "Option::is_none")]
    pub menu_zoom: Option<i32>,
    /// 選單列開著沒有。預設是開,Option 才分得出「舊檔沒存」與「關掉了」。
    #[serde(rename = "menubar", skip_serializing_if = "Option::is_none")]
    pub menu_bar: Option<bool>,
    /// 每個檔案上次看到哪:鍵是完整路徑。
    #[serde(skip_serializing_if = "BTreeMap::is_empty")]
    pub positions: BTreeMap<String, DocPos>,
    /// 介面語言;空的表示沒選過,跟著系統語系走。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub lang: String,
    /// 24 點 CJK 字形來源代號。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub cjk24: String,
    /// 主檔名欄的寬度,0 表示預設。
    #[serde(rename = "namew", skip_serializing_if = "is_zero_i32")]
    pub name_w: i32,
    /// 瀏覽模式停在哪一頁。
    #[serde(skip_serializing_if = "String::is_empty")]
    pub url: String,
}

/// 一份文件的位置。line、col 只有編輯器用。at 是最後一次用的時間(Unix 秒)。
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq, Serialize, Deserialize)]
#[serde(default)]
pub struct DocPos {
    pub top: i32,
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub line: i32,
    #[serde(skip_serializing_if = "is_zero_i32")]
    pub col: i32,
    pub at: i64,
}

/// 逐檔位置表的上限,滿了丟最久沒用的。
pub const MAX_POSITIONS: usize = 500;

impl State {
    /// 記下一份文件的位置。
    pub fn remember(&mut self, key: &str, p: DocPos) {
        if key.is_empty() {
            return;
        }
        self.positions.insert(key.to_string(), p);
        while self.positions.len() > MAX_POSITIONS {
            let oldest = self.positions.iter().min_by_key(|(_, v)| v.at).map(|(k, _)| k.clone());
            match oldest {
                Some(k) => {
                    self.positions.remove(&k);
                }
                None => break,
            }
        }
    }

    /// 寫回預設位置。
    pub fn save(&self) -> io::Result<()> {
        self.save_to(&path())
    }

    /// 先寫暫存檔再改名:程式關掉的那一刻最容易被中斷,寫到一半的 JSON
    /// 讀回來是壞的,那會讓「上次的位置」永久消失。
    pub fn save_to(&self, path: &Path) -> io::Result<()> {
        if path.as_os_str().is_empty() {
            return Err(io::Error::other(wincv_i18n_free("沒有可以寫的位置")));
        }
        if let Some(dir) = path.parent().filter(|d| !d.as_os_str().is_empty()) {
            std::fs::create_dir_all(dir)?;
        }
        let mut b = serde_json::to_vec_pretty(self).map_err(io::Error::other)?;
        b.push(b'\n');
        let mut tmp = path.as_os_str().to_owned();
        tmp.push(".tmp");
        std::fs::write(&tmp, b)?;
        std::fs::rename(&tmp, path)
    }
}

/// 這個 crate 不依賴 i18n;訊息由呼叫端翻譯。
fn wincv_i18n_free(s: &str) -> String {
    s.to_string()
}

/// 讀回上次的狀態。檔案不在、壞掉、是空的都回零值:這是錦上添花的功能,
/// 任何一種失敗都只該讓程式從頭開始。
pub fn load() -> State {
    load_from(&path())
}

pub fn load_from(path: &Path) -> State {
    std::fs::read(path).ok().and_then(|b| serde_json::from_slice(&b).ok()).unwrap_or_default()
}

/// 與 Go 的 os.UserConfigDir 相同的規則。
fn user_config_dir() -> Option<PathBuf> {
    let env = |k: &str| std::env::var_os(k).filter(|v| !v.is_empty()).map(PathBuf::from);
    if cfg!(windows) {
        return env("AppData");
    }
    if cfg!(target_os = "macos") {
        return env("HOME").map(|h| h.join("Library/Application Support"));
    }
    env("XDG_CONFIG_HOME").filter(|p| p.is_absolute()).or_else(|| env("HOME").map(|h| h.join(".config")))
}

/// 狀態檔的位置。取不到設定目錄就退回家目錄或工作目錄底下的隱藏檔:
/// Android 與某些精簡環境沒有這些變數,而「存不了」不該讓程式少一個功能。
pub fn path() -> PathBuf {
    if let Some(d) = user_config_dir() {
        return d.join("wincv").join("session.json");
    }
    if let Some(h) = std::env::var_os("HOME").filter(|v| !v.is_empty()) {
        return PathBuf::from(h).join(".wincv-session.json");
    }
    PathBuf::from(".wincv-session.json")
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tmpdir(tag: &str) -> PathBuf {
        let d = std::env::temp_dir().join(format!("wincv-session-{tag}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&d);
        d
    }

    #[test]
    fn round_trip_and_creates_dir() {
        let d = tmpdir("rt");
        let p = d.join("a/b/session.json");
        let mut s = State { dir: "/x".into(), cursor: "f.txt".into(), cols: 100, rows: 30, menu_zoom: Some(0), menu_bar: Some(false), ..State::default() };
        s.remember("/x/f.txt", DocPos { top: 3, line: 0, col: 0, at: 99 });
        s.save_to(&p).unwrap();
        assert_eq!(load_from(&p), s);
        let _ = std::fs::remove_dir_all(&d);
    }

    #[test]
    fn load_survives_garbage() {
        let d = tmpdir("garbage");
        std::fs::create_dir_all(&d).unwrap();
        let p = d.join("session.json");
        for junk in ["", "{", "not json", "[]"] {
            std::fs::write(&p, junk).unwrap();
            assert_eq!(load_from(&p), State::default(), "{junk:?}");
        }
        let _ = std::fs::remove_dir_all(&d);
    }

    /// Go 版存的檔,Rust 讀得回來;Rust 存出來的,與 Go 的輸出逐字相同。
    #[test]
    fn same_format_as_go() {
        let go = include_str!("../testdata/go-session.json");
        let s: State = serde_json::from_str(go).unwrap();
        assert_eq!(s.dir, "/home/u/docs");
        assert_eq!(s.menu_zoom, Some(-1));
        assert_eq!(s.menu_bar, Some(true));
        assert_eq!(s.positions["/home/u/docs/a.txt"].top, 12);
        let mut out = serde_json::to_string_pretty(&s).unwrap();
        out.push('\n');
        assert_eq!(out, go);
    }

    #[test]
    fn remember_evicts_oldest() {
        let mut s = State::default();
        for i in 0..MAX_POSITIONS + 3 {
            s.remember(&format!("/f{i}"), DocPos { at: i as i64, ..DocPos::default() });
        }
        assert_eq!(s.positions.len(), MAX_POSITIONS);
        assert!(!s.positions.contains_key("/f0") && s.positions.contains_key(&format!("/f{}", MAX_POSITIONS + 2)));
    }
}
