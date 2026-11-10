//! Go 的 `path` 套件(斜線分隔、純字面運算)裡用得到的幾支,逐行照搬。
//!
//! 不用 `std::path`:那是作業系統的路徑,在 Windows 上會認反斜線、
//! 會把 `C:` 當成磁碟機。ZIP 裡的路徑一律是斜線分隔的字串,
//! 規則要跟 Go 版一模一樣,查表才對得上。

/// Go 的 `path.Clean`。
pub fn clean(p: &str) -> String {
    if p.is_empty() {
        return ".".to_string();
    }
    let b = p.as_bytes();
    let n = b.len();
    let rooted = b[0] == b'/';
    // Go 的 lazybuf:buf 是寫過的內容,w 是目前的長度。往回退時只動 w,
    // 之後再寫就蓋掉;index 讀的是 w 位置上「之前寫過」的那個位元組。
    let mut buf: Vec<u8> = Vec::with_capacity(n);
    let mut w = 0usize;
    let append = |buf: &mut Vec<u8>, w: &mut usize, c: u8| {
        if *w < buf.len() {
            buf[*w] = c;
        } else {
            buf.push(c);
        }
        *w += 1;
    };
    let (mut r, mut dotdot) = (0, 0);
    if rooted {
        append(&mut buf, &mut w, b'/');
        r = 1;
        dotdot = 1;
    }
    while r < n {
        if b[r] == b'/' {
            r += 1;
        } else if b[r] == b'.' && (r + 1 == n || b[r + 1] == b'/') {
            r += 1;
        } else if b[r] == b'.' && b[r + 1] == b'.' && (r + 2 == n || b[r + 2] == b'/') {
            r += 2;
            if w > dotdot {
                w -= 1;
                while w > dotdot && buf[w] != b'/' {
                    w -= 1;
                }
            } else if !rooted {
                if w > 0 {
                    append(&mut buf, &mut w, b'/');
                }
                append(&mut buf, &mut w, b'.');
                append(&mut buf, &mut w, b'.');
                dotdot = w;
            }
        } else {
            if rooted && w != 1 || !rooted && w != 0 {
                append(&mut buf, &mut w, b'/');
            }
            while r < n && b[r] != b'/' {
                append(&mut buf, &mut w, b[r]);
                r += 1;
            }
        }
    }
    if w == 0 {
        return ".".to_string();
    }
    String::from_utf8_lossy(&buf[..w]).into_owned()
}

/// Go 的 `path.Split`:最後一個斜線之後是檔名。
pub fn split(p: &str) -> (&str, &str) {
    match p.rfind('/') {
        Some(i) => (&p[..i + 1], &p[i + 1..]),
        None => ("", p),
    }
}

/// Go 的 `path.Dir`。
pub fn dir(p: &str) -> String {
    clean(split(p).0)
}

/// Go 的 `path.Base`。
pub fn base(p: &str) -> String {
    if p.is_empty() {
        return ".".to_string();
    }
    let p = p.trim_end_matches('/');
    if p.is_empty() {
        return "/".to_string();
    }
    match p.rfind('/') {
        Some(i) => p[i + 1..].to_string(),
        None => p.to_string(),
    }
}

/// Go 的 `path.Join`:略過空字串,接起來再 Clean;全部是空的回空字串。
pub fn join(elems: &[&str]) -> String {
    let parts: Vec<&str> = elems.iter().copied().filter(|e| !e.is_empty()).collect();
    if parts.is_empty() {
        return String::new();
    }
    clean(&parts.join("/"))
}

/// Go 的 `path.Ext`:最後一個元素裡最後一個點(含)之後。
pub fn ext(p: &str) -> String {
    for (i, c) in p.bytes().enumerate().rev() {
        if c == b'/' {
            break;
        }
        if c == b'.' {
            return p[i..].to_string();
        }
    }
    String::new()
}
