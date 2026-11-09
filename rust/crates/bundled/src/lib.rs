//! 「把字型打包進執行檔」的開關(對應 Go 版 `internal/bundled`)。
//!
//! 預設(沒開 `fonts` 功能)是空的。對外散布的產物**不含**原版的 `.FON` 與倚天
//! 字庫,那些是第三方版權物,由使用者自備。
//!
//! 開了 `fonts` 建出來的版本把 `internal/bundled/assets/` 底下的字型嵌進執行檔,
//! 不必另外放檔案就是點陣像素對齊的畫面。那種產物**只留在本機**,不進 release。
//!
//! 兩種建法走同一條載入路徑:呼叫端先看磁碟上有沒有檔案,沒有才問這裡。所以
//! 使用者放在執行檔旁邊的字型永遠優先,內嵌的是後備,不是覆蓋。

include!(concat!(env!("OUT_DIR"), "/assets.rs"));

/// 這個執行檔有沒有內嵌字型。
///
/// 呼叫端用它決定「找不到字型」時要不要報錯:沒有內嵌的版本找不到就是錯,有內嵌的
/// 版本找不到磁碟檔案是正常的。
pub const AVAILABLE: bool = cfg!(feature = "fonts");

/// 取一份內嵌字型。名字是原始檔名(如 "cvga.fon"、"STDFONT.15")。沒有就回 None。
pub fn get(name: &str) -> Option<&'static [u8]> {
    ASSETS.iter().find(|(n, _)| *n == name).map(|(_, b)| *b)
}

/// 標出哪些內嵌檔案是 Unicode 後備字型。
///
/// 用檔名前綴而不是寫死清單:`tools/embed-fonts.sh` 換掉打包的字型時,這裡不必跟著
/// 改。兩份東西分開改,總有一天會對不上。
const FALLBACK_PREFIX: &str = "fb-";

/// 內嵌的 Unicode 後備字型,依檔名排序。
///
/// 排序決定「誰先補到這個字」,所以 `embed-fonts.sh` 用數字前綴命名
/// (fb-10-cjk.ttc、fb-20-symbols.ttf …)。
pub fn fallbacks() -> Vec<&'static [u8]> {
    ASSETS.iter().filter(|(n, _)| n.starts_with(FALLBACK_PREFIX)).map(|(_, b)| *b).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn table_matches_feature() {
        if !AVAILABLE {
            assert!(ASSETS.is_empty() && get("cvga.fon").is_none() && fallbacks().is_empty());
        }
        let names: Vec<_> = ASSETS.iter().map(|(n, _)| *n).collect();
        let mut sorted = names.clone();
        sorted.sort();
        assert_eq!(names, sorted, "內嵌表要依名稱排序,後備字型的先後靠它");
    }
}
