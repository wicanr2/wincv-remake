use super::*;

fn blank(w: i32, h: i32) -> Arc<eten::Font> {
    let stride = ((w + 7) / 8 * h) as usize;
    Arc::new(eten::Font::load_bytes(vec![0; stride], None, None, w, h).unwrap())
}

#[test]
fn eten_for_level_prefers_native24() {
    let (et15, et24) = (blank(eten::NATIVE_W, eten::NATIVE_H), blank(eten::NATIVE24_W, eten::NATIVE24_H));
    assert!(Arc::ptr_eq(eten_for_level(12, Some(&et15), Some(&et24)).unwrap(), &et24), "12×24 字級沒有挑選原生 24×24 字庫");
    let (cjk, _) = compose(12, 24 + LINE_GAP, Some(&et24), None);
    let g = cjk.unwrap().glyph('一').expect("24 點字模取不到「一」");
    assert_eq!((g.w, g.h), (24, 24 + LINE_GAP), "24 點字模沒有以原生尺寸交給渲染器");
    assert!(Arc::ptr_eq(eten_for_level(8, Some(&et15), Some(&et24)).unwrap(), &et15), "8×15 字級不應誤用 24×24 字庫");
}

/// 每一個字級都要有全形字。
///
/// 倚天只有 16×15 那一組字模,而半形有三個字級,所以載入尺寸(字庫自己的)與顯示
/// 尺寸(格子的)不是同一件事。把後者傳給字庫的話,除了 8×15 那一級剛好相等以外,
/// 其餘字級的全形字會整批變成缺字方塊。
#[test]
fn levels_have_cjk() {
    let root = Path::new(env!("CARGO_MANIFEST_DIR")).join("../../../original");
    let (app, et) = (root.join("app"), root.join("eten"));
    let std = et.join("STDFONT.15");
    if !std.exists() || !app.join("cvga.fon").exists() {
        eprintln!("沒有倚天字庫或原版字型,跳過");
        return;
    }
    let spc = et.join("SPCFONT.15");
    let levels = load(&app, EtenPaths { std: Some(&std), spc: Some(&spc), ..Default::default() }, None, true);
    assert_eq!(levels.len(), SIZES.len());
    for (l, (_, w, h)) in levels.iter().zip(SIZES) {
        let g = l.cjk.as_ref().unwrap_or_else(|| panic!("{} 這一級沒有全形字", l.name)).glyph('一');
        let g = g.unwrap_or_else(|| panic!("{} 這一級取不到「一」", l.name));
        assert_eq!((g.w, g.h), (w * 2, h + LINE_GAP), "{} 的全形字模尺寸", l.name);
    }
}

/// 非原生尺寸、有後備鏈時:向量字型在前(反鋸齒),倚天縮放在後。
#[test]
fn compose_prefers_vector_off_native() {
    let et15 = blank(eten::NATIVE_W, eten::NATIVE_H);
    // 原生尺寸:倚天,後備原樣轉交。
    let (cjk, fb) = compose(8, 15 + LINE_GAP, Some(&et15), Some(Chain::default()));
    assert!(cjk.is_some() && fb.is_some());
    // 沒有後備:只有倚天縮放。
    let (cjk, fb) = compose(10, 18 + LINE_GAP, Some(&et15), None);
    assert_eq!(cjk.unwrap().glyph('一').map(|g| (g.w, g.h)), Some((20, 19)));
    assert!(fb.is_none());
}

#[test]
fn bundled24_names_avoid_case_collision() {
    assert_eq!(bundled24_name("spcfont.24"), "guoqiao-spcfont.24");
    assert_eq!(bundled24_name("SPCFONT.24"), "SPCFONT.24");
}
