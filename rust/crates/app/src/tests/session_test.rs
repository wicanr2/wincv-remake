//! 對應 Go 版 `internal/app/session_test.go`。

use wincv_cell::Screen;
use wincv_keys::Code;
use wincv_session::State;

use super::*;
use crate::Mode;

fn new_screen() -> Screen {
    Screen::new(80, 25)
}

// 關掉時記下目錄與游標,再開起來要回到同一個地方。
#[test]
fn snapshot_restore_dir_and_cursor() {
    let td = TempDir::new();
    let dir = td.path();
    for n in ["a.txt", "b.txt", "c.txt"] {
        write(dir.join(n), "內容\n");
    }
    let mut a = new_at(dir);
    a.draw(&mut new_screen());
    // 走到 b.txt
    while cursor_name(&a) != "b.txt" {
        assert!(a.handle_key(named(Code::Down)), "走不到 b.txt");
    }
    let st = a.snapshot();
    assert!(st.dir == dir.to_string_lossy() && st.cursor == "b.txt", "快照是 {st:?}");

    let td2 = TempDir::new();
    let mut b = new_at(td2.path());
    b.draw(&mut new_screen());
    b.restore(&st);
    assert_eq!(b.browser.dir, dir, "沒回到原目錄");
    assert_eq!(cursor_name(&b), "b.txt", "游標沒回到 b.txt");
}

// 開著的文件與捲動位置也要回得去。
#[test]
fn snapshot_restore_open_file() {
    let td = TempDir::new();
    let dir = td.path();
    write(dir.join("long.txt"), "這是第幾列\n".repeat(200));

    let mut a = new_at(dir);
    a.draw(&mut new_screen());
    a.focus_on("long.txt");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Viewer, "沒進檢視模式");
    a.draw(&mut new_screen());
    for _ in 0..3 {
        a.handle_key(named(Code::PgDn));
    }
    let st = a.snapshot();
    assert!(st.mode == "viewer" && st.file == "long.txt" && st.top != 0, "快照是 {st:?}");

    let td2 = TempDir::new();
    let mut b = new_at(td2.path());
    b.draw(&mut new_screen());
    b.restore(&st);
    assert_eq!(b.mode, Mode::Viewer, "沒回到檢視模式");
    let v = b.viewer.as_ref().expect("開的不是同一個檔");
    assert_eq!(v.name, "long.txt", "開的不是同一個檔");
    assert_eq!(v.top, st.top, "捲動位置");
}

// [雷] 上次看的那個檔案很可能正是這次被刪掉的那一個。
// 還原是錦上添花,不能讓程式開不起來。
#[test]
fn restore_survives_missing_targets() {
    let td = TempDir::new();
    let dir = td.path().to_string_lossy().into_owned();
    let mut a = new_at(td.path());
    a.draw(&mut new_screen());
    let st = |d: &str, mode: &str, file: &str, top: i32| State {
        dir: d.into(),
        mode: mode.into(),
        file: file.into(),
        top,
        ..Default::default()
    };
    for s in [
        st("/沒有這個目錄", "viewer", "x.txt", 0),
        st(&dir, "viewer", "不存在.txt", 999),
        st(&dir, "edit", "不存在.txt", 0),
        st(&dir, "image", "不存在.png", 0),
        st(&dir, "認不得的模式", "x", 0),
        State::default(),
    ] {
        let r = std::panic::catch_unwind(std::panic::AssertUnwindSafe(|| a.restore(&s)));
        assert!(r.is_ok(), "{s:?} 讓程式炸了");
    }
}

// 檔案在兩次執行之間變短了,捲動位置要夾回去 ——
// 不夾的話畫面是一片空白,而使用者只會覺得檔案不見了。
#[test]
fn restore_clamps_scroll() {
    let td = TempDir::new();
    write(td.path().join("short.txt"), "一\n二\n");
    let mut a = new_at(td.path());
    a.draw(&mut new_screen());
    a.restore(&State {
        dir: td.path().to_string_lossy().into_owned(),
        mode: "viewer".into(),
        file: "short.txt".into(),
        top: 900,
        ..Default::default()
    });
    let v = a.viewer.as_ref().expect("檔案沒開起來");
    assert_eq!(v.top, 0, "捲動位置沒夾回去");
}

// 壓縮檔內部的位置存了也回不去,要退到它外面那一層。
#[test]
fn snapshot_leaves_archive() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.draw(&mut new_screen());
    let st = a.snapshot();
    assert!(!st.dir.contains('!'), "存了壓縮檔內部的路徑:{:?}", st.dir);
}

// 說明是內嵌的,不是檔案 —— 存下來下次會找不到那個「檔名」。
//
// [過渡] Rust 版的 F1 暫時用文字檢視器開說明(見 menu_test::help_opens),
// 這條斷言對 viewer 與 markdown 兩種情形都成立。
#[test]
fn snapshot_skips_help() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F1));
    let st = a.snapshot();
    assert!(st.file != "使用說明" && st.mode != "markdown", "把說明存進去了:{st:?}");
    assert!(st.mode.is_empty() && st.file.is_empty(), "說明不該留下模式或檔名:{st:?}");
}

// 畫面設定也要回得去。
#[test]
fn snapshot_restore_display() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.max_zoom = 2;
    (a.zoom, a.scale, a.menu_bar) = (2, 1.5, false);
    let st = a.snapshot();

    let td2 = TempDir::new();
    let mut b = new_at(td2.path());
    b.max_zoom = 2;
    b.restore(&st);
    assert!(
        b.zoom == 2 && b.scale == 1.5 && !b.menu_bar,
        "還原後 Zoom={} Scale={} MenuBar={}",
        b.zoom,
        b.scale,
        b.menu_bar
    );
}

#[test]
fn snapshot_restore_cjk24() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.cjk24 = "eten-b".into();
    let st = a.snapshot();
    assert_eq!(st.cjk24, "eten-b", "快照沒有字形代號");
    let td2 = TempDir::new();
    let mut b = new_at(td2.path());
    b.restore(&st);
    assert_eq!(b.cjk24, "eten-b", "沒有還原字形代號");
    b.restore(&State { cjk24: "未知".into(), ..Default::default() });
    assert_eq!(b.cjk24, "eten-b", "未知字形代號不應覆寫既有設定");
}

// 視窗的格數要記住:存的是整個畫面的欄列數,不是內容區的列數,
// 也不能只在縮圖模式才有值(GitHub issue #7)。
#[test]
fn snapshot_keeps_window_size() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(120, 40));
    let st = a.snapshot();
    assert!(st.cols == 120 && st.rows == 40, "120×40 的畫面存成 {}×{}", st.cols, st.rows);
}
