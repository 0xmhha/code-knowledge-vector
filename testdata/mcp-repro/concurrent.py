#!/usr/bin/env python3
"""CKV-1 reproduction with concurrent requests — emulates cks composer
which may issue several semantic_search calls in flight at once.

Override paths via env: CKV_BIN, CKV_DATA, CKV_CWD."""
import json, os, subprocess, threading, time, queue

CKV_BIN = os.environ.get("CKV_BIN", "ckv")
CKV_DATA = os.environ.get("CKV_DATA", ".ckv-data")
CWD = os.environ.get("CKV_CWD", ".")

QUERIES = [
    "MCP server tool registration",
    "EvidencePack integrity hash stamping and verification",
    "table-driven tests for FilesystemFetcher line range reads and path escape rejection",
    "ONNX model inference session configuration",
    "memory guard pre-check",
    "CoreML execution provider attach",
    "tokenizer encoding pipeline",
    "vector index sqlite-vec backend",
]

# Single MCP process; responses come back over one stdout stream. mcp-go
# pairs them by id, so we collect all on a single reader thread and pop
# by id when each caller finishes.
p = subprocess.Popen([CKV_BIN, "mcp", "--out", CKV_DATA, "--embedder=mock"],
    cwd=CWD, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
    stderr=subprocess.PIPE, bufsize=0)

responses = {}  # id -> response dict
responses_lock = threading.Lock()
responses_cv = threading.Condition(responses_lock)
stop = threading.Event()

def reader():
    while not stop.is_set():
        line = p.stdout.readline()
        if not line: break
        try:
            r = json.loads(line)
        except Exception:
            continue
        with responses_cv:
            rid = r.get("id")
            if rid is not None:
                responses[rid] = r
                responses_cv.notify_all()

threading.Thread(target=reader, daemon=True).start()

send_lock = threading.Lock()
def send(msg):
    with send_lock:
        p.stdin.write((json.dumps(msg)+"\n").encode())
        p.stdin.flush()

def wait_for(rid, timeout):
    deadline = time.time() + timeout
    with responses_cv:
        while rid not in responses:
            remaining = deadline - time.time()
            if remaining <= 0:
                return None
            responses_cv.wait(remaining)
        return responses.pop(rid)

# initialize (sync)
send({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"r","version":"0"}}})
init = wait_for(1, 5)
print(f"[init] {'OK' if init else 'FAIL'}")
send({"jsonrpc":"2.0","method":"notifications/initialized","params":{}})

# Fire all queries near-simultaneously
def worker(idx, q):
    rid = 100 + idx
    send({"jsonrpc":"2.0","id":rid,"method":"tools/call",
        "params":{"name":"cks.context.semantic_search","arguments":{"intent":q}}})
    t0 = time.time()
    r = wait_for(rid, 15)
    el = time.time() - t0
    if r is None:
        print(f"[#{rid}] HANG after {el:.2f}s | q={q!r}")
    else:
        text = r.get("result",{}).get("content",[{}])[0].get("text","")
        print(f"[#{rid}] {el*1000:.0f}ms bytes={len(text)} | q={q!r}")

threads = [threading.Thread(target=worker, args=(i, q), daemon=True)
           for i, q in enumerate(QUERIES)]
t_start = time.time()
for t in threads: t.start()
for t in threads: t.join(timeout=20)
t_total = time.time() - t_start
print(f"--- total: {t_total*1000:.0f}ms ({len(QUERIES)} concurrent queries) ---")

stop.set()
p.terminate()
try: p.wait(3)
except: p.kill()
