//! 用 gopher 的方式看網頁:只留文字、連結與圖片(對應 Go 版 `internal/web`)。
//!
//! 為什麼是「gopher 的方式」而不是一個瀏覽器:這個畫面是字元格點,
//! 畫不出 HTML 要的排版、字體、浮動與腳本。與其做一個什麼都做不好的
//! 瀏覽器,不如明確地只做一件事,把網頁壓成「一段一段的文字、
//! 一個一個的連結、幾張圖」,那正好是 gopher 選單的形狀,
//! 而這個程式已經有一整套畫那個形狀的東西了。
//!
//! 這一包只做「取回」與「壓成區塊」,不碰畫面,也不決定要不要載圖。
//!
//! 網址的解析與相對位址的接法照 Go 的 net/url 搬(`gourl`),HTTP 用 ureq。
//! 重導向自己跟,不交給 ureq:最終位址要照 Go 的規則接出來,後面的相對連結
//! 都以它為基準。

mod gostr;
mod gourl;
mod html;

#[cfg(test)]
mod tests;

pub use html::parse_html;

use std::fmt;
use std::io::Read;
use std::time::{Duration, Instant};

use wincv_i18n::{t, tr};
use wincv_markdown::{Block, Kind};
use wincv_textenc::{Enc, detect, utf8_lossy_go};

pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(20);
/// 一頁 HTML 的上限。
pub const DEFAULT_MAX_BYTES: u64 = 4 << 20;
/// 單張圖的上限。比 HTML 寬,但仍然有限:
/// 這是檔案管理器裡的一個檢視模式,不是下載器。
pub const DEFAULT_MAX_IMAGE_BYTES: u64 = 8 << 20;
/// 重導向的上限,與 Go 版的 CheckRedirect 相同:已經送出 5 個請求時拒絕下一個。
const MAX_REDIRECTS: usize = 5;

/// 對方送的東西超過上限時的訊息(與 Go 版的 ErrTooLarge 同一句)。
pub const ERR_TOO_LARGE: &str = "內容超過上限,已截斷";

/// 照實說自己是什麼。
///
/// 不假裝成別的瀏覽器:伺服器依 UA 送不同版面時,誠實的 UA 拿到的
/// 通常是比較簡單的那一份,而簡單的那一份正是這個畫面畫得出來的。
pub const DEFAULT_USER_AGENT: &str =
    "WinCV-Remake/0.52 (text-mode; +https://github.com/wicanr2/wincv-remake)";

/// 取回來的一頁。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Doc {
    /// **最終**位址(重導向之後)。後面的相對連結要以它為基準。
    pub url: String,
    pub title: String,
    /// 排版用的區塊。位址指向圖片時是空的。
    pub blocks: Vec<Block>,
    /// 不為空表示這個位址本身就是一張圖,交給看圖模式處理。
    pub image: Vec<u8>,
    /// 內容被上限截斷了。不是失敗,但要說出來。
    pub truncated: bool,
}

/// 取回網頁。
///
/// 兩個上限都是必要的,不是保險:HTTP 的 Content-Length 可以不寫也可以說謊,
/// 而這個程式是檔案管理器,不該因為點到一個大檔就把記憶體吃光。
#[derive(Clone, Default)]
pub struct Client {
    /// 整次取回的期限(含重導向與讀內容)。零值用 [`DEFAULT_TIMEOUT`]。
    pub timeout: Duration,
    /// 一頁的上限。零值用 [`DEFAULT_MAX_BYTES`]。
    pub max_bytes: u64,
    /// 單張圖的上限。零值用 [`DEFAULT_MAX_IMAGE_BYTES`]。
    pub max_image_bytes: u64,
    /// 空的話用 [`DEFAULT_USER_AGENT`]。
    pub user_agent: String,
    /// 可以換掉,測試用(例如用 ureq 的 `Agent::with_parts` 換掉連線方式)。
    /// `None` 用預設的 Agent(rustls、環境變數裡的 proxy)。
    pub http: Option<ureq::Agent>,
}

/// 這個位址是不是這一包處理的範圍。
pub fn is_http(raw: &str) -> bool {
    let s = raw.trim().to_lowercase();
    s.starts_with("http://") || s.starts_with("https://")
}

/// [`Client::fetch_image`] 的結果之外的錯誤。訊息已經過 i18n。
#[derive(Debug, Clone, PartialEq, Eq)]
pub struct Error(pub String);

impl fmt::Display for Error {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&self.0)
    }
}

impl std::error::Error for Error {}

/// get 的結果:內容、Content-Type、是否截斷(最終位址另外回傳)。
struct Got {
    body: Vec<u8>,
    ctype: String,
    truncated: bool,
}

impl Client {
    fn agent(&self) -> ureq::Agent {
        match &self.http {
            Some(a) => a.clone(),
            None => ureq::Agent::new_with_defaults(),
        }
    }

    /// 取一個網頁。
    pub fn fetch(&self, raw: &str) -> Result<Doc, Error> {
        let u = gourl::parse(&gostr::to_bytes(raw.trim()))
            .map_err(|e| Error(tr!("位址解不開: %w", gostr::finish(&e))))?;
        if u.scheme != b"http" && u.scheme != b"https" {
            return Err(Error(tr!(
                "只支援 http 與 https,不是 %s",
                gostr::finish(&gostr::from_bytes(&u.scheme))
            )));
        }
        if u.host.is_empty() {
            return Err(Error(t("沒有主機名")));
        }
        let max = if self.max_bytes == 0 {
            DEFAULT_MAX_BYTES
        } else {
            self.max_bytes
        };
        // [雷] 截斷要在「是不是錯誤」之前判斷。先 return 再問 err 是什麼,
        // truncated 就永遠是 false:大頁面會被靜靜切掉一半而沒有任何提示,
        // 而小頁面測不出來。
        let (got, final_url) = self.get(&u, max)?;
        Ok(build_doc(got.body, &got.ctype, &final_url, got.truncated))
    }

    /// 取一張圖的位元組。給排版引擎的圖片載入器用。超過上限時回截斷的部分。
    pub fn fetch_image(&self, raw: &str) -> Result<Vec<u8>, Error> {
        let u = gourl::parse(&gostr::to_bytes(raw.trim())).map_err(|e| Error(gostr::finish(&e)))?;
        if u.scheme != b"http" && u.scheme != b"https" {
            return Err(Error(tr!("不是 http 位址:%s", raw)));
        }
        let max = if self.max_image_bytes == 0 {
            DEFAULT_MAX_IMAGE_BYTES
        } else {
            self.max_image_bytes
        };
        Ok(self.get(&u, max)?.0.body)
    }

    /// 做一次 GET(自己跟重導向),回傳內容、Content-Type 與最終位址。
    ///
    /// 超過上限時仍然回傳讀到的部分並標記截斷:截斷的 HTML
    /// 通常還是看得懂前半,直接丟掉對使用者沒有好處。
    fn get(&self, u: &gourl::Url, max: u64) -> Result<(Got, gourl::Url), Error> {
        let to = if self.timeout.is_zero() {
            DEFAULT_TIMEOUT
        } else {
            self.timeout
        };
        let deadline = Instant::now() + to;
        let ua = if self.user_agent.is_empty() {
            DEFAULT_USER_AGENT
        } else {
            &self.user_agent
        };
        let host_name = gostr::finish(&gostr::from_bytes(&u.host));
        let fail = |e: String| Error(tr!("連不上 %s: %w", host_name.as_str(), e));

        // Go 的 http.NewRequest 會把 u.String() 再解析一次,並去掉空的埠("host:")。
        let mut req = gourl::parse(&u.string()).map_err(|e| fail(gostr::finish(&e)))?;
        remove_empty_port(&mut req.host);
        let agent = self.agent();
        let mut sent = 0usize;
        loop {
            if req.scheme != b"http" && req.scheme != b"https" {
                return Err(fail(format!(
                    "unsupported protocol scheme {}",
                    gostr::finish(&gostr::from_bytes(&req.scheme))
                )));
            }
            sent += 1;
            let rem = deadline.saturating_duration_since(Instant::now());
            if rem.is_zero() {
                return Err(fail("Client.Timeout exceeded".into()));
            }
            let uri = request_target(&req);
            let resp = agent
                .get(&uri)
                .header("User-Agent", ua)
                .header("Accept", "text/html,text/plain,image/*;q=0.8,*/*;q=0.5")
                .config()
                .max_redirects(0)
                .http_status_as_error(false)
                .timeout_global(Some(rem))
                .build()
                .call()
                .map_err(|e| fail(e.to_string()))?;
            let status = resp.status();
            let code = status.as_u16();
            // 301/302/303/307/308 照 Go 的 redirectBehavior 跟;沒有 Location 的話
            // 那個 3xx 就是結果(之後被當成非 2xx 回報)。
            if matches!(code, 301 | 302 | 303 | 307 | 308) {
                let loc = resp
                    .headers()
                    .get("location")
                    .map(|v| gostr::from_bytes(v.as_bytes()));
                if let Some(loc) = loc.filter(|l| !l.is_empty()) {
                    let next = req.parse_ref(&gostr::to_bytes(&loc)).map_err(|e| {
                        fail(format!(
                            "failed to parse Location header {loc:?}: {}",
                            gostr::finish(&e)
                        ))
                    })?;
                    if sent >= MAX_REDIRECTS {
                        return Err(fail(tr!("重導向超過 %d 次", MAX_REDIRECTS as i64)));
                    }
                    req = next;
                    continue;
                }
            }
            if !(200..300).contains(&code) {
                let status_text = format!("{} {}", code, status.canonical_reason().unwrap_or(""));
                return Err(Error(tr!("伺服器回 %s", status_text.trim_end())));
            }
            let ctype = resp
                .headers()
                .get("content-type")
                .map(|v| go_lower(gostr::finish(&gostr::from_bytes(v.as_bytes())).trim()))
                .unwrap_or_default();
            // 多讀一個位元組,才分得出「剛好等於上限」與「超過上限」。
            let mut body = Vec::new();
            resp.into_body()
                .into_reader()
                .take(max.saturating_add(1))
                .read_to_end(&mut body)
                .map_err(|e| Error(tr!("讀取失敗: %w", e.to_string())))?;
            let truncated = body.len() as u64 > max;
            if truncated {
                body.truncate(max as usize);
            }
            return Ok((
                Got {
                    body,
                    ctype,
                    truncated,
                },
                req,
            ));
        }
    }
}

/// 把內容依型別變成一頁。
fn build_doc(body: Vec<u8>, ctype: &str, final_url: &gourl::Url, truncated: bool) -> Doc {
    let mut d = Doc {
        url: gostr::finish(&gostr::from_bytes(&final_url.string())),
        truncated,
        ..Default::default()
    };
    if ctype.starts_with("image/") {
        d.title = path(final_url);
        d.image = body;
    } else if is_html(ctype, &body) {
        let text = decode(&body, ctype);
        let (title, blocks) = html::parse_html_url(Some(final_url.clone()), &text);
        d.title = if title.is_empty() {
            gostr::finish(&gostr::from_bytes(&final_url.host))
        } else {
            title
        };
        d.title = d.title.trim().to_string();
        d.blocks = blocks;
    } else {
        // 純文字與認不得的型別都當文字看:認不得時倒出可讀的部分,
        // 比一句「不支援」有用。很多站台的 Content-Type 是錯的。
        d.title = path(final_url);
        d.blocks = text_blocks(&body);
    }
    d
}

/// Go 的 removeEmptyPort:主機以「:」結尾(而且那個冒號是埠的分隔)就去掉它。
fn remove_empty_port(host: &mut Vec<u8>) {
    let colon = host.iter().rposition(|&c| c == b':');
    let bracket = host.iter().rposition(|&c| c == b']');
    let has_port = match (colon, bracket) {
        (Some(c), Some(b)) => c > b,
        (Some(_), None) => true,
        _ => false,
    };
    if has_port && host.last() == Some(&b':') {
        host.pop();
    }
}

/// 送給 ureq 的請求位址:scheme、主機與 Go 的 RequestURI。
///
/// Go 的 RawQuery 不逃脫,http crate 的 Uri 卻不收空白、引號這類字元,
/// 所以這裡把那些位元組寫成 `%XX`。這只影響送出去的那一行,不影響最終位址。
fn request_target(u: &gourl::Url) -> String {
    let mut s = Vec::new();
    s.extend_from_slice(&u.scheme);
    s.extend_from_slice(b"://");
    s.extend_from_slice(&u.host);
    s.extend(u.request_uri());
    let mut out = String::with_capacity(s.len());
    for &b in &s {
        if b.is_ascii_graphic()
            && !matches!(
                b,
                b'"' | b'<' | b'>' | b'\\' | b'^' | b'`' | b'{' | b'|' | b'}'
            )
        {
            out.push(b as char);
        } else {
            out.push_str(&format!("%{b:02X}"));
        }
    }
    out
}

/// 把純文字排成區塊。
///
/// 與 gopher 那邊同一個判斷:網路上的文字多半是排好版的,重新斷行
/// 會把表格與 ASCII art 弄壞。
pub fn text_blocks(b: &[u8]) -> Vec<Block> {
    let s = gostr::finish(&gostr::from_bytes(&decode(b, ""))).replace("\r\n", "\n");
    let mut lines: Vec<String> = s.split('\n').map(str::to_string).collect();
    while lines.last().is_some_and(|ln| ln.trim().is_empty()) {
        lines.pop();
    }
    if lines.is_empty() {
        return Vec::new();
    }
    vec![Block {
        kind: Kind::Pre,
        lines,
        ..Default::default()
    }]
}

/// 內容是不是 HTML。
///
/// 不能只信 Content-Type:很多站台送 text/plain 卻給 HTML,
/// 也有送 application/octet-stream 的。型別沒說清楚時看內容開頭。
fn is_html(ctype: &str, body: &[u8]) -> bool {
    if ctype.contains("html") || ctype.contains("xhtml") {
        return true;
    }
    if ctype.contains("text/plain") {
        return false;
    }
    let head = &body[..body.len().min(1024)];
    // 兩個目標字串只有 ASCII 字母,而且不含 i、k(Unicode 小寫化會把 İ、K 變成
    // 這兩個 ASCII 字母),所以只轉 ASCII 的大小寫與 Go 的 strings.ToLower 結果相同。
    let s = head.to_ascii_lowercase();
    contains(&s, b"<!doctype html") || contains(&s, b"<html")
}

fn contains(s: &[u8], sub: &[u8]) -> bool {
    s.windows(sub.len()).any(|w| w == sub)
}

/// Go 版 textenc.Decode 的結果,以 Go 字串(位元組)表示。
///
/// UTF-8 與 ASCII 在 Go 版沒有解碼器,原樣回傳(UTF-8 去掉 BOM),
/// 不合法的位元組留著;其他編碼交給 textenc。
fn go_decode(b: &[u8], e: Enc) -> Vec<u8> {
    match e {
        Enc::Utf8 => b.strip_prefix(&[0xEF, 0xBB, 0xBF]).unwrap_or(b).to_vec(),
        Enc::Ascii | Enc::Unknown | Enc::Binary => b.to_vec(),
        _ => wincv_textenc::decode(b, e).into_bytes(),
    }
}

/// 把位元組變成(Go 的)字串。
///
/// 優先順序:Content-Type 的 charset → HTML 裡的 `<meta charset>` →
/// 內容判讀。前兩者是網站自己宣告的,但**宣告會錯**,所以宣告看起來
/// 是錯的時候(見 [`declared`]),還是要退回內容判讀。
fn decode(b: &[u8], ctype: &str) -> Vec<u8> {
    if let Some(s) = declared(b, charset_of(ctype.as_bytes())) {
        return s;
    }
    let head = &b[..b.len().min(4096)];
    if let Some(s) = declared(b, charset_of(head)) {
        return s;
    }
    go_decode(b, detect(b))
}

/// 用一個編碼名稱(例如 XML 宣告裡的 encoding)解碼,結果是 Go 字串的位元組。
/// 名稱認不得,或解出來看得出宣告是錯的,就改用內容判讀。
pub fn decode_charset(b: &[u8], name: &str) -> Vec<u8> {
    if let Some(s) = declared(b, label(&go_lower(name))) {
        return s;
    }
    go_decode(b, detect(b))
}

/// 把一份沒有 Content-Type 可依的 HTML 或 XHTML(EPUB 的章節)變成
/// Go 字串的位元組,可以直接交給 [`parse_html`]。
///
/// 優先順序:BOM → XML 宣告的 encoding → `<meta charset>` → 內容判讀。
/// BOM 最確定,有就照它;XML 宣告與 meta 是作者寫的,一樣會錯,
/// 錯的時候往下一個依據退。
pub fn decode_markup(b: &[u8]) -> Vec<u8> {
    if b.starts_with(&[0xEF, 0xBB, 0xBF]) {
        return go_decode(b, Enc::Utf8);
    }
    if b.starts_with(&[0xFF, 0xFE]) {
        return go_decode(b, Enc::Utf16Le);
    }
    if b.starts_with(&[0xFE, 0xFF]) {
        return go_decode(b, Enc::Utf16Be);
    }
    if let Some(name) = xml_encoding(b)
        && let Some(s) = declared(b, label(&go_lower(&utf8_lossy_go(name))))
    {
        return s;
    }
    decode(b, "")
}

/// 開頭那個 XML 宣告(`<?xml ... ?>`)裡的 encoding。沒有就回 `None`。
fn xml_encoding(b: &[u8]) -> Option<&[u8]> {
    let ws = |c: &u8| matches!(c, b' ' | b'\t' | b'\r' | b'\n');
    let trim = |d: &[u8]| -> usize { d.iter().position(|c| !ws(c)).unwrap_or(d.len()) };
    let b = &b[trim(b)..];
    if !b.starts_with(b"<?xml") || b.len() < 6 || !ws(&b[5]) {
        return None;
    }
    let end = find(b, b"?>")?;
    let d = &b[5..end];
    let i = find(d, b"encoding")?;
    let d = &d[i + b"encoding".len()..];
    let d = &d[trim(d)..];
    let d = d.strip_prefix(b"=")?;
    let d = &d[trim(d)..];
    let q = *d.first().filter(|&&c| c == b'"' || c == b'\'')?;
    let j = d[1..].iter().position(|&c| c == q)?;
    let name = &d[1..1 + j];
    if name.is_empty() { None } else { Some(name) }
}

fn find(s: &[u8], sub: &[u8]) -> Option<usize> {
    s.windows(sub.len()).position(|w| w == sub)
}

/// 一個編碼宣告對到的解碼方式。
///
/// windows-1252 不在 textenc 裡:內容判讀不會猜它(西歐文字的位元組
/// 落在 Big5 的範圍裡,判讀分不出來),它只會從宣告來,所以在這裡另外標。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
struct Charset {
    enc: Enc,
    cp1252: bool,
}

const NO_CHARSET: Charset = Charset {
    enc: Enc::Unknown,
    cp1252: false,
};

/// 從一段文字裡找 charset= 的宣告。
///
/// 照 Go 版的正規式 `(?i)charset\s*=\s*"?([a-z0-9_\-]+)` 逐位置比對。
/// `(?i)` 在 Go 是 Unicode 的大小寫摺疊,所以 s 也吃 ſ(U+017F)、k 也吃 K(U+212A)。
fn charset_of(s: &[u8]) -> Charset {
    match charset_match(s) {
        Some(name) => label(&name),
        None => NO_CHARSET,
    }
}

/// 把(小寫的)編碼名稱對到解碼方式。認不得的回 [`NO_CHARSET`]。
fn label(name: &str) -> Charset {
    let enc = match name {
        "utf-8" | "utf8" => Enc::Utf8,
        "big5" | "big-5" | "big5-hkscs" | "cp950" | "ms950" => Enc::Big5,
        "gbk" | "gb2312" | "gb18030" | "cp936" => Enc::Gbk,
        "shift_jis" | "shift-jis" | "sjis" | "cp932" | "ms932" | "windows-31j" => Enc::ShiftJis,
        "euc-kr" | "euckr" | "cp949" => Enc::EucKr,
        "us-ascii" | "ascii" => Enc::Ascii,
        // WHATWG 的 Encoding 規格把 ISO-8859-1 這一族都對到 windows-1252:
        // 自稱 Latin-1 的頁面實際上幾乎都用到 0x80–0x9F 的彎引號與破折號。
        "iso-8859-1" | "iso8859-1" | "iso_8859-1" | "latin1" | "l1" | "cp819" | "ibm819"
        | "windows-1252" | "cp1252" | "x-cp1252" => {
            return Charset {
                enc: Enc::Unknown,
                cp1252: true,
            };
        }
        _ => Enc::Unknown,
    };
    Charset { enc, cp1252: false }
}

/// 照宣告的編碼解碼。宣告看起來是錯的就回 `None`,由呼叫端退回下一個依據。
///
/// 「看起來是錯的」有兩種:
/// - 宣告了 UTF-8 以外的編碼,內容卻是含非 ASCII 字元的合法 UTF-8。
///   真正的 Big5 或 windows-1252 文字幾乎不可能整份剛好是合法的 UTF-8;
///   反過來,伺服器預設送 ISO-8859-1、內容其實是 UTF-8 的頁面很常見。
/// - 解出來滿是替代字元(見 [`mojibake`])。宣告 UTF-8 或 ASCII 時沒有解碼器,
///   字串裡留著原始位元組,所以先把每個不合法的位元組算成一個替代字元再數。
fn declared(b: &[u8], cs: Charset) -> Option<Vec<u8>> {
    if cs == NO_CHARSET {
        return None;
    }
    if cs.enc != Enc::Utf8
        && cs.enc != Enc::Ascii
        && !b.is_ascii()
        && std::str::from_utf8(b).is_ok()
    {
        return Some(go_decode(b, Enc::Utf8));
    }
    if cs.cp1252 {
        return Some(decode_cp1252(b).into_bytes());
    }
    let s = go_decode(b, cs.enc);
    let bad = if matches!(cs.enc, Enc::Utf8 | Enc::Ascii) {
        mojibake(utf8_lossy_go(&s).as_bytes())
    } else {
        mojibake(&s)
    };
    if bad { None } else { Some(s) }
}

/// windows-1252 的 0x80–0x9F。其餘位元組與 Unicode 的 U+0000–U+00FF 相同。
///
/// 表照 WHATWG 的 index-windows-1252:沒有定義的 0x81、0x8D、0x8F、0x90、0x9D
/// 對到同值的 C1 控制字元。Go 版的 x/text 把這五個換成 U+FFFD,
/// 所以兩邊都用這張表,不用各自的函式庫。
const CP1252_HIGH: [char; 32] = [
    '\u{20AC}', '\u{0081}', '\u{201A}', '\u{0192}', '\u{201E}', '\u{2026}', '\u{2020}', '\u{2021}',
    '\u{02C6}', '\u{2030}', '\u{0160}', '\u{2039}', '\u{0152}', '\u{008D}', '\u{017D}', '\u{008F}',
    '\u{0090}', '\u{2018}', '\u{2019}', '\u{201C}', '\u{201D}', '\u{2022}', '\u{2013}', '\u{2014}',
    '\u{02DC}', '\u{2122}', '\u{0161}', '\u{203A}', '\u{0153}', '\u{009D}', '\u{017E}', '\u{0178}',
];

fn decode_cp1252(b: &[u8]) -> String {
    b.iter()
        .map(|&c| match c {
            0x80..=0x9F => CP1252_HIGH[(c - 0x80) as usize],
            _ => c as char,
        })
        .collect()
}

const LONG_S: &[u8] = "\u{017F}".as_bytes();
const KELVIN: &[u8] = "\u{212A}".as_bytes();

/// `s[i..]` 開頭是否為字母 `c`(已是小寫),照 (?i) 的摺疊;回傳吃掉的位元組數。
fn fold_letter(s: &[u8], i: usize, c: u8) -> Option<usize> {
    let b = *s.get(i)?;
    if b.to_ascii_lowercase() == c {
        return Some(1);
    }
    if c == b's' && s[i..].starts_with(LONG_S) {
        return Some(LONG_S.len());
    }
    if c == b'k' && s[i..].starts_with(KELVIN) {
        return Some(KELVIN.len());
    }
    None
}

fn charset_match(s: &[u8]) -> Option<String> {
    for start in 0..s.len() {
        let mut i = start;
        let mut ok = true;
        for &c in b"charset" {
            match fold_letter(s, i, c) {
                Some(n) => i += n,
                None => {
                    ok = false;
                    break;
                }
            }
        }
        if !ok {
            continue;
        }
        // Go 的 \s 只是 ASCII 的 [\t\n\f\r ]。
        let ws = |b: u8| matches!(b, b'\t' | b'\n' | b'\x0c' | b'\r' | b' ');
        while i < s.len() && ws(s[i]) {
            i += 1;
        }
        if s.get(i) != Some(&b'=') {
            continue;
        }
        i += 1;
        while i < s.len() && ws(s[i]) {
            i += 1;
        }
        // "? 是貪婪的:先試有引號,接不上名字才退回沒有引號(那時名字要從 " 開始,必定失敗)。
        if s.get(i) == Some(&b'"') {
            i += 1;
        }
        let mut name = String::new();
        loop {
            let Some(&b) = s.get(i) else { break };
            if b.is_ascii_alphanumeric() || b == b'_' || b == b'-' {
                name.push(b.to_ascii_lowercase() as char);
                i += 1;
            } else if s[i..].starts_with(LONG_S) {
                // ToLower(ſ) 仍是 ſ。
                name.push('\u{017F}');
                i += LONG_S.len();
            } else if s[i..].starts_with(KELVIN) {
                // ToLower(K) 是 k。
                name.push('k');
                i += KELVIN.len();
            } else {
                break;
            }
        }
        if !name.is_empty() {
            return Some(name);
        }
    }
    None
}

/// 一段解出來的字是不是壞的。
///
/// 判準是替代字元(U+FFFD)的比例:宣告錯編碼的頁面解出來會滿是這個,
/// 而正常的頁面幾乎不會有。用比例而不是「有沒有」:一個壞位元組
/// 不該讓整份判讀翻盤。
///
/// 照 Go:數的是字串裡**真的** U+FFFD(EF BF BD),不合法的位元組不算,
/// 但它們在字元數裡各算一個。
fn mojibake(s: &[u8]) -> bool {
    if s.is_empty() {
        return false;
    }
    let pat = "\u{FFFD}".as_bytes();
    let (mut n, mut i) = (0usize, 0usize);
    while i + pat.len() <= s.len() {
        if &s[i..i + pat.len()] == pat {
            n += 1;
            i += pat.len();
        } else {
            i += 1;
        }
    }
    let runes = gostr::from_bytes(s).chars().count();
    n > 8 && n * 20 > runes
}

/// 從位址取一個像檔名的東西,給標題用。
fn path(u: &gourl::Url) -> String {
    let mut s: &[u8] = &u.path;
    if let Some(i) = s.iter().rposition(|&c| c == b'/') {
        s = &s[i + 1..];
    }
    if s.is_empty() {
        return gostr::finish(&gostr::from_bytes(&u.host));
    }
    gostr::finish(&gostr::from_bytes(s))
}

/// Go 的 strings.ToLower:逐字元的簡單對應。
fn go_lower(s: &str) -> String {
    s.chars()
        .map(|c| c.to_lowercase().next().unwrap_or(c))
        .collect()
}
