//! 照搬 Go 版 `internal/syntax` 的測試。

use super::*;
use wincv_cell::NAMES;

const C_BODY: &str = "QuoteColor      ltyellow
NumberColor     ltmagenta
LineCommentStart //
========= Ansi C keywords
break      ltred
int        ltorange
return     ltred
";

fn cfg(body: &str) -> Config {
    Config::parse("test", body.as_bytes())
}

fn colors_of(c: &Config, line: &str, in_comment: bool) -> String {
    let r: Vec<char> = line.chars().collect();
    c.highlight_state(line, in_comment)
        .0
        .iter()
        .map(|t| {
            let s: String = r[t.start..t.end].iter().collect();
            format!("{s}:{}", t.color.map_or("-", |c| NAMES[c.0 as usize]))
        })
        .collect::<Vec<_>>()
        .join(" ")
}

#[test]
fn parse_header() {
    let c = cfg(C_BODY);
    assert_eq!(c.quote_color.map(|c| NAMES[c.0 as usize]), Some("ltyellow"));
    assert_eq!(c.number_color.map(|c| NAMES[c.0 as usize]), Some("ltmagenta"));
    assert_eq!(c.line_comment, "//");
    assert_eq!(c.keywords.len(), 3);
}

#[test]
fn highlight_keyword_and_number() {
    let got = colors_of(&cfg(C_BODY), "int x = 42;", false);
    assert!(got.contains("int:ltorange") && got.contains("42:ltmagenta"), "{got}");
}

/// 註解裡的關鍵字不該被上色:判斷順序錯了就會。
#[test]
fn comment_swallows_keywords() {
    let c = cfg(C_BODY);
    let toks = c.highlight("x = 1; // return break");
    let last = toks.last().unwrap();
    assert_eq!(last.color, Some(COMMENT_COLOR));
    let r: Vec<char> = "x = 1; // return break".chars().collect();
    assert_eq!(r[last.start..].iter().collect::<String>(), "// return break");
}

/// 字串裡的關鍵字與符號也不該被上色。
#[test]
fn quote_swallows_content() {
    let got = colors_of(&cfg(C_BODY), r#"s = "return 42";"#, false);
    assert!(!got.contains("return:ltred") && got.contains(r#""return 42":ltyellow"#), "{got}");
}

/// OnlyBeginLineComment=true 時,行中的註解符號不算註解。
#[test]
fn only_begin_line_comment() {
    let c = cfg("LineCommentStart ;\nOnlyBeginLineComment true\n=========\n");
    assert!(c.highlight("key = value ; 這不是註解").iter().all(|t| t.color != Some(COMMENT_COLOR)));
    assert!(c.highlight("  ; 這才是註解").iter().any(|t| t.color == Some(COMMENT_COLOR)));
}

/// 用原版真實的設定檔跑一遍。
#[test]
fn real_config_set() {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../original/app");
    let Ok(s) = Set::load(&dir) else { return };
    for ext in ["c", "h", "cpp", "rc", "java", "xml", "ini", "iss", "txt", "bat"] {
        assert!(s.for_name(&format!("x.{ext}")).is_some(), ".{ext} 沒有對應的設定");
    }
    let cc = s.for_name("main.c").unwrap();
    assert!(cc.keywords.len() >= 100);
    for w in ["break", "auto", "return"] {
        assert!(cc.keywords.contains_key(w), "{w}");
    }
    // .h 與 .c 應該是同一份設定物件
    assert!(Arc::ptr_eq(&s.for_name("a.h").unwrap(), &cc));
}

/// 跨行註解要跨得過行。
#[test]
fn block_comment_spans_lines() {
    let c = cfg(C_BODY);
    assert_eq!((c.block_start.as_str(), c.block_end.as_str()), ("/*", "*/"));
    let (toks, inside) = c.highlight_state("int a; /* 開始", false);
    assert!(inside);
    assert_eq!(toks.last().unwrap().color, Some(COMMENT_COLOR));
    let (toks, inside) = c.highlight_state("return break 都在註解裡", true);
    assert!(inside);
    assert!(toks.iter().all(|t| t.color.is_none_or(|c| c == COMMENT_COLOR)));
    let (_, inside) = c.highlight_state("結束 */ int x;", true);
    assert!(!inside);
    assert!(colors_of(&c, "結束 */ int x;", true).contains("int:ltorange"));
}

/// 單行內開又關的註解,後面的程式碼要正常上色。
#[test]
fn block_comment_inline() {
    assert!(colors_of(&cfg(C_BODY), "int a; /* x */ return b;", false).contains("return:ltred"));
}
