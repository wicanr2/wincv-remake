//! 照搬 Go 版 `browse_test.go`。

use std::collections::HashMap;
use std::io::{Read, Write};
use std::net::TcpListener;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use super::*;

type Routes = Arc<Mutex<HashMap<String, String>>>;

/// 起一個很小的 gopher 伺服器,依 selector 回不同的東西。回傳位址。
fn gopher_server(routes: Routes) -> String {
    let ln = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = ln.local_addr().unwrap().to_string();
    std::thread::spawn(move || {
        for c in ln.incoming() {
            let Ok(mut c) = c else { return };
            let routes = routes.clone();
            std::thread::spawn(move || {
                let mut buf = [0u8; 1024];
                let n = c.read(&mut buf).unwrap_or(0);
                let sel = String::from_utf8_lossy(&buf[..n]).trim_end_matches(['\r', '\n']).to_string();
                let body = routes.lock().unwrap().get(&sel).cloned().unwrap_or_else(|| "3找不到\terror\terror.host\t1\r\n.\r\n".into());
                let _ = c.write_all(body.as_bytes());
            });
        }
    });
    addr
}

/// 回應一個 HTTP 請求:路徑查表,查不到回 404。每條連線只服務一個請求。
fn web_server(routes: HashMap<String, (String, Vec<u8>)>) -> String {
    let ln = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = ln.local_addr().unwrap().to_string();
    let routes = Arc::new(routes);
    std::thread::spawn(move || {
        for c in ln.incoming() {
            let Ok(mut c) = c else { return };
            let routes = routes.clone();
            std::thread::spawn(move || {
                let mut req = Vec::new();
                let mut buf = [0u8; 1024];
                while !req.windows(4).any(|w| w == b"\r\n\r\n") {
                    match c.read(&mut buf) {
                        Ok(0) | Err(_) => return,
                        Ok(n) => req.extend_from_slice(&buf[..n]),
                    }
                }
                let line = String::from_utf8_lossy(&req).lines().next().unwrap_or("").to_string();
                let path = line.split(' ').nth(1).unwrap_or("/").to_string();
                let (status, ctype, body) = match routes.get(&path) {
                    Some((ct, b)) => ("200 OK", ct.as_str(), b.clone()),
                    None => ("404 Not Found", "text/plain", b"404 page not found\n".to_vec()),
                };
                let head = format!("HTTP/1.1 {status}\r\nContent-Type: {ctype}\r\nContent-Length: {}\r\nConnection: close\r\n\r\n", body.len());
                let _ = c.write_all(head.as_bytes());
                let _ = c.write_all(&body);
            });
        }
    });
    format!("http://{addr}")
}

fn html(s: &str) -> (String, Vec<u8>) {
    ("text/html; charset=utf-8".into(), s.as_bytes().to_vec())
}

/// 一直畫到網路取回結束為止。draw 每一幀收一次結果:這正是外殼的行為,測試照著做
/// 才驗得到真實的路徑。
pub(super) fn settle(a: &mut App, s: &mut Screen) {
    for _ in 0..500 {
        a.draw(s);
        if !a.busy() {
            return;
        }
        std::thread::sleep(Duration::from_millis(10));
    }
    panic!("等太久,取回沒有結束");
}

fn new_browse_app() -> (App, Screen, TempDir) {
    let td = TempDir::new();
    let mut a = new_at(td.path());
    (a.cell_w, a.cell_h) = (8, 16);
    a.gopher = Some(wincv_gopher::Client { timeout: Duration::from_secs(5), ..Default::default() });
    (a, Screen::new(60, 20), td)
}

fn web_client() -> wincv_web::Client {
    wincv_web::Client { timeout: Duration::from_secs(5), ..Default::default() }
}

fn split(addr: &str) -> (String, String) {
    let (h, p) = addr.rsplit_once(':').unwrap();
    (h.to_string(), p.to_string())
}

#[test]
fn gopher_opens_menu() {
    let routes: Routes = Default::default();
    let addr = gopher_server(routes.clone());
    let (host, port) = split(&addr);
    let menu = format!("i歡迎光臨\t\terror.host\t1\r\n1文件目錄\t/docs\t{host}\t{port}\r\n0讀我檔案\t/readme\t{host}\t{port}\r\n.\r\n");
    routes.lock().unwrap().insert(String::new(), menu.clone());
    routes.lock().unwrap().insert("/".into(), menu);

    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("gopher://{addr}"));
    settle(&mut a, &mut s);
    let txt = screen_text(&s);
    for want in ["歡迎光臨", "文件目錄", "讀我檔案", "[目錄]", "[文字]"] {
        assert!(txt.contains(want), "畫面上沒有 {want:?}\n{txt}");
    }
    // 資訊列不該有型別標籤:它不是可以點的東西。
    assert!(!txt.contains("[?] 歡迎光臨"), "資訊列被標成連結了");
}

#[test]
fn gopher_follows_link() {
    let routes: Routes = Default::default();
    let addr = gopher_server(routes.clone());
    let (host, port) = split(&addr);
    let menu = format!("i歡迎光臨\t\terror.host\t1\r\n0讀我檔案\t/readme\t{host}\t{port}\r\n.\r\n");
    {
        let mut r = routes.lock().unwrap();
        r.insert(String::new(), menu.clone());
        r.insert("/".into(), menu);
        r.insert("/readme".into(), "這是內文\r\n第二行\r\n.\r\n".into());
    }
    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("gopher://{addr}"));
    settle(&mut a, &mut s);

    // 第一個(也是唯一一個)連結應該已經被選起來。
    a.handle_key(named(Code::Enter));
    settle(&mut a, &mut s);
    let txt = screen_text(&s);
    assert!(txt.contains("這是內文") && txt.contains("第二行"), "沒有跟著連結走\n{txt}");
    // 結束符不是內容。
    assert!(!txt.contains("\n.\n"), "把結束符當成內容印出來了");

    // Backspace 回上一頁。
    a.handle_key(named(Code::Backspace));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("讀我檔案"), "回不去上一頁\n{}", screen_text(&s));
}

/// 連不上的時候要在狀態列說,不能靜靜地停在「連線中」。
#[test]
fn gopher_shows_connection_error() {
    // 先起一個再關掉,拿一個確定沒人聽的埠。
    let dead = {
        let ln = TcpListener::bind("127.0.0.1:0").unwrap();
        ln.local_addr().unwrap().to_string()
    };
    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("gopher://{dead}"));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("連不上"), "沒有把錯誤說出來\n{}", screen_text(&s));
}

/// busy 要在取回期間為真:外殼靠它決定要不要繼續重繪。
#[test]
fn gopher_busy_while_fetching() {
    let ln = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = ln.local_addr().unwrap().to_string();
    let (tx, rx) = std::sync::mpsc::channel::<()>();
    std::thread::spawn(move || {
        let Ok((_c, _)) = ln.accept() else { return };
        let _ = rx.recv(); // 拖著不回應
    });
    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("gopher://{addr}"));
    a.draw(&mut s);
    assert!(a.busy(), "取回期間 busy 應該是真");
    assert!(screen_text(&s).contains("連線"), "狀態列沒有說在連線\n{}", screen_text(&s));
    drop(tx);
}

/// 位址解不開時不該進到瀏覽模式又卡在那裡。
#[test]
fn browse_rejects_bad_url() {
    let (mut a, _s, _td) = new_browse_app();
    a.open_url("ftp://example.org/");
    assert!(!a.busy(), "不該開始取回");
    assert!(a.message.contains("解不開"), "訊息是 {:?}", a.message);
}

#[test]
fn browse_opens_web_page() {
    let base = web_server(HashMap::from([
        ("/".to_string(), html("<title>網頁標題</title><h1>你好</h1>\n\t\t      <p>這是內文。</p><p><a href=\"/next\">下一頁</a></p>")),
        ("/next".to_string(), html("<p>第二頁的內容</p>")),
    ]));
    let (mut a, mut s, _td) = new_browse_app();
    a.web = Some(web_client());
    a.open_url(&format!("{base}/"));
    settle(&mut a, &mut s);
    let txt = screen_text(&s);
    for want in ["你好", "這是內文", "下一頁", "網頁標題"] {
        assert!(txt.contains(want), "畫面上沒有 {want:?}\n{txt}");
    }
    // 連結導覽與 gopher 是同一組按鍵。
    a.handle_key(named(Code::Enter));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("第二頁的內容"), "沒有跟著連結走\n{}", screen_text(&s));
    a.handle_key(named(Code::Backspace));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("這是內文"), "回不去上一頁\n{}", screen_text(&s));
}

/// F2 在檔案清單與瀏覽模式下都要叫得出位址列。
#[test]
fn browse_hotkey_f2() {
    let (mut a, _s, _td) = new_browse_app();
    a.handle_key(named(Code::F2));
    assert!(a.prompting(), "F2 沒有開位址輸入列");
}

/// 同一頁裡 gopher 與 http 的連結都要點得下去:選單上兩種都有。
#[test]
fn browse_follows_http_link_from_gopher_menu() {
    let base = web_server(HashMap::from([("/web".to_string(), html("<p>從 gopher 走到網頁</p>"))]));
    let routes: Routes = Default::default();
    let addr = gopher_server(routes.clone());
    let (host, port) = split(&addr);
    let menu = format!("hWeb 連結\tURL:{base}/web\t{host}\t{port}\r\n.\r\n");
    routes.lock().unwrap().insert(String::new(), menu.clone());
    routes.lock().unwrap().insert("/".into(), menu);

    let (mut a, mut s, _td) = new_browse_app();
    a.web = Some(web_client());
    a.open_url(&format!("gopher://{addr}"));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("Web 連結"), "選單沒出來\n{}", screen_text(&s));
    a.handle_key(named(Code::Enter));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("從 gopher 走到網頁"), "http 連結沒跟過去\n{}", screen_text(&s));
}

/// 網頁裡的圖要真的畫成 overlay,不是只留 alt。
///
/// 圖用 PGM 而不是 Go 測試的 PNG:app 不依賴 PNG 編碼器,這裡要驗的是「圖有沒有變成
/// overlay」,格式無關。
#[test]
fn browse_loads_inline_image() {
    let mut pgm = b"P5\n32 16\n255\n".to_vec();
    pgm.extend(std::iter::repeat_n(200u8, 32 * 16));
    let base = web_server(HashMap::from([
        ("/".to_string(), ("text/html".to_string(), "<p>圖在下面</p><img src=\"/a.pgm\" alt=\"灰色\">".as_bytes().to_vec())),
        ("/a.pgm".to_string(), ("image/x-portable-graymap".to_string(), pgm)),
    ]));
    let (mut a, mut s, _td) = new_browse_app();
    a.web = Some(web_client());
    a.open_url(&format!("{base}/"));
    settle(&mut a, &mut s);
    // 圖是取回頁面之後才去拿的,再等一輪。
    settle(&mut a, &mut s);
    let mut ov = Vec::new();
    for _ in 0..50 {
        ov = a.draw(&mut s);
        if !ov.is_empty() {
            break;
        }
        if a.busy() {
            std::thread::sleep(Duration::from_millis(10));
        }
    }
    assert!(!ov.is_empty(), "圖沒有畫出來\n{}", screen_text(&s));
    assert_eq!((ov[0].img.w, ov[0].img.h), (32, 16), "畫出來的圖尺寸不對");
}

/// 沒設 web 客戶端時不取行內圖(Go 版視窗程式的實際行為),畫面只留 alt。
#[test]
fn browse_skips_inline_image_without_web_client() {
    let base = web_server(HashMap::from([("/".to_string(), ("text/html".to_string(), "<p>圖在下面</p><img src=\"/a.pgm\" alt=\"灰色\">".as_bytes().to_vec()))]));
    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("{base}/"));
    settle(&mut a, &mut s);
    settle(&mut a, &mut s);
    assert!(a.draw(&mut s).is_empty(), "沒設 web 客戶端卻取了圖");
    assert!(screen_text(&s).contains("灰色"), "alt 沒有留下來\n{}", screen_text(&s));
}

/// 點到連結那一列要跟著走(對應 Go 版 mouse.go 的 clickBrowse)。
#[test]
fn browse_click_follows_link() {
    let routes: Routes = Default::default();
    let addr = gopher_server(routes.clone());
    let (host, port) = split(&addr);
    let menu = format!("i歡迎光臨\t\terror.host\t1\r\n0讀我檔案\t/readme\t{host}\t{port}\r\n.\r\n");
    {
        let mut r = routes.lock().unwrap();
        r.insert(String::new(), menu.clone());
        r.insert("/".into(), menu);
        r.insert("/readme".into(), "點進來了\r\n.\r\n".into());
    }
    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("gopher://{addr}"));
    settle(&mut a, &mut s);
    let row = a.bv.links[0].line - a.bv.top;
    assert!(a.click(0, row, false), "點連結沒有反應");
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("點進來了"), "沒有跟著連結走\n{}", screen_text(&s));
}

/// Big5 的 selector 點下去要原封送回伺服器。href 是 String,壞位元組轉進去會變成 U+FFFD;
/// 跟連結時要從原始位址拿位元組,不能把 href 解回來送。
#[test]
fn gopher_follows_non_utf8_selector() {
    let ln = TcpListener::bind("127.0.0.1:0").unwrap();
    let addr = ln.local_addr().unwrap().to_string();
    let (host, port) = split(&addr);
    let got: Arc<Mutex<Vec<Vec<u8>>>> = Default::default();
    let seen = got.clone();
    // 「中文」的 Big5 是 A4 A4 A4 E5。
    let mut menu = b"1\xa4\xa4\xa4\xe5\t/\xa4\xa4\xa4\xe5\t".to_vec();
    menu.extend_from_slice(format!("{host}\t{port}\r\n.\r\n").as_bytes());
    std::thread::spawn(move || {
        for c in ln.incoming() {
            let Ok(mut c) = c else { return };
            let mut buf = [0u8; 1024];
            let n = c.read(&mut buf).unwrap_or(0);
            let sel = buf[..n].strip_suffix(b"\r\n").unwrap_or(&buf[..n]).to_vec();
            let body = if sel.is_empty() { menu.clone() } else { b"iok\t\terror.host\t1\r\n.\r\n".to_vec() };
            seen.lock().unwrap().push(sel);
            let _ = c.write_all(&body);
        }
    });
    let (mut a, mut s, _td) = new_browse_app();
    a.open_url(&format!("gopher://{addr}"));
    settle(&mut a, &mut s);
    assert!(screen_text(&s).contains("中文"), "Big5 選單沒有解出來\n{}", screen_text(&s));
    a.handle_key(named(Code::Enter));
    settle(&mut a, &mut s);
    assert_eq!(got.lock().unwrap().last().cloned(), Some(b"/\xa4\xa4\xa4\xe5".to_vec()), "送出的 selector 被改掉了");

    // 回上一頁再點一次,一樣要送原始位元組。
    a.handle_key(named(Code::Backspace));
    settle(&mut a, &mut s);
    a.handle_key(named(Code::Enter));
    settle(&mut a, &mut s);
    assert_eq!(got.lock().unwrap().last().cloned(), Some(b"/\xa4\xa4\xa4\xe5".to_vec()), "回上一頁之後送出的 selector 被改掉了");
}
