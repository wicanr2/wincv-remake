// big5probe 印出 Big5 補充符號區(C6A1-C8FE)裡哪些碼位是有定義的。
//
// 倚天的 SPCFSUPP.15 把「有定義的碼位」擠在一起存,中間有洞;
// 要用線性索引取字模就得先知道洞在哪。這支程式拿 x/text 的 Big5
// 解碼器當判準,列出認得與不認得的碼位。
package main

import (
	"fmt"

	"golang.org/x/text/encoding/traditionalchinese"
)

func rawIndex(hi, lo int) int {
	off := lo - 0x40
	if lo >= 0x7F {
		off = lo - 0x62
	}
	return (hi-0xA1)*157 + off
}

func main() {
	dec := traditionalchinese.Big5.NewDecoder()
	var ok, bad []int
	for hi := 0xC6; hi <= 0xC8; hi++ {
		for lo := 0x40; lo <= 0xFE; lo++ {
			if lo > 0x7E && lo < 0xA1 {
				continue
			}
			code := hi<<8 | lo
			if code < 0xC6A1 || code > 0xC8FE {
				continue
			}
			s, err := dec.Bytes([]byte{byte(hi), byte(lo)})
			r := []rune(string(s))
			if err != nil || len(r) != 1 || r[0] == 0xFFFD {
				bad = append(bad, code)
			} else {
				ok = append(ok, code)
			}
		}
	}
	fmt.Printf("C6A1-C8FE 共 %d 個碼位:x/text Big5 認得 %d、不認得 %d\n",
		len(ok)+len(bad), len(ok), len(bad))
	fmt.Printf("raw 索引 %d..%d\n", rawIndex(0xC6, 0xA1), rawIndex(0xC8, 0xFE))
	fmt.Print("不認得: ")
	for i, c := range bad {
		if i > 0 && i%16 == 0 {
			fmt.Print("\n         ")
		}
		fmt.Printf("%04X ", c)
	}
	fmt.Println()
}
