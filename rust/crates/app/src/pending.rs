//! 還沒搬到 Rust 版的文件模式:PDF 與 Office 文件。
//!
//! 它們會借瀏覽模式的殼(與 EPUB 相同),位址分別以 `pdf:`、`doc:` 開頭。在搬過來之前,
//! 這幾種檔案照 Go 版的判斷先攔下來(Office 文件多半是 zip,不攔的話按 Enter 會走進去
//! 看到一堆 xml),只給一行訊息。

use std::path::Path;

use wincv_i18n::{t, tr};
use wincv_render::Image;

use crate::bookview::is_book;
use crate::App;

/// PDF 內位址的前綴。
pub(crate) const PDF_SCHEME: &str = "pdf:";
/// Office 文件內位址的前綴。
pub(crate) const DOC_SCHEME: &str = "doc:";

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
    is_book(name) || e == ".pdf" || OFFICE.contains(&e.as_str())
}

impl App {
    pub(crate) fn open_document(&mut self, name: &str) -> bool {
        if is_book(name) {
            return self.open_book(name);
        }
        self.message = tr!("%s:這種文件的檢視還沒搬到 Rust 版", name);
        true
    }

    pub(crate) fn show_pdf(&mut self, raw: &str) {
        self.bv.status = tr!("%s:這種文件的檢視還沒搬到 Rust 版", raw);
    }

    pub(crate) fn show_office(&mut self, raw: &str) {
        self.bv.status = tr!("%s:這種文件的檢視還沒搬到 Rust 版", raw);
    }

    pub(crate) fn pdf_image(&self, _src: &str) -> Result<Image, String> {
        Err(t("找不到"))
    }

    pub(crate) fn office_image(&self, _src: &str) -> Result<Image, String> {
        Err(t("找不到"))
    }

    pub(crate) fn show_pdf_page_image(&mut self) -> bool {
        false
    }

    pub(crate) fn close_pdf(&mut self) {}

    pub(crate) fn close_office(&mut self) {}
}
