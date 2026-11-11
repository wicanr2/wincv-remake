#!/usr/bin/env python3
"""Issue #11 的正式視窗驗證，在 Docker 內以 xvfb-run -a 啟動。

用法: python3 tools/eten2000-smoke.py <程式旁已放字庫的執行檔> <輸出目錄>
程式旁須有 verify-eten2000.py 還原的字庫與三份半形 .FON。
從專案外的空工作目錄啟動，不設 WINCV_HOME，不依賴 original/eten。
"""

import json
import os
from pathlib import Path
import subprocess
import struct
import sys
import tempfile
import time


def xdo(*args):
    return subprocess.check_output(["xdotool", *map(str, args)], text=True).strip()


def key(name):
    xdo("keydown", name)
    time.sleep(0.08)
    xdo("keyup", name)
    time.sleep(0.15)


def verify_pixels(raw, font_dir, suffix):
    header = struct.unpack(">25I", raw[:100])
    offset, stride, size = header[0] + header[19] * 12, header[12], (header[11] + 7) // 8

    def pixel(x, y):
        start = offset + y * stride + x * size
        return int.from_bytes(raw[start:start + size], "little" if header[7] == 0 else "big") & 0xffffff

    std = (font_dir / ("STDFONT.24" + suffix)).read_bytes()
    supp = (font_dir / "SPCFSUPP.24").read_bytes()
    # 選中列的底色是深藍；下一列為黑。字形以實檔逐點比對，排除後備字型。
    for name, data, slot, x0, y0, bg in [("一", std, 0, 168, 25, 0x000080),
                                       ("中", std, 66, 192, 25, 0x000080),
                                       ("ぁ", supp, 70, 120, 50, 0),
                                       ("ゃ", supp, 136, 264, 50, 0)]:
        glyph = data[slot * 72:(slot + 1) * 72]
        for y in range(24):
            for x in range(24):
                ink = bool(glyph[y * 3 + x // 8] & (128 >> (x % 8)))
                assert (pixel(x0 + x, y0 + y) != bg) == ink, (suffix, name, x, y)


def main():
    exe, out = (Path(p).resolve() for p in sys.argv[1:])
    out.mkdir(parents=True, exist_ok=True)
    env = {**os.environ, "XDG_CONFIG_HOME": str(out / "config")}
    env.pop("WINCV_HOME", None)
    sample = out / "sample.txt"
    sample.write_text("倚天中文2000：一中龍，。！？「」\n"
                      "補充字區：ぁあぃいぅうゃやゅゆょよ\n", encoding="utf-8")
    result = {"version": subprocess.check_output([str(exe), "-version"], text=True).strip(),
              "sources": []}
    with tempfile.TemporaryDirectory() as cwd, (out / "app.log").open("w") as log:
        proc = subprocess.Popen([str(exe), "-no-resume", "-no-fallback", "-zoom", "2",
                                 "-scale", "1", "-cols", "80", "-rows", "25", str(sample)],
                                cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
        try:
            for _ in range(150):
                assert proc.poll() is None, "WinCV 提早結束"
                found = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^WinCV$"],
                                       capture_output=True, text=True)
                if found.returncode == 0:
                    win = found.stdout.splitlines()[0]
                    break
                time.sleep(0.1)
            else:
                raise RuntimeError("找不到 WinCV 視窗")
            xdo("windowfocus", win)
            xdo("mousemove", "--window", win, 0, 0)
            time.sleep(1)
            for source, index in [("eten-k", 1), ("eten-l", 2), ("eten-r", 3),
                                  ("eten-b", 4), ("eten-s", 5), ("eten-m", 0)]:
                # 設定的最後一個項目是語言，上個項目是 CJK 字形。
                for name in ["alt+7", "End", "Up", "Return", "Home"] + ["Down"] * index + ["Return"]:
                    key(name)
                time.sleep(0.6)
                shot = out / f"{source}.xwd"
                raw = subprocess.check_output(["xwd", "-id", win, "-silent"])
                shot.write_bytes(raw)
                subprocess.run([sys.executable, str(Path(__file__).with_name("xwd2png.py")),
                                str(shot), str(shot.with_suffix(".png"))], check=True)
                shot.unlink()
                suffix = {"eten-m": "", "eten-k": "K", "eten-l": "L", "eten-r": "R", "eten-b": "B", "eten-s": "S"}[source]
                verify_pixels(raw, exe.parent, suffix)
                result["sources"].append(source)
        finally:
            proc.terminate()
            try:
                proc.wait(timeout=5)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait()
        # -no-resume 同時停用保存。另用全新設定目錄驗證保存，避免舊狀態影響前面的尺寸。
        with tempfile.TemporaryDirectory() as config:
            env["XDG_CONFIG_HOME"] = config
            args = [str(exe), "-no-fallback", "-zoom", "2", "-scale", "1", "-cols", "80", "-rows", "25", str(sample)]
            proc = subprocess.Popen(args, cwd=cwd, env=env, stdout=log, stderr=subprocess.STDOUT)
            try:
                for _ in range(150):
                    found = subprocess.run(["xdotool", "search", "--onlyvisible", "--name", "^WinCV$"], capture_output=True, text=True)
                    if found.returncode == 0:
                        win = found.stdout.splitlines()[0]
                        break
                    time.sleep(0.1)
                else:
                    raise RuntimeError("保存驗證找不到視窗")
                xdo("windowfocus", win)
                time.sleep(1)
                for name in ["alt+7", "End", "Up", "Return", "Home", "Down", "Return"]:
                    key(name)
                time.sleep(0.6)
                state = json.loads((Path(config) / "wincv/session.json").read_text())
                assert state["cjk24"] == "eten-k", state
                (out / "saved-session.json").write_text(json.dumps(state, ensure_ascii=False, indent=2) + "\n")
                result["saved_cjk24"] = state["cjk24"]
            finally:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()
    (out / "result.json").write_text(json.dumps(result, indent=2) + "\n")
    print(json.dumps(result, indent=2))


if __name__ == "__main__":
    main()
