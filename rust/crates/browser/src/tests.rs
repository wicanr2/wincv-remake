//! 移植自 Go 版 browser_test.go。

use super::*;
use std::collections::HashMap;
use std::io;
use std::time::{Duration, UNIX_EPOCH};

/// 不碰真實檔案系統的 Fs。
struct Fake(HashMap<PathBuf, Vec<vfs::Entry>>);

impl Fs for Fake {
    fn read_dir(&self, dir: &Path) -> io::Result<Vec<vfs::Entry>> {
        Ok(self.0.get(dir).cloned().unwrap_or_default())
    }
    fn open(&self, _: &Path) -> io::Result<Box<dyn io::Read>> {
        Err(io::ErrorKind::Unsupported.into())
    }
    fn label(&self, dir: &Path) -> String {
        dir.display().to_string()
    }
}

fn e(name: &str, size: i64, is_dir: bool, secs: u64) -> vfs::Entry {
    vfs::Entry { size, mod_time: Some(UNIX_EPOCH + Duration::from_secs(secs)), ..vfs::Entry::named(name, is_dir) }
}

fn sample() -> Fake {
    let mut m = HashMap::new();
    m.insert(
        PathBuf::from("/w"),
        vec![
            e("bszip.dll", 31232, false, 984_996_000),
            e("docs", 0, true, 1_293_868_800),
            e("7-zip32.dll", 228864, false, 1_096_632_000),
            e("CAB32.DLL", 82432, false, 916_650_000),
            e("big52gbk.txt", 69865, false, 1_035_198_000),
            e("sub", 0, true, 1_273_035_900),
        ],
    );
    Fake(m)
}

fn names(m: &Model) -> Vec<String> {
    m.entries.iter().map(|e| e.name.clone()).collect()
}

fn row(s: &Screen, y: i32) -> String {
    (0..s.cols).filter_map(|x| s.at(x, y)).filter(|c| !c.cont).map(|c| c.ch).collect()
}

#[test]
fn sort_dirs_first_case_insensitive() {
    let m = Model::new(Box::new(sample()), Path::new("/w"));
    assert_eq!(names(&m), ["..", "docs", "sub", "7-zip32.dll", "big52gbk.txt", "bszip.dll", "CAB32.DLL"]);
}

#[test]
fn sort_by_size_keeps_dirs_first() {
    let mut m = Model::new(Box::new(sample()), Path::new("/w"));
    m.sort_key = SortKey::Size;
    m.resort();
    let n = names(&m);
    assert_eq!(&n[..4], ["..", "docs", "sub", "bszip.dll"]);
}

#[test]
fn resort_keeps_cursor_and_marks() {
    let mut m = Model::new(Box::new(sample()), Path::new("/w"));
    m.move_to(4, 20);
    assert_eq!(m.current().unwrap().name, "big52gbk.txt");
    m.current_mut().unwrap().marked = true;
    m.sort_key = SortKey::Size;
    m.resort();
    assert_eq!(m.current().unwrap().name, "big52gbk.txt");
    assert!(m.current().unwrap().marked);
}

#[test]
fn up_entry_cannot_be_marked() {
    let mut m = Model::new(Box::new(sample()), Path::new("/w"));
    m.move_to(0, 20);
    m.toggle_mark(20);
    assert!(!m.entries[0].marked);
    assert_eq!(m.cursor, 1);
}

#[test]
fn mark_all() {
    let mut m = Model::new(Box::new(sample()), Path::new("/w"));
    m.mark_all(false);
    for e in &m.entries {
        assert!(!(e.up && e.marked));
        assert_eq!(e.marked, !e.is_dir, "{}", e.name);
    }
    assert_eq!(m.marked_stats(), (4, 31232 + 228864 + 82432 + 69865));
    m.mark_all(true);
    assert!(m.entries.iter().all(|e| e.up || e.marked));
    m.unmark_all();
    assert_eq!(m.marked_stats().0, 0);
}

#[test]
fn scrolling() {
    let mut es = Vec::new();
    for i in 0..50u8 {
        es.push(vfs::Entry::named(&format!("{}{}.txt", (b'a' + i % 26) as char, (b'0' + i / 26) as char), false));
    }
    let mut m = Model::new(Box::new(Fake(HashMap::from([(PathBuf::from("/w"), es)]))), Path::new("/w"));
    let rows = 10;
    m.move_to(0, rows);
    assert_eq!(m.top, 0);
    m.move_to(9, rows);
    assert_eq!(m.top, 0);
    m.move_to(10, rows);
    assert_eq!(m.top, 1);
    m.end(rows);
    assert_eq!(m.cursor, m.entries.len() as i32 - 1);
    assert_eq!(m.top, m.entries.len() as i32 - rows);
    m.move_by(999, rows);
    assert_eq!(m.cursor, m.entries.len() as i32 - 1);
    m.move_by(-999, rows);
    assert_eq!((m.cursor, m.top), (0, 0));
}

#[test]
fn comma_cases() {
    for (v, want) in [(0, "0"), (7, "7"), (999, "999"), (1000, "1,000"), (228864, "228,864"), (17366865, "17,366,865"), (-1234, "-1,234")] {
        assert_eq!(comma(v), want);
    }
}

#[test]
fn truncate_wide() {
    assert_eq!(truncate("中文字", 5), "中文");
    assert_eq!(truncate("a中b", 2), "a");
}

#[test]
fn cursor_row_keeps_mark_column_and_date_colour() {
    let mut m = Model::new(Box::new(sample()), Path::new("/w"));
    let mut s = Screen::new(80, 12);
    m.move_to(2, 10);
    m.draw(&mut s);
    let y = 1 + (m.cursor - m.top);
    assert_eq!(s.at(0, y).unwrap().bg, m.theme.mark_col_bg);
    for x in 1..s.cols - 1 {
        assert_eq!(s.at(x, y).unwrap().bg, m.theme.cursor_bg, "第 {x} 格");
    }
    assert!(row(&s, y).contains("sub"));
    let fgs: Vec<Color> = (1..s.cols - 1).map(|x| s.at(x, y).unwrap().fg).collect();
    assert!(fgs.contains(&m.theme.date_fg), "日期被游標色蓋掉了");
    assert!(fgs.contains(&m.theme.cursor_fg));
}

#[test]
fn resize_name() {
    let es = vec![vfs::Entry { size: 1, ..vfs::Entry::named("a-very-long-filename.txt", false) }];
    let mut m = Model::new(Box::new(Fake(HashMap::from([(PathBuf::from("/w"), es)]))), Path::new("/w"));
    m.move_to(m.entries.len() as i32 - 1, 4);
    let mut s = Screen::new(80, 6);
    m.draw(&mut s);
    assert!(row(&s, 2).contains("a-very-l "), "{}", row(&s, 2));
    assert!(m.resize_name(12));
    m.draw(&mut s);
    assert!(row(&s, 2).contains("a-very-long-filename txt"), "{}", row(&s, 2));
    m.resize_name(-100);
    assert_eq!(m.name_w(), MIN_NAME_W);
    assert!(!m.resize_name(-1));
}
