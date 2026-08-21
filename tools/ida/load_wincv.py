"""把 WINCV.IMG 當 flat binary 載進 IDA,種下 3663 個 word 的函式邊界與名字。

為什麼要自己種:WINCV.IMG 沒有 entry point,也沒有任何 PE/ELF 結構。
IDA 的自動分析找不到起點,線性掃描又會被 Forth 的內嵌資料帶偏。
word 邊界是自己解出來的(docs/re/words.tsv),直接餵給 IDA 比讓它猜可靠。

headless 的 print 看不到,結果一律寫檔;exit code 不可信,收工前驗檔案。
"""
import json
import sys
import traceback

OUT = sys.argv[1] if len(sys.argv) > 1 else "/work/out/load.json"
TRACE = "/work/out/trace.txt"
WORDS = "/work/re/words.tsv"
SYMS = "/work/re/symbols.tsv"

log = {"steps": []}


def mark(tag):
    """每一步都留痕跡。

    headless 的例外不進 stdout。腳本在 import 掛掉、在任何一行掛掉、
    或**根本沒被執行**(例如語法錯誤),症狀全都是「什麼都沒有」。
    留痕跡才分得出是哪一種。
    """
    with open(TRACE, "a") as f:
        f.write(tag + "\n")
        f.flush()


def step(name, **kw):
    kw["step"] = name
    log["steps"].append(kw)
    mark("step %s %r" % (name, kw))


def run():
    import ida_auto
    import ida_bytes
    import ida_funcs
    import ida_ida
    import ida_name
    import ida_segment
    import idautils
    import idc

    ida_auto.auto_wait()
    mark("auto_wait done")

    # flat binary 進來預設是 16-bit。STC 的指令是 32-bit
    # (`83 ed 04` sub ebp,4 / `8f 45 00` pop [ebp]),不切過去會整片解錯。
    seg = ida_segment.get_first_seg()
    if seg is not None:
        # [雷] set_segm_addressing 的 bitness 是 0=16 / 1=32 / 2=64。
        # 填 2 會把 32 位元的碼當 64 位元解:0x40-0x4F 在 32 位元是
        # inc/dec,在 64 位元是 REX 前綴 —— 整批指令會沿著錯的邊界解下去,
        # 而反組譯**看起來仍然是合理的指令**(只是暫存器變成 rdi/rax),
        # 不會有任何錯誤訊息。
        ida_segment.set_segm_addressing(seg, 1)
        step("segment", start=seg.start_ea, end=seg.end_ea, bitness=32)

    made = skipped = 0
    with open(WORDS) as f:
        next(f)
        for line in f:
            parts = line.split()
            if not parts:
                continue
            body = int(parts[0], 16) + 4  # code field 佔 4 bytes,本體在 +4
            ida_bytes.del_items(body, ida_bytes.DELIT_SIMPLE, 1)
            if ida_funcs.add_func(body):
                made += 1
            else:
                skipped += 1
    step("add_func", made=made, skipped=skipped)

    ida_auto.auto_wait()

    # symbols.tsv 的 xt 有些落在 image 之外(執行期才配的),那些跳過 ——
    # 硬設會建出一個指向未定義位址的名字。
    named = missed = outside = 0
    lo, hi = ida_ida.inf_get_min_ea(), ida_ida.inf_get_max_ea()
    with open(SYMS) as f:
        next(f)
        for line in f:
            parts = line.rstrip("\n").split("\t")
            if len(parts) < 4:
                continue
            xt = int(parts[1], 16)
            name = parts[3]
            if not (lo <= xt < hi):
                outside += 1
                continue
            # IDA 的名字不吃 Forth 的符號(! @ ? # 這些都合法),
            # 轉成安全字元,原名放進註解免得資訊掉了。
            safe = "".join(c if c.isalnum() or c == "_" else "_" for c in name)
            if not safe or safe[0].isdigit():
                safe = "w_" + safe
            if ida_name.set_name(xt, "f_" + safe, ida_name.SN_NOCHECK | ida_name.SN_FORCE):
                idc.set_cmt(xt, name, 0)
                named += 1
            else:
                missed += 1
    step("set_name", named=named, missed=missed, outside=outside)

    ida_auto.auto_wait()
    log["funcs"] = len(list(idautils.Functions()))
    log["min_ea"] = lo
    log["max_ea"] = hi
    log["ok"] = True


def main():
    mark("start %r" % (sys.argv,))
    try:
        run()
    except Exception:
        log["ok"] = False
        log["traceback"] = traceback.format_exc()
        mark("EXC " + log["traceback"])
    with open(OUT, "w") as f:
        json.dump(log, f, indent=1)
        f.flush()
    mark("wrote %s" % OUT)
    import ida_pro

    ida_pro.qexit(0 if log.get("ok") else 1)


main()
