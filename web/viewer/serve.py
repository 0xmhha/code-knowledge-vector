#!/usr/bin/env python3
"""
serve.py — ckv Atlas viewer 백엔드.

역할
----
1. 정적 서빙: index.html + data/(points.json, projection.json)
2. /query: 검색을 100% '진짜 ckv' 로 수행 — `ckv query --json` 호출
   (MCP semantic_search 와 동일한 engine.Search 경로: bge-m3 임베딩 →
   sqlite-vec KNN → threshold → citation 검증). 결과 chunk_id 들이
   화면의 점들과 매핑되어 하이라이트된다.
3. 질의 벡터를 ollama(bge-m3) 로 직접 임베딩해 export_projection.py 가
   저장한 PCA 행렬로 3D 투영 → "질의가 공간 어디에 떨어졌는지"도 반환.
   (ollama 실패 시 query_xyz 없이 히트만 반환 — 기능 저하일 뿐 동작함)

전제: ollama serve + `ollama pull bge-m3`, PATH 의 ckv (또는 --ckv-bin)

실행:
  python3 export_projection.py   # 최초 1회 (data/ 생성)
  python3 serve.py               # → http://localhost:8098
"""

import argparse
import json
import math
import os
import shlex
import subprocess
import urllib.parse
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

HERE = os.path.dirname(os.path.abspath(__file__))
DATA = os.path.join(HERE, "data")
OLLAMA = os.environ.get("CKV_OLLAMA_ENDPOINT", "http://localhost:11434").rstrip("/")
CFG = {}
PROJ = {"loaded": False}


def load_projection():
    p = os.path.join(DATA, "projection.json")
    if not os.path.exists(p):
        return
    with open(p, encoding="utf-8") as f:
        d = json.load(f)
    PROJ.update(mean=d["mean"], comps=d["components"], scale=d["scale"],
                dim=d["dim"], evr=d.get("explained_variance_ratio"),
                axes=d.get("axes"), loaded=True)


def embed_query(text):
    """ollama bge-m3 로 질의 임베딩 (unit-norm). 실패 시 None."""
    try:
        body = json.dumps({"model": CFG["model"], "prompt": text}).encode()
        req = urllib.request.Request(OLLAMA + "/api/embeddings", data=body,
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=30) as r:
            data = json.load(r)
        vec = data.get("embedding") or (data.get("embeddings") or [[]])[0]
        if len(vec) != PROJ.get("dim", 1024):
            return None
        norm = math.sqrt(sum(x * x for x in vec)) or 1.0
        return [x / norm for x in vec]
    except Exception:
        return None


def project(vec):
    """저장된 PCA 행렬(mean/components/scale)로 3D 투영 — 점들과 동일 공간."""
    if not PROJ["loaded"] or vec is None:
        return None
    m, comps, sc = PROJ["mean"], PROJ["comps"], PROJ["scale"]
    c = [v - mv for v, mv in zip(vec, m)]
    return [sum(ci * wi for ci, wi in zip(c, comp)) / sc for comp in comps]


def load_chunk_vectors(chunk_ids):
    """vector.db shadow 테이블에서 특정 chunk 들의 임베딩을 읽는다.
    (유사도 행렬 뷰용 — K≤30 개라 순수 파이썬으로 충분)"""
    import sqlite3
    import struct
    db = CFG["db"] if CFG["db"].endswith(".db") else os.path.join(CFG["db"], "vector.db")
    if not os.path.exists(db):
        return {}
    con = sqlite3.connect(f"file:{db}?mode=ro", uri=True)
    try:
        ph = ",".join("?" * len(chunk_ids))
        rows = con.execute(
            f"SELECT id, chunk_id, chunk_offset FROM chunk_vec_rowids WHERE id IN ({ph})",
            list(chunk_ids)).fetchall()
        out = {}
        blocks = {}
        for cid, blk, off in rows:
            if blk not in blocks:
                r = con.execute(
                    "SELECT vectors FROM chunk_vec_vector_chunks00 WHERE rowid=?", (blk,)).fetchone()
                blocks[blk] = r[0] if r else None
            raw = blocks[blk]
            if raw is None:
                continue
            start = off * 4096  # 1024 dims × 4B
            seg = raw[start:start + 4096]
            if len(seg) == 4096:
                out[cid] = struct.unpack("<1024f", seg)
        return out
    finally:
        con.close()


def sim_matrix(hits):
    """히트 간 pairwise 코사인 (unit-norm 이므로 내적). hits 순서 정렬 유지."""
    ids = [h.get("chunk_id") for h in hits]
    vecs = load_chunk_vectors([i for i in ids if i])
    n = len(ids)
    m = [[None] * n for _ in range(n)]
    for a in range(n):
        va = vecs.get(ids[a])
        if va is None:
            continue
        m[a][a] = 1.0
        for b in range(a + 1, n):
            vb = vecs.get(ids[b])
            if vb is None:
                continue
            s = round(sum(x * y for x, y in zip(va, vb)), 4)
            m[a][b] = m[b][a] = s
    return m


def run_ckv(q, k, lang):
    cmd = shlex.split(CFG["ckv_bin"]) + [
        "--embedder", "ollama", "--model-name", CFG["model"], "--no-footprint",
        "query", q, "--out", CFG["db"], "--json", "-k", str(k),
    ]
    if lang:
        cmd += ["--lang", lang]
    try:
        p = subprocess.run(cmd, capture_output=True, text=True, timeout=120,
                           cwd=CFG["repo"] or None)
    except subprocess.TimeoutExpired:
        return {"error": "ckv query timeout (120s)"}
    except FileNotFoundError:
        return {"error": f"ckv 바이너리 없음: {cmd[0]} — make build 또는 --ckv-bin 지정"}
    if p.returncode != 0:
        return {"error": f"ckv exited {p.returncode}", "stderr": p.stderr[-2000:]}
    try:
        return json.loads(p.stdout)
    except json.JSONDecodeError:
        return {"error": "ckv output not JSON", "stderr": p.stderr[-800:]}


class H(BaseHTTPRequestHandler):
    def _send(self, code, body, ctype="application/json"):
        b = body if isinstance(body, bytes) else body.encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(b)))
        self.end_headers()
        self.wfile.write(b)

    def do_GET(self):
        u = urllib.parse.urlparse(self.path)
        if u.path in ("/", "/index.html"):
            with open(os.path.join(HERE, "index.html"), "rb") as f:
                return self._send(200, f.read(), "text/html; charset=utf-8")
        if u.path.startswith("/data/"):
            p = os.path.normpath(os.path.join(HERE, u.path.lstrip("/")))
            if p.startswith(DATA) and os.path.exists(p):
                with open(p, "rb") as f:
                    return self._send(200, f.read(), "application/json")
            return self._send(404, json.dumps({"error": "no data — run export_projection.py first"}))
        if u.path == "/config":
            return self._send(200, json.dumps({
                "db": CFG["db"], "model": CFG["model"], "projection": PROJ["loaded"],
                "dim": PROJ.get("dim"), "evr": PROJ.get("evr"),
                "axes": PROJ.get("axes")}))
        if u.path == "/query":
            qs = urllib.parse.parse_qs(u.query)
            q = (qs.get("q") or [""])[0].strip()
            k = int((qs.get("k") or ["20"])[0])
            lang = (qs.get("lang") or [""])[0].strip()
            if not q:
                return self._send(400, json.dumps({"error": "empty query"}))
            res = run_ckv(q, k, lang)
            if "error" not in res:
                res["query_xyz"] = project(embed_query(q))
                try:
                    res["sim"] = sim_matrix(res.get("hits") or [])
                except Exception:
                    res["sim"] = None  # 행렬 실패는 검색 자체를 막지 않는다
            return self._send(200, json.dumps(res))
        return self._send(404, json.dumps({"error": "not found"}))

    def log_message(self, *a):
        pass


def main():
    ap = argparse.ArgumentParser()
    # ckv CLI 자체의 --out 기본값(./ckv-data)과 동일한 규약. 머신 종속
    # 절대경로를 기본값으로 두지 않는다 — $CKV_DB 또는 --db 로 지정.
    ap.add_argument("--db", default=os.environ.get("CKV_DB", "./ckv-data"),
                    help="ckv 데이터 디렉토리 ($CKV_DB, 기본 ./ckv-data)")
    ap.add_argument("--ckv-bin", default=os.environ.get("CKV_BIN", "ckv"),
                    help='ckv 실행 커맨드 ($CKV_BIN, 기본 PATH 의 ckv; 예: "go run ./cmd/ckv")')
    ap.add_argument("--repo", default=os.path.normpath(os.path.join(HERE, "..", "..")),
                    help='--ckv-bin 이 "go run ..." 일 때의 cwd (기본: 이 리포 루트)')
    ap.add_argument("--model-name", dest="model", default="bge-m3")
    ap.add_argument("--port", type=int, default=8098)
    args = ap.parse_args()
    CFG.update(db=args.db, ckv_bin=args.ckv_bin, repo=args.repo, model=args.model)
    load_projection()

    print(f"ckv Atlas  →  http://localhost:{args.port}")
    print(f"  db={args.db}\n  ckv-bin={args.ckv_bin} (model={args.model})")
    print(f"  projection={'OK' if PROJ['loaded'] else '없음 — export_projection.py 먼저 실행'}")
    print("  전제: ollama serve + ollama pull bge-m3\n")
    ThreadingHTTPServer(("127.0.0.1", args.port), H).serve_forever()


if __name__ == "__main__":
    main()
