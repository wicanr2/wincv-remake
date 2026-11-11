#!/usr/bin/env python3
"""Issue #11 的倚天 2000 ISO 字庫驗證。僅在 Docker 內執行。

用法: python3 tools/verify-eten2000.py <ISO> <libblast.so> <輸出目錄>
解壓工具來自 https://github.com/madler/zlib/tree/v1.3.1/contrib/blast
取得該版本的 blast.c、blast.h 後以 gcc -shared -fPIC -O2 -o libblast.so blast.c 建置。
輸出含第三方字庫，只能留在本機 workplace/，不可加入公開產物。
"""

import ctypes as c
import hashlib
import json
from pathlib import Path
import struct
import sys


def sha(data):
    return hashlib.sha256(data).hexdigest()


def iso_files(data):
    # 本次輸入是根目錄存檔的 ISO 9660；拒收不同結構，避免靜默讀錯。
    pvd = data[16 * 2048:17 * 2048]
    assert pvd[:7] == b"\x01CD001\x01", "不是預期的 ISO 9660"
    assert struct.unpack_from("<H", pvd, 128)[0] == 2048
    root = pvd[156:190]
    extent = struct.unpack_from("<I", root, 2)[0]
    size = struct.unpack_from("<I", root, 10)[0]
    directory = data[extent * 2048:extent * 2048 + size]
    pos = 0
    while pos < len(directory):
        length = directory[pos]
        if not length:
            pos = (pos // 2048 + 1) * 2048
            continue
        record = directory[pos:pos + length]
        pos += length
        if record[25] & 2:
            continue
        name = record[33:33 + record[32]].decode("ascii").split(";")[0]
        assert Path(name).name == name and name not in (".", "..")
        extent = struct.unpack_from("<I", record, 2)[0]
        size = struct.unpack_from("<I", record, 10)[0]
        start = extent * 2048
        assert start + size <= len(data)
        yield name, data[start:start + size]


def explode(lib, data):
    in_type = c.CFUNCTYPE(c.c_uint, c.c_void_p, c.POINTER(c.POINTER(c.c_ubyte)))
    out_type = c.CFUNCTYPE(c.c_int, c.c_void_p, c.POINTER(c.c_ubyte), c.c_uint)
    chunks = []

    @in_type
    def read(_handle, _buffer):
        return 0

    @out_type
    def write(_handle, buffer, size):
        chunks.append(c.string_at(buffer, size))
        return 0

    lib.blast.argtypes = [in_type, c.c_void_p, out_type, c.c_void_p,
                          c.POINTER(c.c_uint), c.POINTER(c.POINTER(c.c_ubyte))]
    src = c.create_string_buffer(data)
    ptr = c.cast(src, c.POINTER(c.c_ubyte))
    left = c.c_uint(len(data))
    result = lib.blast(read, None, write, None, c.byref(left), c.byref(ptr))
    assert result == 0 and left.value == 0, ("blast", result, left.value)
    return b"".join(chunks)


def main():
    iso, library, output = map(Path, sys.argv[1:])
    data = iso.read_bytes()
    assert sha(data) == "624c89a8d1241f11e490f892dac190878e4c0352e1b22dd82dbaa56a4df60d41", "ISO 與驗證樣本不同"
    lib = c.CDLL(str(library.resolve()))
    output.mkdir(parents=True, exist_ok=True)
    fonts = output / "fonts"
    fonts.mkdir(exist_ok=True)
    report = {"iso_sha256": sha(data), "blast": "zlib v1.3.1", "fonts": {}}
    for name, packed in iso_files(data):
        if not name.startswith(("STDFONT.", "SPCFONT.", "SPCFSUPP.", "ASCFONT.")):
            continue
        target = name.replace("._4", ".24").replace("._5", ".15")
        raw = explode(lib, packed) if "._" in name else packed
        path = fonts / target
        if target in report["fonts"]:
            assert path.read_bytes() == raw, ("重複來源內容不同", name)
        path.write_bytes(raw)
        report["fonts"][target] = {"bytes": len(raw), "sha256": sha(raw)}

    for suffix in ("", "B", "K", "L", "R", "S"):
        assert report["fonts"]["STDFONT.24" + suffix]["bytes"] == 13094 * 72
    assert report["fonts"]["SPCFONT.24"]["bytes"] == 408 * 72
    supp = (fonts / "SPCFSUPP.24").read_bytes()
    assert len(supp) == 365 * 72
    assert sha(supp) == "5febef61214ff45e4d630de6ee0b8a078d410378dbf6cc90590dfc4f3c8a0318"
    report["supp_layout"] = "365 packed; identical to ETEN 3.53 SPCFSUPP.24"
    (output / "extraction.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
