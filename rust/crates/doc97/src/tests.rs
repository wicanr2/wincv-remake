//! 對應 Go 版 `doc97_test.go`,逐條照搬。
//!
//! testdata 的兩份 .doc 是 LibreOffice 產生的真實 Word 97 檔案,
//! 重建方式見 internal/doc97/testdata/README.md。用真檔而不是自己組的最小檔案
//! 是必要的:FIB、piece table、屬性頁三者互相指位置,自己組的檔案只會驗到
//! 自己對格式的理解,驗不到「真的 Word 檔長這樣」。

use std::collections::HashMap;

use wincv_markdown::{Block, Kind, Style};

use super::*;

const TESTDATA: &str = concat!(env!("CARGO_MANIFEST_DIR"), "/../../../internal/doc97/testdata/");

fn open_doc(name: &str) -> Doc {
    Doc::open(format!("{TESTDATA}{name}")).unwrap()
}

fn text(bs: &[Block]) -> String {
    let mut sb = String::new();
    for b in bs {
        for s in &b.spans {
            sb.push_str(&s.text);
        }
        for r in &b.rows {
            sb.push_str(&r.join("|"));
        }
        sb.push('\n');
    }
    sb
}

#[test]
fn rich_document() {
    let bs = open_doc("rich.doc").blocks();
    // 對照 LibreOffice 轉出來的純文字:內容要一字不差。
    let want = [
        "第一章 標題",
        "這是一段粗體與斜體的中文內文。",
        "1.1 小節",
        "條列一",
        "條列二",
        "甲|乙丙|丁",
        "A plain ASCII paragraph with a link.",
    ];
    let all = text(&bs);
    let got: Vec<&str> = all.trim_end_matches('\n').split('\n').collect();
    assert_eq!(got.len(), want.len(), "區塊數不對,拿到 {}:{got:?}", got.len());
    for (i, w) in want.iter().enumerate() {
        assert_eq!(got[i], *w, "第 {i} 個區塊");
    }
    // 標題靠樣式識別碼認出來,與介面語言無關。
    assert!(bs[0].kind == Kind::Heading && bs[0].level == 1, "第一章:kind={:?} level={}", bs[0].kind, bs[0].level);
    assert!(bs[2].kind == Kind::Heading && bs[2].level == 2, "小節:kind={:?} level={}", bs[2].kind, bs[2].level);
    assert!(bs[3].kind == Kind::List && bs[4].kind == Kind::List, "條列沒有認出來:{:?} / {:?}", bs[3].kind, bs[4].kind);
    assert!(
        bs[5].kind == Kind::Table && bs[5].rows.len() == 2 && bs[5].rows[1][1] == "丁",
        "表格不對:{:?}",
        bs[5].rows
    );
}

/// 中文走 UTF-16 的片段,英文走單位元組的片段。同一份文件裡兩種都有,
/// 而單位元組那一種一律是 cp1252:不是文件的語系字碼頁。
#[test]
fn both_piece_kinds() {
    let got = text(&open_doc("rich.doc").blocks());
    assert!(got.contains("的中文內文"), "UTF-16 片段沒解出來:{got:?}");
    assert!(got.contains("A plain ASCII paragraph"), "單位元組片段沒解出來:{got:?}");
}

#[test]
fn character_formatting() {
    let bs = open_doc("rich.doc").blocks();
    let (mut bold, mut italic, mut link) = (String::new(), String::new(), String::new());
    for b in &bs {
        for s in &b.spans {
            if s.style.has(Style::BOLD) {
                bold += &s.text;
            } else if s.style.has(Style::ITALIC) {
                italic += &s.text;
            } else if s.style.has(Style::LINK) {
                link += &s.text;
            }
        }
    }
    assert!(bold == "粗體" && italic == "斜體", "粗體 {bold:?},斜體 {italic:?}");
    assert_eq!(link, "link", "超連結的文字是 {link:?}");
}

#[test]
fn footnotes() {
    let bs = open_doc("notes.doc").blocks();
    let got = text(&bs);
    assert!(got.contains("[註 1]"), "正文少了註腳標記:{got:?}");
    assert!(got.contains("這是註腳內容"), "文末少了註腳內容:{got:?}");
    // 註腳的文字接在正文後面,不能混進正文那一段。
    assert!(!text(&bs[..1]).contains("這是註腳內容"), "註腳跑進正文了:{got:?}");
}

#[test]
fn not_a_doc() {
    assert!(Doc::open(format!("{TESTDATA}rich.html")).is_err(), "不是複合文件應該要失敗");
}

/// 有序清單要有編號。
///
/// [雷] 編號方式不在段落屬性上:段落只說得出「屬於第幾串、第幾層」,
/// 是編號還是項目符號要去清單定義表(PlfLst)裡查。而 lcbPlfLst **只涵蓋
/// 前半段**(cLst + LSTF 陣列),後面接著的 LVL 陣列不算在裡面:
/// 照 lcb 切下去剛好切在第一組 LVL 之前,讀到的 LSTF 完全正確、數量也對,
/// 只是每一串都是零層,於是所有編號清單都變成項目符號。
/// 沒有任何一個地方會報錯,因為切出來的那一段本身是完好的。
///
/// 對照組是 LibreOffice 讀同一份檔案:它轉出的純文字是「1. 條列一」。
#[test]
fn ordered_list_numbers() {
    let bs = open_doc("rich.doc").blocks();
    let mut got = Vec::new();
    for b in &bs {
        if b.kind != Kind::List {
            continue;
        }
        assert!(b.ordered, "清單 {:?} 沒有被認成有序清單", text(std::slice::from_ref(b)));
        got.push(b.num);
    }
    assert_eq!(got, [1, 2], "編號 = {got:?},預期 [1 2]");
}

/// 沒有清單定義的段落仍然要當清單畫出來:段落自己已經說了它屬於某一串,
/// 那個事實比「查不到格式」更可靠:至少縮排與符號是對的。
#[test]
fn list_without_format_still_lists() {
    let mut d = open_doc("rich.doc");
    d.lists = HashMap::new(); // 假裝定義表讀不到
    d.lfo = Vec::new();
    let mut n = 0;
    for b in d.blocks() {
        if b.kind == Kind::List {
            n += 1;
            assert!(!b.ordered, "查不到定義時不該自己宣稱是有序清單");
        }
    }
    assert_eq!(n, 2, "認出 {n} 段清單,預期 2");
}

/// Prc 的 cb 是有號數。cb 小於等於 -3 時位置不會往前走,要當成 piece table 截斷。
/// 對應 Go 版 `TestCLXNegativePrcSize`。
#[test]
fn clx_negative_prc_size() {
    for clx in [&[1u8, 0xFD, 0xFF][..], &[1, 0xFC, 0xFF], &[1, 0x00, 0x80, 2, 0]] {
        assert!(parse_clx(clx).is_err(), "{clx:02X?}:應該回報截斷");
    }
}

/// 組一份只有樣式表的 FIB 與 table 串流。
fn style_table(stds: &[u8], cstd: u16) -> (Vec<u8>, Vec<u8>) {
    let mut st = vec![4, 0, cstd as u8, (cstd >> 8) as u8, 0, 0];
    st.extend_from_slice(stds);
    let mut wd = vec![0u8; 0x9A + 2 * 8];
    let off = 0x9A + FIB_STSHF * 8;
    wd[off + 4..off + 8].copy_from_slice(&(st.len() as u32).to_le_bytes());
    (wd, st)
}

fn empty_doc() -> Doc {
    Doc {
        chars: Vec::new(),
        fcs: Vec::new(),
        ccp_text: 0,
        ccp_ftn: 0,
        paras: Vec::new(),
        runs: Vec::new(),
        stis: Vec::new(),
        lists: HashMap::new(),
        lfo: Vec::new(),
    }
}

/// cbStd 小於 2 的 STD 連樣式識別碼都放不下,不論在表中間還是表尾,
/// 都當成不知道(-1),也不讀到下一筆的長度欄位或表外。
/// 對應 Go 版 `TestStyleTooShortForSti`。
#[test]
fn style_too_short_for_sti() {
    let cases: [(&str, &[u8], u16, &[i64]); 2] = [
        ("表尾", &[2, 0, 1, 0, 1, 0, 5], 2, &[1, -1]),
        ("表中間", &[2, 0, 1, 0, 1, 0, 5, 2, 0, 2, 0], 3, &[1, -1, 2]),
    ];
    for (name, stds, cstd, want) in cases {
        let (wd, tbl) = style_table(stds, cstd);
        let mut d = empty_doc();
        d.read_styles(&wd, &tbl);
        assert_eq!(d.stis, want, "{name}");
    }
}
