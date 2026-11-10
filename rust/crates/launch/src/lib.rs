//! 開啟或執行檔案(對應 Go 版 `internal/launch`)。
//!
//! 原版的 O(開啟)與 G(執行)在 Win32 下都走 ShellExecute。跨平台的對應物三個系統
//! 各不相同,這裡把差異收在一處。
//!
//! 一律**不等它結束**:原版按下去之後馬上回到檔案列表,而不是卡住。

use std::path::Path;
use std::process::{Command, Stdio};

/// 用系統預設的程式開啟一個檔案或目錄。
pub fn open(path: &Path) -> Result<(), String> {
    let mut c = if cfg!(target_os = "macos") {
        Command::new("open")
    } else if cfg!(windows) {
        let mut c = Command::new("cmd");
        c.args(["/c", "start", ""]);
        c
    } else {
        Command::new("xdg-open")
    };
    c.arg(path);
    start(c, None)
}

/// 在 dir 底下執行一行命令。
///
/// 交給 shell 跑,因為使用者輸入的是一整行(可能帶參數、管線),不是一個程式名加
/// 一串已經切好的參數。
pub fn run(dir: &Path, cmdline: &str) -> Result<(), String> {
    if cmdline.trim().is_empty() {
        return Err(wincv_i18n::t("沒有命令"));
    }
    let c = if cfg!(windows) {
        let mut c = Command::new("cmd");
        c.args(["/c", cmdline]);
        c
    } else {
        let sh = std::env::var("SHELL").ok().filter(|s| !s.is_empty()).unwrap_or_else(|| "/bin/sh".into());
        let mut c = Command::new(sh);
        c.args(["-c", cmdline]);
        c
    };
    start(c, Some(dir))
}

/// 這個檔案能不能直接執行。
pub fn executable(path: &Path) -> bool {
    let Ok(m) = std::fs::metadata(path) else { return false };
    if m.is_dir() {
        return false;
    }
    if cfg!(windows) {
        let l = path.to_string_lossy().to_lowercase();
        return [".exe", ".com", ".bat", ".cmd"].iter().any(|e| l.ends_with(e));
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        m.permissions().mode() & 0o111 != 0
    }
    #[cfg(not(unix))]
    false
}

fn start(mut c: Command, dir: Option<&Path>) -> Result<(), String> {
    if let Some(d) = dir.filter(|d| !d.as_os_str().is_empty()) {
        c.current_dir(d);
    }
    // 不接標準輸入輸出:這是 GUI 程式,沒有終端可接,而且接了之後子程序印東西會卡在管線上。
    c.stdin(Stdio::null());
    let mut child = c.spawn().map_err(|e| e.to_string())?;
    // 收掉子程序,不要留殭屍。
    std::thread::spawn(move || {
        let _ = child.wait();
    });
    Ok(())
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn run_rejects_empty_command() {
        assert!(run(Path::new("."), "   ").is_err());
    }

    #[cfg(unix)]
    #[test]
    fn executable_checks_mode_bits() {
        assert!(executable(Path::new("/bin/sh")));
        assert!(!executable(Path::new("/")));
        assert!(!executable(Path::new(env!("CARGO_MANIFEST_DIR")).join("Cargo.toml").as_path()));
    }
}
