//! 畫 WinCV Remake 的應用程式圖示(對應 Go 版 `internal/appicon`)。
//!
//! 圖示是重製版自己畫的,不取用原版的 WinCV.ico(原作者的著作)。畫面元素取自 WinCV
//! 自己的介面:藍底黃字的標題、紅色的游標列、黑底的檔案清單與藍色狀態列,顏色用
//! 調色盤的同一組值。
//!
//! 用像素畫而不是向量圖:這個專案存在的理由之一是與原版逐像素對齊,圖示也沿用點陣的
//! 樣子。16 與 32 各有一張原生設計,較大的尺寸是整數倍放大,較小的尺寸不縮放(縮放會
//! 把一像素寬的線糊掉)。

use wincv_render::Image;

type Rgba = [u8; 4];

const CLEAR: Rgba = [0, 0, 0, 0];
const FRAME: Rgba = [0x40, 0x40, 0x40, 0xFF]; // dkgray
const NAVY: Rgba = [0x00, 0x00, 0x80, 0xFF]; // blue
const BLACK: Rgba = [0x00, 0x00, 0x00, 0xFF];
const YELLOW: Rgba = [0xFF, 0xFF, 0x00, 0xFF]; // ltyellow
const RED: Rgba = [0x80, 0x00, 0x00, 0xFF]; // red(游標列)
const WHITE: Rgba = [0xFF, 0xFF, 0xFF, 0xFF];
const GRAY: Rgba = [0xC0, 0xC0, 0xC0, 0xFF]; // ltgray
const CYAN: Rgba = [0x00, 0xFF, 0xFF, 0xFF]; // ltcyan
const GREEN: Rgba = [0x00, 0xFF, 0x00, 0xFF]; // ltgreen

/// 5×7 的「C」與「V」。
const GLYPH_C: [&str; 7] = [".###.", "#...#", "#....", "#....", "#....", "#...#", ".###."];
const GLYPH_V: [&str; 7] = ["#...#", "#...#", "#...#", "#...#", ".#.#.", ".#.#.", "..#.."];

/// 視窗與 .ico 要提供的尺寸。
pub const SIZES: [u32; 6] = [16, 32, 48, 64, 128, 256];

struct Canvas(Image);

impl Canvas {
    fn new(n: u32) -> Canvas {
        Canvas(Image::new(n, n))
    }

    /// 填一塊矩形,兩端都含。
    fn rect(&mut self, x0: u32, y0: u32, x1: u32, y1: u32, c: Rgba) {
        for y in y0..=y1 {
            for x in x0..=x1 {
                self.0.set(x, y, c);
            }
        }
    }

    fn glyph(&mut self, g: &[&str], x0: u32, y0: u32, scale: u32, c: Rgba) {
        for (y, row) in g.iter().enumerate() {
            for (x, ch) in row.chars().enumerate() {
                if ch == '#' {
                    let (x, y) = (x as u32, y as u32);
                    self.rect(x0 + x * scale, y0 + y * scale, x0 + (x + 1) * scale - 1, y0 + (y + 1) * scale - 1, c);
                }
            }
        }
    }

    /// 把外框的四個角削圓。r 是削掉的像素數。
    fn corners(&mut self, r: u32) {
        let n = self.0.w;
        for i in 0..r {
            for j in 0..r - i {
                for (x, y) in [(i, j), (n - 1 - i, j), (i, n - 1 - j), (n - 1 - i, n - 1 - j)] {
                    self.0.set(x, y, CLEAR);
                }
            }
        }
    }
}

/// 16×16 的原生設計。
fn draw16() -> Image {
    let mut c = Canvas::new(16);
    c.rect(0, 0, 15, 15, FRAME);
    c.rect(1, 1, 14, 8, NAVY);
    c.glyph(&GLYPH_C, 2, 1, 1, YELLOW);
    c.glyph(&GLYPH_V, 9, 1, 1, YELLOW);
    c.rect(1, 9, 14, 14, BLACK);
    c.rect(1, 10, 14, 11, RED);
    c.rect(3, 10, 6, 10, WHITE);
    c.rect(2, 13, 5, 13, CYAN);
    c.rect(8, 13, 13, 13, GRAY);
    c.corners(2);
    c.0
}

/// 32×32 的原生設計。
fn draw32() -> Image {
    let mut c = Canvas::new(32);
    c.rect(0, 0, 31, 31, FRAME);
    // 上半:藍底黃字。
    c.rect(1, 1, 30, 17, NAVY);
    c.glyph(&GLYPH_C, 4, 2, 2, YELLOW);
    c.glyph(&GLYPH_V, 17, 2, 2, YELLOW);
    // 下半:黑底的檔案清單,第一列是紅色游標列。
    c.rect(1, 18, 30, 30, BLACK);
    c.rect(1, 19, 30, 21, RED);
    c.rect(3, 20, 10, 20, WHITE);
    c.rect(13, 20, 17, 20, WHITE);
    c.rect(20, 20, 28, 20, WHITE);
    for (i, y) in [23u32, 25].into_iter().enumerate() {
        let i = i as u32;
        c.rect(3, y, 9 - i * 2, y, CYAN);
        c.rect(13, y, 17, y, GRAY);
        c.rect(20, y, 28 - i * 3, y, GREEN);
    }
    // 狀態列。
    c.rect(1, 27, 30, 30, NAVY);
    c.rect(3, 28, 12, 28, YELLOW);
    c.rect(15, 28, 22, 28, WHITE);
    c.corners(3);
    c.0
}

/// n×n 的圖示。16 與 32 是原生設計;其他尺寸用整數倍放大,32 那張放得下兩倍以上(或剛好
/// 整除)就用它,否則用 16 那張(48 = 16×3)。放不滿的部分置中留透明。
pub fn image(n: u32) -> Image {
    if n <= 16 {
        draw16()
    } else if n % 32 == 0 || n >= 64 {
        place(&draw32(), n)
    } else {
        place(&draw16(), n)
    }
}

fn place(src: &Image, n: u32) -> Image {
    let s = src.w;
    let k = n / s;
    let off = (n - s * k) / 2;
    let mut dst = Image::new(n, n);
    for y in 0..s * k {
        for x in 0..s * k {
            dst.set(off + x, off + y, src.at(x / k, y / k));
        }
    }
    dst
}

#[cfg(test)]
mod tests {
    use super::*;
    use sha2::{Digest, Sha256};

    /// 與 Go 版逐像素相同。真值是 Go 版 `appicon.Image(n).Pix` 的 sha256。
    #[test]
    fn matches_go() {
        let want = [
            (16, "6b1107eb046c59ee99d94de55b4182bc38f6f4f53337e56387afac466aedf2c0"),
            (24, "6b4fe184691287434ee330f4457e6c41f6ddcc6cce60548278ec86e36d2f8d6c"),
            (32, "386f2f83273ee27b4ce75cdc193d7245c63d572edc9b36d9bc5737193a507462"),
            (48, "65155420fd21b4e3f94f567890382cc0d9b3b4aff465671fe98203c2bb474d00"),
            (64, "52b88c03109738610fc8a456d718f97095421358e05491f7d15a30b25495c476"),
            (128, "686590b99ba74056e48e2b2ae8e35194c964f122948be2b833600fea3c68142a"),
            (256, "6edf749ce6e7bf7d0712c6ee4baf61b3b9e3793e306bf1fb811eab0690c7299e"),
        ];
        for (n, h) in want {
            let got: String = Sha256::digest(&image(n).pix).iter().map(|b| format!("{b:02x}")).collect();
            assert_eq!(got, h, "{n}×{n}");
        }
    }
}
