//! 定點數光柵器,逐段移植自 golang.org/x/image/vector(v0.20.0)的
//! `raster_fixed.go` 與 `vector.go` 的路徑切分。
//!
//! 為什麼不用現成的 Rust 光柵器:Go 版在 512 像素以下的圖用這套 22.9 定點數運算,
//! 浮點數光柵器(ab_glyph_rasterizer 等)在邊緣像素的覆蓋率會差一點點,而字模是用
//! 覆蓋率 0x80 當門檻切成 1-bit 的,差一點點就會整格翻轉。照搬同一套整數運算,
//! 兩邊畫出來的字模才會逐位元相同,後備字型的畫面也才能逐像素比對。
//!
//! 原始碼的著作權屬於 The Go Authors,以 BSD 授權釋出,聲明見 repo 根目錄的 NOTICE。

/// 小數點後的位元數:22.9 定點數。
const PHI: u32 = 9;
const FX_ONE: i32 = 1 << PHI;
const FX_ONE_AND_A_HALF: i32 = (1 << PHI) + (1 << (PHI - 1));
const FX_ONE_MINUS_IOTA: i32 = (1 << PHI) - 1;

fn fixed_floor(x: i32) -> i32 {
    x >> PHI
}

fn fixed_ceil(x: i32) -> i32 {
    x.wrapping_add(FX_ONE_MINUS_IOTA) >> PHI
}

/// Go 的 `clamp`:負數夾到 0,超過寬度的回傳寬度本身。注意回傳寬度時索引會落到
/// **下一列**的第一格(buf 是從這一列切到尾端),Go 版就是這樣,照抄。
fn clamp(i: i32, width: i32) -> usize {
    if i < 0 {
        0
    } else if i < width {
        i as usize
    } else {
        width as usize
    }
}

pub struct Raster {
    w: i32,
    h: i32,
    buf: Vec<u32>,
    first: (f32, f32),
    pen: (f32, f32),
}

impl Raster {
    pub fn new(w: i32, h: i32) -> Raster {
        Raster { w, h, buf: vec![0; (w.max(0) * h.max(0)) as usize], first: (0.0, 0.0), pen: (0.0, 0.0) }
    }

    /// 開始一段新路徑。與 Go 版相同,不會自動封閉前一段。
    pub fn move_to(&mut self, x: f32, y: f32) {
        self.first = (x, y);
        self.pen = (x, y);
    }

    pub fn close_path(&mut self) {
        let (x, y) = self.first;
        self.line_to(x, y);
    }

    pub fn quad_to(&mut self, bx: f32, by: f32, cx: f32, cy: f32) {
        let (ax, ay) = self.pen;
        let devsq = dev_squared(ax, ay, bx, by, cx, cy);
        if devsq >= 0.333 {
            const TOL: f64 = 3.0;
            let n = 1 + (TOL * devsq as f64).sqrt().sqrt() as i32;
            let (mut t, n_inv) = (0f32, 1.0 / n as f32);
            for _ in 0..n - 1 {
                t += n_inv;
                let (abx, aby) = lerp(t, ax, ay, bx, by);
                let (bcx, bcy) = lerp(t, bx, by, cx, cy);
                let (x, y) = lerp(t, abx, aby, bcx, bcy);
                self.line_to(x, y);
            }
        }
        self.line_to(cx, cy);
    }

    #[allow(clippy::too_many_arguments)]
    pub fn cube_to(&mut self, bx: f32, by: f32, cx: f32, cy: f32, dx: f32, dy: f32) {
        let (ax, ay) = self.pen;
        let devsq = dev_squared(ax, ay, bx, by, dx, dy).max(dev_squared(ax, ay, cx, cy, dx, dy));
        if devsq >= 0.333 {
            const TOL: f64 = 3.0;
            let n = 1 + (TOL * devsq as f64).sqrt().sqrt() as i32;
            let (mut t, n_inv) = (0f32, 1.0 / n as f32);
            for _ in 0..n - 1 {
                t += n_inv;
                let (abx, aby) = lerp(t, ax, ay, bx, by);
                let (bcx, bcy) = lerp(t, bx, by, cx, cy);
                let (cdx, cdy) = lerp(t, cx, cy, dx, dy);
                let (abcx, abcy) = lerp(t, abx, aby, bcx, bcy);
                let (bcdx, bcdy) = lerp(t, bcx, bcy, cdx, cdy);
                let (x, y) = lerp(t, abcx, abcy, bcdx, bcdy);
                self.line_to(x, y);
            }
        }
        self.line_to(dx, dy);
    }

    /// Go 的 `fixedLineTo`。所有加法照 Go 的 uint32 / int32 環繞語意。
    pub fn line_to(&mut self, bx: f32, by: f32) {
        let (mut ax, mut ay) = self.pen;
        let (mut bx, mut by) = (bx, by);
        self.pen = (bx, by);
        let mut dir: i32 = 1;
        if ay > by {
            dir = -1;
            std::mem::swap(&mut ax, &mut bx);
            std::mem::swap(&mut ay, &mut by);
        }
        // 水平線段不改變覆蓋率;幾乎水平的線段在定點數裡不穩定,也當成水平。
        if by - ay <= 0.000001 {
            return;
        }
        let dxdy = (bx - ax) / (by - ay);
        let one = FX_ONE as f32;
        let ay_p = (ay * one) as i32;
        let by_p = (by * one) as i32;
        let mut x = (ax * one) as i32;
        let mut y = fixed_floor(ay_p);
        let y_max = fixed_ceil(by_p).min(self.h);
        let width = self.w;
        while y < y_max {
            let dy = ((y + 1) << PHI).min(by_p) - (y << PHI).max(ay_p);
            let x_next = x.wrapping_add((dy as f32 * dxdy) as i32);
            if y < 0 {
                x = x_next;
                y += 1;
                continue;
            }
            let row = ((y * width) as usize).min(self.buf.len());
            let buf = &mut self.buf[row..];
            let mut add = |i: usize, v: i32| {
                if let Some(c) = buf.get_mut(i) {
                    *c = c.wrapping_add(v as u32);
                }
            };
            let d = dy.wrapping_mul(dir);
            let (x0, x1) = if x > x_next { (x_next, x) } else { (x, x_next) };
            let x0i = fixed_floor(x0);
            let x0_floor = x0i << PHI;
            let x1i = fixed_ceil(x1);
            let x1_ceil = x1i << PHI;
            if x1i <= x0i + 1 {
                let xmf = ((x.wrapping_add(x_next)) >> 1) - x0_floor;
                add(clamp(x0i, width), d.wrapping_mul(FX_ONE - xmf));
                add(clamp(x0i + 1, width), d.wrapping_mul(xmf));
            } else {
                let one_over_s = x1 - x0;
                let two_over_s = 2 * one_over_s;
                let x0f = x0 - x0_floor;
                let one_minus_x0f = FX_ONE - x0f;
                let one_minus_x0f_sq = one_minus_x0f.wrapping_mul(one_minus_x0f);
                let x1f = x1 - x1_ceil + FX_ONE;
                let x1f_sq = x1f.wrapping_mul(x1f);
                add(clamp(x0i, width), one_minus_x0f_sq.wrapping_mul(d) / two_over_s);
                if x1i == x0i + 2 {
                    let dd = (two_over_s << PHI) - one_minus_x0f_sq - x1f_sq;
                    add(clamp(x0i + 1, width), dd.wrapping_mul(d) / two_over_s);
                } else {
                    let dd = ((FX_ONE_AND_A_HALF - x0f) << (PHI + 1)) - one_minus_x0f_sq;
                    add(clamp(x0i + 1, width), dd.wrapping_mul(d) / two_over_s);
                    let d_times_s = (d << (2 * PHI)) / one_over_s;
                    for xi in x0i + 2..x1i - 1 {
                        add(clamp(xi, width), d_times_s);
                    }
                    const C: i32 = (1 << (PHI + 2)) - (FX_ONE_AND_A_HALF << 1);
                    let mut dd = (x1f << 1) + C;
                    dd <<= PHI;
                    dd -= x1f_sq;
                    add(clamp(x1i - 1, width), dd.wrapping_mul(d) / two_over_s);
                }
                add(clamp(x1i, width), x1f_sq.wrapping_mul(d) / two_over_s);
            }
            x = x_next;
            y += 1;
        }
    }

    /// Go 的 `fixedAccumulateOpSrc`:累加成 8 位元覆蓋率(列優先,寬 × 高)。
    pub fn accumulate(&self) -> Vec<u8> {
        let mut acc: i32 = 0;
        self.buf
            .iter()
            .map(|&v| {
                acc = acc.wrapping_add(v as i32);
                let a = (acc.wrapping_abs()) >> (2 * PHI - 8);
                a.min(0xff) as u8
            })
            .collect()
    }
}

fn dev_squared(ax: f32, ay: f32, bx: f32, by: f32, cx: f32, cy: f32) -> f32 {
    let devx = ax - 2.0 * bx + cx;
    let devy = ay - 2.0 * by + cy;
    devx * devx + devy * devy
}

fn lerp(t: f32, px: f32, py: f32, qx: f32, qy: f32) -> (f32, f32) {
    (px + t * (qx - px), py + t * (qy - py))
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 一個軸對齊的方塊:內部全滿,外面全空。
    #[test]
    fn square_is_fully_covered() {
        let mut r = Raster::new(4, 4);
        r.move_to(1.0, 1.0);
        r.line_to(3.0, 1.0);
        r.line_to(3.0, 3.0);
        r.line_to(1.0, 3.0);
        r.close_path();
        let a = r.accumulate();
        for y in 0..4 {
            for x in 0..4 {
                let inside = (1..3).contains(&x) && (1..3).contains(&y);
                assert_eq!(a[y * 4 + x], if inside { 0xff } else { 0 }, "({x},{y})");
            }
        }
    }

    /// 半格寬的方塊:覆蓋率一半。
    #[test]
    fn half_pixel_coverage() {
        let mut r = Raster::new(2, 1);
        r.move_to(0.5, 0.0);
        r.line_to(1.0, 0.0);
        r.line_to(1.0, 1.0);
        r.line_to(0.5, 1.0);
        r.close_path();
        let a = r.accumulate();
        assert!((0x7f..=0x80).contains(&a[0]) && a[1] == 0, "{a:?}");
    }
}
