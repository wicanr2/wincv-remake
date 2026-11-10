//! 命令列指定的起始目標(對應 Go 版 `startup.go`)。

use std::path::Path;

use wincv_i18n::tr;

use crate::App;

impl App {
    /// 套用原版命令列指定的起始目標。
    ///
    /// 目錄只切換瀏覽器;檔案則沿用 Enter 的既有分派,讓圖片、壓縮檔與文件格式和使用者
    /// 從清單按 Enter 時走完全相同的路徑。edit 為真時直接進編輯器,對應原版的 /e。
    pub fn start_path(&mut self, full: &Path, edit: bool) -> Result<(), String> {
        let md = std::fs::metadata(full).map_err(|e| e.to_string())?;
        let shown = full.to_string_lossy();
        if md.is_dir() {
            if edit {
                return Err(tr!("/e 只能指定檔案，不可指定目錄: %q", &*shown));
            }
            return self.browser.load(full).map_err(|e| e.to_string());
        }
        let dir = full.parent().unwrap_or(Path::new("."));
        let name = full.file_name().map(wincv_vfs::display_name).unwrap_or_default();
        self.browser.load(dir).map_err(|e| e.to_string())?;
        self.focus_on(&name);
        if self.browser.current().is_none_or(|e| e.name != name) {
            return Err(tr!("起始檔案 %q 不在目錄 %q 內", &name, &*dir.to_string_lossy()));
        }
        if edit {
            if !self.open_editor() {
                return Err(tr!("無法編輯 %q", &*shown));
            }
            return Ok(());
        }
        self.enter();
        if !self.message.is_empty() {
            return Err(tr!("無法開啟 %q: %s", &*shown, &self.message));
        }
        Ok(())
    }
}
