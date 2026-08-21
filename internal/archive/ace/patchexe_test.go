package ace

import (
	"bytes"
	"testing"
)

// patchExe 的位移是**固定寬度**的(16 或 32 位元),減法要環繞。
//
// 這條盯的是寬度:寫成平台的 int 再遮罩,在 64 位元機器上「剛好對」,
// 但 32 位元平台(Android 的 armeabi-v7a)連編都編不過,而且就算編得過,
// `& 0xFFFFFFFF` 在 32 位元上是沒有作用的空操作 —— 也就是說同一份程式碼
// 在兩種機器上會解出不同的位元組,而兩邊都不會報錯。
//
// 挑的樣本刻意讓減法借位(rel < pos),那正是寬度錯了才會分岔的情況。
func TestPatchExeWrapsAtFixedWidth(t *testing.T) {
	// CALL rel32:位移 0x00000010,檔案位移 0x20 → 0x10-0x20 環繞成 0xFFFFFFF0
	buf := []byte{0xE8, 0x10, 0x00, 0x00, 0x00, 0x90, 0x90, 0x90, 0x90}
	out, _ := patchExe(append([]byte(nil), buf...), 0x20, 1)
	want := []byte{0xE8, 0xF0, 0xFF, 0xFF, 0xFF, 0x90, 0x90, 0x90, 0x90}
	if !bytes.Equal(out[:len(want)], want) {
		t.Errorf("CALL rel32 借位\n得到 % X\n想要 % X", out[:len(want)], want)
	}

	// CALL rel16(exeMode 0):同樣借位,但只在 16 位元內環繞
	buf = []byte{0xE8, 0x10, 0x00, 0x90, 0x90, 0x90}
	out, _ = patchExe(append([]byte(nil), buf...), 0x20, 0)
	want = []byte{0xE8, 0xF0, 0xFF, 0x90, 0x90, 0x90}
	if !bytes.Equal(out[:len(want)], want) {
		t.Errorf("CALL rel16 借位\n得到 % X\n想要 % X", out[:len(want)], want)
	}

	// JMP rel16
	buf = []byte{0xE9, 0x05, 0x00, 0x90, 0x90, 0x90}
	out, _ = patchExe(append([]byte(nil), buf...), 0x11, 0)
	want = []byte{0xE9, 0xF4, 0xFF, 0x90, 0x90, 0x90}
	if !bytes.Equal(out[:len(want)], want) {
		t.Errorf("JMP rel16 借位\n得到 % X\n想要 % X", out[:len(want)], want)
	}
}

// 一條指令可能跨在兩個區塊的交界上:opcode 在這一段、位移在下一段。
// 尾端的殘留要留給下一段,不能就地處理(那會拿到區塊外的位元組)。
func TestPatchExeKeepsTailForNextBlock(t *testing.T) {
	buf := []byte{0x90, 0x90, 0xE8, 0x01}
	out, tail := patchExe(append([]byte(nil), buf...), 0, 1)
	if len(tail) == 0 {
		t.Fatalf("跨界的指令沒有留殘留,out=% X tail=% X", out, tail)
	}
	if len(out)+len(tail) != len(buf) {
		t.Errorf("out(%d) + tail(%d) != 輸入(%d)", len(out), len(tail), len(buf))
	}
}
