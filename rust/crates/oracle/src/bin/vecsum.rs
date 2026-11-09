//! 列出一串字的向量外框,輸出格式與 Go 端的 `tools/vecsum` 相同,兩邊直接比。
//!
//! ```text
//! vecsum [字型] < 字串
//! ```

use std::borrow::Cow;
use std::io::{Read, Write};

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let owned;
    let set = match args.first() {
        Some(p) => {
            let b = std::fs::read(p).unwrap_or_else(|e| {
                eprintln!("{p}: {e}");
                std::process::exit(1)
            });
            owned = wincv_vecfont::Set::with_fonts(vec![Cow::Owned(b)]);
            if !owned.ready() {
                eprintln!("{p}: 讀不起來");
                std::process::exit(1);
            }
            &owned
        }
        None => wincv_vecfont::default_set(),
    };
    let mut input = String::new();
    std::io::stdin().read_to_string(&mut input).unwrap();
    let mut w = std::io::BufWriter::new(std::io::stdout().lock());
    for r in input.chars().filter(|&c| c != '\n') {
        let (segs, adv, ok) = set.glyph(r);
        if !ok {
            writeln!(w, "U+{:04X}\t-", r as u32).unwrap();
            continue;
        }
        let parts: Vec<String> = segs
            .iter()
            .map(|s| {
                let n = match s.op {
                    b'q' => 2,
                    b'c' => 3,
                    _ => 1,
                };
                let nums: Vec<String> = s.args[..n * 2].iter().map(|v| ((v * 64.0) as i64).to_string()).collect();
                format!("{}:{}", s.op as char, nums.join(","))
            })
            .collect();
        writeln!(w, "U+{:04X}\t{}\t{}", r as u32, (adv * 64.0) as i64, parts.join(" ")).unwrap();
    }
}
