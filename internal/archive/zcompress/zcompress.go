// Package zcompress 解 Unix compress(.Z)。
//
// 格式是可變寬度的 LZW,碼寬從 9 位開始,表滿了就加寬到 maxbits(通常 16)。
//
// 唯一會讓人栽跟頭的地方是**分組對齊**:編碼器把碼放進一個
// n_bits 位元組(剛好 8 個碼)的緩衝區,換寬度或重設時會把當下這一組
// 補滿再寫出去。解碼端必須跳過同樣的填充,否則從那一刻起整串都錯位。
package zcompress

import (
	"fmt"
	"github.com/wicanr2/wincv-remake/internal/i18n"
	"io"
)

const (
	magic1    = 0x1F
	magic2    = 0x9D
	initBits  = 9
	clearCode = 256
	first     = 257 // 區塊模式下,第一個可用的字典編號
)

// IsZ 判斷這串位元組看起來是不是 .Z。
func IsZ(b []byte) bool {
	return len(b) >= 3 && b[0] == magic1 && b[1] == magic2
}

// Decode 解開一份 .Z。
func Decode(data []byte) ([]byte, error) {
	if !IsZ(data) {
		return nil, fmt.Errorf(i18n.T("不是 compress 檔(magic 不對)"))
	}
	maxbits := int(data[2] & 0x1F)
	blockMode := data[2]&0x80 != 0
	if maxbits < initBits || maxbits > 16 {
		return nil, fmt.Errorf(i18n.T("maxbits = %d 超出範圍"), maxbits)
	}
	maxmaxcode := 1 << maxbits

	// prefix/suffix 是字典。0-255 是單一位元組,自己就是自己。
	prefix := make([]uint16, maxmaxcode)
	suffix := make([]byte, maxmaxcode)
	for i := 0; i < 256; i++ {
		suffix[i] = byte(i)
	}

	nBits := initBits
	maxcode := 1<<nBits - 1
	freeEnt := 256
	if blockMode {
		freeEnt = first
	}

	bitPos := 24 // 檔頭三個位元組
	total := len(data) * 8
	inGroup := 0 // 這一組(8 個碼)已經讀了幾個

	// align 跳過填充,讓下一個碼從新的一組開始。
	align := func() {
		if r := inGroup % 8; r != 0 {
			bitPos += (8 - r) * nBits
		}
		inGroup = 0
	}

	readCode := func() (int, bool) {
		if bitPos+nBits > total {
			return 0, false
		}
		v := 0
		for i := 0; i < nBits; i++ {
			p := bitPos + i
			if data[p>>3]&(1<<uint(p&7)) != 0 {
				v |= 1 << uint(i)
			}
		}
		bitPos += nBits
		inGroup++
		return v, true
	}

	var out []byte
	var stack []byte
	oldCode := -1
	finChar := byte(0)

	for {
		if freeEnt > maxcode && nBits < maxbits {
			align()
			nBits++
			if nBits == maxbits {
				maxcode = maxmaxcode
			} else {
				maxcode = 1<<nBits - 1
			}
		}
		code, ok := readCode()
		if !ok {
			break
		}

		if blockMode && code == clearCode {
			align()
			nBits = initBits
			maxcode = 1<<nBits - 1
			freeEnt = first
			oldCode = -1
			continue
		}

		if oldCode < 0 {
			if code >= 256 {
				return out, fmt.Errorf(i18n.T("第一個碼是 %d,應該是單一位元組"), code)
			}
			finChar = byte(code)
			out = append(out, finChar)
			oldCode = code
			continue
		}

		incode := code
		stack = stack[:0]
		if code >= freeEnt {
			// KwKwK:編碼器用了一個它剛建、解碼器還沒建的字串。
			if code > freeEnt {
				return out, fmt.Errorf(i18n.T("字碼 %d 超出字典(%d)"), code, freeEnt)
			}
			stack = append(stack, finChar)
			code = oldCode
		}
		for code >= 256 {
			stack = append(stack, suffix[code])
			code = int(prefix[code])
			if len(stack) > maxmaxcode {
				return out, fmt.Errorf(i18n.T("字典有迴圈"))
			}
		}
		finChar = suffix[code]
		stack = append(stack, finChar)
		for i := len(stack) - 1; i >= 0; i-- {
			out = append(out, stack[i])
		}

		if freeEnt < maxmaxcode {
			prefix[freeEnt] = uint16(oldCode)
			suffix[freeEnt] = finChar
			freeEnt++
		}
		oldCode = incode
	}
	return out, nil
}

// DecodeReader 是給串流呼叫端的便利函式。
func DecodeReader(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return Decode(data)
}
