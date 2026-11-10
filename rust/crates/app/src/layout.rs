//! 視窗尺寸的防呆(對應 Go 版 `layout.go`)。

/// 比任何真實螢幕都大。超過就表示算式壞掉了,不是真的有這麼大的螢幕。
const MAX_DIM: i32 = 1 << 20;

/// 保底尺寸取一支小手機的 dp。畫得出東西比畫得好看重要:這個值只有在「螢幕資訊也拿
/// 不到」時才會用到。
pub(crate) const FALLBACK_W: i32 = 360;
pub(crate) const FALLBACK_H: i32 = 640;

fn ok_dim(v: i32) -> bool {
    v > 0 && v < MAX_DIM
}

/// 把不合理的視窗尺寸換成可用的值。
///
/// 輸入本身就可能是壞的:Android 那一側算版面尺寸時要除以 deviceScale,那個值拿不到時
/// 是 0,除出來的 +Inf 轉成整數會變成負數。規則是「不把壞輸入原樣傳出去」:退回上一次
/// 的好值,連一次都還沒拿到就用螢幕尺寸,再不行給一個保底值。last 與 mon 傳 0 表示沒有
/// 這個資訊。
pub fn sane_layout(w: i32, h: i32, last_w: i32, last_h: i32, mon_w: i32, mon_h: i32) -> (i32, i32) {
    if ok_dim(w) && ok_dim(h) {
        (w, h)
    } else if ok_dim(last_w) && ok_dim(last_h) {
        (last_w, last_h)
    } else if ok_dim(mon_w) && ok_dim(mon_h) {
        (mon_w, mon_h)
    } else {
        (FALLBACK_W, FALLBACK_H)
    }
}
