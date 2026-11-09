//! 可持久化的 24 點 CJK 字形來源代號(對應 Go 版 `internal/cjk24`)。
//!
//! 設定只存短代號,不存使用者電腦的絕對路徑;實際檔案位置由資料目錄決定,設定才可
//! 隨資料目錄與裝置搬移。

/// 一種可選的 24×24 漢字與符號字模來源。
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct Source {
    pub id: &'static str,
    pub std: &'static str,
    pub spc: &'static str,
    /// 同一套字形在其他版本裡的檔名,依序在 std 找不到時才找。倚天 3.53 叫 STD.24M,
    /// 倚天 2000 (5.0) 叫 STDFONT.24;2000 版沒有用 ETUNPACK 壓縮,載入端兩種都認。
    pub alts: &'static [&'static str],
}

impl Source {
    /// 依尋找順序列出漢字區的候選檔名。
    pub fn std_names(&self) -> Vec<&'static str> {
        std::iter::once(self.std).chain(self.alts.iter().copied()).collect()
    }
}

/// 舊版自動尋找的倚天明體,舊 session 沒有欄位時以此相容。
pub const DEFAULT: &str = "eten-m";

pub const SOURCES: &[Source] = &[
    Source { id: "eten-m", std: "STD.24M", spc: "SPCFONT.24", alts: &["STDFONT.24", "STDFONT.24M"] },
    Source { id: "eten-k", std: "STD.24K", spc: "SPCFONT.24", alts: &["STDFONT.24K"] },
    Source { id: "eten-l", std: "STD.24L", spc: "SPCFONT.24", alts: &["STDFONT.24L"] },
    Source { id: "eten-r", std: "STD.24R", spc: "SPCFONT.24", alts: &["STDFONT.24R"] },
    Source { id: "eten-b", std: "STD.24B", spc: "SPCFONT.24", alts: &["STDFONT.24B"] },
    Source { id: "eten-s", std: "STD.24S", spc: "SPCFONT.24", alts: &["STDFONT.24S"] },
    Source { id: "guoqiao", std: "stdfont.24f", spc: "spcfont.24", alts: &[] },
];

/// 依持久化代號取來源。
pub fn find(id: &str) -> Option<Source> {
    SOURCES.iter().find(|s| s.id == id).copied()
}
