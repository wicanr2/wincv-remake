//! 列出壓縮檔內每個檔案的 sha256(對應 Go 端 `tools/archsum`)。
//!
//! ```text
//! archsum [-n 次數] 壓縮檔...
//! ```
//!
//! 每列「名稱的 hex<TAB>大小<TAB>sha256」,依名稱排序。目前支援 LZH。

use std::fs::File;
use std::io::{Read, Seek, SeekFrom};
use std::process::exit;
use std::time::{Duration, Instant};

use sha2::{Digest, Sha256};

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// 打開一個壓縮檔、列目錄、解出每個檔案。與 Go 版一樣,整段計時。
fn one(name: &str, lines: &mut Vec<String>) -> Result<(), String> {
    let mut f = File::open(name).map_err(|e| e.to_string())?;
    let entries = wincv_lzh::list(&mut f).map_err(|e| e.to_string())?;
    // 與 Go 版的 archive.FS 一樣:路徑去掉結尾的 /,同名時取第一個。
    let mut files: Vec<(Vec<u8>, &wincv_lzh::Entry)> = Vec::new();
    for e in entries.iter().filter(|e| !e.is_dir) {
        let p = e.name.strip_suffix(b"/").unwrap_or(&e.name).to_vec();
        if !files.iter().any(|(q, _)| *q == p) {
            files.push((p, e));
        }
    }
    files.sort_by(|a, b| a.0.cmp(&b.0));
    for (p, e) in &files {
        f.seek(SeekFrom::Start(e.offset as u64)).map_err(|e| e.to_string())?;
        // 只讀這個成員的壓縮資料,與 Go 版的 io.LimitReader 相同。
        let src = (&mut f).take(e.packed.max(0) as u64);
        let out = wincv_lzh::decode(src, &e.method, e.original as u64)
            .map_err(|err| format!("{} {err}", String::from_utf8_lossy(p)))?;
        lines.push(format!("{}\t{}\t{}", hex(p), out.len(), hex(&Sha256::digest(&out))));
    }
    Ok(())
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
        eprintln!("{name}: {} 個檔案,最快一次 {best:?}", lines.len());
    }
    if failed {
        exit(1);
    }
}
