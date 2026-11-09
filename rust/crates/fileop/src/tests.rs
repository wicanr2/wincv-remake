//! 照搬 Go 版 `internal/fileop` 的測試。

use super::*;
use std::path::PathBuf;

fn tmp(tag: &str) -> (PathBuf, PathBuf) {
    let d = std::env::temp_dir().join(format!("wincv-fileop-{tag}-{}", std::process::id()));
    let _ = fs::remove_dir_all(&d);
    let (src, dst) = (d.join("src"), d.join("dst"));
    fs::create_dir_all(src.join("sub/deep")).unwrap();
    fs::create_dir_all(&dst).unwrap();
    fs::write(src.join("a.txt"), "aaa").unwrap();
    fs::write(src.join("b.txt"), "bbb").unwrap();
    fs::write(src.join("sub/deep/c.txt"), "ccc").unwrap();
    (src, dst)
}

fn os(v: &[&str]) -> Vec<OsString> {
    v.iter().map(OsString::from).collect()
}

#[test]
fn copy_keeps_content_and_time() {
    let (src, dst) = tmp("copy");
    let t = std::time::SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(1_000_000_000);
    File::options().write(true).open(src.join("a.txt")).unwrap().set_modified(t).unwrap();
    let r = copy(&src, &dst, &os(&["a.txt", "b.txt"]), Options::default());
    assert!(!r.failed());
    assert_eq!(fs::read(dst.join("a.txt")).unwrap(), b"aaa");
    assert_eq!(fs::metadata(dst.join("a.txt")).unwrap().modified().unwrap(), t);
    assert!(!dst.join("a.txt.wincv-tmp").exists());
}

/// 沒人可問就當作不覆蓋:不要默默蓋掉使用者的檔案。
#[test]
fn overwrite_defaults_to_skip() {
    let (src, dst) = tmp("skip");
    fs::write(dst.join("a.txt"), "old").unwrap();
    let r = copy(&src, &dst, &os(&["a.txt"]), Options::default());
    assert_eq!(r.skipped, os(&["a.txt"]));
    assert_eq!(fs::read(dst.join("a.txt")).unwrap(), b"old");
}

/// 「全部覆蓋」之後不再問。
#[test]
fn overwrite_apply_to_all() {
    let (src, dst) = tmp("all");
    fs::write(dst.join("a.txt"), "old").unwrap();
    fs::write(dst.join("b.txt"), "old").unwrap();
    let mut asked = 0;
    let mut confirm = |_: &Path| {
        asked += 1;
        (true, true)
    };
    let r = copy(&src, &dst, &os(&["a.txt", "b.txt"]), Options { confirm: Some(&mut confirm), ..Default::default() });
    assert_eq!(r.done.len(), 2);
    assert_eq!(asked, 1);
    assert_eq!(fs::read(dst.join("b.txt")).unwrap(), b"bbb");
}

#[test]
fn move_and_copy_dir() {
    let (src, dst) = tmp("move");
    let r = move_to(&src, &dst, &os(&["a.txt", "sub"]), Options::default());
    assert!(!r.failed(), "{:?}", r.errors);
    assert!(!src.join("a.txt").exists() && dst.join("a.txt").exists());
    assert_eq!(fs::read(dst.join("sub/deep/c.txt")).unwrap(), b"ccc");
    assert!(copy(&src, &src, &os(&["b.txt"]), Options::default()).failed(), "來源與目的相同應該失敗");
}

#[test]
fn rename_rules() {
    let (src, _) = tmp("rename");
    assert!(rename(&src, OsStr::new("a.txt"), "b.txt").is_err(), "目的地已存在要拒絕");
    assert!(rename(&src, OsStr::new("a.txt"), "x/y").is_err());
    rename(&src, OsStr::new("a.txt"), "z.txt").unwrap();
    assert!(src.join("z.txt").exists());
}

#[test]
fn delete_zero_fill() {
    let (src, _) = tmp("delete");
    let r = delete(&src, &os(&["a.txt", "sub", "missing"]), Options { zero_fill: true, ..Default::default() });
    assert_eq!(r.done, os(&["a.txt", "sub"]));
    assert_eq!(r.errors.len(), 1);
    assert!(!src.join("sub").exists());
}

#[test]
fn compare_files() {
    let (src, _) = tmp("cmp");
    fs::write(src.join("x"), "hello world").unwrap();
    fs::write(src.join("y"), "hello world").unwrap();
    fs::write(src.join("z"), "hello").unwrap();
    fs::write(src.join("w"), "hellO world").unwrap();
    assert_eq!(compare(&src.join("x"), &src.join("y")).unwrap(), (true, None));
    assert_eq!(compare(&src.join("x"), &src.join("z")).unwrap(), (false, Some(5)));
    assert_eq!(compare(&src.join("x"), &src.join("w")).unwrap(), (false, Some(4)));
}

#[test]
fn summary_text() {
    let mut r = Outcome::default();
    r.done = os(&["a", "b"]);
    r.skipped = os(&["c"]);
    assert_eq!(r.summary("拷貝"), "拷貝 2 個檔案,跳過 1 個");
}
