#!/usr/bin/env python3
"""Drive the real nolang LSP binary over stdio and sample its RSS while
simulating a long editing session. Usage: python3 lsp_mem_probe.py [rounds] [strokes_per_round]"""
import json
import subprocess
import sys
import threading
import time
import os
import collections

REPO = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
LSP_BIN = os.path.join(REPO, "vscode-nolang/server/lsp")

SRC = open(os.path.join(REPO, "tests", "arr.no")).read() if os.path.exists(
    os.path.join(REPO, "tests", "arr.no")) else "println(\"hi\")\n"


def frame(obj):
    body = json.dumps(obj).encode()
    return b"Content-Length: " + str(len(body)).encode() + b"\r\n\r\n" + body


class Reader:
    def __init__(self, stream):
        self.stream = stream
        self.lock = threading.Lock()

    def read(self):
        with self.lock:
            headers = {}
            while True:
                line = self.stream.readline()
                if not line:
                    return None
                line = line.strip()
                if not line:
                    break
                k, _, v = line.decode(errors="replace").partition(":")
                headers[k.strip().lower()] = v.strip()
            n = int(headers.get("content-length", 0))
            if n == 0:
                return None
            body = self.stream.read(n)
            return json.loads(body)


def main():
    rounds = int(sys.argv[1]) if len(sys.argv) > 1 else 20
    strokes = int(sys.argv[2]) if len(sys.argv) > 2 else 30
    proc = subprocess.Popen([LSP_BIN], stdin=subprocess.PIPE,
                            stdout=subprocess.PIPE, stderr=subprocess.DEVNULL)
    rd = Reader(proc.stdout)
    msg_id = [0]

    def send(method, params, notify=False):
        if notify:
            msg = {"jsonrpc": "2.0", "method": method, "params": params}
        else:
            msg_id[0] += 1
            msg = {"jsonrpc": "2.0",
                   "id": msg_id[0], "method": method, "params": params}
        proc.stdin.write(frame(msg))
        proc.stdin.flush()
        return msg.get("id")

    def drain(timeout=0.2):
        deadline = time.time() + timeout
        while time.time() < deadline:
            rd.stream.seek  # noqa
            # non-blocking-ish: rely on line availability check via select
            import select
            r, _, _ = select.select([rd.stream], [], [], 0.02)
            if not r:
                continue
            msg = rd.read()
            if msg is None:
                break

    send("initialize", {"processId": os.getpid(),
         "rootUri": "file://" + REPO, "capabilities": {}})
    drain(3.0)

    uri = "file:///tmp/lsp_probe.no"
    send("textDocument/didOpen", {"textDocument": {"uri": uri,
         "languageId": "nolang", "version": 0, "text": SRC}})
    text = SRC
    rss_log = []
    version = 1

    def sample_rss():
        out = subprocess.run(
            ["ps", "-o", "rss=", "-p", str(proc.pid)], capture_output=True, text=True)
        return int(out.stdout.strip() or 0)

    for r in range(rounds):
        for i in range(strokes):
            # edit: append a line every 3rd stroke to change content
            if i % 3 == 0:
                text = text + "\n// edit %d\n" % (r * strokes + i)
            send("textDocument/didChange", {
                "textDocument": {"uri": uri, "version": version},
                "contentChanges": [{"text": text}],
            }, notify=True)
            version += 1
            if i % 5 == 0:
                jid = send("textDocument/semanticTokens/full",
                           {"textDocument": {"uri": uri}})
            if i % 7 == 0:
                send("textDocument/completion", {
                    "textDocument": {"uri": uri},
                    "position": {"line": 1, "character": 2},
                })
            if i % 11 == 0:
                send("textDocument/hover", {
                    "textDocument": {"uri": uri},
                    "position": {"line": 1, "character": 2},
                })
            if i % 13 == 0:
                send("textDocument/formatting", {
                    "textDocument": {"uri": uri},
                    "options": {"tabSize": 4, "insertSpaces": True},
                })
            drain(0.05)
        rss = sample_rss()
        rss_log.append((r, rss))
        print(f"round {r:2d}  RSS={rss/1024:8.1f} MB", flush=True)

    # also open/close many different documents to test per-URI accumulation
    for j in range(100):
        u = f"file:///tmp/lsp_probe_{j}.no"
        send("textDocument/didOpen", {"textDocument": {"uri": u,
             "languageId": "nolang", "version": 1, "text": SRC}})
        send("textDocument/didClose", {"textDocument": {"uri": u}})
        if j % 10 == 9:
            drain(0.1)
    drain(1.0)
    print(f"after 100 open/close: RSS={sample_rss()/1024:.1f} MB", flush=True)

    send("shutdown", {}, notify=False)
    drain(0.5)
    proc.terminate()

    # report trend
    if len(rss_log) >= 6:
        first = rss_log[len(rss_log)//4][1]
        last = rss_log[-1][1]
        growth = (last - first) / 1024
        print(
            f"\ntrend: quarter({rss_log[len(rss_log)//4][0]})={first/1024:.1f}MB end({rss_log[-1][0]})={last/1024:.1f}MB growth={growth:+.1f}MB")


if __name__ == "__main__":
    main()
