//! 解壓縮、打包、尋找、轉換(對應 Go 版 `actions.go`)。

use std::ffi::OsString;
use std::path::{Path, PathBuf};

use wincv_archive::ArchiveFs;
use wincv_cell::{Color, Screen};
use wincv_i18n::{t, tr};
use wincv_keys::{Code, Key};
use wincv_search::{Hit, Kind};
use wincv_textenc::Enc;

use crate::{cell_width, App, Mode};

// --- Z 解壓縮 -------------------------------------------------------------

impl App {
    /// 解壓縮。兩種情境:游標停在壓縮檔上,或者人已經在壓縮檔裡面。原版的 Z 兩種都吃。
    pub(crate) fn start_extract(&mut self) -> bool {
        if let Some(af) = self.archive() {
            let base = af.inner(&self.browser.dir);
            let inner: Vec<Vec<u8>> = self
                .targets()
                .iter()
                .map(|(_, os)| {
                    let n = wincv_archive::path_bytes(Path::new(os));
                    if base.is_empty() {
                        n
                    } else {
                        [base.as_slice(), b"/", &n].concat()
                    }
                })
                .collect();
            let name = af.name().to_path_buf();
            let outer = name.parent().map(Path::to_path_buf).unwrap_or_default();
            self.ask(&tr!("解 %d 個項目到:", inner.len()), &outer.to_string_lossy(), move |a, dst| {
                // 壓縮檔要重開:輸入列是非同步的,這段時間裡列表可能已經換了壓縮檔。
                match ArchiveFs::open(&name) {
                    Ok(af) => a.run_extract(&af, &dst, &inner),
                    Err(e) => a.message = e.to_string(),
                }
            });
            return true;
        }
        let Some(e) = self.browser.current().filter(|e| !e.is_dir && wincv_archive::is_archive(Path::new(&e.name))) else {
            self.message = t("游標要停在壓縮檔上");
            return true;
        };
        let (name, os) = (e.name.clone(), e.os_name.clone());
        let full = self.browser.dir.join(&os);
        let af = match ArchiveFs::open(&full) {
            Ok(af) => af,
            Err(e) => {
                self.message = e.to_string();
                return true;
            }
        };
        // Go 的 strings.TrimSuffix(name, filepath.Ext(name))。
        let base = match name.rfind('.') {
            Some(i) if !name[i..].contains('/') => &name[..i],
            _ => &name,
        };
        let def = self.browser.dir.join(base);
        self.ask(&t("解壓縮到:"), &def.to_string_lossy(), move |a, dst| a.run_extract(&af, &dst, &[]));
        true
    }

    /// names 是空的表示整個壓縮檔。
    fn run_extract(&mut self, af: &ArchiveFs, dst: &str, names: &[Vec<u8>]) {
        if dst.is_empty() {
            return;
        }
        if let Err(e) = std::fs::create_dir_all(dst) {
            self.message = t("目的目錄建不起來: ") + &e.to_string();
            return;
        }
        // 失敗時要報「解出幾個之後」:進度回呼每解完一個叫一次,記下最後一個。
        let mut done = 0;
        let r = af.extract(Path::new(dst), names, Some(&mut |_: &[u8], i: usize, _: usize| done = i));
        self.message = match r {
            Err(e) => tr!("解出 %d 個後失敗: %v", done, e.to_string()),
            Ok(n) => tr!("已解出 %d 個檔案到 %s", n, dst),
        };
        let _ = self.browser.reload();
    }

    // --- Alt-Z 製作壓縮檔 -----------------------------------------------------

    pub(crate) fn start_create_archive(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡不能再建壓縮檔");
            return true;
        }
        let ts = self.targets();
        if ts.is_empty() {
            return false;
        }
        let mut def = self.browser.dir.file_name().map(wincv_vfs::display_name).unwrap_or_default() + ".zip";
        if ts.len() == 1 {
            let n = &ts[0].0;
            def = match n.rfind('.') {
                Some(i) => n[..i].to_string(),
                None => n.clone(),
            } + ".zip";
        }
        let names: Vec<PathBuf> = ts.into_iter().map(|t| PathBuf::from(t.1)).collect();
        self.ask(&tr!("把 %d 個項目打包成(只支援 .zip):", names.len()), &def, move |a, out| {
            if out.is_empty() {
                return;
            }
            let out = if out.to_lowercase().ends_with(".zip") { out } else { out + ".zip" };
            let dst = if Path::new(&out).is_absolute() { PathBuf::from(&out) } else { a.browser.dir.join(&out) };
            if let Err(e) = wincv_archive::create_zip(&dst, &a.browser.dir, &names, None) {
                a.message = t("打包失敗: ") + &e.to_string();
                return;
            }
            let base = dst.file_name().map(wincv_vfs::display_name).unwrap_or_default();
            a.message = tr!("已打包 %d 個項目到 %s", names.len(), &base);
            let _ = a.browser.reload();
            a.focus_on(&base);
        });
        true
    }
}

// --- W 尋找 ---------------------------------------------------------------

/// 搜尋結果清單的狀態。
pub struct FindResult {
    pub pattern: String,
    pub kind: Kind,
    pub hits: Vec<Hit>,
    pub cursor: i32,
    pub top: i32,
}

fn kind_label(k: Kind) -> String {
    match k {
        Kind::Name => t("檔名"),
        Kind::Content => t("字串"),
        Kind::Comment => t("註解"),
    }
}

impl App {
    pub(crate) fn start_find(&mut self) -> bool {
        // confirm 只吃 Y/N/A,這裡要的是三選一,所以自己接一個小狀態機。
        self.prompt = crate::prompt::Prompt {
            active: true,
            title: t("尋找  N)檔名  S)字串  C)註解"),
            ..Default::default()
        };
        self.find_kind_pending = true;
        true
    }

    /// 處理「尋找哪一種」那一步。
    pub(crate) fn find_kind_key(&mut self, k: Key) -> bool {
        let kind = match k.letter() {
            Some('N') => Kind::Name,
            Some('S') => Kind::Content,
            Some('C') => Kind::Comment,
            _ => {
                if k.code == Code::Esc {
                    self.find_kind_pending = false;
                    self.close_prompt();
                }
                return true;
            }
        };
        self.find_kind_pending = false;
        self.close_prompt();
        self.ask(&(t("尋找 ") + &kind_label(kind) + ":"), "", move |a, pattern| a.run_find(kind, &pattern));
        true
    }

    fn run_find(&mut self, kind: Kind, pattern: &str) {
        if pattern.is_empty() {
            return;
        }
        if self.read_only_here() {
            self.message = t("壓縮檔裡還不能搜尋");
            return;
        }
        let o = wincv_search::Options { kind, recursive: true, ..Default::default() };
        let hits = match wincv_search::run(&self.browser.dir, pattern, o) {
            Ok(h) => h,
            Err(e) => {
                self.message = t("搜尋失敗: ") + &e.to_string();
                return;
            }
        };
        if hits.is_empty() {
            self.message = tr!("找不到 %q", pattern);
            return;
        }
        self.find = Some(FindResult { pattern: pattern.to_string(), kind, hits, cursor: 0, top: 0 });
        self.mode = Mode::Find;
    }

    pub(crate) fn find_key(&mut self, k: Key) -> bool {
        let rows = self.page_rows();
        let Some(f) = self.find.as_mut() else { return false };
        let n = f.hits.len() as i32;
        let mut mv = |d: i32| {
            f.cursor = (f.cursor + d).max(0).min(n - 1);
            if f.cursor < f.top {
                f.top = f.cursor;
            }
            if f.cursor >= f.top + rows {
                f.top = f.cursor - rows + 1;
            }
            true
        };
        match k.code {
            Code::Up => mv(-1),
            Code::Down => mv(1),
            Code::PgUp => mv(-rows),
            Code::PgDn => mv(rows),
            Code::Home => {
                f.cursor = 0;
                f.top = 0;
                true
            }
            Code::End => mv(n),
            Code::Esc => {
                self.mode = Mode::Browser;
                true
            }
            Code::Enter => {
                let Some(h) = f.hits.get(f.cursor as usize).cloned() else { return false };
                if let Err(e) = self.browser.load(&h.dir) {
                    self.message = e.to_string();
                    return true;
                }
                self.mode = Mode::Browser;
                self.focus_on(&wincv_vfs::display_name(&h.name));
                true
            }
            _ => false,
        }
    }

    pub(crate) fn draw_find(&self, s: &mut Screen) -> i32 {
        let Some(f) = &self.find else { return 0 };
        s.clear(Color::LT_GRAY, Color::BLACK);
        let rows = (s.rows - 2).max(0);
        s.fill(0, 0, s.cols, 1, ' ', Color::LT_CYAN, Color::BLUE);
        s.print(0, 0, &tr!("尋找 %s: %q", kind_label(f.kind), &f.pattern), Color::LT_CYAN, Color::BLUE);
        let right = format!("{} / {}", f.cursor + 1, f.hits.len());
        let x = s.cols - cell_width(&right);
        if x >= 0 {
            s.print(x, 0, &right, Color::LT_CYAN, Color::BLUE);
        }
        for i in 0..rows {
            let idx = (f.top + i) as usize;
            let Some(h) = f.hits.get(idx) else { break };
            let y = 1 + i;
            let name = wincv_vfs::display_name(&h.name);
            let loc = if h.line > 0 { format!("{name}:{}", h.line) } else { name };
            let x = s.print(1, y, &loc, Color::LT_GREEN, Color::BLACK);
            if !h.text.is_empty() {
                s.print(x + 2, y, &printable(&h.text, s.cols - x - 2), Color::LT_GRAY, Color::BLACK);
            } else {
                s.print(x + 2, y, &rel_dir(&self.browser.dir, &h.dir), Color::DK_GRAY, Color::BLACK);
            }
            if idx as i32 == f.cursor {
                s.set_attr(0, y, s.cols, Color::BLACK, Color::LT_GRAY);
            }
        }
        let y = s.rows - 1;
        s.fill(0, y, s.cols, 1, ' ', Color::BLACK, Color::LT_GRAY);
        if let Some(h) = f.hits.get(f.cursor as usize) {
            s.print(0, y, &h.path().to_string_lossy(), Color::BLACK, Color::LT_GRAY);
        }
        rows
    }
}

/// 把一行內容整理成可以直接畫的樣子。
///
/// 編碼判讀不是二分法:像 store.cab 這種「大部分是純文字、夾著標頭」的檔案不會被判成
/// 二進位,於是控制碼會原樣畫到畫面上。列表這一層自己擋掉比去調判讀門檻安全。
pub(crate) fn printable(s: &str, max: i32) -> String {
    if max < 1 {
        return String::new();
    }
    s.chars()
        .take(max as usize)
        .map(|r| match r {
            '\t' => ' ',
            '\u{FFFD}' => '.',
            r if (r as u32) < 0x20 || r as u32 == 0x7F => '.',
            r => r,
        })
        .collect()
}

/// Go 的 filepath.Rel 的簡化:dir 在 base 底下時回相對路徑,否則回 dir 本身。
fn rel_dir(base: &Path, dir: &Path) -> String {
    match dir.strip_prefix(base) {
        Ok(r) if r.as_os_str().is_empty() => String::new(),
        Ok(r) => r.to_string_lossy().into_owned(),
        Err(_) => dir.to_string_lossy().into_owned(),
    }
}

// --- Ctrl-O 轉換 ----------------------------------------------------------

type ConvertFn = fn(&[u8]) -> Vec<u8>;

impl App {
    pub(crate) fn start_convert(&mut self) -> bool {
        if self.read_only_here() {
            self.message = t("壓縮檔裡的檔案還不能轉換");
            return true;
        }
        // 目錄轉不了(讀進來就是失敗),先濾掉再問要轉什麼。
        let names: Vec<OsString> = self
            .targets()
            .into_iter()
            .map(|t| t.1)
            .filter(|n| std::fs::metadata(self.browser.dir.join(n)).is_ok_and(|m| !m.is_dir()))
            .collect();
        if names.is_empty() {
            self.message = t("沒有可以轉換的檔案(目錄不算)");
            return true;
        }
        let title = wincv_i18n::sprintf(
            &(t("轉換 %d 個檔案  U)UNIX換行  P)PC換行  B)轉Big5  ") + &t("G)轉GBK  H)去HTML  A)去ANSI")),
            &[names.len().into()],
        );
        self.convert_pending = Some(names);
        self.prompt = crate::prompt::Prompt { active: true, title, ..Default::default() };
        true
    }

    /// 處理轉換的選單那一步。
    pub(crate) fn convert_key(&mut self, k: Key) -> bool {
        if k.code == Code::Esc {
            self.convert_pending = None;
            self.close_prompt();
            return true;
        }
        let Some((f, label)) = k.letter().and_then(convert_op) else { return true };
        let names = self.convert_pending.take().unwrap_or_default();
        self.close_prompt();
        let (mut ok, mut failed) = (0, 0);
        for n in &names {
            let p = self.browser.dir.join(n);
            let Ok(data) = std::fs::read(&p) else {
                failed += 1;
                continue;
            };
            if std::fs::write(&p, f(&data)).is_err() {
                failed += 1;
                continue;
            }
            ok += 1;
        }
        self.message = tr!("%s:成功 %d 個", label, ok);
        if failed > 0 {
            self.message += &tr!(",失敗 %d 個", failed);
        }
        let _ = self.browser.reload();
        true
    }
}

/// 把選單字母對到轉換函式。
fn convert_op(letter: char) -> Option<(ConvertFn, String)> {
    use wincv_convert::{recode_lossy, strip_ansi, strip_html, to_eol, Eol};
    use wincv_textenc::{decode, detect};
    let f: ConvertFn = match letter {
        'U' => |b| to_eol(b, Eol::Unix),
        'P' => |b| to_eol(b, Eol::Pc),
        'B' => |b| recode_lossy(b, detect(b), Enc::Big5, "?"),
        'G' => |b| recode_lossy(b, detect(b), Enc::Gbk, "?"),
        'H' => |b| {
            let e = detect(b);
            recode_lossy(strip_html(&decode(b, e)).as_bytes(), Enc::Utf8, e, "?")
        },
        'A' => |b| {
            let e = detect(b);
            recode_lossy(strip_ansi(&decode(b, e)).as_bytes(), Enc::Utf8, e, "?")
        },
        _ => return None,
    };
    let label = match letter {
        'U' => t("轉 UNIX 換行"),
        'P' => t("轉 PC 換行"),
        'B' => t("轉 Big5"),
        'G' => t("轉 GBK"),
        'H' => t("去 HTML"),
        _ => t("去 ANSI 色碼"),
    };
    Some((f, label))
}
