//! 照搬 Go 版 `bookview_test.go` 的 PDF 部分與 `pdfzoom_test.go`。

use std::path::PathBuf;

use super::*;
use crate::pdfview::{parse_pdf_img_ref, parse_pdf_url, pdf_img_ref, pdf_url};
use crate::Mode;

/// 手寫一份兩頁的最小 PDF,回傳它所在的目錄。
fn make_pdf_file() -> TempDir {
    let td = TempDir::new();
    let c1 = "BT /F1 12 Tf 72 720 Td (First) Tj 40 0 Td (page) Tj ET";
    let c2 = "BT /F1 12 Tf 72 720 Td (Second) Tj 50 0 Td (page) Tj ET";
    let objs = [
        "<</Type/Catalog/Pages 2 0 R>>".to_string(),
        "<</Type/Pages/Kids[3 0 R 6 0 R]/Count 2>>".to_string(),
        "<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 4 0 R/Resources<</Font<</F1 5 0 R>>>>>>".to_string(),
        format!("<</Length {}>>\nstream\n{c1}\nendstream", c1.len()),
        "<</Type/Font/Subtype/Type1/BaseFont/Helvetica>>".to_string(),
        "<</Type/Page/Parent 2 0 R/MediaBox[0 0 612 792]/Contents 7 0 R/Resources<</Font<</F1 5 0 R>>>>>>".to_string(),
        format!("<</Length {}>>\nstream\n{c2}\nendstream", c2.len()),
    ];
    let mut sb = String::from("%PDF-1.4\n");
    let mut offs = vec![0; objs.len() + 1];
    for (i, o) in objs.iter().enumerate() {
        offs[i + 1] = sb.len();
        sb += &format!("{} 0 obj\n{o}\nendobj\n", i + 1);
    }
    let x = sb.len();
    sb += &format!("xref\n0 {}\n0000000000 65535 f \n", objs.len() + 1);
    for off in &offs[1..] {
        sb += &format!("{off:010} 00000 n \n");
    }
    sb += &format!("trailer\n<</Size {}/Root 1 0 R>>\nstartxref\n{x}\n%%EOF\n", objs.len() + 1);
    write(td.path().join("doc.pdf"), sb);
    td
}

fn open_doc_pdf() -> (App, Screen, TempDir) {
    let td = make_pdf_file();
    let mut a = new_at(td.path());
    (a.cell_w, a.cell_h) = (8, 16);
    let mut s = Screen::new(70, 20);
    a.draw(&mut s);
    a.focus_on("doc.pdf");
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    (a, s, td)
}

/// 按 Enter 開 .pdf 要直接看到第一頁,不是頁碼清單。
#[test]
fn enter_pdf_shows_first_page() {
    let (_a, s, _td) = open_doc_pdf();
    let txt = screen_text(&s);
    assert!(txt.contains("First") && txt.contains("page"), "第一頁的文字沒出來\n{txt}");
    assert!(txt.contains("下一頁"), "頁末沒有下一頁\n{txt}");
}

/// 換頁與回頁碼清單。
#[test]
fn pdf_page_navigation() {
    let (mut a, mut s, _td) = open_doc_pdf();
    // 第一頁只有「頁碼清單」與「下一頁」兩個連結,往下一個就是下一頁。
    a.handle_key(named(Code::Down));
    a.handle_key(named(Code::Enter));
    a.draw(&mut s);
    assert!(screen_text(&s).contains("Second"), "沒換到第二頁\n{}", screen_text(&s));
    a.handle_key(named(Code::Backspace));
    a.draw(&mut s);
    assert!(screen_text(&s).contains("First"), "回不到第一頁\n{}", screen_text(&s));
}

#[test]
fn pdf_url_round_trip() {
    for (path, page) in [("/tmp/a.pdf", 0), ("/tmp/a.pdf", 1), ("/tmp/a.pdf", 250), ("/tmp/怪#名.pdf", 7)] {
        let raw = pdf_url(path, page);
        assert_eq!(parse_pdf_url(&raw), Some((path.to_string(), page)), "{raw:?}");
    }
}

/// 不同頁可以有同名的 XObject,圖片參照要帶頁碼。
#[test]
fn pdf_image_ref_carries_page() {
    let r = pdf_img_ref(7, "Im1.png");
    assert_eq!(parse_pdf_img_ref(&r), Some((7, "Im1.png".to_string())), "{r:?}");
    assert_eq!(parse_pdf_img_ref("epub:/x#1"), None, "書的位址不該被當成 PDF 的圖");
}

/// 在 PDF 頁面上按 V 要看到整頁畫出來的圖,Esc 退回原來那一頁。
#[test]
fn pdf_page_image() {
    let (mut a, _s, _td) = open_doc_pdf();
    a.handle_key(Key::ch('v'));
    assert_eq!(a.mode, Mode::Image, "按 V 沒有進看圖模式(狀態:{})", a.bv.status);
    let m = a.image.as_ref().expect("沒有畫出圖");
    // 612×792 點的頁面在 150 dpi 下大約 1275×1650。
    assert!(m.img.w >= 1200 && m.img.h >= 1500, "畫出來的尺寸是 {}×{}", m.img.w, m.img.h);
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browse, "Esc 之後應該退回瀏覽模式");
}

fn testdata(name: &str) -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../internal/pdf/testdata").join(name)
}

/// 把 internal/pdf/testdata 的一份 PDF 複製成 a.pdf 並打開。
fn open_testdata(name: &str) -> (App, TempDir) {
    let td = TempDir::new();
    let data = std::fs::read(testdata(name)).unwrap_or_else(|e| panic!("沒有測試用的 PDF {name}:{e}"));
    write(td.path().join("a.pdf"), data);
    let mut a = new_at(td.path());
    let mut s = Screen::new(80, 25);
    a.draw(&mut s);
    a.focus_on("a.pdf");
    a.handle_key(named(Code::Enter));
    assert_eq!(a.mode, Mode::Browse, "開 PDF 之後的模式不對");
    (a, td)
}

/// 開一份多頁 PDF 並進到整頁模式。
fn pdf_app() -> (App, TempDir) {
    let (mut a, td) = open_testdata("twocol.pdf");
    a.handle_key(Key::ch('V'));
    assert_eq!(a.mode, Mode::Image, "按 V 之後的模式不對({})", a.bv.status);
    (a, td)
}

fn img(a: &App) -> &wincv_imgview::Model {
    a.image.as_ref().expect("沒有整頁圖")
}

/// PDF 按 V 看整頁之後要能放大,而且是用更高的解析度重畫,不是把 150 dpi 的點陣圖拉大。
#[test]
fn pdf_page_image_zoom() {
    let (mut a, _td) = open_testdata("rich.pdf");
    a.handle_key(Key::ch('V'));
    assert_eq!(a.mode, Mode::Image, "按 V 之後的模式不對({})", a.bv.status);
    let before = img(&a).img.w;
    assert!(before > 0, "整頁圖是空的");

    a.handle_key(Key::ch('+'));
    assert_eq!(img(&a).zoom, 1.5, "按 + 之後倍率不對");
    let after = img(&a).img.w;
    assert!(after > before, "圖沒有被重畫成更大的:{before} → {after}");
    // 重畫過了就不該再點陣放大一次。
    assert_eq!(img(&a).scale(), 1.0, "重畫之後還在點陣放大");

    // 按 1 回到原尺寸,圖要跟著縮回去。
    a.handle_key(Key::ch('1'));
    assert_eq!(img(&a).zoom, 1.0, "按 1 之後倍率不對");
    assert_eq!(img(&a).img.w, before, "回到原尺寸後寬度不對");
}

/// 整頁模式下 PgDn / PgUp 換頁,而不是跳到目錄裡別的圖檔。
#[test]
fn pdf_page_image_paging() {
    let (mut a, _td) = pdf_app();
    assert!(a.pdf.as_ref().unwrap().pages >= 2, "測試用的 PDF 只有一頁");
    let page = |a: &App| parse_pdf_url(&a.bv.url).unwrap().1;
    assert_eq!(page(&a), 1);
    a.handle_key(named(Code::PgDn));
    assert_eq!(page(&a), 2, "PgDn 之後不在第 2 頁");
    assert_eq!(a.mode, Mode::Image, "換頁之後跳出了整頁模式");
    a.handle_key(named(Code::PgUp));
    assert_eq!(page(&a), 1, "PgUp 之後不在第 1 頁");
    // 第一頁再往前不動,而且要講一句:沒反應的按鍵讓人以為程式當了。
    a.message.clear();
    a.handle_key(named(Code::PgUp));
    assert_eq!(page(&a), 1, "到頂還往前走");
    assert!(!a.message.is_empty(), "到頂沒有訊息");
}

/// 換頁要保住放大倍率:讀表格的人翻頁之後還在讀表格。
#[test]
fn pdf_paging_keeps_zoom() {
    let (mut a, _td) = pdf_app();
    a.handle_key(Key::ch('+'));
    a.handle_key(Key::ch('+'));
    let want = img(&a).zoom;
    assert_ne!(want, 1.0, "放大沒生效");
    a.handle_key(named(Code::PgDn));
    assert_eq!(img(&a).zoom, want, "換頁之後倍率變了");
    assert!(!img(&a).fit, "換頁之後回到了「縮放到視窗」");
}

/// Esc 退回瀏覽模式時要看到同一頁的文字,不是進來之前那一頁。
#[test]
fn pdf_paging_updates_text_view() {
    let (mut a, _td) = pdf_app();
    a.handle_key(named(Code::PgDn));
    a.handle_key(named(Code::Esc));
    assert_eq!(a.mode, Mode::Browse, "Esc 之後的模式不對");
    assert_eq!(parse_pdf_url(&a.bv.url).unwrap().1, 2, "退回瀏覽模式後不在第 2 頁");
    assert!(a.bv.title.contains('2'), "標題沒有跟著換頁:{:?}", a.bv.title);
}
