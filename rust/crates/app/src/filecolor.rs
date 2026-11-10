//! 檔案列表依副檔名上色(對應 Go 版 `filecolor.go`)。

use std::path::Path;

use wincv_browser::Entry;
use wincv_cell::Color;

/// 依副檔名決定一列的顏色。
///
/// 顏色是從原版量出來的(docs/ui/oracle-ext.png 與 oracle-ext2.png,一個副檔名放一個檔
/// 再逐列取樣):
///
/// ```text
/// 目錄      dir-green   #14BE00
/// exe com   ltred       #FF0000
/// bat cmd   ltmagenta   #FF00FF
/// 壓縮檔    dir-cyan    #1EBEBE   實測 7z ace arj gz lzh rar zip
/// 圖檔      dir-ltgreen #00F000   實測 bmp gif ico jpg png
/// 其他      ltgray      #C0C0C0
/// ```
///
/// 壓縮檔與圖檔這兩類,實測的副檔名不是全部:這裡直接沿用 is_archive 與 is_image 的
/// 判斷,把 cab / tar / tiff 這些沒實測到的也歸進去。那是推廣,不是量到的。
pub(crate) fn file_color(e: &Entry) -> Color {
    if e.is_dir {
        return Color::DIR_GREEN;
    }
    // Go 的 path.Ext:最後一個點之後(含點),最後一段裡沒有點就是空的。
    let base = e.name.rsplit('/').next().unwrap_or(&e.name);
    let ext = base.rfind('.').map(|i| base[i..].to_lowercase()).unwrap_or_default();
    match ext.as_str() {
        ".exe" | ".com" => Color::LT_RED,
        ".bat" | ".cmd" => Color::LT_MAGENTA,
        _ if wincv_archive::is_archive(Path::new(&e.name)) => Color::DIR_CYAN,
        _ if wincv_imgfmt::is_image(&e.name) => Color::DIR_LT_GREEN,
        _ => Color::LT_GRAY,
    }
}
