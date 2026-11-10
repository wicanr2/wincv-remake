#!/usr/bin/env bash
# Go 版與 Rust 版的 web / gopher / epub 對照:同一份輸入,區塊的每個欄位逐位元比對。
#
#   tools/net-parity.sh                 # 預設語料 ~/cht,每個專案最多抽 20 個
#   CORPUS=/some/dir PER=40 tools/net-parity.sh
#
# 輸入:
#   - 語料裡的 *.html / *.htm / *.xhtml(依內容去重、每個第一層目錄抽樣,8 MiB 以下)。
#     每個檔案跑三種模式:html(經 Client.Fetch,走解碼與接連結)、raw(EPUB 章節的路)、
#     gopher(把它當選單與文字檔)。
#   - 這裡產生的邊角案例:HTML(畸形標記、實體、各種宣告錯的編碼、相對位址)、
#     gopher 選單與文字檔、EPUB(container 壞掉、OPF 要退回寬鬆模式、宣告 GBK、
#     EPUB 3 的 nav、大小寫不同的路徑、壞位元組的章節、不支援的壓縮法……)。
#     語料裡沒有 EPUB,所以 EPUB 全是產生的,章節內容取自抽到的真實 HTML。
#
# 輸出在 .cache/parity/net/:all.go、all.rust 是整份輸出,go/、rust/ 依輸入切開,
# diff.txt 是不同的那幾份的 diff。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
CORPUS=${CORPUS:-$HOME/cht}
PER=${PER:-20}
OUT=$REPO/.cache/parity/net
[ -d "$CORPUS" ] || { echo "沒有語料目錄 $CORPUS" >&2; exit 2; }
rm -rf "$OUT"
mkdir -p "$OUT/go" "$OUT/rust" "$OUT/synth"

common=(--rm -i --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g --pids-limit 256
    -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$CORPUS":/corpus:ro)

# 1. 清單與邊角案例。抽樣用固定亂數種子,可重現。
docker run "${common[@]}" -v "$OUT":/out python:3.12-slim python3 -I - "$PER" <<'PY'
import hashlib, io, os, random, sys, zipfile
per = int(sys.argv[1])
skip = {'.git', '.cache', 'node_modules', 'target', 'dist', 'dist-all'}
seen, groups = set(), {}
for root, dirs, files in os.walk('/corpus'):
    dirs[:] = sorted(d for d in dirs if d not in skip)
    for f in sorted(files):
        if not f.lower().endswith(('.html', '.htm', '.xhtml')):
            continue
        p = os.path.join(root, f)
        try:
            if os.path.getsize(p) > 8 << 20:
                continue
            h = hashlib.sha256(open(p, 'rb').read()).digest()
        except OSError:
            continue
        if h not in seen:
            seen.add(h)
            groups.setdefault(p.split('/')[2], []).append(p)
rng = random.Random(20261110)
real = []
for g in sorted(groups):
    real += sorted(rng.sample(groups[g], min(per, len(groups[g]))))
print(f'{len(real)} 個 HTML(內容不重複的共 {sum(map(len, groups.values()))} 個,{len(groups)} 個專案)', file=sys.stderr)

S = '/out/synth'
def put(name, data):
    p = os.path.join(S, name)
    open(p, 'wb').write(data if isinstance(data, bytes) else data.encode('utf-8'))
    return '/synth/' + name

big5 = '中文的段落'.encode('big5')
html_edge = {
 'h01-broken.html': '<body><p>一<b>二<p>三<i>四</b>五</i><ul><li>甲<li>乙</ul><li>孤兒<table><tr><td>a<td>b</tr><tr><th>c</table></body>',
 'h02-entities.html': '<p>&amp &copy; &notit; &notin; &#x; &#; &#99999999999; &#128; &#x80; &#0; &ampx &AElig &lt;b&gt; &nbsp;x</p>'
                      '<a href="?a=1&copy=2&amp;b=3&lang=en&notit=1">實體在屬性</a><img src="x.png?a&amp;b" alt="&quot;引號&quot;">',
 'h03-unquoted-slash.html': '<p><a href=http://example.org/>斜線結尾</a> 之後</p><p><a href=rel/path/>相對</a> 字</p>',
 'h04-rawtext.html': '<title>標題 &amp; 甲</title><script/>腳本自閉合之後<p>段</p><title/>標題自閉合<p>x</p>'
                     '<noscript><p>不該出現</p></noscript><xmp><b>不是粗體</b></xmp><textarea>&lt;表單&gt;</textarea>'
                     '<iframe><p>框</p></iframe><svg><text>向量</text><svg>巢狀</svg></svg>尾巴<style>p{}</style>'
                     '<script>document.write("</scr"+"ipt>")</script><script><!--<script>x</script>--></script>後<plaintext><p>純文字到底',
 'h05-latin1.html': b'<html><head><meta charset="iso-8859-1"><title>Caf\xe9</title></head><body><p>na\xefve r\xe9sum\xe9</p>'
                    b'<a href="caf\xe9.html?q=\xe9#fr\xe9">l\xe9n</a><img src="\xe9.png" alt="\xe9"><pre>  \xe9\n  x</pre></body></html>',
 'h06-utf8-but-big5.html': b'<meta charset="utf-8"><p>' + big5 * 20 + b'</p><a href="' + big5 + b'.html">' + big5 + b'</a>',
 'h07-big5-undeclared.html': b'<p>' + big5 * 30 + b'</p>',
 'h08-big5-but-utf8.html': '<meta http-equiv="Content-Type" content="text/html; charset=big5"><p>這其實是 UTF-8 的中文字,'
                           '宣告卻寫成 Big5,解出來會是一堆替代字元,要退回判讀。</p>' * 3,
 'h09-empty-endtag.html': '<p>a </> b</p><p>c</ > d</p><p>e<!-- 註解 -->f g <!-- x --> h</p><p>i <? pi ?> j</p>',
 'h10-nul-cr.html': b'<p>a\x00b\rc\r\nd\x0ce</p><pre>x\ry\r\nz</pre><p title="t\x00">\x00</p>',
 'h11-structure.html': '<h1>一</h1><h2>二</h2><h3>三</h3><h4>四</h4><h5>五</h5><h6>六</h6><h7>七</h7>'
                       '<blockquote>引言<p>段</p>後</blockquote><hr><hr/><ol><li>a<ol><li>b</li><li>c</li></ol></li><li>d</li></ol>'
                       '<pre>  前<br>後\n<b>粗</b>  </pre><pre>\n\n</pre><img src="" alt="空"><img src="a.png"><a>無 href</a>'
                       '<a href="">空</a><a href=" ">空白</a><p><code>碼</code><kbd>k</kbd><s>刪</s><del>d</del><em>斜</em><strong>粗<i>斜粗</i></strong></p>'
                       '<dl><dt>詞<dd>解</dl><center>置中</center><details><summary>摘</summary>內</details>',
 'h12-hrefs.html': ''.join(f'<p><a href="{h}">{i}</a></p>' for i, h in enumerate([
     '../a.html', '../../../../x', '//other.org/x', '?q=1', '#frag', 'mailto:a@b', 'javascript:void(0)', 'a b.html',
     '中文.html', '%zz', '%41', 'http://[::1]:80/x', 'http://a:b@c/d', 'HTTP://UPPER.org/P', ' spaced.html ',
     'a\nb.html', 'a\\b.html', 'http://x:/p', 'g;x?y#s', 'http:', 'http:/rel', 'ftp://x/y', './.', '..', '/abs',
     'http://a/b?c d', 'http://例子.測試/路徑', 'x:y', '/a:b', 'a/./b/../c'])),
 'h13-bom.html': b'\xef\xbb\xbf<!DOCTYPE html><html><body><p>BOM</p></body></html>',
 'h14-big.html': '<p>' + '很長的一段。' * ((5 << 20) // 18) + '</p>',
 'h15-pre-nesting.html': '<pre>外<pre>內</pre>仍在外</pre><p>後</p><pre>a</pre></pre><p>多關一次</p>',
 'h16-whitespace.html': '<p>  開頭\t\t空白 <b> 粗 </b> 結尾  </p><p>\u3000全形空白\u00a0不換行空白</p><p> </p><p><b> </b></p>',
 'h17-utf16.html': '\ufeff<p>UTF-16 的頁面</p>'.encode('utf-16-le'),
 'h18-sjis.html': '<meta charset="shift_jis"><p>日本語のテキスト</p>'.encode('shift_jis'),
 'h19-gbk.html': '<meta charset="gb2312"><p>简体中文的段落</p>'.encode('gbk'),
 'h20-attrs.html': '<a HREF="Up.html" href="second.html">大寫屬性</a><img SRC=a.png ALT=替代><a href=\'單引號\'>s</a>'
                   '<a href=x.html/>自閉合 a</a><b/>粗自閉合<p/>段自閉合<br/>換行</p><a href="a"b="c">黏</a>',
 'h21-mixed-charset.html': b'<meta charset="windows-1252"><p>\x93quoted\x94 \x80 euro</p>',
 'h22-ctype-like.html': '<meta content="text/html; CHARSET = &quot;UTF-8&quot;"><p>大寫 CHARSET</p>',
}
synth = []
for name, data in html_edge.items():
    synth.append(put(name, data))

gopher_edge = {
 'g01-sample.gph': 'iWelcome to the test server\t\terror.host\t1\r\ni\t\terror.host\t1\r\n1Documents\t/docs\texample.org\t70\r\n'
                   '0Read me\t/readme.txt\texample.org\t70\r\nIa picture\t/pic.png\texample.org\t70\r\n'
                   'hProject page\tURL:https://example.org/\texample.org\t70\r\ni沒有欄位的說明列\r\n.\r\n',
 'g02-edge.gph': b'1\xa4\xa4\xa4\xe5\tsel\xa4\xe5\thost\t70\n' b'0x\t/a\tERROR.HOST\t70\n' b'2phone\t/p\th\t\n'
                 b'4bin\t/4\th\t71\n' b'5x\t/5\th\t70\n' b'6x\t/6\th\t70\n' b'8tel\t/8\th\t23\n' b'Ttn\t/T\th\t23\n'
                 b'+mirror\t/m\th\t70\n' b'?unknown\t/q\th\t70\n' b'hweb\turl:http://x/\th\t70\n' b'hnoturl\t/u\th\t70\n'
                 b'7search\t/find\t[::1]\t7070\n' b'\n' b'9\t/empty-display\th\t70\n' b'i\tonly-tabs\t\t\t\textra\n'
                 b'gpic\t/g.gif\th\t70\r\r\n' b'.\n' b'1after\t/a\th\t70\n',
 'g03-big5.txt': '  第一列\r\n\r\n     縮排的第二列\r\n.\r\n之後\r\n'.encode('big5'),
 'g04-empty.txt': b'',
 'g05-dot.txt': b'.\r\n',
 'g06-trailing.txt': b'a\n\n  \n\t\n',
 'g07-utf16.txt': '\ufeff第一列\n第二列\n'.encode('utf-16-le'),
}
for name, data in gopher_edge.items():
    synth.append(put(name, data))

# EPUB:章節內容從抽到的真實 HTML 取幾份(固定順序)。
pick = [open(p, 'rb').read() for p in real[::max(1, len(real) // 6)][:6]] or [b'<p>x</p>']
container = '''<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>'''
def opf(title='測試書', items='', spine='', meta='', head='<?xml version="1.0"?>', toc=' toc="ncx"'):
    return f'''{head}
<package xmlns:opf="http://www.idpf.org/2007/opf" xmlns:dc="http://purl.org/dc/elements/1.1/"
         xmlns:dcterms="http://purl.org/dc/terms/" xmlns="http://www.idpf.org/2007/opf" version="2.0">
  <metadata><dc:title>{title}</dc:title><dc:creator> 某某 </dc:creator>{meta}</metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="ch1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="text/ch2.xhtml" media-type="application/xhtml+xml"/>
    <item id="c3" href="ch3.xhtml" media-type="application/xhtml+xml"/>{items}
  </manifest>
  <spine{toc}><itemref idref="c1"/><itemref idref="c2" linear="no"/><itemref idref="missing"/><itemref idref="c3"/>{spine}</spine>
</package>'''
ncx = '''<?xml version="1.0"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><navMap>
  <navPoint id="n1"><navLabel><text> 第一章 </text></navLabel><content src="ch1.xhtml#a"/>
    <navPoint id="n1b"><navLabel><text>子節</text></navLabel><content src="text/ch2.xhtml"/></navPoint></navPoint>
  <navPoint id="n1c"><navLabel><text>第一章之二</text></navLabel><content src="ch1.xhtml#b"/></navPoint>
  <navPoint id="n3"><navLabel><text>第三</text><text>覆蓋</text></navLabel><content src="ch3.xhtml"/><content/></navPoint>
</navMap></ncx>'''
chap = '<?xml version="1.0" encoding="utf-8"?><!DOCTYPE html><html xmlns="http://www.w3.org/1999/xhtml"><head><title>章</title></head><body><h1>{}</h1><p>內文 {}</p><img src="../images/p.png" alt="圖"/><img src="images/none.png"/></body></html>'
def book(name, files, method=zipfile.ZIP_DEFLATED):
    p = os.path.join(S, name)
    with zipfile.ZipFile(p, 'w', method) as z:
        for k, v in files:
            z.writestr(k, v)
    synth.append('/synth/' + name)
base = [('mimetype', 'application/epub+zip'), ('META-INF/container.xml', container), ('OEBPS/toc.ncx', ncx),
        ('OEBPS/ch1.xhtml', chap.format('一', 1)), ('OEBPS/text/ch2.xhtml', chap.format('二', 2)),
        ('OEBPS/ch3.xhtml', pick[0]), ('OEBPS/images/p.png', b'\x89PNG\r\n\x1a\n')]
book('e01-basic.epub', base + [('OEBPS/content.opf', opf())])
book('e02-nbsp-title.epub', base + [('OEBPS/content.opf', opf(title='有&nbsp;實體'))])
book('e03-meta-autoclose.epub', base + [('OEBPS/content.opf', opf(title='&eacute;', meta='<meta property="dcterms:modified">2020</meta>'))])
book('e04-gbk-decl.epub', base + [('OEBPS/content.opf', opf(head='<?xml version="1.0" encoding="GBK"?>'))])
book('e05-container-broken.epub', [(k, '<container>壞掉的' if k == 'META-INF/container.xml' else v) for k, v in base] + [('OEBPS/content.opf', opf())])
book('e06-no-container.epub', [kv for kv in base if kv[0] != 'META-INF/container.xml'] + [('OEBPS/content.opf', opf())])
book('e07-case.epub', [(k.replace('ch1', 'CH1'), v) for k, v in base] + [('OEBPS/content.opf', opf())])
book('e08-ncx-entity.epub', [(k, v.replace('第三', '第三&mdash;節') if k.endswith('.ncx') else v) for k, v in base] + [('OEBPS/content.opf', opf())])
nav = '<html><body><nav><ol><li><a href="ch1.xhtml#x"> 序章 </a></li><li><a href="text/ch2.xhtml">二</a><a href="ch3.xhtml">三</a></li></ol></nav></body></html>'
book('e09-epub3-nav.epub', [kv for kv in base if not kv[0].endswith('.ncx')] + [('OEBPS/nav.xhtml', nav),
     ('OEBPS/content.opf', opf(items='<item id="nav" href="nav.xhtml" properties="nav" media-type="application/xhtml+xml"/>', toc=''))])
raw_ch = [('OEBPS/ch1.xhtml', b'\xef\xbb\xbf<p>BOM \xa4\xa4\xa4\xe5</p><img src="\xa4.png"/>'), ('OEBPS/text/ch2.xhtml', b'<pre>\r\n a\xff\r\n</pre>')]
book('e10-raw-bytes.epub', [kv for kv in base if kv[0] not in ('OEBPS/ch1.xhtml', 'OEBPS/text/ch2.xhtml')] + raw_ch + [('OEBPS/content.opf', opf())])
book('e11-stored.epub', base + [('OEBPS/content.opf', opf())], zipfile.ZIP_STORED)
p = os.path.join(S, 'e12-mixed-methods.epub')
with zipfile.ZipFile(p, 'w') as z:
    for k, v in base + [('OEBPS/content.opf', opf())]:
        z.writestr(zipfile.ZipInfo(k), v, compress_type=zipfile.ZIP_BZIP2 if k.endswith('ch3.xhtml') else zipfile.ZIP_DEFLATED)
synth.append('/synth/e12-mixed-methods.epub')
root_opf = opf().replace('ch1.xhtml', 'OEBPS/ch1.xhtml')
book('e13-root-opf.epub', [kv for kv in base if kv[0] != 'META-INF/container.xml'] + [('content.opf', root_opf), ('toc.ncx', ncx)])
book('e14-real-chapters.epub', [('META-INF/container.xml', container), ('OEBPS/content.opf', opf()),
     ('OEBPS/ch1.xhtml', pick[min(1, len(pick) - 1)]), ('OEBPS/text/ch2.xhtml', pick[min(2, len(pick) - 1)]), ('OEBPS/ch3.xhtml', pick[-1])])
book('e15-no-chapters.epub', [('META-INF/container.xml', container), ('OEBPS/content.opf', opf())])
book('e16-not-epub.epub', [('a.txt', '這只是個 zip')])
put('e17-not-zip.epub', '根本不是 zip')
synth.append('/synth/e17-not-zip.epub')
book('e18-doctype-cdata.epub', base + [('OEBPS/content.opf', opf(title='<![CDATA[<CDATA 書名>]]>',
     head='<?xml version="1.0"?>\n<!DOCTYPE package [ <!ENTITY x "y"> <!-- 註解 > --> ]>\n<!-- 開頭的註解 -->'))])
book('e19-unclosed.epub', base + [('OEBPS/content.opf', opf().replace('</package>', ''))])
book('e20-multi-rootfile.epub', [(k, v.replace('<rootfile full-path="OEBPS/content.opf"', '<rootfile full-path="nope.opf"/><rootfile full-path="OEBPS/content.opf"')
     if k == 'META-INF/container.xml' else v) for k, v in base] + [('OEBPS/content.opf', opf())])
with zipfile.ZipFile(os.path.join(S, 'e21-duplicate-names.epub'), 'w') as z:
    import warnings; warnings.simplefilter('ignore')
    for k, v in base + [('OEBPS/content.opf', opf()), ('OEBPS/ch3.xhtml', '<p>後寫的同名檔</p>')]:
        z.writestr(k, v)
synth.append('/synth/e21-duplicate-names.epub')
book('e22-mismatch-end.epub', base + [('OEBPS/content.opf', opf(meta='<dc:subject>x</subject>'))])
# 嚴格模式在 spine 之後才失敗:寬鬆模式解進同一個結構,章節重複一份。
book('e23-error-after-spine.epub', base + [('OEBPS/content.opf', opf().replace('</package>', '<guide><reference title="&nbsp;"/></guide></package>'))])
# 嚴格模式在 itemref 裡面失敗:回滾的那一格(帶著 idref="c3")被寬鬆模式的第一個 itemref 沿用,
# 那一個本來沒有 idref,於是多出一章。
book('e24-stale-slot.epub', base + [('OEBPS/content.opf', opf().replace('<spine toc="ncx">', '<spine toc="ncx"><itemref linear="yes"/>').replace('<itemref idref="c3"/>', '<itemref idref="c3"><x>&bad;</x></itemref>'))])

with open('/out/list.txt', 'w', encoding='utf-8') as f:
    for p in real:
        for m in ('html', 'raw', 'gopher'):
            f.write(f'{m}\t{p}\n')
    for p in synth:
        n = os.path.basename(p)
        modes = ('epub',) if n.endswith('.epub') else ('gopher',) if n.startswith('g') else ('html', 'raw', 'gopher')
        for m in modes:
            f.write(f'{m}\t{p}\n')
print(f'邊角案例 {len(synth)} 份', file=sys.stderr)
PY

# 2. Go 與 Rust 各跑一次。
docker run "${common[@]}" -v "$OUT/synth":/synth:ro -v "$REPO":/src:ro -w /src \
    -v "$REPO/.cache/gomod":/gomod -v "$REPO/.cache/gobuild":/gobuild \
    -e GOFLAGS=-buildvcs=false -e GOCACHE=/gobuild -e GOMODCACHE=/gomod -e GOPROXY=off \
    wincv-build:1 go run ./tools/docsum-net < "$OUT/list.txt" > "$OUT/all.go"
"$REPO/tools/cargo.sh" build -q --release -p wincv-epub --example docsum
docker run "${common[@]}" -v "$OUT/synth":/synth:ro -v "$REPO/.cache/cargo-target":/cargo-target:ro \
    wincv-rust:1 /cargo-target/release/examples/docsum < "$OUT/list.txt" > "$OUT/all.rust"

# 3. 依輸入切開比較。
split() {
    awk -v d="$2" -F'\t' '$1 == "F" { n++; if (f) close(f); f = d "/" n } { print > f }' "$1"
}
split "$OUT/all.go" "$OUT/go"
split "$OUT/all.rust" "$OUT/rust"
total=$(wc -l < "$OUT/list.txt")
same=0
: > "$OUT/diff.txt"
declare -A diffmode=()
for i in $(seq 1 "$total"); do
    if [ -f "$OUT/go/$i" ] && cmp -s "$OUT/go/$i" "$OUT/rust/$i"; then
        same=$((same + 1))
    else
        line=$(sed -n "${i}p" "$OUT/list.txt")
        mode=${line%%$'\t'*}
        diffmode[$mode]=$(( ${diffmode[$mode]:-0} + 1 ))
        { echo "=== $i $line"; diff "$OUT/go/$i" "$OUT/rust/$i" | head -20 || true; } >> "$OUT/diff.txt"
    fi
done
printf '%d 份輸入:%d 相同' "$total" "$same"
for m in "${!diffmode[@]}"; do printf ',%s 不同 %d' "$m" "${diffmode[$m]}"; done
echo
cmp -s "$OUT/all.go" "$OUT/all.rust" && echo "整份輸出逐位元相同($(wc -c < "$OUT/all.go") 位元組)"
[ "$same" -eq "$total" ]
