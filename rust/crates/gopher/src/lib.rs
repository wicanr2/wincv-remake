//! Gopher(RFC 1436)的客戶端(對應 Go 版 `internal/gopher`)。
//!
//! 為什麼是 gopher 而不是 HTTP:這支軟體要的是「文字與圖片,簡單就好」的瀏覽。
//! gopher 的協定本身就是那個形狀:一行一個項目、型別寫在第一個位元組、
//! 沒有樣式表也沒有腳本,選單結構幾乎就是目錄列表。HTTP 的難處全在 HTML,
//! 而 HTML 要的東西(排版、字體、浮動、JavaScript)這個畫面一樣都給不了。
//!
//! 這一包只做協定與文件模型,不碰畫面。排版借用 `wincv-markdown` 的引擎:
//! 選單變成一行一個 List、文字檔變成 Pre(不重新斷行,gopher 的內容常常是
//! 排好版的 ASCII art)、連結掛在 `Span::href` 上。
//!
//! Go 的字串是位元組序列,selector 可以是 Big5 之類不合法的 UTF-8,而且要
//! 原樣送回伺服器。所以 [`Url::sel`] 與 [`Item::sel`] 存位元組;給人看的欄位
//! (顯示文字、主機、埠)照 Go 的 range 規則轉成字串:每個壞位元組換一個 U+FFFD,
//! 與 Go 版排版引擎看到的字元一樣多。

mod doc;

#[cfg(test)]
mod tests;

pub use doc::{is_image, label, menu_blocks, text_blocks};

use std::fmt;
use std::io::{self, Read, Write};
use std::net::{TcpStream, ToSocketAddrs};
use std::sync::Arc;
use std::time::{Duration, Instant};

use wincv_i18n::{t, tr};
use wincv_textenc::{Enc, decode, detect, utf8_lossy_go};

/// gopher 的預設埠。
pub const DEFAULT_PORT: &str = "70";

// 項目型別。第一個位元組,定義在 RFC 1436,另有幾個約定俗成的擴充。
pub const TYPE_TEXT: u8 = b'0'; // 文字檔
pub const TYPE_MENU: u8 = b'1'; // 選單(目錄)
pub const TYPE_ERROR: u8 = b'3'; // 錯誤
pub const TYPE_SEARCH: u8 = b'7'; // 查詢介面
pub const TYPE_BINARY: u8 = b'9'; // 二進位
pub const TYPE_GIF: u8 = b'g'; // GIF
pub const TYPE_IMAGE: u8 = b'I'; // 圖片(格式由內容決定)
pub const TYPE_PNG: u8 = b'p'; // PNG(約定俗成)
pub const TYPE_HTML: u8 = b'h'; // HTML;selector 常是 "URL:https://..."
pub const TYPE_INFO: u8 = b'i'; // 純資訊,不是連結(約定俗成,但幾乎每個站都在用)
pub const TYPE_SOUND: u8 = b's'; // 音訊
pub const TYPE_DOC: u8 = b'd'; // 文件檔(約定俗成)

/// 一個 gopher 位址。
///
/// 形式是 `gopher://host[:port]/<型別><selector>`。型別擠在路徑的第一個字元
/// 是 gopher 特有的:它讓客戶端在連線之前就知道對方會回什麼,
/// 不必等 Content-Type。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Url {
    pub host: String,
    /// 空的表示 [`DEFAULT_PORT`]。
    pub port: String,
    /// 0 表示未指定,當成 [`TYPE_MENU`]。
    pub typ: u8,
    /// selector,原樣送出(位元組,不做任何加工)。
    pub sel: Vec<u8>,
    /// 型別 7 的查詢字串。送出時接在 selector 後面,中間一個 tab。
    pub search: String,
}

/// 解析 gopher 位址。scheme 可以省略。
///
/// 幾種都要吃:
///
/// ```text
/// gopher://host           → 選單,selector 空
/// gopher://host:7070/     → 同上,換埠
/// gopher://host/1/foo     → 型別 1,selector "/foo"
/// gopher://host/0/foo.txt → 型別 0
/// host/1/foo              → 沒有 scheme 也可以
/// gopher://host/7/find?貓  → 型別 7,查詢字串「貓」
/// ```
pub fn parse_url(s: &str) -> Result<Url, String> {
    let mut s = go_trim_space(s);
    if s.is_empty() {
        return Err(t("位址是空的"));
    }
    if let Some(i) = s.find("://") {
        if !eq_fold_ascii(&s[..i], "gopher") {
            return Err(tr!("只支援 gopher://,不是 %s://", &s[..i]));
        }
        s = &s[i + 3..];
    }

    let mut u = Url::default();
    // 查詢字串可以用 ? 或 tab 分隔。tab 是協定本身的寫法,
    // ? 是瀏覽器網址列的習慣:兩種都收。
    if let Some(i) = s.find(['?', '\t']) {
        u.search = s[i + 1..].to_string();
        s = &s[..i];
    }

    let (host, path) = match s.find('/') {
        Some(i) => (&s[..i], &s[i + 1..]),
        None => (s, ""),
    };
    if host.is_empty() {
        return Err(t("沒有主機名"));
    }
    // IPv6 的字面位址寫成 [::1]:70,冒號不能當埠的分隔。
    if host.starts_with('[') {
        match host.find(']') {
            Some(j) => {
                u.host = host[..j + 1].to_string();
                if let Some(p) = host[j + 1..].strip_prefix(':') {
                    u.port = p.to_string();
                }
            }
            None => return Err(t("IPv6 位址少了 ]")),
        }
    } else if let Some(i) = host.find(':') {
        u.host = host[..i].to_string();
        u.port = host[i + 1..].to_string();
    } else {
        u.host = host.to_string();
    }
    if u.host.is_empty() {
        return Err(t("沒有主機名"));
    }

    if !path.is_empty() {
        // 型別是**位元組**:路徑開頭若是多位元組字,Go 也是只拿第一個位元組。
        let pb = path.as_bytes();
        u.typ = pb[0];
        u.sel = pb[1..].to_vec();
    }
    Ok(u)
}

impl Url {
    /// 實際要用的型別。未指定時是選單:那是 gopher 的預設進入點。
    fn kind(&self) -> u8 {
        if self.typ == 0 { TYPE_MENU } else { self.typ }
    }

    /// 撥號用的「主機:埠」。照 Go 的 net.JoinHostPort:主機含冒號就加方括號。
    fn addr(&self) -> String {
        let p = if self.port.is_empty() {
            DEFAULT_PORT
        } else {
            &self.port
        };
        if self.host.contains(':') {
            format!("[{}]:{}", self.host, p)
        } else {
            format!("{}:{}", self.host, p)
        }
    }

    /// 還原成位元組形式的 `gopher://`(selector 原樣)。
    pub fn to_bytes(&self) -> Vec<u8> {
        let mut b = Vec::with_capacity(16 + self.host.len() + self.sel.len());
        b.extend_from_slice(b"gopher://");
        b.extend_from_slice(self.host.as_bytes());
        if !self.port.is_empty() && self.port != DEFAULT_PORT {
            b.push(b':');
            b.extend_from_slice(self.port.as_bytes());
        }
        b.push(b'/');
        b.push(self.kind());
        b.extend_from_slice(&self.sel);
        if !self.search.is_empty() {
            b.push(b'?');
            b.extend_from_slice(self.search.as_bytes());
        }
        b
    }
}

/// 把位址還原成 `gopher://` 形式。selector 不是合法 UTF-8 的話,
/// 壞位元組照 Go 的規則換成 U+FFFD(見 [`Url::to_bytes`] 拿原樣的位元組)。
impl fmt::Display for Url {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(&utf8_lossy_go(&self.to_bytes()))
    }
}

/// 選單裡的一列。
///
/// `display` 是解碼過的文字(給人看的);`sel` 是原始位元組,不解碼:
/// 點下去時要原封送回伺服器,換過編碼的 selector 對方認不得。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Item {
    pub typ: u8,
    pub display: String,
    pub sel: Vec<u8>,
    pub host: String,
    pub port: String,
}

impl Item {
    /// 這一列點得下去嗎。
    ///
    /// 資訊列(型別 i)只是文字,沒有目標;錯誤列(型別 3)同理。
    /// 有些站台會用 host 為空或 "error.host" 的假項目來排版,那些也不算連結。
    pub fn is_link(&self) -> bool {
        match self.typ {
            TYPE_INFO | TYPE_ERROR | 0 => false,
            _ => !self.host.is_empty() && !eq_fold_ascii(&self.host, "error.host"),
        }
    }

    /// 這一列指向的 http(s) 位址。
    ///
    /// gopher 用型別 h 加上 "URL:https://..." 這個 selector 來連到 web。
    /// 這個客戶端不解 HTTP,所以只把位址交出去,由上層決定怎麼辦
    /// (交給系統瀏覽器,或只是顯示出來)。
    pub fn web_url(&self) -> Option<String> {
        if self.typ != TYPE_HTML {
            return None;
        }
        // Go 的 strings.EqualFold(s[:4], "URL:"):前 4 個位元組不分大小寫比對。
        let s = &self.sel;
        if s.len() >= 4 && eq_fold_ascii_bytes(&s[..4], b"URL:") {
            return Some(utf8_lossy_go(&s[4..]));
        }
        None
    }

    /// 這一列的 gopher 位址。
    pub fn url(&self) -> Url {
        Url {
            host: self.host.clone(),
            port: self.port.clone(),
            typ: self.typ,
            sel: self.sel.clone(),
            search: String::new(),
        }
    }
}

/// 解析選單。
///
/// 每一列是 `<型別><顯示文字> TAB <selector> TAB <主機> TAB <埠>`,
/// 以只有一個 "." 的那一列結束。
///
/// [雷] 欄位不足的列不能直接丟掉。實務上很多站台的資訊列只寫
/// "i說明文字" 就換行,連 tab 都沒有:丟掉的話整段說明會消失,
/// 而畫面上看起來只是「這個站沒寫什麼」。
///
/// 顯示文字的編碼交給 textenc 判讀,而且**整份選單一起判讀**:gopher 沒有
/// charset 欄位,而一列的顯示文字常常只有幾個字,單看一列 Big5 與 GBK
/// 分不出來,同一份選單的各列卻可能被判成不同的編碼。
///
/// 判讀的樣本是每一列第一個 tab 之前的部分,**連型別字元一起**:型別字元
/// 不是 ASCII 的列(不合規格的選單、被當成選單解的文字檔)拿掉第一個位元組
/// 會把 UTF-8 字切斷,整份 UTF-8 就被誤判成 GBK。selector 不放進樣本,
/// 它不是給人看的,編碼也可能和顯示文字不同。
pub fn parse_menu(b: &[u8]) -> Vec<Item> {
    let raw = parse_menu_raw(b);
    let mut sample = Vec::new();
    for ln in b.split(|&c| c == b'\n') {
        let ln = ln.strip_suffix(b"\r").unwrap_or(ln);
        if ln == b"." {
            break;
        }
        let head = ln.split(|&c| c == b'\t').next().unwrap_or(ln);
        sample.extend_from_slice(head);
        sample.push(b'\n');
    }
    let e = detect(&sample);
    // 這幾種的第二個位元組都不會是 tab 或換行,所以先切欄位再解碼是安全的。
    // 其他結果(ASCII、UTF-8,以及判讀不出來的)照 Go 字串的規則轉成文字。
    let cjk = matches!(e, Enc::Big5 | Enc::Gbk | Enc::ShiftJis | Enc::EucKr);
    raw.into_iter()
        .map(|(mut it, d)| {
            it.display = if cjk { decode(d, e) } else { utf8_lossy_go(d) };
            it
        })
        .collect()
}

/// 切出每一列;顯示文字先留原始位元組,整份判讀完才解碼。
fn parse_menu_raw(b: &[u8]) -> Vec<(Item, &[u8])> {
    let mut out = Vec::new();
    for ln in b.split(|&c| c == b'\n') {
        let ln = ln.strip_suffix(b"\r").unwrap_or(ln);
        if ln == b"." {
            break;
        }
        if ln.is_empty() {
            out.push((
                Item {
                    typ: TYPE_INFO,
                    ..Default::default()
                },
                &[][..],
            ));
            continue;
        }
        let f: Vec<&[u8]> = ln[1..].split(|&c| c == b'\t').collect();
        let mut it = Item {
            typ: ln[0],
            ..Default::default()
        };
        if f.len() > 1 {
            it.sel = f[1].to_vec();
        }
        if f.len() > 2 {
            it.host = utf8_lossy_go(f[2]);
        }
        if f.len() > 3 {
            it.port = utf8_lossy_go(f[3]);
        }
        out.push((it, f[0]));
    }
    out
}

/// 撥號之後拿到的連線。測試或特殊環境可以給自己的實作。
pub trait Conn: Read + Write + Send {
    /// 設定之後每一次讀寫的期限。`None` 表示不限。
    ///
    /// 不支援期限的連線可以什麼都不做;那時只剩 [`Client::max_bytes`] 擋著。
    fn set_io_timeout(&mut self, d: Option<Duration>) -> io::Result<()> {
        let _ = d;
        Ok(())
    }
}

impl Conn for TcpStream {
    fn set_io_timeout(&mut self, d: Option<Duration>) -> io::Result<()> {
        self.set_read_timeout(d)?;
        self.set_write_timeout(d)
    }
}

/// 撥號函式:收「主機:埠」與連線的期限,回一條連線。
pub type Dial = Arc<dyn Fn(&str, Duration) -> io::Result<Box<dyn Conn>> + Send + Sync>;

/// 取回 gopher 資源。
///
/// 兩個上限都是必要的,不是保險:gopher 沒有 Content-Length,
/// 伺服器可以一直送下去,而這支程式是檔案管理器不是下載器。
#[derive(Clone, Default)]
pub struct Client {
    /// 連線與讀取的期限。零值用 [`DEFAULT_TIMEOUT`]。
    pub timeout: Duration,
    /// 單次取回的上限。零值用 [`DEFAULT_MAX_BYTES`]。
    pub max_bytes: u64,
    /// 可以換掉,測試用。`None` 用標準函式庫的 TCP。
    pub dial: Option<Dial>,
}

pub const DEFAULT_TIMEOUT: Duration = Duration::from_secs(20);
pub const DEFAULT_MAX_BYTES: u64 = 8 << 20;

/// 對方送的東西超過上限時的訊息(與 Go 版的 ErrTooLarge 同一句)。
pub const ERR_TOO_LARGE: &str = "內容超過上限,已截斷";

/// [`Client::fetch`] 的錯誤。
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FetchError {
    /// 內容超過上限。帶著截到上限為止的內容:截斷的東西通常還看得懂前半。
    TooLarge(Vec<u8>),
    /// 其他錯誤,訊息已經過 i18n。
    Other(String),
}

impl fmt::Display for FetchError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            FetchError::TooLarge(_) => f.write_str(ERR_TOO_LARGE),
            FetchError::Other(s) => f.write_str(s),
        }
    }
}

impl std::error::Error for FetchError {}

fn default_dial(addr: &str, timeout: Duration) -> io::Result<Box<dyn Conn>> {
    let mut last = io::Error::new(io::ErrorKind::NotFound, "no address");
    for sa in addr.to_socket_addrs()? {
        match TcpStream::connect_timeout(&sa, timeout) {
            Ok(c) => return Ok(Box::new(c)),
            Err(e) => last = e,
        }
    }
    Err(last)
}

impl Client {
    /// 取回一個資源。
    ///
    /// 回傳的位元組是原樣的:文字的編碼判讀交給 `wincv-textenc`,
    /// 圖片交給 `wincv-imgfmt`。這一層不猜內容是什麼。
    pub fn fetch(&self, u: &Url) -> Result<Vec<u8>, FetchError> {
        if u.host.is_empty() {
            return Err(FetchError::Other(t("沒有主機名")));
        }
        let to = if self.timeout.is_zero() {
            DEFAULT_TIMEOUT
        } else {
            self.timeout
        };
        let max = if self.max_bytes == 0 {
            DEFAULT_MAX_BYTES
        } else {
            self.max_bytes
        };
        let deadline = Instant::now() + to;

        let addr = u.addr();
        let conn = match &self.dial {
            Some(d) => d(&addr, to),
            None => default_dial(&addr, to),
        };
        let mut conn = conn
            .map_err(|e| FetchError::Other(tr!("連不上 %s: %w", addr.as_str(), e.to_string())))?;

        // 請求就是 selector 加 CRLF。型別 7 的查詢字串接在後面,中間一個 tab。
        let mut req = u.sel.clone();
        if u.kind() == TYPE_SEARCH && !u.search.is_empty() {
            req.push(b'\t');
            req.extend_from_slice(u.search.as_bytes());
        }
        req.extend_from_slice(b"\r\n");
        let sent = remaining(deadline)
            .and_then(|r| conn.set_io_timeout(Some(r)))
            .and_then(|_| conn.write_all(&req))
            .and_then(|_| conn.flush());
        if let Err(e) = sent {
            return Err(FetchError::Other(tr!(
                "送出 selector 失敗: %w",
                e.to_string()
            )));
        }

        // 多讀一個位元組,才分得出「剛好等於上限」與「超過上限」。
        let mut data = Vec::new();
        let mut buf = [0u8; 32 * 1024];
        let limit = max.saturating_add(1);
        while (data.len() as u64) < limit {
            let want = ((limit - data.len() as u64) as usize).min(buf.len());
            let n = remaining(deadline)
                .and_then(|r| conn.set_io_timeout(Some(r)))
                .and_then(|_| read_retry(&mut *conn, &mut buf[..want]));
            match n {
                Ok(0) => break,
                Ok(n) => data.extend_from_slice(&buf[..n]),
                Err(e) => return Err(FetchError::Other(tr!("讀取失敗: %w", e.to_string()))),
            }
        }
        if data.len() as u64 > max {
            data.truncate(max as usize);
            return Err(FetchError::TooLarge(data));
        }
        Ok(data)
    }
}

/// 期限還剩多少。已經過了就回逾時錯誤(Go 的 SetDeadline 到期也是這樣)。
fn remaining(deadline: Instant) -> io::Result<Duration> {
    let now = Instant::now();
    if now >= deadline {
        return Err(io::Error::new(io::ErrorKind::TimedOut, "i/o timeout"));
    }
    Ok(deadline - now)
}

fn read_retry(r: &mut dyn Read, buf: &mut [u8]) -> io::Result<usize> {
    loop {
        match r.read(buf) {
            Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
            other => return other,
        }
    }
}

/// Go 的 strings.TrimSpace(Unicode 空白,與 Rust 的 `trim` 同一組字元)。
fn go_trim_space(s: &str) -> &str {
    s.trim()
}

/// Go 的 strings.EqualFold,對象限定是 ASCII 字串時的等價寫法。
///
/// Go 的比對走 Unicode 的簡單大小寫摺疊,所以 K(U+212A,克耳文符號)等於 k、
/// ſ(U+017F,長 s)等於 s。目標字串只有 ASCII,這兩個是唯一的非 ASCII 例外。
pub(crate) fn eq_fold_ascii(s: &str, target: &str) -> bool {
    let mut a = s.chars();
    let mut b = target.chars();
    loop {
        match (a.next(), b.next()) {
            (None, None) => return true,
            (Some(x), Some(y)) => {
                let ok = x == y
                    || x.is_ascii() && x.eq_ignore_ascii_case(&y)
                    || x == '\u{212A}' && y.eq_ignore_ascii_case(&'k')
                    || x == '\u{017F}' && y.eq_ignore_ascii_case(&'s');
                if !ok {
                    return false;
                }
            }
            _ => return false,
        }
    }
}

/// [`eq_fold_ascii`] 的位元組版:Go 拿 `s[:len(prefix)]` 去比,切點可能落在多位元組字中間。
fn eq_fold_ascii_bytes(s: &[u8], target: &[u8]) -> bool {
    eq_fold_ascii(
        &utf8_lossy_go(s),
        std::str::from_utf8(target).unwrap_or_default(),
    )
}
