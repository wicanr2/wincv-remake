//! 編輯器的 F6 尋找/取代與 Ctrl-E 註解(對應 Go 版 `editfind.go`)。

use wincv_editor::{BlockKind, Pos};
use wincv_i18n::{t, tr};

use crate::App;

#[derive(Clone, Default)]
pub(crate) struct FindState {
    pub pattern: String,
    pub with: String,
    /// 有輸入取代字串。
    pub replace: bool,
    pub at: Pos,
}

impl App {
    pub(crate) fn start_edit_find(&mut self) -> bool {
        let init = self.edit_find.pattern.clone();
        self.ask(&t("尋找:"), &init, |a, pat| {
            if pat.is_empty() {
                return;
            }
            a.ask(&t("取代為(直接按 Enter = 只尋找):"), "", move |a, with| {
                let at = a.editor.as_ref().map(|e| e.cur).unwrap_or_default();
                a.edit_find = FindState { pattern: pat, replace: !with.is_empty(), with, at };
                a.edit_find_next(false);
            });
        });
        true
    }

    pub(crate) fn edit_find_next(&mut self, from_cursor: bool) {
        let f = self.edit_find.clone();
        if f.pattern.is_empty() {
            self.message = t("還沒設定要找什麼(F6)");
            return;
        }
        let Some(e) = self.editor.as_mut() else { return };
        let from = if from_cursor { e.cur } else { f.at };
        let Some(at) = e.find(&f.pattern, from, false) else {
            self.message = tr!("找不到 %q(已到檔尾)", &f.pattern);
            self.edit_find.at = Pos::default();
            return;
        };
        e.move_to(at);
        let plen = f.pattern.chars().count();
        self.edit_find.at = Pos { line: at.line, col: at.col + plen };
        if !f.replace {
            return;
        }
        self.confirm(&tr!("取代成 %q?", &f.with), true, move |a, yes, all| {
            let e = a.editor.as_mut().unwrap();
            if all && yes {
                let n = e.replace_all(&f.pattern, &f.with, false);
                a.message = tr!("已取代 %d 處", n);
                a.edit_find.at = Pos::default();
            } else if all {
                // Esc:停在這裡,不再問。
                a.message = t("已停止取代");
            } else if yes {
                e.replace(at, plen, &f.with);
                a.edit_find.at = Pos { line: at.line, col: at.col + f.with.chars().count() };
                a.edit_find_next(false);
            } else {
                a.edit_find_next(false);
            }
        });
    }

    pub(crate) fn edit_comment(&mut self) -> bool {
        let Some(e) = self.editor.as_mut() else { return false };
        let (mut top, mut bot) = (e.cur.line, e.cur.line);
        if e.block.kind != BlockKind::None {
            let (t, b, _, _) = e.block.norm();
            (top, bot) = (t, b);
        }
        if !e.comment_lines(top, bot) {
            self.message = match &e.syntax {
                None => t("這個副檔名沒有語法設定,不知道註解記號是什麼"),
                Some(s) => s.name.clone() + &t(" 沒有定義行註解記號"),
            };
        }
        true
    }
}
