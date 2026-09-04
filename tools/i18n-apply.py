#!/usr/bin/env python3
"""把 i18nscan -mode plan 的 auto 那幾條包上 i18n.T()。

由後往前改:一個檔案裡插入文字會讓後面所有 offset 位移,從尾端往回做
就不必重算。只動 kind 為 auto 的,其餘留給人看 —— 那四種位置包起來
會安靜地壞掉(理由寫在 tools/i18nscan 的說明裡)。
"""
import collections, os, subprocess, sys

MOD = "github.com/wicanr2/wincv-remake/internal/i18n"

def add_import(src, path):
    if f'"{MOD}"' in src:
        return src
    i = src.find("\nimport (")
    if i < 0:  # 單行 import 或沒有 import
        j = src.find("\nimport ")
        if j < 0:
            k = src.index("\n", src.index("package "))
            return src[:k+1] + f'\nimport "{MOD}"\n' + src[k+1:]
        k = src.index("\n", j+1)
        return src[:k+1] + f'\nimport "{MOD}"\n' + src[k+1:]
    k = src.index("\n", i+1)
    return src[:k+1] + f'\t"{MOD}"\n' + src[k+1:]

def main(plan):
    edits = collections.defaultdict(list)
    for line in open(plan, encoding="utf-8"):
        parts = line.rstrip("\n").split("\t")
        if len(parts) < 5 or parts[3] != "auto":
            continue
        edits[parts[0]].append((int(parts[1]), int(parts[2])))
    for f, es in sorted(edits.items()):
        src = open(f, encoding="utf-8").read()
        b = src.encode("utf-8")
        for off, ln in sorted(es, reverse=True):
            b = b[:off] + b"i18n.T(" + b[off:off+ln] + b")" + b[off+ln:]
        src = add_import(b.decode("utf-8"), f)
        open(f, "w", encoding="utf-8").write(src)
        print(f"{f}: {len(es)} 條")

if __name__ == "__main__":
    main(sys.argv[1])
