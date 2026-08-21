#!/usr/bin/env bash
# 在 docker 裡跑 IDA(headless)。
#
# [HARD] image 一定要用 ida-pro-9.4-idapython:py312-v1。
#        基底 image 跑 IDAPython 是**零輸出、零訊息的靜默失敗**,
#        而且 exit code 不可信(同一種失敗在不同 image 上回 0 或 1)。
#        判準永遠是輸出檔本身。見 ~/.claude/knowledge-base/retro/ida-pro-9.4.md
#
# 用法:tools/ida.sh <script.py> [傳給腳本的參數...]
set -euo pipefail

REPO=$(cd "$(dirname "$0")/.." && pwd)
WORK=$REPO/.ida
IMG=${IDA_IMG:-ida-pro-9.4-idapython:py312-v1}

mkdir -p "$WORK/out"
# IMG 檔複製進去,不要讓 IDA 在 original/ 底下生 .i64(那個目錄不進版控但也不該被寫)
[ -f "$WORK/WINCV.IMG" ] || cp "$REPO/original/app/WINCV.IMG" "$WORK/WINCV.IMG"
mkdir -p "$WORK/re" && cp "$REPO/docs/re/"*.tsv "$WORK/re/"

SCRIPT=$1; shift

# 先驗語法再燒一次 IDA。
#
# 語法錯誤的症狀是「腳本一行都沒跑」—— 沒有輸出、沒有訊息、沒有 traceback,
# 與「IDAPython 在這個環境不能用」長得一模一樣。一次 IDA 執行要好幾分鐘,
# 拿三秒鐘的 py_compile 換掉那幾分鐘的誤診很划算。
python3 -c "import ast,sys; ast.parse(open(sys.argv[1]).read())" "$REPO/tools/ida/$SCRIPT"

cp "$REPO/tools/ida/$SCRIPT" "$WORK/$SCRIPT"

docker run --rm --log-opt max-size=10m --log-opt max-file=3 \
    -u "$(id -u):$(id -g)" -v "$WORK:/work" -w /work \
    "$IMG" \
    idat -A -c -TBinary -pmetapc -S"/work/$SCRIPT $*" /work/WINCV.IMG
