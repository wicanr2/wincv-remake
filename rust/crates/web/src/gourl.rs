//! Go 1.22 `net/url` 的子集:Parse、String、ResolveReference、RequestURI。
//!
//! 為什麼自己搬而不用 `url` crate:`url` 實作的是 WHATWG URL 標準,Go 的 net/url
//! 照的是 RFC 3986,兩者在相對位址、空白、反斜線、主機大小寫上的結果不同。
//! 這一包的輸出(連結的絕對位址、重導向之後的最終位址)要與 Go 版逐位元相同,
//! 所以照 Go 的原始碼逐段搬。
//!
//! Go 的字串是位元組序列,這裡全部在 `&[u8]` / `Vec<u8>` 上做,
//! 不合法的 UTF-8 位元組照 Go 的規則逃脫成 `%XX`。

/// 一個 URL。欄位語意與 Go 的 `url.URL` 相同。
#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct Url {
    pub scheme: Vec<u8>,
    pub opaque: Vec<u8>,
    pub user: Option<Userinfo>,
    pub host: Vec<u8>,
    pub path: Vec<u8>,
    pub raw_path: Vec<u8>,
    pub omit_host: bool,
    pub force_query: bool,
    pub raw_query: Vec<u8>,
    pub fragment: Vec<u8>,
    pub raw_fragment: Vec<u8>,
}

#[derive(Clone, Debug, Default, PartialEq, Eq)]
pub(crate) struct Userinfo {
    username: Vec<u8>,
    password: Vec<u8>,
    password_set: bool,
}

#[derive(Clone, Copy, PartialEq, Eq)]
enum Mode {
    Path,
    // 這一包用不到 PathEscape,留著是為了 should_escape 與 Go 逐條對得上。
    #[allow(dead_code)]
    PathSegment,
    Host,
    Zone,
    UserPassword,
    QueryComponent,
    Fragment,
}

const UPPERHEX: &[u8; 16] = b"0123456789ABCDEF";

fn ishex(c: u8) -> bool {
    c.is_ascii_hexdigit()
}

fn unhex(c: u8) -> u8 {
    match c {
        b'0'..=b'9' => c - b'0',
        b'a'..=b'f' => c - b'a' + 10,
        b'A'..=b'F' => c - b'A' + 10,
        _ => 0,
    }
}

fn should_escape(c: u8, mode: Mode) -> bool {
    if c.is_ascii_alphanumeric() {
        return false;
    }
    if mode == Mode::Host || mode == Mode::Zone {
        if matches!(
            c,
            b'!' | b'$'
                | b'&'
                | b'\''
                | b'('
                | b')'
                | b'*'
                | b'+'
                | b','
                | b';'
                | b'='
                | b':'
                | b'['
                | b']'
                | b'<'
                | b'>'
                | b'"'
        ) {
            return false;
        }
    }
    match c {
        b'-' | b'_' | b'.' | b'~' => return false,
        b'$' | b'&' | b'+' | b',' | b'/' | b':' | b';' | b'=' | b'?' | b'@' => match mode {
            Mode::Path => return c == b'?',
            Mode::PathSegment => return c == b'/' || c == b';' || c == b',' || c == b'?',
            Mode::UserPassword => return c == b'@' || c == b'/' || c == b'?' || c == b':',
            Mode::QueryComponent => return true,
            Mode::Fragment => return false,
            _ => {}
        },
        _ => {}
    }
    if mode == Mode::Fragment && matches!(c, b'!' | b'(' | b')' | b'*') {
        return false;
    }
    true
}

/// Go 的錯誤訊息照搬(`strconv.Quote` 的部分只處理這裡會遇到的字元)。
fn quote(s: &[u8]) -> String {
    let mut o = String::from("\"");
    for ch in crate::gostr::from_bytes(s).chars() {
        match ch {
            '"' => o.push_str("\\\""),
            '\\' => o.push_str("\\\\"),
            '\n' => o.push_str("\\n"),
            '\r' => o.push_str("\\r"),
            '\t' => o.push_str("\\t"),
            c if crate::gostr::raw_byte(c).is_some() => {
                o.push_str(&format!("\\x{:02x}", crate::gostr::raw_byte(c).unwrap()))
            }
            c if (c as u32) < 0x20 || c == '\u{7f}' => o.push_str(&format!("\\x{:02x}", c as u32)),
            c => o.push(c),
        }
    }
    o.push('"');
    o
}

fn escape_error(s: &[u8]) -> String {
    format!("invalid URL escape {}", quote(s))
}

fn unescape(s: &[u8], mode: Mode) -> Result<Vec<u8>, String> {
    let mut n = 0;
    let mut has_plus = false;
    let mut i = 0;
    while i < s.len() {
        match s[i] {
            b'%' => {
                n += 1;
                if i + 2 >= s.len() || !ishex(s[i + 1]) || !ishex(s[i + 2]) {
                    let mut t = &s[i..];
                    if t.len() > 3 {
                        t = &t[..3];
                    }
                    return Err(escape_error(t));
                }
                if mode == Mode::Host && unhex(s[i + 1]) < 8 && &s[i..i + 3] != b"%25" {
                    return Err(escape_error(&s[i..i + 3]));
                }
                if mode == Mode::Zone {
                    let v = unhex(s[i + 1]) << 4 | unhex(s[i + 2]);
                    if &s[i..i + 3] != b"%25" && v != b' ' && should_escape(v, Mode::Host) {
                        return Err(escape_error(&s[i..i + 3]));
                    }
                }
                i += 3;
            }
            b'+' => {
                has_plus = mode == Mode::QueryComponent;
                i += 1;
            }
            c => {
                if (mode == Mode::Host || mode == Mode::Zone) && c < 0x80 && should_escape(c, mode)
                {
                    return Err(format!(
                        "invalid character {} in host name",
                        quote(&s[i..i + 1])
                    ));
                }
                i += 1;
            }
        }
    }
    if n == 0 && !has_plus {
        return Ok(s.to_vec());
    }
    let mut t = Vec::with_capacity(s.len() - 2 * n);
    let mut i = 0;
    while i < s.len() {
        match s[i] {
            b'%' => {
                t.push(unhex(s[i + 1]) << 4 | unhex(s[i + 2]));
                i += 2;
            }
            b'+' => t.push(if mode == Mode::QueryComponent {
                b' '
            } else {
                b'+'
            }),
            c => t.push(c),
        }
        i += 1;
    }
    Ok(t)
}

fn escape(s: &[u8], mode: Mode) -> Vec<u8> {
    let mut space = 0;
    let mut hex = 0;
    for &c in s {
        if should_escape(c, mode) {
            if c == b' ' && mode == Mode::QueryComponent {
                space += 1;
            } else {
                hex += 1;
            }
        }
    }
    if space == 0 && hex == 0 {
        return s.to_vec();
    }
    let mut t = Vec::with_capacity(s.len() + 2 * hex);
    for &c in s {
        if c == b' ' && mode == Mode::QueryComponent {
            t.push(b'+');
        } else if should_escape(c, mode) {
            t.push(b'%');
            t.push(UPPERHEX[(c >> 4) as usize]);
            t.push(UPPERHEX[(c & 15) as usize]);
        } else {
            t.push(c);
        }
    }
    t
}

impl Userinfo {
    fn string(&self) -> Vec<u8> {
        let mut s = escape(&self.username, Mode::UserPassword);
        if self.password_set {
            s.push(b':');
            s.extend(escape(&self.password, Mode::UserPassword));
        }
        s
    }
}

fn get_scheme(raw: &[u8]) -> Result<(&[u8], &[u8]), String> {
    for (i, &c) in raw.iter().enumerate() {
        match c {
            b'a'..=b'z' | b'A'..=b'Z' => {}
            b'0'..=b'9' | b'+' | b'-' | b'.' => {
                if i == 0 {
                    return Ok((b"", raw));
                }
            }
            b':' => {
                if i == 0 {
                    return Err("missing protocol scheme".into());
                }
                return Ok((&raw[..i], &raw[i + 1..]));
            }
            _ => return Ok((b"", raw)),
        }
    }
    Ok((b"", raw))
}

fn cut(s: &[u8], sep: u8) -> (&[u8], &[u8], bool) {
    match s.iter().position(|&c| c == sep) {
        Some(i) => (&s[..i], &s[i + 1..], true),
        None => (s, b"", false),
    }
}

fn contains(s: &[u8], c: u8) -> bool {
    s.contains(&c)
}

/// Go 的 url.Parse。錯誤訊息照 Go 的 `parse "原文": 原因`。
pub(crate) fn parse(raw: &[u8]) -> Result<Url, String> {
    let (u, frag, _) = cut(raw, b'#');
    let mut url = parse_inner(u, false).map_err(|e| format!("parse {}: {e}", quote(u)))?;
    if frag.is_empty() {
        return Ok(url);
    }
    url.set_fragment(frag)
        .map_err(|e| format!("parse {}: {e}", quote(raw)))?;
    Ok(url)
}

fn parse_inner(raw: &[u8], via_request: bool) -> Result<Url, String> {
    if raw.iter().any(|&b| b < b' ' || b == 0x7f) {
        return Err("net/url: invalid control character in URL".into());
    }
    if raw.is_empty() && via_request {
        return Err("empty url".into());
    }
    let mut url = Url::default();
    if raw == b"*" {
        url.path = b"*".to_vec();
        return Ok(url);
    }
    let (scheme, mut rest) = get_scheme(raw)?;
    url.scheme = scheme.to_ascii_lowercase();

    let rest_owned;
    if rest.ends_with(b"?") && rest.iter().filter(|&&c| c == b'?').count() == 1 {
        url.force_query = true;
        rest = &rest[..rest.len() - 1];
    } else {
        let (r, q, _) = cut(rest, b'?');
        url.raw_query = q.to_vec();
        rest_owned = r;
        rest = rest_owned;
    }

    if !rest.starts_with(b"/") {
        if !url.scheme.is_empty() {
            url.opaque = rest.to_vec();
            return Ok(url);
        }
        if via_request {
            return Err("invalid URI for request".into());
        }
        let (segment, _, _) = cut(rest, b'/');
        if contains(segment, b':') {
            return Err("first path segment in URL cannot contain colon".into());
        }
    }

    if (!url.scheme.is_empty() || !via_request && !rest.starts_with(b"///"))
        && rest.starts_with(b"//")
    {
        let mut authority = &rest[2..];
        rest = b"";
        if let Some(i) = authority.iter().position(|&c| c == b'/') {
            rest = &authority[i..];
            authority = &authority[..i];
        }
        let (user, host) = parse_authority(authority)?;
        url.user = user;
        url.host = host;
    } else if !url.scheme.is_empty() && rest.starts_with(b"/") {
        url.omit_host = true;
    }
    url.set_path(rest)?;
    Ok(url)
}

fn valid_userinfo(s: &[u8]) -> bool {
    for &r in s {
        if r.is_ascii_alphanumeric() {
            continue;
        }
        match r {
            b'-' | b'.' | b'_' | b':' | b'~' | b'!' | b'$' | b'&' | b'\'' | b'(' | b')' | b'*'
            | b'+' | b',' | b';' | b'=' | b'%' | b'@' => {}
            _ => return false,
        }
    }
    true
}

fn parse_authority(authority: &[u8]) -> Result<(Option<Userinfo>, Vec<u8>), String> {
    let i = authority.iter().rposition(|&c| c == b'@');
    let host = match i {
        None => parse_host(authority)?,
        Some(i) => parse_host(&authority[i + 1..])?,
    };
    let Some(i) = i else { return Ok((None, host)) };
    let userinfo = &authority[..i];
    if !valid_userinfo(userinfo) {
        return Err("net/url: invalid userinfo".into());
    }
    let user = if !contains(userinfo, b':') {
        Userinfo {
            username: unescape(userinfo, Mode::UserPassword)?,
            ..Default::default()
        }
    } else {
        let (name, pass, _) = cut(userinfo, b':');
        Userinfo {
            username: unescape(name, Mode::UserPassword)?,
            password: unescape(pass, Mode::UserPassword)?,
            password_set: true,
        }
    };
    Ok((Some(user), host))
}

fn find_sub(s: &[u8], sub: &[u8]) -> Option<usize> {
    if sub.len() > s.len() {
        return None;
    }
    (0..=s.len() - sub.len()).find(|&i| &s[i..i + sub.len()] == sub)
}

fn parse_host(host: &[u8]) -> Result<Vec<u8>, String> {
    if host.starts_with(b"[") {
        let Some(i) = host.iter().rposition(|&c| c == b']') else {
            return Err("missing ']' in host".into());
        };
        let colon_port = &host[i + 1..];
        if !valid_optional_port(colon_port) {
            return Err(format!("invalid port {} after host", quote(colon_port)));
        }
        if let Some(zone) = find_sub(&host[..i], b"%25") {
            let mut h = unescape(&host[..zone], Mode::Host)?;
            h.extend(unescape(&host[zone..i], Mode::Zone)?);
            h.extend(unescape(&host[i..], Mode::Host)?);
            return Ok(h);
        }
    } else if let Some(i) = host.iter().rposition(|&c| c == b':') {
        let colon_port = &host[i..];
        if !valid_optional_port(colon_port) {
            return Err(format!("invalid port {} after host", quote(colon_port)));
        }
    }
    unescape(host, Mode::Host)
}

fn valid_optional_port(port: &[u8]) -> bool {
    if port.is_empty() {
        return true;
    }
    if port[0] != b':' {
        return false;
    }
    // Go 以 rune 走訪:非 ASCII 的位元組也一樣不是數字。
    port[1..].iter().all(|b| b.is_ascii_digit())
}

fn valid_encoded(s: &[u8], mode: Mode) -> bool {
    for &c in s {
        match c {
            b'!' | b'$' | b'&' | b'\'' | b'(' | b')' | b'*' | b'+' | b',' | b';' | b'=' | b':'
            | b'@' => {}
            b'[' | b']' => {}
            b'%' => {}
            _ => {
                if should_escape(c, mode) {
                    return false;
                }
            }
        }
    }
    true
}

impl Url {
    fn set_path(&mut self, p: &[u8]) -> Result<(), String> {
        let path = unescape(p, Mode::Path)?;
        self.raw_path = if escape(&path, Mode::Path) == p {
            Vec::new()
        } else {
            p.to_vec()
        };
        self.path = path;
        Ok(())
    }

    pub(crate) fn escaped_path(&self) -> Vec<u8> {
        if !self.raw_path.is_empty() && valid_encoded(&self.raw_path, Mode::Path) {
            if let Ok(p) = unescape(&self.raw_path, Mode::Path) {
                if p == self.path {
                    return self.raw_path.clone();
                }
            }
        }
        if self.path == b"*" {
            return b"*".to_vec();
        }
        escape(&self.path, Mode::Path)
    }

    fn set_fragment(&mut self, f: &[u8]) -> Result<(), String> {
        let frag = unescape(f, Mode::Fragment)?;
        self.raw_fragment = if escape(&frag, Mode::Fragment) == f {
            Vec::new()
        } else {
            f.to_vec()
        };
        self.fragment = frag;
        Ok(())
    }

    fn escaped_fragment(&self) -> Vec<u8> {
        if !self.raw_fragment.is_empty() && valid_encoded(&self.raw_fragment, Mode::Fragment) {
            if let Ok(f) = unescape(&self.raw_fragment, Mode::Fragment) {
                if f == self.fragment {
                    return self.raw_fragment.clone();
                }
            }
        }
        escape(&self.fragment, Mode::Fragment)
    }

    /// Go 的 `URL.String()`。
    pub(crate) fn string(&self) -> Vec<u8> {
        let mut buf = Vec::new();
        if !self.scheme.is_empty() {
            buf.extend_from_slice(&self.scheme);
            buf.push(b':');
        }
        if !self.opaque.is_empty() {
            buf.extend_from_slice(&self.opaque);
        } else {
            if !self.scheme.is_empty() || !self.host.is_empty() || self.user.is_some() {
                if self.omit_host && self.host.is_empty() && self.user.is_none() {
                    // 省略 authority。
                } else {
                    if !self.host.is_empty() || !self.path.is_empty() || self.user.is_some() {
                        buf.extend_from_slice(b"//");
                    }
                    if let Some(ui) = &self.user {
                        buf.extend(ui.string());
                        buf.push(b'@');
                    }
                    if !self.host.is_empty() {
                        buf.extend(escape(&self.host, Mode::Host));
                    }
                }
            }
            let path = self.escaped_path();
            if !path.is_empty() && path[0] != b'/' && !self.host.is_empty() {
                buf.push(b'/');
            }
            if buf.is_empty() {
                let (segment, _, _) = cut(&path, b'/');
                if contains(segment, b':') {
                    buf.extend_from_slice(b"./");
                }
            }
            buf.extend(path);
        }
        if self.force_query || !self.raw_query.is_empty() {
            buf.push(b'?');
            buf.extend_from_slice(&self.raw_query);
        }
        if !self.fragment.is_empty() {
            buf.push(b'#');
            buf.extend(self.escaped_fragment());
        }
        buf
    }

    /// Go 的 `URL.ResolveReference`。
    pub(crate) fn resolve_reference(&self, r: &Url) -> Url {
        let mut url = r.clone();
        if r.scheme.is_empty() {
            url.scheme = self.scheme.clone();
        }
        if !r.scheme.is_empty() || !r.host.is_empty() || r.user.is_some() {
            let _ = url.set_path(&resolve_path(&r.escaped_path(), b""));
            return url;
        }
        if !r.opaque.is_empty() {
            url.user = None;
            url.host.clear();
            url.path.clear();
            return url;
        }
        if r.path.is_empty() && !r.force_query && r.raw_query.is_empty() {
            url.raw_query = self.raw_query.clone();
            if r.fragment.is_empty() {
                url.fragment = self.fragment.clone();
                url.raw_fragment = self.raw_fragment.clone();
            }
        }
        url.host = self.host.clone();
        url.user = self.user.clone();
        let _ = url.set_path(&resolve_path(&self.escaped_path(), &r.escaped_path()));
        url
    }

    /// Go 的 `URL.Parse(ref)`:解析 ref 再以自己為基準接起來。
    pub(crate) fn parse_ref(&self, r: &[u8]) -> Result<Url, String> {
        Ok(self.resolve_reference(&parse(r)?))
    }

    /// Go 的 `URL.RequestURI()`。
    pub(crate) fn request_uri(&self) -> Vec<u8> {
        let mut result = self.opaque.clone();
        if result.is_empty() {
            result = self.escaped_path();
            if result.is_empty() {
                result = b"/".to_vec();
            }
        } else if result.starts_with(b"//") {
            let mut r = self.scheme.clone();
            r.push(b':');
            r.extend(result);
            result = r;
        }
        if self.force_query || !self.raw_query.is_empty() {
            result.push(b'?');
            result.extend_from_slice(&self.raw_query);
        }
        result
    }
}

fn resolve_path(base: &[u8], r: &[u8]) -> Vec<u8> {
    let full: Vec<u8> = if r.is_empty() {
        base.to_vec()
    } else if r[0] != b'/' {
        let i = base
            .iter()
            .rposition(|&c| c == b'/')
            .map(|i| i as isize)
            .unwrap_or(-1);
        let mut f = base[..(i + 1) as usize].to_vec();
        f.extend_from_slice(r);
        f
    } else {
        r.to_vec()
    };
    if full.is_empty() {
        return Vec::new();
    }
    let mut dst: Vec<u8> = vec![b'/'];
    let mut first = true;
    let mut remaining: &[u8] = &full;
    let mut found = true;
    let mut elem: &[u8] = b"";
    while found {
        let (e, rest, f) = cut(remaining, b'/');
        elem = e;
        remaining = rest;
        found = f;
        if elem == b"." {
            first = false;
            continue;
        }
        if elem == b".." {
            let s = dst[1..].to_vec();
            let index = s.iter().rposition(|&c| c == b'/');
            dst.clear();
            dst.push(b'/');
            match index {
                None => first = true,
                Some(i) => dst.extend_from_slice(&s[..i]),
            }
        } else {
            if !first {
                dst.push(b'/');
            }
            dst.extend_from_slice(elem);
            first = false;
        }
    }
    if elem == b"." || elem == b".." {
        dst.push(b'/');
    }
    if dst.len() > 1 && dst[1] == b'/' {
        dst.remove(0);
    }
    dst
}

#[cfg(test)]
mod tests {
    use super::*;

    fn res(base: &str, r: &str) -> String {
        let b = parse(base.as_bytes()).unwrap();
        String::from_utf8(b.parse_ref(r.as_bytes()).unwrap().string()).unwrap()
    }

    // 數值取自 Go 的 url_test.go 的 resolveReferenceTests(RFC 3986 第 5.4 節的例子)。
    #[test]
    fn rfc3986_examples() {
        let base = "http://a/b/c/d;p?q";
        for (r, want) in [
            ("g:h", "g:h"),
            ("g", "http://a/b/c/g"),
            ("./g", "http://a/b/c/g"),
            ("g/", "http://a/b/c/g/"),
            ("/g", "http://a/g"),
            ("//g", "http://g"),
            ("?y", "http://a/b/c/d;p?y"),
            ("g?y", "http://a/b/c/g?y"),
            ("#s", "http://a/b/c/d;p?q#s"),
            ("g#s", "http://a/b/c/g#s"),
            ("g?y#s", "http://a/b/c/g?y#s"),
            (";x", "http://a/b/c/;x"),
            ("g;x", "http://a/b/c/g;x"),
            ("g;x?y#s", "http://a/b/c/g;x?y#s"),
            ("", "http://a/b/c/d;p?q"),
            (".", "http://a/b/c/"),
            ("./", "http://a/b/c/"),
            ("..", "http://a/b/"),
            ("../", "http://a/b/"),
            ("../g", "http://a/b/g"),
            ("../..", "http://a/"),
            ("../../", "http://a/"),
            ("../../g", "http://a/g"),
            ("../../../g", "http://a/g"),
            ("../../../../g", "http://a/g"),
            ("/./g", "http://a/g"),
            ("/../g", "http://a/g"),
            ("g.", "http://a/b/c/g."),
            (".g", "http://a/b/c/.g"),
            ("g..", "http://a/b/c/g.."),
            ("..g", "http://a/b/c/..g"),
            ("./../g", "http://a/b/g"),
            ("./g/.", "http://a/b/c/g/"),
            ("g/./h", "http://a/b/c/g/h"),
            ("g/../h", "http://a/b/c/h"),
            ("g;x=1/./y", "http://a/b/c/g;x=1/y"),
            ("g;x=1/../y", "http://a/b/c/y"),
        ] {
            assert_eq!(res(base, r), want, "ref {r:?}");
        }
    }

    #[test]
    fn escapes_like_go() {
        assert_eq!(res("http://x/a/", "b c.html"), "http://x/a/b%20c.html");
        assert_eq!(res("http://x/a/", "中.html"), "http://x/a/%E4%B8%AD.html");
        assert_eq!(res("http://x/a/", "p?q=中 文"), "http://x/a/p?q=中 文");
        assert!(parse(b"http://x/a\nb").is_err());
        assert!(parse(b"http://x:y/").is_err());
        assert_eq!(
            String::from_utf8(parse(b"HTTP://Ex.org").unwrap().string()).unwrap(),
            "http://Ex.org"
        );
    }
}
