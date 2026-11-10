//! 對應 Go 版 `officedoc_test.go`,逐條照搬。
//!
//! testdata 裡的三份檔案是 LibreOffice 產生的真實 Office 檔,
//! 重建方式見 internal/officedoc/testdata/README.md。用真檔的理由:自己組的最小檔案
//! 只驗得到自己對格式的理解,驗不到「真的 Office 檔長這樣」:
//! 版面配置的繼承、共用字串表、關聯的相對路徑都只在真檔上才會出現。

use wincv_markdown::Kind as BKind;

use super::*;

const TESTDATA: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../../../internal/officedoc/testdata/");

fn open_t(name: &str) -> Doc {
    open(format!("{TESTDATA}{name}")).unwrap()
}

fn text(bs: &[Block]) -> String {
    let mut sb = String::new();
    for b in bs {
        for s in &b.spans {
            sb.push_str(&s.text);
        }
        for r in &b.rows {
            sb.push_str(&r.join("|"));
        }
        sb.push('\n');
    }
    sb
}

#[test]
fn real_docx() {
    let d = open_t("rich.docx");
    assert!(d.kind == Kind::Word && d.parts.len() == 1, "kind={:?} parts={}", d.kind, d.parts.len());
    let got = text(&d.blocks(0));
    for want in ["第一章 標題", "粗體", "斜體", "條列一", "甲|乙", "ASCII paragraph"] {
        assert!(got.contains(want), "少了 {want:?}:\n{got}");
    }
    let bs = d.blocks(0);
    assert!(bs[0].kind == BKind::Heading && bs[0].level == 1, "標題沒認出來:{:?} level={}", bs[0].kind, bs[0].level);
}

#[test]
fn real_pptx() {
    let d = open_t("deck.pptx");
    assert_eq!(d.kind, Kind::Presentation);
    assert_eq!(d.parts.len(), 2, "要 2 張投影片:{:?}", d.parts);
    assert!(d.parts[0].title == "第一張投影片" && d.parts[1].title == "第二張投影片", "投影片標題:{:?}", d.parts);
    let got = text(&d.blocks(0));
    for want in ["第一張投影片", "條列一", "次層條列", "備忘稿", "這是講稿"] {
        assert!(got.contains(want), "第一張少了 {want:?}:\n{got}");
    }
    // 條列的階層要保留。
    let (mut lv1, mut lv2) = (false, false);
    for b in d.blocks(0) {
        if b.kind != BKind::List {
            continue;
        }
        match b.level {
            1 => lv1 = true,
            2 => lv2 = true,
            _ => {}
        }
    }
    assert!(lv1 && lv2, "條列階層沒保留(第一層 {lv1},第二層 {lv2})");
}

#[test]
fn real_xlsx() {
    let d = open_t("sheet.xlsx");
    assert!(d.kind == Kind::Spreadsheet && !d.parts.is_empty(), "kind={:?} parts={:?}", d.kind, d.parts);
    let mut rows = Vec::new();
    for b in d.blocks(0) {
        if b.kind == BKind::Table {
            rows = b.rows;
        }
    }
    assert_eq!(rows.len(), 3, "要 3 列:{rows:?}");
    assert!(rows[0][0] == "甲" && rows[1][0] == "1", "內容不對:{rows:?}");
    // 日期在檔案裡是一個天數,靠格式碼才知道它是日期。
    assert!(rows[1][2].starts_with("2023-03-15"), "日期沒有還原:{:?}", rows[1][2]);
}

/// 用 .doc 存的 RTF 在真實世界很常見。副檔名不保證內容。
#[test]
fn doc_extension_holding_rtf() {
    let dir = std::env::temp_dir().join(format!("wincv-officedoc-test-{}", std::process::id()));
    std::fs::create_dir_all(&dir).unwrap();
    let name = dir.join("fake.doc");
    std::fs::write(&name, "{\\rtf1\\ansi 其實是 RTF\\par}").unwrap();
    let d = open(&name).unwrap();
    let got = text(&d.blocks(0));
    d.close();
    let _ = std::fs::remove_dir_all(&dir);
    assert!(got.contains("RTF"), "內容是 {got:?}");
}

/// 這張表就是「支援哪些格式」的答案。加格式時要一起加進來。
#[test]
fn formats_table() {
    for &(ext, kind) in FORMATS {
        assert!(ext.starts_with('.') && ext == go_lower(ext), "副檔名要小寫並以點開頭:{ext:?}");
        assert!(is(&format!("x{ext}")), "is 不認得 {ext:?}");
        assert!(!kind.part_word().is_empty(), "{ext:?} 的分段稱呼是空的");
    }
    for no in ["a.txt", "a.pdf", "a.zip", "a", "a.docx.bak"] {
        assert!(!is(no), "is 不該認 {no:?}");
    }
}
