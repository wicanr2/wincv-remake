#!/usr/bin/env python3
"""產生 Rust 版執行檔的第三方授權清單(rust/THIRD-PARTY-LICENSES.txt)。

MIT、BSD、Apache 這類授權都要求散布二進位檔時附上授權全文與著作權聲明。
這支從 cargo 下載的套件原始碼裡收集授權檔(含套件嵌進執行檔的資料,例如
hayro-font 的 Foxit 字型、hayro-cmap 的 Adobe CMap),全文相同的合併成一段。

由 tools/rust-licenses.sh 在容器裡呼叫,輸入是兩個檔:
  cargo tree -p wincv -e normal,build --prefix none --format '{p}' --target all
  cargo metadata --format-version 1
"""
import hashlib
import json
import os
import re
import sys

tree_path, meta_path, out_path = sys.argv[1:4]

want = set()
for line in open(tree_path, encoding="utf-8"):
    m = re.match(r"(\S+) v(\S+)", line.strip())
    if m and not m.group(1).startswith("wincv"):
        want.add((m.group(1), m.group(2)))

meta = json.load(open(meta_path, encoding="utf-8"))
pkgs = {(p["name"], p["version"]): p for p in meta["packages"]}

NAME = re.compile(r"^(LICEN[CS]E|COPYING|NOTICE|UNLICENSE)", re.I)
groups = {}   # 全文雜湊 → (全文, [套件])
missing = []
for key in sorted(want):
    p = pkgs.get(key)
    if p is None:
        missing.append(f"{key[0]} {key[1]}(metadata 裡找不到)")
        continue
    root = os.path.dirname(p["manifest_path"])
    files = []
    for sub in ("", "assets"):
        d = os.path.join(root, sub)
        if os.path.isdir(d):
            files += [os.path.join(d, f) for f in sorted(os.listdir(d)) if NAME.match(f) and os.path.isfile(os.path.join(d, f))]
    label = f"{p['name']} {p['version']}({p.get('license') or '未標'})"
    if not files:
        who = "、".join(p.get("authors") or []) or "未標"
        missing.append(f"{label} 作者:{who} 來源:{p.get('repository') or '未標'}")
        continue
    for f in files:
        text = open(f, encoding="utf-8", errors="replace").read().strip()
        h = hashlib.sha256(text.encode()).hexdigest()
        rel = os.path.relpath(f, root)
        groups.setdefault(h, [text, []])[1].append(f"{label} — {rel}")

with open(out_path, "w", encoding="utf-8", newline="\n") as o:
    o.write("WinCV Remake(Rust 版)使用的第三方套件授權\n")
    o.write("由 tools/rust-licenses.sh 產生,不要手改。\n")
    o.write(f"套件 {len(want)} 個,不同的授權全文 {len(groups)} 份。\n\n")
    for text, who in sorted(groups.values(), key=lambda g: g[1][0]):
        o.write("=" * 78 + "\n")
        for w in who:
            o.write(w + "\n")
        o.write("-" * 78 + "\n" + text + "\n\n")
    if missing:
        o.write("=" * 78 + "\n下列套件的原始碼裡沒有授權檔,授權依其 Cargo.toml 的 license 欄:\n")
        for m in missing:
            o.write(m + "\n")
print(f"{len(want)} 個套件,{len(groups)} 份授權全文,{len(missing)} 個沒有授權檔")
