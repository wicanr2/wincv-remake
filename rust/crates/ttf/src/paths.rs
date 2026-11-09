//! 系統字型放在哪裡:各平台的候選清單與目錄掃描(對應 Go 版 `ttf.Candidates`)。

use std::path::{Path, PathBuf};
use std::sync::OnceLock;

use wincv_i18n::t;

/// 「這台機器長什麼樣」。抽出來是為了測試:字型放在哪裡完全由平台決定,而任何一台
/// 機器上都只驗得到一個平台。把作業系統、環境變數與家目錄變成參數之後,Windows 與
/// macOS 的路徑組法在 Linux 上也驗得動。
pub(crate) struct Host<'a> {
    pub os: &'a str,
    pub env: &'a dyn Fn(&str) -> Option<String>,
    pub home: Option<PathBuf>,
}

fn real_host_env(k: &str) -> Option<String> {
    std::env::var(k).ok().filter(|v| !v.is_empty())
}

fn real_host() -> Host<'static> {
    let home = std::env::var_os(if cfg!(windows) { "USERPROFILE" } else { "HOME" }).map(PathBuf::from);
    Host { os: std::env::consts::OS, env: &real_host_env, home }
}

impl Host<'_> {
    fn join(&self, parts: &[&str]) -> Option<PathBuf> {
        let mut p = self.home.clone()?;
        parts.iter().for_each(|s| p.push(s));
        Some(p)
    }

    /// Windows 的系統字型目錄。
    ///
    /// [雷] 不能寫死 `C:\Windows\Fonts`。Windows 不一定裝在 C:,而症狀是「整台機器一個
    /// 中文都沒有」,看起來像沒裝字型,不像路徑猜錯。
    fn win_fonts_dir(&self) -> PathBuf {
        for k in ["SystemRoot", "windir"] {
            if let Some(v) = (self.env)(k) {
                return Path::new(&v).join("Fonts");
            }
        }
        PathBuf::from(r"C:\Windows\Fonts")
    }

    pub(crate) fn font_dirs(&self) -> Vec<PathBuf> {
        let mut dirs = Vec::new();
        match self.os {
            // Android 10 之後系統被拆成好幾個分割區,字型不再只在 /system/fonts。
            "android" => return ["/system/fonts", "/product/fonts", "/system_ext/fonts"].map(PathBuf::from).to_vec(),
            "macos" => {
                dirs.push(PathBuf::from("/System/Library/Fonts"));
                // Catalina 起附帶的字型都在這裡(Arial Unicode、Songti…)。
                dirs.push(PathBuf::from("/System/Library/Fonts/Supplemental"));
                dirs.push(PathBuf::from("/Library/Fonts"));
                dirs.extend(self.join(&["Library", "Fonts"]));
                return dirs;
            }
            "windows" => {
                dirs.push(self.win_fonts_dir());
                // Windows 10 1803 起,「只為我安裝」的字型放在使用者自己的目錄底下。
                if let Some(v) = (self.env)("LOCALAPPDATA") {
                    dirs.push(Path::new(&v).join("Microsoft").join("Windows").join("Fonts"));
                }
                return dirs;
            }
            _ => {}
        }
        dirs.push(PathBuf::from("/usr/share/fonts"));
        dirs.push(PathBuf::from("/usr/local/share/fonts"));
        // XDG:使用者自己裝的字型。$XDG_DATA_HOME 沒設才是 ~/.local/share。
        match (self.env)("XDG_DATA_HOME") {
            Some(v) => dirs.push(Path::new(&v).join("fonts")),
            None => dirs.extend(self.join(&[".local", "share", "fonts"])),
        }
        dirs.extend(self.join(&[".fonts"]));
        if let Some(v) = (self.env)("XDG_DATA_DIRS") {
            dirs.extend(std::env::split_paths(&v).filter(|d| !d.as_os_str().is_empty()).map(|d| d.join("fonts")));
        }
        // 容器裡跑的時候,主機的字型掛在這裡(Flatpak),NixOS 的系統字型不在 /usr 底下。
        for d in ["/run/host/fonts", "/run/host/user-fonts", "/run/current-system/sw/share/X11/fonts"] {
            dirs.push(PathBuf::from(d));
        }
        dirs
    }

    /// 各平台常見的字型位置(寫死的那一份)。
    pub(crate) fn fixed_candidates(&self) -> Vec<PathBuf> {
        match self.os {
            "android" => {
                // Noto CJK 從 Android 7 起是標配;DroidSansMono 老機器上還在,而且它是等寬的。
                let names = [
                    "DroidSansMono.ttf", "CutiveMono.ttf", "NotoSansCJK-Regular.ttc", "NotoSansMonoCJKtc-Regular.otf",
                    "NotoSansMonoCJKsc-Regular.otf", "NotoSansCJKtc-Regular.otf", "NotoSansKR-Regular.otf",
                    "NotoSansJP-Regular.otf", "NotoSansSymbols-Regular-Subsetted.ttf", "DroidSansFallback.ttf",
                    "Roboto-Regular.ttf",
                ];
                self.font_dirs().iter().flat_map(|d| names.iter().map(move |n| d.join(n))).collect()
            }
            "macos" => [
                "/System/Library/Fonts/Menlo.ttc",
                "/System/Library/Fonts/PingFang.ttc",
                "/System/Library/Fonts/Hiragino Sans GB.ttc",
                "/System/Library/Fonts/AppleSDGothicNeo.ttc",
                "/System/Library/Fonts/STHeiti Medium.ttc",
                "/System/Library/Fonts/Apple Symbols.ttf",
                // Catalina(10.15)把附帶的字型搬進 Supplemental,舊路徑兩個都留著。
                "/System/Library/Fonts/Supplemental/Songti.ttc",
                "/System/Library/Fonts/Supplemental/Arial Unicode.ttf",
                "/Library/Fonts/Arial Unicode.ttf",
            ]
            .map(PathBuf::from)
            .to_vec(),
            "windows" => {
                let d = self.win_fonts_dir();
                [
                    "consola.ttf", "msjh.ttc", "mingliu.ttc", "simsun.ttc", "msyh.ttc", "simhei.ttf", "msgothic.ttc",
                    "malgun.ttf", "segoeui.ttf", "seguisym.ttf", "arialuni.ttf",
                ]
                .iter()
                .map(|n| d.join(n))
                .collect()
            }
            // 路徑因發行版而異。真正管用的是目錄掃描:寫死清單永遠追不上所有發行版。
            _ => [
                "/usr/share/fonts/opentype/noto/NotoSansCJK-Regular.ttc",
                "/usr/share/fonts/truetype/noto/NotoSansCJK-Regular.ttc",
                "/usr/share/fonts/noto-cjk/NotoSansCJK-Regular.ttc",
                "/usr/share/fonts/opentype/noto/NotoSerifCJK-Regular.ttc",
                "/usr/share/fonts/truetype/arphic/uming.ttc",
                "/usr/share/fonts/truetype/arphic/ukai.ttc",
                "/usr/share/fonts/truetype/wqy/wqy-zenhei.ttc",
                "/usr/share/fonts/truetype/wqy/wqy-microhei.ttc",
                "/usr/share/fonts/wqy-zenhei/wqy-zenhei.ttc",
                "/usr/share/fonts/truetype/dejavu/DejaVuSansMono.ttf",
                "/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf",
                "/usr/share/fonts/dejavu/DejaVuSans.ttf",
                "/usr/share/fonts/truetype/liberation/LiberationMono-Regular.ttf",
                "/usr/share/fonts/liberation/LiberationMono-Regular.ttf",
            ]
            .map(PathBuf::from)
            .to_vec(),
        }
    }
}

/// 掃描時認得的字型檔名開頭,依偏好排序。
///
/// 一台桌機的字型目錄裡有好幾百個檔,全部載進來要花數秒、吃掉數十 MB,而其中絕大
/// 多數補不到任何一個缺字。[雷] 這份清單要涵蓋各平台的字型名:只列 Linux 的字型時,
/// 掃描在 Windows 與 macOS 上一個字型都找不到。
const WANTED: &[&str] = &[
    // 跨平台都可能裝到的,涵蓋面最廣的排前面。
    "notosansmonocjk", "notosanscjk", "notoserifcjk", "notosansmono", "sourcehansans", "sourcehanserif",
    // Windows
    "consola", "msgothic", "msjh", "msyh", "mingliu", "simsun", "simhei", "malgun", "gulim", "batang", "yugoth",
    "segoeui", "seguisym", "arialuni",
    // macOS
    "menlo", "pingfang", "hiraginosans", "stheiti", "songti", "heiti", "applesdgothicneo", "applesymbols",
    "arialunicode",
    // Linux
    "wqy-zenhei", "wqy-microhei", "uming", "ukai", "notosanskr", "notosansjp", "notosanssc", "notosanstc",
    "notosanssymbols", "notosansmath", "notosansthai", "notosansdevanagari", "notosansarabic", "notosanshebrew",
    "unifont", "dejavusansmono", "dejavusans", "liberationmono", "freemono", "freeserif",
    // Android
    "droidsansmono", "droidsansfallback", "cutivemono", "roboto",
];

/// 掃描結果的數量上限。每一個字型都會被載進記憶體,而「多一個補得到的字」的邊際
/// 效益掉得非常快,二十個已經涵蓋到罕用符號了。
const SCAN_CAP: usize = 20;

/// 「同一家族但不是 Regular」的檔名。同一家族的不同字重涵蓋的字完全一樣,補不到任何
/// 新字,卻各吃一份記憶體。unifont 附的 *_sample 是展示用的節錄。
fn other_weight(base: &str) -> bool {
    ["black", "bold", "thin", "light", "medium", "semibold", "demi", "extra", "ultra", "heavy", "italic", "oblique", "condensed", "sample"]
        .iter()
        .any(|w| base.contains(w))
}

/// 掃字型目錄,找出名字像「涵蓋面廣」的那幾個。一次執行只掃一次。
pub(crate) fn scan(dirs: &[PathBuf]) -> Vec<PathBuf> {
    let mut hits: Vec<(PathBuf, usize)> = Vec::new();
    for dir in dirs {
        walk(dir, 0, &mut |p| {
            let ext = p.extension().map(|e| e.to_string_lossy().to_lowercase()).unwrap_or_default();
            if !matches!(ext.as_str(), "ttf" | "ttc" | "otf" | "otc") {
                return;
            }
            let base = p.file_name().map(|b| b.to_string_lossy().to_lowercase()).unwrap_or_default().replace(['-', '_', ' '], "");
            if other_weight(&base) {
                return;
            }
            // 比開頭而不是「包含」:包含會把 Inconsolata 當成 Windows 的 consola。
            if let Some(rank) = WANTED.iter().position(|w| base.starts_with(&w.replace('-', ""))) {
                if !hits.iter().any(|(q, _)| q == p) {
                    hits.push((p.to_path_buf(), rank));
                }
            }
        });
    }
    hits.sort_by_key(|h| h.1); // 穩定排序
    hits.into_iter().take(SCAN_CAP).map(|h| h.0).collect()
}

/// 依名稱順序走訪,深度最多 3 層:字型目錄底下通常只有一兩層,遞迴進一個意外的
/// 符號連結會走很久。讀不到就跳過。
fn walk(dir: &Path, depth: usize, f: &mut dyn FnMut(&Path)) {
    let Ok(rd) = std::fs::read_dir(dir) else { return };
    let mut es: Vec<_> = rd.flatten().collect();
    es.sort_by_key(|e| e.file_name());
    for e in es {
        let p = e.path();
        if e.file_type().map(|t| t.is_dir()).unwrap_or(false) {
            if depth < 3 {
                walk(&p, depth + 1, f);
            }
        } else {
            f(&p);
        }
    }
}

/// 要掃的字型目錄。
pub fn font_dirs() -> Vec<PathBuf> {
    real_host().font_dirs()
}

/// 候選字型:寫死的清單接上目錄掃描的結果。第一個讀得起來的就用。
pub fn candidates() -> Vec<String> {
    static SCANNED: OnceLock<Vec<PathBuf>> = OnceLock::new();
    let h = real_host();
    let scanned = SCANNED.get_or_init(|| scan(&h.font_dirs()));
    h.fixed_candidates().iter().chain(scanned).map(|p| p.to_string_lossy().into_owned()).collect()
}

/// 缺字時給使用者的建議:裝哪個套件就有。「缺字」與「字型載不起來」的症狀一模一樣
/// (都是空框),使用者能做的事只有一件,直接說出這個平台上該裝什麼。
pub fn missing_hint() -> String {
    match std::env::consts::OS {
        "linux" => t("缺字:裝一個涵蓋廣的字型就有(Debian/Ubuntu: fonts-noto-cjk、")
            + "Fedora: google-noto-sans-cjk-fonts、Arch: noto-fonts-cjk)",
        "windows" => t("缺字:控制台 → 地區 → 系統管理 → 安裝東亞語系字型"),
        "macos" => t("缺字:系統內建的 PingFang 應該就夠,若仍缺字請用 -fallback 指定字型檔"),
        _ => t("缺字:用 -fallback 指定一個涵蓋廣的字型檔"),
    }
}
