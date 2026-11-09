//! MD5 與 SFV(CRC32)檢驗(對應 Go 版 `internal/checksum`),原版列出的功能之一。
//!
//! SFV 是 1990 年代檔案交換圈的慣例:一個純文字檔,每行「檔名 CRC32」,分號開頭
//! 是註解。它是校驗 usenet / BBS 下傳檔案完整性的標準做法。

use std::fs::File;
use std::io::{self, BufRead, Read, Write};
use std::path::Path;

use md5::{Digest, Md5};

fn hex(b: &[u8]) -> String {
    b.iter().map(|x| format!("{x:02x}")).collect()
}

/// 一個檔案的 MD5(小寫十六進位)。
pub fn md5_file(path: &Path) -> io::Result<String> {
    let mut f = File::open(path)?;
    let mut h = Md5::new();
    let mut buf = vec![0u8; 64 * 1024];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        h.update(&buf[..n]);
    }
    Ok(hex(&h.finalize()))
}

/// 一個檔案的 CRC32(IEEE),8 位大寫十六進位。
pub fn crc32_file(path: &Path) -> io::Result<String> {
    let mut f = File::open(path)?;
    let mut h = crc32fast::Hasher::new();
    let mut buf = vec![0u8; 64 * 1024];
    loop {
        let n = f.read(&mut buf)?;
        if n == 0 {
            break;
        }
        h.update(&buf[..n]);
    }
    Ok(format!("{:08X}", h.finalize()))
}

/// .sfv 裡的一行。
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct SfvEntry {
    pub name: String,
    /// 8 位大寫十六進位。
    pub crc: String,
}

/// 讀一個 .sfv。分號開頭是註解,空行略過。
///
/// 檔名可能含空白,所以以**最後一個空白**分隔檔名與 CRC,不是第一個:用第一個
/// 的話 `my file.zip ABCD1234` 會拆錯。
pub fn parse_sfv<R: BufRead>(r: R) -> io::Result<Vec<SfvEntry>> {
    let mut out = Vec::new();
    for line in r.split(b'\n') {
        let line = String::from_utf8_lossy(&line?).into_owned();
        let line = line.trim_end_matches([' ', '\t', '\r']);
        if line.is_empty() || line.trim().starts_with(';') {
            continue;
        }
        let Some(i) = line.rfind([' ', '\t']) else { continue };
        let name = line[..i].trim();
        let crc = line[i + 1..].trim();
        if name.is_empty() || crc.len() != 8 || u32::from_str_radix(crc, 16).is_err() {
            continue;
        }
        out.push(SfvEntry { name: name.into(), crc: crc.to_uppercase() });
    }
    Ok(out)
}

/// 產生一個 .sfv(CRLF 換行)。
pub fn write_sfv<W: Write>(mut w: W, entries: &[SfvEntry]) -> io::Result<()> {
    for e in entries {
        write!(w, "{} {}\r\n", e.name, e.crc)?;
    }
    Ok(())
}

/// 一筆檢驗結果。
#[derive(Clone, Debug, PartialEq, Eq, Default)]
pub struct Check {
    pub name: String,
    pub want: String,
    pub got: String,
    pub ok: bool,
    pub missing: bool,
}

/// 依 .sfv 檢驗同目錄下的檔案。
pub fn verify_sfv(sfv: &Path) -> io::Result<Vec<Check>> {
    let entries = parse_sfv(io::BufReader::new(File::open(sfv)?))?;
    let dir = sfv.parent().unwrap_or(Path::new("."));
    Ok(entries
        .into_iter()
        .map(|e| match crc32_file(&dir.join(&e.name)) {
            Ok(got) => Check { ok: got == e.crc, name: e.name, want: e.crc, got, missing: false },
            Err(_) => Check { name: e.name, want: e.crc, missing: true, ..Default::default() },
        })
        .collect())
}

/// Go 的 strings.ToLower(逐字元簡單對應)。
fn go_lower(s: &str) -> String {
    s.chars().map(|c| c.to_lowercase().next().unwrap_or(c)).collect()
}

/// 對一批檔案算出 CRC,依檔名(不分大小寫)排序。
pub fn make_sfv(dir: &Path, names: &[String]) -> io::Result<Vec<SfvEntry>> {
    let mut out = names
        .iter()
        .map(|n| Ok(SfvEntry { name: n.clone(), crc: crc32_file(&dir.join(n))? }))
        .collect::<io::Result<Vec<_>>>()?;
    out.sort_by_key(|e| go_lower(&e.name));
    Ok(out)
}

/// 把結果數成「幾個對、幾個錯、幾個缺」。
pub fn summary(rs: &[Check]) -> (usize, usize, usize) {
    let missing = rs.iter().filter(|r| r.missing).count();
    let ok = rs.iter().filter(|r| !r.missing && r.ok).count();
    (ok, rs.len() - ok - missing, missing)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn tmp(tag: &str) -> std::path::PathBuf {
        let d = std::env::temp_dir().join(format!("wincv-checksum-{tag}-{}", std::process::id()));
        let _ = std::fs::remove_dir_all(&d);
        std::fs::create_dir_all(&d).unwrap();
        d
    }

    #[test]
    fn known_vectors() {
        let d = tmp("vec");
        let p = d.join("a");
        std::fs::write(&p, "The quick brown fox jumps over the lazy dog").unwrap();
        assert_eq!(md5_file(&p).unwrap(), "9e107d9d372bb6826bd81d3542a419d6");
        assert_eq!(crc32_file(&p).unwrap(), "414FA339");
    }

    #[test]
    fn parse_sfv_filename_with_spaces() {
        let es = parse_sfv("; comment\r\nmy file.zip abcd1234\r\n\nbad line\nx.bin 1234567\n".as_bytes()).unwrap();
        assert_eq!(es, [SfvEntry { name: "my file.zip".into(), crc: "ABCD1234".into() }]);
    }

    #[test]
    fn verify_and_make() {
        let d = tmp("verify");
        std::fs::write(d.join("B.txt"), "hello").unwrap();
        std::fs::write(d.join("a.txt"), "world").unwrap();
        let es = make_sfv(&d, &["B.txt".into(), "a.txt".into()]).unwrap();
        assert_eq!(es.iter().map(|e| e.name.as_str()).collect::<Vec<_>>(), ["a.txt", "B.txt"]);
        let mut f = File::create(d.join("x.sfv")).unwrap();
        write_sfv(&mut f, &es).unwrap();
        writeln!(f, "gone.txt 00000000").unwrap();
        std::fs::write(d.join("a.txt"), "changed").unwrap();
        let rs = verify_sfv(&d.join("x.sfv")).unwrap();
        assert_eq!(summary(&rs), (1, 1, 1));
    }
}
