//! 照搬 Go 版 `internal/pdfdoc/pdfdoc_test.go` 與 `realpdf_test.go`。

use std::path::PathBuf;

/// 手寫一份最小的 PDF。xref 的偏移量要真的算,算錯的話物件層讀不到任何物件。
fn make_pdf(content: &str) -> Vec<u8> {
    let objs = [
        "<</Type/Catalog/Pages 2 0 R>>".to_string(),
        "<</Type/Pages/Kids[3 0 R]/Count 1>>".to_string(),
        "<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>".to_string(),
        format!("<</Length {}>>\nstream\n{}\nendstream", content.len(), content),
        "<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>".to_string(),
    ];
    let mut sb = String::from("%PDF-1.4\n");
    let mut offsets = vec![0usize; objs.len() + 1];
    for (i, o) in objs.iter().enumerate() {
        offsets[i + 1] = sb.len();
        sb.push_str(&format!("{} 0 obj\n{}\nendobj\n", i + 1, o));
    }
    let xref = sb.len();
    sb.push_str(&format!("xref\n0 {}\n0000000000 65535 f \n", objs.len() + 1));
    for off in &offsets[1..] {
        sb.push_str(&format!("{off:010} 00000 n \n"));
    }
    sb.push_str(&format!("trailer\n<</Size {}/Root 1 0 R>>\nstartxref\n{}\n%%EOF\n", objs.len() + 1, xref));
    sb.into_bytes()
}

fn doc(content: &str) -> wincv_pdfdoc::Doc {
    wincv_pdfdoc::from_bytes("t.pdf", make_pdf(content)).expect("打得開")
}

#[test]
fn open_and_text() {
    // 用 Td 位移而不是寫成 "(Hello World)":要測的是「靠座標間距還原空白」這條路。
    let d = doc("BT /F1 12 Tf 72 720 Td (Hello) Tj 40 0 Td (World) Tj ET\nBT /F1 12 Tf 72 700 Td (Second) Tj 50 0 Td (line) Tj ET");
    assert_eq!(d.pages, 1);
    let lines = d.text(1).unwrap();
    assert_eq!(lines.len(), 2, "解出 {lines:?}");
    // [雷] PDF 裡沒有空格字元,字與字之間的空白是座標間距。
    assert!(lines[0].text.contains("Hello") && lines[0].text.contains("World"), "第一列是 {:?}", lines[0].text);
    assert!(lines[0].text.contains(' '), "兩個字之間沒有補空格:{:?}", lines[0].text);
    // 由上往下:PDF 的 Y 軸是由下往上長的。
    assert!(lines[0].y >= lines[1].y, "列的順序反了:{} / {}", lines[0].y, lines[1].y);
    assert!(lines[1].text.contains("Second") && lines[1].text.contains("line"), "第二列是 {:?}", lines[1].text);
}

// 沒有嵌入字型時還原不出詞間空白:Go 版記成已知限制(只記錄,不判失敗)。
#[test]
fn no_embedded_font_loses_spaces() {
    let d = doc("BT /F1 12 Tf 72 720 Td (Second line) Tj ET");
    let lines = d.text(1).unwrap();
    assert_eq!(lines.len(), 1, "{lines:?}");
    if lines[0].text != "Secondline" {
        eprintln!("行為變了(可能是好事):{:?}", lines[0].text);
    }
}

// 緊接著的兩段字不該被塞進空格。
#[test]
fn no_spurious_space() {
    let d = doc("BT /F1 12 Tf 72 720 Td (Con) Tj (cat) Tj ET");
    let lines = d.text(1).unwrap();
    assert_eq!(lines.len(), 1, "{lines:?}");
    assert!(!lines[0].text.contains(' '), "接在一起的字被拆開了:{:?}", lines[0].text);
}

// 截斷或根本不是 PDF 的檔案:打不開,而且不能讓程式炸掉。
#[test]
fn bad_files_do_not_panic() {
    for (name, body) in [
        ("empty.pdf", ""),
        ("text.pdf", "這根本不是 PDF"),
        ("header.pdf", "%PDF-1.4\n然後就沒了"),
        ("trunc.pdf", "%PDF-1.4\n1 0 obj\n<</Type/Catalog"),
    ] {
        let r = std::panic::catch_unwind(|| wincv_pdfdoc::from_bytes(name, body.as_bytes().to_vec()).is_ok());
        match r {
            Err(_) => panic!("{name} 讓程式炸了"),
            Ok(true) => panic!("{name} 不該打得開"),
            Ok(false) => {}
        }
    }
}

#[test]
fn page_out_of_range() {
    let d = doc("BT /F1 12 Tf 72 720 Td (x) Tj ET");
    for pg in [0usize, 2, 999] {
        assert!(d.text(pg).is_err(), "第 {pg} 頁不該有東西");
    }
}

// --- realpdf_test.go:借 internal/pdf 的測試檔(LibreOffice 產生的真實 PDF) ---

fn testdata(name: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../internal/pdf/testdata").join(name)
}

fn page_text(name: &str, page: usize) -> String {
    let d = wincv_pdfdoc::open(testdata(name)).unwrap();
    d.text(page).unwrap().iter().map(|l| format!("{}\n", l.text)).collect()
}

// 中文 PDF 用的是子集化的 CID 字型:要走 ToUnicode 才變得回文字。
#[test]
fn real_cjk_text() {
    let got = page_text("rich.pdf", 1);
    for want in ["第一章 標題", "這是一段粗體與斜體的中文內文。", "1.1 小節", "A plain ASCII paragraph with a link."] {
        assert!(got.contains(want), "少了 {want:?}:\n{got}");
    }
}

#[test]
fn real_outline() {
    let d = wincv_pdfdoc::open(testdata("rich.pdf")).unwrap();
    let marks = d.outline();
    assert!(marks.len() >= 2, "書籤只有 {} 筆", marks.len());
    assert_eq!((marks[0].title.as_str(), marks[0].page), ("第一章 標題", 1));
    assert!(marks[1].level > marks[0].level, "小節應該比章更深一層:{} / {}", marks[1].level, marks[0].level);
}

// 雙欄的內文要一欄讀完再讀下一欄。段落編號是連續的,讀錯順序就會亂掉。
#[test]
fn real_two_column_order() {
    let got = page_text("twocol.pdf", 1);
    let mut last = 0;
    for i in 1..=12 {
        let mark = format!("Paragraph {i}.");
        let Some(at) = got.find(&mark) else { continue };
        assert!(at >= last, "第 {i} 段出現在前一段之前,欄的順序讀反了\n{got}");
        last = at;
    }
    assert!(got.contains("Paragraph 1. The quick brown fox jumps over"), "第一列不是完整的一句開頭:\n{got}");
    for line in got.trim().lines() {
        assert!(line.len() <= 70, "這一列有 {} 個字元,是兩欄被併起來了:{line:?}", line.len());
    }
}

// 以下兩項 Go 版的 pdfdoc 沒有直接測(在 internal/pdf 測),這裡補上介面層的煙霧測試。

#[test]
fn render_page() {
    let d = wincv_pdfdoc::open(testdata("rich.pdf")).unwrap();
    let r = d.render(1, 72.0).unwrap();
    let (w, h) = d.page_size(1).unwrap();
    assert_eq!((r.img.w, r.img.h), (w.ceil() as u32, h.ceil() as u32));
    assert_eq!(r.img.pix.len(), (r.img.w * r.img.h * 4) as usize);
    assert!(r.img.pix.chunks(4).any(|p| p[0] < 128), "整頁是空白的");
}

#[test]
fn images_on_page() {
    let d = wincv_pdfdoc::open(testdata("image.pdf")).unwrap();
    let names = d.image_names(1).unwrap();
    assert!(!names.is_empty(), "image.pdf 第 1 頁沒有圖");
    for n in &names {
        let b = d.image(1, n).unwrap();
        assert!(wincv_imgfmt::decode(n, &b).is_ok(), "{n} 解不開");
    }
    assert!(d.image(1, "nope.png").is_err());
}
