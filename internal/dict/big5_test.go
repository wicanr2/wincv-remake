package dict

import "golang.org/x/text/encoding/traditionalchinese"

// big5 把 UTF-8 轉成 Big5,測試造資料用。
func big5(s string) ([]byte, error) {
	return traditionalchinese.Big5.NewEncoder().Bytes([]byte(s))
}
