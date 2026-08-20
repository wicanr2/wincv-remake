package ace

import "fmt"

const (
	widthWidthBits = 3
	maxWidthWidth  = (1 << widthWidthBits) - 1
)

// huffTree 是一棵解碼用的 Huffman 表。
//
// 不是走樹,而是「一次看 maxWidth 個位元直接查表」:codes 的長度是
// 2^maxWidth,每個碼依它的實際寬度重複 2^(maxWidth-寬度) 次填進去。
type huffTree struct {
	codes    []int
	widths   []int
	maxWidth uint
}

func (t *huffTree) readSymbol(b *bitStream) (int, error) {
	v, err := b.peek(t.maxWidth)
	if err != nil {
		return 0, err
	}
	if int(v) >= len(t.codes) {
		return 0, fmt.Errorf("Huffman 查表越界")
	}
	sym := t.codes[v]
	b.skip(uint(t.widths[sym]))
	return sym, nil
}

// aceSort 是 ACE 專用的排序:依 keys 由大到小,同時搬動 values。
//
// [雷] **不能換成一般的排序。** 這個 quicksort 對相等的鍵是不穩定的,
// 而且是以特定的方式不穩定;碼的指派順序直接取決於相等寬度的符號
// 最後排成什麼次序。換成 sort.Slice(即使加上 tie-break)會得到
// 一套自洽但與編碼器對不上的碼,解出來是垃圾而不是錯誤。
// 這裡逐行照參考實作搬過來。
func aceSort(keys, values []int) {
	var sub func(left, right int)
	swap := func(a, b int) {
		keys[a], keys[b] = keys[b], keys[a]
		values[a], values[b] = values[b], values[a]
	}
	sub = func(left, right int) {
		nl, nr := left, right
		m := keys[right]
		for {
			for keys[nl] > m {
				nl++
			}
			for keys[nr] < m {
				nr--
			}
			if nl <= nr {
				swap(nl, nr)
				nl++
				nr--
			}
			if nl >= nr {
				break
			}
		}
		if left < nr {
			if left < nr-1 {
				sub(left, nr)
			} else if keys[left] < keys[nr] {
				swap(left, nr)
			}
		}
		if right > nl {
			if nl < right-1 {
				sub(nl, right)
			} else if keys[nl] < keys[right] {
				swap(nl, right)
			}
		}
	}
	if len(keys) > 0 {
		sub(0, len(keys)-1)
	}
}

// makeTree 由每個符號的碼長算出查表用的碼表。
func makeTree(widths []int, maxWidth uint) (*huffTree, error) {
	syms := make([]int, len(widths))
	for i := range syms {
		syms[i] = i
	}
	sortedWidths := append([]int(nil), widths...)
	aceSort(sortedWidths, syms)

	used := 0
	for used < len(sortedWidths) && sortedWidths[used] != 0 {
		used++
	}
	if used < 2 {
		widths[syms[0]] = 1
		sortedWidths[0] = 1
		if used == 0 {
			used++
		}
	}
	syms = syms[:used]
	sortedWidths = sortedWidths[:used]

	maxCodes := 1 << maxWidth
	codes := make([]int, 0, maxCodes)
	for i := len(syms) - 1; i >= 0; i-- {
		w := sortedWidths[i]
		if uint(w) > maxWidth {
			return nil, fmt.Errorf("碼長 %d 超過上限 %d", w, maxWidth)
		}
		repeat := 1 << (maxWidth - uint(w))
		for j := 0; j < repeat; j++ {
			codes = append(codes, syms[i])
		}
		if len(codes) > maxCodes {
			return nil, fmt.Errorf("碼表長度超過 %d", maxCodes)
		}
	}
	return &huffTree{codes: codes, widths: widths, maxWidth: maxWidth}, nil
}

// readTree 從位元流讀一棵 Huffman 表。
//
// 表本身也是壓縮的:先讀一棵「碼長的碼長表」,再用它解出各符號的碼長,
// 而那串碼長還是差分編碼的(對 upper_width 取模)。
func readTree(b *bitStream, maxWidth uint, numCodes int) (*huffTree, error) {
	nw, err := b.read(9)
	if err != nil {
		return nil, err
	}
	numWidths := int(nw) + 1
	if numWidths > numCodes+1 {
		numWidths = numCodes + 1
	}
	lower, err := b.read(4)
	if err != nil {
		return nil, err
	}
	upper, err := b.read(4)
	if err != nil {
		return nil, err
	}

	ww := make([]int, upper+1)
	for i := range ww {
		v, err := b.read(widthWidthBits)
		if err != nil {
			return nil, err
		}
		ww[i] = int(v)
	}
	wtree, err := makeTree(ww, maxWidthWidth)
	if err != nil {
		return nil, fmt.Errorf("碼長表: %w", err)
	}

	widths := make([]int, 0, numWidths)
	for len(widths) < numWidths {
		sym, err := wtree.readSymbol(b)
		if err != nil {
			return nil, err
		}
		if sym < int(upper) {
			widths = append(widths, sym)
			continue
		}
		n, err := b.read(4)
		if err != nil {
			return nil, err
		}
		length := int(n) + 4
		if length > numWidths-len(widths) {
			length = numWidths - len(widths)
		}
		for i := 0; i < length; i++ {
			widths = append(widths, 0)
		}
	}

	if upper > 0 {
		for i := 1; i < len(widths); i++ {
			widths[i] = (widths[i] + widths[i-1]) % int(upper)
		}
	}
	for i := range widths {
		if widths[i] > 0 {
			widths[i] += int(lower)
		}
	}
	return makeTree(widths, maxWidth)
}
