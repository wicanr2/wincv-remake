# 產生檔案列表比對用的目錄。內容與時間戳固定,可重現。
import os, sys
root = sys.argv[1]
os.makedirs(root, exist_ok=True)
def touch(name, size=0, t=1_000_000_000):
    p = os.path.join(root.encode(), name if isinstance(name, bytes) else name.encode())
    with open(p, 'wb') as f: f.write(b'x' * size)
    os.utime(p, (t, t))
for i in range(40):
    touch(f"file{i:02d}.dat", size=i * 1234567, t=900_000_000 + i * 86_400 * 17)
touch("README"); touch("readme", 5); touch("ReadMe.txt", 7); touch("readme.TXT", 7)
touch("a-very-long-filename-that-overflows.extension", 3)
touch("中文檔名.txt", 12345); touch("全形ＡＢＣ.dat", 1)
touch(b"\xa4\xa4\xa4\xe5big5.txt", 42)       # Big5「中文」的原始位元組,不是合法 UTF-8
touch(b"bad\xff\xfename.bin", 9)
touch(".hidden", 2); touch("noext", 8); touch("x.y.z", 9)
for d in ["subdir", "Another Dir", "目錄"]:
    os.makedirs(os.path.join(root, d), exist_ok=True)
    os.utime(os.path.join(root, d), (1_200_000_000, 1_200_000_000))
link = os.path.join(root, "link-to-subdir")
if not os.path.lexists(link): os.symlink("subdir", link)
