# 第三方參考:用 Pillow(內建 libjpeg-turbo 與 libwebp)解 JPEG / WebP,
# 給 tools/img-parity.sh 的 REF=1 用。stdin 每行「序號<TAB>相對於 /corpus 的路徑」。
import sys
from PIL import Image
out = sys.argv[1]
print(Image.__version__, file=sys.stderr)
for line in sys.stdin:
    i, p = line.rstrip('\n').split('\t', 1)
    try:
        im = Image.open('/corpus/' + p)
        im.load()
        im = im.convert('RGBA')
        open(f'{out}/{i}.rgba', 'wb').write(im.tobytes())
        print(f'{i}\t{im.width}\t{im.height}')
    except Exception as e:
        print(f'{i}\tERR\t{e}')
