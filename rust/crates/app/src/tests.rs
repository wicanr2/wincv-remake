//! 照搬 Go 版 `internal/app` 的單元測試。
//!
//! 每個子模組對應一個 Go 測試檔(`app_test.go` → `app_test`,以此類推)。
//! 依賴還沒搬過來的模式(網路瀏覽、EPUB、PDF、Office)的測試還沒有搬。

mod app_test;
mod docpos_test;
mod mdview_test;
mod mouse_test;
mod touch_test;
mod drivepane_test;
mod layout_test;
mod menu_test;
mod session_test;
mod zoom_test;

use std::path::{Path, PathBuf};
use std::sync::atomic::{AtomicUsize, Ordering};

use wincv_cell::Screen;
use wincv_keys::{Code, Key};

use crate::App;

/// 測試用的暫存目錄,離開範圍時整個刪掉(對應 Go 的 `t.TempDir()`)。
pub(super) struct TempDir(PathBuf);

impl TempDir {
    pub(super) fn new() -> TempDir {
        static SEQ: AtomicUsize = AtomicUsize::new(0);
        let nanos = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .map(|d| d.as_nanos())
            .unwrap_or(0);
        let name = format!(
            "wincv-app-test-{}-{}-{}",
            std::process::id(),
            SEQ.fetch_add(1, Ordering::Relaxed),
            nanos
        );
        let p = std::env::temp_dir().join(name);
        std::fs::create_dir_all(&p).expect("建不了暫存目錄");
        TempDir(p)
    }

    pub(super) fn path(&self) -> &Path {
        &self.0
    }
}

impl Drop for TempDir {
    fn drop(&mut self) {
        let _ = std::fs::remove_dir_all(&self.0);
    }
}

pub(super) fn write(p: impl AsRef<Path>, data: impl AsRef<[u8]>) {
    std::fs::write(p.as_ref(), data).unwrap_or_else(|e| panic!("寫不進 {}: {e}", p.as_ref().display()));
}

/// 用真實檔案系統的暫存目錄,因為 app 層要驗的正是「進出目錄、開檔」這些真的會碰
/// 檔案系統的行為。
pub(super) fn fixture() -> TempDir {
    let td = TempDir::new();
    let root = td.path();
    std::fs::create_dir_all(root.join("sub")).unwrap();
    write(root.join("a.txt"), "line1\nline2\nline3\n");
    write(root.join("b.txt"), "bbb\n");
    write(root.join("sub").join("c.txt"), "ccc\n");
    // 一個真的二進位檔
    let bin: Vec<u8> = (0..512).map(|i| (i % 5) as u8).collect();
    write(root.join("z.bin"), bin);
    td
}

pub(super) fn new_at(dir: &Path) -> App {
    App::new(Box::new(wincv_vfs::Os), dir)
}

/// fixture 加上一個已經畫過一次的 App(先畫一次讓 rows 有值)。TempDir 要留著,
/// 不然目錄會在測試途中被刪掉。
pub(super) fn new_app() -> (App, Screen, TempDir) {
    let td = fixture();
    let mut a = new_at(td.path());
    let mut s = Screen::new(78, 20);
    a.draw(&mut s);
    (a, s, td)
}

pub(super) fn cursor_name(a: &App) -> String {
    a.browser.current().map(|e| e.name.clone()).unwrap_or_default()
}

pub(super) fn named(c: Code) -> Key {
    Key::named(c)
}

/// 往下走到指定名稱那一筆。走不到(Down 回報沒變)就讓測試失敗。
pub(super) fn walk_to(a: &mut App, s: &mut Screen, name: &str) {
    for _ in 0..1000 {
        if cursor_name(a) == name {
            return;
        }
        if !a.handle_key(named(Code::Down)) {
            break;
        }
        a.draw(s);
    }
    assert_eq!(cursor_name(a), name, "找不到 {name}");
}

/// 一次打進一串字。
pub(super) fn type_str(a: &mut App, s: &str) {
    for r in s.chars() {
        a.handle_key(Key::ch(r));
    }
}

/// 打一串字再按 Enter。
pub(super) fn type_in(a: &mut App, s: &str) {
    type_str(a, s);
    a.handle_key(named(Code::Enter));
}

pub(super) fn row_text(s: &Screen, y: i32) -> String {
    let mut b = String::new();
    for x in 0..s.cols {
        if let Some(c) = s.at(x, y) {
            if c.cont {
                continue;
            }
            b.push(if c.ch == '\0' { ' ' } else { c.ch });
        }
    }
    b
}

pub(super) fn screen_text(s: &Screen) -> String {
    (0..s.rows).map(|y| row_text(s, y) + "\n").collect()
}

/// 數畫面上有幾列含有 sub。
pub(super) fn count_rows(s: &Screen, sub: &str) -> usize {
    (0..s.rows).filter(|&y| row_text(s, y).contains(sub)).count()
}

pub(super) fn alt_rune(r: char) -> Key {
    Key::alt_ch(r)
}

pub(super) fn ctrl_rune(r: char) -> Key {
    Key::ctrl_ch(r)
}
