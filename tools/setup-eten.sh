#!/usr/bin/env bash
# 從倚天中文系統 3.53 的光碟映像抽出點陣字庫,放進 original/eten/。
#
# 全形字的來源。尺寸與 WinCV 隨附的半形 .FON 剛好成對:
#   cvga     8x15  ←→ STDFONT.15 / SPCFONT.15  16x15
#   cvga1224 12x24 ←→ STD.24x                  24x24  (ETUNPACK 壓縮,尚未支援)
#
# 預設從 ~/cht/etan_font/ET353S.iso 抽。那個目錄裡的 stdfont.15
# 與光碟裡的是同一份(md5 相同),抽光碟的好處是連 SPCFONT 一起拿到。
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
ISO=${1:-$HOME/cht/etan_font/ET353S.iso}
OUT=$REPO/original/eten

if [ ! -f "$ISO" ]; then
    echo "找不到倚天光碟映像 $ISO" >&2
    echo "也可以直接把 STDFONT.15 與 SPCFONT.15 放進 $OUT" >&2
    exit 1
fi

mkdir -p "$OUT"
7z e -o"$OUT" "$ISO" -y \
    "DISKS/DISK2/STDFONT.15" \
    "DISKS/DISK1/SPCFONT.15" \
    "DISKS/DISK1/SPCFSUPP.15" \
    "DISKS/DISK1/ASCFONT.15" \
    "DISKS/DISK1/SPCFONT.24" \
    "DISKS/DISK1/SPCFSUPP.24" \
    "DISKS/DISK1/ASCFONT.24" >/dev/null

echo "抽到 $OUT:"
ls -1 "$OUT"
echo
echo "註:STD.24x(24 點漢字,六種字體)是 ETUNPACK 壓縮的,還沒支援,"
echo "    所以 12x24 那個字級目前只有半形與全形標點。"
