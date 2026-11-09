//! 解壓縮到目錄,以及打包成 ZIP。

use std::fs::{self, File};
use std::io;
use std::path::{Path, PathBuf};

use wincv_i18n::tr;
use wincv_vfs as vfs;

use crate::{bytes_os, go_path_clean, other, ArchiveFs, Item};

pub(crate) fn extract(
    a: &ArchiveFs,
    dst_dir: &Path,
    names: &[Vec<u8>],
    mut progress: Option<&mut dyn FnMut(&[u8], usize, usize)>,
) -> io::Result<usize> {
    let want: Vec<Vec<u8>> = names.iter().map(|n| crate::trim_slashes(n).to_vec()).collect();
    let todo: Vec<&Item> = a
        .items
        .iter()
        .filter(|e| !e.is_dir)
        .filter(|e| want.is_empty() || want.iter().any(|w| e.path == *w || crate::under(&e.path, w).is_some()))
        .collect();
    let mut n = 0;
    for (i, e) in todo.iter().enumerate() {
        extract_one(a, dst_dir, e).map_err(|err| {
            other(format!("{}: {err}", wincv_textenc::utf8_lossy_go(&e.path)))
        })?;
        n += 1;
        if let Some(p) = progress.as_mut() {
            p(&e.path, i + 1, todo.len());
        }
    }
    Ok(n)
}

/// [HARD] 壓縮檔裡的路徑不可信。含有 `..` 的路徑可以寫到目標目錄之外
/// (zip slip),所以每一個目的路徑都要確認仍在 `dst_dir` 底下。
fn extract_one(a: &ArchiveFs, dst_dir: &Path, e: &Item) -> io::Result<()> {
    let mut rooted = vec![b'/'];
    rooted.extend_from_slice(&e.path);
    let clean = go_path_clean(&rooted)[1..].to_vec();
    if clean.is_empty() || clean == b"." {
        return Ok(());
    }
    let mut dst = dst_dir.to_path_buf();
    for seg in clean.split(|&c| c == b'/') {
        dst.push(bytes_os(seg));
    }
    let abs_root = vfs::clean(&std::path::absolute(dst_dir)?);
    let abs_dst = vfs::clean(&std::path::absolute(&dst)?);
    if abs_dst != abs_root && !abs_dst.starts_with(&abs_root) {
        return Err(other(tr!("路徑跑到目標目錄之外了: %q", wincv_textenc::utf8_lossy_go(&e.path))));
    }
    if let Some(parent) = dst.parent() {
        fs::create_dir_all(parent)?;
    }
    let body = a.read_item(e)?;
    let mut out = File::create(&dst)?;
    io::Write::write_all(&mut out, &body)?;
    if let Some(t) = e.mod_time {
        let _ = out.set_modified(t);
    }
    Ok(())
}

/// 把 `src_dir` 底下的 `names` 打包成一個 ZIP。
///
/// 只做 ZIP。原版可以建 RAR 與其他格式(靠外掛 DLL),但 RAR 的壓縮演算法
/// 是專有的,其他格式也沒有可用的純 Rust 編碼器。
pub fn create_zip(
    dst: &Path,
    src_dir: &Path,
    names: &[PathBuf],
    mut progress: Option<&mut dyn FnMut(&Path, usize, usize)>,
) -> io::Result<()> {
    let mut zw = zip::ZipWriter::new(File::create(dst)?);
    for (i, n) in names.iter().enumerate() {
        add(&mut zw, src_dir, n, "")?;
        if let Some(p) = progress.as_mut() {
            p(n, i + 1, names.len());
        }
    }
    zw.finish().map_err(|e| other(e.to_string()))?;
    Ok(())
}

fn add(zw: &mut zip::ZipWriter<File>, root: &Path, name: &Path, prefix: &str) -> io::Result<()> {
    let full = root.join(name);
    let md = fs::metadata(&full)?;
    let rel = name.to_string_lossy().replace('\\', "/");
    let inner = if prefix.is_empty() { rel } else { format!("{prefix}/{rel}") };
    let inner = String::from_utf8_lossy(&go_path_clean(inner.as_bytes())).into_owned();
    if md.is_dir() {
        // 與 Go 的 os.ReadDir 一樣依名稱排序。
        let mut kids: Vec<_> = fs::read_dir(&full)?.filter_map(|d| d.ok().map(|d| d.file_name())).collect();
        kids.sort();
        for k in kids {
            add(zw, &full, Path::new(&k), &inner)?;
        }
        return Ok(());
    }
    let mut opt = zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Deflated);
    if let Ok(t) = md.modified() {
        let lt: chrono::DateTime<chrono::Local> = t.into();
        use chrono::{Datelike, Timelike};
        if let Ok(d) = zip::DateTime::from_date_and_time(
            lt.year() as u16,
            lt.month() as u8,
            lt.day() as u8,
            lt.hour() as u8,
            lt.minute() as u8,
            lt.second() as u8,
        ) {
            opt = opt.last_modified_time(d);
        }
    }
    #[cfg(unix)]
    {
        use std::os::unix::fs::PermissionsExt;
        opt = opt.unix_permissions(md.permissions().mode() & 0o777);
    }
    zw.start_file(inner, opt).map_err(|e| other(e.to_string()))?;
    io::copy(&mut File::open(&full)?, zw)?;
    Ok(())
}
