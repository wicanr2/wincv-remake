//! 照搬 Go 版 `internal/archive` 的測試,另加格式表與 Go 版的逐列比對。

use super::*;
use std::io::Write;

/// 每個測試自己的暫存目錄。
fn tmpdir(tag: &str) -> PathBuf {
    let d = std::env::temp_dir().join(format!("wincv-archive-{tag}-{}", std::process::id()));
    let _ = std::fs::remove_dir_all(&d);
    std::fs::create_dir_all(&d).unwrap();
    d
}

fn repo() -> PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../..")
}

/// 刻意「只存檔案、不存目錄項」:很多壓縮工具就是這樣,中間層目錄要自己補。
fn write_zip(dir: &Path) -> PathBuf {
    let p = dir.join("a.zip");
    let mut zw = zip::ZipWriter::new(File::create(&p).unwrap());
    for (name, body) in [("readme.txt", "hello\n"), ("docs/guide.txt", "guide\n"), ("docs/img/a.txt", "img\n")] {
        let opt = zip::write::SimpleFileOptions::default().compression_method(zip::CompressionMethod::Deflated);
        zw.start_file(name, opt).unwrap();
        zw.write_all(body.as_bytes()).unwrap();
    }
    zw.finish().unwrap();
    p
}

fn write_tar_gz(dir: &Path) -> PathBuf {
    let mut tb = tar::Builder::new(Vec::new());
    for (name, body) in [("x.txt", "xxx\n"), ("sub/y.txt", "yyy\n")] {
        let mut h = tar::Header::new_ustar();
        h.set_size(body.len() as u64);
        h.set_mode(0o644);
        h.set_mtime(1293840000);
        tb.append_data(&mut h, name, body.as_bytes()).unwrap();
    }
    let data = tb.into_inner().unwrap();
    let p = dir.join("b.tar.gz");
    let mut gz = flate2::write::GzEncoder::new(File::create(&p).unwrap(), flate2::Compression::default());
    gz.write_all(&data).unwrap();
    gz.finish().unwrap();
    p
}

fn names_of(a: &ArchiveFs, dir: &Path) -> Vec<String> {
    a.read_dir(dir)
        .unwrap()
        .into_iter()
        .map(|e| if e.is_dir { format!("{}/", e.name) } else { e.name })
        .collect()
}

fn read_all(a: &ArchiveFs, inner: &str) -> Vec<u8> {
    let mut b = Vec::new();
    a.open(&a.path(inner.as_bytes())).unwrap().read_to_end(&mut b).unwrap();
    b
}

#[test]
fn zip_browse() {
    let dir = tmpdir("zipbrowse");
    let a = ArchiveFs::open(&write_zip(&dir)).unwrap();
    assert_eq!(names_of(&a, &a.root()).join(" "), "docs/ readme.txt");
    assert_eq!(names_of(&a, &a.path(b"docs")).join(" "), "guide.txt img/");
    assert_eq!(names_of(&a, &a.path(b"docs/img")).join(" "), "a.txt");
    assert_eq!(read_all(&a, "docs/guide.txt"), b"guide\n");
    assert_eq!(a.label(&a.path(b"docs")), "a.zip!docs");
    assert!(a.is_root(&a.root()) && !a.is_root(&a.path(b"docs")));
}

/// .tar.gz 要展開成裡面的多個檔,不是一個叫 b.tar 的檔。
#[test]
fn tar_gz_expands() {
    let dir = tmpdir("targz");
    let a = ArchiveFs::open(&write_tar_gz(&dir)).unwrap();
    assert_eq!(names_of(&a, &a.root()).join(" "), "sub/ x.txt");
    assert_eq!(read_all(&a, "sub/y.txt"), b"yyy\n");
}

/// .txt.gz 裡面不是 tar,應該只有一個檔,名字去掉 .gz。
#[test]
fn plain_gz_is_single_file() {
    let dir = tmpdir("plaingz");
    let p = dir.join("note.txt.gz");
    let mut gz = flate2::write::GzEncoder::new(File::create(&p).unwrap(), flate2::Compression::default());
    gz.write_all(b"just a text file\n").unwrap();
    gz.finish().unwrap();
    let a = ArchiveFs::open(&p).unwrap();
    assert_eq!(names_of(&a, &a.root()), ["note.txt"]);
    assert_eq!(read_all(&a, "note.txt"), b"just a text file\n");
}

#[test]
fn detect_format_longest_wins() {
    for (name, want) in [("a.tar.gz", "GZIP"), ("a.gz", "GZIP"), ("a.zip", "ZIP"), ("a.RAR", "RAR"), ("a.tar.Z", "compress")] {
        assert_eq!(detect_format(Path::new(name)).map(|f| f.name), Some(want), "{name}");
    }
    assert!(detect_format(Path::new("a.txt")).is_none());
}

/// 認不得的副檔名要說清楚,不是含糊的失敗。
#[test]
fn unknown_extension_message() {
    let dir = tmpdir("unknown");
    let p = dir.join("x.wibble");
    std::fs::write(&p, b"dummy").unwrap();
    let err = ArchiveFs::open(&p).err().unwrap().to_string();
    assert!(err.contains("認不出壓縮格式"), "{err}");
}

/// 副檔名對但內容不對時,錯誤訊息要指出是哪一種格式在抱怨。
#[test]
fn corrupt_file_message_names_format() {
    let dir = tmpdir("corrupt");
    for f in FORMATS.iter().filter(|f| f.supported) {
        if matches!(f.name, "ZIP" | "TAR" | "GZIP" | "BZIP2" | "RAR" | "7-Zip" | "compress") {
            continue; // 錯誤訊息來自 crate,不保證帶格式名
        }
        let p = dir.join(format!("x{}", f.ext[0]));
        std::fs::write(&p, "這不是壓縮檔,只是一段中文").unwrap();
        let err = ArchiveFs::open(&p).err().unwrap_or_else(|| panic!("{}: 內容不對應該回錯誤", f.name)).to_string();
        let want = if f.name == "LHA" { "LZH" } else { f.name.split('/').next().unwrap() };
        assert!(err.contains(want), "{}: 訊息沒指出格式: {err}", f.name);
    }
}

/// 真的能把 LZH 當目錄瀏覽。
#[test]
fn lzh_as_fs() {
    let a = ArchiveFs::open(&repo().join("testdata/lzh/dosbox-sample.lha")).unwrap();
    // C 是目錄項,C.info / dosbox.diff / dosbox.readme 是檔案
    assert_eq!(names_of(&a, &a.path(b"DOSBox")).len(), 4);
    assert_eq!(read_all(&a, "DOSBox/dosbox.readme").len(), 3097);
}

/// 真實的 .7z(自行產生)。
#[test]
fn seven_zip() {
    let a = ArchiveFs::open(&repo().join("internal/archive/testdata/sample.7z")).unwrap();
    assert_eq!(names_of(&a, &a.root()).join(" "), "docs/ top.txt");
    assert_eq!(read_all(&a, "docs/deep.txt"), b"deep\n");
    assert_eq!(read_all(&a, "top.txt").is_empty(), false);
}

/// 真實世界的 .rar(1999 年的檔,solid 壓縮),來源見 internal/archive/testdata/README.md。
#[test]
fn rar() {
    let a = ArchiveFs::open(&repo().join("internal/archive/testdata/sample.rar")).unwrap();
    assert_eq!(names_of(&a, &a.root()).len(), 2);
    assert_eq!(read_all(&a, "EOB3FIX.DOC").len(), 303);
}

/// .Z 用 ncompress 壓出來的測試檔(testdata/z)。
#[test]
fn compress_z() {
    for (file, size) in [("real_small.Z", 2000), ("real16.Z", 229880), ("real12.Z", 229880)] {
        let data = std::fs::read(repo().join("testdata/z").join(file)).unwrap();
        assert_eq!(zcompress::decode(&data).unwrap().len(), size, "{file}");
    }
    assert!(zcompress::decode(b"not a Z file").is_err());
    assert!(!zcompress::is_z(&[0x1F, 0x8B, 0x08]));
}

#[test]
fn extract_all_and_subset() {
    let dir = tmpdir("extract");
    let a = ArchiveFs::open(&write_zip(&dir)).unwrap();
    let out = dir.join("out");
    assert_eq!(a.extract(&out, &[], None).unwrap(), 3);
    assert_eq!(std::fs::read(out.join("docs/img/a.txt")).unwrap(), b"img\n");
    // 指定一個目錄,底下的東西都要跟著出來
    let out2 = dir.join("out2");
    assert_eq!(a.extract(&out2, &[b"docs".to_vec()], None).unwrap(), 2);
    assert!(!out2.join("readme.txt").exists(), "沒指定的檔案不該被解出來");
}

/// 壓縮檔裡的路徑不可信:`..` 可以寫到目標目錄之外(zip slip)。
#[test]
fn extract_rejects_path_escape() {
    let dir = tmpdir("escape");
    let p = dir.join("evil.zip");
    let mut zw = zip::ZipWriter::new(File::create(&p).unwrap());
    zw.start_file("../escaped.txt", zip::write::SimpleFileOptions::default()).unwrap();
    zw.write_all(b"pwned").unwrap();
    zw.finish().unwrap();
    let a = ArchiveFs::open(&p).unwrap();
    let _ = a.extract(&dir.join("out"), &[], None);
    assert!(!dir.join("escaped.txt").exists(), "檔案被寫到目標目錄之外了");
}

#[test]
fn create_zip_round_trip() {
    let dir = tmpdir("create");
    let src = dir.join("src");
    std::fs::create_dir_all(src.join("sub")).unwrap();
    std::fs::write(src.join("a.txt"), b"aaa").unwrap();
    std::fs::write(src.join("sub/b.txt"), b"bbb").unwrap();
    let out = dir.join("made.zip");
    create_zip(&out, &src, &[PathBuf::from("a.txt"), PathBuf::from("sub")], None).unwrap();
    let a = ArchiveFs::open(&out).unwrap();
    assert_eq!(names_of(&a, &a.root()).join(" "), "a.txt sub/");
    assert_eq!(read_all(&a, "sub/b.txt"), b"bbb");
}

#[test]
fn go_path_helpers() {
    assert_eq!(go_path_dir(b"./docs/a"), b"docs");
    assert_eq!(go_path_dir(b"a"), b".");
    assert_eq!(go_path_dir(b"/a"), b"/");
    assert_eq!(go_path_clean(b"/../x"), b"/x");
    assert_eq!(go_path_clean(b"a/../../b"), b"../b");
}

#[test]
fn dos_time_normalizes_like_go() {
    use chrono::{Datelike, Timelike};
    // 2011-11-25 10:00:00
    let t = ((31u32 << 9 | 11 << 5 | 25) << 16) | (10 << 11);
    let lt: chrono::DateTime<chrono::Local> = dos_time(t).unwrap().into();
    assert_eq!((lt.year(), lt.month(), lt.day(), lt.hour(), lt.minute()), (2011, 11, 25, 10, 0));
    // 月 0、日 0:Go 的 time.Date 進位成前一年 11 月 30 日
    let lt: chrono::DateTime<chrono::Local> = local_time(1980, 0, 0, 0, 0, 0).unwrap().into();
    assert_eq!((lt.year(), lt.month(), lt.day()), (1979, 11, 30));
}

/// 格式表的副檔名與名稱要與 Go 版的 `archive.Formats` 逐列相同。
#[test]
fn formats_match_go() {
    let src = std::fs::read_to_string(repo().join("internal/archive/archive.go")).unwrap();
    let mut go = Vec::new();
    for line in src.lines() {
        let l = line.trim();
        let Some(rest) = l.strip_prefix("{[]string{") else { continue };
        let (exts, rest) = rest.split_once('}').unwrap();
        let exts: Vec<String> = exts.split(',').map(|e| e.trim().trim_matches('"').to_string()).collect();
        let name = rest.trim_start_matches(',').trim().split('"').nth(1).unwrap().to_string();
        go.push((exts, name));
    }
    let rust: Vec<(Vec<String>, String)> =
        FORMATS.iter().map(|f| (f.ext.iter().map(|e| e.to_string()).collect(), f.name.to_string())).collect();
    assert_eq!(rust, go);
}

fn sha_prefix(b: &[u8], n: usize) -> String {
    use sha2::{Digest, Sha256};
    Sha256::digest(b)[..n].iter().map(|x| format!("{x:02x}")).collect()
}

fn testdata(rel: &str) -> Vec<u8> {
    std::fs::read(repo().join("testdata").join(rel)).unwrap()
}

/// 三份 arj 3.10 壓出來的檔(方法 0、1、4)。期望值是原始檔 sha256 的前 16 位元組。
#[test]
fn arj_methods() {
    let want = [
        ("big.txt", 229880, "a0a61c73569bdbfbd4252b6b0c664bf1"),
        ("small.txt", 2000, "799f9cc5416fe970d46ebaaa5d3a4e6b"),
        // arj 存的是含路徑的名字,列表指令顯示時才省略掉目錄
        ("sub/mid.txt", 100000, "dad9797d73725309f3e5e2e8886c0589"),
    ];
    for (file, n) in [("m1.arj", 3), ("m4.arj", 2), ("m0.arj", 1)] {
        let fs = arj::read(&testdata(&format!("arj/{file}"))).unwrap();
        assert_eq!(fs.len(), n, "{file}");
        for f in fs {
            let name = String::from_utf8(f.name).unwrap();
            let (_, size, sum) = want.iter().find(|w| w.0 == name).unwrap_or_else(|| panic!("多出 {name}"));
            assert_eq!(f.data.len(), *size, "{file} {name}");
            assert_eq!(sha_prefix(&f.data, 16), *sum, "{file} {name}(方法 {})", f.method);
        }
    }
    assert!(arj::read(b"not an arj file").is_err());
}

/// gcab 造的 CAB,內容用 cabextract 對過。big.txt 會切成七個以上的 MSZIP 區塊,
/// 每個區塊的 LZ77 視窗接續前一個區塊的輸出。
#[test]
fn cab_mszip_and_stored() {
    let want = [
        ("big.txt", 229880, "a0a61c73569bdbfbd4252b6b0c664bf1"),
        ("small.txt", 2000, "799f9cc5416fe970d46ebaaa5d3a4e6b"),
        ("sub/mid.txt", 100000, "dad9797d73725309f3e5e2e8886c0589"),
    ];
    let fs = cab::read(&testdata("cab/mszip.cab")).unwrap();
    assert_eq!(fs.len(), 3);
    for f in &fs {
        let name = String::from_utf8(f.name.clone()).unwrap();
        let (_, size, sum) = want.iter().find(|w| w.0 == name).unwrap_or_else(|| panic!("多出 {name}"));
        assert_eq!((f.size, f.data.len()), (*size as i64, *size), "{name}");
        assert_eq!(sha_prefix(&f.data, 16), *sum, "{name}");
    }
    let fs = cab::read(&testdata("cab/store.cab")).unwrap();
    assert_eq!(fs.len(), 1);
    assert_eq!(sha_prefix(&fs[0].data, 16), want[1].2);
    assert!(cab::read(b"not a cab file at all............").is_err());
}

#[test]
fn arc_crunched_and_mixed() {
    for file in ["t.arc", "mix.arc"] {
        let fs = arc::read(&testdata(&format!("arc/{file}"))).unwrap();
        assert!(!fs.is_empty(), "{file}");
        for f in &fs {
            assert_eq!(f.data.len() as i64, f.size, "{file} {}", String::from_utf8_lossy(&f.name));
        }
    }
    assert!(arc::read(b"not an arc").is_err());
}

/// 每個成員解出來都會比對標頭裡的 CRC-32;sha256 是 acefile(獨立實作)對過的值。
#[test]
fn ace_blocked_and_stored() {
    let want = [
        ("winappdbg-winappdbg_v1.6/doc/Makefile", 3136, "d476fb416efdb1c7"),
        ("winappdbg-winappdbg_v1.6/winappdbg/plugins/README", 49, "8efd4ce2c5c19a44"),
        ("winappdbg-winappdbg_v1.6/distro.bat", 8637, "ddc6c757b15ac84a"),
        ("winappdbg-winappdbg_v1.6/install.bat", 1848, "47717acbd26023ca"),
        ("winappdbg-winappdbg_v1.6/doc/make.bat", 3194, "ea81a1047b6b2369"),
        ("winappdbg-winappdbg_v1.6/epydoc.cfg", 351, "bc682ec8b2a49092"),
        ("winappdbg-winappdbg_v1.6/tools/example.cfg", 4966, "4bc0d5fdc0ca3180"),
        ("winappdbg-winappdbg_v1.6/install.cfg", 664, "f6ee9501635926b2"),
    ];
    let mut got = 0;
    for f in ace::read(&testdata("ace/blocked.ace")).unwrap() {
        let name = String::from_utf8(f.name).unwrap();
        let Some((_, size, sum)) = want.iter().find(|w| w.0 == name) else {
            assert!(f.data.is_empty(), "多出沒預期的成員 {name}");
            continue;
        };
        got += 1;
        assert_eq!(f.data.len(), *size, "{name}");
        assert_eq!(sha_prefix(&f.data, 8), *sum, "{name}");
    }
    assert_eq!(got, want.len());
    let fs = ace::read(&testdata("ace/cafe.ace")).unwrap();
    assert_eq!(fs.len(), 1);
    assert_eq!(fs[0].comp_type, 0);
    assert_eq!(sha_prefix(&fs[0].data, 8), "29acbd14ec3622e5");
    assert!(ace::read(b"this is not an ace archive at all").is_err());
}

/// 完整的 acefile-testdata 語料(涵蓋 LZ77_EXE 與 LZ77_DELTA),設 WINCV_ACE_CORPUS 才跑。
#[test]
fn ace_full_corpus() {
    let Ok(dir) = std::env::var("WINCV_ACE_CORPUS") else { return };
    let mut total = 0;
    for sub in std::fs::read_dir(&dir).unwrap().flatten() {
        let Ok(rd) = std::fs::read_dir(sub.path()) else { continue };
        for f in rd.flatten().filter(|f| f.path().extension().is_some_and(|e| e == "ace")) {
            let fs = ace::read(&std::fs::read(f.path()).unwrap()).unwrap_or_else(|e| panic!("{:?}: {e}", f.path()));
            total += fs.len();
        }
    }
    assert!(total > 0, "{dir} 底下找不到 .ace");
    eprintln!("ACE 語料 {total} 個成員全部通過 CRC-32");
}

/// PAX 全域標頭不是檔案,不列出來。
#[test]
fn pax_global_header_is_not_a_file() {
    let dir = tmpdir("pax");
    let mut tb = tar::Builder::new(Vec::new());
    let mut g = tar::Header::new_ustar();
    g.set_entry_type(tar::EntryType::XGlobalHeader);
    let rec = b"52 comment=0123456789abcdef0123456789abcdef01234567\n";
    g.set_size(rec.len() as u64);
    tb.append_data(&mut g, "pax_global_header", &rec[..]).unwrap();
    let mut h = tar::Header::new_ustar();
    h.set_size(3);
    tb.append_data(&mut h, "a.txt", &b"abc"[..]).unwrap();
    let p = dir.join("g.tar");
    std::fs::write(&p, tb.into_inner().unwrap()).unwrap();
    let a = ArchiveFs::open(&p).unwrap();
    assert_eq!(names_of(&a, &a.root()), ["a.txt"]);
}

/// `./` 開頭、絕對路徑、重複斜線的成員都正規化,最上層不會多一個「.」目錄。
#[test]
fn member_paths_are_normalized() {
    let dir = tmpdir("dotslash");
    let mut tb = tar::Builder::new(Vec::new());
    for (name, body) in [("./", ""), ("./docs/a.txt", "a"), ("./b.txt", "b"), ("/abs/c.txt", "c"), ("x//y.txt", "y")] {
        let mut h = tar::Header::new_gnu();
        h.set_size(body.len() as u64);
        if name.ends_with('/') {
            h.set_entry_type(tar::EntryType::Directory);
        }
        // append_data 會拒絕 ./ 與絕對路徑,直接寫進標頭。
        h.as_gnu_mut().unwrap().name[..name.len()].copy_from_slice(name.as_bytes());
        h.set_cksum();
        tb.append(&h, body.as_bytes()).unwrap();
    }
    let p = dir.join("dot.tar");
    std::fs::write(&p, tb.into_inner().unwrap()).unwrap();
    let a = ArchiveFs::open(&p).unwrap();
    assert_eq!(names_of(&a, &a.root()).join(" "), "abs/ b.txt docs/ x/");
    assert_eq!(names_of(&a, &a.path(b"docs")), ["a.txt"]);
    assert_eq!(read_all(&a, "abs/c.txt"), b"c");
    assert_eq!(read_all(&a, "x/y.txt"), b"y");
}
