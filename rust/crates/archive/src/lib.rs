//! 讓壓縮檔可以像目錄一樣被瀏覽(對應 Go 版 `internal/archive`)。
//!
//! 原版靠一堆 Windows DLL(unrar.dll、unlha32.dll、CAB32.DLL…)。這裡一律用
//! 純 Rust:現成的 crate 或自寫的解碼器,不連 C 函式庫,才能跨平台與編成 wasm。
//!
//! 格式支援狀態見 [`FORMATS`]。這張表是進度的唯一真相,不在文件裡重複維護。
//!
//! 路徑形式是「壓縮檔路徑!內部路徑」,例如 `/w/a.zip!docs`。browser 因此不必
//! 知道自己在壓縮檔裡。壓縮檔裡的檔名是原始位元組(常是 Big5),一律原樣保留,
//! 顯示時才換成字串。

pub mod ace;
pub mod arc;
pub mod arj;
pub mod cab;
mod write;
pub mod zcompress;

use std::collections::HashSet;
use std::ffi::OsString;
use std::fs::File;
use std::io::{self, Cursor, Read, Seek, SeekFrom};
use std::path::{Path, PathBuf};
use std::sync::{Arc, Mutex};
use std::time::SystemTime;

use wincv_i18n::tr;
use wincv_vfs::{self as vfs, Fs};

/// 一種壓縮格式的支援狀態。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Format {
    pub ext: &'static [&'static str],
    pub name: &'static str,
    pub supported: bool,
    /// 實作方式;沒支援時寫下一步要怎麼做。存中文原文,顯示時才過語系目錄。
    pub note: &'static str,
}

/// 原版支援的全部格式與 Rust 版目前的狀態。副檔名、名稱與 Go 版的
/// `archive.Formats` 逐列相同(`tests::formats_match_go` 盯著)。
pub const FORMATS: &[Format] = &[
    Format { ext: &[".zip"], name: "ZIP", supported: true, note: "zip crate" },
    Format { ext: &[".tar"], name: "TAR", supported: true, note: "tar crate" },
    Format { ext: &[".gz", ".tgz", ".tar.gz"], name: "GZIP", supported: true, note: "flate2" },
    Format { ext: &[".bz2", ".tbz", ".tar.bz2"], name: "BZIP2", supported: true, note: "bzip2(libbz2-rs-sys)" },
    Format { ext: &[".rar"], name: "RAR", supported: true, note: "rars" },
    Format { ext: &[".7z"], name: "7-Zip", supported: true, note: "sevenz-rust2" },
    Format { ext: &[".lzh", ".lha"], name: "LHA", supported: true, note: "自寫,見 rust/crates/lzh" },
    Format { ext: &[".arj"], name: "ARJ", supported: true, note: "自寫,方法 0-4;見 arj.rs" },
    // ACE 在 WINCV.IMG 裡沒有演算法可逆向(原版只是載入 WinACE 原廠的
    // unace.dll / unacev2.dll),但格式有公開文件,所以照文件自己寫。
    // SOUND / PIC 兩種子模式與加密還沒做,遇到會明確回報。
    Format { ext: &[".ace"], name: "ACE", supported: true, note: "自寫,見 ace.rs" },
    Format { ext: &[".cab"], name: "CAB", supported: true, note: "自寫,MSZIP + 不壓縮;LZX / Quantum 未做" },
    Format { ext: &[".z", ".taz", ".tar.z"], name: "compress", supported: true, note: "自寫,見 zcompress.rs" },
    Format { ext: &[".arc", ".pak"], name: "ARC/PAK", supported: true, note: "自寫,方法 1/2/3/5/6/8/9;4 與 7 未做" },
];

/// 依副檔名判斷格式。長的副檔名優先(`.tar.gz` 要贏過 `.gz`)。
pub fn detect_format(name: &Path) -> Option<&'static Format> {
    let low = vfs::go_lower(&vfs::display_name(name.as_os_str()));
    let mut best: Option<(&'static Format, usize)> = None;
    for f in FORMATS {
        for e in f.ext {
            if low.ends_with(e) && best.is_none_or(|(_, n)| e.len() > n) {
                best = Some((f, e.len()));
            }
        }
    }
    best.map(|(f, _)| f)
}

/// 這個檔名看起來是不是壓縮檔(不論支不支援)。
pub fn is_archive(name: &Path) -> bool {
    detect_format(name).is_some()
}

/// 壓縮檔與內部路徑之間的分隔字元。
pub const SEP: u8 = b'!';

/// 成員的內容從哪裡來。
#[derive(Clone)]
enum Body {
    /// 打開壓縮檔時就解好了。
    Mem(Arc<[u8]>),
    /// 打開壓縮檔時就知道解不出來;讀這個成員時才報錯。
    Fail(String),
    /// 讀的時候才解:ZIP 的第幾個成員。
    Zip(usize),
    /// 讀的時候才解:7z 的第幾個成員。
    SevenZ(usize),
}

#[derive(Clone)]
struct Item {
    /// 壓縮檔內的完整路徑,以 `/` 分隔,原始位元組。
    path: Vec<u8>,
    size: i64,
    mod_time: Option<SystemTime>,
    is_dir: bool,
    body: Body,
}

/// 讀的時候才解的格式,要留著打開的壓縮檔。
enum Backend {
    None,
    Zip(Mutex<zip::ZipArchive<File>>),
    SevenZ(Box<Mutex<SevenZ>>),
}

struct SevenZ {
    archive: sevenz_rust2::Archive,
    file: File,
    /// 最近解過的那一個區塊。solid 壓縮檔一個區塊要從頭解,所以整塊留著,
    /// 依序讀同一塊的成員時不必重解。
    block: Option<usize>,
    cache: Vec<(usize, Result<Arc<[u8]>, String>)>,
    /// 那個區塊解到一半壞掉的原因。還沒解到的成員都報這個錯。
    block_err: Option<String>,
}

/// 把一個壓縮檔包成 [`vfs::Fs`]。
pub struct ArchiveFs {
    name: PathBuf,
    name_bytes: Vec<u8>,
    items: Vec<Item>,
    dirs: HashSet<Vec<u8>>,
    backend: Backend,
}

fn other(msg: String) -> io::Error {
    io::Error::other(msg)
}

/// 路徑的原始位元組。
pub fn path_bytes(p: &Path) -> Vec<u8> {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        p.as_os_str().as_bytes().to_vec()
    }
    #[cfg(not(unix))]
    {
        p.to_string_lossy().into_owned().into_bytes()
    }
}

/// 原始位元組組回路徑。非 Unix 系統的檔名不是位元組,壞掉的位元組換成 U+FFFD。
pub fn bytes_os(b: &[u8]) -> OsString {
    #[cfg(unix)]
    {
        use std::os::unix::ffi::OsStrExt;
        std::ffi::OsStr::from_bytes(b).to_os_string()
    }
    #[cfg(not(unix))]
    {
        OsString::from(wincv_textenc::utf8_lossy_go(b))
    }
}

fn base_name(p: &Path) -> String {
    p.file_name().map(vfs::display_name).unwrap_or_else(|| vfs::display_name(p.as_os_str()))
}

impl ArchiveFs {
    /// 打開一個壓縮檔。
    pub fn open(name: &Path) -> io::Result<ArchiveFs> {
        let base = base_name(name);
        let Some(f) = detect_format(name) else {
            return Err(other(tr!("%s: 認不出壓縮格式", base)));
        };
        if !f.supported {
            return Err(other(tr!("%s: %s 格式還沒實作(%s)", base, f.name, wincv_i18n::t(f.note))));
        }
        let mut a = ArchiveFs {
            name: name.to_path_buf(),
            name_bytes: path_bytes(name),
            items: Vec::new(),
            dirs: HashSet::new(),
            backend: Backend::None,
        };
        match f.name {
            "ZIP" => a.load_zip()?,
            "TAR" => {
                let data = std::fs::read(name)?;
                a.items = load_tar(&data)?;
            }
            "GZIP" => a.load_compressed(|raw| {
                let mut out = Vec::new();
                flate2::read::MultiGzDecoder::new(raw).read_to_end(&mut out)?;
                Ok(out)
            })?,
            "BZIP2" => a.load_compressed(|raw| {
                let mut out = Vec::new();
                bzip2::read::MultiBzDecoder::new(raw).read_to_end(&mut out)?;
                Ok(out)
            })?,
            "RAR" => a.load_rar()?,
            "7-Zip" => a.load_7z()?,
            "LHA" => a.load_lzh()?,
            "ARJ" => {
                let fs = arj::read(&std::fs::read(name)?).map_err(other)?;
                a.items = fs.into_iter().map(|f| mem_item(f.name, f.size, f.mod_time, f.is_dir, f.data)).collect();
            }
            "ACE" => {
                let fs = ace::read(&std::fs::read(name)?).map_err(other)?;
                a.items = fs.into_iter().map(|f| mem_item(f.name, f.size, f.mod_time, f.is_dir, f.data)).collect();
            }
            "CAB" => {
                let fs = cab::read(&std::fs::read(name)?).map_err(other)?;
                a.items = fs.into_iter().map(|f| mem_item(f.name, f.size, f.mod_time, f.is_dir, f.data)).collect();
            }
            "ARC/PAK" => {
                let fs = arc::read(&std::fs::read(name)?).map_err(other)?;
                a.items = fs.into_iter().map(|f| mem_item(f.name, f.size, f.mod_time, false, f.data)).collect();
            }
            "compress" => a.load_compressed(|raw| zcompress::decode(raw).map_err(other))?,
            other_name => return Err(other(tr!("%s 沒有對應的讀取器", other_name))),
        }
        a.normalize_paths();
        a.build_dirs();
        Ok(a)
    }

    /// 成員路徑正規化:`./docs/a.txt`、`/docs//a.txt` 都變成 `docs/a.txt`。
    ///
    /// `tar czf x.tgz .` 打包出來的路徑都帶 `./`;不處理的話最上層會多一個叫
    /// 「.」的目錄,而中間層目錄是從清理過的路徑補出來的,兩邊對不上,進去是空的。
    /// 絕對路徑的成員則整個看不到。`..` 在根目錄就停住(與解壓時的規則相同)。
    /// 正規化後是空字串的(壓縮檔根目錄自己,例如 `./`)不列。
    fn normalize_paths(&mut self) {
        for e in &mut self.items {
            let mut rooted = Vec::with_capacity(e.path.len() + 1);
            rooted.push(b'/');
            rooted.extend_from_slice(&e.path);
            e.path = go_path_clean(&rooted)[1..].to_vec();
        }
        self.items.retain(|e| !e.path.is_empty());
    }

    fn load_zip(&mut self) -> io::Result<()> {
        let mut za = zip::ZipArchive::new(File::open(&self.name)?).map_err(|e| other(e.to_string()))?;
        for i in 0..za.len() {
            let zf = za.by_index_raw(i).map_err(|e| other(e.to_string()))?;
            self.items.push(Item {
                path: trim_suffix(zf.name_raw(), b"/").to_vec(),
                size: zf.size() as i64,
                mod_time: zf.last_modified().and_then(|t| {
                    local_time(t.year() as i32, t.month() as i32, t.day() as i32, t.hour() as i32, t.minute() as i32, t.second() as i32)
                }),
                is_dir: zf.is_dir(),
                body: Body::Zip(i),
            });
        }
        // 與 Go 版一樣,reader 活到所有成員都讀完。
        self.backend = Backend::Zip(Mutex::new(za));
        Ok(())
    }

    /// RAR 是循序讀取的格式,沒有中央目錄可以隨機存取,所以打開時就整份解好。
    fn load_rar(&mut self) -> io::Result<()> {
        let ar = rars::ArchiveReader::read_path(&self.name).map_err(|e| other(e.to_string()))?;
        let unix_time = matches!(ar.family(), rars::ArchiveFamily::Rar50Plus);
        type Got = Vec<(rars::ExtractedEntryMeta, Arc<Mutex<Vec<u8>>>)>;
        let got: Arc<Mutex<Got>> = Arc::new(Mutex::new(Vec::new()));
        let g2 = got.clone();
        ar.extract_to(None, move |meta| {
            let buf = Arc::new(Mutex::new(Vec::new()));
            g2.lock().unwrap_or_else(|e| e.into_inner()).push((meta.clone(), buf.clone()));
            Ok(Box::new(SharedWriter(buf)) as Box<dyn io::Write>)
        })
        .map_err(|e| other(e.to_string()))?;
        let got = std::mem::take(&mut *got.lock().unwrap_or_else(|e| e.into_inner()));
        for (meta, buf) in got {
            let body: Vec<u8> = std::mem::take(&mut *buf.lock().unwrap_or_else(|e| e.into_inner()));
            let mod_time = meta.file_time.and_then(|t| {
                if unix_time {
                    Some(SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(t as u64))
                } else {
                    dos_time(t)
                }
            });
            self.items.push(Item {
                path: normalize_slash(&meta.name),
                size: body.len() as i64,
                mod_time,
                is_dir: meta.is_directory,
                body: Body::Mem(body.into()),
            });
        }
        Ok(())
    }

    fn load_7z(&mut self) -> io::Result<()> {
        let mut file = File::open(&self.name)?;
        let archive = sevenz_rust2::Archive::read(&mut file, &sevenz_rust2::Password::empty())
            .map_err(|e| other(e.to_string()))?;
        for (i, f) in archive.files.iter().enumerate() {
            self.items.push(Item {
                path: normalize_slash(trim_suffix(f.name.as_bytes(), b"/")),
                size: f.size as i64,
                mod_time: f.has_last_modified_date.then(|| SystemTime::from(f.last_modified_date)),
                is_dir: f.is_directory,
                body: Body::SevenZ(i),
            });
        }
        self.backend = Backend::SevenZ(Box::new(Mutex::new(SevenZ { archive, file, block: None, cache: Vec::new(), block_err: None })));
        Ok(())
    }

    /// LZH:解出來的內容留在記憶體裡。LZSS 的視窗就是輸出本身,沒辦法只解一部分;
    /// 那個年代的壓縮檔不會大到裝不下。
    fn load_lzh(&mut self) -> io::Result<()> {
        let base = base_name(&self.name);
        let mut f = File::open(&self.name)?;
        let (es, err) = wincv_lzh::list_partial(&mut f);
        if es.is_empty() {
            // 一筆都沒有:多半根本不是 LZH(或壞掉了)。要報錯,不能當成空的
            // 壓縮檔。訊息帶格式名,使用者才知道是哪一種格式在抱怨。
            return Err(other(match err {
                Some(e) => format!("{base}: LZH {}", wincv_i18n::t(&e.0)),
                None => tr!("%s: 讀不到任何成員,可能不是 LZH 檔", base),
            }));
        }
        for e in es {
            let mod_time = match e.mtime {
                wincv_lzh::MTime::Unix(t) => Some(SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(t as u64)),
                wincv_lzh::MTime::Dos(t) => dos_time(t),
            };
            if e.is_dir {
                self.items.push(Item {
                    path: trim_suffix(&e.name, b"/").to_vec(),
                    size: 0,
                    mod_time,
                    is_dir: true,
                    body: Body::Mem(Arc::from(&[][..])),
                });
                continue;
            }
            f.seek(SeekFrom::Start(e.offset.max(0) as u64))?;
            let src = (&mut f).take(e.packed.max(0) as u64);
            let body = match wincv_lzh::decode(src, &e.method, e.original.max(0) as u64) {
                Ok(b) => Body::Mem(b.into()),
                Err(err) => Body::Fail(format!("{}: {}", wincv_textenc::utf8_lossy_go(&e.name), wincv_i18n::t(&err.0))),
            };
            self.items.push(Item { path: e.name.clone(), size: e.original, mod_time, is_dir: false, body });
        }
        Ok(())
    }

    /// 單檔壓縮(.gz / .bz2 / .Z)。裡面若是 tar 就展開成多筆,否則就是一個檔;
    /// 這正是 `.tar.gz` 與 `.txt.gz` 的差別。
    fn load_compressed(&mut self, unpack: impl FnOnce(&[u8]) -> io::Result<Vec<u8>>) -> io::Result<()> {
        let raw = std::fs::read(&self.name)?;
        let data = unpack(&raw)?;
        if let Ok(items) = load_tar(&data) {
            if !items.is_empty() {
                self.items = items;
                return Ok(());
            }
        }
        // 與 Go 的 path.Ext 相同:最後一段裡最後一個點之後算副檔名。
        let full = &self.name_bytes;
        let base = match full.iter().rposition(|&b| b == b'/') {
            Some(i) => &full[i + 1..],
            None => &full[..],
        };
        let inner = match base.iter().rposition(|&b| b == b'.') {
            Some(i) => &base[..i],
            None => base,
        };
        let mod_time = std::fs::metadata(&self.name).ok().and_then(|m| m.modified().ok());
        self.items = vec![Item {
            path: inner.to_vec(),
            size: data.len() as i64,
            mod_time,
            is_dir: false,
            body: Body::Mem(data.into()),
        }];
        Ok(())
    }

    /// 補出中間層目錄。很多壓縮檔只存檔案、不存目錄項,沒補的話
    /// `docs/a.txt` 會讓 `docs` 這一層看起來不存在。
    fn build_dirs(&mut self) {
        for e in &self.items {
            if e.is_dir {
                self.dirs.insert(e.path.clone());
            }
            let mut d = go_path_dir(&e.path);
            while !(d.is_empty() || d == b"." || d == b"/") {
                let next = go_path_dir(&d);
                self.dirs.insert(d);
                d = next;
            }
        }
    }

    /// 這個壓縮檔內某個目錄的 vfs 路徑。
    pub fn path(&self, inner: &[u8]) -> PathBuf {
        let mut b = self.name_bytes.clone();
        b.push(SEP);
        if !(inner.is_empty() || inner == b".") {
            b.extend_from_slice(inner);
        }
        PathBuf::from(bytes_os(&b))
    }

    /// 壓縮檔的最上層。
    pub fn root(&self) -> PathBuf {
        self.path(b"")
    }

    /// 壓縮檔本身的路徑。
    pub fn name(&self) -> &Path {
        &self.name
    }

    /// 某個路徑是不是這個壓縮檔的最上層。在最上層再往上就要跳回真實檔案系統。
    pub fn is_root(&self, dir: &Path) -> bool {
        self.inner(dir).is_empty()
    }

    /// 把「a.zip!docs/x」拆出內部路徑「docs/x」。
    ///
    /// 先比對這個壓縮檔自己的路徑,再退回 Go 版的「第一個 !」:壓縮檔所在的
    /// 目錄名稱含有 ! 時,Go 版的拆法會切錯位置。
    pub fn inner(&self, p: &Path) -> Vec<u8> {
        let b = path_bytes(p);
        let n = self.name_bytes.len();
        let rest: &[u8] = if b.len() > n && b.starts_with(&self.name_bytes) && b[n] == SEP {
            &b[n + 1..]
        } else if let Some(i) = b.iter().position(|&c| c == SEP) {
            &b[i + 1..]
        } else {
            &[]
        };
        trim_slashes(rest).to_vec()
    }

    /// 解出一個成員的內容。
    fn read_item(&self, it: &Item) -> io::Result<Arc<[u8]>> {
        match &it.body {
            Body::Mem(b) => Ok(b.clone()),
            Body::Fail(msg) => Err(other(msg.clone())),
            Body::Zip(i) => {
                let Backend::Zip(za) = &self.backend else { unreachable!() };
                let mut za = za.lock().unwrap_or_else(|e| e.into_inner());
                let mut zf = za.by_index(*i).map_err(|e| other(e.to_string()))?;
                let mut out = Vec::with_capacity(zf.size().min(1 << 26) as usize);
                zf.read_to_end(&mut out)?;
                Ok(out.into())
            }
            Body::SevenZ(i) => {
                let Backend::SevenZ(sz) = &self.backend else { unreachable!() };
                sz.lock().unwrap_or_else(|e| e.into_inner()).read(*i)
            }
        }
    }

    /// 把壓縮檔裡的東西解到 `dst_dir`。`names` 為空時解全部,否則只解這些
    /// (內部路徑;指定目錄時連同底下的檔案)。回傳解出來的檔案數。
    pub fn extract(
        &self,
        dst_dir: &Path,
        names: &[Vec<u8>],
        progress: Option<&mut dyn FnMut(&[u8], usize, usize)>,
    ) -> io::Result<usize> {
        write::extract(self, dst_dir, names, progress)
    }
}

/// 把 `src_dir` 底下的 `names` 打包成一個 ZIP。
pub use write::create_zip;

impl SevenZ {
    fn read(&mut self, i: usize) -> io::Result<Arc<[u8]>> {
        let f = &self.archive.files[i];
        if !f.has_stream {
            return Ok(Arc::from(&[][..]));
        }
        let Some(block) = self.archive.stream_map.file_block_index.get(i).copied().flatten() else {
            return Ok(Arc::from(&[][..]));
        };
        if self.block != Some(block) {
            self.block = None;
            self.cache.clear();
            let files = self.archive.files.as_ptr() as usize;
            let size = std::mem::size_of::<sevenz_rust2::ArchiveEntry>();
            let password = sevenz_rust2::Password::empty();
            let mut cache = Vec::new();
            let mut first_err: Option<String> = None;
            let res = sevenz_rust2::BlockDecoder::new(1, block, &self.archive, &password, &mut self.file)
                .for_each_entries(&mut |entry, reader| {
                    let idx = (entry as *const _ as usize - files) / size;
                    let mut data = Vec::with_capacity(entry.size.min(1 << 26) as usize);
                    match reader.read_to_end(&mut data) {
                        Ok(_) => cache.push((idx, Ok(Arc::from(data)))),
                        Err(e) => {
                            first_err.get_or_insert_with(|| e.to_string());
                            cache.push((idx, Err(e.to_string())));
                        }
                    }
                    Ok(true)
                });
            // 區塊解到一半壞掉:已經解好的成員照樣可讀,其餘的報這個錯。
            self.block_err = res.err().map(|e| e.to_string()).or(first_err);
            self.cache = cache;
            self.block = Some(block);
        }
        match self.cache.iter().find(|(j, _)| *j == i) {
            Some((_, Ok(b))) => Ok(b.clone()),
            Some((_, Err(m))) => Err(other(m.clone())),
            None => Err(other(match &self.block_err {
                Some(m) => m.clone(),
                None => tr!("%s: 壓縮檔內找不到這個項目", i.to_string()),
            })),
        }
    }
}

impl Fs for ArchiveFs {
    fn read_dir(&self, dir: &Path) -> io::Result<Vec<vfs::Entry>> {
        let inner = self.inner(dir);
        let mut seen: HashSet<Vec<u8>> = HashSet::new();
        let mut out: Vec<(Vec<u8>, vfs::Entry)> = Vec::new();
        let mut add = |name: &[u8], is_dir: bool, size: i64, mod_time: Option<SystemTime>| {
            if name.is_empty() || !seen.insert(name.to_vec()) {
                return;
            }
            out.push((
                name.to_vec(),
                vfs::Entry {
                    name: wincv_textenc::utf8_lossy_go(name),
                    os_name: bytes_os(name),
                    size,
                    mod_time,
                    is_dir,
                    link: String::new(),
                },
            ));
        };
        for e in &self.items {
            let Some(rel) = under(&e.path, &inner) else { continue };
            if let Some(i) = rel.iter().position(|&c| c == b'/') {
                add(&rel[..i], true, 0, None); // 中間層目錄
                continue;
            }
            add(rel, e.is_dir, e.size, e.mod_time);
        }
        for d in &self.dirs {
            if let Some(rel) = under(d, &inner) {
                if !rel.is_empty() && !rel.contains(&b'/') {
                    add(rel, true, 0, None);
                }
            }
        }
        out.sort_by(|a, b| a.0.cmp(&b.0));
        Ok(out.into_iter().map(|(_, e)| e).collect())
    }

    fn open(&self, path: &Path) -> io::Result<Box<dyn Read>> {
        let inner = self.inner(path);
        match self.items.iter().find(|e| e.path == inner) {
            Some(it) => Ok(Box::new(Cursor::new(self.read_item(it)?))),
            None => Err(other(tr!(
                "%s: 壓縮檔內找不到這個項目",
                wincv_textenc::utf8_lossy_go(&inner)
            ))),
        }
    }

    /// 顯示成「a.zip!docs」,讓使用者一眼看出自己在壓縮檔裡。
    fn label(&self, dir: &Path) -> String {
        let inner = self.inner(dir);
        let mut s = base_name(&self.name);
        s.push(SEP as char);
        s.push_str(&wincv_textenc::utf8_lossy_go(&inner));
        s
    }
}

/// rars 的輸出寫進共用的緩衝區。
struct SharedWriter(Arc<Mutex<Vec<u8>>>);

impl io::Write for SharedWriter {
    fn write(&mut self, buf: &[u8]) -> io::Result<usize> {
        self.0.lock().unwrap_or_else(|e| e.into_inner()).extend_from_slice(buf);
        Ok(buf.len())
    }
    fn flush(&mut self) -> io::Result<()> {
        Ok(())
    }
}

fn mem_item(path: Vec<u8>, size: i64, mod_time: Option<SystemTime>, is_dir: bool, data: Vec<u8>) -> Item {
    Item { path, size, mod_time, is_dir, body: Body::Mem(data.into()) }
}

fn load_tar(data: &[u8]) -> io::Result<Vec<Item>> {
    let mut ar = tar::Archive::new(Cursor::new(data));
    let mut got = Vec::new();
    for e in ar.entries()? {
        let mut e = e?;
        // PAX 全域標頭(`git archive` 產生的 tar 開頭就有一筆)是中繼資料,不是檔案。
        // GNU tar 解壓時不會產生它;Go 版會把它列成一個叫 pax_global_header 的空檔。
        if e.header().entry_type() == tar::EntryType::XGlobalHeader {
            continue;
        }
        let path = trim_suffix(&e.path_bytes(), b"/").to_vec();
        let h = e.header();
        let size = h.size()? as i64;
        let mod_time = h.mtime().ok().map(|t| SystemTime::UNIX_EPOCH + std::time::Duration::from_secs(t));
        let is_dir = h.entry_type().is_dir();
        let mut body = Vec::with_capacity(size.clamp(0, 1 << 26) as usize);
        e.read_to_end(&mut body)?;
        got.push(Item { path, size, mod_time, is_dir, body: Body::Mem(body.into()) });
    }
    Ok(got)
}

fn trim_suffix<'a>(b: &'a [u8], suf: &[u8]) -> &'a [u8] {
    b.strip_suffix(suf).unwrap_or(b)
}

fn trim_slashes(mut b: &[u8]) -> &[u8] {
    while let [b'/', rest @ ..] = b {
        b = rest;
    }
    while let [rest @ .., b'/'] = b {
        b = rest;
    }
    b
}

/// 反斜線換成斜線,去掉結尾的斜線。RAR 與部分 zip 工具用 Windows 的路徑分隔,
/// 壓縮檔內部一律當成斜線處理。
fn normalize_slash(p: &[u8]) -> Vec<u8> {
    let v: Vec<u8> = p.iter().map(|&b| if b == b'\\' { b'/' } else { b }).collect();
    trim_suffix(&v, b"/").to_vec()
}

/// `p` 在 `dir` 底下時回傳相對路徑。
fn under<'a>(p: &'a [u8], dir: &[u8]) -> Option<&'a [u8]> {
    if dir.is_empty() {
        return Some(p);
    }
    if p.len() > dir.len() && p.starts_with(dir) && p[dir.len()] == b'/' {
        Some(&p[dir.len() + 1..])
    } else {
        None
    }
}

/// Go 的 `path.Clean`。
pub fn go_path_clean(p: &[u8]) -> Vec<u8> {
    if p.is_empty() {
        return b".".to_vec();
    }
    let rooted = p[0] == b'/';
    let mut parts: Vec<&[u8]> = Vec::new();
    for seg in p.split(|&c| c == b'/') {
        match seg {
            b"" | b"." => {}
            b".." => {
                if matches!(parts.last(), Some(l) if *l != b"..") {
                    parts.pop();
                } else if !rooted {
                    parts.push(b"..");
                }
            }
            s => parts.push(s),
        }
    }
    let mut out = Vec::new();
    if rooted {
        out.push(b'/');
    }
    out.extend_from_slice(&parts.join(&b'/'));
    if out.is_empty() {
        out.push(b'.');
    }
    out
}

/// Go 的 `path.Dir`:去掉最後一段再 Clean。
pub fn go_path_dir(p: &[u8]) -> Vec<u8> {
    let i = p.iter().rposition(|&c| c == b'/').map(|i| i + 1).unwrap_or(0);
    go_path_clean(&p[..i])
}

/// DOS 日期時間(高 16 位日期、低 16 位時間)當成本地時間。
pub fn dos_time(t: u32) -> Option<SystemTime> {
    let (d, tm) = ((t >> 16) as i32, (t & 0xFFFF) as i32);
    local_time(1980 + (d >> 9), (d >> 5) & 0x0F, d & 0x1F, tm >> 11, (tm >> 5) & 0x3F, (tm & 0x1F) * 2)
}

/// 年月日時分秒當成本地時間。超出範圍的值照 Go 的 `time.Date` 進位
/// (月 0 是前一年 12 月、日 0 是前一個月的最後一天)。
pub fn local_time(y: i32, mo: i32, d: i32, h: i32, mi: i32, s: i32) -> Option<SystemTime> {
    use chrono::{Duration, NaiveDate, TimeZone};
    let m0 = mo - 1;
    let first = NaiveDate::from_ymd_opt(y + m0.div_euclid(12), (m0.rem_euclid(12) + 1) as u32, 1)?;
    let ndt = first.and_hms_opt(0, 0, 0)?
        + Duration::days((d - 1) as i64)
        + Duration::seconds(h as i64 * 3600 + mi as i64 * 60 + s as i64);
    let lt = match chrono::Local.from_local_datetime(&ndt) {
        chrono::LocalResult::Single(t) => t,
        chrono::LocalResult::Ambiguous(a, _) => a,
        // 夏令時間跳過的那一段:往後推一小時。
        chrono::LocalResult::None => chrono::Local.from_local_datetime(&(ndt + Duration::hours(1))).earliest()?,
    };
    Some(lt.into())
}

#[cfg(test)]
mod tests;
