# Plan S1 — CKV (Code Knowledge Vector)

> **문서 버전**: 0.1 (초안)
> **작성일**: 2026-05-08
> **연관 문서**: `featurelist.md`, `use-cases.md`,
> `/Users/wm-it-22-00661/Work/github/study/ai/01.study/projects/stablenet-ai-agent/claudedocs/EXECUTION-GUIDE.md` §3.2 S1
> **목적**: EXECUTION-GUIDE의 vertical slice S1 실행 계획을 CKV repo의 모듈 분해(featurelist) 및 UC(use-cases)와 정합시켜 한 단위(stage S1)로 출시 가능한 산출물을 정의한다.

---

## 1. Mission (EXECUTION-GUIDE §3.2 S1 verbatim)

### 산출물
- **CKV repo**: embedding 모델 선택, vector store 선택, Go 라이브러리 export.
- **CKS repo**: `go.mod`에서 `code-knowledge-graph`·`code-knowledge-vector` import. BM25 fusion + MCP 서버(`cks-mcp` 바이너리).
- MCP capability (최소): `query_code(intent, query) → EvidencePack` (CKV → CKG 두 단계 검색 결과 융합).

### Acceptance (전부 충족 필요)
1. Claude Code에서 `claude mcp add cks --command ./bin/cks-mcp` 등록 후 `query_code` 호출 정상.
2. 1개 known query에 대해 정확한 `file:line` 반환 (citation accuracy 100%).
3. CKV→CKG hybrid가 BM25-only 대비 의미적 검색 정확도 향상 시연.

### S0와의 경계
- **S0 (CKG 마무리)** = Go-only graph indexing, `find_callers`/`impact_of_change`, HTTP 8080 loopback. CKG repo에서 완료.
- **S1**은 S0를 전제로 한다: CKV는 CKG의 graph.db를 read-only join으로 사용, citation 형식을 맞춘다.
- **multi-language** (TS/Sol) graph 노드는 S0 시점에 CKG에 이미 들어와 있어야 한다 (`commit ed0359f`로 .ckgignore matcher 수정 + ts/sol parser 활성). S1은 그 노드들을 vector index의 입력으로 사용한다.

---

## 2. CKV repo milestones 매핑

`featurelist.md`의 M0~M7과 S1의 관계:

| Milestone | featurelist 산출물 | S1 포함? | 근거 |
|---|---|---|---|
| **M0 — Skeleton** | `cmd/ckv`, Make, `bin/ckv --help` | ✅ S1 필수 | binary 없이 mcp 등록 불가 |
| **M1 — Indexer α** | tree-sitter + chunking + 더미 임베딩 | ✅ S1 필수 | UC-V1 |
| **M2 — Vector Store** | embedded backend + 영속화 | ✅ S1 필수 | persistent index |
| **M3 — Query α** | `semantic_search` + citation | ✅ S1 필수 | acceptance #2 |
| **M4 — Incremental** | UC-V2/V11 | ⚠️ **S1.5 승격** (사용자 결정 2026-05-19) | 첫 출시는 full rebuild 허용. `ckv reindex` 는 S1.5 마일스톤 entry condition — retrieval-quality-roadmap.md §7.5 의존 |
| **M5 — MCP / Working Memory** | MCP 서버, `query_code` | ✅ S1 필수 | acceptance #1 |
| **M6 — Sanitize + Hybrid hooks** | UC-V13/V8 | 🔸 부분 | hybrid hook(rank·score 노출)만 S1, sanitize는 S2 (S1 LLM caller는 신뢰됨) |
| **M7 — Eval & Report** | UC-V12, KPI | ❌ S2+ | S4 (regression scoring harness)에서 다룸 |

**S1 = M0 + M1 + M2 + M3 + M5(MCP transport+query_code) + M6 부분(hybrid hook).** 나머지는 S2+ 위임.

---

## 3. Embedding model — decision matrix

### 후보 비교

| 모델 | 라이선스 | 차원 | 컨텍스트 | 특화 | 1M 토큰 비용 | 로컬 가능 | 코드 retrieval 적합성 |
|---|---|---|---|---|---|---|---|
| **BGE-M3** (BAAI) | MIT | 1024 | 8192 | multilingual, dense+sparse+colbert | 0 (local) | ✅ ONNX/GGUF | 높음 (bge-code 변형 존재) |
| **BGE-small-en-v1.5** (BAAI) | MIT | 384 | 512 | 영문 | 0 (local) | ✅ | 중 (코드는 영문 변수명 의존) |
| **bge-code-v1** | MIT | 1024 | 8192 | 코드 특화 | 0 (local) | ✅ | **최상** |
| **OpenAI text-embedding-3-small** | proprietary | 1536 | 8191 | 범용 | $0.02 | ❌ API | 높음 |
| **OpenAI text-embedding-3-large** | proprietary | 3072 | 8191 | 범용 | $0.13 | ❌ API | 매우 높음 |
| **Codestral-embed** (Mistral) | proprietary | 1024 | 32k | 코드 특화 | API 비용 | ❌ API | **최상** |
| **Voyage code-2** | proprietary | 1536 | 16k | 코드 특화 | $0.10 | ❌ API | 높음 |
| **nomic-embed-code** | Apache 2.0 | 768 | 8192 | 코드 특화 | 0 (local) | ✅ | 높음 (실측 필요) |

### 평가 차원

- **EXECUTION-GUIDE 권고**: "로컬 권고 (재현성 + 비용)". S1은 이를 따른다.
- **S0 acceptance "결정성"**: 동일 입력 → 동일 출력. API 모델은 server-side 모델 drift 위험 (text-embedding-3-small이 3개월마다 silent retraining된 사례 보고). 로컬은 `embedding_model` + checksum 메타 저장으로 결정성 보장.
- **Privacy**: stablenet 같은 기업 코드는 외부 API 전송 회피가 default.
- **Throughput**: M-series Mac에서 ONNX BGE-M3 ≈ 200 chunks/s (실측 필요). 1M LOC ≈ 200K chunks → 약 17분 — featurelist UC-V1 success criteria(< 10분)와 충돌. **mitigation**: GPU 가속 ON 시 30s 내 완료, 또는 chunk 수 축소 (function-only).

### 권장 (S1 default) — **결정됨 2026-05-18**

**default = `bge-large-en-v1.5`** (BERT, 1024d, CLS pooling). 이전 권고였던 `bge-code-v1`(Qwen2 1.5B, 1024d, last-token pooling)은 D1 PoC 단계 (2026-05-18) 에서 어댑터 정합성·다운로드 크기(5.8GB) 사유로 pivot.

| 항목 | 채택 | 사유 |
|---|---|---|
| default | **bge-large-en-v1.5** (~2.5GB, ONNX in-repo) | BERT/CLS 어댑터 기존 scaffold와 정합, ONNX export 사전 포함 |
| fallback | `BGE-M3` (multilingual 필요 시) | 한국어 주석 등 |
| 향후 (D2) | `bge-code-v1` Qwen2 adapter | code retrieval 정확도 잠재 우위, D1-FU-6 open |
| 인터페이스 | featurelist §2.1 `Embedder` | 추후 교체 가능 |

```go
type Embedder interface {
    Name() string                                          // "bge-large-en-v1.5"
    Dimension() int                                        // 1024
    MaxInputTokens() int                                   // 512 (bge-large) — bge-code-v1로 교체 시 8192
    Embed(ctx context.Context, batch []string) ([][]float32, error)
}
```

### 메타 키 (featurelist §1.6)
- `embedding_model = "bge-large-en-v1.5"`
- `embedding_dim = 1024`
- `embedding_checksum = "<onnx file sha256>"` (모델 파일 무결성)
- `embedding_normalize = "l2"` (cosine similarity 사용)

모델 변경 감지 (featurelist §2.5): 메타와 현재 설정 mismatch → `IndexUnavailable` MCP 에러 + `ckv reindex` 권유.

### 모델 다운로드
- `~/.cache/ckv/models/bge-code-v1.onnx` + `.tokenizer.json`
- 첫 실행 시 download or 사용자가 `ckv model fetch` 호출 (airgap 환경 대비).
- checksum verification 필수 (featurelist §2.2).

---

## 4. Vector store — decision matrix

### 후보 비교

| 백엔드 | 의존성 | persistence | ANN 알고리즘 | hybrid 지원 | scale ceiling | setup |
|---|---|---|---|---|---|---|
| **chromem-go** | pure Go, in-process | DB file | brute-force / HNSW (옵션) | partial | ~100K vectors | trivial |
| **sqlite-vec** | C ext, SQLite | SQLite file | brute force + IVF (실험) | SQL JOIN | ~1M vectors | medium (CGO) |
| **LanceDB** | Rust ext | columnar Lance file | IVF-PQ | partial | 100M+ | medium |
| **Qdrant** | external server | server volume | HNSW | full | 1B+ | heavy |
| **pgvector** | Postgres ext | Postgres | IVF / HNSW | SQL JOIN | 100M+ | heavy (Postgres ops) |

### EXECUTION-GUIDE 권고
"in-process (chromem-go 또는 sqlite-vec). 규모 커지면 외부로 마이그레이션."

### CKG와의 정합
- CKG는 SQLite (`internal/persist/sqlite.go`) 사용, schema 1.7로 진화 중.
- **`sqlite-vec`이 자연스러운 선택** — CKV가 별도 SQLite DB를 가지고 CKG의 SQLite와 file system 상에서 같은 디렉토리에 위치, ID join은 application 레이어.
  - 옵션 1: **분리 DB** (default) — CKV `vector.db` + CKG `graph.db` 공존, manifest로 동기화 메타 공유.
  - 옵션 2: **공유 DB** — CKV가 CKG의 `graph.db`에 vector 테이블 추가 (ATTACH DATABASE 또는 schema 확장).
  - 추천 = 옵션 1 (분리). 이유: build/index 라이프사이클이 다름 (CKG는 graph build, CKV는 embedding build). 같은 file에 두면 둘 중 하나만 rebuild할 때 lock 충돌.

### 권장 (S1 default)

| 항목 | 선택 | 근거 |
|---|---|---|
| 1차 백엔드 | **`sqlite-vec`** | CKG와 idiom 일관, ATTACH로 join 가능, embedded |
| 미래 백엔드 | LanceDB → Qdrant | 1M+ chunk 또는 멀티테넌트 시 |
| 인터페이스 | `VectorStore` (featurelist §3.1) | 백엔드 교체 가능 |

```go
type VectorStore interface {
    Upsert(ctx context.Context, chunks []Chunk) error
    DeleteByFile(ctx context.Context, path string) error
    Search(ctx context.Context, query []float32, k int, filter Filter) ([]Hit, error)
    Stats(ctx context.Context) (Stats, error)
    Close() error
}
```

### Schema (sqlite-vec)

```sql
-- 메타 테이블 (CKG의 manifest 패턴 참조)
CREATE TABLE manifest (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
-- key: indexed_head, embedding_model, embedding_dim, embedded_at, indexer_version

-- chunk 메타 (vector와 분리; sqlite-vec 한계 우회)
CREATE TABLE chunks (
  id            TEXT PRIMARY KEY,         -- sha256(file + start_line + end_line + content_hash)
  file          TEXT NOT NULL,
  start_line    INTEGER NOT NULL,
  end_line      INTEGER NOT NULL,
  language      TEXT NOT NULL,
  symbol_name   TEXT,                     -- e.g. "Server.handleEdges"
  symbol_kind   TEXT,                     -- e.g. "Method"
  commit_hash   TEXT NOT NULL,
  content_sha256 TEXT NOT NULL,
  ckg_node_id   TEXT,                     -- CKG nodes.id (when symbol-aligned)
  text          TEXT NOT NULL             -- chunk source for re-embedding / display
);
CREATE INDEX idx_chunks_file ON chunks(file);
CREATE INDEX idx_chunks_lang ON chunks(language);
CREATE INDEX idx_chunks_symbol ON chunks(symbol_name);
CREATE INDEX idx_chunks_ckg_node ON chunks(ckg_node_id);

-- vec 가상 테이블 (sqlite-vec)
CREATE VIRTUAL TABLE chunk_vec USING vec0(
  chunk_id TEXT PRIMARY KEY,
  embedding FLOAT[1024]    -- bge-code-v1 = 1024d
);

-- 인덱싱: chunks ↔ chunk_vec join via chunk_id
```

### Manifest 메타 호환
CKV manifest와 CKG manifest를 동일 키로 정렬:

| 키 | CKG | CKV | 의미 |
|---|---|---|---|
| `src_root` | ✅ | ✅ | 인덱싱 대상 경로 |
| `src_commit` | ✅ | ✅ | 인덱싱 시점 git HEAD |
| `current_commit` | ✅ | ✅ | 현재 git HEAD (staleness) |
| `schema_version` | 1.7 | 1.0 | 각 repo schema |
| `embedding_model` | — | "bge-code-v1" | CKV 전용 |
| `embedding_dim` | — | 1024 | CKV 전용 |
| `built_at` | ✅ | ✅ | RFC3339 |

CKS Orchestrator가 `src_commit`을 비교해 두 인덱스 동기화 여부 판단.

---

## 5. Indexing pipeline (M1 + chunking + metadata)

### 5.1 입력
- `--src <path>`: 코드 루트 (CKG와 동일 경로 권장).
- `--ckg <path>`: CKG graph.db 경로 (선택적; symbol-align metadata 활용 시).
- `--out <path>`: CKV 데이터 디렉토리 (`<out>/vector.db` + `<out>/manifest.json`).

### 5.2 파일 디스커버리 (featurelist §1.1)
- gitignore 존중.
- `.ckvignore` 추가 (CKG의 `.ckgignore`와 동일 syntax).
- `node_modules`, `.next`, `out`, `vendor` 등 디폴트 제외 (CKG `.ckgignore`와 정합).
- 심링크 / >1MB 바이너리 / 비텍스트 자동 스킵.

### 5.3 멀티언어 파서 (featurelist §1.2)
- tree-sitter grammar wrapper: `github.com/smacker/go-tree-sitter` (CKG와 동일 의존성).
- S1 언어 우선순위: **Go, TS/TSX, Solidity** (CKG가 이미 지원하는 언어와 1:1 정합).
  - JS, Bash, Python: S2 위임.
- 함수/메서드/타입/contract 노드의 line range 추출.

### 5.4 Chunking (featurelist §1.3)

#### 1차 단위 (symbol-level)
- 각 함수/메서드/타입/contract는 1개 chunk.
- chunk text = 함수 본문 (signature 포함). Embedder의 max input(8192 tokens) 초과 시 분할.

#### 2차 분할 (long-function split)
- 함수 본문 > 1500 토큰 → AST top-level statement 단위로 분할.
- 각 sub-chunk의 metadata는 `:chunk:<n>` suffix가 붙는다 (`<sha>:foo:1`, `<sha>:foo:2`).

#### 3차 fallback (file-level)
- 모듈 import 절 / 전역 const 등 함수 외부 코드: 파일 첫 50줄을 하나의 chunk로 묶음.
- chunk_kind = `file_header`로 표시.

#### chunk_id 결정성
```
chunk_id = sha256(
    file_path + "\n" +
    start_line + ":" + end_line + "\n" +
    content_sha256
)
```
- 동일 commit, 동일 content → 동일 chunk_id (재인덱싱 시 안정).
- 파일 이동 (rename) → file_path 변경 → chunk_id 변경 (의도된 동작; 메타로 별도 추적).

### 5.5 CKG symbol alignment (featurelist §10.2 + S1 신규)
- `--ckg <path>` 지정 시:
  1. CKG의 `nodes` 테이블에서 type ∈ {Function, Method, Type, Struct, Interface, Contract}만 SELECT.
  2. `(file_path, start_line, end_line)`로 CKV chunk와 1:1 매칭.
  3. 매칭 성공 시 chunk.ckg_node_id = CKG 노드 id.
  4. 매칭 실패 (CKG에 없는 chunk; 예: file_header) → ckg_node_id = NULL.
- 통계: 매칭률 ≥ 90% 기대 (코드 chunk만 셀 때).

### 5.6 임베딩 (M1+M2)
- 배치 크기: 32 (CPU) / 256 (GPU). 자동 결정.
- 토크나이저 + truncation: 8192 토큰 초과 시 앞부분 truncate (signature가 head이라 의미 보존).
- 멱등성: 동일 chunk_id 재호출 시 cache hit.

### 5.7 영속화 (featurelist §3.5)
- `vector.db.tmp` → atomic rename (POSIX `rename(2)` 보장).
- `manifest.json` 함께 atomic write.
- 부팅 시 manifest 검증, 손상 시 안전 모드 (full rebuild 권유).

### 5.8 CLI (featurelist §11)
```bash
ckv build --src=. --ckg=/path/to/ckg-data --out=./ckv-data
ckv reindex --since=<commit>     # M4, S2에서 강화
ckv query "intent text"          # M3
ckv mcp                          # M5
ckv freshness                    # M5
```

---

## 6. Query engine (M3)

### 6.1 Semantic search 흐름
1. 입력: `{intent, k, filter, budget_tokens}`.
2. Embedder.Embed(intent) → query vector.
3. VectorStore.Search(query, k', filter) → top-k' chunk hits (k' = k * 3 for re-rank head room).
4. Citation enforcement: 모든 hit의 file/line 검증 (existence check).
5. Snippet density 조정: token budget 안에서 chunk text를 full body / signature+5lines / signature only 중 선택.
6. 응답: `[{chunk_id, citation: {file, start_line, end_line, commit_hash}, snippet, score, lang, symbol, ckg_node_id?}]`.

### 6.2 Score 정규화
- raw distance = cosine distance (`1 - cosine_similarity`, range [0, 2]).
- score = `1 - (distance / 2)` → range [0, 1], 높을수록 유사.
- response metadata에 raw distance + model_id 함께 노출 (RRF 입력에 사용).

### 6.3 Filter (featurelist §3.4)
- `lang`: "go" | "typescript" | "solidity"
- `path`: glob (e.g., `cmd/**/*.go`)
- `symbol_kind`: "Function" | "Method" | ...
- `commit_hash`: 특정 historical commit의 chunk만 (incremental snapshot 용도)

### 6.4 Threshold drop
- `score < 0.4`인 결과는 자동 drop (configurable).
- 모든 결과 drop 시 응답: `{hits: [], warnings: ["all_results_below_threshold"]}`.

---

## 7. CKG ↔ CKV 통합 (`cks-mcp` 통합 binary) — **CKS 책임 (정보용)**

> **방향 정정 (2026-05-12)**: 본 절의 통합 작업은 **CKS repo** (`tools/code-knowledge-system`)에서 수행한다. CKV는 CKG를 import하지 않으며, `cks-mcp` 바이너리도 CKV에서 빌드하지 않는다. CKV는 read-only MCP 표면(`pkg/mcp`)을 *노출* 하고, CKS가 이를 import해서 CKG MCP와 multiplex한다.

### 7.1 Module dependency (목표 구조)
```
github.com/0xmhha/code-knowledge-system   ← S1 이후 별도 repo
├── go.mod imports:
│   ├── github.com/0xmhha/code-knowledge-graph (CKG, read-only graph)
│   └── github.com/0xmhha/code-knowledge-vector (CKV, read-only vector)
└── cmd/cks-mcp/main.go:
    multiplexes CKG MCP + CKV MCP (pkg/mcp.Server.Underlying()) + RRF
```

CKV가 CKS에 노출하는 표면 (이미 W3-T8에서 완료):
- `pkg/mcp.NewServer(eng)` → `*Server`
- `Server.Underlying()` → `*server.MCPServer` (CKS가 자기 MCPServer에 직접 등록 가능)
- `query.Engine` (CKV의 vector retrieval API)

### 7.2 build target
```bash
# CKV repo
make build              # bin/ckv (standalone)
# CKV는 cks-mcp를 빌드하지 않는다. CKS repo의 Makefile이 책임.
```

### 7.3 query_code MCP capability

#### Input schema
```json
{
  "intent": "string (required)",
  "query": "string (optional, exact lexical hint)",
  "k": "int (default 10)",
  "filters": {
    "language": ["go", "typescript", "solidity"],
    "type": ["Function", "Method", "Hunk"]
  },
  "budget_tokens": "int (default 4000)"
}
```

#### Internal flow
1. **Vector search (CKV)**: Embedder.Embed(intent) → VectorStore.Search(...) → top-K' chunks.
2. **BM25 search (CKG)**: CKG의 `pkg/bm25/scorer.go` 호출. CKG nodes의 (qname + signature + doc_comment) corpus에 대해 score.
3. **RRF fusion** (Reciprocal Rank Fusion):
   ```
   rrf_score(item) = Σ over backends: 1 / (60 + rank_in_backend(item))
   ```
   - rank 60은 표준 RRF default (CIKM 2009).
   - vector backend의 chunk_id를 ckg_node_id로 매핑한 후 dedup.
4. **EvidencePack assembly**:
   - 각 top-k 항목에 대해 CKG 추가 fetch:
     - `/api/edges?seeds=<node_id>` 1-hop 호출 관계.
     - (H3 단계) `/api/hunks/by-node?qname=<q>` recent hunks.
   - bundle to JSON.

#### Output schema (EvidencePack v1)
```json
{
  "hits": [
    {
      "ckg_node_id": "string | null",
      "chunk_id": "string",
      "citation": {
        "file": "string",
        "start_line": "int",
        "end_line": "int",
        "commit_hash": "string"
      },
      "snippet": "string (token-budget-trimmed)",
      "score": {
        "rrf": 0.0,
        "vector_rank": 1,
        "vector_distance": 0.34,
        "bm25_rank": 3,
        "bm25_score": 8.71
      },
      "language": "string",
      "symbol": "string (qname)",
      "kind": "Function | Method | Type | Hunk",
      "neighbours": [
        {"node_id": "...", "edge": "calls", "qname": "..."}
      ]
    }
  ],
  "metadata": {
    "tokens_used": 0,
    "warnings": [],
    "indexed_head_ckv": "<sha>",
    "indexed_head_ckg": "<sha>",
    "fresh": true
  }
}
```

### 7.4 Citation enforcement (featurelist §5)
- 모든 hit에 citation 강제. 누락 시 drop + warning.
- citation 실재성 cheap check: file 존재 + commit_hash 매칭.
- `citation accuracy = 1.0` 100% 보장 — acceptance #2.

### 7.5 BM25 fusion 위치
- BM25 자체 구현은 CKG의 `pkg/bm25/scorer.go`에 이미 있다 (Okapi K1=1.5, B=0.75).
- CKV는 BM25를 호출만 하고 자체 구현 안 함 (single source of truth).
- RRF 자체는 cks-mcp의 fusion 레이어 (CKV repo의 `internal/fusion/rrf.go` 신설).

---

## 8. MCP transport & registration

### 8.1 Transport
- stdio (Claude Code default).
- `ckv mcp` binary (and the CKS-side `cks-mcp`)는 `os.Stdin` / `os.Stdout`으로 JSON-RPC 처리.
- 추가: `--http :8080` 옵션 (개발/디버깅 모드, S2에서 정식 지원).

### 8.2 Two-MCP Architecture (방향 정정 — 2026-05-12)

> Coding agent / 상위 캘러는 **두 종류의 MCP 표면**을 봐야 한다 (사용자 명시):
> 1. **Read-only MCP**: 바이브 요청 사항을 분석해 의미를 추론. 코드/그래프 인덱스에서 *읽기*만.
> 2. **Read-write MCP** *(working memory / footprint)*: 작업 요청·내부 처리·응답 결과를 *축적*하여 knowledge 처리를 점점 똑똑하게.
>
> 두 표면은 동일 바이너리 안에서 namespace 분리, **또는** 별도 바이너리로 분리 가능. 분리하면 read-write에 더 strict한 정책(예: append-only, mTLS, run-id whitelist)을 적용하기 쉽다.

CKV가 노출하는 read-only tool (W3-T8 완료):
```
cks.context.semantic_search       # CKV vector → ranked hits + citation (READ-ONLY)
cks.ops.get_freshness             # indexed_head vs git HEAD          (READ-ONLY)
cks.ops.health                    # index identity probe              (READ-ONLY)
```

CKV가 노출할 예정인 read-write tool (W3-T14 footprint + future working memory):
```
cks.memory.log_interaction        # 작업 요청·응답·메타 적재          (READ-WRITE)
cks.memory.remember_fact          # (planned) UC-V9 명시 writeback     (READ-WRITE)
cks.memory.record_decision        # (planned) UC-V9                    (READ-WRITE)
cks.memory.recall_session         # (planned) UC-V14                   (READ-ONLY on RW store)
```

CKS가 추가로 multiplex할 tool (CKS의 책임, CKV에는 포함되지 않음):
```
cks.context.query_code            # CKV + CKG hybrid (RRF)             (READ-ONLY)
cks.context.find_symbol           # CKG 단독                          (READ-ONLY)
cks.context.find_callers          # CKG 단독                          (READ-ONLY)
cks.context.impact_of_change      # CKG 단독                          (READ-ONLY)
```

### 8.3 등록
```bash
# CKV 단독 (vector-layer만 시연; acceptance #1 부분 충족)
claude mcp add ckv --command "$(pwd)/bin/ckv mcp --out=$(pwd)/ckv-data"

# CKS 통합 (CKS repo가 빌드; acceptance #1 완전 충족)
cd /path/to/cks && make build
claude mcp add cks --command "$(pwd)/bin/cks-mcp"
```

acceptance #1을 위해 위 흐름이 빈 manifest 없이도 동작해야 한다. CKV 데이터 디렉토리는 `--out` 플래그 또는 환경변수 `CKV_DATA_DIR`로 전달.

### 8.4 Auth
- S1 = loopback (127.0.0.1) only. mTLS는 S6에서 도입 (EXECUTION-GUIDE §5.3 보안 검증).
- caller cert SAN 검증 등은 S1 범위 외.
- Read-write MCP는 read-only보다 더 strict한 정책 대상 (write 권한은 별도 capability — coding agent에 한정).

---

## 9. Acceptance criteria — concrete tests

### Acceptance #1: MCP 등록 + query_code 호출 정상

**Test**:
```bash
# given
make build-cks
ckv build --src=/path/to/go-stablenet --ckg=$CKG_DATA --out=$CKV_DATA
export CKS_DATA_DIR=$CKV_DATA

# when
claude mcp add cks --command "$PWD/bin/cks-mcp"
# in claude code session:
# > query_code intent="connection pool initialization"

# then
# - response is valid JSON matching EvidencePack v1 schema
# - hits.length > 0
# - 첫 번째 hit의 citation.file가 실제 존재
# - response time < 5s warm
```

### Acceptance #2: 1개 known query → file:line 정확도 100%

**Test**: ground-truth 1개 known query
```yaml
test_id: t1
intent: "TCP socket bind on port"
expected_file_pattern: "*/listen.go"
expected_function: "Listen"
```
- query_code 호출 → top-3에 expected가 포함되어야 함.
- citation.file이 expected_file_pattern과 매칭.
- citation.start_line ≤ expected function start ≤ citation.end_line.

5개 known queries 추가 시 모두 통과 시 acceptance #2 충족.

### Acceptance #3: hybrid > BM25-only

**Test**: 10개 known queries
1. CKV-only 모드: VectorStore.Search 단독.
2. CKG-only 모드: CKG의 bm25_search 단독.
3. hybrid 모드: query_code (RRF fusion).

지표:
- recall@5 (top-5 안에 정답 포함률)
- MRR (Mean Reciprocal Rank)

성공 조건: hybrid의 recall@5 > max(CKV-only, CKG-only) — 즉 hybrid가 두 단독보다 정확.

### Determinism check (S0 acceptance 연장)
- 동일 query 2회 실행 → 동일 top-K (within tied-score tolerance ε=0.001).
- 인덱스 변경 없을 때 보장.

---

## 10. Open decisions

featurelist §21의 q1~q5 + 추가:

### q1 (featurelist §21): Solidity event/modifier chunk_kind 분리
- **권고**: 분리. event는 ABI 검색에 별도 의미. modifier는 함수 wrapping이라 별도.
- **확정 시점**: M1.

### q2: 임베딩 모델 — 코드 특화 vs 범용 — **결정됨 2026-05-18**
- **확정**: `bge-large-en-v1.5` (BERT, 1024d, CLS pooling, ~2.5GB). 어댑터 정합성 + ONNX in-repo + 다운로드 크기 우위로 D1 PoC pivot.
- **이전 권고였던 `bge-code-v1`(Qwen2)** 은 D1-FU-6 (open, D2 scope) 으로 이관. recall@5=1.0 / MRR=0.77 (N=10) baseline 측정 완료.
- **fallback**: BGE-M3 (multilingual 필요 시 — 한국어 주석 등).

### q3: sqlite-vec ANN 성능
- **검증 필요**: 1M chunk 환경에서 latency p95 측정.
- **fallback plan**: LanceDB (1M+ chunk 시).
- **확정 시점**: 첫 1M LOC 인덱스 후.

### q4: MCP 라이브러리 — CKG와 일치 vs 별도
- **권고**: 일치. CKG가 `mark3labs/mcp-go` 사용 중이면 CKV도 동일.
- **이유**: cks-mcp 통합 binary에서 두 server를 multiplex할 때 동일 라이브러리가 필수.
- **확정 시점**: M5 시작 시.

### q5: Working memory 다중 프로세스 동시성
- **권고**: SQLite WAL + file lock. CKV+CKG가 같은 run_id에 write 시 application 레이어 mutex.
- **확정 시점**: M5+M7.

### S1 신규 결정 포인트

- **S1-d1**: vector.db / graph.db 분리 vs 공유. **권고 = 분리** (라이프사이클 분리).
- **S1-d2**: chunk text의 임베딩 입력에 doc_comment 포함 여부. **권고 = 포함** (intent 캡처에 도움).
- **S1-d3**: cks-mcp가 CKV 인덱스 부재 시 fallback (CKG-only)? **권고 = 명확한 에러** (`IndexUnavailable`). 사용자가 `ckv build` 호출하도록.
- **S1-d4**: query_code 응답에 추가로 1-hop graph 포함할지. **권고 = 포함** (LLM에게 즉시 활용 가능 데이터).

---

## 11. Risks & mitigations

| 리스크 | 영향 | 완화 |
|---|---|---|
| 임베딩 모델 첫 로드 시간 (~5s) | MCP 호출 지연 | lazy-load + warm at MCP startup. health check로 ready 노출 |
| 모델 파일 missing (~2GB) | 첫 실행 fail | `ckv model fetch` 별도 명령 + 진행률 표시 |
| sqlite-vec ANN 정확도 (brute force) | 1M+ chunk 시 p95 ↑ | LanceDB 마이그레이션 path |
| CKV/CKG indexed_head mismatch | citation stale | manifest 양쪽 비교 + warning |
| MCP JSON-RPC payload 크기 | EvidencePack 50KB+ 시 transport 부담 | budget_tokens 기본 4000으로 강제 truncate |
| Privacy: 회사 코드 외부 API 전송 | data leak | local default + 명시적 opt-in 시만 외부 |
| 결정성: GPU vs CPU 다른 결과 | acceptance 실패 | embedding precision = float32 고정 + checksum 메타 비교 |

---

## 12. Rollout (W1~W4)

### W1 — Skeleton + scaffolding (M0)
- `cmd/ckv/main.go` Cobra CLI shell.
- Makefile (build/test/lint/tidy/fmt).
- `go.mod` — **CKG는 import하지 않는다** (정정 2026-05-12). CKS가 양쪽을 import.
- README quickstart.
- Tests: `bin/ckv --help` 출력 검증.

### W2 — Indexer + embedding (M1+M2)  ✅ 완료
- `internal/parse/<lang>/` go walker (TS/Sol은 W3-T9/T10).
- `internal/chunk/` chunking 로직.
- `internal/embed/` Embedder 인터페이스 + mock + bgeonnx 스텁.
- `internal/store/sqlitevec/` VectorStore 구현.
- `ckv build` 명령 동작.
- Tests: 작은 sample repo → chunk 수, manifest 검증.

### W3 — MCP + query (M3+M5, CKV 단독) — **정정**
- `pkg/mcp/server.go` — `cks.context.*`, `cks.ops.*` (read-only). ✅
- `ckv query`, `ckv mcp`, `ckv freshness` 동작. ✅
- TS / Solidity tree-sitter parser (W3-T9, W3-T10) — *진행 예정*
- **Footprint logging** (W3-T14, neu): slog + JSONL sink, 모든 build/query/mcp 경로에 latency·hit count·citation drop 계측 — *진행 예정*
- **Skill extension hook** (W3-T15, neu): `<src>/.claude/` 또는 `ckv.yaml`로 per-project chunking/필터 커스터마이즈 — *진행 예정*
- **삭제**: `cmd/cks-mcp` / `internal/fusion/rrf.go` — CKS의 책임으로 이관.
- Tests: known query → expected file:line, footprint event schema 검증.

### W4 — Eval & acceptance (M7 부분, CKV 단독) — **정정**
- 5개 known query test fixture (`testdata/queries.yaml`) — W4-T1
- `ckv eval` runner — W4-T2: recall@k, MRR, citation accuracy
- *(opt-in)* cli-wrapper LLM-as-judge — W4-T3: harness/cli-wrapper로 headless Claude Code 호출
- **삭제**: "Hybrid > BM25-only 시연" — CKS에서 수행 (acceptance #3).
- CKV 단독으로 가능한 acceptance: #2 (citation accuracy 100%) — formal demo.
- README의 acceptance 섹션 갱신.
- S1 (CKV 측) done. CKS 작업과 병행 가능.

각 주차 끝에 demo + acceptance check. 실패 항목은 follow-up task로 분해 (EXECUTION-GUIDE §5.2 원칙).

---

## 13. Out of scope (S1)

다음은 S1 명시적 제외, S2 이후:
- Sanitize default-deny (UC-V13) — S2 (외부 caller 도입 시).
- Working Memory writeback (UC-V9 명시 호출) — S2.
- HTTP API — MCP만 S1.
- Cross-language semantic discovery 정밀도 (UC-V7) — S2 (multi-lang corpus 더 풍부 후).
- Bootstrap report (UC-V12) — S4 eval harness.
- Hunk graph 통합 (CKG H1~H4의 Hunk 노드 임베딩) — CKG H1 land 후 S2 또는 S3.
- mTLS auth — S6.
- Observability (Prometheus exporter) — S2.
- **`cks-mcp` 통합 binary, RRF fusion, `cks.context.query_code`** — **CKS repo의 책임** (`tools/code-knowledge-system`). CKV는 `pkg/mcp.Server.Underlying()`로 표면만 노출.
- **Hybrid (CKV+CKG) acceptance** (#3) — CKS에서 수행.

---

## 14. Dependencies on CKG (S0 산출물)

S1 진입 전 CKG 측에서 완료되어야 할 것:

| 항목 | 상태 |
|---|---|
| schema 1.6+ (manifest, FTS5, blobs) | ✅ 완료 |
| Go indexing 정상 | ✅ 완료 |
| `pkg/bm25/scorer.go` Okapi 구현 | ✅ 완료 |
| `pkg/types/node.go` Node 구조 | ✅ 완료 (id 16-char, qname, file_path, signature) |
| `pkg/store/store.go` Reader 공개 API | ✅ 완료 (StoreReader 인터페이스) |
| `internal/mcp/server.go` (mark3labs/mcp-go) | ✅ 완료 |
| `find_symbol`, `find_callers`, `impact_of_change` MCP 노출 | ✅ 완료 |
| TS / Solidity 파서 활성 (ed0359f) | ✅ 완료 |
| 8080 loopback bind | ⚠️ 현재 8787, S0 spec 정합 검토 필요 (별도 trivia) |

S0의 8080 vs 현재 8787 정합 — 별도 작은 fix.

---

## 15. 참조 매트릭스 — UC ↔ feature ↔ S1 단계

| UC | featurelist § | S1 단계 | acceptance |
|---|---|---|---|
| UC-V1 Bootstrap | §1, §2, §3, §11 | W1+W2 | acceptance #1 (build success) |
| UC-V3 Semantic Search | §4 | W3 | acceptance #2 (file:line 100%) |
| UC-V5 Evidence Pack | §4, §10 | W3 | acceptance #1 (response schema) |
| UC-V6 MCP Exposure | §8, §11 | W3 | acceptance #1 (claude mcp add) |
| UC-V8 Hybrid | §10, §4 | W4 | acceptance #3 (hybrid > BM25-only) |
| UC-V10 Citation | §5 | W3 | acceptance #2 (100% accuracy) |
| UC-V15 Local-First | §2, §15 | W2 | airgap test |
| UC-V2 Incremental | §6 | **(S1.5)** | retrieval-quality-roadmap.md §7.5 / §12 #8 — multi-granularity 도입 전 architectural 전제 |
| UC-V13 Sanitize | §9 | (S2) | — |

---

## 16. 변경 이력

| 일자 | 버전 | 변경 |
|---|---|---|
| 2026-05-08 | 0.1 | 초안 작성 (CKG b60a50f 기준, EXECUTION-GUIDE 2026-05-06 기준) |
| 2026-05-12 | 0.2 | **방향 정정**: CKV는 CKG를 import하지 않음. `cks-mcp` 빌드 + RRF fusion + `query_code` tool은 별도 CKS repo의 책임. CKV는 `pkg/mcp.Server.Underlying()`로 read-only 표면만 노출. **Two-MCP 아키텍처** 추가 (read-only context / read-write memory). §7·§8·§12·§13 정정, W3-T9/T10 (TS/Sol parser) + W3-T14 (footprint) + W3-T15 (skill hook) + W4-T1/T2/T3 (eval + cli-wrapper judge) 명시. |
| 2026-05-19 | 0.3 | **§3 + §10 q2 정정**: 기본 임베딩 모델을 `bge-code-v1`에서 **`bge-large-en-v1.5`**로 변경 (D1 PoC pivot 2026-05-18). 메타 키 (embedding_model, MaxInputTokens) 갱신. `bge-code-v1` Qwen2 어댑터는 D1-FU-6 (D2 scope) 으로 이관 표기. q2는 "미해결"에서 "결정됨"으로. |
