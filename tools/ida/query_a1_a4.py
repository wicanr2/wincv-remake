"""回答 A1(EDI 是什麼)與 A4(VF-/EF-/VP- 前綴的語意)。

A1 的作法:掃遍整個資料庫,收集所有 `[edi+X]` 形式的存取,看 X 的分布。
如果 EDI 是 image base,X 會散布在整個 image(0-0x186618);
如果是 user area 指標,X 會集中在一小段(Forth 的 user area 通常幾百 bytes)。

A4 的作法:各前綴挑幾個 word,把反組譯與它們呼叫的對象倒出來,看名字。
不猜指令語意 —— 呼叫對象的**名字**是一手資料。
"""
import json
import sys
import traceback
import collections

OUT = sys.argv[1] if len(sys.argv) > 1 else "/work/out/a1a4.json"
TRACE = "/work/out/trace.txt"


def mark(tag):
    with open(TRACE, "a") as f:
        f.write(tag + "\n")
        f.flush()


res = {}


def run():
    import ida_auto
    import ida_bytes
    import ida_funcs
    import ida_name
    import idautils
    import idc

    ida_auto.auto_wait()

    # --- A1:[edi+X] 的位移分布 -------------------------------------------
    disp = collections.Counter()
    samples = []
    for ea in idautils.Heads():
        if not ida_bytes.is_code(ida_bytes.get_flags(ea)):
            continue
        t = idc.GetDisasm(ea)
        if "edi+" not in t:
            continue
        # 只取 [edi+CONST] 這種,不取 [edi+eax*4] 之類
        i = t.find("edi+")
        j = i + 4
        k = j
        while k < len(t) and (t[k].isalnum() or t[k] == "h"):
            k += 1
        tok = t[j:k]
        try:
            v = int(tok[:-1], 16) if tok.endswith("h") else int(tok)
        except ValueError:
            continue
        disp[v] += 1
        if len(samples) < 40:
            samples.append({"ea": ea, "func": ida_funcs.get_func_name(ea), "asm": t})
    res["a1"] = {
        "total": sum(disp.values()),
        "distinct": len(disp),
        "min": min(disp) if disp else None,
        "max": max(disp) if disp else None,
        "top": [[v, n] for v, n in disp.most_common(30)],
        "hist": {
            "lt_0x1000": sum(n for v, n in disp.items() if v < 0x1000),
            "lt_0x10000": sum(n for v, n in disp.items() if v < 0x10000),
            "lt_0x100000": sum(n for v, n in disp.items() if v < 0x100000),
            "ge_0x100000": sum(n for v, n in disp.items() if v >= 0x100000),
        },
        "samples": samples,
    }
    mark("a1 done total=%d" % res["a1"]["total"])

    # --- A4:前綴 word 呼叫了誰 -------------------------------------------
    by_prefix = {}
    for pfx in ("f_VF_", "f_EF_", "f_VP_"):
        names = []
        for ea, name in idautils.Names():
            if name.startswith(pfx):
                names.append((ea, name))
        names.sort()
        entry = {"count": len(names), "words": [n for _, n in names[:60]], "detail": []}
        for ea, name in names[:6]:
            calls = []
            f = ida_funcs.get_func(ea)
            if f:
                for h in idautils.Heads(f.start_ea, f.end_ea):
                    for x in idautils.CodeRefsFrom(h, 0):
                        cn = ida_name.get_name(x)
                        if cn:
                            calls.append(cn)
            entry["detail"].append({
                "name": name, "ea": ea,
                "calls": calls[:40],
            })
        by_prefix[pfx] = entry
        mark("a4 %s n=%d" % (pfx, len(names)))
    res["a4"] = by_prefix


def main():
    mark("query start %r" % (sys.argv,))
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
    mark("query wrote %s" % OUT)
    import ida_pro

    ida_pro.qexit(0 if res.get("ok") else 1)


main()
