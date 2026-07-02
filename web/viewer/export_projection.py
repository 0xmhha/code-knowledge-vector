#!/usr/bin/env python3
"""
export_projection.py — ckv vector.db 의 임베딩(1024d)을 PCA 로 3D 투영해
viewer 정적 데이터(data/points.json, data/projection.json)를 생성한다.

원리
----
ckv 의 vector.db 는 sqlite-vec vec0 가상 테이블을 쓰며, 벡터 실체는 shadow
테이블에 raw float32(LE) 로 저장된다 (확장 로드 없이 순수 파싱 가능):

  chunk_vec_vector_chunks00(rowid, vectors BLOB)      # 블록당 1024개 × 1024d × 4B
  chunk_vec_rowids(rowid, id, chunk_id, chunk_offset) # id(=chunks.id) → 블록·오프셋
  chunks(id, file, symbol_name, ...)                  # 메타데이터

전 벡터에 PCA(top-3) 를 적용해 3D 좌표를 만들고, **투영 행렬(mean + 3
components + scale)** 도 함께 저장한다 — serve.py 가 질의 임베딩을 같은
행렬로 투영해 "질의가 공간 어디에 떨어졌는지"를 표시할 수 있게 하기 위함.

사용법
------
  python3 export_projection.py                 # 기본 db → data/
  python3 export_projection.py --db /path/to/ckv-data-dir
"""

import argparse
import json
import os
import sqlite3
import sys

import numpy as np

HERE = os.path.dirname(os.path.abspath(__file__))
DIM = 1024
VEC_BYTES = DIM * 4


def clean(s):
    if s is None:
        return ""
    return str(s).replace("\t", " ").replace("\n", " ").strip()


def main():
    ap = argparse.ArgumentParser()
    # serve.py 와 동일 규약: $CKV_DB 또는 --db (기본 ./ckv-data — ckv CLI
    # 의 --out 기본값). 머신 종속 절대경로를 기본값으로 두지 않는다.
    ap.add_argument("--db", default=os.environ.get("CKV_DB", "./ckv-data"),
                    help="ckv 데이터 디렉토리 또는 vector.db 경로 ($CKV_DB, 기본 ./ckv-data)")
    ap.add_argument("--out", default=os.path.join(HERE, "data"))
    args = ap.parse_args()

    db_path = args.db if args.db.endswith(".db") else os.path.join(args.db, "vector.db")
    if not os.path.exists(db_path):
        sys.exit(f"[!] vector.db 없음: {db_path}")
    os.makedirs(args.out, exist_ok=True)

    con = sqlite3.connect(f"file:{db_path}?mode=ro", uri=True)
    con.row_factory = sqlite3.Row

    # 1) 벡터 블록 → 하나의 (N, 1024) float32 행렬
    blocks = {r["rowid"]: r["vectors"] for r in
              con.execute("SELECT rowid, vectors FROM chunk_vec_vector_chunks00")}
    rows = con.execute("""
        SELECT r.chunk_id AS block, r.chunk_offset AS off, r.id AS id,
               c.symbol_name, c.symbol_kind, c.chunk_kind, c.file,
               c.language, c.category, c.is_test, c.start_line, c.end_line
        FROM chunk_vec_rowids r JOIN chunks c ON c.id = r.id
        ORDER BY r.rowid
    """).fetchall()
    con.close()

    vecs = np.empty((len(rows), DIM), dtype=np.float32)
    meta = []
    n = 0
    for r in rows:
        blk = blocks.get(r["block"])
        if blk is None:
            continue
        start = r["off"] * VEC_BYTES
        raw = blk[start:start + VEC_BYTES]
        if len(raw) != VEC_BYTES:
            continue
        vecs[n] = np.frombuffer(raw, dtype="<f4")
        base = os.path.basename(r["file"] or "")
        meta.append([
            r["id"],                                   # 0 chunk_id (검색 히트 매핑 키)
            clean(r["symbol_name"]) or base,           # 1 label
            clean(r["file"]),                          # 2 file
            clean(r["category"]),                      # 3 category
            clean(r["language"]),                      # 4 language
            clean(r["symbol_kind"] or r["chunk_kind"]),# 5 kind
            f'{r["start_line"]}-{r["end_line"]}',      # 6 lines
            int(r["is_test"] or 0),                    # 7 is_test
        ])
        n += 1
    vecs = vecs[:n]
    print(f"[+] 벡터 로드: {n} × {DIM}")

    # 2) PCA top-3 (경제형 SVD; bge-m3 는 unit-norm 이라 표준화 불필요)
    # float64 로 계산 — macOS Accelerate BLAS 가 float32 matmul 에서
    # 스퓨리어스 overflow 경고를 내는 케이스 회피 (결과는 동일).
    mean = vecs.mean(axis=0, dtype=np.float64)
    centered = (vecs.astype(np.float64)) - mean
    # 16K×1024 SVD 는 수 초 내외.
    _, s, vt = np.linalg.svd(centered, full_matrices=False)
    comps = vt[:3]                       # (3, 1024)
    xyz = centered @ comps.T             # (N, 3)
    # 표시용 정규화: 세 축 공통 스케일(형태 보존) → 대략 [-1, 1]
    scale = float(np.abs(xyz).max()) or 1.0
    xyz = (xyz / scale).astype(np.float32)
    var = (s[:3] ** 2) / float((s ** 2).sum())
    print(f"[+] PCA 완료 — 설명 분산: {var[0]:.3f} / {var[1]:.3f} / {var[2]:.3f} (합 {var.sum():.3f})")

    # 3) 저장 — points.json (좌표+메타), projection.json (질의 투영용 행렬)
    pts_path = os.path.join(args.out, "points.json")
    with open(pts_path, "w", encoding="utf-8") as f:
        json.dump({
            "n": n,
            "xyz": [round(float(v), 4) for v in xyz.reshape(-1)],
            "meta": meta,
            "columns": ["chunk_id", "label", "file", "category", "language", "kind", "lines", "is_test"],
        }, f, ensure_ascii=False)
    proj_path = os.path.join(args.out, "projection.json")
    with open(proj_path, "w", encoding="utf-8") as f:
        json.dump({
            "dim": DIM,
            "mean": [float(v) for v in mean],
            "components": [[float(v) for v in c] for c in comps],
            "scale": scale,
            "explained_variance_ratio": [float(v) for v in var],
            "src_db": db_path,
        }, f)
    print(f"[+] {pts_path} ({os.path.getsize(pts_path)//1024} KB)")
    print(f"[+] {proj_path} ({os.path.getsize(proj_path)//1024} KB)")
    print("\n다음: python3 serve.py  →  http://localhost:8098")


if __name__ == "__main__":
    main()
