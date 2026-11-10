//! 關掉時記下人在哪裡,下次開回同一個位置(對應 Go 版 app 的 `session.go`)。

use std::path::PathBuf;

use wincv_session::State;

use crate::help::HELP_NAME;
use crate::{App, Mode, MAX_SCALE, MIN_SCALE};

impl App {
    /// 取一份「回到原處要知道的事」。
    ///
    /// 壓縮檔內部不存:那個路徑對下一次啟動沒有意義(要先把壓縮檔開回來才走得到),存了
    /// 反而會讓還原失敗。改存壓縮檔本身所在的目錄,並讓游標停在那個壓縮檔上。
    pub fn snapshot(&mut self) -> State {
        let mut st = State {
            dir: self.browser.dir.to_string_lossy().into_owned(),
            cols: self.screen_cols,
            rows: self.screen_rows,
            zoom: self.zoom,
            lang: self.lang.clone(),
            cjk24: self.cjk24.clone(),
            name_w: self.browser.name_w,
            scale: self.scale,
            menu_bar: Some(self.menu_bar),
            menu_zoom: Some(self.menu_zoom),
            ..Default::default()
        };
        self.remember_pos();
        st.positions = self.positions.clone();
        if let Some(e) = self.browser.current() {
            st.cursor = e.name.clone();
        }
        if let Some(af) = self.archive() {
            let outer = af.name();
            st.dir = outer.parent().map(|p| p.to_string_lossy().into_owned()).unwrap_or_default();
            st.cursor = outer.file_name().map(wincv_vfs::display_name).unwrap_or_default();
            return st;
        }
        let (mode, file, top, cur) = match self.mode {
            Mode::Viewer => self.viewer.as_ref().map(|v| ("viewer", v.name.clone(), v.top, v.cur)),
            Mode::Hex => self.hex.as_ref().map(|h| ("hex", h.name.clone(), h.top, 0)),
            Mode::Edit => self.editor.as_ref().map(|e| ("edit", e.name.clone(), e.top as i32, 0)),
            Mode::Image => self.image.as_ref().map(|m| ("image", m.name.clone(), 0, 0)),
            _ => None,
        }
        .unwrap_or(("", String::new(), 0, 0));
        // 說明是內嵌的,不是檔案:還原時會找不到那個「檔名」。
        if file != HELP_NAME {
            (st.mode, st.file, st.top, st.cur) = (mode.to_string(), file, top, cur);
        }
        st
    }

    /// 回到上次的位置。
    ///
    /// 每一步都可以失敗而不影響下一步:目錄不見了就留在原處、檔案不見了就只回到目錄。
    /// **還原是錦上添花,不能讓程式開不起來**:上次看的那個檔案很可能正是這次被刪掉的
    /// 那一個。
    pub fn restore(&mut self, st: &State) {
        if !st.dir.is_empty() && std::fs::metadata(&st.dir).is_ok_and(|m| m.is_dir()) {
            self.browser.dir = PathBuf::from(&st.dir);
            let _ = self.browser.reload();
        }
        if st.zoom > 0 && st.zoom <= self.max_zoom {
            self.zoom = st.zoom;
        }
        if (wincv_browser::MIN_NAME_W..=wincv_browser::MAX_NAME_W).contains(&st.name_w) {
            self.browser.name_w = st.name_w;
        }
        if (MIN_SCALE..=MAX_SCALE).contains(&st.scale) {
            self.scale = st.scale;
        }
        if let Some(b) = st.menu_bar {
            self.menu_bar = b;
        }
        if let Some(z) = st.menu_zoom.filter(|&z| z >= -1 && z <= self.max_zoom) {
            self.menu_zoom = z;
        }
        if !st.positions.is_empty() {
            self.positions = st.positions.clone();
        }
        if let Some(l) = wincv_i18n::valid(&st.lang) {
            wincv_i18n::set(l);
            self.lang = st.lang.clone();
        }
        if wincv_eten::cjk24::find(&st.cjk24).is_some() {
            self.cjk24 = st.cjk24.clone();
        }
        if !st.cursor.is_empty() {
            self.focus_on(&st.cursor);
        }
        if st.mode.is_empty() || st.file.is_empty() {
            return;
        }
        let full = self.browser.dir.join(self.os_name(&st.file));
        if !std::fs::metadata(&full).is_ok_and(|m| !m.is_dir()) {
            return;
        }
        self.focus_on(&st.file);
        match st.mode.as_str() {
            "edit" => {
                self.open_editor();
                if let Some(e) = self.editor.as_mut() {
                    e.top = clamp_top(st.top, e.lines.len() as i32) as usize;
                }
            }
            "image" => {
                self.open_image(&st.file);
            }
            "hex" => {
                let Ok(data) = self.read_current(&st.file) else { return };
                self.open_hex(&st.file, data);
                if let Some(h) = self.hex.as_mut() {
                    h.top = clamp_top(st.top, h.data.len() as i32 / 16 + 1);
                }
            }
            // viewer / markdown:open_viewer 會依內容自己選一個。
            _ => {
                self.open_viewer(&st.file);
                if self.mode == Mode::Viewer {
                    if let Some(v) = self.viewer.as_mut() {
                        let n = v.lines.len() as i32;
                        v.top = clamp_top(st.top, n);
                        v.cur = clamp_top(st.cur, n);
                    }
                }
            }
        }
    }
}

/// 把存下來的捲動位置夾回合理範圍。
///
/// 檔案在兩次執行之間會變:上次捲到第 900 列,這次那個檔只剩 10 列。不夾的話畫面會是
/// 一片空白,而使用者只會覺得「檔案不見了」。
pub(crate) fn clamp_top(top: i32, n: i32) -> i32 {
    if top < 0 || n <= 0 || top >= n {
        return 0;
    }
    top
}
