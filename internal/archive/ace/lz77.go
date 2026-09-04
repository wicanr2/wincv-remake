package ace

import "fmt"

import "github.com/wicanr2/wincv-remake/internal/i18n"

// LZ77 的符號配置:
//
//	  0..255  原樣的位元組
//	256..259  比對,距離取自最近用過的四個距離
//	260..282  比對,距離的位元寬由符號決定(0..22 位)
//	283       模式切換碼
const (
	maxCodeWidth  = 11
	maxDistAtLen2 = 255
	maxDistAtLen3 = 8191
	minDicBits    = 10
	maxDicBits    = 22
	minDicSize    = 1 << minDicBits
	maxDicSize    = 1 << maxDicBits
	typeCode      = 260 + maxDicBits + 1
	numMainCodes  = 260 + maxDicBits + 2
	numLenCodes   = 256 - 1
)

// distHist 記住最近用過的四個距離。取用之後那個距離會被移到最新,
// 所以連續回頭引用同一段時碼會很短。
type distHist struct{ h [4]int }

func (d *distHist) append(v int) {
	copy(d.h[:], d.h[1:])
	d.h[3] = v
}

func (d *distHist) retrieve(off int) int {
	i := 4 - off - 1
	v := d.h[i]
	copy(d.h[i:], d.h[i+1:])
	d.h[3] = v
	return v
}

// symReader 讀一個個 Huffman 區塊。主碼與長度碼各有一棵表,
// 區塊用完時再讀下一組表。
type symReader struct {
	main, length *huffTree
	toRead       int
}

func (s *symReader) readTrees(b *bitStream) error {
	var err error
	if s.main, err = readTree(b, maxCodeWidth, numMainCodes); err != nil {
		return fmt.Errorf(i18n.T("主碼表: %w"), err)
	}
	if s.length, err = readTree(b, maxCodeWidth, numLenCodes); err != nil {
		return fmt.Errorf(i18n.T("長度碼表: %w"), err)
	}
	n, err := b.read(15)
	if err != nil {
		return err
	}
	s.toRead = int(n)
	return nil
}

func (s *symReader) readMain(b *bitStream) (int, error) {
	if s.toRead == 0 {
		if err := s.readTrees(b); err != nil {
			return 0, err
		}
	}
	s.toRead--
	return s.main.readSymbol(b)
}

func (s *symReader) readLen(b *bitStream) (int, error) { return s.length.readSymbol(b) }

// lz77 是 LZ77 解碼器。字典就是輸出本身 —— 同一個壓縮檔裡的成員
// 共用一個字典(solid 壓縮檔會跨成員回頭引用)。
type lz77 struct {
	out    []byte // 整個壓縮檔到目前為止的輸出,當成滑動視窗用
	sym    symReader
	dist   distHist
	dicLen int
}

func newLZ77() *lz77 { return &lz77{dicLen: minDicSize} }

func (l *lz77) setDicSize(n int) {
	if n > l.dicLen {
		l.dicLen = n
	}
	if l.dicLen > maxDicSize {
		l.dicLen = maxDicSize
	}
}

// reinit 重設每個成員都要歸零的狀態。**字典不歸零**。
func (l *lz77) reinit() {
	l.sym = symReader{}
	l.dist = distHist{}
}

// register 把其他模式(或不壓縮的成員)產生的位元組併進字典。
func (l *lz77) register(buf []byte) { l.out = append(l.out, buf...) }

// read 解出最多 want 個位元組,或直到遇到模式切換碼。
// 回傳新產生的位元組,以及模式切換碼(沒有就是 nil)。
func (l *lz77) read(b *bitStream, want int) ([]byte, *aceMode, error) {
	start := len(l.out)
	var next *aceMode
	for len(l.out)-start < want {
		sym, err := l.sym.readMain(b)
		if err != nil {
			return l.out[start:], nil, err
		}
		switch {
		case sym <= 255:
			l.out = append(l.out, byte(sym))
		case sym < typeCode:
			var dist, length int
			if sym <= 259 {
				n, err := l.sym.readLen(b)
				if err != nil {
					return l.out[start:], nil, err
				}
				off := sym & 0x03
				dist = l.dist.retrieve(off)
				if off > 1 {
					length = n + 3
				} else {
					length = n + 2
				}
			} else {
				d, err := b.readKnownWidthUint(uint(sym - 260))
				if err != nil {
					return l.out[start:], nil, err
				}
				dist = int(d)
				n, err := l.sym.readLen(b)
				if err != nil {
					return l.out[start:], nil, err
				}
				l.dist.append(dist)
				switch {
				case dist <= maxDistAtLen2:
					length = n + 2
				case dist <= maxDistAtLen3:
					length = n + 3
				default:
					length = n + 4
				}
			}
			dist++
			if len(l.out)-start+length > want {
				return l.out[start:], nil, fmt.Errorf(i18n.T("比對長度超出這一段要的位元組數"))
			}
			src := len(l.out) - dist
			if src < 0 {
				return l.out[start:], nil, fmt.Errorf(i18n.T("比對距離 %d 超出視窗(目前 %d 個位元組)"), dist, len(l.out))
			}
			// 逐個位元組複製:來源與目的可能重疊
			for i := 0; i < length; i++ {
				l.out = append(l.out, l.out[src+i])
			}
		case sym == typeCode:
			m, err := readMode(b)
			if err != nil {
				return l.out[start:], nil, err
			}
			next = m
			return l.out[start:], next, nil
		default:
			return l.out[start:], nil, fmt.Errorf(i18n.T("主碼 %d 超出範圍"), sym)
		}
	}
	return l.out[start:], next, nil
}
