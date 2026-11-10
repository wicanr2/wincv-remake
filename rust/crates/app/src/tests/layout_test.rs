//! 對應 Go 版 `internal/app/layout_test.go`。
//!
//! Go 版的參數是 int(64 位元),Rust 版是 i32。Go 測試的壞值是 `int(math.Inf(1))`,
//! 在 amd64 上得到 INT64_MIN;Rust 的浮點轉整數是飽和的,`f64::INFINITY as i32` 得到
//! i32::MAX。兩種壞值都測。

use crate::layout::{sane_layout, FALLBACK_H, FALLBACK_W};

const BAD_VALUES: [i32; 2] = [i32::MIN, f64::INFINITY as i32];

// Android 上真的發生過的那組值:EbitenView 用 deviceScale=0 去除,
// 得到 +Inf,Ebiten 的 Layout 介面收 int,轉出來是 INT64_MIN。
#[test]
fn sane_layout_rejects_inf_converted_to_int() {
    for bad in BAD_VALUES {
        let (w, h) = sane_layout(bad, bad, 0, 0, 0, 0);
        assert!(w > 0 && h > 0, "回了非正數 {w}×{h}");
        assert_eq!((w, h), (FALLBACK_W, FALLBACK_H), "沒有上一次的好值也沒有螢幕尺寸時應該用保底值");
    }
}

#[test]
fn sane_layout_cases() {
    for bad in BAD_VALUES {
        let cases: [(&str, [i32; 6], (i32, i32)); 7] = [
            ("正常值直接用", [1080, 2340, 0, 0, 0, 0], (1080, 2340)),
            ("壞值退回上一次的好值", [bad, bad, 393, 851, 0, 0], (393, 851)),
            ("沒有上一次就用螢幕尺寸", [bad, bad, 0, 0, 393, 851], (393, 851)),
            ("零也是壞值", [0, 0, 393, 851, 0, 0], (393, 851)),
            ("負值是壞值", [-5, -5, 393, 851, 0, 0], (393, 851)),
            ("只有一邊壞掉也整組換掉", [1080, 0, 393, 851, 0, 0], (393, 851)),
            ("大得不合理也是壞值", [1 << 30, 1 << 30, 393, 851, 0, 0], (393, 851)),
        ];
        for (name, [w, h, lw, lh, mw, mh], want) in cases {
            assert_eq!(sane_layout(w, h, lw, lh, mw, mh), want, "{name}(bad={bad})");
        }
    }
}
