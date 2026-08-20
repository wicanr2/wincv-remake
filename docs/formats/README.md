# 檔案格式規格

WinCV 隨附的資料檔逐一逆向的結果。每份規格都附「怎麼驗」,
並標明哪些是實測、哪些還是推測。

| 規格 | 對象 | 狀態 |
|---|---|---|
| [fon-bitmap-font.md](fon-bitmap-font.md) | `cvga.fon` `CVGA1018.FON` `cvga1224.FON` `WinCV.fon` | 完整,兩支解析器互為對照 |
| [eten-bitmap-font.md](eten-bitmap-font.md) | 倚天 `STDFONT.15` `SPCFONT.15` `SPCFSUPP.15` | 三區完整,補充區的洞表未解 |
| [dict-dat.md](dict-dat.md) | `eng.txt.dat` `chi.txt.dat` `kk.txt.dat` `origin-verb.txt.dat` | 資料完整,`.idx` 只解到一半(不需要) |
| [keyword-cfg.md](keyword-cfg.md) | `keyword.cfg` `keyword_*.cfg` | 完整 |
| [misc-config.md](misc-config.md) | `default.fil` `dir.doc` `ce.ful` `file_id.diz` | 完整 |
| [forth-image.md](forth-image.md) | `WINCV.IMG` | 版面與符號表完整,部分欄位語意未解 |

## 通則

**編碼**:所有文字檔都是 Big5(CP950)。`.cfg` 的註解與說明文字、
字典的中文解釋、`ce.ful` 的符號表都是。

**換行**:多為 CRLF,但不一致 —— 解析時兩種都要吃。

**驗證方式**:每份規格的解析器都有測試,而且盡量用**原版自己的檔案**
當 golden data,而不是自己造的樣本。自己造的樣本只會驗到自己的理解,
驗不到真實檔案的邊角。
