//! 讀 Office Open XML 的封裝格式(OPC)(對應 Go 版 `internal/ooxml`)。
//!
//! `.docx` / `.pptx` / `.xlsx` 三者的外殼是同一套:一個 ZIP,裡面有
//! `[Content_Types].xml` 說明每個組件是什麼,加上散在各處的 `_rels/*.rels`
//! 說明組件之間怎麼互相參照。三者**只共用這層外殼**:裡面的 XML 結構
//! 完全不同,沒有共用的餘地,也不該硬湊。
//!
//! 這一包提供的就是外殼:組件查表、關聯解析、以及一組讓上層寫遞迴下降
//! 解析器時不必重複的 XML 走訪工具。XML 解碼器是 Go `encoding/xml` 的移植
//! (見 [`xml`]),理由寫在那個模組的說明裡。

pub mod gopath;
pub mod xml;
mod xml_tables;

#[cfg(test)]
mod tests;

use std::cell::RefCell;
use std::collections::HashMap;
use std::io::{Read, Seek};
use std::path::Path;
use std::rc::Rc;

use wincv_i18n::tr;
use zip::{CompressionMethod, ZipArchive};

pub use xml::{Attr, Decoder, Error, Name, StartElement, Token};

// 常用的命名空間。比對一律用網址而不是前綴:前綴是檔案自己取的,
// 同一個命名空間在不同檔案裡可以叫不同的名字。
pub const NS_REL: &str = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
pub const NS_DRAWING: &str = "http://schemas.openxmlformats.org/drawingml/2006/main";
pub const NS_WORD: &str = "http://schemas.openxmlformats.org/wordprocessingml/2006/main";
pub const NS_PRES: &str = "http://schemas.openxmlformats.org/presentationml/2006/main";
pub const NS_SHEET: &str = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
pub const NS_MARKUP_C: &str = "http://schemas.openxmlformats.org/markup-compatibility/2006";
pub const NS_PACK_REL: &str = "http://schemas.openxmlformats.org/package/2006/relationships";
pub const NS_CTYPES: &str = "http://schemas.openxmlformats.org/package/2006/content-types";
pub const NS_XML1998: &str = "http://www.w3.org/XML/1998/namespace";
pub const NS_DRAW_WP: &str = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing";
pub const NS_DRAW_SPRE: &str = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing";

/// 單一組件解開後的上限。
///
/// 有這道限制不是為了省記憶體,是因為 ZIP 的解壓比可以做到很大:
/// 一個幾 KB 的檔案能解出好幾 GB。檔案管理器會開到來路不明的檔案。
pub const MAX_PART_BYTES: u64 = 96 << 20;

/// 一筆關聯。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Rel {
    pub id: String,
    pub typ: String,
    pub target: String,
    /// 為真表示 target 是外部位址(http、mailto、磁碟路徑),
    /// 不是包內的組件,不能拿去查表。
    pub external: bool,
}

/// 一個組件的關聯表。
///
/// Go 版用 map:同一個 Id 出現兩次時後面的蓋掉前面的,走訪順序是亂的。
/// 這裡保留第一次出現的位置(值照樣由後面的蓋掉),走訪依文件順序,
/// 結果才固定。
#[derive(Clone, Debug, Default)]
pub struct Rels {
    list: Vec<Rel>,
    idx: HashMap<String, usize>,
}

impl Rels {
    /// 依關聯編號查。
    pub fn get(&self, id: &str) -> Option<&Rel> {
        self.idx.get(id).map(|&i| &self.list[i])
    }

    /// 依文件順序走訪。
    pub fn iter(&self) -> impl Iterator<Item = &Rel> {
        self.list.iter()
    }

    pub fn len(&self) -> usize {
        self.list.len()
    }

    pub fn is_empty(&self) -> bool {
        self.list.is_empty()
    }

    fn insert(&mut self, r: Rel) {
        match self.idx.get(&r.id) {
            Some(&i) => self.list[i] = r,
            None => {
                self.idx.insert(r.id.clone(), self.list.len());
                self.list.push(r);
            }
        }
    }
}

trait ReadSeek: Read + Seek {}
impl<T: Read + Seek> ReadSeek for T {}

/// 一個打開著的 OPC 包。
pub struct Package {
    zr: RefCell<Option<ZipArchive<Box<dyn ReadSeek>>>>,

    files: HashMap<String, Member>,
    names: Vec<String>,
    /// 副檔名(小寫) → 內容型別
    defaults: HashMap<String, String>,
    /// 組件路徑 → 內容型別
    overrides: HashMap<String, String>,
    rels: RefCell<HashMap<String, Rc<Rels>>>,
}

#[derive(Clone, Copy)]
struct Member {
    index: usize,
    size: u64,
    method: CompressionMethod,
}

/// ZIP 的錯誤換成 Go `archive/zip` 的寫法,兩邊的訊息才能並排比對。
fn zip_err(e: zip::result::ZipError) -> String {
    use zip::result::ZipError;
    match e {
        ZipError::Io(e) => {
            let mut m = e.to_string();
            if let Some(i) = m.find(" (os error ") {
                m.truncate(i);
            }
            if e.kind() == std::io::ErrorKind::InvalidData && m.to_lowercase().contains("checksum") {
                return "zip: checksum error".to_string();
            }
            m
        }
        ZipError::CompressionMethodNotSupported(_) => "zip: unsupported compression algorithm".to_string(),
        _ => "zip: not a valid zip file".to_string(),
    }
}

impl Package {
    /// 打開一個 OPC 包。
    pub fn open(name: impl AsRef<Path>) -> Result<Package, String> {
        let p = name.as_ref();
        let f = std::fs::File::open(p).map_err(|e| {
            let mut m = e.to_string();
            if let Some(i) = m.find(" (os error ") {
                m.truncate(i);
            }
            let mut cs = m.chars();
            let m: String = match cs.next() {
                Some(c) => c.to_lowercase().chain(cs).collect(),
                None => m,
            };
            tr!("打不開:%w", format!("open {}: {}", p.to_string_lossy(), m))
        })?;
        Package::from_reader(std::io::BufReader::new(f))
    }

    /// 從任何可以 seek 的來源建 Package(例如記憶體裡的位元組)。
    pub fn from_reader<R: Read + Seek + 'static>(r: R) -> Result<Package, String> {
        let boxed: Box<dyn ReadSeek> = Box::new(r);
        let zr = ZipArchive::new(boxed).map_err(|e| tr!("打不開:%w", zip_err(e)))?;
        let mut files = HashMap::new();
        let mut names = Vec::new();
        for i in 0..zr.len() {
            let Ok(ent) = zr.by_index_data(i) else { continue };
            let raw = ent.name_raw();
            if raw.ends_with(b"/") {
                continue;
            }
            // Go 的 zip 不轉碼檔名,原始位元組直接當字串。
            let n = clean(&wincv_textenc::utf8_lossy_go(raw));
            if files.contains_key(&n) {
                continue;
            }
            files.insert(n.clone(), Member { index: i, size: ent.size(), method: ent.compression() });
            names.push(n);
        }
        let mut p = Package {
            zr: RefCell::new(Some(zr)),
            files,
            names,
            defaults: HashMap::new(),
            overrides: HashMap::new(),
            rels: RefCell::new(HashMap::new()),
        };
        p.read_content_types();
        Ok(p)
    }

    /// 關掉底下的檔案。
    pub fn close(&self) {
        self.zr.borrow_mut().take();
    }

    /// 包裡有沒有這個組件。
    pub fn has(&self, name: &str) -> bool {
        self.files.contains_key(&clean(name))
    }

    /// 所有組件的路徑。
    pub fn names(&self) -> &[String] {
        &self.names
    }

    /// 讀出一個組件。
    pub fn bytes(&self, name: &str) -> Result<Vec<u8>, String> {
        let Some(m) = self.files.get(&clean(name)).copied() else {
            return Err(tr!("包裡沒有 %s", name));
        };
        if m.size > MAX_PART_BYTES {
            return Err(tr!("%s 太大(%d 位元組)", name, m.size));
        }
        // Go 的 archive/zip 只認得儲存與 deflate。zip crate 認得更多,
        // 但同一個 workspace 裡開了別的功能時,這裡也會跟著認得,所以自己擋。
        if m.method != CompressionMethod::Stored && m.method != CompressionMethod::Deflated {
            return Err("zip: unsupported compression algorithm".to_string());
        }
        let mut guard = self.zr.borrow_mut();
        let Some(zr) = guard.as_mut() else {
            return Err("zip: archive closed".to_string());
        };
        let f = zr.by_index(m.index).map_err(zip_err)?;
        // 即使標頭說得小也要夾:標頭的大小欄位是壓縮方寫的,不保證誠實。
        let mut b = Vec::with_capacity(m.size.min(MAX_PART_BYTES) as usize);
        f.take(MAX_PART_BYTES + 1).read_to_end(&mut b).map_err(|e| zip_err(zip::result::ZipError::Io(e)))?;
        if b.len() as u64 > MAX_PART_BYTES {
            return Err(tr!("%s 太大", name));
        }
        Ok(b)
    }

    /// 開一個組件的 XML 解碼器。
    pub fn decoder(&self, name: &str) -> Result<Decoder, String> {
        Ok(new_decoder(self.bytes(name)?))
    }

    /// 查一個組件的內容型別。查不到回空字串。
    pub fn content_type(&self, name: &str) -> String {
        let n = clean(name);
        if let Some(ct) = self.overrides.get(&format!("/{n}")) {
            return ct.clone();
        }
        let ext = gopath::ext(&n);
        let ext = vlower(ext.strip_prefix('.').unwrap_or(&ext));
        self.defaults.get(&ext).cloned().unwrap_or_default()
    }

    /// 找出所有屬於某個內容型別的組件。
    pub fn parts_by_type(&self, ct: &str) -> Vec<String> {
        self.names.iter().filter(|n| self.content_type(n) == ct).cloned().collect()
    }

    fn read_content_types(&mut self) {
        // 沒有這一份就不是合規的 OPC 包,但上層通常是靠固定路徑
        // (word/document.xml)找主體的,所以不當成致命錯誤。
        let Ok(b) = self.bytes("[Content_Types].xml") else { return };
        let mut d = new_decoder(b);
        while let Ok(tok) = d.token() {
            let Token::Start(se) = tok else { continue };
            match se.name.local.as_str() {
                "Default" => {
                    let ext = vlower(&attr(&se, "Extension"));
                    if !ext.is_empty() {
                        self.defaults.insert(ext, attr(&se, "ContentType"));
                    }
                }
                "Override" => {
                    let pn = attr(&se, "PartName");
                    if !pn.is_empty() {
                        self.overrides.insert(pn, attr(&se, "ContentType"));
                    }
                }
                _ => {}
            }
        }
    }

    /// 取一個組件的關聯表。沒有關聯檔就回空表,不是錯誤:
    /// 大部分組件本來就沒有關聯。
    pub fn rels(&self, owner: &str) -> Rc<Rels> {
        let key = clean(owner);
        if let Some(m) = self.rels.borrow().get(&key) {
            return m.clone();
        }
        let mut m = Rels::default();
        if let Ok(b) = self.bytes(&rels_path(owner)) {
            let mut d = new_decoder(b);
            while let Ok(tok) = d.token() {
                let Token::Start(se) = tok else { continue };
                if se.name.local != "Relationship" {
                    continue;
                }
                let r = Rel {
                    id: attr(&se, "Id"),
                    typ: attr(&se, "Type"),
                    target: attr(&se, "Target"),
                    external: xml::equal_fold(&attr(&se, "TargetMode"), "External"),
                };
                if !r.id.is_empty() {
                    m.insert(r);
                }
            }
        }
        let m = Rc::new(m);
        self.rels.borrow_mut().insert(key, m.clone());
        m
    }

    /// 由關聯編號查出包內的絕對路徑。外部位址與查不到的回空字串。
    pub fn rel_target(&self, owner: &str, id: &str) -> String {
        let rels = self.rels(owner);
        match rels.get(id) {
            Some(r) if !r.external && !r.target.is_empty() => resolve(owner, &r.target),
            _ => String::new(),
        }
    }

    /// 取某個組件裡屬於某種型別的關聯,已解析成包內路徑。
    pub fn rels_by_type(&self, owner: &str, typ: &str) -> Vec<String> {
        self.rels(owner)
            .iter()
            .filter(|r| !r.external && r.typ.ends_with(typ))
            .map(|r| resolve(owner, &r.target))
            .collect()
    }
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn vlower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

/// 把 ZIP 內的路徑正規化成查表用的形式。
pub fn clean(name: &str) -> String {
    let n = name.replace('\\', "/");
    let n = n.strip_prefix('/').unwrap_or(&n);
    if n.is_empty() {
        return String::new();
    }
    gopath::clean(n)
}

/// 持有某個組件之關聯的那一份 .rels 的路徑。
/// owner 傳空字串表示包本身的關聯(`_rels/.rels`)。
pub fn rels_path(owner: &str) -> String {
    if owner.is_empty() {
        return "_rels/.rels".to_string();
    }
    let n = clean(owner);
    gopath::join(&[&gopath::dir(&n), "_rels", &format!("{}.rels", gopath::base(&n))])
}

/// 把關聯裡的目標接成包內的絕對路徑。
///
/// [雷] 目標是相對於**持有關聯的那個組件所在的目錄**,不是相對於 ZIP 根。
/// `ppt/slides/slide1.xml` 的關聯寫 `../media/image1.png`,要接成
/// `ppt/media/image1.png`。拿根目錄去接會得到 `media/image1.png`:
/// 查不到,而查不到圖不會報錯,只會少一張圖。
pub fn resolve(owner: &str, target: &str) -> String {
    let t = target.replace('\\', "/");
    if t.starts_with('/') {
        return clean(&t);
    }
    let mut dir = String::new();
    if !owner.is_empty() {
        dir = gopath::dir(&clean(owner));
        if dir == "." {
            dir.clear();
        }
    }
    clean(&gopath::join(&[&dir, &t]))
}

/// 建一個容忍度高一點的 XML 解碼器。
///
/// Office 產生的檔案本身是合規的,但經過各種工具轉手之後不一定,
/// 而一個沒宣告的實體參照不該讓整份文件打不開。非嚴格模式收下這類
/// 實體與錯配的結束標記。
///
/// [雷] 不設 `auto_close`。`HTML_AUTO_CLOSE` 是給 HTML 的空元素用的,只看
/// local name、不分命名空間,而 OOXML 裡也有叫 br、col 的元素:
/// PowerPoint 的換行寫成 `<a:br><a:rPr/></a:br>`,br 一打開就被關掉,
/// 後面的 `</a:br>` 對不上,非嚴格模式把它改成關閉外層並一路往上關,
/// 換行之後的內容全部遺失。OOXML 是 XML,沒有省略結束標記的寫法。
/// 與 Go 版 `ooxml.NewDecoder` 相同。
pub fn new_decoder(b: Vec<u8>) -> Decoder {
    let mut d = Decoder::new(b);
    d.strict = false;
    d.html_entity = true;
    d
}

// --- XML 走訪 ---

/// 依 local name 取屬性。
///
/// 大部分屬性在同一個元素上不會跨命名空間撞名,所以照 local name 取
/// 就夠。會撞的只有關聯編號(`r:id` 與 `id` 常常同時出現),那個用
/// [`rel_id`] 取,不要用這一支。
pub fn attr(se: &StartElement, local: &str) -> String {
    se.attr.iter().find(|a| a.name.local == local).map(|a| a.value.clone()).unwrap_or_default()
}

/// 依命名空間加 local name 取屬性。
pub fn attr_ns(se: &StartElement, space: &str, local: &str) -> String {
    se.attr
        .iter()
        .find(|a| a.name.local == local && a.name.space == space)
        .map(|a| a.value.clone())
        .unwrap_or_default()
}

/// 取關聯編號屬性(`r:id`、`r:embed`、`r:link`)。
///
/// [雷] 不能用 local name 取。`<p:sldId id="256" r:id="rId2"/>` 上面
/// 兩個屬性的 local name 都是 id:照 local name 取會拿到投影片編號 256,
/// 而那是一個合法的字串,不會有任何錯誤,只是查不到組件。
pub fn rel_id(se: &StartElement) -> String {
    for a in &se.attr {
        if a.name.space != NS_REL {
            continue;
        }
        if matches!(a.name.local.as_str(), "id" | "embed" | "link") {
            return a.value.clone();
        }
    }
    String::new()
}

/// 走訪目前元素的每一個子元素,走到對應的結束標記為止。
///
/// f 回傳 true 表示「這個子樹我自己讀完了」(自己遞迴呼叫 each,
/// 或自己呼叫 `d.skip()`);回傳 false 由 each 跳過整個子樹。
/// 兩種情形之後,解碼器都停在該子元素的結束標記之後:這個不變式
/// 是整組工具能巢狀使用的前提。
///
/// 解碼器以參數交給 f,而不是讓 f 捕捉:Go 的閉包可以同時捕捉解碼器又被
/// 傳進去用,Rust 的借用規則不允許,改成傳參數是同一件事。
pub fn each<F>(d: &mut Decoder, mut f: F) -> Result<(), Error>
where
    F: FnMut(&mut Decoder, &StartElement) -> Result<bool, Error>,
{
    loop {
        let tok = match d.token() {
            Ok(t) => t,
            // 截斷的檔案:讀到哪裡算哪裡。已經解出來的內容仍然有用,
            // 而整份拒絕對使用者沒有任何好處。
            Err(Error::Eof) => return Ok(()),
            Err(e) => return Err(e),
        };
        match tok {
            Token::Start(se) => {
                let handled = f(d, &se)?;
                if !handled {
                    match d.skip() {
                        Ok(()) => {}
                        Err(Error::Eof) => return Ok(()),
                        Err(e) => return Err(e),
                    }
                }
            }
            Token::End(_) => return Ok(()),
            _ => {}
        }
    }
}

/// 把目前元素底下的所有文字接起來,走到結束標記為止。
pub fn text(d: &mut Decoder) -> String {
    let mut sb = String::new();
    let mut depth = 0;
    while let Ok(tok) = d.token() {
        match tok {
            Token::CharData(t) => sb.push_str(&t),
            Token::Start(_) => depth += 1,
            Token::End(_) => {
                if depth == 0 {
                    return sb;
                }
                depth -= 1;
            }
            _ => {}
        }
    }
    sb
}

/// 讀到第一個開始標記為止,回傳它。
pub fn root(d: &mut Decoder) -> Result<StartElement, Error> {
    loop {
        if let Token::Start(se) = d.token()? {
            return Ok(se);
        }
    }
}
