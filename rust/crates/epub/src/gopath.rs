//! Go 的 `path` 套件(Clean、Join、Dir),逐位元組照搬。
//!
//! zip 內的路徑用 `/` 分隔,與作業系統無關,所以不能用 `std::path`
//! (Windows 上會把 `\` 也當分隔符號,`..` 的處理也不同)。

pub(crate) fn clean(path: &str) -> String {
    if path.is_empty() {
        return ".".into();
    }
    let p = path.as_bytes();
    let rooted = p[0] == b'/';
    let n = p.len();
    let mut out: Vec<u8> = Vec::with_capacity(n);
    let (mut r, mut dotdot) = (0usize, 0usize);
    if rooted {
        out.push(b'/');
        r = 1;
        dotdot = 1;
    }
    while r < n {
        if p[r] == b'/' {
            r += 1;
        } else if p[r] == b'.' && (r + 1 == n || p[r + 1] == b'/') {
            r += 1;
        } else if p[r] == b'.' && p[r + 1] == b'.' && (r + 2 == n || p[r + 2] == b'/') {
            r += 2;
            if out.len() > dotdot {
                // Go 的 lazybuf:寫入位置 w 往回退,停在 buf[w] 是 / 的地方(那個 / 不留)。
                let mut w = out.len() - 1;
                while w > dotdot && out[w] != b'/' {
                    w -= 1;
                }
                out.truncate(w);
            } else if !rooted {
                if !out.is_empty() {
                    out.push(b'/');
                }
                out.extend_from_slice(b"..");
                dotdot = out.len();
            }
        } else {
            if rooted && out.len() != 1 || !rooted && !out.is_empty() {
                out.push(b'/');
            }
            while r < n && p[r] != b'/' {
                out.push(p[r]);
                r += 1;
            }
        }
    }
    if out.is_empty() {
        return ".".into();
    }
    String::from_utf8(out).unwrap_or_default()
}

pub(crate) fn join(a: &str, b: &str) -> String {
    if a.is_empty() && b.is_empty() {
        return String::new();
    }
    let s = if a.is_empty() {
        b.to_string()
    } else if b.is_empty() {
        a.to_string()
    } else {
        format!("{a}/{b}")
    };
    clean(&s)
}

pub(crate) fn dir(path: &str) -> String {
    let i = path.rfind('/').map(|i| i + 1).unwrap_or(0);
    clean(&path[..i])
}

#[cfg(test)]
mod tests {
    use super::*;

    // 取自 Go 的 path_test.go 的 cleantests。
    #[test]
    fn clean_like_go() {
        for (i, o) in [
            ("", "."),
            ("abc", "abc"),
            ("abc/def", "abc/def"),
            ("a/b/c", "a/b/c"),
            (".", "."),
            ("..", ".."),
            ("../..", "../.."),
            ("../../abc", "../../abc"),
            ("/abc", "/abc"),
            ("/", "/"),
            ("abc/", "abc"),
            ("abc/def/", "abc/def"),
            ("a/b/c/", "a/b/c"),
            ("./", "."),
            ("../", ".."),
            ("../../", "../.."),
            ("/abc/", "/abc"),
            ("abc//def//ghi", "abc/def/ghi"),
            ("//abc", "/abc"),
            ("///abc", "/abc"),
            ("//abc//", "/abc"),
            ("abc//", "abc"),
            ("abc/./def", "abc/def"),
            ("/./abc/def", "/abc/def"),
            ("abc/.", "abc"),
            ("abc/def/ghi/../jkl", "abc/def/jkl"),
            ("abc/def/../ghi/../jkl", "abc/jkl"),
            ("abc/def/..", "abc"),
            ("abc/def/../..", "."),
            ("/abc/def/../..", "/"),
            ("abc/def/../../..", ".."),
            ("/abc/def/../../..", "/"),
            ("abc/def/../../../ghi/jkl/../../../mno", "../../mno"),
            ("abc/./../def", "def"),
            ("abc//./../def", "def"),
            ("abc/../../././../def", "../../def"),
        ] {
            assert_eq!(clean(i), o, "clean({i:?})");
        }
        assert_eq!(dir("a/b/c.xhtml"), "a/b");
        assert_eq!(dir("c.xhtml"), ".");
        assert_eq!(join("OEBPS", "../x.html"), "x.html");
        assert_eq!(join("", "a/./b"), "a/b");
    }
}
