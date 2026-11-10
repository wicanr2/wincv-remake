//! 照搬 Go 版 `mdview_test.go`。

use super::*;
use crate::Mode;

/// 測試用的目錄:一份有圖的 .md、一份試圖讀目錄外檔案的 .md。
///
/// 圖用 PGM 而不是 Go 測試的 PNG:app 不依賴 PNG 編碼器,而這裡要驗的是「圖有沒有變成
/// overlay」,格式無關。
fn md_fixture() -> TempDir {
    let td = TempDir::new();
    let root = td.path();
    let mut pgm = b"P5\n64 32\n255\n".to_vec();
    pgm.extend(std::iter::repeat_n(128u8, 64 * 32));
    write(root.join("pic.pgm"), pgm);
    std::fs::create_dir_all(root.join("sub")).unwrap();
    write(root.join("secret.txt"), "x");
    // 內文要夠長才折得出差別:短文字在寬窄兩種版面下列數相同,那樣的測試看起來會通過,
    // 但它什麼都沒驗到。
    let long = "這是一段夠長的內文，用來確認換欄寬時會重新折行。".repeat(4);
    write(root.join("a.md"), format!("# 標題\n\n{long}\n\n![圖](pic.pgm)\n\n結尾\n"));
    write(root.join("esc.md"), "![壞](../../../etc/passwd)\n\n![絕對](/etc/passwd)\n");
    td
}

fn open_md(root: &std::path::Path, name: &str) -> (App, Screen) {
    let mut a = new_at(root);
    let mut s = Screen::new(60, 20);
    a.draw(&mut s);
    walk_to(&mut a, &mut s, name);
    a.handle_key(named(Code::Enter));
    (a, s)
}

/// .md 要走排版模式,不是純文字檢視。
#[test]
fn markdown_mode_opens() {
    let td = md_fixture();
    let (mut a, mut s) = open_md(td.path(), "a.md");
    assert_eq!(a.mode, Mode::Markdown);
    let ov = a.draw(&mut s);
    assert_eq!(ov.len(), 1, "內嵌圖片沒有變成 overlay");
    assert!(ov[0].w > 0 && ov[0].h > 0, "overlay 矩形是空的");
    assert!(screen_text(&s).contains("標題"), "畫面上沒有標題:\n{}", screen_text(&s));
}

/// 一份來路不明的 .md 不該有辦法讓程式去讀文件目錄之外的檔案。
#[test]
fn markdown_image_stays_in_dir() {
    let td = md_fixture();
    let (a, _) = open_md(td.path(), "esc.md");
    assert_eq!(a.mode, Mode::Markdown);
    for src in ["../../../etc/passwd", "/etc/passwd"] {
        assert!(a.md_image(src).is_err(), "{src:?} 竟然讀得到");
    }
}

/// 捲動:PgDn / End 之後不能跑出範圍,Esc 要退回檔案清單。
#[test]
fn markdown_scroll_and_exit() {
    let td = md_fixture();
    let (mut a, mut s) = open_md(td.path(), "a.md");
    a.draw(&mut s);
    a.handle_key(named(Code::End));
    a.draw(&mut s);
    let n = a.md.lines.as_ref().map_or(0, |l| l.len()) as i32;
    assert!(a.md.top >= 0 && a.md.top <= n, "End 之後 top = {},總列數 {n}", a.md.top);
    a.handle_key(named(Code::Home));
    assert_eq!(a.md.top, 0);
    a.handle_key(named(Code::Up));
    assert_eq!(a.md.top, 0, "在最上面按上鍵跑掉了");
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browser);
}

/// 從 markdown 進看圖模式,Esc 要退回 markdown 而不是檔案清單。
#[test]
fn markdown_enter_picture_then_back() {
    let td = md_fixture();
    let (mut a, mut s) = open_md(td.path(), "a.md");
    a.draw(&mut s);
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Image);
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Markdown, "Esc 應該退回 markdown");
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browser, "再按一次 Esc 應該回檔案清單");
}

/// 視窗變窄要重排。不重排的話文字會被畫到畫面外。
#[test]
fn markdown_relayout_on_resize() {
    let td = md_fixture();
    let (mut a, _) = open_md(td.path(), "a.md");
    let mut wide = Screen::new(80, 20);
    a.draw(&mut wide);
    let n_wide = a.md.lines.as_ref().unwrap().len();
    let mut narrow = Screen::new(24, 20);
    a.draw(&mut narrow);
    assert_ne!(a.md.lines.as_ref().unwrap().len(), n_wide, "變窄之後列數沒變,沒有重排");
    for y in 0..narrow.rows {
        let w = row_text(&narrow, y).trim_end().chars().count();
        assert!(w <= 24, "第 {y} 列寬 {w},超出畫面");
    }
}
