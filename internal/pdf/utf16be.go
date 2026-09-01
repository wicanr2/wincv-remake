package pdf

import (
	"unicode/utf16"

	"golang.org/x/text/transform"
)

// utf16beTransformer 把 UTF-16BE 的位元組換成 UTF-8。
type utf16beTransformer struct{ transform.NopResetter }

func (utf16beTransformer) Transform(dst, src []byte, atEOF bool) (nDst, nSrc int, err error) {
	for nSrc+1 < len(src) {
		u := uint16(src[nSrc])<<8 | uint16(src[nSrc+1])
		var s string
		if utf16.IsSurrogate(rune(u)) && nSrc+3 < len(src) {
			lo := uint16(src[nSrc+2])<<8 | uint16(src[nSrc+3])
			if r := utf16.DecodeRune(rune(u), rune(lo)); r != 0xFFFD {
				s = string(r)
				if nDst+len(s) > len(dst) {
					return nDst, nSrc, transform.ErrShortDst
				}
				nDst += copy(dst[nDst:], s)
				nSrc += 4
				continue
			}
		}
		s = string(utf16.Decode([]uint16{u}))
		if nDst+len(s) > len(dst) {
			return nDst, nSrc, transform.ErrShortDst
		}
		nDst += copy(dst[nDst:], s)
		nSrc += 2
	}
	if atEOF {
		return nDst, len(src), nil
	}
	return nDst, nSrc, transform.ErrShortSrc
}
