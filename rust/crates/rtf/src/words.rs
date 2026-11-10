//! 控制字與字碼頁(對應 Go 版 `words.go`)。

use encoding_rs::{Encoding, BIG5, EUC_KR, GBK, SHIFT_JIS};
use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Style};

use crate::cp_tables as t;
use crate::{Dest, Parser};

impl Parser<'_> {
    /// 處理一個控制字。
    pub(crate) fn word(&mut self, w: &str, num: i64, has_num: bool) {
        let on = !has_num || num != 0;
        match w {
            // --- 文件層級 ---
            "ansicpg" => {
                let cp = valid_cp(num);
                if cp != 0 {
                    self.cur.cp = cp;
                    self.raw_cp = cp;
                }
            }
            "mac" => self.cur.cp = 10000,
            "pc" => self.cur.cp = 437,
            "pca" => self.cur.cp = 850,

            // --- 要丟掉的目的地 ---
            "colortbl" | "info" | "generator" | "themedata" | "colorschememapping" | "latentstyles" | "datastore"
            | "xmlnstbl" | "listtable" | "listoverridetable" | "rsidtbl" | "mmathPr" | "objdata" | "result"
            | "nonshppict" | "pnseclvl" | "header" | "footer" | "headerl" | "headerr" | "headerf" | "footerl"
            | "footerr" | "footerf" | "filetbl" | "revtbl" | "upr" | "annotation" | "atnid" | "atnauthor" => {
                self.cur.dest = Dest::Skip;
            }
            "fonttbl" => {
                self.cur.dest = Dest::FontTbl;
                self.font_cp.clear();
            }
            "stylesheet" => {
                self.cur.dest = Dest::StyleSheet;
                self.styles.clear();
            }
            "pict" => {
                self.cur.dest = Dest::Pict;
                self.pic_hex.clear();
                self.pic_bin.clear();
                self.pic_kind.clear();
            }
            "listtext" | "pntext" => {
                self.cur.dest = Dest::ListText;
                self.push_sink();
            }
            "fldinst" => {
                self.cur.dest = Dest::FldInst;
                self.instr.clear();
            }
            "footnote" => {
                self.foot_n += 1;
                let s = tr!("[註 %d]", self.foot_n);
                self.emit_text(&s);
                self.cur.dest = Dest::Footnote;
                self.push_sink();
            }

            // --- 字型與字碼頁 ---
            "f" => {
                if self.cur.dest == Dest::FontTbl {
                    self.cur_font = num;
                    return;
                }
                if let Some(&cp) = self.font_cp.get(&num) {
                    if cp != 0 {
                        self.cur.cp = cp;
                    }
                }
            }
            "fcharset" => {
                if self.cur.dest == Dest::FontTbl {
                    self.font_cp.insert(self.cur_font, charset_cp(num));
                }
            }
            "cpg" => {
                if self.cur.dest == Dest::FontTbl {
                    let cp = valid_cp(num);
                    if cp != 0 {
                        self.font_cp.insert(self.cur_font, cp);
                    }
                }
            }
            "uc" => {
                if (0..=32).contains(&num) {
                    self.cur.uc = num;
                }
            }
            "u" => self.unicode(num),

            // --- 字元格式 ---
            "plain" => self.cur.style = Style::default(),
            "b" => self.set_style(Style::BOLD, on),
            "i" => self.set_style(Style::ITALIC, on),
            "strike" | "striked" => self.set_style(Style::STRIKE, on),
            "v" => {
                // 隱藏文字。索引項目與目錄欄位靠它藏東西,顯示出來是雜訊。
                if on {
                    self.cur.dest = Dest::Skip;
                }
            }

            // --- 段落 ---
            "par" | "line" | "sect" => self.end_para(),
            "pard" => {
                self.cur.style_num = 0;
                self.cur.outline = 0;
                self.cur.in_table = false;
                self.in_list = false;
                self.list_level = 0;
                self.list_mark.clear();
            }
            "page" => {
                self.end_para();
                self.emit_block(Block { kind: Kind::Rule, ..Default::default() });
            }
            "s" => {
                if self.cur.dest == Dest::StyleSheet {
                    self.style_cur = num;
                    return;
                }
                self.cur.style_num = num;
            }
            "ls" => self.in_list = true,
            "ilvl" => self.list_level = num.clamp(0, 8),
            "outlinelevel" => {
                if has_num && (0..=8).contains(&num) {
                    self.cur.outline = num + 1;
                }
            }

            // --- 表格 ---
            "intbl" | "trowd" => self.cur.in_table = true,
            "cell" | "nestcell" => self.end_cell(),
            "row" | "nestrow" => self.end_row(),

            // --- 特殊字元 ---
            "tab" => self.emit_text("    "),
            "emdash" => self.emit_text("—"),
            "endash" => self.emit_text("–"),
            "lquote" => self.emit_text("‘"),
            "rquote" => self.emit_text("’"),
            "ldblquote" => self.emit_text("“"),
            "rdblquote" => self.emit_text("”"),
            "bullet" => self.emit_text("•"),
            "chftn" => {
                // 註腳編號的自動欄位。標記已經由 \footnote 那邊放好了。
            }
            "bin" => self.binary(num),

            // --- 圖片格式 ---
            "pngblip" => self.pic_kind = "png".to_string(),
            "jpegblip" => self.pic_kind = "jpg".to_string(),
            "emfblip" => self.pic_kind = "emf".to_string(),
            "wmetafile" => self.pic_kind = "wmf".to_string(),
            "dibitmap" | "wbitmap" => self.pic_kind = "bmp".to_string(),
            _ => {}
        }
    }

    fn set_style(&mut self, s: Style, on: bool) {
        if on {
            self.cur.style |= s;
        } else {
            self.cur.style = Style(self.cur.style.0 & !s.0);
        }
    }

    /// 處理 \uN。
    ///
    /// [雷] 數值大於 32767 的字碼是用負數寫的(16 位元有號數溢位)。
    /// 不還原的話會拿到一個負的字碼,而 Go 的 string(rune) 會安靜地
    /// 給出一個替換字元:每個罕用字都變成問號,但不會有任何錯誤。
    fn unicode(&mut self, n: i64) {
        let n = if n < 0 { n.wrapping_add(0x10000) } else { n };
        // Go 的 rune(n):int 轉 int32 直接截斷。
        let r = n as i32;
        let is_surrogate = (0xD800..0xE000).contains(&r);
        if is_surrogate && self.high_surrogate == 0 {
            self.high_surrogate = r;
        } else if self.high_surrogate != 0 {
            let s = decode_rune(self.high_surrogate, r).to_string();
            self.emit_text(&s);
            self.high_surrogate = 0;
        } else {
            let s = go_rune(r).to_string();
            self.emit_text(&s);
        }
        self.skip = self.cur.uc;
    }

    /// 跳過 \binN 後面那一段原始位元組。
    fn binary(&mut self, n: i64) {
        // Go 版的 p.i+n 在 n 接近 int 上限時會溢位成負數,之後切片 panic(見報告);
        // 這裡用不會溢位的比法。
        if n <= 0 || n as u64 > (self.src.len() - self.i) as u64 {
            return;
        }
        let n = n as usize;
        let data = &self.src[self.i..self.i + n];
        self.i += n;
        if self.cur.dest == Dest::Pict {
            self.pic_bin.extend_from_slice(data);
        }
    }
}

/// Go 的 `utf16.DecodeRune`。
fn decode_rune(r1: i32, r2: i32) -> char {
    if (0xD800..0xDC00).contains(&r1) && (0xDC00..0xE000).contains(&r2) {
        let c = (((r1 - 0xD800) << 10) | (r2 - 0xDC00)) + 0x10000;
        return char::from_u32(c as u32).unwrap_or('\u{FFFD}');
    }
    '\u{FFFD}'
}

/// Go 的 `string(rune(r))`:不合法的碼位(負數、代理區、超過 U+10FFFF)給 U+FFFD。
fn go_rune(r: i32) -> char {
    if r < 0 {
        return '\u{FFFD}';
    }
    char::from_u32(r as u32).unwrap_or('\u{FFFD}')
}

// --- 字碼頁 ---

/// 把 \fcharset 的字集編號換成字碼頁。
///
/// 這張表是這一包能不能讀中文的關鍵:一份繁體中文的 RTF 只在字型表裡
/// 寫 `\fcharset136`,正文全部是 `\'hh`。查不到就會照 cp1252 解,
/// 得到一串拉丁字母:那是合法的字串,不會有任何錯誤訊息。
fn charset_cp(n: i64) -> i64 {
    match n {
        0 => 1252,
        2 => 0, // Symbol:沒有對應的字碼頁,交回預設
        77 => 10000,
        128 => 932, // 日文
        129 => 949, // 韓文
        130 => 1361,
        134 => 936, // 簡體中文
        136 => 950, // 繁體中文
        161 => 1253,
        162 => 1254,
        163 => 1258,
        177 => 1255,
        178 => 1256,
        186 => 1257,
        204 => 1251,
        222 => 874,
        238 => 1250,
        254 => 437,
        255 => 850,
        _ => 0,
    }
}

fn valid_cp(n: i64) -> i64 {
    if !matches!(enc_for(n), Codec::None) || n == 65001 {
        return n;
    }
    0
}

/// 一個字碼頁的解碼方式。
enum Codec {
    /// 多位元組:encoding_rs(WHATWG)。
    Multi(&'static Encoding),
    /// 單位元組:x/text 產生的表。
    Single(&'static [char; 128]),
    /// 不認得,照 cp1252 解。
    None,
}

fn enc_for(cp: i64) -> Codec {
    match cp {
        950 => Codec::Multi(BIG5),
        936 => Codec::Multi(GBK),
        932 => Codec::Multi(SHIFT_JIS),
        949 => Codec::Multi(EUC_KR),
        1250 => Codec::Single(&t::CP1250),
        1251 => Codec::Single(&t::CP1251),
        1252 => Codec::Single(&t::CP1252),
        1253 => Codec::Single(&t::CP1253),
        1254 => Codec::Single(&t::CP1254),
        1255 => Codec::Single(&t::CP1255),
        1256 => Codec::Single(&t::CP1256),
        1257 => Codec::Single(&t::CP1257),
        1258 => Codec::Single(&t::CP1258),
        874 => Codec::Single(&t::CP874),
        437 => Codec::Single(&t::CP437),
        850 => Codec::Single(&t::CP850),
        10000 => Codec::Single(&t::CP10000),
        _ => Codec::None,
    }
}

/// Go 的字串可以裝不合法的 UTF-8;這裡每個壞位元組換成一個 U+FFFD,
/// 與 Go 逐字走訪時看到的字數相同。
fn utf8_lossy_go(b: &[u8]) -> String {
    let mut out = String::with_capacity(b.len());
    let mut rest = b;
    loop {
        match std::str::from_utf8(rest) {
            Ok(s) => {
                out.push_str(s);
                return out;
            }
            Err(e) => {
                let (good, bad) = rest.split_at(e.valid_up_to());
                out.push_str(std::str::from_utf8(good).unwrap_or_default());
                out.push('\u{FFFD}');
                rest = &bad[1..];
            }
        }
    }
}

pub(crate) fn decode(b: &[u8], cp: i64) -> String {
    if cp == 65001 {
        return utf8_lossy_go(b);
    }
    let table = match enc_for(cp) {
        Codec::Multi(e) => return e.decode_without_bom_handling(b).0.into_owned(),
        Codec::Single(t) => t,
        Codec::None => &t::CP1252,
    };
    // Go 版在解碼器回報錯誤時改照 Latin-1 逐位元組解;x/text 的單位元組
    // 解碼器遇到沒有定義的位元組給 U+FFFD 而不是錯誤,那條路實際上走不到。
    b.iter().map(|&c| if c < 0x80 { c as char } else { table[c as usize - 0x80] }).collect()
}

/// Go 的 strings.ToUpper:逐字元的簡單對應(一個字元對一個字元)。
fn go_upper(s: &str) -> String {
    s.chars()
        .map(|c| {
            let mut u = c.to_uppercase();
            match (u.next(), u.next()) {
                (Some(v), None) => v,
                _ => c,
            }
        })
        .collect()
}

/// 從欄位指令裡挑出網址。
pub(crate) fn field_hyperlink(instr: &str) -> String {
    let s = instr.trim();
    if !go_upper(s).starts_with("HYPERLINK") {
        return String::new();
    }
    // Go 以位元組切 s[len("HYPERLINK"):];大寫前後長度不同的字(ı → I)
    // 會讓切點落在字中間,那時 Go 留下半個字,這裡換成 U+FFFD。
    let rest = String::from_utf8_lossy(&s.as_bytes()[9.min(s.len())..]).into_owned();
    let rest = rest.trim();
    if let Some(i) = rest.find('"') {
        let rest = &rest[i + 1..];
        return match rest.find('"') {
            Some(j) => rest[..j].to_string(),
            None => String::new(),
        };
    }
    for f in rest.split_whitespace() {
        if !f.starts_with('\\') {
            return f.to_string();
        }
    }
    String::new()
}
