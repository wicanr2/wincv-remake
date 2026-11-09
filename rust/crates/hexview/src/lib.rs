//! 16 進位檢視(對應 Go 版 `internal/hexview`)。右邊的字元欄認得 Big5:
//! 中文檔案用 HEX 看時,旁邊仍看得出是哪個字。

use wincv_cell::{is_wide, Color, Screen};
use wincv_textenc::{decode, Enc};

/// 每列幾個位元組。
pub const BYTES_PER_LINE: usize = 16;

#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub offset_fg: Color,
    /// 交替兩色,方便數位置。
    pub hex_fg: Color,
    pub hex_alt_fg: Color,
    pub ascii_fg: Color,
    pub wide_fg: Color,
    pub bg: Color,
    pub status_fg: Color,
    pub status_bg: Color,
}

impl Default for Theme {
    fn default() -> Theme {
        Theme {
            offset_fg: Color::LT_CYAN,
            hex_fg: Color::LT_GRAY,
            hex_alt_fg: Color::WHITE,
            ascii_fg: Color::LT_GREEN,
            wide_fg: Color::YELLOW,
            bg: Color::BLACK,
            status_fg: Color::BLACK,
            status_bg: Color::LT_GRAY,
        }
    }
}

#[derive(Clone, Debug)]
pub struct Model {
    pub name: String,
    pub data: Vec<u8>,
    /// 畫面第一列對應的列號。
    pub top: i32,
    pub theme: Theme,
    /// 字元欄把 Big5 雙位元組畫成中文。
    pub big5: bool,
}

impl Model {
    pub fn load(name: &str, data: Vec<u8>) -> Model {
        Model { name: name.to_string(), data, top: 0, theme: Theme::default(), big5: true }
    }

    /// 總列數。
    pub fn lines(&self) -> i32 {
        self.data.len().div_ceil(BYTES_PER_LINE) as i32
    }

    fn clamp(&mut self) {
        self.top = self.top.clamp(0, (self.lines() - 1).max(0));
    }

    pub fn scroll_by(&mut self, d: i32) {
        self.top += d;
        self.clamp();
    }
    pub fn home(&mut self) {
        self.top = 0;
        self.clamp();
    }
    pub fn end(&mut self, rows: i32) {
        self.top = self.lines() - rows;
        self.clamp();
    }

    pub fn draw(&mut self, s: &mut Screen) -> i32 {
        let t = self.theme;
        s.clear(t.hex_fg, t.bg);
        let rows = (s.rows - 1).max(0);
        self.clamp();
        for y in 0..rows {
            let ln = self.top + y;
            if ln >= self.lines() {
                break;
            }
            self.draw_line(s, y, ln as usize);
        }
        self.draw_status(s);
        rows
    }

    fn draw_line(&self, s: &mut Screen, y: i32, ln: usize) {
        let t = self.theme;
        let off = ln * BYTES_PER_LINE;
        let row = &self.data[off..(off + BYTES_PER_LINE).min(self.data.len())];
        let mut x = s.print(0, y, &format!("{off:08X}"), t.offset_fg, t.bg) + 2;
        for i in 0..BYTES_PER_LINE {
            let fg = if i % 2 == 1 { t.hex_alt_fg } else { t.hex_fg };
            let txt = row.get(i).map_or("  ".to_string(), |b| format!("{b:02X}"));
            s.print(x, y, &txt, fg, t.bg);
            x += 3; // 兩位數字加一格間隔
            if i == 7 {
                x += 1; // 中間再空一格,一眼看出前後半
            }
        }
        self.draw_chars(s, x, y, row);
    }

    fn draw_chars(&self, s: &mut Screen, x: i32, y: i32, row: &[u8]) {
        let t = self.theme;
        let mut i = 0;
        while i < row.len() {
            if self.big5 && i + 1 < row.len() && is_big5_lead(row[i]) && is_big5_trail(row[i + 1]) {
                let d = decode(&row[i..i + 2], Enc::Big5);
                let mut it = d.chars();
                if let (Some(r), None) = (it.next(), it.next()) {
                    if is_wide(r) {
                        s.print(x + i as i32, y, &r.to_string(), t.wide_fg, t.bg);
                        i += 2;
                        continue;
                    }
                }
            }
            let c = row[i];
            let ch = if (0x20..0x7F).contains(&c) { c as char } else { '.' };
            s.set(x + i as i32, y, ch, t.ascii_fg, t.bg);
            i += 1;
        }
    }

    fn draw_status(&self, s: &mut Screen) {
        let t = self.theme;
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', t.status_fg, t.status_bg);
        let mut left = format!("{}  [HEX]  {} bytes", self.name, self.data.len());
        if self.big5 {
            left.push_str("  Big5");
        }
        s.print(0, y, &left, t.status_fg, t.status_bg);
        let n = self.lines();
        let pct = if n > 0 { (self.top + 1) * 100 / n } else { 100 };
        let right = format!("{:08X}  {pct}%", self.top as usize * BYTES_PER_LINE);
        let x = s.cols - right.len() as i32;
        if x >= 0 {
            s.print(x, y, &right, t.status_fg, t.status_bg);
        }
    }
}

fn is_big5_lead(b: u8) -> bool {
    (0xA1..=0xF9).contains(&b)
}

fn is_big5_trail(b: u8) -> bool {
    (0x40..=0x7E).contains(&b) || (0xA1..=0xFE).contains(&b)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn layout_and_big5_column() {
        let mut data = b"AB\x00".to_vec();
        data.extend_from_slice(&[0xA4, 0xA4]); // Big5「中」
        let mut m = Model::load("x.bin", data);
        let mut s = Screen::new(80, 3);
        m.draw(&mut s);
        let row: String = (0..s.cols).filter_map(|x| s.at(x, 0)).filter(|c| !c.cont).map(|c| c.ch).collect();
        assert!(row.starts_with("00000000  41 42 00 A4 A4"), "{row}");
        assert!(row.contains("AB.中"), "{row}");
        assert_eq!(m.lines(), 1);
    }
}
