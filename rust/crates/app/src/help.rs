//! F1 使用說明(對應 Go 版 `help.go`)。
//!
//! 內嵌而不是讀檔:說明必須在任何情況下都叫得出來,包括沒有安裝目錄、從壓縮檔裡跑、
//! 或是 Android 上根本沒有可讀的程式目錄的時候。

use wincv_i18n::Lang;

use crate::{App, Mode};

/// 說明在文件模式裡用的「檔名」。它不是檔案:session 還原與逐檔位置記憶都要認得它,
/// 才不會拿它去 stat 或記進位置表。
///
/// **不翻譯**:它同時是識別字。翻了的話換一次語言就對不上上一次存的 session。
pub(crate) const HELP_NAME: &str = "使用說明";

/// 目前語言的使用說明。找不到那個語言的版本就用繁體中文的:說明是這個程式最需要
/// 「一定叫得出來」的東西,少一份翻譯不該讓 F1 變成空白畫面。
pub(crate) fn help_text() -> &'static str {
    match wincv_i18n::current() {
        Lang::ZhHans => include_str!("../../../../internal/app/help.zh-Hans.md"),
        Lang::En => include_str!("../../../../internal/app/help.en.md"),
        Lang::Ja => include_str!("../../../../internal/app/help.ja.md"),
        Lang::ZhHant => include_str!("../../../../internal/app/help.md"),
    }
}

impl App {
    /// 用 markdown 模式顯示使用說明。
    ///
    /// 借用既有的 markdown 檢視器而不是另寫一個畫面:說明書要的東西(標題階層、清單、
    /// 表格、程式碼樣式)那邊都已經有了,而且使用者在看 .md 檔時學到的按鍵在這裡照樣通。
    pub(crate) fn open_help(&mut self) -> bool {
        self.md = crate::mdview::MdView {
            name: HELP_NAME.to_string(),
            blocks: wincv_markdown::parse(help_text()),
            ..Default::default()
        };
        self.mode = Mode::Markdown;
        self.md_return = false;
        true
    }
}
