//! 回答「原版素材放在哪裡」(對應 Go 版 `internal/datadir`)。
//!
//! 要找的是原版的半形 `.FON`、倚天字庫、語法上色設定(`keyword_*.cfg`)與字典資料。
//! 這些是第三方版權物,**不隨產物散布**,由使用者自備,所以「使用者把檔案放在哪裡」
//! 這件事程式必須答得出來。
//!
//! [雷] 只認一個相對於工作目錄的 `original/app/` 是不夠的:打包安裝之後,Windows 上
//! 按兩下的工作目錄是桌面或 system32,macOS 上是 `/`,Linux 上是啟動器給的任何地方。
//! 使用者把字型放在執行檔旁邊卻沒有作用,畫面照樣出得來(改用系統字型),看起來像
//! 「這個功能沒做」而不像「檔案沒找到」。

use std::ffi::OsString;
use std::path::{Component, Path, PathBuf};

/// 指定素材目錄的環境變數。按兩下啟動、桌面捷徑、Android 上都沒有地方打旗標,
/// 而那正是工作目錄最不可預測的幾種情形。
pub const ENV_HOME: &str = "WINCV_HOME";

/// 每一個位置底下還要看的子目錄。原版的安裝目錄把半形字型與倚天字庫分開放
/// (`original/app` 與 `original/eten`),而使用者多半會把所有東西丟在同一個資料夾。
const SUBDIRS: [&[&str]; 4] = [&[], &["wincv"], &["original", "app"], &["original", "eten"]];

/// Go 的 `filepath.Join` 會順便整理路徑(去掉 `.`、解開 `..`),去重靠的就是整理過的
/// 字串。`PathBuf::join` 不整理,`"."` 接 `""` 會變成 `"./"`,所以自己整理一次。
fn clean(p: &Path) -> PathBuf {
    let mut out: Vec<Component<'_>> = Vec::new();
    for c in p.components() {
        match c {
            Component::CurDir => {}
            Component::ParentDir => match out.last() {
                Some(Component::Normal(_)) => {
                    out.pop();
                }
                Some(Component::RootDir) | Some(Component::Prefix(_)) => {}
                _ => out.push(c),
            },
            _ => out.push(c),
        }
    }
    if out.is_empty() {
        return PathBuf::from(".");
    }
    out.iter().collect()
}

fn env(k: &str) -> Option<OsString> {
    std::env::var_os(k).filter(|v| !v.is_empty())
}

/// 與 Go 的 `os.UserConfigDir` 相同的規則。
fn user_config_dir() -> Option<PathBuf> {
    if cfg!(windows) {
        return env("AppData").map(PathBuf::from);
    }
    if cfg!(target_os = "macos") {
        return env("HOME").map(|h| PathBuf::from(h).join("Library/Application Support"));
    }
    match env("XDG_CONFIG_HOME").map(PathBuf::from) {
        // Go 版:相對路徑視為錯誤,不退回 $HOME。
        Some(p) => p.is_absolute().then_some(p),
        None => env("HOME").map(|h| PathBuf::from(h).join(".config")),
    }
}

/// 與 Go 的 `os.UserHomeDir` 相同的規則。
fn user_home_dir() -> Option<PathBuf> {
    env(if cfg!(windows) { "USERPROFILE" } else { "HOME" }).map(PathBuf::from)
}

/// 依序列出要找素材的目錄,前面的贏。
///
/// 使用者明講的(環境變數)> 放在程式旁邊的(最多人會這樣做)> 個人設定目錄(與
/// session.json 同一套慣例)> 工作目錄(開發時從 repo 根目錄跑的情形)。
pub fn dirs() -> Vec<PathBuf> {
    let mut roots: Vec<PathBuf> = Vec::new();
    if let Some(h) = env(ENV_HOME) {
        roots.push(PathBuf::from(h));
    }
    if let Ok(exe) = std::env::current_exe() {
        let exe = exe.canonicalize().unwrap_or(exe);
        if let Some(d) = exe.parent() {
            roots.push(d.to_path_buf());
        }
    }
    if let Some(c) = user_config_dir() {
        roots.push(c.join("wincv"));
    }
    if let Some(h) = user_home_dir() {
        roots.push(h.join(".wincv"));
    }
    roots.push(PathBuf::from("."));

    let mut out: Vec<PathBuf> = Vec::new();
    for r in &roots {
        for sub in SUBDIRS {
            let p = clean(&sub.iter().fold(r.clone(), |p, s| p.join(s)));
            if !out.contains(&p) {
                out.push(p);
            }
        }
    }
    out
}

/// 第一個放著 `name` 的目錄。找不到時第二項為假,第一項仍然回一個「最合理的位置」,
/// 讓錯誤訊息有東西可講。
pub fn find(name: &str) -> (PathBuf, bool) {
    let ds = dirs();
    for d in &ds {
        if d.join(name).is_file() {
            return (d.clone(), true);
        }
    }
    (ds.last().cloned().unwrap_or_else(|| PathBuf::from(".")), false)
}

/// 把一個檔名變成完整路徑。已經帶目錄的照原樣回:使用者明講的位置永遠贏。
pub fn resolve(name: &str) -> PathBuf {
    if name.is_empty() {
        return PathBuf::new();
    }
    // Go 的 filepath.Dir(name) != ".":「a」與「./a」都算只有檔名。
    if clean(Path::new(name).parent().unwrap_or(Path::new(""))) != Path::new(".") {
        return PathBuf::from(name);
    }
    let (d, _) = find(name);
    clean(&d.join(name))
}

/// 給命令列旗標用的:使用者給了就照他給的,沒給就自己找。
pub fn resolve2(given: Option<&Path>, name: &str) -> PathBuf {
    match given {
        Some(p) if !p.as_os_str().is_empty() => p.to_path_buf(),
        _ => resolve(name),
    }
}

/// 把搜尋順序排成幾列,給錯誤訊息用。「找不到檔案」不說找過哪裡,使用者只能猜。
pub fn explain() -> String {
    dirs().iter().map(|d| format!("        {}\n", d.display())).collect()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn clean_matches_go_join() {
        assert_eq!(clean(Path::new(".")), PathBuf::from("."));
        assert_eq!(clean(&PathBuf::from(".").join("")), PathBuf::from("."));
        assert_eq!(clean(&PathBuf::from(".").join("original").join("app")), PathBuf::from("original/app"));
        assert_eq!(clean(Path::new("/a/b/../c")), PathBuf::from("/a/c"));
        assert_eq!(clean(Path::new("/..")), PathBuf::from("/"));
    }

    #[test]
    fn resolve_keeps_explicit_path() {
        assert_eq!(resolve("x/y.fon"), PathBuf::from("x/y.fon"));
        assert_eq!(resolve("/abs/y.fon"), PathBuf::from("/abs/y.fon"));
        assert_eq!(resolve(""), PathBuf::new());
        assert_eq!(resolve2(Some(Path::new("given.fon")), "cvga.fon"), PathBuf::from("given.fon"));
    }

    /// 工作目錄與它底下的子目錄一定在清單裡,而且不重複。
    #[test]
    fn dirs_cover_cwd_and_are_deduped() {
        let ds = dirs();
        for want in [".", "wincv", "original/app", "original/eten"] {
            assert!(ds.contains(&PathBuf::from(want)), "少了 {want}: {ds:?}");
        }
        let mut sorted = ds.clone();
        sorted.sort();
        sorted.dedup();
        assert_eq!(sorted.len(), ds.len(), "有重複: {ds:?}");
    }

    /// 找不到也要回一個位置(清單的最後一個),錯誤訊息才有東西可講。
    #[test]
    fn find_falls_back_to_something() {
        let (d, ok) = find("no-such-file-wincv-test.xyz");
        assert!(!ok);
        assert_eq!(Some(&d), dirs().last());
    }
}
