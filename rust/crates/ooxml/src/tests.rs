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

/// `new_decoder` 不自動關閉 br:PowerPoint 的 `<a:br>` 底下帶著 `<a:rPr/>`,
/// 結構要照檔案原樣解出來,`</a:br>` 之後的內容留在原本的段落裡。
#[test]
fn br_with_children_keeps_structure() {
    let got = tokens(r#"<r xmlns:a="u"><a:p><a:br><a:rPr/></a:br><a:t>x</a:t></a:p></r>"#);
    let want = [
        "<:r>", "<u:p>", "<u:br>", "<u:rPr>", "</u:rPr>", "</u:br>", "<u:t>", "\"x\"", "</u:t>", "</u:p>", "</:r>",
        "ERR EOF",
    ];
    assert_eq!(got, want);
}

/// 錯配的結束標記在非嚴格模式下改成關閉堆疊頂端,一路往上關(與 Go 相同)。
#[test]
fn mismatched_end_closes_outward() {
    let got = tokens(r#"<r><p><b>x</p></r>"#);
    let want = ["<:r>", "<:p>", "<:b>", "\"x\"", "</:b>", "</:p>", "</:r>", "ERR EOF"];
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
