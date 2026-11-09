//! PE2 式的區塊文字編輯器(對應 Go 版 `internal/editor`)。
//!
//! PE2 的特徵是**矩形區塊**:標記的是一個矩形範圍,不是「從某個位置到另一個
//! 位置」的連續文字。原版 changelog 講得很清楚:「標記寬度 5 的區塊,然後以
//! 插入模式填充空白」,那是矩形才做得到的事。
//!
//! 區塊有兩種:Alt-B 矩形區塊(每一列取相同的欄位範圍)與 Alt-L 整列區塊。
//! 兩種都能拷貝、移動、刪除、填充,行為不同,所以 [`Block`] 帶著自己的種類。

mod draw;

use std::sync::Arc;

use wincv_cell::Color;
use wincv_syntax::Config;
use wincv_textenc::Enc;

/// 區塊的種類。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum BlockKind {
    #[default]
    None,
    Rect,
    Line,
}

/// 緩衝區裡的一個位置。`col` 以字元計,不是位元組也不是顯示格。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Pos {
    pub line: usize,
    pub col: usize,
}

/// 目前標記的區塊。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Block {
    pub kind: BlockKind,
    pub anchor: Pos,
    pub head: Pos,
}

impl Block {
    /// 左上與右下(含端點的列、不含端點的欄)。
    pub fn norm(&self) -> (usize, usize, usize, usize) {
        let (a, h) = (self.anchor, self.head);
        (a.line.min(h.line), a.line.max(h.line), a.col.min(h.col), a.col.max(h.col))
    }
}

/// 編輯器的配色。
#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub fg: Color,
    pub bg: Color,
    pub block_fg: Color,
    pub block_bg: Color,
    pub status_fg: Color,
    pub status_bg: Color,
    pub line_no_fg: Color,
}

impl Default for Theme {
    fn default() -> Theme {
        Theme {
            fg: Color::LT_GRAY,
            bg: Color::BLACK,
            block_fg: Color::BLACK,
            block_bg: Color::LT_GRAY,
            status_fg: Color::BLACK,
            status_bg: Color::LT_GRAY,
            line_no_fg: Color::DK_GRAY,
        }
    }
}

/// 編輯器的狀態。
pub struct Model {
    pub name: String,
    pub enc: Enc,
    /// 存檔時用的換行,載入時記下來。
    pub eol: &'static str,
    pub lines: Vec<Vec<char>>,
    pub cur: Pos,
    /// 畫面第一列對應的行號。
    pub top: usize,
    /// 水平捲動量(顯示格)。
    pub left: usize,
    pub insert: bool,
    pub tab_width: usize,
    pub dirty: bool,
    pub block: Block,
    pub clipboard: Vec<String>,
    pub clip_kind: BlockKind,
    pub syntax: Option<Arc<Config>>,
    pub theme: Theme,
    undo: Vec<(Vec<Vec<char>>, Pos)>,
    /// 跨行註解的狀態快取,見 draw.rs 的 comment_state_at。
    cmt_state: Vec<bool>,
    cmt_dirty: bool,
}

/// 保留幾步復原。原版有 Ctrl-U 復原,層數未知,先給一個夠用的值。
pub const MAX_UNDO: usize = 200;

/// 游標能到的最遠欄位。只是個防呆上限,不是排版限制。
pub const MAX_COL: usize = 4096;

/// Go 的 unicode.ToLower:一個字對一個字,不會變長。
fn lower(c: char) -> char {
    c.to_lowercase().next().unwrap_or(c)
}

impl Model {
    /// 從位元組建立編輯緩衝區。
    pub fn load(name: &str, data: &[u8], enc: Enc, syntax: Option<Arc<Config>>) -> Model {
        let enc = if enc == Enc::Unknown { wincv_textenc::detect(data) } else { enc };
        let eol = if data.windows(2).any(|w| w == b"\r\n") { "\r\n" } else { "\n" };
        let s = wincv_textenc::decode(data, enc).replace("\r\n", "\n").replace('\r', "\n");
        let mut raw: Vec<&str> = s.split('\n').collect();
        if raw.len() > 1 && raw.last() == Some(&"") {
            raw.pop();
        }
        let mut lines: Vec<Vec<char>> = raw.iter().map(|l| l.chars().collect()).collect();
        if lines.is_empty() {
            lines.push(Vec::new());
        }
        Model {
            name: name.into(),
            enc,
            eol,
            lines,
            cur: Pos::default(),
            top: 0,
            left: 0,
            insert: true,
            tab_width: 8,
            dirty: false,
            block: Block::default(),
            clipboard: Vec::new(),
            clip_kind: BlockKind::None,
            syntax,
            theme: Theme::default(),
            undo: Vec::new(),
            cmt_state: Vec::new(),
            cmt_dirty: true,
        }
    }

    /// 把緩衝區寫回位元組,用載入時的編碼與換行。有字放不進原編碼時寫成 UTF-8
    /// (與 Go 版相同,不讓存檔失敗)。
    pub fn bytes(&self) -> Vec<u8> {
        let mut s: String = self.lines.iter().map(|l| l.iter().collect::<String>()).collect::<Vec<_>>().join(self.eol);
        s.push_str(self.eol);
        wincv_textenc::encode(&s, self.enc).unwrap_or_else(|| s.into_bytes())
    }

    // ---- 復原 ----

    fn push(&mut self) {
        self.undo.push((self.lines.clone(), self.cur));
        self.cmt_dirty = true;
        if self.undo.len() > MAX_UNDO {
            let n = self.undo.len() - MAX_UNDO;
            self.undo.drain(..n);
        }
        self.dirty = true;
    }

    /// 回到上一步。
    pub fn undo(&mut self) -> bool {
        let Some((lines, cur)) = self.undo.pop() else { return false };
        self.lines = lines;
        self.cur = cur;
        self.cmt_dirty = true;
        self.clamp_cur();
        true
    }

    // ---- 游標與編輯 ----

    /// 夾住游標位置。
    ///
    /// **欄位不夾到行尾**:PE2 這類 DOS 編輯器允許游標停在行尾之後的「虛擬空白」
    /// 上。矩形區塊要能在 3 個字的行上標出 5 格寬,靠的就是這件事。真的在虛擬
    /// 空白處打字時,才把中間補成空白(見 insert_char)。
    fn clamp_cur(&mut self) {
        self.cur.line = self.cur.line.min(self.lines.len() - 1);
        self.cur.col = self.cur.col.min(MAX_COL);
    }

    pub fn move_to(&mut self, p: Pos) {
        self.cur = p;
        self.clamp_cur();
    }

    /// 相對移動。超出範圍的負值停在 0。
    pub fn move_by(&mut self, dl: isize, dc: isize) {
        self.cur.line = self.cur.line.saturating_add_signed(dl);
        self.cur.col = self.cur.col.saturating_add_signed(dc);
        self.clamp_cur();
    }

    fn pad_to(l: &mut Vec<char>, n: usize) {
        if l.len() < n {
            l.resize(n, ' ');
        }
    }

    /// 在游標處插入一個字。覆蓋模式下取代原有的字。
    pub fn insert_char(&mut self, r: char) {
        self.push();
        let col = self.cur.col;
        let insert = self.insert;
        let l = &mut self.lines[self.cur.line];
        // 游標在虛擬空白處:先把中間補成空白,才有地方放這個字。
        Self::pad_to(l, col);
        if !insert && col < l.len() {
            l[col] = r;
        } else {
            l.insert(col, r);
        }
        self.cur.col += 1;
    }

    /// 斷行。
    pub fn new_line(&mut self) {
        self.push();
        let l = &mut self.lines[self.cur.line];
        let col = self.cur.col.min(l.len());
        let tail = l.split_off(col);
        self.lines.insert(self.cur.line + 1, tail);
        self.cur.line += 1;
        self.cur.col = 0;
    }

    /// 刪除游標左邊一個字;在行首則與上一行合併。
    pub fn backspace(&mut self) {
        if self.cur.col == 0 && self.cur.line == 0 {
            return;
        }
        self.push();
        if self.cur.col > 0 {
            let l = &mut self.lines[self.cur.line];
            if self.cur.col <= l.len() {
                l.remove(self.cur.col - 1);
            }
            // 在虛擬空白處往左退,只動游標,不動內容。
            self.cur.col -= 1;
            return;
        }
        let cur = self.lines.remove(self.cur.line);
        self.cur.line -= 1;
        let prev = &mut self.lines[self.cur.line];
        self.cur.col = prev.len();
        prev.extend(cur);
    }

    /// 刪除游標所在的字;在行尾(含虛擬空白)則與下一行合併。
    pub fn delete(&mut self) {
        let len = self.lines[self.cur.line].len();
        if self.cur.col >= len {
            if self.cur.line + 1 >= self.lines.len() {
                return;
            }
            self.push();
            let next = self.lines.remove(self.cur.line + 1);
            self.lines[self.cur.line].extend(next);
            return;
        }
        self.push();
        self.lines[self.cur.line].remove(self.cur.col);
    }

    // ---- 區塊 ----

    /// 開始或延伸一個區塊。同一種類再按一次就把終點移到游標。
    pub fn mark_block(&mut self, k: BlockKind) {
        if self.block.kind != k {
            self.block = Block { kind: k, anchor: self.cur, head: self.cur };
        } else {
            self.block.head = self.cur;
        }
    }

    /// 取消區塊(原版 Alt-U)。
    pub fn unmark_block(&mut self) {
        self.block = Block::default();
    }

    /// 取出區塊內容。矩形取每列的同一段欄位,整列取整行。
    fn block_text(&self) -> Option<Vec<String>> {
        if self.block.kind == BlockKind::None {
            return None;
        }
        let (top, bot, left, right) = self.block.norm();
        let mut out = Vec::new();
        for l in self.lines.iter().take(bot + 1).skip(top) {
            if self.block.kind == BlockKind::Line {
                out.push(l.iter().collect());
            } else {
                let (a, b) = (left.min(l.len()), right.min(l.len()));
                out.push(l[a..b].iter().collect());
            }
        }
        Some(out)
    }

    /// 把區塊放進剪貼區(原版 Ctrl-C / Alt-Z 的來源)。
    pub fn copy_block(&mut self) -> bool {
        let Some(t) = self.block_text() else { return false };
        self.clipboard = t;
        self.clip_kind = self.block.kind;
        true
    }

    /// 刪掉區塊(原版 Alt-D)。
    pub fn delete_block(&mut self) -> bool {
        if self.block.kind == BlockKind::None {
            return false;
        }
        self.push();
        let (top, bot, left, right) = self.block.norm();
        if self.block.kind == BlockKind::Line {
            let bot = bot.min(self.lines.len() - 1);
            if top <= bot {
                self.lines.drain(top..=bot);
            }
            if self.lines.is_empty() {
                self.lines.push(Vec::new());
            }
            self.cur = Pos { line: top, col: 0 };
        } else {
            for l in self.lines.iter_mut().take(bot + 1).skip(top) {
                let (a, b) = (left.min(l.len()), right.min(l.len()));
                l.drain(a..b);
            }
            self.cur = Pos { line: top, col: left };
        }
        self.unmark_block();
        self.clamp_cur();
        true
    }

    /// 在游標處貼上(原版 Ctrl-V / Alt-Z 的插入)。
    ///
    /// 矩形區塊是**插入**而不是覆蓋:每一列在游標欄插進去,右邊的字往後推。
    /// 這是 PE2 的行為,也是「在每一行前面加上 5 個空白」這種用法成立的原因。
    pub fn paste_block(&mut self) -> bool {
        if self.clipboard.is_empty() {
            return false;
        }
        self.push();
        let at = self.cur.line.min(self.lines.len());
        if self.clip_kind == BlockKind::Line {
            let ins: Vec<Vec<char>> = self.clipboard.iter().map(|s| s.chars().collect()).collect();
            self.lines.splice(at..at, ins);
            return true;
        }
        let col = self.cur.col;
        for (i, s) in self.clipboard.iter().enumerate() {
            let ln = self.cur.line + i;
            while ln >= self.lines.len() {
                self.lines.push(Vec::new());
            }
            let l = &mut self.lines[ln];
            Self::pad_to(l, col);
            l.splice(col..col, s.chars());
        }
        true
    }

    /// 用同一個字填滿矩形區塊(原版 Alt-F)。`insert` 為 true 時是插入而不是覆蓋
    /// (changelog 明講這是 0.5 版新增的選項)。
    pub fn fill_block(&mut self, r: char, insert: bool) -> bool {
        if self.block.kind != BlockKind::Rect {
            return false;
        }
        self.push();
        let (top, bot, left, right) = self.block.norm();
        if right <= left {
            return false;
        }
        let w = right - left;
        for l in self.lines.iter_mut().take(bot + 1).skip(top) {
            Self::pad_to(l, left);
            if insert {
                l.splice(left..left, std::iter::repeat_n(r, w));
                continue;
            }
            Self::pad_to(l, right);
            l[left..right].fill(r);
        }
        true
    }

    /// 把區塊搬到游標處(原版 Alt-M)。
    pub fn move_block(&mut self) -> bool {
        if !self.copy_block() {
            return false;
        }
        let (top, _, left, _) = self.block.norm();
        let mut dst = self.cur;
        self.delete_block();
        // 刪掉之後,如果目標在被刪區塊的後面,位置要往前挪。
        if self.clip_kind == BlockKind::Line && dst.line > top {
            dst.line = dst.line.saturating_sub(self.clipboard.len()).max(top);
        } else if self.clip_kind == BlockKind::Rect && dst.line >= top && dst.col > left {
            dst.col = dst.col.saturating_sub(self.clipboard[0].chars().count());
        }
        self.move_to(dst);
        self.paste_block()
    }

    // ---- 搜尋與取代 ----

    /// 從 `from` 開始找下一個 `pattern`。
    pub fn find(&self, pattern: &str, from: Pos, case_sensitive: bool) -> Option<Pos> {
        if pattern.is_empty() {
            return None;
        }
        let norm = |c: char| if case_sensitive { c } else { lower(c) };
        let pat: Vec<char> = pattern.chars().map(norm).collect();
        for ln in from.line..self.lines.len() {
            let l = &self.lines[ln];
            let start = if ln == from.line { from.col } else { 0 };
            if start > l.len() {
                continue;
            }
            let s: Vec<char> = l[start..].iter().map(|&c| norm(c)).collect();
            if s.len() >= pat.len() {
                if let Some(i) = (0..=s.len() - pat.len()).find(|&i| s[i..i + pat.len()] == pat[..]) {
                    return Some(Pos { line: ln, col: start + i });
                }
            }
        }
        None
    }

    /// 把 `at` 開始的 `n` 個字換成 `with`。
    pub fn replace(&mut self, at: Pos, n: usize, with: &str) {
        if at.line >= self.lines.len() {
            return;
        }
        self.push();
        let l = &mut self.lines[at.line];
        if at.col > l.len() {
            return;
        }
        let end = (at.col + n).min(l.len());
        l.splice(at.col..end, with.chars());
    }

    /// 全部取代,回傳換了幾處。
    pub fn replace_all(&mut self, pattern: &str, with: &str, case_sensitive: bool) -> usize {
        if pattern.is_empty() {
            return 0;
        }
        self.push();
        let plen = pattern.chars().count();
        let wlen = with.chars().count();
        let mut n = 0;
        let mut pos = Pos::default();
        while let Some(at) = self.find(pattern, pos, case_sensitive) {
            self.lines[at.line].splice(at.col..at.col + plen, with.chars());
            n += 1;
            pos = Pos { line: at.line, col: at.col + wlen };
        }
        n
    }

    /// 對 [top, bot] 這幾行加上或拿掉行註解。
    ///
    /// 註解記號取自語法設定(keyword_*.cfg 的 LineComment),沒有設定就什麼都
    /// 不做:猜一個 `//` 上去,在 .ini 或 .asm 裡是錯的。只要**還有一行沒被註解**
    /// 就整段加註解;全部都已註解才是拿掉。
    pub fn comment_lines(&mut self, top: usize, bot: usize) -> bool {
        let Some(mark) = self.syntax.as_ref().map(|s| s.line_comment.clone()).filter(|m| !m.is_empty()) else {
            return false;
        };
        let bot = bot.min(self.lines.len() - 1);
        if top > bot {
            return false;
        }
        let all_commented = self.lines[top..=bot].iter().all(|l| {
            let s: String = l.iter().collect();
            let s = s.trim_start_matches([' ', '\t']);
            s.is_empty() || s.starts_with(&mark)
        });
        self.push();
        for l in &mut self.lines[top..=bot] {
            let s: String = l.iter().collect();
            if s.trim().is_empty() {
                continue;
            }
            if all_commented {
                let i = l.iter().position(|&c| c != ' ' && c != '\t').unwrap_or(l.len());
                let rest: String = l[i..].iter().collect();
                let rest = rest.strip_prefix(&mark).unwrap_or(&rest);
                // 加註解時補的那一個空白,拿掉時也要收回來
                let rest = rest.strip_prefix(' ').unwrap_or(rest);
                l.truncate(i);
                l.extend(rest.chars());
            } else {
                // 註解記號放在行首,不跟著縮排走:縮排不同的幾行一起註解時,
                // 記號對齊比較看得出範圍。
                let mut nl: Vec<char> = format!("{mark} ").chars().collect();
                nl.extend(l.iter());
                *l = nl;
            }
        }
        self.dirty = true;
        self.cmt_dirty = true;
        self.clamp_cur();
        true
    }
}

#[cfg(test)]
mod tests;
