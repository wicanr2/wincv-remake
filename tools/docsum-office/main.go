// docsum-office 用 officedoc 打開一批 Office 文件,把每一段(Parts)的每個區塊
// 的所有欄位、每個 span,以及內嵌圖片的大小與像素 sha256,以 TAB 分隔印出,
// 當 Rust 改寫(rust/crates/officedoc/examples/docsum.rs)的對照真值。
//
//	tools/go.sh run ./tools/docsum-office [@清單檔] [檔案...]
//
// 參數是檔案路徑;寫成 @清單檔 時從那個檔案讀,每行一個路徑(清單很長時參數會超過上限,
// 而 tools/go.sh 的容器不接 stdin)。序號依參數展開後的順序從 0 起算。
// 輸出的列(欄位一律以 TAB 分隔,字串裡的 \ TAB 換行 CR 寫成 \\ \t \n \r):
//
//	F  序號  路徑
//	E  officedoc.Open 的錯誤訊息
//	D  種類(0 文件 1 簡報 2 試算表)  標題  段數
//	P  段號  段名
//	B  段號  區塊號  kind  level  lang  ordered  num  marker  src  alt  aligns  span數  line數  row數
//	S  style  href  text
//	L  一行(Code / Pre 的 Lines)
//	R  一列(儲存格以 TAB 分隔)
//	I  src  OK  位元組數  原始資料 sha256  格式  寬  高  像素 sha256(非預乘 RGBA8)
//	I  src  DECODE  位元組數  原始資料 sha256(解不開;訊息不印,兩邊的解碼器不同)
//	I  src  ERR  Image() 的錯誤訊息
//	X  panic 的訊息(Go 版當掉時)
//
// 加 -xml 時改成逐 token 印出每個 ZIP 組件的 XML(見 xmlTokens)。
//
// Go 的字串可以裝不合法的 UTF-8,Rust 的不行。印出前一律把每個壞位元組換成
// 一個 U+FFFD(與 Rust 端 utf8_lossy_go 相同),兩邊的輸出才能逐位元組比較。
package main

import (
	"bufio"
	"crypto/sha256"
	"encoding/xml"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/wicanr2/wincv-remake/internal/imgfmt"
	"github.com/wicanr2/wincv-remake/internal/markdown"
	"github.com/wicanr2/wincv-remake/internal/officedoc"
	"github.com/wicanr2/wincv-remake/internal/ooxml"
)

var out = bufio.NewWriterSize(os.Stdout, 1<<20)

// esc 把字串整理成一欄:壞位元組換成 U+FFFD,再跳脫分隔字元。
func esc(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		i += n
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '\t':
			sb.WriteString(`\t`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		default:
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

func row(fields ...string) {
	out.WriteString(strings.Join(fields, "\t"))
	out.WriteByte('\n')
}

func b2s(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func main() {
	defer out.Flush()
	var names []string
	tokens := false
	for _, a := range os.Args[1:] {
		if a == "-xml" {
			tokens = true
			continue
		}
		if !strings.HasPrefix(a, "@") {
			names = append(names, a)
			continue
		}
		b, err := os.ReadFile(a[1:])
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l != "" {
				names = append(names, l)
			}
		}
	}
	for i, name := range names {
		row("F", fmt.Sprint(i), esc(name))
		if tokens {
			xmlTokens(name)
			continue
		}
		one(name)
	}
}

func one(name string) {
	defer func() {
		if r := recover(); r != nil {
			row("X", esc(fmt.Sprint(r)))
		}
	}()
	d, err := officedoc.Open(name)
	if err != nil {
		row("E", esc(err.Error()))
		return
	}
	defer d.Close()
	row("D", fmt.Sprint(int(d.Kind)), esc(d.Title), fmt.Sprint(len(d.Parts)))
	for i, p := range d.Parts {
		row("P", fmt.Sprint(i), esc(p.Title))
	}
	for i := range d.Parts {
		for j, b := range d.Blocks(i) {
			block(d, i, j, b)
		}
	}
}

func block(d *officedoc.Doc, i, j int, b markdown.Block) {
	row("B", fmt.Sprint(i), fmt.Sprint(j), fmt.Sprint(int(b.Kind)), fmt.Sprint(b.Level), esc(b.Lang),
		b2s(b.Ordered), fmt.Sprint(b.Num), esc(b.Marker), esc(b.Src), esc(b.Alt), esc(string(b.Aligns)),
		fmt.Sprint(len(b.Spans)), fmt.Sprint(len(b.Lines)), fmt.Sprint(len(b.Rows)))
	for _, s := range b.Spans {
		row("S", fmt.Sprint(int(s.Style)), esc(s.Href), esc(s.Text))
	}
	for _, l := range b.Lines {
		row("L", esc(l))
	}
	for _, r := range b.Rows {
		cells := make([]string, len(r))
		for k, c := range r {
			cells[k] = esc(c)
		}
		row(append([]string{"R"}, cells...)...)
	}
	if b.Kind == markdown.Image {
		img(d, b.Src)
	}
}

func img(d *officedoc.Doc, src string) {
	data, err := d.Image(src)
	if err != nil {
		row("I", esc(src), "ERR", esc(err.Error()))
		return
	}
	raw := sha256.Sum256(data)
	m, kind, err := decode(src, data)
	if err != nil {
		// 錯誤訊息不印:兩邊的圖片解碼器不同(Go 的 image/*、Rust 的 image crate),
		// 同一張壞圖的訊息不會一樣。要比的是「哪些圖解不開」。
		row("I", esc(src), "DECODE", fmt.Sprint(len(data)), hex.EncodeToString(raw[:]))
		return
	}
	bd := m.Bounds()
	n := image.NewNRGBA(image.Rect(0, 0, bd.Dx(), bd.Dy()))
	draw.Draw(n, n.Bounds(), m, bd.Min, draw.Src)
	pix := sha256.Sum256(n.Pix)
	row("I", esc(src), "OK", fmt.Sprint(len(data)), hex.EncodeToString(raw[:]), kind,
		fmt.Sprint(bd.Dx()), fmt.Sprint(bd.Dy()), hex.EncodeToString(pix[:]))
}

// decode 把解碼器的 panic 轉成錯誤,一張壞圖不影響其餘的。
func decode(name string, data []byte) (m image.Image, kind string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return imgfmt.Decode(name, data)
}

// xmlTokens 是 -xml 模式:把 ZIP 包裡每一個組件交給 ooxml.NewDecoder,印出每一個 token,
// 直到錯誤或結尾。用來單獨對照 XML 解碼器(Rust 端是 Go encoding/xml 的移植)。
//
//	Z  打不開的錯誤訊息
//	N  組件路徑
//	T  S  space  local  (屬性 space  local  value)...
//	T  E  space  local
//	T  C  文字      T  M  註解      T  P  target  內容      T  D  宣告
//	T  X  錯誤訊息(io.EOF 寫成 EOF)
func xmlTokens(name string) {
	defer func() {
		if r := recover(); r != nil {
			row("X", esc(fmt.Sprint(r)))
		}
	}()
	p, err := ooxml.Open(name)
	if err != nil {
		row("Z", esc(err.Error()))
		return
	}
	defer p.Close()
	for _, n := range p.Names() {
		row("N", esc(n))
		b, err := p.Bytes(n)
		if err != nil {
			row("T", "X", esc(err.Error()))
			continue
		}
		d := ooxml.NewDecoder(b)
		for {
			tok, err := d.Token()
			if err != nil {
				row("T", "X", esc(err.Error()))
				break
			}
			switch t := tok.(type) {
			case xml.StartElement:
				f := []string{"T", "S", esc(t.Name.Space), esc(t.Name.Local)}
				for _, a := range t.Attr {
					f = append(f, esc(a.Name.Space), esc(a.Name.Local), esc(a.Value))
				}
				row(f...)
			case xml.EndElement:
				row("T", "E", esc(t.Name.Space), esc(t.Name.Local))
			case xml.CharData:
				row("T", "C", esc(string(t)))
			case xml.Comment:
				row("T", "M", esc(string(t)))
			case xml.ProcInst:
				row("T", "P", esc(t.Target), esc(string(t.Inst)))
			case xml.Directive:
				row("T", "D", esc(string(t)))
			}
		}
	}
}
