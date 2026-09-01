// Package ooxml 讀 Office Open XML 的封裝格式(OPC)。
//
// `.docx` / `.pptx` / `.xlsx` 三者的外殼是同一套:一個 ZIP,裡面有
// `[Content_Types].xml` 說明每個組件是什麼,加上散在各處的 `_rels/*.rels`
// 說明組件之間怎麼互相參照。三者**只共用這層外殼** —— 裡面的 XML 結構
// 完全不同,沒有共用的餘地,也不該硬湊。
//
// 這一包提供的就是外殼:組件查表、關聯解析、以及一組讓上層寫遞迴下降
// 解析器時不必重複的 XML 走訪工具。
package ooxml

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strings"
)

// 常用的命名空間。比對一律用網址而不是前綴 —— 前綴是檔案自己取的,
// 同一個命名空間在不同檔案裡可以叫不同的名字。
const (
	NSRel      = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	NSDrawing  = "http://schemas.openxmlformats.org/drawingml/2006/main"
	NSWord     = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
	NSPres     = "http://schemas.openxmlformats.org/presentationml/2006/main"
	NSSheet    = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
	NSMarkupC  = "http://schemas.openxmlformats.org/markup-compatibility/2006"
	NSPackRel  = "http://schemas.openxmlformats.org/package/2006/relationships"
	NSCTypes   = "http://schemas.openxmlformats.org/package/2006/content-types"
	NSXML1998  = "http://www.w3.org/XML/1998/namespace"
	NSDrawWP   = "http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing"
	NSDrawSpre = "http://schemas.openxmlformats.org/drawingml/2006/spreadsheetDrawing"
)

// MaxPartBytes 是單一組件解開後的上限。
//
// 有這道限制不是為了省記憶體,是因為 ZIP 的解壓比可以做到很大:
// 一個幾 KB 的檔案能解出好幾 GB。檔案管理器會開到來路不明的檔案。
const MaxPartBytes = 96 << 20

// Rel 是一筆關聯。
type Rel struct {
	ID     string
	Type   string
	Target string
	// External 為真表示 Target 是外部位址(http、mailto、磁碟路徑),
	// 不是包內的組件,不能拿去查表。
	External bool
}

// Package 是一個打開著的 OPC 包。用完要 Close。
type Package struct {
	zr *zip.Reader
	rc io.Closer

	files    map[string]*zip.File
	names    []string
	defaults map[string]string // 副檔名(小寫) → 內容型別
	override map[string]string // 組件路徑 → 內容型別
	rels     map[string]map[string]Rel
}

// Open 打開一個 OPC 包。
func Open(name string) (*Package, error) {
	zr, err := zip.OpenReader(name)
	if err != nil {
		return nil, fmt.Errorf("打不開:%w", err)
	}
	p, err := New(&zr.Reader, zr)
	if err != nil {
		zr.Close()
		return nil, err
	}
	return p, nil
}

// New 從一個已經打開的 ZIP 建 Package。rc 可以是 nil。
func New(zr *zip.Reader, rc io.Closer) (*Package, error) {
	p := &Package{
		zr: zr, rc: rc,
		files:    map[string]*zip.File{},
		defaults: map[string]string{},
		override: map[string]string{},
		rels:     map[string]map[string]Rel{},
	}
	for _, f := range zr.File {
		if strings.HasSuffix(f.Name, "/") {
			continue
		}
		n := Clean(f.Name)
		if _, dup := p.files[n]; dup {
			continue
		}
		p.files[n] = f
		p.names = append(p.names, n)
	}
	if err := p.readContentTypes(); err != nil {
		return nil, err
	}
	return p, nil
}

// Close 關掉底下的檔案。
func (p *Package) Close() error {
	if p.rc != nil {
		return p.rc.Close()
	}
	return nil
}

// Clean 把 ZIP 內的路徑正規化成查表用的形式。
func Clean(name string) string {
	n := strings.ReplaceAll(name, "\\", "/")
	n = strings.TrimPrefix(n, "/")
	if n == "" {
		return n
	}
	return path.Clean(n)
}

// Has 回答包裡有沒有這個組件。
func (p *Package) Has(name string) bool {
	_, ok := p.files[Clean(name)]
	return ok
}

// Names 回傳所有組件的路徑。
func (p *Package) Names() []string { return p.names }

// Bytes 讀出一個組件。
func (p *Package) Bytes(name string) ([]byte, error) {
	f, ok := p.files[Clean(name)]
	if !ok {
		return nil, fmt.Errorf("包裡沒有 %s", name)
	}
	if f.UncompressedSize64 > MaxPartBytes {
		return nil, fmt.Errorf("%s 太大(%d 位元組)", name, f.UncompressedSize64)
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	// 即使標頭說得小也要夾:標頭的大小欄位是壓縮方寫的,不保證誠實。
	b, err := io.ReadAll(io.LimitReader(rc, MaxPartBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > MaxPartBytes {
		return nil, fmt.Errorf("%s 太大", name)
	}
	return b, nil
}

// Decoder 開一個組件的 XML 解碼器。
func (p *Package) Decoder(name string) (*xml.Decoder, error) {
	b, err := p.Bytes(name)
	if err != nil {
		return nil, err
	}
	return NewDecoder(b), nil
}

// NewDecoder 建一個容忍度高一點的 XML 解碼器。
//
// Office 產生的檔案本身是合規的,但經過各種工具轉手之後不一定 ——
// 而一個沒宣告的實體參照不該讓整份文件打不開。
func NewDecoder(b []byte) *xml.Decoder {
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	return d
}

// ContentType 查一個組件的內容型別。查不到回空字串。
func (p *Package) ContentType(name string) string {
	n := Clean(name)
	if ct, ok := p.override["/"+n]; ok {
		return ct
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(n), "."))
	return p.defaults[ext]
}

// PartsByType 找出所有屬於某個內容型別的組件。
func (p *Package) PartsByType(ct string) []string {
	var out []string
	for _, n := range p.names {
		if p.ContentType(n) == ct {
			out = append(out, n)
		}
	}
	return out
}

func (p *Package) readContentTypes() error {
	b, err := p.Bytes("[Content_Types].xml")
	if err != nil {
		// 沒有這一份就不是合規的 OPC 包,但上層通常是靠固定路徑
		// (word/document.xml)找主體的,所以不當成致命錯誤。
		return nil
	}
	d := NewDecoder(b)
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "Default":
			ext := strings.ToLower(Attr(se, "Extension"))
			if ext != "" {
				p.defaults[ext] = Attr(se, "ContentType")
			}
		case "Override":
			if pn := Attr(se, "PartName"); pn != "" {
				p.override[pn] = Attr(se, "ContentType")
			}
		}
	}
	return nil
}

// RelsPath 回傳持有某個組件之關聯的那一份 .rels 的路徑。
// owner 傳空字串表示包本身的關聯(`_rels/.rels`)。
func RelsPath(owner string) string {
	if owner == "" {
		return "_rels/.rels"
	}
	n := Clean(owner)
	return path.Join(path.Dir(n), "_rels", path.Base(n)+".rels")
}

// Rels 取一個組件的關聯表。沒有關聯檔就回空表,不是錯誤 ——
// 大部分組件本來就沒有關聯。
func (p *Package) Rels(owner string) map[string]Rel {
	key := Clean(owner)
	if m, ok := p.rels[key]; ok {
		return m
	}
	m := map[string]Rel{}
	if b, err := p.Bytes(RelsPath(owner)); err == nil {
		d := NewDecoder(b)
		for {
			tok, err := d.Token()
			if err != nil {
				break
			}
			se, ok := tok.(xml.StartElement)
			if !ok || se.Name.Local != "Relationship" {
				continue
			}
			r := Rel{
				ID:       Attr(se, "Id"),
				Type:     Attr(se, "Type"),
				Target:   Attr(se, "Target"),
				External: strings.EqualFold(Attr(se, "TargetMode"), "External"),
			}
			if r.ID != "" {
				m[r.ID] = r
			}
		}
	}
	p.rels[key] = m
	return m
}

// Resolve 把關聯裡的目標接成包內的絕對路徑。
//
// [雷] 目標是相對於**持有關聯的那個組件所在的目錄**,不是相對於 ZIP 根。
// `ppt/slides/slide1.xml` 的關聯寫 `../media/image1.png`,要接成
// `ppt/media/image1.png`。拿根目錄去接會得到 `media/image1.png` ——
// 查不到,而查不到圖不會報錯,只會少一張圖。
func Resolve(owner, target string) string {
	t := strings.ReplaceAll(target, "\\", "/")
	if strings.HasPrefix(t, "/") {
		return Clean(t)
	}
	dir := ""
	if owner != "" {
		dir = path.Dir(Clean(owner))
		if dir == "." {
			dir = ""
		}
	}
	return Clean(path.Join(dir, t))
}

// RelTarget 由關聯編號查出包內的絕對路徑。外部位址與查不到的回空字串。
func (p *Package) RelTarget(owner, id string) string {
	r, ok := p.Rels(owner)[id]
	if !ok || r.External || r.Target == "" {
		return ""
	}
	return Resolve(owner, r.Target)
}

// RelsByType 取某個組件裡屬於某種型別的關聯,已解析成包內路徑。
func (p *Package) RelsByType(owner, typ string) []string {
	var out []string
	for _, r := range p.Rels(owner) {
		if r.External || !strings.HasSuffix(r.Type, typ) {
			continue
		}
		out = append(out, Resolve(owner, r.Target))
	}
	return out
}

// --- XML 走訪 ---

// Attr 依 local name 取屬性。
//
// 大部分屬性在同一個元素上不會跨命名空間撞名,所以照 local name 取
// 就夠。會撞的只有關聯編號(`r:id` 與 `id` 常常同時出現),那個用
// RelID 取,不要用這一支。
func Attr(se xml.StartElement, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local {
			return a.Value
		}
	}
	return ""
}

// AttrNS 依命名空間加 local name 取屬性。
func AttrNS(se xml.StartElement, space, local string) string {
	for _, a := range se.Attr {
		if a.Name.Local == local && a.Name.Space == space {
			return a.Value
		}
	}
	return ""
}

// RelID 取關聯編號屬性(`r:id`、`r:embed`、`r:link`)。
//
// [雷] 不能用 local name 取。`<p:sldId id="256" r:id="rId2"/>` 上面
// 兩個屬性的 local name 分別是 id 與 id —— 照 local name 取會拿到
// 投影片編號 256,而那是一個合法的字串,不會有任何錯誤,只是查不到組件。
func RelID(se xml.StartElement) string {
	for _, a := range se.Attr {
		if a.Name.Space != NSRel {
			continue
		}
		switch a.Name.Local {
		case "id", "embed", "link":
			return a.Value
		}
	}
	return ""
}

// Each 走訪目前元素的每一個子元素,走到對應的結束標記為止。
//
// fn 回傳 true 表示「這個子樹我自己讀完了」(自己遞迴呼叫 Each,
// 或自己呼叫 d.Skip);回傳 false 由 Each 跳過整個子樹。
// 兩種情形之後,解碼器都停在該子元素的結束標記之後 —— 這個不變式
// 是整組工具能巢狀使用的前提。
func Each(d *xml.Decoder, fn func(xml.StartElement) (bool, error)) error {
	for {
		tok, err := d.Token()
		if err == io.EOF {
			// 截斷的檔案:讀到哪裡算哪裡。已經解出來的內容仍然有用,
			// 而整份拒絕對使用者沒有任何好處。
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			handled, err := fn(t)
			if err != nil {
				return err
			}
			if !handled {
				if err := d.Skip(); err != nil {
					if err == io.EOF {
						return nil
					}
					return err
				}
			}
		case xml.EndElement:
			return nil
		}
	}
}

// Text 把目前元素底下的所有文字接起來,走到結束標記為止。
func Text(d *xml.Decoder) string {
	var sb strings.Builder
	depth := 0
	for {
		tok, err := d.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.CharData:
			sb.Write(t)
		case xml.StartElement:
			depth++
		case xml.EndElement:
			if depth == 0 {
				return sb.String()
			}
			depth--
		}
	}
	return sb.String()
}

// Root 讀到第一個開始標記為止,回傳它。
func Root(d *xml.Decoder) (xml.StartElement, error) {
	for {
		tok, err := d.Token()
		if err != nil {
			return xml.StartElement{}, err
		}
		if se, ok := tok.(xml.StartElement); ok {
			return se, nil
		}
	}
}
