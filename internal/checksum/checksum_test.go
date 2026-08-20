package checksum

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// 對照 RFC 1321 的已知值,而不是拿自己的實作當基準。
func TestMD5KnownVector(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "abc.txt", "abc")
	got, err := MD5File(filepath.Join(dir, "abc.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "900150983cd24fb0d6963f7d28e17f72" {
		t.Errorf("MD5(\"abc\") = %s", got)
	}
}

func TestCRC32KnownVector(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "c.txt", "123456789")
	got, err := CRC32File(filepath.Join(dir, "c.txt"))
	if err != nil {
		t.Fatal(err)
	}
	// CRC-32/ISO-HDLC 對 "123456789" 的標準檢查值
	if got != "CBF43926" {
		t.Errorf("CRC32(\"123456789\") = %s, 應為 CBF43926", got)
	}
}

// 檔名含空白時要以最後一個空白分隔,不是第一個。
func TestParseSFVFilenameWithSpaces(t *testing.T) {
	in := `; 這是註解
my file.zip ABCD1234
plain.txt 00000000

badline
short.txt 123
`
	es, err := ParseSFV(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(es) != 2 {
		t.Fatalf("解出 %d 筆: %+v", len(es), es)
	}
	if es[0].Name != "my file.zip" || es[0].CRC != "ABCD1234" {
		t.Errorf("第一筆 = %+v", es[0])
	}
}

func TestVerifySFV(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "good.txt", "123456789")
	write(t, dir, "bad.txt", "wrong content")
	sfv := "good.txt CBF43926\nbad.txt CBF43926\ngone.txt CBF43926\n"
	write(t, dir, "list.sfv", sfv)

	rs, err := VerifySFV(filepath.Join(dir, "list.sfv"))
	if err != nil {
		t.Fatal(err)
	}
	ok, bad, missing := Summary(rs)
	if ok != 1 || bad != 1 || missing != 1 {
		t.Errorf("ok=%d bad=%d missing=%d, 應為 1/1/1", ok, bad, missing)
	}
}

func TestMakeSFVSorted(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "b.txt", "b")
	write(t, dir, "A.txt", "a")
	es, err := MakeSFV(dir, []string{"b.txt", "A.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if es[0].Name != "A.txt" || es[1].Name != "b.txt" {
		t.Errorf("排序不對: %+v", es)
	}
	var sb strings.Builder
	if err := WriteSFV(&sb, es); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sb.String(), "\r\n") {
		t.Error("SFV 慣例是 CRLF 換行")
	}
}
