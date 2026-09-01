package pdf

import (
	"strconv"
	"strings"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/korean"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/unicode/norm"
)

// Font 是一個字型資源:把字串裡的位元組變回文字與寬度所需要的一切。
type Font struct {
	// composite 為真表示這是複合字型(Type0),字碼可能不只一個位元組。
	composite bool
	// enc 是碼 → CID 的對照。Identity 編碼時為 nil,那時候碼就是 CID。
	enc      *cmap
	toUni    *cmap
	simple   [256]string // 簡單字型:碼 → 文字
	widths   map[uint32]float64
	dw       float64
	coreFont string            // 核心字型名,用來查內建度量
	cjk      encoding.Encoding // 預先定義的中日韓編碼
	codeLen  int
	// symbolFont 為真表示這是 Symbol 或 ZapfDingbats。它們的字碼與
	// ASCII 沒有關係,不能套用下面那條退路。
	symbolFont bool
	// scale 是寬度的換算倍率。一般字型的寬度以千分之一字寬為單位,
	// Type3 字型則是自己的座標系,要靠 FontMatrix 換算。
	scale float64
}

// Glyph 是解出來的一個字。
type Glyph struct {
	Code uint32
	Text string
	// Width 是前進量,以千分之一字寬為單位。
	Width float64
	// Space 為真表示這是單位元組的空白字元。字間距(Tw)只加在它身上,
	// 加錯地方會讓中文的字距整片跑掉。
	Space bool
}

// Decode 把一個字串拆成字。
func (f *Font) Decode(s string) []Glyph {
	if f.cjk != nil {
		return f.decodeCJK(s)
	}
	defLen := 1
	if f.composite {
		defLen = 2
	}
	if f.codeLen > 0 {
		defLen = f.codeLen
	}
	var codes []code
	switch {
	case f.enc != nil && len(f.enc.spaces) > 0:
		codes = f.enc.split(s, defLen)
	case f.toUni != nil && len(f.toUni.spaces) > 0 && f.composite:
		codes = f.toUni.split(s, defLen)
	default:
		for i := 0; i+defLen <= len(s); i += defLen {
			codes = append(codes, code{beUint(s[i : i+defLen]), defLen})
		}
	}
	out := make([]Glyph, 0, len(codes))
	for _, c := range codes {
		g := Glyph{Code: c.val, Space: c.n == 1 && c.val == 32}
		g.Text = f.textOf(c.val)
		g.Width = f.widthOf(c.val, g.Text)
		out = append(out, g)
	}
	return out
}

// textOf 查一個字碼是什麼字。
//
// 順序是刻意的:ToUnicode 最可靠(那是產生檔案的程式自己寫下來的
// 對照表),其次才是編碼表。反過來的話,凡是有自訂編碼的字型都會解錯。
func (f *Font) textOf(v uint32) string {
	if f.toUni != nil {
		if s, ok := f.toUni.lookup(v); ok && s != "" && s != "\x00" {
			return s
		}
	}
	if !f.composite && v < 256 {
		if s := f.simple[v]; s != "" {
			return s
		}
		// 最後的退路:符號字型以外,把可列印的 ASCII 範圍照原樣當字。
		// 沒有這一條的話,「內建編碼、又沒有 ToUnicode」的字型會整頁
		// 空白 —— 而那種字型的字碼多半就是 ASCII。
		if !f.symbolFont && v >= 32 && v < 127 {
			return string(rune(v))
		}
		return ""
	}
	if f.composite {
		// 沒有 ToUnicode 的複合字型:字碼是字型內部的編號,
		// 沒有任何線索可以變回文字。硬轉會得到一串看起來像字的東西,
		// 那比空白更糟 —— 使用者會以為那就是內容。
		return ""
	}
	return ""
}

func (f *Font) scaleOr1() float64 {
	if f.scale > 0 {
		return f.scale
	}
	return 1
}

func (f *Font) widthOf(v uint32, text string) float64 {
	key := v
	if f.composite && f.enc != nil {
		if cid, ok := f.enc.cid(v); ok {
			key = cid
		}
	}
	if w, ok := f.widths[key]; ok {
		return w * f.scaleOr1()
	}
	if f.coreFont != "" && text != "" {
		if w := coreWidth(f.coreFont, []rune(text)[0]); w > 0 {
			return w
		}
	}
	if f.dw > 0 {
		return f.dw
	}
	if f.composite {
		return 1000
	}
	return 500
}

// decodeCJK 處理用預先定義的中日韓 CMap 編碼的字型。
//
// 那些 CMap 的檔案不在 PDF 裡(它們是 Adobe 發布的外部資源),但它們的
// 字碼**就是該語系的傳統編碼的位元組**,所以直接用字集解碼器就解得出來。
// 舊的中文 PDF 幾乎都是這一種,而它們通常沒有 ToUnicode。
func (f *Font) decodeCJK(s string) []Glyph {
	dec := f.cjk.NewDecoder()
	var out []Glyph
	for i := 0; i < len(s); {
		n := 1
		if s[i] >= 0x80 && i+1 < len(s) {
			n = 2
		}
		raw := s[i : i+n]
		i += n
		text := ""
		if b, err := dec.Bytes([]byte(raw)); err == nil {
			text = string(b)
		}
		g := Glyph{Code: beUint(raw), Text: text, Space: n == 1 && raw[0] == 32}
		g.Width = f.dw
		if g.Width == 0 {
			g.Width = 1000
		}
		if n == 1 {
			g.Width = 500
		}
		out = append(out, g)
	}
	return out
}

// predefinedCMap 把預先定義的 CMap 名稱對到字集。
func predefinedCMap(name string) (encoding.Encoding, bool) {
	switch {
	case strings.HasPrefix(name, "Identity"):
		return nil, false
	case strings.Contains(name, "UCS2"), strings.Contains(name, "UTF16"):
		return unicodeBE{}, true
	case strings.Contains(name, "B5"):
		return traditionalchinese.Big5, true
	case strings.Contains(name, "GBK"), strings.Contains(name, "GB-EUC"),
		strings.Contains(name, "GBpc"), strings.Contains(name, "GB2312"):
		return simplifiedchinese.GBK, true
	case strings.Contains(name, "RKSJ"):
		return japanese.ShiftJIS, true
	case strings.Contains(name, "EUC-H"), strings.Contains(name, "EUC-V"):
		return japanese.EUCJP, true
	case strings.Contains(name, "KSC"), strings.Contains(name, "UHC"):
		return korean.EUCKR, true
	}
	return nil, false
}

// unicodeBE 是 UTF-16BE 的解碼器。
//
// x/text 的 unicode 套件要先建一個 Encoding 物件才用得了,而這裡只需要
// 「兩個位元組換一個字」,自己做比拉一層設定短。
type unicodeBE struct{}

func (unicodeBE) NewDecoder() *encoding.Decoder {
	return &encoding.Decoder{Transformer: utf16beTransformer{}}
}

func (unicodeBE) NewEncoder() *encoding.Encoder { return nil }

// --- 簡單字型的編碼 ---

// baseEncoding 取一張基礎編碼表。
//
// WinAnsi 與 cp1252 只在幾個沒有字形的碼位上不同,MacRoman 與
// Macintosh 字集同理 —— 兩者都直接借用現成的解碼器,不再抄一份 256 格的表。
func baseEncoding(name string) [256]string {
	var t [256]string
	var dec *encoding.Decoder
	switch name {
	case "WinAnsiEncoding":
		dec = charmap.Windows1252.NewDecoder()
	case "MacRomanEncoding":
		dec = charmap.Macintosh.NewDecoder()
	default:
		// StandardEncoding。ASCII 的範圍與 ASCII 相同,只有兩個引號
		// 不一樣;上半部是另一套排法,交給名稱表處理。
		for i := 32; i < 127; i++ {
			t[i] = string(rune(i))
		}
		t[0x27] = "’"
		t[0x60] = "‘"
		for c, n := range standardUpper {
			t[c] = glyphRune(n)
		}
		return t
	}
	for i := 32; i < 256; i++ {
		b, err := dec.Bytes([]byte{byte(i)})
		if err != nil || len(b) == 0 {
			continue
		}
		t[i] = string(b)
	}
	return t
}

// standardUpper 是 StandardEncoding 上半部的字形名稱。
//
// 這一段不能用任何現成的字集代替:它是 Adobe 自己的排法,
// 與 Latin-1 沒有關係。
var standardUpper = map[int]string{
	0241: "exclamdown", 0242: "cent", 0243: "sterling", 0244: "fraction",
	0245: "yen", 0246: "florin", 0247: "section", 0250: "currency",
	0251: "quotesingle", 0252: "quotedblleft", 0253: "guillemotleft",
	0254: "guilsinglleft", 0255: "guilsinglright", 0256: "fi", 0257: "fl",
	0261: "endash", 0262: "dagger", 0263: "daggerdbl", 0264: "periodcentered",
	0266: "paragraph", 0267: "bullet", 0270: "quotesinglbase", 0271: "quotedblbase",
	0272: "quotedblright", 0273: "guillemotright", 0274: "ellipsis",
	0275: "perthousand", 0277: "questiondown", 0301: "grave", 0302: "acute",
	0303: "circumflex", 0304: "tilde", 0305: "macron", 0306: "breve",
	0307: "dotaccent", 0310: "dieresis", 0312: "ring", 0313: "cedilla",
	0315: "hungarumlaut", 0316: "ogonek", 0317: "caron", 0320: "emdash",
	0341: "AE", 0343: "ordfeminine", 0350: "Lslash", 0351: "Oslash",
	0352: "OE", 0353: "ordmasculine", 0361: "ae", 0365: "dotlessi",
	0370: "lslash", 0371: "oslash", 0372: "oe", 0373: "germandbls",
}

// glyphRune 把字形名稱換成文字。
//
// 三條路,依可靠度排序:`uniXXXX` 這種名稱直接帶著字碼;常見的符號名
// 查表;剩下的靠「基本字母 + 重音名稱」組出來再正規化 —— 那一類名稱
// (eacute、Ocircumflex、ccedilla)有幾百個,但全部是同一個規則,
// 列成表只是把規則抄成資料。
func glyphRune(name string) string {
	if name == "" {
		return ""
	}
	if i := strings.IndexByte(name, '.'); i > 0 {
		name = name[:i]
	}
	if s, ok := glyphNames[name]; ok {
		return s
	}
	if rest, ok := strings.CutPrefix(name, "uni"); ok && len(rest) >= 4 {
		var sb strings.Builder
		for i := 0; i+4 <= len(rest); i += 4 {
			if n, err := strconv.ParseUint(rest[i:i+4], 16, 32); err == nil {
				sb.WriteRune(rune(n))
			}
		}
		if sb.Len() > 0 {
			return sb.String()
		}
	}
	if rest, ok := strings.CutPrefix(name, "u"); ok && len(rest) >= 4 && len(rest) <= 6 {
		if n, err := strconv.ParseUint(rest, 16, 32); err == nil {
			return string(rune(n))
		}
	}
	if len([]rune(name)) == 1 {
		return name
	}
	// 重音字母:基本字母加上重音的名稱。
	for suffix, mark := range accentMarks {
		if base, ok := strings.CutSuffix(name, suffix); ok && len([]rune(base)) == 1 {
			return norm.NFC.String(base + string(mark))
		}
	}
	return ""
}

// accentMarks 是重音名稱對應的組合用符號。
var accentMarks = map[string]rune{
	"acute": 0x0301, "grave": 0x0300, "circumflex": 0x0302, "tilde": 0x0303,
	"macron": 0x0304, "breve": 0x0306, "dotaccent": 0x0307, "dieresis": 0x0308,
	"ring": 0x030A, "hungarumlaut": 0x030B, "caron": 0x030C, "cedilla": 0x0327,
	"ogonek": 0x0328, "slash": 0x0338,
}

// glyphNames 是符號與標點的字形名稱。字母的名稱不必列(名稱就是那個字母),
// 重音字母也不必(用組合的)。
var glyphNames = map[string]string{
	"space": " ", "exclam": "!", "quotedbl": "\"", "numbersign": "#",
	"dollar": "$", "percent": "%", "ampersand": "&", "quotesingle": "'",
	"quoteright": "’", "quoteleft": "‘", "parenleft": "(",
	"parenright": ")", "asterisk": "*", "plus": "+", "comma": ",",
	"hyphen": "-", "period": ".", "slash": "/", "colon": ":", "semicolon": ";",
	"less": "<", "equal": "=", "greater": ">", "question": "?", "at": "@",
	"bracketleft": "[", "backslash": "\\", "bracketright": "]",
	"asciicircum": "^", "underscore": "_", "grave": "`", "braceleft": "{",
	"bar": "|", "braceright": "}", "asciitilde": "~",
	"zero": "0", "one": "1", "two": "2", "three": "3", "four": "4",
	"five": "5", "six": "6", "seven": "7", "eight": "8", "nine": "9",
	"exclamdown": "¡", "cent": "¢", "sterling": "£", "fraction": "⁄",
	"yen": "¥", "florin": "ƒ", "section": "§", "currency": "¤",
	"quotedblleft": "“", "quotedblright": "”",
	"guillemotleft": "«", "guillemotright": "»",
	"guilsinglleft": "‹", "guilsinglright": "›",
	"fi": "ﬁ", "fl": "ﬂ", "ff": "ﬀ", "ffi": "ﬃ", "ffl": "ﬄ",
	"endash": "–", "emdash": "—", "dagger": "†", "daggerdbl": "‡",
	"periodcentered": "·", "paragraph": "¶", "bullet": "•",
	"quotesinglbase": "‚", "quotedblbase": "„", "ellipsis": "…",
	"perthousand": "‰", "questiondown": "¿", "dotlessi": "ı",
	"circumflex": "ˆ", "tilde": "˜", "macron": "¯", "breve": "˘",
	"dotaccent": "˙", "dieresis": "¨", "ring": "˚", "cedilla": "¸",
	"hungarumlaut": "˝", "ogonek": "˛", "caron": "ˇ", "acute": "´",
	"AE": "Æ", "ae": "æ", "OE": "Œ", "oe": "œ",
	"Lslash": "Ł", "lslash": "ł", "Oslash": "Ø", "oslash": "ø",
	"ordfeminine": "ª", "ordmasculine": "º", "germandbls": "ß",
	"degree": "°", "plusminus": "±", "mu": "µ", "multiply": "×",
	"divide": "÷", "copyright": "©", "registered": "®", "trademark": "™",
	"onehalf": "½", "onequarter": "¼", "threequarters": "¾",
	"onesuperior": "¹", "twosuperior": "²", "threesuperior": "³",
	"brokenbar": "¦", "logicalnot": "¬", "minus": "−", "notequal": "≠",
	"Euro": "€", "euro": "€", "nbspace": " ", "softhyphen": "­",
	"Delta": "Δ", "Omega": "Ω", "pi": "π", "summation": "∑", "radical": "√",
	"infinity": "∞", "integral": "∫", "approxequal": "≈", "lessequal": "≤",
	"greaterequal": "≥", "partialdiff": "∂", "product": "∏", "lozenge": "◊",
	"apple": "", "arrowleft": "←", "arrowright": "→",
	"arrowup": "↑", "arrowdown": "↓",
}
