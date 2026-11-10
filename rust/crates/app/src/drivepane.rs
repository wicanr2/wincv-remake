//! 左側的磁碟窗格(對應 Go 版 `drivepane.go`)。

use wincv_i18n::t;
use wincv_keys::{Code, Key};

use crate::App;

/// 磁碟窗格的寬度。
pub const DRIVE_PANE_COLS: i32 = 10;

impl App {
    pub(crate) fn toggle_drive_pane(&mut self) -> bool {
        let b = &mut self.browser;
        if b.drive_pane > 0 {
            b.drive_pane = 0;
            b.drives.clear();
            self.drive_focus = false;
            return true;
        }
        b.drives = wincv_vfs::drives();
        if b.drives.is_empty() {
            self.message = t("找不到可切換的磁碟");
            return true;
        }
        b.drive_pane = DRIVE_PANE_COLS;
        self.browser.drive_cursor = self.current_drive_index();
        true
    }

    /// 目前目錄所在的磁碟:路徑前綴最長的那一個。
    fn current_drive_index(&self) -> i32 {
        let dir = self.browser.dir.to_string_lossy();
        let (mut best, mut best_len) = (0, -1i64);
        for (i, d) in self.browser.drives.iter().enumerate() {
            let p = d.path.to_string_lossy();
            if has_path_prefix(&dir, &p) && p.len() as i64 > best_len {
                (best, best_len) = (i as i32, p.len() as i64);
            }
        }
        best
    }

    pub(crate) fn drive_key(&mut self, k: Key) -> bool {
        let b = &mut self.browser;
        let n = b.drives.len() as i32;
        match k.code {
            Code::Up => {
                if b.drive_cursor > 0 {
                    b.drive_cursor -= 1;
                }
            }
            Code::Down => {
                if b.drive_cursor < n - 1 {
                    b.drive_cursor += 1;
                }
            }
            Code::Home => b.drive_cursor = 0,
            Code::End => b.drive_cursor = n - 1,
            Code::Enter | Code::Right => {
                let i = b.drive_cursor;
                if i >= 0 && i < n {
                    self.drive_focus = false;
                    let p = b.drives[i as usize].path.clone();
                    if let Err(e) = self.browser.load(&p) {
                        self.message = e.to_string();
                    }
                }
            }
            Code::Esc | Code::Left | Code::Tab => self.drive_focus = false,
            _ => return false,
        }
        true
    }
}

pub(crate) fn has_path_prefix(p: &str, prefix: &str) -> bool {
    if p == prefix {
        return true;
    }
    if prefix.is_empty() || p.len() <= prefix.len() || !p.starts_with(prefix) {
        return false;
    }
    let next = p.as_bytes()[prefix.len()];
    prefix.ends_with('/') || next == b'/' || next == b'\\'
}
