# Code Knowledge Vector (CKV) — Feature List

> **문서 버전**: 1.0
> **작성일**: 2026-05-05
> **연관 문서**: [`use-cases.md`](./use-cases.md), `04-cks-deep-dive.md`
> **목적**: 본 프로젝트가 CKS Vector 계층의 책임을 완수하기 위해 구현해야 할 기능을 모듈/컴포넌트 단위로 분해.

---

## 0. 우선순위 / 단계 표기

- **P0**: MVP 필수 (use-cases.md UC-V1/V2/V3/V5/V6/V8/V10/V13/V15 충족에 직접 필요)
- **P1**: MVP 직후 (UC-V4/V7/V9/V11)
- **P2**: 통합/고도화 (UC-V12/V14, observability, eval harness)

| 단계 | 목표 | 대표 산출물 |
|---|---|---|
| **M0 — Skeleton** | Go 프로젝트 골격, Make 빌드, CLI shell | `cmd/ckv`, `Makefile`, `bin/ckv --help` |
| **M1 — Indexer α** | tree-sitter 파싱 + chunking + 더미 임베딩 | `ckv build` (in-memory) |
| **M2 — Vector Store** | embedded vector backend + 영속화 | `ckv build` → on-disk DB |
| **M3 — Query α** | semantic_search + citation | `ckv query "..."` |
| **M4 — Incremental + Freshness** | UC-V2/V11 | fsnotify, git diff 기반 reindex |
| **M5 — MCP / Working Memory** | MCP 서버, run_id, remember/recall | `ckv mcp` |
| **M6 — Sanitize + Hybrid hooks** | UC-V13/V8 (CKG 와 인용 포맷 정합) | sanitize, score 정규화 |
| **M7 — Eval & Report** | UC-V12, KPI 측정 | `ckv eval`, `ckv bootstrap --report` |

---

## 0.1 구현 상태 마스터 테이블 (2026-05-19)

> **목적**: 본 문서의 P0/P1/P2 항목 대비 S1 진행 시점의 실제 구현 상태를 한 화면에 집약. 본문 sub-section의 stale claim은 본 표가 SoT. 코드 inventory 기준.
>
> **범례**: ✅ 구현 완료 · ⚠️ 부분 구현 · ❌-S2 S2 이관 결정 · ❌-CKS CKS 책임으로 이관 · ❌-제거 영구 제거 · ❌-planned 예정 (이관 미결정)

| 섹션 | 항목 | P? | 상태 | 실 구현 위치 / 비고 |
|---|---|---|---|---|
| §1.1 | 파일 디스커버리 (gitignore + .ckvignore + 메타) | P0 | ✅ | `internal/discover` |
| §1.2 | Go / TypeScript / Solidity parser | P0 | ✅ | `internal/parse/{golang,typescript,solidity}` |
| §1.2 | JavaScript parser | P0 | ✅ | `internal/parse/javascript/` (commit `e4977fa`, 2026-05-21). tree-sitter-typescript binding delegation; `.js` / `.jsx` / `.mjs` / `.cjs` 인덱싱. S2 → S1 끌어옴 (TS parser 패턴 재사용 비용 작음). |
| §1.2 | Bash parser | P0 | ❌-S2 | 사용자 결정 2026-05-19 (S2 이관) |
| §1.3 | Chunking — symbol + file_header | P0 | ✅ | `internal/chunk` |
| §1.3 | Chunking — 큰 함수 sliding window | P0 | ⚠️ | head-truncate만 (sliding split deferred to W3 enhancement) |
| §1.4 | Compiler/LSP hook | P1 | ❌-S3+ | 인터페이스 미정의 |
| §1.5 | `ckv build` CLI | P0 | ✅ | `cmd/ckv/build.go` |
| §1.6 | manifest 메타 저장 | P0 | ✅ | `internal/manifest` |
| §2.1 | Embedder 인터페이스 | P0 | ✅ | `pkg/types/Embedder` |
| §2.2 | 기본 로컬 모델 (**bge-large-en-v1.5**) | P0 | ✅ | D1 PoC pivot 2026-05-18 (이전: bge-code-v1) |
| §2.3 | 배치 임베딩 | P0 | ⚠️ | 단건 처리만, D1-FU-8 open (배치 + CoreML EP) |
| §2.4 | 임베딩 캐시 (per-text) | P1 | ❌-S2 | |
| §2.5 | 모델 버전 변경 감지 | P1 | ✅ | manifest mismatch → `IndexUnavailable` |
| §3.1 | VectorStore 인터페이스 | P0 | ✅ | |
| §3.2 | sqlite-vec 기본 백엔드 | P0 | ✅ | `internal/store/sqlitevec` |
| §3.4 | Filter (lang/path/symbol_kind) | P0 | ✅ | |
| §3.4 | Filter — commit_hash | P0 | ⚠️ | metadata만 저장, 필터링 미연결 |
| §3.5 | 영속화 (atomic rename + manifest) | P0 | ✅ | |
| §4.1 | Semantic search | P0 | ✅ | `internal/query/engine.go` |
| §4.2 | Code-as-Query mode (UC-V4) | P1 | ❌-S2 | |
| §4.3 | Snippet density 3-tier | P0 | ⚠️ | `budget_tokens`만 적용 (full/sig+5/sig ladder 미구현) |
| §4.4 | Score 정규화 (0~1) + raw distance | P0 | ✅ | `Hit.Score.Normalized` |
| §4.5 | Query plan (intent classification) | P1 | ❌-S2 | |
| §5.1 | 인용 강제 부착 | P0 | ✅ | |
| §5.2 | 인용 실재성 cheap check | P0 | ⚠️ | file existence만, commit_hash 매칭 미구현 |
| §5.3 | Citation test suite | P1 | ✅ | `internal/eval` citation accuracy |
| §6.1 | 변경 감지 (git diff) | P0 | ⚠️ | freshness check만, fsnotify 미구현 |
| §6.2 | `ckv reindex` (UC-V2) | P0 | ❌-S1.5 | **S1.5 승격** (사용자 결정 2026-05-19). Phase B (multi-granularity) 도입 전 architectural 전제 — retrieval-quality-roadmap.md §7.5 |
| §6.3 | `cks.ops.get_freshness` | P0 | ✅ | `internal/freshness` |
| §6.3 | `cks.ops.request_refresh` | P0 | ❌-S2 | |
| §6.4 | Stale 정책 (auto_refresh / warn_only / block) | P1 | ❌-S2 | |
| §7 | Working Memory (run/cache/writeback/recall) — 전체 | P0/P1 | ❌-planned | plan §8.2 read-write MCP planned, S2 |
| §8.1 | `cks.context.semantic_search` | P0 | ✅ | `pkg/mcp` |
| §8.1 | `cks.context.get_context_for_task` | P0 | ❌-S2 | sanitize 의존 |
| §8.1 | `cks.memory.*` (3종) | P0 | ❌-planned | |
| §8.1 | `cks.ops.health` (실측 추가) | — | ✅ | featurelist 누락 항목, 코드에 존재 |
| §8.2 | Envelope/Budget 검증 (trace_id/dry_run) | P0 | ⚠️ | `budget_tokens`만, trace_id/dry_run 미구현 |
| §8.3 | mTLS auth | P1 | ❌-S6 | plan §8.4 |
| §8.4 | Error model (FreshnessStale, BudgetExceeded, CitationNotFound, SanitizeFailed, IndexUnavailable, PolicyError) | P0 | ✅ | 6 종 모두 `internal/query/errors.go` 에 sentinel + `pkg/ckv` 재노출. Raise points: IndexUnavailable (Open), BudgetExceeded (Search), CitationNotFound (Search 카타스트로픽), FreshnessStale (Engine.CheckFreshness). SanitizeFailed / PolicyError 는 sentinel만 (S2 / S6 모듈 도착 시 raise). |
| §8.5 | Health (실측) | P1 | ✅ | `cks.ops.health` |
| §8.5 | `cks.ops.stats` | P1 | ❌-S2 | |
| §9 | Sanitize (5 sub-section) — 전체 | P0 | ❌-S2 | plan §13 명시 |
| §10.1 | 공통 citation 포맷 (CKG 정합) | P0 | ✅ | |
| §10.2 | Symbol id 호환 | P0 | ⚠️ | `ckg_node_id` 필드만, 정규화 규칙 미합의 |
| §10.3 | RRF 입력용 score 노출 | P0 | ✅ | rank+normalized score 노출 |
| §10.4 | 공유 Working Memory 스키마 | P1 | ❌-planned | |
| §10.5 | Single binary (CKV/CKG/CKS 통합) | P2 | ❌-CKS | plan §7 — CKS repo 책임 |
| §11.1 | `ckv build`, `ckv query`, `ckv mcp` | P0 | ✅ | |
| §11.1 | `ckv reindex` | P0 | ❌-S2 | |
| §11.1 | `ckv serve` (HTTP) | (옵션) | ❌-S2 | |
| §11.1 | `ckv freshness` (실측 추가) | — | ✅ | featurelist 누락 항목 |
| §11.1 | `ckv eval` (실측 추가) | — | ✅ | featurelist 누락 항목 |
| §11.1 | `ckv model fetch/list` | — | ⚠️ | stub만 (D1-FU-4 open, D2 scope) |
| §11.1 | `ckv bootstrap --report` (UC-V12) | P0 | ❌-S4 | plan M7 |
| §11.2 | 공통 플래그 (--json, --log-level, --profile) | P0 | ⚠️ | `--json` 일부만 |
| §11.3 | Configuration (`ckv.yaml`) | P0 | ✅ | `internal/projectcfg` (W3-T15) |
| §12 | HTTP API 전체 | P1 | ❌-S2 | |
| §13 | Bootstrap & Systemization Report | P0/P2 | ❌-S4 | |
| §14.1 | Structured logging (slog) | P0 | ✅ | |
| §14.1 | **Footprint logging** (실측 추가) | — | ✅ | `internal/footprint` (W3-T14), featurelist 누락 |
| §14.2 | Prometheus metrics | P1 | ❌-S2 | |
| §14.3 | OpenTelemetry tracing | P2 | ❌ | |
| §15.1 | Read-only source | P0 | ✅ | |
| §15.2 | Secret 회피 (.env/*.pem 패턴) | P0 | ✅ | `internal/discover.DefaultSecretPatterns` 25개 패턴 — `.env*` env variants / `*.pem` `*.key` `*.p12` `*.pfx` `*.keystore` / SSH keys (`id_rsa*` `id_ed25519*` `id_ecdsa*` `id_dsa*`) / `credentials.json` `service-account*.json` / `.npmrc` `.pypirc` `.netrc` / `.aws/credentials` `.aws/config`. opt-out: `CKV_DISABLE_SECRET_FILTER=1`. |
| §15.3 | Output audit (sanitize pass) | P0 | ❌-S2 | §9 의존 |
| §16.1 | 단위 테스트 | P0 | ✅ | 25개 test 파일 |
| §16.2 | 통합 테스트 | P0 | ✅ | `testdata/sample` |
| §16.3 | Eval harness | P1 | ✅ | `internal/eval` + `internal/judge` |
| §16.4 | Fuzz/property tests | P2 | ❌ | |
| §17.1 | Makefile (build/test/lint/fmt/tidy/clean) | P0 | ✅ | |
| §17.1 | `make eval` | P0 | ❌-제거 | 실측 부재 (cli `ckv eval`만 사용) |
| §17.1 | `make test-race`, `make audit` (실측 추가) | — | ✅ | featurelist 누락 |
| §17.2 | 멀티-OS 빌드 | P1 | ❌ | D1-FU-5 open |
| §17.3 | Release (`make release` + CI matrix) | P2 | ❌ | |
| §18.1 | README | P0 | ✅ | |
| §18.2 | ARCHITECTURE.md | P1 | ❌ | (`plan-S1-ckv.md` 일부 역할) |
| §18.3 | SCHEMA.md | P1 | ❌ | plan-S1-ckv.md에 분산 |
| §18.4 | CKS integration guide | P2 | ❌-CKS | CKS repo 책임 |

**S1 진행 요약**: P0 항목 중 ✅ = 35%, ⚠️ = 15%, ❌-S2 이관 결정 = 30%, ❌-planned/CKS = 20%. 본문 sub-section의 미구현 claim은 본 표의 "❌-..." 분류로 해석.

---

## 1. 인덱싱 파이프라인 (Indexer)

### 1.1 파일 디스커버리 (P0)
- `--src` 디렉터리 재귀 워킹 (gitignore 존중)
- `.ckvignore` 또는 CLI flag 로 추가 제외 패턴 지원
- 파일별 메타: 경로, 크기, mtime, content hash (sha256), 언어 분류
- 심볼릭 링크 / 매우 큰 파일 / 바이너리 자동 스킵

### 1.2 멀티언어 파서 (Tree-sitter Level 1) (P0)
- **S1 구현 (2026-05-19 기준)**: Go, TypeScript (`.ts`/`.tsx`), Solidity — `internal/parse/{golang,typescript,solidity}`
- **S2 이관 (사용자 결정 2026-05-19)**: JavaScript (`.js`/`.jsx`), Bash
- tree-sitter grammar wrapper (`go-tree-sitter`; Solidity는 vendored grammar)
- 파일별 AST 추출, 함수/메서드/타입/contract 노드 위치 식별
- 파서 결과 캐시 (key: `{commit_hash, file_path, content_hash}`) — 현재 미구현 (P1)

### 1.3 Chunking 전략 (P0)
- 1차 단위: function / method / type / contract 선언
- 큰 함수: sliding window (default 50 LOC, 10 LOC overlap, configurable)
- 작은 파일/주석 군집: file-level fallback chunk
- 각 chunk 메타: `chunk_id`, `file`, `start_line`, `end_line`, `lang`, `symbol_name`, `symbol_kind`, `commit_hash`
- chunk_id 결정성: `sha256(file + start_line + end_line + content_hash)` (재인덱싱 시 안정 식별)

### 1.4 보조 분석기 후크 (P1)
- Compiler / LSP 결과를 chunk metadata 에 부착할 수 있는 인터페이스 (gopls / typescript / solc) — Level 2 정보를 vector 검색 가중치에 활용 가능
- 초기 구현은 인터페이스만 정의, 통합은 후속

### 1.5 Bootstrap CLI (P0)
- `ckv build --src=<path> --out=<dir> [--lang go,solidity,...] [--config ckv.yaml]`
- 진행률 리포트 (파일 / chunk / 임베딩)
- 결과 요약: chunk 수, 언어 분포, 소요 시간, indexed_head

### 1.6 인덱싱 메타 저장 (P0)
- `indexed_head` (git rev-parse), `indexer_version`, `embedding_model`, `embedding_dim`, `built_at`
- 호환성 체크: 기존 메타와 새 설정 불일치 시 안전 모드 (전체 재빌드 권유)

---

## 2. 임베딩 (Embedding)

### 2.1 모델 추상화 인터페이스 (P0)
```go
type Embedder interface {
    Name() string
    Dimension() int
    Embed(ctx context.Context, batch []string) ([][]float32, error)
}
```
- 기본 구현체: 로컬 ONNX 또는 GGUF 모델 로더
- Plugin slot: 외부 API provider (OpenAI/Voyage)는 옵션 모듈로 분리

### 2.2 로컬-우선 기본 모델 (P0)
- 기본 권장: `BAAI/bge-small-en-v1.5` (다국어/코드 균형) 또는 코드 특화 (`bge-code`, `nomic-embed-code`) 중 빌드 시 선택
- 모델 다운로드 캐시 (`~/.cache/ckv/models/`) + checksum 검증
- airgap mode: 모델 미존재 시 명확한 에러 메시지 + 수동 배치 경로 안내

### 2.3 배치 임베딩 (P0)
- 자동 배치 크기 (CPU/GPU 메모리 기반)
- 토크나이저 + truncation 정책 (chunk 가 모델 max length 초과 시 추가 분할)
- 멱등성: 동일 텍스트 반복 호출 시 동일 vector

### 2.4 임베딩 캐시 (P1)
- key = `sha256(text)` + model_id, 디스크 캐시
- 동일 chunk 가 파일 이동/리네이밍 등으로 다시 만나면 재임베딩 회피

### 2.5 모델 버전 / 전체 재임베딩 (P1)
- 모델 변경 감지 → "전체 재인덱싱 필요" 플래그 + CLI confirm
- 백그라운드 dual-write 모드 (옵션, 마이그레이션 시)

---

## 3. Vector Store (저장소)

### 3.1 백엔드 추상화 (P0)
```go
type VectorStore interface {
    Upsert(ctx, []Chunk) error
    DeleteByFile(ctx, path string) error
    Search(ctx, query []float32, k int, filter Filter) ([]Hit, error)
    Stats(ctx) (Stats, error)
    Close() error
}
```

### 3.2 기본 구현체: embedded (P0)
- 1순위: **sqlite-vec** (가장 가볍고 의존성 적음, 단일 파일 DB)
- 2순위 옵션: **LanceDB** (빠른 ANN, 컬럼형) — pluggable

### 3.3 외부 백엔드 옵션 (P2)
- Qdrant 어댑터 (대규모 멀티테넌트 환경)
- 운영 환경 변경이 코드에 영향 없도록 인터페이스 격리

### 3.4 Filter / Metadata 인덱스 (P0)
- 필터 가능 필드: `lang`, `path` (glob), `symbol_kind`, `commit_hash`
- 메타데이터 컬럼은 별도 SQLite 테이블, vector 와 join

### 3.5 영속화 / 무결성 (P0)
- atomic write (`*.tmp` rename)
- checksum manifest (`manifest.json` — chunk 수, hash, indexed_head)
- 부팅 시 manifest 검증, 손상 시 안전 모드

---

## 4. Query Engine

### 4.1 Semantic Search (P0)
- 입력: `{query: string, k: int, filter: Filter, budget_tokens: int, lang_hint?: string}`
- 출력: `[{chunk_id, citation, snippet, score, lang, symbol}]`
- ANN top-K + post-filter
- threshold 옵션 (낮은 score 자동 drop)

### 4.2 Code-as-Query 모드 (P1)
- 입력이 코드 스니펫일 경우 식별 → 동일 언어 가중치 부여
- UC-V4 (pattern similarity) 지원

### 4.3 Snippet Density 조정 (P0)
- token budget 대응: full body → signature + 5 lines → signature only
- 항상 citation 은 budget 외(추가 hook)

### 4.4 Score 정규화 / 노출 (P0)
- 0–1 정규화 score
- raw distance / model_id 도 metadata 로 함께 노출 (RRF 입력에서 활용)

### 4.5 Query Plan (P1)
- 단순 intent classification (heuristic): `symbol_lookup` 후보면 BM25 위임 권장 메시지 + 그래도 vector 결과 제공
- 향후 Layer 3 가 take over 할 수 있는 인터페이스만 노출

---

## 5. Citation Enforcement

### 5.1 인용 부착 (P0)
- 모든 응답 chunk 에 `{file, start_line, end_line, commit_hash}` 강제
- 누락 시 결과 drop + warning

### 5.2 인용 실재성 검증 (P0)
- cheap check: file existence at `commit_hash`
- mismatch 시 stale 표시 (UC-V11 과 연동)

### 5.3 Citation Test Suite (P1)
- 테스트: 인덱스 직후 모든 chunk citation 의 100% 실재성 보증

---

## 6. Incremental Update / Freshness

### 6.1 변경 감지 (P0)
- `git diff --name-only ${indexed_head} HEAD`
- fsnotify watch (옵션, dev 모드)

### 6.2 재인덱싱 단위 (P0)
- 파일 단위 cascade delete → re-parse → re-embed → upsert
- batch 모드 (1000 파일 청크 처리)

### 6.3 Freshness API (P0)
- `cks.ops.get_freshness(scope?)` → `{indexed_head, current_head, changed_files, stale: bool, affects_scope: bool}`
- `cks.ops.request_refresh(scope)` → 즉시 부분 재인덱싱 후 ack

### 6.4 Stale 정책 (P1)
- 자동 백그라운드 풀 재인덱싱 시작 + 응답에 `warnings: ["partial_stale"]`
- 정책 토글: `auto_refresh`, `warn_only`, `block_on_stale`

---

## 7. Working Memory (Layer 2)

### 7.1 Run / Session 모델 (P0)
- `run_id` (UUID), `started_at`, `ticket_ref?`, `caller`
- 전체 entry 스토리지: SQLite (옵션: JSON file for dev)

### 7.2 Q&A Cache (자동 writeback) (P0)
- key = `sha256(intent + scope + budget + indexed_head)`
- TTL = run lifetime, 추가 invalidation = freshness change

### 7.3 명시적 Writeback Tools (P0)
- `cks.memory.remember_fact(subject, predicate, object, citation, confidence?)`
- `cks.memory.record_decision(context, chosen, alternatives_considered?, rationale?)`
- 모든 entry 는 citation 또는 `source_msg_id` 필수

### 7.4 Recall (P1)
- `cks.memory.recall_session(run_id)` — 이전 run 의 facts/decisions/touched_files 요약
- sanitize 후 반환

### 7.5 Cleanup / Archive (P2)
- 완료된 run 일정 기간 후 archive 디렉터리로 이동 (eval data)

---

## 8. MCP Server (Layer 4 의 vector 부분)

### 8.1 Tool Group 등록 (P0)
- `cks.context.semantic_search`
- `cks.context.get_context_for_task` (vector 부분만 반환, 통합 단계에서 Layer 3 가 fuse)
- `cks.memory.remember_fact`, `record_decision`, `recall_session`
- `cks.ops.get_freshness`, `request_refresh`

### 8.2 Envelope / Budget 검증 (P0)
- 요청 envelope: `{trace_id, run_id, caller, manifest_ref?, budget_tokens, budget_hops, dry_run, payload}`
- budget_tokens 초과 시 playbook 조기 종료 + warning

### 8.3 mTLS / 인증 (P1)
- mTLS optional (dev 모드는 plaintext 허용)
- caller cert SAN ↔ envelope `caller` 일치 검증

### 8.4 Error Model (P0)
6 종 모두 `internal/query/errors.go` sentinel + `pkg/ckv` 재노출 (impl 2026-05-21, B6):

| Sentinel | Raise point | 호출자 가이드 |
|---|---|---|
| `ErrIndexUnavailable` | `query.Open` — manifest 없음 / 모델 dim·name 불일치 | `ckv build` 재실행. 재시도 무의미. |
| `ErrFreshnessStale` | `Engine.CheckFreshness` — manifest.IndexedHead ≠ git HEAD | 결과 여전히 사용 가능. 편할 때 reindex 스케줄. |
| `ErrBudgetExceeded` | `Engine.Search` — `BudgetTokens > 0 && BudgetTokens < MinBudgetTokens(20)` | `BudgetTokens` 올리거나 `<0` 으로 비활성. |
| `ErrCitationNotFound` | `Engine.Search` — threshold 통과 후 citation 강제로 전수 drop | `ckv build --src <현재경로>` 로 src_root 재정렬. |
| `ErrSanitizeFailed` | (예약, S2 sanitize 모듈) | sanitize_report.reason 로깅, 동일 intent 재시도 금지. |
| `ErrPolicyError` | (예약, S6 mTLS / policy 게이트) | 하드 거부. 재시도 금지. 운영자 surface. |

- 호출자는 `errors.Is(err, ckv.ErrXxx)` 로 분기. wrapping safe (errors.Join / fmt.Errorf %w 모두 OK).

### 8.5 Health / Stats (P1)
- `/healthz` (HTTP), `cks.ops.stats` (chunk 수, last index time, embedding model)

---

## 9. Sanitize Evidence (default-deny)

### 9.1 Origin Tagging (P0)
- 코드 주석 / commit message / PR / Jira / Confluence / issue comment 별 sentinel
- XML tag + (옵션) markdown fence + language hint
- escape 규약 (XML escape, fence 충돌 시 4-tick 승격)

### 9.2 Rule Engine (P0)
- `policies/sanitization_rules.yaml#cks_evidence_pack` 로드
- baseline 6개 패턴: `pi-imperative-001`, `pi-system-002`, `pi-jailbreak-003`, `pi-tool-call-004`, `pi-credential-005`, `pi-base64-006`
- 매치 시 `[REDACTED:{rule_id}]` 치환

### 9.3 Audit Log (P0)
- 원본은 별도 audit log entry 로 보존
- `audit_ref` 응답 metadata 에 포함

### 9.4 Fail-Closed (P0)
- 룰 평가 panic / timeout / regex 오류 → origin 통째로 redact_full
- `sanitize_report.fail_closed_count` 카운터 + 운영 알림 hook

### 9.5 Hot-Reload + Signature (P2)
- ECDSA P-256 detached `.sig` 검증
- fsnotify watch + atomic swap
- 파일 부재 / 서명 오류 시 startup abort

---

## 10. CKG (graph) 와의 통합 인터페이스

### 10.1 공통 인용 포맷 (P0)
- `{file, start_line, end_line, commit_hash}` ↔ CKG node 의 `defined_in` 위치와 1:1 호환

### 10.2 Symbol ID 호환 (P0)
- CKG 의 symbol id (예: `pkg:func`) 를 chunk metadata 의 `symbol_name` / `symbol_kind` 와 join 가능하도록 정규화 규칙 합의

### 10.3 RRF 입력용 Score 노출 (P0)
- vector 결과의 ranked list 를 외부에 그대로 제공 (rank, score)
- 통합 단계에서 Layer 3 가 graph/bm25 결과와 RRF 합산

### 10.4 공유 Working Memory 스키마 (P1)
- run_id, fact entry, decision entry 의 JSON 스키마를 CKG 와 공동 정의 (이 레포 또는 별도 spec 레포)

### 10.5 통합 후 single binary 옵션 (P2)
- CKV/CKG 가 동일 데이터 디렉터리·동일 manifest 를 공유할 수 있도록 `out` 디렉터리 레이아웃 합의 (`<out>/vector/`, `<out>/graph/`)

---

## 11. CLI / 실행 표면

### 11.1 명령어 (P0)
| 명령 | 설명 | 상태 |
|---|---|---|
| `ckv build` | 전체 인덱스 구축 (UC-V1) | ✅ |
| `ckv query <text>` | semantic search (UC-V3) | ✅ |
| `ckv mcp` | MCP 서버 실행 (UC-V6) | ✅ |
| `ckv freshness` | 인덱스 신선도 출력 (UC-V11) | ✅ |
| `ckv eval` | KPI 측정 (recall@k/MRR/citation accuracy, optional LLM judge) | ✅ |
| `ckv model fetch <name>` | 모델 다운로드 + sha256 검증 | ⚠️ stub (D1-FU-4 open, D2 scope) |
| `ckv model list` | 캐시된 모델 list | ⚠️ stub |
| `ckv footprint` | footprint event log 조회 (W3-T14) | ✅ |
| `ckv reindex` | 변경분만 재인덱싱 (UC-V2) | ❌-S1.5 (사용자 결정 2026-05-19) |
| `ckv serve` | HTTP API 서버 | ❌-S2 |
| `ckv bootstrap --report` | systemization 리포트 (UC-V12) | ❌-S4 |

### 11.2 공통 플래그 (P0)
- `--graph=<dir>` (기존 ckg 와 일관)
- `--config=<path>`
- `--json` (machine-readable output)
- `--log-level`, `--profile`

### 11.3 Configuration (P0)
- YAML config (`ckv.yaml`): 언어 목록, chunk size, 임베딩 모델, vector backend, sanitize 정책 경로
- 환경 변수 override

---

## 12. HTTP API (옵션, P1)

### 12.1 엔드포인트
| Method | Path | 대응 MCP tool |
|---|---|---|
| POST | `/v1/semantic_search` | cks.context.semantic_search |
| POST | `/v1/get_context_for_task` | cks.context.get_context_for_task |
| GET | `/v1/freshness` | cks.ops.get_freshness |
| POST | `/v1/refresh` | cks.ops.request_refresh |
| POST | `/v1/memory/fact` | cks.memory.remember_fact |
| POST | `/v1/memory/decision` | cks.memory.record_decision |
| GET | `/v1/memory/session/{run_id}` | cks.memory.recall_session |
| GET | `/healthz` | (health) |

### 12.2 OpenAPI 스펙 (P2)
- 자동 생성 + CI 검증

---

## 13. Bootstrap & Systemization Report

### 13.1 5-Phase 파이프라인 — vector 책임 (P0/P2)
- A. Detection & Parsing (P0)
- D. Index Materialization (P0)
- E. Systemization Report (P2)
- B/C 단계는 CKG 책임이지만, vector 측 systemizer 가 의미 클러스터를 추가 생성하여 통합 리포트에 기여

### 13.2 의미 클러스터링 (P2)
- 임베딩 위에서 HDBSCAN 또는 mini-batch k-means
- 클러스터별 대표 chunk(centroid 근처) + 자동 라벨 (top-k tokens / LLM-free TF-IDF)

### 13.3 Onboarding 데이터 (P2)
- 자주 등장하는 의미 영역 ranking → UC-B4 (CKS Onboarding Walkthrough)에 입력

---

## 14. Observability

### 14.1 Logging (P0)
- 구조화 로그 (zerolog/slog)
- run_id, trace_id 포함

### 14.2 Metrics (P1)
- Prometheus exporter
- 지표:
  - `ckv_embed_latency_seconds`
  - `ckv_search_latency_seconds`
  - `ckv_index_freshness_lag_seconds`
  - `ckv_chunk_count`
  - `ckv_cache_hit_ratio`
  - `ckv_sanitize_redacted_total{rule_id}`
  - `ckv_sanitize_fail_closed_total`

### 14.3 Tracing (P2)
- OpenTelemetry 호환

---

## 15. 보안 / 격리

### 15.1 Read-only Source (P0)
- CKV 는 `--src` 경로를 read-only 로만 사용
- write 는 `--out` 데이터 디렉터리에 한정

### 15.2 Secret 회피 (P0)
- `internal/discover.DefaultSecretPatterns` 25개 사전 정의 패턴이 indexing 입구에서 secret 파일 차단 (impl 2026-05-21, commit `b1ad8aa`)
- 적용 순서: `DefaultIgnore` → `.ckvignore` → `Options.Extra` → `DefaultSecretPatterns` (마지막 적용으로 user override 영향 없음)
- Opt-out (테스트 전용): `CKV_DISABLE_SECRET_FILTER=1`
- `.env.example` / `.env.sample` 같은 합법적 템플릿은 명시 환경 suffix만 차단하여 false-positive 회피
- 한계: pattern-based만 (content-based heuristic 미적용 — generic `config.json` 안의 키는 통과). 후속: B10 fuzz/property test 영역

### 15.3 Output Audit (P0)
- 모든 외부 노출 응답은 sanitize pass 통과 (UC-V13)
- internal-only 도구 (`bm25_search`, `graph_query`)는 vector 측에 없음 (CKG 영역)

---

## 16. 테스트 / 평가

### 16.1 단위 테스트 (P0)
- 파서 (각 언어별 함수/타입 추출 정확성)
- chunking (sliding window 경계 정확성)
- embedder mock (인터페이스 계약)
- sanitize (6개 baseline 패턴 100% 매치)

### 16.2 통합 테스트 (P0)
- 작은 sample repo 인덱싱 → query → citation 검증 end-to-end
- incremental update: 파일 수정 후 변경 chunk 만 갱신 확인

### 16.3 Eval Harness (P1)
- ground truth 데이터셋: (자연어 질의 ↔ 정답 file:line)
- relevance@K, MRR, latency, citation accuracy 측정
- vector-only / hybrid (CKG 합산) 비교 모드

### 16.4 Fuzz / Property tests (P2)
- 임의 코드 입력으로 파서 패닉 부재 확인

---

## 17. 빌드 & 배포 (Make 기반)

### 17.1 Makefile 타겟 (P0)
| Target | 동작 | 상태 |
|---|---|---|
| `make build` | `go build -o bin/ckv ./cmd/ckv` | ✅ |
| `make test` | `go test ./...` | ✅ |
| `make test-race` | race detector + coverage | ✅ (featurelist v1.0에 누락이었음) |
| `make lint` | `go vet` (golangci-lint optional) | ✅ |
| `make fmt` | `gofmt -s -w .` + `goimports` | ✅ |
| `make tidy` | `go mod tidy` | ✅ |
| `make audit` | `govulncheck` (call-graph reachable vulns) | ✅ (featurelist v1.0에 누락이었음) |
| `make clean` | build artifact 제거 | ✅ |
| ~~`make eval`~~ | (제거됨 — `bin/ckv eval`을 직접 호출) | ❌-제거 |

### 17.2 멀티-OS 빌드 (P1)
- linux/amd64, linux/arm64, darwin/arm64

### 17.3 Release (P2)
- `make release` → tar.gz + checksum
- GitHub Actions CI: lint + test + build matrix

---

## 18. 문서화

### 18.1 README (P0)
- Quick start (build → query → mcp)
- 지원 언어, 기본 모델, 백엔드

### 18.2 ARCHITECTURE.md (P1)
- 4-Layer 중 본 프로젝트 위치, 모듈 도식

### 18.3 SCHEMA.md (P1)
- chunk metadata 스키마, working memory entry 스키마, sanitize_report 스키마

### 18.4 CKS Integration Guide (P2)
- CKG 와 통합 시 디렉터리 레이아웃, 인용 포맷 호환성, RRF 입력 예시

---

## 19. 의존성 / 기술 선택

| 영역 | 1차 선택 | 비고 |
|---|---|---|
| 언어 | Go (1.22+) | CKG 와 일관 |
| Tree-sitter 바인딩 | `github.com/smacker/go-tree-sitter` 또는 자체 ffi wrapper | grammar 부족 시 직접 빌드 |
| Vector store (default) | sqlite-vec | embedded, 단일 파일 |
| Vector store (option) | LanceDB / Qdrant | 외부 어댑터 |
| ONNX runtime | `onnxruntime-go` 또는 `gorgonia/onnx-go` | 로컬 임베딩 |
| Tokenizer | `huggingface/tokenizers` (CGO) 또는 순수 Go 포팅 | 모델별 |
| MCP | `mcp-go` 또는 자체 구현 (CKG 와 동일 채택) | |
| 로깅 | `log/slog` (표준 라이브러리) | |
| 설정 | YAML (`gopkg.in/yaml.v3`) | |

> 외부 라이브러리 채택 전 라이선스 / 보안 / 유지관리 빈도 검토 필수 (PRINCIPLES.md, security.md).

---

## 20. UC ↔ Feature 매핑 매트릭스

| UC | 핵심 Feature 영역 |
|---|---|
| UC-V1 Bootstrap | §1 (Indexer) + §2 (Embed) + §3 (Store) + §11 (CLI) + §1.5 |
| UC-V2 Incremental | §1 + §6.1–6.3 |
| UC-V3 Semantic Search | §4.1 + §5 (Citation) + §2.1 |
| UC-V4 Pattern Similarity | §4.2 + §4.1 |
| UC-V5 Evidence Pack | §4 + §10.3 + §9 |
| UC-V6 MCP Exposure | §8 + §11.1 (`ckv mcp`) |
| UC-V7 Cross-Language | §1.2 + §2.2 (모델 다국어성) + §4.4 |
| UC-V8 Hybrid w/ Graph | §10 (전체) + §4.4 |
| UC-V9 Working Memory | §7 + §8.1 |
| UC-V10 Citation Enforcement | §5 (전체) |
| UC-V11 Freshness | §6.3 + §6.4 + §8.5 (health) |
| UC-V12 Systemization Report | §13 + §14.2 |
| UC-V13 Sanitize | §9 (전체) + §15.3 |
| UC-V14 Resume | §7.4 + §10.4 |
| UC-V15 Local-First | §2.2 + §15.2 |

---

## 21. 결정 사항 + 미해결

### 21.1 결정 완료

| ID | 항목 | 결정 | 결정일 | 근거 |
|---|---|---|---|---|
| q1 | Solidity event/modifier chunk_kind | 별도 분리 (event/modifier 독립) | W3-T10 | plan §10 q1 |
| **q2** | 기본 임베딩 모델 | **bge-large-en-v1.5** (1024d BERT, CLS pooling) | 2026-05-18 | D1 PoC pivot — bge-code-v1(Qwen2 5.8GB) 가설 검증 후 BERT 어댑터 정합성 우위로 전환. |
| q4 | MCP 라이브러리 | `mark3labs/mcp-go` (CKG와 일치) | M5 | plan §10 q4 |
| **JS/Bash parser** | `*.js`/`*.jsx`/Bash 인덱싱 | **S2 이관** (S1 범위 밖) | 2026-05-19 | review-direction §6.1 사용자 결정 |
| **BM25 위치** | CKV·CKG 양쪽 BM25 dual-track | 개발 단계 유지, 동작 검증 후 수렴 결정 | 2026-05-18 | review-direction §6.1 Challenge 1 |
| **PR-regression target** | `stable-net/go-stablenet#70` | base SHA `aa28927fb1...` + plan↔diff LLM-judge similarity | 2026-05-18 | review-direction Appendix C |

### 21.2 미해결

- **q3**: sqlite-vec ANN 성능 — 1M+ chunk 환경 latency p95 측정 필요. fallback = LanceDB. 측정 시점: 첫 1M LOC 인덱스 직후.
- **q5**: Working memory 의 다중 프로세스 동시성 (CKV+CKG 같은 run_id 동시 write) — file lock vs SQLite WAL. S2 working memory 도입 시 결정.

---

## 22. 변경 이력

| 일자 | 버전 | 변경 |
|---|---|---|
| 2026-05-05 | 1.0 | 초안 — UC-V1~V15 대응 모듈/컴포넌트 분해, P0/P1/P2 마킹 |
| 2026-05-19 | 1.1 | **광범위 정정** — (a) §0.1 **구현 상태 마스터 테이블** 신설 (60+ sub-section 실측 상태). (b) §1.2 언어 list: "Go/TS/Sol 구현, JS/Bash S2 이관" (사용자 결정). (c) §11.1 명령표: stale `reindex/serve/bootstrap` 표기 + `freshness/eval/model/footprint` 실측 명령 추가 + 상태 column. (d) §17.1 Makefile: `make eval` 제거 + `test-race`/`audit` 실측 추가. (e) §21 결정 사항 6건(q1/**q2**/q4/JS-Bash/BM25/PR#70 target) 정리. q3/q5는 미해결 유지. |
| 2026-05-19 | 1.2 | **`ckv reindex` S2 → S1.5 승격** (사용자 결정). retrieval-quality-roadmap.md §7.5의 architectural 의존성 — Phase B (multi-granularity) 적용 시 throughput 0.5 chunks/s 까지 악화되면 full rebuild가 비현실적 → incremental indexing이 *S1.5 마일스톤 entry condition*. §0.1 master table §6.2 + §11.1 명령표 정정. |
