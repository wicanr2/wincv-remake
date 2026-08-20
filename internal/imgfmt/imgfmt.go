// Package imgfmt 是看圖用的解碼器集合。
//
// 原版靠 FreeImage.dll 與 ijl15.dll(Intel JPEG),支援
// .JPG .GIF .PNG .TIF .PCX .TGA .BMP(.RLE .DIB) .ICO .PBM .PGM .PPM .KOA .PCD。
//
// Go 這邊:JPEG/PNG/GIF 用 stdlib,BMP/TIFF 用 x/image,
// PCX/TGA/PNM/ICO 自己寫(格式單純,不值得為它們拉相依)。
// KOA(Koala,C64 圖檔)與 PCD(Photo CD)還沒做,狀態記在 Formats。
package imgfmt

import (
	"bytes"
	"fmt"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"path"
	"strings"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Format 是一種圖檔格式的支援狀態。
type Format struct {
	Ext       []string
	Name      string
	Supported bool
	Note      string
}

// Formats 是原版支援的圖檔格式與 remake 目前的狀態。
var Formats = []Format{
	{[]string{".jpg", ".jpeg"}, "JPEG", true, "image/jpeg"},
	{[]string{".png"}, "PNG", true, "image/png"},
	{[]string{".gif"}, "GIF", true, "image/gif"},
	{[]string{".bmp", ".dib", ".rle"}, "BMP", true, "x/image/bmp"},
	{[]string{".tif", ".tiff"}, "TIFF", true, "x/image/tiff"},
	{[]string{".webp"}, "WebP", true, "x/image/webp(原版沒有,順手支援)"},
	{[]string{".pcx"}, "PCX", true, "自寫"},
	{[]string{".tga"}, "TGA", true, "自寫"},
	{[]string{".pbm", ".pgm", ".ppm", ".pnm"}, "PNM", true, "自寫"},
	{[]string{".ico"}, "ICO", true, "自寫(挑最大的那張)"},
	{[]string{".koa"}, "Koala", false, "C64 圖檔,尚未實作"},
	{[]string{".pcd"}, "PhotoCD", false, "Kodak Photo CD,尚未實作"},
}

// DetectFormat 依副檔名判斷。
func DetectFormat(name string) (Format, bool) {
	ext := strings.ToLower(path.Ext(name))
	for _, f := range Formats {
		for _, e := range f.Ext {
			if e == ext {
				return f, true
			}
		}
	}
	return Format{}, false
}

// IsImage 判斷這個檔名看起來是不是圖檔(不論支不支援)。
func IsImage(name string) bool {
	_, ok := DetectFormat(name)
	return ok
}

// Decode 解一張圖。先看副檔名選自寫的解碼器,其餘交給 image.Decode
// (它會用註冊過的解碼器認 magic number)。
func Decode(name string, data []byte) (image.Image, string, error) {
	f, ok := DetectFormat(name)
	if ok && !f.Supported {
		return nil, f.Name, fmt.Errorf("%s: %s 格式還沒實作(%s)", path.Base(name), f.Name, f.Note)
	}
	switch f.Name {
	case "PCX":
		img, err := DecodePCX(data)
		return img, "PCX", err
	case "TGA":
		img, err := DecodeTGA(data)
		return img, "TGA", err
	case "PNM":
		img, err := DecodePNM(data)
		return img, "PNM", err
	case "ICO":
		img, err := DecodeICO(data)
		return img, "ICO", err
	}
	img, kind, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", err
	}
	return img, strings.ToUpper(kind), nil
}
