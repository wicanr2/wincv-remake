//! 與 Go 版 `tools/docsum-net` 同格式的輸出:網頁、gopher 選單與 EPUB 解成區塊。
//!
//! ```text
//! tools/cargo.sh run --release -p wincv-epub --example docsum < 清單
//! ```
//!
//! 清單每列「模式 TAB 路徑」;模式與輸出格式見 `tools/docsum-net/main.go`。
//! html 模式同樣開一個 127.0.0.1 的伺服器,經 `wincv_web::Client::fetch` 取回。

use std::io::{BufRead, BufReader, BufWriter, Write};
use std::net::TcpListener;
use std::sync::{Arc, Mutex};
use std::time::Duration;

use wincv_markdown::{Block, Kind};
use wincv_textenc::utf8_lossy_go;

fn esc(s: &str) -> String {
    s.replace('\\', "\\\\")
        .replace('\t', "\\t")
        .replace('\n', "\\n")
        .replace('\r', "\\r")
}

fn b01(b: bool) -> i32 {
    b as i32
}

fn kind_name(k: Kind) -> &'static str {
    match k {
        Kind::Para => "Para",
        Kind::Heading => "Heading",
        Kind::Code => "Code",
        Kind::Quote => "Quote",
        Kind::List => "List",
        Kind::Rule => "Rule",
        Kind::Image => "Image",
        Kind::Table => "Table",
        Kind::Pre => "Pre",
    }
}

fn blocks(w: &mut Vec<String>, name: &str, bs: &[Block]) {
    w.push(format!("G\t{name}\n"));
    for (i, b) in bs.iter().enumerate() {
        w.push(format!(
            "B\t{i}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\n",
            kind_name(b.kind),
            b.level,
            esc(&b.lang),
            b01(b.ordered),
            b.num,
            esc(&b.marker),
            esc(&b.src),
            esc(&b.alt),
            esc(&String::from_utf8_lossy(&b.aligns)),
            b.rows.len()
        ));
        for sp in &b.spans {
            w.push(format!(
                "S\t{}\t{}\t{}\n",
                sp.style.0,
                esc(&sp.text),
                esc(&sp.href)
            ));
        }
        for ln in &b.lines {
            w.push(format!("L\t{}\n", esc(ln)));
        }
        for r in &b.rows {
            let cells: Vec<String> = r.iter().map(|c| esc(c)).collect();
            w.push(format!("R\t{}\n", cells.join("\t")));
        }
    }
}

fn chapter_err(s: &str) -> String {
    for p in ["讀不到 ", "沒有第 ", "書裡沒有 "] {
        if s.starts_with(p) {
            return s.to_string();
        }
    }
    "讀取錯誤".to_string()
}

/// 只回 /f/<序號>/page.html 的 HTTP/1.1 伺服器,一條連線一個回應。
fn serve(files: Arc<Mutex<Vec<String>>>) -> String {
    let ln = TcpListener::bind("127.0.0.1:0").expect("開不了本機的埠");
    let host = ln.local_addr().unwrap().to_string();
    std::thread::spawn(move || {
        for c in ln.incoming() {
            let Ok(mut c) = c else { continue };
            let files = files.clone();
            std::thread::spawn(move || {
                let mut r = BufReader::new(c.try_clone().unwrap());
                let mut first = String::new();
                if r.read_line(&mut first).is_err() {
                    return;
                }
                loop {
                    let mut l = String::new();
                    if r.read_line(&mut l).unwrap_or(0) == 0 || l == "\r\n" {
                        break;
                    }
                }
                let path = first.split(' ').nth(1).unwrap_or("");
                let parts: Vec<&str> = path.split('/').collect();
                let body = (parts.len() == 4)
                    .then(|| parts[2].parse::<usize>().ok())
                    .flatten()
                    .and_then(|n| files.lock().unwrap().get(n).cloned())
                    .and_then(|p| std::fs::read(p).ok());
                let (head, body) = match body {
                    Some(b) => (
                        format!(
                            "HTTP/1.1 200 OK\r\nContent-Type: text/html\r\nContent-Length: {}\r\nConnection: close\r\n\r\n",
                            b.len()
                        ),
                        b,
                    ),
                    None => (
                        "HTTP/1.1 404 Not Found\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"
                            .to_string(),
                        Vec::new(),
                    ),
                };
                let _ = c.write_all(head.as_bytes());
                let _ = c.write_all(&body);
            });
        }
    });
    host
}

fn main() {
    let files = Arc::new(Mutex::new(Vec::<String>::new()));
    let host = serve(files.clone());
    let client = wincv_web::Client {
        timeout: Duration::from_secs(60),
        ..Default::default()
    };

    let stdout = std::io::stdout();
    let mut out = BufWriter::new(stdout.lock());
    for line in std::io::stdin().lock().lines() {
        let Ok(line) = line else { break };
        let Some((mode, path)) = line.split_once('\t') else {
            continue;
        };
        let n = {
            let mut f = files.lock().unwrap();
            f.push(path.to_string());
            f.len() - 1
        };
        let mut w: Vec<String> = Vec::new();
        w.push(format!("F\t{mode}\t{}\n", esc(path)));
        match mode {
            "html" => match client.fetch(&format!("http://{host}/f/{n}/page.html")) {
                Err(e) => w.push(format!("E\t{}\n", esc(&e.to_string()))),
                Ok(d) => {
                    w.push(format!(
                        "D\t{}\t{}\t{}\t{}\n",
                        esc(&d.url),
                        esc(&d.title),
                        b01(d.truncated),
                        d.image.len()
                    ));
                    blocks(&mut w, "doc", &d.blocks);
                }
            },
            "raw" => match std::fs::read(path) {
                Err(_) => w.push("E\t讀不到\n".into()),
                Ok(b) => {
                    let (title, bs) = wincv_web::parse_html(None, &b);
                    w.push(format!("T\t{}\n", esc(&title)));
                    blocks(&mut w, "doc", &bs);
                }
            },
            "gopher" => match std::fs::read(path) {
                Err(_) => w.push("E\t讀不到\n".into()),
                Ok(b) => {
                    let items = wincv_gopher::parse_menu(&b);
                    for (i, it) in items.iter().enumerate() {
                        let web = it.web_url();
                        w.push(format!(
                            "I\t{i}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\n",
                            it.typ,
                            esc(&it.display),
                            esc(&utf8_lossy_go(&it.sel)),
                            esc(&it.host),
                            esc(&it.port),
                            b01(it.is_link()),
                            b01(web.is_some()),
                            esc(web.as_deref().unwrap_or("")),
                            esc(&it.url().to_string())
                        ));
                    }
                    blocks(&mut w, "menu", &wincv_gopher::menu_blocks(&items));
                    blocks(&mut w, "text", &wincv_gopher::text_blocks(&b));
                }
            },
            "epub" => match wincv_epub::open(path) {
                Err(e) => {
                    let msg = if e.starts_with("打不開:") {
                        "打不開".to_string()
                    } else {
                        e
                    };
                    w.push(format!("E\t{}\n", esc(&msg)));
                }
                Ok(bk) => {
                    w.push(format!(
                        "K\t{}\t{}\t{}\n",
                        esc(&bk.title),
                        esc(&bk.author),
                        bk.chapters.len()
                    ));
                    for (i, c) in bk.chapters.iter().enumerate() {
                        w.push(format!("C\t{i}\t{}\t{}\n", esc(&c.title), esc(&c.href)));
                    }
                    for i in 0..bk.chapters.len() {
                        w.push(format!("X\t{i}\n"));
                        let bs = match bk.blocks(i) {
                            Ok(bs) => bs,
                            Err(e) => {
                                w.push(format!("E\t{}\n", esc(&chapter_err(&e))));
                                continue;
                            }
                        };
                        blocks(&mut w, "doc", &bs);
                        for b in bs.iter().filter(|b| b.kind == Kind::Image) {
                            match bk.image(&b.src) {
                                Err(e) => w.push(format!(
                                    "M\t{}\t{}\n",
                                    esc(&b.src),
                                    esc(&chapter_err(&e))
                                )),
                                Ok(data) => w.push(format!("M\t{}\t{}\n", esc(&b.src), data.len())),
                            }
                        }
                    }
                }
            },
            _ => w.push("E\t不認得的模式\n".into()),
        }
        for l in w {
            let _ = out.write_all(l.replace(&host, "SRV").as_bytes());
        }
    }
}
