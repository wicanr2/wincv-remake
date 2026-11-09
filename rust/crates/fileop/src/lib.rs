//! 檔案操作:拷貝、移動、改名、刪除、比對(對應 Go 版 `internal/fileop`)。
//!
//! 對應原版主畫面的 C / M / R / D 與 Alt-C。從原版的字串與 changelog 推出來的行為:
//!
//! - 覆蓋詢問有「不覆蓋」與「全部覆蓋」兩種答案(image 字串「&N 不覆蓋」「&A 全部覆蓋」)
//! - 刪除可以選「不放資源回收桶時,先把內容填 0 再刪」(0.5 版新增的選項)
//! - Alt-C 比對兩個標記檔案的內容是否相同

use std::collections::BTreeMap;
use std::ffi::{OsStr, OsString};
use std::fs::{self, File};
use std::io::{self, Read, Write};
use std::path::Path;

use wincv_i18n::{t, tr};

/// 遇到同名檔案時的處置。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Default)]
pub enum Overwrite {
    /// 要問呼叫端(透過 confirm 回呼)。
    #[default]
    Ask,
    /// 「不覆蓋」,跳過這一個繼續下一個。
    Skip,
    /// 「全部覆蓋」,之後都不再問。
    All,
}

/// 一次操作的設定。
#[derive(Default)]
pub struct Options<'a> {
    pub overwrite: Overwrite,
    /// 覆蓋詢問。回傳這一次要不要覆蓋,以及要不要把答案套用到之後所有檔案。
    pub confirm: Option<&'a mut dyn FnMut(&Path) -> (bool, bool)>,
    /// 每處理完一個檔案呼叫一次:(名稱, 已完成, 總數)。
    pub progress: Option<&'a mut dyn FnMut(&OsStr, usize, usize)>,
    /// 刪除前先把檔案內容填 0(原版「刪除檔案時若不放到資源回收桶,則使用填0方式刪除?」)。
    pub zero_fill: bool,
}

/// 一次批次操作的結果。
#[derive(Debug, Default)]
pub struct Outcome {
    pub done: Vec<OsString>,
    pub skipped: Vec<OsString>,
    pub errors: BTreeMap<OsString, String>,
}

impl Outcome {
    pub fn failed(&self) -> bool {
        !self.errors.is_empty()
    }

    /// 給狀態列用的一行摘要。
    pub fn summary(&self, verb: &str) -> String {
        let mut s = tr!("%s %d 個檔案", verb, self.done.len());
        if !self.skipped.is_empty() {
            s += &tr!(",跳過 %d 個", self.skipped.len());
        }
        if !self.errors.is_empty() {
            s += &tr!(",失敗 %d 個", self.errors.len());
        }
        s
    }
}

/// 把 src_dir 底下的 names 拷貝到 dst_dir。
pub fn copy(src_dir: &Path, dst_dir: &Path, names: &[OsString], o: Options) -> Outcome {
    batch(src_dir, dst_dir, names, o, copy_one)
}

/// 搬移。同一個檔案系統上用 rename,跨檔案系統退回「拷貝後刪除」。
pub fn move_to(src_dir: &Path, dst_dir: &Path, names: &[OsString], o: Options) -> Outcome {
    batch(src_dir, dst_dir, names, o, move_one)
}

fn batch(src_dir: &Path, dst_dir: &Path, names: &[OsString], mut o: Options, f: fn(&Path, &Path) -> io::Result<()>) -> Outcome {
    let mut res = Outcome::default();
    if src_dir == dst_dir {
        res.errors.insert("*".into(), t("來源與目的是同一個目錄"));
        return res;
    }
    for (i, n) in names.iter().enumerate() {
        let (src, dst) = (src_dir.join(n), dst_dir.join(n));
        if !decide_overwrite(&dst, &mut o) {
            res.skipped.push(n.clone());
            continue;
        }
        match f(&src, &dst) {
            Ok(()) => res.done.push(n.clone()),
            Err(e) => {
                res.errors.insert(n.clone(), e.to_string());
            }
        }
        if let Some(p) = o.progress.as_mut() {
            p(n, i + 1, names.len());
        }
    }
    res
}

/// 要不要繼續處理這個目的檔案。
fn decide_overwrite(dst: &Path, o: &mut Options) -> bool {
    if fs::metadata(dst).is_err() {
        return true; // 不存在,直接做
    }
    match o.overwrite {
        Overwrite::All => return true,
        Overwrite::Skip => return false,
        Overwrite::Ask => {}
    }
    // 沒人可問就當作不覆蓋:不要默默蓋掉使用者的檔案。
    let Some(c) = o.confirm.as_mut() else { return false };
    let (ow, all) = c(dst);
    if all {
        o.overwrite = if ow { Overwrite::All } else { Overwrite::Skip };
    }
    ow
}

fn copy_one(src: &Path, dst: &Path) -> io::Result<()> {
    let md = fs::metadata(src)?;
    if md.is_dir() { copy_dir(src, dst) } else { copy_file(src, dst, &md) }
}

fn copy_file(src: &Path, dst: &Path, md: &fs::Metadata) -> io::Result<()> {
    let mut input = File::open(src)?;
    // 先寫到暫存檔再改名。中途失敗時目的地不會留下半個檔:半個檔比沒有檔更糟,
    // 因為它看起來是成功的。
    let mut tmp = dst.as_os_str().to_owned();
    tmp.push(".wincv-tmp");
    let tmp = Path::new(&tmp);
    let res = (|| {
        let mut out = File::create(tmp)?;
        io::copy(&mut input, &mut out)?;
        out.flush()?;
        fs::set_permissions(tmp, md.permissions())?;
        // 保留時間戳:檔案管理程式把日期弄丟是很惱人的事。
        if let Ok(t) = md.modified() {
            out.set_modified(t)?;
        }
        drop(out);
        fs::rename(tmp, dst)
    })();
    if res.is_err() {
        let _ = fs::remove_file(tmp);
    }
    res
}

fn copy_dir(src: &Path, dst: &Path) -> io::Result<()> {
    let md = fs::metadata(src)?;
    fs::create_dir_all(dst)?;
    fs::set_permissions(dst, md.permissions())?;
    let mut es: Vec<_> = fs::read_dir(src)?.collect::<io::Result<_>>()?;
    es.sort_by_key(|e| e.file_name());
    for e in es {
        let (s, d) = (src.join(e.file_name()), dst.join(e.file_name()));
        if e.file_type()?.is_dir() {
            copy_dir(&s, &d)?;
        } else {
            copy_file(&s, &d, &fs::metadata(&s)?)?;
        }
    }
    Ok(())
}

fn remove_all(p: &Path) -> io::Result<()> {
    match fs::symlink_metadata(p) {
        Ok(md) if md.is_dir() => fs::remove_dir_all(p),
        Ok(_) => fs::remove_file(p),
        Err(e) if e.kind() == io::ErrorKind::NotFound => Ok(()),
        Err(e) => Err(e),
    }
}

fn move_one(src: &Path, dst: &Path) -> io::Result<()> {
    if fs::rename(src, dst).is_ok() {
        return Ok(());
    }
    // 跨檔案系統的 rename 會失敗,退回拷貝後刪除。
    copy_one(src, dst)?;
    remove_all(src)
}

/// 改一個檔案的名字。目的地已存在時一律拒絕:改名不像拷貝,沒有「批次覆蓋」的
/// 合理情境。
pub fn rename(dir: &Path, from: &OsStr, to: &str) -> io::Result<()> {
    if to.is_empty() || to.contains(['/', '\\']) {
        return Err(io::Error::other(tr!("檔名不合法: %q", to)));
    }
    let dst = dir.join(to);
    if fs::symlink_metadata(&dst).is_ok() {
        return Err(io::Error::other(tr!("%s 已存在", to)));
    }
    fs::rename(dir.join(from), dst)
}

/// 刪除。zero_fill 為 true 時先把內容覆寫成 0 再刪。
pub fn delete(dir: &Path, names: &[OsString], mut o: Options) -> Outcome {
    let mut res = Outcome::default();
    for (i, n) in names.iter().enumerate() {
        let p = dir.join(n);
        let r = if o.zero_fill { zero_fill(&p) } else { Ok(()) }.and_then(|_| remove_all(&p));
        match r {
            Ok(()) => res.done.push(n.clone()),
            Err(e) => {
                res.errors.insert(n.clone(), e.to_string());
            }
        }
        if let Some(pr) = o.progress.as_mut() {
            pr(n, i + 1, names.len());
        }
    }
    res
}

/// 把一個檔案的內容覆寫成 0。目錄則遞迴處理裡面的檔案。
fn zero_fill(p: &Path) -> io::Result<()> {
    let md = fs::metadata(p)?;
    if md.is_dir() {
        for e in fs::read_dir(p)? {
            zero_fill(&e?.path())?;
        }
        return Ok(());
    }
    let mut f = fs::OpenOptions::new().write(true).open(p)?;
    let zero = vec![0u8; 64 * 1024];
    let mut left = md.len();
    while left > 0 {
        let n = left.min(zero.len() as u64) as usize;
        f.write_all(&zero[..n])?;
        left -= n as u64;
    }
    f.sync_all()
}

/// 比對兩個檔案的內容是否完全相同(原版 Alt-C)。回傳第二個值是第一個不同的
/// 位移,相同時為 None。長度不同時也回報第一個相異位移,讓使用者知道是「前面
/// 就不一樣」還是「只是長度不同」。
pub fn compare(a: &Path, b: &Path) -> io::Result<(bool, Option<u64>)> {
    let (mut fa, mut fb) = (File::open(a)?, File::open(b)?);
    let at = first_diff(&mut fa, &mut fb)?;
    Ok((at.is_none(), at))
}

fn read_full(r: &mut File, buf: &mut [u8]) -> io::Result<usize> {
    let mut n = 0;
    while n < buf.len() {
        match r.read(&mut buf[n..])? {
            0 => break,
            k => n += k,
        }
    }
    Ok(n)
}

fn first_diff(a: &mut File, b: &mut File) -> io::Result<Option<u64>> {
    const CHUNK: usize = 64 * 1024;
    let (mut ba, mut bb) = (vec![0u8; CHUNK], vec![0u8; CHUNK]);
    let mut off = 0u64;
    loop {
        let (na, nb) = (read_full(a, &mut ba)?, read_full(b, &mut bb)?);
        let n = na.min(nb);
        if let Some(k) = (0..n).find(|&k| ba[k] != bb[k]) {
            return Ok(Some(off + k as u64));
        }
        if na != nb {
            return Ok(Some(off + n as u64));
        }
        if na < CHUNK {
            return Ok(None);
        }
        off += n as u64;
    }
}

#[cfg(test)]
mod tests;
