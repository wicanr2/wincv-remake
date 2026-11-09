//! 照搬 Go 版 `internal/ttf` 的測試。需要真字型的那幾條在這台機器沒有字型時跳過。

use super::paths::{scan, Host};
use super::*;
use std::path::PathBuf;

fn fake<'a>(os: &'a str, env: &'a dyn Fn(&str) -> Option<String>) -> Host<'a> {
    Host { os, env, home: Some(PathBuf::from("/home/u")) }
}

fn none(_: &str) -> Option<String> {
    None
}

fn has(list: &[PathBuf], want: &str) -> bool {
    list.iter().any(|p| p == Path::new(want))
}

/// Windows 的系統目錄不一定在 C:,「只為我安裝」的字型不在系統目錄裡。
#[test]
fn windows_font_dirs() {
    let env = |k: &str| match k {
        "SystemRoot" => Some(r"D:\WinNT".to_string()),
        "LOCALAPPDATA" => Some(r"C:\Users\u\AppData\Local".to_string()),
        _ => None,
    };
    let h = fake("windows", &env);
    let dirs = h.font_dirs();
    assert!(has(&dirs, &Path::new(r"D:\WinNT").join("Fonts").to_string_lossy()));
    assert!(dirs.iter().any(|d| d.to_string_lossy().contains("Microsoft")));
    let only_windir = |k: &str| (k == "windir").then(|| r"E:\Win".to_string());
    assert!(fake("windows", &only_windir).font_dirs()[0].to_string_lossy().starts_with(r"E:\Win"));
    assert_eq!(fake("windows", &none).font_dirs()[0], PathBuf::from(r"C:\Windows\Fonts"));
    // 寫死的候選清單也要跟著系統目錄走,不能自己拼 C:\。
    assert!(h.fixed_candidates().iter().all(|p| !p.to_string_lossy().starts_with(r"C:\Windows")));
}

/// macOS 從 Catalina 起把附帶的字型搬進 Supplemental。
#[test]
fn macos_font_dirs() {
    let dirs = fake("macos", &none).font_dirs();
    for d in ["/System/Library/Fonts", "/System/Library/Fonts/Supplemental", "/Library/Fonts", "/home/u/Library/Fonts"] {
        assert!(has(&dirs, d), "{d}");
    }
}

/// Android 10 之後系統被拆成好幾個分割區;候選清單要對每一個目錄各展開一次。
#[test]
fn android_font_dirs() {
    let h = fake("android", &none);
    for d in ["/system/fonts", "/product/fonts", "/system_ext/fonts"] {
        assert!(has(&h.font_dirs(), d));
    }
    let c = h.fixed_candidates();
    assert!(has(&c, "/system/fonts/DroidSansMono.ttf") && has(&c, "/product/fonts/NotoSansCJK-Regular.ttc"));
}

/// Linux:XDG 的兩個變數照規格解讀,容器裡主機的字型掛在別的地方。
#[test]
fn linux_font_dirs() {
    let env = |k: &str| match k {
        "XDG_DATA_HOME" => Some("/home/u/.xdgdata".to_string()),
        "XDG_DATA_DIRS" => Some("/opt/share:/srv/share".to_string()),
        _ => None,
    };
    let dirs = fake("linux", &env).font_dirs();
    for d in ["/usr/share/fonts", "/usr/local/share/fonts", "/home/u/.xdgdata/fonts", "/opt/share/fonts", "/srv/share/fonts", "/run/host/fonts"] {
        assert!(has(&dirs, d), "{d}");
    }
    assert!(!has(&dirs, "/home/u/.local/share/fonts"), "XDG_DATA_HOME 有設,不該再掃 ~/.local/share/fonts");
    let dirs = fake("linux", &none).font_dirs();
    assert!(has(&dirs, "/home/u/.local/share/fonts") && has(&dirs, "/home/u/.fonts"));
}

/// 沒有家目錄時不可以組出相對路徑:那會從當下的工作目錄開始掃。
#[test]
fn no_home_dir_produces_no_relative_paths() {
    for os in ["linux", "macos", "windows", "android"] {
        let h = Host { os, env: &none, home: None };
        for p in h.font_dirs() {
            let s = p.to_string_lossy();
            assert!(p.is_absolute() || s.starts_with('/') || s.get(1..2) == Some(":"), "{os}: {s}");
        }
    }
}

/// 掃描:同家族的其他字重跳過、比開頭不比包含、依偏好排序。
#[test]
fn scan_rules() {
    let d = std::env::temp_dir().join(format!("wincv-ttf-scan-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&d);
    std::fs::create_dir_all(d.join("a/b")).unwrap();
    for n in ["DejaVuSans.ttf", "DejaVuSans-Bold.ttf", "Inconsolata.otf", "a/NotoSansCJK-Regular.ttc", "a/b/readme.txt"] {
        std::fs::write(d.join(n), b"").unwrap();
    }
    let got: Vec<String> = scan(&[d.clone()]).iter().map(|p| p.file_name().unwrap().to_string_lossy().into_owned()).collect();
    assert_eq!(got, ["NotoSansCJK-Regular.ttc", "DejaVuSans.ttf"]);
}

#[test]
fn missing_hint_says_something() {
    assert!(!missing_hint().is_empty());
}

/// 需要真字型:字模不能超出格子,空白回 None,半形照 CP437 取字。
#[test]
fn real_font_glyphs() {
    let (chain, used, _) = load_chain(8, 16, 16);
    let Some(f) = chain.0.first() else { return };
    eprintln!("用 {}", used[0]);
    for r in ['简', '体', '한', 'Ω', 'Д', 'A'] {
        let Some(g) = f.glyph(r) else { continue };
        assert_eq!((g.w, g.h), (if is_wide(r) { 16 } else { 8 }, 16), "{r}");
    }
    assert!(f.glyph(' ').is_none(), "空白一個筆畫都沒有,要回 None");
    let hf = HalfFont::new(f.clone(), 8, 16);
    assert_eq!(hf.glyph(0x41).map(|g| g.bits), f.glyph('A').map(|g| g.bits));
    assert!(hf.glyph(0).is_none());
}
