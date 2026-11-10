//! 把 markdown 拆成適合畫進字元格點的區塊(對應 Go 版 `internal/markdown`)。
//!
//! 為什麼自己寫而不是接一個現成的套件:現成的 markdown 套件產出的是
//! HTML 或抽象語法樹,兩者都要再轉一次才能畫進格子,而中間那一層
//! (行內樣式怎麼落到「第幾格是粗體」)才是這裡真正的工作量。
//! 直接產出「帶樣式的行內片段」少一次轉換,也少一份要對齊的語意。
//!
//! 支援的範圍以「原版 CView 會拿來讀的文件」為準:標題、段落、清單、
//! 引言、程式碼區塊、水平線、圖片、連結、表格、強調與行內程式碼。
//! 沒有做的:HTML 內嵌、腳註、定義清單。遇到會當成普通文字,不會出錯。
//!
//! 輸出與 Go 版逐位元相同(`tools/md-parity.sh` 對照)。Go 的字串是位元組序列,
//! 行內掃描器逐位元組走;這裡的 [`inline`] 也在 `&[u8]` 上走,避免在多位元組字
//! 的中間切 `&str`。

mod inline;
mod layout;
mod parse;
mod wrap;

#[cfg(test)]
mod tests;

use std::ops::{BitAnd, BitOr, BitOrAssign, BitXorAssign};

pub use layout::{default_theme, layout, Line, Loader, Pic, Piece, Theme, MAX_PIC_ROWS};
pub use parse::parse;

/// 區塊的種類。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum Kind {
    #[default]
    Para,
    Heading,
    Code,
    Quote,
    List,
    Rule,
    Image,
    Table,
    /// 「保留原樣」的區塊:不重新斷行、不套程式碼樣式。
    /// markdown 的解析器不會產生它,它是給其他來源(例如 gopher)
    /// 借用這套排版引擎時用的。
    Pre,
}

/// 行內樣式。可以疊加,所以用位元。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default, Hash)]
pub struct Style(pub u8);

impl Style {
    pub const BOLD: Style = Style(1);
    pub const ITALIC: Style = Style(1 << 1);
    pub const MONO: Style = Style(1 << 2);
    pub const LINK: Style = Style(1 << 3);
    pub const STRIKE: Style = Style(1 << 4);

    /// 是否帶有 `s` 的任一個位元(Go 的 `st&s != 0`)。
    pub fn has(self, s: Style) -> bool {
        self.0 & s.0 != 0
    }
}

impl BitOr for Style {
    type Output = Style;
    fn bitor(self, o: Style) -> Style {
        Style(self.0 | o.0)
    }
}

impl BitOrAssign for Style {
    fn bitor_assign(&mut self, o: Style) {
        self.0 |= o.0;
    }
}

impl BitAnd for Style {
    type Output = Style;
    fn bitand(self, o: Style) -> Style {
        Style(self.0 & o.0)
    }
}

impl BitXorAssign for Style {
    fn bitxor_assign(&mut self, o: Style) {
        self.0 ^= o.0;
    }
}

/// 一段同樣式的文字。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Span {
    pub text: String,
    pub style: Style,
    /// 只有 Link 樣式才有意義。
    pub href: String,
}

/// 一個區塊。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Block {
    pub kind: Kind,
    /// 標題階層 1-6;清單的縮排層數。
    pub level: i32,
    /// 程式碼區塊的語言。
    pub lang: String,
    /// 文字內容(Para / Heading / Quote / List)。
    pub spans: Vec<Span>,
    pub lines: Vec<String>,
    // 清單
    pub ordered: bool,
    /// 有序清單的序號。Go 的 int 是 64 位元,超長的數字溢位時照 Go 繞回。
    pub num: i64,
    /// 蓋掉預設的項目符號(• 或編號)。markdown 的解析器不會設它,
    /// 它是給借用這套排版引擎的來源用的,例如 gopher 要在每一列前面
    /// 標出項目型別(「[目錄]」「[文字]」)。
    pub marker: String,
    // 圖片
    pub src: String,
    pub alt: String,
    /// 表格:第一列是表頭。
    pub rows: Vec<Vec<String>>,
    /// `b'l'` `b'c'` `b'r'`。
    pub aligns: Vec<u8>,
}
