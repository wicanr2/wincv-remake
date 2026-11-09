//! LHA 的位元讀取器與靜態 Huffman 解碼。表的建法與碼的指派順序照
//! LHA 原始碼,換一種建法就對不上原檔。

use std::io::Read;

use crate::Error;

const MAX_MATCH: usize = 256;
pub(crate) const THRESHOLD: usize = 3;
/// 字碼表大小:256 個位元組值加 (MAX_MATCH - THRESHOLD + 1) 種長度 = 510。
///
/// [雷] 少算一個(509)只有在某個區塊剛好用滿全部字碼時才現形:讀碼長表
/// 的迴圈以 NC 為上限,少一格就漏掉最後一個字碼,Huffman 權重和變成
/// 65024 而不是 65536。小檔案全部正常。
const NC: usize = 255 + MAX_MATCH + 2 - THRESHOLD;
const NT: usize = 16 + 3;
const CBIT: u32 = 9;
const TBIT: u32 = 5;
const NPT: usize = 0x80;

/// 一次看 16 個位元(碼長最長 16),MSB 優先;讀到檔尾之後補 0。
pub(crate) struct BitReader<R> {
    r: R,
    buf: Box<[u8; 4096]>,
    n: usize,
    i: usize,
    eof: bool,
    pub(crate) bitbuf: u32,
    subbitbuf: u32,
    bitcount: u32,
}

impl<R: Read> BitReader<R> {
    pub(crate) fn new(r: R) -> Self {
        let mut b = BitReader { r, buf: Box::new([0; 4096]), n: 0, i: 0, eof: false, bitbuf: 0, subbitbuf: 0, bitcount: 0 };
        b.fill(16);
        b
    }

    fn byte_at(&mut self) -> u32 {
        if self.i >= self.n {
            if self.eof {
                return 0;
            }
            let n = loop {
                match self.r.read(&mut self.buf[..]) {
                    Ok(n) => break n,
                    Err(e) if e.kind() == std::io::ErrorKind::Interrupted => continue,
                    Err(_) => break 0,
                }
            };
            if n == 0 {
                self.eof = true;
                return 0;
            }
            self.n = n;
            self.i = 0;
        }
        let c = self.buf[self.i] as u32;
        self.i += 1;
        c
    }

    pub(crate) fn fill(&mut self, mut n: u32) {
        while n > self.bitcount {
            n -= self.bitcount;
            self.bitbuf = (self.bitbuf << self.bitcount) & 0xFFFF;
            if self.bitcount > 0 {
                self.bitbuf |= self.subbitbuf >> (8 - self.bitcount);
            }
            self.subbitbuf = self.byte_at();
            self.bitcount = 8;
        }
        self.bitcount -= n;
        self.bitbuf = (self.bitbuf << n) & 0xFFFF;
        if n > 0 {
            self.bitbuf |= (self.subbitbuf >> (8 - n)) & ((1 << n) - 1);
        }
        self.subbitbuf = (self.subbitbuf << n) & 0xFF;
    }

    pub(crate) fn peek(&self, n: u32) -> u32 {
        self.bitbuf >> (16 - n)
    }

    pub(crate) fn get(&mut self, n: u32) -> u32 {
        if n == 0 {
            return 0;
        }
        let v = self.peek(n);
        self.fill(n);
        v
    }
}

/// 位元讀取器的公開介面。ARJ 用的是同一套讀法,共用這一份。
pub struct Bits<R>(BitReader<R>);

impl<R: Read> Bits<R> {
    pub fn new(r: R) -> Self {
        Bits(BitReader::new(r))
    }
    /// 讀 n 個位元。n 為 0 時回 0 且不動指標。
    pub fn get(&mut self, n: u32) -> u32 {
        self.0.get(n)
    }
}

/// 樹節點的位置:快表的一格,或某個節點的左、右子。
#[derive(Clone, Copy)]
enum Loc {
    Table(usize),
    Left(usize),
    Right(usize),
}

pub(crate) struct Huffman {
    left: Vec<u16>,
    right: Vec<u16>,
    c_len: Vec<u8>,
    pt_len: [u8; NPT],
    c_table: Vec<u16>,
    pt_table: [u16; 256],
    blocksize: u32,
    pub(crate) blocks: usize,
    np: usize,
    pbit: u32,
}

/// 由碼長建查表。前 tablebits 位直接查,更長的碼走 left/right 樹。
fn make_table(
    left: &mut [u16],
    right: &mut [u16],
    nchar: usize,
    bitlen: &[u8],
    tablebits: u32,
    table: &mut [u16],
    avail: &mut usize,
) -> Result<(), Error> {
    let mut count = [0u32; 17];
    let mut weight = [0u32; 17];
    let mut start = [0u32; 18];
    for &l in &bitlen[..nchar] {
        count[l as usize] += 1;
    }
    count[0] = 0;
    for i in 1..=16usize {
        start[i + 1] = start[i].wrapping_add(count[i].wrapping_shl(16 - i as u32));
    }
    if start[17] != 1 << 16 {
        return Err(Error(format!("碼長表不合法(不是完整的 Huffman 樹,權重和 {})", start[17])));
    }
    let jutbits = 16 - tablebits;
    let mut i = 1u32;
    while i <= tablebits {
        start[i as usize] >>= jutbits;
        weight[i as usize] = 1 << (tablebits - i);
        i += 1;
    }
    while i <= 16 {
        weight[i as usize] = 1 << (16 - i);
        i += 1;
    }
    let k = start[tablebits as usize + 1] >> jutbits;
    if k != 1 << tablebits {
        for j in k..(1 << tablebits) {
            table[j as usize] = 0;
        }
    }
    let mask = 1u32 << (15 - tablebits);
    for (ch, &l) in bitlen[..nchar].iter().enumerate() {
        let l = l as u32;
        if l == 0 {
            continue;
        }
        let next = start[l as usize].wrapping_add(weight[l as usize]);
        if l <= tablebits {
            if next > 1 << tablebits {
                return Err(Error("碼長表越界".into()));
            }
            for j in start[l as usize]..next {
                table[j as usize] = ch as u16;
            }
        } else {
            let mut k = start[l as usize];
            let idx = (k >> jutbits) as usize;
            if idx >= table.len() {
                return Err(Error("碼長表越界".into()));
            }
            let mut p = Loc::Table(idx);
            let mut j = l - tablebits;
            while j != 0 {
                let cur = match p {
                    Loc::Table(x) => table[x],
                    Loc::Left(x) => left[x],
                    Loc::Right(x) => right[x],
                };
                let node = if cur == 0 {
                    left[*avail] = 0;
                    right[*avail] = 0;
                    let a = *avail as u16;
                    match p {
                        Loc::Table(x) => table[x] = a,
                        Loc::Left(x) => left[x] = a,
                        Loc::Right(x) => right[x] = a,
                    }
                    *avail += 1;
                    if *avail >= 2 * NC {
                        return Err(Error("碼長表節點用盡".into()));
                    }
                    a as usize
                } else {
                    cur as usize
                };
                p = if k & mask != 0 { Loc::Right(node) } else { Loc::Left(node) };
                k = k.wrapping_shl(1);
                j -= 1;
            }
            match p {
                Loc::Table(x) => table[x] = ch as u16,
                Loc::Left(x) => left[x] = ch as u16,
                Loc::Right(x) => right[x] = ch as u16,
            }
        }
        start[l as usize] = next;
    }
    Ok(())
}

impl Huffman {
    pub(crate) fn new(np: usize, pbit: u32) -> Huffman {
        Huffman {
            left: vec![0; 2 * NC],
            right: vec![0; 2 * NC],
            c_len: vec![0; NC],
            pt_len: [0; NPT],
            c_table: vec![0; 4096],
            pt_table: [0; 256],
            blocksize: 0,
            blocks: 0,
            np,
            pbit,
        }
    }

    /// 讀「碼長表的碼長表」。i_special 有值時,讀到第 i_special 筆之後
    /// 有一個 2 位的欄位表示要再補幾個 0,這是格式裡的特例。
    fn read_pt_len<R: Read>(&mut self, b: &mut BitReader<R>, nn: usize, nbit: u32, i_special: Option<usize>) -> Result<(), Error> {
        let n = b.get(nbit) as usize;
        if n == 0 {
            let c = b.get(nbit);
            self.pt_len[..nn].fill(0);
            self.pt_table.fill(c as u16);
            return Ok(());
        }
        let mut i = 0;
        while i < n && i < NPT {
            let mut c = b.peek(3);
            if c != 7 {
                b.fill(3);
            } else {
                let mut mask = 1u32 << (16 - 4);
                while mask & b.bitbuf != 0 {
                    mask >>= 1;
                    c += 1;
                    if c > 16 {
                        return Err(Error("碼長超過 16".into()));
                    }
                }
                b.fill(c - 3);
            }
            self.pt_len[i] = c as u8;
            i += 1;
            if Some(i) == i_special {
                let mut c = b.get(2);
                while c > 0 && i < nn {
                    self.pt_len[i] = 0;
                    i += 1;
                    c -= 1;
                }
            }
        }
        while i < nn {
            self.pt_len[i] = 0;
            i += 1;
        }
        let mut avail = nn;
        make_table(&mut self.left, &mut self.right, nn, &self.pt_len, 8, &mut self.pt_table, &mut avail)
            .map_err(|e| Error(format!("pt(nn={nn},n={n}): {}", e.0)))
    }

    fn read_c_len<R: Read>(&mut self, b: &mut BitReader<R>) -> Result<(), Error> {
        let n = b.get(CBIT) as usize;
        if n == 0 {
            let c = b.get(CBIT);
            self.c_len.fill(0);
            self.c_table.fill(c as u16);
            return Ok(());
        }
        let mut i = 0;
        while i < n && i < NC {
            let mut c = self.pt_table[b.peek(8) as usize] as usize;
            if c >= NT {
                let mut mask = 1u32 << (16 - 9);
                while c >= NT {
                    c = if b.bitbuf & mask != 0 { self.right[c] } else { self.left[c] } as usize;
                    mask >>= 1;
                    if mask == 0 {
                        return Err(Error("碼長表解碼卡住".into()));
                    }
                }
            }
            b.fill(self.pt_len[c] as u32);
            if c <= 2 {
                let mut run = match c {
                    0 => 1,
                    1 => b.get(4) + 3,
                    _ => b.get(CBIT) + 20,
                };
                while run > 0 && i < NC {
                    self.c_len[i] = 0;
                    i += 1;
                    run -= 1;
                }
            } else {
                self.c_len[i] = (c - 2) as u8;
                i += 1;
            }
        }
        self.c_len[i..].fill(0);
        let mut avail = NC;
        make_table(&mut self.left, &mut self.right, NC, &self.c_len, 12, &mut self.c_table, &mut avail)
            .map_err(|e| Error(format!("c(n={n}): {}", e.0)))
    }

    pub(crate) fn decode_c<R: Read>(&mut self, b: &mut BitReader<R>) -> Result<usize, Error> {
        if self.blocksize == 0 {
            self.blocksize = b.get(16);
            self.blocks += 1;
            self.read_pt_len(b, NT, TBIT, Some(3))?;
            self.read_c_len(b)?;
            let (np, pbit) = (self.np, self.pbit);
            self.read_pt_len(b, np, pbit, None)?;
        }
        self.blocksize = self.blocksize.wrapping_sub(1);
        let mut j = self.c_table[b.peek(12) as usize] as usize;
        if j >= NC {
            let mut mask = 1u32 << (16 - 13);
            while j >= NC {
                j = if b.bitbuf & mask != 0 { self.right[j] } else { self.left[j] } as usize;
                mask >>= 1;
                if mask == 0 {
                    return Err(Error("字碼解碼卡住".into()));
                }
            }
        }
        b.fill(self.c_len[j] as u32);
        Ok(j)
    }

    pub(crate) fn decode_p<R: Read>(&mut self, b: &mut BitReader<R>) -> Result<usize, Error> {
        let mut j = self.pt_table[b.peek(8) as usize] as usize;
        if j >= self.np {
            let mut mask = 1u32 << (16 - 9);
            while j >= self.np {
                j = if b.bitbuf & mask != 0 { self.right[j] } else { self.left[j] } as usize;
                mask >>= 1;
                if mask == 0 {
                    return Err(Error("距離解碼卡住".into()));
                }
            }
        }
        b.fill(self.pt_len[j] as u32);
        if j != 0 {
            j = (1 << (j - 1)) + b.get(j as u32 - 1) as usize;
        }
        Ok(j)
    }
}
