//! WinCV Remake 的視窗進入點(對應 Go 版 `cmd/wincv`)。
//!
//! 畫面由 `wincv-app` 畫成格點、`wincv-render` 的 CPU 光柵器轉成像素,softbuffer 只負責
//! 把像素貼到視窗上。放大用最近鄰自己做,點陣字才不會被插值糊掉。

// Windows 上不要多開一個主控台視窗(Go 版用 -H windowsgui 做同一件事)。
#![cfg_attr(windows, windows_subsystem = "windows")]

mod input;

use std::num::NonZeroU32;
use std::path::{Path, PathBuf};
use std::process::exit;
use std::rc::Rc;

use softbuffer::{Context, Surface};
use winit::application::ApplicationHandler;
use winit::dpi::PhysicalSize;
use winit::event::WindowEvent;
use winit::event_loop::{ActiveEventLoop, EventLoop};
use winit::window::{Fullscreen, Window, WindowId};

use wincv_app::{App, Mode, MAX_SCALE, MIN_SCALE};
use wincv_cell::Screen;
use wincv_fontset as fontset;
use wincv_i18n::{t, tr};
use wincv_render::{CjkSource, Overlay, Rasterizer};
use wincv_session::State;

fn die(msg: &str) -> ! {
    eprintln!("{} {msg}", t("錯誤:"));
    exit(1)
}

/// 「差這麼多就當剛好」的容差。5% 是尺寸上看不出來、但銳利度上差很多的那個區間:
/// 8×16 的格子放大 1.19 倍與 1.20 倍只差 0.16 個像素,而前者可以整格換成原生的 10×18 字模。
const SCALE_SNAP: f64 = 0.05;

/// 命令列。
struct Args {
    half: Option<PathBuf>,
    std: Option<PathBuf>,
    spc: Option<PathBuf>,
    std24: Option<PathBuf>,
    spc24: Option<PathBuf>,
    cols: i32,
    rows: i32,
    cols_given: bool,
    scale: f64,
    zoom: i32,
    fallback: Option<PathBuf>,
    no_fallback: bool,
    lang: String,
    bitmap_cjk: bool,
    no_resume: bool,
    menu_scale: f64,
    /// 原版的啟動語法:一個路徑,或 /p、/e 加一個路徑。
    target: Option<PathBuf>,
    start_dir_only: bool,
    start_edit: bool,
}

fn parse_args() -> Args {
    let mut a = Args {
        half: None,
        std: None,
        spc: None,
        std24: None,
        spc24: None,
        cols: 80,
        rows: 30,
        cols_given: false,
        scale: 2.0,
        zoom: 0,
        fallback: None,
        no_fallback: false,
        lang: String::new(),
        bitmap_cjk: false,
        no_resume: false,
        menu_scale: 0.0,
        target: None,
        start_dir_only: false,
        start_edit: false,
    };
    let mut rest = Vec::new();
    let mut it = std::env::args().skip(1);
    while let Some(k) = it.next() {
        // Go 的 flag 套件接受 -x 與 --x 兩種寫法。
        let flag = k.strip_prefix("--").map(|s| format!("-{s}")).unwrap_or_else(|| k.clone());
        let mut v = || it.next().unwrap_or_else(|| die(&format!("{k} 少了參數")));
        let num = |s: String, name: &str| -> f64 { s.parse().unwrap_or_else(|_| die(&format!("{name} 要是數字"))) };
        match flag.as_str() {
            "-version" => {
                println!("{}", wincv_app::VERSION);
                exit(0);
            }
            "-half" => a.half = Some(v().into()),
            "-eten-std" => a.std = Some(v().into()),
            "-eten-spc" => a.spc = Some(v().into()),
            "-eten24-std" => a.std24 = Some(v().into()),
            "-eten24-spc" => a.spc24 = Some(v().into()),
            "-cols" => {
                a.cols = num(v(), "-cols") as i32;
                a.cols_given = true;
            }
            "-rows" => {
                a.rows = num(v(), "-rows") as i32;
                a.cols_given = true;
            }
            "-scale" => a.scale = num(v(), "-scale"),
            "-zoom" => a.zoom = num(v(), "-zoom") as i32,
            "-fallback" => a.fallback = Some(v().into()),
            "-no-fallback" => a.no_fallback = true,
            "-lang" => a.lang = v(),
            "-bitmap-cjk" => a.bitmap_cjk = true,
            "-no-resume" => a.no_resume = true,
            "-menu-scale" => a.menu_scale = num(v(), "-menu-scale"),
            _ if flag.starts_with('-') && flag.len() > 1 => die(&format!("不認得的參數 {k}")),
            _ => rest.push(k),
        }
    }
    // 原版保留的啟動語法:/p 只能指定目錄,/e 直接進編輯器。
    match rest.as_slice() {
        [] => {}
        [m, p] if m == "/p" || m == "/e" => {
            a.target = Some(p.into());
            a.start_dir_only = m == "/p";
            a.start_edit = m == "/e";
        }
        [m, ..] if m == "/p" || m == "/e" => die(&tr!("%s 需要恰好一個路徑", m)),
        [p] => a.target = Some(p.into()),
        _ => die(&t("預期是一個啟動路徑，或 /p／/e 加一個路徑")),
    }
    a
}

/// 24 點字形來源代號對到檔案。JSON 不保存絕對路徑,所以移動 WINCV_HOME 後不用重新設定。
fn cjk24_paths(id: &str) -> Option<(PathBuf, PathBuf)> {
    let s = wincv_eten::cjk24::find(id)?;
    for name in s.std_names() {
        if let (d, true) = wincv_datadir::find(name) {
            return Some((d.join(name), wincv_datadir::resolve(s.spc)));
        }
    }
    Some((wincv_datadir::resolve(s.std), wincv_datadir::resolve(s.spc)))
}

fn new_rast(l: &fontset::Level) -> Rasterizer {
    let mut r = Rasterizer::new(Box::new(l.half.clone()), l.cjk.clone().map(|c| Box::new(c) as Box<dyn CjkSource>));
    r.fallback = l.fb.clone().map(|f| Box::new(f) as Box<dyn CjkSource>);
    r.missing_mark = true;
    r
}

/// 視窗那一層的狀態(對應 Go 版的 game)。
struct Shell {
    app: App,
    levels: Vec<fontset::Level>,
    /// 各字級的光柵器,懶建。pick_level 每次都要問各字級的格子多大,而建一個光柵器要
    /// 配緩衝區,每幀重建會讓捲動變成幻燈片。
    rasts: Vec<Option<Rasterizer>>,
    zoom: i32,
    scale: f64,
    /// pick_level 挑出來的「實際用哪一級、還要再放大幾倍」。
    eff_idx: usize,
    eff_scale: f64,
    cols: i32,
    rows: i32,
    screen: Screen,
    /// 選單層目前用第幾級點陣字,-1 表示跟內容一樣;menu_scale 是那時的倍率。
    menu_zoom: i32,
    menu_scale: f64,
    /// 目前已成功套用的 24 點字形來源。app.cjk24 不同時,代表選單剛要求換字形。
    cjk24: String,
    font_dir: PathBuf,
    eten_std: PathBuf,
    eten_spc: PathBuf,
    fallback: Option<PathBuf>,
    no_fallback: bool,
    resume: bool,
    last_st: State,
    keys: input::Keys,
    window: Option<Rc<Window>>,
    surface: Option<Surface<Rc<Window>, Rc<Window>>>,
    /// 視窗的像素大小。
    win: (u32, u32),
    /// 啟動時要的視窗大小(視窗建立前算好)。
    want_px: (u32, u32),
}

impl Shell {
    fn rast(&mut self, i: usize) -> Option<&mut Rasterizer> {
        if i >= self.levels.len() {
            return None;
        }
        if self.rasts.len() != self.levels.len() {
            self.rasts = (0..self.levels.len()).map(|_| None).collect();
        }
        if self.rasts[i].is_none() {
            self.rasts[i] = Some(new_rast(&self.levels[i]));
        }
        self.rasts[i].as_mut()
    }

    fn cell_of(&mut self, i: usize) -> (i32, i32) {
        self.rast(i).map(|r| (r.cell_w, r.cell_h)).unwrap_or((0, 0))
    }

    /// 依「使用者選的字級 + 想要的倍率」決定**實際**用哪一個字級,以及還要再放大幾倍。
    ///
    /// 原版隨附三種點陣字(8×15 / 10×18 / 12×24),它們之間的比例正好是 1.19 與 1.56。
    /// 「放大 1.2 倍」可以用**原生的 10×18 字模**達成,而不是把 8×15 的字模拉大 1.2 倍:
    /// 後者必然有一列被複製兩次、一列沒有。只往**大**的字級找,往小找等於縮小再放大。
    fn pick_level(&mut self) -> (usize, f64) {
        let zoom = self.zoom.max(0) as usize;
        let (_, base_h) = self.cell_of(zoom);
        if base_h <= 0 {
            return (zoom, self.scale);
        }
        let target = base_h as f64 * self.scale;
        let (mut best_idx, mut best_rest) = (zoom, self.scale);
        let mut best_err = (self.scale - self.scale.round()).abs();
        for i in zoom + 1..self.levels.len() {
            let (_, ch) = self.cell_of(i);
            if ch <= 0 {
                continue;
            }
            let rest = target / ch as f64;
            // 再大的字級就超過使用者要的尺寸了:那等於縮小,不做。
            if rest < 1.0 - SCALE_SNAP {
                break;
            }
            let e = (rest - rest.round()).abs();
            if e < best_err {
                (best_idx, best_rest, best_err) = (i, rest, e);
            }
        }
        // 差一點點就當剛好。1.02 倍與 1.00 倍的尺寸差不到一個像素,但前者會讓某一列的
        // 像素變成雙倍粗。
        if (best_rest - best_rest.round()).abs() <= SCALE_SNAP {
            best_rest = best_rest.round();
        }
        (best_idx, best_rest.max(1.0))
    }

    fn apply_level(&mut self) {
        let (idx, rest) = self.pick_level();
        let Some((cw, ch)) = self.rast(idx).map(|r| (r.cell_w, r.cell_h)) else { return };
        (self.eff_idx, self.eff_scale) = (idx, rest);
        (self.app.cell_w, self.app.cell_h) = (cw, ch);
    }

    fn set_zoom(&mut self, n: i32) {
        if n < 0 || n as usize >= self.levels.len() || n == self.zoom {
            return;
        }
        self.zoom = n;
        self.apply_level();
        // 換字級要保住**格數**而不是視窗大小:使用者按放大字體想要的是「字變大」,不是
        // 「同樣大的視窗裡剩下一半的內容」。
        if self.cols > 0 && self.rows > 0 {
            self.apply_size(self.cols, self.rows);
        }
    }

    /// 放大之後一格佔幾個螢幕像素。無條件進位:取小的那邊會讓最後一列被視窗切掉一半。
    fn cell_px(&mut self) -> (i32, i32) {
        let (cw, ch) = self.cell_of(self.eff_idx);
        ((cw as f64 * self.eff_scale).ceil() as i32, (ch as f64 * self.eff_scale).ceil() as i32)
    }

    /// 選單層用哪個字級、倍率,以及一格的螢幕像素。倍率只在這裡算一次。
    fn menu(&mut self) -> (usize, f64, i32, i32) {
        let (idx, mut sc) = if self.menu_zoom >= 0 && (self.menu_zoom as usize) < self.levels.len() {
            (self.menu_zoom as usize, self.menu_scale)
        } else {
            // 沒有另外指定時整層沿用內容的字級與倍率。
            (self.eff_idx, self.eff_scale)
        };
        if sc <= 0.0 {
            sc = 1.0;
        }
        let (cw, ch) = self.cell_of(idx);
        (idx, sc, (cw as f64 * sc).ceil() as i32, (ch as f64 * sc).ceil() as i32)
    }

    /// 選單列佔掉的螢幕像素高。關著時是 0。
    fn menu_bar_px(&mut self) -> i32 {
        if !self.app.menu_bar {
            return 0;
        }
        self.menu().3
    }

    /// 依視窗像素大小算出裝得下幾欄幾列。格子大小固定,視窗變大就多幾格。
    fn resize(&mut self, w: i32, h: i32) {
        let (cw, ch) = self.cell_px();
        if cw <= 0 || ch <= 0 {
            return;
        }
        // 選單列吃掉的是**像素**不是格:它可以用完全不同的字型大小。
        let cols = (w / cw).max(20);
        let rows = ((h - self.menu_bar_px()) / ch).max(5);
        if (cols, rows) == (self.cols, self.rows) {
            return;
        }
        (self.cols, self.rows) = (cols, rows);
        self.screen = Screen::new(cols, rows);
    }

    /// 把視窗調成 cols×rows 格。全螢幕時不動視窗:那會把使用者踢出全螢幕。
    fn apply_size(&mut self, cols: i32, rows: i32) {
        let (cw, ch) = self.cell_px();
        let (w, h) = (cols * cw, rows * ch + self.menu_bar_px());
        match &self.window {
            Some(win) if win.fullscreen().is_some() => {
                let s = win.inner_size();
                self.resize(s.width as i32, s.height as i32);
            }
            Some(win) => {
                let _ = win.request_inner_size(PhysicalSize::new(w as u32, h as u32));
                self.resize(w, h);
            }
            None => {
                self.want_px = (w as u32, h as u32);
                self.resize(w, h);
            }
        }
    }

    fn reload_cjk24(&mut self) {
        let Some(source) = wincv_eten::cjk24::find(&self.app.cjk24) else {
            self.app.cjk24 = self.cjk24.clone();
            return;
        };
        let (std24, spc24) = cjk24_paths(source.id).unwrap_or_default();
        let f24 = match fontset::eten24(Some(&std24), Some(&spc24)) {
            Ok(f) => f,
            Err(e) => {
                self.app.cjk24 = self.cjk24.clone();
                let mut msg = e.to_string();
                if !std24.exists() {
                    // 找不到時 datadir 給的是「最後一個找過的位置」,那個路徑對使用者沒有意義。
                    // 講要找的檔名,與該放在哪裡。
                    msg = tr!("找不到 %s,請放在程式旁", source.std_names().join(" / "));
                }
                self.app.message = tr!("CJK 字形未變更: %v", msg);
                return;
            }
        };
        let p = fontset::EtenPaths { std: Some(&self.eten_std), spc: Some(&self.eten_spc), std24: Some(&std24), spc24: Some(&spc24) };
        let levels = fontset::load(&self.font_dir, p, self.fallback.as_deref(), self.no_fallback);
        if levels.is_empty() {
            self.app.cjk24 = self.cjk24.clone();
            self.app.message = t("CJK 字形未變更: 無可用字級");
            return;
        }
        let (cols, rows) = (self.cols, self.rows);
        self.levels = levels;
        self.rasts.clear();
        self.app.max_zoom = self.levels.len() as i32 - 1;
        self.app.zoom = self.app.zoom.min(self.app.max_zoom);
        self.zoom = -1;
        self.set_zoom(self.app.zoom);
        self.menu_zoom = self.app.menu_zoom;
        if cols > 0 && rows > 0 {
            self.apply_size(cols, rows);
        }
        self.cjk24 = source.id.to_string();
        self.app.message = tr!("CJK 字形已套用: %s", wincv_app::cjk24_name(source.id));
        if !f24.supp_note.is_empty() {
            self.app.message += &format!("; {}", f24.supp_note);
        }
    }

    /// 每次有輸入之後,把 app 那一層翻的旗標套到視窗上(對應 Go 版 Update 的後半)。
    fn sync(&mut self, el: &ActiveEventLoop) {
        if self.app.quit {
            el.exit();
            return;
        }
        self.save_state();
        if self.app.cjk24 != self.cjk24 {
            self.reload_cjk24();
        }
        if let Some(win) = &self.window {
            if self.app.fullscreen != win.fullscreen().is_some() {
                win.set_fullscreen(self.app.fullscreen.then_some(Fullscreen::Borderless(None)));
            }
        }
        if self.app.zoom != self.zoom {
            self.set_zoom(self.app.zoom);
        }
        // 倍率改變時,視窗的像素大小要跟著改,否則格數會少掉一半。換倍率時重挑字級:
        // 1.2 倍用原生的 10×18 字模比把 8×15 拉大 1.2 倍銳利得多。
        if self.app.scale != self.scale && (MIN_SCALE..=MAX_SCALE).contains(&self.app.scale) {
            let (cols, rows) = (self.cols, self.rows);
            self.scale = self.app.scale;
            self.apply_level();
            self.apply_size(cols, rows);
        }
        if self.app.menu_zoom != self.menu_zoom {
            self.menu_zoom = self.app.menu_zoom;
        }
        // 選單裡的「視窗大小」只留下請求,真的去動視窗是這裡的事。
        if self.app.want_cols > 0 && self.app.want_rows > 0 {
            self.apply_size(self.app.want_cols, self.app.want_rows);
            (self.app.want_cols, self.app.want_rows) = (0, 0);
        }
        if let Some(win) = &self.window {
            // 編輯器與輸入列要收輸入法送來的字。
            win.set_ime_allowed(self.text_mode());
            win.request_redraw();
        }
    }

    fn text_mode(&self) -> bool {
        self.app.mode == Mode::Edit || self.app.prompting()
    }

    /// 在位置變了的時候記下來。不等到關閉才存:程式被 kill、系統關機、當掉時「關閉時才存」
    /// 等於沒存。只比對「人在哪裡」那幾個欄位,所以捲一頁不會寫檔。
    fn save_state(&mut self) {
        if !self.resume {
            return;
        }
        let st = self.app.snapshot();
        let l = &self.last_st;
        if st.dir == l.dir && st.mode == l.mode && st.file == l.file && st.cjk24 == l.cjk24 {
            return;
        }
        // 存不了就算了。這是錦上添花的功能,不該在每一幀噴一次錯誤。
        let _ = st.save();
        self.last_st = st;
    }

    /// 把內容層與選單層合成成視窗的像素(0x00RRGGBB)。兩層各自放大再貼:兩者的倍率
    /// 是分開的,而且點陣字必須整格複製像素。
    fn paint(&mut self, w: u32, h: u32) -> Vec<u32> {
        let mut canvas = vec![0u32; (w * h) as usize];
        // 縮圖要先解好才畫得出來。一個目錄幾十張 JPEG 解起來要幾秒,之後改到背景做。
        if self.app.mode == Mode::Thumbs {
            self.app.decode_thumbs(self.cols, self.rows);
        }
        let top = self.menu_bar_px();
        let ov: Vec<Overlay> = self.app.draw(&mut self.screen);
        let refs: Vec<&Overlay> = ov.iter().collect();
        let (eff, sc) = (self.eff_idx, self.eff_scale);
        let screen = std::mem::replace(&mut self.screen, Screen::new(0, 0));
        if let Some(r) = self.rast(eff) {
            let (iw, ih) = r.size(screen.cols, screen.rows);
            let img = r.draw_with(&screen, &refs);
            blit(&mut canvas, w, h, img, iw, ih, 0, top, sc);
        }
        self.screen = screen;
        if top > 0 {
            let (mi, msc, mcw, mch) = self.menu();
            let layer = self.app.menu_layer(w as i32 / mcw.max(1), h as i32 / mch.max(1));
            if let Some(r) = self.rast(mi) {
                if let Some(bar) = &layer.bar {
                    let (bw, bh) = r.size(bar.cols, bar.rows);
                    blit(&mut canvas, w, h, r.draw(bar), bw, bh, 0, 0, msc);
                }
                if let Some(drop) = &layer.drop {
                    // 光柵器的緩衝區會被下一次呼叫重用,所以下拉一定要在選單列貼完之後才畫。
                    let (dw, dh) = r.size(drop.cols, drop.rows);
                    blit(&mut canvas, w, h, r.draw(drop), dw, dh, layer.drop_x * mcw, mch, msc);
                }
            }
        }
        canvas
    }

    fn redraw(&mut self) {
        let Some(win) = self.window.clone() else { return };
        let s = win.inner_size();
        let (w, h) = (s.width.max(1), s.height.max(1));
        if (w, h) != self.win {
            self.win = (w, h);
            self.resize(w as i32, h as i32);
        }
        let canvas = self.paint(w, h);
        let Some(surface) = self.surface.as_mut() else { return };
        let (Some(nw), Some(nh)) = (NonZeroU32::new(w), NonZeroU32::new(h)) else { return };
        if surface.resize(nw, nh).is_err() {
            return;
        }
        let Ok(mut buf) = surface.buffer_mut() else { return };
        buf.copy_from_slice(&canvas);
        let _ = buf.present();
    }
}

/// 把一張 RGBA 格點圖放大 sc 倍(最近鄰)貼到 canvas 的 (x, y)。
#[allow(clippy::too_many_arguments)]
fn blit(canvas: &mut [u32], cw: u32, ch: u32, img: &[u8], iw: i32, ih: i32, x: i32, y: i32, sc: f64) {
    if iw <= 0 || ih <= 0 {
        return;
    }
    let (dw, dh) = ((iw as f64 * sc).ceil() as i32, (ih as f64 * sc).ceil() as i32);
    for dy in 0..dh {
        let ty = y + dy;
        if ty < 0 || ty >= ch as i32 {
            continue;
        }
        // 取樣點在目標像素的中心,與 GPU 的最近鄰取樣相同。
        let sy = (((dy as f64 + 0.5) / sc) as i32).min(ih - 1);
        let row = &img[(sy * iw * 4) as usize..((sy + 1) * iw * 4) as usize];
        let out = &mut canvas[(ty as u32 * cw) as usize..((ty as u32 + 1) * cw) as usize];
        for dx in 0..dw {
            let tx = x + dx;
            if tx < 0 || tx >= cw as i32 {
                continue;
            }
            let sx = (((dx as f64 + 0.5) / sc) as i32).min(iw - 1) as usize * 4;
            out[tx as usize] = (row[sx] as u32) << 16 | (row[sx + 1] as u32) << 8 | row[sx + 2] as u32;
        }
    }
}

impl ApplicationHandler for Shell {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.window.is_some() {
            return;
        }
        let attrs = Window::default_attributes()
            .with_title("WinCV")
            .with_inner_size(PhysicalSize::new(self.want_px.0.max(1), self.want_px.1.max(1)))
            .with_resizable(true);
        // 網頁上視窗就是一個 canvas,要自己掛進頁面。
        #[cfg(target_arch = "wasm32")]
        let attrs = {
            use winit::platform::web::WindowAttributesExtWebSys;
            attrs.with_append(true)
        };
        let window = Rc::new(el.create_window(attrs).unwrap_or_else(|e| die(&e.to_string())));
        let ctx = Context::new(window.clone()).unwrap_or_else(|e| die(&e.to_string()));
        self.surface = Some(Surface::new(&ctx, window.clone()).unwrap_or_else(|e| die(&e.to_string())));
        // 不是每個平台都會在建立後自動送一次重繪(網頁就不會)。
        window.request_redraw();
        self.window = Some(window);
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _id: WindowId, ev: WindowEvent) {
        match ev {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::RedrawRequested => self.redraw(),
            WindowEvent::Resized(_) => {
                if let Some(w) = &self.window {
                    w.request_redraw();
                }
            }
            WindowEvent::ModifiersChanged(m) => self.keys.modifiers(m.state()),
            WindowEvent::KeyboardInput { event, .. } => {
                let ks = self.keys.key_event(&event, self.text_mode());
                if !ks.is_empty() {
                    for k in ks {
                        self.app.handle_key(k);
                    }
                    self.sync(el);
                }
            }
            WindowEvent::Ime(winit::event::Ime::Commit(s)) if self.text_mode() => {
                for r in s.chars().filter(|&r| r as u32 >= 0x20 && r as u32 != 0x7F) {
                    self.app.handle_key(wincv_keys::Key::ch(r));
                }
                self.sync(el);
            }
            WindowEvent::MouseInput { .. } => self.keys.mouse_pressed(),
            _ => {}
        }
    }
}

fn main() {
    let a = parse_args();
    let target = a.target.clone().unwrap_or_else(|| PathBuf::from("."));
    let abs = std::path::absolute(&target).unwrap_or_else(|e| die(&e.to_string()));
    // 檔案啟動時 App 必須先有一個可列目錄的目錄;真正的模式分派在字型與 session 都
    // 就緒後交給 start_path,與清單上的 Enter 共用。
    let mut initial_dir = abs.clone();
    if a.target.is_some() {
        let md = std::fs::metadata(&abs).unwrap_or_else(|e| die(&format!("{}: {e}", abs.display())));
        if a.start_dir_only && !md.is_dir() {
            die(&tr!("/p 只能指定目錄，不可指定 %q", &*target.to_string_lossy()));
        }
        if !md.is_dir() {
            initial_dir = abs.parent().map(Path::to_path_buf).unwrap_or(abs.clone());
        }
    }
    let mut app = App::new(Box::new(wincv_vfs::Os), &initial_dir);
    // 命令列指定了目錄就以它為準:使用者說了要去哪,不該被上次的位置蓋掉。
    let mut st = State::default();
    if !a.no_resume {
        st = wincv_session::load();
        if a.target.is_some() {
            st.dir = initial_dir.to_string_lossy().into_owned();
            st.cursor.clear();
            st.mode.clear();
            st.file.clear();
        }
    }
    if wincv_eten::cjk24::find(&st.cjk24).is_some() {
        app.cjk24 = st.cjk24.clone();
    }
    // 原版素材放在哪裡由 datadir 決定,不是相對於工作目錄猜一個。
    use wincv_datadir::resolve2;
    let half = resolve2(a.half.as_deref(), "cvga.fon");
    let std = resolve2(a.std.as_deref(), "STDFONT.15");
    let spc = resolve2(a.spc.as_deref(), "SPCFONT.15");
    let (def24, defspc24) = cjk24_paths(&app.cjk24).unwrap_or_default();
    let std24 = a.std24.as_deref().map(|p| resolve2(Some(p), "STD.24M")).unwrap_or(def24);
    let spc24 = a.spc24.as_deref().map(|p| resolve2(Some(p), "SPCFONT.24")).unwrap_or(defspc24);
    // 語法上色設定跟半形字型放在一起(原版是同一個安裝目錄)。
    let cfg_dir = half.parent().map(Path::to_path_buf).unwrap_or_default();
    app.load_syntax(&cfg_dir);
    app.dict_dir = cfg_dir.clone();

    // 語言的優先序:旗標 > session 記住的 > 系統語系。session 在 restore 那裡才讀得到,
    // 所以這裡先定下「旗標或系統」這一層。旗標永遠贏。
    match wincv_i18n::valid(&a.lang) {
        Some(l) => {
            wincv_i18n::set(l);
            app.lang = a.lang.clone();
        }
        None => {
            if !a.lang.is_empty() {
                eprint!("{}", tr!("警告:不認得的語言 %q,支援的是 zh-Hant / zh-Hans / en / ja\n", &a.lang));
            }
            wincv_i18n::set(wincv_i18n::detect());
        }
    }

    fontset::set_bitmap_cjk(a.bitmap_cjk);
    let paths = fontset::EtenPaths { std: Some(&std), spc: Some(&spc), std24: Some(&std24), spc24: Some(&spc24) };
    let mut levels = fontset::load(&cfg_dir, paths, a.fallback.as_deref(), a.no_fallback);
    if levels.is_empty() {
        // 沒有原版的 .FON 就用系統字型現場產一份。畫面不是原版的點陣字,但版面、欄位
        // 與按鍵完全一樣:這比「跑不起來」有用得多。
        levels = fontset::from_ttf(paths, a.fallback.as_deref(), a.no_fallback);
        if !levels.is_empty() {
            eprint!(
                "{}",
                wincv_i18n::sprintf(
                    &(t("提示:沒有原版的點陣字型,改用系統字型。\n")
                        + &t("      要與原版逐像素相同的畫面,把 cvga.fon 放進下列任一個目錄,\n")
                        + &t("      或用 -half 直接指定;也可以設 %s 指到素材目錄。\n%s")),
                    &[wincv_datadir::ENV_HOME.into(), wincv_datadir::explain().into()]
                )
            );
        }
    }
    if levels.is_empty() {
        die(&wincv_i18n::sprintf(
            &(t("一個半形字型都載不到:找不到原版的 .FON,") + &t("系統上也找不到可用的 TrueType。\n%s%s")),
            &[wincv_datadir::explain().into(), wincv_ttf::missing_hint().into()],
        ));
    }
    app.max_zoom = levels.len() as i32 - 1;
    let zoom = a.zoom.clamp(0, app.max_zoom);
    app.zoom = zoom;
    let scale = a.scale.clamp(MIN_SCALE, MAX_SCALE);
    app.scale = scale;
    let (mut cols, mut rows) = (a.cols, a.rows);
    if st.cols >= 20 && st.rows >= 5 && !a.cols_given {
        (cols, rows) = (st.cols, st.rows);
    }
    let mut sh = Shell {
        app,
        levels,
        rasts: Vec::new(),
        zoom: -1,
        scale,
        eff_idx: 0,
        eff_scale: scale,
        cols: 0,
        rows: 0,
        screen: Screen::new(cols, rows),
        menu_zoom: -1,
        menu_scale: a.menu_scale,
        cjk24: String::new(),
        font_dir: cfg_dir,
        eten_std: std,
        eten_spc: spc,
        fallback: a.fallback.clone(),
        no_fallback: a.no_fallback,
        resume: !a.no_resume,
        last_st: State::default(),
        keys: input::Keys::default(),
        window: None,
        surface: None,
        win: (0, 0),
        want_px: (0, 0),
    };
    sh.cjk24 = sh.app.cjk24.clone();
    sh.set_zoom(zoom);
    // -cols / -rows 與選單「視窗大小」、session 記住的格數是同一個意思:內容區的格數,
    // 選單列另外加在上面。resize 會把選單列的像素扣掉,所以這裡要先加回去。
    sh.apply_size(cols, rows);
    sh.app.restore(&st);
    sh.menu_zoom = sh.app.menu_zoom;
    if a.target.is_some() {
        if let Err(e) = sh.app.start_path(&abs, a.start_edit) {
            die(&e);
        }
    }
    sh.last_st = sh.app.snapshot();

    let el = EventLoop::new().unwrap_or_else(|e| die(&e.to_string()));
    let res = el.run_app(&mut sh);
    // 關掉時再存一次:這一次連捲動位置都記下來(sync 那邊只在「人換了地方」時寫)。
    if sh.resume {
        if let Err(e) = sh.app.snapshot().save() {
            eprintln!("{} {e}", t("記不下這次的位置:"));
        }
    }
    if let Err(e) = res {
        die(&e.to_string());
    }
}
