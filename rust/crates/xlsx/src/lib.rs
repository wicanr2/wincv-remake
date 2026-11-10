//! 讀 Excel 的 .xlsx(SpreadsheetML)(對應 Go 版 `internal/xlsx`)。
//!
//! 一張工作表在字元格點上就是一個表格。要處理的不是版面而是**值**:
//! 試算表裡存的是數字與參照,人看到的字是「數字加上格式」算出來的。
//! 兩件事因此必須做:
//!
//!   - 共用字串表:儲存格寫的是編號,字在另一個組件裡。
//!   - 日期:Excel 的日期是一個天數,靠格式碼才知道它是日期不是數字。
//!     不還原的話一整欄日期會變成 45000 這種數字,而那是合法的顯示結果,
//!     不會有任何錯誤。
//!
//! 沒有做的是完整的數字格式引擎(千分位、會計格式、條件式格式碼)。
//! 那要把 Excel 的格式碼語言整套實作,而在字元格點上看一份試算表時,
//! 「數字本身是對的」比「逗號點在正確的位置」重要。

mod gonum;

#[cfg(test)]
mod tests;

use std::cell::RefCell;
use std::collections::HashMap;
use std::path::Path;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_ooxml::{self as ooxml, Decoder, Package, StartElement};

use gonum::{format_float, parse_float, Civil};

/// 一張表最多取這麼多列與欄。
///
/// 上限存在的理由是畫面而不是記憶體:一份三萬列的表格排進格點要花很久,
/// 而看的人最多往下捲幾百列。被截掉時會明講。
pub const MAX_ROWS: i64 = 4000;
pub const MAX_COLS: i64 = 128;

/// 一份打開著的活頁簿。
pub struct Book {
    pub title: String,
    pub sheets: Vec<Sheet>,

    pkg: Package,
    shared: Vec<String>,
    styles: Vec<CellStyle>,
    /// 日期的基準日(午夜)
    epoch: Civil,
    loaded: RefCell<HashMap<usize, Vec<Block>>>,
    wb_part: String,
    num_fmts: HashMap<i64, String>,
}

/// 一張工作表。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Sheet {
    pub name: String,
    pub part: String,
    pub hidden: bool,
}

#[derive(Clone, Debug, Default)]
struct CellStyle {
    num_fmt_id: i64,
    code: String,
}

impl Book {
    /// 打開一份 .xlsx。
    pub fn open(name: impl AsRef<Path>) -> Result<Book, String> {
        let p = Package::open(name)?;
        Book::new(p)
    }

    /// 從一個已經打開的 OPC 包建 Book。
    pub fn new(p: Package) -> Result<Book, String> {
        let wb = wb_part(&p);
        if wb.is_empty() {
            p.close();
            return Err(tr!("這不是 Excel 活頁簿(找不到 workbook.xml)"));
        }
        let mut b = Book {
            title: String::new(),
            sheets: Vec::new(),
            pkg: p,
            shared: Vec::new(),
            styles: Vec::new(),
            epoch: Civil::new(1899, 12, 30),
            loaded: RefCell::new(HashMap::new()),
            wb_part: wb,
            num_fmts: HashMap::new(),
        };
        b.read_workbook();
        b.read_shared();
        b.read_styles();
        if b.sheets.is_empty() {
            b.pkg.close();
            return Err(tr!("這份活頁簿沒有工作表"));
        }
        Ok(b)
    }

    pub fn close(&self) {
        self.pkg.close();
    }

    fn read_workbook(&mut self) {
        let Ok(raw) = self.pkg.bytes(&self.wb_part) else { return };
        let mut dec = ooxml::new_decoder(raw);
        match ooxml::root(&mut dec) {
            Ok(r) if r.name.local == "workbook" => {}
            _ => return,
        }
        let pkg = &self.pkg;
        let wb = &self.wb_part;
        let (epoch, sheets) = (&mut self.epoch, &mut self.sheets);
        let _ = ooxml::each(&mut dec, |d, se| {
            match se.name.local.as_str() {
                "workbookPr" => {
                    // [雷] 1904 制的活頁簿(Mac 上建的)日期基準差 1462 天。
                    // 照 1900 制解會整批早四年,而每一個日期看起來都是合法日期。
                    let v = ooxml::attr(se, "date1904");
                    if v == "1" || ooxml::xml::equal_fold(&v, "true") {
                        *epoch = Civil::new(1904, 1, 1);
                    }
                }
                "sheets" => {
                    return ooxml::each(d, |_, s| {
                        if s.name.local != "sheet" {
                            return Ok(false);
                        }
                        let t = pkg.rel_target(wb, &ooxml::rel_id(s));
                        if t.is_empty() || !pkg.has(&t) {
                            return Ok(false);
                        }
                        let state = ooxml::attr(s, "state");
                        sheets.push(Sheet {
                            name: ooxml::attr(s, "name"),
                            part: t,
                            hidden: !state.is_empty() && state != "visible",
                        });
                        Ok(false)
                    })
                    .map(|_| true);
                }
                _ => {}
            }
            Ok(false)
        });
    }

    fn read_shared(&mut self) {
        let part = self.rel_part("/sharedStrings");
        if part.is_empty() {
            return;
        }
        let Ok(raw) = self.pkg.bytes(&part) else { return };
        let mut dec = ooxml::new_decoder(raw);
        if ooxml::root(&mut dec).is_err() {
            return;
        }
        let shared = &mut self.shared;
        let _ = ooxml::each(&mut dec, |d, se| {
            if se.name.local != "si" {
                return Ok(false);
            }
            // 一個項目可以是單純的 <t>,也可以是好幾段帶樣式的 <r><t>。
            // 兩種都只要文字,接起來就是。
            shared.push(ooxml::text(d));
            Ok(true)
        });
    }

    fn read_styles(&mut self) {
        let part = self.rel_part("/styles");
        if part.is_empty() {
            return;
        }
        let Ok(raw) = self.pkg.bytes(&part) else { return };
        let mut dec = ooxml::new_decoder(raw);
        if ooxml::root(&mut dec).is_err() {
            return;
        }
        let (num_fmts, styles) = (&mut self.num_fmts, &mut self.styles);
        let _ = ooxml::each(&mut dec, |d, se| match se.name.local.as_str() {
            "numFmts" => ooxml::each(d, |_, n| {
                if n.name.local == "numFmt" {
                    num_fmts.insert(atoi_def(&ooxml::attr(n, "numFmtId"), -1), ooxml::attr(n, "formatCode"));
                }
                Ok(false)
            })
            .map(|_| true),
            "cellXfs" => ooxml::each(d, |_, x| {
                if x.name.local == "xf" {
                    styles.push(CellStyle { num_fmt_id: atoi_def(&ooxml::attr(x, "numFmtId"), 0), code: String::new() });
                }
                Ok(false)
            })
            .map(|_| true),
            _ => Ok(false),
        });
        for s in &mut self.styles {
            s.code = self.num_fmts.get(&s.num_fmt_id).cloned().unwrap_or_default();
        }
    }

    fn rel_part(&self, typ: &str) -> String {
        self.pkg.rels_by_type(&self.wb_part, typ).into_iter().find(|t| self.pkg.has(t)).unwrap_or_default()
    }

    /// 排出第 i 張工作表。
    pub fn blocks(&self, i: usize) -> Vec<Block> {
        if i >= self.sheets.len() {
            return Vec::new();
        }
        if let Some(got) = self.loaded.borrow().get(&i) {
            return got.clone();
        }
        let out = self.sheet_blocks(&self.sheets[i]);
        self.loaded.borrow_mut().insert(i, out.clone());
        out
    }

    fn sheet_blocks(&self, sh: &Sheet) -> Vec<Block> {
        let (rows, more) = self.read_sheet(&sh.part);
        let mut out = vec![Block { kind: Kind::Heading, level: 1, spans: vec![span(sh.name.clone())], ..Default::default() }];
        if rows.is_empty() {
            out.push(Block { kind: Kind::Para, spans: vec![span(tr!("(這張工作表是空的)"))], ..Default::default() });
            return out;
        }
        out.push(Block { kind: Kind::Table, rows, ..Default::default() });
        if more {
            out.push(Block {
                kind: Kind::Para,
                spans: vec![Span { text: tr!("(只顯示前 %d 列 × %d 欄)", MAX_ROWS, MAX_COLS), style: Style::ITALIC, ..Default::default() }],
                ..Default::default()
            });
        }
        out
    }

    /// 讀一張工作表的儲存格,回傳補齊成矩形的內容。
    fn read_sheet(&self, part: &str) -> (Vec<Vec<String>>, bool) {
        let Ok(raw) = self.pkg.bytes(part) else { return (Vec::new(), false) };
        let mut dec = ooxml::new_decoder(raw);
        match ooxml::root(&mut dec) {
            Ok(r) if r.name.local == "worksheet" => {}
            _ => return (Vec::new(), false),
        }
        let mut cells: HashMap<i64, HashMap<i64, String>> = HashMap::new();
        let (mut max_row, mut max_col) = (0i64, 0i64);
        let mut truncated = false;

        let _ = ooxml::each(&mut dec, |d, se| {
            if se.name.local != "sheetData" {
                return Ok(false);
            }
            let mut row_n = 0i64;
            ooxml::each(d, |d, r| {
                if r.name.local != "row" {
                    return Ok(false);
                }
                row_n = atoi_def(&ooxml::attr(r, "r"), row_n.wrapping_add(1));
                if row_n > MAX_ROWS {
                    truncated = true;
                    return Ok(false);
                }
                let mut col_n = 0i64;
                ooxml::each(d, |d, c| {
                    if c.name.local != "c" {
                        return Ok(false);
                    }
                    let rf = ooxml::attr(c, "r");
                    if !rf.is_empty() {
                        col_n = col_of(&rf);
                    } else {
                        col_n = col_n.wrapping_add(1);
                    }
                    if col_n > MAX_COLS {
                        truncated = true;
                        return Ok(false);
                    }
                    let v = self.cell_value(d, c);
                    if v.is_empty() {
                        return Ok(true);
                    }
                    cells.entry(row_n).or_default().insert(col_n, v);
                    max_row = max_row.max(row_n);
                    max_col = max_col.max(col_n);
                    Ok(true)
                })
                .map(|_| true)
            })
            .map(|_| true)
        });

        if max_row == 0 || max_col == 0 {
            return (Vec::new(), truncated);
        }
        let mut out = Vec::with_capacity(max_row as usize);
        for r in 1..=max_row {
            let row = cells.get(&r);
            out.push((1..=max_col).map(|c| row.and_then(|m| m.get(&c)).cloned().unwrap_or_default()).collect());
        }
        (out, truncated)
    }

    /// 算出一個儲存格顯示出來是什麼。
    fn cell_value(&self, dec: &mut Decoder, c: &StartElement) -> String {
        let typ = ooxml::attr(c, "t");
        let style_idx = atoi_def(&ooxml::attr(c, "s"), -1);
        let (mut v, mut inline) = (String::new(), String::new());
        let _ = ooxml::each(dec, |d, inn| match inn.name.local.as_str() {
            "v" => {
                v = ooxml::text(d);
                Ok(true)
            }
            "is" => {
                inline = ooxml::text(d);
                Ok(true)
            }
            _ => Ok(false),
        });
        match typ.as_str() {
            "s" => match v.trim().parse::<i64>() {
                Ok(n) if n >= 0 && (n as u64) < self.shared.len() as u64 => self.shared[n as usize].clone(),
                _ => String::new(),
            },
            "inlineStr" => inline,
            "str" | "e" | "d" => v,
            "b" => {
                if v.trim() == "1" {
                    "TRUE".to_string()
                } else {
                    "FALSE".to_string()
                }
            }
            _ => self.format_number(&v, style_idx),
        }
    }

    /// 把數值換成看得懂的字。
    fn format_number(&self, v: &str, style_idx: i64) -> String {
        let s = v.trim();
        if s.is_empty() {
            return String::new();
        }
        let Some(f) = parse_float(s) else { return s.to_string() };
        let (mut code, mut fmt_id) = ("", 0i64);
        if style_idx >= 0 && (style_idx as u64) < self.styles.len() as u64 {
            let st = &self.styles[style_idx as usize];
            code = &st.code;
            fmt_id = st.num_fmt_id;
        }
        if is_date_format(fmt_id, code) {
            return self.serial_to_date(f, fmt_id, code);
        }
        if strip_quoted(code).contains(&b'%') {
            return format_float(f * 100.0) + "%";
        }
        format_float(f)
    }

    /// 把 Excel 的日期序號換成日期。
    fn serial_to_date(&self, f: f64, id: i64, code: &str) -> String {
        // [雷] 1900 制把 1900 年當成閏年(那是 Lotus 1-2-3 留下來的錯,
        // Excel 為了相容照抄)。基準取 1899-12-30 就把那一天的偏移吸收掉,
        // 序號 60 之後全部對得上。
        // 超出 Excel 日期範圍的序號當成一般數字。硬換算會得到
        // 一個看起來合法但毫無意義的年份。
        // NaN 在 Go 版會通過這道檢查(比較全是 false),接著 int(NaN) 在 amd64 上是
        // int64 的最小值,算出一個依平台而異的日期(見報告)。這裡當成一般數字。
        if f < 0.0 || f > 2958465.0 || f.is_nan() {
            return format_float(f);
        }
        let days = f.trunc();
        let frac = f - days;
        // Go 的 time.Duration(frac * 24 * float64(time.Hour)):浮點數轉整數直接截斷。
        let ns = (frac * 24.0 * 3_600_000_000_000.0) as i64;
        let t = self.epoch.add_days(days as i64).add_nanos(ns);
        let c = strip_quoted(code);
        let time_only = (18..=21).contains(&id) || id == 45 || id == 46 || id == 47;
        let has_time = contains_any(&c, b"hHsS") || time_only;
        let has_date = contains_any(&c, b"yYdD") || (14..=17).contains(&id) || id == 22;
        if time_only && !has_date {
            t.format_time()
        } else if (has_time && has_date) || id == 22 {
            format!("{} {}", t.format_date(), t.format_time())
        } else {
            t.format_date()
        }
    }
}

fn span(text: String) -> Span {
    Span { text, ..Default::default() }
}

fn wb_part(p: &Package) -> String {
    if let Some(t) = p.rels_by_type("", "/officeDocument").into_iter().find(|t| p.has(t)) {
        return t;
    }
    if p.has("xl/workbook.xml") {
        return "xl/workbook.xml".to_string();
    }
    String::new()
}

fn contains_any(s: &[u8], chars: &[u8]) -> bool {
    s.iter().any(|c| chars.contains(c))
}

/// 判斷一個格式碼是不是日期。
///
/// 內建編號要寫死:14–22 與 45–47 是 Excel 保留給日期時間的,
/// 檔案裡不會附格式碼。自訂格式則看碼裡有沒有日期欄位字母,
/// 但要先把引號裡的文字拿掉:"May" 裡的 y 不是年份。
fn is_date_format(id: i64, code: &str) -> bool {
    if (14..=22).contains(&id) || (45..=47).contains(&id) {
        return true;
    }
    contains_any(&strip_quoted(code), b"yYdDhHsS")
}

/// 去掉格式碼裡不是欄位的部分:引號括起來的字面文字、
/// 反斜線跳脫的單一字元,以及中括號裡的顏色與地區設定。
///
/// 少了這一步會誤判:`"May"` 裡的 y 不是年份,`[$-409]` 裡的 4 不是欄位。
/// 回傳位元組:Go 版逐位元組走,反斜線可能跳過多位元組字的第一個位元組。
fn strip_quoted(code: &str) -> Vec<u8> {
    let b = code.as_bytes();
    let mut sb = Vec::with_capacity(b.len());
    let (mut in_quote, mut in_bracket) = (false, false);
    let mut i = 0;
    while i < b.len() {
        let c = b[i];
        if c == b'"' {
            in_quote = !in_quote;
        } else if in_quote {
        } else if c == b'[' {
            in_bracket = true;
        } else if c == b']' {
            in_bracket = false;
        } else if in_bracket {
        } else if c == b'\\' {
            i += 1;
        } else {
            sb.push(c);
        }
        i += 1;
    }
    sb
}

/// 從 A1 式參照算出欄號,從 1 起算。
pub fn col_of(rf: &str) -> i64 {
    let mut n: i64 = 0;
    for &ch in rf.as_bytes() {
        let v = match ch {
            b'A'..=b'Z' => (ch - b'A') as i64 + 1,
            b'a'..=b'z' => (ch - b'a') as i64 + 1,
            _ => return n,
        };
        n = n.wrapping_mul(26).wrapping_add(v);
    }
    n
}

fn atoi_def(s: &str, def: i64) -> i64 {
    s.trim().parse::<i64>().unwrap_or(def)
}
