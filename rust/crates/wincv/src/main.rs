//! WinCV Remake 的視窗進入點(對應 Go 版 `cmd/wincv`)。
//!
//! 目前是第一階段的試作:開一個視窗,畫 celldump 的示範畫面,
//! 用來量產物大小、啟動時間與記憶體。畫面由 `wincv-render` 的 CPU
//! 光柵器產生,softbuffer 只負責把像素貼到視窗上。

// Windows 上不要多開一個主控台視窗(Go 版用 -H windowsgui 做同一件事)。
#![cfg_attr(windows, windows_subsystem = "windows")]

use std::num::NonZeroU32;
use std::path::PathBuf;
use std::process::exit;
use std::rc::Rc;

use softbuffer::{Context, Surface};
use winit::application::ApplicationHandler;
use winit::dpi::PhysicalSize;
use winit::event::{ElementState, KeyEvent, WindowEvent};
use winit::event_loop::{ActiveEventLoop, EventLoop};
use winit::keyboard::{Key, NamedKey};
use winit::window::{Window, WindowId};

use wincv_cell::{Color, Screen};
use wincv_eten as eten;
use wincv_render::{scale_cjk, CjkSource, Rasterizer, LINE_GAP};

struct Eten(eten::Font);

impl CjkSource for Eten {
    fn glyph(&self, r: char) -> Option<wincv_fnt::Glyph> {
        self.0.glyph(r)
    }
}

struct Args {
    half: PathBuf,
    std: PathBuf,
    spc: PathBuf,
    cols: i32,
    rows: i32,
    scale: u32,
}

fn die(msg: &str) -> ! {
    eprintln!("wincv: {msg}");
    exit(1)
}

fn parse_args() -> Args {
    let mut a = Args {
        half: "cvga.fon".into(),
        std: "STDFONT.15".into(),
        spc: "SPCFONT.15".into(),
        cols: 80,
        rows: 25,
        scale: 1,
    };
    let mut it = std::env::args().skip(1);
    while let Some(k) = it.next() {
        if k == "-version" {
            println!("{}", env!("CARGO_PKG_VERSION"));
            exit(0);
        }
        let mut v = || it.next().unwrap_or_else(|| die(&format!("{k} 少了參數")));
        match k.as_str() {
            "-half" => a.half = v().into(),
            "-eten-std" => a.std = v().into(),
            "-eten-spc" => a.spc = v().into(),
            "-cols" => a.cols = v().parse().unwrap_or_else(|_| die("-cols 要是整數")),
            "-rows" => a.rows = v().parse().unwrap_or_else(|_| die("-rows 要是整數")),
            "-scale" => a.scale = v().parse::<u32>().unwrap_or_else(|_| die("-scale 要是正整數")).max(1),
            _ => die(&format!("不認得的參數 {k}")),
        }
    }
    a
}

struct App {
    raster: Rasterizer,
    screen: Screen,
    scale: u32,
    window: Option<Rc<Window>>,
    surface: Option<Surface<Rc<Window>, Rc<Window>>>,
}

impl App {
    fn pixel_size(&self) -> (u32, u32) {
        let (w, h) = self.raster.size(self.screen.cols, self.screen.rows);
        (w as u32 * self.scale, h as u32 * self.scale)
    }

    fn redraw(&mut self) {
        let (w, h) = self.pixel_size();
        let scale = self.scale as usize;
        let src_w = w as usize / scale;
        let rgba = self.raster.draw(&self.screen).to_vec();
        let Some(surface) = self.surface.as_mut() else { return };
        let (Some(nw), Some(nh)) = (NonZeroU32::new(w), NonZeroU32::new(h)) else { return };
        if surface.resize(nw, nh).is_err() {
            return;
        }
        let Ok(mut buf) = surface.buffer_mut() else { return };
        // softbuffer 的像素是 0x00RRGGBB;整數倍放大用最近鄰,點陣字不糊。
        for (y, row) in buf.chunks_mut(w as usize).enumerate() {
            let sy = y / scale;
            for (x, px) in row.iter_mut().enumerate() {
                let o = (sy * src_w + x / scale) * 4;
                *px = (rgba[o] as u32) << 16 | (rgba[o + 1] as u32) << 8 | rgba[o + 2] as u32;
            }
        }
        let _ = buf.present();
    }
}

impl ApplicationHandler for App {
    fn resumed(&mut self, el: &ActiveEventLoop) {
        if self.window.is_some() {
            return;
        }
        let (w, h) = self.pixel_size();
        let attrs = Window::default_attributes()
            .with_title("WinCV")
            .with_inner_size(PhysicalSize::new(w, h))
            .with_resizable(false);
        let window = Rc::new(el.create_window(attrs).unwrap_or_else(|e| die(&e.to_string())));
        let ctx = Context::new(window.clone()).unwrap_or_else(|e| die(&e.to_string()));
        self.surface = Some(Surface::new(&ctx, window.clone()).unwrap_or_else(|e| die(&e.to_string())));
        self.window = Some(window);
    }

    fn window_event(&mut self, el: &ActiveEventLoop, _id: WindowId, ev: WindowEvent) {
        match ev {
            WindowEvent::CloseRequested => el.exit(),
            WindowEvent::KeyboardInput {
                event: KeyEvent { logical_key: Key::Named(NamedKey::Escape), state: ElementState::Pressed, .. },
                ..
            } => el.exit(),
            WindowEvent::RedrawRequested => self.redraw(),
            _ => {}
        }
    }
}

fn main() {
    let a = parse_args();
    let half_bytes = std::fs::read(&a.half).unwrap_or_else(|e| die(&format!("{}: {e}", a.half.display())));
    let half = wincv_fnt::Font::parse(half_bytes).unwrap_or_else(|e| die(&e.to_string()));
    let (hw, hh) = half.size();
    let cjk: Option<Box<dyn CjkSource>> = eten::Font::load(&a.std, Some(&a.spc), eten::NATIVE_W, eten::NATIVE_H)
        .map(|f| Box::new(scale_cjk(Eten(f), hw * 2, hh + LINE_GAP)) as Box<dyn CjkSource>)
        .map_err(|e| eprintln!("警告:載不到倚天字庫 ({e})"))
        .ok();
    let raster = Rasterizer::new(Box::new(half), cjk);
    let mut screen = Screen::new(a.cols, a.rows);
    screen.fill(0, 0, a.cols, 1, ' ', Color::BLACK, Color::LT_GRAY);
    screen.print(1, 0, "1檔案 2目錄/磁碟 3網路 4標記 5檢視 6轉檔/轉碼/其他 7設定 8說明", Color::BLACK, Color::LT_GRAY);
    screen.print(2, 2, "WinCV Remake(Rust 試作)", Color::LT_YELLOW, Color::BLACK);
    screen.print(2, 4, "全形漢字:檔案瀏覽器 壓縮檔管理 文字檢視 編碼轉換", Color::LT_CYAN, Color::BLACK);
    screen.print(2, 5, "按 Esc 離開", Color::LT_GRAY, Color::BLACK);

    let el = EventLoop::new().unwrap_or_else(|e| die(&e.to_string()));
    let mut app = App { raster, screen, scale: a.scale, window: None, surface: None };
    el.run_app(&mut app).unwrap_or_else(|e| die(&e.to_string()));
}
