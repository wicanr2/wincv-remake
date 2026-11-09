//! 對應 Go 端 `tools/textencsum`:判讀結果與強制用各種編碼解碼後的 sha256。

use sha2::{Digest, Sha256};
use wincv_textenc::{decode, detect, Enc};

const ALL: [Enc; 8] = [Enc::Utf8, Enc::Utf16Le, Enc::Utf16Be, Enc::Big5, Enc::Gbk, Enc::ShiftJis, Enc::EucKr, Enc::Binary];

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() == 3 && args[0] == "-dump" {
        let b = std::fs::read(&args[2]).expect("讀不到檔案");
        let e = ALL.iter().find(|e| e.name() == args[1]).expect("不認得的編碼");
        print!("{}", decode(&b, *e));
        return;
    }
    for p in args {
        let b = match std::fs::read(&p) {
            Ok(b) => b,
            Err(e) => {
                eprintln!("{p} {e}");
                continue;
            }
        };
        let parts: Vec<String> = ALL
            .iter()
            .map(|&e| {
                let s = decode(&b, e);
                let h = Sha256::digest(s.as_bytes());
                let (n, bad) = (s.chars().count(), s.chars().filter(|&c| c == '\u{FFFD}').count());
                format!("{}={}:{n}:{bad}", e.name(), h.iter().map(|x| format!("{x:02x}")).collect::<String>())
            })
            .collect();
        println!("{p}\t{}\t{}", detect(&b).name(), parts.join(" "));
    }
}
