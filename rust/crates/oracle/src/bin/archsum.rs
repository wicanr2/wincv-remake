//! 列出壓縮檔內每個檔案的 sha256(對應 Go 端 `tools/archsum`)。
//!
//! ```text
//! archsum [-n 次數] 壓縮檔...
//! ```
//!
//! 每列「名稱的 hex<TAB>大小<TAB>sha256」,依名稱排序。
//! 走 `wincv-archive` 的 vfs 介面(打開、ReadDir 遞迴、Open),與 Go 版走同一條路徑。

use std::io::Write;
use std::path::Path;
use std::process::exit;
use std::time::{Duration, Instant};

use sha2::{Digest, Sha256};
use wincv_archive::{path_bytes, ArchiveFs};
use wincv_vfs::Fs;

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// 打開一個壓縮檔、列目錄、解出每個檔案。與 Go 版一樣走 vfs 介面,整段計時。
fn one(name: &str, lines: &mut Vec<String>) -> Result<(), String> {
    let a = ArchiveFs::open(Path::new(name)).map_err(|e| e.to_string())?;
    let mut paths = Vec::new();
    walk(&a, b"", &mut paths)?;
    let mut files: Vec<(Vec<u8>, u64, String)> = Vec::new();
    let mut err = None;
    for p in paths {
        let mut h = Sha256::new();
        let res = a.open(&a.path(&p)).and_then(|mut r| std::io::copy(&mut r, &mut HashOnly(&mut h)));
        match res {
            Ok(n) => files.push((p, n, hex(&h.finalize()))),
            Err(e) => {
                eprintln!("{name} {} {e}", String::from_utf8_lossy(&p));
                err = Some("有檔案讀不出來".to_string());
            }
        }
    }
    files.sort_by(|a, b| a.0.cmp(&b.0));
    for (p, n, sum) in files {
        lines.push(format!("{}\t{n}\t{sum}", hex(&p)));
    }
    err.map_or(Ok(()), Err)
}

/// 依 ReadDir 遞迴走訪,收集檔案的內部路徑(與 Go 版的 walk 相同)。
fn walk(a: &ArchiveFs, dir: &[u8], out: &mut Vec<Vec<u8>>) -> Result<(), String> {
    for e in a.read_dir(&a.path(dir)).map_err(|e| e.to_string())? {
        let mut p = dir.to_vec();
        if !p.is_empty() {
            p.push(b'/');
        }
        p.extend_from_slice(&path_bytes(Path::new(&e.os_name)));
        if e.is_dir {
            walk(a, &p, out)?;
        } else {
            out.push(p);
        }
    }
    Ok(())
}

struct HashOnly<'a>(&'a mut Sha256);

impl Write for HashOnly<'_> {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        self.0.update(buf);
        Ok(buf.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

fn main() {
    let mut args: Vec<String> = std::env::args().skip(1).collect();
    let mut rounds = 1;
    if args.first().map(String::as_str) == Some("-n") && args.len() >= 2 {
        rounds = args[1].parse().unwrap_or(1).max(1);
        args.drain(..2);
    }
    let mut failed = false;
    for name in &args {
        let mut lines = Vec::new();
        let mut best = Duration::MAX;
        for _ in 0..rounds {
            lines.clear();
            let t0 = Instant::now();
            if let Err(e) = one(name, &mut lines) {
                eprintln!("{name} {e}");
                failed = true;
                break;
            }
            best = best.min(t0.elapsed());
        }
        for l in &lines {
            println!("{l}");
        }
        if best == Duration::MAX {
            eprintln!("{name}: 失敗,沒有計時");
        } else {
            eprintln!("{name}: {} 個檔案,最快一次 {best:?}", lines.len());
        }
    }
    if failed {
        exit(1);
    }
}
