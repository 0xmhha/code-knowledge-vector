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


def kmeans_cosine(X, k, iters=30, seed=42):
    """구면 k-means (X 는 unit-norm) — 1024차원 '실제 의미 공간'에서 군집화.

    화면 3D 좌표가 아니라 원본 임베딩으로 군집을 계산한다: PCA 3D 는
    ~11% 정보의 근사라 3D 에서 묶으면 가짜 군집이 생길 수 있다. 여기서
    계산한 군집이 화면에서 색으로 뭉쳐 보인다면 그것이 '진짜'(고차원)
    구조가 투영에서도 살아남았다는 증거가 된다.
    """
    rng = np.random.default_rng(seed)
    # k-means++ 풍 초기화 (코사인 거리 기반 farthest sampling)
    idx = [int(rng.integers(len(X)))]
    d2 = None
    for _ in range(k - 1):
        dist = 1.0 - X @ X[idx[-1]]
        d2 = dist ** 2 if d2 is None else np.minimum(d2, dist ** 2)
        p = d2 / d2.sum()
        idx.append(int(rng.choice(len(X), p=p)))
    C = X[idx].copy()
    assign = np.zeros(len(X), dtype=np.int32)
    for _ in range(iters):
        assign = (X @ C.T).argmax(1)
        newC = C.copy()
        for j in range(k):
            m = X[assign == j]
            if len(m):
                v = m.mean(0)
                n = np.linalg.norm(v) or 1.0
                newC[j] = v / n
        if np.allclose(newC, C, atol=1e-5):
            C = newC
            break
        C = newC
    return (X @ C.T).argmax(1)


def axis_poles(xyz, metas, axis, frac=0.08):
    """PC 축의 경험적 의미: 축 양끝(상·하위 frac)에 몰린 코드의 최빈
    디렉토리(2단)를 요약한다. PCA 축은 태생적 이름이 없지만, '이 축의
    +쪽엔 solidity 계약, −쪽엔 tracers 가 몰린다'는 식의 데이터 기반
    해석은 가능하다 (요인분석의 loading 해석과 동일한 접근)."""
    from collections import Counter
    order = np.argsort(xyz[:, axis])
    k = max(50, int(len(order) * frac))

    def summ(idx):
        dirs = Counter("/".join(metas[i][2].split("/")[:2]) for i in idx if metas[i][2])
        return ", ".join(d for d, _ in dirs.most_common(2)) or "?"

    return {"neg": summ(order[:k]), "pos": summ(order[-k:])}


def cluster_labels(assign, k, metas):
    """군집별 자동 라벨: 최빈 디렉토리(2단) + 최빈 category."""
    from collections import Counter
    out = []
    for j in range(k):
        files = [m[2] for i, m in enumerate(metas) if assign[i] == j and m[2]]
        cats = [m[3] for i, m in enumerate(metas) if assign[i] == j and m[3]]
        dirs = Counter("/".join(f.split("/")[:2]) for f in files)
        top = dirs.most_common(1)[0][0] if dirs else "?"
        cat = Counter(cats).most_common(1)
        out.append(top + (f" · {cat[0][0]}" if cat else ""))
    return out


def main():
    ap = argparse.ArgumentParser()
    # serve.py 와 동일 규약: $CKV_DB 또는 --db (기본 ./ckv-data — ckv CLI
    # 의 --out 기본값). 머신 종속 절대경로를 기본값으로 두지 않는다.
    ap.add_argument("--db", default=os.environ.get("CKV_DB", "./ckv-data"),
                    help="ckv 데이터 디렉토리 또는 vector.db 경로 ($CKV_DB, 기본 ./ckv-data)")
    ap.add_argument("--out", default=os.path.join(HERE, "data"))
    ap.add_argument("--clusters", type=int, default=16,
                    help="k-means 군집 수 (0=비활성; 기본 16)")
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

    # 2.5) 군집화 — 원본 1024차원(코사인)에서. meta 에 cluster id 부착.
    clabels = []
    if args.clusters > 0:
        assign = kmeans_cosine(vecs.astype(np.float64), args.clusters)
        clabels = cluster_labels(assign, args.clusters, meta)
        for i, m in enumerate(meta):
            m.append(int(assign[i]))
        sizes = np.bincount(assign, minlength=args.clusters)
        print(f"[+] k-means k={args.clusters} — 군집 크기: {sizes.tolist()}")
    else:
        for m in meta:
            m.append(-1)

    # 3) 저장 — points.json (좌표+메타), projection.json (질의 투영용 행렬)
    pts_path = os.path.join(args.out, "points.json")
    with open(pts_path, "w", encoding="utf-8") as f:
        json.dump({
            "n": n,
            "xyz": [round(float(v), 4) for v in xyz.reshape(-1)],
            "meta": meta,
            "columns": ["chunk_id", "label", "file", "category", "language", "kind", "lines", "is_test", "cluster"],
            "cluster_labels": clabels,
        }, f, ensure_ascii=False)
    axes = [axis_poles(xyz, meta, a) for a in range(3)]
    for a, ax in enumerate(axes):
        print(f"[+] PC{a+1} 극단 요약: (−) {ax['neg']}  ↔  (+) {ax['pos']}")

    proj_path = os.path.join(args.out, "projection.json")
    with open(proj_path, "w", encoding="utf-8") as f:
        json.dump({
            "dim": DIM,
            "mean": [float(v) for v in mean],
            "components": [[float(v) for v in c] for c in comps],
            "scale": scale,
            "explained_variance_ratio": [float(v) for v in var],
            "axes": axes,
            "src_db": db_path,
        }, f, ensure_ascii=False)
    print(f"[+] {pts_path} ({os.path.getsize(pts_path)//1024} KB)")
    print(f"[+] {proj_path} ({os.path.getsize(proj_path)//1024} KB)")
    print("\n다음: python3 serve.py  →  http://localhost:8098")


if __name__ == "__main__":
    main()
