//! Alt-P 預視窗格:畫面底部顯示游標所在檔案的開頭(對應 Go 版 `preview.go`)。

use std::io::Read;

use wincv_cell::{Color, Screen};
use wincv_i18n::t;
use wincv_textenc::Enc;

use crate::actions::printable;
use crate::App;

/// 預視窗格的高度。
pub const PREVIEW_ROWS: i32 = 8;
/// 預視最多讀多少位元組。只看開頭,大檔案不必整個讀進來。
pub const MAX_PREVIEW_BYTES: u64 = 64 << 10;

#[derive(Clone)]
pub(crate) struct Preview {
    key: String,
    lines: Option<Vec<String>>,
    enc: Enc,
    err: String,
}

impl Default for Preview {
    fn default() -> Preview {
        Preview { key: String::new(), lines: None, enc: Enc::Unknown, err: String::new() }
    }
}

impl App {
    pub(crate) fn toggle_preview(&mut self) -> bool {
        self.show_preview = !self.show_preview;
        self.browser.reserve_bottom = if self.show_preview { PREVIEW_ROWS } else { 0 };
        true
    }

    fn preview_for(&mut self, cols: i32) -> Preview {
        let Some(e) = self.browser.current() else { return Preview::default() };
        let key = format!("{}\0{}", self.browser.dir.to_string_lossy(), e.name);
        if self.prev.key == key && self.prev.lines.is_some() {
            return self.prev.clone();
        }
        let mut p = Preview { key: key.clone(), ..Default::default() };
        if e.up {
            p.err = t("上一層");
        } else if e.is_dir {
            p.err = t("<目錄>");
        } else {
            let name = e.name.clone();
            p = self.read_preview(key, &name, cols);
        }
        self.prev = p.clone();
        p
    }

    fn read_preview(&self, key: String, name: &str, cols: i32) -> Preview {
        let mut p = Preview { key, ..Default::default() };
        let data = match self.browser.fs.open(&self.join_dir(name)) {
            Err(e) => {
                p.err = e.to_string();
                return p;
            }
            Ok(mut rc) => {
                let mut d = Vec::new();
                if let Err(e) = (&mut rc).take(MAX_PREVIEW_BYTES).read_to_end(&mut d) {
                    p.err = e.to_string();
                    return p;
                }
                d
            }
        };
        if data.is_empty() {
            p.err = t("(空檔案)");
            return p;
        }
        if wincv_imgfmt::detect_format(name).is_some() {
            if let Ok((img, kind)) = wincv_imgfmt::decode(name, &data) {
                p.err = format!("{kind}  {} x {}", img.w, img.h);
                return p;
            }
        }
        p.enc = wincv_textenc::detect(&data);
        if p.enc == Enc::Binary {
            p.lines = Some(hex_lines(&data, PREVIEW_ROWS as usize));
            return p;
        }
        let text = wincv_textenc::decode(&data, p.enc).replace("\r\n", "\n");
        let lines = text.split('\n').take(PREVIEW_ROWS as usize).map(|ln| printable(ln.trim_end_matches('\r'), cols)).collect();
        p.lines = Some(lines);
        p
    }

    pub(crate) fn draw_preview(&mut self, s: &mut Screen) {
        let y0 = s.rows - PREVIEW_ROWS;
        if y0 < 2 {
            return;
        }
        s.fill(0, y0, s.cols, PREVIEW_ROWS, ' ', Color::LT_GRAY, Color::BLACK);
        let p = self.preview_for(s.cols);
        if !p.err.is_empty() {
            s.print(1, y0, &p.err, Color::GRAY, Color::BLACK);
            return;
        }
        let fg = if p.enc == Enc::Binary { Color::GRAY } else { Color::LT_GRAY };
        for (i, ln) in p.lines.iter().flatten().take(PREVIEW_ROWS as usize).enumerate() {
            s.print(0, y0 + i as i32, ln, fg, Color::BLACK);
        }
    }
}

/// 二進位檔的開頭排成 16 進位列。
///
/// 字元欄把 0x80 以上的位元組原樣放進去,再照 Go 的規則解成字串(合法的 UTF-8 序列成為
/// 一個字,壞的位元組一個換一個 U+FFFD),畫出來與 Go 版相同。
fn hex_lines(data: &[u8], rows: usize) -> Vec<String> {
    const PER: usize = 16;
    let mut out = Vec::new();
    for (n, chunk) in data.chunks(PER).enumerate() {
        if out.len() >= rows {
            break;
        }
        let mut b = format!("{:08X}  ", n * PER).into_bytes();
        for j in 0..PER {
            match chunk.get(j) {
                Some(c) => b.extend_from_slice(format!("{c:02X} ").as_bytes()),
                None => b.extend_from_slice(b"   "),
            }
            if j == 7 {
                b.push(b' ');
            }
        }
        b.push(b' ');
        b.extend(chunk.iter().map(|&c| if c < 0x20 || c == 0x7F { b'.' } else { c }));
        out.push(wincv_textenc::utf8_lossy_go(&b));
    }
    out
}
