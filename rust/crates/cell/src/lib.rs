//! 整個 UI 的唯一渲染介面(對應 Go 版 `internal/cell`)。
//!
//! WinCV 的畫面是固定格點的自繪介面:每格一個半形字,全形字佔兩格。
//! 上層只寫字元與屬性,不碰像素;換字型、換縮放、換 backend 都不影響上層。

mod cp437;

pub use cp437::{from_cp437, CP437};

/// 調色盤的索引。實際 RGB 由 render 層決定。
///
/// 前 29 個是語法上色用的具名顏色,名稱與順序取自 WINCV.IMG 0x5692d 的
/// 斜線分隔清單;後面 14 個是 image 裡另外定義的顏色(檔案清單的
/// 副檔名配色用 DIR-* 系列)。順序必須與 Go 版相同,session 與設定檔
/// 不存索引,但測試與比對工具依賴它。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Default)]
pub struct Color(pub u8);

#[allow(missing_docs)]
impl Color {
    pub const BLACK: Color = Color(0);
    pub const DK_GRAY: Color = Color(1);
    pub const RED: Color = Color(2);
    pub const LT_RED: Color = Color(3);
    pub const GREEN: Color = Color(4);
    pub const LT_GREEN: Color = Color(5);
    pub const BLUE: Color = Color(6);
    pub const LT_BLUE: Color = Color(7);
    pub const YELLOW: Color = Color(8);
    pub const MILD_YELLOW: Color = Color(9);
    pub const LT_YELLOW: Color = Color(10);
    pub const MAGENTA: Color = Color(11);
    pub const LT_MAGENTA: Color = Color(12);
    pub const CYAN: Color = Color(13);
    pub const LT_CYAN: Color = Color(14);
    pub const GRAY: Color = Color(15);
    pub const WHITE: Color = Color(16);
    pub const LT_GRAY: Color = Color(17);
    pub const PURPLE: Color = Color(18);
    pub const LT_PURPLE: Color = Color(19);
    pub const ORANGE: Color = Color(20);
    pub const LT_ORANGE: Color = Color(21);
    pub const GOOSE_YELLOW: Color = Color(22);
    pub const BLUE_GREEN: Color = Color(23);
    pub const INK_GREEN: Color = Color(24);
    pub const MILD_WHITE: Color = Color(25);
    pub const MILD_GREEN: Color = Color(26);
    pub const MILD_CYAN: Color = Color(27);
    pub const MILD_MAGENTA: Color = Color(28);
    pub const DIR_GREEN: Color = Color(29);
    pub const DIR_LT_GREEN: Color = Color(30);
    pub const DIR_CYAN: Color = Color(31);
    pub const DIR_LT_CYAN: Color = Color(32);
    pub const DIR_YELLOW: Color = Color(33);
    pub const LT_GRAY1: Color = Color(34);
    pub const NEW_WHITE: Color = Color(35);
    pub const TOOL_GRAY: Color = Color(36);
    pub const HISTOGRAM_LT_GRAY: Color = Color(37);
    pub const LT_BLUE_GREEN: Color = Color(38);
    pub const EF_CYAN: Color = Color(39);
    pub const ORIGIN_GREEN: Color = Color(40);
    pub const REMOVABLE_DISK_GREEN: Color = Color(41);
    pub const C_LCCW_LT_GRAY: Color = Color(42);
    /// image 裡 LTGRAY 的第二個定義(0x50210,#C5C5C5)。
    pub const LT_GRAY2: Color = Color(43);
}

/// 顏色總數。
pub const NUM_COLORS: usize = 44;

/// keyword_*.cfg 用得到的具名顏色數(前 29 個)。
pub const NUM_CONFIG_COLORS: usize = 29;

/// 每個顏色的名字,順序與 [`Color`] 的常數一致。
pub const NAMES: [&str; NUM_COLORS] = [
    "black", "dkgray", "red", "ltred", "green", "ltgreen",
    "blue", "ltblue", "yellow", "mildyellow", "ltyellow",
    "magenta", "ltmagenta", "cyan", "ltcyan", "gray",
    "white", "ltgray", "purple", "ltpurple", "orange", "ltorange",
    "gooseyellow", "bluegreen", "inkgreen",
    "mildwhite", "mildgreen", "mildcyan", "mildmagenta",
    "dir-green", "dir-ltgreen", "dir-cyan", "dir-ltcyan", "dir-yellow",
    "ltgray1", "newwhite", "toolgray", "histogram-ltgray",
    "ltbluegreen", "ef-cyan", "origin-green",
    "removeable-disk-green", "c-lccw-ltgray", "ltgray2",
];

/// 依名字找顏色。
///
/// `ltgray` 在 image 裡定義了兩次,走名稱查詢的那一路(keyword_*.cfg 與
/// 狀態列)拿到的是後定義的 #C5C5C5,所以對到 [`Color::LT_GRAY2`]。
pub fn by_name(n: &str) -> Option<Color> {
    if n == "ltgray" {
        return Some(Color::LT_GRAY2);
    }
    NAMES.iter().position(|v| *v == n).map(|i| Color(i as u8))
}

/// 畫面上的一格。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub struct Cell {
    pub ch: char,
    pub fg: Color,
    pub bg: Color,
    /// 這格是全形字的左半。
    pub wide: bool,
    /// 這格是前一格全形字的右半。
    pub cont: bool,
    /// 格子上緣畫一條分隔線。
    pub rule: bool,
    /// 格子下緣畫底線。
    pub under: bool,
}

/// cols×rows 的格點畫面。
#[derive(Clone, Debug)]
pub struct Screen {
    pub cols: i32,
    pub rows: i32,
    cells: Vec<Cell>,
}

impl Screen {
    /// 新畫面,淺灰字黑底。
    pub fn new(cols: i32, rows: i32) -> Screen {
        let n = (cols.max(0) * rows.max(0)) as usize;
        let mut s = Screen { cols, rows, cells: vec![Cell::default(); n] };
        s.clear(Color::LT_GRAY, Color::BLACK);
        s
    }

    fn in_bounds(&self, x: i32, y: i32) -> bool {
        x >= 0 && y >= 0 && x < self.cols && y < self.rows
    }

    fn idx(&self, x: i32, y: i32) -> usize {
        (y * self.cols + x) as usize
    }

    /// 取一格,超出範圍回 None。
    pub fn at(&self, x: i32, y: i32) -> Option<&Cell> {
        if !self.in_bounds(x, y) {
            return None;
        }
        Some(&self.cells[self.idx(x, y)])
    }

    /// 取一格來改,超出範圍回 None。
    pub fn at_mut(&mut self, x: i32, y: i32) -> Option<&mut Cell> {
        if !self.in_bounds(x, y) {
            return None;
        }
        let i = self.idx(x, y);
        Some(&mut self.cells[i])
    }

    /// 整個畫面填空白。
    pub fn clear(&mut self, fg: Color, bg: Color) {
        for c in &mut self.cells {
            *c = Cell { ch: ' ', fg, bg, ..Cell::default() };
        }
    }

    /// 換尺寸,內容清空。
    pub fn resize(&mut self, cols: i32, rows: i32) {
        if cols == self.cols && rows == self.rows {
            return;
        }
        *self = Screen::new(cols, rows);
    }

    /// 寫一格(清掉全形、分隔線等屬性)。
    pub fn set(&mut self, x: i32, y: i32, ch: char, fg: Color, bg: Color) {
        if let Some(c) = self.at_mut(x, y) {
            *c = Cell { ch, fg, bg, ..Cell::default() };
        }
    }

    /// 從 (x, y) 起印字串,回傳用掉幾格。
    ///
    /// 全形字佔兩格;放不下全形字就停,不切一半。
    pub fn print(&mut self, x: i32, y: i32, s: &str, fg: Color, bg: Color) -> i32 {
        let mut n = 0;
        for r in s.chars() {
            if x + n >= self.cols {
                break;
            }
            if is_wide(r) {
                if x + n + 1 >= self.cols {
                    break;
                }
                if !self.in_bounds(x + n, y) {
                    break;
                }
                let i = self.idx(x + n, y);
                self.cells[i] = Cell { ch: r, fg, bg, wide: true, ..Cell::default() };
                self.cells[i + 1] = Cell { ch: r, fg, bg, cont: true, ..Cell::default() };
                n += 2;
            } else {
                if !self.in_bounds(x + n, y) {
                    break;
                }
                let i = self.idx(x + n, y);
                self.cells[i] = Cell { ch: r, fg, bg, ..Cell::default() };
                n += 1;
            }
        }
        n
    }

    /// 填一塊矩形。
    pub fn fill(&mut self, x: i32, y: i32, w: i32, h: i32, ch: char, fg: Color, bg: Color) {
        for yy in y..y + h {
            for xx in x..x + w {
                self.set(xx, yy, ch, fg, bg);
            }
        }
    }

    /// 一列裡從 x 起 w 格的上緣分隔線開或關。
    pub fn rule(&mut self, x: i32, y: i32, w: i32, on: bool) {
        for xx in x..x + w {
            if let Some(c) = self.at_mut(xx, y) {
                c.rule = on;
            }
        }
    }

    /// 一列裡從 x 起 w 格的底線開或關。
    pub fn underline(&mut self, x: i32, y: i32, w: i32, on: bool) {
        for xx in x..x + w {
            if let Some(c) = self.at_mut(xx, y) {
                c.under = on;
            }
        }
    }

    /// 把 src 整個貼到 (x, y),超出的部分丟掉。
    pub fn copy_from(&mut self, src: &Screen, x: i32, y: i32) {
        for sy in 0..src.rows {
            for sx in 0..src.cols {
                let v = src.cells[src.idx(sx, sy)];
                if let Some(d) = self.at_mut(x + sx, y + sy) {
                    *d = v;
                }
            }
        }
    }

    /// 只換屬性,字不動(反白游標列用)。
    pub fn set_attr(&mut self, x: i32, y: i32, w: i32, fg: Color, bg: Color) {
        for xx in x..x + w {
            if let Some(c) = self.at_mut(xx, y) {
                c.fg = fg;
                c.bg = bg;
            }
        }
    }
}

/// 這個字在畫面上佔兩格嗎。範圍與 Go 版相同。
pub fn is_wide(r: char) -> bool {
    matches!(r as u32,
        0x1100..=0x115F
        | 0x2E80..=0x303E
        | 0x3041..=0x33FF
        | 0x3400..=0x4DBF
        | 0x4E00..=0x9FFF
        | 0xA000..=0xA4CF
        | 0xAC00..=0xD7A3
        | 0xF900..=0xFAFF
        | 0xFE30..=0xFE6F
        | 0xFF00..=0xFF60
        | 0xFFE0..=0xFFE6)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn ltgray_picks_later_definition() {
        assert_eq!(by_name("ltgray"), Some(Color::LT_GRAY2));
        assert_eq!(by_name("black"), Some(Color::BLACK));
        assert_eq!(by_name("ltgreen"), Some(Color::LT_GREEN));
        assert_eq!(by_name("white"), Some(Color::WHITE));
        assert_eq!(by_name("gray"), Some(Color::GRAY));
        assert_eq!(by_name("nosuchcolor"), None);
    }

    #[test]
    fn print_out_of_range_rows_do_not_panic() {
        let mut s = Screen::new(80, 25);
        for (x, y, t) in [(2, 26, "中文字"), (2, 26, "abc"), (2, -1, "中文字"), (2, -1, "abc"), (2, 24, "中文字")] {
            s.print(x, y, t, Color::WHITE, Color::BLACK);
        }
    }

    #[test]
    fn wide_char_takes_two_cells_and_never_splits() {
        let mut s = Screen::new(5, 1);
        let n = s.print(0, 0, "中文字", Color::WHITE, Color::BLACK);
        assert_eq!(n, 4, "第三個全形字放不下,不能切一半");
        assert!(s.at(0, 0).unwrap().wide && s.at(1, 0).unwrap().cont);
        assert_eq!(s.at(4, 0).unwrap().ch, ' ');
    }
}
