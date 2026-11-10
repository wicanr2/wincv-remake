//! 把 Word、PowerPoint、Excel 收在同一個介面後面(對應 Go 版 `internal/officedoc`)。
//!
//! 五種格式(.docx / .doc / .rtf / .pptx / .xlsx)的解析差異很大,但對
//! 畫面來說它們是同一種東西:一份文件,可能分成幾段(章節、投影片、
//! 工作表),每一段是一疊排版區塊,外加一些可以按名字取回的圖。
//!
//! 上層只認得這個介面。要加第六種格式時,app 一行都不用改。

#[cfg(test)]
mod tests;

use std::path::Path;

use wincv_i18n::tr;
use wincv_markdown::Block;

/// 文件的種類。用來決定分段叫什麼名字(章節 / 投影片 / 工作表)。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Kind {
    Word,
    Presentation,
    Spreadsheet,
}

impl Kind {
    /// 種類的名稱(Go 的 `Kind.String`)。
    pub fn name(self) -> String {
        match self {
            Kind::Presentation => tr!("簡報"),
            Kind::Spreadsheet => tr!("試算表"),
            Kind::Word => tr!("文件"),
        }
    }

    /// 分段在畫面上的稱呼。
    pub fn part_word(self) -> String {
        match self {
            Kind::Presentation => tr!("投影片"),
            Kind::Spreadsheet => tr!("工作表"),
            Kind::Word => tr!("章節"),
        }
    }
}

/// 文件裡可以分別打開的一段。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub struct Part {
    pub title: String,
}

/// 各格式的接法。
trait Impl {
    fn blocks(&self, i: usize) -> Vec<Block>;
    fn image(&self, r: &str) -> Result<Vec<u8>, String>;
    fn close(&self);
}

/// 一份打開著的 Office 文件。
pub struct Doc {
    pub kind: Kind,
    pub title: String,
    pub parts: Vec<Part>,

    imp: Box<dyn Impl>,
}

/// 支援的副檔名與它們的種類。
///
/// 做成一張表而不是一串 if:這張表同時是「支援哪些格式」的答案,
/// 測試盯著它,不會悄悄過期。
pub static FORMATS: &[(&str, Kind)] = &[
    (".docx", Kind::Word),
    (".docm", Kind::Word),
    (".dotx", Kind::Word),
    (".dotm", Kind::Word),
    (".doc", Kind::Word),
    (".dot", Kind::Word),
    (".rtf", Kind::Word),
    (".pptx", Kind::Presentation),
    (".pptm", Kind::Presentation),
    (".ppsx", Kind::Presentation),
    (".ppsm", Kind::Presentation),
    (".potx", Kind::Presentation),
    (".xlsx", Kind::Spreadsheet),
    (".xlsm", Kind::Spreadsheet),
    (".xltx", Kind::Spreadsheet),
];

/// 查一個副檔名(小寫、含點)的種類。
pub fn format_kind(ext: &str) -> Option<Kind> {
    FORMATS.iter().find(|(e, _)| *e == ext).map(|&(_, k)| k)
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn go_lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

/// 路徑換成字串。不合法的 UTF-8 每個位元組換一個 U+FFFD,與 Go 逐字走訪時看到的字數相同。
fn path_str(p: &Path) -> String {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        wincv_textenc::utf8_lossy_go(p.as_os_str().as_bytes())
    }
    #[cfg(not(unix))]
    {
        p.to_string_lossy().into_owned()
    }
}

fn is_sep(c: char) -> bool {
    c == '/' || (cfg!(windows) && c == '\\')
}

/// Go 的 `filepath.Ext`。
fn ext_of(path: &str) -> String {
    for (i, c) in path.char_indices().rev() {
        if is_sep(c) {
            break;
        }
        if c == '.' {
            return path[i..].to_string();
        }
    }
    String::new()
}

/// Go 的 `filepath.Base`。
fn base_of(path: &str) -> String {
    if path.is_empty() {
        return ".".to_string();
    }
    let p = path.trim_end_matches(is_sep);
    if p.is_empty() {
        return std::path::MAIN_SEPARATOR.to_string();
    }
    match p.rfind(is_sep) {
        Some(i) => p[i + 1..].to_string(),
        None => p.to_string(),
    }
}

/// 一個檔名是不是這一包處理的格式。
pub fn is(name: &str) -> bool {
    format_kind(&go_lower(&ext_of(name))).is_some()
}

/// 打開一份文件。
pub fn open(path: impl AsRef<Path>) -> Result<Doc, String> {
    let path = path.as_ref();
    let shown = path_str(path);
    let ext = go_lower(&ext_of(&shown));
    if format_kind(&ext).is_none() {
        return Err(tr!("不認得的格式:%s", ext));
    }
    match ext.as_str() {
        ".pptx" | ".pptm" | ".ppsx" | ".ppsm" | ".potx" => open_pptx(path, &shown),
        ".xlsx" | ".xlsm" | ".xltx" => open_xlsx(path, &shown),
        ".rtf" => open_rtf(path, &shown),
        ".doc" | ".dot" => {
            // [雷] 副檔名不保證內容。用 .doc 存的 RTF 在真實世界很常見
            // (很多程式「輸出 Word 檔」就是輸出 RTF 再改副檔名),
            // 而那種檔案照複合文件去解會在第一個位元組就失敗。
            if looks_like_rtf(path) {
                return open_rtf(path, &shown);
            }
            open_doc(path, &shown)
        }
        _ => open_docx(path, &shown),
    }
}

fn looks_like_rtf(path: &Path) -> bool {
    use std::io::Read;
    let Ok(mut f) = std::fs::File::open(path) else { return false };
    let mut head = [0u8; 8];
    let n = f.read(&mut head).unwrap_or(0);
    head[..n].starts_with(b"{\\rt")
}

impl Doc {
    /// 排出第 i 段。
    pub fn blocks(&self, i: usize) -> Vec<Block> {
        if i >= self.parts.len() {
            return Vec::new();
        }
        self.imp.blocks(i)
    }

    /// 取一張圖。
    pub fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        self.imp.image(r)
    }

    pub fn close(&self) {
        self.imp.close();
    }
}

// --- 各格式的接法 ---

struct DocxDoc(wincv_docx::Doc);

impl Impl for DocxDoc {
    fn blocks(&self, _: usize) -> Vec<Block> {
        self.0.blocks()
    }
    fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        self.0.image(r)
    }
    fn close(&self) {
        self.0.close()
    }
}

fn open_docx(path: &Path, shown: &str) -> Result<Doc, String> {
    let w = wincv_docx::Doc::open(path)?;
    Ok(Doc {
        kind: Kind::Word,
        title: title_or(&w.title, shown),
        parts: vec![Part { title: base_of(shown) }],
        imp: Box::new(DocxDoc(w)),
    })
}

struct Doc97Doc(wincv_doc97::Doc);

impl Impl for Doc97Doc {
    fn blocks(&self, _: usize) -> Vec<Block> {
        self.0.blocks()
    }
    fn image(&self, _: &str) -> Result<Vec<u8>, String> {
        Err(tr!("這個格式的圖片尚未支援"))
    }
    fn close(&self) {
        self.0.close()
    }
}

fn open_doc(path: &Path, shown: &str) -> Result<Doc, String> {
    let w = wincv_doc97::Doc::open(path)?;
    Ok(Doc { kind: Kind::Word, title: base_of(shown), parts: vec![Part { title: base_of(shown) }], imp: Box::new(Doc97Doc(w)) })
}

struct RtfDoc(wincv_rtf::Doc);

impl Impl for RtfDoc {
    fn blocks(&self, _: usize) -> Vec<Block> {
        self.0.blocks()
    }
    fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        self.0.image(r)
    }
    fn close(&self) {
        self.0.close()
    }
}

fn open_rtf(path: &Path, shown: &str) -> Result<Doc, String> {
    let w = wincv_rtf::Doc::open(path)?;
    Ok(Doc { kind: Kind::Word, title: base_of(shown), parts: vec![Part { title: base_of(shown) }], imp: Box::new(RtfDoc(w)) })
}

struct PptxDoc(wincv_pptx::Deck);

impl Impl for PptxDoc {
    fn blocks(&self, i: usize) -> Vec<Block> {
        self.0.slides.get(i).map(|s| s.blocks.clone()).unwrap_or_default()
    }
    fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        self.0.image(r)
    }
    fn close(&self) {
        self.0.close()
    }
}

fn open_pptx(path: &Path, shown: &str) -> Result<Doc, String> {
    let p = wincv_pptx::Deck::open(path)?;
    let parts = p
        .slides
        .iter()
        .enumerate()
        .map(|(i, s)| Part { title: if s.title.is_empty() { tr!("第 %d 張", i + 1) } else { s.title.clone() } })
        .collect();
    Ok(Doc { kind: Kind::Presentation, title: title_or(&p.title, shown), parts, imp: Box::new(PptxDoc(p)) })
}

struct XlsxDoc(wincv_xlsx::Book);

impl Impl for XlsxDoc {
    fn blocks(&self, i: usize) -> Vec<Block> {
        self.0.blocks(i)
    }
    fn image(&self, _: &str) -> Result<Vec<u8>, String> {
        Err(tr!("試算表裡沒有圖"))
    }
    fn close(&self) {
        self.0.close()
    }
}

fn open_xlsx(path: &Path, shown: &str) -> Result<Doc, String> {
    let b = wincv_xlsx::Book::open(path)?;
    let parts = b
        .sheets
        .iter()
        .enumerate()
        .map(|(i, s)| {
            let mut t = if s.name.is_empty() { tr!("工作表 %d", i + 1) } else { s.name.clone() };
            if s.hidden {
                t += &tr!("(隱藏)");
            }
            Part { title: t }
        })
        .collect();
    Ok(Doc { kind: Kind::Spreadsheet, title: title_or(&b.title, shown), parts, imp: Box::new(XlsxDoc(b)) })
}

fn title_or(title: &str, path: &str) -> String {
    if !title.trim().is_empty() {
        return title.to_string();
    }
    base_of(path)
}
