//! 對應 Go 版 `internal/app/drivepane_test.go`。

use std::path::PathBuf;

use wincv_cell::Screen;
use wincv_keys::Code;
use wincv_vfs::Drive;

use super::*;
use crate::drivepane::has_path_prefix;

fn drive(label: &str, path: impl Into<PathBuf>) -> Drive {
    Drive { label: label.into(), path: path.into(), volume: false }
}

// Alt-D 開關磁碟窗格,而且開的時候游標要停在**目前所在的那個磁碟**上。
#[test]
fn drive_pane_toggle_and_focus() {
    let td = fixture();
    let mut a = new_at(td.path());
    let mut s = Screen::new(80, 20);
    a.draw(&mut s);

    assert!(a.handle_key(alt_rune('D')), "Alt-D 沒有被處理");
    assert!(
        a.browser.drive_pane != 0 && !a.browser.drives.is_empty(),
        "窗格沒開:pane={} drives={}",
        a.browser.drive_pane,
        a.browser.drives.len()
    );
    let cur = &a.browser.drives[a.browser.drive_cursor as usize];
    assert!(
        has_path_prefix(&a.browser.dir.to_string_lossy(), &cur.path.to_string_lossy()),
        "游標停在 {:?},但目前在 {:?}",
        cur.path,
        a.browser.dir
    );

    a.handle_key(alt_rune('D'));
    assert!(
        a.browser.drive_pane == 0 && a.browser.drives.is_empty() && !a.drive_focus,
        "再按一次沒有關掉"
    );
}

// Tab 把焦點交給窗格,Enter 切過去,Esc 交回來。
#[test]
fn drive_pane_navigation() {
    let td = TempDir::new();
    let root = td.path().to_path_buf();
    let sub = root.join("sub");
    std::fs::create_dir_all(&sub).unwrap();
    let mut a = new_at(&sub);
    a.draw(&mut Screen::new(80, 20));

    a.handle_key(alt_rune('D'));
    // 換成兩筆自己控制的,免得測試被這台機器的掛載點影響。
    a.browser.drives = vec![drive("root", &root), drive("sub", &sub)];
    a.browser.drive_cursor = 1;

    assert!(a.handle_key(named(Code::Tab)) && a.drive_focus, "Tab 沒有把焦點交給磁碟窗格");
    a.handle_key(named(Code::Up));
    assert_eq!(a.browser.drive_cursor, 0, "上鍵之後的游標");
    assert!(a.handle_key(named(Code::Enter)), "Enter 沒有被處理");
    assert_eq!(a.browser.dir, root, "沒有切過去");
    assert!(!a.drive_focus, "切過去之後焦點應該交還給清單");

    a.handle_key(named(Code::Tab));
    a.handle_key(named(Code::Esc));
    assert!(!a.drive_focus, "Esc 沒有把焦點交回清單");
}

// 焦點在窗格時,方向鍵不能同時動到檔案清單的游標 ——
// 兩邊一起動的話,按一次上鍵會有兩個地方在跑。
#[test]
fn drive_pane_keys_do_not_leak() {
    let td = fixture();
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 20));
    a.handle_key(alt_rune('D'));
    a.browser.drives = vec![drive("a", "/"), drive("b", "/")];
    a.browser.drive_cursor = 0;
    a.browser.cursor = 0;
    a.handle_key(named(Code::Tab));

    a.handle_key(named(Code::Down));
    assert_eq!(a.browser.cursor, 0, "檔案清單的游標也跟著動了");
    assert_eq!(a.browser.drive_cursor, 1, "磁碟窗格的游標沒動");
}

// 最長前綴比對:根目錄是每個路徑的前綴,先比到它就永遠選不到別的。
// 而且前綴必須停在路徑分隔上,否則 /media 會是 /mediafoo 的前綴。
#[test]
fn has_path_prefix_cases() {
    for (p, prefix, want) in [
        ("/media/usb/x", "/media/usb", true),
        ("/media/usb", "/media/usb", true),
        ("/mediafoo/x", "/media", false),
        ("/home/u", "/", true),
        (r"C:\a\b", r"C:\a", true),
        ("/a", "/ab", false),
    ] {
        assert_eq!(has_path_prefix(p, prefix), want, "has_path_prefix({p:?},{prefix:?})");
    }
}
