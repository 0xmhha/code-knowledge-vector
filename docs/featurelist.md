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

## 1. 인덱싱 파이프라인 (Indexer)

### 1.1 파일 디스커버리 (P0)
- `--src` 디렉터리 재귀 워킹 (gitignore 존중)
- `.ckvignore` 또는 CLI flag 로 추가 제외 패턴 지원
- 파일별 메타: 경로, 크기, mtime, content hash (sha256), 언어 분류
- 심볼릭 링크 / 매우 큰 파일 / 바이너리 자동 스킵

### 1.2 멀티언어 파서 (Tree-sitter Level 1) (P0)
- 언어 지원: Go, Solidity, JavaScript, TypeScript, Bash
- tree-sitter grammar wrapper (`go-tree-sitter` 등)
- 파일별 AST 추출, 함수/메서드/타입/contract 노드 위치 식별
- 파서 결과 캐시 (key: `{commit_hash, file_path, content_hash}`)

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
- `FreshnessStale`, `BudgetExceeded`, `CitationNotFound`, `SanitizeFailed`, `IndexUnavailable`, `PolicyError`
- 각 에러는 호출자 처리 가이드와 함께 반환 (use-cases.md §error model)

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
| 명령 | 설명 |
|---|---|
| `ckv build` | 전체 인덱스 구축 (UC-V1) |
| `ckv reindex` | 변경분만 재인덱싱 (UC-V2) |
| `ckv query <text>` | semantic search (UC-V3/V4) |
| `ckv mcp` | MCP 서버 실행 (UC-V6) |
| `ckv serve` | HTTP API 서버 (옵션) |
| `ckv freshness` | 인덱스 신선도 출력 (UC-V11) |
| `ckv bootstrap --report` | systemization 리포트 (UC-V12) |
| `ckv eval` | KPI 측정 (UC-V12) |

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
- `.env`, `*.pem`, `*.key` 등 사전 정의 패턴은 임베딩 제외
- `.ckvignore` 로 사용자 추가 규칙

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
| Target | 동작 |
|---|---|
| `make build` | `go build -o bin/ckv ./cmd/ckv` |
| `make test` | `go test ./...` |
| `make lint` | `golangci-lint run` |
| `make fmt` | `gofmt -s -w .` + `goimports` |
| `make tidy` | `go mod tidy` |
| `make eval` | `bin/ckv eval` 호출 |
| `make clean` | `rm -rf bin/ data/ /tmp/ckv-*` |

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

## 21. 미해결 / 결정 필요 사항

- **q1**: chunk 단위로 Solidity 의 `event`/`modifier` 를 별도 chunk_kind 로 분리할지, function 과 묶을지
- **q2**: 기본 임베딩 모델: 코드 특화(`bge-code`) vs 범용(`bge-small`) — eval 결과로 결정
- **q3**: Vector store 기본: sqlite-vec 의 ANN 성능이 1M+ chunk 에서 충분한지 측정 필요
- **q4**: MCP 라이브러리: CKG 가 사용하는 라이브러리와 일치시킬지 별도 채택할지
- **q5**: Working memory 의 다중 프로세스 동시성 (CKV+CKG 가 같은 run_id 에 동시 write 시) — file lock vs SQLite WAL

(위 항목은 M1–M3 진행 중 결정. 결정 시 본 문서 §21 에 기록 후 해당 feature 항목을 갱신.)
