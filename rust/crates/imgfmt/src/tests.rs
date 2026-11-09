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
