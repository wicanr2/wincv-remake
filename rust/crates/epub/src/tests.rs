//! 對應 Go 版 `epub_test.go`,逐條照搬。

use std::io::{Cursor, Write};

use wincv_markdown::Kind;

use crate::*;

/// 產生一本 EPUB(記憶體裡)。鍵是 zip 內的路徑。
fn build(files: &[(&str, &[u8])]) -> Vec<u8> {
    let mut zw = zip::ZipWriter::new(Cursor::new(Vec::new()));
    for (name, body) in files {
        zw.start_file(*name, zip::write::SimpleFileOptions::default())
            .unwrap();
        zw.write_all(body).unwrap();
    }
    zw.finish().unwrap().into_inner()
}

/// 寫到暫存檔,走 [`open`] 那條路。
fn build_file(files: &[(&str, &[u8])]) -> (std::path::PathBuf, Cleanup) {
    use std::sync::atomic::{AtomicUsize, Ordering};
    static N: AtomicUsize = AtomicUsize::new(0);
    let p = std::env::temp_dir().join(format!(
        "wincv-epub-{}-{}.epub",
        std::process::id(),
        N.fetch_add(1, Ordering::SeqCst)
    ));
    std::fs::write(&p, build(files)).unwrap();
    (p.clone(), Cleanup(p))
}

struct Cleanup(std::path::PathBuf);
impl Drop for Cleanup {
    fn drop(&mut self) {
        let _ = std::fs::remove_file(&self.0);
    }
}

const CONTAINER_XML: &str = r#"<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf"
     media-type="application/oebps-package+xml"/></rootfiles>
</container>"#;

// 與 Go 版的測試資料相同:照真書那樣一次宣告好幾個 xmlns。
// (寬鬆模式把 OPF 解成空的,實際的觸發條件是有內容的 `<meta>`,
// 見 lib.rs 的 unmarshal 與 goxml 的 lenient_meta_autoclose_cascades。)
const CONTENT_OPF: &str = r#"<?xml version="1.0"?>
<package xmlns:opf="http://www.idpf.org/2007/opf"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:dcterms="http://purl.org/dc/terms/"
         xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
         xmlns="http://www.idpf.org/2007/opf" version="2.0" unique-identifier="id">
  <metadata>
    <dc:title>測試書</dc:title>
    <dc:creator>某某</dc:creator>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="img" href="images/p.png" media-type="image/png"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="c1"/>
    <itemref idref="c2"/>
  </spine>
</package>"#;

const TOC_NCX: &str = r#"<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1">
  <navMap>
    <navPoint id="n1" playOrder="1">
      <navLabel><text>第一章 開始</text></navLabel>
      <content src="ch1.xhtml"/>
    </navPoint>
    <navPoint id="n2" playOrder="2">
      <navLabel><text>第二章 結束</text></navLabel>
      <content src="ch2.xhtml#top"/>
    </navPoint>
  </navMap>
</ncx>"#;

const CH1: &str = r#"<html><body><h1>開始</h1>
			<p>第一章的內文。</p><img src="images/p.png" alt="插圖"/></body></html>"#;
const CH2: &str = "<html><body><h1>結束</h1><p>第二章的內文。</p></body></html>";
const PNG: &[u8] = b"\x89PNG\r\n\x1a\n\xe5\x81\x87\xe7\x9a\x84";

fn sample() -> (std::path::PathBuf, Cleanup) {
    build_file(&[
        ("mimetype", b"application/epub+zip"),
        ("META-INF/container.xml", CONTAINER_XML.as_bytes()),
        ("OEBPS/content.opf", CONTENT_OPF.as_bytes()),
        ("OEBPS/toc.ncx", TOC_NCX.as_bytes()),
        ("OEBPS/ch1.xhtml", CH1.as_bytes()),
        ("OEBPS/ch2.xhtml", CH2.as_bytes()),
        ("OEBPS/images/p.png", PNG),
    ])
}

#[test]
fn open_reads_metadata_and_spine() {
    let (p, _c) = sample();
    let b = open(p.to_str().unwrap()).unwrap();
    assert!(
        b.title == "測試書" && b.author == "某某",
        "書名 {:?} 作者 {:?}",
        b.title,
        b.author
    );
    assert_eq!(b.chapters.len(), 2);
    // 章節標題取自 NCX,而且 #片段 要去掉才對得上檔案。
    assert_eq!(
        (b.chapters[0].title.as_str(), b.chapters[1].title.as_str()),
        ("第一章 開始", "第二章 結束")
    );
}

#[test]
fn blocks_and_images() {
    let (p, _c) = sample();
    let b = open(p.to_str().unwrap()).unwrap();
    let blocks = b.blocks(0).unwrap();
    let mut text = String::new();
    let mut img_src = String::new();
    for blk in &blocks {
        for sp in &blk.spans {
            text.push_str(&sp.text);
        }
        if blk.kind == Kind::Image {
            img_src = blk.src.clone();
        }
    }
    assert!(text.contains("第一章的內文"), "內文是 {text:?}");
    // 圖片的相對路徑要接成 zip 內的路徑,否則讀不到。
    assert!(!img_src.is_empty(), "沒有解出圖片");
    let data = b
        .image(&img_src)
        .unwrap_or_else(|e| panic!("讀不到圖 {img_src:?}: {e}"));
    assert!(data.starts_with(b"\x89PNG"), "讀出來的不是那張圖");
}

// [雷] OPF 寫 Text/ch1.xhtml、zip 裡是 text/ch1.xhtml 這種事很常見。
// 區分大小寫的話整本書一章都讀不出來。
#[test]
fn paths_are_case_insensitive() {
    let opf = CONTENT_OPF.replacen(r#"href="ch1.xhtml""#, r#"href="Ch1.XHTML""#, 1);
    let (p, _c) = build_file(&[
        ("META-INF/container.xml", CONTAINER_XML.as_bytes()),
        ("OEBPS/content.opf", opf.as_bytes()),
        ("OEBPS/toc.ncx", TOC_NCX.as_bytes()),
        (
            "OEBPS/ch1.xhtml",
            "<html><body><p>大小寫不同也要讀得到</p></body></html>".as_bytes(),
        ),
        (
            "OEBPS/ch2.xhtml",
            "<html><body><p>二</p></body></html>".as_bytes(),
        ),
    ]);
    let b = open(p.to_str().unwrap()).unwrap();
    assert!(!b.blocks(0).unwrap().is_empty(), "第一章是空的");
}

// container.xml 壞掉或不見時退回掃一個 .opf:
// 讀不了整本書和少一個索引檔是完全不同的嚴重程度。
#[test]
fn survives_broken_container() {
    for c in [
        "",
        "<container>壞掉的",
        "<container><rootfiles/></container>",
    ] {
        let (p, _c) = build_file(&[
            ("META-INF/container.xml", c.as_bytes()),
            ("OEBPS/content.opf", CONTENT_OPF.as_bytes()),
            ("OEBPS/toc.ncx", TOC_NCX.as_bytes()),
            (
                "OEBPS/ch1.xhtml",
                "<html><body><p>一</p></body></html>".as_bytes(),
            ),
            (
                "OEBPS/ch2.xhtml",
                "<html><body><p>二</p></body></html>".as_bytes(),
            ),
        ]);
        let b = open(p.to_str().unwrap()).unwrap_or_else(|e| panic!("container={c:?} 打不開:{e}"));
        assert_eq!(b.chapters.len(), 2, "container={c:?}");
    }
}

// EPUB 3 用 nav.xhtml 當目錄,沒有 NCX。
#[test]
fn epub3_nav() {
    let opf3 = r#"<?xml version="1.0"?>
<package xmlns:opf="http://www.idpf.org/2007/opf"
         xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:dcterms="http://purl.org/dc/terms/"
         xmlns="http://www.idpf.org/2007/opf" version="3.0">
  <metadata><dc:title>三版</dc:title></metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine><itemref idref="c1"/></spine>
</package>"#;
    let (p, _c) = build_file(&[
        ("META-INF/container.xml", CONTAINER_XML.as_bytes()),
        ("OEBPS/content.opf", opf3.as_bytes()),
        (
            "OEBPS/nav.xhtml",
            r#"<html><body><nav epub:type="toc"><ol>
			<li><a href="ch1.xhtml">序章</a></li></ol></nav></body></html>"#
                .as_bytes(),
        ),
        (
            "OEBPS/ch1.xhtml",
            "<html><body><p>內文</p></body></html>".as_bytes(),
        ),
    ]);
    let b = open(p.to_str().unwrap()).unwrap();
    assert_eq!(b.title, "三版");
    assert!(
        b.chapters.len() == 1 && b.chapters[0].title == "序章",
        "章節是 {:?}",
        b.chapters
    );
}

// 不是 EPUB 的東西要明確報錯,不能裝作讀得懂。
#[test]
fn rejects_non_epub() {
    assert!(
        open_bytes(build(&[("a.txt", "這只是個 zip".as_bytes())])).is_err(),
        "普通的 zip 不該被當成 EPUB"
    );
    let bad = std::env::temp_dir().join(format!("wincv-epub-bad-{}.epub", std::process::id()));
    std::fs::write(&bad, "根本不是 zip").unwrap();
    let _c = Cleanup(bad.clone());
    assert!(open(bad.to_str().unwrap()).is_err(), "壞檔不該打得開");
}

// 記憶體裡的一份也要打得開(壓縮檔內部的 .epub 走這條)。
#[test]
fn open_bytes_works() {
    let (p, _c) = sample();
    let b = open_bytes(std::fs::read(&p).unwrap()).unwrap();
    assert_eq!(b.chapters.len(), 2);
}

// 沒有目錄的章節依序編號。
#[test]
fn untitled_chapters_are_numbered() {
    let opf = CONTENT_OPF
        .replace(r#"<spine toc="ncx">"#, "<spine>")
        .replace(
            r#"<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>"#,
            "",
        );
    let (p, _c) = build_file(&[
        ("META-INF/container.xml", CONTAINER_XML.as_bytes()),
        ("OEBPS/content.opf", opf.as_bytes()),
        ("OEBPS/ch1.xhtml", CH1.as_bytes()),
        ("OEBPS/ch2.xhtml", CH2.as_bytes()),
    ]);
    let b = open(p.to_str().unwrap()).unwrap();
    assert_eq!(b.chapters[1].title, "第 2 節");
}

fn big5(s: &str) -> Vec<u8> {
    wincv_textenc::encode(s, wincv_textenc::Enc::Big5).unwrap()
}

fn utf16le(s: &str) -> Vec<u8> {
    wincv_textenc::encode(s, wincv_textenc::Enc::Utf16Le).unwrap()
}

// 舊的中文電子書常把 OPF 與 NCX 寫成 Big5,並在 XML 宣告裡寫 encoding="big5"。
// encoding/xml 沒有 CharsetReader 時整份拒絕,書就打不開。
// UTF-16(有 BOM)是 EPUB 規格允許的另一種編碼,也要打得開。
#[test]
fn open_non_utf8_opf() {
    let decl = r#"<?xml version="1.0" encoding="big5"?>"#;
    let plain = r#"<?xml version="1.0"?>"#;
    let cases: [(&str, Vec<u8>, Vec<u8>); 2] = [
        (
            "big5",
            big5(&CONTENT_OPF.replacen(plain, decl, 1)),
            big5(&TOC_NCX.replacen(plain, decl, 1)),
        ),
        (
            "utf-16",
            utf16le(&CONTENT_OPF.replacen(plain, r#"<?xml version="1.0" encoding="UTF-16"?>"#, 1)),
            TOC_NCX.as_bytes().to_vec(),
        ),
    ];
    for (name, opf, ncx) in cases {
        let data = build(&[
            ("META-INF/container.xml", CONTAINER_XML.as_bytes()),
            ("OEBPS/content.opf", &opf),
            ("OEBPS/toc.ncx", &ncx),
            ("OEBPS/ch1.xhtml", "<p>一</p>".as_bytes()),
            ("OEBPS/ch2.xhtml", "<p>二</p>".as_bytes()),
        ]);
        let b = open_bytes(data).unwrap_or_else(|e| panic!("{name}: {e}"));
        assert_eq!(
            (b.title.as_str(), b.author.as_str()),
            ("測試書", "某某"),
            "{name}"
        );
        assert_eq!(b.chapters.len(), 2, "{name}");
        assert_eq!(b.chapters[0].title, "第一章 開始", "{name}");
    }
}

// 章節 XHTML 的編碼照 BOM、XML 宣告、meta charset 的順序決定,都沒有就判讀。
// BOM 不能留在內文裡:留著的話會多出一段只有 U+FEFF 的空段落。
#[test]
fn chapter_encoding() {
    let body = "<html><body><h1>開始</h1><p>第一章的內文。</p></body></html>";
    let mut bom = b"\xef\xbb\xbf".to_vec();
    bom.extend_from_slice(body.as_bytes());
    let cases: Vec<(&str, Vec<u8>)> = vec![
        ("utf-8 BOM", bom),
        ("utf-16 BOM", utf16le(body)),
        (
            "XML 宣告",
            big5(&format!(r#"<?xml version="1.0" encoding="Big5"?>{body}"#)),
        ),
        (
            "meta",
            big5(&format!(
                r#"<html><head><meta http-equiv="Content-Type" content="text/html; charset=big5"/></head>{body}"#
            )),
        ),
        ("判讀", big5(body)),
        // 宣告寫錯(內容其實是 UTF-8)時不能照宣告解成亂碼。
        (
            "宣告錯",
            format!(r#"<?xml version="1.0" encoding="big5"?>{body}"#).into_bytes(),
        ),
    ];
    for (name, ch) in cases {
        let data = build(&[
            ("META-INF/container.xml", CONTAINER_XML.as_bytes()),
            ("OEBPS/content.opf", CONTENT_OPF.as_bytes()),
            ("OEBPS/toc.ncx", TOC_NCX.as_bytes()),
            ("OEBPS/ch1.xhtml", &ch),
            ("OEBPS/ch2.xhtml", "<p>二</p>".as_bytes()),
        ]);
        let b = open_bytes(data).unwrap_or_else(|e| panic!("{name}: {e}"));
        let got: Vec<String> = b
            .blocks(0)
            .unwrap()
            .iter()
            .map(|blk| blk.spans.iter().map(|s| s.text.as_str()).collect())
            .collect();
        assert_eq!(got, ["開始", "第一章的內文。"], "{name}");
    }
}
