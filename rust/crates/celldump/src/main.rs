//! 不開視窗,直接把畫面輸出成 PNG(對應 Go 版 `cmd/celldump`)。
//!
//! 目前有示範畫面、檔案列表(`-dir`)、看檔(`-file`)與編輯器(`-edit`),用來與 Go 版逐像素比對:
//!
//! ```text
//! celldump -half cvga.fon -eten-std STDFONT.15 -eten-spc SPCFONT.15 -o out.png
//! ```

use std::path::PathBuf;
use std::process::exit;

use wincv_cell::{Color, Screen, NAMES, NUM_COLORS};
use wincv_eten as eten;
use wincv_render::{scale_cjk, CjkSource, Rasterizer, LINE_GAP};

struct Args {
    half: PathBuf,
    std: PathBuf,
    spc: PathBuf,
    out: PathBuf,
    cols: i32,
    rows: i32,
    dir: Option<PathBuf>,
    file: Option<PathBuf>,
    edit: Option<PathBuf>,
    cfg: Option<PathBuf>,
}

fn parse_args() -> Args {
    let mut a = Args {
        half: "cvga.fon".into(),
        std: "STDFONT.15".into(),
        spc: "SPCFONT.15".into(),
        out: "screen.png".into(),
        cols: 80,
        rows: 25,
        dir: None,
        file: None,
        edit: None,
        cfg: None,
    };
    let mut it = std::env::args().skip(1);
    while let Some(k) = it.next() {
        let mut v = || it.next().unwrap_or_else(|| die(&format!("{k} 少了參數")));
        match k.as_str() {
            "-half" => a.half = v().into(),
            "-eten-std" => a.std = v().into(),
            "-eten-spc" => a.spc = v().into(),
            "-o" => a.out = v().into(),
            "-cols" => a.cols = v().parse().unwrap_or_else(|_| die("-cols 要是整數")),
            "-rows" => a.rows = v().parse().unwrap_or_else(|_| die("-rows 要是整數")),
            "-dir" => a.dir = Some(v().into()),
            "-file" => a.file = Some(v().into()),
            "-edit" => a.edit = Some(v().into()),
            "-cfg" => a.cfg = Some(v().into()),
            _ => die(&format!("不認得的參數 {k}")),
        }
    }
    a
}

fn die(msg: &str) -> ! {
    eprintln!("celldump: {msg}");
    exit(1)
}

struct Eten(eten::Font);

impl CjkSource for Eten {
    fn glyph(&self, r: char) -> Option<wincv_fnt::Glyph> {
        self.0.glyph(r)
    }
}

fn main() {
    let a = parse_args();
    let half_bytes = std::fs::read(&a.half).unwrap_or_else(|e| die(&format!("{}: {e}", a.half.display())));
    let half = wincv_fnt::Font::parse(half_bytes).unwrap_or_else(|e| die(&e.to_string()));
    let (hw, hh) = half.size();
    let cjk: Option<Box<dyn CjkSource>> = match eten::Font::load(&a.std, Some(&a.spc), eten::NATIVE_W, eten::NATIVE_H) {
        Ok(f) => Some(Box::new(scale_cjk(Eten(f), hw * 2, hh + LINE_GAP))),
        Err(e) => {
            eprintln!("警告:載不到倚天字庫,全形字會留白 ({e})");
            None
        }
    };
    let mut r = Rasterizer::new(Box::new(half), cjk);
    r.missing_mark = true;

    let mut s = Screen::new(a.cols, a.rows);
    if let Some(f) = &a.edit {
        // 與 Go 版 celldump -edit 相同:語法設定目錄留空時跟著 -half 走。
        let cfg_dir = a.cfg.clone().unwrap_or_else(|| a.half.parent().map(PathBuf::from).unwrap_or_default());
        let data = std::fs::read(f).unwrap_or_else(|e| die(&format!("{}: {e}", f.display())));
        let syn = wincv_syntax::Set::load(&cfg_dir).ok().and_then(|set| set.for_name(&f.to_string_lossy()));
        let base = f.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
        wincv_editor::Model::load(&base, &data, wincv_textenc::Enc::Unknown, syn).draw(&mut s);
    } else if let Some(f) = &a.file {
        draw_file(&mut s, f);
    } else { match &a.dir {
        // 檔案列表:與 Go 版 celldump -dir 相同,不帶磁碟統計與註解。
        Some(dir) => {
            // 「a.zip!docs」是壓縮檔裡的目錄,與 Go 版相同。
            let raw = wincv_archive::path_bytes(dir);
            let fs: Box<dyn wincv_vfs::Fs> = match raw.iter().position(|&c| c == wincv_archive::SEP) {
                Some(i) if wincv_archive::is_archive(std::path::Path::new(&wincv_archive::bytes_os(&raw[..i]))) => {
                    let outer = PathBuf::from(wincv_archive::bytes_os(&raw[..i]));
                    Box::new(wincv_archive::ArchiveFs::open(&outer).unwrap_or_else(|e| die(&format!("{}: {e}", outer.display()))))
                }
                _ => Box::new(wincv_vfs::Os),
            };
            let mut m = wincv_browser::Model::new(fs, dir);
            m.load(dir).unwrap_or_else(|e| die(&format!("{}: {e}", dir.display())));
            m.draw(&mut s);
        }
        None => demo(&mut s),
    } }
    let (w, h) = r.size(s.cols, s.rows);
    let px = r.draw(&s);

    let f = std::fs::File::create(&a.out).unwrap_or_else(|e| die(&format!("{}: {e}", a.out.display())));
    let mut enc = png::Encoder::new(std::io::BufWriter::new(f), w as u32, h as u32);
    enc.set_color(png::ColorType::Rgba);
    enc.set_depth(png::BitDepth::Eight);
    let mut wr = enc.write_header().unwrap_or_else(|e| die(&e.to_string()));
    wr.write_image_data(px).unwrap_or_else(|e| die(&e.to_string()));
    println!(
        "{}  {w}x{h} px  ({}x{} 格,每格 {}x{})",
        a.out.display(), s.cols, s.rows, r.cell_w, r.cell_h
    );
}

/// 看檔(與 Go 版 celldump -file 相同):文字用文字檢視,二進位用 HEX。
/// 圖檔還沒搬,先不處理。
fn draw_file(s: &mut Screen, path: &std::path::Path) {
    let data = std::fs::read(path).unwrap_or_else(|e| die(&format!("{}: {e}", path.display())));
    let name = path.file_name().map(wincv_vfs::display_name).unwrap_or_default();
    let v = wincv_viewer::Model::load(&name, &data, wincv_textenc::Enc::Unknown);
    if v.enc == wincv_textenc::Enc::Binary {
        wincv_hexview::Model::load(&name, data).draw(s);
        return;
    }
    let mut v = v;
    v.draw(s);
}

/// 一眼看出「半形、全形、配色、格點」都對的畫面。內容與 Go 版逐字相同。
fn demo(s: &mut Screen) {
    s.clear(Color::LT_GRAY, Color::BLACK);
    s.fill(0, 0, s.cols, 1, ' ', Color::BLACK, Color::LT_GRAY);
    s.print(1, 0, "1檔案 2目錄/磁碟 3顏色 4檢視 5環境 6其他 7說明", Color::BLACK, Color::LT_GRAY);

    s.print(2, 2, "半形 ASCII:", Color::WHITE, Color::BLACK);
    let line: String = (0x20u8..0x7F).map(|c| c as char).collect();
    let n = line.len().min((s.cols - 2).max(0) as usize);
    s.print(2, 3, &line[..n], Color::LT_GRAY, Color::BLACK);

    s.print(2, 5, "全形漢字:", Color::WHITE, Color::BLACK);
    s.print(2, 6, "檔案瀏覽器 壓縮檔管理 文字檢視 編碼轉換", Color::LT_CYAN, Color::BLACK);
    s.print(2, 7, "全形標點:，。！？「」『』（）《》～", Color::YELLOW, Color::BLACK);

    s.print(2, 9, "29 色(名稱與 RGB 都取自 image):", Color::WHITE, Color::BLACK);
    const PER_ROW: usize = 3;
    for i in 0..NUM_COLORS {
        let (col, row) = (i % PER_ROW, i / PER_ROW);
        let (x, y) = (2 + col as i32 * 24, 10 + row as i32);
        s.print(x, y, &format!("{i:2} {:<12}", NAMES[i]), Color(i as u8), Color::BLACK);
        s.fill(x + 16, y, 4, 1, ' ', Color::BLACK, Color(i as u8));
    }

    let y = 10 + NUM_COLORS.div_ceil(PER_ROW) as i32 + 1;
    s.print(2, y, "反白游標列示範(整列換屬性,字不動):", Color::WHITE, Color::BLACK);
    s.print(2, y + 1, "7-zip32  dll     228,864  2004-10-01  10:00", Color::LT_GRAY, Color::BLACK);
    s.print(2, y + 2, "big52gbk txt      69,865  2002-10-21  10:00", Color::LT_GRAY, Color::BLACK);
    s.set_attr(2, y + 2, 44, Color::BLACK, Color::LT_GRAY);

    s.print(2, y + 4, "全形字佔兩格,右半格是佔位:中|文|字", Color::LT_GREEN, Color::BLACK);
    s.print(2, y + 5, "混排 mixed 中英 test 對齊 align", Color::LT_MAGENTA, Color::BLACK);
}
