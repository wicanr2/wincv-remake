"""A1:EDI 到底是什麼。

不 grep 反組譯文字 —— 那是二手資料,而且名字套上去之後位移可能被符號化。
改用指令解碼 API 直接讀運算元:型別是 o_displ、基底暫存器是 EDI 的,
把位移收集起來看分布。

判準:
  EDI = image base  → 位移散布在整個 image(0-0x186618),而且會落在資料區
  EDI = user area   → 位移集中在很小的一段(Forth 的 user area 通常幾百 bytes)
"""
import collections
import json
import sys
import traceback

OUT = sys.argv[1] if len(sys.argv) > 1 else "/work/out/edi.json"
TRACE = "/work/out/trace.txt"
R_DI = 7  # x86 暫存器編號:eax=0 ecx=1 edx=2 ebx=3 esp=4 ebp=5 esi=6 edi=7


def mark(tag):
    with open(TRACE, "a") as f:
        f.write(tag + "\n")
        f.flush()


res = {}


def run():
    import ida_auto
    import ida_bytes
    import ida_funcs
    import ida_ua
    import idautils
    import idc

    ida_auto.auto_wait()

    disp = collections.Counter()
    samples = []
    insn = ida_ua.insn_t()
    scanned = codeitems = 0
    for ea in idautils.Heads():
        scanned += 1
        if not ida_bytes.is_code(ida_bytes.get_flags(ea)):
            continue
        codeitems += 1
        if ida_ua.decode_insn(insn, ea) <= 0:
            continue
        for op in insn.ops:
            if op.type == 0:  # o_void,後面沒有運算元了
                break
            if op.type != ida_ua.o_displ:
                continue
            if op.reg != R_DI:
                continue
            v = op.addr & 0xFFFFFFFF
            disp[v] += 1
            if len(samples) < 60:
                samples.append({"ea": ea, "func": ida_funcs.get_func_name(ea),
                                "asm": idc.GetDisasm(ea), "disp": v})
    res["scanned"] = scanned
    res["code_items"] = codeitems
    res["total"] = sum(disp.values())
    res["distinct"] = len(disp)
    if disp:
        res["min"] = min(disp)
        res["max"] = max(disp)
        res["top"] = [[v, n] for v, n in disp.most_common(40)]
        res["hist"] = {
            "lt_0x100": sum(n for v, n in disp.items() if v < 0x100),
            "0x100_0x1000": sum(n for v, n in disp.items() if 0x100 <= v < 0x1000),
            "0x1000_0x10000": sum(n for v, n in disp.items() if 0x1000 <= v < 0x10000),
            "0x10000_0x122794": sum(n for v, n in disp.items() if 0x10000 <= v < 0x122794),
            "ge_header_space": sum(n for v, n in disp.items() if v >= 0x122794),
        }
    res["samples"] = samples
    mark("edi total=%d distinct=%d" % (res["total"], res["distinct"]))

    # 順便把三個前綴的完整清單倒出來(A4)
    pfx = {}
    for p in ("f_VF_", "f_EF_", "f_VP_"):
        pfx[p] = sorted(n for _, n in idautils.Names() if n.startswith(p))
    res["prefixes"] = pfx


def main():
    mark("edi query start")
    try:
        run()
        res["ok"] = True
    except Exception:
        res["ok"] = False
        res["traceback"] = traceback.format_exc()
        mark("EXC " + res["traceback"])
    with open(OUT, "w") as f:
        json.dump(res, f, indent=1)
        f.flush()
    import ida_pro

    ida_pro.qexit(0 if res.get("ok") else 1)


main()
