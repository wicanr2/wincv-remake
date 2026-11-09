//! Koala Painter 的 C64 圖檔。
//!
//! 版面固定,沒有壓縮:載入位址(2)+ 點陣圖(8000)+ 螢幕記憶體(1000)+ 色彩
//! 記憶體(1000)+ 背景色(1)= 10003 個位元組。有些檔案沒有前面的載入位址,所以
//! 10001 也收。
//!
//! 多色點陣模式下每個像素佔 2 個位元,決定**去哪裡取顏色**:00 背景色、01 螢幕
//! 記憶體的高 4 位、10 螢幕記憶體的低 4 位、11 色彩記憶體的低 4 位。四個來源接錯
//! 任何一個,圖看起來仍然「有東西」,只是顏色亂掉。每個像素在螢幕上是兩倍寬
//! (160x200 的資料畫成 320x200)。

use wincv_i18n::tr;

use crate::Image;

/// C64 的 16 色,取自 Pepto 量測 VIC-II 訊號得到的那一組。
const C64: [[u8; 3]; 16] = [
    [0x00, 0x00, 0x00], [0xFF, 0xFF, 0xFF], [0x68, 0x37, 0x2B], [0x70, 0xA4, 0xB2],
    [0x6F, 0x3D, 0x86], [0x58, 0x8D, 0x43], [0x35, 0x28, 0x79], [0xB8, 0xC7, 0x6F],
    [0x6F, 0x4F, 0x25], [0x43, 0x39, 0x00], [0x9A, 0x67, 0x59], [0x44, 0x44, 0x44],
    [0x6C, 0x6C, 0x6C], [0x9A, 0xD2, 0x84], [0x6C, 0x5E, 0xB5], [0x95, 0x95, 0x95],
];

pub fn decode(data: &[u8]) -> Result<Image, String> {
    let data = match data.len() {
        10003 => &data[2..], // 跳過載入位址
        10001 => data,
        n => return Err(tr!("KOA: 長度是 %d,應該是 10003 或 10001", n)),
    };
    let (bitmap, screen, colram) = (&data[..8000], &data[8000..9000], &data[9000..10000]);
    let bg = data[10000] & 0x0F;
    let mut img = Image::new(320, 200);
    for y in 0..200 {
        for x in 0..160 {
            let cell = (y / 8) * 40 + x / 4;
            // 最左邊的像素在最高的兩個位元
            let bits = (bitmap[cell * 8 + y % 8] >> (6 - 2 * (x % 4))) & 0x03;
            let idx = match bits {
                0 => bg,
                1 => screen[cell] >> 4,
                2 => screen[cell] & 0x0F,
                _ => colram[cell] & 0x0F,
            };
            let [r, g, b] = C64[idx as usize];
            img.set(x as u32 * 2, y as u32, [r, g, b, 0xFF]);
            img.set(x as u32 * 2 + 1, y as u32, [r, g, b, 0xFF]);
        }
    }
    Ok(img)
}
