//! 縮圖列表(對應 Go 版 `internal/thumbs`)。
//!
//! 整個網格畫進**一張**合成圖,再交給 [`Overlay`] 一次貼上,而不是每張縮圖各自疊
//! 一層。格點層只負責檔名與狀態列。
//!
//! 解碼可以在背景做:[`decode_item`] 不碰 Model,呼叫端在別的執行緒跑完再用
//! [`Model::set_decoded`] 放回來。已解好的先畫,還沒好的留白。

use std::sync::Arc;

use wincv_cell::{is_wide, Color, Screen};
use wincv_i18n::t;
use wincv_render::{Image, Overlay};

/// 一張縮圖。
#[derive(Clone, Debug, Default)]
pub struct Item {
    pub name: String,
    /// 解好之前是 None。
    pub img: Option<Arc<Image>>,
    pub err: Option<String>,
    /// 原圖尺寸。
    pub w: u32,
    pub h: u32,
}

/// 配色。
#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub bg: Color,
    pub name_fg: Color,
    pub sel_fg: Color,
    pub sel_bg: Color,
    pub status_fg: Color,
    pub status_bg: Color,
}

impl Default for Theme {
    fn default() -> Theme {
        Theme {
            bg: Color::BLACK,
            name_fg: Color::LT_GRAY,
            sel_fg: Color::BLACK,
            sel_bg: Color::LT_GRAY,
            status_fg: Color::BLACK,
            status_bg: Color::LT_GRAY,
        }
    }
}

/// 縮圖列表的狀態。
pub struct Model {
    pub items: Vec<Item>,
    pub cursor: usize,
    /// 畫面最上面那一列是第幾列縮圖。
    pub top: usize,
    /// 一格縮圖佔幾個字格,含底下的檔名那一列。
    pub cell_cols: i32,
    pub cell_rows: i32,
    pub theme: Theme,
    /// 已經送去解的(或解好的)。
    pub requested: Vec<bool>,
}

/// 解一張縮圖。`load` 取檔案內容:縮圖列表不該知道檔案從哪來(真實目錄或壓縮檔內
/// 都可能)。可以在別的執行緒呼叫。
pub fn decode_item(name: &str, load: &dyn Fn(&str) -> Result<Vec<u8>, String>) -> Item {
    let res = load(name).and_then(|d| wincv_imgfmt::decode(name, &d).map(|(i, _)| i));
    match res {
        Ok(img) => Item { name: name.into(), w: img.w, h: img.h, img: Some(Arc::new(img)), err: None },
        Err(e) => Item { name: name.into(), err: Some(e), ..Default::default() },
    }
}

impl Model {
    /// 建一個縮圖列表。names 應該只含圖檔。
    pub fn new(names: &[String]) -> Model {
        Model {
            items: names.iter().map(|n| Item { name: n.clone(), ..Default::default() }).collect(),
            cursor: 0,
            top: 0,
            cell_cols: 14,
            cell_rows: 8,
            theme: Theme::default(),
            requested: vec![false; names.len()],
        }
    }

    /// 放回背景解好的一張。
    pub fn set_decoded(&mut self, i: usize, it: Item) {
        if let Some(slot) = self.items.get_mut(i) {
            *slot = it;
        }
    }

    /// 目前畫面上看得到、還沒送去解的那些(呼叫端拿去背景解)。標成已送出。
    pub fn take_visible(&mut self, cols: i32, rows: i32) -> Vec<usize> {
        let per_row = self.per_row(cols);
        if per_row == 0 {
            return Vec::new();
        }
        let vis_rows = (rows / self.cell_rows).max(0) as usize;
        let first = self.top * per_row;
        let last = (first + vis_rows * per_row + per_row).min(self.items.len());
        (first..last).filter(|&i| !std::mem::replace(&mut self.requested[i], true)).collect()
    }

    /// 同一個執行緒裡直接解畫面上看得到的那些。
    pub fn decode_visible(&mut self, cols: i32, rows: i32, load: &dyn Fn(&str) -> Result<Vec<u8>, String>) {
        for i in self.take_visible(cols, rows) {
            let it = decode_item(&self.items[i].name, load);
            self.set_decoded(i, it);
        }
    }

    fn per_row(&self, cols: i32) -> usize {
        (cols / self.cell_cols).max(0) as usize
    }

    fn clamp(&mut self, cols: i32, rows: i32) {
        self.cursor = self.cursor.min(self.items.len().saturating_sub(1));
        let per_row = self.per_row(cols);
        if per_row == 0 {
            return;
        }
        let vis_rows = ((rows / self.cell_rows).max(1)) as usize;
        let row = self.cursor / per_row;
        if row < self.top {
            self.top = row;
        }
        if row >= self.top + vis_rows {
            self.top = row + 1 - vis_rows;
        }
    }

    /// 依格數移動游標。dx 是左右,dy 是上下(一次一整列)。
    pub fn move_by(&mut self, dx: i32, dy: i32, cols: i32, rows: i32) {
        let per_row = self.per_row(cols).max(1) as i64;
        let c = (self.cursor as i64 + dx as i64 + dy as i64 * per_row).max(0);
        self.cursor = c as usize;
        self.clamp(cols, rows);
    }

    /// 游標所在的那一張。
    pub fn current(&self) -> Option<&Item> {
        self.items.get(self.cursor)
    }

    /// 畫檔名與狀態列,回傳縮圖要疊在哪。
    pub fn draw(&mut self, s: &mut Screen, cell_w: i32, cell_h: i32) -> Option<Overlay> {
        let th = self.theme;
        s.clear(th.name_fg, th.bg);
        let rows = (s.rows - 1).max(0);
        self.clamp(s.cols, rows);
        let per_row = self.per_row(s.cols);
        if per_row == 0 {
            self.draw_status(s);
            return None;
        }
        let vis_rows = (rows / self.cell_rows).max(0) as usize;
        // 合成圖:一格縮圖的圖片區是 cell_rows-1 列高(最後一列放檔名)。
        let (img_w, img_h) = (s.cols * cell_w, rows * cell_h);
        let mut canvas = Image::new(img_w.max(0) as u32, img_h.max(0) as u32);
        for r in 0..vis_rows {
            for c in 0..per_row {
                let idx = (self.top + r) * per_row + c;
                let Some(it) = self.items.get(idx) else { break };
                let (cx, cy) = (c as i32 * self.cell_cols, r as i32 * self.cell_rows);
                if let Some(img) = &it.img {
                    let rect = (cx * cell_w + 2, cy * cell_h + 2, (cx + self.cell_cols) * cell_w - 2, (cy + self.cell_rows - 1) * cell_h - 2);
                    blit_fit(&mut canvas, img, rect);
                }
                // 檔名那一列
                let ny = cy + self.cell_rows - 1;
                let (mut fg, mut bg) = (th.name_fg, th.bg);
                if idx == self.cursor {
                    (fg, bg) = (th.sel_fg, th.sel_bg);
                    s.fill(cx, cy, self.cell_cols, self.cell_rows, ' ', fg, bg);
                }
                s.fill(cx, ny, self.cell_cols, 1, ' ', fg, bg);
                s.print(cx, ny, truncate(&it.name, self.cell_cols), fg, bg);
                if it.img.is_none() && it.err.is_some() {
                    s.print(cx + 1, cy + 1, &t("解不開"), Color::LT_RED, bg);
                }
            }
        }
        self.draw_status(s);
        Some(Overlay { img: Arc::new(canvas), x: 0, y: 0, w: img_w, h: img_h, fit: false, pan_x: 0, pan_y: 0, zoom: 1.0 })
    }

    fn draw_status(&self, s: &mut Screen) {
        let th = self.theme;
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', th.status_fg, th.status_bg);
        let left = match self.current() {
            Some(it) if it.w > 0 => format!("{}  {}x{}", it.name, it.w, it.h),
            Some(it) => it.name.clone(),
            None => t("縮圖列表"),
        };
        s.print(0, y, &left, th.status_fg, th.status_bg);
        let right = format!("{} / {}", self.cursor + 1, self.items.len());
        let x = s.cols - right.chars().map(|r| if is_wide(r) { 2 } else { 1 }).sum::<i32>();
        if x >= 0 {
            s.print(x, y, &right, th.status_fg, th.status_bg);
        }
    }
}

/// 把一張圖等比例縮放後置中畫進 dst 的矩形(x0, y0, x1, y1)。最近鄰取樣。寫進去的
/// 是預乘過的顏色、alpha 255(與 Go 版相同),全透明的像素不寫,留給底下的格點。
fn blit_fit(dst: &mut Image, src: &Image, (x0, y0, x1, y1): (i32, i32, i32, i32)) {
    let (sw, sh) = (src.w as i64, src.h as i64);
    let (rw, rh) = ((x1 - x0) as i64, (y1 - y0) as i64);
    if sw <= 0 || sh <= 0 || rw <= 0 || rh <= 0 {
        return;
    }
    let (num, den) = if rh * sw < rw * sh { (rh, sh) } else { (rw, sw) };
    let (ow, oh) = (sw * num / den, sh * num / den);
    if ow <= 0 || oh <= 0 {
        return;
    }
    let (ox, oy) = (x0 as i64 + (rw - ow) / 2, y0 as i64 + (rh - oh) / 2);
    for y in 0..oh {
        let dy = oy + y;
        if dy < 0 || dy >= dst.h as i64 {
            continue;
        }
        let sy = y * sh / oh;
        for x in 0..ow {
            let dx = ox + x;
            if dx < 0 || dx >= dst.w as i64 {
                continue;
            }
            let [r, g, b, a] = src.at((x * sw / ow) as u32, sy as u32);
            if a == 0 {
                continue;
            }
            let pre = |c: u8| ((c as u32 * 0x101 * (a as u32 * 0x101) / 0xFFFF) >> 8) as u8;
            dst.set(dx as u32, dy as u32, [pre(r), pre(g), pre(b), 0xFF]);
        }
    }
}

/// 截到 w 格寬,全形字不切一半。
fn truncate(s: &str, w: i32) -> &str {
    let mut n = 0;
    for (i, r) in s.char_indices() {
        let cw = if is_wide(r) { 2 } else { 1 };
        if n + cw > w {
            return &s[..i];
        }
        n += cw;
    }
    s
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn move_and_scroll() {
        let names: Vec<String> = (0..30).map(|i| format!("p{i:02}.png")).collect();
        let mut m = Model::new(&names);
        // 80 欄 → 每列 5 張;24 列內容 → 3 列縮圖
        m.move_by(0, 4, 80, 24);
        assert_eq!((m.cursor, m.top), (20, 2));
        m.move_by(-100, 0, 80, 24);
        assert_eq!((m.cursor, m.top), (0, 0));
        assert_eq!(m.take_visible(80, 24), (0..20).collect::<Vec<_>>());
        assert!(m.take_visible(80, 24).is_empty(), "送出過的不再送");
    }

    #[test]
    fn decode_and_draw() {
        let mut m = Model::new(&["a.pgm".into(), "bad.pgm".into()]);
        let load = |n: &str| -> Result<Vec<u8>, String> {
            if n == "a.pgm" { Ok(b"P5\n2 2\n255\n\x00\xff\xff\x00".to_vec()) } else { Ok(b"junk".to_vec()) }
        };
        m.decode_visible(80, 24, &load);
        assert_eq!((m.items[0].w, m.items[0].h), (2, 2));
        assert!(m.items[1].err.is_some());
        let mut s = Screen::new(80, 25);
        let ov = m.draw(&mut s, 8, 16).unwrap();
        assert_eq!((ov.w, ov.h), (640, 384));
        // 第一格的圖片區被畫上了,其他地方是全透明
        assert_eq!(ov.img.at(0, 0)[3], 0);
        assert_eq!(ov.img.at(56, 56)[3], 255);
    }

    #[test]
    fn truncate_keeps_whole_wide_chars() {
        assert_eq!(truncate("中文檔名.png", 5), "中文");
        assert_eq!(truncate("abc", 5), "abc");
    }
}
