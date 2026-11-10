//! 讀 Word 的 .docx(WordprocessingML)(對應 Go 版 `internal/docx`)。
//!
//! 產出的是 `wincv-markdown` 的區塊,不是 HTML 也不是 DOM:畫面只有
//! 字元格點,能表現的就是標題、段落、清單、表格、圖片與行內樣式那幾種,
//! 多解出來的東西沒有地方畫。
//!
//! 走的是遞迴下降:XML 的巢狀結構(表格裡有段落,段落裡有超連結,
//! 超連結裡有文字段)剛好對應函式呼叫的巢狀,不必自己維護堆疊,
//! 也不必把整份文件建成樹:一份幾百頁的 document.xml 建成樹會是
//! 好幾百 MB,而這裡從頭到尾只留產出的區塊。

mod conv;

#[cfg(test)]
mod tests;

use std::cell::RefCell;
use std::collections::HashMap;
use std::path::Path;

use wincv_i18n::tr;
use wincv_markdown::{Block, Kind, Span};
use wincv_ooxml::{self as ooxml, Package, StartElement};

/// 一份打開著的 .docx。
pub struct Doc {
    pub title: String,

    pkg: Package,
    /// 主體組件的路徑,通常是 word/document.xml
    main: String,

    styles: HashMap<String, StyleDef>,
    /// numId → 這一串的各層設定
    nums: HashMap<String, Numbering>,
    /// 解析時遇到的圖片,鍵是給上層用的參照,值是包內路徑。
    imgs: RefCell<HashMap<String, String>>,
}

#[derive(Clone, Debug, Default)]
pub(crate) struct StyleDef {
    pub id: String,
    pub name: String,
    pub based_on: String,
    /// 0-8;-1 表示沒有
    pub outline: i64,
    pub bold: bool,
    pub italic: bool,
    pub num_id: String,
}

#[derive(Clone, Debug, Default)]
pub(crate) struct Numbering {
    pub lvl: HashMap<i64, NumLevel>,
}

#[derive(Clone, Debug, Default)]
pub(crate) struct NumLevel {
    pub format: String,
    pub text: String,
    pub start: i64,
    pub ordered: bool,
}

impl Doc {
    /// 打開一份 .docx。
    pub fn open(name: impl AsRef<Path>) -> Result<Doc, String> {
        let p = Package::open(name)?;
        Doc::new(p)
    }

    /// 從一個已經打開的 OPC 包建 Doc。Doc 會接手它的生命週期。
    pub fn new(p: Package) -> Result<Doc, String> {
        let main = main_part(&p);
        if main.is_empty() {
            p.close();
            return Err(tr!("這不是 Word 文件(找不到 document.xml)"));
        }
        let mut d = Doc {
            title: doc_title(&p),
            pkg: p,
            main,
            styles: HashMap::new(),
            nums: HashMap::new(),
            imgs: RefCell::new(HashMap::new()),
        };
        d.read_styles();
        d.read_numbering();
        Ok(d)
    }

    pub fn close(&self) {
        self.pkg.close();
    }

    /// 讀樣式表。要的只有三件事:這個樣式是不是標題(第幾階)、
    /// 它預設粗體或斜體嗎、它有沒有綁一串編號。
    fn read_styles(&mut self) {
        let part = self.rel_part("/styles");
        if part.is_empty() {
            return;
        }
        let Ok(b) = self.pkg.bytes(&part) else { return };
        let mut dec = ooxml::new_decoder(b);
        match ooxml::root(&mut dec) {
            Ok(r) if r.name.local == "styles" => {}
            _ => return,
        }
        let styles = &mut self.styles;
        let _ = ooxml::each(&mut dec, |d, se| {
            if se.name.local != "style" {
                return Ok(false);
            }
            let mut st = StyleDef { id: ooxml::attr(se, "styleId"), outline: -1, ..Default::default() };
            let r = ooxml::each(d, |d, c| {
                match c.name.local.as_str() {
                    "name" => st.name = ooxml::attr(c, "val"),
                    "basedOn" => st.based_on = ooxml::attr(c, "val"),
                    "pPr" => {
                        ooxml::each(d, |d, pp| {
                            match pp.name.local.as_str() {
                                "outlineLvl" => st.outline = atoi_def(&ooxml::attr(pp, "val"), -1),
                                "numPr" => {
                                    ooxml::each(d, |_, np| {
                                        if np.name.local == "numId" {
                                            st.num_id = ooxml::attr(np, "val");
                                        }
                                        Ok(false)
                                    })?;
                                    return Ok(true);
                                }
                                _ => {}
                            }
                            Ok(false)
                        })?;
                        return Ok(true);
                    }
                    "rPr" => {
                        ooxml::each(d, |_, rp| {
                            match rp.name.local.as_str() {
                                "b" => st.bold = on_off(rp),
                                "i" => st.italic = on_off(rp),
                                _ => {}
                            }
                            Ok(false)
                        })?;
                        return Ok(true);
                    }
                    _ => {}
                }
                Ok(false)
            });
            if !st.id.is_empty() {
                styles.insert(st.id.clone(), st);
            }
            r.map(|_| true)
        });
    }

    /// 算出一個樣式代表第幾階標題,0 表示不是標題。
    ///
    /// 先看 outlineLvl 再看名字:outlineLvl 是 Word 自己用來建目錄的欄位,
    /// 是這件事的真值;名字則會被翻譯(中文版是「標題 1」)、被使用者改。
    /// 兩個都沒有就沿著 basedOn 往上找:自訂樣式常常只是「以標題 1 為基礎」。
    pub(crate) fn heading_level(&self, id: &str) -> i64 {
        let mut id = id.to_string();
        let mut i = 0;
        // 上限擋住互相 basedOn 的迴圈
        while i < 8 && !id.is_empty() {
            let Some(st) = self.styles.get(&id) else { return 0 };
            if (0..=8).contains(&st.outline) {
                return (st.outline + 1).clamp(1, 6);
            }
            let n = heading_by_name(&st.name);
            if n > 0 {
                return n;
            }
            let n = heading_by_name(&st.id);
            if n > 0 {
                return n;
            }
            id = st.based_on.clone();
            i += 1;
        }
        0
    }

    /// 讀編號定義。
    ///
    /// 有兩層轉接:段落寫的是 numId,numId 指到 abstractNumId,層級設定在
    /// abstractNum 底下。中間那一層存在的理由是「同一套編號格式可以被好幾串
    /// 清單共用,但各自從頭數」。
    fn read_numbering(&mut self) {
        let part = self.rel_part("/numbering");
        if part.is_empty() {
            return;
        }
        let Ok(b) = self.pkg.bytes(&part) else { return };
        let mut abstract_: HashMap<String, Numbering> = HashMap::new();
        // numId → abstractNumId
        let mut link: HashMap<String, String> = HashMap::new();

        let mut dec = ooxml::new_decoder(b);
        match ooxml::root(&mut dec) {
            Ok(r) if r.name.local == "numbering" => {}
            _ => return,
        }
        let _ = ooxml::each(&mut dec, |d, se| match se.name.local.as_str() {
            "abstractNum" => {
                let id = ooxml::attr(se, "abstractNumId");
                let mut n = Numbering::default();
                let r = ooxml::each(d, |d, c| {
                    if c.name.local != "lvl" {
                        return Ok(false);
                    }
                    let ilvl = atoi_def(&ooxml::attr(c, "ilvl"), 0);
                    let mut lv = NumLevel { start: 1, ..Default::default() };
                    let r = ooxml::each(d, |_, l| {
                        match l.name.local.as_str() {
                            "start" => lv.start = atoi_def(&ooxml::attr(l, "val"), 1),
                            "numFmt" => lv.format = ooxml::attr(l, "val"),
                            "lvlText" => lv.text = ooxml::attr(l, "val"),
                            _ => {}
                        }
                        Ok(false)
                    });
                    lv.ordered = !lv.format.is_empty() && lv.format != "bullet" && lv.format != "none";
                    n.lvl.insert(ilvl, lv);
                    r.map(|_| true)
                });
                abstract_.insert(id, n);
                r.map(|_| true)
            }
            "num" => {
                let id = ooxml::attr(se, "numId");
                ooxml::each(d, |_, c| {
                    if c.name.local == "abstractNumId" {
                        link.insert(id.clone(), ooxml::attr(c, "val"));
                    }
                    Ok(false)
                })
                .map(|_| true)
            }
            _ => Ok(false),
        });
        for (num_id, abs_id) in link {
            if let Some(n) = abstract_.get(&abs_id) {
                self.nums.insert(num_id, n.clone());
            }
        }
    }

    pub(crate) fn rel_part(&self, typ: &str) -> String {
        self.pkg.rels_by_type(&self.main, typ).into_iter().find(|t| self.pkg.has(t)).unwrap_or_default()
    }

    /// 取一張解析時遇到的圖。
    pub fn image(&self, r: &str) -> Result<Vec<u8>, String> {
        let part = self.imgs.borrow().get(r).cloned();
        match part {
            Some(p) => self.pkg.bytes(&p),
            None => Err(tr!("這份文件裡沒有 %s", r)),
        }
    }

    /// 排出整份文件。
    pub fn blocks(&self) -> Vec<Block> {
        let mut c = conv::Conv::new(self);
        if let Ok(b) = self.pkg.bytes(&self.main) {
            let mut dec = ooxml::new_decoder(b);
            if let Ok(root) = ooxml::root(&mut dec) {
                if root.name.local == "document" {
                    let main = self.main.clone();
                    let _ = ooxml::each(&mut dec, |d, se| {
                        if se.name.local == "body" {
                            return c.body(d, &main).map(|_| true);
                        }
                        Ok(false)
                    });
                }
            }
        }
        c.notes();
        let mut out = c.out;
        if out.is_empty() {
            out.push(Block {
                kind: Kind::Para,
                spans: vec![Span { text: tr!("(這份文件沒有可以顯示的內容)"), ..Default::default() }],
                ..Default::default()
            });
        }
        out
    }
}

/// 找主體組件。
///
/// 先問包的關聯而不是直接用 word/document.xml:巨集啟用的 .docm 與
/// 範本 .dotx 主體叫別的名字,而關聯型別三者相同。
fn main_part(p: &Package) -> String {
    if let Some(t) = p.rels_by_type("", "/officeDocument").into_iter().find(|t| p.has(t)) {
        return t;
    }
    for n in ["word/document.xml", "word/document2.xml"] {
        if p.has(n) {
            return n.to_string();
        }
    }
    String::new()
}

fn doc_title(p: &Package) -> String {
    for t in p.rels_by_type("", "/metadata/core-properties") {
        let Ok(b) = p.bytes(&t) else { continue };
        let mut dec = ooxml::new_decoder(b);
        let mut title = String::new();
        let _ = ooxml::each(&mut dec, |d, se| {
            if se.name.local == "coreProperties" {
                return ooxml::each(d, |d, c| {
                    if c.name.local == "title" {
                        title = ooxml::text(d).trim().to_string();
                        return Ok(true);
                    }
                    Ok(false)
                })
                .map(|_| true);
            }
            Ok(false)
        });
        if !title.is_empty() {
            return title;
        }
    }
    String::new()
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
pub(crate) fn go_lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

/// Go 的 strconv.Atoi:可以有正負號,不能有空白;超出 int 範圍是錯誤。
pub(crate) fn atoi(s: &str) -> Option<i64> {
    s.parse::<i64>().ok()
}

pub(crate) fn heading_by_name(name: &str) -> i64 {
    let s = go_lower(name.trim());
    for pre in [
        "heading ".to_string(),
        "heading".to_string(),
        tr!("標題 "),
        tr!("標題"),
        tr!("标题 "),
        tr!("标题"),
    ] {
        if let Some(rest) = s.strip_prefix(pre.as_str()) {
            if let Some(n) = atoi(rest.trim()) {
                if (1..=9).contains(&n) {
                    return n.clamp(1, 6);
                }
            }
        }
    }
    if s == "title" || s == tr!("標題") {
        return 1;
    }
    if s == "subtitle" || s == tr!("副標題") {
        return 2;
    }
    0
}

pub(crate) fn atoi_def(s: &str, def: i64) -> i64 {
    atoi(s.trim()).unwrap_or(def)
}

/// 判讀 Word 的開關屬性。
///
/// [雷] `<w:b/>` 沒有 val 屬性時是**開**,不是關。照「沒有值就是 false」
/// 寫的話所有粗體都會消失:而畫面看起來完全正常,只是沒有粗體。
pub(crate) fn on_off(se: &StartElement) -> bool {
    matches!(go_lower(&ooxml::attr(se, "val")).as_str(), "" | "1" | "true" | "on")
}
