//! 放大倍率與視窗大小(對應 Go 版 `viewsize.go`)。

use wincv_i18n::{t, tr};

use crate::menu::MenuItem;
use crate::App;

/// 放大倍率的範圍與階距。
///
/// 上限 4 是實用的界線:8×16 的格子放到 4 倍是 32×64 px,在 4K 螢幕上一列還放得下 60 格。
///
/// [取捨] 階距 0.1 換來的是「調得準」,代價是**點陣字不再等倍複製**:1.1 倍時有些原始
/// 像素佔 1 個螢幕像素、有些佔 2 個,筆畫粗細會不勻。要與原版逐像素對齊請用整數倍。
pub const MIN_SCALE: f64 = 1.0;
pub const MAX_SCALE: f64 = 4.0;
pub const SCALE_STEP: f64 = 0.1;

/// 視窗大小的預設檔位。原版的 792×506 client area 換算成格子是 93×21。第一個檔位是
/// 93×22:選單列自己吃掉一列,多給一列之後內容區才是原版的 21 列。
///
/// 函式而不是常數:名稱要翻譯。
fn size_presets() -> [(String, i32, i32); 4] {
    [
        (t("原版版面 93×21(+選單列)"), 93, 22),
        ("80×25".into(), 80, 25),
        ("100×30".into(), 100, 30),
        ("120×40".into(), 120, 40),
    ]
}

/// 「視窗大小」子選單的內容。
pub(crate) fn size_menu_items(_: &App) -> Vec<MenuItem> {
    let mut out: Vec<MenuItem> = size_presets()
        .into_iter()
        .map(|(name, c, r)| MenuItem { label: name, run: Some(std::rc::Rc::new(move |a: &mut App| a.request_size(c, r))), ..Default::default() })
        .collect();
    let item = |label: String, f: fn(&mut App) -> bool| MenuItem { label, run: Some(std::rc::Rc::new(f)), ..Default::default() };
    out.push(MenuItem { sep: true, ..Default::default() });
    out.push(item(t("放大倍率 +"), |a| a.set_scale(a.scale + SCALE_STEP)));
    out.push(item(t("放大倍率 -"), |a| a.set_scale(a.scale - SCALE_STEP)));
    out.push(item(t("自訂欄列數…"), App::start_custom_size));
    out
}

impl App {
    /// 換放大倍率。回傳是否真的變了。
    ///
    /// 與字級分開:字級換的是**點陣字本身**(8×15 / 10×18 / 12×24),放大倍率是把同一份
    /// 字模拉大。只有三種點陣字,單靠字級在 4K 螢幕上還是太小。
    pub(crate) fn set_scale(&mut self, n: f64) -> bool {
        // 先量化到 0.1:0.1 在二進位裡除不盡,一路累加下去會走到 1.2000000000000002,
        // 顯示成「1.2×」卻與 1.2 不相等,「已經是這個倍率了」的判斷永遠不成立。
        // Go 的 math.Round 是遠離零捨入,與 f64::round 相同。
        let n = ((n * 10.0).round() / 10.0).clamp(MIN_SCALE, MAX_SCALE);
        if n == self.scale {
            return false;
        }
        self.scale = n;
        self.message = tr!("放大倍率 %.1f×", n);
        true
    }

    /// 請視窗層把視窗調成 cols×rows 格。app 這一層算不出視窗要幾個像素(那取決於字級
    /// 與倍率),所以只留下請求,由視窗層每幀檢查後執行。
    pub(crate) fn request_size(&mut self, cols: i32, rows: i32) -> bool {
        self.want_cols = cols;
        self.want_rows = rows;
        self.message = tr!("視窗調成 %d×%d 格", cols, rows);
        true
    }

    /// 問使用者要幾欄幾列。
    fn start_custom_size(&mut self) -> bool {
        let init = format!("{}×{}", self.screen_cols, self.screen_rows);
        self.ask(&t("欄×列"), &init, |a, s| {
            // 逗號、x、×、空白都收:使用者不會記得該用哪一個。
            let norm: String = s.chars().map(|c| if "xX×, *".contains(c) { ' ' } else { c }).collect();
            let Some((c, r)) = scan_two(&norm) else {
                a.message = t("看不懂,格式是「欄×列」,例如 100×30");
                return;
            };
            if !(20..=500).contains(&c) || !(5..=200).contains(&r) {
                a.message = t("超出範圍(欄 20-500、列 5-200)");
                return;
            }
            a.request_size(c, r);
        });
        true
    }
}

/// Go 的 `fmt.Sscanf(s, "%d %d", &c, &r)`:兩個以空白分隔的整數,後面多出來的不管。
fn scan_two(s: &str) -> Option<(i32, i32)> {
    let mut it = s.split_whitespace();
    let c = it.next()?.parse().ok()?;
    let r = it.next()?.parse().ok()?;
    Some((c, r))
}
