#!/usr/bin/env bash
# Go 版與 Rust 版的 Office 文件對照:同一份檔案經 officedoc 打開,每一段的每個區塊
# (所有欄位與 span)、內嵌圖片的大小與像素 sha256,逐位元組比對。
#
#   tools/office-parity.sh
#   OFFICE_CORPUS_ROOT=/某處 OFFICE_PER_PROJECT=60 tools/office-parity.sh
#
# 語料有四份,全部放在 .cache/parity/office/ 底下,兩邊的容器都看得到:
#   1. internal/doc97/testdata、internal/officedoc/testdata 的檔案(直接讀,不複製)。
#   2. 真實文件:在容器裡把 $OFFICE_CORPUS_ROOT(預設 ~/cht)唯讀掛進去,找 officedoc
#      認得的副檔名(不分大小寫),依內容 sha256 去重,每個專案(第一層目錄)依 sha256
#      排序取前 $OFFICE_PER_PROJECT 份(預設 60),複製到 corpus/<序號>/<原檔名>
#      (檔名就是 officedoc 的標題,所以照原樣保留)。corpus.tsv 記著複製品與原路徑的對照。
#   3. 產生的邊角案例(gen/):下面那段 Python 產生的 docx / pptx / xlsx / rtf
#      (XML 解碼器的邊角、各種結構、數值與日期、字碼頁、\u 與 \bin),以及把真實文件
#      做位元組翻轉、截斷、XML 組件截半的變形。亂數種子固定,每次產生的內容相同。
#   4. LibreOffice 轉出來的 docx / doc / rtf / pptx / xlsx(lo/out/),來源是上面的真實文件與
#      testdata 的原始檔(見那一段的說明)。OFFICE_LO=0 跳過。
# 最後另外把每一份 ZIP 包的每個 XML 組件逐 token 對照一次(docsum 的 -xml 模式)。
#
# 結果:all.go / all.rust 是兩邊的完整輸出,go/ rust/ 依檔案切開,diff.txt 是不同的那幾份。
set -euo pipefail
REPO=$(cd "$(dirname "$0")/.." && pwd)
OUT=$REPO/.cache/parity/office
ROOT=${OFFICE_CORPUS_ROOT:-$HOME/cht}
PER=${OFFICE_PER_PROJECT:-60}
test -d "$ROOT" || { echo "語料目錄不存在:$ROOT" >&2; exit 2; }
rm -rf "$OUT/go" "$OUT/rust" "$OUT/corpus" "$OUT/gen" "$OUT/lo"
mkdir -p "$OUT/go" "$OUT/rust" "$OUT/corpus" "$OUT/gen"

run=(--rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 2g --pids-limit 256
    -u "$(id -u):$(id -g)" -e HOME=/tmp)

# --- 2. 真實文件 ---
# 在容器裡找、算 sha256、複製(主機不做大量搜尋)。排除建置與快取目錄:它們是產物,不是文件。
# 路徑裡有 TAB 或換行的檔案會讓清單錯位,不收。
read -r -d '' LIST_SH <<'SH' || true
set -e
per=$1
find /corpus \( -name .cache -o -name .git -o -name node_modules -o -name target -o -name cargo-target \) -prune \
    -o -type f -size -64M \( -iname '*.docx' -o -iname '*.docm' -o -iname '*.dotx' -o -iname '*.dotm' -o -iname '*.doc' \
    -o -iname '*.dot' -o -iname '*.rtf' -o -iname '*.pptx' -o -iname '*.pptm' -o -iname '*.ppsx' -o -iname '*.ppsm' \
    -o -iname '*.potx' -o -iname '*.xlsx' -o -iname '*.xlsm' -o -iname '*.xltx' \) -print0 2>/dev/null |
    xargs -0 -r sha256sum | grep -v "$(printf '\t')" | LC_ALL=C sort -k2 |
    awk '{ sha = $1; path = substr($0, 67) } seen[sha]++ { next } { split(path, a, "/"); print a[3] "\t" sha "\t" path }' |
    LC_ALL=C sort -t "$(printf '\t')" -k1,1 -k2,2 |
    awk -F '\t' -v per="$per" 'n[$1]++ < per { print $3 }' |
    { i=0; while IFS= read -r f; do
        mkdir -p "/out/$i"; cp "$f" "/out/$i/"
        printf '/src/.cache/parity/office/corpus/%d/%s\t%s\n' "$i" "$(basename "$f")" "${f#/corpus/}"
        i=$((i + 1)); done; }
SH
docker run "${run[@]}" -v "$ROOT":/corpus:ro -v "$OUT/corpus":/out wincv-build:1 sh -c "$LIST_SH" sh "$PER" > "$OUT/corpus.tsv"

# --- 3. 產生的邊角案例 ---
{ ls "$REPO"/internal/doc97/testdata/*.doc "$REPO"/internal/officedoc/testdata/*.*x | sed "s|^$REPO/|/repo/|"
    cut -f1 "$OUT/corpus.tsv" | sed 's|^/src/|/repo/|'; } > "$OUT/gen-src.txt"
read -r -d '' GEN_PY <<'PY' || true
import io, os, random, sys, zipfile

out = sys.argv[1]
# 第二個參數是清單檔:真實的 .doc / .docx / .pptx / .xlsx,拿來做變形
srcs = [l for l in open(sys.argv[2], encoding='utf-8').read().split('\n') if l] if len(sys.argv) > 2 else []
os.makedirs(out, exist_ok=True)
rng = random.Random(20261110)

W = 'xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006" xmlns:wps="http://schemas.microsoft.com/office/word/2010/wordprocessingShape" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:v="urn:schemas-microsoft-com:vml"'
REL = 'http://schemas.openxmlformats.org/officeDocument/2006/relationships/'
CT = '<?xml version="1.0"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="xml" ContentType="application/xml"/><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/></Types>'


def rels(items):
    s = '<?xml version="1.0"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
    for rid, typ, tgt, *ext in items:
        s += '<Relationship Id="%s" Type="%s%s" Target="%s"%s/>' % (rid, REL, typ, tgt, ' TargetMode="External"' if ext else '')
    return s + '</Relationships>'


def write_zip(name, parts):
    with zipfile.ZipFile(os.path.join(out, name), 'w', zipfile.ZIP_DEFLATED) as z:
        for k in sorted(parts):
            v = parts[k]
            z.writestr(k, v if isinstance(v, bytes) else v.encode('utf-8'))


def make_png(w, h):
    import struct, zlib
    def chunk(t, d):
        return struct.pack('>I', len(d)) + t + d + struct.pack('>I', zlib.crc32(t + d) & 0xffffffff)
    raw = b''.join(b'\x00' + bytes(v for x in range(w) for v in ((x * 70) % 256, (y * 90) % 256, 128)) for y in range(h))
    return (b'\x89PNG\r\n\x1a\n' + chunk(b'IHDR', struct.pack('>IIBBBBB', w, h, 8, 2, 0, 0, 0))
            + chunk(b'IDAT', zlib.compress(raw)) + chunk(b'IEND', b''))


PNG = make_png(3, 2)


def docx(name, body, extra=None, doc_rels=None, head=None):
    d = (head if head is not None else '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>')
    d = d.encode() if isinstance(d, str) else d
    body = body.encode() if isinstance(body, str) else body
    parts = {
        '[Content_Types].xml': CT,
        '_rels/.rels': rels([('rId1', 'officeDocument', 'word/document.xml'), ('rId2', 'metadata/core-properties', 'docProps/core.xml')]),
        'docProps/core.xml': '<?xml version="1.0"?><cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:title> 標題 %s </dc:title></cp:coreProperties>' % name,
        'word/document.xml': d + b'<w:document ' + W.encode() + b'><w:body>' + body + b'</w:body></w:document>',
        'word/media/image1.png': PNG,
        'word/_rels/document.xml.rels': rels(doc_rels or [
            ('rS', 'styles', 'styles.xml'), ('rN', 'numbering', 'numbering.xml'), ('rF', 'footnotes', 'footnotes.xml'),
            ('rE', 'endnotes', 'endnotes.xml'), ('rI', 'image', 'media/image1.png'), ('rL', 'hyperlink', 'https://ex.example/?a=1&amp;b=2', 1),
            ('rM', 'image', 'media/missing.png'), ('rX', 'image', 'http://ex.example/x.png', 1)]),
        'word/styles.xml': '<?xml version="1.0"?><w:styles ' + W + '>'
            '<w:style w:styleId="H1"><w:name w:val="heading 1"/></w:style>'
            '<w:style w:styleId="T"><w:name w:val="Title"/></w:style>'
            '<w:style w:styleId="Sub"><w:name w:val="副標題"/></w:style>'
            '<w:style w:styleId="Loop1"><w:name w:val="x"/><w:basedOn w:val="Loop2"/></w:style>'
            '<w:style w:styleId="Loop2"><w:name w:val="y"/><w:basedOn w:val="Loop1"/></w:style>'
            '<w:style w:styleId="Q"><w:name w:val="Intense Quote"/></w:style>'
            '<w:style w:styleId="O9"><w:name w:val="z"/><w:pPr><w:outlineLvl w:val="9"/></w:pPr></w:style>'
            '<w:style w:styleId="O8"><w:name w:val="z"/><w:pPr><w:outlineLvl w:val=" 8 "/></w:pPr></w:style>'
            '<w:style w:styleId="Heading3"><w:name w:val="??"/></w:style>'
            '<w:style w:styleId="L"><w:name w:val="List"/><w:pPr><w:numPr><w:numId w:val="1"/></w:numPr></w:pPr></w:style>'
            '</w:styles>',
        'word/numbering.xml': '<?xml version="1.0"?><w:numbering ' + W + '>'
            '<w:abstractNum w:abstractNumId="0"><w:lvl w:ilvl="0"><w:start w:val="0"/><w:numFmt w:val="decimal"/></w:lvl>'
            '<w:lvl w:ilvl="1"><w:start w:val="-3"/><w:numFmt w:val="lowerRoman"/></w:lvl>'
            '<w:lvl w:ilvl="2"><w:numFmt w:val="none"/></w:lvl><w:lvl w:ilvl="3"><w:numFmt w:val="bullet"/><w:lvlText w:val="◆"/></w:lvl>'
            '<w:lvl w:ilvl="4"><w:numFmt w:val="bullet"/><w:lvlText w:val="ab"/></w:lvl></w:abstractNum>'
            '<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num><w:num w:numId="2"><w:abstractNumId w:val="7"/></w:num>'
            '</w:numbering>',
        'word/footnotes.xml': '<?xml version="1.0"?><w:footnotes ' + W + '><w:footnote w:type="separator" w:id="-1"><w:p><w:r><w:t>sep</w:t></w:r></w:p></w:footnote>'
            '<w:footnote w:id="1"><w:p><w:r><w:t>一號註腳</w:t></w:r></w:p><w:tbl><w:tr><w:tc><w:p><w:r><w:t>表</w:t></w:r></w:p></w:tc></w:tr></w:tbl></w:footnote>'
            '<w:footnote w:id="2" w:type="normal"><w:p/></w:footnote></w:footnotes>',
        'word/endnotes.xml': '<?xml version="1.0"?><w:endnotes ' + W + '><w:endnote w:id="1"><w:p><w:r><w:t>尾註</w:t></w:r></w:p></w:endnote></w:endnotes>',
    }
    for k, v in (extra or {}).items():
        if v is None:
            parts.pop(k, None)
        else:
            parts[k] = v
    write_zip(name, parts)


def p(t, ppr=''):
    return '<w:p>%s<w:r><w:t>%s</w:t></w:r></w:p>' % (ppr, t)


# --- docx:XML 解碼器的邊角 ---
docx('xml-br-children.docx', '<w:p><w:r><w:t>前</w:t><w:br><w:t>br 裡的字</w:t></w:br><w:t>後</w:t></w:r></w:p>' + p('之後的段落'))
docx('xml-autoclose-names.docx', '<w:p><w:r><w:t>a<img src=x>b</w:t></w:r></w:p><w:p><w:r><w:t>c<hr>d<col/>e<link>f</w:t></w:r></w:p>' + p('尾巴'))
docx('xml-entities.docx', p('&lt;&gt;&amp;&apos;&quot;&nbsp;&copy;&hellip;&bogus;&#x1F600;&#128512;&#xD800;&#x110000;&#99999999999999999999;&#;&#x;&amp') + p('&nbsp &copy x &'))
docx('xml-cdata-comment-pi.docx', '<w:p><!-- 註解 --><w:r><w:t><![CDATA[<不是標記> & 也不是實體]]>尾</w:t><?pi 指令?></w:r></w:p>' + p('a<![CDATA[]]>b'))
docx('xml-doctype.docx', p('x'), head='<?xml version="1.0"?><!DOCTYPE w:document [ <!ENTITY e "y"> <!-- c > --> <!ELEMENT a (b)> ]>')
docx('xml-crlf.docx', '<w:p><w:r><w:t>一\r\n二\r三\n四</w:t></w:r></w:p><w:p><w:hyperlink r:id="rL" w:tooltip="a\r\nb"><w:r><w:t>連\r結</w:t></w:r></w:hyperlink></w:p>')
docx('xml-unquoted-attrs.docx', '<w:p><w:r><w:rPr><w:b w:val=0/><w:i w:val/><w:strike/></w:rPr><w:t>屬性</w:t></w:r></w:p><w:p><w:pPr><w:pStyle w:val=H1></w:pPr><w:r><w:t>標題</w:t></w:r></w:p>')
docx('xml-mismatch.docx', '<w:p><w:r><w:t>a</w:p>' + p('b') + '<w:tbl><w:tr><w:tc>' + p('c') + '</w:tr></w:tbl>' + p('d'))
docx('xml-space-mismatch.docx', '<w:p><w:r><w:t>a</t></w:r></w:p>' + p('之後'))
docx('xml-truncated.docx', '', extra={'word/document.xml': '<?xml version="1.0"?><w:document ' + W + '><w:body>' + p('截斷') + '<w:p><w:r><w:t>ab'})
docx('xml-encoding-1252.docx', p('x'), head='<?xml version="1.0" encoding="windows-1252"?>')
docx('xml-encoding-utf8-variants.docx', p('x'), head="<?xml version='1.0' encoding='Utf-8'?>")
docx('xml-version-11.docx', p('x'), head='<?xml version="1.1"?>')
docx('xml-bom.docx', p('有 BOM'), head=b'\xef\xbb\xbf<?xml version="1.0"?>')
docx('xml-utf16.docx', p('x'), head=b'\xff\xfe<\x00?\x00')
docx('xml-bad-utf8.docx', b'<w:p><w:r><w:t>ok</w:t></w:r></w:p><w:p><w:r><w:t>\xff\xfe</w:t></w:r></w:p>')
docx('xml-illegal-char.docx', p('前') + '<w:p><w:r><w:t>a&#1;b</w:t></w:r></w:p>' + p('後'))
docx('xml-raw-control.docx', p('前') + '<w:p><w:r><w:t>a\x01b</w:t></w:r></w:p>' + p('後'))
docx('xml-cdata-end-in-text.docx', p('a]]>b') + p('後'))
docx('xml-lt-in-attr.docx', '<w:p><w:pPr><w:pStyle w:val="a<b"/></w:pPr><w:r><w:t>x</w:t></w:r></w:p>' + p('後'))
docx('xml-namespaces.docx', '<x:p xmlns:x="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><x:r><x:t>別的前綴</x:t></x:r></x:p>'
     '<p xmlns="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><r><t>預設命名空間</t></r></p>'
     '<q:p><q:r><q:t>不認得的前綴</q:t></q:r></q:p><w:p><w:r xmlns:w="urn:other"><w:t>重新定義</w:t></w:r></w:p>' + p('最後'))
docx('xml-odd-names.docx', '<w:p><w:é>x</w:é><w:r><w:t>名字</w:t></w:r></w:p><w:p><a:b:c/></w:p>' + p('後'))
docx('xml-deep.docx', '<w:p>' + '<w:ins>' * 200 + '<w:r><w:t>深</w:t></w:r>' + '</w:ins>' * 200 + '</w:p>')
# --- docx:結構 ---
docx('docx-structure.docx',
     p('標題', '<w:pPr><w:pStyle w:val="H1"/></w:pPr>') + p('文件標題', '<w:pPr><w:pStyle w:val="T"/></w:pPr>') +
     p('副標', '<w:pPr><w:pStyle w:val="Sub"/></w:pPr>') + p('迴圈樣式', '<w:pPr><w:pStyle w:val="Loop1"/></w:pPr>') +
     p('引言', '<w:pPr><w:pStyle w:val="Q"/></w:pPr>') + p('大綱 9', '<w:pPr><w:pStyle w:val="O9"/></w:pPr>') +
     p('大綱 8', '<w:pPr><w:pStyle w:val="O8"/></w:pPr>') + p('靠 id', '<w:pPr><w:pStyle w:val="Heading3"/></w:pPr>') +
     p('樣式清單', '<w:pPr><w:pStyle w:val="L"/></w:pPr>') +
     ''.join(p('清單 %d-%s' % (i, l), '<w:pPr><w:numPr><w:ilvl w:val="%s"/><w:numId w:val="%s"/></w:numPr></w:pPr>' % (l, n))
             for i, (n, l) in enumerate([('1', '0'), ('1', '0'), ('1', '1'), ('1', '1'), ('1', '2'), ('1', '3'), ('1', '4'), ('1', '9'), ('1', '-1'), ('2', '0'), ('9', '0'), ('1', 'x')])) +
     '<w:p><w:r><w:t>註</w:t></w:r><w:r><w:footnoteReference w:id="1"/></w:r><w:r><w:footnoteReference w:id="2"/></w:r><w:r><w:footnoteReference w:id="1"/></w:r><w:r><w:endnoteReference w:id="1"/></w:r></w:p>' +
     '<w:p><w:r><w:sym w:font="Wingdings" w:char="F0B7"/><w:sym w:char="F0A7"/><w:sym w:char="F0FC"/><w:sym w:char="F041"/><w:sym w:char="263A"/><w:sym w:char="0041"/><w:sym w:char="1F"/><w:sym w:char="zz"/><w:sym w:char="FFFFFFFFFFFFFFFFFF41"/><w:sym w:char="D800"/><w:tab/><w:noBreakHyphen/><w:t>符號</w:t></w:r></w:p>' +
     '<w:p><w:r><w:t>一</w:t><w:cr/><w:t>二</w:t><w:br w:type="PAGE"/><w:t>三</w:t></w:r><w:r><w:br w:type="column"/></w:r><w:r><w:t>四</w:t></w:r></w:p>' +
     '<w:p><w:fldSimple w:instr=" HYPERLINK  \\l &quot;x&quot; "><w:r><w:t>書籤</w:t></w:r></w:fldSimple><w:fldSimple w:instr="hyperlink http://a.example/ \\o tip"><w:r><w:t>簡單</w:t></w:r></w:fldSimple><w:fldSimple w:instr="PAGE"><w:r><w:t>1</w:t></w:r></w:fldSimple></w:p>' +
     '<w:p><w:r><w:fldChar w:fldCharType="begin"/></w:r><w:r><w:instrText>HYPERLINK "unterminated</w:instrText></w:r><w:r><w:fldChar w:fldCharType="separate"/></w:r><w:r><w:t>壞欄位</w:t></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r><w:r><w:fldChar w:fldCharType="end"/></w:r></w:p>' +
     '<w:p><w:hyperlink r:id="rL"><w:r><w:rPr><w:b/></w:rPr><w:t>外部</w:t></w:r><w:r><w:t>連結</w:t></w:r></w:hyperlink><w:hyperlink w:anchor="x"><w:r><w:t>內部</w:t></w:r></w:hyperlink><w:hyperlink r:id="rNo"><w:r><w:t>沒有關聯</w:t></w:r></w:hyperlink></w:p>' +
     '<w:p><w:r><w:drawing><wp:anchor><wp:docPr name="名字" descr=""/><a:graphic><a:graphicData><a:blip r:embed="rI"/><a:blip r:embed="rM"/><a:blip r:link="rX"/><a:blip r:embed="rI"/></a:graphicData></a:graphic></wp:anchor></w:drawing><w:pict><v:shape><v:imagedata r:id="rI" o:title="vml"/></v:shape></w:pict></w:r></w:p>' +
     '<w:tbl><w:tr><w:tc><w:tcPr><w:gridSpan w:val="99"/></w:tcPr>' + p('寬') + '</w:tc></w:tr><w:tr><w:tc><w:tcPr><w:gridSpan w:val="-2"/></w:tcPr><w:p><w:r><w:drawing><a:blip r:embed="rI"/></w:drawing><w:t>格內圖</w:t></w:r></w:p></w:tc><w:tc><w:customXml>' + p('custom') + '</w:customXml><w:sdt><w:sdtContent>' + p('sdt') + '</w:sdtContent></w:sdt><w:tbl><w:tr><w:tc>' + p('巢狀') + '</w:tc></w:tr></w:tbl></w:tc></w:tr><w:tr/></w:tbl>' +
     '<w:sdt><w:sdtContent>' + p('目錄') + '</w:sdtContent></w:sdt><mc:AlternateContent><mc:Fallback>' + p('只有 fallback') + '</mc:Fallback><mc:Choice>' + p('第二個') + '</mc:Choice></mc:AlternateContent>' +
     '<w:p><w:del><w:r><w:t>刪</w:t></w:r></w:del><w:ins><w:r><w:t>插</w:t></w:r><w:del><w:r><w:t>x</w:t></w:r></w:del></w:ins><w:smartTag><w:r><w:t>smart</w:t></w:r></w:smartTag><w:bdo><w:r><w:t>bdo</w:t></w:r></w:bdo></w:p>')
docx('docx-no-rels.docx', p('沒有關聯檔') + '<w:p><w:r><w:footnoteReference w:id="1"/></w:r></w:p>', extra={'word/_rels/document.xml.rels': None})
docx('docx-empty.docx', '')
docx('docx-docm-name.docx', p('主體在別處'), extra={'word/document.xml': None, 'word/document2.xml': '<?xml version="1.0"?><w:document ' + W + '><w:body>' + p('document2') + '</w:body></w:document>', '_rels/.rels': rels([('r', 'officeDocument', '/word/document2.xml')])})
docx('docx-backslash.docx', p('x'), extra={'_rels/.rels': rels([('r', 'officeDocument', '\\word\\document.xml')])})
docx('docx-root-not-document.docx', p('x'), extra={'word/document.xml': '<?xml version="1.0"?><w:documentx ' + W + '><w:body>' + p('x') + '</w:body></w:documentx>'})
docx('docx-dup-rel-ids.docx', '<w:p><w:hyperlink r:id="rL"><w:r><w:t>重複</w:t></w:r></w:hyperlink></w:p>', doc_rels=[('rL', 'hyperlink', 'https://first.example/', 1), ('rL', 'hyperlink', 'https://second.example/', 1)])

# --- pptx ---
P = 'xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:mc="http://schemas.openxmlformats.org/markup-compatibility/2006"'


def sp(ph, xy, paras):
    return '<p:sp><p:nvSpPr><p:cNvPr id="1" name="n"/><p:nvPr>%s</p:nvPr></p:nvSpPr>%s<p:txBody>%s</p:txBody></p:sp>' % (
        '<p:ph %s/>' % ph if ph else '', '<p:spPr><a:xfrm><a:off %s/></a:xfrm></p:spPr>' % xy if xy else '', paras)


def ap(t, ppr='', rpr=''):
    return '<a:p>%s<a:r>%s<a:t>%s</a:t></a:r></a:p>' % (ppr, rpr, t)


def pptx(name, slides, notes=None, layout=None):
    parts = {'[Content_Types].xml': CT, '_rels/.rels': rels([('rId1', 'officeDocument', 'ppt/presentation.xml')]), 'ppt/media/image1.png': PNG}
    lst = ''.join('<p:sldId id="%d" r:id="rS%d"/>' % (256 + i, i) for i in range(len(slides))) + '<p:sldId id="999" r:id="rMissing"/>'
    parts['ppt/presentation.xml'] = '<?xml version="1.0"?><p:presentation %s><p:sldIdLst>%s</p:sldIdLst></p:presentation>' % (P, lst)
    parts['ppt/_rels/presentation.xml.rels'] = rels([('rS%d' % i, 'slide', 'slides/slide%d.xml' % (i + 1)) for i in range(len(slides))])
    for i, s in enumerate(slides):
        parts['ppt/slides/slide%d.xml' % (i + 1)] = '<?xml version="1.0"?><p:sld %s><p:cSld><p:spTree>%s</p:spTree></p:cSld></p:sld>' % (P, s)
        items = [('rI', 'image', '../media/image1.png'), ('rH', 'hyperlink', 'https://h.example/', 1)]
        if layout:
            items.append(('rL', 'slideLayout', '../slideLayouts/slideLayout1.xml'))
        if notes and i < len(notes):
            items.append(('rN', 'notesSlide', '../notesSlides/notesSlide%d.xml' % (i + 1)))
            parts['ppt/notesSlides/notesSlide%d.xml' % (i + 1)] = '<?xml version="1.0"?><p:notes %s><p:cSld><p:spTree>%s</p:spTree></p:cSld></p:notes>' % (P, notes[i])
        parts['ppt/slides/_rels/slide%d.xml.rels' % (i + 1)] = rels(items)
    if layout:
        parts['ppt/slideLayouts/slideLayout1.xml'] = '<?xml version="1.0"?><p:sldLayout %s><p:cSld><p:spTree>%s</p:spTree></p:cSld></p:sldLayout>' % (P, layout)
    write_zip(name, parts)


pptx('pptx-br-children.pptx', [
    sp('type="title"', '', '<a:p><a:r><a:t>第一行</a:t></a:r><a:br><a:rPr lang="zh-TW"/></a:br><a:r><a:t>第二行</a:t></a:r></a:p>') + sp('type="body" idx="1"', '', ap('內文一') + ap('內文二')),
    sp('', 'x="0" y="0"', '<a:p><a:r><a:t>前</a:t></a:r><a:br/><a:r><a:t>後</a:t></a:r></a:p>') + sp('', 'x="5" y="0"', ap('右邊')),
])
pptx('pptx-structure.pptx', [
    sp('idx="0"', '', ap('版面給的標題')) + sp('idx="7"', '', ap('沒對到的 idx')) + sp('type="ctrTitle"', '', '') +
    sp('type="body" idx="1"', '', ap('a', '<a:pPr lvl="9"><a:buChar char="&#xF0B7;"/></a:pPr>') + ap('b', '<a:pPr lvl="-1"><a:buChar char="★"/></a:pPr>') +
       ap('c', '<a:pPr><a:buAutoNum type="arabicPeriod"/><a:buChar char="x"/></a:pPr>') + ap('d', '<a:pPr><a:buChar char="ab"/></a:pPr>') +
       '<a:p><a:fld type="slidenum"><a:t>7</a:t></a:fld><a:r><a:rPr b="1" i="1" strike="sngStrike"><a:hlinkClick r:id="rH"/></a:rPr><a:t>樣式</a:t></a:r><a:r><a:rPr strike="noStrike" b="true"/><a:t>x</a:t></a:r></a:p>') +
    '<p:grpSp>' + sp('', 'x="1" y="2"', ap('群組裡')) + '<p:cxnSp><p:nvCxnSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvCxnSpPr><p:txBody>' + ap('連接線') + '</p:txBody></p:cxnSp></p:grpSp>' +
    '<p:pic><p:nvPicPr><p:cNvPr id="3" name="pic" descr="說明"/></p:nvPicPr><p:blipFill><a:blip r:embed="rI"/></p:blipFill><p:spPr><a:xfrm><a:off x="99999999999999999999" y="-3"/></a:xfrm></p:spPr></p:pic>' +
    '<p:graphicFrame><p:xfrm><a:off x="10" y="10"/></p:xfrm><a:graphic><a:graphicData><c:chart xmlns:c="urn:c" r:id="rC"/></a:graphicData></a:graphic></p:graphicFrame>' +
    '<p:graphicFrame><a:graphic><a:graphicData><a:tbl><a:tr><a:tc gridSpan="3"><a:txBody>' + ap('跨') + ap('兩段') + '</a:txBody></a:tc></a:tr><a:tr><a:tc><a:txBody><a:p/></a:txBody></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>' +
    '<mc:AlternateContent><mc:Choice>' + sp('', '', ap('選擇')) + '</mc:Choice><mc:Fallback>' + sp('', '', ap('備援')) + '</mc:Fallback></mc:AlternateContent>',
    '',
    sp('', 'x=" 7 " y="3"', ap('一段很長' * 20)) + sp('', 'x="1" y="3"', ap('同一列')),
], notes=['', sp('type="sldNum"', '', ap('2')) + '<p:pic><p:blipFill><a:blip r:embed="rI"/></p:blipFill></p:pic>' + '<p:graphicFrame><a:graphic><a:graphicData><a:tbl><a:tr><a:tc><a:txBody>' + ap('備忘表格') + '</a:txBody></a:tc></a:tr></a:tbl></a:graphicData></a:graphic></p:graphicFrame>'],
    layout='<p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr></p:sp><p:sp><p:nvSpPr><p:nvPr><p:ph idx="3" type="body"/></p:nvPr></p:nvSpPr></p:sp>')
pptx('pptx-empty.pptx', [''])

# --- xlsx ---
X = 'xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"'


def xlsx(name, sheets, styles='', shared='', wbpr='', extra=None):
    parts = {'[Content_Types].xml': CT, '_rels/.rels': rels([('rId1', 'officeDocument', 'xl/workbook.xml')])}
    sh = ''.join('<sheet name="%s" sheetId="%d" r:id="rS%d"%s/>' % (n, i + 1, i, st) for i, (n, st, _) in enumerate(sheets))
    parts['xl/workbook.xml'] = '<?xml version="1.0"?><workbook %s>%s<sheets>%s<sheet name="壞" r:id="rNone"/></sheets></workbook>' % (X, wbpr, sh)
    parts['xl/_rels/workbook.xml.rels'] = rels([('rS%d' % i, 'worksheet', 'worksheets/sheet%d.xml' % (i + 1)) for i in range(len(sheets))] +
                                               [('rSS', 'sharedStrings', 'sharedStrings.xml'), ('rSt', 'styles', 'styles.xml')])
    for i, (_, _, data) in enumerate(sheets):
        parts['xl/worksheets/sheet%d.xml' % (i + 1)] = '<?xml version="1.0"?><worksheet %s><sheetData>%s</sheetData></worksheet>' % (X, data)
    parts['xl/sharedStrings.xml'] = '<?xml version="1.0"?><sst %s>%s</sst>' % (X, shared)
    parts['xl/styles.xml'] = '<?xml version="1.0"?><styleSheet %s>%s</styleSheet>' % (X, styles)
    for k, v in (extra or {}).items():
        parts[k] = v
    write_zip(name, parts)


codes = ['yyyy-mm-dd', 'h:mm', '[h]:mm:ss', '"Day" 0', '\\y 0', '0.00%', '"%" 0', '[$-404]e/m/d', 'mm:ss', '[Red]0.0', '\\中y', 'General', '0%;[Red]-0%', 'dddd', 'AM/PM h']
styles = '<numFmts>' + ''.join('<numFmt numFmtId="%d" formatCode="%s"/>' % (200 + i, c.replace('&', '&amp;').replace('"', '&quot;')) for i, c in enumerate(codes)) + '</numFmts><cellXfs>' + \
    ''.join('<xf numFmtId="%d"/>' % i for i in [0, 14, 15, 18, 19, 20, 21, 22, 45, 46, 47, 9, 10]) + ''.join('<xf numFmtId="%d"/>' % (200 + i) for i in range(len(codes))) + '<xf numFmtId="x"/></cellXfs>'
nstyles = 13 + len(codes) + 1
vals = ['45000', '45000.5', '0.1', '0.9999999999999999', '45000.99999999999', '1e400', '-1e400', 'NaN', 'nan', '+nan', 'inf', '-Infinity', '0x1p3', '1_000', ' 12 ', '1.', '.5', '-0', '1e-400',
        '2958465', '2958465.5', '-1', '3.14159265358979323846', '123456789012345678901234567890', '1E5', 'abc', '', '0.000001', '1e21', '1e-7', '60', '61', '59.75',
        '1__0', '_1', '1_', '1e1_0', '1_.5', '0x_1.8p1', '0x1', '0X1P-2', '-0x1p-1074', '0x1p1024', '0x1.fffffffffffff8p1023', '0x.8p1', 'infin', '-infinity', '+Inf', 'NaNx', '1e', '1e+', '--1', '+.5e-3', '0.1e400000', '1e-400000']
cells = ''
for r, v in enumerate(vals):
    cells += '<row r="%d">' % (r + 1) + ''.join('<c r="%s%d" s="%d"><v>%s</v></c>' % (chr(65 + (s % 26)) * (1 + s // 26), r + 1, s, v) for s in range(nstyles)) + '</row>'
xlsx('xlsx-numbers.xlsx', [('數值', '', cells)], styles=styles)
xlsx('xlsx-1904-true.xlsx', [('一', '', '<row r="1"><c r="A1" s="1"><v>45000</v></c></row>')], styles=styles, wbpr='<workbookPr date1904="TRUE"/>')
types = ('<row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v> 1 </v></c><c r="C1" t="s"><v>9</v></c><c r="D1" t="s"><v>-1</v></c><c r="E1" t="s"><v>x</v></c>'
         '<c r="F1" t="b"><v> 1 </v></c><c r="G1" t="b"><v>true</v></c><c r="H1" t="e"><v>#DIV/0!</v></c><c r="I1" t="d"><v>2023-01-01T00:00:00</v></c><c r="J1" t="str"><v>公式</v></c>'
         '<c r="K1" t="inlineStr"><is><r><t>富</t></r><rPh><t>ふ</t></rPh></is></c><c r="L1" t="n"><f>1+1</f><v>2</v></c></row>'
         '<row><c><v>無參照</v></c><c t="inlineStr"><is><t>第二欄</t></is></c></row><row r="0"><c r="A0"><v>零列</v></c></row><row r="-3"><c r="A1"><v>負列</v></c></row>'
         '<row r="9223372036854775807"><c r="A1"><v>1</v></c></row><row><c r="A1"><v>溢位之後</v></c></row>'
         '<row r="7"><c r="1A"><v>壞參照</v></c><c r="AAAAAAAAAAAAAAAAAAAA7"><v>溢位欄</v></c><c r="DX7"><v>超過欄</v></c><c r="B7"></c><c r="C7"><v></v></c></row>')
xlsx('xlsx-types.xlsx', [('型別', '', types), ('', 'state="hidden"', ''), ('超大', 'state="veryHidden"', ''.join('<row r="%d"><c r="A%d"><v>%d</v></c></row>' % (i, i, i) for i in range(3995, 4010))), ('可見', 'state="visible"', '<row><c r="ZZ1"><v>1</v></c></row>')],
     shared='<si><t>零</t></si><si><r><t>一</t></r><r><rPr/><t xml:space="preserve"> 一 </t></r></si>')

# --- rtf ---
def rtf(name, body):
    with open(os.path.join(out, name), 'wb') as f:
        f.write(body if isinstance(body, bytes) else body.encode('utf-8').replace(b'\xc2\x89', b'\x89'))


rtf('rtf-codepages.rtf', r"{\rtf1\ansi\ansicpg1252{\fonttbl{\f0\fcharset0 A;}{\f1\fcharset204 R;}{\f2\fcharset238 C;}{\f3\fcharset161 G;}{\f4\fcharset162 T;}{\f5\fcharset177 H;}{\f6\fcharset178 Ar;}{\f7\fcharset186 B;}{\f8\fcharset163 V;}{\f9\fcharset222 Th;}{\f10\fcharset254 D;}{\f11\fcharset255 D2;}{\f12\fcharset77 M;}{\f13\fcharset128 J;}{\f14\fcharset129 K;}{\f15\fcharset134 S;}{\f16\fcharset136 T;}{\f17\fcharset130 Jo;}{\f18\fcharset2 Sym;}{\f19\cpg874 X;}{\f20\cpg9999 Y;}}"
    + ''.join(r"\f%d " % f + ''.join(r"\'%02x" % b for b in range(0x80, 0x100)) + r"\par " for f in range(21))
    + r"\f13\'82\'a0\'82\f14\'b0\'a1\f15\'c4\'e3\'80\f16\'a4\'a4\'a4\par\mac\'8e\'a5\par\pc\'b0\'b1\par\pca\'90\par\ansicpg437\'b0\par\ansicpg65001\'e4\'b8\'ad\'e4\par}")
rtf('rtf-unicode.rtf', r"{\rtf1\ansi\uc2  3 ??\u-10179 ??\u-8704 ??\uc0\u-10179\u65唵7嚃2\u-8704 x瀀0 \u-70000 䊔967361 香9999999999999999 \u0 \uc33\u97 q\uc-1\u98 r\par唵7 tail\par}")
rtf('rtf-structure.rtf', r"""{\rtf1\ansi\deff0{\fonttbl{\f0 Arial;}}{\colortbl;\red1;}{\stylesheet{\s0 Normal;}{\s1\sbasedon0 heading 1;}{\s2 Heading2;}{\s3 標題 3;}{\s4 标题  4 ;}{\s5 heading 12;}{\*\cs10 Default Paragraph Font;}{\s6\snext6 Title;}}
{\*\generator x;}{\info{\title 不要}}{\header 頁首}{\footer 頁尾}
\pard\s1 大標\par\pard\s4 簡體標題\par\s5 不是標題\par\pard\outlinelevel3 大綱四\par\outlinelevel9 大綱十\par\pard
{\pntext\f0 \'b7\tab}{\*\pn\pnlvlblt}第一點\par{\listtext \uc0慢3 \tab}私用區符號\par{\listtext 123456789\tab}太長\par{\listtext  a. \tab}\ls2\ilvl12 深\par
\pard 粗{\b 體}{\i 斜\i0 正}{\strike 刪}{\striked1 刪2}{\b0 x}\plain 平\par
{\field{\*\fldinst{HYPERLINK "http://a.example/" \\o "t"}}{\fldrslt{\ul 連結一}}}與{\field{\*\fldinst HYPERLINK \\l "bm" }{\fldrslt 書籤}}與{\field{\*\fldinst hyperlink http://b.example/}{\fldrslt 小寫}}\par
\trowd\cellx1\cellx2\intbl 甲\cell 乙\par 換行\cell\row\trowd\intbl 單格\cell\row 表格後沒有 pard\par
\pard\intbl 一格\cell\nestcell\nestrow 巢\par
文字\tab 跳\emdash\endash\lquote\rquote\ldblquote\rdblquote\bullet\~\_\-\{\}\\ 結\line 斷行\page 新頁\sect 節\par
{\*\unknown 丟掉}{\*\fldinst 單獨}{\v 隱藏}{\v0 不隱藏}可見\par
正文{\footnote\pard\plain \chftn 註一{\b 粗}\par 第二段}再{\*\footnote 註二}尾\par
{\pict\pngblip\picw2 89504e470d0a1a0a}{\pict\jpegblip ffd8ff}{\pict\emfblip 0102}{\pict\wmetafile8 0102}{\pict\dibitmap 01}{\pict 0102}{\pict\pngblip 123}{\pict\pngblip zz}{\pict\pngblip\bin4 \x89PNG}{\pict\pngblip \'89\'50}
\'
}""".replace(r'\x89', '\x89'))
rtf('rtf-bin-huge.rtf', r"{\rtf1\ansi 前\bin99999999999999999999 x\par 後\bin-5 y\bin0 z\bin3 abcd\par}")
rtf('rtf-unbalanced.rtf', r"}}}{\rtf1\ansi {{{ 多 } 少 \par")
rtf('rtf-bom.rtf', b'\xef\xbb\xbf  \r\n{\\rtf1\\ansi BOM\\par}')
rtf('rtf-ws-prefix.rtf', b'\r\n\t {\\rtf1\\ansi \\ansicpg950 \\\'a4\\\'a4\\par}')
rtf('rtf-not-quite.rtf', b'{\\rtx1 hello}')
rtf('rtf-empty.rtf', b'{\\rtf1}')
rtf('rtf-trailing-backslash.rtf', b'{\\rtf1\\ansi a\\')

# --- 變形:真實檔案的位元組翻轉與截斷 ---
for si, s in enumerate(srcs):
    data = open(s, 'rb').read()
    base = os.path.basename(s).replace(' ', '_')
    ext = os.path.splitext(s)[1].lower()
    if data[:4] == b'\xd0\xcf\x11\xe0':
        for k in range(6):
            b = bytearray(data)
            for _ in range(rng.randint(1, 40)):
                b[rng.randrange(512, len(b))] = rng.randrange(256)
            open(os.path.join(out, 'mut-%d-%d-%s' % (si, k, base)), 'wb').write(b)
        for cut in (600, len(data) // 2):
            open(os.path.join(out, 'cut-%d-%d-%s' % (si, cut, base)), 'wb').write(data[:cut])
    elif data[:2] == b'PK':
        zin = zipfile.ZipFile(io.BytesIO(data))
        names = zin.namelist()
        # XML 組件逐一截斷:每份文件取幾個組件,各截一半
        for k, n in enumerate([n for n in names if n.endswith('.xml') or n.endswith('.rels')][:6]):
            parts = {m: zin.read(m) for m in names}
            parts[n] = parts[n][:len(parts[n]) // 2]
            write_zip('xmlcut-%d-%d%s' % (si, k, ext), parts)
        # ZIP 本身截斷與翻轉
        open(os.path.join(out, 'zipcut-%d%s' % (si, ext)), 'wb').write(data[:len(data) * 3 // 4])
        b = bytearray(data)
        for _ in range(5):
            b[rng.randrange(30, max(31, len(b) // 2))] ^= 0x55
        open(os.path.join(out, 'zipflip-%d%s' % (si, ext)), 'wb').write(b)
PY
docker run "${run[@]}" -v "$REPO":/repo:ro -v "$OUT/gen":/out wincv-build:1 \
    python3 -I -c "$GEN_PY" /out /repo/.cache/parity/office/gen-src.txt

# --- 4. LibreOffice 轉出來的檔案 ---
# ~/cht 裡真正的 Office 文件很少(大部分 .doc 是 DOS 時代的純文字)。把真實的 .doc / .rtf、
# 純文字的 .DOC、testdata 的原始檔交給 LibreOffice 轉成 docx / doc / rtf / pptx / xlsx,
# 換一個產生器、換一批內容。LibreOffice 會在某些檔案上當掉而中止整批,所以一份一份轉。
# 約三分鐘;OFFICE_LO=0 跳過。
if [ "${OFFICE_LO:-1}" != 0 ]; then
    rm -rf "$OUT/lo" && mkdir -p "$OUT/lo/in-w" "$OUT/lo/in-p" "$OUT/lo/in-c"
    read -r -d '' STAGE_SH <<'SH' || true
set -e
n=0; t=0
cut -f1 /o/corpus.tsv | sed 's|^/src/.cache/parity/office/|/o/|' | while IFS= read -r f; do
    m=$(head -c4 "$f" | od -An -tx1 | tr -d ' \n')
    n=$((n + 1))
    case "$m" in
    d0cf11e0 | 7b5c7274) cp "$f" "/o/lo/in-w/$n-$(basename "$f" | tr -d ' !')" ;;
    *) case "$f" in *.doc | *.DOC)
        # 純文字的 .DOC 取前十份就夠
        if [ "$(ls /o/lo/in-w | grep -c '^t' || true)" -lt 10 ]; then cp "$f" "/o/lo/in-w/t$n-$(basename "$f" | tr -d ' !').txt"; fi ;;
       esac ;;
    esac
done
cp /src/internal/doc97/testdata/rich.html /src/internal/doc97/testdata/notes.rtf /o/lo/in-w/
cp /src/internal/officedoc/testdata/deck.fodp /o/lo/in-p/
cp /src/internal/officedoc/testdata/sheet.csv /o/lo/in-c/
for x in big5-strings words symbols; do cp "/src/docs/re/$x.tsv" "/o/lo/in-c/$x.csv"; done
SH
    docker run "${run[@]}" -v "$REPO":/src:ro -v "$OUT":/o wincv-build:1 sh -c "$STAGE_SH"
    read -r -d '' LO_SH <<'SH' || true
lo() { timeout 60 /usr/bin/soffice --headless -env:UserInstallation=file:///w/.lo "$@" >/dev/null 2>&1 || true; }
for f in in-w/*; do
    lo --convert-to "docx:MS Word 2007 XML" --outdir out/docx "$f"
    lo --convert-to "doc:MS Word 97" --outdir out/doc "$f"
    lo --convert-to rtf --outdir out/rtf "$f"
done
lo --convert-to "pptx:Impress MS PowerPoint 2007 XML" --outdir out/pptx in-p/deck.fodp
lo --infilter="Text - txt - csv (StarCalc):9,34,76,1" --convert-to xlsx --outdir out/xlsx in-c/*.csv
SH
    docker run --rm --log-opt max-size=10m --log-opt max-file=3 --network none --cpus 2 --memory 3g --pids-limit 512 \
        -u "$(id -u):$(id -g)" -e HOME=/w -v "$OUT/lo":/w -w /w --entrypoint sh linuxserver/libreoffice:latest -c "$LO_SH"
fi

# --- 清單 ---
{
    ls "$REPO"/internal/doc97/testdata/* "$REPO"/internal/officedoc/testdata/* |
        grep -iE '\.(docx|docm|dotx|dotm|doc|dot|rtf|pptx|pptm|ppsx|ppsm|potx|xlsx|xlsm|xltx)$' | sed "s|^$REPO/|/src/|"
    cut -f1 "$OUT/corpus.tsv"
    ls "$OUT/gen" | LC_ALL=C sort | sed 's|^|/src/.cache/parity/office/gen/|'
    if [ -d "$OUT/lo/out" ]; then find "$OUT/lo/out" -type f | LC_ALL=C sort | sed "s|^$REPO/|/src/|"; fi
} > "$OUT/files.txt"

# --- 兩邊各跑一次 ---
# Rust 端的 example 叫 officesum,不叫 docsum:epub 也有一支 docsum,兩支建好都放在共用的
# target/release/examples/ 底下,同名會互相覆蓋。
"$REPO/tools/go.sh" run ./tools/docsum-office @/src/.cache/parity/office/files.txt > "$OUT/all.go"
"$REPO/tools/cargo.sh" run -q --release -p wincv-officedoc --example officesum -- @/src/.cache/parity/office/files.txt > "$OUT/all.rust"

# 依檔案切開:檔名是 F 列的序號。
split() {
    awk -v d="$2" -F'\t' '$1 == "F" { if (f) close(f); f = d "/" $2 } { print > f }' "$1"
}
split "$OUT/all.go" "$OUT/go"
split "$OUT/all.rust" "$OUT/rust"

total=$(grep -c . "$OUT/files.txt")
same=0 fail=0
: > "$OUT/diff.txt"
for ((n = 0; n < total; n++)); do
    if [ -f "$OUT/go/$n" ] && cmp -s "$OUT/go/$n" "$OUT/rust/$n"; then
        same=$((same + 1))
    else
        fail=1
        f=$(sed -n "$((n + 1))p" "$OUT/files.txt")
        printf '不同:%s\n' "$f"
        { echo "=== $n $f"; diff "$OUT/go/$n" "$OUT/rust/$n" | head -20 || true; } >> "$OUT/diff.txt"
    fi
done
opened=$(grep -c '^D' "$OUT/all.go" || true)
errs=$(grep -c '^E' "$OUT/all.go" || true)
imgs=$(grep -c '^I' "$OUT/all.go" || true)
blocks=$(grep -c '^B' "$OUT/all.go" || true)
printf '%d 份文件(真實文件 %d、testdata／產生的／LibreOffice 轉出的 %d;打得開 %d、回報錯誤 %d;區塊 %d 個、內嵌圖片 %d 張):%d / %d 相同\n' \
    "$total" "$(grep -c . "$OUT/corpus.tsv")" "$((total - $(grep -c . "$OUT/corpus.tsv")))" "$opened" "$errs" "$blocks" "$imgs" "$same" "$total"
cmp -s "$OUT/all.go" "$OUT/all.rust" && echo "整份輸出逐位元組相同($(wc -c < "$OUT/all.go") 位元組)"

# --- XML 解碼器單獨對照 ---
# 每一份 ZIP 包的每一個組件逐 token 印出來比。上面的對照只走得到 officedoc 會讀的那幾個組件,
# 這裡連圖表、主題、設定、自訂 XML 都走一遍,也看得到錯誤訊息與行號。
grep -iE '\.(docx|docm|dotx|dotm|pptx|pptm|ppsx|ppsm|potx|xlsx|xlsm|xltx)$' "$OUT/files.txt" > "$OUT/zips.txt" || true
"$REPO/tools/go.sh" run ./tools/docsum-office -xml @/src/.cache/parity/office/zips.txt > "$OUT/xml.go"
"$REPO/tools/cargo.sh" run -q --release -p wincv-officedoc --example officesum -- -xml @/src/.cache/parity/office/zips.txt > "$OUT/xml.rust"
parts=$(grep -c '^N' "$OUT/xml.go" || true)
toks=$(grep -c '^T' "$OUT/xml.go" || true)
if cmp -s "$OUT/xml.go" "$OUT/xml.rust"; then
    printf 'XML token:%d 份 ZIP、%d 個組件、%d 個 token,逐位元組相同\n' "$(grep -c . "$OUT/zips.txt")" "$parts" "$toks"
else
    fail=1
    printf 'XML token 不同(%d 個組件、%d 個 token):\n' "$parts" "$toks"
    diff "$OUT/xml.go" "$OUT/xml.rust" | head -20 || true
fi
exit $fail
