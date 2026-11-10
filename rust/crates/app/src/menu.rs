//! 選單列、下拉與 MD5 / SFV(對應 Go 版 `menu.go`)。

use std::path::{Path, PathBuf};
use std::rc::Rc;

use wincv_cell::{Color, Screen};
use wincv_i18n::{t, tr, Lang};
use wincv_keys::{Code, Key};
use wincv_textenc::Enc;

use crate::{cell_width, App, Mode, SCALE_STEP};

/// 選單列佔掉的列數。
///
/// 原版的選單是 Win32 的原生選單,掛在標題列底下、client area 之外,不佔任何一個
/// 字元格。remake 全部自繪,所以它一定會吃掉一列:預設視窗因此比原版高一列
/// (93×22),內容區才維持原版的 21 列。
pub const MENU_BAR_ROWS: i32 = 1;

type Run = Rc<dyn Fn(&mut App) -> bool>;

/// 下拉選單的一列。sep 為真時是分隔線。
#[derive(Clone, Default)]
pub(crate) struct MenuItem {
    pub label: String,
    pub key: Key,
    pub sep: bool,
    /// 沒有對應按鍵的功能(例如 MD5/SFV)用這個。
    pub run: Option<Run>,
    /// 子選單。選中時換成它的內容,Esc 退回來。
    pub sub: Option<fn(&App) -> Vec<MenuItem>>,
}

fn item(label: String, key: Key) -> MenuItem {
    MenuItem { label, key, ..Default::default() }
}

fn sep() -> MenuItem {
    MenuItem { sep: true, ..Default::default() }
}

fn run(label: String, f: impl Fn(&mut App) -> bool + 'static) -> MenuItem {
    MenuItem { label, run: Some(Rc::new(f)), ..Default::default() }
}

fn sub(label: String, f: fn(&App) -> Vec<MenuItem>) -> MenuItem {
    MenuItem { label, sub: Some(f), ..Default::default() }
}

/// 選單列上的一個分類。items 是函式而不是清單:項目的標籤會隨狀態變(例如放大倍率),
/// 每次展開才建構,不必在意快取失效。
pub(crate) struct MenuCat {
    pub name: String,
    items: fn(&App) -> Vec<MenuItem>,
}

/// 選單列與它展開的下拉。選單列本身一直在畫面最上方那一列,active 說的是下拉有沒有
/// 展開。下拉同時當說明書用:每一列都寫著對應的按鍵,按過幾次就記得了。
#[derive(Default)]
pub(crate) struct Menu {
    pub active: bool,
    /// 選單列上的第幾個分類。
    pub cat: i32,
    pub items: Vec<MenuItem>,
    pub cursor: i32,
    /// 項目比畫面高時,第一列顯示的是第幾項。
    pub top: i32,
    /// 最近一次繪製時裝得下幾項。
    pub rows: i32,
    /// 下拉框的左緣,對齊分類標題。
    pub x: i32,
    /// 子選單的返回點。只存一層:選單是給人按的,深到要記路徑就表示分類方式錯了。
    pub back: Option<Vec<MenuItem>>,
    pub back_at: i32,
    pub title: String,
}

impl Menu {
    fn open() -> Menu {
        Menu { active: true, ..Default::default() }
    }

    /// 畫面裝得下幾項。還沒畫過時給一個保守值。
    fn visible(&self) -> i32 {
        if self.rows > 0 {
            self.rows
        } else {
            10
        }
    }

    /// 讓游標所在的項目留在可見範圍內。
    fn scroll_to_cursor(&mut self) {
        let n = self.visible();
        if self.cursor < self.top {
            self.top = self.cursor;
        }
        if self.cursor >= self.top + n {
            self.top = self.cursor - n + 1;
        }
        self.top = self.top.max(0);
    }
}

/// 選單列右側一顆可以點的功能鍵。x0..=x1 是它在選單列上的格位,由 draw_bar_keys
/// 每次繪製時算出。
pub(crate) struct BarKey {
    pub x0: i32,
    pub x1: i32,
    pub key: Key,
}

/// 選單列上第 i 個分類的字樣:前面帶原版的編號,那個數字就是 Alt-數字 的鍵。
pub(crate) fn cat_label(i: usize, name: &str) -> String {
    format!(" {}{name} ", i + 1)
}

/// 選單列上的分類。分類與順序照原版的八類(WINCV.IMG 0x101fd0 起的 `&1檔案` 到
/// `&8說明/註冊/更新版本`),所以 Alt-1 到 Alt-8 的肌肉記憶與原版一致。原版沒有、
/// 重製版才有的功能放進意思最接近的那一類,不另開分類。
pub(crate) fn menu_cats() -> Vec<MenuCat> {
    vec![
        MenuCat { name: t("檔案"), items: file_menu_items },
        MenuCat { name: t("目錄/磁碟"), items: dir_menu_items },
        MenuCat { name: t("網路"), items: net_menu_items },
        MenuCat { name: t("標記"), items: mark_menu_items },
        MenuCat { name: t("檢視"), items: view_menu_items },
        MenuCat { name: t("轉檔/轉碼/其他"), items: tool_menu_items },
        MenuCat { name: t("設定"), items: setup_menu_items },
        MenuCat { name: t("說明"), items: help_menu_items },
    ]
}

fn file_menu_items(_: &App) -> Vec<MenuItem> {
    let (k, alt) = (Key::ch, Key::alt_ch);
    vec![
        item(t("檢視"), Key::named(Code::Enter)),
        item(t("編輯"), k('E')),
        item(t("開啟(系統預設程式)"), k('O')),
        item(t("執行"), k('G')),
        sep(),
        item(t("拷貝"), k('C')),
        item(t("移動"), k('M')),
        item(t("更名"), k('R')),
        item(t("刪除"), k('D')),
        item(t("註解"), alt('E')),
        sep(),
        item(t("解壓縮"), k('Z')),
        item(t("製作壓縮檔(.zip)"), alt('Z')),
        sep(),
        item(t("離開"), alt('X')),
    ]
}

fn dir_menu_items(_: &App) -> Vec<MenuItem> {
    vec![
        item(t("改變路徑"), Key::ch('P')),
        item(t("排序方式"), Key::ch('S')),
        item(t("磁碟窗格"), Key::alt_ch('D')),
        sep(),
        item(t("尋找 檔名/字串/註解"), Key::ch('W')),
    ]
}

fn net_menu_items(_: &App) -> Vec<MenuItem> {
    vec![item(t("網路瀏覽(gopher / http)"), Key::named(Code::F2))]
}

fn mark_menu_items(_: &App) -> Vec<MenuItem> {
    vec![
        item(t("標記全部檔案"), Key::ch('T')),
        item(t("標記全部(含目錄)"), Key::alt_ch('T')),
        item(t("解除所有標記"), Key::ch('U')),
        sep(),
        item(t("比對兩個標記的檔案"), Key::alt_ch('C')),
    ]
}

fn view_menu_items(_: &App) -> Vec<MenuItem> {
    vec![
        item(t("縮圖列表"), Key::ch('5')),
        item(t("預視窗格"), Key::alt_ch('P')),
        sep(),
        item(t("切換 中英文顯示"), Key::named(Code::F8)),
        item(t("全螢幕"), Key::named(Code::F11)),
    ]
}

fn tool_menu_items(_: &App) -> Vec<MenuItem> {
    vec![
        item(t("轉換(換行/編碼/去 HTML)"), Key::ctrl_ch('O')),
        sep(),
        run(t("算 MD5"), App::run_md5),
        run(t("建 SFV(.sfv)"), App::run_make_sfv),
        run(t("驗 SFV"), App::run_verify_sfv),
    ]
}

fn setup_menu_items(a: &App) -> Vec<MenuItem> {
    vec![
        item(t("放大字體"), Key::ctrl_ch('+')),
        item(t("縮小字體"), Key::ctrl_ch('-')),
        sep(),
        item(tr!("放大倍率 +%.1f(現在 %.1f×)", SCALE_STEP, a.scale), Key::alt_ch('+')),
        item(tr!("放大倍率 -%.1f", SCALE_STEP), Key::alt_ch('-')),
        sub(t("視窗大小…"), crate::viewsize::size_menu_items),
        sep(),
        sub(a.menu_zoom_label(), menu_font_items),
        sep(),
        sub(a.cjk24_label(), cjk24_menu_items),
        sep(),
        sub(lang_label(), lang_menu_items),
    ]
}

/// 現在用哪個語言。用該語言自己的名字:找自己母語的人認得的是「日本語」,
/// 不是翻譯過的「日文」。
fn lang_label() -> String {
    t("語言…") + "(" + wincv_i18n::current().name() + ")"
}

fn lang_menu_items(_: &App) -> Vec<MenuItem> {
    wincv_i18n::langs()
        .into_iter()
        .map(|l| {
            let mut label = l.name().to_string();
            if l == wincv_i18n::current() {
                label = "• ".to_string() + &label;
            }
            run(label, move |a| a.set_lang(l))
        })
        .collect()
}

/// 「選單字級」子選單。選單與內容各自選字級,不必動到對方。
fn menu_font_items(a: &App) -> Vec<MenuItem> {
    let mut out = vec![run(t("跟著內容"), |a| a.set_menu_zoom(-1))];
    for n in 0..=a.max_zoom {
        out.push(run(tr!("第 %d 級", n + 1), move |a| a.set_menu_zoom(n)));
    }
    out
}

/// 可寫入 session.json 的 CJK 字形來源。這裡只變更使用者選擇;外殼會在下一幀驗證
/// 檔案並換掉光柵器。
pub(crate) fn cjk24_menu_items(a: &App) -> Vec<MenuItem> {
    wincv_eten::cjk24::SOURCES
        .iter()
        .map(|s| {
            let id = s.id;
            let mut label = cjk24_name(id);
            if id == a.cjk24 {
                label = "• ".to_string() + &label;
            }
            run(label, move |a| a.set_cjk24(id))
        })
        .collect()
}

/// 代號在介面上的翻譯名稱。原文直接放在 t 呼叫裡,語系檢查才能證明所有翻譯鍵都有用到。
pub fn cjk24_name(id: &str) -> String {
    match id {
        "eten-k" => t("倚天 楷體 (24 點)"),
        "eten-l" => t("倚天 隸書 (24 點)"),
        "eten-r" => t("倚天 圓體 (24 點)"),
        "eten-b" => t("倚天 黑體 (24 點)"),
        "eten-s" => t("倚天 宋體 (24 點)"),
        "guoqiao" => t("國喬 (24 點)"),
        _ => t("倚天 明體 (24 點)"),
    }
}

fn help_menu_items(_: &App) -> Vec<MenuItem> {
    vec![item(t("使用說明"), Key::named(Code::F1)), run(t("關於"), App::open_about)]
}

/// 選單列右側的功能鍵,順序就是畫出來的順序。
fn bar_key_defs() -> [(Code, String); 3] {
    [(Code::F9, t("選單")), (Code::F1, t("說明")), (Code::F2, t("網路"))]
}

/// 選單列與展開的下拉,各自畫在自己的畫面上。
///
/// 分開交出去而不是畫進主畫面:選單可以用與內容**不同的字型與大小**,兩者的格點根本
/// 對不起來。外殼拿它們各自光柵化,再依像素座標疊上去。
#[derive(Default)]
pub struct MenuLayer {
    /// 最上面那一列。None 表示選單列關著。
    pub bar: Option<Screen>,
    /// 展開的下拉。None 表示沒展開。
    pub drop: Option<Screen>,
    /// 下拉左緣,以**選單格**為單位。
    pub drop_x: i32,
}

impl App {
    /// 現在下拉是不是展開著。
    pub fn menuing(&self) -> bool {
        self.menu.active
    }

    fn set_lang(&mut self, l: Lang) -> bool {
        if wincv_i18n::current() == l {
            return true;
        }
        wincv_i18n::set(l);
        self.lang = l.tag().to_string();
        // 選單標籤、觸控列、狀態列全部是每次繪製才組出來的,換完語言下一幀就是新語言。
        self.message = t("已換成") + " " + l.name();
        true
    }

    fn menu_zoom_label(&self) -> String {
        if self.menu_zoom < 0 {
            return t("選單字級…(跟著內容)");
        }
        tr!("選單字級…(第 %d 級)", self.menu_zoom + 1)
    }

    /// 換選單那一層的字級。
    fn set_menu_zoom(&mut self, n: i32) -> bool {
        let n = n.max(-1).min(self.max_zoom);
        if n == self.menu_zoom {
            return false;
        }
        self.menu_zoom = n;
        self.message = if n < 0 { t("選單字級跟著內容") } else { tr!("選單字級 第 %d 級", n + 1) };
        true
    }

    /// 目前內容層的 24 點 CJK 字形來源。
    fn cjk24_label(&self) -> String {
        let id = if wincv_eten::cjk24::find(&self.cjk24).is_some() { self.cjk24.as_str() } else { wincv_eten::cjk24::DEFAULT };
        t("CJK 字形…") + "(" + &cjk24_name(id) + ")"
    }

    pub(crate) fn set_cjk24(&mut self, id: &str) -> bool {
        let Some(s) = wincv_eten::cjk24::find(id) else { return false };
        if id == self.cjk24 {
            return false;
        }
        self.cjk24 = id.to_string();
        self.message = tr!("CJK 字形已選為 %s", cjk24_name(s.id));
        true
    }

    /// 叫出選單的兩種鍵盤操作,與原版的 Win32 選單相同:
    ///
    /// - Alt-1 到 Alt-8 直接展開對應的分類。下拉開著時按,換到那一類。
    /// - 單按一下 Alt 展開第一類;下拉開著時按,一次收起所有層(Esc 只退一層,這是
    ///   Win32 選單的慣例)。
    ///
    /// 不是這兩種鍵回 None,交給後面的分派。
    pub(crate) fn menu_shortcut(&mut self, k: Key) -> Option<bool> {
        if k.code == Code::MenuKey {
            if self.menu.active {
                self.menu = Menu::default();
            } else {
                self.menu = Menu::open();
                self.select_cat(0);
            }
            return Some(true);
        }
        if k.code != Code::Rune || !k.alt || k.ctrl || !('1'..='9').contains(&k.r) {
            return None;
        }
        let i = k.r as i32 - '1' as i32;
        if i as usize >= menu_cats().len() {
            return None;
        }
        if !self.menu.active {
            self.menu = Menu::open();
        }
        self.select_cat(i);
        Some(true)
    }

    /// 展開下拉,已經展開的話收起來。
    pub(crate) fn open_menu(&mut self) -> bool {
        if self.menu.active {
            self.menu = Menu::default();
            return true;
        }
        self.menu = Menu::open();
        self.select_cat(0);
        true
    }

    /// 換到第 i 個分類並載入它的項目。左右兩端會繞回來。
    pub(crate) fn select_cat(&mut self, i: i32) {
        let cats = menu_cats();
        let n = cats.len() as i32;
        let i = if i < 0 {
            n - 1
        } else if i >= n {
            0
        } else {
            i
        };
        let items = (cats[i as usize].items)(self);
        let m = &mut self.menu;
        m.cat = i;
        m.items = items;
        m.cursor = 0;
        m.top = 0;
        m.back = None;
        m.back_at = 0;
        m.title.clear();
        // 游標不要停在分隔線上
        while (m.cursor as usize) < m.items.len() && m.items[m.cursor as usize].sep {
            m.cursor += 1;
        }
    }

    pub(crate) fn menu_key(&mut self, k: Key) -> bool {
        let m = &mut self.menu;
        let n = m.items.len() as i32;
        let mv = |m: &mut Menu, d: i32| {
            for _ in 0..n {
                m.cursor += d;
                if m.cursor < 0 {
                    m.cursor = n - 1;
                }
                if m.cursor >= n {
                    m.cursor = 0;
                }
                if !m.items[m.cursor as usize].sep {
                    break;
                }
            }
            m.scroll_to_cursor();
            true
        };
        // 子選單裡的左鍵是「退回上一層」而不是「換分類」:換掉分類會把使用者丟到別的
        // 地方,而他只是想退一步。
        let in_sub = m.back.is_some();
        match k.code {
            Code::Up => return mv(m, -1),
            Code::Down => return mv(m, 1),
            Code::Left => {
                if in_sub {
                    return self.menu_leave_sub();
                }
                let c = m.cat;
                self.select_cat(c - 1);
                return true;
            }
            Code::Right => {
                if !in_sub {
                    let c = m.cat;
                    self.select_cat(c + 1);
                }
                return true;
            }
            Code::Home => {
                // 先退到範圍外再往前走一格,才會停在**第一個**可選項目上。
                m.cursor = -1;
                return mv(m, 1);
            }
            Code::End => {
                m.cursor = n;
                return mv(m, -1);
            }
            Code::PgUp => {
                let v = m.visible();
                return mv(m, -v);
            }
            Code::PgDn => {
                let v = m.visible();
                return mv(m, v);
            }
            Code::Esc | Code::F9 => {
                if in_sub && k.code == Code::Esc {
                    return self.menu_leave_sub();
                }
                self.menu = Menu::default();
                return true;
            }
            Code::Enter => {
                let Some(it) = m.items.get(m.cursor as usize).cloned() else { return false };
                if let Some(f) = it.sub {
                    let items = f(self);
                    let m = &mut self.menu;
                    m.back = Some(std::mem::take(&mut m.items));
                    m.back_at = m.cursor;
                    m.title = it.label;
                    m.items = items;
                    m.cursor = 0;
                    m.top = 0;
                    return true;
                }
                // 下拉收起來本身就是畫面變化。項目的動作可能什麼都沒改(字級已到頂),
                // 那時它回 false;照傳的話外殼不重繪,畫面會停在一個已經不存在的下拉上。
                self.menu = Menu::default();
                self.message.clear();
                match it.run {
                    Some(f) => {
                        f(self);
                    }
                    None => {
                        self.handle_key(it.key);
                    }
                }
                return true;
            }
            _ => {}
        }
        // 直接按該功能的鍵也算數:下拉同時是說明書。
        if m.items.iter().any(|it| !it.sep && it.run.is_none() && it.key == k) {
            self.menu = Menu::default();
            self.message.clear();
            self.handle_key(k);
        }
        true // 下拉開著,其他鍵一律吃掉
    }

    /// 從子選單退回上一層。
    fn menu_leave_sub(&mut self) -> bool {
        let m = &mut self.menu;
        m.items = m.back.take().unwrap_or_default();
        m.cursor = m.back_at;
        m.title.clear();
        m.scroll_to_cursor();
        true
    }

    /// 畫最上方那一列。
    fn draw_menu_bar(&mut self, s: &mut Screen) {
        s.fill(0, 0, s.cols, 1, ' ', Color::BLACK, Color::LT_GRAY);
        let mut x = 1;
        for (i, c) in menu_cats().iter().enumerate() {
            let label = cat_label(i, &c.name);
            let w = cell_width(&label);
            let (mut fg, mut bg) = (Color::BLACK, Color::LT_GRAY);
            if self.menu.active && i as i32 == self.menu.cat {
                (fg, bg) = (Color::WHITE, Color::BLUE);
                // 下拉要對齊自己的分類標題。位置在這裡才算得出來,因為標題寬度取決於
                // 字型(全形字兩格)。
                self.menu.x = x;
            }
            if x + w > s.cols {
                break;
            }
            s.fill(x, 0, w, 1, ' ', fg, bg);
            s.print(x, 0, &label, fg, bg);
            x += w;
        }
        self.draw_bar_keys(s, x);
    }

    /// 在選單列右側畫 F9 / F1 / F2,滑鼠點了等同按那顆鍵。
    ///
    /// 先試「F9 選單  F1 說明  F2 網路」,擠不下就只畫鍵名「F9 F1 F2」,再擠不下才不畫。
    fn draw_bar_keys(&mut self, s: &mut Screen, x: i32) {
        self.bar_keys.clear();
        let defs = bar_key_defs();
        for long in [true, false] {
            let labels: Vec<String> = defs
                .iter()
                .map(|(c, name)| {
                    let k = Key::named(*c).to_string();
                    if long {
                        k + " " + name
                    } else {
                        k
                    }
                })
                .collect();
            let gap = if long { 2 } else { 1 };
            let w: i32 = labels.iter().map(|l| cell_width(l)).sum::<i32>() + gap * (defs.len() as i32 - 1);
            let mut px = s.cols - w;
            if long {
                px -= 1; // 寬的那一種與右緣留一格
            }
            if px < x {
                continue;
            }
            for (i, (c, _)) in defs.iter().enumerate() {
                let lw = cell_width(&labels[i]);
                s.print(px, 0, &labels[i], Color::BLUE, Color::LT_GRAY);
                self.bar_keys.push(BarKey { x0: px, x1: px + lw - 1, key: Key::named(*c) });
                px += lw + gap;
            }
            return;
        }
    }

    /// 選單列第 col 格是哪一顆功能鍵。
    pub fn bar_key_at(&self, col: i32) -> Option<Key> {
        self.bar_keys.iter().find(|b| col >= b.x0 && col <= b.x1).map(|b| b.key)
    }

    /// 下拉要多大。max_cols / max_rows 是選單格點下整個畫面的大小。
    fn menu_box_size(&mut self, max_cols: i32, max_rows: i32) -> (i32, i32) {
        let m = &mut self.menu;
        // 寬度用**顯示格數**算,不是位元組數:一個中文字是 3 個位元組但只佔 2 格。
        let mut w = 0;
        for it in &m.items {
            let mut l = cell_width(&it.label) + 4;
            if it.run.is_none() {
                l += it.key.to_string().len() as i32 + 4;
            }
            w = w.max(l);
        }
        let w = w.min(max_cols - 2).max(8);
        let head = if m.title.is_empty() { 0 } else { 1 };
        // 底下留兩列給狀態列與呼吸空間,選單再高就自己捲。最後一列是捲動提示。
        let mut max_h = max_rows - 2;
        if max_h < 3 {
            max_h = max_rows;
        }
        let h = (m.items.len() as i32 + head + 1).min(max_h).max(2);
        m.rows = h - head - 1;
        m.scroll_to_cursor();
        (w, h)
    }

    /// 畫展開的下拉。s 就是下拉那一塊,左上角是 (0,0)。
    fn draw_menu(&self, s: &mut Screen) {
        let m = &self.menu;
        let (w, h) = (s.cols, s.rows);
        let head = if m.title.is_empty() { 0 } else { 1 };
        s.fill(0, 0, w, h, ' ', Color::BLACK, Color::LT_GRAY);
        if head == 1 {
            s.fill(0, 0, w, 1, ' ', Color::WHITE, Color::BLUE);
            s.print(1, 0, &format!(" {} ", m.title), Color::WHITE, Color::BLUE);
        }
        for row in 0..m.rows {
            let i = m.top + row;
            let Some(it) = m.items.get(i as usize) else { break };
            let y = head + row;
            if it.sep {
                // 分隔線用 '-':cvga 是 Big5 的半形字型,0x80 以上是雙位元組字的前導
                // 位元組,沒有製表符號的字模,拿 U+2500 來畫會是一片空白。
                s.fill(2, y, w - 4, 1, '-', Color::GRAY, Color::LT_GRAY);
                continue;
            }
            // [雷] 按鍵的顏色要跟著反白換。兩種狀態都用 Blue 的話,游標那一列是藍字
            // 畫在藍底上,按鍵安靜地消失。
            let (mut fg, mut bg, mut kfg) = (Color::BLACK, Color::LT_GRAY, Color::BLUE);
            if i == m.cursor {
                (fg, bg, kfg) = (Color::WHITE, Color::BLUE, Color::LT_CYAN);
                s.fill(0, y, w, 1, ' ', fg, bg);
            }
            s.print(2, y, &it.label, fg, bg);
            // 子選單項目沒有按鍵:它的 key 是零值,印出來是 U+0000 的缺字框。
            if it.run.is_none() && it.sub.is_none() {
                let ks = it.key.to_string();
                let px = w - 2 - ks.len() as i32;
                if px > 2 {
                    s.print(px, y, &ks, kfg, bg);
                }
            }
        }
        // 還有沒顯示到的項目時給個提示,不然看起來像選單就這麼多。
        if (m.rows as usize) < m.items.len() {
            let y = h - 1;
            s.fill(0, y, w, 1, ' ', Color::BLUE, Color::LT_GRAY);
            s.print(2, y, &format!("{}/{}", m.cursor + 1, m.items.len()), Color::BLUE, Color::LT_GRAY);
            if m.top > 0 {
                s.print(w - 4, y, "^", Color::BLUE, Color::LT_GRAY);
            }
            if ((m.top + m.rows) as usize) < m.items.len() {
                s.print(w - 2, y, "v", Color::BLUE, Color::LT_GRAY);
            }
        }
    }

    /// 依選單格點的大小畫出選單。cols / rows 是選單字型下整個視窗裝得下幾格幾列。
    pub fn menu_layer(&mut self, cols: i32, rows: i32) -> MenuLayer {
        if !self.menu_bar || cols < 8 || rows < 3 {
            return MenuLayer::default();
        }
        let mut bar = Screen::new(cols, 1);
        self.draw_menu_bar(&mut bar);
        let mut l = MenuLayer { bar: Some(bar), ..Default::default() };
        if self.menu.active {
            let (w, h) = self.menu_box_size(cols, rows);
            let mut drop = Screen::new(w, h);
            self.draw_menu(&mut drop);
            l.drop_x = self.menu.x.min(cols - w).max(0);
            l.drop = Some(drop);
        }
        l
    }

    // --- MD5 / SFV ------------------------------------------------------------

    fn target_names(&mut self) -> Option<Vec<String>> {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案要先解出來");
            return None;
        }
        let names: Vec<String> = self.targets().into_iter().map(|t| t.0).collect();
        if names.is_empty() {
            self.message = t("沒有選到檔案");
            return None;
        }
        Some(names)
    }

    fn run_md5(&mut self) -> bool {
        let Some(names) = self.target_names() else { return true };
        let mut sb = String::new();
        for n in &names {
            match wincv_checksum::md5_file(&self.browser.dir.join(self.os_name(n))) {
                Ok(sum) => sb += &format!("{sum}  {n}\n"),
                Err(e) => sb += &format!("{n}  <{e}>\n"),
            }
        }
        self.show_text("MD5", &sb);
        true
    }

    fn run_make_sfv(&mut self) -> bool {
        let Some(names) = self.target_names() else { return true };
        let def = self.browser.dir.file_name().map(wincv_vfs::display_name).unwrap_or_default() + ".sfv";
        self.ask(&tr!("把 %d 個檔案的 CRC 寫進:", names.len()), &def, move |a, out| {
            if out.is_empty() {
                return;
            }
            let out = if out.to_lowercase().ends_with(".sfv") { out } else { out + ".sfv" };
            let dst = if Path::new(&out).is_absolute() { PathBuf::from(&out) } else { a.browser.dir.join(&out) };
            let entries = match wincv_checksum::make_sfv(&a.browser.dir, &names) {
                Ok(e) => e,
                Err(e) => {
                    a.message = t("算 CRC 失敗: ") + &e.to_string();
                    return;
                }
            };
            let base = dst.file_name().map(wincv_vfs::display_name).unwrap_or_default();
            let f = match std::fs::File::create(&dst) {
                Ok(f) => f,
                Err(e) => {
                    a.message = tr!("建不了 %s: %w", &base, e.to_string());
                    return;
                }
            };
            if let Err(e) = wincv_checksum::write_sfv(f, &entries) {
                a.message = t("寫不進去: ") + &e.to_string();
                return;
            }
            a.message = tr!("已寫入 %d 筆到 %s", entries.len(), &base);
            let _ = a.browser.reload();
            a.focus_on(&base);
        });
        true
    }

    fn run_verify_sfv(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案要先解出來");
            return true;
        }
        let Some(e) = self.browser.current().filter(|e| !e.is_dir && e.name.to_lowercase().ends_with(".sfv")) else {
            self.message = t("游標要停在 .sfv 檔上");
            return true;
        };
        let name = e.name.clone();
        let rs = match wincv_checksum::verify_sfv(&self.browser.dir.join(&e.os_name)) {
            Ok(r) => r,
            Err(e) => {
                self.message = e.to_string();
                return true;
            }
        };
        let (ok, bad, missing) = wincv_checksum::summary(&rs);
        let mut sb = tr!("%s:相符 %d,不符 %d,找不到 %d\n\n", &name, ok, bad, missing);
        for r in &rs {
            let mark = if r.missing {
                t("缺檔")
            } else if !r.ok {
                t("不符")
            } else {
                "OK  ".to_string()
            };
            sb += &format!("{mark}  {}  {}\n", r.want, r.name);
        }
        self.show_text(&t("SFV 驗證"), &sb);
        true
    }

    /// 把一段產生出來的報告丟進檢視器。它不是檔案,所以退出時要回瀏覽器。
    fn show_text(&mut self, title: &str, body: &str) {
        self.view_data = body.as_bytes().to_vec();
        self.view_raw = body.to_string();
        self.viewer = Some(wincv_viewer::Model::load(title, body.as_bytes(), Enc::Utf8));
        self.mode = Mode::Viewer;
    }
}
