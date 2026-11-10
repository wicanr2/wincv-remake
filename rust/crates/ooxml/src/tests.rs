//! Go 版的 ooxml 沒有單元測試;這裡的案例是移植時為了釘住 Go 行為補的。
//! 預期值取自 Go 1.22.12 的 `path` 套件說明與 `encoding/xml` 的實際輸出。

use super::*;

#[test]
fn go_path_clean() {
    // 取自 Go 的 path_test.go(cleantests)。
    let cases = [
        ("", "."),
        ("abc", "abc"),
        ("abc/def", "abc/def"),
        ("a/b/c", "a/b/c"),
        (".", "."),
        ("..", ".."),
        ("../..", "../.."),
        ("../../abc", "../../abc"),
        ("/abc", "/abc"),
        ("/", "/"),
        ("abc/", "abc"),
        ("abc/def/", "abc/def"),
        ("a/b/c/", "a/b/c"),
        ("./", "."),
        ("../", ".."),
        ("../../", "../.."),
        ("/abc/", "/abc"),
        ("abc//def//ghi", "abc/def/ghi"),
        ("//abc", "/abc"),
        ("///abc", "/abc"),
        ("//abc//", "/abc"),
        ("abc//", "abc"),
        ("abc/./def", "abc/def"),
        ("/./abc/def", "/abc/def"),
        ("abc/.", "abc"),
        ("abc/def/ghi/../jkl", "abc/def/jkl"),
        ("abc/def/../ghi/../jkl", "abc/jkl"),
        ("abc/def/..", "abc"),
        ("abc/def/../..", "."),
        ("/abc/def/../..", "/"),
        ("abc/def/../../..", ".."),
        ("/abc/def/../../..", "/"),
        ("abc/def/../../../ghi/jkl/../../../mno", "../../mno"),
        ("abc/./../def", "def"),
        ("abc//./../def", "def"),
        ("abc/../../././../def", "../../def"),
    ];
    for (i, w) in cases {
        assert_eq!(gopath::clean(i), w, "clean({i:?})");
    }
}

#[test]
fn resolve_relative_to_owner() {
    assert_eq!(resolve("ppt/slides/slide1.xml", "../media/image1.png"), "ppt/media/image1.png");
    assert_eq!(resolve("", "word/document.xml"), "word/document.xml");
    assert_eq!(resolve("word/document.xml", "/word/media/a.png"), "word/media/a.png");
    assert_eq!(rels_path("ppt/slides/slide1.xml"), "ppt/slides/_rels/slide1.xml.rels");
    assert_eq!(rels_path(""), "_rels/.rels");
}

fn tokens(src: &str) -> Vec<String> {
    let mut d = new_decoder(src.as_bytes().to_vec());
    let mut out = Vec::new();
    loop {
        match d.token() {
            Ok(Token::Start(se)) => out.push(format!("<{}:{}>", se.name.space, se.name.local)),
            Ok(Token::End(n)) => out.push(format!("</{}:{}>", n.space, n.local)),
            Ok(Token::CharData(s)) => out.push(format!("{s:?}")),
            Ok(_) => {}
            Err(e) => {
                out.push(format!("ERR {e}"));
                break;
            }
        }
    }
    out
}

/// HTMLAutoClose 把 local name 叫 br 的元素一打開就關掉。底下有子元素時,
/// 後面那個 `</a:br>` 對不上,非嚴格模式把它改成關閉外層,並一路往上關。
/// 這是 Go 版實際的行為(移植時照抄,見 xml 模組的說明)。
#[test]
fn auto_close_cascade_like_go() {
    let got = tokens(r#"<r xmlns:a="u"><a:p><a:br><a:rPr/></a:br><a:t>x</a:t></a:p></r>"#);
    let want = [
        "<:r>", "<u:p>", "<u:br>", "</u:br>", "<u:rPr>", "</u:rPr>", "</a:p>", "</:r>",
        "ERR XML syntax error on line 1: unexpected end element </br>",
    ];
    assert_eq!(got, want);
}

#[test]
fn entities_and_newlines() {
    let got = tokens("<a>&lt;&nbsp;&bogus;&#x4E2D;\r\n&#xD800;</a>");
    assert_eq!(got, ["<:a>", "\"<\\u{a0}&bogus;中\\n\u{fffd}\"", "</:a>", "ERR EOF"]);
}

#[test]
fn unquoted_and_valueless_attributes() {
    let mut d = new_decoder(b"<a b=c d></a>".to_vec());
    let Token::Start(se) = d.token().unwrap() else { panic!() };
    assert_eq!(attr(&se, "b"), "c");
    assert_eq!(attr(&se, "d"), "d");
}
