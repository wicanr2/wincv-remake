//! 對應 Go 版 `gopher_test.go`,逐條照搬。伺服器開在 127.0.0.1,不連外。

use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::mpsc;
use std::thread;
use std::time::Duration;

use wincv_markdown::{Kind, Style, default_theme, layout};

use crate::*;

#[test]
fn parse_url_cases() {
    let cases: &[(&str, &str, &str, &[u8], &str, u8)] = &[
        ("gopher://example.org", "example.org", "", b"", "", 0),
        ("gopher://example.org/", "example.org", "", b"", "", 0),
        ("example.org", "example.org", "", b"", "", 0),
        (
            "gopher://example.org:7070/",
            "example.org",
            "7070",
            b"",
            "",
            0,
        ),
        (
            "gopher://example.org/1/dir",
            "example.org",
            "",
            b"/dir",
            "",
            b'1',
        ),
        (
            "gopher://example.org/0/a.txt",
            "example.org",
            "",
            b"/a.txt",
            "",
            b'0',
        ),
        ("example.org/1/dir", "example.org", "", b"/dir", "", b'1'),
        (
            "gopher://example.org/7/find?貓",
            "example.org",
            "",
            b"/find",
            "貓",
            b'7',
        ),
        (
            "gopher://example.org/7/find\t貓",
            "example.org",
            "",
            b"/find",
            "貓",
            b'7',
        ),
        ("gopher://[::1]:70/1/", "[::1]", "70", b"/", "", b'1'),
    ];
    for &(input, host, port, sel, query, typ) in cases {
        let u = parse_url(input).unwrap_or_else(|e| panic!("{input}: 解不開: {e}"));
        assert!(
            u.host == host && u.port == port && u.sel == sel && u.search == query && u.typ == typ,
            "{input}: 得到 {u:?}"
        );
    }
}

#[test]
fn parse_url_rejects() {
    for input in ["", "   ", "http://example.org/", "gopher:///1/x"] {
        if let Ok(u) = parse_url(input) {
            panic!("{input:?} 應該報錯,卻解成 {u:?}");
        }
    }
}

// 型別沒寫的時候要當成選單:那是 gopher 的進入點。
#[test]
fn url_string_defaults_to_menu() {
    let u = parse_url("gopher://example.org").unwrap();
    assert_eq!(u.to_string(), "gopher://example.org/1");
}

// 預設埠不寫進字串,非預設埠要寫。
#[test]
fn url_string_port() {
    let u = parse_url("gopher://example.org:70/1/x").unwrap();
    assert_eq!(u.to_string(), "gopher://example.org/1/x", "預設埠不該出現");
    let u = parse_url("gopher://example.org:7070/1/x").unwrap();
    assert_eq!(
        u.to_string(),
        "gopher://example.org:7070/1/x",
        "非預設埠要出現"
    );
}

const SAMPLE_MENU: &str = "iWelcome to the test server\t\terror.host\t1\r\n\
i\t\terror.host\t1\r\n\
1Documents\t/docs\texample.org\t70\r\n\
0Read me\t/readme.txt\texample.org\t70\r\n\
Ia picture\t/pic.png\texample.org\t70\r\n\
hProject page\tURL:https://example.org/\texample.org\t70\r\n\
i沒有欄位的說明列\r\n\
.\r\n";

#[test]
fn parse_menu_basics() {
    let items = parse_menu(SAMPLE_MENU.as_bytes());
    assert_eq!(items.len(), 7, "解出 {} 列,期望 7", items.len());
    let it = &items[2];
    assert!(
        it.typ == b'1'
            && it.display == "Documents"
            && it.sel == b"/docs"
            && it.host == "example.org"
            && it.port == "70",
        "第 3 列不對: {it:?}"
    );
    assert!(items[2].is_link(), "目錄應該是連結");
    assert!(!items[0].is_link(), "資訊列不該是連結");
    // error.host 是排版用的假主機,不是真的目標。
    assert!(!items[1].is_link(), "error.host 的列不該是連結");
    assert_eq!(
        items[5].web_url().as_deref(),
        Some("https://example.org/"),
        "網頁連結解錯"
    );
}

// [雷] 欄位不足的列不能丟掉:很多站台的說明列只寫 "i文字" 就換行。
// 丟掉的話整段說明消失,而畫面上看起來只是「這個站沒寫什麼」。
#[test]
fn parse_menu_keeps_short_lines() {
    let items = parse_menu(SAMPLE_MENU.as_bytes());
    let last = items.last().unwrap();
    assert_eq!(
        last.display, "沒有欄位的說明列",
        "欄位不足的列被丟掉了,最後一列是 {last:?}"
    );
}

// "." 之後的東西不是內容。
#[test]
fn parse_menu_stops_at_terminator() {
    let items = parse_menu(b"iA\t\t\t\r\n.\r\niB\t\t\t\r\n");
    assert_eq!(items.len(), 1);
}

// Big5 的選單:顯示文字要解碼,selector 留原始位元組。
// 連結點下去要原封送回伺服器,換過編碼的 selector 對方認不得。
#[test]
fn parse_menu_decodes_big5() {
    let sel: &[u8] = b"/\xa5\xd8\xbf\xfd"; // 「/目錄」的 Big5
    let mut menu = b"i\xbb\xa1\xa9\xfa\t\terror.host\t1\r\n".to_vec(); // 「說明」
    menu.extend_from_slice(b"1\xa4\xa4\xa4\xe5\xa5\xd8\xbf\xfd\t"); // 「中文目錄」
    menu.extend_from_slice(sel);
    menu.extend_from_slice(b"\texample.org\t70\r\n.\r\n");
    let items = parse_menu(&menu);
    assert_eq!(items.len(), 2, "解出 {} 列,期望 2", items.len());
    assert_eq!(
        (items[0].display.as_str(), items[1].display.as_str()),
        ("說明", "中文目錄"),
        "顯示文字沒有解碼"
    );
    assert_eq!(items[1].sel, sel, "selector 被改動了");
    let mut want = b"gopher://example.org/1".to_vec();
    want.extend_from_slice(sel);
    assert_eq!(
        items[1].url().to_bytes(),
        want,
        "位址裡的 selector 不是原樣"
    );
}

// 編碼要整份選單一起判讀。「目录」(GBK)單看那一列,Big5 與 GBK 都解得開,
// 平手時判成 Big5;同一份選單的另一列有「中」「的」,整份看就是 GBK。
#[test]
fn parse_menu_detects_whole_menu() {
    let menu: &[u8] =
        b"i\xbc\xf2\xcc\xe5\xd6\xd0\xce\xc4\xb5\xc4\xcb\xb5\xc3\xf7\t\terror.host\t1\r\n\
1\xc4\xbf\xc2\xbc\t/dir\texample.org\t70\r\n.\r\n"; // 「简体中文的说明」「目录」
    let items = parse_menu(menu);
    assert_eq!(items.len(), 2, "解出 {} 列,期望 2", items.len());
    assert_eq!(
        (items[0].display.as_str(), items[1].display.as_str()),
        ("简体中文的说明", "目录")
    );
}

// UTF-8 的選單不能被判成別的編碼。有一列的型別字元不是 ASCII(漏寫型別的
// 說明列)時,只拿顯示文字判讀會從中間切斷一個 UTF-8 字,整份被誤判成 GBK。
#[test]
fn parse_menu_keeps_utf8() {
    let menu = "1目錄\t/dir\texample.org\t70\r\n中文說明沒有寫型別\r\n.\r\n";
    let items = parse_menu(menu.as_bytes());
    assert_eq!(items.len(), 2, "解出 {items:?}");
    assert_eq!(items[0].display, "目錄");
}

#[test]
fn menu_blocks_shape() {
    let blocks = menu_blocks(&parse_menu(SAMPLE_MENU.as_bytes()));
    assert_eq!(blocks.len(), 7);
    // 連結是 List(排版引擎在連續的 List 之間不插空行,選單要單倍行距),
    // 資訊列是 Pre(原樣輸出,不加項目符號:那是站台自己排的版面)。
    for (i, b) in blocks.iter().enumerate() {
        assert!(
            b.kind == Kind::List || b.kind == Kind::Pre,
            "第 {i} 個區塊是 {:?}",
            b.kind
        );
    }
    assert!(
        blocks[0].kind == Kind::Pre && blocks[0].lines[0] == "Welcome to the test server",
        "資訊列應該是原樣的 Pre,得到 {:?}",
        blocks[0]
    );
    assert_eq!(blocks[2].marker, "[目錄] ", "目錄的標籤");
    let sp = &blocks[2].spans[0];
    assert!(
        sp.style.has(Style::LINK) && sp.href == "gopher://example.org/1/docs",
        "連結不對: {sp:?}"
    );
    // 資訊列不給 href 也不給標籤:不該長得像可以點的東西。
    assert!(
        blocks[0].spans.is_empty() && blocks[0].marker.is_empty(),
        "資訊列不該有 span 或標籤: {:?}",
        blocks[0]
    );
}

// 排版之後 href 要還在,不然畫得出底線卻沒有東西可以點。
#[test]
fn link_survives_layout() {
    let blocks = menu_blocks(&parse_menu(SAMPLE_MENU.as_bytes()));
    let (lines, _) = layout(&blocks, 60, 8, 16, None, default_theme());
    let hrefs: Vec<&str> = lines
        .iter()
        .flat_map(|l| l.pieces.iter())
        .filter(|p| !p.href.is_empty())
        .map(|p| p.href.as_str())
        .collect();
    assert!(!hrefs.is_empty(), "排版之後一個連結目標都沒有");
    assert_eq!(hrefs[0], "gopher://example.org/1/docs");
}

// gopher 的文字內容常常是排好版的,不能重新斷行。
#[test]
fn text_blocks_is_preformatted() {
    let blocks = text_blocks("  第一列\r\n     縮排的第二列\r\n.\r\n".as_bytes());
    assert!(
        blocks.len() == 1 && blocks[0].kind == Kind::Pre,
        "得到 {blocks:?}"
    );
    assert_eq!(blocks[0].lines.len(), 2, "結束符不算內容");
    assert_eq!(blocks[0].lines[1], "     縮排的第二列", "縮排被吃掉了");
}

// Big5 的站台要判讀得出來:gopher 沒有 charset 欄位。
#[test]
fn text_blocks_detects_big5() {
    let big5 = [0xa4, 0xe5, 0xa6, 0x72, 0xc0, 0xc9]; // 「文字檔」
    let blocks = text_blocks(&big5);
    assert_eq!(blocks.len(), 1);
    assert_eq!(blocks[0].lines[0], "文字檔");
}

/// 起一個只回固定內容的 gopher 伺服器,回傳埠與收到的 selector。
fn serve(reply: Vec<u8>) -> (u16, mpsc::Receiver<Vec<u8>>) {
    let ln = TcpListener::bind("127.0.0.1:0").unwrap();
    let port = ln.local_addr().unwrap().port();
    let (tx, rx) = mpsc::channel();
    thread::spawn(move || {
        let Ok((mut c, _)) = ln.accept() else { return };
        let mut buf = [0u8; 512];
        let n = c.read(&mut buf).unwrap_or(0);
        let mut got = buf[..n].to_vec();
        while got.last().is_some_and(|&b| b == b'\r' || b == b'\n') {
            got.pop();
        }
        let _ = tx.send(got);
        let _ = c.write_all(&reply);
    });
    (port, rx)
}

fn client(max: u64) -> Client {
    Client {
        timeout: Duration::from_secs(5),
        max_bytes: max,
        dial: None,
    }
}

#[test]
fn fetch_menu() {
    let (port, got) = serve(SAMPLE_MENU.as_bytes().to_vec());
    let u = Url {
        host: "127.0.0.1".into(),
        port: port.to_string(),
        typ: b'1',
        sel: b"/docs".to_vec(),
        ..Default::default()
    };
    let data = client(0)
        .fetch(&u)
        .unwrap_or_else(|e| panic!("取不到: {e}"));
    assert_eq!(got.recv().unwrap(), b"/docs", "伺服器收到的 selector");
    assert_eq!(parse_menu(&data).len(), 7, "回來的內容解不出選單");
}

// 型別 7 的查詢字串接在 selector 後面,中間一個 tab。
#[test]
fn fetch_search_sends_tab() {
    let (port, got) = serve(b".\r\n".to_vec());
    let u = Url {
        host: "127.0.0.1".into(),
        port: port.to_string(),
        typ: b'7',
        sel: b"/find".to_vec(),
        search: "貓".into(),
    };
    client(0).fetch(&u).unwrap();
    assert_eq!(
        got.recv().unwrap(),
        "/find\t貓".as_bytes(),
        "送出的不是 \"/find\\t貓\""
    );
}

// 沒有 Content-Length,伺服器可以一直送。上限要真的截斷並說出來。
#[test]
fn fetch_truncates() {
    let (port, _) = serve(vec![b'x'; 5000]);
    let u = Url {
        host: "127.0.0.1".into(),
        port: port.to_string(),
        ..Default::default()
    };
    match client(100).fetch(&u) {
        Err(FetchError::TooLarge(data)) => assert_eq!(data.len(), 100, "截到的長度"),
        other => panic!("期望 TooLarge,得到 {other:?}"),
    }
}

#[test]
fn fetch_needs_host() {
    assert!(
        Client::default().fetch(&Url::default()).is_err(),
        "沒有主機名應該報錯"
    );
}

// 撥號函式換得掉:測試與特殊環境不必真的連線。
#[test]
fn fetch_uses_custom_dial() {
    struct Fake(std::io::Cursor<Vec<u8>>, Vec<u8>);
    impl Read for Fake {
        fn read(&mut self, b: &mut [u8]) -> std::io::Result<usize> {
            self.0.read(b)
        }
    }
    impl Write for Fake {
        fn write(&mut self, b: &[u8]) -> std::io::Result<usize> {
            self.1.extend_from_slice(b);
            Ok(b.len())
        }
        fn flush(&mut self) -> std::io::Result<()> {
            Ok(())
        }
    }
    impl Conn for Fake {}
    let seen = std::sync::Arc::new(std::sync::Mutex::new(String::new()));
    let s2 = seen.clone();
    let c = Client {
        dial: Some(std::sync::Arc::new(move |addr: &str, _| {
            *s2.lock().unwrap() = addr.to_string();
            Ok(Box::new(Fake(
                std::io::Cursor::new(b"iok\r\n.\r\n".to_vec()),
                Vec::new(),
            )) as Box<dyn Conn>)
        })),
        ..Default::default()
    };
    let data = c
        .fetch(&parse_url("gopher://example.org/1/").unwrap())
        .unwrap();
    assert_eq!(*seen.lock().unwrap(), "example.org:70");
    assert_eq!(parse_menu(&data)[0].display, "ok");
}
