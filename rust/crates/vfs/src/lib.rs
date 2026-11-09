//! 檔案系統抽象(對應 Go 版 `internal/vfs`)。
//!
//! 真實目錄與壓縮檔內部走同一個介面,檔案列表不必知道自己在哪一種裡面。

mod drives;
pub mod gosort;

pub use drives::{drives, unescape_mount, Drive};

use std::ffi::OsString;
use std::io::{self, Read};
use std::path::{Component, Path, PathBuf};
use std::time::SystemTime;

/// 目錄裡的一個項目。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct Entry {
    /// 顯示用的名字。檔名的原始位元組不一定是合法 UTF-8(壓縮檔解出來的
    /// 常是 Big5),壞位元組依 Go 的規則一個換一個 U+FFFD。
    pub name: String,
    /// 原始檔名,開檔與組路徑用它,不用 `name`。
    pub os_name: OsString,
    pub size: i64,
    /// 修改時間。None 表示沒有(例如「..」)。
    pub mod_time: Option<SystemTime>,
    pub is_dir: bool,
    /// 連結目標或長檔名(畫在日期之後,加底線)。
    pub link: String,
}

impl Entry {
    /// 只有名字的項目(測試與合成項目用)。
    pub fn named(name: &str, is_dir: bool) -> Entry {
        Entry {
            name: name.to_string(),
            os_name: name.into(),
            size: 0,
            mod_time: None,
            is_dir,
            link: String::new(),
        }
    }

    /// 主檔名:目錄是整個名字,檔案去掉最後一個 `.` 之後的部分。
    /// 開頭的 `.`(隱藏檔)不算副檔名分隔。
    pub fn base(&self) -> &str {
        if self.is_dir {
            return &self.name;
        }
        match self.name.rfind('.') {
            Some(i) if i > 0 => &self.name[..i],
            _ => &self.name,
        }
    }

    /// 副檔名,不含 `.`。
    pub fn ext(&self) -> &str {
        if self.is_dir {
            return "";
        }
        match self.name.rfind('.') {
            Some(i) if i > 0 => &self.name[i + 1..],
            _ => "",
        }
    }
}

/// 把作業系統的檔名換成顯示用的字串(規則見 [`Entry::name`])。
pub fn display_name(s: &std::ffi::OsStr) -> String {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        wincv_textenc::utf8_lossy_go(s.as_bytes())
    }
    #[cfg(not(unix))]
    {
        s.to_string_lossy().into_owned()
    }
}

/// 檔案系統。
pub trait Fs {
    fn read_dir(&self, dir: &Path) -> io::Result<Vec<Entry>>;
    fn open(&self, path: &Path) -> io::Result<Box<dyn Read>>;
    /// 路徑列上顯示的字樣。
    fn label(&self, dir: &Path) -> String;
}

/// 真實的檔案系統。
#[derive(Clone, Copy, Debug, Default)]
pub struct Os;

impl Fs for Os {
    fn read_dir(&self, dir: &Path) -> io::Result<Vec<Entry>> {
        let mut out = Vec::new();
        for de in std::fs::read_dir(dir)? {
            let Ok(de) = de else { continue };
            // 列表途中檔案被刪掉是正常的,跳過而不是整個失敗。
            // 不跟隨符號連結,與 Go 的 DirEntry.Info(lstat)相同。
            let Ok(md) = std::fs::symlink_metadata(de.path()) else { continue };
            let os_name = de.file_name();
            out.push(Entry {
                name: display_name(&os_name),
                os_name,
                size: md.len() as i64,
                mod_time: md.modified().ok(),
                is_dir: md.is_dir(),
                link: String::new(),
            });
        }
        // Go 的 os.ReadDir 依檔名排好才回傳;read_dir 的順序不固定。
        // 之後的排序是穩定排序,同鍵值的先後就取決於這一步。
        out.sort_by(|a, b| a.os_name.cmp(&b.os_name));
        Ok(out)
    }

    fn open(&self, path: &Path) -> io::Result<Box<dyn Read>> {
        Ok(Box::new(std::fs::File::open(path)?))
    }

    fn label(&self, dir: &Path) -> String {
        display_name(dir.as_os_str())
    }
}

/// 排序方式。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum SortKey {
    #[default]
    Name,
    Ext,
    Size,
    Time,
}

/// Go 的 strings.ToLower:逐字元的簡單對應(一個字元對一個字元)。
pub fn go_lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

/// 排序:目錄一律在前(不受 desc 影響),其餘依 key,相同時依名字(不分大小寫)。
///
/// 比較規則與演算法都與 Go 版相同,包括遞減時 `!less` 對同鍵項目的效果
/// (見 [`gosort`])。
pub fn sort(es: &mut [Entry], key: SortKey, desc: bool) {
    gosort::slice_stable(es, |a, b| {
        if a.is_dir != b.is_dir {
            return a.is_dir;
        }
        let name_less = || go_lower(&a.name) < go_lower(&b.name);
        let less = match key {
            SortKey::Ext => {
                let (ea, eb) = (go_lower(a.ext()), go_lower(b.ext()));
                if ea != eb { ea < eb } else { name_less() }
            }
            SortKey::Size if a.size != b.size => a.size < b.size,
            SortKey::Time if a.mod_time != b.mod_time => a.mod_time < b.mod_time,
            _ => name_less(),
        };
        if desc { !less } else { less }
    });
}

/// 上一層目錄。已經在最上層時回傳自己(與 Go 的 filepath.Dir(Clean(dir)) 相同)。
pub fn parent(dir: &Path) -> PathBuf {
    let clean = clean(dir);
    match clean.parent() {
        Some(p) if !p.as_os_str().is_empty() => p.to_path_buf(),
        Some(_) => PathBuf::from("."),
        None => clean,
    }
}

/// Go 的 filepath.Clean:去掉多餘的分隔字元、`.`,消掉能消的 `..`。
pub fn clean(p: &Path) -> PathBuf {
    let mut out: Vec<Component> = Vec::new();
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

/// 可用與總位元組。用 statfs(與 Go 版相同的系統呼叫),取 f_bavail:
/// 非特權使用者真正寫得進去的量,不含保留給 root 的那一塊。
#[cfg(unix)]
pub fn disk_usage(path: &Path) -> io::Result<(u64, u64)> {
    use std::os::unix::ffi::OsStrExt;
    let c = std::ffi::CString::new(path.as_os_str().as_bytes()).map_err(io::Error::other)?;
    let mut st: libc::statfs = unsafe { std::mem::zeroed() };
    // SAFETY:c 是以 NUL 結尾的路徑,st 是足夠大的輸出緩衝區。
    if unsafe { libc::statfs(c.as_ptr(), &mut st) } != 0 {
        return Err(io::Error::last_os_error());
    }
    let bs = st.f_bsize as u64;
    Ok((st.f_bavail as u64 * bs, st.f_blocks as u64 * bs))
}

#[cfg(not(unix))]
pub fn disk_usage(_path: &Path) -> io::Result<(u64, u64)> {
    Err(io::Error::new(io::ErrorKind::Unsupported, "還沒實作"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parent_native_semantics() {
        assert_eq!(parent(Path::new("/a/b")), PathBuf::from("/a"));
        assert_eq!(parent(Path::new("/a/b/")), PathBuf::from("/a"));
        assert_eq!(parent(Path::new("/")), PathBuf::from("/"));
        assert_eq!(parent(Path::new("/a")), PathBuf::from("/"));
    }

    #[test]
    fn base_and_ext() {
        let f = Entry::named("archive.tar.gz", false);
        assert_eq!((f.base(), f.ext()), ("archive.tar", "gz"));
        let h = Entry::named(".profile", false);
        assert_eq!((h.base(), h.ext()), (".profile", ""));
        let d = Entry::named("dir.d", true);
        assert_eq!((d.base(), d.ext()), ("dir.d", ""));
    }

    #[test]
    fn disk_usage_works() {
        let (free, total) = disk_usage(Path::new(".")).unwrap();
        assert!(total > 0 && free <= total);
        assert!(disk_usage(Path::new("/no/such/path/zzz")).is_err());
    }
}

#[cfg(test)]
mod gosort_tests {
    use super::*;
    use std::time::{Duration, UNIX_EPOCH};

    fn data() -> Vec<Entry> {
        let base = ["readme", "README", "Readme.txt", "readme.TXT", "a.c", "A.C", "b.h", "B.H", "zz", "ZZ"];
        (0..45)
            .map(|i: usize| {
                let mut n = base[i % base.len()].to_string();
                if i >= base.len() {
                    n = format!("{n}{}", i / base.len());
                }
                if i % 11 == 0 {
                    n = base[(i / 11) % base.len()].to_uppercase();
                }
                Entry {
                    size: ((i * 7) % 5) as i64,
                    is_dir: i % 9 == 0,
                    mod_time: Some(UNIX_EPOCH + Duration::from_secs(1000 + ((i * 13) % 4) as u64)),
                    ..Entry::named(&n, false)
                }
            })
            .collect()
    }

    /// 期望值是 Go 版 vfs.Sort 的實際輸出(testdata/go-sort.txt)。
    #[test]
    fn gosort_matches_go() {
        let want = include_str!("../testdata/go-sort.txt");
        let mut lines = 0;
        for line in want.lines() {
            let mut it = line.splitn(3, ' ');
            let key = match it.next().unwrap() {
                "0" => SortKey::Name,
                "1" => SortKey::Ext,
                "2" => SortKey::Size,
                _ => SortKey::Time,
            };
            let desc = it.next().unwrap() == "true";
            let mut es = data();
            sort(&mut es, key, desc);
            let got: Vec<&str> = es.iter().map(|e| e.name.as_str()).collect();
            assert_eq!(got.join(","), it.next().unwrap(), "{key:?} desc={desc}");
            lines += 1;
        }
        assert_eq!(lines, 6);
    }
}
