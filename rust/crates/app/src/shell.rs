//! 改變路徑、開啟、執行、註解、中英文切換、全螢幕(對應 Go 版 `shell.go`)。

use std::collections::HashMap;
use std::path::{Path, PathBuf};

use wincv_i18n::t;
use wincv_textenc::Enc;

use crate::{App, Mode};

impl App {
    // --- P 改變路徑 -----------------------------------------------------------

    pub(crate) fn start_change_dir(&mut self) -> bool {
        let dir = self.browser.dir.to_string_lossy().into_owned();
        self.ask(&t("改變路徑:"), &dir, |a, p| {
            if p.is_empty() {
                return;
            }
            let mut p = expand_home(&p);
            if !p.is_absolute() {
                p = a.browser.dir.join(p);
            }
            // 路徑指到檔案時,進它所在的目錄並把游標停上去:貼一整條檔案路徑進來是很
            // 自然的用法。
            let mut want = String::new();
            if std::fs::metadata(&p).is_ok_and(|m| !m.is_dir()) {
                want = p.file_name().map(wincv_vfs::display_name).unwrap_or_default();
                p = p.parent().map(Path::to_path_buf).unwrap_or(p);
            }
            if let Err(e) = a.browser.load(&p) {
                a.message = e.to_string();
                return;
            }
            if !want.is_empty() {
                a.focus_on(&want);
            }
        });
        true
    }

    // --- O 開啟 / G 執行 ------------------------------------------------------

    /// 用系統預設的程式開啟游標上的項目。
    pub(crate) fn start_open(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案要先解出來才能開啟");
            return true;
        }
        let Some(e) = self.browser.current().filter(|e| !e.up) else { return false };
        let (name, p) = (e.name.clone(), self.browser.dir.join(&e.os_name));
        self.message = match wincv_launch::open(&p) {
            Ok(()) => t("已交給系統開啟: ") + &name,
            Err(e) => t("開不起來: ") + &e,
        };
        true
    }

    /// 執行一行命令,預設帶入游標上的檔案。
    pub(crate) fn start_run(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案不能直接執行");
            return true;
        }
        let mut def = String::new();
        if let Some(e) = self.browser.current().filter(|e| !e.up && !e.is_dir) {
            if wincv_launch::executable(&self.browser.dir.join(&e.os_name)) {
                def = shell_quote(&format!("./{}", e.name));
            } else {
                def = " ".to_string() + &shell_quote(&e.name);
            }
        }
        self.ask(&t("執行:"), &def, |a, line| {
            if line.trim().is_empty() {
                return;
            }
            a.message = match wincv_launch::run(&a.browser.dir, &line) {
                Ok(()) => t("已執行: ") + &line,
                Err(e) => t("執行失敗: ") + &e,
            };
        });
        true
    }

    // --- Alt-E 註解 -----------------------------------------------------------

    pub(crate) fn start_note(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡不能加註解");
            return true;
        }
        let Some(e) = self.browser.current().filter(|e| !e.up) else { return false };
        let (dir, name) = (self.browser.dir.clone(), e.name.clone());
        let init = self.browser.notes.get(&name).cloned().unwrap_or_default();
        self.ask(&(t("註解 ") + &name + ":"), &init, move |a, text| {
            let mut s = match wincv_note::Notes::load(&dir) {
                Ok(s) => s,
                Err(e) => {
                    a.message = t("讀不到註解檔: ") + &e.to_string();
                    return;
                }
            };
            s.set(&name, &text);
            if let Err(e) = s.save(&dir) {
                a.message = t("寫不進註解檔: ") + &e.to_string();
                return;
            }
            let _ = a.browser.reload();
            if text.trim().is_empty() {
                a.message = t("已清掉 ") + &name + &t(" 的註解");
            }
        });
        true
    }

    // --- F8 切換中英文顯示 ----------------------------------------------------

    /// 在「解讀成中文」與「一個位元組一個字」之間切換。
    ///
    /// 原版的 F8 叫「切換 中英文顯示」:關掉之後雙位元組不再被併成一個中文字,而是照
    /// .FON 的 0x00-0xFF 各畫一格。用來看混了控制碼的檔案,或確認某兩個位元組是什麼。
    pub(crate) fn toggle_cjk(&mut self) -> bool {
        self.english_only = !self.english_only;
        if self.mode == Mode::Viewer {
            if let Some(v) = self.viewer.as_mut() {
                self.view_raw = decode_view(self.english_only, &self.view_data, v.enc);
                let on = v.ansi;
                v.set_ansi(on, &self.view_raw);
            }
        }
        self.message = if self.english_only { t("英文顯示(一個位元組一格)") } else { t("中文顯示") };
        true
    }

    // --- F11 全螢幕 -----------------------------------------------------------

    pub(crate) fn toggle_fullscreen(&mut self) -> bool {
        self.fullscreen = !self.fullscreen;
        true
    }
}

fn expand_home(p: &str) -> PathBuf {
    if p == "~" || p.starts_with("~/") {
        let home = std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" }).filter(|h| !h.is_empty());
        if let Some(h) = home {
            return PathBuf::from(h).join(p.trim_start_matches('~').trim_start_matches('/'));
        }
    }
    PathBuf::from(p)
}

/// 把檔名包成 shell 安全的字面值。檔名裡有空白或引號很常見,不包起來會被切成好幾個參數。
pub(crate) fn shell_quote(s: &str) -> String {
    if s.is_empty() {
        return "''".into();
    }
    if s.chars().all(|r| ".//-_+".contains(r) || r.is_ascii_alphanumeric()) {
        return s.into();
    }
    format!("'{}'", s.replace('\'', r"'\''"))
}

/// 給檔案列表的註解讀取掛勾。
pub(crate) fn load_notes(dir: &Path) -> HashMap<String, String> {
    match wincv_note::Notes::load(dir) {
        Ok(s) if !s.is_empty() => s.raw().clone(),
        _ => HashMap::new(),
    }
}

/// 給檔案列表的磁碟容量掛勾。
pub(crate) fn disk_stat(dir: &Path) -> (i64, i64) {
    match wincv_vfs::disk_usage(dir) {
        Ok((f, t)) => (f as i64, t as i64),
        Err(_) => (0, 0),
    }
}

/// 依中英文顯示設定把位元組轉成要畫的文字。
pub(crate) fn decode_view(english_only: bool, data: &[u8], enc: Enc) -> String {
    if !english_only {
        return wincv_textenc::decode(data, enc);
    }
    // 每個位元組畫一格,走字型自己的字碼表(CP437)。不能把位元組當 Latin-1:0x80 以上
    // 會對到別的字模,而 DOS 時代的 ANSI art 用的正好就是 0xB0-0xDF 那一段方框與網點。
    data.iter().map(|&b| raw_rune(b)).collect()
}

/// 一個位元組在字型裡的字形。換行與 tab 例外:它們決定的是**版面**,不是字形。
/// 0x1A 是 DOS 的檔尾記號,同理。
fn raw_rune(b: u8) -> char {
    match b {
        b'\t' | b'\n' | b'\r' | 0x1A => b as char,
        _ => match wincv_cell::CP437[b as usize] {
            '\0' => ' ',
            r => r,
        },
    }
}
