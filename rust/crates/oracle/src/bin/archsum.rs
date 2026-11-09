//! 列出壓縮檔內每個檔案的 sha256(對應 Go 端 `tools/archsum`)。
//!
//! ```text
//! archsum [-n 次數] 壓縮檔...
//! ```
//!
//! 每列「名稱的 hex<TAB>大小<TAB>sha256」,依名稱排序。
//! 支援 LZH(`wincv-lzh`)與 RAR(候選 crate `rars`,第一階段評估用)。

use std::fs::File;
use std::io::{Read, Seek, SeekFrom, Write};
use std::sync::{Arc, Mutex};
use std::process::exit;
use std::time::{Duration, Instant};

use sha2::{Digest, Sha256};

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// 打開一個壓縮檔、列目錄、解出每個檔案。與 Go 版一樣,整段計時。
fn one(name: &str, lines: &mut Vec<String>) -> Result<(), String> {
    if name.to_ascii_lowercase().ends_with(".rar") {
        return one_rar(name, lines);
    }
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

/// 一邊寫一邊算 sha256,不把整個檔案留在記憶體。
struct HashWriter(Arc<Mutex<(Sha256, u64)>>);

impl Write for HashWriter {
    fn write(&mut self, buf: &[u8]) -> std::io::Result<usize> {
        let mut g = self.0.lock().unwrap_or_else(|e| e.into_inner());
        g.0.update(buf);
        g.1 += buf.len() as u64;
        Ok(buf.len())
    }
    fn flush(&mut self) -> std::io::Result<()> {
        Ok(())
    }
}

type Pending = Vec<(Vec<u8>, Arc<Mutex<(Sha256, u64)>>)>;

/// RAR 用 rars。檔名與 Go 版一樣把 `\` 換成 `/`;同名時取第一個。
fn one_rar(name: &str, lines: &mut Vec<String>) -> Result<(), String> {
    let ar = rars::ArchiveReader::read_path(name).map_err(|e| e.to_string())?;
    let pending: Arc<Mutex<Pending>> = Arc::new(Mutex::new(Vec::new()));
    let p2 = pending.clone();
    ar.extract_to(None, move |meta| {
        if meta.is_directory {
            return Ok(Box::new(std::io::sink()) as Box<dyn Write>);
        }
        let path: Vec<u8> = meta.name.iter().map(|&b| if b == b'\\' { b'/' } else { b }).collect();
        let st = Arc::new(Mutex::new((Sha256::new(), 0u64)));
        p2.lock().unwrap_or_else(|e| e.into_inner()).push((path, st.clone()));
        Ok(Box::new(HashWriter(st)) as Box<dyn Write>)
    })
    .map_err(|e| e.to_string())?;
    let mut done: Vec<(Vec<u8>, u64, String)> = Vec::new();
    for (path, st) in pending.lock().unwrap_or_else(|e| e.into_inner()).drain(..) {
        if done.iter().any(|d| d.0 == path) {
            continue;
        }
        let (h, n) = std::mem::take(&mut *st.lock().unwrap_or_else(|e| e.into_inner()));
        done.push((path, n, hex(&h.finalize())));
    }
    done.sort_by(|a, b| a.0.cmp(&b.0));
    for (p, n, sum) in done {
        lines.push(format!("{}\t{n}\t{sum}", hex(&p)));
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
