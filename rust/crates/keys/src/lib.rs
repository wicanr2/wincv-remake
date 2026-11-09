//! 與後端無關的按鍵表示(對應 Go 版 `internal/keys`)。
//!
//! app 層依賴這個而不是視窗函式庫,所以按鍵行為可以完全用測試驅動。

use std::fmt;

use wincv_i18n::tr;

/// 具名按鍵。一般字元用 [`Code::Rune`] 加 [`Key::r`] 帶。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Default)]
pub enum Code {
    #[default]
    Rune,
    Up,
    Down,
    Left,
    Right,
    PgUp,
    PgDn,
    Home,
    End,
    Enter,
    Backspace,
    Esc,
    Delete,
    Insert,
    Tab,
    F1,
    F2,
    F3,
    F4,
    F5,
    F6,
    F7,
    F8,
    F9,
    F10,
    F11,
    F12,
    /// 單按一下 Alt:按下再放開,中間沒有按別的鍵。與 [`Key::alt`]
    /// 修飾旗標是兩回事,那是「按著 Alt 再按別的鍵」。
    MenuKey,
}

const CODE_NAMES: &[(Code, &str)] = &[
    (Code::Up, "Up"), (Code::Down, "Down"), (Code::Left, "Left"), (Code::Right, "Right"),
    (Code::PgUp, "PgUp"), (Code::PgDn, "PgDn"), (Code::Home, "Home"), (Code::End, "End"),
    (Code::Enter, "Enter"), (Code::Backspace, "BackSpace"), (Code::Esc, "Esc"),
    (Code::Delete, "Del"), (Code::Insert, "Ins"), (Code::Tab, "Tab"),
    (Code::F1, "F1"), (Code::F2, "F2"), (Code::F3, "F3"), (Code::F4, "F4"), (Code::F5, "F5"),
    (Code::F6, "F6"), (Code::F7, "F7"), (Code::F8, "F8"), (Code::F9, "F9"), (Code::F10, "F10"),
    (Code::F11, "F11"), (Code::F12, "F12"),
    (Code::MenuKey, "Alt"),
];

/// 一次按鍵事件。
#[derive(Clone, Copy, Debug, PartialEq, Eq, Hash, Default)]
pub struct Key {
    pub code: Code,
    /// `code == Code::Rune` 時有意義。
    pub r: char,
    pub alt: bool,
    pub ctrl: bool,
    pub shift: bool,
}

impl Key {
    /// 純字元按鍵。
    pub fn ch(r: char) -> Key {
        Key { code: Code::Rune, r, ..Key::default() }
    }
    /// 按著 Alt 的字元按鍵。
    pub fn alt_ch(r: char) -> Key {
        Key { alt: true, ..Key::ch(r) }
    }
    /// 按著 Ctrl 的字元按鍵。
    pub fn ctrl_ch(r: char) -> Key {
        Key { ctrl: true, ..Key::ch(r) }
    }
    /// 具名按鍵。
    pub fn named(c: Code) -> Key {
        Key { code: c, ..Key::default() }
    }

    /// 對應的英文字母(大寫),不是字母回 None。
    ///
    /// 原版的單鍵指令不分大小寫,比對時一律轉大寫。
    pub fn letter(&self) -> Option<char> {
        if self.code != Code::Rune {
            return None;
        }
        let r = self.r.to_ascii_uppercase();
        r.is_ascii_uppercase().then_some(r)
    }
}

impl fmt::Display for Key {
    /// 格式與 docs/ui/keymap.md 一致,例如 `Ctrl-O`、`Alt-F4`、`Shift-Tab`。
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        if self.ctrl {
            f.write_str("Ctrl-")?;
        }
        if self.alt {
            f.write_str("Alt-")?;
        }
        if self.shift && self.code != Code::Rune {
            f.write_str("Shift-")?;
        }
        if self.code == Code::Rune {
            write!(f, "{}", self.r)
        } else {
            let n = CODE_NAMES.iter().find(|(c, _)| *c == self.code).map_or("?", |(_, n)| n);
            f.write_str(n)
        }
    }
}

/// 把 `F1`、`C-o`、`A-e`、`Down`、`a` 這種寫法變成 Key。
///
/// 短的寫法打起來快,長的(`Ctrl-O`)是 Display 的輸出,也是 keymap 文件
/// 裡的寫法,兩種都收。
pub fn parse(s: &str) -> Option<Key> {
    let mut k = Key::default();
    let mut s = s;
    loop {
        if let Some(r) = s.strip_prefix("Ctrl-") {
            k.ctrl = true;
            s = r;
        } else if let Some(r) = s.strip_prefix("Alt-") {
            k.alt = true;
            s = r;
        } else if let Some(r) = s.strip_prefix("Shift-") {
            k.shift = true;
            s = r;
        } else if let Some(r) = s.strip_prefix("C-") {
            k.ctrl = true;
            s = r;
        } else if let Some(r) = s.strip_prefix("A-") {
            k.alt = true;
            s = r;
        } else if let Some(r) = s.strip_prefix("S-") {
            k.shift = true;
            s = r;
        } else {
            break;
        }
    }
    if s.is_empty() {
        return None;
    }
    if s == "Space" {
        k.code = Code::Rune;
        k.r = ' ';
        return Some(k);
    }
    if let Some((c, _)) = CODE_NAMES.iter().find(|(_, n)| *n == s) {
        k.code = *c;
        return Some(k);
    }
    let mut it = s.chars();
    let (Some(r), None) = (it.next(), it.next()) else {
        return None;
    };
    k.code = Code::Rune;
    k.r = r;
    Some(k)
}

/// 解析以逗號分隔的一串按鍵。
pub fn parse_all(s: &str) -> Result<Vec<Key>, String> {
    let mut out = Vec::new();
    for part in s.split(',') {
        let part = part.trim();
        if part.is_empty() {
            continue;
        }
        match parse(part) {
            Some(k) => out.push(k),
            None => return Err(tr!("看不懂的按鍵: %q", part)),
        }
    }
    Ok(out)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_cases() {
        for (inp, want) in [
            ("a", Key::ch('a')),
            ("F1", Key::named(Code::F1)),
            ("F11", Key::named(Code::F11)),
            ("Down", Key::named(Code::Down)),
            ("Enter", Key::named(Code::Enter)),
            ("Space", Key::ch(' ')),
            ("C-o", Key::ctrl_ch('o')),
            ("A-e", Key::alt_ch('e')),
            ("BackSpace", Key::named(Code::Backspace)),
            ("Alt", Key::named(Code::MenuKey)),
        ] {
            assert_eq!(parse(inp), Some(want), "{inp}");
        }
        assert_eq!(parse("Nope"), None);
    }

    #[test]
    fn parse_display_round_trip() {
        for k in [Key::ch('a'), Key::named(Code::F6), Key::ctrl_ch('O'), Key::alt_ch('Z'), Key::named(Code::PgDn)] {
            assert_eq!(parse(&k.to_string()), Some(k), "{k}");
        }
    }

    #[test]
    fn parse_all_cases() {
        let ks = parse_all("F1, Down ,Enter").unwrap();
        assert_eq!(ks, vec![Key::named(Code::F1), Key::named(Code::Down), Key::named(Code::Enter)]);
        assert_eq!(parse_all("F1,???").unwrap_err(), "看不懂的按鍵: \"???\"");
    }

    #[test]
    fn letter_ignores_case() {
        assert_eq!(Key::ch('c').letter(), Some('C'));
        assert_eq!(Key::ch('5').letter(), None);
        assert_eq!(Key::named(Code::F1).letter(), None);
    }
}
