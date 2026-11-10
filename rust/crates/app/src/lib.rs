//! 把各個模式接起來:檔案瀏覽器、文字檢視器,以及它們之間的切換與按鍵分派
//! (對應 Go 版 `internal/app`)。
//!
//! 這一層不依賴視窗系統,所以整個互動流程可以 headless 測。

mod about;
mod actions;
mod bookview;
mod browse;
mod docview;
mod drivepane;
mod editfind;
mod filecolor;
mod help;
mod layout;
mod mdview;
mod menu;
mod mouse;
mod pdfview;
mod preview;
mod prompt;
mod session;
mod shell;
mod startup;
mod touch;
mod viewsize;

#[cfg(test)]
mod tests;

use std::collections::BTreeMap;
use std::ffi::OsString;
use std::io::Read;
use std::path::{Path, PathBuf};

use wincv_archive::ArchiveFs;
use wincv_browser::Model as Browser;
use wincv_cell::{is_wide, Color, Screen};
use wincv_i18n::{t, tr};
use wincv_keys::{Code, Key};
use wincv_render::Overlay;
use wincv_session::DocPos;
use wincv_textenc::Enc;
use wincv_vfs::{Fs, SortKey};

pub use about::VERSION;
pub use actions::FindResult;
pub use layout::sane_layout;
pub use mdview::{is_markdown, MAX_MD_IMAGE_BYTES};
pub use menu::{cjk24_name, MenuLayer, MENU_BAR_ROWS};
pub use preview::{MAX_PREVIEW_BYTES, PREVIEW_ROWS};
pub use touch::TOUCH_ROWS;
pub use viewsize::{MAX_SCALE, MIN_SCALE, SCALE_STEP};

/// 目前在哪一個畫面。
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Mode {
    #[default]
    Browser,
    Viewer,
    Hex,
    Image,
    Edit,
    Thumbs,
    Find,
    Markdown,
    Browse,
}

/// 檢視器一次讀進來的上限。原版能順順地看大檔;先設一個保守上限,之後改成串流。
pub const MAX_VIEW_BYTES: u64 = 64 << 20;

/// 字典視窗佔幾列。
pub const DICT_PANEL_ROWS: i32 = 4;

/// 進入壓縮檔之前在哪。進壓縮檔時 push,從壓縮檔最上層再往上時 pop,
/// 使用者感覺就只是進出目錄。
struct Layer {
    fs: Box<dyn Fs>,
    dir: PathBuf,
    cursor: i32,
}

/// 整個程式的狀態。
pub struct App {
    pub browser: Browser,
    pub viewer: Option<wincv_viewer::Model>,
    pub hex: Option<wincv_hexview::Model>,
    pub image: Option<wincv_imgview::Model>,
    pub editor: Option<wincv_editor::Model>,
    pub thumbs: Option<wincv_thumbs::Model>,
    /// 縮圖列表的代次。每開一次縮圖列表加一:背景解好的縮圖回來時,列表可能已經換成別的
    /// 目錄,代次對不上的就丟掉。
    thumbs_gen: u64,
    pub find: Option<FindResult>,
    pub mode: Mode,

    /// 語法上色設定,從原版的 keyword.cfg 載入。
    pub syntax: Option<wincv_syntax::Set>,

    /// 字典資料所在的目錄。show_dict 打開時才會真的載入:eng.txt.dat 有 5.6 MB,
    /// 不該在啟動時就吃掉那個時間。
    pub dict_dir: PathBuf,
    pub show_dict: bool,
    dict: Option<wincv_dict::Dict>,
    dict_err: String,

    /// 格子的像素尺寸。看圖模式要用它把格點座標換算成像素矩形;外層要在建立後設好。
    pub cell_w: i32,
    pub cell_h: i32,

    /// 暫時顯示在狀態列的訊息(錯誤、提示)。
    pub message: String,
    /// 被設成 true 時,外層應該結束程式。
    pub quit: bool,
    /// F11 的狀態。app 這一層不會自己去改視窗,外層每一幀比對這個旗標。
    pub fullscreen: bool,
    /// F8 的狀態:關掉中文解讀,一個位元組畫一格。
    pub english_only: bool,
    /// 焦點在左側磁碟窗格。窗格關著時永遠是 false。
    pub drive_focus: bool,
    /// 底部的觸控功能列(Android 版的介面)。
    pub touch: bool,
    /// 最上方那一列選單列。預設開著。
    pub menu_bar: bool,
    /// 選單那一層要用第幾個字級。-1 表示跟內容一樣。
    ///
    /// 與 zoom 分開:內容要的是「與原版逐像素對齊的點陣字」,選單只是介面,在高解析度
    /// 螢幕上大一級反而好認。
    pub menu_zoom: i32,
    /// Alt-P 的狀態:畫面底部留一塊,顯示游標所在檔案的開頭。
    pub show_preview: bool,
    prev: preview::Preview,

    /// 字級索引與最大值(由外層設定,因為有幾級取決於載到了幾種字型)。
    pub zoom: i32,
    pub max_zoom: i32,
    /// 放大倍率,以 0.1 為一階。zoom 換的是點陣字本身,scale 只是把同一份字模放大。
    pub scale: f64,
    /// 「請視窗層把視窗調成這麼多格」的請求。0 表示沒有請求,視窗層處理完要自己歸零。
    pub want_cols: i32,
    pub want_rows: i32,

    /// 最近一次繪製時內容區的列數,按鍵處理要用來算翻頁。
    rows: i32,
    /// 最近一次繪製時的欄數,縮圖列表算格位要用。
    thumb_cols: i32,
    /// 目前的介面語言(BCP 47 標籤)。空的表示還沒決定過,由外殼在啟動時填。
    pub lang: String,
    /// 目前選定的 24 點 CJK 字形來源代號。實際檔案載入由外殼完成。
    pub cjk24: String,
    /// 逐檔的位置記憶:開檔時查、每次按鍵之後寫。
    positions: BTreeMap<String, DocPos>,
    /// 拿時間用的,測試可以換掉。
    now: Option<fn() -> i64>,
    /// 最近一次 draw 拿到的整個畫面大小(含觸控功能列)。
    screen_cols: i32,
    screen_rows: i32,
    /// 選單列右側可以點的功能鍵,最近一次繪製選單列時算出。
    bar_keys: Vec<menu::BarKey>,
    prompt: prompt::Prompt,
    menu: menu::Menu,
    about: bool,
    /// markdown 檢視模式,見 mdview.rs。
    md: mdview::MdView,

    /// gopher 與 http 兩個協定的客戶端。None 表示用預設設定。網頁的行內圖片只在設了
    /// web 時才去取(Go 版的視窗程式沒有設,所以實際上不取;這裡照做)。
    pub gopher: Option<wincv_gopher::Client>,
    pub web: Option<wincv_web::Client>,
    /// 網路瀏覽模式,見 browse.rs。
    bv: browse::BrowseView,
    /// 還沒回來的網路取回。
    pending: Option<std::sync::mpsc::Receiver<(String, browse::Fetched)>>,
    /// 瀏覽模式正開著的一本 EPUB,與它的路徑。
    book: Option<wincv_epub::Book>,
    book_path: String,
    /// 瀏覽模式正開著的 PDF。用 Rc 是因為整頁圖的重畫函式也要拿著它。
    pdf: Option<std::rc::Rc<wincv_pdfdoc::Doc>>,
    pdf_path: String,
    /// 瀏覽模式正開著的 Office 文件。
    office: Option<wincv_officedoc::Doc>,
    office_path: String,

    /// 看圖是從瀏覽模式進來的,Esc 要退回去。
    g_return: bool,
    /// 看圖是從 markdown 進來的,Esc 要退回 markdown 而不是退回檔案清單。
    md_return: bool,
    edit_find: editfind::FindState,
    /// 「選單那一步」的暫存狀態:這兩個功能都要先問「做哪一種」再問細節,
    /// 而輸入列一次只能問一件事。
    find_kind_pending: bool,
    convert_pending: Option<Vec<OsString>>,

    /// 原始解碼文字,切換 ANSI 時要重新解析。
    view_raw: String,
    /// 目前檢視中的原始位元組,文字與 16 進位之間切換要用。
    view_data: Vec<u8>,

    stack: Vec<Layer>,
}

/// 一串字佔幾格。全形字兩格。
pub(crate) fn cell_width(s: &str) -> i32 {
    s.chars().map(|r| if is_wide(r) { 2 } else { 1 }).sum()
}

/// 掛上檔案列表的掛勾。在壓縮檔裡不掛:註解沒有真的目錄可讀,磁碟容量顯示宿主
/// 磁碟的剩餘空間會誤導。Go 版在掛勾裡問「現在是不是在壓縮檔裡」,這裡改成進出
/// 壓縮檔時換掛勾,效果相同。
fn set_hooks(b: &mut Browser, in_archive: bool) {
    b.color_of = Some(Box::new(filecolor::file_color));
    if in_archive {
        b.note_loader = None;
        b.disk_stat = None;
    } else {
        b.note_loader = Some(Box::new(shell::load_notes));
        b.disk_stat = Some(Box::new(shell::disk_stat));
    }
}

impl App {
    pub fn new(fs: Box<dyn Fs>, dir: &Path) -> App {
        let in_archive = fs.as_any().is_some_and(|a| a.is::<ArchiveFs>());
        let mut browser = Browser::new(fs, dir);
        set_hooks(&mut browser, in_archive);
        // new 時掛勾還沒接上,重讀一次才會有註解。
        let _ = browser.reload();
        App {
            browser,
            viewer: None,
            hex: None,
            image: None,
            editor: None,
            thumbs: None,
            thumbs_gen: 0,
            find: None,
            mode: Mode::Browser,
            syntax: None,
            dict_dir: PathBuf::new(),
            show_dict: false,
            dict: None,
            dict_err: String::new(),
            cell_w: 8,
            cell_h: 15,
            message: String::new(),
            quit: false,
            fullscreen: false,
            english_only: false,
            drive_focus: false,
            touch: false,
            menu_bar: true,
            menu_zoom: -1,
            show_preview: false,
            prev: Default::default(),
            zoom: 0,
            max_zoom: 0,
            scale: 1.0,
            want_cols: 0,
            want_rows: 0,
            rows: 0,
            thumb_cols: 0,
            lang: String::new(),
            cjk24: wincv_eten::cjk24::DEFAULT.to_string(),
            positions: BTreeMap::new(),
            now: None,
            screen_cols: 0,
            screen_rows: 0,
            bar_keys: Vec::new(),
            prompt: Default::default(),
            menu: Default::default(),
            about: false,
            md: Default::default(),
            gopher: None,
            web: None,
            bv: Default::default(),
            pending: None,
            book: None,
            book_path: String::new(),
            pdf: None,
            pdf_path: String::new(),
            office: None,
            office_path: String::new(),
            g_return: false,
            md_return: false,
            edit_find: Default::default(),
            find_kind_pending: false,
            convert_pending: None,
            view_raw: String::new(),
            view_data: Vec::new(),
            stack: Vec::new(),
        }
    }

    /// 載入語法上色設定。找不到就不上色,不算錯。
    pub fn load_syntax(&mut self, dir: &Path) {
        if let Ok(s) = wincv_syntax::Set::load(dir) {
            self.syntax = Some(s);
        }
    }

    /// 依目前模式畫出畫面。回傳的 overlay 要交給 `Rasterizer::draw_with`。
    /// 回傳的是一組而不是一個:markdown 一頁可以有好幾張圖。
    pub fn draw(&mut self, s: &mut Screen) -> Vec<Overlay> {
        self.screen_cols = s.cols;
        self.screen_rows = s.rows;
        // 觸控功能列佔掉底部幾列。與其讓每個模式各自知道「底下被佔了幾列」,不如先畫進
        // 一個矮幾列的畫面再貼上去:一次對所有模式成立,之後加新模式不必記得這件事。
        //
        // 選單列不在這裡:它可以用與內容不同的字型與大小,格點對不起來,所以由外殼用
        // menu_layer 拿去自己畫。
        if self.touch && s.rows > touch::TOUCH_ROWS + 2 {
            let mut inner = Screen::new(s.cols, s.rows - touch::TOUCH_ROWS);
            let ov = self.draw_modes(&mut inner);
            s.copy_from(&inner, 0, 0);
            self.draw_touch_bar(s);
            return ov;
        }
        self.draw_modes(s)
    }

    fn draw_modes(&mut self, s: &mut Screen) -> Vec<Overlay> {
        // 每一幀收一次網路結果。放在這裡是因為 app 這一層沒有自己的迴圈,而外殼會在
        // busy() 為真時持續重繪。
        self.browse_poll();
        if self.mode == Mode::Browse {
            self.browse_images();
        }
        let mut ov = Vec::new();
        match self.mode {
            Mode::Browse => ov = self.draw_browse(s),
            Mode::Markdown => ov = self.draw_markdown(s),
            Mode::Viewer => self.rows = self.viewer.as_mut().map_or(0, |v| v.draw(s)),
            Mode::Hex => self.rows = self.hex.as_mut().map_or(0, |h| h.draw(s)),
            Mode::Image => {
                if let Some(m) = &self.image {
                    ov.push(m.draw(s, self.cell_w, self.cell_h));
                }
                self.rows = s.rows - 1;
            }
            Mode::Edit => {
                self.rows = self.editor.as_mut().map_or(0, |e| e.draw(s));
                if self.show_dict {
                    self.draw_dict(s);
                }
            }
            Mode::Thumbs => {
                if let Some(o) = self.thumbs.as_mut().and_then(|t| t.draw(s, self.cell_w, self.cell_h)) {
                    ov.push(o);
                }
                self.rows = s.rows - 1;
                self.thumb_cols = s.cols;
            }
            Mode::Find => self.rows = self.draw_find(s),
            Mode::Browser => {
                self.rows = self.browser.draw(s);
                if self.show_preview {
                    self.draw_preview(s);
                }
            }
        }
        if self.about {
            self.draw_about(s);
        }
        if self.prompt.active {
            self.draw_prompt(s);
        } else if !self.message.is_empty() {
            let y = s.rows - 1;
            s.fill(0, y, s.cols, 1, ' ', Color::WHITE, Color::RED);
            s.print(0, y, &self.message, Color::WHITE, Color::RED);
        }
        ov
    }

    /// 分派一次按鍵。回傳是否有東西改變(需要重畫)。
    pub fn handle_key(&mut self, k: Key) -> bool {
        let changed = self.handle_key_inner(k);
        self.remember_pos();
        changed
    }

    /// 現在開著的文件在位置表裡的鍵(完整路徑),沒開文件回空字串。
    fn doc_key(&self) -> String {
        let name = match self.mode {
            Mode::Viewer => self.viewer.as_ref().map(|v| v.name.as_str()),
            Mode::Hex => self.hex.as_ref().map(|h| h.name.as_str()),
            Mode::Edit => self.editor.as_ref().map(|e| e.name.as_str()),
            Mode::Markdown => Some(self.md.name.as_str()),
            _ => None,
        }
        .unwrap_or("");
        if name.is_empty() || name == help::HELP_NAME {
            return String::new();
        }
        self.browser.dir.join(name).to_string_lossy().into_owned()
    }

    /// 把現在開著的文件的位置記進表裡。
    ///
    /// 每次按鍵之後都做:寫一個 map 很便宜,而「離開時才記」會漏掉被 kill 的那一次,
    /// 使用者失去的正是他讀到一半的那個位置。
    fn remember_pos(&mut self) {
        let key = self.doc_key();
        if key.is_empty() {
            return;
        }
        let mut p = DocPos { at: self.unix(), ..Default::default() };
        match self.mode {
            Mode::Viewer => {
                let v = self.viewer.as_ref().unwrap();
                (p.top, p.line) = (v.top, v.cur);
            }
            Mode::Hex => p.top = self.hex.as_ref().unwrap().top,
            Mode::Edit => {
                let e = self.editor.as_ref().unwrap();
                (p.top, p.line, p.col) = (e.top as i32, e.cur.line as i32, e.cur.col as i32);
            }
            Mode::Markdown => p.top = self.md.top,
            _ => {}
        }
        let mut st = wincv_session::State { positions: std::mem::take(&mut self.positions), ..Default::default() };
        st.remember(&key, p);
        self.positions = st.positions;
    }

    /// 開檔之後把上次的位置套回去。檔案內容可能變了,一律夾邊界。
    fn recall_pos(&mut self) {
        let Some(p) = self.positions.get(&self.doc_key()).copied() else { return };
        match self.mode {
            Mode::Viewer => {
                let v = self.viewer.as_mut().unwrap();
                let n = v.lines.len() as i32;
                v.top = session::clamp_top(p.top, n);
                v.cur = session::clamp_top(p.line, n);
            }
            Mode::Hex => {
                let h = self.hex.as_mut().unwrap();
                h.top = session::clamp_top(p.top, h.data.len() as i32 / 16 + 1);
            }
            Mode::Edit => {
                let e = self.editor.as_mut().unwrap();
                e.top = session::clamp_top(p.top, e.lines.len() as i32) as usize;
                e.move_to(wincv_editor::Pos { line: p.line.max(0) as usize, col: p.col.max(0) as usize });
            }
            Mode::Markdown => self.md.top = session::clamp_top(p.top, self.md.blocks.len() as i32 * 4),
            _ => {}
        }
    }

    fn unix(&self) -> i64 {
        match self.now {
            Some(f) => f(),
            None => std::time::SystemTime::now()
                .duration_since(std::time::UNIX_EPOCH)
                .map(|d| d.as_secs() as i64)
                .unwrap_or(0),
        }
    }

    fn handle_key_inner(&mut self, k: Key) -> bool {
        if self.prompt.active {
            // 這兩個是「先選一種再問細節」的兩段式流程,第一段不走一般的輸入列處理。
            if self.find_kind_pending {
                return self.find_kind_key(k);
            }
            if self.convert_pending.is_some() {
                return self.convert_key(k);
            }
            return self.prompt_key(k);
        }
        if self.about {
            return self.about_key(k);
        }
        if let Some(r) = self.menu_shortcut(k) {
            return r;
        }
        if self.menu.active {
            return self.menu_key(k);
        }
        self.message.clear();
        // 字級與縮放:Ctrl-+ / Ctrl-- / Ctrl-0。每個模式下都通。
        if k.ctrl && k.code == Code::Rune {
            match k.r {
                '+' | '=' => return self.set_zoom(self.zoom + 1),
                '-' | '_' => return self.set_zoom(self.zoom - 1),
                '0' => return self.set_zoom(0),
                _ => {}
            }
        }
        // 放大倍率:Alt-+ / Alt-- 每次 0.1,Alt-0 回到 1.0。
        if k.alt && k.code == Code::Rune {
            match k.r {
                '+' | '=' => return self.set_scale(self.scale + SCALE_STEP),
                '-' | '_' => return self.set_scale(self.scale - SCALE_STEP),
                '0' => return self.set_scale(1.0),
                _ => {}
            }
        }
        // Alt-X 離開。原版沒有離開鍵(Win32 程式關視窗就是離開),但重製版需要一條
        // 「程式自己知道要收尾」的路徑:位置就是在那裡記下的。
        if k.alt && k.code == Code::Rune && (k.r == 'x' || k.r == 'X') {
            return self.do_quit();
        }
        // F1 說明、F2 網路瀏覽、F9 選單、F8 中英文、F11 全螢幕在每個模式下都通。
        match k.code {
            Code::F1 => return self.open_help(),
            Code::F2 => return self.browse_ask(),
            Code::F9 => return self.open_menu(),
            Code::F8 if !k.ctrl => return self.toggle_cjk(),
            Code::F11 => return self.toggle_fullscreen(),
            _ => {}
        }
        match self.mode {
            Mode::Browse => self.browse_key(k),
            Mode::Markdown => self.markdown_key(k),
            Mode::Viewer => self.viewer_key(k),
            Mode::Hex => self.hex_key(k),
            Mode::Image => self.image_key(k),
            Mode::Edit => self.edit_key(k),
            Mode::Thumbs => self.thumbs_key(k),
            Mode::Find => self.find_key(k),
            _ => self.browser_key(k),
        }
    }

    /// 收尾離開。編輯中還有沒存的東西就先問一句(原版也問:image 裡有
    /// 「檔案已修改,要先存檔才離開嗎?」)。
    fn do_quit(&mut self) -> bool {
        if self.editor.as_ref().is_some_and(|e| e.dirty) {
            self.ask(&t("檔案已修改,要先存檔才離開嗎?(y/n)"), "y", |a, ans| {
                // 「是 / 否」是使用者打進來的字,兩邊都要翻才對得起來。
                let (yes, no) = (t("是"), t("否"));
                let s = ans.trim().to_lowercase();
                if s == "y" || s == "yes" || s == yes {
                    a.save_editor();
                    a.quit = true;
                } else if s == "n" || s == "no" || s == no {
                    a.quit = true;
                }
                // 其他答案:當作取消,留在原地。
            });
            return true;
        }
        self.quit = true;
        true
    }

    /// 換字級。回傳是否真的變了。
    fn set_zoom(&mut self, n: i32) -> bool {
        let n = n.max(0).min(self.max_zoom);
        if n == self.zoom {
            return false;
        }
        self.zoom = n;
        true
    }

    /// 在壓縮檔裡的話,回傳那個壓縮檔。
    fn archive(&self) -> Option<&ArchiveFs> {
        self.browser.fs.as_any().and_then(|a| a.downcast_ref::<ArchiveFs>())
    }

    /// 現在在壓縮檔裡,不能改。
    fn read_only_here(&self) -> bool {
        self.archive().is_some()
    }

    fn page_rows(&self) -> i32 {
        self.rows.max(1)
    }

    // --- 檔案瀏覽器 -----------------------------------------------------------

    fn browser_key(&mut self, k: Key) -> bool {
        let rows = self.page_rows();
        // 焦點在磁碟窗格時,方向鍵歸它。Tab 在兩邊之間切。
        if self.drive_focus {
            if self.drive_key(k) {
                return true;
            }
        } else if k.code == Code::Tab && self.browser.drive_pane > 0 {
            self.drive_focus = true;
            return true;
        }
        // Ctrl-← / Ctrl-→ 調主檔名欄的寬度。
        if k.ctrl && (k.code == Code::Left || k.code == Code::Right) {
            return self.browser.resize_name(if k.code == Code::Left { -1 } else { 1 });
        }
        let b = &mut self.browser;
        match k.code {
            Code::Up => {
                b.move_by(-1, rows);
                return true;
            }
            Code::Down => {
                b.move_by(1, rows);
                return true;
            }
            Code::PgUp => {
                b.move_by(-rows, rows);
                return true;
            }
            Code::PgDn => {
                b.move_by(rows, rows);
                return true;
            }
            Code::Home => {
                b.home(rows);
                return true;
            }
            Code::End => {
                b.end(rows);
                return true;
            }
            Code::Enter => return self.enter(),
            Code::Backspace => return self.go_parent(),
            // 徹底刪除:先填 0 再刪(0.5 版新增的選項)。
            Code::Delete => return self.start_delete(true),
            _ => {}
        }
        if k.code == Code::Rune && k.r == ' ' && !k.alt && !k.ctrl {
            self.browser.toggle_mark(rows);
            return true;
        }
        // 原版的 5 是「改變檔案列表的方式」(0.5 版 changelog),縮圖列表正是一種列表方式。
        if k.code == Code::Rune && k.r == '5' && !k.alt && !k.ctrl {
            return self.open_thumbs();
        }
        match k.letter() {
            Some('T') => {
                // T 只標檔案,Alt-T 連目錄一起標(0.5 版起的行為)。
                self.browser.mark_all(k.alt);
                return true;
            }
            Some('U') if !k.alt && !k.ctrl => {
                self.browser.unmark_all();
                return true;
            }
            Some('E') if k.alt => return self.start_note(),
            Some('E') if !k.ctrl => return self.open_editor(),
            Some('C') if k.alt => return self.compare_marked(),
            Some('C') => return self.start_transfer(false),
            Some('M') if !k.alt && !k.ctrl => return self.start_transfer(true),
            Some('R') if !k.alt && !k.ctrl => return self.start_rename(),
            Some('D') if k.alt => return self.toggle_drive_pane(),
            Some('D') if !k.ctrl => return self.start_delete(false),
            Some('Z') if k.alt => return self.start_create_archive(),
            Some('Z') if !k.ctrl => return self.start_extract(),
            Some('W') if !k.alt && !k.ctrl => return self.start_find(),
            Some('O') if k.ctrl => return self.start_convert(),
            Some('O') if !k.alt => return self.start_open(),
            Some('P') if k.alt => return self.toggle_preview(),
            Some('P') if !k.ctrl => return self.start_change_dir(),
            Some('G') if k.alt => return self.browse_ask(),
            Some('G') if !k.ctrl => return self.start_run(),
            Some('S') if !k.alt && !k.ctrl => {
                // 依序切換排序欄位。原版的 '5' 是「改變檔案列表的方式」,語意還沒實測清楚,
                // 先給一個明確可用的排序切換。
                let b = &mut self.browser;
                b.sort_key = match b.sort_key {
                    SortKey::Name => SortKey::Ext,
                    SortKey::Ext => SortKey::Size,
                    SortKey::Size => SortKey::Time,
                    SortKey::Time => SortKey::Name,
                };
                b.resort();
                self.message = t("排序:") + &sort_name(self.browser.sort_key);
                return true;
            }
            _ => {}
        }
        false
    }

    /// 進入目錄或開啟檔案。
    fn enter(&mut self) -> bool {
        let Some(e) = self.browser.current() else { return false };
        let (name, is_dir, up) = (e.name.clone(), e.is_dir, e.up);
        if is_dir {
            if up {
                return self.go_parent();
            }
            let dir = self.join_dir(&name);
            if let Err(err) = self.browser.load(&dir) {
                self.message = t("無法進入 ") + &dir.to_string_lossy() + ": " + &err.to_string();
            }
            return true;
        }
        // [雷] .epub、.docx/.pptx/.xlsx 要在壓縮檔判斷**之前**攔下來。它們就是 zip,不先攔
        // 的話按 Enter 會走進去看到 META-INF 與一堆 xml,而那不是任何人想看一份文件時要的
        // 東西。
        if bookview::is_book(&name) && !self.read_only_here() {
            return self.open_book(&name);
        }
        if pdfview::is_pdf(&name) && !self.read_only_here() {
            return self.open_pdf(&name);
        }
        if docview::is_office_doc(&name) && !self.read_only_here() {
            return self.open_office(&name);
        }
        if wincv_archive::is_archive(Path::new(&name)) {
            return self.enter_archive(&name);
        }
        if wincv_imgfmt::is_image(&name) {
            return self.open_image(&name);
        }
        self.open_viewer(&name)
    }

    /// 目前目錄裡某個名字的原始檔名(開檔與組路徑用)。顯示名稱不一定能還原成原始
    /// 位元組(壓縮檔裡的 Big5 檔名),所以從列表裡找。
    fn os_name(&self, name: &str) -> OsString {
        self.browser
            .entries
            .iter()
            .find(|e| e.name == name)
            .map(|e| e.os_name.clone())
            .unwrap_or_else(|| OsString::from(name))
    }

    /// 接出子目錄或檔案的路徑。壓縮檔內用 / 分隔,不能用 PathBuf::join
    /// (Windows 上會變成反斜線,而壓縮檔裡的路徑一律是斜線)。
    fn join_dir(&self, name: &str) -> PathBuf {
        let os = self.os_name(name);
        if let Some(af) = self.archive() {
            let mut inner = af.inner(&self.browser.dir);
            if !inner.is_empty() {
                inner.push(b'/');
            }
            inner.extend_from_slice(&wincv_archive::path_bytes(Path::new(&os)));
            return af.path(&inner);
        }
        self.browser.dir.join(os)
    }

    /// 把壓縮檔當目錄進去。
    fn enter_archive(&mut self, name: &str) -> bool {
        let full = self.browser.dir.join(self.os_name(name));
        let af = match ArchiveFs::open(&full) {
            Ok(af) => af,
            Err(e) => {
                self.message = e.to_string();
                return true;
            }
        };
        let root = af.root();
        let old = std::mem::replace(&mut self.browser.fs, Box::new(af));
        self.stack.push(Layer { fs: old, dir: self.browser.dir.clone(), cursor: self.browser.cursor });
        set_hooks(&mut self.browser, true);
        if let Err(e) = self.browser.load(&root) {
            self.message = e.to_string();
            self.pop_layer();
        }
        true
    }

    fn pop_layer(&mut self) -> bool {
        let Some(l) = self.stack.pop() else { return false };
        self.browser.fs = l.fs;
        let in_archive = self.read_only_here();
        set_hooks(&mut self.browser, in_archive);
        if let Err(e) = self.browser.load(&l.dir) {
            self.message = e.to_string();
            return true;
        }
        self.browser.move_to(l.cursor, self.rows);
        true
    }

    fn go_parent(&mut self) -> bool {
        // 在壓縮檔最上層再往上,就是離開這個壓縮檔。
        if let Some(af) = self.archive() {
            if af.is_root(&self.browser.dir) {
                return self.pop_layer();
            }
            let inner = af.inner(&self.browser.dir);
            let up = wincv_archive::go_path_dir(&inner);
            let up = if up == b"." { Vec::new() } else { up };
            let prev = inner.rsplit(|&c| c == b'/').next().unwrap_or(&inner).to_vec();
            let target = af.path(&up);
            if let Err(e) = self.browser.load(&target) {
                self.message = e.to_string();
                return true;
            }
            let prev = wincv_vfs::display_name(&wincv_archive::bytes_os(&prev));
            self.focus_on(&prev);
            return true;
        }
        let p = wincv_vfs::parent(&self.browser.dir);
        if p == self.browser.dir {
            return false;
        }
        let prev = self.browser.dir.file_name().map(wincv_vfs::display_name).unwrap_or_default();
        if let Err(e) = self.browser.load(&p) {
            self.message = t("無法回上一層: ") + &e.to_string();
            return true;
        }
        self.focus_on(&prev);
        true
    }

    /// 把游標移到指定名稱那一筆。回上一層時游標停在剛才離開的目錄上:一路往回走時
    /// 每次都跳回第一筆會很難用。
    fn focus_on(&mut self, name: &str) {
        if let Some(i) = self.browser.entries.iter().position(|e| e.name == name) {
            self.browser.move_to(i as i32, self.rows);
        }
    }

    /// 讀目前目錄下的一個檔案(壓縮檔內也適用)。
    fn read_current(&self, name: &str) -> Result<Vec<u8>, String> {
        let mut rc = self.browser.fs.open(&self.join_dir(name)).map_err(|e| tr!("開不了 %s: %w", name, e.to_string()))?;
        let mut data = Vec::new();
        (&mut rc).take(MAX_VIEW_BYTES).read_to_end(&mut data).map_err(|e| tr!("讀取失敗: %w", e.to_string()))?;
        Ok(data)
    }

    fn open_viewer(&mut self, name: &str) -> bool {
        let data = match self.read_current(name) {
            Ok(d) => d,
            Err(e) => {
                self.message = e;
                return true;
            }
        };
        // markdown 走排版模式:標題、清單、表格照結構畫,圖片直接嵌在文件裡。要看原始碼按 E 進編輯器。
        if is_markdown(name) {
            self.open_markdown(name, &data);
            return true;
        }
        // 判成二進位就直接開 16 進位檢視:原版 0.5 版起就是這個行為
        // (「按 enter 看檔時自動將可能為執行檔的檔案以 16 進位方式看檔」)。
        let m = wincv_viewer::Model::load(name, &data, Enc::Unknown);
        if m.enc == Enc::Binary {
            self.open_hex(name, data);
            return true;
        }
        self.view_raw = wincv_textenc::decode(&data, m.enc);
        self.view_data = data;
        self.viewer = Some(m);
        self.mode = Mode::Viewer;
        self.recall_pos();
        true
    }

    fn open_image(&mut self, name: &str) -> bool {
        let data = match self.read_current(name) {
            Ok(d) => d,
            Err(e) => {
                self.message = e;
                return true;
            }
        };
        match wincv_imgview::Model::load(name, &data) {
            Ok(m) => {
                self.image = Some(m);
                self.mode = Mode::Image;
            }
            Err(e) => {
                // 解不開就退回 16 進位:至少看得到內容,而不是什麼都沒有。
                self.message = e;
                self.open_hex(name, data);
            }
        }
        true
    }

    /// 目前目錄裡所有圖檔的索引,用於看上一張/下一張。
    fn image_neighbours(&self) -> Vec<usize> {
        let es = &self.browser.entries;
        (0..es.len()).filter(|&i| !es[i].is_dir && wincv_imgfmt::is_image(&es[i].name)).collect()
    }

    /// 換到上一張或下一張圖(原版 Enter / BackSpace)。
    fn step_image(&mut self, d: i32) -> bool {
        let idx = self.image_neighbours();
        if idx.is_empty() {
            return false;
        }
        let cur = idx.iter().position(|&v| v as i32 == self.browser.cursor).unwrap_or(0) as i32;
        let n = idx.len() as i32;
        let next = ((cur + d) % n + n) % n;
        self.browser.move_to(idx[next as usize] as i32, self.rows);
        let Some(name) = self.browser.current().map(|e| e.name.clone()) else { return false };
        self.open_image(&name)
    }

    fn image_key(&mut self, k: Key) -> bool {
        // PDF 的整頁圖:上/下一「張」就是上/下一頁(見 pdf_page_step)。
        if self.in_pdf_page() {
            match k.code {
                Code::Enter | Code::PgDn => return self.pdf_page_step(1),
                Code::Backspace | Code::PgUp => return self.pdf_page_step(-1),
                _ => {}
            }
        }
        match k.code {
            Code::Enter | Code::PgDn => return self.step_image(1),
            Code::Backspace | Code::PgUp => return self.step_image(-1),
            Code::Esc => {
                // 從瀏覽模式進來的就退回瀏覽模式;從 markdown 進來的就退回 markdown。
                if self.g_return {
                    self.g_return = false;
                    self.mode = Mode::Browse;
                    return true;
                }
                if self.md_return {
                    self.md_return = false;
                    self.mode = Mode::Markdown;
                    return true;
                }
                self.mode = Mode::Browser;
                return true;
            }
            _ => {}
        }
        let Some(m) = self.image.as_mut() else { return false };
        match k.code {
            Code::Up => {
                m.pan_by(0, -32);
                return true;
            }
            Code::Down => {
                m.pan_by(0, 32);
                return true;
            }
            Code::Left => {
                m.pan_by(-32, 0);
                return true;
            }
            Code::Right => {
                m.pan_by(32, 0);
                return true;
            }
            _ => {}
        }
        // 放大 / 縮小 / 回到原尺寸。裸鍵沒有歧義:看圖模式沒有文字輸入,而帶修飾鍵的
        // Ctrl-+ 與 Alt-+ 已經是字級與整個畫面的放大倍率。
        if k.code == Code::Rune && !k.ctrl && !k.alt {
            match k.r {
                '+' | '=' => return m.zoom_by(1),
                '-' | '_' => return m.zoom_by(-1),
                '1' => return m.set_zoom(1.0),
                _ => {}
            }
        }
        if k.code == Code::Rune && k.r == ' ' {
            // 標記此檔,換看下一張(原版行為)。
            if let Some(e) = self.browser.current_mut().filter(|e| !e.up) {
                e.marked = !e.marked;
            }
            return self.step_image(1);
        }
        // `;` 或 Alt-E 加註解(原版 0xd6908「; 或 Alt-E 註解」)。
        if k.code == Code::Rune && k.r == ';' && !k.ctrl {
            return self.start_note();
        }
        match k.letter() {
            Some('I') => {
                m.info = !m.info;
                true
            }
            Some('F') => {
                m.toggle_fit();
                true
            }
            Some('E') if k.alt => self.start_note(),
            _ => false,
        }
    }

    fn open_hex(&mut self, name: &str, data: Vec<u8>) {
        self.view_data = data.clone();
        self.hex = Some(wincv_hexview::Model::load(name, data));
        self.mode = Mode::Hex;
        self.recall_pos();
    }

    // --- 檔案操作 -------------------------------------------------------------

    /// 這次操作要處理哪些檔案:有標記就用標記的,沒有就用游標所在的那一個。
    fn targets(&self) -> Vec<(String, OsString)> {
        let mut out: Vec<_> =
            self.browser.entries.iter().filter(|e| e.marked && !e.up).map(|e| (e.name.clone(), e.os_name.clone())).collect();
        if out.is_empty() {
            if let Some(e) = self.browser.current().filter(|e| !e.up) {
                out.push((e.name.clone(), e.os_name.clone()));
            }
        }
        out
    }

    /// 開始拷貝(mv=false)或移動(mv=true)。
    fn start_transfer(&mut self, mv: bool) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案還不能拷貝或移動");
            return true;
        }
        let names: Vec<OsString> = self.targets().into_iter().map(|t| t.1).collect();
        if names.is_empty() {
            return false;
        }
        let verb = if mv { t("移動") } else { t("拷貝") };
        let title = tr!("%s %d 個檔案到:", verb, names.len());
        let dir = self.browser.dir.to_string_lossy().into_owned();
        self.ask(&title, &dir, move |a, dst| a.run_transfer(mv, names, &dst));
        true
    }

    fn run_transfer(&mut self, mv: bool, names: Vec<OsString>, dst: &str) {
        if dst.is_empty() {
            return;
        }
        let dst = PathBuf::from(dst);
        if let Err(e) = std::fs::create_dir_all(&dst) {
            self.message = t("目的目錄建不起來: ") + &e.to_string();
            return;
        }
        // 覆蓋詢問走同一個輸入列。輸入列是非同步的(要等使用者按鍵),所以先用
        // 「不覆蓋」跑一遍,把撞名的挑出來再一次問。
        let src = self.browser.dir.clone();
        let run = |names: &[OsString], ow| {
            let o = wincv_fileop::Options { overwrite: ow, ..Default::default() };
            if mv {
                wincv_fileop::move_to(&src, &dst, names, o)
            } else {
                wincv_fileop::copy(&src, &dst, names, o)
            }
        };
        let res = run(&names, wincv_fileop::Overwrite::Skip);
        let verb = if mv { t("已移動") } else { t("已拷貝") };
        if !res.skipped.is_empty() {
            let skipped = res.skipped.clone();
            let done = res.summary(&verb);
            self.confirm(&tr!("%d 個檔案已存在,要覆蓋嗎?", skipped.len()), false, move |a, yes, _| {
                if !yes {
                    a.message = done;
                    let _ = a.browser.reload();
                    return;
                }
                let o = wincv_fileop::Options { overwrite: wincv_fileop::Overwrite::All, ..Default::default() };
                let r2 = if mv {
                    wincv_fileop::move_to(&a.browser.dir, &dst, &skipped, o)
                } else {
                    wincv_fileop::copy(&a.browser.dir, &dst, &skipped, o)
                };
                a.message = done + &t(";覆蓋 ") + &r2.summary(&verb);
                let _ = a.browser.reload();
            });
            return;
        }
        self.message = res.summary(&verb);
        let _ = self.browser.reload();
    }

    fn start_rename(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案還不能改名");
            return true;
        }
        let Some(e) = self.browser.current().filter(|e| !e.up) else { return false };
        let (old, old_os) = (e.name.clone(), e.os_name.clone());
        self.ask(&t("改名為:"), &old.clone(), move |a, to| {
            if to.is_empty() || to == old {
                return;
            }
            if let Err(e) = wincv_fileop::rename(&a.browser.dir, &old_os, &to) {
                a.message = t("改名失敗: ") + &e.to_string();
                return;
            }
            let _ = a.browser.reload();
            a.focus_on(&to);
            a.message = format!("{old} → {to}");
        });
        true
    }

    /// 刪除。zero 為 true 時先把內容填 0 再刪。
    fn start_delete(&mut self, zero: bool) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案還不能刪除");
            return true;
        }
        let ts = self.targets();
        if ts.is_empty() {
            return false;
        }
        let mut what =
            if zero { tr!("徹底刪除 %d 個檔案(先填 0)?", ts.len()) } else { tr!("刪除 %d 個檔案?", ts.len()) };
        if ts.len() == 1 {
            what = if zero { t("徹底刪除 ") + &ts[0].0 + &t("(先填 0)?") } else { t("刪除 ") + &ts[0].0 + "?" };
        }
        let names: Vec<OsString> = ts.into_iter().map(|t| t.1).collect();
        self.confirm(&what, false, move |a, yes, _| {
            if !yes {
                return;
            }
            let res =
                wincv_fileop::delete(&a.browser.dir, &names, wincv_fileop::Options { zero_fill: zero, ..Default::default() });
            a.message = res.summary(&t("已刪除"));
            let _ = a.browser.reload();
        });
        true
    }

    /// 比對標記的兩個檔案(原版 Alt-C)。
    fn compare_marked(&mut self) -> bool {
        let names: Vec<(String, OsString)> =
            self.browser.entries.iter().filter(|e| e.marked && !e.is_dir).map(|e| (e.name.clone(), e.os_name.clone())).collect();
        if names.len() != 2 {
            self.message = t("請先標記剛好兩個檔案");
            return true;
        }
        let d = &self.browser.dir;
        self.message = match wincv_fileop::compare(&d.join(&names[0].1), &d.join(&names[1].1)) {
            Err(e) => t("比對失敗: ") + &e.to_string(),
            Ok((true, _)) => names[0].0.clone() + &t(" 與 ") + &names[1].0 + &t(" 內容相同"),
            Ok((false, at)) => tr!("內容不同,第一個相異位移 0x%X", at.unwrap_or(0)),
        };
        true
    }

    // --- 縮圖列表 -------------------------------------------------------------

    /// 把目前目錄的圖檔排成縮圖列表。
    fn open_thumbs(&mut self) -> bool {
        let names: Vec<String> =
            self.browser.entries.iter().filter(|e| !e.is_dir && wincv_imgfmt::is_image(&e.name)).map(|e| e.name.clone()).collect();
        if names.is_empty() {
            self.message = t("這個目錄沒有圖檔");
            return true;
        }
        self.thumbs = Some(wincv_thumbs::Model::new(&names));
        self.thumbs_gen += 1;
        self.mode = Mode::Thumbs;
        true
    }

    /// 背景解縮圖的工作:畫面上看得到、還沒送出的那些,連同檔案內容與代次。
    ///
    /// 讀檔在呼叫端的執行緒做(檔案系統不一定能跨執行緒,壓縮檔裡的檔案要經過它),
    /// 解碼才是慢的那一段,交給別的執行緒。解好用 thumb_done 放回來。
    pub fn thumb_jobs(&mut self, cols: i32, rows: i32) -> Vec<(u64, usize, String, Result<Vec<u8>, String>)> {
        let generation = self.thumbs_gen;
        let Some(mut t) = self.thumbs.take() else { return Vec::new() };
        let jobs = t
            .take_visible(cols, rows)
            .into_iter()
            .map(|i| {
                let name = t.items[i].name.clone();
                let data = self.read_current(&name);
                (generation, i, name, data)
            })
            .collect();
        self.thumbs = Some(t);
        jobs
    }

    /// 放回背景解好的一張縮圖。代次對不上(列表已經換了)就丟掉。
    pub fn thumb_done(&mut self, generation: u64, i: usize, it: wincv_thumbs::Item) {
        if generation != self.thumbs_gen {
            return;
        }
        if let Some(t) = self.thumbs.as_mut() {
            t.set_decoded(i, it);
        }
    }

    /// 解目前看得到的縮圖。呼叫端應該在背景做:一個目錄幾十張 JPEG 解起來要好幾秒,
    /// 卡著等會像當掉。
    pub fn decode_thumbs(&mut self, cols: i32, rows: i32) {
        let Some(mut t) = self.thumbs.take() else { return };
        t.decode_visible(cols, rows, &|n| self.read_current(n));
        self.thumbs = Some(t);
    }

    fn thumbs_key(&mut self, k: Key) -> bool {
        let cols = if self.thumb_cols <= 0 { 80 } else { self.thumb_cols };
        let rows = if self.rows <= 0 { 24 } else { self.rows };
        let Some(t) = self.thumbs.as_mut() else { return false };
        match k.code {
            Code::Left => t.move_by(-1, 0, cols, rows),
            Code::Right => t.move_by(1, 0, cols, rows),
            Code::Up => t.move_by(0, -1, cols, rows),
            Code::Down => t.move_by(0, 1, cols, rows),
            Code::Enter => {
                let Some(name) = t.current().map(|it| it.name.clone()) else { return false };
                // 游標也跟著移到那個檔案上,離開看圖時才接得回來。
                self.focus_on(&name);
                return self.open_image(&name);
            }
            Code::Esc => self.mode = Mode::Browser,
            _ => return false,
        }
        true
    }

    // --- 字典視窗 -------------------------------------------------------------

    /// 游標所在的英文單字。
    fn word_under_cursor(&self) -> String {
        let Some(e) = &self.editor else { return String::new() };
        let Some(l) = e.lines.get(e.cur.line) else { return String::new() };
        if l.is_empty() {
            return String::new();
        }
        let i = e.cur.col.min(l.len() - 1);
        if !is_word_rune(l[i]) {
            return String::new();
        }
        let mut from = i;
        while from > 0 && is_word_rune(l[from - 1]) {
            from -= 1;
        }
        let mut to = i;
        while to < l.len() && is_word_rune(l[to]) {
            to += 1;
        }
        l[from..to].iter().collect()
    }

    /// 在畫面底部畫字典視窗。
    fn draw_dict(&mut self, s: &mut Screen) {
        if self.dict.is_none() && self.dict_err.is_empty() {
            match wincv_dict::Dict::load(&self.dict_dir) {
                Ok(d) => self.dict = Some(d),
                Err(e) => self.dict_err = t("載不到字典: ") + &e.to_string(),
            }
        }
        let top = s.rows - 1 - DICT_PANEL_ROWS;
        if top < 0 {
            return;
        }
        s.fill(0, top, s.cols, DICT_PANEL_ROWS, ' ', Color::LT_CYAN, Color::BLUE);
        if !self.dict_err.is_empty() {
            s.print(1, top, &self.dict_err, Color::LT_YELLOW, Color::BLUE);
            return;
        }
        let w = self.word_under_cursor();
        if w.is_empty() {
            s.print(1, top, &t("字典:把游標移到英文單字上"), Color::LT_GRAY, Color::BLUE);
            return;
        }
        let Some(e) = self.dict.as_ref().and_then(|d| d.lookup(&w)) else {
            s.print(1, top, &(w + &t(" — 查無此字")), Color::LT_GRAY, Color::BLUE);
            return;
        };
        let mut head = e.word.clone();
        if !e.kk.is_empty() {
            head += &format!("  [{}]", e.kk);
        }
        if !e.base.is_empty() && e.base != e.word {
            head += &(t("  (原形 ") + &e.base + ")");
        }
        s.print(1, top, &head, Color::LT_YELLOW, Color::BLUE);
        // 解釋可能很長,換行塞進剩下的幾列。
        let mut y = top + 1;
        for line in wrap_cells(&e.trans, s.cols - 2) {
            if y >= s.rows - 1 {
                break;
            }
            s.print(1, y, &line, Color::LT_CYAN, Color::BLUE);
            y += 1;
        }
    }

    // --- 文字編輯器 -----------------------------------------------------------

    /// 用編輯器開游標所在的檔案(原版主畫面的 E)。
    fn open_editor(&mut self) -> bool {
        let Some(e) = self.browser.current().filter(|e| !e.is_dir) else { return false };
        let name = e.name.clone();
        let data = match self.read_current(&name) {
            Ok(d) => d,
            Err(e) => {
                self.message = e;
                return true;
            }
        };
        let syn = self.syntax.as_ref().and_then(|s| s.for_name(&name));
        self.editor = Some(wincv_editor::Model::load(&name, &data, Enc::Unknown, syn));
        self.mode = Mode::Edit;
        self.recall_pos();
        true
    }

    /// 把編輯中的內容寫回檔案。壓縮檔內的檔案不能存:那需要重建整個壓縮檔,是另一件事。
    pub fn save_editor(&mut self) -> bool {
        let Some(e) = &self.editor else { return false };
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案還不能直接存回去");
            return true;
        }
        let path = self.browser.dir.join(self.os_name(&e.name));
        if let Err(err) = std::fs::write(&path, e.bytes()) {
            self.message = t("存檔失敗: ") + &err.to_string();
            return true;
        }
        let e = self.editor.as_mut().unwrap();
        e.dirty = false;
        self.message = t("已存檔 ") + &e.name;
        true
    }

    fn edit_key(&mut self, k: Key) -> bool {
        let rows = self.page_rows() as isize;
        let Some(e) = self.editor.as_mut() else { return false };
        match k.code {
            Code::Up => e.move_by(-1, 0),
            Code::Down => e.move_by(1, 0),
            Code::Left => e.move_by(0, -1),
            Code::Right => e.move_by(0, 1),
            Code::PgUp => e.move_by(-rows, 0),
            Code::PgDn => e.move_by(rows, 0),
            Code::Home => e.cur.col = 0,
            Code::End => e.cur.col = e.lines[e.cur.line].len(),
            Code::Enter => e.new_line(),
            Code::Backspace => e.backspace(),
            Code::Delete => e.delete(),
            Code::Insert => e.insert = !e.insert,
            Code::Tab => e.insert_char('\t'),
            Code::Esc => self.mode = Mode::Browser,
            Code::F6 => return self.start_edit_find(),
            // 字典視窗。原版是設定項(「顯示字典視窗?」),沒找到對應的快捷鍵,先給 F7。
            Code::F7 => self.show_dict = !self.show_dict,
            _ => return self.edit_key_letters(k),
        }
        true
    }

    fn edit_key_letters(&mut self, k: Key) -> bool {
        use wincv_editor::BlockKind;
        let e = self.editor.as_mut().unwrap();
        if k.alt {
            match k.letter() {
                Some('B') => e.mark_block(BlockKind::Rect),
                Some('L') => e.mark_block(BlockKind::Line),
                Some('U') => e.unmark_block(),
                Some('Z') => {
                    if e.copy_block() {
                        e.unmark_block();
                        e.paste_block();
                    }
                }
                Some('M') => {
                    e.move_block();
                }
                Some('D') => {
                    e.delete_block();
                }
                Some('F') => {
                    e.fill_block(' ', true);
                }
                Some('E') => return self.edit_comment(),
                _ => return false,
            }
            return true;
        }
        if k.ctrl {
            match k.letter() {
                Some('C') => {
                    e.copy_block();
                }
                Some('X') => {
                    if e.copy_block() {
                        e.delete_block();
                    }
                }
                Some('V') => {
                    e.paste_block();
                }
                Some('U') => {
                    e.undo();
                }
                Some('S') => return self.save_editor(),
                // 原版 0x8838c「Ctrl-E ; 標記轉註解」。
                Some('E') => return self.edit_comment(),
                // 原版 0xe7e97「續找」。
                Some('N') => self.edit_find_next(false),
                _ => return false,
            }
            return true;
        }
        // 一般字元輸入。控制鍵在上面已經處理掉了。
        if k.code == Code::Rune && k.r as u32 >= 0x20 {
            e.insert_char(k.r);
            return true;
        }
        false
    }

    // --- 16 進位檢視 ----------------------------------------------------------

    fn hex_key(&mut self, k: Key) -> bool {
        let rows = self.page_rows();
        let has_viewer = self.viewer.is_some();
        let Some(h) = self.hex.as_mut() else { return false };
        match k.code {
            Code::Up => h.scroll_by(-1),
            Code::Down => h.scroll_by(1),
            Code::PgUp => h.scroll_by(-rows),
            Code::PgDn => h.scroll_by(rows),
            Code::Home => h.home(),
            Code::End => h.end(rows),
            Code::Esc | Code::Enter | Code::Backspace => self.mode = Mode::Browser,
            _ => match k.letter() {
                // 從 16 進位切回文字:只有本來就是文字檔才切得回去。
                Some('H') if has_viewer => self.mode = Mode::Viewer,
                Some('F') if !k.ctrl && !k.alt => h.big5 = !h.big5,
                _ => return false,
            },
        }
        true
    }

    // --- 文字檢視器 -----------------------------------------------------------

    fn viewer_key(&mut self, k: Key) -> bool {
        let rows = self.page_rows();
        let Some(v) = self.viewer.as_mut() else { return false };
        // 方向鍵移游標,畫面跟著游標走。原版的工具列本來就在報游標在第幾行
        // (「1 字 1 行/ 626」),只是它沒有把那一列畫出來。
        match k.code {
            Code::Up => v.move_by(-1, rows),
            Code::Down => v.move_by(1, rows),
            Code::PgUp => v.page_by(-1, rows),
            Code::PgDn => v.page_by(1, rows),
            Code::Home => v.home(),
            Code::End => v.end(rows),
            Code::Left if !v.wrap && v.left > 0 => v.left = (v.left - 8).max(0),
            Code::Right if !v.wrap => v.left += 8,
            // Esc 是回上一層,不是離開程式。
            Code::Esc | Code::Enter | Code::Backspace => self.mode = Mode::Browser,
            _ => match k.letter() {
                Some('W') if !k.ctrl && !k.alt => {
                    v.wrap = !v.wrap;
                    v.left = 0;
                }
                Some('A') if !k.ctrl && !k.alt => {
                    let on = !v.ansi;
                    v.set_ansi(on, &self.view_raw);
                }
                Some('N') if k.ctrl => v.next_hit(rows),
                // H 切到 16 進位(image 內的標籤是「&Hex模式」)。
                Some('H') if !k.ctrl && !k.alt => {
                    let name = v.name.clone();
                    let data = self.view_data.clone();
                    self.open_hex(&name, data);
                }
                // 光棒開關。ANSI 彩色簽名檔的底色本身有意義,蓋掉會失真,所以要留一個
                // 關得掉的辦法。
                Some('L') if !k.ctrl && !k.alt => v.bar = !v.bar,
                _ => return false,
            },
        }
        true
    }
}

fn sort_name(k: SortKey) -> String {
    match k {
        SortKey::Ext => t("副檔名"),
        SortKey::Size => t("大小"),
        SortKey::Time => t("時間"),
        SortKey::Name => t("檔名"),
    }
}

fn is_word_rune(r: char) -> bool {
    r == '\'' || r == '-' || r.is_ascii_alphabetic()
}

/// 依顯示格數折行,不把全形字切一半。
fn wrap_cells(s: &str, width: i32) -> Vec<String> {
    if width <= 0 {
        return Vec::new();
    }
    let (mut out, mut cur, mut n) = (Vec::new(), String::new(), 0);
    for r in s.chars() {
        let w = if is_wide(r) { 2 } else { 1 };
        if n + w > width {
            out.push(std::mem::take(&mut cur));
            n = 0;
        }
        cur.push(r);
        n += w;
    }
    if !cur.is_empty() {
        out.push(cur);
    }
    out
}
