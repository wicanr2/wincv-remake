//! 照搬 Go 版 `internal/imgfmt` 的測試,另加格式表與 Go 版的逐列比對。

use super::*;
use std::path::Path;

fn repo() -> std::path::PathBuf {
    Path::new(env!("CARGO_MANIFEST_DIR")).join("../../..")
}

/// PBM 的 1 是黑、0 是白:反了整張圖就反相。
#[test]
fn pnm_ascii_pbm_polarity() {
    let img = pnm::decode("P1\n# 註解\n2 2\n1 0\n0 1\n".as_bytes()).unwrap();
    assert_eq!(img.at(0, 0)[0], 0);
    assert_eq!(img.at(1, 0)[0], 255);
}

#[test]
fn pnm_binary_ppm() {
    let mut src = b"P6\n2 1\n255\n".to_vec();
    src.extend([255, 0, 0, 0, 255, 0]);
    let img = pnm::decode(&src).unwrap();
    assert_eq!(img.at(0, 0), [255, 0, 0, 255]);
    assert_eq!(img.at(1, 0), [0, 255, 0, 255]);
}

#[test]
fn pnm_scales_maxval() {
    let mut src = b"P5\n1 1\n15\n".to_vec();
    src.push(15);
    assert_eq!(pnm::decode(&src).unwrap().at(0, 0)[0], 255);
}

/// TGA 預設原點在左下,忘了翻轉會整張上下顛倒。
#[test]
fn tga_bottom_up_default() {
    let mut hdr = vec![0u8; 18];
    hdr[2] = 2;
    hdr[12] = 2;
    hdr[14] = 2;
    hdr[16] = 24;
    let body = [0, 0, 255, 0, 0, 255, 255, 0, 0, 255, 0, 0]; // 檔案第 1 列紅紅、第 2 列藍藍(BGR)
    let img = tga::decode(&[hdr.clone(), body.to_vec()].concat()).unwrap();
    assert_eq!(img.at(0, 0)[2], 255, "畫面第一列應為藍");
    assert_eq!(img.at(0, 1)[0], 255, "畫面第二列應為紅");
    hdr[17] = 0x20; // top-down 就不翻轉
    assert_eq!(tga::decode(&[hdr, body.to_vec()].concat()).unwrap().at(0, 0)[0], 255);
}

#[test]
fn tga_rle() {
    let mut hdr = vec![0u8; 18];
    hdr[2] = 10;
    hdr[12] = 4;
    hdr[14] = 1;
    hdr[16] = 24;
    hdr[17] = 0x20;
    hdr.extend([0x83, 255, 0, 0]); // 重複 4 次的藍色
    let img = tga::decode(&hdr).unwrap();
    assert!((0..4).all(|x| img.at(x, 0) == [0, 0, 255, 255]));
}

/// PCX 的 256 色調色盤在檔尾,前面有一個 0x0C 標記。
#[test]
fn pcx_8bpp_with_tail_palette() {
    let mut d = vec![0u8; 128];
    d[0] = 0x0A;
    d[2] = 1;
    d[3] = 8;
    d[8] = 3;
    d[10] = 1;
    d[0x41] = 1;
    d[0x42] = 4;
    d.extend([0xC4, 1, 0xC4, 1]);
    let mut pal = vec![0u8; 769];
    pal[0] = 0x0C;
    pal[4..7].copy_from_slice(&[10, 20, 30]);
    d.extend(pal);
    let img = pcx::decode(&d).unwrap();
    assert_eq!((img.w, img.h), (4, 2));
    assert_eq!(img.at(0, 0), [10, 20, 30, 255]);
}

/// ICO 目錄項的 0 表示 256,照字面讀會挑錯張。
#[test]
fn ico_picks_largest() {
    let mk = |n: u32, c: [u8; 4]| {
        let im = image::RgbaImage::from_pixel(n, n, image::Rgba(c));
        let mut buf = std::io::Cursor::new(Vec::new());
        im.write_to(&mut buf, image::ImageFormat::Png).unwrap();
        buf.into_inner()
    };
    let (small, big) = (mk(16, [255, 0, 0, 255]), mk(64, [0, 255, 0, 255]));
    let mut out = vec![0, 0, 1, 0, 2, 0];
    let dir = 6 + 2 * 16;
    for (w, data, off) in [(16u8, &small, dir), (0, &big, dir + small.len())] {
        let mut e = vec![0u8; 16];
        e[0] = w;
        e[1] = w;
        e[8..12].copy_from_slice(&(data.len() as u32).to_le_bytes());
        e[12..16].copy_from_slice(&(off as u32).to_le_bytes());
        out.extend(e);
    }
    out.extend(&small);
    out.extend(&big);
    assert_eq!(ico::decode(&out).unwrap().at(0, 0)[1], 255, "應該挑標成 0(=256)的那張");
}

/// 用原版自己的 .ico 跑一遍。
#[test]
fn real_ico_files() {
    for n in ["WinCV.ico", "wincv1.ico", "wincvbusy.ico"] {
        let p = repo().join("original/app").join(n);
        let Ok(d) = std::fs::read(&p) else { return };
        let (img, kind) = decode(n, &d).unwrap();
        assert_eq!(kind, "ICO");
        assert!(img.w > 0);
    }
}

/// 照規格自己組一個 .koa,四種顏色來源各用一次。
fn build_koa(bg: u8, hi: u8, lo: u8, col: u8) -> Vec<u8> {
    let mut d = vec![0u8; 10003];
    d[1] = 0x60;
    d[2] = 1 << 4 | 2 << 2 | 3;
    d[2 + 8000] = hi << 4 | lo;
    d[2 + 9000] = col;
    d[2 + 10000] = bg;
    d
}

#[test]
fn koa_color_sources() {
    let img = koa::decode(&build_koa(6, 1, 2, 5)).unwrap();
    assert_eq!((img.w, img.h), (320, 200));
    for (x, rgb) in [(0, [0x35, 0x28, 0x79]), (2, [0xFF, 0xFF, 0xFF]), (4, [0x68, 0x37, 0x2B]), (6, [0x58, 0x8D, 0x43])] {
        assert_eq!(img.at(x, 0)[..3], rgb, "x={x}");
        assert_eq!(img.at(x + 1, 0), img.at(x, 0), "x={x} 沒有加倍寬度");
    }
}

#[test]
fn koa_rejects_wrong_length() {
    assert!(koa::decode(&[0; 1234]).is_err());
    assert!(koa::decode(&build_koa(0, 1, 2, 3)[2..]).is_ok(), "10001 位元組的變體也要收");
}

/// 格式表的副檔名與名稱要與 Go 版的 `imgfmt.Formats` 逐列相同。
#[test]
fn formats_match_go() {
    let src = std::fs::read_to_string(repo().join("internal/imgfmt/imgfmt.go")).unwrap();
    let mut go = Vec::new();
    for line in src.lines() {
        let Some(rest) = line.trim().strip_prefix("{[]string{") else { continue };
        let (exts, rest) = rest.split_once('}').unwrap();
        let exts: Vec<String> = exts.split(',').map(|e| e.trim().trim_matches('"').to_string()).collect();
        go.push((exts, rest.trim_start_matches(',').trim().split('"').nth(1).unwrap().to_string()));
    }
    let rust: Vec<_> = FORMATS.iter().map(|f| (f.ext.iter().map(|e| e.to_string()).collect::<Vec<_>>(), f.name.to_string())).collect();
    assert_eq!(rust, go);
}

/// 副檔名是 .ico、內容其實是 PNG:依 magic number 解得開。
#[test]
fn misnamed_png_falls_back_to_magic() {
    let im = image::RgbaImage::from_pixel(3, 2, image::Rgba([1, 2, 3, 255]));
    let mut buf = std::io::Cursor::new(Vec::new());
    im.write_to(&mut buf, image::ImageFormat::Png).unwrap();
    let (img, kind) = decode("x.ico", buf.get_ref()).unwrap();
    assert_eq!((img.w, img.h, kind), (3, 2, "PNG"));
    assert!(decode("x.ico", b"not an image").unwrap_err().contains("ICO"));
}

fn svg(src: &str) -> Image {
    let (img, kind) = decode("x.svg", src.as_bytes()).unwrap_or_else(|e| panic!("解不開:{e}"));
    assert_eq!(kind, "SVG");
    img
}

/// 一塊區域裡有多少像素不是背景色(左上角那一點)。
fn ink(img: &Image, x0: u32, y0: u32, x1: u32, y1: u32) -> usize {
    let bg = img.at(0, 0);
    (y0..y1).flat_map(|y| (x0..x1).map(move |x| (x, y))).filter(|&(x, y)| img.at(x, y) != bg).count()
}

/// 尺寸要從 width/height 來,而且顏色要對:全白的圖也會通過只檢查尺寸的測試。
#[test]
fn svg_size_and_colors() {
    let d = std::fs::read(repo().join("internal/imgfmt/testdata/tri.svg")).unwrap();
    let (img, kind) = decode("tri.svg", &d).unwrap();
    assert_eq!((img.w, img.h, kind), (40, 20, "SVG"));
    assert_eq!(img.at(5, 10), [0xff, 0, 0, 0xff], "左半紅");
    assert_eq!(img.at(30, 10), [0xff, 0xff, 0xff, 0xff], "右半白");
}

/// 壞掉的 SVG 要回錯誤,不能回一張空白圖:空白圖看起來像「這張圖就是白的」。
#[test]
fn svg_broken_is_error() {
    assert!(decode("x.svg", b"<svg><this is not xml").is_err());
}

/// viewBox 很大的檔案不能配置到爆。
#[test]
fn svg_clamps_huge_viewbox() {
    let img = svg(r#"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100000 100000"></svg>"#);
    assert!(img.w as f32 <= SVG_MAX_SIDE && img.h as f32 <= SVG_MAX_SIDE, "{}x{}", img.w, img.h);
}

/// 沒有 width、height、viewBox 時,尺寸取原點到內容右下角的範圍(usvg 的做法),
/// 連內容都沒有時是 100×100。Go 版一律是 256×256。
#[test]
fn svg_size_without_dimensions() {
    let img = svg(r#"<svg xmlns="http://www.w3.org/2000/svg"><rect x="2" y="3" width="10" height="10"/></svg>"#);
    assert_eq!((img.w, img.h), (12, 13));
    let img = svg(r#"<svg xmlns="http://www.w3.org/2000/svg"></svg>"#);
    assert_eq!((img.w, img.h), (100, 100));
}

/// 少了 xmlns 的檔案也要解得開(手寫的 SVG 常漏掉)。
#[test]
fn svg_without_namespace() {
    let img = svg(r#"<svg width="20" height="10"><rect width="20" height="10"/></svg>"#);
    assert_eq!((img.w, img.h, img.at(5, 5)), (20, 10, [0, 0, 0, 255]));
}

fn have_fonts() -> bool {
    !wincv_ttf::candidates().is_empty() || !wincv_bundled::fallbacks().is_empty()
}

/// <text> 是圖表的意義所在:少了它剩下一堆無從解讀的長條,而畫面看起來是完整的。
#[test]
fn svg_draws_text() {
    if !have_fonts() {
        eprintln!("這台機器沒有可用的字型,跳過");
        return;
    }
    let img = svg(r##"<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60" viewBox="0 0 200 60">
      <rect width="200" height="60" fill="#ffffff"/>
      <text x="10" y="40" font-size="24" fill="#000000">Hi 中文</text>
    </svg>"##);
    assert!(ink(&img, 0, 0, 100, 60) > 0, "完全沒有畫出文字");
    assert_eq!(ink(&img, 150, 0, 200, 60), 0, "右邊不該有東西");
}

/// text-anchor 決定文字往哪一邊長。算錯的話標籤整排偏到另一側,而每個標籤自己看起來都很正常。
#[test]
fn svg_text_anchor() {
    if !have_fonts() {
        return;
    }
    let tmpl = |a: &str| {
        svg(&format!(
            r##"<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60" viewBox="0 0 200 60">
          <rect width="200" height="60" fill="#ffffff"/>
          <text x="100" y="40" font-size="24" fill="#000000" text-anchor="{a}">Hi</text></svg>"##
        ))
    };
    let (start, end) = (tmpl("start"), tmpl("end"));
    assert_eq!(ink(&start, 0, 0, 99, 60), 0, "start 不該畫到 x=100 的左邊");
    assert!(ink(&start, 100, 0, 200, 60) > 0, "start 應該畫在 x=100 的右邊");
    assert_eq!(ink(&end, 101, 0, 200, 60), 0, "end 不該畫到 x=100 的右邊");
    assert!(ink(&end, 0, 0, 100, 60) > 0, "end 應該畫在 x=100 的左邊");
}

/// 文字要跟著 viewBox 一起縮放。
#[test]
fn svg_text_scales_with_viewbox() {
    if !have_fonts() {
        return;
    }
    let img = svg(r##"<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 8000 4000">
      <rect width="8000" height="4000" fill="#ffffff"/>
      <text x="400" y="2000" font-size="800" fill="#000000">Hi</text></svg>"##);
    assert!(img.w as f32 <= SVG_MAX_SIDE && img.h as f32 <= SVG_MAX_SIDE);
    assert!(ink(&img, 0, 0, img.w / 2, img.h) > 0, "縮放後文字不見了");
    assert_eq!(ink(&img, img.w / 2, img.h * 3 / 4, img.w, img.h), 0, "右下角不該有東西");
}

/// fill="none" 的文字看不見(常見於只拿來量尺寸的輔助元素)。
#[test]
fn svg_text_fill_none() {
    if !have_fonts() {
        return;
    }
    let img = svg(r##"<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60">
      <rect width="200" height="60" fill="#ffffff"/>
      <text x="10" y="40" font-size="24" fill="none">Hi</text></svg>"##);
    assert_eq!(ink(&img, 0, 0, img.w, img.h), 0);
}

/// Arial 這類 Windows 字型名稱在 Linux 上沒有,要換成對應的泛用字族,不能整段字不畫。
#[test]
fn svg_windows_font_names_still_draw() {
    if !have_fonts() {
        return;
    }
    let img = svg(r##"<svg xmlns="http://www.w3.org/2000/svg" width="200" height="60">
      <rect width="200" height="60" fill="#ffffff"/>
      <text x="10" y="40" font-size="24" font-family="Arial" fill="#000000">Hi</text></svg>"##);
    assert!(ink(&img, 0, 0, 200, 60) > 0);
}
