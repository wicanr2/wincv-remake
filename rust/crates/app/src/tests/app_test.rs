//! 對應 Go 版 `internal/app/app_test.go`。

use std::path::PathBuf;
use std::sync::Arc;

use wincv_cell::Screen;
use wincv_keys::{Code, Key};
use wincv_vfs::Fs;

use super::*;
use crate::{Mode, PREVIEW_ROWS};

#[test]
fn enter_dir_and_back() {
    let (mut a, mut s, _td) = new_app();
    let root = a.browser.dir.clone();

    walk_to(&mut a, &mut s, "sub");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    assert_eq!(a.browser.dir.file_name().unwrap(), "sub", "Enter 之後應在 sub,現在在 {:?}", a.browser.dir);

    a.handle_key(named(Code::Backspace));
    a.draw(&mut s);
    assert_eq!(a.browser.dir, root, "BackSpace 之後應回到 root");
    // 回上一層時游標要停在剛才離開的目錄上。
    assert_eq!(cursor_name(&a), "sub", "回上一層後游標應停在 sub");
}

#[test]
fn enter_text_file_opens_viewer() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "a.txt");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Viewer, "Enter 在文字檔上應進入檢視器");
    let v = a.viewer.as_ref().unwrap();
    assert!(v.name == "a.txt" && v.lines.len() == 3, "檢視器載入 {:?}, {} 行", v.name, v.lines.len());

    // Esc 是回上一層,不是離開程式。
    a.draw(&mut s);
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browser, "Esc 應回到瀏覽器");
    assert!(!a.quit, "Esc 不該讓程式結束");
}

#[test]
fn start_path_uses_normal_viewer_and_editor_paths() {
    let (mut a, _s, _td) = new_app();
    let root = a.browser.dir.clone();
    let text = root.join("sub").join("c.txt");

    a.start_path(&text, false).expect("StartPath 檢視檔案");
    assert_eq!(a.mode, Mode::Viewer, "StartPath 檢視應進文字檢視器");
    assert_eq!(a.viewer.as_ref().map(|v| v.name.as_str()), Some("c.txt"));
    assert_eq!(a.browser.dir, root.join("sub"), "StartPath 檢視應切到檔案所在目錄");

    a.start_path(&text, true).expect("StartPath 編輯檔案");
    assert_eq!(a.mode, Mode::Edit, "StartPath 編輯應進編輯器");
    assert_eq!(a.editor.as_ref().map(|e| e.name.as_str()), Some("c.txt"));
}

// 二進位檔按 Enter 直接開 16 進位檢視。
// 原版 0.5 版起就是這個行為(changelog:「按 enter 看檔時自動將可能為
// 執行檔的檔案以 16 進位方式看檔」),不是丟一個訊息就算了。
#[test]
fn enter_binary_file_opens_hex() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "z.bin");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Hex, "二進位檔應開 16 進位檢視");
    let h = a.hex.as_ref().expect("16 進位檢視沒開");
    assert_eq!(h.data.len(), 512, "16 進位檢視載入的資料不對");
    assert_eq!(h.lines(), 32, "512 bytes 應為 32 列");
    a.draw(&mut s);
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browser, "Esc 應回到瀏覽器");
}

// 文字檔可以用 H 切到 16 進位,再按 H 切回來。
#[test]
fn text_to_hex_and_back() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "a.txt");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    assert_eq!(a.mode, Mode::Viewer, "應先進文字檢視");
    a.handle_key(Key::ch('h'));
    assert_eq!(a.mode, Mode::Hex, "H 應切到 16 進位");
    let h = a.hex.as_ref().unwrap();
    assert!(
        h.name == "a.txt" && h.data.len() == "line1\nline2\nline3\n".len(),
        "16 進位載入的是 {:?}, {} bytes",
        h.name,
        h.data.len()
    );
    a.draw(&mut s);
    a.handle_key(Key::ch('h'));
    assert_eq!(a.mode, Mode::Viewer, "再按 H 應切回文字檢視");
}

#[test]
fn mark_keys() {
    let (mut a, mut s, _td) = new_app();
    a.handle_key(Key::ch(' '));
    a.draw(&mut s);
    assert_eq!(a.browser.marked_stats().0, 0, "游標在「..」上按 Space 不該標記到任何東西");

    a.handle_key(Key::ch('t'));
    // a.txt b.txt z.bin,不含 sub
    assert_eq!(a.browser.marked_stats().0, 3, "小寫 t 應只標記檔案");

    a.handle_key(Key::ch('u'));
    assert_eq!(a.browser.marked_stats().0, 0, "u 之後還有標記");

    a.handle_key(Key::alt_ch('T'));
    // 多一個 sub
    assert_eq!(a.browser.marked_stats().0, 4, "Alt-T 應連目錄一起標記");
}

#[test]
fn viewer_toggles() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "a.txt");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);

    assert!(!a.viewer.as_ref().unwrap().wrap, "預設不該開自動換行");
    a.handle_key(Key::ch('w'));
    assert!(a.viewer.as_ref().unwrap().wrap, "W 應切換自動換行");
    a.handle_key(Key::ch('w'));
    assert!(!a.viewer.as_ref().unwrap().wrap, "再按一次 W 應關掉自動換行");

    assert!(a.viewer.as_ref().unwrap().ansi, "預設應解讀 ANSI 色碼");
    a.handle_key(Key::ch('a'));
    assert!(!a.viewer.as_ref().unwrap().ansi, "A 應切換 ANSI");
}

// 按鍵不分大小寫 —— 原版的單鍵指令都是這樣。
#[test]
fn letter_case_insensitive() {
    for (k, want) in [
        (Key::ch('t'), Some('T')),
        (Key::ch('T'), Some('T')),
        (Key::ch('5'), None),
        (named(Code::Enter), None),
    ] {
        assert_eq!(k.letter(), want, "{k}.letter()");
    }
}

#[test]
fn key_string() {
    for (k, want) in [
        (Key::ch('C'), "C"),
        (Key::alt_ch('Z'), "Alt-Z"),
        (Key::ctrl_ch('N'), "Ctrl-N"),
        (named(Code::F11), "F11"),
        (named(Code::Backspace), "BackSpace"),
    ] {
        assert_eq!(k.to_string(), want);
    }
}

/// 在 dir 裡建一個 zip。files 是 (壓縮檔內路徑, 內容);先在旁邊的暫存目錄排好再打包,
/// 打包用 wincv_archive::create_zip(它對子目錄遞迴,只寫檔案、不寫目錄項目,
/// 與 Go 測試用 zip.Writer.Create 建出來的一樣)。
fn make_zip(zip_path: &std::path::Path, files: &[(&str, &str)]) {
    let stage = TempDir::new();
    let mut tops: Vec<PathBuf> = Vec::new();
    for (name, body) in files {
        let p = stage.path().join(name);
        std::fs::create_dir_all(p.parent().unwrap()).unwrap();
        write(&p, body);
        let top = PathBuf::from(name.split('/').next().unwrap());
        if !tops.contains(&top) {
            tops.push(top);
        }
    }
    wincv_archive::create_zip(zip_path, stage.path(), &tops, None).expect("建不了 zip");
}

// 壓縮檔要能像目錄一樣進出:進去、往下一層、退回來、退出壓縮檔,
// 而且退出後游標要停在那個壓縮檔上。
#[test]
fn enter_archive_like_directory() {
    let td = fixture();
    let root = td.path().to_path_buf();
    make_zip(&root.join("pack.zip"), &[("top.txt", "top\n"), ("docs/deep.txt", "deep\n")]);

    let mut a = new_at(&root);
    let mut s = Screen::new(78, 20);
    a.draw(&mut s);

    walk_to(&mut a, &mut s, "pack.zip");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);

    let got: Vec<String> = a.browser.entries.iter().map(|e| e.name.clone()).collect();
    assert_eq!(got.join(" "), ".. docs top.txt", "壓縮檔最上層");

    // 往下一層
    walk_to(&mut a, &mut s, "docs");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    assert_eq!(cursor_name(&a), "..", "進 docs 之後第一筆應是 ..");
    assert!(a.browser.entries.iter().any(|e| e.name == "deep.txt"), "docs/ 裡看不到 deep.txt");

    // 看壓縮檔裡的檔案
    walk_to(&mut a, &mut s, "deep.txt");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Viewer, "壓縮檔裡的文字檔應該打得開");
    assert_eq!(a.viewer.as_ref().unwrap().lines[0].text(), "deep");
    a.draw(&mut s);
    a.handle_key(named(Code::Esc));
    a.draw(&mut s);

    // 退回壓縮檔最上層
    a.handle_key(named(Code::Backspace));
    a.draw(&mut s);
    assert_eq!(cursor_name(&a), "docs", "退回上一層後游標應停在 docs");

    // 再退一次就離開壓縮檔
    a.handle_key(named(Code::Backspace));
    a.draw(&mut s);
    assert_eq!(a.browser.dir, root, "應該回到 root");
    assert_eq!(cursor_name(&a), "pack.zip", "離開壓縮檔後游標應停在 pack.zip");
}

// 還沒實作的格式要給訊息,不是默默什麼都不做。
#[test]
fn enter_unsupported_archive() {
    let td = fixture();
    write(td.path().join("x.ace"), "dummy");
    let mut a = new_at(td.path());
    let mut s = Screen::new(78, 20);
    a.draw(&mut s);
    walk_to(&mut a, &mut s, "x.ace");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Browser, "不支援的格式不該切換模式");
    assert!(a.message.contains("ACE"), "應該說明是什麼格式沒支援,得到 {:?}", a.message);
}

// R 改名:輸入列 → Enter → 檔案真的改名 → 游標停在新名字上。
#[test]
fn rename_flow() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "a.txt");
    let dir = a.browser.dir.clone();

    a.handle_key(Key::ch('r'));
    assert!(a.prompting(), "R 應該開輸入列");
    // 清掉預設值再輸入新名字
    for _ in 0..20 {
        a.handle_key(named(Code::Backspace));
    }
    type_in(&mut a, "new.txt");
    assert!(!a.prompting(), "Enter 之後輸入列應該關掉");
    assert!(dir.join("new.txt").exists(), "檔案沒改名");
    a.draw(&mut s);
    assert_eq!(cursor_name(&a), "new.txt", "游標應停在新名字上");
}

// Esc 取消輸入列,不可以動到檔案。
#[test]
fn prompt_cancel() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "a.txt");
    let dir = a.browser.dir.clone();
    a.handle_key(Key::ch('r'));
    a.handle_key(Key::ch('X'));
    a.handle_key(named(Code::Esc));
    assert!(!a.prompting(), "Esc 應該關掉輸入列");
    assert!(dir.join("a.txt").exists(), "取消之後原檔應該還在");
}

// D 刪除要先問過。答 N 不可以刪掉東西。
#[test]
fn delete_asks_first() {
    let (mut a, mut s, _td) = new_app();
    walk_to(&mut a, &mut s, "b.txt");
    let dir = a.browser.dir.clone();
    a.handle_key(Key::ch('d'));
    assert!(a.prompting(), "D 應該先問");
    a.handle_key(Key::ch('n'));
    assert!(dir.join("b.txt").exists(), "答 N 之後檔案不該被刪");

    a.handle_key(Key::ch('d'));
    a.handle_key(Key::ch('y'));
    assert!(!dir.join("b.txt").exists(), "答 Y 之後檔案應該被刪掉");
}

// Alt-C 比對:要剛好兩個標記。
#[test]
fn compare_needs_exactly_two() {
    let (mut a, mut s, _td) = new_app();
    a.handle_key(Key::alt_ch('C'));
    assert!(a.message.contains("兩個"), "沒標記時應該提示,得到 {:?}", a.message);

    // 造兩個內容相同的檔
    let dir = a.browser.dir.clone();
    write(dir.join("s1.txt"), "same");
    write(dir.join("s2.txt"), "same");
    a.browser.load(&dir).unwrap();
    a.draw(&mut s);
    let mut n = 0;
    for e in a.browser.entries.iter_mut() {
        if e.name.starts_with('s') && e.name.ends_with(".txt") {
            e.marked = true;
            n += 1;
        }
    }
    assert_eq!(n, 2, "標記數不對");
    a.handle_key(Key::alt_ch('C'));
    assert!(a.message.contains("相同"), "兩個相同的檔案應回報相同,得到 {:?}", a.message);
}

// 壓縮檔裡不能做檔案操作,要給明確訊息而不是默默失敗。
#[test]
fn file_ops_blocked_inside_archive() {
    let td = fixture();
    make_zip(&td.path().join("p.zip"), &[("x.txt", "x")]);

    let mut a = new_at(td.path());
    let mut s = Screen::new(78, 20);
    a.draw(&mut s);
    walk_to(&mut a, &mut s, "p.zip");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);

    for k in [Key::ch('c'), Key::ch('m'), Key::ch('r'), Key::ch('d')] {
        a.handle_key(k);
        if a.prompting() {
            a.handle_key(named(Code::Esc));
            panic!("{k} 在壓縮檔裡不該開輸入列");
        }
        assert!(a.message.contains("壓縮檔"), "{k} 應該說明壓縮檔裡不能做,得到 {:?}", a.message);
    }
}

// Z 解壓縮:游標停在壓縮檔上 → 輸入目的地 → 檔案真的解出來。
#[test]
fn extract_flow() {
    let td = fixture();
    let root = td.path().to_path_buf();
    make_zip(&root.join("pack.zip"), &[("top.txt", "top\n"), ("d/deep.txt", "deep\n")]);

    let mut a = new_at(&root);
    let mut s = Screen::new(78, 20);
    a.draw(&mut s);
    walk_to(&mut a, &mut s, "pack.zip");
    a.handle_key(Key::ch('z'));
    assert!(a.prompting(), "Z 應該問目的地");
    for _ in 0..200 {
        a.handle_key(named(Code::Backspace));
    }
    let out = root.join("unpacked");
    type_in(&mut a, &out.to_string_lossy());

    let got = std::fs::read(out.join("d").join("deep.txt"));
    assert!(matches!(&got, Ok(b) if b == b"deep\n"), "沒解出來: {got:?}");
    assert!(a.message.contains('2'), "訊息 = {:?}", a.message);
}

// Alt-Z 打包:標記檔案 → 輸入檔名 → zip 真的建出來且讀得回去。
#[test]
fn create_archive_flow() {
    let (mut a, mut s, _td) = new_app();
    let dir = a.browser.dir.clone();
    a.handle_key(Key::ch('t')); // 標記所有檔案
    a.draw(&mut s);

    a.handle_key(Key::alt_ch('Z'));
    assert!(a.prompting(), "Alt-Z 應該問檔名");
    for _ in 0..100 {
        a.handle_key(named(Code::Backspace));
    }
    type_in(&mut a, "out.zip");

    let zp = dir.join("out.zip");
    assert!(zp.exists(), "zip 沒建出來 (訊息 {:?})", a.message);
    let af = wincv_archive::ArchiveFs::open(&zp).expect("建出來的 zip 開不起來");
    let names: Vec<String> = af.read_dir(&af.root()).unwrap().into_iter().map(|e| e.name).collect();
    for want in ["a.txt", "b.txt", "z.bin"] {
        assert!(names.iter().any(|n| n == want), "zip 裡少了 {want} (有 {names:?})");
    }
}

// W 尋找:選種類 → 輸入關鍵字 → 結果清單 → Enter 跳到那個檔案。
#[test]
fn find_flow() {
    let (mut a, mut s, _td) = new_app();
    a.draw(&mut s);

    a.handle_key(Key::ch('w'));
    assert!(a.prompting(), "W 應該先問種類");
    a.handle_key(Key::ch('s')); // 找字串
    assert!(a.prompting(), "選了種類之後應該問關鍵字");
    type_in(&mut a, "line2");

    assert_eq!(a.mode, Mode::Find, "應該進結果清單(訊息 {:?})", a.message);
    let f = a.find.as_ref().unwrap();
    assert_eq!(f.hits.len(), 1, "命中筆數");
    let h = &f.hits[0];
    assert!(h.name == "a.txt" && h.line == 2, "命中 = {h:?}");
    a.draw(&mut s);
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Browser, "Enter 應該跳回瀏覽器");
    assert_eq!(cursor_name(&a), "a.txt", "游標應停在命中的檔案上");
}

// Ctrl-O 轉換:換行樣式真的被改掉。
#[test]
fn convert_eol_flow() {
    let (mut a, mut s, _td) = new_app();
    let dir = a.browser.dir.clone();
    walk_to(&mut a, &mut s, "a.txt");
    a.handle_key(Key::ctrl_ch('O'));
    assert!(a.prompting(), "Ctrl-O 應該開選單");
    a.handle_key(Key::ch('p')); // 轉 PC 換行
    let b = std::fs::read(dir.join("a.txt")).unwrap();
    assert!(String::from_utf8_lossy(&b).contains("\r\n"), "沒轉成 CRLF: {b:?}");

    a.handle_key(Key::ctrl_ch('O'));
    a.handle_key(Key::ch('u')); // 轉回 UNIX
    let b = std::fs::read(dir.join("a.txt")).unwrap();
    assert!(!b.contains(&b'\r'), "沒轉回 LF: {b:?}");
}

// Esc 要能從選單那一步退出來,不可以卡住。
#[test]
fn two_step_prompts_cancel() {
    let (mut a, mut s, _td) = new_app();
    a.draw(&mut s);
    a.handle_key(Key::ch('w'));
    a.handle_key(named(Code::Esc));
    assert!(!a.prompting(), "尋找的種類選單 Esc 不掉");
    a.handle_key(Key::ctrl_ch('O'));
    a.handle_key(named(Code::Esc));
    assert!(!a.prompting(), "轉換的選單 Esc 不掉");
    // Esc 之後一般按鍵要恢復正常
    assert!(a.handle_key(named(Code::Down)), "Esc 之後方向鍵應該又能用");
}

// --- F9 選單 / F1 說明 / F8 / F11 / Alt-E 註解 / P 改路徑 -----------------

#[test]
fn menu_opens_and_runs() {
    let td = TempDir::new();
    write(td.path().join("a.txt"), "hello\n");
    let mut a = new_at(td.path());
    a.handle_key(named(Code::F9));
    assert!(a.menuing(), "F9 應該打開選單");
    // 選單開著時,一般的瀏覽器按鍵不該漏過去
    a.handle_key(Key::ch('D'));
    assert!(!a.prompting(), "選單開著時 D 不該觸發刪除");
    a.handle_key(named(Code::Esc));
    assert!(!a.menuing(), "Esc 應該關掉選單");
}

// 選單裡直接按該功能的鍵,等同選它 —— 選單同時是說明書。
#[test]
fn menu_hotkey_passes_through() {
    let td = TempDir::new();
    write(td.path().join("a.txt"), "hello\n");
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 25));
    a.handle_key(named(Code::Down)); // 離開 ".."
    a.handle_key(named(Code::F9));
    a.handle_key(Key::ch('R')); // 更名
    assert!(!a.menuing(), "按了功能鍵之後選單應該關掉");
    assert!(a.prompting(), "R 應該開更名輸入列");
}

#[test]
fn fullscreen_toggle() {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    assert!(!a.fullscreen, "預設不該是全螢幕");
    a.handle_key(named(Code::F11));
    assert!(a.fullscreen, "F11 沒切成全螢幕");
    a.handle_key(named(Code::F11));
    assert!(!a.fullscreen, "F11 沒切回來");
}

// F8 關掉中文解讀之後,一個位元組畫一格:Big5 的「中」會變成兩格。
#[test]
fn english_only_splits_double_bytes() {
    let td = TempDir::new();
    write(td.path().join("a.txt"), [0xA4, 0xA4, 0xA4, 0xE5, b'\n']); // "中文"
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 25));
    a.handle_key(named(Code::Down));
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Viewer, "沒進檢視器");
    assert_eq!(a.viewer.as_ref().unwrap().lines[0].text(), "中文", "中文顯示");
    a.handle_key(named(Code::F8));
    let got: Vec<char> = a.viewer.as_ref().unwrap().lines[0].text().chars().collect();
    assert_eq!(got.len(), 4, "英文顯示應該是 4 格,拿到 {got:?}");
    // 位元組走字型自己的字碼表(CP437),不是 Latin-1 ——
    // 0xA4 在 CP437 是 ñ、0xE5 是 σ。
    assert!(got[0] == 'ñ' && got[3] == 'σ', "位元組沒有走 CP437: {got:?}");
}

#[test]
fn note_edit_flow() {
    let td = TempDir::new();
    let dir = td.path();
    write(dir.join("a.txt"), "x");
    let mut a = new_at(dir);
    a.draw(&mut Screen::new(80, 25));
    a.handle_key(named(Code::Down));

    a.handle_key(Key::alt_ch('E'));
    assert!(a.prompting(), "Alt-E 應該開註解輸入列");
    type_in(&mut a, "說明");

    let b = std::fs::read(dir.join("dir.doc")).expect("沒寫出 dir.doc");
    assert!(String::from_utf8_lossy(&b).contains("a.txt"), "dir.doc 內容不對: {b:?}");
    // 註解要立刻反映在瀏覽器上,不用重開
    assert_eq!(a.browser.notes.get("a.txt").map(String::as_str), Some("說明"), "Notes 沒更新");

    // 再按一次 Alt-E,輸入列要帶出現有的註解
    a.handle_key(Key::alt_ch('E'));
    a.handle_key(named(Code::Enter));
    assert_eq!(a.browser.notes.get("a.txt").map(String::as_str), Some("說明"), "原樣按 Enter 不該改掉註解");
}

#[test]
fn change_dir_to_file_focuses_it() {
    let td = TempDir::new();
    let sub = td.path().join("sub");
    std::fs::create_dir(&sub).unwrap();
    write(sub.join("target.txt"), "x");
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 25));

    a.handle_key(Key::ch('P'));
    assert!(a.prompting(), "P 應該開改路徑輸入列");
    // 輸入列預設帶目前路徑,先清掉
    for _ in 0..200 {
        a.handle_key(named(Code::Backspace));
    }
    type_in(&mut a, &sub.join("target.txt").to_string_lossy());

    assert_eq!(a.browser.dir, sub, "沒切到 sub");
    assert_eq!(cursor_name(&a), "target.txt", "游標沒停在 target.txt 上");
}

// --- 編輯器 F6 尋找/取代 -------------------------------------------------

fn open_ed(dir: &std::path::Path, name: &str, body: &str) -> App {
    write(dir.join(name), body);
    let mut a = new_at(dir);
    a.draw(&mut Screen::new(80, 25));
    a.handle_key(named(Code::Down)); // 離開 ".."
    a.handle_key(Key::ch('E'));
    assert_eq!(a.mode, Mode::Edit, "沒進編輯器");
    a
}

fn ed_bytes(a: &App) -> String {
    String::from_utf8(a.editor.as_ref().unwrap().bytes()).unwrap()
}

#[test]
fn edit_find_only() {
    let td = TempDir::new();
    let mut a = open_ed(td.path(), "a.txt", "one\ntwo\nthree two\n");
    a.handle_key(named(Code::F6));
    type_in(&mut a, "two"); // 尋找
    type_in(&mut a, ""); // 取代為:空 = 只尋找
    assert!(!a.prompting(), "只尋找不該再問確認");
    let c = a.editor.as_ref().unwrap().cur;
    assert!(c.line == 1 && c.col == 0, "游標沒停在第一處: {c:?}");
    a.handle_key(Key::ctrl_ch('N')); // 續找
    let c = a.editor.as_ref().unwrap().cur;
    assert!(c.line == 2 && c.col == 6, "Ctrl-N 沒跳到第二處: {c:?}");
    a.handle_key(Key::ctrl_ch('N'));
    assert!(a.message.contains("找不到"), "找完了應該說找不到,拿到 {:?}", a.message);
}

#[test]
fn edit_replace_one_by_one() {
    let td = TempDir::new();
    let mut a = open_ed(td.path(), "a.txt", "aa\naa\naa\n");
    a.handle_key(named(Code::F6));
    type_in(&mut a, "aa");
    type_in(&mut a, "bb");
    assert!(a.prompting(), "有取代字串時應該逐一問");
    a.handle_key(Key::ch('y')); // 換第一處
    a.handle_key(Key::ch('n')); // 跳過第二處
    a.handle_key(Key::ch('y')); // 換第三處
    assert_eq!(ed_bytes(&a), "bb\naa\nbb\n");
}

#[test]
fn edit_replace_all() {
    let td = TempDir::new();
    let mut a = open_ed(td.path(), "a.txt", "aa\naa\naa\n");
    a.handle_key(named(Code::F6));
    type_in(&mut a, "aa");
    type_in(&mut a, "bb");
    a.handle_key(Key::ch('a'));
    assert_eq!(ed_bytes(&a), "bb\nbb\nbb\n");
    assert!(a.message.contains('3'), "沒回報換了幾處: {:?}", a.message);
}

// 編輯器裡的 `;` 是一個可以打出來的字,不是指令 ——
// 「`;` 或 Alt-E 註解」那條證據在看圖段落,不適用於編輯器。
#[test]
fn semicolon_is_literal_in_editor() {
    let td = TempDir::new();
    let mut a = open_ed(td.path(), "a.c", "int a\n");
    a.handle_key(named(Code::End));
    a.handle_key(Key::ch(';'));
    assert_eq!(ed_bytes(&a), "int a;\n", "分號應該被打進去");
}

#[test]
fn editor_ctrl_e_comments() {
    let td = TempDir::new();
    let mut a = open_ed(td.path(), "a.c", "int a;\n");
    a.editor.as_mut().unwrap().syntax =
        Some(Arc::new(wincv_syntax::Config { name: "c".into(), line_comment: "//".into(), ..Default::default() }));
    a.handle_key(Key::ctrl_ch('E'));
    assert_eq!(ed_bytes(&a), "// int a;\n");
    a.handle_key(Key::ctrl_ch('E'));
    assert_eq!(ed_bytes(&a), "int a;\n", "再按一次沒拿掉");
}

// 游標停在目錄上時,Ctrl-O 不該開選單 —— 目錄讀不進來,
// 選單開了才發現一個都做不了。
#[test]
fn convert_skips_directories() {
    let td = TempDir::new();
    std::fs::create_dir(td.path().join("sub")).unwrap();
    let mut a = new_at(td.path());
    a.draw(&mut Screen::new(80, 25));
    a.handle_key(named(Code::Down)); // 停在 sub 上

    a.handle_key(Key::ctrl_ch('O'));
    assert!(!a.prompting(), "對著目錄不該開轉換選單");
    assert!(a.message.contains("目錄"), "應該說明原因,拿到 {:?}", a.message);
}

#[test]
fn printable_strips_control_bytes() {
    use crate::actions::printable;
    assert_eq!(printable("a\x00b\x1fc\td\x7fe", 100), "a.b.c d.e");
    assert_eq!(printable("abcdef", 3), "abc", "沒有截到 3 格");
}

// --- Alt-P 預視窗格 -------------------------------------------------------

#[test]
fn preview_toggle() {
    let td = TempDir::new();
    write(td.path().join("a.txt"), "第一行\n第二行\n第三行\n");
    let mut a = new_at(td.path());
    let mut s = Screen::new(80, 25);
    a.draw(&mut s);
    a.handle_key(named(Code::Down));

    assert!(!a.show_preview, "預設不該開預視");
    a.handle_key(Key::alt_ch('P'));
    assert!(a.show_preview, "Alt-P 沒打開預視");
    a.draw(&mut s);

    // 預視區在最底下 8 列,應該畫得出檔案的頭幾行
    let found = (s.rows - PREVIEW_ROWS..s.rows).any(|y| row_text(&s, y).contains("第一行"));
    assert!(found, "預視區沒有畫出檔案內容\n{}", screen_text(&s));

    a.handle_key(Key::alt_ch('P'));
    assert!(!a.show_preview, "再按一次沒關掉");
}

// 開了預視之後,列表要變短、狀態列要往上讓 —— 不能被預視蓋住。
#[test]
fn preview_shrinks_list() {
    let td = TempDir::new();
    for i in 0..40 {
        write(td.path().join(format!("f{i:02}.txt")), "x");
    }
    let mut a = new_at(td.path());
    let mut s = Screen::new(80, 25);
    a.handle_key(named(Code::Down)); // 讓狀態列顯示檔案而不是 ".."
    a.draw(&mut s);
    let before = count_rows(&s, "txt");
    a.handle_key(Key::alt_ch('P'));
    a.draw(&mut s);
    let after = count_rows(&s, "txt");
    // 預視區本身也可能出現 "txt"(它畫的是檔案內容),所以只比列表那一段
    assert!(after < before, "看得到的檔案列 {before} → {after},開了預視應該變少");
    // 狀態列(有檔名的那一列)不該落在預視區裡
    let status_y = s.rows - 1 - PREVIEW_ROWS;
    assert!(
        row_text(&s, status_y).contains("f00.txt"),
        "狀態列沒有移到預視區之上,第 {status_y} 列是 {:?}",
        row_text(&s, status_y)
    );
}

// 目錄與 `..` 沒有內容可預視,要給說明而不是空白或錯誤。
#[test]
fn preview_on_directory() {
    let td = TempDir::new();
    std::fs::create_dir(td.path().join("sub")).unwrap();
    let mut a = new_at(td.path());
    let mut s = Screen::new(80, 25);
    a.draw(&mut s);
    a.handle_key(Key::alt_ch('P'));
    a.handle_key(named(Code::Down)); // 停在 sub
    a.draw(&mut s);
    let got = row_text(&s, s.rows - PREVIEW_ROWS);
    assert!(got.contains("目錄"), "預視區應該說明這是目錄,得到 {got:?}");
}
