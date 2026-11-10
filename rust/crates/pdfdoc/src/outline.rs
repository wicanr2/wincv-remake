//! 書籤(PDF 自己帶的目錄)。hayro 沒有這一層的 API,直接走物件。
//!
//! 走法照 Go 版 `internal/pdf` 的 `Outline`:`/Outlines` 的 `/First` 起,
//! 每一層沿 `/Next` 走、往 `/First` 下一層,最多 8 層、總數 5000 筆。
//! 標題是空字串的不收,但它底下的子項照收。

use std::collections::HashMap;

use hayro::hayro_syntax::Pdf;
use hayro::hayro_syntax::object::{Array, Dict, MaybeRef, Name, Object, ObjRef};

use crate::Bookmark;

/// 書籤數的上限,擋住損壞檔案裡的迴圈。
pub const MAX_BOOKMARKS: usize = 5000;

/// 名稱樹最多往下走幾層。
const MAX_TREE_DEPTH: usize = 32;

pub(crate) fn outline(pdf: &Pdf) -> Vec<Bookmark> {
    let xref = pdf.xref();
    let Some(cat) = xref.get::<Dict<'_>>(xref.root_id()) else {
        return Vec::new();
    };
    let Some(root) = cat.get::<Dict<'_>>(b"Outlines") else {
        return Vec::new();
    };
    // 物件編號 → 頁碼。與 Go 版相同只看物件編號,不看世代號。
    let mut refs = HashMap::new();
    for (i, p) in pdf.pages().iter().enumerate() {
        if let Some(id) = p.raw().obj_id() {
            refs.entry(ObjRef::from(id).obj_number).or_insert(i + 1);
        }
    }
    let w = Walker { cat: &cat, refs: &refs };
    let mut out = Vec::new();
    w.walk(root.get::<Dict<'_>>(b"First"), 0, &mut out);
    out
}

struct Walker<'w, 'a> {
    cat: &'w Dict<'a>,
    refs: &'w HashMap<u32, usize>,
}

impl<'a> Walker<'_, 'a> {
    fn walk(&self, mut node: Option<Dict<'a>>, level: i32, out: &mut Vec<Bookmark>) {
        let mut i = 0;
        while i < MAX_BOOKMARKS && out.len() < MAX_BOOKMARKS {
            i += 1;
            let Some(n) = node else { return };
            let title = n.get::<hayro::hayro_syntax::object::String<'_>>(b"Title").map(|s| text_string(&s)).unwrap_or_default();
            let page = self.dest_page(&n);
            if !title.is_empty() {
                out.push(Bookmark { title, page, level });
            }
            if level < 8 {
                self.walk(n.get::<Dict<'_>>(b"First"), level + 1, out);
            }
            node = n.get::<Dict<'_>>(b"Next");
        }
    }

    /// 一筆書籤指到第幾頁。目的地有三種寫法:直接的陣列、動作(`/A`)裡的
    /// `/D`、具名目的地。指不到回 0。
    fn dest_page(&self, node: &Dict<'a>) -> usize {
        let dest = node.get::<Object<'_>>(b"Dest").or_else(|| node.get::<Dict<'_>>(b"A").and_then(|a| a.get::<Object<'_>>(b"D")));
        match dest {
            Some(Object::Array(arr)) => self.page_of_dest(&arr),
            Some(Object::Name(n)) => self.named_dest(Key::Name(n)),
            Some(Object::String(s)) => self.named_dest(Key::String(s.as_bytes().to_vec())),
            _ => 0,
        }
    }

    fn page_of_dest(&self, arr: &Array<'a>) -> usize {
        match arr.raw_iter().next() {
            Some(MaybeRef::Ref(r)) => self.refs.get(&r.obj_number).copied().unwrap_or(0),
            // 有些檔案直接寫頁碼(從 0 起算)。
            Some(MaybeRef::NotRef(Object::Number(n))) => {
                let v = n.as_f64();
                if v >= 0.0 { v as usize + 1 } else { 0 }
            }
            _ => 0,
        }
    }

    /// 具名目的地。名稱(PDF 1.1)查目錄的 `/Dests` 字典,字串查 `/Names` 的
    /// `/Dests` 名稱樹;查不到時兩邊都再試一次(有些產生器混著寫)。
    /// 查到的值是陣列,或是帶 `/D` 的字典。
    fn named_dest(&self, key: Key<'a>) -> usize {
        let bytes: Vec<u8> = match &key {
            Key::Name(n) => n.as_ref().to_vec(),
            Key::String(s) => s.clone(),
        };
        let from_dict = || self.cat.get::<Dict<'_>>(b"Dests").and_then(|d| d.get::<Object<'_>>(&bytes));
        let from_tree = || {
            self.cat
                .get::<Dict<'_>>(b"Names")
                .and_then(|n| n.get::<Dict<'_>>(b"Dests"))
                .and_then(|t| name_tree_lookup(&t, &bytes, 0))
        };
        let found = match key {
            Key::Name(_) => from_dict().or_else(from_tree),
            Key::String(_) => from_tree().or_else(from_dict),
        };
        match found {
            Some(Object::Array(a)) => self.page_of_dest(&a),
            Some(Object::Dict(d)) => d.get::<Array<'_>>(b"D").map_or(0, |a| self.page_of_dest(&a)),
            _ => 0,
        }
    }
}

enum Key<'a> {
    Name(Name<'a>),
    String(Vec<u8>),
}

/// 在名稱樹裡找一個鍵。葉節點的 `/Names` 是 `[鍵 值 鍵 值 ...]`,
/// 中間節點的 `/Kids` 靠 `/Limits` 決定要不要往下走。
fn name_tree_lookup<'a>(node: &Dict<'a>, key: &[u8], depth: usize) -> Option<Object<'a>> {
    if depth > MAX_TREE_DEPTH {
        return None;
    }
    if let Some(names) = node.get::<Array<'_>>(b"Names") {
        let mut it = names.iter::<Object<'_>>();
        while let Some(k) = it.next() {
            let v = it.next()?;
            if let Object::String(s) = &k {
                if s.as_bytes() == key {
                    return Some(v);
                }
            }
        }
    }
    let kids = node.get::<Array<'_>>(b"Kids")?;
    for kid in kids.iter::<Dict<'_>>() {
        if let Some(lim) = kid.get::<Array<'_>>(b"Limits") {
            let mut l = lim.iter::<hayro::hayro_syntax::object::String<'_>>();
            if let (Some(lo), Some(hi)) = (l.next(), l.next()) {
                if key < lo.as_bytes() || key > hi.as_bytes() {
                    continue;
                }
            }
        }
        if let Some(v) = name_tree_lookup(&kid, key, depth + 1) {
            return Some(v);
        }
    }
    None
}

/// PDF 的文字字串換成 Rust 字串。規則照 Go 版(pdfcpu 的
/// `StringLiteralToString`):開頭 FE FF 是 UTF-16BE;否則去掉 UTF-8 的
/// BOM,本身是合法 UTF-8 就照用,不是的話每個位元組當一個 Latin-1 碼位
/// (pdfcpu 的函式名叫 `CP1252ToUTF8`,實作是 Latin-1)。UTF-16 要偶數長度,
/// 與 pdfcpu 的 `IsUTF16BE` 相同。
pub fn text_string(b: &[u8]) -> String {
    if b.len() >= 2 && b.len().is_multiple_of(2) && b[0] == 0xFE && b[1] == 0xFF {
        let u: Vec<u16> = b[2..].chunks_exact(2).map(|c| u16::from_be_bytes([c[0], c[1]])).collect();
        return String::from_utf16_lossy(&u);
    }
    let b = b.strip_prefix(&[0xEF, 0xBB, 0xBF][..]).unwrap_or(b);
    match std::str::from_utf8(b) {
        Ok(s) => s.to_string(),
        Err(_) => b.iter().map(|&c| c as char).collect(),
    }
}

