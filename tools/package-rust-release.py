#!/usr/bin/env python3
"""由 rust-release.sh 在容器內打包，不含使用者圖片、原版資料或外部字庫。"""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys
import zipfile


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def main():
    version, commit = sys.argv[1:]
    assert re.fullmatch(r"v\.\d+\.\d+\.\d+-\d{8}", version), version
    assert re.fullmatch(r"[0-9a-f]{40}", commit), commit
    root = Path(__file__).resolve().parent.parent
    out = root / "dist-all" / version
    patch = out / "patch"
    manifest = {"version": version, "commit": commit, "engine": "rust", "release_channel": "preview",
                "inputs": {p: digest(root / p) for p in ["rust/Cargo.lock", "LICENSE", "NOTICE", "rust/THIRD-PARTY-LICENSES.txt"]},
                "files": {}}
    archives, binaries = [], []
    for source, exe, platform, arches in [
        ("wincv-linux-amd64", "wincv", "linux-amd64", ["x86_64"]),
        ("wincv-windows-amd64.exe", "wincv.exe", "windows-amd64", ["x86_64"]),
        ("wincv-darwin-universal", "wincv", "macos-universal", ["arm64", "x86_64"]),
    ]:
        binary = patch / source
        assert binary.is_file() and binary.stat().st_size > 0
        binary.chmod(0o755)
        readme = subprocess.check_output([str(root / "tools/readme-for.sh"), platform, exe, "rust"], text=True)
        readme = readme.replace("WinCV Remake\n", f"WinCV Remake {version}\n", 1)
        readme_bytes = readme.encode()
        if platform == "windows-amd64":
            readme_bytes = b"\xef\xbb\xbf" + readme.replace("\n", "\r\n").encode()
        folder = f"wincv-remake-rust-{platform}"
        archive = patch / f"wincv-remake-rust-{version}-{platform}.zip"
        with zipfile.ZipFile(archive, "w", compression=zipfile.ZIP_DEFLATED, compresslevel=9) as z:
            z.write(binary, f"{folder}/{exe}")
            for label, path in [("LICENSE", root / "LICENSE"), ("NOTICE", root / "NOTICE"),
                                ("THIRD-PARTY-LICENSES.txt", root / "rust/THIRD-PARTY-LICENSES.txt")]:
                z.write(path, f"{folder}/{label}")
            z.writestr(f"{folder}/README.txt", readme_bytes)
        with zipfile.ZipFile(archive) as z:
            assert z.testzip() is None
            assert len(z.infolist()) == 5
            for info in z.infolist():
                if not info.filename.isascii():
                    assert info.flag_bits & 0x800
            assert z.read(f"{folder}/{exe}") == binary.read_bytes()
            if platform == "windows-amd64":
                actual = z.read(f"{folder}/README.txt")
                assert actual.startswith(b"\xef\xbb\xbf") and b"\r\n" in actual
                assert b"\n" not in actual.replace(b"\r\n", b"")
        for path, kind in [(binary, "executable"), (archive, "archive")]:
            manifest["files"][str(path.relative_to(out))] = {"bytes": path.stat().st_size, "sha256": digest(path),
                                                            "platform": platform, "architectures": arches,
                                                            "rights": "public engine; external fonts and original data excluded", "kind": kind}
        archives.append(archive)
        binaries.append(binary)
    for label, paths in [("SHA256SUMS", binaries), ("SHA256SUMS-zip", archives)]:
        (patch / label).write_text("".join(f"{digest(p)}  {p.name}\n" for p in paths))
    (patch / "MANIFEST.txt").write_text(
        f"WinCV Remake (Rust preview) {version}\ncommit {commit}\n\n"
        "Linux x86-64 / Windows x86-64 / macOS universal (arm64 + x86_64)\n"
        "公開封包不含原版、倚天、國喬字庫或使用者插畫。\n"
        "每包附 LICENSE、NOTICE、THIRD-PARTY-LICENSES.txt 與 README.txt。\n"
        "SHA256SUMS-zip 為公開 ZIP 的校驗表。\n")
    (out / "SHA256SUMS.json").write_text(json.dumps(manifest, ensure_ascii=False, indent=2) + "\n")
    print(json.dumps(manifest, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
