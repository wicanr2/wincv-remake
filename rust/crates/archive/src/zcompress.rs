//! Unix `compress`(.Z)解碼器:LZW,9 到 16 位元的可變長度碼。
//!
//! 照搬 Go 版 `internal/archive/zcompress`。碼長增加或遇到清除碼時,
//! 讀取位置要跳到 8 個碼一組的邊界:原版 compress 一次讀一組 8 個碼,
//! 換碼長時把那一組剩下的位元丟掉。

const MAGIC1: u8 = 0x1F;
const MAGIC2: u8 = 0x9D;
const INIT_BITS: usize = 9;
const CLEAR_CODE: usize = 256;
/// 區塊模式下,第一個可用的字典編號。
const FIRST: usize = 257;

/// 看開頭的 magic 判斷是不是 .Z。
pub fn is_z(b: &[u8]) -> bool {
    b.len() >= 3 && b[0] == MAGIC1 && b[1] == MAGIC2
}

/// 解出整個檔案。
pub fn decode(data: &[u8]) -> Result<Vec<u8>, String> {
    if !is_z(data) {
        return Err(wincv_i18n::t("不是 compress 檔(magic 不對)"));
    }
    let maxbits = (data[2] & 0x1F) as usize;
    let block_mode = data[2] & 0x80 != 0;
    if !(INIT_BITS..=16).contains(&maxbits) {
        return Err(wincv_i18n::tr!("maxbits = %d 超出範圍", maxbits));
    }
    let maxmaxcode = 1usize << maxbits;

    let mut prefix = vec![0u16; maxmaxcode];
    let mut suffix = vec![0u8; maxmaxcode];
    for (i, s) in suffix.iter_mut().enumerate().take(256) {
        *s = i as u8;
    }

    let mut n_bits = INIT_BITS;
    let mut maxcode = (1usize << n_bits) - 1;
    let mut free_ent = if block_mode { FIRST } else { 256 };

    let mut bit_pos = 24usize; // 檔頭三個位元組
    let total = data.len() * 8;
    let mut in_group = 0usize; // 這一組(8 個碼)已經讀了幾個

    let mut out = Vec::new();
    let mut stack: Vec<u8> = Vec::new();
    let mut old_code: Option<usize> = None;
    let mut fin_char = 0u8;

    loop {
        if free_ent > maxcode && n_bits < maxbits {
            align(&mut bit_pos, &mut in_group, n_bits);
            n_bits += 1;
            maxcode = if n_bits == maxbits { maxmaxcode } else { (1 << n_bits) - 1 };
        }
        if bit_pos + n_bits > total {
            break;
        }
        let mut code = 0usize;
        for i in 0..n_bits {
            let p = bit_pos + i;
            if data[p >> 3] & (1 << (p & 7)) != 0 {
                code |= 1 << i;
            }
        }
        bit_pos += n_bits;
        in_group += 1;

        if block_mode && code == CLEAR_CODE {
            align(&mut bit_pos, &mut in_group, n_bits);
            n_bits = INIT_BITS;
            maxcode = (1 << n_bits) - 1;
            free_ent = FIRST;
            old_code = None;
            continue;
        }

        let Some(old) = old_code else {
            if code >= 256 {
                return Err(wincv_i18n::tr!("第一個碼是 %d,應該是單一位元組", code));
            }
            fin_char = code as u8;
            out.push(fin_char);
            old_code = Some(code);
            continue;
        };

        let incode = code;
        stack.clear();
        if code >= free_ent {
            if code > free_ent {
                return Err(wincv_i18n::tr!("字碼 %d 超出字典(%d)", code, free_ent));
            }
            stack.push(fin_char);
            code = old;
        }
        while code >= 256 {
            stack.push(suffix[code]);
            code = prefix[code] as usize;
            if stack.len() > maxmaxcode {
                return Err(wincv_i18n::t("字典有迴圈"));
            }
        }
        fin_char = suffix[code];
        stack.push(fin_char);
        out.extend(stack.iter().rev());

        if free_ent < maxmaxcode {
            prefix[free_ent] = old as u16;
            suffix[free_ent] = fin_char;
            free_ent += 1;
        }
        old_code = Some(incode);
    }
    Ok(out)
}

fn align(bit_pos: &mut usize, in_group: &mut usize, n_bits: usize) {
    let r = *in_group % 8;
    if r != 0 {
        *bit_pos += (8 - r) * n_bits;
    }
    *in_group = 0;
}
