//! 照搬 Go 版 `mouse_test.go`。

use super::*;
use crate::menu::{cat_label, menu_cats};
use crate::{cell_width, Mode};
use wincv_cell::Color;

fn click_app() -> (App, TempDir) {
    let td = TempDir::new();
    for n in ["a.txt", "b.txt", "c.txt", "d.txt"] {
        write(td.path().join(n), format!("{n}\n"));
    }
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 25));
    (a, td)
}

/// 選單列上第 i 個分類的某一格(標題的第二格)。
fn cat_col(i: usize) -> i32 {
    let mut x = 1;
    for (j, c) in menu_cats().iter().enumerate() {
        if j == i {
            return x + 1;
        }
        x += cell_width(&cat_label(j, &c.name));
    }
    x
}

#[test]
fn click_moves_cursor() {
    let (mut a, _td) = click_app();
    assert!(a.click(10, 3, false), "點清單沒有反應");
    assert_eq!(a.browser.cursor, 2, "第 3 列扣掉路徑列應該是 2");
    assert!(!a.click(10, 0, false), "點路徑列不該有反應");
    assert!(!a.click(10, 20, false), "點空白列不該有反應");
}

#[test]
fn double_click_opens() {
    let (mut a, _td) = click_app();
    a.click(10, 2, false);
    assert!(a.browser.current().is_some_and(|e| !e.is_dir), "第 2 列不是檔案");
    a.click(10, 2, true);
    assert_eq!(a.mode, Mode::Viewer, "雙擊之後應該是文字檢視");
}

#[test]
fn menu_bar_click() {
    let (mut a, _td) = click_app();
    a.menu_bar_click(cat_col(1));
    assert!(a.menuing() && a.menu.cat == 1, "點「目錄/磁碟」後 cat={}", a.menu.cat);
    a.menu_bar_click(cat_col(1));
    assert!(!a.menuing(), "再點一次同一個分類應該收起來");
    a.menu_bar_click(cat_col(0));
    assert!(a.menuing() && a.menu.cat == 0);
    a.menu_bar_click(cat_col(menu_cats().len()) + 1);
    assert!(!a.menuing(), "點空白處應該收起來");
}

#[test]
fn click_outside_closes_menu() {
    let (mut a, _td) = click_app();
    a.handle_key(named(Code::F9));
    let before = a.browser.cursor;
    a.click(10, 3, false);
    assert!(!a.menuing(), "點內容區應該收起選單");
    assert_eq!(a.browser.cursor, before, "收選單那一下不該同時移游標");
}

#[test]
fn menu_drop_click() {
    let (mut a, _td) = click_app();
    a.handle_key(named(Code::F9));
    a.select_cat(7); // 說明
    a.menu_layer(80, 25);
    let want = a.menu.items.iter().position(|it| !it.sep).expect("說明選單沒有項目") as i32;
    assert!(a.menu_drop_click(want), "點項目沒反應");
    assert!(!a.menuing(), "執行之後選單應該收起來");
}

#[test]
fn menu_drop_hover() {
    let (mut a, _td) = click_app();
    a.menu_bar_click(cat_col(6)); // 設定
    a.menu_layer(80, 25);
    let zoom = a.zoom;
    assert!(a.menu_drop_hover(1), "移入縮小字體沒有更新選取列");
    assert!(a.menuing() && a.zoom == zoom, "移入項目不應執行功能或關閉選單");
    let drop = a.menu_layer(80, 25).drop.unwrap();
    assert!(drop.at(0, 1).unwrap().bg == Color::BLUE && drop.at(0, 0).unwrap().bg != Color::BLUE, "藍色選取列沒有移到縮小字體");
    for row in [1, 2, -1, a.menu.rows, 100] {
        assert!(!a.menu_drop_hover(row) && a.menu.cursor == 1, "重複位置或非項目列 {row} 不應改變選取");
    }
    a.handle_key(named(Code::Esc));
    assert!(!a.menu_drop_hover(1), "選單關閉後不應回應移入");
}

#[test]
fn menu_drop_hover_submenu_and_scroll() {
    let (mut a, _td) = click_app();
    a.menu_bar_click(cat_col(6)); // 設定
    a.menu_layer(80, 25);
    a.menu_drop_click(5); // 視窗大小
    let drop = a.menu_layer(80, 6).drop.unwrap();
    assert!(a.menu_drop_hover(2) && a.menu.cursor == 1, "子選單移入項目時沒有扣掉標題列");
    assert!(!a.menu_drop_hover(0) && !a.menu_drop_hover(drop.rows - 1) && a.menu.cursor == 1, "移入子選單標題或捲動提示不應改變選取");
    a.handle_key(named(Code::Down));
    a.menu_layer(80, 6);
    let top = a.menu.top;
    assert!(top != 0, "測試未捲動子選單");
    assert!(a.menu_drop_hover(1) && a.menu.cursor == top, "捲動後移入第一列沒有選到可見的第一個項目");
}

#[test]
fn wheel() {
    let (mut a, _td) = click_app();
    a.wheel(3);
    assert_eq!(a.browser.cursor, 3);
    a.wheel(-2);
    assert_eq!(a.browser.cursor, 1);
}

#[test]
fn click_editor() {
    let (mut a, _td) = click_app();
    write(a.browser.dir.join("e.txt"), "ab中文cd\nxyz\n");
    a.browser.reload().unwrap();
    let i = a.browser.entries.iter().position(|e| e.name == "e.txt").unwrap();
    a.browser.move_to(i as i32, 20);
    a.handle_key(Key::ch('E'));
    assert_eq!(a.mode, Mode::Edit);
    a.draw(&mut Screen::new(80, 25));
    a.click(5, 0, false); // 「文」的後半格(a b 中=2,3 文=4,5)
    let c = a.editor.as_ref().unwrap().cur;
    assert_eq!((c.line, c.col), (0, 3));
    a.click(70, 1, false); // 超出行尾 → 行尾
    let c = a.editor.as_ref().unwrap().cur;
    assert_eq!((c.line, c.col), (1, 3));
}

#[test]
fn menu_bar_function_keys_clickable() {
    let (mut a, _td) = click_app();
    let bar = row_text(a.menu_layer(80, 25).bar.as_ref().unwrap(), 0);
    assert!(bar.trim_end().ends_with("F9 F1 F2"), "80 欄的選單列右側應該是「F9 F1 F2」:{bar:?}");
    let col = |a: &App, k: Code| a.bar_keys.iter().find(|b| b.key.code == k).map(|b| b.x0).expect("選單列上沒有這顆鍵");
    let f9 = col(&a, Code::F9);
    assert!(a.menu_bar_click(f9) && a.menuing(), "點 F9 應該打開選單");
    assert!(a.menu_bar_click(f9) && !a.menuing(), "選單開著時點 F9 應該收起");
    a.menu_bar_click(f9);
    let f1 = col(&a, Code::F1);
    assert!(a.menu_bar_click(f1) && !a.menuing() && a.mode == Mode::Markdown, "點 F1 應該收起選單並打開說明");
    let wide = row_text(a.menu_layer(120, 25).bar.as_ref().unwrap(), 0);
    assert!(wide.contains("F9 選單") && wide.contains("F2 網路"), "120 欄應該畫出完整的提示:{wide:?}");
}
