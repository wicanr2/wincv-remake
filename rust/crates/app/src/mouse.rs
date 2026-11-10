//! 滑鼠與觸控的共同入口(對應 Go 版 `mouse.go`)。
//!
//! 這一層只認**格子座標**,不認像素:像素怎麼換成格子取決於字型、倍率、選單層用不用
//! 另一套字級,那些都是外殼的事。外殼把「點到內容區第幾欄第幾列」「點到選單列第幾格」
//! 翻好再進來,這裡才知道格子底下是什麼。
//!
//! 原版是 Win32 程式,滑鼠本來就能用:點清單移游標、雙擊開檔、點選單列展開。這裡做的
//! 是同一件事,不是新功能。

use wincv_keys::{Code, Key};

use crate::menu::{cat_label, menu_cats, Menu};
use crate::touch::TOUCH_ROWS;
use crate::{cell_width, App, Mode};

impl App {
    /// 內容區被點了一下。col / row 以內容畫面(draw 拿到的那一份)為準,double 表示
    /// 這是連點的第二下。回傳畫面有沒有變。
    ///
    /// 單擊與雙擊的分工照 Windows 的慣例:單擊選,雙擊做。網頁的連結是例外:單擊就跟過去,
    /// 那是瀏覽器教會所有人的手感。
    pub fn click(&mut self, col: i32, row: i32, double: bool) -> bool {
        let r = self.click_inner(col, row, double);
        self.remember_pos();
        r
    }

    fn click_inner(&mut self, col: i32, row: i32, double: bool) -> bool {
        // 下拉開著時點到內容區:收起來,這一下就到此為止。點在選單外面的意思是「不要
        // 選單了」,不是「順便做那件事」。
        if self.menu.active {
            self.menu = Menu::default();
            return true;
        }
        if self.about {
            self.about = false;
            return true;
        }
        if self.prompt.active {
            return false;
        }
        // 觸控功能列在最底下幾列,各模式都一樣(見 draw)。
        if self.touch {
            let r = row - (self.screen_rows - TOUCH_ROWS);
            if r >= 0 {
                return match self.touch_key_at(col, r, self.screen_cols) {
                    Some(k) => self.handle_key(k),
                    None => false,
                };
            }
        }
        match self.mode {
            Mode::Browser => self.click_browser(col, row, double),
            Mode::Browse => self.click_browse(row),
            Mode::Thumbs => self.click_thumbs(col, row, double),
            Mode::Edit => self.click_editor(col, row),
            _ => false,
        }
    }

    /// 點到哪一列,就看那一列屬於哪個連結。點在文字上什麼都不做。
    fn click_browse(&mut self, row: i32) -> bool {
        let line = self.bv.top + row;
        let Some(i) = self.bv.links.iter().position(|l| line >= l.line && line < l.line + l.rows) else { return false };
        self.bv.cur = i as i32;
        self.browse_follow()
    }

    /// 第 0 列是路徑列,清單從第 1 列起;左邊可能有磁碟窗格。
    fn click_browser(&mut self, col: i32, row: i32, double: bool) -> bool {
        let i = row - 1;
        if i < 0 || i >= self.rows {
            return false;
        }
        let b = &mut self.browser;
        if b.drive_pane > 0 && col < b.drive_pane {
            if i as usize >= b.drives.len() {
                return false;
            }
            self.drive_focus = true;
            b.drive_cursor = i;
            if double {
                return self.drive_key(Key::named(Code::Enter));
            }
            return true;
        }
        let idx = b.top + i;
        if idx as usize >= b.entries.len() {
            return false;
        }
        self.drive_focus = false;
        self.browser.move_to(idx, self.rows);
        if double {
            return self.handle_key(Key::named(Code::Enter));
        }
        true
    }

    /// 縮圖是一個 cell_cols×cell_rows 的格子陣列,由 top 那一列排起。
    fn click_thumbs(&mut self, col: i32, row: i32, double: bool) -> bool {
        let (rows, thumb_cols) = (self.rows, self.thumb_cols);
        let Some(t) = self.thumbs.as_mut() else { return false };
        if t.cell_cols <= 0 || t.cell_rows <= 0 || row >= rows {
            return false;
        }
        let per = thumb_cols / t.cell_cols;
        if per <= 0 {
            return false;
        }
        let idx = (t.top as i32 + row / t.cell_rows) * per + col / t.cell_cols;
        if col / t.cell_cols >= per || idx as usize >= t.items.len() {
            return false;
        }
        t.cursor = idx as usize;
        if double {
            return self.handle_key(Key::named(Code::Enter));
        }
        true
    }

    /// 把游標放到點的那個字上。
    fn click_editor(&mut self, col: i32, row: i32) -> bool {
        let rows = self.rows;
        let Some(e) = self.editor.as_mut() else { return false };
        let ln = e.top + row.max(0) as usize;
        if row >= rows || ln >= e.lines.len() {
            return false;
        }
        let c = e.screen_to_col(&e.lines[ln].clone(), col.max(0) as usize + e.left);
        e.move_to(wincv_editor::Pos { line: ln, col: c });
        true
    }

    /// 按著左鍵橫向拖了 dx 格。目前只有檔案清單用得到:拖寬主檔名欄。
    pub fn drag(&mut self, dx: i32) -> bool {
        if dx == 0 || self.mode != Mode::Browser || self.menu.active || self.prompt.active {
            return false;
        }
        self.browser.resize_name(dx)
    }

    /// 滾輪。d 是格數,正數往下。
    ///
    /// 翻成上下鍵而不是直接動捲動位置:每個模式的「往下」意思不同(清單移游標、文字捲
    /// 一列、選單換項目),而那些差別已經全在各自的按鍵處理裡了。
    pub fn wheel(&mut self, d: i32) -> bool {
        let k = Key::named(if d < 0 { Code::Up } else { Code::Down });
        let mut changed = false;
        for _ in 0..d.abs() {
            changed |= self.handle_key(k);
        }
        changed
    }

    /// 選單列被點了。col 以**選單格**為單位。點到分類就展開它;已經展開同一個就收起來;
    /// 點在空白處收起來。
    pub fn menu_bar_click(&mut self, col: i32) -> bool {
        // 右側的 F9 / F1 / F2 等同按那顆鍵。下拉開著時先收起來:按 F9 本來就是開合,
        // 其他兩顆要換到別的畫面,不該留著下拉。
        if let Some(k) = self.bar_key_at(col) {
            let was_open = self.menu.active;
            self.menu = Menu::default();
            if k.code == Code::F9 && was_open {
                return true;
            }
            self.handle_key(k);
            return true;
        }
        let i = menu_cat_at(col);
        if i < 0 || (self.menu.active && self.menu.cat == i) {
            self.menu = Menu::default();
            return true;
        }
        if !self.menu.active {
            self.menu = Menu { active: true, ..Default::default() };
        }
        self.select_cat(i);
        true
    }

    /// 展開的下拉被點了。row 是下拉框內的第幾列(含標題列)。點到項目就執行,點到分隔線
    /// 或捲動提示不動。
    pub fn menu_drop_click(&mut self, row: i32) -> bool {
        let i = self.menu_drop_at(row);
        if i < 0 {
            return false;
        }
        self.menu.cursor = i;
        self.menu_key(Key::named(Code::Enter))
    }

    /// 只移動下拉的選取列,點擊時才執行項目。
    pub fn menu_drop_hover(&mut self, row: i32) -> bool {
        let i = self.menu_drop_at(row);
        if i < 0 || i == self.menu.cursor {
            return false;
        }
        self.menu.cursor = i;
        true
    }

    /// 下拉內的列換成項目索引,標題、分隔線與捲動提示回 -1。
    fn menu_drop_at(&self, row: i32) -> i32 {
        let m = &self.menu;
        if !m.active {
            return -1;
        }
        let row = if m.title.is_empty() { row } else { row - 1 };
        if row < 0 || row >= m.rows {
            return -1;
        }
        let i = m.top + row;
        match m.items.get(i as usize) {
            Some(it) if !it.sep => i,
            _ => -1,
        }
    }
}

/// 選單列第 col 格是哪個分類,不是分類回 -1。版面與 draw_menu_bar 相同:從第 1 格起。
fn menu_cat_at(col: i32) -> i32 {
    let mut x = 1;
    for (i, c) in menu_cats().iter().enumerate() {
        let w = cell_width(&cat_label(i, &c.name));
        if col >= x && col < x + w {
            return i as i32;
        }
        x += w;
    }
    -1
}
