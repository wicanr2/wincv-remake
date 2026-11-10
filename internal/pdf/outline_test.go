package pdf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// buildPDF 把物件依序編號(第 i 個是 i 0 obj),算好 xref 寫成一份 PDF。
func buildPDF(t *testing.T, objs []string) string {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("%PDF-1.4\n")
	offs := make([]int, len(objs))
	for i, o := range objs {
		offs[i] = sb.Len()
		fmt.Fprintf(&sb, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := sb.Len()
	fmt.Fprintf(&sb, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offs {
		fmt.Fprintf(&sb, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&sb, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	name := filepath.Join(t.TempDir(), "named.pdf")
	if err := os.WriteFile(name, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}

// 具名目的地有兩個存放處:字串鍵查 /Names 底下的 /Dests 名稱樹,
// 名稱鍵(PDF 1.1)查目錄的 /Dests 字典;查不到時兩邊都再試一次。
// pdfcpu 的 DereferenceDestArray 在只讀檔案時會 nil pointer panic
// (它查的 XRefTable.Names 沒有建),實測 Vorbis_I_spec.pdf。
func TestNamedDestinations(t *testing.T) {
	page := "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 200] >>"
	name := buildPDF(t, []string{
		/* 1 */ "<< /Type /Catalog /Pages 2 0 R /Outlines 6 0 R /Names << /Dests 11 0 R >> /Dests 14 0 R >>",
		/* 2 */ "<< /Type /Pages /Kids [3 0 R 4 0 R 5 0 R] /Count 3 >>",
		/* 3 */ page,
		/* 4 */ page,
		/* 5 */ page,
		/* 6 */ "<< /Type /Outlines /First 7 0 R /Last 10 0 R /Count 4 >>",
		/* 7 */ "<< /Title (string key) /Parent 6 0 R /Next 8 0 R /Dest (sec.2) >>",
		/* 8 */ "<< /Title (name key) /Parent 6 0 R /Prev 7 0 R /Next 9 0 R /Dest /chap3 >>",
		/* 9 */ "<< /Title (hex key in action) /Parent 6 0 R /Prev 8 0 R /Next 10 0 R /A << /S /GoTo /D <7365632E31> >> >>",
		/* 10 */ "<< /Title (name falls back to tree) /Parent 6 0 R /Prev 9 0 R /Dest /sec.3 >>",
		/* 11 */ "<< /Kids [12 0 R 13 0 R] >>",
		/* 12 */ "<< /Limits [(sec.1) (sec.1)] /Names [(sec.1) [3 0 R /Fit]] >>",
		/* 13 */ "<< /Limits [(sec.2) (sec.3)] /Names [(sec.2) [4 0 R /XYZ 0 0 0] (sec.3) << /D [5 0 R /Fit] >>] >>",
		/* 14 */ "<< /chap3 [5 0 R /Fit] >>",
	})
	d, err := Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var got []string
	for _, b := range d.Outline() {
		got = append(got, fmt.Sprintf("%s=%d", b.Title, b.Page))
	}
	want := []string{"string key=2", "name key=3", "hex key in action=1", "name falls back to tree=3"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("拿到 %q\n要   %q", got, want)
	}
}
