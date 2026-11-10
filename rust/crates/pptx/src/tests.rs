//! 對應 Go 版 `pptx_test.go`,逐條照搬。

use std::collections::BTreeMap;
use std::io::{Cursor, Write};

use wincv_markdown::{Block, Kind};
use wincv_ooxml::Package;

use super::*;

const NS: &str = r#"xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main"
 xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"
 xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"
 xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006""#;

const RELS_HEAD: &str = r#"<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">"#;

fn build(parts: &BTreeMap<String, String>) -> Vec<u8> {
    let mut all: BTreeMap<String, String> = BTreeMap::new();
    all.insert(
        "[Content_Types].xml".into(),
        r#"<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
		  <Default Extension="xml" ContentType="application/xml"/><Default Extension="png" ContentType="image/png"/>
		  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/></Types>"#
            .into(),
    );
    all.insert(
        "_rels/.rels".into(),
        format!(
            r#"{RELS_HEAD}
		  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="ppt/presentation.xml"/></Relationships>"#
        ),
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

fn slide(body: &str) -> String {
    format!(r#"<?xml version="1.0"?><p:sld {NS}><p:cSld><p:spTree>{body}</p:spTree></p:cSld></p:sld>"#)
}

/// 組一個圖形。ph 是佔位屬性(可以是空的),xy 是位置(可以是空的)。
fn sp(ph: &str, xy: &str, paras: &str) -> String {
    let ph_el = if ph.is_empty() { String::new() } else { format!("<p:ph {ph}/>") };
    let pos = if xy.is_empty() { String::new() } else { format!("<p:spPr><a:xfrm><a:off {xy}/></a:xfrm></p:spPr>") };
    format!("<p:sp><p:nvSpPr><p:nvPr>{ph_el}</p:nvPr></p:nvSpPr>{pos}<p:txBody>{paras}</p:txBody></p:sp>")
}

fn para(p_pr: &str, text: &str) -> String {
    format!("<a:p>{p_pr}<a:r><a:t>{text}</a:t></a:r></a:p>")
}

fn open(parts: BTreeMap<String, String>) -> Deck {
    Deck::new(Package::from_reader(Cursor::new(build(&parts))).unwrap()).unwrap()
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

fn map(kv: &[(&str, String)]) -> BTreeMap<String, String> {
    kv.iter().map(|(k, v)| (k.to_string(), v.clone())).collect()
}

/// 放映順序寫在 sldIdLst,不是檔名的數字。搬動過投影片的簡報兩者不一樣。
#[test]
fn slide_order_follows_id_list() {
    let d = open(map(&[
        (
            "ppt/presentation.xml",
            format!(r#"<?xml version="1.0"?><p:presentation {NS}><p:sldIdLst>
		  <p:sldId id="258" r:id="rB"/><p:sldId id="256" r:id="rA"/></p:sldIdLst></p:presentation>"#),
        ),
        (
            "ppt/_rels/presentation.xml.rels",
            format!(
                r#"{RELS_HEAD}
		  <Relationship Id="rA" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/>
		  <Relationship Id="rB" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide2.xml"/></Relationships>"#
            ),
        ),
        ("ppt/slides/slide1.xml", slide(&sp(r#"type="title""#, "", &para("", "第一個檔名")))),
        ("ppt/slides/slide2.xml", slide(&sp(r#"type="title""#, "", &para("", "第一張放映")))),
    ]));
    assert_eq!(d.slides.len(), 2, "要 2 張");
    assert!(d.slides[0].title == "第一張放映" && d.slides[1].title == "第一個檔名", "順序不對:{:?} / {:?}", d.slides[0].title, d.slides[1].title);
}

/// 只有一張投影片時的共用骨架。
fn one_slide(body: String, extra: &[(&str, String)]) -> BTreeMap<String, String> {
    let mut m = map(&[
        (
            "ppt/presentation.xml",
            format!(r#"<?xml version="1.0"?><p:presentation {NS}><p:sldIdLst>
		  <p:sldId id="256" r:id="rA"/></p:sldIdLst></p:presentation>"#),
        ),
        (
            "ppt/_rels/presentation.xml.rels",
            format!(
                r#"{RELS_HEAD}
		  <Relationship Id="rA" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>"#
            ),
        ),
        ("ppt/slides/slide1.xml", body),
    ]);
    for (k, v) in extra {
        m.insert(k.to_string(), v.clone());
    }
    m
}

#[test]
fn title_and_bullet_levels() {
    let d = open(one_slide(
        slide(
            &(sp(r#"type="title""#, "", &para("", "標題"))
                + &sp(
                    r#"type="body" idx="1""#,
                    "",
                    &(para(r#"<a:pPr lvl="0"/>"#, "第一層") + &para(r#"<a:pPr lvl="1"/>"#, "第二層") + &para("<a:pPr><a:buNone/></a:pPr>", "沒有符號")),
                )),
        ),
        &[],
    ));
    let bs = &d.slides[0].blocks;
    assert!(bs[0].kind == Kind::Heading && bs[0].spans[0].text == "標題", "第一個區塊應該是標題:{:?}", text(bs));
    assert!(bs[1].kind == Kind::List && bs[1].level == 1, "第一層條列不對:{:?}", bs[1]);
    assert!(bs[2].kind == Kind::List && bs[2].level == 2, "第二層條列不對:{:?}", bs[2]);
    assert_eq!(bs[3].kind, Kind::Para, "buNone 應該變成一般段落");
}

/// 投影片自己常常只寫 idx 不寫 type,型別要回頭問版面配置。
#[test]
fn placeholder_type_from_layout() {
    let d = open(one_slide(
        slide(&sp(r#"idx="0""#, "", &para("", "我是標題"))),
        &[
            (
                "ppt/slides/_rels/slide1.xml.rels",
                format!(
                    r#"{RELS_HEAD}
			  <Relationship Id="rL" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slideLayout" Target="../slideLayouts/slideLayout1.xml"/></Relationships>"#
                ),
            ),
            (
                "ppt/slideLayouts/slideLayout1.xml",
                format!(r#"<?xml version="1.0"?><p:sldLayout {NS}><p:cSld><p:spTree>
			  <p:sp><p:nvSpPr><p:nvPr><p:ph type="title" idx="0"/></p:nvPr></p:nvSpPr></p:sp></p:spTree></p:cSld></p:sldLayout>"#),
            ),
        ],
    ));
    assert_eq!(d.slides[0].title, "我是標題", "標題沒有從版面配置認出來:{:?}", text(&d.slides[0].blocks));
}

#[test]
fn table_and_image_and_notes() {
    let png = "\u{89}PNG\r\n\u{1a}\n內容";
    let body = slide(
        &(sp(r#"type="title""#, "", &para("", "T"))
            + r#"<p:graphicFrame><a:graphic><a:graphicData><a:tbl>
			  <a:tr><a:tc><a:txBody><a:p><a:r><a:t>甲</a:t></a:r></a:p></a:txBody></a:tc>
			       <a:tc><a:txBody><a:p><a:r><a:t>乙</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
			  <a:tr><a:tc gridSpan="2"><a:txBody><a:p><a:r><a:t>橫跨</a:t></a:r></a:p></a:txBody></a:tc></a:tr>
			</a:tbl></a:graphicData></a:graphic></p:graphicFrame>"#
            + r#"<p:pic><p:nvPicPr><p:cNvPr id="3" name="圖.png"/></p:nvPicPr>
			  <p:blipFill><a:blip r:embed="rI"/></p:blipFill></p:pic>"#),
    );
    let d = open(one_slide(
        body,
        &[
            ("ppt/media/image1.png", png.to_string()),
            (
                "ppt/slides/_rels/slide1.xml.rels",
                format!(
                    r#"{RELS_HEAD}
		  <Relationship Id="rI" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="../media/image1.png"/>
		  <Relationship Id="rN" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/notesSlide" Target="../notesSlides/notesSlide1.xml"/></Relationships>"#
                ),
            ),
            (
                "ppt/notesSlides/notesSlide1.xml",
                format!(r#"<?xml version="1.0"?><p:notes {NS}><p:cSld><p:spTree>"#)
                    + &sp(r#"type="body" idx="1""#, "", &para("", "講稿內容"))
                    + "</p:spTree></p:cSld></p:notes>",
            ),
        ],
    ));
    let bs = &d.slides[0].blocks;
    let got = text(bs);
    let tbl = bs.iter().find(|b| b.kind == Kind::Table);
    let img = bs.iter().find(|b| b.kind == Kind::Image);
    assert!(matches!(tbl, Some(t) if t.rows[0][0] == "甲" && t.rows[1][1].is_empty()), "表格不對:{got:?}");
    let img = img.unwrap_or_else(|| panic!("少了圖片:{got:?}"));
    assert_eq!(d.image(&img.src).unwrap(), png.as_bytes(), "取不回圖片內容");
    assert!(got.contains("備忘稿") && got.contains("講稿內容"), "備忘稿沒有接上:{got:?}");
}

/// 每個圖形都寫明位置時依位置排;少一個就整批照原順序。
#[test]
fn position_ordering() {
    let with_pos = open(one_slide(slide(&(sp("", r#"x="0" y="5000""#, &para("", "下面")) + &sp("", r#"x="0" y="1000""#, &para("", "上面")))), &[]));
    let got = text(&with_pos.slides[0].blocks);
    assert!(got.find("上面") < got.find("下面"), "有位置時要由上而下:{got:?}");
    let no_pos = open(one_slide(slide(&(sp("", r#"x="0" y="5000""#, &para("", "下面")) + &sp("", "", &para("", "沒位置")))), &[]));
    let got = text(&no_pos.slides[0].blocks);
    assert!(got.find("下面") < got.find("沒位置"), "少一個位置就該照原順序:{got:?}");
}

#[test]
fn not_a_pptx() {
    let p = Package::from_reader(Cursor::new(build(&map(&[("x.xml", "<a/>".to_string())])))).unwrap();
    assert!(Deck::new(p).is_err(), "沒有 presentation.xml 應該要失敗");
}

/// 沒有佔位資訊的簡報靠位置與長度猜標題。條件不合就不猜:
/// 把一段內文抬成標題比少一個標題糟。
#[test]
fn title_guess_without_placeholders() {
    let short = open(one_slide(
        slide(&(sp("", r#"x="0" y="1000""#, &para("", "短短的標題")) + &sp("", r#"x="0" y="5000""#, &(para("", "內文一") + &para("", "內文二"))))),
        &[],
    ));
    assert_eq!(short.slides[0].title, "短短的標題", "該猜的沒猜到");

    let long = "很長的一段內文".repeat(12);
    let multi = open(one_slide(slide(&sp("", r#"x="0" y="1000""#, &para("", &long))), &[]));
    assert_eq!(multi.slides[0].title, "", "太長的段落不該被當成標題");
    assert!(text(&multi.slides[0].blocks).contains(&long), "沒被當成標題就要留在內文裡");
}
