//! 移植自 Go 版 viewer_test.go。

use super::*;

fn texts(m: &Model) -> Vec<String> {
    m.lines.iter().map(Line::text).collect()
}

#[test]
fn line_splitting() {
    for (inp, want) in [
        ("a\nb\nc\n", vec!["a", "b", "c"]),
        ("a\r\nb\r\nc\r\n", vec!["a", "b", "c"]),
        ("a\rb\rc", vec!["a", "b", "c"]),
        ("a\nb", vec!["a", "b"]),
        ("a\n\nb\n", vec!["a", "", "b"]),
    ] {
        assert_eq!(texts(&Model::load("t", inp.as_bytes(), Enc::Ascii)), want, "{inp:?}");
    }
}

#[test]
fn ansi_bright_is_color_not_bold() {
    let m = Model::load("t", "\x1b[1;31m紅\x1b[0m白\n".as_bytes(), Enc::Utf8);
    let sp = &m.lines[0].spans;
    assert!(sp.len() >= 2);
    assert_eq!(sp[0].fg, Color::LT_RED);
    assert_eq!(sp[1].fg, Theme::default().fg);
    assert_eq!(m.lines[0].text(), "紅白");
}

#[test]
fn ansi_background() {
    let m = Model::load("t", b"\x1b[44mblue bg\x1b[0m\n", Enc::Ascii);
    assert_eq!(m.lines[0].spans[0].bg, Color::BLUE);
}

#[test]
fn ansi_off_strips_codes() {
    let raw = "\x1b[1;31m紅\x1b[0m白";
    let mut m = Model::load("t", raw.as_bytes(), Enc::Utf8);
    m.set_ansi(false, raw);
    assert_eq!(m.lines[0].text(), "紅白");
    assert_eq!(m.lines[0].spans.len(), 1);
}

#[test]
fn width_counts_wide_as_two() {
    assert_eq!(Model::load("t", "中a文".as_bytes(), Enc::Utf8).lines[0].width(), 5);
}

#[test]
fn search_wraps_and_ignores_case() {
    let mut m = Model::load("t", b"alpha\nBETA\ngamma\nbeta again\ndelta\n", Enc::Ascii);
    assert_eq!(m.search("beta", 3), 2);
    assert_eq!(m.hits, [1, 3]);
    m.next_hit(3);
    assert_eq!(m.hit_idx, 1);
    m.next_hit(3);
    assert_eq!(m.hit_idx, 0);
    m.prev_hit(3);
    assert_eq!(m.hit_idx, 1);
    assert_eq!(m.cur, 3, "命中會移動游標");
}

#[test]
fn scroll_clamp() {
    let mut m = Model::load("t", "x\n".repeat(100).as_bytes(), Enc::Ascii);
    m.scroll_by(-5, 10);
    assert_eq!(m.top, 0);
    m.scroll_by(9999, 10);
    assert_eq!(m.top, m.lines.len() as i32 - 1);
    m.home();
    assert_eq!((m.top, m.left), (0, 0));
}

#[test]
fn wrap_does_not_split_wide_char() {
    let mut m = Model::load("t", "aaa中文".as_bytes(), Enc::Utf8);
    m.wrap = true;
    let mut s = Screen::new(4, 5);
    m.draw(&mut s);
    assert_eq!(s.at(3, 0).unwrap().ch, ' ');
    let c = s.at(0, 1).unwrap();
    assert!(c.ch == '中' && c.wide);
}

#[test]
fn real_big5_file() {
    let Ok(b) = std::fs::read("../../../original/app/WinCV.txt") else {
        eprintln!("略過:沒有 WinCV.txt");
        return;
    };
    let m = Model::load("WinCV.txt", &b, Enc::Unknown);
    assert_eq!(m.enc, Enc::Big5);
    assert!(m.lines.len() >= 50);
    let all: String = texts(&m).concat();
    for want in ["WinCV", "版權聲明", "註冊", "發展環境"] {
        assert!(all.contains(want), "{want}");
    }
    assert!(!all.contains("\x1b["));
}

fn bar_model(n: usize) -> Model {
    let body: String = (1..=n).map(|i| format!("第 {i} 行\n")).collect();
    Model::load("t.txt", body.as_bytes(), Enc::Utf8)
}

#[test]
fn bar_paints_cursor_row_only() {
    let mut m = bar_model(10);
    assert!(m.bar);
    let mut s = Screen::new(20, 6);
    let rows = m.draw(&mut s);
    m.move_by(2, rows);
    let mut s = Screen::new(20, 6);
    m.draw(&mut s);
    for x in 0..s.cols {
        assert_eq!(s.at(x, 2).unwrap().bg, m.theme.bar_bg);
    }
    for y in [0, 1, 3, 4] {
        assert_ne!(s.at(0, y).unwrap().bg, m.theme.bar_bg);
    }
    m.bar = false;
    let mut s = Screen::new(20, 6);
    m.draw(&mut s);
    for y in 0..5 {
        assert!((0..s.cols).all(|x| s.at(x, y).unwrap().bg != m.theme.bar_bg));
    }
}

#[test]
fn cursor_scrolls_only_at_the_edge() {
    let mut m = bar_model(100);
    let rows = 5;
    for _ in 0..rows - 1 {
        m.move_by(1, rows);
        assert_eq!(m.top, 0);
    }
    m.move_by(1, rows);
    assert_eq!(m.top, 1);
    for _ in 0..4 {
        m.move_by(-1, rows);
    }
    assert_eq!((m.cur, m.top), (1, 1));
    m.move_by(-1, rows);
    assert_eq!(m.top, 0);
}

#[test]
fn page_keeps_cursor_offset() {
    let mut m = bar_model(100);
    m.page_by(1, 10);
    assert_eq!((m.top, m.cur), (10, 10));
}
