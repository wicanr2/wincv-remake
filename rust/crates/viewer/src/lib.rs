//! 文字檢視(對應 Go 版 `internal/viewer`):編碼判讀、ANSI 色碼、
//! 自動換行、搜尋、光棒。

use wincv_cell::{is_wide, Color, Screen};
use wincv_textenc::{self as textenc, Enc};

/// 同一種顏色的一段文字。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Span {
    pub text: String,
    pub fg: Color,
    pub bg: Color,
}

/// 一行(換行之前)。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Line {
    pub spans: Vec<Span>,
}

impl Line {
    /// 去掉顏色的純文字。
    pub fn text(&self) -> String {
        self.spans.iter().map(|s| s.text.as_str()).collect()
    }

    /// 顯示格數(全形算兩格)。
    pub fn width(&self) -> i32 {
        self.spans.iter().flat_map(|s| s.text.chars()).map(|r| if is_wide(r) { 2 } else { 1 }).sum()
    }
}

/// 文字檢視的配色。
#[derive(Clone, Copy, Debug)]
pub struct Theme {
    pub fg: Color,
    pub bg: Color,
    pub status_fg: Color,
    pub status_bg: Color,
    /// 搜尋命中。
    pub hit_fg: Color,
    pub hit_bg: Color,
    /// 游標所在那一列(光棒)。
    pub bar_fg: Color,
    pub bar_bg: Color,
}

impl Default for Theme {
    fn default() -> Theme {
        Theme {
            fg: Color::LT_GRAY,
            bg: Color::BLACK,
            status_fg: Color::BLACK,
            status_bg: Color::LT_GRAY,
            hit_fg: Color::BLACK,
            hit_bg: Color::YELLOW,
            bar_fg: Color::WHITE,
            bar_bg: Color::BLUE,
        }
    }
}

/// 一份打開的文字檔。
#[derive(Clone, Debug)]
pub struct Model {
    pub name: String,
    pub enc: Enc,
    pub lines: Vec<Line>,
    /// 畫面第一列對應的行號。
    pub top: i32,
    /// 不換行時的水平捲動量(格)。
    pub left: i32,
    /// 游標(光棒)所在的行號。
    pub cur: i32,
    /// 自動換行(原版 W)。
    pub wrap: bool,
    /// 解讀 ANSI 色碼(原版 A)。
    pub ansi: bool,
    /// 光棒:整列反白標出游標在哪一行。
    pub bar: bool,
    pub theme: Theme,
    pub hits: Vec<i32>,
    pub hit_idx: i32,
    pub pattern: String,
}

impl Model {
    /// 載入。`enc_hint` 是 Unknown 時自動判讀。
    pub fn load(name: &str, data: &[u8], enc_hint: Enc) -> Model {
        let enc = if enc_hint == Enc::Unknown { textenc::detect(data) } else { enc_hint };
        let theme = Theme::default();
        Model {
            name: name.to_string(),
            enc,
            lines: parse(&textenc::decode(data, enc), true, &theme),
            top: 0,
            left: 0,
            cur: 0,
            wrap: false,
            ansi: true,
            bar: true,
            theme,
            hits: Vec::new(),
            hit_idx: 0,
            pattern: String::new(),
        }
    }

    /// 開關 ANSI 色碼。`raw` 是解碼後的原文。
    pub fn set_ansi(&mut self, on: bool, raw: &str) {
        self.ansi = on;
        self.lines = parse(raw, on, &self.theme);
    }

    fn max_line(&self) -> i32 {
        (self.lines.len() as i32 - 1).max(0)
    }

    fn clamp(&mut self) {
        let max = self.max_line();
        self.top = self.top.clamp(0, max);
        self.cur = self.cur.clamp(0, max);
        self.left = self.left.max(0);
    }

    /// 捲到看得見游標。
    fn reveal(&mut self, rows: i32) {
        let rows = rows.max(1);
        if self.cur < self.top {
            self.top = self.cur;
        }
        if self.cur > self.top + rows - 1 {
            self.top = self.cur - rows + 1;
        }
        self.top = self.top.max(0);
    }

    /// 游標上下移動,走到畫面外才捲動。
    pub fn move_by(&mut self, d: i32, rows: i32) {
        self.cur += d;
        self.clamp();
        self.reveal(rows);
    }

    /// 翻頁,游標在畫面上的相對位置不變。
    pub fn page_by(&mut self, pages: i32, rows: i32) {
        let rows = rows.max(1);
        let off = self.cur - self.top;
        self.top = (self.top + pages * rows).max(0);
        self.cur = self.top + off;
        self.clamp();
        self.reveal(rows);
    }

    /// 畫面捲動 d 行,游標被推著留在畫面內。
    pub fn scroll_by(&mut self, d: i32, rows: i32) {
        self.top += d;
        self.clamp();
        let rows = rows.max(1);
        if self.cur < self.top {
            self.cur = self.top;
        }
        if self.cur > self.top + rows - 1 {
            self.cur = self.top + rows - 1;
        }
        self.clamp();
    }

    pub fn home(&mut self) {
        self.top = 0;
        self.left = 0;
        self.cur = 0;
        self.clamp();
    }

    pub fn end(&mut self, rows: i32) {
        self.cur = self.lines.len() as i32 - 1;
        self.top = self.lines.len() as i32 - rows;
        self.clamp();
        self.reveal(rows);
    }

    /// 跳到第 n 行(0 起算),放在畫面中間。
    pub fn go_to_line(&mut self, n: i32, rows: i32) {
        self.cur = n;
        self.top = n - rows / 2;
        self.clamp();
        self.reveal(rows);
    }

    /// 不分大小寫搜尋,跳到第一個命中。回傳命中的行數。
    pub fn search(&mut self, pattern: &str, rows: i32) -> usize {
        self.pattern = pattern.to_string();
        self.hits.clear();
        self.hit_idx = 0;
        if pattern.is_empty() {
            return 0;
        }
        let low = go_lower(pattern);
        self.hits = (0..self.lines.len()).filter(|&i| go_lower(&self.lines[i].text()).contains(&low)).map(|i| i as i32).collect();
        if let Some(&h) = self.hits.first() {
            self.go_to_line(h, rows);
        }
        self.hits.len()
    }

    pub fn next_hit(&mut self, rows: i32) {
        if self.hits.is_empty() {
            return;
        }
        self.hit_idx = (self.hit_idx + 1) % self.hits.len() as i32;
        self.go_to_line(self.hits[self.hit_idx as usize], rows);
    }

    pub fn prev_hit(&mut self, rows: i32) {
        if self.hits.is_empty() {
            return;
        }
        let n = self.hits.len() as i32;
        self.hit_idx = (self.hit_idx - 1 + n) % n;
        self.go_to_line(self.hits[self.hit_idx as usize], rows);
    }

    /// 畫整個畫面,回傳內容區的列數。
    pub fn draw(&mut self, s: &mut Screen) -> i32 {
        let t = self.theme;
        s.clear(t.fg, t.bg);
        let rows = (s.rows - 1).max(0); // 最後一列是狀態列
        self.clamp();
        let mut y = 0;
        let mut i = self.top;
        while (i as usize) < self.lines.len() && y < rows {
            let used = self.draw_line(s, y, rows, &self.lines[i as usize]);
            if self.bar && i == self.cur {
                for dy in 0..used {
                    if y + dy >= rows {
                        break;
                    }
                    s.set_attr(0, y + dy, s.cols, t.bar_fg, t.bar_bg);
                }
            }
            y += used;
            i += 1;
        }
        self.draw_status(s);
        rows
    }

    fn draw_line(&self, s: &mut Screen, y: i32, rows: i32, l: &Line) -> i32 {
        let mut buf = [0u8; 4];
        if !self.wrap {
            let mut x = -self.left;
            for sp in &l.spans {
                for r in sp.text.chars() {
                    let w = if is_wide(r) { 2 } else { 1 };
                    if x >= 0 && x + w <= s.cols {
                        s.print(x, y, r.encode_utf8(&mut buf), sp.fg, sp.bg);
                    }
                    x += w;
                    if x >= s.cols {
                        break;
                    }
                }
            }
            return 1;
        }
        let (mut x, mut y, mut used) = (0, y, 1);
        for sp in &l.spans {
            for r in sp.text.chars() {
                let w = if is_wide(r) { 2 } else { 1 };
                if x + w > s.cols {
                    x = 0;
                    y += 1;
                    used += 1;
                    if y >= rows {
                        return used;
                    }
                }
                s.print(x, y, r.encode_utf8(&mut buf), sp.fg, sp.bg);
                x += w;
            }
        }
        used
    }

    fn draw_status(&self, s: &mut Screen) {
        let t = self.theme;
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', t.status_fg, t.status_bg);
        let n = self.lines.len() as i32;
        let pct = if n > 0 { (self.cur + 1) * 100 / n } else { 100 };
        let mut left = format!("{}  [{}]", self.name, wincv_i18n::t(self.enc.name()));
        if self.wrap {
            left.push_str(&wincv_i18n::t("  自動換行"));
        }
        if !self.ansi {
            left.push_str("  ANSI off");
        }
        s.print(0, y, &left, t.status_fg, t.status_bg);
        let mut right = format!("{}/{}  {pct}%", self.cur + 1, n);
        if !self.hits.is_empty() {
            right = format!("{}/{}{}{right}", self.hit_idx + 1, self.hits.len(), wincv_i18n::t(" 命中  "));
        }
        // Go 版用位元組長度算位置,右側含中文(「命中」)時會偏;這裡用顯示格數。
        let x = s.cols - display_width(&right);
        if x >= 0 {
            s.print(x, y, &right, t.status_fg, t.status_bg);
        }
    }
}

fn display_width(s: &str) -> i32 {
    s.chars().map(|r| if is_wide(r) { 2 } else { 1 }).sum()
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn go_lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

fn is_final(c: u8) -> bool {
    (0x40..=0x7E).contains(&c)
}

/// 去掉 ANSI 控制序列。沒有結束字元的序列連同後面全部丟掉(與 Go 版相同)。
fn strip_ansi(s: &str) -> String {
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        if b[i] == 0x1B && i + 1 < b.len() && b[i + 1] == b'[' {
            let mut j = i + 2;
            while j < b.len() && !is_final(b[j]) {
                j += 1;
            }
            i = j + 1;
            continue;
        }
        out.push(b[i]);
        i += 1;
    }
    // 控制序列只含 ASCII,切的位置都在字元邊界上。
    String::from_utf8(out).unwrap_or_default()
}

/// 拆行並解讀 ANSI 色碼。CR LF 與單獨的 CR 都算換行;檔尾的空行不算一行。
fn parse(s: &str, ansi: bool, th: &Theme) -> Vec<Line> {
    let s = s.replace("\r\n", "\n").replace('\r', "\n");
    let mut raw: Vec<&str> = s.split('\n').collect();
    if raw.last() == Some(&"") {
        raw.pop();
    }
    let (mut fg, mut bg) = (th.fg, th.bg);
    let mut out = Vec::with_capacity(raw.len());
    for r in raw {
        if !ansi {
            out.push(Line { spans: vec![Span { text: strip_ansi(r), fg: th.fg, bg: th.bg }] });
            continue;
        }
        let b = r.as_bytes();
        let mut line = Line::default();
        let mut sb: Vec<u8> = Vec::new();
        let flush = |sb: &mut Vec<u8>, line: &mut Line, fg, bg| {
            if !sb.is_empty() {
                line.spans.push(Span { text: String::from_utf8(std::mem::take(sb)).unwrap_or_default(), fg, bg });
            }
        };
        let mut i = 0;
        while i < b.len() {
            if b[i] == 0x1B && i + 1 < b.len() && b[i + 1] == b'[' {
                let mut j = i + 2;
                while j < b.len() && !is_final(b[j]) {
                    j += 1;
                }
                if j < b.len() {
                    if b[j] == b'm' {
                        flush(&mut sb, &mut line, fg, bg);
                        (fg, bg) = apply_sgr(&r[i + 2..j], fg, bg, th);
                    }
                    i = j + 1;
                    continue;
                }
            }
            sb.push(b[i]);
            i += 1;
        }
        flush(&mut sb, &mut line, fg, bg);
        if line.spans.is_empty() {
            line.spans.push(Span { text: String::new(), fg, bg });
        }
        out.push(line);
    }
    out
}

const ANSI_DIM: [Color; 8] = [Color::BLACK, Color::RED, Color::GREEN, Color::YELLOW, Color::BLUE, Color::MAGENTA, Color::CYAN, Color::LT_GRAY];
const ANSI_BRIGHT: [Color; 8] = [Color::DK_GRAY, Color::LT_RED, Color::LT_GREEN, Color::LT_YELLOW, Color::LT_BLUE, Color::LT_MAGENTA, Color::LT_CYAN, Color::WHITE];

fn ansi_to_cell(n: i32, bright: bool) -> Color {
    if !(0..=7).contains(&n) {
        return Color::LT_GRAY;
    }
    if bright { ANSI_BRIGHT[n as usize] } else { ANSI_DIM[n as usize] }
}

/// 套用一個 SGR 序列。「1」是亮色,不是粗體:BBS 的 ANSI 圖就是這樣用的。
fn apply_sgr(params: &str, mut fg: Color, mut bg: Color, th: &Theme) -> (Color, Color) {
    let params = if params.is_empty() { "0" } else { params };
    let (mut base, mut bright) = (-1, false);
    for p in params.split(';') {
        let Ok(n) = go_atoi(p) else { continue };
        match n {
            0 => {
                (base, bright, bg) = (-1, false, th.bg);
                fg = th.fg;
            }
            1 => bright = true,
            22 => bright = false,
            30..=37 => base = n - 30,
            39 => {
                (base, bright) = (-1, false);
                fg = th.fg;
            }
            40..=47 => bg = ansi_to_cell(n - 40, false),
            49 => bg = th.bg,
            90..=97 => (base, bright) = (n - 90, true),
            100..=107 => bg = ansi_to_cell(n - 100, true),
            _ => {}
        }
    }
    if base >= 0 {
        fg = ansi_to_cell(base, bright);
    } else if bright {
        if let Some(i) = ANSI_DIM.iter().position(|&c| c == fg) {
            fg = ANSI_BRIGHT[i];
        }
    }
    (fg, bg)
}

/// Go 的 strconv.Atoi:可帶正負號的十進位,不接受空字串與空白。
fn go_atoi(s: &str) -> Result<i32, ()> {
    let d = s.strip_prefix(['+', '-']).unwrap_or(s);
    if d.is_empty() || !d.bytes().all(|c| c.is_ascii_digit()) {
        return Err(());
    }
    s.parse::<i64>().ok().and_then(|v| i32::try_from(v).ok()).ok_or(())
}

#[cfg(test)]
mod tests;
