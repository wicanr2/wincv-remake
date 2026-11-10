//! 對應 Go 版 `internal/app/menu_test.go`。

use wincv_cell::Screen;
use wincv_keys::{Code, Key};

use super::*;
use crate::menu::{cjk24_menu_items, menu_cats};
use crate::Mode;

// 選單列一直在,不必按任何鍵。
//
// 它畫在自己的畫面上而不是主格點裡 —— 選單可以用與內容不同的字型與
// 大小,兩者的格點對不起來,所以由外殼各自光柵化再疊。
#[test]
fn menu_bar_always_visible() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    let l = a.menu_layer(80, 25);
    let bar = l.bar.as_ref().expect("選單列不見了");
    let row0 = row_text(bar, 0).trim_end().to_string();
    for want in ["1檔案", "2目錄/磁碟", "3網路", "4標記", "5檢視", "6轉檔/轉碼/其他", "7設定", "8說明"] {
        assert!(row0.contains(want), "選單列上沒有「{want}」:{row0:?}");
    }
    // 收起來的時候不該有下拉。
    assert!(l.drop.is_none(), "沒展開卻有下拉");
}

// 選單列不佔主畫面的格 —— 內容從第 0 列開始。
#[test]
fn menu_bar_does_not_steal_content_rows() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    let mut s = Screen::new(80, 25);
    a.draw(&mut s);
    let with_bar = row_text(&s, 0).trim_end().to_string();

    a.menu_bar = false;
    let mut s2 = Screen::new(80, 25);
    a.draw(&mut s2);
    let without_bar = row_text(&s2, 0).trim_end().to_string();

    assert_eq!(with_bar, without_bar, "選單列動到了主畫面");
    // 關掉之後連選單列都不該給。
    assert!(a.menu_layer(80, 25).bar.is_none(), "關掉了還畫選單列");
}

// 展開時要有下拉,而且裡面是那個分類的項目。
#[test]
fn dropdown_has_items() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F9));
    let l = a.menu_layer(80, 25);
    let drop = l.drop.as_ref().expect("展開了卻沒有下拉");
    let txt = screen_text(drop);
    for want in ["檢視", "編輯", "拷貝", "刪除"] {
        assert!(txt.contains(want), "「檔案」的下拉裡沒有 {want:?}\n{txt}");
    }
}

// 左右換分類,而且換過去的項目真的不一樣。
#[test]
fn menu_bar_switches_category() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F9));
    assert_eq!(a.menu.cat, 0, "F9 應該停在第一個分類");
    let first = a.menu.items[a.menu.cursor as usize].label.clone();
    a.handle_key(named(Code::Right));
    assert_eq!(a.menu.cat, 1, "→ 應該換到第二個分類");
    let got = a.menu.items[a.menu.cursor as usize].label.clone();
    assert_ne!(got, first, "換了分類但項目一樣");
    // 最左邊再往左要繞到最後一個分類。
    a.handle_key(named(Code::Left));
    a.handle_key(named(Code::Left));
    assert_eq!(a.menu.cat, menu_cats().len() as i32 - 1, "繞回去之後應該是最後一個分類");
}

// F9 再按一次收起來 —— 開合用同一顆鍵。
#[test]
fn menu_bar_toggles() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F9));
    a.handle_key(named(Code::F9));
    assert!(!a.menuing(), "第二次 F9 應該收起下拉");
}

#[test]
fn cjk24_menu_changes_selection() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    let items = cjk24_menu_items(&a);
    assert_eq!(items.len(), 7, "字形項目數");
    assert!(a.set_cjk24("guoqiao") && a.cjk24 == "guoqiao", "國喬選擇失敗: {:?}", a.cjk24);
    assert!(!a.set_cjk24("不存在") && a.cjk24 == "guoqiao", "未知選擇改壞設定: {:?}", a.cjk24);
}

// 下拉要貼在自己的分類標題底下,不是浮在畫面正中央。
#[test]
fn dropdown_aligns_to_category() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F9));
    let first = a.menu_layer(80, 25);
    a.handle_key(named(Code::Right)); // 換到「目錄/磁碟」
    let second = a.menu_layer(80, 25);

    assert!(first.drop.is_some() && second.drop.is_some(), "沒有下拉");
    // 換了分類,下拉就該往右移。
    assert!(second.drop_x > first.drop_x, "下拉沒有跟著分類移動:{} → {}", first.drop_x, second.drop_x);
    // 而且要對得上選單列上那個標題的位置。
    let bar = row_text(second.bar.as_ref().unwrap(), 0);
    assert!(bar.contains("檢視"), "選單列上找不到「檢視」");
}

// 下拉不能超出畫面右緣 —— 超出去的部分就是看不到的部分。
#[test]
fn dropdown_stays_on_screen() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F9));
    for _ in 0..4 {
        a.handle_key(named(Code::Right));
    }
    let l = a.menu_layer(40, 25); // 很窄的畫面
    let drop = l.drop.as_ref().expect("沒有下拉");
    assert!(l.drop_x + drop.cols <= 40, "下拉超出右緣:x={} w={} cols=40", l.drop_x, drop.cols);
    assert!(l.drop_x >= 0, "下拉跑到畫面外:x={}", l.drop_x);
}

// F1 開使用說明,而且說明裡真的有按鍵表。
#[test]
fn help_opens() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F1));
    assert_eq!(a.mode, Mode::Markdown, "F1 之後的模式");
    assert_eq!(a.md.name, crate::help::HELP_NAME);
    let mut s = Screen::new(80, 25);
    a.draw(&mut s);
    let txt = screen_text(&s);
    assert!(txt.contains("使用說明"), "說明畫面上沒有標題\n{txt}");
}

// 原版的選單是 `&1檔案` 到 `&8說明/…`,Alt-數字直接打開那一類。
// 下拉開著時按別的數字,換到那一類。
#[test]
fn menu_alt_digit_opens_category() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    assert!(
        a.handle_key(Key::alt_ch('7')) && a.menu.active && a.menu.cat == 6,
        "Alt-7 應該打開第七類(設定),現在 active={} cat={}",
        a.menu.active,
        a.menu.cat
    );
    assert!(
        a.handle_key(Key::alt_ch('2')) && a.menu.active && a.menu.cat == 1,
        "下拉開著時 Alt-2 應該換到第二類,現在 cat={}",
        a.menu.cat
    );
    // 超過分類數的數字不歸選單管。
    let td2 = TempDir::new();
    let mut b = new_at(td2.path());
    b.handle_key(Key::alt_ch('9'));
    assert!(!b.menu.active, "只有八類,Alt-9 不該打開選單");
}

/// 在目前的下拉裡找第一個子選單並進去。
fn enter_sub(a: &mut App) {
    let i = a.menu.items.iter().position(|it| it.sub.is_some()).expect("這一類沒有子選單");
    a.menu.cursor = i as i32;
    a.handle_key(named(Code::Enter));
    assert!(a.menu.back.is_some(), "沒有進到子選單");
}

// 單按 Alt:收著時打開第一類,開著時(即使在子選單裡)一次全部收起。
// Esc 只退一層,這是 Win32 選單的慣例。
#[test]
fn menu_key_alone_alt_toggles_all() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::MenuKey));
    assert!(a.menu.active && a.menu.cat == 0, "單按 Alt 應該打開第一類,現在 active={} cat={}", a.menu.active, a.menu.cat);
    a.handle_key(Key::alt_ch('7'));
    enter_sub(&mut a);
    a.handle_key(named(Code::Esc));
    assert!(a.menu.active && a.menu.back.is_none(), "子選單裡 Esc 應該只退回上一層");
    enter_sub(&mut a);
    assert!(a.handle_key(named(Code::MenuKey)) && !a.menu.active, "子選單裡單按 Alt 應該收起所有選單");
}

// 選了一個「什麼都沒改」的項目(字級已到頂),下拉照樣收起來,
// 而且要回報畫面有變 —— 否則外殼不重繪,畫面停在已經收起的下拉上(GitHub issue #10)。
#[test]
fn menu_no_op_item_still_redraws() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    (a.max_zoom, a.zoom) = (2, 2);
    a.handle_key(Key::alt_ch('7'));
    let i = a.menu.items.iter().position(|it| it.key == Key::ctrl_ch('+')).expect("設定裡找不到「放大字體」");
    a.menu.cursor = i as i32;
    assert!(a.handle_key(named(Code::Enter)), "字級已到頂時選「放大字體」,下拉收起來了卻回報沒變化");
    assert!(!a.menu.active, "下拉應該收起來");
    assert_eq!(a.zoom, 2, "字級不該變");
    // 滑鼠點下拉走同一條路。
    a.handle_key(Key::alt_ch('7'));
    let i = a.menu.items.iter().position(|it| it.key == Key::ctrl_ch('+')).unwrap();
    a.menu.cursor = i as i32;
    a.menu_layer(80, 25); // 下拉要畫過一次才知道裝得下幾列
    let row = a.menu.cursor - a.menu.top;
    assert!(a.menu_drop_click(row), "滑鼠點「放大字體」(已到頂)應該回報畫面有變");
}

/// 有子選單的項目(視窗大小、語言…)右邊不該出現按鍵欄。它們的 key 是零值,印出來是
/// U+0000,畫面上是一個缺字框。
#[test]
fn submenu_items_have_no_key_column() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 25));
    a.handle_key(Key::named(Code::F9));
    for _ in 0..6 {
        a.handle_key(Key::named(Code::Right));
    }
    let l = a.menu_layer(80, 25);
    let d = l.drop.as_ref().expect("設定選單沒展開");
    assert!(screen_text(d).contains("語言"), "展開的不是設定選單:\n{}", screen_text(d));
    for y in 0..d.rows {
        for x in 0..d.cols {
            if let Some(c) = d.at(x, y) {
                assert!(c.cont || c.ch != '\0', "第 {y} 列第 {x} 欄畫了 U+0000");
            }
        }
    }
}
