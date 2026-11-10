//! 對應 Go 版 `internal/app/docpos_test.go`。

use wincv_cell::Screen;
use wincv_keys::{Code, Key};

use super::*;
use crate::Mode;

/// 建一個目錄,裡面一份 200 行的文字檔。
fn pos_app() -> (App, TempDir) {
    let td = TempDir::new();
    write(td.path().join("long.txt"), "line\n".repeat(200));
    let mut a = new_at(td.path());
    a.now = Some(|| 1);
    a.draw(&mut Screen::new(80, 25));
    (a, td)
}

fn open_named(a: &mut App, name: &str, k: Key) {
    if let Some(i) = a.browser.entries.iter().rposition(|e| e.name == name) {
        a.browser.move_to(i as i32, 20);
    }
    a.handle_key(k);
    a.draw(&mut Screen::new(80, 25));
}

// 看檔捲到某處、離開、再開同一檔,要回到同一處。
#[test]
fn viewer_position_recalled() {
    let (mut a, _td) = pos_app();
    open_named(&mut a, "long.txt", named(Code::Enter));
    assert_eq!(a.mode, Mode::Viewer);
    a.handle_key(named(Code::PgDn));
    a.handle_key(named(Code::PgDn));
    let want = a.viewer.as_ref().unwrap().top;
    assert_ne!(want, 0, "PgDn 兩次後 Top 還是 0");
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browser, "Esc 後的模式");
    open_named(&mut a, "long.txt", named(Code::Enter));
    assert_eq!(a.viewer.as_ref().unwrap().top, want, "重開後 Top");
}

// 編輯器記的是游標(行、欄)與捲動位置。
#[test]
fn editor_position_recalled() {
    let (mut a, _td) = pos_app();
    open_named(&mut a, "long.txt", Key::ch('E'));
    assert_eq!(a.mode, Mode::Edit);
    for _ in 0..50 {
        a.handle_key(named(Code::Down));
    }
    a.handle_key(named(Code::Right));
    let c = a.editor.as_ref().unwrap().cur;
    a.handle_key(named(Code::Esc));
    open_named(&mut a, "long.txt", Key::ch('E'));
    let got = a.editor.as_ref().unwrap().cur;
    assert!(got.line == c.line && got.col == c.col, "重開後游標 {got:?},預期 {c:?}");
}

// 位置表要跟著 session 走:Snapshot 帶出去、Restore 進另一個 App 之後
// 開同一檔仍然回到同一處。檔案變短時夾回範圍,不會是空白畫面。
#[test]
fn positions_survive_session() {
    let (mut a, td) = pos_app();
    let dir = td.path();
    open_named(&mut a, "long.txt", named(Code::Enter));
    a.handle_key(named(Code::PgDn));
    let want = a.viewer.as_ref().unwrap().top;
    a.handle_key(named(Code::Esc));
    let st = a.snapshot();
    assert_eq!(st.positions.len(), 1, "Snapshot 的位置表筆數");

    let mut b = new_at(dir);
    b.restore(&st);
    b.draw(&mut Screen::new(80, 25));
    open_named(&mut b, "long.txt", named(Code::Enter));
    assert_eq!(b.viewer.as_ref().unwrap().top, want, "另一個 App 重開後 Top");
    b.handle_key(named(Code::Esc));

    // 檔案縮成 3 行:位置要夾回去。
    write(dir.join("long.txt"), "a\nb\nc\n");
    open_named(&mut b, "long.txt", named(Code::Enter));
    assert_eq!(b.viewer.as_ref().unwrap().top, 0, "檔案變短後 Top");
}

// 說明不是檔案,不進位置表。
#[test]
fn help_not_remembered() {
    let (mut a, _td) = pos_app();
    a.handle_key(named(Code::F1));
    a.handle_key(named(Code::PgDn));
    assert!(a.positions.is_empty(), "說明被記進位置表:{:?}", a.positions);
}
