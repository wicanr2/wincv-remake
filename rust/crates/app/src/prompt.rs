//! 畫面底部的一行輸入或詢問(對應 Go 版 `prompt.go`)。
//!
//! 原版用的是真正的對話框(Win32 視窗),remake 全部自繪,所以改成佔一列的輸入列。
//! 功能等價:要嘛輸入一串字,要嘛回答是/否/全部。

use wincv_cell::{is_wide, Color, Screen};
use wincv_i18n::t;
use wincv_keys::{Code, Key};

use crate::App;

type OnDone = Box<dyn FnOnce(&mut App, String)>;
type OnAnswer = Box<dyn FnOnce(&mut App, bool, bool)>;

#[derive(Default)]
pub(crate) struct Prompt {
    pub active: bool,
    pub title: String,
    // 輸入模式
    pub input: Vec<char>,
    pub caret: usize,
    pub on_done: Option<OnDone>,
    // 詢問模式(是 / 否 / 全部)
    /// 顯示「全部」這個選項。
    pub ask_all: bool,
    pub on_answer: Option<OnAnswer>,
}

impl App {
    /// 現在是不是在等使用者輸入。
    pub fn prompting(&self) -> bool {
        self.prompt.active
    }

    /// 開一個輸入列。
    pub(crate) fn ask(&mut self, title: &str, initial: &str, on_done: impl FnOnce(&mut App, String) + 'static) {
        let input: Vec<char> = initial.chars().collect();
        self.prompt = Prompt {
            active: true,
            title: title.to_string(),
            caret: input.len(),
            input,
            on_done: Some(Box::new(on_done)),
            ..Default::default()
        };
    }

    /// 開一個是/否詢問。all 為 true 時多一個「全部」選項。
    pub(crate) fn confirm(&mut self, title: &str, all: bool, on_answer: impl FnOnce(&mut App, bool, bool) + 'static) {
        self.prompt = Prompt {
            active: true,
            title: title.to_string(),
            ask_all: all,
            on_answer: Some(Box::new(on_answer)),
            ..Default::default()
        };
    }

    pub(crate) fn close_prompt(&mut self) {
        self.prompt = Prompt::default();
    }

    /// 處理輸入列的按鍵。輸入中或詢問中,其他鍵一律吃掉。
    pub(crate) fn prompt_key(&mut self, k: Key) -> bool {
        if self.prompt.on_answer.is_some() {
            let answer = match k.letter() {
                Some('Y') => Some((true, false)),
                Some('N') => Some((false, false)),
                Some('A') if self.prompt.ask_all => Some((true, true)),
                // Esc = 全部都不要,不要一個一個問下去。
                _ if k.code == Code::Esc => Some((false, true)),
                _ => None,
            };
            if let Some((yes, all)) = answer {
                let f = self.prompt.on_answer.take().unwrap();
                self.close_prompt();
                f(self, yes, all);
            }
            return true;
        }
        let p = &mut self.prompt;
        match k.code {
            Code::Enter => {
                let (f, v) = (p.on_done.take(), p.input.iter().collect::<String>());
                self.close_prompt();
                if let Some(f) = f {
                    f(self, v);
                }
            }
            Code::Esc => self.close_prompt(),
            Code::Backspace => {
                if p.caret > 0 {
                    p.input.remove(p.caret - 1);
                    p.caret -= 1;
                }
            }
            Code::Delete => {
                if p.caret < p.input.len() {
                    p.input.remove(p.caret);
                }
            }
            Code::Left => p.caret = p.caret.saturating_sub(1),
            Code::Right => p.caret = (p.caret + 1).min(p.input.len()),
            Code::Home => p.caret = 0,
            Code::End => p.caret = p.input.len(),
            Code::Rune if k.r as u32 >= 0x20 && !k.ctrl && !k.alt => {
                p.input.insert(p.caret, k.r);
                p.caret += 1;
            }
            _ => {}
        }
        true
    }

    /// 畫輸入列。佔最後一列,蓋掉狀態列。
    pub(crate) fn draw_prompt(&self, s: &mut Screen) {
        let p = &self.prompt;
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', Color::BLACK, Color::LT_CYAN);
        let x = s.print(0, y, &p.title, Color::BLACK, Color::LT_CYAN);
        if p.on_answer.is_some() {
            let mut opts = t("  Y 是   N 否");
            if p.ask_all {
                opts += &t("   A 全部");
            }
            s.print(x, y, &opts, Color::BLUE, Color::LT_CYAN);
            return;
        }
        let x = x + 1;
        let input: String = p.input.iter().collect();
        s.print(x, y, &input, Color::BLACK, Color::LT_CYAN);
        // 游標
        let cx = x + p.input[..p.caret].iter().map(|&r| if is_wide(r) { 2 } else { 1 }).sum::<i32>();
        if let Some(c) = s.at_mut(cx, y) {
            std::mem::swap(&mut c.fg, &mut c.bg);
        }
    }
}
