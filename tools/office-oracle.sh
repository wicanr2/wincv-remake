#!/usr/bin/env bash
# 用容器裡的 LibreOffice 做兩件事:產生測試用的 Office 檔,以及把同一份
# 檔案轉成純文字當驗收的對照。主機上不裝 LibreOffice。
#
#   tools/office-oracle.sh <輸出格式> <檔案>...
#
# 例:
#   tools/office-oracle.sh 'doc:MS Word 97' src.html    # 產生 src.doc
#   tools/office-oracle.sh txt src.doc                  # 產生 src.txt 當對照
#
# 產物與輸入放在同一個目錄。LibreOffice 的使用者設定寫在該目錄下的
# .lo/,不碰 $HOME。
set -euo pipefail
IMG=${LO_IMG:-linuxserver/libreoffice:latest}
fmt=$1
shift
[ $# -gt 0 ] || { echo "用法:$0 <格式> <檔案>..." >&2; exit 2; }

dir=$(cd "$(dirname "$1")" && pwd)
names=()
for f in "$@"; do names+=("$(basename "$f")"); done

exec docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -e HOME=/w -v "$dir":/w -w /w --network none \
    --entrypoint /usr/bin/soffice "$IMG" \
    --headless -env:UserInstallation=file:///w/.lo \
    --convert-to "$fmt" "${names[@]}"
