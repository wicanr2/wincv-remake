package arc

import "fmt"

import "github.com/wicanr2/wincv-remake/internal/i18n"

const (
	unused     = -1
	realMaxStr = 65536
)

// lzwState 是 ARC 的 LZW 字典。
//
// 字串用「指標 + 尾字元」串成鏈:ptr 指向前綴的編號,chr 是最後一個字元,
// ptr1st 是這條鏈的第一個字元(直接記下來,免得每次都要走到底)。
type lzwState struct {
	ptr, chr, ptr1st []int
	last             int
	maxStr           int
}

func newLZWState(maxStr int, oldver bool) *lzwState {
	s := &lzwState{
		ptr:    make([]int, realMaxStr),
		chr:    make([]int, realMaxStr),
		ptr1st: make([]int, realMaxStr),
		maxStr: maxStr,
	}
	for i := range s.ptr {
		s.ptr[i], s.chr[i], s.ptr1st[i] = unused, unused, unused
	}
	if oldver {
		s.last = -1
	} else {
		for i := 0; i < 256; i++ {
			s.chr[i] = i
		}
		s.last = 255
	}
	return s
}

func (s *lzwState) add(oldCode, chr int) {
	s.last++
	if s.last&s.maxStr != 0 { // 溢位:停在最後一格,不再長
		s.last = s.maxStr - 1
		return
	}
	idx := s.last
	s.chr[idx] = chr
	if oldCode >= s.maxStr {
		return
	}
	s.ptr[idx] = oldCode
	if s.ptr[oldCode] == unused {
		s.ptr1st[idx] = oldCode // 指到根,那個根自己就是第一個字元
	} else {
		s.ptr1st[idx] = s.ptr1st[oldCode]
	}
}

func (s *lzwState) firstChar(code int) int {
	if s.ptr[code] != unused {
		code = s.ptr1st[code]
	}
	return s.chr[code]
}

// codeReader 讀變動寬度的碼。
//
// [雷] 位元是 **LSB 優先**(和 LHA / ARJ 的 MSB 優先相反),
// 而且換寬度時要「重新對齊」:編碼器每 8 個碼算一組,換寬度前
// 會把那一組補滿,解碼端得照樣把剩下的碼讀掉丟棄。
type codeReader struct {
	in       []byte
	pos      int
	bitbox   int
	bitsLeft int
	codeOfs  int
	maxStr   int
}

func (r *codeReader) read(numBits int) (int, bool) {
	filled, code := 0, 0
	for filled < numBits {
		if r.bitsLeft == 0 {
			if r.pos >= len(r.in) {
				return 0, false
			}
			r.bitbox = int(r.in[r.pos])
			r.pos++
			r.bitsLeft = 8
		}
		got := r.bitsLeft
		if got > numBits-filled {
			got = numBits - filled
		}
		code |= (r.bitbox & ((1 << got) - 1)) << filled
		r.bitbox >>= got
		r.bitsLeft -= got
		filled += got
	}
	if code < 0 || code > r.maxStr-1 {
		return 0, false
	}
	r.codeOfs = (r.codeOfs + 1) & 7
	return code, true
}

// resync 把目前這一組(8 個碼)剩下的讀完丟棄。
func (r *codeReader) resync(old int) {
	for r.codeOfs != 0 {
		if _, ok := r.read(old); !ok {
			return
		}
	}
}

// lzwDynamic 解 ARC 的 LZW。maxBits 為 0 表示固定 12 位的舊版。
func lzwDynamic(in []byte, maxBits int, useRLE bool, origSize int64) ([]byte, error) {
	oldver := maxBits == 0
	maxStr := 1 << maxBits
	csize := 9
	if oldver {
		csize, maxStr = 12, 4096
	}
	orgcsize := csize

	r := &codeReader{in: in, maxStr: maxStr}
	if maxBits == 12 {
		r.pos++ // 方法 8 的第一個位元組是「最大碼寬」,永遠是 12
	}
	if maxBits == 16 && r.pos < len(in) {
		// compress 型(Spark)可以自己改上限
		maxStr = 1 << in[r.pos]
		r.pos++
		r.maxStr = maxStr
	}

	st := newLZWState(maxStr, oldver)
	w := &rleWriter{out: make([]byte, 0, origSize), limit: origSize}
	emit := func(c int) {
		if useRLE {
			w.write(byte(c))
		} else {
			w.raw(byte(c))
		}
	}
	// outputString 把一條鏈倒著吐出來。
	buf := make([]int, 0, realMaxStr)
	outputString := func(code int) {
		buf = buf[:0]
		for st.ptr[code] != unused && len(buf) < maxStr {
			buf = append(buf, st.chr[code])
			code = st.ptr[code]
		}
		emit(st.chr[code])
		for i := len(buf) - 1; i >= 0; i-- {
			emit(buf[i])
		}
	}

	oldCode, k := 0, 0
	first := true
	for {
		newCode, ok := r.read(csize)
		if !ok {
			break
		}
		noadd := false
		if first {
			k, first = newCode, false
			if oldver {
				noadd = true
			}
		}

		if newCode == 256 && !oldver {
			// [雷] 這個碼**不清空字典**,只是把碼寬降回去 ——
			// 新字串會直接蓋掉舊的格子。當成 GIF 那種 clear 來處理
			// 會從這裡開始整份錯掉。
			st.last = 255
			r.resync(csize)
			csize = orgcsize
			newCode, ok = r.read(csize)
			if !ok {
				break
			}
		}

		known := newCode <= st.last
		if oldver {
			known = st.chr[newCode] != unused
		}
		if known {
			outputString(newCode)
			k = st.firstChar(newCode)
		} else {
			// KwKwK:編碼器用了它剛建、這邊還沒建的字串
			outputString(oldCode)
			emit(k)
		}

		if st.last != maxStr-1 && !noadd {
			st.add(oldCode, k)
			if st.last != maxStr-1 && st.last == (1<<csize)-1 {
				csize++
				r.resync(csize - 1)
			}
		}
		oldCode = newCode
	}

	if int64(len(w.out)) != origSize {
		return w.out, fmt.Errorf(i18n.T("解出 %d 個位元組,期望 %d"), len(w.out), origSize)
	}
	return w.out, nil
}
