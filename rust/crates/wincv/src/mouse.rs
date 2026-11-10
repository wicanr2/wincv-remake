//! 滑鼠:像素換成格子,交給 app(對應 Go 版 `cmd/wincv/input.go` 的滑鼠部分)。
//!
//! 像素 → 格子的換算在這裡做,因為只有外殼知道選單層與內容層各用什麼倍率、選單列佔幾個
//! 像素、下拉框貼在哪。app 那一層只收格子座標。

use std::time::{Duration, Instant};

/// 雙擊的時間窗。
const DOUBLE_CLICK: Duration = Duration::from_millis(400);

/// 滾輪一格捲幾列。
pub const WHEEL_CELLS: i32 = 3;

/// 點到的區域。
#[derive(Clone, Copy, PartialEq, Eq, Default)]
pub enum Zone {
    #[default]
    Content,
    MenuBar,
    MenuDrop,
}

/// 下拉框在視窗上佔的像素矩形(x0, y0, x1, y1),空的表示沒展開。
pub type Rect = (i32, i32, i32, i32);

#[derive(Default)]
pub struct Mouse {
    pub pos: (f64, f64),
    last_at: Option<Instant>,
    last_cell: (i32, i32),
    last_zone: Zone,
    /// 按著左鍵時記住上一次換算出來的欄位,移過一格就送一次 drag。-1 表示不是從內容區開始拖。
    pub dragging: bool,
    pub drag_col: i32,
    /// 觸控板會送很多小數,累積到一格才送。
    pub wheel: f64,
}

impl Mouse {
    /// 記下這一次點擊,回傳它是不是雙擊。連點兩下之後歸零,第三下不算再一次雙擊。
    pub fn press(&mut self, zone: Zone, cell: (i32, i32)) -> bool {
        let now = Instant::now();
        let double = zone == Zone::Content
            && zone == self.last_zone
            && cell == self.last_cell
            && self.last_at.is_some_and(|t| now.duration_since(t) < DOUBLE_CLICK);
        self.last_at = if double { None } else { Some(now) };
        self.last_cell = cell;
        self.last_zone = zone;
        double
    }
}
