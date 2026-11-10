//! 還沒搬到 Rust 版的模式:EPUB、PDF、Office 文件、網路瀏覽(gopher / http)。
//!
//! 第三階段 3d–3f 會把它們搬過來(見 docs/plan/rust-rewrite.md)。在那之前,這幾種檔案
//! 照 Go 版的判斷先攔下來(它們多半是 zip,不攔的話按 Enter 會走進去看到一堆 xml),
//! 只給一行訊息。

use std::path::Path;

use wincv_i18n::tr;

use crate::App;

/// Go 版 officedoc.Formats 的副檔名。
const OFFICE: &[&str] = &[
    ".docx", ".docm", ".dotx", ".dotm", ".doc", ".dot", ".rtf", ".pptx", ".pptm", ".ppsx", ".ppsm", ".potx", ".xlsx", ".xlsm",
    ".xltx",
];

fn ext(name: &str) -> String {
    Path::new(name).extension().map(|e| format!(".{}", e.to_string_lossy().to_lowercase())).unwrap_or_default()
}

/// 以文件模式開的格式(EPUB、PDF、Office)。
pub(crate) fn is_document(name: &str) -> bool {
    let e = ext(name);
    e == ".epub" || e == ".pdf" || OFFICE.contains(&e.as_str())
}

impl App {
    pub(crate) fn open_document(&mut self, name: &str) -> bool {
        self.message = tr!("%s:這種文件的檢視還沒搬到 Rust 版", name);
        true
    }

    /// F2 / Alt-G 網路瀏覽。
    pub(crate) fn browse_ask(&mut self) -> bool {
        self.message = tr!("%s:這種文件的檢視還沒搬到 Rust 版", "gopher / http");
        true
    }
}
