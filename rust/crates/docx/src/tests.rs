//! 對應 Go 版 `docx_test.go`,逐條照搬。

use std::collections::BTreeMap;
use std::io::{Cursor, Write};

use wincv_markdown::{Block, Kind, Style};
use wincv_ooxml::Package;

use super::*;

/// 組一份 .docx。鍵是 ZIP 內的路徑,值是內容。
fn build(parts: &[(&str, &str)]) -> Vec<u8> {
    let mut all: BTreeMap<String, String> = BTreeMap::new();
    all.insert(
        "[Content_Types].xml".into(),
        r#"<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
		  <Default Extension="xml" ContentType="application/xml"/>
		  <Default Extension="png" ContentType="image/png"/>
		  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
		  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
		</Types>"#
            .into(),
    );
    all.insert(
        "_rels/.rels".into(),
        r#"<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
		</Relationships>"#
            .into(),
    );
    for (k, v) in parts {
        all.insert(k.to_string(), v.to_string());
    }
    let mut zw = zip::ZipWriter::new(Cursor::new(Vec::new()));
    let opt = zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Stored);
    for (k, v) in &all {
        zw.start_file(k.as_str(), opt).unwrap();
        zw.write_all(v.as_bytes()).unwrap();
    }
    zw.finish().unwrap().into_inner()
}

/// 把 body 的內容包成一份完整的 document.xml。
fn doc(body: &str) -> String {
    format!(
        r#"<?xml version="1.0"?><w:document
	  xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	  xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	  xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"
	  xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape"
	  xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
	  xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	  xmlns:v="urn:schemas-microsoft-com:vml"><w:body>{body}</w:body></w:document>"#
    )
}

fn open(parts: &[(&str, &str)]) -> Doc {
    let p = Package::from_reader(Cursor::new(build(parts))).unwrap();
    Doc::new(p).unwrap()
}

fn text(bs: &[Block]) -> String {
    let mut sb = String::new();
    for b in bs {
        for s in &b.spans {
            sb.push_str(&s.text);
        }
        sb.push('\n');
    }
    sb
}

const DOC_RELS_HEAD: &str = r#"<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">"#;

#[test]
fn paragraphs_and_runs() {
    let d = open(&[(
        "word/document.xml",
        &doc(r#"
		  <w:p><w:r><w:t xml:space="preserve">hello </w:t></w:r>
		       <w:r><w:rPr><w:b/></w:rPr><w:t>world</w:t></w:r></w:p>
		  <w:p><w:r><w:rPr><w:i w:val="1"/></w:rPr><w:t>斜體</w:t></w:r></w:p>"#),
    )]);
    let bs = d.blocks();
    assert_eq!(bs.len(), 2, "要 2 個區塊,拿到 {}:{:?}", bs.len(), text(&bs));
    assert_eq!(text(&bs[..1]), "hello world\n");
    assert!(bs[0].spans[1].style.has(Style::BOLD), "<w:b/> 沒有 val 屬性時應該是粗體");
    assert!(bs[1].spans[0].style.has(Style::ITALIC), "斜體沒套上");
}

/// 空段落是排版用的間距,不該變成空區塊。
#[test]
fn empty_paragraph_dropped() {
    let d = open(&[("word/document.xml", &doc(r#"<w:p/><w:p><w:r><w:t>x</w:t></w:r></w:p><w:p><w:r><w:t></w:t></w:r></w:p>"#))]);
    let bs = d.blocks();
    assert_eq!(bs.len(), 1, "要 1 個區塊,拿到 {}:{:?}", bs.len(), text(&bs));
}

#[test]
fn heading_from_outline_level_and_name() {
    let styles = r#"<?xml version="1.0"?><w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
	  <w:style w:type="paragraph" w:styleId="H1"><w:name w:val="Whatever"/><w:pPr><w:outlineLvl w:val="0"/></w:pPr></w:style>
	  <w:style w:type="paragraph" w:styleId="X"><w:name w:val="標題 3"/></w:style>
	  <w:style w:type="paragraph" w:styleId="Y"><w:name w:val="我的樣式"/><w:basedOn w:val="X"/></w:style>
	</w:styles>"#;
    let rels = format!(
        r#"{DOC_RELS_HEAD}
		  <Relationship Id="rs" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
		</Relationships>"#
    );
    let d = open(&[
        (
            "word/document.xml",
            &doc(r#"
		  <w:p><w:pPr><w:pStyle w:val="H1"/></w:pPr><w:r><w:t>甲</w:t></w:r></w:p>
		  <w:p><w:pPr><w:pStyle w:val="X"/></w:pPr><w:r><w:t>乙</w:t></w:r></w:p>
		  <w:p><w:pPr><w:pStyle w:val="Y"/></w:pPr><w:r><w:t>丙</w:t></w:r></w:p>"#),
        ),
        ("word/styles.xml", styles),
        ("word/_rels/document.xml.rels", &rels),
    ]);
    let bs = d.blocks();
    let want = [1, 3, 3]; // 第三個靠 basedOn 繼承
    for (i, w) in want.iter().enumerate() {
        assert!(bs[i].kind == Kind::Heading && bs[i].level == *w, "第 {i} 段是 kind={:?} level={},要 Heading level={w}", bs[i].kind, bs[i].level);
    }
}

#[test]
fn list_numbering_and_reset() {
    let num = r#"<?xml version="1.0"?><w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
	  <w:abstractNum w:abstractNumId="0">
	    <w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%1."/></w:lvl>
	    <w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%2."/></w:lvl>
	  </w:abstractNum>
	  <w:abstractNum w:abstractNumId="9">
	    <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/><w:lvlText w:val="&#xF0B7;"/></w:lvl>
	  </w:abstractNum>
	  <w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
	  <w:num w:numId="2"><w:abstractNumId w:val="9"/></w:num>
	</w:numbering>"#;
    let item = |num_id: &str, ilvl: &str, s: &str| {
        format!(r#"<w:p><w:pPr><w:numPr><w:ilvl w:val="{ilvl}"/><w:numId w:val="{num_id}"/></w:numPr></w:pPr><w:r><w:t>{s}</w:t></w:r></w:p>"#)
    };
    let body = [item("1", "0", "a"), item("1", "1", "a1"), item("1", "1", "a2"), item("1", "0", "b"), item("1", "1", "b1"), item("2", "0", "點")].concat();
    let rels = format!(
        r#"{DOC_RELS_HEAD}
		  <Relationship Id="rn" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>
		</Relationships>"#
    );
    let d = open(&[("word/document.xml", &doc(&body)), ("word/numbering.xml", num), ("word/_rels/document.xml.rels", &rels)]);
    let bs = d.blocks();
    let want_num = [1, 1, 2, 2, 1, 0];
    let want_lvl = [1, 2, 2, 1, 2, 1];
    for i in 0..want_num.len() {
        assert_eq!(bs[i].kind, Kind::List, "第 {i} 個不是清單");
        assert!(
            bs[i].num == want_num[i] && bs[i].level == want_lvl[i],
            "第 {i} 個 num={} level={},要 num={} level={}",
            bs[i].num,
            bs[i].level,
            want_num[i],
            want_lvl[i]
        );
    }
    assert!(!bs[5].ordered, "bullet 不該是有序清單");
    // Wingdings 的字碼在私人使用區,畫出來是缺字方框,要交回預設符號。
    assert_eq!(bs[5].marker, "", "私人使用區的項目符號應該丟掉");
}

#[test]
fn table_with_grid_span() {
    let d = open(&[(
        "word/document.xml",
        &doc(r#"<w:tbl>
		  <w:tr><w:tc><w:p><w:r><w:t>甲</w:t></w:r></w:p></w:tc>
		        <w:tc><w:p><w:r><w:t>乙</w:t></w:r></w:p></w:tc>
		        <w:tc><w:p><w:r><w:t>丙</w:t></w:r></w:p></w:tc></w:tr>
		  <w:tr><w:tc><w:tcPr><w:gridSpan w:val="2"/></w:tcPr><w:p><w:r><w:t>橫跨</w:t></w:r></w:p></w:tc>
		        <w:tc><w:p><w:r><w:t>丁</w:t></w:r></w:p></w:tc></w:tr>
		</w:tbl>"#),
    )]);
    let bs = d.blocks();
    assert!(bs.len() == 1 && bs[0].kind == Kind::Table, "要一個表格,拿到 {bs:?}");
    let want = [["甲", "乙", "丙"], ["橫跨", "", "丁"]];
    for (i, row) in want.iter().enumerate() {
        for (j, cell) in row.iter().enumerate() {
            assert_eq!(bs[0].rows[i][j], *cell, "第 {i} 列第 {j} 欄");
        }
    }
}

#[test]
fn hyperlink_both_forms() {
    let rels = format!(
        r#"{DOC_RELS_HEAD}
		  <Relationship Id="rL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="https://a.example/" TargetMode="External"/>
		</Relationships>"#
    );
    let d = open(&[
        (
            "word/document.xml",
            &doc(r#"
		  <w:p><w:hyperlink r:id="rL"><w:r><w:t>關聯式</w:t></w:r></w:hyperlink></w:p>
		  <w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r>
		       <w:r><w:instrText xml:space="preserve"> HYPERLINK "https://b.example/" </w:instrText></w:r>
		       <w:r><w:fldChar w:fldCharType="separate"/></w:r>
		       <w:r><w:t>欄位式</w:t></w:r>
		       <w:r><w:fldChar w:fldCharType="end"/></w:r>
		       <w:r><w:t>之後</w:t></w:r></w:p>"#),
        ),
        ("word/_rels/document.xml.rels", &rels),
    ]);
    let bs = d.blocks();
    assert_eq!(bs[0].spans[0].href, "https://a.example/", "關聯式連結的目標");
    let (mut link, mut plain) = (String::new(), String::new());
    for s in &bs[1].spans {
        if s.style.has(Style::LINK) {
            link += &s.text;
        } else {
            plain += &s.text;
        }
    }
    assert!(link == "欄位式" && plain == "之後", "欄位式連結:連結部分 {link:?},一般部分 {plain:?}");
}

/// mc:AlternateContent 的兩個分支是同一段內容的兩個版本,只能取一個。
#[test]
fn alternate_content_not_duplicated() {
    let d = open(&[(
        "word/document.xml",
        &doc(r#"<w:p><w:r><mc:AlternateContent>
		  <mc:Choice Requires="wps"><w:drawing><wp:inline><a:graphic><a:graphicData>
		    <wps:wsp><wps:txbx><w:txbxContent><w:p><w:r><w:t>方塊裡的字</w:t></w:r></w:p></w:txbxContent></wps:txbx></wps:wsp>
		  </a:graphicData></a:graphic></wp:inline></w:drawing></mc:Choice>
		  <mc:Fallback><w:pict><v:shape><v:textbox><w:txbxContent>
		    <w:p><w:r><w:t>方塊裡的字</w:t></w:r></w:p></w:txbxContent></v:textbox></v:shape></w:pict></mc:Fallback>
		</mc:AlternateContent></w:r></w:p>"#),
    )]);
    let got = text(&d.blocks());
    assert_eq!(got.matches("方塊裡的字").count(), 1, "文字方塊的內容應該只有 1 次:{got:?}");
}

#[test]
fn image_and_bytes() {
    let png = "\u{89}PNG\r\n\u{1a}\n這是假的但夠用";
    let rels = format!(
        r#"{DOC_RELS_HEAD}
		  <Relationship Id="rI" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.png"/>
		</Relationships>"#
    );
    let d = open(&[
        (
            "word/document.xml",
            &doc(r#"<w:p><w:r><w:drawing><wp:inline>
		  <wp:docPr id="1" name="pic.png" descr="一張圖"/>
		  <a:graphic><a:graphicData><a:blip r:embed="rI"/></a:graphicData></a:graphic>
		</wp:inline></w:drawing></w:r></w:p>"#),
        ),
        ("word/media/image1.png", png),
        ("word/_rels/document.xml.rels", &rels),
    ]);
    let bs = d.blocks();
    assert!(bs.len() == 1 && bs[0].kind == Kind::Image, "要一個圖片區塊,拿到 {bs:?}");
    assert_eq!(bs[0].alt, "一張圖", "替代文字");
    let got = d.image(&bs[0].src).unwrap();
    assert_eq!(got, png.as_bytes(), "取回的圖片內容不對");
}

#[test]
fn footnotes_appended() {
    let rels = format!(
        r#"{DOC_RELS_HEAD}
		  <Relationship Id="rf" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/footnotes" Target="footnotes.xml"/>
		</Relationships>"#
    );
    let d = open(&[
        ("word/document.xml", &doc(r#"<w:p><w:r><w:t>正文</w:t></w:r>
		  <w:r><w:footnoteReference w:id="2"/></w:r></w:p>"#)),
        (
            "word/footnotes.xml",
            r#"<?xml version="1.0"?><w:footnotes xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
		  <w:footnote w:type="separator" w:id="0"><w:p><w:r><w:t>不要這個</w:t></w:r></w:p></w:footnote>
		  <w:footnote w:id="2"><w:p><w:r><w:t>註腳內容</w:t></w:r></w:p></w:footnote>
		</w:footnotes>"#,
        ),
        ("word/_rels/document.xml.rels", &rels),
    ]);
    let got = text(&d.blocks());
    assert!(got.contains("[註 1]"), "正文少了註腳標記:{got:?}");
    assert!(got.contains("註腳內容"), "文末少了註腳內容:{got:?}");
    assert!(!got.contains("不要這個"), "分隔用的 footnote 不該出現:{got:?}");
}

/// 修訂:插入的內容要留,刪除的不要。
#[test]
fn tracked_changes() {
    let d = open(&[(
        "word/document.xml",
        &doc(r#"<w:p>
		  <w:ins><w:r><w:t>新增的</w:t></w:r></w:ins>
		  <w:del><w:r><w:delText>刪掉的</w:delText></w:r></w:del>
		  <w:r><w:t>原有的</w:t></w:r></w:p>"#),
    )]);
    let got = text(&d.blocks());
    assert!(got.contains("新增的") && got.contains("原有的"), "內容不完整:{got:?}");
    assert!(!got.contains("刪掉的"), "刪除的內容不該出現:{got:?}");
}

/// 分頁在段落中間出現時要把段落切開,不能把前後接成一段。
#[test]
fn page_break_splits_paragraph() {
    let d = open(&[("word/document.xml", &doc(r#"<w:p><w:r><w:t>前</w:t><w:br w:type="page"/></w:r><w:r><w:t>後</w:t></w:r></w:p>"#))]);
    let bs = d.blocks();
    assert!(bs.len() == 3 && bs[1].kind == Kind::Rule, "要「段落 / 分隔線 / 段落」,拿到 {} 個區塊:{:?}", bs.len(), text(&bs));
}

#[test]
fn not_a_docx() {
    let p = Package::from_reader(Cursor::new(build(&[("foo.xml", "<a/>")]))).unwrap();
    assert!(Doc::new(p).is_err(), "沒有 document.xml 應該要失敗");
}
