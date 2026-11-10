//! 照搬 Go 版 `docview_test.go`。

use std::io::{Cursor, Write};

use wincv_markdown::Block;

use super::*;
use crate::docview::{doc_url, parse_doc_url};
use crate::Mode;

/// 在 dir 底下寫一份最小的 .docx。
fn write_docx(dir: &std::path::Path, name: &str, body: &str) {
    let parts = [
        ("[Content_Types].xml", r#"<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/></Types>"#.to_string()),
        ("_rels/.rels", "<?xml version=\"1.0\"?><Relationships xmlns=\"http://schemas.openxmlformats.org/package/2006/relationships\">\n\t\t  <Relationship Id=\"r1\" Type=\"http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument\" Target=\"word/document.xml\"/></Relationships>".to_string()),
        ("word/document.xml", format!(r#"<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>{body}</w:body></w:document>"#)),
    ];
    let mut zw = zip::ZipWriter::new(Cursor::new(Vec::new()));
    for (k, v) in parts {
        zw.start_file(k, zip::write::SimpleFileOptions::default()).unwrap();
        zw.write_all(v.as_bytes()).unwrap();
    }
    write(dir.join(name), zw.finish().unwrap().into_inner());
}

/// 開一個以 dir 為起點的 App。
fn doc_app(dir: &std::path::Path) -> App {
    let mut a = new_at(dir);
    (a.cell_w, a.cell_h) = (8, 16);
    a.draw(&mut Screen::new(70, 20));
    a
}

fn blocks_text(bs: &[Block]) -> String {
    bs.iter().map(|b| b.spans.iter().map(|s| s.text.as_str()).collect::<String>() + "\n").collect()
}

/// [雷] .docx 是 zip。按 Enter 要看到文件,不是走進去看到 word/document.xml。
#[test]
fn enter_docx_opens_document() {
    let td = TempDir::new();
    write_docx(td.path(), "報告.docx", "<w:p><w:r><w:t>文件內容</w:t></w:r></w:p>");
    let mut a = doc_app(td.path());
    a.focus_on("報告.docx");
    assert!(a.enter(), "按 Enter 沒有反應");
    assert_eq!(a.mode, Mode::Browse, "應該進瀏覽模式(訊息:{})", a.message);
    let got = blocks_text(&a.bv.blocks);
    assert!(got.contains("文件內容"), "畫面上沒有文件內容:{got:?} / 狀態:{}", a.bv.status);
    // 只有一段的文件不需要導覽列。
    assert!(!got.contains("章節清單"), "單段文件不該有導覽:{got:?}");
}

#[test]
fn doc_url_round_trip() {
    for (path, part) in [("/tmp/a.docx", 0), ("/tmp/a.pptx", 3), ("/tmp/怪#名字.xlsx", 2)] {
        let raw = doc_url(path, part);
        assert_eq!(parse_doc_url(&raw), Some((path.to_string(), part)), "{raw:?}");
    }
}

/// 離開瀏覽模式要把文件關掉,不然檔案一直開著。
#[test]
fn esc_closes_document() {
    let td = TempDir::new();
    write_docx(td.path(), "a.docx", "<w:p><w:r><w:t>x</w:t></w:r></w:p>");
    let mut a = doc_app(td.path());
    a.focus_on("a.docx");
    a.enter();
    assert!(a.office.is_some(), "文件沒有開起來");
    a.handle_key(named(Code::Esc));
    assert!(a.office.is_none(), "離開瀏覽模式之後文件還開著");
}
