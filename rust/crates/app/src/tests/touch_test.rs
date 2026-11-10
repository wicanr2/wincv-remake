//! 照搬 Go 版 `touch_test.go`。

use super::*;
use crate::{Mode, TOUCH_ROWS};

fn touch_app() -> (App, Screen, TempDir) {
    let td = fixture();
    let mut a = new_at(td.path());
    a.touch = true;
    (a, Screen::new(44, 24), td)
}

/// 觸控列不能蓋掉檔案清單與狀態列。
#[test]
fn touch_bar_does_not_cover_content() {
    let (mut a, mut s, _td) = touch_app();
    a.draw(&mut s);
    let (bar, hud1, hud2) = (row_text(&s, s.rows - 3), row_text(&s, s.rows - 2), row_text(&s, s.rows - 1));
    assert!(bar.contains("選單"), "動作列不見了: {bar:?}");
    assert!(hud1.contains("Esc") && hud1.contains("Enter"), "HUD 第一列少了 Esc / Enter: {hud1:?}");
    assert!(hud2.contains('◀') && hud2.contains('▶'), "HUD 第二列少了左右鍵: {hud2:?}");
    let above: Vec<String> = (0..s.rows - TOUCH_ROWS).map(|y| row_text(&s, y)).collect();
    assert!(above.iter().any(|r| r.contains("txt")), "檔案清單被觸控列蓋掉了");
    assert!(above.iter().any(|r| r.contains("剩餘")), "狀態列被觸控列蓋掉了");
}

#[test]
fn touch_bar_follows_mode() {
    let (mut a, mut s, _td) = touch_app();
    a.draw(&mut s);
    assert!(row_text(&s, s.rows - TOUCH_ROWS).contains("拷貝"), "瀏覽模式的功能列");
    walk_to(&mut a, &mut s, "a.txt");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    assert_eq!(a.mode, Mode::Viewer);
    let got = row_text(&s, s.rows - TOUCH_ROWS);
    assert!(!got.contains("拷貝") && !got.contains("標記"), "檢視模式還在顯示瀏覽的動作: {got:?}");
    assert!(row_text(&s, s.rows - 2).contains("Esc"), "檢視模式的 HUD 少了 Esc");
}

/// 畫面太小時不畫觸控列,也不能 panic。
#[test]
fn touch_bar_skipped_on_tiny_screen() {
    let (mut a, _, _td) = touch_app();
    a.draw(&mut Screen::new(20, 3));
}

/// 點擊的判定要與畫出來的位置一致,而且整列沒有死區。
#[test]
fn touch_hit_test_matches_drawing() {
    for cols in [30, 44, 60, 93] {
        let (mut a, _, _td) = touch_app();
        let mut s = Screen::new(cols, 20);
        a.draw(&mut s);
        let bar_y = s.rows - TOUCH_ROWS;
        let bar = a.touch_bar();
        for (i, sp) in a.touch_spans(cols).iter().enumerate().take(bar.len()) {
            let mid = sp.x + sp.w / 2;
            let got = a.touch_key_at(mid, 0, cols);
            assert_eq!(got, Some(bar[i].key), "{cols} 欄:點在「{}」的位置卻得到別的鍵", bar[i].label);
            let row = row_text(&s, bar_y);
            assert!(row.contains(&bar[i].label), "{cols} 欄:畫面上找不到「{}」→ {row:?}", bar[i].label);
        }
        for x in 0..cols {
            assert!(a.touch_key_at(x, 0, cols).is_some(), "{cols} 欄:第 {x} 格是死區");
            for r in 1..=2 {
                assert!(a.touch_key_at(x, r, cols).is_some(), "{cols} 欄:HUD 第 {r} 列第 {x} 格是死區");
            }
        }
    }
}

#[test]
fn touch_hud_hit_test() {
    for cols in [30, 44, 60, 93] {
        let (mut a, _, _td) = touch_app();
        let mut s = Screen::new(cols, 20);
        a.draw(&mut s);
        for (r, nav) in crate::touch::touch_nav().iter().enumerate() {
            let y = s.rows - TOUCH_ROWS + 1 + r as i32;
            for (i, (label, key)) in nav.iter().enumerate() {
                let w = cols / nav.len() as i32;
                let mid = i as i32 * w + w / 2;
                assert_eq!(a.touch_key_at(mid, r as i32 + 1, cols), Some(*key), "{cols} 欄:HUD 第 {} 列「{label}」", r + 1);
                assert!(row_text(&s, y).contains(label), "{cols} 欄:第 {y} 列畫面上沒有「{label}」");
            }
        }
    }
}

#[test]
fn list_cursor_row() {
    let td = fixture();
    let mut a = new_at(td.path());
    let mut s = Screen::new(44, 20);
    a.draw(&mut s);
    assert_eq!(a.list_cursor_row(), 0);
    a.handle_key(named(Code::Down));
    a.draw(&mut s);
    assert_eq!(a.list_cursor_row(), 1);
    a.mode = Mode::Viewer;
    assert_eq!(a.list_cursor_row(), -1, "非瀏覽模式應該回 -1");
}

/// 開一張圖進看圖模式。看圖模式的按鍵有幾顆要有「上一張」可走,所以目錄裡要有兩張圖。
/// 圖用 PGM(Go 版用 PNG):app 不依賴 PNG 編碼器,格式與這裡要驗的事無關。
fn image_app() -> (App, TempDir) {
    let td = TempDir::new();
    let mut pgm = b"P5\n8 8\n255\n".to_vec();
    pgm.extend([0u8; 64]);
    for n in ["a.pgm", "b.pgm"] {
        write(td.path().join(n), &pgm);
    }
    let mut a = new_at(td.path());
    a.touch = true;
    let mut s = Screen::new(44, 20);
    a.draw(&mut s);
    walk_to(&mut a, &mut s, "a.pgm");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Image);
    (a, td)
}

/// 看圖模式的功能列:每一格都要對應到**看圖模式真的認得**的按鍵。這一列曾經有兩顆按了
/// 沒反應的按鈕,畫面完全正常,按下去就是沒事發生。
#[test]
fn image_touch_bar_keys_are_handled() {
    let (probe, _td) = image_app();
    for b in probe.touch_bar() {
        let (mut a, _td2) = image_app();
        assert!(a.handle_key(b.key), "看圖模式不認得「{}」送出的 {}", b.label, b.key);
    }
}

/// 三十欄是手機直立時的寬度下限,標籤在那裡要畫得完。
#[test]
fn image_touch_bar_fits_narrow_screen() {
    let (mut a, _td) = image_app();
    let mut s = Screen::new(30, 20);
    a.draw(&mut s);
    let row = row_text(&s, s.rows - TOUCH_ROWS);
    for b in a.touch_bar() {
        assert!(row.contains(&b.label), "30 欄時畫不出「{}」:{row:?}", b.label);
    }
}
