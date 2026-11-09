//! 照搬 Go 版 `internal/editor` 的測試。

use super::*;

fn p(line: usize, col: usize) -> Pos {
    Pos { line, col }
}

fn mk(lines: &[&str]) -> Model {
    Model::load("t", format!("{}\n", lines.join("\n")).as_bytes(), Enc::Ascii, None)
}

fn text(m: &Model) -> Vec<String> {
    m.lines.iter().map(|l| l.iter().collect()).collect()
}

fn mark(m: &mut Model, k: BlockKind, a: Pos, b: Pos) {
    m.move_to(a);
    m.mark_block(k);
    m.move_to(b);
    m.mark_block(k);
}

#[test]
fn insert_and_overwrite() {
    let mut m = mk(&["abc"]);
    m.move_to(p(0, 1));
    m.insert_char('X');
    assert_eq!(text(&m), ["aXbc"]);
    m.insert = false;
    m.move_to(p(0, 0));
    m.insert_char('Z');
    assert_eq!(text(&m), ["ZXbc"]);
}

#[test]
fn new_line_and_backspace_join() {
    let mut m = mk(&["abcd"]);
    m.move_to(p(0, 2));
    m.new_line();
    assert_eq!(text(&m), ["ab", "cd"]);
    assert_eq!(m.cur, p(1, 0));
    m.backspace();
    assert_eq!(text(&m), ["abcd"]);
    assert_eq!(m.cur, p(0, 2), "合併後游標應回到接點");
}

/// 矩形區塊:每一列取同一段欄位,不是連續文字。
#[test]
fn rect_block_copy() {
    let mut m = mk(&["abcdef", "ghijkl", "mnopqr"]);
    mark(&mut m, BlockKind::Rect, p(0, 1), p(2, 4));
    assert!(m.copy_block());
    assert_eq!(m.clipboard, ["bcd", "hij", "nop"]);
}

#[test]
fn rect_block_delete() {
    let mut m = mk(&["abcdef", "ghijkl"]);
    mark(&mut m, BlockKind::Rect, p(0, 1), p(1, 4));
    m.delete_block();
    assert_eq!(text(&m), ["aef", "gkl"]);
}

/// 矩形貼上是**插入**,右邊的字往後推:這是 PE2 的行為。
#[test]
fn rect_paste_inserts() {
    let mut m = mk(&["abc", "def"]);
    m.clipboard = vec!["12".into(), "34".into()];
    m.clip_kind = BlockKind::Rect;
    m.move_to(p(0, 1));
    m.paste_block();
    assert_eq!(text(&m), ["a12bc", "d34ef"]);
}

/// 「在每一行前面加上 5 個空白」:changelog 明講的用法。
#[test]
fn fill_block_insert_mode() {
    let mut m = mk(&["aaa", "bbb", "ccc"]);
    mark(&mut m, BlockKind::Rect, p(0, 0), p(2, 5));
    assert!(m.fill_block(' ', true));
    assert_eq!(text(&m), ["     aaa", "     bbb", "     ccc"]);
}

#[test]
fn fill_block_overwrite() {
    let mut m = mk(&["abcdef", "ghijkl"]);
    mark(&mut m, BlockKind::Rect, p(0, 1), p(1, 4));
    m.fill_block('.', false);
    assert_eq!(text(&m), ["a...ef", "g...kl"]);
}

/// 整列區塊刪掉的是整行,不是欄位範圍。
#[test]
fn line_block_delete() {
    let mut m = mk(&["one", "two", "three", "four"]);
    mark(&mut m, BlockKind::Line, p(1, 2), p(2, 0));
    m.delete_block();
    assert_eq!(text(&m), ["one", "four"]);
}

#[test]
fn line_block_paste() {
    let mut m = mk(&["one", "two"]);
    m.clipboard = vec!["X".into(), "Y".into()];
    m.clip_kind = BlockKind::Line;
    m.move_to(p(1, 0));
    m.paste_block();
    assert_eq!(text(&m), ["one", "X", "Y", "two"]);
}

#[test]
fn move_block_rect() {
    let mut m = mk(&["abcdef", "ghijkl"]);
    mark(&mut m, BlockKind::Rect, p(0, 0), p(1, 2));
    m.move_to(p(0, 4));
    assert!(m.move_block());
    assert_eq!(text(&m), ["cdabef", "ijghkl"]);
}

#[test]
fn undo() {
    let mut m = mk(&["abc"]);
    m.move_to(p(0, 0));
    m.insert_char('X');
    m.insert_char('Y');
    assert_eq!(text(&m), ["XYabc"]);
    m.undo();
    assert_eq!(text(&m), ["Xabc"]);
    m.undo();
    assert_eq!(text(&m), ["abc"]);
    assert!(!m.undo(), "沒東西可復原時應回 false");
}

#[test]
fn find_and_replace_all() {
    let mut m = mk(&["foo bar", "BAR foo", "baz"]);
    assert_eq!(m.find("bar", p(0, 0), false), Some(p(0, 4)));
    assert_eq!(m.find("bar", p(0, 5), false), Some(p(1, 0)));
    assert!(m.find("bar", p(0, 0), true).is_some());
    assert_eq!(m.replace_all("foo", "QUX", false), 2);
    assert_eq!(text(&m), ["QUX bar", "BAR QUX", "baz"]);
}

/// Tab 要對齊到 tab_width 的倍數,全形字算兩格。
#[test]
fn col_to_screen() {
    let m = mk(&[""]);
    let line: Vec<char> = "a\tb".chars().collect();
    assert_eq!(m.col_to_screen(&line, 1), 1);
    assert_eq!(m.col_to_screen(&line, 2), 8);
    let wide: Vec<char> = "中a".chars().collect();
    assert_eq!(m.col_to_screen(&wide, 1), 2);
    assert_eq!(m.screen_to_col(&wide, 1), 0, "點到全形字的後半格也算那個字");
}

/// 存回去要保留原本的編碼與換行樣式。
#[test]
fn round_trip_keeps_encoding_and_eol() {
    let src = b"\xa4\xa4\xa4\xe5\r\n\xa4\xa4\xa4\xe5\r\n"; // Big5「中文」兩行,CRLF
    let m = Model::load("t", src, Enc::Unknown, None);
    assert_eq!(m.enc, Enc::Big5);
    assert_eq!(m.eol, "\r\n");
    assert_eq!(m.bytes(), src);
}

/// UTF-16 存回 UTF-16(帶 BOM)。Go 版會寫成 UTF-8 位元組。
#[test]
fn utf16_round_trip() {
    let mut src = vec![0xFF, 0xFE];
    for u in "中a\n".encode_utf16() {
        src.extend_from_slice(&u.to_le_bytes());
    }
    let m = Model::load("t", &src, Enc::Unknown, None);
    assert_eq!(m.enc, Enc::Utf16Le);
    assert_eq!(m.bytes(), src);
}

/// 游標可以停在行尾之後(虛擬空白),在那裡打字才把中間補成空白。
#[test]
fn virtual_space() {
    let mut m = mk(&["ab"]);
    m.move_to(p(0, 6));
    assert_eq!(m.cur.col, 6);
    assert_eq!(text(&m), ["ab"], "只移動游標不該改內容");
    m.insert_char('X');
    assert_eq!(text(&m), ["ab    X"]);
    let mut m2 = mk(&["ab"]);
    m2.move_to(p(0, 5));
    m2.backspace();
    assert_eq!(m2.cur.col, 4, "在虛擬空白處退格應只移游標");
    assert_eq!(text(&m2), ["ab"]);
}

/// 在虛擬空白處斷行,不該產生一堆空白。
#[test]
fn new_line_in_virtual_space() {
    let mut m = mk(&["ab", "cd"]);
    m.move_to(p(0, 9));
    m.new_line();
    assert_eq!(text(&m), ["ab", "", "cd"]);
}

fn c_syntax() -> Option<Arc<Config>> {
    Some(Arc::new(Config { name: "c".into(), line_comment: "//".into(), ..Default::default() }))
}

#[test]
fn comment_lines_toggle() {
    let mut m = Model::load("a.c", b"int a;\nint b;\n\nint c;\n", Enc::Utf8, c_syntax());
    assert!(m.comment_lines(0, 3));
    assert_eq!(m.bytes(), b"// int a;\n// int b;\n\n// int c;\n");
    // 全部都已註解,再按一次是拿掉
    m.comment_lines(0, 3);
    assert_eq!(m.bytes(), b"int a;\nint b;\n\nint c;\n");
}

/// 半註解的區塊按一次,結果要是「全部註解」,不是各自反轉。
#[test]
fn comment_lines_partial_goes_all_on() {
    let mut m = Model::load("a.c", b"// int a;\nint b;\n", Enc::Utf8, c_syntax());
    m.comment_lines(0, 1);
    assert_eq!(m.bytes(), b"// // int a;\n// int b;\n");
}

/// 沒有語法設定就不要猜一個註解記號上去。
#[test]
fn comment_lines_no_config() {
    let mut m = Model::load("a.txt", b"hello\n", Enc::Utf8, None);
    assert!(!m.comment_lines(0, 0));
    assert_eq!(m.bytes(), b"hello\n");
}
