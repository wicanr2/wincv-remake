#!/usr/bin/env python3
"""在 Docker 內以正式 Rust 視窗擷取 README 的 ETEN 2000 詩詞展示。

用法: xvfb-run -a python3 tools/eten2000-readme-shots.py <執行檔> <repo>
洛神圖文: 在上述兩個位置參數前加 --luoshen。
執行檔旁需有 verify-eten2000.py 還原的字庫與三份半形 .FON。
"""

import json
import hashlib
import os
from pathlib import Path
import runpy
import struct
import subprocess
import sys
import tempfile
import time


def check_glyphs(raw, text, std, spc):
    header = struct.unpack(">25I", raw[:100])
    offset, stride, size = header[0] + header[19] * 12, header[12], (header[11] + 7) // 8
    assert (header[4], header[5]) == (768, 475), (header[4], header[5])
    count = 0
    for row, line in enumerate(text.splitlines()):
        x0 = 0
        for char in line:
            if char == " ":
                x0 += 12
                continue
            pair = char.encode("cp950")
            assert len(pair) == 2, char
            hi, lo = pair
            idx = (hi - 0xA1) * 157 + (lo - 0x40 if lo < 0x7F else lo - 0x62)
            if idx <= 407:
                data, slot = spc, idx
            elif hi < 0xC9:
                data, slot = std, idx - 471
            else:
                data, slot = std, 5401 + idx - 6280
            glyph = data[slot * 72:(slot + 1) * 72]
            assert len(glyph) == 72, char
            for y in range(24):
                for x in range(24):
                    start = offset + (25 + row * 25 + y) * stride + (x0 + x) * size
                    rgb = int.from_bytes(raw[start:start + size], "little" if header[7] == 0 else "big") & 0xffffff
                    ink = bool(glyph[y * 3 + x // 8] & (128 >> (x % 8)))
                    assert (rgb != 0) == ink, (char, row, x, y)
            x0 += 24
            count += 1
    return count


def main():
    if sys.argv[1:2] == ["--luoshen"]:
        luoshen_shots(*(Path(p).resolve() for p in sys.argv[2:]))
        return
    exe, repo = (Path(p).resolve() for p in sys.argv[1:])
    helpers = runpy.run_path(str(repo / "tools/eten2000-smoke.py"))
    xdo, key = helpers["xdo"], helpers["key"]
    result = {"version": subprocess.check_output([str(exe), "-version"], text=True).strip(), "shots": []}
    spc = (exe.parent / "SPCFONT.24").read_bytes()
    for name, suffix, index in [("linjiangxian", "", 0), ("niannujiao-chibihuaigu", "K", 1)]:
        sample = repo / f"docs/demo/{name}.txt"
        output = repo / f"docs/ui/eten2000-{name}.png"
        with tempfile.TemporaryDirectory() as cwd:
            env = {**os.environ, "XDG_CONFIG_HOME": str(Path(cwd) / "config")}
            env.pop("WINCV_HOME", None)
            with (repo / f"workplace/issue11/{name}-shot.log").open("w") as log:
                proc = subprocess.Popen([str(exe), "-no-resume", "-no-fallback", "-zoom", "2",
                                         "-scale", "1", "-cols", "64", "-rows", "18", str(sample)],
                                        cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
                try:
                    for _ in range(150):
                        assert proc.poll() is None, "WinCV 提早結束"
                        found = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^WinCV$"], capture_output=True, text=True)
                        if found.returncode == 0:
                            win = found.stdout.splitlines()[0]
                            break
                        time.sleep(0.1)
                    else:
                        raise RuntimeError("找不到 WinCV 視窗")
                    xdo("windowfocus", win)
                    xdo("mousemove", "--window", win, 0, 0)
                    time.sleep(1)
                    # 明體先切楷體再切回，讓兩張圖的訊息列都顯示實際套用的字體。
                    for target in ([1, 0] if index == 0 else [index]):
                        for k in ["alt+7", "End", "Up", "Return", "Home"] + ["Down"] * target + ["Return"]:
                            key(k)
                        time.sleep(0.5)
                    raw = subprocess.check_output(["xwd", "-id", win, "-silent"])
                    count = check_glyphs(raw, sample.read_text(), (exe.parent / ("STDFONT.24" + suffix)).read_bytes(), spc)
                    shot = Path(cwd) / "shot.xwd"
                    shot.write_bytes(raw)
                    subprocess.run([sys.executable, str(repo / "tools/xwd2png.py"), str(shot), str(output)], check=True)
                    result["shots"].append({"file": str(output.relative_to(repo)), "font": "STDFONT.24" + suffix, "checked_glyphs": count})
                finally:
                    proc.terminate()
                    try:
                        proc.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait()
    (repo / "workplace/issue11/readme-shots.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


def luoshen_shots(exe, repo):
    """同一份 Markdown 在三種字體下的大視窗；擷取原始視窗，不另拼圖。"""
    helpers = runpy.run_path(str(repo / "tools/eten2000-smoke.py"))
    xdo, key = helpers["xdo"], helpers["key"]
    sample = repo / "docs/demo/luoshen.md"
    receipt = {"version": subprocess.check_output([str(exe), "-version"], text=True).strip(),
               "source": str(sample.relative_to(repo)), "scale": 1.5, "shots": []}
    for name, index in [("kai", 1), ("ming", 0), ("li", 2)]:
        output = repo / f"docs/ui/eten2000-luoshen-{name}.png"
        with tempfile.TemporaryDirectory() as cwd:
            env = {**os.environ, "XDG_CONFIG_HOME": str(Path(cwd) / "config")}
            env.pop("WINCV_HOME", None)
            with (repo / f"workplace/issue11/luoshen-{name}.log").open("w") as log:
                proc = subprocess.Popen([str(exe), "-no-resume", "-no-fallback", "-zoom", "2",
                                         "-scale", "1.5", "-cols", "56", "-rows", "56", str(sample)],
                                        cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
                try:
                    for _ in range(150):
                        assert proc.poll() is None, "WinCV 提早結束"
                        found = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^WinCV$"], capture_output=True, text=True)
                        if found.returncode == 0:
                            win = found.stdout.splitlines()[0]
                            break
                        time.sleep(0.1)
                    else:
                        raise RuntimeError("找不到 WinCV 視窗")
                    xdo("windowfocus", win)
                    xdo("mousemove", "--window", win, 0, 0)
                    time.sleep(1)
                    for target in ([1, 0] if index == 0 else [index]):
                        for k in ["alt+7", "End", "Up", "Return", "Home"] + ["Down"] * target + ["Return"]:
                            key(k)
                        time.sleep(0.7)
                    raw = subprocess.check_output(["xwd", "-id", win, "-silent"])
                    header = struct.unpack(">25I", raw[:100])
                    assert header[4] >= 1000 and header[5] >= 2100, (header[4], header[5])
                    offset = header[0] + header[19] * 12
                    size = (header[11] + 7) // 8
                    def region(y0, y1):
                        return b"".join(raw[offset + y * header[12]:offset + y * header[12] + 580 * size] for y in range(y0, y1))
                    image_pixels = region(250, 1000)
                    colors = {image_pixels[i:i + size] for i in range(0, len(image_pixels), size)}
                    assert len(colors) > 1000, "圖片區未成功載入"
                    shot = Path(cwd) / "shot.xwd"
                    shot.write_bytes(raw)
                    subprocess.run([sys.executable, str(repo / "tools/xwd2png.py"), str(shot), str(output)], check=True)
                    receipt["shots"].append({"file": str(output.relative_to(repo)), "width": header[4], "height": header[5],
                                             "source_index": index,
                                             "image_pixels_sha256": hashlib.sha256(image_pixels).hexdigest(),
                                             "text_pixels_sha256": hashlib.sha256(region(1250, 1800)).hexdigest()})
                finally:
                    proc.terminate()
                    try:
                        proc.wait(timeout=5)
                    except subprocess.TimeoutExpired:
                        proc.kill()
                        proc.wait()
    assert len({s["image_pixels_sha256"] for s in receipt["shots"]}) == 1, "三張圖片區應相同"
    assert len({s["text_pixels_sha256"] for s in receipt["shots"]}) == 3, "三種字體的文字區應不同"
    (repo / "workplace/issue11/luoshen-shots.json").write_text(json.dumps(receipt, indent=2) + "\n")
    print(json.dumps(receipt, indent=2))


if __name__ == "__main__":
    main()
