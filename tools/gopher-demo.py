#!/usr/bin/env python3
"""README 截圖用的 gopher 示範站(RFC 1436)。

只綁 127.0.0.1,內容寫死在這裡,不讀任何檔案。給 tools/readme-shots.sh 在容器裡
與 celldump 一起跑:不連外,截圖可以重現。

    python3 -I tools/gopher-demo.py [埠號,預設 7070]
"""
import socket
import sys
import threading

PORT = int(sys.argv[1]) if len(sys.argv) > 1 else 7070
H = "127.0.0.1"


def item(t, text, sel="", host=H, port=PORT):
    return f"{t}{text}\t{sel}\t{host}\t{port}\r\n"


def info(text):
    return item("i", text, "", "error.host", 1)


MENU = (
    info("歡迎來到 WinCV 的 gopher 示範站")
    + info("")
    + info("這一行是資訊列,沒有連結,不該長得像可以點的東西。")
    + info("")
    + item("1", "台灣共享軟體年表", "/history")
    + item("0", "關於 WinCV", "/about")
    + item("0", "一段很長的標題用來確認窄畫面折行之後游標還是把整個項目一起反白", "/long")
    + item("7", "站內搜尋", "/search")
    + item("I", "WinCV 主畫面截圖", "/shot.png")
    + item("h", "WinCV 的 GitHub", "URL:https://github.com/wicanr2/wincv-remake")
    + ".\r\n"
)

PAGES = {
    "": MENU,
    "/": MENU,
    "/about": "WinCV(CView for Windows)\r\n作者 Lcc Wizard,1999-2011。\r\n.\r\n",
    "/long": "折行測試。\r\n.\r\n",
    "/history": info("年表") + ".\r\n",
}


def serve(c):
    with c:
        sel = c.recv(1024).decode("utf-8", "replace").split("\t")[0].strip()
        body = PAGES.get(sel, "3找不到\terror\terror.host\t1\r\n.\r\n")
        c.sendall(body.encode("utf-8"))


s = socket.socket()
s.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
s.bind((H, PORT))
s.listen()
while True:
    conn, _ = s.accept()
    threading.Thread(target=serve, args=(conn,), daemon=True).start()
