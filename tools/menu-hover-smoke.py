#!/usr/bin/env python3
"""在 Xvfb 中操作正式 Linux 執行檔,以實際視窗像素驗證下拉反白。

用法: xvfb-run -a python3 tools/menu-hover-smoke.py <執行檔> <輸出目錄>
需要 Dockerfile.build 提供的 xdotool、xwd 與 Python 3。
"""
import json
import os
from pathlib import Path
import struct
import subprocess
import sys
import tempfile
import time


def xdo(*args):
    return subprocess.check_output(["xdotool", *map(str, args)], text=True).strip()


def main():
    exe = Path(sys.argv[1]).resolve()
    out = Path(sys.argv[2]).resolve()
    out.mkdir(parents=True, exist_ok=True)
    converter = Path(__file__).with_name("xwd2png.py")
    with tempfile.TemporaryDirectory() as temp:
        Path(temp, "hover.txt").write_text("WinCV menu hover\n")
        with (out / "app.log").open("w") as log:
            proc = subprocess.Popen(
                [str(exe), "-no-resume", "-scale", "1", "-cols", "80",
                 "-rows", "25", "-lang", "zh-Hant", temp],
                cwd=temp, env={**os.environ, "WINCV_HOME": "/nonexistent"},
                stdout=log, stderr=subprocess.STDOUT,
            )
            try:
                win = None
                for _ in range(100):
                    if proc.poll() is not None:
                        raise RuntimeError(f"WinCV 提早結束: {proc.returncode}")
                    found = subprocess.run(
                        ["xdotool", "search", "--onlyvisible", "--name", "^WinCV$"],
                        text=True, capture_output=True,
                    )
                    if found.returncode == 0:
                        win = found.stdout.splitlines()[0]
                        break
                    time.sleep(0.1)
                if win is None:
                    raise RuntimeError("找不到 WinCV 視窗")
                xdo("windowfocus", win)
                time.sleep(1)

                def shot(name):
                    time.sleep(0.3)
                    raw = subprocess.check_output(["xwd", "-id", win, "-silent"])
                    xwd = out / f"{name}.xwd"
                    png = out / f"{name}.png"
                    xwd.write_bytes(raw)
                    subprocess.run([sys.executable, str(converter), str(xwd), str(png)], check=True)
                    xwd.unlink()
                    h = struct.unpack(">25I", raw[:100])
                    offset, stride, size = h[0] + h[19] * 12, h[12], (h[11] + 7) // 8

                    def pixel(x, y):
                        at = offset + y * stride + x * size
                        return int.from_bytes(raw[at:at + size], "little" if h[7] == 0 else "big") & 0xffffff

                    return pixel

                # 8x16 格點:設定標題在第 19–24 格,下拉左緣在第 19 格。
                def click():
                    xdo("mousedown", 1)
                    time.sleep(0.08)
                    xdo("mouseup", 1)

                xdo("mousemove", "--window", win, 164, 8)
                click()
                p = shot("setup-open")
                blue = p(153, 24)
                assert blue == 0x000080, f"設定未展開或格點不符: {blue:#x}"
                # 移到索引 3 的「放大字體」,不按滑鼠。
                xdo("mousemove", "--window", win, 180, 72)
                p = shot("hover-enlarge")
                assert p(153, 72) == blue and p(153, 24) != blue, "滑鼠移入未移動藍色選取列"
                # 滑鼠不動,鍵盤仍可移動選取。
                xdo("keydown", "Down")
                time.sleep(0.08)
                xdo("keyup", "Down")
                p = shot("keyboard-after-hover")
                assert p(153, 88) == blue and p(153, 72) != blue, "靜止滑鼠干擾鍵盤選取"
                xdo("mousemove", "--window", win, 181, 72)
                p = shot("hover-again")
                assert p(153, 72) == blue, "再次移動滑鼠未取回選取"
                # 點擊才執行放大。
                click()
                p = shot("click-enlarge")
                assert p(153, 72) != blue, "點擊放大後選單仍未關閉"
                (out / "result.json").write_text(json.dumps({
                    "executable": str(exe), "hover": "passed",
                    "stationary_mouse_keyboard": "passed", "click": "passed",
                    "environment": "Linux Xvfb, software OpenGL, no-resume",
                }, ensure_ascii=False, indent=2) + "\n")
                print("選單移入、鍵盤接續與點擊:通過")
            finally:
                proc.terminate()
                try:
                    proc.wait(timeout=5)
                except subprocess.TimeoutExpired:
                    proc.kill()
                    proc.wait()


if __name__ == "__main__":
    main()
