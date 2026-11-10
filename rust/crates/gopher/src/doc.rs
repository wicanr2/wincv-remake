//! 選單與文字檔排成 markdown 區塊(對應 Go 版 `doc.go`)。

use wincv_i18n::t;
use wincv_markdown::{Block, Kind, Span, Style};
use wincv_textenc::{decode, detect};

use crate::*;

/// 項目型別給人看的名稱。
///
/// 用中文而不是原始的型別字元:畫面上一整排 "0"、"1"、"I" 對使用者
/// 沒有意義,而這些字元本來就是給程式看的。
pub fn label(typ: u8) -> String {
    match typ {
        TYPE_TEXT => t("文字"),
        TYPE_MENU => t("目錄"),
        TYPE_ERROR => t("錯誤"),
        TYPE_SEARCH => t("查詢"),
        TYPE_BINARY | TYPE_DOC => t("檔案"),
        TYPE_GIF | TYPE_IMAGE | TYPE_PNG => t("圖片"),
        TYPE_HTML => t("網頁"),
        TYPE_SOUND => t("音訊"),
        b'2' => t("電話簿"),
        b'4' | b'5' | b'6' => t("封裝檔"),
        b'8' | b'T' => t("終端機"),
        b'+' => t("備援"),
        _ => "?".to_string(),
    }
}

/// 這個型別該用看圖模式開。
pub fn is_image(typ: u8) -> bool {
    matches!(typ, TYPE_GIF | TYPE_IMAGE | TYPE_PNG)
}

/// 把選單排成可以交給 `wincv_markdown::layout` 的區塊。
///
/// 每一列一個 List 區塊:選 List 是因為 markdown 的排版引擎在連續的
/// List 之間不插空行,而選單要單倍行距。型別標籤放在 marker,
/// 顯示文字放在 spans,連結掛在 `Span::href`。
///
/// 資訊列(型別 i)沒有目標,所以不給 href,也不給標籤:它就是一行說明,
/// 不該長得像可以點的東西。
pub fn menu_blocks(items: &[Item]) -> Vec<Block> {
    let mut out = Vec::with_capacity(items.len());
    for it in items {
        if !it.is_link() {
            // 資訊列用 Pre:原樣輸出,不加項目符號也不縮排。
            // 那是站台作者自己排的版面(橫線、置中的標題、ASCII banner),
            // 前面多任何一個字元都會把它推歪。
            out.push(Block {
                kind: Kind::Pre,
                lines: vec![it.display.clone()],
                ..Default::default()
            });
            continue;
        }
        let mut sp = Span {
            text: it.display.clone(),
            style: Style::LINK,
            href: String::new(),
        };
        // 網頁連結交出原始位址。這個客戶端不解 HTTP,
        // 上層決定要不要丟給系統瀏覽器。
        sp.href = match it.web_url() {
            Some(web) => web,
            None => it.url().to_string(),
        };
        if sp.text.is_empty() {
            sp.text = sp.href.clone();
        }
        out.push(Block {
            kind: Kind::List,
            marker: format!("[{}] ", label(it.typ)),
            spans: vec![sp],
            ..Default::default()
        });
    }
    out
}

/// 把文字檔排成區塊。
///
/// 用 Pre 而不是 Para:gopher 的文字內容幾乎都是已經排好版的
/// (70 欄的說明、ASCII art、表格),重新斷行會把它們弄壞。
///
/// 編碼交給 textenc 判讀:gopher 沒有 charset 欄位,而中文站台
/// 那個年代多半是 Big5。
pub fn text_blocks(b: &[u8]) -> Vec<Block> {
    let s = decode(b, detect(b)).replace("\r\n", "\n");
    let mut lines: Vec<String> = s.split('\n').map(str::to_string).collect();
    // 協定用單獨一列的 "." 結束,那不是內容。
    if let Some(i) = lines.iter().position(|ln| ln == ".") {
        lines.truncate(i);
    }
    // 尾端的空行沒有意義,但中間的有(段落分隔),只砍尾端。
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
