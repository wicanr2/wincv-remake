//! 檔案列表(對應 Go 版 `internal/browser`):游標、標記、排序、欄位、
//! 路徑列、狀態列、捲軸、磁碟窗格。
//!
//! 版面與 Go 版逐格相同(由 tools/rust-parity.sh 的目錄畫面比對)。

mod text;

pub use text::{comma, lpad, pad, tail_of, truncate, width};

use std::collections::HashMap;
use std::ops::{Deref, DerefMut};
use std::path::{Path, PathBuf};
use std::time::SystemTime;

use wincv_cell::{Color, Screen};
use wincv_i18n::tr;
use wincv_vfs::{self as vfs, Drive, Fs, SortKey};

const COL_BASE: i32 = 8; // 主檔名(預設;可調,見 Model::name_w)
const COL_EXT: i32 = 3;
const COL_SIZE: i32 = 11;
const COL_DATE: i32 = 8; // MM-DD-YY
const COL_TIME: i32 = 5; // HH:MM
const SCROLL_W: i32 = 1;

/// 主檔名欄可調的範圍。
pub const MIN_NAME_W: i32 = COL_BASE;
pub const MAX_NAME_W: i32 = 60;

/// 檔案列表的配色。預設值量自原版畫面。
#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub path_fg: Color,
    pub path_bg: Color,
    /// 路徑列尾巴的 `*.*`。
    pub mask_fg: Color,
    /// 路徑列右邊的「第幾筆 / 共幾筆」。
    pub count_fg: Color,
    /// 「標記:」後面的兩個數字。
    pub mark_stat_fg: Color,
    /// 目錄總位元組。
    pub total_fg: Color,
    pub dir_fg: Color,
    pub file_fg: Color,
    pub date_fg: Color,
    pub link_fg: Color,
    pub mark_fg: Color,
    /// 最左邊那一欄(游標與標記的指示欄)的底色。
    pub mark_col_bg: Color,
    pub cursor_fg: Color,
    pub cursor_bg: Color,
    pub status_fg: Color,
    pub status_bg: Color,
    pub status_disk_fg: Color,
    pub status_name_fg: Color,
    pub scroll_fg: Color,
    pub scroll_bg: Color,
    pub scroll_thumb_fg: Color,
    pub scroll_arrow_fg: Color,
    pub drive_fg: Color,
    pub drive_bg: Color,
    pub drive_volume_fg: Color,
    pub bg: Color,
}

impl Default for Theme {
    fn default() -> Theme {
        Theme {
            path_fg: Color::LT_YELLOW,
            path_bg: Color::BLUE,
            mask_fg: Color::LT_GREEN,
            count_fg: Color::LT_GRAY2,
            mark_stat_fg: Color::LT_YELLOW,
            total_fg: Color::WHITE,
            dir_fg: Color::DIR_GREEN,
            file_fg: Color::LT_GRAY,
            date_fg: Color::LT_GREEN,
            link_fg: Color::LT_GRAY,
            mark_fg: Color::YELLOW,
            mark_col_bg: Color::BLUE,
            cursor_fg: Color::WHITE,
            cursor_bg: Color::RED,
            status_fg: Color::LT_YELLOW,
            status_bg: Color::BLUE,
            status_disk_fg: Color::LT_GRAY2,
            status_name_fg: Color::WHITE,
            scroll_fg: Color::TOOL_GRAY,
            scroll_bg: Color::TOOL_GRAY,
            scroll_thumb_fg: Color::WHITE,
            scroll_arrow_fg: Color::BLACK,
            drive_fg: Color::LT_GRAY,
            drive_bg: Color::BLUE,
            drive_volume_fg: Color::REMOVABLE_DISK_GREEN,
            bg: Color::BLACK,
        }
    }
}

/// 列表上的一項:vfs 的項目加上標記狀態。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    pub entry: vfs::Entry,
    pub marked: bool,
    /// 「..」那一項。
    pub up: bool,
}

impl Deref for Entry {
    type Target = vfs::Entry;
    fn deref(&self) -> &vfs::Entry {
        &self.entry
    }
}

impl DerefMut for Entry {
    fn deref_mut(&mut self) -> &mut vfs::Entry {
        &mut self.entry
    }
}

type NoteLoader = Box<dyn Fn(&Path) -> HashMap<String, String>>;
type ColorOf = Box<dyn Fn(&Entry) -> Color>;
type DiskStat = Box<dyn Fn(&Path) -> (i64, i64)>;

/// 檔案列表。
pub struct Model {
    pub fs: Box<dyn Fs>,
    pub dir: PathBuf,
    pub entries: Vec<Entry>,
    /// 游標所在的 index。
    pub cursor: i32,
    /// 畫面第一列對應的 index。
    pub top: i32,
    pub sort_key: SortKey,
    pub sort_desc: bool,
    pub theme: Theme,
    /// 目錄裡檔案(不含子目錄)的總位元組。
    pub total_bytes: i64,
    pub free_bytes: i64,
    pub disk_bytes: i64,
    /// 檔名 → 註解(dir.doc)。
    pub notes: HashMap<String, String>,
    pub note_loader: Option<NoteLoader>,
    /// 依檔案決定名字的顏色(副檔名配色)。沒設就是一般檔案色。
    pub color_of: Option<ColorOf>,
    pub drives: Vec<Drive>,
    /// 磁碟窗格的寬度;0 表示不顯示。
    pub drive_pane: i32,
    pub drive_cursor: i32,
    /// 主檔名欄寬;0 表示預設。
    pub name_w: i32,
    pub disk_stat: Option<DiskStat>,
    /// 底部保留給別人(預視窗格)的列數。
    pub reserve_bottom: i32,
}

impl Model {
    pub fn new(fs: Box<dyn Fs>, dir: &Path) -> Model {
        let mut m = Model {
            fs,
            dir: PathBuf::new(),
            entries: Vec::new(),
            cursor: 0,
            top: 0,
            sort_key: SortKey::Name,
            sort_desc: false,
            theme: Theme::default(),
            total_bytes: 0,
            free_bytes: 0,
            disk_bytes: 0,
            notes: HashMap::new(),
            note_loader: None,
            color_of: None,
            drives: Vec::new(),
            drive_pane: 0,
            drive_cursor: 0,
            name_w: 0,
            disk_stat: None,
            reserve_bottom: 0,
        };
        let _ = m.load(dir);
        m
    }

    /// 讀一個目錄,游標回到第一項。
    pub fn load(&mut self, dir: &Path) -> std::io::Result<()> {
        let mut es = self.fs.read_dir(dir)?;
        vfs::sort(&mut es, self.sort_key, self.sort_desc);
        let mut out = Vec::with_capacity(es.len() + 1);
        if vfs::parent(dir) != dir {
            out.push(Entry { entry: vfs::Entry::named("..", true), marked: false, up: true });
        }
        let mut total = 0;
        for e in es {
            if !e.is_dir {
                total += e.size;
            }
            out.push(Entry { entry: e, marked: false, up: false });
        }
        self.dir = dir.to_path_buf();
        self.entries = out;
        self.total_bytes = total;
        self.cursor = 0;
        self.top = 0;
        self.notes = self.note_loader.as_ref().map(|f| f(dir)).unwrap_or_default();
        (self.free_bytes, self.disk_bytes) = self.disk_stat.as_ref().map(|f| f(dir)).unwrap_or((0, 0));
        Ok(())
    }

    /// 重讀目前的目錄,游標盡量留在同一個檔名上。
    pub fn reload(&mut self) -> std::io::Result<()> {
        let cur = self.current().map(|e| e.name.clone());
        let top = self.top;
        let dir = self.dir.clone();
        self.load(&dir)?;
        let Some(cur) = cur else { return Ok(()) };
        if let Some(i) = self.entries.iter().position(|e| e.name == cur) {
            self.cursor = i as i32;
            self.top = top;
            return Ok(());
        }
        self.cursor = self.cursor.clamp(0, (self.entries.len() as i32 - 1).max(0));
        Ok(())
    }

    /// 換排序方式之後重排,保留標記與游標所在的檔名。
    pub fn resort(&mut self) {
        let cur = self.current().map(|e| e.name.clone());
        let up_n = usize::from(self.entries.first().is_some_and(|e| e.up));
        let rest = self.entries.split_off(up_n);
        let marked: Vec<String> = rest.iter().filter(|e| e.marked).map(|e| e.name.clone()).collect();
        let mut plain: Vec<vfs::Entry> = rest.into_iter().map(|e| e.entry).collect();
        vfs::sort(&mut plain, self.sort_key, self.sort_desc);
        for e in plain {
            let m = marked.contains(&e.name);
            self.entries.push(Entry { entry: e, marked: m, up: false });
        }
        if let Some(cur) = cur {
            if let Some(i) = self.entries.iter().position(|e| e.name == cur) {
                self.cursor = i as i32;
            }
        }
    }

    /// 游標所在的項目。
    pub fn current(&self) -> Option<&Entry> {
        usize::try_from(self.cursor).ok().and_then(|i| self.entries.get(i))
    }

    pub fn current_mut(&mut self) -> Option<&mut Entry> {
        usize::try_from(self.cursor).ok().and_then(|i| self.entries.get_mut(i))
    }

    /// 不含「..」的項目數。
    fn count_real(&self) -> i32 {
        let n = self.entries.len() as i32;
        if self.entries.first().is_some_and(|e| e.up) { n - 1 } else { n }
    }

    /// 標記了幾個、共多少位元組。
    pub fn marked_stats(&self) -> (i32, i64) {
        self.entries.iter().filter(|e| e.marked).fold((0, 0), |(n, b), e| (n + 1, b + e.size))
    }

    fn clamp(&mut self, rows: i32) {
        let n = self.entries.len() as i32;
        if self.cursor >= n {
            self.cursor = n - 1;
        }
        if self.cursor < 0 {
            self.cursor = 0;
        }
        if rows <= 0 {
            return;
        }
        if self.cursor < self.top {
            self.top = self.cursor;
        }
        if self.cursor >= self.top + rows {
            self.top = self.cursor - rows + 1;
        }
        if self.top < 0 {
            self.top = 0;
        }
    }

    pub fn move_by(&mut self, d: i32, rows: i32) {
        self.cursor += d;
        self.clamp(rows);
    }
    pub fn move_to(&mut self, i: i32, rows: i32) {
        self.cursor = i;
        self.clamp(rows);
    }
    pub fn home(&mut self, rows: i32) {
        self.move_to(0, rows);
    }
    pub fn end(&mut self, rows: i32) {
        self.move_to(self.entries.len() as i32 - 1, rows);
    }

    /// 切換游標項目的標記,游標往下一格。「..」不能標記。
    pub fn toggle_mark(&mut self, rows: i32) {
        if let Some(e) = self.current_mut() {
            if !e.up {
                e.marked = !e.marked;
            }
        }
        self.move_by(1, rows);
    }

    pub fn mark_all(&mut self, with_dirs: bool) {
        for e in &mut self.entries {
            if e.up || (e.is_dir && !with_dirs) {
                continue;
            }
            e.marked = true;
        }
    }

    pub fn unmark_all(&mut self) {
        for e in &mut self.entries {
            e.marked = false;
        }
    }

    fn name_w(&self) -> i32 {
        if self.name_w <= 0 { COL_BASE } else { self.name_w }
    }

    /// 主檔名欄加寬或縮窄 d 格。回傳是否真的變了。
    pub fn resize_name(&mut self, d: i32) -> bool {
        let w = (self.name_w() + d).clamp(MIN_NAME_W, MAX_NAME_W);
        if w == self.name_w() {
            return false;
        }
        self.name_w = w;
        true
    }

    fn list_x(&self) -> i32 {
        self.drive_pane
    }

    fn list_w(&self, s: &Screen) -> i32 {
        (s.cols - self.list_x() - SCROLL_W).max(1)
    }

    /// 畫整個列表,回傳內容區的列數(翻頁用)。
    pub fn draw(&mut self, s: &mut Screen) -> i32 {
        let t = self.theme;
        s.clear(t.file_fg, t.bg);
        self.draw_path_bar(s);
        let rows = (s.rows - 3 - self.reserve_bottom).max(0);
        self.clamp(rows);
        for i in 0..rows {
            let idx = self.top + i;
            if idx >= self.entries.len() as i32 {
                break;
            }
            let e = self.entries[idx as usize].clone();
            self.draw_row(s, 1 + i, &e, idx == self.cursor);
        }
        s.fill(self.list_x(), 1, 1, rows, ' ', t.file_fg, t.mark_col_bg);
        self.draw_drive_pane(s, rows);
        self.draw_scrollbar(s, rows);
        self.draw_status(s);
        rows
    }

    fn draw_path_bar(&self, s: &mut Screen) {
        let t = self.theme;
        s.fill(0, 0, s.cols, 1, ' ', t.path_fg, t.path_bg);
        let (n, bytes) = self.marked_stats();
        let segs = [
            (format!("{}/{:4}  ", self.cursor + 1, self.count_real()), t.count_fg),
            (wincv_i18n::t("標記: "), t.path_fg),
            (format!("{n} / {} / ", comma(bytes)), t.mark_stat_fg),
            (comma(self.total_bytes), t.total_fg),
        ];
        let total: i32 = segs.iter().map(|(s, _)| width(s)).sum();
        let right = (s.cols - total).max(0);
        let mut path = self.fs.label(&self.dir);
        path.push(std::path::MAIN_SEPARATOR);
        let avail = (right - 4).max(4); // 留給 "*.*" 與一格間隔
        if width(&path) > avail {
            path = format!("…{}", tail_of(&path, avail - 1));
        }
        let x = s.print(0, 0, &path, t.path_fg, t.path_bg);
        s.print(x, 0, "*.*", t.mask_fg, t.path_bg);
        let mut x = right;
        for (text, fg) in &segs {
            x += s.print(x, 0, text, *fg, t.path_bg);
        }
    }

    fn draw_row(&self, s: &mut Screen, y: i32, e: &Entry, cursor: bool) {
        let t = self.theme;
        let mut bg = t.bg;
        if cursor {
            bg = t.cursor_bg;
            s.fill(self.list_x() + 1, y, self.list_w(s) - 1, 1, ' ', t.cursor_fg, bg);
        }
        let mut name_fg = if e.marked {
            t.mark_fg
        } else if e.is_dir {
            t.dir_fg
        } else if let Some(f) = &self.color_of {
            f(e)
        } else {
            t.file_fg
        };
        if cursor {
            name_fg = t.cursor_fg;
        }
        let mut x = self.list_x() + 1;
        let (mut base, mut ext) = (e.base().to_string(), e.ext().to_string());
        let mut long = e.link.clone();
        // 名字放不進 8.3 的欄位就截斷,完整的名字畫在日期後面。
        if width(&base) > self.name_w() || width(&ext) > COL_EXT {
            if long.is_empty() {
                long = e.name.clone();
            }
            base = truncate(&base, self.name_w()).to_string();
            ext = truncate(&ext, COL_EXT).to_string();
        }
        x += s.print(x, y, &pad(&base, self.name_w()), name_fg, bg);
        x += 1;
        x += s.print(x, y, &pad(&ext, COL_EXT), name_fg, bg);
        x += 1;
        let size = if e.is_dir { "<DIR>".to_string() } else { comma(e.size) };
        x += s.print(x, y, &lpad(&size, COL_SIZE), name_fg, bg);
        x += 1;
        if let Some(mt) = e.mod_time {
            let (date, time) = local_date_time(mt);
            x += s.print(x, y, &date, t.date_fg, bg);
            x += 1;
            x += s.print(x, y, &time, name_fg, bg);
            x += 1;
        } else {
            x += COL_DATE + COL_TIME + 2;
        }
        let link_fg = if cursor { t.cursor_fg } else { t.link_fg };
        let right = self.list_x() + self.list_w(s);
        if !long.is_empty() && x < right {
            let w = right - x;
            if width(&long) > w {
                long = truncate(&long, w).to_string();
            }
            let n = s.print(x, y, &long, link_fg, bg);
            s.underline(x, y, n, true);
        }
    }

    fn draw_scrollbar(&self, s: &mut Screen, rows: i32) {
        let t = self.theme;
        let x = s.cols - SCROLL_W;
        if x < 1 || rows < 2 {
            return;
        }
        s.fill(x, 1, SCROLL_W, rows, ' ', t.scroll_fg, t.scroll_bg);
        s.set(x, 1, '▲', t.scroll_arrow_fg, t.scroll_bg);
        s.set(x, rows, '▼', t.scroll_arrow_fg, t.scroll_bg);
        let track = rows - 2; // 扣掉兩個箭頭
        let n = self.entries.len() as i32;
        if track < 1 || n <= rows {
            return; // 全部看得到就不畫滑塊,跟原版一樣
        }
        let th = (track * rows / n).max(1);
        let max_top = n - rows;
        let pos = if max_top > 0 { self.top * (track - th) / max_top } else { 0 };
        for i in 0..th {
            s.set(x, 2 + pos + i, '█', t.scroll_thumb_fg, t.scroll_bg);
        }
    }

    fn draw_drive_pane(&self, s: &mut Screen, rows: i32) {
        if self.drive_pane <= 0 {
            return;
        }
        let t = self.theme;
        let w = self.drive_pane;
        s.fill(0, 1, w, rows, ' ', t.drive_fg, t.drive_bg);
        for (i, d) in self.drives.iter().enumerate() {
            let i = i as i32;
            if i >= rows {
                break;
            }
            let mut fg = if d.volume { t.drive_volume_fg } else { t.drive_fg };
            let mut bg = t.drive_bg;
            if i == self.drive_cursor {
                (fg, bg) = (t.cursor_fg, t.cursor_bg);
                s.fill(0, 1 + i, w, 1, ' ', fg, bg);
            }
            s.print(0, 1 + i, truncate(&d.label, w), fg, bg);
        }
    }

    fn status_y(&self, s: &Screen) -> i32 {
        let mut y = s.rows - 2 - self.reserve_bottom;
        if y < 1 {
            y = s.rows - 2;
        }
        y.max(1)
    }

    fn draw_status(&self, s: &mut Screen) {
        let t = self.theme;
        let y = self.status_y(s);
        s.fill(0, y, s.cols, 2, ' ', t.status_fg, t.status_bg);
        if let Some(e) = self.current() {
            let mut x = 1;
            x += s.print(x, y, &pad(truncate(e.base(), self.name_w()), self.name_w()), t.status_fg, t.status_bg);
            x += 1;
            x += s.print(x, y, &pad(truncate(e.ext(), COL_EXT), COL_EXT), t.status_fg, t.status_bg);
            x += 1;
            let size = if e.is_dir { "<DIR>".to_string() } else { comma(e.size) };
            x += s.print(x, y, &lpad(&size, COL_SIZE), t.status_fg, t.status_bg);
            x += 1;
            if let Some(mt) = e.mod_time {
                let (date, time) = local_date_time(mt);
                x += s.print(x, y, &date, t.status_fg, t.status_bg);
                x += 1;
                s.print(x, y, &time, t.status_fg, t.status_bg);
            }
            let mut full = e.name.clone();
            if let Some(n) = self.notes.get(&e.name).filter(|n| !n.is_empty()) {
                full.push_str("  ; ");
                full.push_str(n);
            }
            s.print(1, y + 1, &full, t.status_name_fg, t.status_bg);
        }
        if self.disk_bytes > 0 {
            let right = tr!("剩餘: %sMB / %sMB", comma(self.free_bytes >> 20), comma(self.disk_bytes >> 20));
            let mut x = 1 + self.name_w() + 1 + COL_EXT + 1 + COL_SIZE + 1 + COL_DATE + 1 + COL_TIME + 1;
            if x + width(&right) > s.cols {
                x = s.cols - width(&right);
            }
            if x >= 0 {
                s.print(x, y, &right, t.status_disk_fg, t.status_bg);
            }
        }
        s.rule(0, y, s.cols, true);
    }
}

/// 本地時間的「MM-DD-YY」與「HH:MM」。時區照作業系統(TZ 與 /etc/localtime)。
pub fn local_date_time(t: SystemTime) -> (String, String) {
    let dt: chrono::DateTime<chrono::Local> = t.into();
    (dt.format("%m-%d-%y").to_string(), dt.format("%H:%M").to_string())
}

#[cfg(test)]
mod tests;
