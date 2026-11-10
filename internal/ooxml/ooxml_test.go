package ooxml

import (
	"encoding/xml"
	"fmt"
	"reflect"
	"testing"
)

func tokens(src string) []string {
	d := NewDecoder([]byte(src))
	var out []string
	for {
		tok, err := d.Token()
		if err != nil {
			out = append(out, "ERR "+err.Error())
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			out = append(out, fmt.Sprintf("<%s:%s>", t.Name.Space, t.Name.Local))
		case xml.EndElement:
			out = append(out, fmt.Sprintf("</%s:%s>", t.Name.Space, t.Name.Local))
		case xml.CharData:
			out = append(out, fmt.Sprintf("%q", string(t)))
		}
	}
}

// NewDecoder 不自動關閉 br:PowerPoint 的 <a:br> 底下帶著 <a:rPr/>,
// 結構要照檔案原樣解出來,</a:br> 之後的內容留在原本的段落裡。
// 對應 Rust 版 br_with_children_keeps_structure。
func TestBrWithChildrenKeepsStructure(t *testing.T) {
	got := tokens(`<r xmlns:a="u"><a:p><a:br><a:rPr/></a:br><a:t>x</a:t></a:p></r>`)
	want := []string{"<:r>", "<u:p>", "<u:br>", "<u:rPr>", "</u:rPr>", "</u:br>",
		"<u:t>", `"x"`, "</u:t>", "</u:p>", "</:r>", "ERR EOF"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("拿到 %q\n要   %q", got, want)
	}
}

// 錯配的結束標記在非嚴格模式下改成關閉堆疊頂端,一路往上關。
func TestMismatchedEndClosesOutward(t *testing.T) {
	got := tokens(`<r><p><b>x</p></r>`)
	want := []string{"<:r>", "<:p>", "<:b>", `"x"`, "</:b>", "</:p>", "</:r>", "ERR EOF"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("拿到 %q\n要   %q", got, want)
	}
}
