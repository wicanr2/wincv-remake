// inkdiff 比較兩張頁面圖有沒有把東西畫在同樣的地方。
//
// 不做逐像素比對:兩個渲染器對反鋸齒、字型微調(hinting)、次像素定位的
// 處理一定不同,逐像素比永遠是紅的,而且紅在無關緊要的地方。這裡改成把
// 兩張圖各自切成格子,量每一格的「墨水密度」(有多少比例的像素不是背景),
// 再比較兩張的密度圖 —— 要驗的是「東西有沒有畫、畫在哪一區、濃淡對不對」。
//
//	tools/go.sh run ./tools/inkdiff a.png b.png [格數]
//
// 輸出每一格的最大差、平均差與相關係數,並在超過門檻時以非零狀態結束。
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
)

func main() {
	cols := flag.Int("cols", 32, "橫向切幾格")
	rows := flag.Int("rows", 44, "縱向切幾格")
	maxBlock := flag.Float64("max-block", 0.25, "單格密度差的上限")
	minCorr := flag.Float64("min-corr", 0.80, "相關係數的下限")
	flag.Parse()
	if flag.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "用法:inkdiff [旗標] a.png b.png")
		os.Exit(2)
	}
	a, err := density(flag.Arg(0), *cols, *rows)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	b, err := density(flag.Arg(1), *cols, *rows)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}

	var sum, worst float64
	var wx, wy int
	for i := range a {
		d := math.Abs(a[i] - b[i])
		sum += d
		if d > worst {
			worst, wx, wy = d, i%*cols, i / *cols
		}
	}
	mean := sum / float64(len(a))
	corr := correlation(a, b)

	fmt.Printf("格數        %d×%d\n", *cols, *rows)
	fmt.Printf("平均密度差  %.4f\n", mean)
	fmt.Printf("最大密度差  %.4f(第 %d 列第 %d 欄)\n", worst, wy, wx)
	fmt.Printf("相關係數    %.4f\n", corr)
	fmt.Printf("墨水總量    %s=%.4f  %s=%.4f\n",
		flag.Arg(0), meanOf(a), flag.Arg(1), meanOf(b))

	fail := false
	if worst > *maxBlock {
		fmt.Printf("不過:有格子差超過 %.2f\n", *maxBlock)
		fail = true
	}
	if corr < *minCorr {
		fmt.Printf("不過:相關係數低於 %.2f\n", *minCorr)
		fail = true
	}
	if fail {
		os.Exit(1)
	}
	fmt.Println("通過")
}

// density 把一張圖切成格子,算每一格的墨水密度。
func density(path string, cols, rows int) ([]float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m, _, err := image.Decode(f)
	if err != nil {
		// image.Decode 需要註冊過的格式;png 一定有,jpeg 由 import 帶進來。
		f.Seek(0, 0)
		m, err = png.Decode(f)
		if err != nil {
			return nil, fmt.Errorf("%s 讀不開:%w", path, err)
		}
	}
	bd := m.Bounds()
	if bd.Dx() < cols || bd.Dy() < rows {
		return nil, fmt.Errorf("%s 太小(%d×%d)", path, bd.Dx(), bd.Dy())
	}
	out := make([]float64, cols*rows)
	count := make([]float64, cols*rows)
	for y := bd.Min.Y; y < bd.Max.Y; y++ {
		gy := (y - bd.Min.Y) * rows / bd.Dy()
		for x := bd.Min.X; x < bd.Max.X; x++ {
			gx := (x - bd.Min.X) * cols / bd.Dx()
			i := gy*cols + gx
			count[i]++
			r, g, b, _ := m.At(x, y).RGBA()
			// 灰階值 0.75 以下當成有墨。反鋸齒的邊緣會落在中間,
			// 門檻訂得高一點才不會讓「同一個字畫粗一點」變成差異。
			lum := (0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)) / 65535
			if lum < 0.75 {
				out[i]++
			}
		}
	}
	for i := range out {
		if count[i] > 0 {
			out[i] /= count[i]
		}
	}
	return out, nil
}

func meanOf(v []float64) float64 {
	s := 0.0
	for _, x := range v {
		s += x
	}
	return s / float64(len(v))
}

func correlation(a, b []float64) float64 {
	ma, mb := meanOf(a), meanOf(b)
	var num, da, db float64
	for i := range a {
		x, y := a[i]-ma, b[i]-mb
		num += x * y
		da += x * x
		db += y * y
	}
	if da == 0 || db == 0 {
		return 0
	}
	return num / math.Sqrt(da*db)
}
