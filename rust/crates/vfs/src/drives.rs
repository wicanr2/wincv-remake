//! 左側磁碟窗格的清單(對應 Go 版 `drives.go`)。Unix 沒有磁碟機代號,
//! 列的是根目錄、家目錄與真的掛載點;可卸除的另外標記。

use std::path::{Path, PathBuf};

/// 一個磁碟或掛載點。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Drive {
    /// 顯示用的短名,如 `/`、`~`、`usb-stick`。
    pub label: String,
    /// 切過去要用的路徑。
    pub path: PathBuf,
    /// 可卸除或外接的(原版用不同顏色畫)。
    pub volume: bool,
}

/// 虛擬或系統用的檔案系統類型,不列出來。
const PSEUDO_FS: &[&str] = &[
    "proc", "sysfs", "devtmpfs", "devpts", "tmpfs", "cgroup", "cgroup2", "securityfs",
    "pstore", "bpf", "debugfs", "tracefs", "configfs", "fusectl", "hugetlbfs", "mqueue",
    "efivarfs", "autofs", "binfmt_misc", "squashfs", "nsfs", "overlay", "ramfs", "rpc_pipefs",
    "fuse.portal", "fuse.gvfsd-fuse", "fuse.snapfuse",
];

const REMOVABLE_ROOTS: &[&str] = &["/media", "/run/media", "/mnt", "/Volumes"];

fn file_name(p: &Path) -> String {
    p.file_name().map(crate::display_name).unwrap_or_else(|| crate::display_name(p.as_os_str()))
}

/// 列出磁碟。固定的(根目錄、家目錄、掛載點)在前,可卸除的在後。
pub fn drives() -> Vec<Drive> {
    let mut out: Vec<Drive> = Vec::new();
    let mut add = |label: String, path: PathBuf, volume: bool| {
        if path.as_os_str().is_empty() || out.iter().any(|d| d.path == path) || !path.is_dir() {
            return;
        }
        out.push(Drive { label, path, volume });
    };
    add("/".into(), PathBuf::from("/"), false);
    if let Some(h) = std::env::var_os("HOME") {
        add("~".into(), PathBuf::from(h), false);
    }
    if let Ok(mounts) = std::fs::read_to_string("/proc/mounts") {
        for line in mounts.lines() {
            let fs: Vec<&str> = line.split_whitespace().collect();
            if fs.len() < 3 || PSEUDO_FS.contains(&fs[2]) {
                continue;
            }
            let mp = PathBuf::from(unescape_mount(fs[1]));
            // 與 Go 版相同:排除的是字串前綴 "/snap/",/snap 本身不排除。
            if mp == Path::new("/") || mp.to_string_lossy().starts_with("/snap/") {
                continue;
            }
            add(file_name(&mp), mp.clone(), is_removable(&mp));
        }
    }
    for root in REMOVABLE_ROOTS {
        let Ok(ents) = std::fs::read_dir(root) else { continue };
        let mut names: Vec<_> = ents.flatten().map(|e| e.file_name()).collect();
        names.sort();
        for n in names {
            let p = Path::new(root).join(&n);
            // /media/<使用者>/<磁碟> 這種多一層的,把裡面那層也列出來。
            if *root != "/Volumes" {
                if let Ok(sub) = std::fs::read_dir(&p) {
                    let sub: Vec<_> = sub.flatten().collect();
                    let has_files = sub.iter().any(|e| e.file_type().map(|t| !t.is_dir()).unwrap_or(false));
                    if !has_files {
                        let mut subn: Vec<_> = sub.iter().map(|e| e.file_name()).collect();
                        subn.sort();
                        for s in subn {
                            add(crate::display_name(&s), p.join(&s), true);
                        }
                    }
                }
            }
            add(crate::display_name(&n), p, true);
        }
    }
    // 固定的在前;同一類維持原本的順序。
    out.sort_by_key(|d| d.volume);
    out
}

fn is_removable(mp: &Path) -> bool {
    REMOVABLE_ROOTS.iter().any(|r| mp.starts_with(r) && mp != Path::new(r))
}

/// /proc/mounts 把空白等字元寫成 `\040` 這種八進位跳脫,還原回來。
/// 不完整的跳脫原樣保留。
pub fn unescape_mount(s: &str) -> String {
    if !s.contains('\\') {
        return s.to_string();
    }
    let b = s.as_bytes();
    let mut out = Vec::with_capacity(b.len());
    let mut i = 0;
    while i < b.len() {
        // 與 Go 版相同的條件:i+3 < len,也就是跳脫後面至少還有三個位元組。
        if b[i] == b'\\' && i + 3 < b.len() {
            let oct = &b[i + 1..i + 4];
            if oct.iter().all(|c| (b'0'..=b'7').contains(c)) {
                let v = oct.iter().fold(0u32, |v, c| v * 8 + (c - b'0') as u32);
                out.push(v as u8);
                i += 4;
                continue;
            }
        }
        out.push(b[i]);
        i += 1;
    }
    String::from_utf8_lossy(&out).into_owned()
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn unescape() {
        for (inp, want) in [
            (r"/media/u/My\040Disk", "/media/u/My Disk"),
            ("/mnt/data", "/mnt/data"),
            (r"/a\134b", r"/a\b"),
            (r"/trailing\04", r"/trailing\04"),
        ] {
            assert_eq!(unescape_mount(inp), want, "{inp}");
        }
    }

    #[test]
    fn has_root_and_no_duplicates() {
        let ds = drives();
        assert!(ds.iter().any(|d| d.path == Path::new("/")));
        assert!(ds.len() <= 24, "列出 {} 個,過濾大概沒生效", ds.len());
        for (i, d) in ds.iter().enumerate() {
            assert!(!d.label.is_empty());
            assert!(!ds[..i].iter().any(|e| e.path == d.path), "{:?} 重複", d.path);
        }
    }
}
