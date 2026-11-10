//! 對應 Go 版 `xlsx_test.go`,逐條照搬。

use std::collections::BTreeMap;
use std::io::{Cursor, Write};

use wincv_markdown::Kind;
use wincv_ooxml::Package;

use super::*;

const NS: &str = r#"xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships""#;

fn build(parts: &BTreeMap<String, String>) -> Vec<u8> {
    let mut all: BTreeMap<String, String> = BTreeMap::new();
    all.insert(
        "[Content_Types].xml".into(),
        r#"<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
		  <Default Extension="xml" ContentType="application/xml"/>
		  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/></Types>"#
            .into(),
    );
    all.insert(
        "_rels/.rels".into(),
        r#"<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>"#
            .into(),
    );
    for (k, v) in parts {
        all.insert(k.clone(), v.clone());
    }
    let mut zw = zip::ZipWriter::new(Cursor::new(Vec::new()));
    let opt = zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Stored);
    for (k, v) in &all {
        zw.start_file(k.as_str(), opt).unwrap();
        zw.write_all(v.as_bytes()).unwrap();
    }
    zw.finish().unwrap().into_inner()
}

/// 組一份只有一張工作表的活頁簿。
fn book(sheet_data: &str, styles: &str, shared: &str, wb_pr: &str) -> Book {
    let mut parts = BTreeMap::new();
    parts.insert(
        "xl/workbook.xml".to_string(),
        format!(r#"<?xml version="1.0"?><workbook {NS}>{wb_pr}<sheets><sheet name="表一" sheetId="1" r:id="rS"/></sheets></workbook>"#),
    );
    parts.insert(
        "xl/_rels/workbook.xml.rels".to_string(),
        r#"<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
		  <Relationship Id="rS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/>
		  <Relationship Id="rSS" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/sharedStrings" Target="sharedStrings.xml"/>
		  <Relationship Id="rSt" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>"#
            .to_string(),
    );
    parts.insert(
        "xl/worksheets/sheet1.xml".to_string(),
        format!(r#"<?xml version="1.0"?><worksheet {NS}><sheetData>{sheet_data}</sheetData></worksheet>"#),
    );
    parts.insert("xl/sharedStrings.xml".to_string(), format!(r#"<?xml version="1.0"?><sst {NS}>{shared}</sst>"#));
    parts.insert("xl/styles.xml".to_string(), format!(r#"<?xml version="1.0"?><styleSheet {NS}>{styles}</styleSheet>"#));
    Book::new(Package::from_reader(Cursor::new(build(&parts))).unwrap()).unwrap()
}

fn table(b: &Book) -> Vec<Vec<String>> {
    for blk in b.blocks(0) {
        if blk.kind == Kind::Table {
            return blk.rows;
        }
    }
    panic!("這張工作表沒有表格");
}

#[test]
fn shared_strings_and_inline_and_types() {
    let b = book(
        r#"<row r="1"><c r="A1" t="s"><v>1</v></c><c r="B1" t="inlineStr"><is><t>行內</t></is></c>
		  <c r="C1" t="b"><v>1</v></c><c r="D1"><v>3.5</v></c></row>"#,
        r#"<cellXfs><xf numFmtId="0"/></cellXfs>"#,
        r#"<si><t>第零個</t></si><si><r><t>富</t></r><r><t>文字</t></r></si>"#,
        "",
    );
    let got = table(&b);
    for (i, w) in ["富文字", "行內", "TRUE", "3.5"].iter().enumerate() {
        assert_eq!(got[0][i], *w, "第 {i} 欄");
    }
}

/// 儲存格的參照決定它在哪一欄。跳過的欄要留空,不能整列往左擠。
#[test]
fn sparse_cells_keep_columns() {
    let b = book(r#"<row r="2"><c r="C2" t="inlineStr"><is><t>丙</t></is></c></row>"#, r#"<cellXfs><xf numFmtId="0"/></cellXfs>"#, "", "");
    let got = table(&b);
    assert!(got.len() == 2 && got[0].len() == 3, "表格大小是 {} 列 × {} 欄", got.len(), got[0].len());
    assert!(got[1][2] == "丙" && got[0][0].is_empty(), "位置不對:{got:?}");
}

#[test]
fn date_formats() {
    let styles = r#"<numFmts><numFmt numFmtId="200" formatCode="yyyy&quot;年&quot;m&quot;月&quot;"/>
	  <numFmt numFmtId="201" formatCode="&quot;May&quot; 0.00"/></numFmts>
	  <cellXfs><xf numFmtId="0"/><xf numFmtId="14"/><xf numFmtId="22"/><xf numFmtId="200"/><xf numFmtId="201"/></cellXfs>"#;
    let b = book(
        r#"<row r="1">
	   <c r="A1" s="1"><v>45000</v></c>
	   <c r="B1" s="2"><v>45000.5</v></c>
	   <c r="C1" s="3"><v>45000</v></c>
	   <c r="D1" s="4"><v>1.5</v></c>
	   <c r="E1" s="0"><v>45000</v></c></row>"#,
        styles,
        "",
        "",
    );
    let got = &table(&b)[0];
    assert_eq!(got[0], "2023-03-15", "內建日期格式");
    assert_eq!(got[1], "2023-03-15 12:00:00", "日期加時間");
    assert_eq!(got[2], "2023-03-15", "自訂日期格式");
    // "May" 在引號裡,那個 y 不是年份欄位。
    assert_eq!(got[3], "1.5", "引號裡的字母不該讓它變成日期");
    assert_eq!(got[4], "45000", "沒有日期格式的數字要照原樣");
}

/// Mac 上建的活頁簿用 1904 制。同一個序號在兩制之間差 1462 天:
/// 1900 制的 45000 是 2023-03-15,1904 制的 45000 是 2027-03-16。
#[test]
fn date_1904() {
    let b = book(
        r#"<row r="1"><c r="A1" s="1"><v>45000</v></c></row>"#,
        r#"<cellXfs><xf numFmtId="0"/><xf numFmtId="14"/></cellXfs>"#,
        "",
        r#"<workbookPr date1904="1"/>"#,
    );
    assert_eq!(table(&b)[0][0], "2027-03-16", "1904 制");
}

#[test]
fn percent() {
    let b = book(
        r#"<row r="1"><c r="A1" s="1"><v>0.125</v></c></row>"#,
        r#"<numFmts><numFmt numFmtId="180" formatCode="0.0%"/></numFmts><cellXfs><xf numFmtId="0"/><xf numFmtId="180"/></cellXfs>"#,
        "",
        "",
    );
    assert_eq!(table(&b)[0][0], "12.5%", "百分比");
}

#[test]
fn col_of_refs() {
    for (r, want) in [("A1", 1), ("Z9", 26), ("AA1", 27), ("AB12", 28), ("BA1", 53)] {
        assert_eq!(col_of(r), want, "col_of({r:?})");
    }
}

#[test]
fn not_an_xlsx() {
    let mut parts = BTreeMap::new();
    parts.insert("x.xml".to_string(), "<a/>".to_string());
    let p = Package::from_reader(Cursor::new(build(&parts))).unwrap();
    assert!(Book::new(p).is_err(), "沒有 workbook.xml 應該要失敗");
}
