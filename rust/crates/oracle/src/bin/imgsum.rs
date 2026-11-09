//! 用 `wincv-imgfmt` 解一批圖檔(對應 Go 端 `tools/imgsum`)。
//!
//! ```text
//! imgsum <輸出目錄> < 清單
//! ```
//!
//! 清單每行一個圖檔路徑。從 stdin 讀而不收參數:清單很長時 xargs 會分批執行,
//! 每批的序號都從 0 起算,序號就對不上檔名了。
//!
//! stdout 每列「序號<TAB>格式<TAB>寬<TAB>高<TAB>sha256」,解不開的寫「序號<TAB>ERR<TAB>訊息」。
//! 第 i 張的像素(非預乘 RGBA8)在 `<輸出目錄>/<i>.rgba`。

use sha2::{Digest, Sha256};

fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let [out] = args.as_slice() else {
        eprintln!("用法: imgsum <輸出目錄> < 清單");
        std::process::exit(2);
    };
    use std::io::BufRead;
    for (i, name) in std::io::stdin().lock().lines().map_while(Result::ok).enumerate() {
        let name = &name;
        // 與 Go 端相同:解碼器的 panic 當成這一張的錯誤,一張壞圖不影響其餘的。
        let res = std::fs::read(name).map_err(|e| e.to_string()).and_then(|d| {
            std::panic::catch_unwind(|| wincv_imgfmt::decode(name, &d)).unwrap_or_else(|_| Err("panic".to_string()))
        });
        match res {
            Ok((img, kind)) => {
                let sum: String = Sha256::digest(&img.pix).iter().map(|b| format!("{b:02x}")).collect();
                let _ = std::fs::write(format!("{out}/{i}.rgba"), &img.pix);
                println!("{i}\t{kind}\t{}\t{}\t{sum}", img.w, img.h);
            }
            Err(e) => println!("{i}\tERR\t{}", e.replace(['\t', '\n'], " ")),
        }
    }
}
