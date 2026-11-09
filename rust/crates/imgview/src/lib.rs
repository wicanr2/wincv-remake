//! 看圖模式(對應 Go 版 `internal/imgview`)。
//!
//! 圖片不走格點:硬塞進 8x15 的格子會失去解析度。格點只負責狀態列與訊息,圖片
//! 本身透過 [`Overlay`] 直接畫在像素層。

use std::sync::Arc;

use wincv_cell::{is_wide, Color, Screen};
use wincv_i18n::{t, tr};
use wincv_render::{Image, Overlay};

/// 配色。
#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub bg: Color,
    pub status_fg: Color,
    pub status_bg: Color,
    pub info_fg: Color,
    pub info_bg: Color,
}

impl Default for Theme {
    fn default() -> Theme {
        Theme { bg: Color::BLACK, status_fg: Color::BLACK, status_bg: Color::LT_GRAY, info_fg: Color::LT_CYAN, info_bg: Color::BLUE }
    }
}

/// 能重畫的來源(PDF 頁面)以指定倍率重新產生一張圖。
pub type Rerender = Box<dyn FnMut(f64) -> Result<Image, String>>;

/// 看圖的狀態。
pub struct Model {
    pub name: String,
    pub kind: String,
    pub img: Arc<Image>,
    /// true = 縮放到視窗;false = 依 zoom。
    pub fit: bool,
    pub pan: (i32, i32),
    /// 顯示檔案資訊(原版看圖時按 I)。
    pub info: bool,
    /// 使用者要的顯示倍率(1 = 原尺寸)。只在 fit 為 false 時有意義。
    pub zoom: f64,
    /// 目前這張 img 是用幾倍產生的。
    ///
    /// 兩個數字而不是一個:PDF 這種**能重畫**的來源放大時應該用更高的解析度重畫
    /// (那才看得到更多細節),而不是把點陣圖拉大;重畫完 img 本身就已經是放大的,
    /// 再乘一次就會放大兩倍。一般圖片沒有重畫這回事,base 永遠是 1。
    base: f64,
    /// None 表示這張圖只能點陣放大。回錯誤時退回點陣放大:放大失敗不該讓畫面停在
    /// 原地什麼都沒發生。
    pub rerender: Option<Rerender>,
    /// 原始檔案大小,顯示在狀態列。
    pub size: i64,
    pub theme: Theme,
}

/// 放大倍率的階梯。要有整數倍:整數倍時每個來源像素放大成同樣大小的方塊,
/// 點陣圖才不會有的一格有的兩格。
pub const ZOOM_STEPS: [f64; 11] = [0.25, 0.33, 0.5, 0.67, 1.0, 1.5, 2.0, 3.0, 4.0, 6.0, 8.0];

impl Model {
    /// 解一張圖。
    pub fn load(name: &str, data: &[u8]) -> Result<Model, String> {
        let (img, kind) = wincv_imgfmt::decode(name, data)?;
        Ok(Model::from_image(name, kind, img, data.len() as i64))
    }

    /// 用一張已經解好的圖建 Model。markdown 內嵌的圖已經在排版時解過了,再解一次
    /// 只是浪費,而且有些來源(壓縮檔成員)第二次未必拿得到同一份位元組。
    pub fn from_image(name: &str, kind: &str, img: Image, size: i64) -> Model {
        Model {
            name: name.into(),
            kind: kind.into(),
            img: Arc::new(img),
            fit: true,
            pan: (0, 0),
            info: false,
            zoom: 1.0,
            base: 1.0,
            rerender: None,
            size,
            theme: Theme::default(),
        }
    }

    /// 這張圖實際要放大幾倍才畫成使用者要的大小。能重畫的來源重畫過之後,img 本身
    /// 已經是放大的,這裡就會回 1。
    pub fn scale(&self) -> f64 {
        if self.zoom <= 0.0 || self.base <= 0.0 { 1.0 } else { self.zoom / self.base }
    }

    /// 沿著階梯移動 d 格(正數放大)。回傳倍率有沒有變。放大會離開「縮放到視窗」。
    pub fn zoom_by(&mut self, d: i32) -> bool {
        if self.zoom <= 0.0 {
            self.zoom = 1.0;
        }
        let i = ZOOM_STEPS.iter().rposition(|&z| z <= self.zoom + 1e-9).unwrap_or(0) as i32 + d;
        self.set_zoom(ZOOM_STEPS[i.clamp(0, ZOOM_STEPS.len() as i32 - 1) as usize])
    }

    /// 直接指定倍率。
    pub fn set_zoom(&mut self, z: f64) -> bool {
        let z = if z <= 0.0 { 1.0 } else { z };
        if !self.fit && z == self.zoom {
            return false;
        }
        let old = self.scale();
        self.fit = false;
        self.zoom = z;
        if self.base <= 0.0 {
            self.base = 1.0;
        }
        // 能重畫的來源(PDF 頁面)重畫一張。點陣放大只是把同樣的像素攤開。
        if let Some(r) = self.rerender.as_mut() {
            if let Ok(img) = r(z) {
                self.img = Arc::new(img);
                self.base = z;
            }
        }
        // 平移量跟著倍率走,不然放大之後看的會是別的地方。
        let s = self.scale();
        if old > 0.0 && s > 0.0 && s != old {
            self.pan = ((self.pan.0 as f64 * old / s) as i32, (self.pan.1 as f64 * old / s) as i32);
        }
        self.clamp_pan();
        true
    }

    /// 平移(只在非 fit 模式有意義)。
    pub fn pan_by(&mut self, dx: i32, dy: i32) {
        if self.fit {
            return;
        }
        self.pan.0 += dx;
        self.pan.1 += dy;
        self.clamp_pan();
    }

    fn clamp_pan(&mut self) {
        let (w, h) = (self.img.w as i32, self.img.h as i32);
        self.pan = (self.pan.0.clamp(-w, w), self.pan.1.clamp(-h, h));
    }

    /// 在「縮放到視窗」與「使用者指定的倍率」之間切換。
    pub fn toggle_fit(&mut self) {
        self.fit = !self.fit;
        self.pan = (0, 0);
        if !self.fit && self.zoom <= 0.0 {
            self.zoom = 1.0;
        }
    }

    /// 畫狀態列與資訊框,回傳圖片要疊在哪。cell_w / cell_h 是格子的像素尺寸,用來把
    /// 格點座標換算成像素矩形。
    pub fn draw(&self, s: &mut Screen, cell_w: i32, cell_h: i32) -> Overlay {
        s.clear(Color::LT_GRAY, self.theme.bg);
        let rows = (s.rows - 1).max(0);
        self.draw_status(s);
        if self.info {
            self.draw_info(s);
        }
        Overlay {
            img: self.img.clone(),
            x: 0,
            y: 0,
            w: s.cols * cell_w,
            h: rows * cell_h,
            fit: self.fit,
            pan_x: self.pan.0,
            pan_y: self.pan.1,
            zoom: self.scale(),
        }
    }

    fn draw_status(&self, s: &mut Screen) {
        let th = self.theme;
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', th.status_fg, th.status_bg);
        let left = format!("{}  [{}]  {}x{}", self.name, self.kind, self.img.w, self.img.h);
        s.print(0, y, &left, th.status_fg, th.status_bg);
        let mode = if self.fit {
            t("縮放")
        } else if self.zoom > 0.0 && self.zoom != 1.0 {
            format!("{}×", self.zoom)
        } else {
            "1:1".into()
        };
        let right = format!("{}  {mode}", human_size(self.size));
        let x = s.cols - width(&right);
        if x >= 0 {
            s.print(x, y, &right, th.status_fg, th.status_bg);
        }
    }

    /// 左上角的資訊框(原版看圖時按 I)。
    fn draw_info(&self, s: &mut Screen) {
        let th = self.theme;
        let lines = [
            t("檔名: ") + &self.name,
            tr!("格式: %s", self.kind.as_str()),
            tr!("尺寸: %d x %d", self.img.w, self.img.h),
            tr!("大小: %s", human_size(self.size)),
        ];
        let w = (lines.iter().map(|l| width(l)).max().unwrap_or(0) + 2).min(s.cols);
        s.fill(0, 0, w, lines.len() as i32 + 2, ' ', th.info_fg, th.info_bg);
        for (i, l) in lines.iter().enumerate() {
            s.print(1, i as i32 + 1, l, th.info_fg, th.info_bg);
        }
    }
}

/// 檔案大小的人類可讀寫法。
pub fn human_size(n: i64) -> String {
    if n >= 1 << 20 {
        format!("{:.1} MB", n as f64 / (1 << 20) as f64)
    } else if n >= 1 << 10 {
        format!("{:.1} KB", n as f64 / (1 << 10) as f64)
    } else {
        format!("{n} bytes")
    }
}

fn width(s: &str) -> i32 {
    s.chars().map(|r| if is_wide(r) { 2 } else { 1 }).sum()
}

#[cfg(test)]
mod tests {
    use super::*;

    fn solid(w: u32, h: u32) -> Image {
        Image::new(w, h)
    }

    /// zoom_by 沿階梯走,而且會離開「縮放到視窗」。
    #[test]
    fn zoom_by_steps() {
        let mut m = Model::from_image("a.png", "PNG", solid(100, 100), 0);
        assert!(m.fit);
        m.zoom_by(1);
        assert!(!m.fit);
        assert_eq!(m.zoom, 1.5);
        m.zoom_by(1);
        assert_eq!(m.zoom, 2.0);
        m.zoom_by(-2);
        assert_eq!(m.zoom, 1.0);
        // 階梯的兩端要夾住,不是繞回去。
        (0..20).for_each(|_| _ = m.zoom_by(1));
        assert_eq!(m.zoom, 8.0);
        (0..20).for_each(|_| _ = m.zoom_by(-1));
        assert_eq!(m.zoom, 0.25);
    }

    /// 沒有 rerender 的圖(一般圖檔)靠點陣放大:scale 就是使用者要的倍率。
    #[test]
    fn scale_without_rerender() {
        let mut m = Model::from_image("a.png", "PNG", solid(100, 100), 0);
        m.set_zoom(2.0);
        assert_eq!((m.scale(), m.img.w), (2.0, 100));
    }

    /// 能重畫的來源要用更高的解析度重畫,重畫完就不再點陣放大。
    #[test]
    fn rerender_replaces_image() {
        let mut m = Model::from_image("p.pdf", "PDF", solid(100, 100), 0);
        let asked = std::rc::Rc::new(std::cell::RefCell::new(Vec::new()));
        let a2 = asked.clone();
        m.rerender = Some(Box::new(move |z| {
            a2.borrow_mut().push(z);
            Ok(solid((100.0 * z) as u32, (100.0 * z) as u32))
        }));
        m.set_zoom(2.0);
        assert_eq!(*asked.borrow(), [2.0]);
        assert_eq!((m.img.w, m.scale()), (200, 1.0));
        // 再放大一階,要拿累積的倍率去重畫,不是相對值。
        m.zoom_by(1);
        assert_eq!(asked.borrow().last(), Some(&3.0));
    }

    /// 重畫失敗要退回點陣放大。
    #[test]
    fn rerender_failure_falls_back() {
        let mut m = Model::from_image("p.pdf", "PDF", solid(100, 100), 0);
        m.rerender = Some(Box::new(|_| Err("太大".into())));
        m.set_zoom(4.0);
        assert_eq!(m.scale(), 4.0);
    }

    /// 按 1 回到原尺寸。
    #[test]
    fn set_zoom_one() {
        let mut m = Model::from_image("a.png", "PNG", solid(100, 100), 0);
        m.set_zoom(4.0);
        m.set_zoom(1.0);
        assert!(m.zoom == 1.0 && !m.fit);
    }
}
