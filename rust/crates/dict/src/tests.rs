//! 照搬 Go 版 `internal/dict` 的測試。

use super::*;

fn big5(s: &str) -> Vec<u8> {
    wincv_textenc::encode(s, Enc::Big5).unwrap()
}

fn mk() -> Dict {
    let lines = |ls: &[&str]| big5(&ls.iter().map(|l| format!("{l}\n")).collect::<String>());
    Dict::from_parts(
        &lines(&["go\x01vi. 去,走", "apple\x01n. 蘋果", "apple\x01n. 蘋果公司", "file\x01n. 檔案"]),
        &lines(&["go\x01go", "apple\x01`^pl"]),
        &lines(&["檔 ㄉㄤˋ 檔案"]),
        &lines(&["went*go", "gone*go"]),
    )
}

#[test]
fn kk_to_ipa_cases() {
    for (raw, want) in [("B!", "bi"), ("M\"A", "ʃɪp"), ("I\"Q", "θɪn"), (">S\"CW", "ˈlɪtl̩"), (">B)CX", "ˈbʌtn̩"), ("CM", "tʃ"), ("DN", "dʒ")] {
        assert_eq!(kk_to_ipa(raw), want, "{raw}");
    }
}

/// 認不得的位元組(夾在條目裡的中文註記)原樣留著。
#[test]
fn kk_keeps_unknown_bytes() {
    assert_eq!(kk_to_ipa("#用口上語調"), "ɛ用口上語調");
}

#[test]
fn lookup_basic() {
    let e = mk().lookup("go").unwrap();
    assert!(e.trans.contains('走'));
    assert_eq!(e.kk, "go");
}

/// 同一個詞出現多次要接起來,不能後面的蓋掉前面的。
#[test]
fn duplicate_keys_merge() {
    let e = mk().lookup("apple").unwrap();
    assert!(e.trans.contains("蘋果") && e.trans.contains("公司"), "{}", e.trans);
}

/// 查 went 要找得到 go。
#[test]
fn inflected_form_falls_back_to_base() {
    let e = mk().lookup("went").unwrap();
    assert_eq!(e.base, "go");
    assert!(e.trans.contains('走'));
}

#[test]
fn case_insensitive() {
    let d = mk();
    assert!(d.lookup("GO").is_some() && d.lookup("Apple").is_some());
}

#[test]
fn chinese_lookup() {
    assert!(mk().lookup("檔").unwrap().trans.contains("檔案"));
}

#[test]
fn prefix() {
    let d = mk();
    assert_eq!(d.prefix("f", 10), ["file"]);
    assert!(d.prefix("zzz", 10).is_empty());
}

/// 用原版真實資料跑一遍。
#[test]
fn real_dictionaries() {
    let dir = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../original/app");
    if !dir.join("eng.txt.dat").exists() {
        return;
    }
    let d = Dict::load(&dir).unwrap();
    let (eng, kk, _, verb) = d.len();
    assert!(eng >= 100000 && kk >= 50000 && verb >= 300, "{:?}", d.len());
    for w in ["file", "computer", "dictionary"] {
        assert!(!d.lookup(w).unwrap().trans.is_empty(), "{w}");
    }
    // 直接收錄的變化形走 exact,不該被當成 fallback。
    assert_eq!(d.lookup("went").unwrap().base, "");
    // 沒被收錄的變化形才走 origin-verb。
    let e = d.lookup("abided").unwrap();
    assert_eq!(e.base, "abide");
    assert!(!e.trans.is_empty());
}
