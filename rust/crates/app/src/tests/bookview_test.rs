//! 照搬 Go 版 `bookview_test.go` 的 EPUB 部分。PDF 的幾個測試等 PDF 檢視搬過來再搬。

use std::io::{Cursor, Write};

use super::*;
use crate::bookview::{book_url, parse_book_url};
use crate::Mode;

/// 造一本兩章的書,回傳它所在的目錄。
fn make_epub() -> TempDir {
    let td = TempDir::new();
    let files = [
        ("mimetype", "application/epub+zip"),
        (
            "META-INF/container.xml",
            r#"<?xml version="1.0"?>
<container xmlns="urn:oasis:names:tc:opendocument:xmlns:container" version="1.0">
<rootfiles><rootfile full-path="OEBPS/content.opf"
 media-type="application/oebps-package+xml"/></rootfiles></container>"#,
        ),
        (
            "OEBPS/content.opf",
            r#"<?xml version="1.0"?>
<package xmlns:opf="http://www.idpf.org/2007/opf"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata><dc:title>測試之書</dc:title><dc:creator>作者甲</dc:creator></metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>
    <item id="b" href="b.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx"><itemref idref="a"/><itemref idref="b"/></spine>
</package>"#,
        ),
        (
            "OEBPS/toc.ncx",
            r#"<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
<navPoint><navLabel><text>楔子</text></navLabel><content src="a.xhtml"/></navPoint>
<navPoint><navLabel><text>正文</text></navLabel><content src="b.xhtml"/></navPoint>
</navMap></ncx>"#,
        ),
        ("OEBPS/a.xhtml", "<html><body><h1>楔子</h1><p>這是楔子的內文。</p></body></html>"),
        ("OEBPS/b.xhtml", "<html><body><h1>正文</h1><p>這是正文的內文。</p></body></html>"),
    ];
    let mut zw = zip::ZipWriter::new(Cursor::new(Vec::new()));
    for (name, body) in files {
        zw.start_file(name, zip::write::SimpleFileOptions::default()).unwrap();
        zw.write_all(body.as_bytes()).unwrap();
    }
    write(td.path().join("book.epub"), zw.finish().unwrap().into_inner());
    td
}

fn book_app() -> (App, Screen, TempDir) {
    let td = make_epub();
    let mut a = new_at(td.path());
    (a.cell_w, a.cell_h) = (8, 16);
    let mut s = Screen::new(70, 20);
    a.draw(&mut s);
    a.focus_on("book.epub");
    (a, s, td)
}

/// 按 Enter 開 .epub 要看到目錄,不是壓縮檔的內容。
#[test]
fn enter_epub_shows_toc() {
    let (mut a, mut s, _td) = book_app();
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    let txt = screen_text(&s);
    for want in ["測試之書", "作者甲", "楔子", "正文"] {
        assert!(txt.contains(want), "目錄上沒有 {want:?}\n{txt}");
    }
    // [雷] EPUB 就是一個 zip。被當成壓縮檔進去的話會看到這些。
    for bad in ["META-INF", "content.opf", "mimetype"] {
        assert!(!txt.contains(bad), "被當成壓縮檔打開了(看到 {bad:?})\n{txt}");
    }
}

/// 點章節看內文,Backspace 回目錄。
#[test]
fn book_chapter_navigation() {
    let (mut a, mut s, _td) = book_app();
    a.handle_key(named(Code::Enter)); // 開書
    a.draw(&mut s);
    a.handle_key(named(Code::Enter)); // 第一個連結 = 第一章
    a.draw(&mut s);
    let txt = screen_text(&s);
    assert!(txt.contains("這是楔子的內文"), "沒進到第一章\n{txt}");
    // 章末的導覽:目錄與下一節。
    assert!(txt.contains("下一節"), "章末沒有下一節\n{txt}");

    a.handle_key(named(Code::Backspace));
    a.draw(&mut s);
    assert!(screen_text(&s).contains("測試之書"), "回不到目錄\n{}", screen_text(&s));
}

/// 位址的解析要對得起來,而且書的路徑本身含 # 也不能拆錯。
#[test]
fn book_url_round_trip() {
    for (path, ch) in [("/tmp/a.epub", -1), ("/tmp/a.epub", 0), ("/tmp/a.epub", 42), ("/tmp/怪#名字.epub", 3)] {
        let raw = book_url(path, ch);
        assert_eq!(parse_book_url(&raw), Some((path.to_string(), ch)), "{raw:?}");
    }
    assert_eq!(parse_book_url("gopher://x/1"), None, "gopher 位址不該被當成書");
}

/// 離開瀏覽模式要把書關掉,不然那個 zip 一直開著。
#[test]
fn esc_closes_book() {
    let (mut a, mut s, _td) = book_app();
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    assert!(a.book.is_some(), "書沒開起來");
    a.handle_key(named(Code::Esc));
    assert!(a.book.is_none(), "離開之後書還開著");
}

/// V 只在 PDF 上有意義。其他來源按了不該有反應,更不該當掉。
#[test]
fn page_image_only_for_pdf() {
    let (mut a, _s, _td) = book_app();
    a.handle_key(named(Code::Enter));
    let before = a.mode;
    a.handle_key(Key::ch('v'));
    assert_eq!(a.mode, before, "在書上按 V 不該換模式");
}

/// 關掉時停在書裡的哪一節,下次要開回同一節(對應 Go 版 session.go 的 browse 還原)。
#[test]
fn book_session_restores_chapter() {
    let (mut a, mut s, td) = book_app();
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    let st = a.snapshot();
    assert_eq!(st.mode, "browse");
    assert!(st.url.starts_with("epub:") && st.url.ends_with("#0"), "存下的位址是 {:?}", st.url);

    let mut b = new_at(td.path());
    (b.cell_w, b.cell_h) = (8, 16);
    b.restore(&st);
    assert_eq!(b.mode, Mode::Browse);
    b.draw(&mut s);
    assert!(screen_text(&s).contains("這是楔子的內文"), "沒有開回同一節\n{}", screen_text(&s));
}

/// 網頁的位址只記下來,不在啟動時就連出去。
#[test]
fn web_session_does_not_fetch() {
    let (mut a, _s, _td) = book_app();
    let st = wincv_session::State { mode: "browse".into(), url: "http://example.invalid/".into(), ..Default::default() };
    a.restore(&st);
    assert!(!a.busy(), "還原時不該連出去");
    assert_eq!(a.bv.url, "http://example.invalid/");
}
