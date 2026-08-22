#!/usr/bin/env bash
# 產生放進 zip 的「讀我」。用法:readme-for.sh <平台代號> <執行檔名>
set -euo pipefail
PLAT=$1
EXE=$2

case "$PLAT" in
linux-amd64)   RUN="chmod +x $EXE   # 如果解壓工具沒保住權限
./$EXE [要瀏覽的目錄]" ;;
windows-amd64) RUN="$EXE [要瀏覽的目錄]" ;;
macos-universal) RUN="chmod +x $EXE
xattr -dr com.apple.quarantine $EXE   # 沒有簽章,第一次要先解除隔離
./$EXE [要瀏覽的目錄]" ;;
android)       RUN="adb install $EXE
(或把 $EXE 傳到手機上點開,要允許安裝未知來源)" ;;
esac

cat <<EOF
WinCV Remake
============

1999–2011 年台灣共享軟體 WinCV 0.52(CView for Windows,原作者 Lcc Wizard)
的重製版,用 Go 重寫。原始碼:https://github.com/wicanr2/wincv-remake

怎麼跑
------

$RUN

第一次進去按 F1 有完整的使用說明。F9 是選單。

字型
----

原版的半形點陣字型與倚天的全形字庫已經嵌在執行檔裡,**不必自己準備**,
解開就是與原版對齊的畫面。那些字型的權利仍在原權利人手上,見 NOTICE。

要換成自己的字型就用這幾個參數:

  -half      半形點陣字型(原版的 cvga.fon 是 8x15)
  -eten-std  全形漢字(倚天 STDFONT.15)
  -eten-spc  全形標點(倚天 SPCFONT.15)

Big5 以外的字(簡體、日文、韓文、符號)靠系統字型補。缺字的話裝一份
涵蓋廣的就好(Debian/Ubuntu: fonts-noto-cjk、Fedora: google-noto-sans-cjk-fonts、
Arch: noto-fonts-cjk)。Windows 與 macOS 內建的通常夠用。

授權
----

重製版自己寫的原始碼走 BSD 2-Clause,見同目錄的 LICENSE。
**原版 WinCV 的著作權屬於原作者**,LICENSE 不涵蓋它 —— 重寫是為了
保存這份文化資產,不是取得原版的權利。

逐項的著作權與出處見 NOTICE。其中 internal/archive/ace 有一段是從
BSD 授權的 droe/acefile 逐行移植的,它的著作權聲明也在那份裡面。
EOF
