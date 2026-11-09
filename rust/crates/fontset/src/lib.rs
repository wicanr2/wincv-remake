//! 決定「一個字級長什麼樣」:半形點陣字型、配合它的全形來源、以及補缺字的後備鏈
//! (對應 Go 版 `internal/fontset`)。
//!
//! 桌面、celldump 與 Android 三個進入點都要做同一件事,而其中一項細節很容易做錯:
//! 倚天字庫載入時要給的是**字庫自己的**點陣尺寸(16×15),不是要畫多大。傳錯的話
//! 那個字級整批沒有中文,而且沒有錯誤訊息。這裡有測試盯著。

use std::path::Path;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::{Arc, OnceLock};

use wincv_eten as eten;
use wincv_i18n::tr;
use wincv_render::{scale_cjk, CjkSource, HalfSource, Sources, LINE_GAP};
use wincv_ttf::Chain;

/// 全形來源。字級之間切換時共用,所以是 `Arc`。
pub type Cjk = Arc<dyn CjkSource + Send + Sync>;
/// 半形來源。
pub type Half = Arc<dyn HalfSource + Send + Sync>;

static BITMAP_CJK: AtomicBool = AtomicBool::new(false);

/// 開著時所有字級的全形字都用倚天字模縮放(舊行為)。預設關:倚天原生尺寸以外的
/// 字級改用向量字型反鋸齒,倚天當後備。
pub fn set_bitmap_cjk(on: bool) {
    BITMAP_CJK.store(on, Ordering::Relaxed);
}

pub fn bitmap_cjk() -> bool {
    BITMAP_CJK.load(Ordering::Relaxed)
}

/// 決定一個字級的全形來源與後備。
///
/// 倚天只有 16×15(與 24×24)那幾份字模,其餘字級的漢字只能縮放,而非整數倍的
/// 最近鄰縮放會讓筆畫有的一格有的兩格,放大之後整片鋸齒。原版其實是把全形字交給
/// Windows 用系統字型畫(image 裡指名「新細明體」),所以大字級改問向量字型更接近它。
///
/// 規則:格子正好是倚天的原生尺寸就用倚天(逐像素對齊);否則向量字型(反鋸齒)
/// 優先,倚天縮放當後備。什麼字型都沒裝的機器仍然有中文可看,只是鋸齒。
pub fn compose(cw: i32, ch: i32, et: Option<&Arc<eten::Font>>, fb: Option<Chain>) -> (Option<Cjk>, Option<Chain>) {
    let scaled: Option<Cjk> = et.map(|e| Arc::new(scale_cjk(e.clone(), cw * 2, ch)) as Cjk);
    let native = et.is_some_and(|e| cw * 2 == e.w && ch - LINE_GAP == e.h);
    let Some(fb) = fb else { return (scaled, None) };
    if native || bitmap_cjk() {
        return (scaled, Some(fb));
    }
    // 開反鋸齒。後備鏈與這裡回傳的是同一批字型,所以兩邊都會帶覆蓋率,與 Go 版相同。
    fb.set_aa(true);
    let mut list: Vec<Box<dyn CjkSource + Send + Sync>> = vec![Box::new(fb.clone())];
    if let Some(s) = scaled {
        list.push(Box::new(s));
    }
    (Some(Arc::new(Sources(list))), Some(fb))
}

/// 一個字級:一種半形點陣字型,加上配合它的全形來源。
pub struct Level {
    pub name: String,
    pub half: Half,
    pub cjk: Option<Cjk>,
    pub fb: Option<Chain>,
}

/// 原版隨附的三種半形點陣字型,由小到大。
pub const FON_NAMES: [&str; 3] = ["cvga.fon", "CVGA1018.FON", "cvga1224.FON"];

/// 原版三種半形點陣字型的尺寸。沒有那三個 .FON 時照同樣的尺寸從系統字型現場產:
/// 尺寸一樣,版面、欄位對齊、按鍵行為就完全一樣,只有字形不同。
pub const SIZES: [(&str, i32, i32); 3] = [("8x15", 8, 15), ("10x18", 10, 18), ("12x24", 12, 24)];

/// 字庫檔的路徑。`None` 表示沒指定。
#[derive(Clone, Copy, Default)]
pub struct EtenPaths<'a> {
    pub std: Option<&'a Path>,
    pub spc: Option<&'a Path>,
    pub std24: Option<&'a Path>,
    pub spc24: Option<&'a Path>,
}

/// 把能載到的字級都準備好。載不到的就跳過,但要說出來:少一個字級的症狀是
/// 「Ctrl-+ 沒反應」,那看起來像壞掉而不像缺檔案。
pub fn load(dir: &Path, p: EtenPaths<'_>, fb_path: Option<&Path>, no_fb: bool) -> Vec<Level> {
    let et15 = eten15(p.std, p.spc, eten::NATIVE_W, eten::NATIVE_H).map(Arc::new);
    let et24 = eten24(p.std24, p.spc24).map(Arc::new);
    let mut out = Vec::new();
    for name in FON_NAMES {
        // 磁碟優先,內嵌是後備:使用者放在執行檔旁邊的字型永遠贏。
        let d = match std::fs::read(dir.join(name)) {
            Ok(d) => d,
            Err(_) => match wincv_bundled::get(name) {
                Some(b) => b.to_vec(),
                None => continue,
            },
        };
        let half = match wincv_fnt::Font::parse(d) {
            Ok(h) => h,
            Err(e) => {
                eprint!("{}", tr!("警告:%s 解不開 (%v)\n", name, e.to_string()));
                continue;
            }
        };
        let (pw, ph) = half.size();
        let (cw, ch) = (pw, ph + LINE_GAP);
        let et = eten_for_level(cw, et15.as_ref().ok(), et24.as_ref().ok());
        if et.is_none() {
            // 每一個字級都要報。只報第一個的話,「只有某幾個字級沒有中文」這種狀況
            // 會完全沒有訊息,而使用者要按到那一級才看得到。
            let e = et15.as_ref().err().map(|e| e.to_string()).unwrap_or_default();
            eprint!("{}", tr!("提示:%s 這一級沒有倚天字庫,全形字改用後備字型 (%v)\n", name, e));
        } else if cw * 2 == eten::NATIVE24_W {
            if let Err(e) = &et24 {
                eprint!(
                    "{}",
                    tr!("提示:%s 這一級沒有原生 24 點字庫,全形字以 15 點字庫或後備字型顯示 (%v)\n", name, e.to_string())
                );
            }
        }
        let fb = if no_fb { None } else { fallback(fb_path, cw, ch) };
        let (cjk, fb) = compose(cw, ch, et, fb);
        out.push(Level { name: name.to_string(), half: Arc::new(half), cjk, fb });
    }
    out
}

/// 讀倚天 15 點字庫:磁碟優先,內嵌是後備。
pub fn eten15(std: Option<&Path>, spc: Option<&Path>, w: i32, h: i32) -> Result<eten::Font, eten::Error> {
    // 沒指定就當成讀不到檔案,與 Go 版拿空字串去讀的結果相同。
    let disk = eten::Font::load(std.unwrap_or(Path::new("")), spc, w, h);
    if disk.is_ok() {
        return disk;
    }
    if let Some(std) = wincv_bundled::get("STDFONT.15") {
        let get = |n: &str| wincv_bundled::get(n).map(<[u8]>::to_vec);
        return eten::Font::load_bytes(std.to_vec(), get("SPCFONT.15"), get("SPCFSUPP.15"), w, h);
    }
    disk
}

/// 讀 24×24 字庫。磁碟上的字型優先;本機完整版才可能帶有內嵌後備,公開產物沒有這些
/// 資產,因此仍會回傳原本的讀檔錯誤。
pub fn eten24(std: Option<&Path>, spc: Option<&Path>) -> Result<eten::Font, eten::Error> {
    let Some(std_path) = std else { return Err(eten::Error(tr!("未指定 24 點漢字區"))) };
    let disk_err = match eten::Font::load24(std_path, spc) {
        Ok(f) => return Ok(f),
        Err(e) => e,
    };
    // 公開版的 bundled 一律是空的,所以這條後備不會把第三方字型帶進發行檔。
    // 完整版以檔名對應可攜的 cjk24 來源代號。
    let base = |p: &Path| p.file_name().map(|n| n.to_string_lossy().into_owned()).unwrap_or_default();
    let Some(std) = wincv_bundled::get(&bundled24_name(&base(std_path))) else { return Err(disk_err) };
    let std = if std.starts_with(eten::ETUNPACK_MAGIC) {
        eten::unpack_etunpack(std).map_err(eten::Error)?
    } else {
        std.to_vec()
    };
    let spc_path = spc.unwrap_or(Path::new(""));
    let spc = wincv_bundled::get(&bundled24_name(&base(spc_path))).map(<[u8]>::to_vec);
    // SPCFSUPP 與 SPCFONT 同目錄、同副檔名。
    let ext = spc_path.extension().map(|e| e.to_string_lossy().into_owned()).unwrap_or_default();
    let supp_name = if ext.is_empty() { "SPCFSUPP".to_string() } else { format!("SPCFSUPP.{ext}") };
    let supp = wincv_bundled::get(&bundled24_name(&supp_name)).map(<[u8]>::to_vec);
    eten::Font::load_bytes(std, spc, supp, eten::NATIVE24_W, eten::NATIVE24_H)
}

/// 內嵌時避開大小寫不分檔案系統上的碰撞:倚天的 SPCFONT.24 與國喬的 spcfont.24
/// 是不同來源,卻只差大小寫(`tools/embed-fonts.sh` 用同一套命名)。
fn bundled24_name(base: &str) -> String {
    match base {
        "stdfont.24f" | "spcfont.24" => format!("guoqiao-{base}"),
        _ => base.to_string(),
    }
}

/// 為半形格挑對應的全形原生尺寸。24×24 存在時只給 cvga1224(12×24)用;
/// 其他字級仍由 16×15 字庫處理。
pub fn eten_for_level<'a>(cw: i32, et15: Option<&'a Arc<eten::Font>>, et24: Option<&'a Arc<eten::Font>>) -> Option<&'a Arc<eten::Font>> {
    match et24 {
        Some(f) if cw * 2 == eten::NATIVE24_W => Some(f),
        _ => et15,
    }
}

/// 用系統字型現場產出三個字級,當作沒有原版 .FON 時的退路。
///
/// 原版的 cvga.fon 是第三方版權物,不能隨產物散布,所以對外的版本解開之後那三個檔
/// 一定不在。「少一個字型檔」與「這個程式跑不起來」是完全不同的嚴重程度。
pub fn from_ttf(p: EtenPaths<'_>, fb_path: Option<&Path>, no_fb: bool) -> Vec<Level> {
    let et15 = eten15(p.std, p.spc, eten::NATIVE_W, eten::NATIVE_H).ok().map(Arc::new);
    let et24 = eten24(p.std24, p.spc24).ok().map(Arc::new);
    let mut out = Vec::new();
    let mut last_errs = Vec::new();
    for (name, w, h) in SIZES {
        // 半形優先用等寬字型,字級縮到塞得進格子。全形用同一份字型但不縮:
        // 那邊格子有兩倍寬。
        let (hf, used, errs) = wincv_ttf::HalfFont::find_mono(w, h);
        let Some(hf) = hf else {
            last_errs = errs;
            continue;
        };
        let (cw, ch) = (w, h + LINE_GAP);
        let mut fb = if no_fb { None } else { fallback(fb_path, cw, ch) };
        if fb.is_none() {
            // 沒有後備鏈時至少用同一份系統字型畫全形。
            if let Some(f) = used.last().and_then(|p| wincv_ttf::Font::load(Path::new(p), w, w * 2, h).ok()) {
                fb = Some(Chain(vec![Arc::new(f)]));
            }
        }
        // 倚天字庫有的話,原生尺寸那一級仍然優先:那才是與原版對齊的字形。
        let et = eten_for_level(cw, et15.as_ref(), et24.as_ref());
        let (cjk, fb) = compose(cw, ch, et, fb);
        out.push(Level { name: wincv_i18n::t("系統字型 ") + name, half: Arc::new(hf), cjk, fb });
    }
    for e in last_errs {
        eprint!("{}", tr!("警告:字型載不起來 %v\n", e));
    }
    out
}

/// 內嵌的後備字型,整個程式只複製一次(Noto CJK 一份就十幾 MB,三個字級共用)。
fn bundled_fallbacks() -> &'static [Arc<[u8]>] {
    static FB: OnceLock<Vec<Arc<[u8]>>> = OnceLock::new();
    FB.get_or_init(|| wincv_bundled::fallbacks().into_iter().map(Arc::from).collect())
}

/// 掛上後備字型,補倚天(Big5 索引)沒有的字:简体字、韓文、希臘文、多數符號。
/// 找不到就沒有,缺字會畫成空框。
pub fn fallback(path: Option<&Path>, cw: i32, ch: i32) -> Option<Chain> {
    if let Some(p) = path {
        return match wincv_ttf::Font::load(p, cw, cw * 2, ch) {
            Ok(f) => Some(Chain(vec![Arc::new(f)])),
            Err(e) => {
                eprint!("{}", tr!("警告:載不到後備字型 %s (%v)\n", p.display().to_string(), e));
                None
            }
        };
    }
    let (mut chain, _, errs) = wincv_ttf::load_chain(cw, cw * 2, ch);
    // 內嵌的後備接在系統字型後面,不是取代它:系統上裝的字型通常比較新、比較齊,
    // 內嵌的那份是「這台機器什麼都沒裝」時的保險。
    for b in bundled_fallbacks() {
        if let Ok(f) = wincv_ttf::Font::load_bytes(b.clone(), cw, cw * 2, ch) {
            chain.0.push(Arc::new(f));
        }
    }
    if !chain.0.is_empty() {
        // 個別字型載不起來在有其他字型頂上時只是雜訊。一條都沒載到才是使用者需要
        // 知道的事。
        return Some(chain);
    }
    for e in errs {
        eprint!("{}", tr!("警告:字型載不起來 %v\n", e));
    }
    eprintln!("{}{}", wincv_i18n::t("警告:"), wincv_ttf::missing_hint());
    None
}

#[cfg(test)]
mod tests;
