//! 讀 EPUB 電子書(對應 Go 版 `internal/epub`)。
//!
//! EPUB 就是一個 ZIP:裡面是 XHTML 的章節、圖片,加上一份說明「照什麼
//! 順序讀」的 OPF。所以這一包幾乎沒有自己的解碼工作:ZIP 交給 zip crate,
//! XHTML 交給 `wincv-web` 的 HTML 解析器(那是同一件事:
//! 把標記壓成一串排版區塊),這裡只負責把兩者接起來、找出章節順序。
//!
//! 支援 EPUB 2 與 3。兩者的差別在目錄檔(2 用 NCX、3 用 nav.xhtml),
//! 而閱讀順序都在 OPF 的 spine 裡,所以主要路徑是同一條。
//!
//! OPF、NCX 與 container.xml 的 XML 照 Go 的 encoding/xml 解(見 `goxml`),
//! 壞掉的檔案讀出幾章、哪幾章,與 Go 版相同。zip 內的路徑照 Go 的 `path` 套件接。

mod gopath;
mod goxml;
mod xmltables;

#[cfg(test)]
mod tests;

use std::collections::HashMap;
use std::fs::File;
use std::io::{Cursor, Read, Seek};
use std::sync::Mutex;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind};
use wincv_textenc::utf8_lossy_go;

use goxml::{Dec, Field, GoSlice, Start, Target, XmlError};

/// 單一章節的上限。
///
/// 有些書會把整本塞進一個 XHTML,那種檔案排版起來會讓畫面停很久。
pub const MAX_CHAPTER_BYTES: u64 = 8 << 20;

trait ReadSeek: Read + Seek + Send {}
impl<T: Read + Seek + Send> ReadSeek for T {}

/// 一本打開著的書。
pub struct Book {
    pub title: String,
    pub author: String,
    pub chapters: Vec<Chapter>,

    zr: Mutex<zip::ZipArchive<Box<dyn ReadSeek>>>,
    /// zip 成員的名稱,依中央目錄的順序(Go 的 `zr.File`)。
    names: Vec<String>,
    /// OPF 所在的目錄,章節與圖片的相對路徑以它為基準。
    root: String,
    /// 壓縮檔成員的索引,以正規化過的路徑為鍵,值是成員的序號。
    files: HashMap<String, usize>,
}

/// 閱讀順序上的一章。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Chapter {
    /// 取自目錄;目錄沒寫的話是「第 N 節」。
    pub title: String,
    /// zip 內的路徑(已經接上 root)。
    pub href: String,
}

/// 打開一本書。
pub fn open(name: &str) -> Result<Book, String> {
    let f = File::open(name).map_err(|e| tr!("打不開:%w", e.to_string()))?;
    new_book(Box::new(f))
}

/// 從記憶體裡的一份 EPUB 打開。壓縮檔內部的 .epub 走這條。
pub fn open_bytes(data: Vec<u8>) -> Result<Book, String> {
    new_book(Box::new(Cursor::new(data)))
}

fn new_book(r: Box<dyn ReadSeek>) -> Result<Book, String> {
    let mut zr = zip::ZipArchive::new(r).map_err(|e| tr!("打不開:%w", e.to_string()))?;
    let mut names = Vec::with_capacity(zr.len());
    let mut files = HashMap::new();
    for i in 0..zr.len() {
        let zf = zr
            .by_index_raw(i)
            .map_err(|e| tr!("打不開:%w", e.to_string()))?;
        // Go 的 f.Name 是原始位元組。壞位元組在 norm(ToLower)裡會變成 U+FFFD,
        // 這裡先換,索引的鍵相同。
        let name = utf8_lossy_go(zf.name_raw());
        files.insert(norm(&name), i);
        names.push(name);
    }
    let mut b = Book {
        title: String::new(),
        author: String::new(),
        chapters: Vec::new(),
        zr: Mutex::new(zr),
        names,
        root: String::new(),
        files,
    };
    let opf_path = b.find_opf()?;
    b.root = gopath::dir(&opf_path);
    if b.root == "." {
        b.root.clear();
    }
    b.read_opf(&opf_path)?;
    if b.chapters.is_empty() {
        return Err(tr!("這本書沒有可讀的章節"));
    }
    // 目錄沒寫標題的就編號。空白的章節名在清單上是一整排空行,
    // 看起來像壞掉;實際上只是那本書的目錄沒寫到那一節。
    for (i, c) in b.chapters.iter_mut().enumerate() {
        if c.title.is_empty() {
            c.title = tr!("第 %d 節", (i + 1) as i64);
        }
    }
    Ok(b)
}

// ---- XML 的結構(Go 版的 struct 與 tag)---------------------------------

/// META-INF/container.xml:`xml:"rootfiles>rootfile"`。
#[derive(Default)]
struct Container {
    rootfiles: GoSlice<Rootfile>,
}

#[derive(Default)]
struct Rootfile {
    full_path: String,
}

impl Target for Container {
    const FIELDS: &'static [Field] = &[Field {
        parents: &["rootfiles"],
        name: "rootfile",
    }];
    fn attr(&mut self, _: &str, _: &str) {}
    fn field(&mut self, _: usize, d: &mut Dec, s: &Start) -> Result<(), XmlError> {
        d.objects(&mut self.rootfiles, s)
    }
}

impl Target for Rootfile {
    const FIELDS: &'static [Field] = &[];
    fn attr(&mut self, k: &str, v: &str) {
        if k == "full-path" {
            self.full_path = v.to_string();
        }
    }
    fn field(&mut self, _: usize, _: &mut Dec, _: &Start) -> Result<(), XmlError> {
        Ok(())
    }
}

/// OPF:metadata、manifest、spine。
#[derive(Default)]
struct Opf {
    metadata: Metadata,
    manifest: Manifest,
    spine: Spine,
}

#[derive(Default)]
struct Metadata {
    title: GoSlice<String>,
    creator: GoSlice<String>,
}

#[derive(Default)]
struct Manifest {
    items: GoSlice<ManifestItem>,
}

#[derive(Default, Clone)]
struct ManifestItem {
    id: String,
    href: String,
    media_type: String,
    properties: String,
}

#[derive(Default)]
struct Spine {
    toc: String,
    items: GoSlice<ItemRef>,
}

#[derive(Default)]
struct ItemRef {
    idref: String,
    linear: String,
}

impl Target for Opf {
    const FIELDS: &'static [Field] = &[
        Field {
            parents: &[],
            name: "metadata",
        },
        Field {
            parents: &[],
            name: "manifest",
        },
        Field {
            parents: &[],
            name: "spine",
        },
    ];
    fn attr(&mut self, _: &str, _: &str) {}
    fn field(&mut self, i: usize, d: &mut Dec, s: &Start) -> Result<(), XmlError> {
        match i {
            0 => d.object(&mut self.metadata, s),
            1 => d.object(&mut self.manifest, s),
            _ => d.object(&mut self.spine, s),
        }
    }
}

impl Target for Metadata {
    const FIELDS: &'static [Field] = &[
        Field {
            parents: &[],
            name: "title",
        },
        Field {
            parents: &[],
            name: "creator",
        },
    ];
    fn attr(&mut self, _: &str, _: &str) {}
    fn field(&mut self, i: usize, d: &mut Dec, _: &Start) -> Result<(), XmlError> {
        match i {
            0 => d.strings(&mut self.title),
            _ => d.strings(&mut self.creator),
        }
    }
}

impl Target for Manifest {
    const FIELDS: &'static [Field] = &[Field {
        parents: &[],
        name: "item",
    }];
    fn attr(&mut self, _: &str, _: &str) {}
    fn field(&mut self, _: usize, d: &mut Dec, s: &Start) -> Result<(), XmlError> {
        d.objects(&mut self.items, s)
    }
}

impl Target for ManifestItem {
    const FIELDS: &'static [Field] = &[];
    fn attr(&mut self, k: &str, v: &str) {
        match k {
            "id" => self.id = v.to_string(),
            "href" => self.href = v.to_string(),
            "media-type" => self.media_type = v.to_string(),
            "properties" => self.properties = v.to_string(),
            _ => {}
        }
    }
    fn field(&mut self, _: usize, _: &mut Dec, _: &Start) -> Result<(), XmlError> {
        Ok(())
    }
}

impl Target for Spine {
    const FIELDS: &'static [Field] = &[Field {
        parents: &[],
        name: "itemref",
    }];
    fn attr(&mut self, k: &str, v: &str) {
        if k == "toc" {
            self.toc = v.to_string();
        }
    }
    fn field(&mut self, _: usize, d: &mut Dec, s: &Start) -> Result<(), XmlError> {
        d.objects(&mut self.items, s)
    }
}

impl Target for ItemRef {
    const FIELDS: &'static [Field] = &[];
    fn attr(&mut self, k: &str, v: &str) {
        match k {
            "idref" => self.idref = v.to_string(),
            "linear" => self.linear = v.to_string(),
            _ => {}
        }
    }
    fn field(&mut self, _: usize, _: &mut Dec, _: &Start) -> Result<(), XmlError> {
        Ok(())
    }
}

/// NCX:`xml:"navMap>navPoint"`,每個 navPoint 取 `navLabel>text` 與 `content` 的 src。
#[derive(Default)]
struct Ncx {
    points: GoSlice<NavPoint>,
}

#[derive(Default)]
struct NavPoint {
    label: String,
    content: NavContent,
}

#[derive(Default)]
struct NavContent {
    src: String,
}

impl Target for Ncx {
    const FIELDS: &'static [Field] = &[Field {
        parents: &["navMap"],
        name: "navPoint",
    }];
    fn attr(&mut self, _: &str, _: &str) {}
    fn field(&mut self, _: usize, d: &mut Dec, s: &Start) -> Result<(), XmlError> {
        d.objects(&mut self.points, s)
    }
}

impl Target for NavPoint {
    const FIELDS: &'static [Field] = &[
        Field {
            parents: &["navLabel"],
            name: "text",
        },
        Field {
            parents: &[],
            name: "content",
        },
    ];
    fn attr(&mut self, _: &str, _: &str) {}
    fn field(&mut self, i: usize, d: &mut Dec, s: &Start) -> Result<(), XmlError> {
        match i {
            0 => d.string(&mut self.label),
            _ => d.object(&mut self.content, s),
        }
    }
}

impl Target for NavContent {
    const FIELDS: &'static [Field] = &[];
    fn attr(&mut self, k: &str, v: &str) {
        if k == "src" {
            self.src = v.to_string();
        }
    }
    fn field(&mut self, _: usize, _: &mut Dec, _: &Start) -> Result<(), XmlError> {
        Ok(())
    }
}

impl Book {
    /// 找出 OPF 的位置。
    ///
    /// 規格說一定在 META-INF/container.xml 裡指著。有些書的 container.xml
    /// 壞掉或不見,那時退回「掃一個 .opf 出來」:讀不了整本書
    /// 和少一個索引檔是完全不同的嚴重程度。
    fn find_opf(&self) -> Result<String, String> {
        if let Some(&i) = self.files.get("meta-inf/container.xml") {
            let mut c = Container::default();
            if self.unmarshal(i, &mut c).is_ok() {
                for rf in &c.rootfiles.v {
                    if !rf.full_path.is_empty() && self.files.contains_key(&norm(&rf.full_path)) {
                        return Ok(rf.full_path.clone());
                    }
                }
            }
        }
        for name in &self.names {
            if go_lower(name).ends_with(".opf") {
                return Ok(name.clone());
            }
        }
        Err(tr!("找不到 OPF,這可能不是 EPUB"))
    }

    fn read_opf(&mut self, p: &str) -> Result<(), String> {
        let Some(&f) = self.files.get(&norm(p)) else {
            return Err(tr!("讀不到 %s", p));
        };
        let mut o = Opf::default();
        if let Err(e) = self.unmarshal(f, &mut o) {
            return Err(tr!("OPF 解不開:%w", e));
        }
        if let Some(t) = o.metadata.title.v.first() {
            self.title = t.trim().to_string();
        }
        if let Some(c) = o.metadata.creator.v.first() {
            self.author = c.trim().to_string();
        }

        let mut by_id: HashMap<&str, &str> = HashMap::new();
        let (mut nav_href, mut ncx_href) = ("", "");
        for it in &o.manifest.items.v {
            by_id.insert(&it.id, &it.href);
            if it.properties.contains("nav") {
                nav_href = &it.href;
            }
            if it.media_type.contains("dtbncx") {
                ncx_href = &it.href;
            }
        }
        if ncx_href.is_empty() && !o.spine.toc.is_empty() {
            ncx_href = by_id.get(o.spine.toc.as_str()).copied().unwrap_or("");
        }
        // 目錄只用來取章節標題。取不到就用編號:沒有標題還是讀得下去,
        // 讀不到內容才是問題。
        let titles = self.read_toc(nav_href, ncx_href);

        let mut chapters = Vec::new();
        for r in &o.spine.items.v {
            // linear="no" 是封面、版權頁那類「不在正文順序上」的東西。
            // 仍然收進來:使用者按順序翻到那裡不該撞到一片空白。
            let href = by_id.get(r.idref.as_str()).copied().unwrap_or("");
            if href.is_empty() {
                continue;
            }
            let full = self.resolve(href);
            if !self.files.contains_key(&norm(&full)) {
                continue;
            }
            let title = titles.get(&norm(&full)).cloned().unwrap_or_default();
            chapters.push(Chapter { title, href: full });
        }
        self.chapters = chapters;
        Ok(())
    }

    /// 從目錄檔取「路徑 → 標題」。EPUB 3 用 nav.xhtml,2 用 NCX。
    fn read_toc(&self, nav_href: &str, ncx_href: &str) -> HashMap<String, String> {
        let mut out: HashMap<String, String> = HashMap::new();
        if !ncx_href.is_empty() {
            if let Some(&f) = self.files.get(&norm(&self.resolve(ncx_href))) {
                let mut n = Ncx::default();
                if self.unmarshal(f, &mut n).is_ok() {
                    let base = gopath::dir(&self.resolve(ncx_href));
                    for p in &n.points.v {
                        let src = strip_frag(&p.content.src);
                        if src.is_empty() {
                            continue;
                        }
                        // [雷] 取**第一個**。目錄常常有好幾個 navPoint 指向
                        // 同一個檔案的不同 #片段(整本切成幾個大檔的書都是
                        // 這樣),後寫的會蓋掉先寫的:那本書的第二節就會
                        // 掛著它裡面最後一章的名字。
                        out.entry(norm(&gopath::join(&base, src)))
                            .or_insert_with(|| p.label.trim().to_string());
                    }
                }
            }
        }
        if !nav_href.is_empty() && out.is_empty() {
            if let Some(&f) = self.files.get(&norm(&self.resolve(nav_href))) {
                // nav.xhtml 是 HTML 不是 XML:借 web 的解析器,
                // 連結的位址與文字正好就是目錄要的兩欄。
                if let Ok(data) = self.read(f) {
                    let (_, blocks) = wincv_web::parse_html(None, &wincv_web::decode_markup(&data));
                    let base = gopath::dir(&self.resolve(nav_href));
                    for blk in &blocks {
                        for sp in &blk.spans {
                            if sp.href.is_empty() {
                                continue;
                            }
                            let src = strip_frag(&sp.href);
                            if src.is_empty() {
                                continue;
                            }
                            out.entry(norm(&gopath::join(&base, src)))
                                .or_insert_with(|| sp.text.trim().to_string());
                        }
                    }
                }
            }
        }
        out
    }

    /// 把第 i 章排成區塊。
    pub fn blocks(&self, i: usize) -> Result<Vec<Block>, String> {
        let Some(ch) = self.chapters.get(i) else {
            return Err(tr!("沒有第 %d 章", (i + 1) as i64));
        };
        let Some(&f) = self.files.get(&norm(&ch.href)) else {
            return Err(tr!("讀不到 %s", ch.href.as_str()));
        };
        let data = self.read(f)?;
        // 章節沒有 Content-Type 可依,編碼照 BOM、XML 宣告、meta、內容判讀的順序決定。
        // 不先解碼的話,Big5 的章節是亂碼,開頭的 BOM 也會變成一段空段落。
        //
        // base 傳 None:章節裡的連結是 zip 內的相對路徑,不是網址。
        // 圖片的路徑在 image 那邊接,連結目前不跨章跳(見 app 那一層)。
        let (_, mut blocks) = wincv_web::parse_html(None, &wincv_web::decode_markup(&data));
        // 圖片的 src 接成 zip 內的絕對路徑,image 才找得到。
        let dir = gopath::dir(&ch.href);
        for b in &mut blocks {
            if b.kind == Kind::Image && !b.src.is_empty() {
                b.src = norm(&gopath::join(&dir, &b.src));
            }
        }
        Ok(blocks)
    }

    /// 讀出書裡的一張圖。路徑是 [`Book::blocks`] 給的那個。
    pub fn image(&self, p: &str) -> Result<Vec<u8>, String> {
        let Some(&f) = self.files.get(&norm(p)) else {
            return Err(tr!("書裡沒有 %s", p));
        };
        self.read(f)
    }

    /// 把 OPF 裡的相對路徑接成 zip 內的路徑。
    fn resolve(&self, href: &str) -> String {
        let href = strip_frag(href);
        if self.root.is_empty() {
            return href.to_string();
        }
        gopath::join(&self.root, href)
    }

    /// 讀一個成員,最多 [`MAX_CHAPTER_BYTES`]。
    ///
    /// Go 的 archive/zip 只解 Store 與 Deflate;zip crate 在這個 workspace 開了
    /// 更多方法(給 wincv-archive 用),這裡照 Go 拒絕其他方法,兩邊讀得出的書相同。
    fn read(&self, i: usize) -> Result<Vec<u8>, String> {
        let mut zr = self.zr.lock().unwrap_or_else(|e| e.into_inner());
        let mut zf = zr.by_index(i).map_err(|e| e.to_string())?;
        if !matches!(
            zf.compression(),
            zip::CompressionMethod::Stored | zip::CompressionMethod::Deflated
        ) {
            return Err("zip: unsupported compression algorithm".into());
        }
        let mut data = Vec::new();
        (&mut zf)
            .take(MAX_CHAPTER_BYTES)
            .read_to_end(&mut data)
            .map_err(|e| e.to_string())?;
        Ok(data)
    }

    fn unmarshal<T: Target>(&self, i: usize, v: &mut T) -> Result<(), String> {
        let data = xml_text(self.read(i)?);
        // 先照規格解。OPF 與 NCX 都是正規的 XML,嚴格模式解得出來就不必冒險。
        //
        // [雷] 寬鬆模式會把一本正常的 OPF **整份解成空的而且不回報任何錯誤**:
        // 症狀是「這本書沒有章節」,看起來像檔案壞了。原因是寬鬆模式帶著
        // HTMLAutoClose,把 `<meta>` 當成沒有內容的 HTML 元素,一讀到它的內容就先
        // 關掉它;後面真正的 `</meta>` 對不上,於是把外層的 metadata、package
        // 一路關掉,manifest 與 spine 從此讀不到。EPUB 3 的 OPF 幾乎都有
        // `<meta property="dcterms:modified">…</meta>`。(Go 版的註解把原因寫成
        // 「宣告了多個 xmlns」;實測只宣告 xmlns 不會觸發,有內容的 meta 才會。)
        let cr = data.charset();
        if goxml::decode_with(&data.bytes, v, true, cr).is_ok() {
            return Ok(());
        }
        // 真的壞掉的檔案(未閉合的標籤、HTML 實體)再用寬鬆模式試一次。
        // 注意是解進**同一個** v:嚴格模式已經填進去的部分不會清掉(與 Go 相同)。
        goxml::decode_with(&data.bytes, v, false, cr).map_err(|e| e.to_string())
    }
}

/// 交給 goxml 的 XML 檔:位元組,加上有沒有 BOM。
struct XmlText {
    bytes: Vec<u8>,
    bom: bool,
}

/// UTF-16 的檔案先整份轉成 UTF-8:goxml(與 Go 的 encoding/xml)一個位元組
/// 一個位元組讀,連 `<?xml` 都認不出來。BOM 比宣告確定,有 BOM 的檔案就不再理會 encoding。
fn xml_text(data: Vec<u8>) -> XmlText {
    use wincv_textenc::{Enc, decode};
    if data.starts_with(&[0xFF, 0xFE]) {
        return XmlText {
            bytes: decode(&data, Enc::Utf16Le).into_bytes(),
            bom: true,
        };
    }
    if data.starts_with(&[0xFE, 0xFF]) {
        return XmlText {
            bytes: decode(&data, Enc::Utf16Be).into_bytes(),
            bom: true,
        };
    }
    let bom = data.starts_with(&[0xEF, 0xBB, 0xBF]);
    XmlText { bytes: data, bom }
}

/// 宣告 UTF-8 以外的編碼(舊的中文書常寫 encoding="big5")時,Go 的 encoding/xml
/// 沒有 CharsetReader 就整份拒絕。宣告後面的內容交給 web 的同一套規則解:
/// 認得的編碼照宣告解,宣告看起來是錯的或認不得,改用內容判讀。
fn charset_reader(label: &str, rest: &[u8]) -> Vec<u8> {
    wincv_web::decode_charset(rest, label)
}

fn passthrough(_: &str, rest: &[u8]) -> Vec<u8> {
    rest.to_vec()
}

impl XmlText {
    fn charset(&self) -> Option<goxml::CharsetReader<'static>> {
        Some(if self.bom {
            &passthrough
        } else {
            &charset_reader
        })
    }
}

/// 把 zip 內的路徑正規化,好當索引的鍵。
///
/// 大小寫不敏感:同一本書的 OPF 寫 Text/ch1.xhtml、zip 裡是 text/ch1.xhtml
/// 這種事很常見,而區分大小寫的話整本書都讀不出來。
fn norm(p: &str) -> String {
    let p = p.replace('\\', "/");
    let p = gopath::clean(p.strip_prefix("./").unwrap_or(&p));
    go_lower(p.strip_prefix('/').unwrap_or(&p))
}

fn strip_frag(s: &str) -> &str {
    match s.find(['#', '?']) {
        Some(i) => &s[..i],
        None => s,
    }
}

/// Go 的 strings.ToLower:逐字元的簡單對應(İ 變 i,不是兩個字元)。
fn go_lower(s: &str) -> String {
    s.chars()
        .map(|c| c.to_lowercase().next().unwrap_or(c))
        .collect()
}
