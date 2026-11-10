//! 底部的觸控功能列(對應 Go 版 `touch.go`)。
//!
//! 手機上沒有實體鍵盤,原版的快捷鍵得換成點得到的按鈕。功能列分三列:第一列是目前
//! 模式最常用的幾個動作,下面兩列是方向鍵、翻頁與 Esc / Enter。

use wincv_cell::{Color, Screen, V_LINE};
use wincv_i18n::t;
use wincv_keys::{Code, Key};

use crate::{cell_width, App, Mode};

/// 觸控功能列佔掉的列數。
pub const TOUCH_ROWS: i32 = 3;

pub(crate) struct TouchButton {
    pub label: String,
    pub key: Key,
    /// 主要動作佔兩倍寬。
    wide: bool,
}

fn btn(label: String, key: Key) -> TouchButton {
    TouchButton { label, key, wide: false }
}

fn wide(label: String, key: Key) -> TouchButton {
    TouchButton { label, key, wide: true }
}

#[derive(Clone, Copy)]
pub(crate) struct Span {
    pub x: i32,
    pub w: i32,
}

fn center_pad(label: &str, w: i32) -> i32 {
    ((w - cell_width(label)) / 2).max(0)
}

/// 下面兩列的方向鍵與翻頁。
pub(crate) fn touch_nav() -> [[(&'static str, Key); 5]; 2] {
    let n = Key::named;
    [
        [("Esc", n(Code::Esc)), ("PgUp", n(Code::PgUp)), ("▲", n(Code::Up)), ("PgDn", n(Code::PgDn)), ("Enter", n(Code::Enter))],
        [("Home", n(Code::Home)), ("◀", n(Code::Left)), ("▼", n(Code::Down)), ("▶", n(Code::Right)), ("End", n(Code::End))],
    ]
}

impl App {
    pub(crate) fn touch_bar(&self) -> Vec<TouchButton> {
        let k = Key::ch;
        match self.mode {
            Mode::Viewer | Mode::Markdown => vec![
                btn(t("尋找"), Key::named(Code::F7)),
                btn(t("編碼"), k('T')),
                btn(t("中英"), Key::named(Code::F8)),
                btn(t("放大"), Key::ctrl_ch('+')),
                btn(t("縮小"), Key::ctrl_ch('-')),
                wide(t("返回"), Key::named(Code::Esc)),
            ],
            Mode::Image => vec![
                btn(t("上張"), Key::named(Code::PgUp)),
                btn(t("下張"), Key::named(Code::PgDn)),
                btn(t("縮小"), k('-')),
                btn(t("放大"), k('+')),
                btn(t("原寸"), k('1')),
                wide(t("返回"), Key::named(Code::Esc)),
            ],
            Mode::Hex => vec![btn(t("尋找"), Key::named(Code::F7)), wide(t("返回"), Key::named(Code::Esc))],
            _ => vec![
                btn(t("標記"), k(' ')),
                btn(t("拷貝"), k('C')),
                btn(t("移動"), k('M')),
                btn(t("更名"), k('R')),
                btn(t("磁碟"), Key::alt_ch('D')),
                wide(t("選單"), Key::named(Code::F9)),
            ],
        }
    }

    /// 第一列各按鈕的位置。寬按鈕佔兩份,最後一個吃掉餘數。
    pub(crate) fn touch_spans(&self, cols: i32) -> Vec<Span> {
        let bar = self.touch_bar();
        let units: i32 = bar.iter().map(|b| if b.wide { 2 } else { 1 }).sum();
        if units == 0 {
            return Vec::new();
        }
        let mut x = 0;
        let n = bar.len();
        bar.iter()
            .enumerate()
            .map(|(i, b)| {
                let mut w = cols / units * if b.wide { 2 } else { 1 };
                if i == n - 1 {
                    w = cols - x;
                }
                let s = Span { x, w: w.max(1) };
                x += s.w;
                s
            })
            .collect()
    }

    pub(crate) fn draw_touch_bar(&self, s: &mut Screen) {
        if s.rows < TOUCH_ROWS + 2 {
            return;
        }
        let bar = self.touch_bar();
        let mut y = s.rows - TOUCH_ROWS;
        s.fill(0, y, s.cols, 1, ' ', Color::BLACK, Color::TOOL_GRAY);
        let spans = self.touch_spans(s.cols);
        for (i, b) in bar.iter().enumerate() {
            let Some(&Span { x, w }) = spans.get(i) else { break };
            // 主要動作用不同顏色,不是靠大小分辨。
            let fg = if b.wide { Color::RED } else { Color::BLACK };
            s.fill(x, y, w, 1, ' ', fg, Color::TOOL_GRAY);
            s.print(x + center_pad(&b.label, w), y, &b.label, fg, Color::TOOL_GRAY);
            if i < bar.len() - 1 && x + w < s.cols {
                s.set(x + w - 1, y, V_LINE, Color::GRAY, Color::TOOL_GRAY);
            }
        }
        for nav in touch_nav() {
            y += 1;
            s.fill(0, y, s.cols, 1, ' ', Color::LT_GRAY2, Color::BLUE);
            let mut x = 0;
            let n = nav.len() as i32;
            for (i, (label, key)) in nav.iter().enumerate() {
                let w = if i as i32 == n - 1 { s.cols - x } else { s.cols / n };
                // 兩顆「決定」鍵顏色不同,閉著眼也分得出兩端。
                let fg = if key.code == Code::Esc || key.code == Code::Enter { Color::YELLOW } else { Color::LT_GRAY2 };
                s.print(x + center_pad(label, w), y, label, fg, Color::BLUE);
                if (i as i32) < n - 1 {
                    s.set(x + w - 1, y, V_LINE, Color::LT_BLUE, Color::BLUE);
                }
                x += w;
            }
        }
    }

    /// 清單游標在內容區的第幾列(不在清單模式時回 -1)。外殼用它把軟鍵盤以外的東西捲到
    /// 看得見的地方。
    pub fn list_cursor_row(&self) -> i32 {
        if self.mode != Mode::Browser {
            return -1;
        }
        self.browser.cursor - self.browser.top
    }

    /// 功能列第 row 列(0 起算)、第 col 格是哪一顆鍵。cols 是整個畫面的欄數。
    pub fn touch_key_at(&self, col: i32, row: i32, cols: i32) -> Option<Key> {
        match row {
            0 => {
                let bar = self.touch_bar();
                self.touch_spans(cols)
                    .iter()
                    .enumerate()
                    .find(|(i, w)| *i < bar.len() && col >= w.x && col < w.x + w.w)
                    .map(|(i, _)| bar[i].key)
            }
            1 | 2 => {
                let nav = touch_nav()[(row - 1) as usize];
                let n = nav.len() as i32;
                (0..n).find_map(|i| {
                    let w = cols / n;
                    let x = i * w;
                    let w = if i == n - 1 { cols - x } else { w };
                    (col >= x && col < x + w).then_some(nav[i as usize].1)
                })
            }
            _ => None,
        }
    }
}
