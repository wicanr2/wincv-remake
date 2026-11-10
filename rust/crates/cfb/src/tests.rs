//! 對應 Go 版 `cfb_test.go`,逐條照搬。

use super::*;

/// 借 doc97 的測試檔:那是 LibreOffice 產生的真實 Word 97 檔案,
/// 也就是一個真實的複合文件。自己組一個最小的容器只會驗到自己對
/// 格式的理解,而 FAT、迷你 FAT、目錄三者的互相指涉正是容易解錯的地方。
const SAMPLE: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../../../internal/doc97/testdata/rich.doc");

#[test]
fn streams() {
    let f = File::open(SAMPLE).unwrap();
    let names = f.names().join(" ");
    for want in ["WordDocument", "Root Entry"] {
        assert!(names.contains(want), "目錄裡少了 {want}:{names}");
    }
    let wd = f.stream("WordDocument").unwrap();
    assert!(wd.len() >= 512, "WordDocument 只讀到 {} 個位元組", wd.len());
    assert!(wd[0] == 0xEC && wd[1] == 0xA5, "WordDocument 開頭不是 Word 的識別碼:{:02x?}", &wd[..2]);
}

/// 小於門檻的串流走迷你配置表,大的走一般配置表。同一份檔案裡兩種都有。
#[test]
fn both_chain_kinds() {
    let f = File::open(SAMPLE).unwrap();
    let (mut small, mut large) = (0, 0);
    for n in f.names() {
        if !f.has(&n) {
            continue;
        }
        let b = match f.stream(&n) {
            Ok(b) => b,
            Err(e) => panic!("讀 {n} 失敗:{e}"),
        };
        if b.is_empty() {
            continue;
        }
        if (b.len() as u32) < f.mini_cutoff {
            small += 1;
        } else {
            large += 1;
        }
    }
    assert!(small != 0 && large != 0, "兩種鏈結沒有都走到(小 {small} 大 {large})");
}

#[test]
fn not_a_compound_file() {
    assert!(File::parse("x".repeat(1024).into_bytes()).is_err(), "簽章不符應該要失敗");
    assert!(File::parse(b"short".to_vec()).is_err(), "太短應該要失敗");
}
