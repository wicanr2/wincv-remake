//! 視窗的按鍵事件轉成 `wincv_keys::Key`(對應 Go 版 `internal/ebikeys`)。
//!
//! 一般模式看**實體按鍵**(與鍵盤配置無關,A 鍵就是 A),與 Go 版相同;編輯器與輸入列
//! 要打得出 shift 後的符號與中文,所以改收字元(含輸入法送來的字)。

use winit::event::{ElementState, KeyEvent};
use winit::keyboard::{KeyCode, ModifiersState, PhysicalKey};

use wincv_keys::{Code, Key};

#[derive(Default)]
pub struct Keys {
    mods: ModifiersState,
    /// 單按一下 Alt:按下之後還沒有按過別的鍵、沒有點過滑鼠。
    alt_armed: bool,
}

fn named(k: KeyCode) -> Option<Code> {
    Some(match k {
        KeyCode::ArrowUp => Code::Up,
        KeyCode::ArrowDown => Code::Down,
        KeyCode::ArrowLeft => Code::Left,
        KeyCode::ArrowRight => Code::Right,
        KeyCode::PageUp => Code::PgUp,
        KeyCode::PageDown => Code::PgDn,
        KeyCode::Home => Code::Home,
        KeyCode::End => Code::End,
        KeyCode::Enter | KeyCode::NumpadEnter => Code::Enter,
        KeyCode::Backspace => Code::Backspace,
        KeyCode::Escape => Code::Esc,
        KeyCode::Delete => Code::Delete,
        KeyCode::Insert => Code::Insert,
        KeyCode::Tab => Code::Tab,
        KeyCode::F1 => Code::F1,
        KeyCode::F2 => Code::F2,
        KeyCode::F3 => Code::F3,
        KeyCode::F4 => Code::F4,
        KeyCode::F5 => Code::F5,
        KeyCode::F6 => Code::F6,
        KeyCode::F7 => Code::F7,
        KeyCode::F8 => Code::F8,
        KeyCode::F9 => Code::F9,
        KeyCode::F10 => Code::F10,
        KeyCode::F11 => Code::F11,
        KeyCode::F12 => Code::F12,
        _ => return None,
    })
}

/// 會自動重複的鍵。按住捲動是預期的;按住 Enter 一路開檔不是。
fn repeats(c: Code) -> bool {
    matches!(c, Code::Up | Code::Down | Code::Left | Code::Right | Code::PgUp | Code::PgDn | Code::Backspace | Code::Delete)
}

/// 實體按鍵對到的字元,與 Go 版 runeOf 相同的那幾顆。
fn rune_of(k: KeyCode) -> Option<char> {
    use KeyCode::*;
    let letters = [KeyA, KeyB, KeyC, KeyD, KeyE, KeyF, KeyG, KeyH, KeyI, KeyJ, KeyK, KeyL, KeyM, KeyN, KeyO, KeyP, KeyQ, KeyR, KeyS, KeyT, KeyU, KeyV, KeyW, KeyX, KeyY, KeyZ];
    if let Some(i) = letters.iter().position(|&l| l == k) {
        return Some((b'A' + i as u8) as char);
    }
    let digits = [Digit0, Digit1, Digit2, Digit3, Digit4, Digit5, Digit6, Digit7, Digit8, Digit9];
    if let Some(i) = digits.iter().position(|&d| d == k) {
        return Some((b'0' + i as u8) as char);
    }
    Some(match k {
        Space => ' ',
        Semicolon => ';',
        Slash => '/',
        Backslash => '\\',
        Period => '.',
        Comma => ',',
        Minus => '-',
        Equal => '=',
        _ => return None,
    })
}

fn is_alt(k: PhysicalKey) -> bool {
    matches!(k, PhysicalKey::Code(KeyCode::AltLeft | KeyCode::AltRight))
}

impl Keys {
    pub fn modifiers(&mut self, m: ModifiersState) {
        self.mods = m;
        if m.control_key() || m.shift_key() {
            self.alt_armed = false;
        }
    }

    /// 點滑鼠會取消「單按 Alt」。
    pub fn mouse_pressed(&mut self) {
        self.alt_armed = false;
    }

    /// 一次按鍵事件轉成零到多個 Key。text_mode 為真時(編輯器、輸入列)一般字元改收文字。
    pub fn key_event(&mut self, ev: &KeyEvent, text_mode: bool) -> Vec<Key> {
        let (alt, ctrl, shift) = (self.mods.alt_key(), self.mods.control_key(), self.mods.shift_key());
        let mut out = Vec::new();
        if is_alt(ev.physical_key) {
            match ev.state {
                ElementState::Pressed if !ev.repeat => self.alt_armed = !ctrl && !shift,
                ElementState::Released if self.alt_armed => {
                    // 按下 Alt 再放開,中間沒有按別的鍵:原版 Win32 的選單鍵。
                    self.alt_armed = false;
                    out.push(Key::named(Code::MenuKey));
                }
                _ => {}
            }
            return out;
        }
        if ev.state != ElementState::Pressed {
            return out;
        }
        self.alt_armed = false;
        let PhysicalKey::Code(code) = ev.physical_key else { return out };
        if let Some(c) = named(code) {
            if !ev.repeat || repeats(c) {
                out.push(Key { code: c, r: '\0', alt, ctrl, shift });
            }
            return out;
        }
        if text_mode && !alt && !ctrl {
            // 這一顆交給文字處理。輸入法組字時沒有 text,字會從 Ime::Commit 送來。
            if let Some(t) = &ev.text {
                out.extend(t.chars().filter(|&r| r as u32 >= 0x20 && r as u32 != 0x7F).map(|r| Key { shift, ..Key::ch(r) }));
            }
            return out;
        }
        if ev.repeat {
            return out;
        }
        if let Some(r) = rune_of(code) {
            out.push(Key { code: Code::Rune, r, alt, ctrl, shift });
        }
        out
    }
}
