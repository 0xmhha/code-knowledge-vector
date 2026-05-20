#!/usr/bin/env python3
"""CKV-1 reproduction: spawn ckv MCP server, send three queries over stdio,
measure latency and detect hangs.

Override paths via env: CKV_BIN, CKV_DATA, CKV_CWD, CKV_TIMEOUT_S."""

import json
import os
import subprocess
import sys
import threading
import time

CKV_BIN = os.environ.get("CKV_BIN", "ckv")
CKV_DATA = os.environ.get("CKV_DATA", ".ckv-data")
CKS_CWD = os.environ.get("CKV_CWD", ".")
TIMEOUT_S = int(os.environ.get("CKV_TIMEOUT_S", "12"))  # cks uses 10s

QUERIES = [
    ("control", "MCP server tool registration"),
    ("fail1", "EvidencePack integrity hash stamping and verification"),
    ("fail2", "table-driven tests for FilesystemFetcher line range reads and path escape rejection"),
]


def readline_with_timeout(stream, timeout):
    """Read one line from stream, or return None on timeout."""
    result = [None]
    done = threading.Event()

    def reader():
        result[0] = stream.readline()
        done.set()

    t = threading.Thread(target=reader, daemon=True)
    t.start()
    done.wait(timeout)
    return result[0]


def main():
    p = subprocess.Popen(
        [CKV_BIN, "mcp", "--out=.ckv-data", "--embedder=mock"],
        cwd=CKS_CWD,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        bufsize=0,
    )

    def send(msg):
        line = (json.dumps(msg) + "\n").encode()
        p.stdin.write(line)
        p.stdin.flush()

    # 1. initialize
    send({
        "jsonrpc": "2.0", "id": 1, "method": "initialize",
        "params": {
            "protocolVersion": "2024-11-05",
            "capabilities": {},
            "clientInfo": {"name": "repro", "version": "0"},
        },
    })
    init_line = readline_with_timeout(p.stdout, 5)
    if not init_line:
        print("FATAL: no initialize response")
        p.kill()
        sys.exit(1)
    print(f"[init] OK len={len(init_line)}")

    # 2. initialized notification
    send({"jsonrpc": "2.0", "method": "notifications/initialized", "params": {}})

    # 3. tools/call for each query
    for i, (label, q) in enumerate(QUERIES, start=2):
        send({
            "jsonrpc": "2.0", "id": i, "method": "tools/call",
            "params": {
                "name": "cks.context.semantic_search",
                "arguments": {"intent": q},
            },
        })
        start = time.time()
        line = readline_with_timeout(p.stdout, TIMEOUT_S)
        elapsed = time.time() - start
        if line is None:
            print(f"[{label}] HANG after {elapsed:.2f}s | q={q!r}")
            break
        resp = json.loads(line)
        # response shape: {"id":..., "result":{"content":[{"type":"text","text":"..."}], "isError":false}}
        if "error" in resp:
            print(f"[{label}] ERROR {elapsed:.2f}s | {resp['error']}")
        else:
            text = resp.get("result", {}).get("content", [{}])[0].get("text", "")
            is_err = resp.get("result", {}).get("isError", False)
            print(f"[{label}] {elapsed:.3f}s | bytes={len(text)} isError={is_err} | q={q!r}")

    p.terminate()
    try:
        p.wait(timeout=3)
    except subprocess.TimeoutExpired:
        p.kill()


if __name__ == "__main__":
    main()
