//! 照搬 Go 版 `internal/convert` 的測試。

use super::*;

#[test]
fn eol() {
    // 混用兩種換行的檔案也要被統一:這正是原版拿來修壞掉檔案的用法。
    let mixed = b"a\r\nb\nc\rd";
    assert_eq!(to_eol(mixed, Eol::Unix), b"a\nb\nc\nd");
    assert_eq!(to_eol(mixed, Eol::Pc), b"a\r\nb\r\nc\r\nd");
    assert_eq!(to_eol(b"a\r\nb\r\n", Eol::Unix), b"a\nb\n");
    assert_eq!(detect_eol(b"a\r\nb\r\n"), Eol::Pc);
    assert_eq!(detect_eol(b"a\nb\n"), Eol::Unix);
    assert_eq!(detect_eol(b"a\rb\r"), Eol::Mac);
}

/// 轉碼是原版的招牌功能,round-trip 一定要對得回來。
#[test]
fn recode_round_trip() {
    let zh = "檔案瀏覽壓縮管理";
    let b5 = recode(zh.as_bytes(), Enc::Utf8, Enc::Big5).unwrap();
    assert_eq!(wincv_textenc::detect(&b5), Enc::Big5);
    assert_eq!(recode(&b5, Enc::Big5, Enc::Utf8).unwrap(), zh.as_bytes());
    assert!(recode("한".as_bytes(), Enc::Utf8, Enc::Big5).unwrap_err().contains('한'));
}

/// 繁簡互轉一定會遇到對不過去的字,不可以整份失敗。
#[test]
fn recode_lossy_keeps_what_fits() {
    let got = recode_lossy("あいう漢字".as_bytes(), Enc::Utf8, Enc::EucKr, "?");
    assert!(!got.is_empty());
    assert!(decode(&got, Enc::EucKr).contains('漢'));
}

#[test]
fn strip_ansi_codes() {
    assert_eq!(strip_ansi("\x1b[1;31m紅\x1b[0m白\x1b[44m底\x1b[m"), "紅白底");
}

/// script/style 的內容也要拿掉,不能只拆標籤留下程式碼。
#[test]
fn strip_html_drops_script_and_style() {
    let got = strip_html(
        "<html><head><style>body{color:red}</style>\n<script>var x = 1 < 2;</script></head>\n\
         <body><p>第一段</p><p>第二段<br>換行</p>\n<a href=\"#\">連結</a> &amp; &lt;實體&gt;</body></html>",
    );
    for bad in ["color:red", "var x", "<p>", "href"] {
        assert!(!got.contains(bad), "{bad}:\n{got}");
    }
    for want in ["第一段", "第二段", "換行", "連結", "& <實體>"] {
        assert!(got.contains(want), "{want}:\n{got}");
    }
}

/// &amp;lt; 表示的是字面上的 "&lt;",不是 "<"。
#[test]
fn entity_order() {
    assert_eq!(strip_html("<p>&amp;lt;</p>"), "&lt;");
}

#[test]
fn rename_case_modes() {
    for (input, c, want) in [
        ("MyFile.TXT", NameCase::Lower, "myfile.txt"),
        ("MyFile.TXT", NameCase::Upper, "MYFILE.TXT"),
        ("myFILE.TXT", NameCase::Title, "Myfile.txt"),
        ("noext", NameCase::Upper, "NOEXT"),
        (".hidden", NameCase::Upper, ".HIDDEN"),
        ("a.b.c", NameCase::Lower, "a.b.c"),
        // 首字不是 ASCII:中文沒有大小寫,é 轉成 É。Go 版這兩個會弄壞檔名。
        ("中文.TXT", NameCase::Title, "中文.txt"),
        ("éCOLE.DOC", NameCase::Title, "École.doc"),
    ] {
        assert_eq!(rename_case(input, c), want, "{input} {c:?}");
    }
}

#[test]
fn numbered_rename_pads() {
    for (pat, n, want) in [
        ("pic###.jpg", 7, "pic007.jpg"),
        ("pic###.jpg", 123, "pic123.jpg"),
        ("pic#.jpg", 5, "pic5.jpg"),
        ("pic#.jpg", 42, "pic42.jpg"), // 位數不夠就長出來,不截斷
        ("noplaceholder.jpg", 3, "noplaceholder.jpg"),
    ] {
        assert_eq!(numbered_rename(pat, n), want);
    }
}
