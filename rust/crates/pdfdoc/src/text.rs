//! 收字的 hayro `Device`:每個字形的 Unicode、原點、字級與前進量。
//!
//! 欄位意義與 Go 版 `pdf.Text` 相同:座標是使用者空間(基底變換為單位矩陣,
//! 不扣 MediaBox 原點),`size` 是字的縱軸在使用者空間的長度,`w` 是這個字的
//! 前進量在橫軸上的投影。

use hayro::hayro_interpret::font::GlyphRun;
use hayro::hayro_interpret::hayro_cmap::BfString;
use hayro::hayro_interpret::{BlendMode, ClipPath, Device, DrawMode, DrawProps, Image, ImageDrawProps, SoftMask};
use hayro::kurbo::{Affine, BezPath, Point, Vec2};

use crate::Text;

/// 一頁最多取幾個字。上限是為了「用一整頁文字畫出一張圖」的檔案,
/// 那種頁面有幾十萬個字,排出來不是文章。與 Go 版 `pdf.MaxTexts` 相同。
pub const MAX_TEXTS: usize = 300_000;

/// hayro 的字形變換以 1000 為一個字寬(`UNITS_PER_EM`),換回字級要乘回來。
const UNITS_PER_EM: f64 = 1000.0;

#[derive(Default)]
pub(crate) struct TextDevice {
    pub out: Vec<Text>,
    /// 上一次填色字串的指紋。「填色加描邊」(Tr 2、6)的同一串字 hayro 會
    /// 送兩次,Go 版的解譯器只收一次,這裡靠指紋把第二次濾掉。
    last_fill: Option<RunKey>,
}

#[derive(PartialEq, Clone, Copy)]
struct RunKey {
    ptr: usize,
    len: usize,
    first: [f64; 6],
    ctm: [f64; 6],
}

impl TextDevice {
    fn key(run: &GlyphRun<'_, '_>, props: &DrawProps<'_>) -> RunKey {
        let gs = run.glyphs();
        RunKey {
            ptr: gs.as_ptr() as usize,
            len: gs.len(),
            first: gs.first().map_or([0.0; 6], |g| g.transform().as_coeffs()),
            ctm: props.transform.as_coeffs(),
        }
    }
}

fn bf_text(b: BfString) -> String {
    match b {
        BfString::Char(c) => c.to_string(),
        BfString::String(s) => s,
    }
}

impl<'a> Device<'a> for TextDevice {
    fn draw_path(&mut self, _: &BezPath, _: DrawProps<'a>, _: &DrawMode) {}
    fn push_clip_path(&mut self, _: &ClipPath) {}
    fn push_transparency_group(&mut self, _: f32, _: Option<SoftMask<'a>>, _: BlendMode) {}
    fn draw_image(&mut self, _: Image<'a, '_>, _: ImageDrawProps<'a>) {}
    fn pop_clip(&mut self) {}
    fn pop_transparency_group(&mut self) {}

    fn draw_glyph_run(&mut self, run: &GlyphRun<'_, 'a>, props: DrawProps<'a>, mode: &DrawMode) {
        let key = Self::key(run, &props);
        match mode {
            DrawMode::Stroke(_) => {
                if self.last_fill.take() == Some(key) {
                    return;
                }
            }
            DrawMode::Fill(_) => self.last_fill = Some(key),
            _ => self.last_fill = None,
        }

        let glyphs = run.glyphs();
        // 先算每個字在使用者空間的原點,下一個字的原點拿來校正前進量。
        let mats: Vec<Affine> = glyphs.iter().map(|g| props.transform * g.transform()).collect();
        let origins: Vec<Point> = mats.iter().map(|m| *m * Point::ZERO).collect();
        for (i, g) in glyphs.iter().enumerate() {
            if self.out.len() >= MAX_TEXTS {
                return;
            }
            let Some(u) = g.as_unicode() else { continue };
            let s = bf_text(u);
            if s.is_empty() {
                continue;
            }
            let m = mats[i];
            let [a, _, c, d, _, _] = m.as_coeffs();
            let size = Vec2::new(c, d).hypot() * UNITS_PER_EM;
            // 字型的前進量(千分之一字寬)投影到橫軸。hayro 沒有把 Tc、Tw 交出來,
            // 所以同一串裡有下一個字時,改用兩個原點的實際距離:差距小於字級的
            // 0.1 倍時當作是字距(Tc)或微調,大的差距多半是 TJ 裡的詞間位移,
            // 那要留給排列的那一層判斷成空格,不能算進這個字的寬度。
            let adv = match &**g {
                hayro::hayro_interpret::font::Glyph::Outline(o) => o.advance_width(),
                hayro::hayro_interpret::font::Glyph::Type3(t) => t.advance_width(),
            };
            let mut w = adv.map_or(0.0, |aw| (a * aw as f64).abs());
            if let Some(next) = origins.get(i + 1) {
                let delta = (next.x - origins[i].x).abs();
                let blank = s.chars().all(char::is_whitespace);
                if (delta - w).abs() < size * 0.1 || (blank && delta > w) {
                    w = delta;
                }
            }
            let o = origins[i];
            if !(o.x.is_finite() && o.y.is_finite() && w.is_finite() && size.is_finite()) {
                continue;
            }
            self.out.push(Text { x: o.x, y: o.y, w, size, s });
        }
    }
}
