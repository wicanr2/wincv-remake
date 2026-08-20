package textenc

import (
	"os"
	"strings"
	"testing"

	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

func enc(t *testing.T, e Enc, s string) []byte {
	t.Helper()
	var b []byte
	var err error
	switch e {
	case Big5:
		b, err = traditionalchinese.Big5.NewEncoder().Bytes([]byte(s))
	case GBK:
		b, err = simplifiedchinese.GBK.NewEncoder().Bytes([]byte(s))
	case ShiftJIS:
		b, err = japanese.ShiftJIS.NewEncoder().Bytes([]byte(s))
	case EUCKR:
		b, err = korean.EUCKR.NewEncoder().Bytes([]byte(s))
	default:
		t.Fatalf("不支援的測試編碼 %v", e)
	}
	if err != nil {
		t.Fatalf("編碼成 %v 失敗: %v", e, err)
	}
	return b
}

const (
	zhTW = "這是一份繁體中文的說明檔案，用來測試編碼判讀是否正確。" +
		"檔案瀏覽、壓縮檔管理、文字檢視都是這個程式的主要功能。" +
		"如果判讀錯了，畫面上會看到一堆亂碼，那就沒有意義了。"
	zhCN = "这是一份简体中文的说明文件，用来测试编码判读是否正确。" +
		"文件浏览、压缩文件管理、文字查看都是这个程序的主要功能。" +
		"如果判读错了，画面上会看到一堆乱码，那就没有意义了。"
	jaJP = "これは日本語のテキストファイルです。文字コードの判定が" +
		"正しく動くかどうかを確認するために使います。" +
		"判定を間違えると画面が文字化けしてしまいます。"
	koKR = "이것은 한국어 텍스트 파일입니다. 문자 인코딩 판별이 " +
		"제대로 동작하는지 확인하기 위해 사용합니다."
)

func TestDetectRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		e    Enc
		text string
	}{
		{"繁中 Big5", Big5, zhTW},
		{"簡中 GBK", GBK, zhCN},
		{"日文 Shift_JIS", ShiftJIS, jaJP},
		{"韓文 EUC-KR", EUCKR, koKR},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := enc(t, tc.e, tc.text)
			got := Detect(b)
			if got != tc.e {
				t.Errorf("Detect = %v, 應為 %v", got, tc.e)
			}
			// 判對之後還要真的解得回原文。
			if s := Decode(b, tc.e); s != tc.text {
				t.Errorf("解碼結果不符\n得到 %q\n預期 %q", s, tc.text)
			}
		})
	}
}

func TestDetectUTF8AndASCII(t *testing.T) {
	if got := Detect([]byte(zhTW)); got != UTF8 {
		t.Errorf("UTF-8 判成 %v", got)
	}
	if got := Detect([]byte("\xEF\xBB\xBFhello")); got != UTF8 {
		t.Errorf("帶 BOM 的 UTF-8 判成 %v", got)
	}
	if got := Detect([]byte("plain ascii text\nsecond line\n")); got != ASCII {
		t.Errorf("純 ASCII 判成 %v", got)
	}
	if got := Detect(nil); got != ASCII {
		t.Errorf("空檔判成 %v", got)
	}
}

func TestDetectUTF16(t *testing.T) {
	le := []byte{0xFF, 0xFE, 'h', 0, 'i', 0}
	if got := Detect(le); got != UTF16LE {
		t.Errorf("UTF-16LE(有 BOM) 判成 %v", got)
	}
	if s := Decode(le, UTF16LE); s != "hi" {
		t.Errorf("UTF-16LE 解出 %q", s)
	}
	// 沒有 BOM,靠交錯的 0x00 判
	var noBOM []byte
	for _, c := range "hello world, this is utf16 without bom" {
		noBOM = append(noBOM, byte(c), 0)
	}
	if got := Detect(noBOM); got != UTF16LE {
		t.Errorf("UTF-16LE(無 BOM) 判成 %v", got)
	}
}

// ESC 不可以被當成二進位 —— BBS 簽名檔的 ANSI 色碼就是 ESC 開頭,
// 而「可看 ANSI 彩色控制字碼」是原版列出的功能之一。
func TestAnsiEscapeIsText(t *testing.T) {
	ansi := "\x1b[1;31m紅色\x1b[0m 一般\n\x1b[44m藍底\x1b[0m\n"
	b := enc(t, Big5, ansi)
	if got := Detect(b); got != Big5 {
		t.Errorf("含 ANSI 色碼的 Big5 判成 %v", got)
	}
}

func TestDetectBinary(t *testing.T) {
	var b []byte
	for i := 0; i < 512; i++ {
		b = append(b, byte(i%7)) // 大量控制字元與 NUL
	}
	if got := Detect(b); got != Binary {
		t.Errorf("二進位資料判成 %v", got)
	}
}

// 壞位元組不可以讓整份檔案解不出來。
func TestDecodeLossyKeepsGoodParts(t *testing.T) {
	good := enc(t, Big5, "前面正常")
	tail := enc(t, Big5, "後面也正常")
	b := append(append(append([]byte{}, good...), 0xFF, 0xFF), tail...)
	s := Decode(b, Big5)
	if !strings.Contains(s, "前面正常") {
		t.Errorf("壞位元組之前的內容掉了: %q", s)
	}
	if !strings.Contains(s, "後面也正常") {
		t.Errorf("壞位元組之後的內容掉了 —— decoder 停在壞的地方了: %q", s)
	}
	if !strings.Contains(s, "�") {
		t.Errorf("壞位元組沒有變成替代字: %q", s)
	}
}

// 用原版自己的檔案當 golden data:這些是真的 Big5,不是我們造出來的。
func TestRealOriginalFiles(t *testing.T) {
	for _, name := range []string{
		"../../original/app/WinCV.txt",
		"../../original/app/whatsnew.txt",
		"../../original/app/file_id.diz",
	} {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Skipf("找不到 %s,跳過", name)
		}
		if got := Detect(b); got != Big5 {
			t.Errorf("%s 判成 %v, 應為 Big5", name, got)
		}
		s := Decode(b, Big5)
		if !strings.Contains(s, "WinCV") {
			t.Errorf("%s 解出來看不到 WinCV 字樣", name)
		}
	}
}
