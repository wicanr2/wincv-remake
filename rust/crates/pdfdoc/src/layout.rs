//! 把散落的字組回一列一列,必要時先分欄。逐行照 Go 版 `internal/pdfdoc/layout.go`。

use std::cmp::Ordering::Equal;

use crate::{Line, Text};

/// 多寬的間距算一個空格,以字級為單位。
///
/// 取得比實際的空格窄(一個空格大約是字級的四分之一)是刻意的:
/// 多一個空格只是版面鬆一點,少一個空格會讓兩個詞黏成一個看不懂的字串。
pub const SPACE_RATIO: f64 = 0.2;

/// 把散落的字組回一列一列,必要時先分欄。`x0`、`x1` 是頁面的左右邊界。
pub fn layout(ts: &[Text], x0: f64, x1: f64) -> Vec<Line> {
    if ts.is_empty() {
        return Vec::new();
    }
    let mut out = Vec::new();
    for col in columns(ts, x0, x1) {
        out.extend(lines_of(&col));
    }
    out
}

struct Row<'a> {
    y: f64,
    items: Vec<&'a Text>,
}

/// 依縱座標分成列。容差用字級的一部分:固定值在放大過的頁面上會把兩列
/// 併成一列,在小字的頁面上會把一列拆成兩列。
fn group_rows<'a>(ts: &[&'a Text]) -> Vec<Row<'a>> {
    let mut rows: Vec<Row<'a>> = Vec::new();
    for &t in ts {
        let tol = (t.size * 0.4).max(1.0);
        match rows.iter_mut().find(|r| (r.y - t.y).abs() <= tol) {
            Some(r) => r.items.push(t),
            None => rows.push(Row { y: t.y, items: vec![t] }),
        }
    }
    // 由上往下:PDF 的 Y 軸由下往上長。座標在收字時已經濾掉 NaN;
    // -0 與 +0 視為相等,與 Go 的 `<` 一致。
    rows.sort_by(|a, b| b.y.partial_cmp(&a.y).unwrap_or(Equal));
    for r in &mut rows {
        r.items.sort_by(|a, b| a.x.partial_cmp(&b.x).unwrap_or(Equal));
    }
    rows
}

/// 把一欄的字排成一列一列的文字。
fn lines_of(ts: &[&Text]) -> Vec<Line> {
    let rows = group_rows(ts);
    let mut left = 1e18f64;
    for r in &rows {
        if let Some(first) = r.items.first() {
            if first.x < left {
                left = first.x;
            }
        }
    }
    let mut out = Vec::new();
    for r in &rows {
        let mut sb = String::new();
        let (mut prev_end, mut size) = (0f64, 0f64);
        let mut have_prev = false;
        for t in &r.items {
            if t.size > size {
                size = t.size;
            }
            // 上一個字沒有寬度資訊時不判斷間距,不然每個字之間都會被塞一個空格。
            // [雷] PDF 裡沒有空格字元,字與字之間的空白是靠座標間距表示的。
            if have_prev && t.x - prev_end > t.size * SPACE_RATIO {
                sb.push(' ');
            }
            sb.push_str(&t.s);
            prev_end = t.x + t.w;
            have_prev = t.w > 0.0;
        }
        let text = sb.trim_end_matches([' ', '\t']).to_string();
        if text.trim().is_empty() {
            continue;
        }
        let mut indent = 0;
        if size > 0.0 && !r.items.is_empty() && left < 1e17 {
            let x = r.items[0].x - left;
            if x > 0.0 {
                indent = (x / (size * 0.5)) as i32;
            }
        }
        out.push(Line { y: r.y, indent, text });
    }
    out
}

/// 一欄至少要有幾列才算數。
const MIN_COL_ROWS: usize = 8;
/// 整頁至少要有幾列才考慮分欄。
const MIN_PAGE_ROWS: usize = 15;
/// 「這一列算填滿」的門檻(佔欄寬的比例)。
const FULL_LINE_RATIO: f64 = 0.6;
/// 一欄裡至少要有多少比例的列是填滿的。
const MIN_FULL_RATIO: f64 = 0.6;
/// 最多分成幾欄。
const MAX_COLUMNS: usize = 3;

/// 把一頁的字分成欄。
///
/// 判準是「一條從頁首通到頁尾、完全沒有字跨過的縱向空白帶」,它同時也是
/// 表格的樣子,所以另加三道條件:整頁夠多列、每一欄夠多列、每一欄大部分
/// 的列接近欄寬。寧可漏判:漏判只是把兩欄併成一列讀,還看得懂。
fn columns(ts: &[Text], x0: f64, x1: f64) -> Vec<Vec<&Text>> {
    let all: Vec<&Text> = ts.iter().collect();
    let rows = group_rows(&all);
    if rows.len() < MIN_PAGE_ROWS || x1 <= x0 {
        return vec![all];
    }
    let med = median_size(ts);
    if med <= 0.0 {
        return vec![all];
    }
    // 欄間距通常是一個半到兩個字寬。擋掉表格靠的是「有沒有填滿」那道檢查。
    let gap_min = (med * 1.5).max(10.0);

    // 佔用圖:每一點寬一格,有字經過就標起來。
    let n = (x1 - x0) as i64 + 2;
    if !(16..=20000).contains(&n) {
        return vec![all];
    }
    let n = n as usize;
    let mut occ = vec![false; n];
    for t in ts {
        let mut a = (t.x - x0) as i64;
        let mut b = (t.x + t.w - x0) as i64;
        if b < a {
            std::mem::swap(&mut a, &mut b);
        }
        for i in a.max(0)..=b.min(n as i64 - 1) {
            occ[i as usize] = true;
        }
    }
    // 找出兩側都有字的空白帶。
    let mut cuts: Vec<f64> = Vec::new();
    let mut i = 0;
    while i < n && !occ[i] {
        i += 1;
    }
    let last = i;
    while i < n {
        if occ[i] {
            i += 1;
            continue;
        }
        let start = i;
        while i < n && !occ[i] {
            i += 1;
        }
        if i >= n {
            break; // 右邊界的空白不是欄間距
        }
        if (i - start) as f64 >= gap_min && start > last {
            cuts.push(x0 + (start + i) as f64 / 2.0);
        }
    }
    if cuts.is_empty() || cuts.len() >= MAX_COLUMNS {
        return vec![all];
    }

    let mut groups: Vec<Vec<&Text>> = vec![Vec::new(); cuts.len() + 1];
    for t in ts {
        // 與 Go 的 sort.SearchFloat64s 相同:第一個 >= x 的切點。
        let g = cuts.partition_point(|&c| c < t.x);
        groups[g].push(t);
    }
    // 每一欄可以用到多寬,由切點與整頁文字的左右邊界決定。拿「這一欄實際最寬
    // 的一列」當分母不行:表格的儲存格每一列一樣寬,除下來永遠是 100%。
    let (min_x, max_x) = text_bounds(ts);
    let mut bounds = vec![min_x];
    bounds.extend(&cuts);
    bounds.push(max_x);
    for (i, g) in groups.iter().enumerate() {
        if !looks_like_column(g, bounds[i + 1] - bounds[i]) {
            return vec![all];
        }
    }
    groups
}

/// 整頁文字的左右邊界。
fn text_bounds(ts: &[Text]) -> (f64, f64) {
    let (mut lo, mut hi) = (1e18f64, -1e18f64);
    for t in ts {
        if t.x < lo {
            lo = t.x;
        }
        if t.x + t.w > hi {
            hi = t.x + t.w;
        }
    }
    (lo, hi)
}

/// 一群字看起來像不像一欄內文:大部分的列有沒有把這一欄填滿。
fn looks_like_column(ts: &[&Text], region_w: f64) -> bool {
    let rows = group_rows(ts);
    if rows.len() < MIN_COL_ROWS || region_w <= 0.0 {
        return false;
    }
    let full = rows.iter().filter(|r| row_width(r) >= region_w * FULL_LINE_RATIO).count();
    full as f64 / rows.len() as f64 >= MIN_FULL_RATIO
}

fn row_width(r: &Row<'_>) -> f64 {
    match (r.items.first(), r.items.last()) {
        (Some(first), Some(last)) => last.x + last.w - first.x,
        _ => 0.0,
    }
}

fn median_size(ts: &[Text]) -> f64 {
    let mut sizes: Vec<f64> = ts.iter().map(|t| t.size).filter(|&s| s > 0.0).collect();
    if sizes.is_empty() {
        return 0.0;
    }
    sizes.sort_by(f64::total_cmp);
    sizes[sizes.len() / 2]
}

#[cfg(test)]
mod tests {
    use super::*;

    /// 依「每一列一個字串」造出字。x 是列的起點,每個字寬 size/2。
    fn texts(size: f64, rows: &[(f64, f64, &str)]) -> Vec<Text> {
        let mut out = Vec::new();
        for &(x0, y, s) in rows {
            let mut x = x0;
            for ch in s.chars() {
                let w = size / 2.0;
                out.push(Text { x, y, w, size, s: ch.to_string() });
                x += w;
            }
        }
        out
    }

    fn t(x: f64, y: f64, w: f64, size: f64, s: &str) -> Text {
        Text { x, y, w, size, s: s.to_string() }
    }

    // 雙欄內文要一欄讀完再讀下一欄。
    #[test]
    fn two_columns_read_down() {
        let mut rows = Vec::new();
        for i in 0..20 {
            let y = 700.0 - i as f64 * 14.0;
            rows.push((72.0, y, "LEFTLEFTLEFTLEFTLEFTLEFT"));
            rows.push((250.0, y, "RIGHTRIGHTRIGHTRIGHTRIGH"));
        }
        let lines = layout(&texts(10.0, &rows), 0.0, 595.0);
        assert_eq!(lines.len(), 40, "要 40 列");
        for (i, l) in lines.iter().enumerate() {
            let want = if i >= 20 { "RIGHT" } else { "LEFT" };
            assert!(l.text.starts_with(want), "第 {i} 列是 {:?},應該屬於 {want} 欄", l.text);
        }
    }

    // 表格也有一條通到底的縱向空白帶,但它要橫著讀。
    #[test]
    fn table_is_not_split_into_columns() {
        let mut rows = Vec::new();
        for i in 0..20 {
            let y = 700.0 - i as f64 * 14.0;
            rows.push((72.0, y, "cell"));
            rows.push((330.0, y, "cell"));
        }
        let lines = layout(&texts(10.0, &rows), 0.0, 595.0);
        assert_eq!(lines.len(), 20, "表格應該一列一列讀");
        assert_eq!(lines[0].text.matches("cell").count(), 2, "同一列的兩格要在同一列:{:?}", lines[0].text);
    }

    // 列數太少時不分欄:證據不足。
    #[test]
    fn short_page_is_not_split() {
        let mut rows = Vec::new();
        for i in 0..4 {
            let y = 700.0 - i as f64 * 14.0;
            rows.push((72.0, y, "LEFTLEFTLEFTLEFTLEFTLEFT"));
            rows.push((250.0, y, "RIGHTRIGHTRIGHTRIGHTRIGH"));
        }
        assert_eq!(layout(&texts(10.0, &rows), 0.0, 595.0).len(), 4);
    }

    // 座標之間的間距要還原成空格。
    #[test]
    fn space_from_gap() {
        let ts = [t(72.0, 700.0, 6.0, 12.0, "a"), t(78.0, 700.0, 6.0, 12.0, "b"), t(100.0, 700.0, 6.0, 12.0, "c")];
        let lines = layout(&ts, 0.0, 595.0);
        assert_eq!(lines.len(), 1);
        assert_eq!(lines[0].text, "ab c");
    }

    // 寬度是 0 的字型不能拿來算間距:每個字後面都會多一個空格。
    #[test]
    fn zero_width_does_not_insert_spaces() {
        let ts: Vec<Text> =
            "hello".chars().enumerate().map(|(i, c)| t(72.0 + i as f64 * 6.0, 700.0, 0.0, 12.0, &c.to_string())).collect();
        let lines = layout(&ts, 0.0, 595.0);
        assert_eq!(lines.len(), 1);
        assert_eq!(lines[0].text, "hello");
    }
}
