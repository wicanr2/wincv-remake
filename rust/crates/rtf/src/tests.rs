//! 對應 Go 版 `rtf_test.go`,逐條照搬。

use wincv_markdown::{Block, Kind, Style};

use super::*;

fn parse(src: &str) -> Doc {
    Doc::parse(src.as_bytes()).unwrap()
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
fn paragraphs_and_styles() {
    let bs = parse(r"{\rtf1\ansi one \b two\b0  three\par four\par}").blocks();
    assert_eq!(bs.len(), 2, "要 2 段,拿到 {}:{:?}", bs.len(), text(&bs));
    let bold: String = bs[0].spans.iter().filter(|s| s.style.has(Style::BOLD)).map(|s| s.text.as_str()).collect();
    assert_eq!(bold, "two", "粗體的部分是 {bold:?}");
}

/// 群組是 RTF 唯一的作用域機制:離開群組,裡面的格式設定就沒了。
#[test]
fn group_scoping() {
    let bs = parse(r"{\rtf1\ansi {\b inside} outside\par}").blocks();
    for s in &bs[0].spans {
        assert!(!(s.text.contains("outside") && s.style.has(Style::BOLD)), "群組外面不該還是粗體:{s:?}");
    }
}

/// 一份繁體中文 RTF 只在字型表裡寫 fcharset136,正文全是 \'hh。
#[test]
fn big5_via_font_charset() {
    let d = parse(concat!(
        r"{\rtf1\ansi\ansicpg1252{\fonttbl{\f0\fnil\fcharset0 Arial;}{\f1\fnil\fcharset136 PMingLiU;}}",
        r"\f1\'a4\'a4\'a4\'e5\f0 abc\par}"
    ));
    let got = text(&d.blocks());
    assert!(got.contains("中文"), "Big5 沒有解出來:{got:?}");
    assert!(got.contains("abc"), "換回西文字型之後的內容不見了:{got:?}");
}

/// 組出一個 \uN 控制字。
///
/// 用組的而不是直接寫在原始碼裡:反斜線接 u 再接四個十六進位數字這個形狀,
/// 在通往檔案的路上會被跳脫規則搶走,寫進去的就不是原本要寫的東西。
fn uw(n: i64) -> String {
    format!("\\u{n} ")
}

#[test]
fn unicode_escape_and_skip() {
    // \uc1 表示每個 \u 後面有一個替代字元要跳過。
    let d = parse(&format!(r"{{\rtf1\ansi\uc1 {}?{}?!\par}}", uw(20013), uw(25991)));
    let got = text(&d.blocks());
    assert_eq!(got.trim(), "中文!");
}

/// 大於 32767 的字碼在 RTF 裡是用負數寫的。
#[test]
fn unicode_negative_and_surrogate() {
    let d = parse(&format!(r"{{\rtf1\ansi\uc0 {}{}\par}}", uw(-10179), uw(-8704)));
    let got = text(&d.blocks());
    assert_eq!(got.trim(), "\u{1F600}", "代理對沒有組回來");
}

#[test]
fn table() {
    let bs = parse(concat!(r"{\rtf1\ansi\ansicpg65001\trowd\intbl 甲\cell 乙\cell\row", r"\trowd\intbl 丙\cell 丁\cell\row\pard 之後\par}"))
        .blocks();
    assert_eq!(bs[0].kind, Kind::Table, "第一個區塊不是表格:{:?}", text(&bs));
    assert!(bs[0].rows.len() == 2 && bs[0].rows[1][1] == "丁", "表格內容不對:{:?}", bs[0].rows);
    assert_eq!(bs[1].kind, Kind::Para, "表格後面應該接一般段落");
}

#[test]
fn hyperlink_field() {
    let bs = parse(r#"{\rtf1\ansi\ansicpg65001 {\field{\*\fldinst HYPERLINK "https://a.example/" }{\fldrslt 按這裡}}後面\par}"#).blocks();
    let (mut link, mut plain) = (String::new(), String::new());
    for s in &bs[0].spans {
        if s.style.has(Style::LINK) {
            link += &s.text;
            assert_eq!(s.href, "https://a.example/");
        } else {
            plain += &s.text;
        }
    }
    assert!(link == "按這裡" && plain == "後面", "連結 {link:?},一般 {plain:?}");
}

/// 清單的符號在 listtext 群組裡,那是給不懂清單表的讀取器看的備援。
#[test]
fn list_text_becomes_marker() {
    let bs = parse(r"{\rtf1\ansi\ansicpg65001\ls1\ilvl0{\listtext 1.\tab}第一項\par\ls1\ilvl1{\listtext 2.\tab}第二項\par}").blocks();
    assert!(bs[0].kind == Kind::List && bs[0].marker == "1.", "第一項:kind={:?} marker={:?}", bs[0].kind, bs[0].marker);
    assert_eq!(bs[1].level, 2, "第二項的層級");
    assert!(!text(&bs).contains("1.第一項"), "項目符號不該混進內文:{:?}", text(&bs));
}

#[test]
fn skipped_destinations() {
    let d = parse(concat!(
        r"{\rtf1\ansi\ansicpg65001{\colortbl;\red255\green0\blue0;}{\info{\title 標題}}",
        r"{\*\generator Word;}{\*\bkmkstart X}正文\par}"
    ));
    assert_eq!(text(&d.blocks()).trim(), "正文", "該丟掉的東西跑進正文了");
}

#[test]
fn picture_and_dedup() {
    let png = "89504e470d0a1a0a0000";
    let d = parse(&format!(r"{{\rtf1\ansi{{\*\shppict{{\pict\pngblip {png}}}}}{{\*\nonshppict{{\pict\wmetafile8 0102}}}}\par}}"));
    let imgs: Vec<Block> = d.blocks().into_iter().filter(|b| b.kind == Kind::Image).collect();
    assert_eq!(imgs.len(), 1, "圖片應該只有一張:{:?}", text(&d.blocks()));
    let got = d.image(&imgs[0].src).unwrap();
    assert_eq!(&got[..4], b"\x89PNG", "解出來的不是 PNG");
}

#[test]
fn footnote() {
    let d = parse(r"{\rtf1\ansi\ansicpg65001 正文{\*\footnote \chftn 註腳內容}後面\par}");
    let got = text(&d.blocks());
    assert!(got.contains("[註 1]"), "少了註腳標記:{got:?}");
    assert!(got.contains("註腳內容"), "少了註腳內容:{got:?}");
    // 註腳內容不能留在正文那一段裡。
    assert!(!text(&d.blocks()[..1]).contains("註腳內容"), "註腳跑進正文了:{got:?}");
}

#[test]
fn heading_from_stylesheet() {
    let bs = parse(concat!(r"{\rtf1\ansi\ansicpg65001{\stylesheet{\s1 heading 1;}{\s2 標題 2;}}", r"\s1 大標\par\s2 小標\par\pard 內文\par}"))
        .blocks();
    assert!(bs[0].kind == Kind::Heading && bs[0].level == 1, "第一段:{:?} level={}", bs[0].kind, bs[0].level);
    assert!(bs[1].kind == Kind::Heading && bs[1].level == 2, "第二段:{:?} level={}", bs[1].kind, bs[1].level);
    assert_eq!(bs[2].kind, Kind::Para, "第三段應該是一般段落");
}

#[test]
fn not_rtf() {
    assert!(Doc::parse(b"hello").is_err(), "不是 RTF 應該要失敗");
}

/// `\bin` 的長度大到超過 int 的範圍時,要當成沒有資料,後面的字照常解。
/// 對應 Go 版 `TestBinLengthOverflow`。
#[test]
fn bin_length_overflow() {
    let got = text(&parse(r"{\rtf1\ansi before\par {\bin99999999999999999999 x}after\par}").blocks());
    assert!(got.contains("before") && got.contains("after"), "內容不對:{got:?}");
}
