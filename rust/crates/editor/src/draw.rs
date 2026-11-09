//! 編輯器的繪製。

use wincv_cell::{is_wide, Screen};
use wincv_i18n::{t, tr};

use crate::{BlockKind, Model};

fn display_width(s: &str) -> i32 {
    s.chars().map(|r| if is_wide(r) { 2 } else { 1 }).sum()
}

impl Model {
    /// 一個字佔幾格。Tab 依 tab_width 對齊。
    fn disp_width(&self, r: char, at_col: usize) -> usize {
        if r == '\t' {
            let w = self.tab_width - at_col % self.tab_width;
            return if w == 0 { self.tab_width } else { w };
        }
        if is_wide(r) { 2 } else { 1 }
    }

    /// 把字元索引換成顯示格位置。Tab 與全形字都要算進去。
    pub fn col_to_screen(&self, line: &[char], col: usize) -> usize {
        let mut x = 0;
        for &r in line.iter().take(col) {
            x += self.disp_width(r, x);
        }
        x
    }

    /// col_to_screen 的反向:畫面上第 x 格(已加回 left)落在第幾個字上。
    /// 全形字佔兩格,點到後半格也算那個字;超出行尾就是行尾。
    pub fn screen_to_col(&self, line: &[char], x: usize) -> usize {
        let mut sx = 0;
        for (i, &r) in line.iter().enumerate() {
            let w = self.disp_width(r, sx);
            if x < sx + w {
                return i;
            }
            sx += w;
        }
        line.len()
    }

    /// 第 ln 行**開始**時是不是在跨行註解裡。
    ///
    /// 跨行註解的狀態要從檔頭累積,所以整份算一次並快取起來;內容改動時才重算。
    fn comment_state_at(&mut self, ln: usize) -> bool {
        let Some(sx) = self.syntax.clone().filter(|s| !s.block_start.is_empty()) else { return false };
        if self.cmt_dirty || self.cmt_state.len() != self.lines.len() + 1 {
            let mut st = Vec::with_capacity(self.lines.len() + 1);
            let mut inside = false;
            for l in &self.lines {
                st.push(inside);
                inside = sx.highlight_state(&l.iter().collect::<String>(), inside).1;
            }
            st.push(inside);
            self.cmt_state = st;
            self.cmt_dirty = false;
        }
        self.cmt_state.get(ln).copied().unwrap_or(false)
    }

    /// 把編輯器畫進 s,回傳內容區有幾列。
    pub fn draw(&mut self, s: &mut Screen) -> i32 {
        let th = self.theme;
        s.clear(th.fg, th.bg);
        let rows = (s.rows - 1).max(0);
        self.scroll_into_view(s.cols.max(0) as usize, rows as usize);
        for y in 0..rows {
            let ln = self.top + y as usize;
            if ln >= self.lines.len() {
                break;
            }
            self.draw_line(s, y, ln);
        }
        self.draw_cursor(s, rows);
        self.draw_status(s);
        rows
    }

    /// 讓游標留在畫面內。
    fn scroll_into_view(&mut self, cols: usize, rows: usize) {
        if rows == 0 {
            return;
        }
        if self.cur.line < self.top {
            self.top = self.cur.line;
        }
        if self.cur.line >= self.top + rows {
            self.top = self.cur.line + 1 - rows;
        }
        let x = self.col_to_screen(&self.lines[self.cur.line], self.cur.col);
        if x < self.left {
            self.left = x;
        }
        if x >= self.left + cols {
            self.left = (x + 1).saturating_sub(cols);
        }
    }

    fn draw_line(&mut self, s: &mut Screen, y: i32, ln: usize) {
        let th = self.theme;
        let mut colors = vec![None; self.lines[ln].len()];
        if let Some(sx) = self.syntax.clone() {
            let start = self.comment_state_at(ln);
            let text: String = self.lines[ln].iter().collect();
            for tok in sx.highlight_state(&text, start).0 {
                for c in colors.iter_mut().take(tok.end).skip(tok.start) {
                    *c = tok.color;
                }
            }
        }
        let line = &self.lines[ln];
        let mut x = 0usize;
        for (i, &r) in line.iter().enumerate() {
            let w = self.disp_width(r, x);
            let sx = x as i32 - self.left as i32;
            let (mut fg, mut bg) = (colors[i].unwrap_or(th.fg), th.bg);
            if self.in_block(ln, i) {
                (fg, bg) = (th.block_fg, th.block_bg);
            }
            if r == '\t' {
                for k in 0..w as i32 {
                    if sx + k >= 0 && sx + k < s.cols {
                        s.set(sx + k, y, ' ', fg, bg);
                    }
                }
            } else if sx >= 0 && sx < s.cols {
                let mut b = [0u8; 4];
                s.print(sx, y, r.encode_utf8(&mut b), fg, bg);
            }
            x += w;
        }
        // 整列區塊要把行尾之後也反白,才看得出整列被選起來。
        if self.block.kind == BlockKind::Line && self.in_block_line(ln) {
            for sx in (x as i32 - self.left as i32).max(0)..s.cols {
                s.set(sx, y, ' ', th.block_fg, th.block_bg);
            }
        }
    }

    fn in_block_line(&self, ln: usize) -> bool {
        if self.block.kind == BlockKind::None {
            return false;
        }
        let (top, bot, _, _) = self.block.norm();
        ln >= top && ln <= bot
    }

    fn in_block(&self, ln: usize, col: usize) -> bool {
        if !self.in_block_line(ln) {
            return false;
        }
        if self.block.kind == BlockKind::Line {
            return true;
        }
        let (_, _, left, right) = self.block.norm();
        col >= left && col < right
    }

    /// 把游標那一格反白。
    fn draw_cursor(&self, s: &mut Screen, rows: i32) {
        let y = self.cur.line as i32 - self.top as i32;
        if y < 0 || y >= rows {
            return;
        }
        let x = self.col_to_screen(&self.lines[self.cur.line], self.cur.col) as i32 - self.left as i32;
        if let Some(c) = s.at_mut(x, y) {
            std::mem::swap(&mut c.fg, &mut c.bg);
        }
    }

    fn draw_status(&self, s: &mut Screen) {
        let th = self.theme;
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', th.status_fg, th.status_bg);
        let mark = match self.block.kind {
            BlockKind::Rect => t("  區塊"),
            BlockKind::Line => t("  整列"),
            BlockKind::None => String::new(),
        };
        let mode = if self.insert { t("插入") } else { t("覆蓋") };
        let dirty = if self.dirty { " *" } else { "" };
        let left = format!("{}{dirty}  [{}]  {mode}{mark}", self.name, t(self.enc.name()));
        s.print(0, y, &left, th.status_fg, th.status_bg);
        let right = tr!("%d 行 %d 欄 / %d", self.cur.line + 1, self.cur.col + 1, self.lines.len());
        let x = s.cols - display_width(&right);
        if x >= 0 {
            s.print(x, y, &right, th.status_fg, th.status_bg);
        }
    }
}
