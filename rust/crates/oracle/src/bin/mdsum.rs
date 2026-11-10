//! 對應 Go 端 `tools/mdsum`:markdown 的區塊、排版後的每一列、每個 Piece 與每張圖,
//! 欄位與格式見 Go 端的說明。

use std::fmt::Write as _;
use std::io::Write as _;
use std::sync::Arc;

use wincv_markdown::{default_theme, layout, parse, Block, Kind, Span, Style};
use wincv_render::Image;

fn esc(s: &str) -> String {
    let mut o = String::with_capacity(s.len());
    for c in s.chars() {
        match c {
            '\\' => o.push_str("\\\\"),
            '\t' => o.push_str("\\t"),
            '\n' => o.push_str("\\n"),
            '\r' => o.push_str("\\r"),
            _ => o.push(c),
        }
    }
    o
}

fn b01(b: bool) -> i32 {
    i32::from(b)
}

fn no_load(_: &str) -> Result<Arc<Image>, String> {
    Err("不載圖".to_string())
}

/// 只有尺寸的圖:排版只看寬高,不必真的配置像素。
fn size_load(src: &str) -> Result<Arc<Image>, String> {
    let bad = || format!("壞的尺寸 {src}");
    let (a, b) = src.split_once('x').ok_or_else(bad)?;
    // Go 的 strconv.Atoi 接受前面的 +/- 號;這裡的輸入只有固定的幾組,不涵蓋那些情況。
    let w: i64 = a.parse().map_err(|_| bad())?;
    let h: i64 = b.parse().map_err(|_| bad())?;
    Ok(Arc::new(Image { w: w.max(0) as u32, h: h.max(0) as u32, pix: Vec::new() }))
}

fn synth() -> Vec<Block> {
    let sp = |t: &str, st: Style, href: &str| Span { text: t.to_string(), style: st, href: href.to_string() };
    let n = Style::default();
    let long = "全形與 half 混排的一段字,".repeat(12);
    let blk = |kind: Kind| Block { kind, ..Default::default() };
    let s = |v: &[&str]| v.iter().map(|x| x.to_string()).collect::<Vec<_>>();
    vec![
        Block { level: 3, spans: vec![sp(&long, n, "")], ..blk(Kind::Heading) },
        Block { level: 6, spans: vec![sp("六級", Style::BOLD, "")], ..blk(Kind::Heading) },
        Block { lines: s(&["a\tb\tc", "中\t文\tx", &"x".repeat(130), &"全形".repeat(70), ""]), ..blk(Kind::Pre) },
        Block { lines: s(&["緊接著的 Pre 不空行"]), ..blk(Kind::Pre) },
        Block { lines: s(&["\t縮排\t的碼", &"碼".repeat(80)]), ..blk(Kind::Code) },
        Block { level: 1, marker: "[目錄]".into(), spans: vec![sp(&long, n, "")], ..blk(Kind::List) },
        Block { level: 0, marker: "•".into(), spans: vec![sp("沒有尾巴空白的記號", n, "")], ..blk(Kind::List) },
        Block { ordered: true, num: 0, spans: vec![sp("零號", n, "")], ..blk(Kind::List) },
        Block { ordered: true, num: -5, spans: vec![sp("負號", n, "")], ..blk(Kind::List) },
        Block { ordered: true, num: 1234567, level: 3, spans: vec![sp(&long, Style::ITALIC, "")], ..blk(Kind::List) },
        Block {
            spans: vec![
                sp("粗連結 ", Style::BOLD | Style::LINK, "http://a/b"),
                sp("粗碼", Style::BOLD | Style::MONO, ""),
                sp(" 刪除 ", Style::STRIKE, ""),
                sp("斜刪", Style::ITALIC | Style::STRIKE, ""),
                sp("\u{3000}全形空白\u{3000}開頭", n, ""),
                sp("   ", n, ""),
                sp(&"https://example.com/very/long/path/".repeat(6), Style::LINK, "https://x"),
            ],
            ..blk(Kind::Para)
        },
        Block { spans: vec![sp(&long, n, "")], ..blk(Kind::Quote) },
        Block {
            aligns: b"cr".to_vec(),
            rows: vec![
                s(&["置中", "靠右", "預設"]),
                s(&["中文字格", "1", "a\tb"]),
                s(&["短"]),
                s(&[&"寬".repeat(60), "", &"w".repeat(90)]),
            ],
            ..blk(Kind::Table)
        },
        Block { rows: vec![s(&["x"]), s(&["", ""])], ..blk(Kind::Table) },
        blk(Kind::Table),
        blk(Kind::Rule),
        Block { src: "160x160".into(), alt: "方圖".into(), ..blk(Kind::Image) },
        Block { src: "100x8000".into(), alt: "直圖".into(), ..blk(Kind::Image) },
        Block { src: "5000x10".into(), alt: "橫圖".into(), ..blk(Kind::Image) },
        Block { src: "1x1".into(), alt: "點".into(), ..blk(Kind::Image) },
        Block { src: "0x10".into(), alt: "零寬".into(), ..blk(Kind::Image) },
        Block { src: "壞".into(), alt: "很長的替代文字".repeat(20), ..blk(Kind::Image) },
        blk(Kind::Para),
    ]
}

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() < 2 {
        eprintln!("用法: mdsum <cols,cols,...> <檔案|-synth>...");
        std::process::exit(2);
    }
    let widths: Vec<i32> = args[0]
        .split(',')
        .map(|s| {
            s.parse().unwrap_or_else(|_| {
                eprintln!("寬度不是數字: {s}");
                std::process::exit(2)
            })
        })
        .collect();
    let mut w = String::new();
    for name in &args[1..] {
        writeln!(w, "F\t{}", esc(name)).unwrap();
        let blocks;
        let load: &dyn Fn(&str) -> Result<Arc<Image>, String>;
        if name == "-synth" {
            blocks = synth();
            load = &size_load;
        } else {
            let Ok(b) = std::fs::read(name) else {
                writeln!(w, "E\t讀不到").unwrap();
                continue;
            };
            let Ok(s) = String::from_utf8(b) else {
                writeln!(w, "E\t不是 UTF-8").unwrap();
                continue;
            };
            blocks = parse(&s);
            load = &no_load;
        }
        for (i, b) in blocks.iter().enumerate() {
            writeln!(
                w,
                "B\t{i}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}\t{}",
                b.kind as i32,
                b.level,
                esc(&b.lang),
                b01(b.ordered),
                b.num,
                esc(&b.marker),
                esc(&b.src),
                esc(&b.alt),
                esc(&String::from_utf8_lossy(&b.aligns))
            )
            .unwrap();
            for s in &b.spans {
                writeln!(w, "S\t{}\t{}\t{}", s.style.0, esc(&s.text), esc(&s.href)).unwrap();
            }
            for l in &b.lines {
                writeln!(w, "X\t{}", esc(l)).unwrap();
            }
            for r in &b.rows {
                write!(w, "R\t{}", r.len()).unwrap();
                for c in r {
                    write!(w, "\t{}", esc(c)).unwrap();
                }
                w.push('\n');
            }
        }
        for &cols in &widths {
            writeln!(w, "C\t{cols}").unwrap();
            let (lines, pics) = layout(&blocks, cols, 8, 16, Some(load), default_theme());
            for (li, l) in lines.iter().enumerate() {
                let mut pi = "-".to_string();
                for (j, p) in pics.iter().enumerate() {
                    if l.pic.as_ref().is_some_and(|q| Arc::ptr_eq(q, p)) {
                        pi = j.to_string();
                    }
                }
                writeln!(w, "L\t{li}\t{pi}\t{}", l.pic_row).unwrap();
                for p in &l.pieces {
                    writeln!(w, "P\t{}\t{}\t{}\t{}\t{}", esc(&p.text), p.fg.0, p.bg.0, b01(p.under), esc(&p.href)).unwrap();
                }
            }
            for (j, p) in pics.iter().enumerate() {
                writeln!(
                    w,
                    "I\t{j}\t{}\t{}\t{}\t{}\t{}\t{}\t{}",
                    esc(&p.src),
                    esc(&p.alt),
                    b01(p.img.is_some()),
                    esc(&p.err),
                    p.cols,
                    p.rows,
                    p.row
                )
                .unwrap();
            }
        }
    }
    std::io::stdout().lock().write_all(w.as_bytes()).unwrap();
}
