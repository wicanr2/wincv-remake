import json, sys, traceback
out = sys.argv[1]
try:
    import ida_auto, ida_pro, ida_ida, idautils
    ida_auto.auto_wait()
    r = {"ok": True, "funcs": len(list(idautils.Functions())),
         "min": ida_ida.inf_get_min_ea(), "max": ida_ida.inf_get_max_ea()}
except Exception:
    r = {"ok": False, "traceback": traceback.format_exc()}
with open(out, "w") as f:
    json.dump(r, f, indent=1)
    f.flush()
try:
    import ida_pro
    ida_pro.qexit(0)
except Exception:
    pass
