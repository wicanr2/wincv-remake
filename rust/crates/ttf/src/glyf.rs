//! TrueType `glyf` 外框,逐段移植自 golang.org/x/image v0.20.0 的
//! `font/sfnt/truetype.go`(`loadGlyf`、`loadCompoundGlyf`、`glyfIter`)。
//!
//! 為什麼不用 ttf-parser 的 `outline_glyph`:複合字形(Ã、Ñ、Ğ 這類「字母 + 附加符號」)
//! 的元件有位移,而 Go 版是**先在元件自己的座標裡算隱含中點(整數除 2,向零截斷),
//! 再加位移**;ttf-parser 先套位移再給浮點數的中點。附加符號在元件座標裡常是負的 x,
//! 兩種順序的截斷方向就不同,差一個字型單位。照搬同一套整數運算,外框才逐段相同。
//!
//! 原始碼的著作權屬於 The Go Authors,以 BSD 授權釋出,聲明見 repo 根目錄的 NOTICE。

use ttf_parser::{Face, GlyphId, Tag};

const MAX_COMPOUND_RECURSION_DEPTH: u32 = 8;
const MAX_COMPOUND_STACK_SIZE: u32 = 64;
const MAX_GLYPH_DATA_LENGTH: usize = 64 * 1024;
const GLYF_HEADER_LEN: usize = 10;

const FLAG_ON_CURVE: u8 = 1 << 0;
const FLAG_X_SHORT_VECTOR: u8 = 1 << 1;
const FLAG_Y_SHORT_VECTOR: u8 = 1 << 2;
const FLAG_REPEAT: u8 = 1 << 3;
const FLAG_POSITIVE_X_SHORT_VECTOR: u8 = 1 << 4;
const FLAG_THIS_X_IS_SAME: u8 = 1 << 4;
const FLAG_POSITIVE_Y_SHORT_VECTOR: u8 = 1 << 5;
const FLAG_THIS_Y_IS_SAME: u8 = 1 << 5;

const FLAG_ARG1_AND_2_ARE_WORDS: u16 = 1 << 0;
const FLAG_ARGS_ARE_XY_VALUES: u16 = 1 << 1;
const FLAG_WE_HAVE_A_SCALE: u16 = 1 << 3;
const FLAG_MORE_COMPONENTS: u16 = 1 << 5;
const FLAG_WE_HAVE_AN_X_AND_Y_SCALE: u16 = 1 << 6;
const FLAG_WE_HAVE_A_TWO_BY_TWO: u16 = 1 << 7;

/// 一段外框,座標是字型單位(Go 版存在 `fixed.Int26_6` 裡,但還沒縮放)。
/// `op`:`b'm'`、`b'l'`、`b'q'`。三個點都會被位移與縮放,沒用到的也一樣(Go 版如此)。
#[derive(Clone, Copy)]
pub(crate) struct GSeg {
    pub op: u8,
    pub p: [(i32, i32); 3],
}

/// glyf 與 loca 兩張表。
pub(crate) struct Glyf<'a> {
    glyf: &'a [u8],
    loca: &'a [u8],
    long: bool,
}

fn u16_at(b: &[u8], i: usize) -> Option<u16> {
    Some(u16::from_be_bytes([*b.get(i)?, *b.get(i + 1)?]))
}

impl<'a> Glyf<'a> {
    /// 字型有 glyf 表才有;CFF 外框的字型回 None。loca 的長度要剛好對上字形數,
    /// 對不上 Go 版整份字型都不收,這裡也回 None 讓呼叫端知道。
    pub fn new(face: &Face<'a>) -> Option<Glyf<'a>> {
        let raw = face.raw_face();
        let glyf = raw.table(Tag::from_bytes(b"glyf"))?;
        let loca = raw.table(Tag::from_bytes(b"loca"))?;
        let n = face.number_of_glyphs() as usize + 1;
        let long = matches!(face.tables().head.index_to_location_format, ttf_parser::head::IndexToLocationFormat::Long);
        if loca.len() != if long { 4 * n } else { 2 * n } {
            return None;
        }
        Some(Glyf { glyf, loca, long })
    }

    /// 一個字形的外框(字型單位)。資料有問題時回 None,與 Go 版 `LoadGlyph` 回錯誤相同。
    pub fn load(&self, gid: GlyphId) -> Option<Vec<GSeg>> {
        let mut out = Vec::new();
        self.load_glyf(gid.0 as usize, 0, 0, &mut out)?;
        Some(out)
    }

    /// loca 的第 x 筆。每次查一筆,不先整張解開(一份字型可以有六萬多個字形)。
    fn loc(&self, x: usize) -> Option<usize> {
        if self.long {
            let b = self.loca.get(4 * x..4 * x + 4)?;
            Some(u32::from_be_bytes([b[0], b[1], b[2], b[3]]) as usize)
        } else {
            Some(2 * u16_at(self.loca, 2 * x)? as usize)
        }
    }

    fn data(&self, x: usize) -> Option<&'a [u8]> {
        let (i, j) = (self.loc(x)?, self.loc(x + 1)?);
        if j < i || j - i > MAX_GLYPH_DATA_LENGTH {
            return None;
        }
        self.glyf.get(i..j)
    }

    fn load_glyf(&self, x: usize, stack_bottom: u32, depth: u32, out: &mut Vec<GSeg>) -> Option<()> {
        let data = self.data(x)?;
        if data.is_empty() {
            return Some(());
        }
        if data.len() < GLYF_HEADER_LEN {
            return None;
        }
        let mut index = GLYF_HEADER_LEN;
        let num_contours = u16_at(data, 0)? as i16;
        let num_points = match num_contours {
            -1 => return self.load_compound(&data[GLYF_HEADER_LEN..], stack_bottom, depth, out),
            0 => return Some(()),
            n if n > 0 => {
                index += 2 * n as usize;
                if index > data.len() {
                    return None;
                }
                // 檔案裡存的是最後一個點的索引(含),所以 +1。
                1 + u16_at(data, index - 2)? as usize
            }
            _ => return None,
        };
        // 跳過 hinting 指令。
        index += 2;
        if index > data.len() {
            return None;
        }
        index += u16_at(data, index - 2)? as usize;
        if index > data.len() {
            return None;
        }
        let (x_index, y_index) = find_xy_indexes(data, index, num_points)?;
        let mut g = Iter {
            data,
            flag_index: index,
            x_index,
            y_index,
            end_index: GLYF_HEADER_LEN,
            prev_end: -1,
            final_end: num_points as i32 - 1,
            c: 0,
            num_contours: num_contours as i32,
            p: 0,
            n_points: 0,
            x: 0,
            y: 0,
            on: false,
            flag: 0,
            repeats: 0,
            closing: false,
            closed: false,
            first_on_valid: false,
            first_off_valid: false,
            last_off_valid: false,
            first_on: (0, 0),
            first_off: (0, 0),
            last_off: (0, 0),
            err: false,
        };
        while g.next_contour() {
            while let Some(s) = g.next_segment() {
                out.push(s);
            }
        }
        (!g.err).then_some(())
    }

    fn load_compound(&self, mut data: &[u8], stack_bottom: u32, depth: u32, out: &mut Vec<GSeg>) -> Option<()> {
        let depth = depth + 1;
        if depth == MAX_COMPOUND_RECURSION_DEPTH {
            return None;
        }
        struct Elem {
            gid: usize,
            dx: i32,
            dy: i32,
            t: Option<[i16; 4]>,
        }
        let mut elems = Vec::new();
        let mut stack_top = stack_bottom;
        loop {
            if stack_top >= MAX_COMPOUND_STACK_SIZE {
                return None;
            }
            stack_top += 1;
            if data.len() < 4 {
                return None;
            }
            let flags = u16_at(data, 0)?;
            let gid = u16_at(data, 2)? as usize;
            let (dx, dy);
            if flags & FLAG_ARG1_AND_2_ARE_WORDS == 0 {
                if data.len() < 6 {
                    return None;
                }
                (dx, dy) = (data[4] as i8 as i32, data[5] as i8 as i32);
                data = &data[6..];
            } else {
                if data.len() < 8 {
                    return None;
                }
                (dx, dy) = (u16_at(data, 4)? as i16 as i32, u16_at(data, 6)? as i16 as i32);
                data = &data[8..];
            }
            if flags & FLAG_ARGS_ARE_XY_VALUES == 0 {
                return None; // 以點對位的元件,Go 版不支援。
            }
            let rd = |d: &[u8], i: usize| u16_at(d, i).map(|v| v as i16);
            let t = if flags & FLAG_WE_HAVE_A_SCALE != 0 {
                let s = rd(data, 0)?;
                data = &data[2..];
                Some([s, 0, 0, s])
            } else if flags & FLAG_WE_HAVE_AN_X_AND_Y_SCALE != 0 {
                let (sx, sy) = (rd(data, 0)?, rd(data, 2)?);
                data = &data[4..];
                Some([sx, 0, 0, sy])
            } else if flags & FLAG_WE_HAVE_A_TWO_BY_TWO != 0 {
                let m = [rd(data, 0)?, rd(data, 2)?, rd(data, 4)?, rd(data, 6)?];
                data = &data[8..];
                Some(m)
            } else {
                None
            };
            elems.push(Elem { gid, dx, dy, t });
            if flags & FLAG_MORE_COMPONENTS == 0 {
                break;
            }
        }
        for e in elems {
            let base = out.len();
            self.load_glyf(e.gid, stack_top, depth, out)?;
            for s in &mut out[base..] {
                for p in &mut s.p {
                    *p = match e.t {
                        Some([txx, txy, tyx, tyy]) => tform(txx, txy, tyx, tyy, e.dx, e.dy, *p),
                        None => (p.0.wrapping_add(e.dx), p.1.wrapping_add(e.dy)),
                    };
                }
            }
        }
        Some(())
    }
}

/// 2.14 定點的仿射變換,捨入方式照 Go 版 `tform`。
fn tform(txx: i16, txy: i16, tyx: i16, tyy: i16, dx: i32, dy: i32, p: (i32, i32)) -> (i32, i32) {
    const HALF: i64 = 1 << 13;
    let m = |a: i32, t: i16| ((a as i64 * t as i64 + HALF) >> 14) as i32;
    (dx.wrapping_add(m(p.0, txx)).wrapping_add(m(p.1, tyx)), dy.wrapping_add(m(p.0, txy)).wrapping_add(m(p.1, tyy)))
}

fn find_xy_indexes(data: &[u8], mut index: usize, num_points: usize) -> Option<(usize, usize)> {
    let (mut x_len, mut y_len) = (0usize, 0usize);
    let mut i = 0;
    loop {
        if i > num_points {
            return None;
        }
        if i == num_points {
            break;
        }
        let mut repeat = 1;
        let flag = *data.get(index)?;
        index += 1;
        if flag & FLAG_REPEAT != 0 {
            repeat += *data.get(index)? as usize;
            index += 1;
        }
        let xs = if flag & FLAG_X_SHORT_VECTOR != 0 {
            1
        } else if flag & FLAG_THIS_X_IS_SAME == 0 {
            2
        } else {
            0
        };
        let ys = if flag & FLAG_Y_SHORT_VECTOR != 0 {
            1
        } else if flag & FLAG_THIS_Y_IS_SAME == 0 {
            2
        } else {
            0
        };
        x_len += xs * repeat;
        y_len += ys * repeat;
        i += repeat;
    }
    if index + x_len + y_len > data.len() {
        return None;
    }
    Some((index, index + x_len))
}

fn mid(p: (i32, i32), q: (i32, i32)) -> (i32, i32) {
    // Go 的整數除法向零截斷,Rust 的 `/` 也是。
    ((p.0 + q.0) / 2, (p.1 + q.1) / 2)
}

/// Go 版的 `glyfIter`:把「點」(on-curve / off-curve)轉成「線段」。
struct Iter<'a> {
    data: &'a [u8],
    flag_index: usize,
    x_index: usize,
    y_index: usize,
    end_index: usize,
    prev_end: i32,
    final_end: i32,
    c: i32,
    num_contours: i32,
    p: i32,
    n_points: i32,
    x: i16,
    y: i16,
    on: bool,
    flag: u8,
    repeats: u8,
    closing: bool,
    closed: bool,
    first_on_valid: bool,
    first_off_valid: bool,
    last_off_valid: bool,
    first_on: (i32, i32),
    first_off: (i32, i32),
    last_off: (i32, i32),
    err: bool,
}

fn seg(op: u8, a: (i32, i32), b: (i32, i32)) -> GSeg {
    GSeg { op, p: [a, b, (0, 0)] }
}

impl Iter<'_> {
    fn next_contour(&mut self) -> bool {
        if self.c == self.num_contours {
            if self.prev_end != self.final_end {
                self.err = true;
            }
            return false;
        }
        self.c += 1;
        let Some(end) = u16_at(self.data, self.end_index) else {
            self.err = true;
            return false;
        };
        let end = end as i32;
        self.end_index += 2;
        if end <= self.prev_end || self.final_end < end {
            self.err = true;
            return false;
        }
        self.n_points = end - self.prev_end;
        self.p = 0;
        self.prev_end = end;
        self.closing = false;
        self.closed = false;
        self.first_on_valid = false;
        self.first_off_valid = false;
        self.last_off_valid = false;
        true
    }

    fn close(&mut self) -> GSeg {
        match (self.first_off_valid, self.last_off_valid) {
            (false, false) => {
                self.closed = true;
                seg(b'l', self.first_on, (0, 0))
            }
            (false, true) => {
                self.closed = true;
                seg(b'q', self.last_off, self.first_on)
            }
            (true, false) => {
                self.closed = true;
                seg(b'q', self.first_off, self.first_on)
            }
            (true, true) => {
                self.last_off_valid = false;
                seg(b'q', self.last_off, mid(self.last_off, self.first_off))
            }
        }
    }

    fn next_segment(&mut self) -> Option<GSeg> {
        while !self.closed {
            if self.closing || !self.next_point() {
                self.closing = true;
                return Some(self.close());
            }
            let p = (self.x as i32, self.y as i32);
            if !self.first_on_valid {
                if self.on {
                    self.first_on = p;
                    self.first_on_valid = true;
                    return Some(seg(b'm', p, (0, 0)));
                } else if !self.first_off_valid {
                    self.first_off = p;
                    self.first_off_valid = true;
                    continue;
                } else {
                    self.first_on = mid(self.first_off, p);
                    self.first_on_valid = true;
                    self.last_off = p;
                    self.last_off_valid = true;
                    return Some(seg(b'm', self.first_on, (0, 0)));
                }
            } else if !self.last_off_valid {
                if !self.on {
                    self.last_off = p;
                    self.last_off_valid = true;
                    continue;
                }
                return Some(seg(b'l', p, (0, 0)));
            } else if !self.on {
                let s = seg(b'q', self.last_off, mid(self.last_off, p));
                self.last_off = p;
                self.last_off_valid = true;
                return Some(s);
            } else {
                let s = seg(b'q', self.last_off, p);
                self.last_off_valid = false;
                return Some(s);
            }
        }
        None
    }

    fn next_point(&mut self) -> bool {
        if self.p == self.n_points {
            return false;
        }
        self.p += 1;
        let d = self.data;
        if self.repeats > 0 {
            self.repeats -= 1;
        } else {
            // 索引在 find_xy_indexes 已經檢查過範圍。
            self.flag = d[self.flag_index];
            self.flag_index += 1;
            if self.flag & FLAG_REPEAT != 0 {
                self.repeats = d[self.flag_index];
                self.flag_index += 1;
            }
        }
        if self.flag & FLAG_X_SHORT_VECTOR != 0 {
            let v = d[self.x_index] as i16;
            self.x = if self.flag & FLAG_POSITIVE_X_SHORT_VECTOR != 0 { self.x.wrapping_add(v) } else { self.x.wrapping_sub(v) };
            self.x_index += 1;
        } else if self.flag & FLAG_THIS_X_IS_SAME == 0 {
            self.x = self.x.wrapping_add(u16::from_be_bytes([d[self.x_index], d[self.x_index + 1]]) as i16);
            self.x_index += 2;
        }
        if self.flag & FLAG_Y_SHORT_VECTOR != 0 {
            let v = d[self.y_index] as i16;
            self.y = if self.flag & FLAG_POSITIVE_Y_SHORT_VECTOR != 0 { self.y.wrapping_add(v) } else { self.y.wrapping_sub(v) };
            self.y_index += 1;
        } else if self.flag & FLAG_THIS_Y_IS_SAME == 0 {
            self.y = self.y.wrapping_add(u16::from_be_bytes([d[self.y_index], d[self.y_index + 1]]) as i16);
            self.y_index += 2;
        }
        self.on = self.flag & FLAG_ON_CURVE != 0;
        true
    }
}
