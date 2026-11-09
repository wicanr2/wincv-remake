//! KK 音標的自訂編碼。
//!
//! kk.txt.dat 的音標是**一個位元組一個音素**的自訂編碼,不是可以直接顯示的文字。
//! 作者在資料裡留了兩筆條目,把整套符號按順序列了一遍:
//!
//! ```text
//! aaaaa         !"#$%&'()*+,-.>?ABCDEFGHIJKLMNOPQRSTUVWXa
//! aaaaaaaaaaaa  >?ABCDEFGHIJKLMNOPQRSTUVWX!"#$%&'()*+,-.a
//! ```
//!
//! 也就是 14 個母音(0x21-0x2E)、2 個重音記號、24 個子音(0x41-0x58),外加一個
//! 'a' 當雙母音的前半。各符號的音值再拿已知發音的單字反推(be `B!` → bi、ship
//! `M"A` → ʃɪp、little `>S"CW` → ˈlɪtl̩ 等),最後兩筆定出 W 與 X 是成音節的 l 與 n。

fn symbol(c: u8) -> Option<&'static str> {
    Some(match c {
        // 母音 0x21-0x2E
        b'!' => "i", b'"' => "ɪ", b'#' => "ɛ", b'$' => "æ", b'%' => "ɑ", b'&' => "ɔ", b'\'' => "ʊ",
        b'(' => "u", b')' => "ʌ", b'*' => "ɝ", b'+' => "ə", b',' => "ɚ", b'-' => "e", b'.' => "o",
        // 雙母音的前半:a 之後接 " 或 ' 就是 aɪ / aʊ
        b'a' => "a",
        // 重音
        b'>' => "ˈ", b'?' => "ˌ",
        // 子音 0x41-0x58
        b'A' => "p", b'B' => "b", b'C' => "t", b'D' => "d", b'E' => "k", b'F' => "ɡ",
        b'G' => "f", b'H' => "v", b'I' => "θ", b'J' => "ð", b'K' => "s", b'L' => "z",
        b'M' => "ʃ", b'N' => "ʒ", b'O' => "h", b'P' => "m", b'Q' => "n", b'R' => "ŋ",
        b'S' => "l", b'T' => "r", b'U' => "j", b'V' => "w",
        b'W' => "l̩", b'X' => "n̩",
        _ => return None,
    })
}

/// 把原始編碼轉成看得懂的音標。認不得的位元組原樣留著:少數條目夾了 Big5 的
/// 中文註記(例如 eh 的「用口上語調」),那些不是音素,硬轉會變成亂碼。
pub fn kk_to_ipa(s: &str) -> String {
    let b = s.as_bytes();
    let mut out: Vec<u8> = Vec::with_capacity(b.len() * 2);
    let mut i = 0;
    while i < b.len() {
        // tʃ 與 dʒ 沒有各自的符號,寫成兩個位元組
        match (b[i], b.get(i + 1)) {
            (b'C', Some(b'M')) => {
                out.extend_from_slice("tʃ".as_bytes());
                i += 2;
                continue;
            }
            (b'D', Some(b'N')) => {
                out.extend_from_slice("dʒ".as_bytes());
                i += 2;
                continue;
            }
            _ => {}
        }
        match symbol(b[i]) {
            Some(v) => out.extend_from_slice(v.as_bytes()),
            None => out.push(b[i]),
        }
        i += 1;
    }
    // 只換了 ASCII 位元組,非 ASCII 的部分原樣保留,所以仍是合法 UTF-8。
    String::from_utf8(out).unwrap_or_default()
}
