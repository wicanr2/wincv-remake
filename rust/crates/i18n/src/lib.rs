//! 介面文字的語系目錄(對應 Go 版 `internal/i18n`)。
//!
//! 繁體中文原文就是 key。翻譯檔與 Go 版共用同一份
//! `internal/i18n/locales/*.json`,不複製:改字的時候只改一處。
//!
//! key 裡帶 Go 的格式動詞(`%d`、`%q`…),所以格式化在執行期做:
//! [`sprintf`] 依 Go 的規則展開,[`tr!`] 是「先翻譯再格式化」的捷徑,
//! 對應 Go 的 `fmt.Sprintf(i18n.T(...), ...)`。

mod format;

pub use format::{sprintf, Arg};

use std::collections::HashMap;
use std::sync::RwLock;

/// 介面語言。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash)]
pub enum Lang {
    /// 繁體中文,原生語言。
    ZhHant,
    ZhHans,
    En,
    Ja,
}

impl Lang {
    /// BCP 47 標籤。
    pub fn tag(self) -> &'static str {
        match self {
            Lang::ZhHant => "zh-Hant",
            Lang::ZhHans => "zh-Hans",
            Lang::En => "en",
            Lang::Ja => "ja",
        }
    }

    /// 該語言自己的名字:找母語的人認得的是「日本語」,不是「日文」。
    pub fn name(self) -> &'static str {
        match self {
            Lang::ZhHant => "繁體中文",
            Lang::ZhHans => "简体中文",
            Lang::En => "English",
            Lang::Ja => "日本語",
        }
    }

    fn catalog(self) -> Option<&'static str> {
        match self {
            Lang::ZhHant => None,
            Lang::ZhHans => Some(include_str!("../../../../internal/i18n/locales/zh-Hans.json")),
            Lang::En => Some(include_str!("../../../../internal/i18n/locales/en.json")),
            Lang::Ja => Some(include_str!("../../../../internal/i18n/locales/ja.json")),
        }
    }
}

/// 支援的語言,順序就是選單上的順序。
pub fn langs() -> [Lang; 4] {
    [Lang::ZhHant, Lang::ZhHans, Lang::En, Lang::Ja]
}

/// 標籤是否為支援的語言(必須完全相同,例如 `zh-Hant`)。
pub fn valid(s: &str) -> Option<Lang> {
    langs().into_iter().find(|l| l.tag() == s)
}

struct State {
    cur: Lang,
    dict: Option<HashMap<String, String>>,
}

static STATE: RwLock<State> = RwLock::new(State { cur: Lang::ZhHant, dict: None });

/// 換語言。目錄讀不到或壞掉時維持原狀(與 Go 版相同)。
pub fn set(l: Lang) {
    let dict = match l.catalog() {
        None => None,
        Some(src) => match serde_json::from_str::<HashMap<String, String>>(src) {
            Ok(m) => Some(m),
            Err(_) => return,
        },
    };
    let mut st = STATE.write().unwrap_or_else(|e| e.into_inner());
    st.cur = l;
    st.dict = dict;
}

/// 目前的語言。
pub fn current() -> Lang {
    STATE.read().unwrap_or_else(|e| e.into_inner()).cur
}

/// 翻譯。查不到、或譯文是空的,原樣回傳。
pub fn t(s: &str) -> String {
    let st = STATE.read().unwrap_or_else(|e| e.into_inner());
    match st.dict.as_ref().and_then(|d| d.get(s)) {
        Some(v) if !v.is_empty() => v.clone(),
        _ => s.to_string(),
    }
}

/// 先翻譯再依 Go 的格式動詞展開:`tr!("已輸出 %d 個", n)`。
#[macro_export]
macro_rules! tr {
    ($fmt:expr $(,)?) => { $crate::t($fmt) };
    ($fmt:expr, $($arg:expr),+ $(,)?) => {
        $crate::sprintf(&$crate::t($fmt), &[$($crate::Arg::from($arg)),+])
    };
}

/// 由環境變數猜系統語言(LC_ALL > LC_MESSAGES > LANG),猜不到是繁中。
pub fn detect() -> Lang {
    for k in ["LC_ALL", "LC_MESSAGES", "LANG"] {
        if let Ok(v) = std::env::var(k) {
            if !v.is_empty() {
                if let Some(l) = from_tag(&v) {
                    return l;
                }
            }
        }
    }
    Lang::ZhHant
}

/// 把 `zh_TW.UTF-8`、`ja`、`zh-Hans-CN` 這類寫法對到支援的語言。
///
/// 只寫 `zh` 或大陸地區碼算簡體;台灣、香港、澳門與 `Hant` 算繁體。
pub fn from_tag(s: &str) -> Option<Lang> {
    let mut s = s.to_lowercase();
    if let Some(i) = s.find(['.', '@']) {
        s.truncate(i);
    }
    let s = s.replace('_', "-");
    if s.starts_with("ja") {
        return Some(Lang::Ja);
    }
    if s.starts_with("en") {
        return Some(Lang::En);
    }
    if s.starts_with("zh") {
        if s.contains("hant") {
            return Some(Lang::ZhHant);
        }
        if s.contains("hans") {
            return Some(Lang::ZhHans);
        }
        if ["-tw", "-hk", "-mo"].iter().any(|r| s.contains(r)) {
            return Some(Lang::ZhHant);
        }
        return Some(Lang::ZhHans);
    }
    None
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::sync::Mutex;

    /// 語言是全域狀態,改它的測試要排隊。
    static SERIAL: Mutex<()> = Mutex::new(());

    #[test]
    fn from_tag_cases() {
        for (inp, want) in [
            ("ja_JP.UTF-8", Lang::Ja), ("ja", Lang::Ja),
            ("en_US.UTF-8", Lang::En), ("en-GB", Lang::En),
            ("zh_TW.UTF-8", Lang::ZhHant), ("zh-Hant", Lang::ZhHant), ("zh_HK", Lang::ZhHant),
            ("zh_CN.UTF-8", Lang::ZhHans), ("zh-Hans-CN", Lang::ZhHans), ("zh", Lang::ZhHans),
        ] {
            assert_eq!(from_tag(inp), Some(want), "{inp}");
        }
        assert_eq!(from_tag("de_DE"), None);
    }

    #[test]
    fn set_and_fallback() {
        let _g = SERIAL.lock().unwrap_or_else(|e| e.into_inner());
        set(Lang::En);
        assert_eq!(current(), Lang::En);
        assert_eq!(t("這個 key 一定不在目錄裡 zzz"), "這個 key 一定不在目錄裡 zzz");
        assert_eq!(t("檔案"), "File");
        set(Lang::ZhHant);
        assert_eq!(t("檔案"), "檔案");
    }

    #[test]
    fn tr_formats_translated_text() {
        let _g = SERIAL.lock().unwrap_or_else(|e| e.into_inner());
        set(Lang::En);
        let got = tr!("視窗調成 %d×%d 格", 100, 30);
        set(Lang::ZhHant);
        assert!(got.contains("100") && got.contains("30") && !got.contains('%'), "{got}");
        assert_eq!(tr!("視窗調成 %d×%d 格", 100, 30), "視窗調成 100×30 格");
    }

    /// 譯文的格式動詞要與原文相同,數量也要相同(與 Go 版的測試相同)。
    #[test]
    fn catalog_verbs_match_source() {
        for l in langs() {
            let Some(src) = l.catalog() else { continue };
            let m: HashMap<String, String> = serde_json::from_str(src).expect("目錄不是合法 JSON");
            for (k, v) in &m {
                if v.is_empty() {
                    continue;
                }
                let (mut a, mut b) = (format::verbs(k), format::verbs(v));
                a.sort();
                b.sort();
                assert_eq!(a, b, "{}:{k:?} → {v:?}", l.tag());
            }
        }
    }
}
