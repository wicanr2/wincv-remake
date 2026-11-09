#!/bin/sh
# 在建置容器裡產生 Windows 執行檔的圖示資源(cmd/wincv/rsrc_windows_amd64.syso)。
#
# 由 tools/build-all.sh 與 tools/build-full.sh 在 windows 那一步呼叫,
# 建完就刪:.syso 是產物,設計的唯一來源是 internal/appicon。
#
# windres 產生的 COFF 物件 Go 的連結器直接收(檔名帶 _windows_amd64,
# 其他平台的建置不會碰到它)。工作區先找一個寫得進去的暫存目錄,
# 不寫進 repo 以外的地方。
set -eu
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
go run ./tools/mkicon -out "$TMP"
printf '1 ICON "%s/wincv.ico"\n' "$TMP" > "$TMP/wincv.rc"
x86_64-w64-mingw32-windres -O coff -i "$TMP/wincv.rc" -o cmd/wincv/rsrc_windows_amd64.syso
