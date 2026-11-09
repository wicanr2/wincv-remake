//! 期望值來自 7z(p7zip)對同一個檔案的解壓縮結果,與 Go 版的測試相同。

use std::fs::File;
use std::io::{Seek, SeekFrom};

use sha2::{Digest, Sha256};

const WANT: &[(&str, usize, &str)] = &[
    ("118BOARD.DOC", 8625, "d26f46c3e43be61d"),
    ("INIT118.ASM", 31572, "9464e17080074373"),
    ("INIT118.COM", 4175, "01a059bc898060e5"),
    ("INIT118.DOC", 5198, "8c709e098ca085e0"),
    ("DOSBox/C.info", 8504, "6a662e50967d5c38"),
    ("DOSBox/dosbox.diff", 41241, "ac9fe9afc6031d00"),
    ("DOSBox/dosbox.readme", 3097, "2627623613a7c37e"),
];

#[test]
fn decode_against_7z() {
    let mut seen = 0;
    for name in ["init1181.lzh", "dosbox-sample.lha"] {
        let path = format!("../../../testdata/lzh/{name}");
        let mut f = File::open(&path).unwrap_or_else(|e| panic!("{path}: {e}"));
        let es = wincv_lzh::list(&mut f).unwrap();
        assert!(!es.is_empty(), "{name} 一筆都沒讀到");
        for e in es.iter().filter(|e| !e.is_dir) {
            let n = String::from_utf8_lossy(&e.name).into_owned();
            let &(_, size, sum) = WANT.iter().find(|w| w.0 == n).unwrap_or_else(|| panic!("多出沒預期的成員 {n}"));
            f.seek(SeekFrom::Start(e.offset as u64)).unwrap();
            let out = wincv_lzh::decode(&mut f, &e.method, e.original as u64).unwrap_or_else(|err| panic!("{n}: {err}"));
            assert_eq!(out.len(), size, "{n} 大小");
            let h = Sha256::digest(&out);
            let hex: String = h.iter().take(8).map(|b| format!("{b:02x}")).collect();
            assert_eq!(hex, sum, "{n} 內容");
            seen += 1;
        }
    }
    assert_eq!(seen, WANT.len());
}
