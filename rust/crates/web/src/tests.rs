//! 對應 Go 版 `web_test.go`,逐條照搬。HTTP 伺服器開在 127.0.0.1,不連外。

use std::io::{BufRead, BufReader, Write};
use std::net::TcpListener;
use std::sync::Arc;
use std::thread;
use std::time::Duration;

use wincv_markdown::{Block, Kind};

use crate::*;

/// 把區塊攤成「種類:文字」的字串陣列,好比對。
fn blocks(bs: &[Block]) -> Vec<String> {
    bs.iter()
        .map(|b| {
            let txt = match b.kind {
                Kind::Pre => b.lines.join("\\n"),
                Kind::Image => format!("{} [{}]", b.src, b.alt),
                _ => b.spans.iter().map(|s| s.text.as_str()).collect(),
            };
            format!("{}:{}", kind_name(b.kind), txt)
        })
        .collect()
}

fn kind_name(k: Kind) -> &'static str {
    match k {
        Kind::Para => "P",
        Kind::Heading => "H",
        Kind::List => "LI",
        Kind::Quote => "Q",
        Kind::Pre => "PRE",
        Kind::Rule => "HR",
        Kind::Image => "IMG",
        _ => "?",
    }
}

fn parse(base: &str, src: &str) -> (String, Vec<Block>) {
    parse_html(
        if base.is_empty() { None } else { Some(base) },
        src.as_bytes(),
    )
}

fn hrefs(bs: &[Block]) -> Vec<String> {
    bs.iter()
        .flat_map(|b| b.spans.iter())
        .filter(|s| !s.href.is_empty())
        .map(|s| s.href.clone())
        .collect()
}

#[test]
fn parse_html_basics() {
    let (title, bs) = parse(
        "",
        r#"
<html><head><title>測試頁</title></head>
<body>
  <h1>標題</h1>
  <p>第一段。<b>粗</b>的字。</p>
  <ul><li>甲</li><li>乙</li></ul>
  <hr>
  <blockquote>引一段話</blockquote>
</body></html>"#,
    );
    assert_eq!(title, "測試頁");
    let want = [
        "H:標題",
        "P:第一段。粗的字。",
        "LI:甲",
        "LI:乙",
        "HR:",
        "Q:引一段話",
    ];
    assert_eq!(blocks(&bs), want);
    assert_eq!(bs[0].level, 1, "h1 的階層");
}

// script 與 style 的內容不是給人看的,整段都不能留。
#[test]
fn parse_html_drops_script_and_style() {
    let (_, bs) = parse(
        "",
        r#"<body>
	<style>body{color:red}</style>
	<script>var x = "不該出現";</script>
	<p>只有這一段</p>
	<noscript>也不該出現</noscript>
	</body>"#,
    );
    let got = blocks(&bs).join("|");
    assert!(
        !got.contains("不該出現") && !got.contains("color") && !got.contains("也不該出現"),
        "腳本或樣式漏進來了:{got}"
    );
    assert_eq!(got, "P:只有這一段");
}

// 連結要接成絕對位址:相對位址在下一頁的基準變了之後就失效。
#[test]
fn parse_html_resolves_links() {
    let (_, bs) = parse(
        "https://example.org/a/b.html",
        r#"<body>
	<p><a href="c.html">同層</a> <a href="/top">根</a>
	<a href="https://other.org/x">外站</a></p></body>"#,
    );
    assert_eq!(
        hrefs(&bs),
        [
            "https://example.org/a/c.html",
            "https://example.org/top",
            "https://other.org/x"
        ]
    );
}

// 圖片自己成一段,位址也要絕對化。
#[test]
fn parse_html_images() {
    let (_, bs) = parse(
        "https://example.org/a/",
        r#"<body>
	<p>圖在下面</p><img src="pic.png" alt="一張圖"><p>圖在上面</p></body>"#,
    );
    assert_eq!(
        blocks(&bs),
        [
            "P:圖在下面",
            "IMG:https://example.org/a/pic.png [一張圖]",
            "P:圖在上面"
        ]
    );
}

// <pre> 裡的空白與換行是內容,不能摺疊。
#[test]
fn parse_html_pre_keeps_whitespace() {
    let (_, bs) = parse("", "<body><pre>  第一列\n     第二列</pre></body>");
    assert!(
        bs.len() == 1 && bs[0].kind == Kind::Pre,
        "得到 {:?}",
        blocks(&bs)
    );
    assert_eq!(bs[0].lines[1], "     第二列", "縮排被吃掉了");
}

// [雷] HTML 原始碼裡到處是為了可讀性換的行。摺疊沒做好的話,
// 每一個縮排都會變成內容,整頁看起來像被拉開一樣。
#[test]
fn parse_html_collapses_whitespace() {
    let (_, bs) = parse("", "<body><p>一段\n    很長的\n    文字</p></body>");
    assert_eq!(blocks(&bs), ["P:一段 很長的 文字"]);
}

// <br> 換行但不換段。
#[test]
fn parse_html_break_stays_in_paragraph() {
    let (_, bs) = parse("", "<body><p>上<br>下</p></body>");
    assert_eq!(bs.len(), 1, "拆成 {} 段:{:?}", bs.len(), blocks(&bs));
}

// 巢狀清單的縮排層數要對。
#[test]
fn parse_html_nested_list() {
    let (_, bs) = parse("", "<body><ul><li>外</li><ul><li>內</li></ul></ul></body>");
    let levels: Vec<i32> = bs
        .iter()
        .filter(|b| b.kind == Kind::List)
        .map(|b| b.level)
        .collect();
    assert_eq!(levels, [1, 2]);
}

// 標籤沒關的頁面不能讓整份解析爛掉:網路上這種頁面很多。
#[test]
fn parse_html_survives_broken_markup() {
    let (_, bs) = parse("", "<body><p>一<b>二<p>三</body>");
    let got = blocks(&bs).join("|");
    for want in ["一", "二", "三"] {
        assert!(got.contains(want), "{want:?} 不見了:{got}");
    }
}

// 一段文字被 html5ever 切成好幾個 token(實體、NUL)時,要先接起來再摺疊空白,
// 否則切點兩側的空白會各留一個(Go 的 tokenizer 是整段一次給)。
#[test]
fn parse_html_joins_text_tokens() {
    let (_, bs) = parse("", "<p>甲 \n&amp;\n 乙</p>");
    assert_eq!(blocks(&bs), ["P:甲 & 乙"]);
}

// Go 的 tokenizer 不丟 BOM;不合法的 UTF-8 每個位元組換一個 U+FFFD,
// 連結裡的則照 Go 的 net/url 寫成 %XX。
#[test]
fn parse_html_bytes_like_go() {
    let (_, bs) = parse_html(None, b"\xef\xbb\xbf<p>a\xa4b</p>");
    assert_eq!(blocks(&bs), ["P:\u{feff}", "P:a\u{fffd}b"]);
    let (_, bs) = parse_html(Some("http://x/d/"), b"<a href=\"\xa4.html\">t</a>");
    assert_eq!(hrefs(&bs), ["http://x/d/%A4.html"]);
}

// --- Fetch ---------------------------------------------------------------

struct Reply {
    status: &'static str,
    headers: Vec<(String, String)>,
    body: Vec<u8>,
}

fn reply(status: &'static str, ctype: &str, body: &[u8]) -> Reply {
    Reply {
        status,
        headers: vec![("Content-Type".into(), ctype.into())],
        body: body.to_vec(),
    }
}

type Handler = Arc<dyn Fn(&str) -> Reply + Send + Sync>;

/// 起一個 HTTP/1.1 伺服器,每條連線回一個回應就關掉。回傳 "http://127.0.0.1:埠"。
fn serve(h: Handler) -> String {
    let ln = TcpListener::bind("127.0.0.1:0").unwrap();
    let base = format!("http://{}", ln.local_addr().unwrap());
    thread::spawn(move || {
        for c in ln.incoming() {
            let Ok(mut c) = c else { return };
            let h = h.clone();
            thread::spawn(move || {
                let mut r = BufReader::new(c.try_clone().unwrap());
                let mut first = String::new();
                if r.read_line(&mut first).is_err() {
                    return;
                }
                loop {
                    let mut ln = String::new();
                    if r.read_line(&mut ln).unwrap_or(0) == 0 || ln == "\r\n" {
                        break;
                    }
                }
                let path = first.split(' ').nth(1).unwrap_or("/").to_string();
                let rep = h(&path);
                let mut out = format!(
                    "HTTP/1.1 {}\r\nContent-Length: {}\r\nConnection: close\r\n",
                    rep.status,
                    rep.body.len()
                );
                for (k, v) in &rep.headers {
                    out.push_str(&format!("{k}: {v}\r\n"));
                }
                out.push_str("\r\n");
                let _ = c.write_all(out.as_bytes());
                let _ = c.write_all(&rep.body);
            });
        }
    });
    base
}

fn client(max: u64) -> Client {
    Client {
        timeout: Duration::from_secs(5),
        max_bytes: max,
        ..Default::default()
    }
}

#[test]
fn fetch_html() {
    let base = serve(Arc::new(|_| {
        reply(
            "200 OK",
            "text/html; charset=utf-8",
            "<title>首頁</title><h1>你好</h1><p><a href=\"/next\">下一頁</a></p>".as_bytes(),
        )
    }));
    let d = client(0).fetch(&base).unwrap();
    assert_eq!(d.title, "首頁");
    let got = blocks(&d.blocks).join("|");
    assert!(
        got.contains("你好") && got.contains("下一頁"),
        "內容是 {got:?}"
    );
    // 連結要以最終位址為基準接成絕對位址。
    assert_eq!(hrefs(&d.blocks).last().unwrap(), &format!("{base}/next"));
}

// Big5 的站台要解得出來,而且宣告在 header 或 meta 都算。
#[test]
fn fetch_big5() {
    let big5: &[u8] = &[0xa4, 0xe5, 0xa6, 0x72]; // 「文字」
    for (name, ctype, meta) in [
        ("header 宣告", "text/html; charset=big5", ""),
        ("meta 宣告", "text/html", r#"<meta charset="Big5">"#),
    ] {
        let mut body = meta.as_bytes().to_vec();
        body.extend_from_slice(b"<p>");
        body.extend_from_slice(big5);
        body.extend_from_slice(b"</p>");
        let ctype = ctype.to_string();
        let base = serve(Arc::new(move |_| reply("200 OK", &ctype, &body)));
        let d = client(0).fetch(&base).unwrap();
        let got = blocks(&d.blocks).join("|");
        assert!(got.contains("文字"), "{name}:解出來是 {got:?}");
    }
}

// 圖片位址直接交出位元組,不解析成區塊。
#[test]
fn fetch_image_doc() {
    let png = b"\x89PNG\r\n\x1a\n\xe6\x93\xba\xe8\x91\x97".to_vec();
    let p2 = png.clone();
    let base = serve(Arc::new(move |_| reply("200 OK", "image/png", &p2)));
    let d = client(0).fetch(&format!("{base}/a.png")).unwrap();
    assert_eq!(d.image, png);
    assert!(d.blocks.is_empty(), "圖片不該解析成區塊");
    assert_eq!(d.title, "a.png");
}

// 超過上限時內容要截斷,而且**要說出來**:靜靜地少一半最難發現。
#[test]
fn fetch_truncates() {
    let base = serve(Arc::new(|_| reply("200 OK", "text/plain", &[b'x'; 5000])));
    let d = client(100).fetch(&base).unwrap();
    assert!(d.truncated, "截斷了卻沒有說");
    assert_eq!(d.blocks[0].lines[0].len(), 100);
}

// 重導向之後,相對連結要以**最終**位址為基準。
#[test]
fn fetch_follows_redirect() {
    let base = serve(Arc::new(|path| {
        if path == "/old" {
            Reply {
                status: "302 Found",
                headers: vec![("Location".into(), "/new/here".into())],
                body: Vec::new(),
            }
        } else {
            reply(
                "200 OK",
                "text/html",
                "<a href=\"x.html\">相對</a>".as_bytes(),
            )
        }
    }));
    let d = client(0).fetch(&format!("{base}/old")).unwrap();
    assert_eq!(d.url, format!("{base}/new/here"));
    assert_eq!(
        hrefs(&d.blocks).last().unwrap(),
        &format!("{base}/new/x.html")
    );
}

// 重導向的上限與 Go 版相同:送出 5 個請求之後不再跟。
#[test]
fn fetch_redirect_limit() {
    let base = serve(Arc::new(|path| {
        let n: u32 = path.trim_start_matches('/').parse().unwrap_or(0);
        Reply {
            status: "302 Found",
            headers: vec![("Location".into(), format!("/{}", n + 1))],
            body: Vec::new(),
        }
    }));
    let err = client(0).fetch(&format!("{base}/0")).unwrap_err();
    assert!(err.0.contains('5'), "錯誤是 {err}");
}

#[test]
fn fetch_rejects_non_http() {
    assert!(
        Client::default().fetch("gopher://example.org/").is_err(),
        "gopher 位址不該由這一包處理"
    );
}

#[test]
fn fetch_reports_status() {
    let base = serve(Arc::new(|_| {
        reply(
            "404 Not Found",
            "text/plain; charset=utf-8",
            "沒有這一頁\n".as_bytes(),
        )
    }));
    let err = client(0).fetch(&base).unwrap_err();
    assert!(err.0.contains("404"), "錯誤是 {err}");
}

#[test]
fn is_http_cases() {
    for s in ["http://a", "HTTPS://a", " https://a "] {
        assert!(is_http(s), "{s:?} 應該算 http");
    }
    for s in ["gopher://a", "a.org", "ftp://a"] {
        assert!(!is_http(s), "{s:?} 不該算 http");
    }
}

#[test]
fn charset_and_mojibake_like_go() {
    assert_eq!(charset_of(b"text/html; charset=\"Big5\""), Enc::Big5);
    assert_eq!(charset_of(b"<meta charset = utf-8>"), Enc::Utf8);
    assert_eq!(charset_of(b"charset=; charset=gbk"), Enc::Gbk);
    assert_eq!(
        charset_of("CHAR\u{017F}ET=big5-h\u{212A}scs".as_bytes()),
        Enc::Big5
    );
    // 宣告 UTF-8 卻是 Big5:Go 版數的是真的 U+FFFD,壞位元組不算,所以不會退回判讀。
    let raw = [0xa4u8, 0xe5, 0xa6, 0x72].repeat(20);
    assert!(!mojibake(&raw));
    assert!(mojibake("\u{FFFD}".repeat(9).as_bytes()));
}
