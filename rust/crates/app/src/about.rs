//! 「關於」畫面(對應 Go 版 `about.go`)。

use wincv_cell::{Color, Screen};
use wincv_i18n::t;
use wincv_keys::Key;

use crate::{cell_width, App};

/// 版本字串,建置時由 `WINCV_VERSION` 環境變數帶入(對應 Go 版的 -ldflags -X)。
pub const VERSION: &str = match option_env!("WINCV_VERSION") {
    Some(v) => v,
    None => "development",
};

fn about_lines() -> Vec<(String, Color)> {
    vec![
        (format!("WinCV Remake {VERSION}"), Color::LT_CYAN),
        (String::new(), Color::LT_GRAY),
        (t("原作   WinCV 0.52(CView for Windows)"), Color::LT_GRAY),
        (t("       Lcc Wizard(林健總)   cview.com.tw"), Color::LT_GRAY),
        (t("       最後更新 2011-11-24"), Color::GRAY),
        (String::new(), Color::LT_GRAY),
        (t("重製   王俊又"), Color::LT_YELLOW),
        (t("       為保存台灣中文軟體盡一份心力"), Color::LT_YELLOW),
        (String::new(), Color::LT_GRAY),
        ("Rust".to_string(), Color::GRAY),
        ("Linux / Windows / macOS / Android".to_string(), Color::GRAY),
        (t("半形 cvga.fon + 全形倚天點陣字"), Color::GRAY),
        (t("Android 沒有這兩份,改用系統字型"), Color::GRAY),
        (String::new(), Color::LT_GRAY),
        (t("按任意鍵關閉"), Color::DK_GRAY),
    ]
}

impl App {
    pub fn abouting(&self) -> bool {
        self.about
    }

    pub(crate) fn open_about(&mut self) -> bool {
        self.about = true;
        true
    }

    pub(crate) fn about_key(&mut self, _: Key) -> bool {
        self.about = false;
        true
    }

    pub(crate) fn draw_about(&self, s: &mut Screen) {
        let lines = about_lines();
        let w = lines.iter().map(|(l, _)| cell_width(l) + 6).max().unwrap_or(0).min(s.cols);
        let h = (lines.len() as i32 + 2).min(s.rows);
        let (x0, y0) = (((s.cols - w) / 2).max(0), ((s.rows - h) / 2).max(0));
        s.fill(x0, y0, w, h, ' ', Color::LT_GRAY, Color::BLUE);
        for (i, (text, fg)) in lines.iter().enumerate() {
            let y = y0 + 1 + i as i32;
            if y >= y0 + h {
                break;
            }
            if text.is_empty() {
                continue;
            }
            // 前兩列置中,其餘靠左對齊比較好讀。
            let x = if i > 1 { x0 + 3 } else { x0 + (w - cell_width(text)) / 2 };
            s.print(x.max(x0), y, text, *fg, Color::BLUE);
        }
    }
}
