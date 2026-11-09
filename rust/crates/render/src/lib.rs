//! 純 CPU 光柵器:cell 畫面 → RGBA 像素(對應 Go 版 `internal/render`)。
//!
//! 不依賴任何視窗或繪圖後端,所以 headless 畫出來的與視窗裡看到的是同一份像素。

use wincv_cell::{from_cp437, Cell, Color, Screen, NUM_COLORS};
use wincv_fnt::Glyph;

/// RGBA 顏色。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Rgba(pub u8, pub u8, pub u8, pub u8);

const fn c(r: u8, g: u8, b: u8) -> Rgba {
    Rgba(r, g, b, 0xFF)
}

/// 43 個具名顏色加 LTGRAY 第二定義的 RGB,取自 WINCV.IMG 本體。
/// 每個顏色 word 的 body 第 8–10 個位元組就是 R、G、B。
pub const DEFAULT_PALETTE: [Rgba; NUM_COLORS] = [
    c(0x00, 0x00, 0x00), c(0x40, 0x40, 0x40), c(0x80, 0x00, 0x00), c(0xFF, 0x00, 0x00),
    c(0x00, 0x80, 0x00), c(0x00, 0xFF, 0x00), c(0x00, 0x00, 0x80), c(0x00, 0x00, 0xFF),
    c(0x80, 0x80, 0x00), c(0xA0, 0xA0, 0x00), c(0xFF, 0xFF, 0x00), c(0x80, 0x00, 0x80),
    c(0xFF, 0x00, 0xFF), c(0x00, 0x80, 0x80), c(0x00, 0xFF, 0xFF), c(0x80, 0x80, 0x80),
    c(0xFF, 0xFF, 0xFF), c(0xC0, 0xC0, 0xC0), c(0x80, 0x80, 0xFF), c(0xC0, 0xC0, 0xFF),
    c(0xFF, 0x80, 0x40), c(0xFF, 0xC0, 0x80), c(0xFF, 0xFF, 0xD7), c(0xAA, 0xDC, 0xBE),
    c(0x00, 0x66, 0x00), c(0xE1, 0xE1, 0xE1), c(0x00, 0xC0, 0x00), c(0x00, 0xD6, 0xD6),
    c(0xC0, 0x00, 0xC0), c(0x14, 0xBE, 0x00), c(0x00, 0xF0, 0x00), c(0x1E, 0xBE, 0xBE),
    c(0x14, 0xDC, 0xDC), c(0xAF, 0x46, 0x00), c(0xB9, 0xB9, 0xB9), c(0xC8, 0xC8, 0xC8),
    c(0xAA, 0xAA, 0xAA), c(0xAA, 0xAA, 0xAA), c(0xAA, 0xFF, 0xE6), c(0x00, 0xB4, 0xB4),
    c(0x1E, 0xC8, 0x00), c(0x00, 0xB4, 0x00), c(0x96, 0x96, 0x96), c(0xC5, 0xC5, 0xC5),
];

/// 半形字模來源:原版的 cvga 系列 .FON,或系統字型現場產的。
pub trait HalfSource {
    fn glyph(&self, code: u8) -> Option<Glyph>;
    fn size(&self) -> (i32, i32);
}

/// 全形字模來源。
pub trait CjkSource {
    fn glyph(&self, r: char) -> Option<Glyph>;
}

impl HalfSource for wincv_fnt::Font {
    fn glyph(&self, code: u8) -> Option<Glyph> {
        wincv_fnt::Font::glyph(self, code)
    }
    fn size(&self) -> (i32, i32) {
        wincv_fnt::Font::size(self)
    }
}

/// 字模下方多留一列。原版的列距實測比字高多 1。
pub const LINE_GAP: i32 = 1;

/// 依序問幾個來源,第一個有字的贏。
pub struct Sources(pub Vec<Box<dyn CjkSource>>);

impl CjkSource for Sources {
    fn glyph(&self, r: char) -> Option<Glyph> {
        self.0.iter().find_map(|s| s.glyph(r))
    }
}

/// 把全形字模最近鄰縮放到另一個格子大小。
///
/// 只差一列行距時直接貼上(下方留白),不縮放 —— 15 點字放進 16 高的
/// 格子,縮放會讓某一列重複,字形變形。
pub struct ScaledCjk<S> {
    src: S,
    w: i32,
    h: i32,
}

/// 包成 w×h 的來源。字模本來就是這個大小時原樣轉交,
/// 與 Go 版 `ScaleCJK` 在相同尺寸時不包裝的效果一樣。
pub fn scale_cjk<S: CjkSource>(src: S, w: i32, h: i32) -> ScaledCjk<S> {
    ScaledCjk { src, w, h }
}

impl<S: CjkSource> CjkSource for ScaledCjk<S> {
    fn glyph(&self, r: char) -> Option<Glyph> {
        let g = self.src.glyph(r)?;
        if g.w == self.w && g.h == self.h {
            return Some(g);
        }
        let mut out = Glyph::blank(self.w, self.h);
        if g.w == self.w && self.h > g.h && self.h - g.h <= LINE_GAP {
            out.bits[..g.bits.len()].copy_from_slice(&g.bits);
            return Some(out);
        }
        for y in 0..self.h {
            let sy = y * g.h / self.h;
            for x in 0..self.w {
                let sx = x * g.w / self.w;
                out.bits[(y * self.w + x) as usize] = g.at(sx, sy);
            }
        }
        Some(out)
    }
}

impl CjkSource for Box<dyn CjkSource> {
    fn glyph(&self, r: char) -> Option<Glyph> {
        (**self).glyph(r)
    }
}

/// cell 畫面 → RGBA 緩衝區。
pub struct Rasterizer {
    pub half: Box<dyn HalfSource>,
    pub cjk: Option<Box<dyn CjkSource>>,
    pub palette: [Rgba; NUM_COLORS],
    /// 主來源沒有的字問它。
    pub fallback: Option<Box<dyn CjkSource>>,
    /// 缺字畫一個框(驗收用),否則留白。
    pub missing_mark: bool,
    pub cell_w: i32,
    pub cell_h: i32,
    /// 分隔線的亮線與陰影。
    pub rule_hi: Color,
    pub rule_shadow: Color,
    buf: Vec<u8>,
    buf_w: i32,
    buf_h: i32,
}

fn clamp_color(c: Color) -> usize {
    if (c.0 as usize) < NUM_COLORS { c.0 as usize } else { 0 }
}

fn blend(bg: u8, fg: u8, a: u8) -> u8 {
    ((bg as i32 * (255 - a as i32) + fg as i32 * a as i32 + 127) / 255) as u8
}

impl Rasterizer {
    pub fn new(half: Box<dyn HalfSource>, cjk: Option<Box<dyn CjkSource>>) -> Rasterizer {
        let (hw, hh) = half.size();
        Rasterizer {
            half,
            cjk,
            palette: DEFAULT_PALETTE,
            fallback: None,
            missing_mark: false,
            cell_w: hw,
            cell_h: hh + LINE_GAP,
            rule_hi: Color::WHITE,
            rule_shadow: Color::LT_GRAY2,
            buf: Vec::new(),
            buf_w: 0,
            buf_h: 0,
        }
    }

    /// cols×rows 格的像素大小。
    pub fn size(&self, cols: i32, rows: i32) -> (i32, i32) {
        (cols * self.cell_w, rows * self.cell_h)
    }

    /// 畫整個畫面,回傳 RGBA 緩衝區(寬 × 高 × 4)。緩衝區下一次呼叫會重用。
    pub fn draw(&mut self, s: &Screen) -> &[u8] {
        self.draw_with(s, &[])
    }

    /// 畫格點,再把 overlay 依序疊上去。
    ///
    /// 收多個而不是一個:markdown 一頁可以有好幾張圖,每張各佔自己的格位。
    pub fn draw_with(&mut self, s: &Screen, ovs: &[&Overlay]) -> &[u8] {
        let (w, h) = self.size(s.cols, s.rows);
        if self.buf_w != w || self.buf_h != h {
            self.buf = vec![0; (w * h * 4).max(0) as usize];
            self.buf_w = w;
            self.buf_h = h;
        }
        for y in 0..s.rows {
            for x in 0..s.cols {
                self.fill_bg(s, x, y);
            }
        }
        for y in 0..s.rows {
            for x in 0..s.cols {
                self.draw_glyph(s, x, y);
            }
        }
        for ov in ovs {
            self.blit(ov);
        }
        &self.buf
    }

    /// 把 overlay 的圖片畫進緩衝區。用最近鄰取樣:這是點陣風格的介面,平滑縮放
    /// 會跟旁邊的點陣字打架。
    fn blit(&mut self, ov: &Overlay) {
        let img = &ov.img;
        let (x0, y0) = (ov.x.max(0), ov.y.max(0));
        let (x1, y1) = ((ov.x + ov.w).min(self.buf_w), (ov.y + ov.h).min(self.buf_h));
        if x0 >= x1 || y0 >= y1 {
            return;
        }
        let (sw, sh) = (img.w as i64, img.h as i64);
        if sw == 0 || sh == 0 {
            return;
        }
        if ov.fit {
            // 等比例縮放後置中。
            let (rw, rh) = (ov.w as i64, ov.h as i64);
            let (num, den) = if rh * sw < rw * sh { (rh, sh) } else { (rw, sw) };
            let (out_w, out_h) = (sw * num / den, sh * num / den);
            let off_x = ov.x as i64 + (rw - out_w) / 2;
            let off_y = ov.y as i64 + (rh - out_h) / 2;
            for y in 0..out_h {
                let dy = off_y + y;
                if dy < y0 as i64 || dy >= y1 as i64 {
                    continue;
                }
                let sy = y * sh / out_h;
                for x in 0..out_w {
                    let dx = off_x + x;
                    if dx < x0 as i64 || dx >= x1 as i64 {
                        continue;
                    }
                    self.set_pix(dx as i32, dy as i32, img.at((x * sw / out_w) as u32, sy as u32));
                }
            }
            return;
        }
        let z = if ov.zoom <= 0.0 { 1.0 } else { ov.zoom };
        for dy in y0..y1 {
            let sy = ((dy - ov.y) as f64 / z) as i64 + ov.pan_y as i64;
            if sy < 0 || sy >= sh {
                continue;
            }
            for dx in x0..x1 {
                let sx = ((dx - ov.x) as f64 / z) as i64 + ov.pan_x as i64;
                if sx < 0 || sx >= sw {
                    continue;
                }
                self.set_pix(dx, dy, img.at(sx as u32, sy as u32));
            }
        }
    }

    /// 把一個像素疊上去,尊重 alpha。
    ///
    /// 透明度不能忽略:.ICO 的 AND 遮罩、PNG 與 GIF 的透明色都會產生 alpha = 0 的
    /// 像素,直接寫 RGB 會把「應該看不見」的顏色畫出來。半透明的算法與 Go 版相同
    /// (16 位元預乘再疊上背景),兩邊畫出來逐像素一樣。
    fn set_pix(&mut self, x: i32, y: i32, c: [u8; 4]) {
        let o = self.offset(x, y);
        if o < 0 || o + 3 >= self.buf.len() as i64 {
            return;
        }
        let o = o as usize;
        let a = c[3] as u32 * 0x101;
        match c[3] {
            0 => return, // 全透明,保留底下的格點畫面
            255 => self.buf[o..o + 3].copy_from_slice(&c[..3]),
            _ => {
                let inv = 0xFFFF - a;
                for k in 0..3 {
                    let pre = c[k] as u32 * 0x101 * a / 0xFFFF;
                    self.buf[o + k] = ((pre + self.buf[o + k] as u32 * 257 * inv / 0xFFFF) >> 8) as u8;
                }
            }
        }
        self.buf[o + 3] = 0xFF;
    }

    fn offset(&self, x: i32, y: i32) -> i64 {
        (y as i64 * self.buf_w as i64 + x as i64) * 4
    }

    fn put(&mut self, o: i64, col: Rgba) {
        if o < 0 || o + 3 >= self.buf.len() as i64 {
            return;
        }
        let o = o as usize;
        self.buf[o] = col.0;
        self.buf[o + 1] = col.1;
        self.buf[o + 2] = col.2;
        self.buf[o + 3] = 0xFF;
    }

    fn fill_bg(&mut self, s: &Screen, cx: i32, cy: i32) {
        let Some(c) = s.at(cx, cy) else { return };
        let (px, py) = (cx * self.cell_w, cy * self.cell_h);
        let bg = self.palette[clamp_color(c.bg)];
        for y in 0..self.cell_h {
            let row = self.offset(px, py + y);
            for x in 0..self.cell_w {
                self.put(row + x as i64 * 4, bg);
            }
        }
    }

    fn hline(&mut self, px: i32, py: i32, w: i32, col: Rgba) {
        if py < 0 || py >= self.buf_h {
            return;
        }
        let row = self.offset(px, py);
        for x in 0..w {
            self.put(row + x as i64 * 4, col);
        }
    }

    fn draw_glyph(&mut self, s: &Screen, cx: i32, cy: i32) {
        let Some(&c) = s.at(cx, cy) else { return };
        if c.cont {
            return; // 全形字在左半格一次畫完
        }
        let (px, py) = (cx * self.cell_w, cy * self.cell_h);
        if c.rule {
            let w = if c.wide { self.cell_w * 2 } else { self.cell_w };
            self.hline(px, py, w, self.palette[clamp_color(self.rule_hi)]);
            self.hline(px, py + 1, w, self.palette[clamp_color(self.rule_shadow)]);
        }
        if c.under && !c.wide {
            self.hline(px, py + self.cell_h - 1, self.cell_w, self.palette[clamp_color(c.fg)]);
        }
        let mut g = if c.wide {
            self.cjk.as_ref().and_then(|s| s.glyph(c.ch))
        } else {
            from_cp437(c.ch).and_then(|b| self.half.glyph(b))
        };
        if g.is_none() {
            g = self.fallback.as_ref().and_then(|f| f.glyph(c.ch));
        }
        let Some(g) = g else {
            if self.missing_mark && c.ch != ' ' && c.ch != '\0' {
                self.draw_missing(cx, cy, &c);
            }
            return;
        };
        let fg = self.palette[clamp_color(c.fg)];
        for y in 0..g.h.min(self.cell_h) {
            let row = self.offset(px, py + y);
            for x in 0..g.w {
                let a = g.cov(x, y);
                if a == 0 {
                    continue;
                }
                let o = row + x as i64 * 4;
                if o < 0 || o + 3 >= self.buf.len() as i64 {
                    continue;
                }
                if a == 255 {
                    self.put(o, fg);
                } else {
                    let o = o as usize;
                    let p = &mut self.buf[o..o + 4];
                    p[0] = blend(p[0], fg.0, a);
                    p[1] = blend(p[1], fg.1, a);
                    p[2] = blend(p[2], fg.2, a);
                    p[3] = 0xFF;
                }
            }
        }
    }

    /// 缺字框:比格子內縮一點的空心矩形,用前景色畫。
    fn draw_missing(&mut self, cx: i32, cy: i32, c: &Cell) {
        let w = if c.wide { self.cell_w * 2 } else { self.cell_w };
        let (px, py) = (cx * self.cell_w, cy * self.cell_h);
        let fg = self.palette[clamp_color(c.fg)];
        let (top, bot, left, right) = (2, self.cell_h - 3, 1, w - 2);
        let set = |me: &mut Rasterizer, x: i32, y: i32| {
            if x < 0 || y < 0 {
                return;
            }
            let o = me.offset(px + x, py + y);
            me.put(o, fg);
        };
        for x in left..=right {
            set(self, x, top);
            set(self, x, bot);
        }
        for y in top..=bot {
            set(self, left, y);
            set(self, right, y);
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    struct Box8;
    impl HalfSource for Box8 {
        fn glyph(&self, code: u8) -> Option<Glyph> {
            (code == b'#').then(|| Glyph { w: 8, h: 15, bits: vec![true; 120], alpha: None })
        }
        fn size(&self) -> (i32, i32) {
            (8, 15)
        }
    }

    #[test]
    fn cell_is_glyph_height_plus_line_gap() {
        let r = Rasterizer::new(Box::new(Box8), None);
        assert_eq!((r.cell_w, r.cell_h), (8, 16));
        assert_eq!(r.size(80, 25), (640, 400));
    }

    #[test]
    fn glyph_uses_fg_and_gap_row_keeps_bg() {
        let mut r = Rasterizer::new(Box::new(Box8), None);
        let mut s = Screen::new(1, 1);
        s.set(0, 0, '#', Color::WHITE, Color::BLUE);
        let px = r.draw(&s).to_vec();
        assert_eq!(&px[0..4], &[0xFF, 0xFF, 0xFF, 0xFF]);
        let last_row = (15 * 8) * 4;
        assert_eq!(&px[last_row..last_row + 4], &[0x00, 0x00, 0x80, 0xFF]);
    }
}

/// 一張解好的圖:RGBA,每個像素 4 個位元組,**非預乘**(alpha 不乘進顏色)。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Image {
    pub w: u32,
    pub h: u32,
    pub pix: Vec<u8>,
}

impl Image {
    pub fn new(w: u32, h: u32) -> Image {
        Image { w, h, pix: vec![0; w as usize * h as usize * 4] }
    }

    pub fn at(&self, x: u32, y: u32) -> [u8; 4] {
        let o = (y as usize * self.w as usize + x as usize) * 4;
        self.pix.get(o..o + 4).map_or([0; 4], |p| [p[0], p[1], p[2], p[3]])
    }

    pub fn set(&mut self, x: u32, y: u32, c: [u8; 4]) {
        if x < self.w && y < self.h {
            let o = (y as usize * self.w as usize + x as usize) * 4;
            self.pix[o..o + 4].copy_from_slice(&c);
        }
    }
}

/// 畫在格點之上的像素層。
///
/// 看圖模式需要它:圖片不是字元,硬塞進格點會失去解析度。格點仍然負責周邊的
/// 狀態列與訊息,只有圖片本身跳出格點。
#[derive(Clone, Debug)]
pub struct Overlay {
    pub img: std::sync::Arc<Image>,
    /// 目標矩形(像素座標)。
    pub x: i32,
    pub y: i32,
    pub w: i32,
    pub h: i32,
    /// true 時圖片等比例縮放後置中放進來;false 時依 zoom 貼上,超出的部分裁掉。
    pub fit: bool,
    /// 非 fit 模式下的平移量(來源座標)。
    pub pan_x: i32,
    pub pan_y: i32,
    /// 非 fit 模式下的放大倍率,0 或 1 表示原尺寸。
    pub zoom: f64,
}

