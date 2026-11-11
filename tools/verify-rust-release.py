#!/usr/bin/env python3
"""在容器內驗證 Rust 公開封包。macOS 僅核對格式、動態庫與簽章頁雜湊。

CodeDirectory 格式來源：Apple xnu osfmk/kern/cs_blobs.h。
https://github.com/apple-oss-distributions/xnu/blob/main/osfmk/kern/cs_blobs.h
頁雜湊通過不代表通過 Gatekeeper、公證或 macOS 實機啟動。
"""
import hashlib
import json
from pathlib import Path
import re
import struct
import sys
import zipfile


def signature_pages(thin, offset, size):
    blob = thin[offset:offset + size]
    magic, length, count = struct.unpack_from(">III", blob)
    assert magic == 0xfade0cc0 and length <= len(blob)
    directories = []
    for i in range(count):
        kind, at = struct.unpack_from(">II", blob, 12 + 8 * i)
        if kind != 0 and not 0x1000 <= kind < 0x1005:
            continue
        cd = blob[at:]
        magic, length, version, flags, hashes, _, _, slots, limit = struct.unpack_from(">9I", cd)
        assert magic == 0xfade0c02 and length <= len(cd)
        width, algorithm, _, page = struct.unpack_from("4B", cd, 36)
        if version >= 0x20100:
            assert struct.unpack_from(">I", cd, 44)[0] == 0, "未支援 scatter 簽章"
        if version >= 0x20300 and struct.unpack_from(">Q", cd, 56)[0]:
            limit = struct.unpack_from(">Q", cd, 56)[0]
        assert limit <= offset and page <= 30
        step = (1 << page) if page else limit
        assert slots == (limit + step - 1) // step
        assert hashes + slots * width <= length
        alg = {1: "sha1", 2: "sha256", 3: "sha256", 4: "sha384"}[algorithm]
        for slot in range(slots):
            data = thin[slot * step:min((slot + 1) * step, limit)]
            actual = hashlib.new(alg, data).digest()[:width]
            assert actual == cd[hashes + slot * width:hashes + (slot + 1) * width], ("簽章頁雜湊不符", slot)
        directories.append({"flags": flags, "algorithm": alg, "pages_verified": slots})
    assert directories, "沒有 CodeDirectory"
    return directories


def macho(data):
    assert data[:4] == b"\xca\xfe\xba\xbe"
    count = struct.unpack_from(">I", data, 4)[0]
    assert count == 2
    result = {}
    for i in range(count):
        cpu, _, start, size, _ = struct.unpack_from(">5I", data, 8 + i * 20)
        name = {0x100000c: "arm64", 0x1000007: "x86_64"}[cpu]
        assert name not in result and start + size <= len(data)
        thin = data[start:start + size]
        assert thin[:4] == b"\xcf\xfa\xed\xfe"
        assert struct.unpack_from("<I", thin, 4)[0] == cpu
        ncmds, command_bytes = struct.unpack_from("<II", thin, 16)
        assert 32 + command_bytes <= len(thin)
        at, libs, signature = 32, [], None
        for _ in range(ncmds):
            command, length = struct.unpack_from("<II", thin, at)
            assert length >= 8 and at + length <= 32 + command_bytes
            if command in (0xc, 0x80000018, 0x8000001f, 0x80000023):
                offset = struct.unpack_from("<I", thin, at + 8)[0]
                lib = thin[at + offset:at + length].split(b"\0", 1)[0].decode()
                assert lib.startswith(("/usr/lib/", "/System/Library/")), lib
                libs.append(lib)
            if command == 0x1d:
                signature = struct.unpack_from("<II", thin, at + 8)
            at += length
        assert at == 32 + command_bytes
        if name == "arm64":
            assert signature, "Apple Silicon 必須有簽章"
        result[name] = {"libraries": libs, "code_directories": signature_pages(thin, *signature) if signature else []}
    return result


def main():
    version = sys.argv[1]
    assert re.fullmatch(r"v\.\d+\.\d+\.\d+-\d{8}", version)
    root = Path(__file__).resolve().parent.parent
    out = root / "dist-all" / version
    manifest = json.loads((out / "SHA256SUMS.json").read_text())
    assert manifest["version"] == version
    fingerprints = []
    for directory in [root / "original/app", root / "original/eten", root / "workplace/issue11/fonts"]:
        if not directory.is_dir():
            continue
        for path in directory.iterdir():
            if path.is_file() and (path.suffix.lower() == ".fon" or path.name.startswith(("STD", "SPCF", "ASCF"))):
                data = path.read_bytes()
                for at in [len(data) // 4, len(data) // 2]:
                    block = data[at:at + 1024]
                    if len(block) == 1024 and len(set(block)) > 8:
                        fingerprints.append((path.name, block))
    report = {"version": version, "commit": manifest["commit"], "font_fingerprints": len(fingerprints), "archives": []}
    for relative, entry in manifest["files"].items():
        path = out / relative
        data = path.read_bytes()
        assert len(data) == entry["bytes"] and hashlib.sha256(data).hexdigest() == entry["sha256"], relative
        if entry["kind"] == "executable":
            assert version.encode() in data, (relative, "版本字串遺失")
            for label, block in fingerprints:
                assert block not in data, (relative, "疑似內嵌私有字庫", label)
            if "linux" in relative:
                assert data[:6] == b"\x7fELF\x02\x01" and struct.unpack_from("<H", data, 18)[0] == 62
            elif "windows" in relative:
                pe = struct.unpack_from("<I", data, 0x3c)[0]
                assert data[pe:pe + 4] == b"PE\0\0" and struct.unpack_from("<H", data, pe + 4)[0] == 0x8664
            else:
                report["macos"] = macho(data)
        else:
            platform = entry["platform"]
            folder = f"wincv-remake-rust-{platform}"
            exe = "wincv.exe" if platform == "windows-amd64" else "wincv"
            with zipfile.ZipFile(path) as z:
                assert z.testzip() is None
                expected = {f"{folder}/{name}" for name in [exe, "README.txt", "LICENSE", "NOTICE", "THIRD-PARTY-LICENSES.txt"]}
                assert set(z.namelist()) == expected
                for name in ["LICENSE", "NOTICE", "THIRD-PARTY-LICENSES.txt"]:
                    source = root / ("rust/" + name if name == "THIRD-PARTY-LICENSES.txt" else name)
                    assert z.read(f"{folder}/{name}") == source.read_bytes()
                readme = z.read(f"{folder}/README.txt")
                assert version.encode() in readme
                if platform == "windows-amd64":
                    assert readme.startswith(b"\xef\xbb\xbf")
                    assert b"\n" not in readme.replace(b"\r\n", b"")
            report["archives"].append(relative)
    assert len(report["archives"]) == 3
    (out / "smoke/static-verification.json").write_text(json.dumps(report, indent=2) + "\n")
    print(json.dumps(report, indent=2))


if __name__ == "__main__":
    main()
