//! 網路瀏覽模式:gopher 與 http 共用的畫面(對應 Go 版 `browse.go`)。
//!
//! gopher 與 http 共用同一份狀態、同一個畫面、同一組按鍵。差別只在「怎麼取回」與
//! 「怎麼變成區塊」兩件事,那兩件事各自關在 `wincv-gopher` 與 `wincv-web` 裡:這一層
//! 看到的都是「一串區塊加一堆連結」。本機的書(EPUB)也借用這個畫面。

use std::collections::HashMap;
use std::sync::mpsc::TryRecvError;
use std::sync::Arc;

use wincv_cell::{Color, Screen};
use wincv_i18n::t;
use wincv_keys::{Code, Key};
use wincv_markdown::{Block, Kind, Line};
use wincv_render::{Image, Overlay};

use crate::bookview::EPUB_SCHEME;
use crate::docview::DOC_SCHEME;
use crate::pdfview::PDF_SCHEME;
use crate::{cell_width, App, Mode};

/// 排版之後畫面上的一個可點目標。記的是「第幾列」而不是「第幾個區塊」:一個項目在窄畫面上
/// 會折成好幾列,而游標要停在整個項目上,捲動也要以列為單位。
pub(crate) struct BrowseLink {
    pub href: String,
    /// 起始列。
    pub line: i32,
    /// 佔幾列。
    pub rows: i32,
}

/// 網路瀏覽模式的狀態。
pub(crate) struct BrowseView {
    /// 目前這一頁的完整位址(含 scheme)。
    pub url: String,
    pub title: String,
    pub blocks: Vec<Block>,
    pub lines: Option<Vec<Line>>,
    pub links: Vec<BrowseLink>,
    /// links 的索引;沒有連結時是 -1。
    pub cur: i32,
    pub top: i32,
    pub cols: i32,
    /// 上一頁的堆疊。只存位址,回上一頁時重抓:內容不大,而快取整棵樹會讓「重新整理」
    /// 變得沒有意義。
    pub hist: Vec<String>,
    /// 狀態列要顯示的東西(連線中、錯誤)。空的表示正常。
    pub status: String,
    /// 這一頁已經取回的圖片,以位址為鍵;值是 None 表示取過但沒取到。排版引擎是同步的,
    /// 而取圖是非同步的:先排一次(圖片只有 alt),圖回來了再排一次。
    pub imgs: HashMap<String, Option<Arc<Image>>>,
    /// 還沒取回的圖片位址。
    pub want: Vec<String>,
}

impl Default for BrowseView {
    fn default() -> BrowseView {
        BrowseView {
            url: String::new(),
            title: String::new(),
            blocks: Vec::new(),
            lines: None,
            links: Vec::new(),
            cur: -1,
            top: 0,
            cols: 0,
            hist: Vec::new(),
            status: String::new(),
            imgs: HashMap::new(),
            want: Vec::new(),
        }
    }
}

/// 一次取回的結果,由背景執行緒送回來。
pub(crate) enum Fetched {
    Web(Result<wincv_web::Doc, wincv_web::Error>),
    Gopher(wincv_gopher::Url, Result<Vec<u8>, wincv_gopher::FetchError>),
    /// 這一頁裡的一張內嵌圖,不是新的一頁。
    Img(Result<Vec<u8>, wincv_web::Error>),
}

/// 把使用者打的東西變成一個完整位址。沒寫協定時當 gopher:這個模式的起點是 gopher,
/// 而 http 的位址幾乎都是從別處複製貼上來的,自然會帶著 scheme。
pub(crate) fn normalize_url(raw: &str) -> Result<String, String> {
    let raw = raw.trim();
    if raw.is_empty() {
        return Err(t("位址是空的"));
    }
    if wincv_web::is_http(raw) || raw.starts_with(EPUB_SCHEME) || raw.starts_with(PDF_SCHEME) || raw.starts_with(DOC_SCHEME) {
        return Ok(raw.to_string());
    }
    wincv_gopher::parse_url(raw).map(|u| u.to_string())
}

/// 從位址取主機名,給狀態列用。取不出來就原樣顯示。
fn host_of(raw: &str) -> String {
    let mut s = raw;
    if let Some(i) = s.find("://") {
        s = &s[i + 3..];
    }
    if let Some(i) = s.find(['/', '?']) {
        s = &s[..i];
    }
    if s.is_empty() {
        raw.to_string()
    } else {
        s.to_string()
    }
}

/// 從位址取檔名:imgfmt 靠副檔名選解碼器。
pub(crate) fn img_name_of(raw: &str) -> &str {
    let s = raw.find(['?', '#']).map_or(raw, |i| &raw[..i]);
    s.rfind('/').map_or(s, |i| &s[i + 1..])
}

/// 從 gopher 位址取一個像檔名的東西,給看圖模式的標題用。
fn browse_name(u: &wincv_gopher::Url) -> String {
    let sel = wincv_textenc::utf8_lossy_go(&u.sel);
    let s = sel.rsplit('/').next().unwrap_or(&sel);
    if s.is_empty() {
        u.host.clone()
    } else {
        s.to_string()
    }
}

impl App {
    /// 有沒有還沒完成的網路取回。外殼要在這時持續重繪:取回是非同步的,不持續重繪的話
    /// 結果回來了畫面也不動,使用者看到的是永遠停在「連線中」。
    pub fn busy(&self) -> bool {
        self.pending.is_some()
    }

    /// 開一個位址。gopher:// 與 http(s):// 都吃,沒寫協定時當 gopher。
    pub fn open_url(&mut self, raw: &str) -> bool {
        if let Err(e) = normalize_url(raw) {
            self.message = t("位址解不開:") + &e;
            return true;
        }
        self.bv = BrowseView::default();
        self.mode = Mode::Browse;
        self.browse_fetch(raw, false);
        true
    }

    /// 開始取一個位址。push 為真時把目前的位址推進上一頁堆疊。
    pub(crate) fn browse_fetch(&mut self, raw: &str, push: bool) {
        let full = match normalize_url(raw) {
            Ok(f) => f,
            Err(e) => {
                self.bv.status = t("位址解不開:") + &e;
                return;
            }
        };
        if push && !self.bv.url.is_empty() {
            let u = self.bv.url.clone();
            self.bv.hist.push(u);
        }
        // 本機的文件同步做完就好:為它多開一條非同步的路只會多一種「正在取回」的狀態。
        if full.starts_with(EPUB_SCHEME) {
            self.show_book(&full);
            return;
        }
        if full.starts_with(PDF_SCHEME) {
            self.show_pdf(&full);
            return;
        }
        if full.starts_with(DOC_SCHEME) {
            self.show_office(&full);
            return;
        }
        let (tx, rx) = std::sync::mpsc::channel();
        self.pending = Some(rx);
        if wincv_web::is_http(&full) {
            self.bv.status = t("連線 ") + &host_of(&full) + " …";
            let client = self.web.clone().unwrap_or_default();
            std::thread::spawn(move || {
                let _ = tx.send((full.clone(), Fetched::Web(client.fetch(&full))));
            });
            return;
        }
        let u = match wincv_gopher::parse_url(&full) {
            Ok(u) => u,
            Err(e) => {
                self.pending = None;
                self.bv.status = t("位址解不開:") + &e;
                return;
            }
        };
        self.bv.status = t("連線 ") + &u.host + " …";
        let client = self.gopher.clone().unwrap_or_default();
        std::thread::spawn(move || {
            let r = client.fetch(&u);
            let _ = tx.send((u.to_string(), Fetched::Gopher(u, r)));
        });
    }

    /// 收取回的結果。沒有結果就立刻回來:這支在每一幀都會被呼叫。
    pub(crate) fn browse_poll(&mut self) {
        let Some(rx) = &self.pending else { return };
        let r = match rx.try_recv() {
            Ok(r) => r,
            Err(TryRecvError::Empty) => return,
            Err(TryRecvError::Disconnected) => {
                self.pending = None;
                return;
            }
        };
        self.pending = None;
        match r {
            (url, Fetched::Img(res)) => self.show_inline_image(&url, res),
            (url, f) => self.browse_show(url, f),
        }
    }

    /// 把取回的東西變成畫面。
    fn browse_show(&mut self, url: String, f: Fetched) {
        // 截斷不是失敗:內容還是可以看,只是不完整,要說出來。
        let mut truncated = false;
        let f = match f {
            Fetched::Gopher(u, Err(wincv_gopher::FetchError::TooLarge(d))) => {
                truncated = true;
                Fetched::Gopher(u, Ok(d))
            }
            Fetched::Gopher(_, Err(e)) => {
                self.bv.status = e.to_string();
                return;
            }
            Fetched::Web(Err(e)) => {
                self.bv.status = e.to_string();
                return;
            }
            f => f,
        };
        let bv = &mut self.bv;
        bv.url = url;
        bv.status.clear();
        (bv.top, bv.cur, bv.cols) = (0, -1, 0);
        bv.lines = None;
        bv.imgs.clear();
        bv.want.clear();
        match f {
            Fetched::Web(Ok(d)) => self.show_web_doc(d),
            Fetched::Gopher(u, Ok(data)) => {
                self.bv.title = browse_name(&u);
                if wincv_gopher::is_image(u.typ) {
                    let name = browse_name(&u);
                    self.show_image_bytes(&name, &data);
                    return;
                }
                self.bv.blocks = if u.typ == wincv_gopher::TYPE_TEXT {
                    wincv_gopher::text_blocks(&data)
                } else {
                    // 選單、查詢結果,以及型別不認得的東西。不認得時當選單解:解不出項目的話
                    // 畫面會是空的,那比把選單當成純文字倒出來好判讀。
                    let items = wincv_gopher::parse_menu(&data);
                    if items.is_empty() {
                        wincv_gopher::text_blocks(&data)
                    } else {
                        wincv_gopher::menu_blocks(&items)
                    }
                };
                if truncated {
                    self.bv.status = t("內容超過上限,只顯示前面的部分");
                }
            }
            _ => {}
        }
    }

    /// 把一份網頁放上畫面。
    fn show_web_doc(&mut self, d: wincv_web::Doc) {
        self.bv.url = d.url;
        self.bv.title = d.title;
        if !d.image.is_empty() {
            let title = self.bv.title.clone();
            self.show_image_bytes(&title, &d.image);
            return;
        }
        self.bv.blocks = d.blocks;
        if self.bv.blocks.is_empty() {
            self.bv.status = t("這一頁沒有可以顯示的文字");
        }
        if d.truncated {
            self.bv.status = t("內容超過上限,只顯示前面的部分");
        }
        // 頁面裡的圖片先列出來,排版時發現裝得下才真的去取。
        self.bv.want = self.bv.blocks.iter().filter(|b| b.kind == Kind::Image && wincv_web::is_http(&b.src)).map(|b| b.src.clone()).collect();
    }

    /// 把一段位元組當圖片開,Esc 退回瀏覽模式。
    pub(crate) fn show_image_bytes(&mut self, name: &str, data: &[u8]) {
        match wincv_imgview::Model::load(name, data) {
            Ok(m) => {
                self.image = Some(m);
                self.mode = Mode::Image;
                self.g_return = true;
            }
            Err(e) => self.bv.status = t("圖解不開:") + &e,
        }
    }

    /// 取這一頁還沒取回的圖。一次取一張,取回一張就重排一次:圖片會改變後面所有內容的
    /// 行號,而使用者在等的時候看得到文字比看到空白好。取不到的就留 alt,不重試。
    ///
    /// 只有設了 web 客戶端才取(與 Go 版相同:Go 版的視窗程式沒有設,網頁的行內圖片
    /// 實際上不會去取)。
    pub(crate) fn browse_images(&mut self) {
        if self.pending.is_some() || self.bv.want.is_empty() {
            return;
        }
        let Some(client) = self.web.clone() else { return };
        let src = self.bv.want.remove(0);
        if self.bv.imgs.contains_key(&src) {
            return;
        }
        // 先佔位,取不回來也不會再排進佇列。
        self.bv.imgs.insert(src.clone(), None);
        let (tx, rx) = std::sync::mpsc::channel();
        self.pending = Some(rx);
        std::thread::spawn(move || {
            let r = client.fetch_image(&src);
            let _ = tx.send((src, Fetched::Img(r)));
        });
    }

    /// 把取回的內嵌圖填進這一頁,並逼排版重做一次。取不到就留著 alt,不重試:網頁上壞掉
    /// 的圖連結非常多,一直重試只會讓 busy 一直為真,畫面因此永遠停不下來。
    fn show_inline_image(&mut self, url: &str, res: Result<Vec<u8>, wincv_web::Error>) {
        let Ok(data) = res else { return };
        if data.is_empty() {
            return;
        }
        let Ok((img, _)) = wincv_imgfmt::decode(img_name_of(url), &data) else { return };
        self.bv.imgs.insert(url.to_string(), Some(Arc::new(img)));
        // 圖片會改變它後面所有內容的行號,整頁都要重排。
        self.bv.cols = 0;
        self.bv.lines = None;
    }

    /// 排版引擎要圖時的來源。只從已經取回的那一份拿,不在這裡發網路請求:排版是同步的,
    /// 而且會在捲動時反覆呼叫。本機文件(書)裡的圖讀完就有。
    fn browse_image(&mut self, src: &str) -> Result<Arc<Image>, String> {
        if let Some(Some(m)) = self.bv.imgs.get(src) {
            return Ok(m.clone());
        }
        // PDF 與 Office 文件的圖也是本機的,內嵌在檔案裡,讀出來就有。
        if src.starts_with(PDF_SCHEME) || src.starts_with(DOC_SCHEME) {
            let r = if src.starts_with(PDF_SCHEME) { self.pdf_image(src) } else { self.office_image(src) };
            let m = Arc::new(r?);
            self.bv.imgs.insert(src.to_string(), Some(m.clone()));
            return Ok(m);
        }
        // 書裡的圖是本機 zip 的成員,讀完就有,不必等一輪。
        if self.book.is_some() && !wincv_web::is_http(src) {
            let m = Arc::new(self.book_image(src)?);
            self.bv.imgs.insert(src.to_string(), Some(m.clone()));
            return Ok(m);
        }
        Err(t("還沒取回"))
    }

    /// 需要時重新排版,並重建連結表。
    fn browse_layout(&mut self, cols: i32) {
        if cols == self.bv.cols && self.bv.lines.is_some() {
            return;
        }
        self.bv.cols = cols;
        // gopher 的選單裡沒有內嵌圖(圖片是獨立的項目),網頁有。兩邊走同一條路:載入器只從
        // 已經取回的那一份拿,拿不到就留 alt。
        //
        // 排版引擎的載入器是 Fn(不可變借用),而本機文件的圖要邊讀邊記進快取,所以先把
        // 要的圖讀好放進快取,再排版。
        let srcs: Vec<String> = self.bv.blocks.iter().filter(|b| b.kind == Kind::Image).map(|b| b.src.clone()).collect();
        let mut errs: HashMap<String, String> = HashMap::new();
        for s in srcs {
            if let Err(e) = self.browse_image(&s) {
                errs.insert(s, e);
            }
        }
        let imgs = &self.bv.imgs;
        let load = |src: &str| match imgs.get(src) {
            Some(Some(m)) => Ok(m.clone()),
            _ => Err(errs.get(src).cloned().unwrap_or_else(|| t("還沒取回"))),
        };
        let (lines, _) = wincv_markdown::layout(&self.bv.blocks, cols, self.cell_w, self.cell_h, Some(&load), wincv_markdown::default_theme());
        let mut links: Vec<BrowseLink> = Vec::new();
        for (i, ln) in lines.iter().enumerate() {
            let Some(href) = ln.pieces.iter().find(|p| !p.href.is_empty()).map(|p| p.href.clone()) else { continue };
            // 同一個連結折成多列時只記一次,但要把列數加上去:游標要把整個項目反白。
            if let Some(l) = links.last_mut() {
                if l.href == href && l.line + l.rows == i as i32 {
                    l.rows += 1;
                    continue;
                }
            }
            links.push(BrowseLink { href, line: i as i32, rows: 1 });
        }
        let bv = &mut self.bv;
        bv.lines = Some(lines);
        bv.links = links;
        let n = bv.links.len() as i32;
        if bv.cur >= n {
            bv.cur = n - 1;
        }
        if bv.cur < 0 && n > 0 {
            bv.cur = 0;
        }
    }

    fn bv_lines_len(&self) -> i32 {
        self.bv.lines.as_ref().map_or(0, |l| l.len()) as i32
    }

    /// 處理瀏覽模式的按鍵。
    pub(crate) fn browse_key(&mut self, k: Key) -> bool {
        let rows = self.rows.max(1);
        match k.code {
            Code::Esc => {
                self.mode = Mode::Browser;
                self.close_book();
                self.close_pdf();
                self.close_office();
                return true;
            }
            Code::Backspace => return self.browse_back(),
            Code::Up => return self.browse_move(-1, rows),
            Code::Down | Code::Tab => return self.browse_move(1, rows),
            Code::PgUp => {
                self.bv.top -= rows;
                self.browse_clamp(rows);
                return true;
            }
            Code::PgDn => {
                self.bv.top += rows;
                self.browse_clamp(rows);
                return true;
            }
            Code::Home => {
                self.bv.top = 0;
                if !self.bv.links.is_empty() {
                    self.bv.cur = 0;
                }
                return true;
            }
            Code::End => {
                self.bv.top = self.bv_lines_len();
                self.browse_clamp(rows);
                if !self.bv.links.is_empty() {
                    self.bv.cur = self.bv.links.len() as i32 - 1;
                }
                return true;
            }
            Code::Enter => return self.browse_follow(),
            _ => {}
        }
        if k.code == Code::Rune && k.alt && (k.r == 'g' || k.r == 'G') {
            return self.browse_ask();
        }
        // V:把 PDF 的這一頁整頁畫出來。只有 PDF 有這個動作:其他來源(gopher、網頁、書、
        // Office 文件)本來就沒有「頁面」這回事。
        if k.code == Code::Rune && !k.alt && (k.r == 'v' || k.r == 'V') && self.bv.url.starts_with(PDF_SCHEME) {
            return self.show_pdf_page_image();
        }
        false
    }

    /// 移動連結游標。沒有連結的頁面(文字檔)就直接捲。
    fn browse_move(&mut self, d: i32, rows: i32) -> bool {
        if self.bv.links.is_empty() {
            self.bv.top += d;
            self.browse_clamp(rows);
            return true;
        }
        let bv = &mut self.bv;
        bv.cur = (bv.cur + d).max(0).min(bv.links.len() as i32 - 1);
        // 游標要留在畫面內。
        let l = &bv.links[bv.cur as usize];
        if l.line < bv.top {
            bv.top = l.line;
        }
        if l.line + l.rows > bv.top + rows {
            bv.top = l.line + l.rows - rows;
        }
        self.browse_clamp(rows);
        true
    }

    fn browse_clamp(&mut self, rows: i32) {
        let max = self.bv_lines_len() - rows;
        if self.bv.top > max {
            self.bv.top = max;
        }
        if self.bv.top < 0 {
            self.bv.top = 0;
        }
    }

    /// 跟著目前的連結走。
    pub(crate) fn browse_follow(&mut self) -> bool {
        let Some(href) = self.bv.links.get(self.bv.cur.max(0) as usize).filter(|_| self.bv.cur >= 0).map(|l| l.href.clone()) else {
            return false;
        };
        if wincv_web::is_http(&href) || href.starts_with(EPUB_SCHEME) || href.starts_with(PDF_SCHEME) {
            self.browse_fetch(&href, true);
            return true;
        }
        let mut u = match wincv_gopher::parse_url(&href) {
            Ok(u) => u,
            Err(e) => {
                self.message = e;
                return true;
            }
        };
        // 型別 7 是查詢介面,要先問查什麼再送出去。
        if u.typ == wincv_gopher::TYPE_SEARCH {
            let title = t("查詢 ") + &u.host;
            self.ask(&title, "", move |a, q| {
                if q.is_empty() {
                    return;
                }
                u.search = q;
                a.browse_fetch(&u.to_string(), true);
            });
            return true;
        }
        self.browse_fetch(&href, true);
        true
    }

    /// 回上一頁。
    fn browse_back(&mut self) -> bool {
        let Some(u) = self.bv.hist.pop() else {
            self.message = t("已經是第一頁");
            return true;
        };
        self.browse_fetch(&u, false);
        true
    }

    /// 問一個新位址。
    pub(crate) fn browse_ask(&mut self) -> bool {
        let from_browser = self.mode != Mode::Browse;
        let init = self.bv.url.clone();
        self.ask(&t("位址(gopher:// 或 http://)"), &init, move |a, s| {
            if s.is_empty() {
                return;
            }
            if let Err(e) = normalize_url(&s) {
                a.message = t("位址解不開:") + &e;
                return;
            }
            // 從檔案清單叫進來的話,這裡是這一輪瀏覽的起點:換模式並清掉上一輪的歷史,
            // 不然 Backspace 會退回上次瀏覽的東西。
            if from_browser {
                a.bv = BrowseView::default();
                a.mode = Mode::Browse;
                a.browse_fetch(&s, false);
                return;
            }
            a.browse_fetch(&s, true);
        });
        true
    }

    pub(crate) fn draw_browse(&mut self, s: &mut Screen) -> Vec<Overlay> {
        let th = wincv_markdown::default_theme();
        s.clear(th.text, th.bg);
        let rows = (s.rows - 1).max(1);
        self.rows = rows;
        self.browse_layout(s.cols);
        self.browse_clamp(rows);
        // 目前游標所在的列範圍,用來反白。
        let (sel_from, sel_to) = match self.bv.links.get(self.bv.cur.max(0) as usize).filter(|_| self.bv.cur >= 0) {
            Some(l) => (l.line, l.line + l.rows),
            None => (-1, -1),
        };
        let mut ovs = Vec::new();
        let top = self.bv.top;
        let lines = self.bv.lines.as_deref().unwrap_or(&[]);
        for i in 0..rows {
            let idx = top + i;
            let Some(ln) = lines.get(idx as usize) else { break };
            if let Some(p) = &ln.pic {
                // 一張圖只在它的第一列排一次 overlay,而且要考慮圖被捲出畫面上緣的情況:
                // 那時 overlay 的矩形要往上超出,由光柵器自己裁掉。
                if ln.pic_row == 0 || idx == top {
                    if let Some(img) = &p.img {
                        let y = i - ln.pic_row;
                        ovs.push(Overlay {
                            img: img.clone(),
                            x: 0,
                            y: y * self.cell_h,
                            w: p.cols * self.cell_w,
                            h: p.rows * self.cell_h,
                            fit: true,
                            pan_x: 0,
                            pan_y: 0,
                            zoom: 0.0,
                        });
                    }
                }
                continue;
            }
            let sel = idx >= sel_from && idx < sel_to;
            if sel {
                s.fill(0, i, s.cols, 1, ' ', Color::BLACK, Color::LT_GRAY);
            }
            let mut x = 0;
            for pc in &ln.pieces {
                if x >= s.cols {
                    break;
                }
                let (fg, bg) = if sel { (Color::BLACK, Color::LT_GRAY) } else { (pc.fg, pc.bg) };
                let n = s.print(x, i, &pc.text, fg, bg);
                if pc.under && !sel {
                    s.underline(x, i, n, true);
                }
                x += n;
            }
        }
        self.draw_browse_status(s);
        ovs
    }

    fn draw_browse_status(&self, s: &mut Screen) {
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', Color::LT_GRAY2, Color::BLUE);
        // 標題比位址好認,但位址才說得出「現在在哪一台」。標題有就先給標題,後面補主機名:
        // 網頁的標題常常完全看不出是哪個站。
        let bv = &self.bv;
        let mut left = bv.url.clone();
        // PDF 與 Office 文件的位址是本機路徑,沒有主機名可補,只給標題。
        if bv.url.starts_with(PDF_SCHEME) || bv.url.starts_with(DOC_SCHEME) {
            left = bv.title.clone();
        } else if let Some((book, _)) = crate::bookview::parse_book_url(&bv.url) {
            left = bv.title.clone();
            if let Some(b) = &self.book {
                if b.title != bv.title {
                    left = format!("{} — {}", b.title, bv.title);
                }
            }
            if left.is_empty() {
                left = book.rsplit(['/', std::path::MAIN_SEPARATOR]).next().unwrap_or(&book).to_string();
            }
        } else if !bv.title.is_empty() && bv.title != host_of(&bv.url) {
            left = format!("{} — {}", bv.title, host_of(&bv.url));
        }
        if !bv.status.is_empty() {
            left = bv.status.clone();
        }
        if !self.message.is_empty() {
            left = self.message.clone();
        }
        s.print(0, y, &left, Color::LT_GRAY2, Color::BLUE);
        // [雷] 寬度要用顯示格數不能用字元數:中文字佔兩格。用字元數會低估,判斷式失準之後
        // 提示會安靜地整條消失。由長到短試,擠不下最短的那個就不畫。
        let cands = [
            t("Enter 開啟  Backspace 上一頁  F2 位址  Esc 離開"),
            t("Enter 開啟  ← 上一頁  F2 位址"),
            t("F2 位址  Esc 離開"),
        ];
        let pos = if bv.links.is_empty() { String::new() } else { format!("{}/{}  ", bv.cur + 1, bv.links.len()) };
        for c in cands {
            let right = pos.clone() + &c;
            let x = s.cols - cell_width(&right) - 1;
            if x > cell_width(&left) + 1 {
                s.print(x, y, &right, Color::LT_GRAY2, Color::BLUE);
                break;
            }
        }
    }
}

