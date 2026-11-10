//! 對應 Go 版 `internal/app/zoom_test.go`。

use wincv_cell::Screen;
use wincv_keys::Key;

use super::*;
use crate::{MAX_SCALE, MIN_SCALE};

// 字級由外層(cmd/wincv)決定有幾級,app 只負責夾在 [0, MaxZoom] 之間。
// 這個測試盯的是邊界:在最大級再放大、在 0 級再縮小都不能越界,
// 而且「沒變」要回傳 false —— 外層靠這個回傳值決定要不要重載字型。
#[test]
fn zoom_clamps_to_range() {
    let td = fixture();
    let mut a = new_at(td.path());
    a.max_zoom = 2;

    for (i, want) in [1, 2, 2].into_iter().enumerate() {
        let changed = a.handle_key(ctrl_rune('+'));
        assert_eq!(a.zoom, want, "第 {} 次放大後的 Zoom", i + 1);
        let last = i == 2;
        assert_ne!(changed, last, "第 {} 次放大 changed = {changed}", i + 1);
    }
    assert!(a.handle_key(ctrl_rune('0')) && a.zoom == 0, "Ctrl-0 之後 Zoom = {}, 想要 0", a.zoom);
    assert!(!a.handle_key(ctrl_rune('-')) && a.zoom == 0, "在 0 級縮小之後 Zoom = {}, 想要 0", a.zoom);
}

// MaxZoom 沒設(只有一種字型可載)時,放大鍵不能把 Zoom 推到 1 ——
// 外層會拿它去索引字型陣列。
#[test]
fn zoom_with_single_level() {
    let td = fixture();
    let mut a = new_at(td.path());
    assert!(!a.handle_key(ctrl_rune('+')) && a.zoom == 0, "只有一級時放大 Zoom = {}, 想要 0", a.zoom);
}

// 關於畫面:內容要真的畫出來,而且任意鍵都關得掉。
// 署名是這個 repo 存在的理由之一,漏掉不能只靠人眼看截圖。
#[test]
fn about_screen() {
    let td = fixture();
    let mut a = new_at(td.path());
    assert!(a.open_about() && a.abouting(), "open_about 沒開起來");
    let mut s = Screen::new(78, 20);
    a.draw(&mut s);
    let got = screen_text(&s);
    for want in ["WinCV Remake", "Lcc Wizard", "王俊又", "為保存台灣中文軟體盡一份心力"] {
        assert!(got.contains(want), "關於畫面少了 {want:?}\n{got}");
    }
    assert!(a.handle_key(Key::ch('x')) && !a.abouting(), "按鍵之後關於畫面沒關掉");
}

// 放大倍率一階 0.1,而且要真的停在 0.1 的整數倍上。
//
// [雷] 0.1 在二進位裡除不盡。1.0 一路加十次 0.1 會走到
// 0.9999999999999999,顯示成「1.0×」卻與 1.0 不相等 —— 於是
// 「已經是這個倍率了」永遠不成立,每按一次都回傳 true,
// 外層每次都重建視窗與畫布。所以每一階都要量化回去。
#[test]
fn scale_steps_by_tenth() {
    let td = fixture();
    let mut a = new_at(td.path());
    assert_eq!(a.scale, 1.0, "預設倍率");
    for (i, want) in [1.1, 1.2, 1.3, 1.4, 1.5].into_iter().enumerate() {
        a.handle_key(alt_rune('+'));
        assert_eq!(a.scale, want, "第 {} 次放大之後", i + 1);
    }
    for (i, want) in [1.4, 1.3].into_iter().enumerate() {
        a.handle_key(alt_rune('-'));
        assert_eq!(a.scale, want, "第 {} 次縮小之後", i + 1);
    }
    assert!(a.handle_key(alt_rune('0')) && a.scale == 1.0, "Alt-0 之後是 {},想要 1", a.scale);
}

// 上下限要夾住,而且「沒變」要回傳 false —— 外層靠這個決定要不要重建視窗。
#[test]
fn scale_clamps() {
    let td = fixture();
    let mut a = new_at(td.path());
    for _ in 0..40 {
        a.handle_key(alt_rune('+'));
    }
    assert_eq!(a.scale, MAX_SCALE, "一路放大之後");
    assert!(!a.handle_key(alt_rune('+')), "已經在上限了還回報有變");
    for _ in 0..40 {
        a.handle_key(alt_rune('-'));
    }
    assert_eq!(a.scale, MIN_SCALE, "一路縮小之後");
    assert!(!a.handle_key(alt_rune('-')), "已經在下限了還回報有變");
}

// 訊息要說得出現在是幾倍 —— 倍率沒有別的地方看得到。
#[test]
fn scale_reports_value() {
    let td = fixture();
    let mut a = new_at(td.path());
    a.handle_key(alt_rune('+'));
    assert!(a.message.contains("1.1"), "訊息是 {:?}", a.message);
}
