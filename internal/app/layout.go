package app

// SaneLayout 把不合理的視窗尺寸換成可用的值。
//
// Ebiten 規定 Layout 要回正數,回不出來就 panic 掉整個程式。而輸入本身
// 就可能是壞的:Android 那一側算版面尺寸時要除以 deviceScale,那個值
// 拿不到時是 0,除出來的 +Inf 轉成 int 會變成負數。
//
// 規則是「不把壞輸入原樣傳出去」—— 退回上一次的好值,連一次都還沒拿到
// 就用螢幕尺寸,再不行給一個保底值。
//
// 這一步只管好 Layout 這個介面的契約。Ebiten 另外會拿**原始**的外部尺寸
// 乘上 deviceScale 去配置畫布,那條路徑看不到這裡的回傳值,所以尺寸真的
// 壞掉時光靠這裡救不回來 —— 源頭要在平台那一側處理。
//
// last 與 mon 傳 0 表示「沒有這個資訊」。
func SaneLayout(w, h, lastW, lastH, monW, monH int) (int, int) {
	if okDim(w) && okDim(h) {
		return w, h
	}
	if okDim(lastW) && okDim(lastH) {
		return lastW, lastH
	}
	if okDim(monW) && okDim(monH) {
		return monW, monH
	}
	return fallbackW, fallbackH
}

// maxDim 比任何真實螢幕都大。超過就表示算式壞掉了,不是真的有這麼大的螢幕。
const maxDim = 1 << 20

// 保底尺寸取一支小手機的 dp。畫得出東西比畫得好看重要 ——
// 這個值只有在「螢幕資訊也拿不到」時才會用到。
const (
	fallbackW = 360
	fallbackH = 640
)

func okDim(v int) bool { return v > 0 && v < maxDim }
