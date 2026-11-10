//! 用 `wincv-officedoc` 打開一批 Office 文件,印出每個區塊的所有欄位
//! (對應 Go 端 `tools/docsum-office`,輸出格式逐位元組相同)。
//!
//! ```text
//! tools/cargo.sh run --release -p wincv-officedoc --example officesum -- [@清單檔] [檔案...]
//! ```
//!
//! 欄位的意義見 Go 端的說明。字串裡的 `\` TAB 換行 CR 寫成 `\\` `\t` `\n` `\r`。

use std::io::Write;

use sha2::{Digest, Sha256};
use wincv_markdown::{Block, Kind};

fn esc(s: &str) -> String {
    let mut sb = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '\\' => sb.push_str("\\\\"),
            '\t' => sb.push_str("\\t"),
            '\n' => sb.push_str("\\n"),
            '\r' => sb.push_str("\\r"),
            _ => sb.push(c),
        }
    }
    sb
}

fn hex(b: &[u8]) -> String {
    b.iter().map(|c| format!("{c:02x}")).collect()
}

struct Out(std::io::BufWriter<std::io::StdoutLock<'static>>);

impl Out {
    fn row(&mut self, fields: &[String]) {
        let _ = self.0.write_all(fields.join("\t").as_bytes());
        let _ = self.0.write_all(b"\n");
    }
}

macro_rules! row {
    ($o:expr, $($f:expr),+ $(,)?) => { $o.row(&[$($f.to_string()),+]) };
}

fn main() {
    let mut names = Vec::new();
    let mut tokens = false;
    for a in std::env::args().skip(1) {
        if a == "-xml" {
            tokens = true;
            continue;
        }
        match a.strip_prefix('@') {
            Some(list) => match std::fs::read_to_string(list) {
                Ok(s) => names.extend(s.split('\n').filter(|l| !l.is_empty()).map(str::to_string)),
                Err(e) => {
                    eprintln!("{e}");
                    std::process::exit(2);
                }
            },
            None => names.push(a),
        }
    }
    let mut o = Out(std::io::BufWriter::with_capacity(1 << 20, std::io::stdout().lock()));
    for (i, name) in names.iter().enumerate() {
        row!(o, "F", i, esc(name));
        if tokens {
            xml_tokens(&mut o, name);
            continue;
        }
        one(&mut o, name);
    }
    let _ = o.0.flush();
}

fn one(o: &mut Out, name: &str) {
    let d = match wincv_officedoc::open(name) {
        Ok(d) => d,
        Err(e) => {
            row!(o, "E", esc(&e));
            return;
        }
    };
    let kind = match d.kind {
        wincv_officedoc::Kind::Word => 0,
        wincv_officedoc::Kind::Presentation => 1,
        wincv_officedoc::Kind::Spreadsheet => 2,
    };
    row!(o, "D", kind, esc(&d.title), d.parts.len());
    for (i, p) in d.parts.iter().enumerate() {
        row!(o, "P", i, esc(&p.title));
    }
    for i in 0..d.parts.len() {
        for (j, b) in d.blocks(i).iter().enumerate() {
            block(o, &d, i, j, b);
        }
    }
    d.close();
}

fn block(o: &mut Out, d: &wincv_officedoc::Doc, i: usize, j: usize, b: &Block) {
    let ordered = if b.ordered { "1" } else { "0" };
    row!(
        o,
        "B",
        i,
        j,
        b.kind as i32,
        b.level,
        esc(&b.lang),
        ordered,
        b.num,
        esc(&b.marker),
        esc(&b.src),
        esc(&b.alt),
        esc(&String::from_utf8_lossy(&b.aligns)),
        b.spans.len(),
        b.lines.len(),
        b.rows.len(),
    );
    for s in &b.spans {
        row!(o, "S", s.style.0, esc(&s.href), esc(&s.text));
    }
    for l in &b.lines {
        row!(o, "L", esc(l));
    }
    for r in &b.rows {
        let mut f = vec!["R".to_string()];
        f.extend(r.iter().map(|c| esc(c)));
        o.row(&f);
    }
    if b.kind == Kind::Image {
        img(o, d, &b.src);
    }
}

fn img(o: &mut Out, d: &wincv_officedoc::Doc, src: &str) {
    let data = match d.image(src) {
        Ok(b) => b,
        Err(e) => {
            row!(o, "I", esc(src), "ERR", esc(&e));
            return;
        }
    };
    let raw = hex(&Sha256::digest(&data));
    match wincv_imgfmt::decode(src, &data) {
        Ok((m, kind)) => {
            let pix = hex(&Sha256::digest(&m.pix));
            row!(o, "I", esc(src), "OK", data.len(), raw, kind, m.w, m.h, pix);
        }
        // 錯誤訊息不印:兩邊的圖片解碼器不同,同一張壞圖的訊息不會一樣。
        Err(_) => row!(o, "I", esc(src), "DECODE", data.len(), raw),
    }
}

/// -xml 模式:把 ZIP 包裡每一個組件交給 `new_decoder`,印出每一個 token(對應 Go 端 xmlTokens)。
fn xml_tokens(o: &mut Out, name: &str) {
    use wincv_ooxml::Token;
    let lossy = |b: &[u8]| esc(&wincv_textenc::utf8_lossy_go(b));
    let p = match wincv_ooxml::Package::open(name) {
        Ok(p) => p,
        Err(e) => {
            row!(o, "Z", esc(&e));
            return;
        }
    };
    for n in p.names().to_vec() {
        row!(o, "N", esc(&n));
        let b = match p.bytes(&n) {
            Ok(b) => b,
            Err(e) => {
                row!(o, "T", "X", esc(&e));
                continue;
            }
        };
        let mut d = wincv_ooxml::new_decoder(b);
        loop {
            match d.token() {
                Err(e) => {
                    row!(o, "T", "X", esc(&e.to_string()));
                    break;
                }
                Ok(Token::Start(se)) => {
                    let mut f = vec!["T".to_string(), "S".to_string(), esc(&se.name.space), esc(&se.name.local)];
                    for a in &se.attr {
                        f.extend([esc(&a.name.space), esc(&a.name.local), esc(&a.value)]);
                    }
                    o.row(&f);
                }
                Ok(Token::End(n)) => row!(o, "T", "E", esc(&n.space), esc(&n.local)),
                Ok(Token::CharData(s)) => row!(o, "T", "C", esc(&s)),
                Ok(Token::Comment(b)) => row!(o, "T", "M", lossy(&b)),
                Ok(Token::ProcInst { target, inst }) => row!(o, "T", "P", esc(&target), lossy(&inst)),
                Ok(Token::Directive(b)) => row!(o, "T", "D", lossy(&b)),
            }
        }
    }
    p.close();
}
