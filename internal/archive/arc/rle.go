package arc

// rleWriter 是 ARC 的 RLE 解碼器。
//
// 0x90 是跳脫字元:`0x90 00` 是一個原樣的 0x90,`0x90 n`(n>0)表示
// 前一個位元組總共出現 n 次(已經吐出的那一個算在內,所以再補 n-1 個)。
//
// 重複之後**不更新 lastchr** —— 連續兩組 0x90 都是指同一個位元組。
type rleWriter struct {
	out       []byte
	last      byte
	repeating bool
	limit     int64
}

func (w *rleWriter) raw(c byte) {
	if int64(len(w.out)) < w.limit {
		w.out = append(w.out, c)
	}
}

func (w *rleWriter) write(c byte) {
	if w.repeating {
		if c == 0 {
			w.raw(0x90)
		} else {
			for i := 1; i < int(c); i++ {
				w.raw(w.last)
			}
		}
		w.repeating = false
		return
	}
	if c == 0x90 {
		w.repeating = true
		return
	}
	w.raw(c)
	w.last = c
}

func unRLE(in []byte, origSize int64) []byte {
	w := &rleWriter{out: make([]byte, 0, origSize), limit: origSize}
	for _, c := range in {
		w.write(c)
	}
	return w.out
}
