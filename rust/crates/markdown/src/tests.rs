//! 對應 Go 版 `markdown_test.go`,逐條照搬。

use std::sync::Arc;

use wincv_render::Image;

use crate::inline::inline;
use crate::wrap::width;
use crate::*;

fn text_of(ls: &[Line]) -> String {
    let mut b = String::new();
    for l in ls {
        for p in &l.pieces {
            b.push_str(&p.text);
        }
        b.push('\n');
    }
    b
}

fn lay(src: &str, cols: i32) -> (Vec<Line>, Vec<Arc<Pic>>) {
    layout(&parse(src), cols, 8, 16, None, default_theme())
}

#[test]
fn headings_and_paragraph() {
    let (ls, _) = lay("# 標題\n\n一段文字。\n\n## 次標題\n", 40);
    let got = text_of(&ls);
    for want in ["# 標題", "一段文字。", "## 次標題"] {
        assert!(got.contains(want), "少了 {want:?}\n{got}");
    }
}

/// setext 標題(底下一行 === 或 ---)不能被當成段落 + 水平線。
#[test]
fn setext_heading() {
    let (ls, _) = lay("標題\n====\n\n內文\n", 40);
    let got = text_of(&ls);
    assert!(got.contains("# 標題"), "setext 標題沒有被認出來:\n{got}");
}

/// 程式碼區塊裡的東西一律原樣保留,那正是它的用途。
/// 裡面的 # 不是標題,* 不是清單,- 不是水平線。
#[test]
fn code_fence_keeps_content_verbatim() {
    let src = "```go\n# not a heading\n* not a list\n---\n```\n";
    let (ls, _) = lay(src, 40);
    let got = text_of(&ls);
    for want in ["# not a heading", "* not a list", "---"] {
        assert!(got.contains(want), "程式碼區塊的 {want:?} 被改掉了:\n{got}");
    }
}

/// 落單的星號不能讓後面整段變成斜體。
#[test]
fn unpaired_emphasis_is_literal() {
    let sp = inline("2 * 3 = 6");
    let mut b = String::new();
    for s in &sp {
        b.push_str(&s.text);
        assert_eq!(s.style, Style::default(), "{:?} 不該有樣式 {:?}", s.text, s.style);
    }
    assert_eq!(b, "2 * 3 = 6", "文字被吃掉了");
}

#[test]
fn inline_styles() {
    let sp = inline("普通 **粗** 與 *斜* 與 `碼` 與 [連結](http://x/y)");
    let find = |txt: &str| -> Span {
        sp.iter().find(|s| s.text == txt).cloned().unwrap_or_else(|| panic!("找不到片段 {txt:?},實際 {sp:?}"))
    };
    assert!(find("粗").style.has(Style::BOLD), "粗體沒認出來");
    assert!(find("斜").style.has(Style::ITALIC), "斜體沒認出來");
    assert!(find("碼").style.has(Style::MONO), "行內程式碼沒認出來");
    let l = find("連結");
    assert!(l.style.has(Style::LINK) && l.href == "http://x/y", "連結 = {l:?}");
}

/// [![alt](img)](url) 這種巢狀在 README 裡很常見。
/// 只找第一個 ] 會切在錯的地方,把後面整行吃掉。
#[test]
fn nested_image_link() {
    let sp = inline("[![badge](b.svg)](https://ci/x) 後面的字");
    assert!(sp.len() >= 2, "解析結果太短: {sp:?}");
    assert!(sp[0].href == "https://ci/x" && sp[0].text == "badge", "巢狀連結 = {:?}", sp[0]);
    assert!(sp[sp.len() - 1].text.contains("後面的字"), "後面的文字被吃掉了: {sp:?}");
}

/// 中文不靠空白斷字。只按空白折的話,一整段中文會變成一列超長的字。
#[test]
fn wrap_cjk() {
    let long = "中文".repeat(30);
    let (ls, _) = lay(&long, 20);
    for (i, l) in ls.iter().enumerate() {
        let w: i32 = l.pieces.iter().map(|p| width(&p.text)).sum();
        assert!(w <= 20, "第 {i} 列寬 {w},超過 20");
    }
    assert!(ls.len() >= 5, "只折成 {} 列,應該更多", ls.len());
}

/// 超長且沒有空白的字串(例如網址)要硬切,不能無限卡住。
#[test]
fn wrap_very_long_word() {
    let (ls, _) = lay(&"a".repeat(200), 20);
    assert!(ls.len() >= 5 && ls.len() <= 40, "折成 {} 列,不合理", ls.len());
    for l in &ls {
        let w: i32 = l.pieces.iter().map(|p| width(&p.text)).sum();
        assert!(w <= 20, "有一列寬 {w}");
    }
}

#[test]
fn table() {
    let src = "| 名稱 | 大小 |\n|---|---:|\n| a.txt | 12 |\n| b.bin | 3400 |\n";
    let (ls, _) = lay(src, 40);
    let got = text_of(&ls);
    for want in ["名稱", "a.txt", "3400"] {
        assert!(got.contains(want), "表格少了 {want:?}:\n{got}");
    }
}

/// 圖片載不到時要把 alt 與原因寫出來。留白的話使用者會以為本來就沒東西。
#[test]
fn image_failure_is_visible() {
    let (ls, pics) = lay("![一張圖](a.png)\n", 40);
    assert_eq!(pics.len(), 1, "圖片數");
    assert!(text_of(&ls).contains("一張圖"), "alt 沒顯示:\n{}", text_of(&ls));
}

/// 「哪些位址可以載」是載入器的政策,排版引擎一律照送。
///
/// 這一條之前是反過來的:Layout 自己擋掉 http 開頭的位址。那對「開一份
/// .md 檔」是對的,但同一個引擎也服務使用者自己輸入網址的瀏覽模式,
/// 在那裡擋掉等於整頁的圖都不會出現。政策移到各自的載入器上。
#[test]
fn remote_image_passed_to_loader() {
    let got = std::cell::RefCell::new(String::new());
    let load = |src: &str| -> Result<Arc<Image>, String> {
        *got.borrow_mut() = src.to_string();
        Err("載入器說不行".to_string())
    };
    let (_, pics) = layout(&parse("![x](https://example.com/a.png)"), 40, 8, 16, Some(&load), default_theme());
    assert_eq!(*got.borrow(), "https://example.com/a.png", "載入器收到的位址");
    assert!(pics.len() == 1 && pics[0].err == "載入器說不行", "載入器的說法沒有傳出來: {pics:?}");
}

/// 圖片要佔掉整數列,而且後面的文字要接在圖的下面,不能疊在上面。
#[test]
fn image_occupies_rows() {
    let load = |_: &str| -> Result<Arc<Image>, String> { Ok(Arc::new(Image::new(160, 160))) };
    let (ls, pics) = layout(&parse("![x](a.png)\n\n後面\n"), 40, 8, 16, Some(&load), default_theme());
    assert!(pics.len() == 1 && pics[0].img.is_some(), "圖沒載進來: {pics:?}");
    let p = &pics[0];
    assert!(p.rows >= 2 && p.cols >= 2, "尺寸 = {}×{} 格", p.cols, p.rows);
    let mine = |l: &Line| l.pic.as_ref().is_some_and(|q| Arc::ptr_eq(q, p));
    let n = ls.iter().filter(|l| mine(l)).count() as i32;
    assert_eq!(n, p.rows, "圖佔了 {n} 列,宣告 {} 列", p.rows);
    // 「後面」必須在圖之後
    let last = ls.iter().rposition(mine).map_or(-1, |i| i as i64);
    let found = ls.iter().rposition(|l| l.pieces.iter().any(|pc| pc.text.contains("後面"))).map_or(-1, |i| i as i64);
    assert!(found > last, "文字在第 {found} 列,圖到第 {last} 列,疊到一起了");
}

/// 一張很大的圖不能把整個畫面吃掉。
#[test]
fn image_rows_capped() {
    let load = |_: &str| -> Result<Arc<Image>, String> { Ok(Arc::new(Image::new(100, 8000))) };
    let (_, pics) = layout(&parse("![x](a.png)"), 40, 8, 16, Some(&load), default_theme());
    assert!(pics[0].rows <= MAX_PIC_ROWS, "佔了 {} 列,上限是 {MAX_PIC_ROWS}", pics[0].rows);
}

#[test]
fn list() {
    let (ls, _) = lay("- 第一\n- 第二\n\n1. 甲\n2. 乙\n", 40);
    let got = text_of(&ls);
    for want in ["• 第一", "• 第二", "1. 甲", "2. 乙"] {
        assert!(got.contains(want), "清單少了 {want:?}:\n{got}");
    }
}

/// `---` 單獨出現是水平線,接在一行文字後面是二級標題。
/// 同一串字元、兩種語意,差別只在前一行有沒有東西。
#[test]
fn dashes_rule_vs_setext() {
    let (ls, _) = lay("一段文字\n\n---\n\n另一段\n", 40);
    assert!(!text_of(&ls).contains("# 一段文字"), "空行之後的 --- 被當成標題了:\n{}", text_of(&ls));
    let (ls, _) = lay("一段文字\n---\n\n另一段\n", 40);
    assert!(text_of(&ls).contains("## 一段文字"), "緊接在文字後的 --- 應該是二級標題:\n{}", text_of(&ls));
}

/// 標題底下要有一條線。看不見的顏色等於沒有這條線,所以順便盯著顏色。
#[test]
fn heading_rule() {
    let (ls, _) = lay("# 標題\n", 20);
    let mut found = false;
    for l in &ls {
        for p in &l.pieces {
            if p.text.contains("───") {
                found = true;
                // DkGray:#404040,畫在黑底上等於看不見
                assert_ne!(p.fg, wincv_cell::Color::DK_GRAY, "分隔線用了看不見的顏色");
            }
        }
    }
    assert!(found, "標題底下沒有線:\n{}", text_of(&ls));
}

fn lines_of(ls: &[Line]) -> Vec<String> {
    text_of(ls).trim_end_matches('\n').split('\n').map(String::from).collect()
}

fn span(t: &str) -> Span {
    Span { text: t.into(), ..Default::default() }
}

/// 折到下一列的詞不能帶著前導空白:那會讓續行多一格縮排。
#[test]
fn wrapped_lines_do_not_start_with_space() {
    let (ls, _) = lay("alpha beta gamma delta epsilon zeta eta theta", 12);
    for (i, l) in lines_of(&ls).iter().enumerate() {
        assert!(!l.starts_with(' '), "第 {i} 列以空白開頭: {l:?}");
    }
}

/// 段落後面接清單要空一行;清單項目之間、Pre 後面接清單(gopher 選單)不空。
#[test]
fn blank_line_before_list() {
    let (ls, _) = lay("段落\n- 一\n- 二\n", 40);
    assert_eq!(text_of(&ls), "段落\n\n• 一\n• 二\n");
    let blocks = vec![
        Block { kind: Kind::Pre, lines: vec!["說明".into()], ..Default::default() },
        Block { kind: Kind::List, marker: "[目錄] ".into(), spans: vec![span("項目")], ..Default::default() },
        Block { kind: Kind::List, marker: "[文字] ".into(), spans: vec![span("另一項")], ..Default::default() },
    ];
    let (ls, _) = layout(&blocks, 40, 8, 16, None, default_theme());
    assert_eq!(text_of(&ls), "說明\n[目錄] 項目\n[文字] 另一項\n", "gopher 選單被空行拆開了");
}

/// 清單記號含全形字時,續行縮排與折行寬度要以顯示格數算。
#[test]
fn list_marker_width_is_display_width() {
    let blocks = vec![Block { kind: Kind::List, marker: "[目錄] ".into(), spans: vec![span("aaaa bbbb cccc dddd")], ..Default::default() }];
    let (ls, _) = layout(&blocks, 16, 8, 16, None, default_theme());
    let lines = lines_of(&ls);
    assert!(lines.len() >= 2, "沒有折行: {lines:?}");
    for (i, l) in lines.iter().enumerate() {
        assert!(width(l) <= 16, "第 {i} 列寬 {},超過 16: {l:?}", width(l));
    }
    assert!(lines[1].starts_with(&" ".repeat(width("[目錄] ") as usize)), "續行縮排沒有對齊記號: {:?}", lines[1]);
}

/// Pre 與 Code 的裁切以顯示格數算,全形字不能讓一列超出畫面。
#[test]
fn pre_and_code_clip_by_display_width() {
    let long = "中".repeat(30);
    let blocks = vec![
        Block { kind: Kind::Pre, lines: vec![long.clone()], ..Default::default() },
        Block { kind: Kind::Code, lines: vec![long], ..Default::default() },
    ];
    let (ls, _) = layout(&blocks, 20, 8, 16, None, default_theme());
    for (i, l) in lines_of(&ls).iter().enumerate() {
        assert!(width(l) <= 20, "第 {i} 列寬 {},超過 20", width(l));
    }
}

/// tab 的停點以顯示格數算。
#[test]
fn expand_tabs_uses_display_width() {
    assert_eq!(crate::wrap::expand_tabs("中\tx"), "中  x");
    assert_eq!(crate::wrap::expand_tabs("ab\tc"), "ab  c");
}

/// 全是空格子的表格在很窄的畫面上不能除以零。
#[test]
fn empty_table_on_narrow_screen() {
    let row = vec![String::new(), String::new(), String::new()];
    let blocks = vec![Block { kind: Kind::Table, rows: vec![row.clone(), row], ..Default::default() }];
    layout(&blocks, 8, 8, 16, None, default_theme()); // 不能 panic
}

/// 底線只能接在段落文字後面:清單或引言後面的 === / --- 不是標題。
#[test]
fn setext_only_after_paragraph() {
    for src in ["- 項目\n===\n", "3) 括號序號\n---\n", "> 引言\n===\n"] {
        assert!(parse(src).iter().all(|b| b.kind != Kind::Heading), "{src:?} 被當成標題");
    }
    let bs = parse("文字\n===\n");
    assert!(bs.len() == 1 && bs[0].kind == Kind::Heading, "段落後面的 === 應該是標題");
}

/// ATX 標題結尾的 # 串前面要有空白才是收尾記號。
#[test]
fn atx_closing_sequence() {
    for (src, want) in [("# 標題 C#", "標題 C#"), ("# 標題 C# ##", "標題 C#"), ("## 標題 ##", "標題"), ("# ###", ""), ("# a#b", "a#b")] {
        let bs = parse(src);
        assert!(bs.len() == 1 && bs[0].kind == Kind::Heading, "{src:?} 不是標題");
        let got: String = bs[0].spans.iter().map(|s| s.text.as_str()).collect();
        assert_eq!(got, want, "{src:?}");
    }
}

/// 表格格子裡的 tab 展開成空白。
#[test]
fn table_cell_tab_expanded() {
    let bs = parse("| a\tb | c |\n|---|---|\n| d | e |\n");
    assert!(bs.len() == 1 && bs[0].kind == Kind::Table);
    assert!(!bs[0].rows[0][0].contains('\t'), "格子裡還有 tab: {:?}", bs[0].rows[0][0]);
}

/// 有序清單的序號最多 9 位;再長就不是清單(不然序號會溢位成別的數字)。
#[test]
fn ordered_list_number_limit() {
    let bs = parse("123456789. 項目\n");
    assert!(bs.len() == 1 && bs[0].kind == Kind::List && bs[0].num == 123456789);
    let bs = parse("99999999999999999999. 項目\n");
    assert!(bs.len() == 1 && bs[0].kind == Kind::Para, "20 位序號不該是清單");
}
