//! 產生內嵌檔案的表。
//!
//! Go 版用 `//go:embed assets` 一次嵌整個目錄;Rust 的 `include_bytes!` 只能嵌單一
//! 檔案,所以在建置時列目錄、為每個檔案產生一行 `include_bytes!`。沒開 `fonts`
//! 功能時產生空表,不碰那個目錄。

use std::fmt::Write;
use std::path::PathBuf;

fn main() {
    let out = PathBuf::from(std::env::var("OUT_DIR").unwrap()).join("assets.rs");
    println!("cargo:rerun-if-env-changed=WINCV_BUNDLED_DIR");
    let mut src = String::from("pub static ASSETS: &[(&str, &[u8])] = &[\n");
    if std::env::var_os("CARGO_FEATURE_FONTS").is_some() {
        let dir = match std::env::var_os("WINCV_BUNDLED_DIR") {
            Some(d) => PathBuf::from(d),
            None => PathBuf::from(std::env::var("CARGO_MANIFEST_DIR").unwrap()).join("../../../internal/bundled/assets"),
        };
        println!("cargo:rerun-if-changed={}", dir.display());
        let entries = std::fs::read_dir(&dir).unwrap_or_else(|e| {
            panic!("fonts 功能要內嵌 {} 底下的字型,但讀不到這個目錄 ({e});先跑 tools/embed-fonts.sh full", dir.display())
        });
        let mut files: Vec<(String, PathBuf)> = entries
            .filter_map(|e| e.ok())
            .filter(|e| e.file_type().map(|t| t.is_file()).unwrap_or(false))
            .map(|e| (e.file_name().to_string_lossy().into_owned(), e.path()))
            .collect();
        // 名稱排序:Go 的 fs.ReadDir 也是依名稱回傳,後備字型的先後(fb-10、fb-20…)靠這個。
        files.sort();
        for (name, path) in files {
            println!("cargo:rerun-if-changed={}", path.display());
            let abs = path.canonicalize().unwrap_or(path);
            writeln!(src, "    ({name:?}, include_bytes!({:?})),", abs.display().to_string()).unwrap();
        }
    }
    src.push_str("];\n");
    std::fs::write(out, src).unwrap();
}
