# CKS 오케스트레이터 설계 초안

문서 버전: 0.1 (2026-05-29)
대상: 별도 프로젝트(`code-knowledge-system`)의 진입점 설계
선행 합의: D3 아키텍처 분리 (CKV semantic+keyword, CKG graph, CKS orchestrate)

---

## 0. 개요

CKS는 **coding-agent ↔ CKV/CKG** 사이의 중개자다. agent가 jira ticket을 받으면 단일 entry point(`cks` MCP)로 호출하고, CKS가 멀티홉 retrieval·캐시·히스토리를 책임진다.

핵심 책임 4가지:

1. **오케스트레이션**: CKV+CKG의 16+α 도구를 멀티홉 워크플로우로 조합
2. **캐시**: in-memory hot + sqlite warm (세션 변경 시 회복)
3. **쿼리 히스토리**: 모든 retrieval 트레이스를 영속화 → 인간 리뷰어 재현 가능
4. **응답 병합**: CKV의 hits + CKG의 hints + 도메인 메타 → 단일 응답

CKS는 CKV의 wrapper가 아니다. CKV/CKG의 raw 도구는 그대로 노출되지만, CKS의 **고수준 도구**(`cks.flow.*`)가 멀티홉을 자동 수행해서 agent의 토큰을 절약한다.

---

## 1. 시스템 위치

```
┌──────────────────────────┐
│  coding-agent (Claude)   │
│  - jira ticket reader    │
│  - design/impl/test loop │
└────────┬─────────────────┘
         │ MCP (stdio / HTTP)
         ▼
┌──────────────────────────┐         ┌──────────────────────────┐
│  CKS orchestrator        │         │  query history (sqlite)  │
│  - flow tools            │ ──────► │  + replay store          │
│  - cache layer           │         └──────────────────────────┘
│  - history logger        │
└────────┬────────┬────────┘
         │        │
    MCP  │        │ MCP
         ▼        ▼
   ┌───────┐  ┌───────┐
   │  CKV  │  │  CKG  │
   │ vec+  │  │ graph │
   │ bm25  │  │ ckg   │
   └───────┘  └───────┘
```

각 컴포넌트의 라이프사이클:
- CKV: 빌드 시점에 데이터 생성, 쿼리 시점에 read-only
- CKG: 동일 (별도 repo)
- CKS: agent 세션마다 1개 인스턴스. CKV/CKG에 MCP client로 접속

---

## 2. CKS 도구 분류 (제안)

CKS는 두 종류의 도구를 노출한다:

### 2.1 Pass-through 도구 (raw 노출)

CKV/CKG의 도구를 그대로 노출. 이름 prefix만 다름 (`cks.context.*`, `cks.graph.*`).

이유: agent가 직접 primitive 호출이 필요한 경우 (예: 디버깅, 학습 단계).

### 2.2 Flow 도구 (멀티홉 자동화)

CKS의 고유 가치. 한 번 호출에 여러 hop을 자동 수행.

| Flow 도구 | 내부 호출 시퀀스 | 목적 |
|----------|-------------------|------|
| `cks.flow.investigate` | semantic_search → narrow → find_invariants → get_conventions → expand → enrich (git history) | jira ticket 1차 분석 |
| `cks.flow.plan_change` | investigate → CKG callgraph → co-modified → impact set | 변경 영향도 |
| `cks.flow.verify_design` | plan_change → invariants 교차 검증 → guidance.required_tests 수집 | 설계 검토 |
| `cks.flow.explain_decision` | explain_match + git blame + PR 컨텍스트 | 인간 리뷰 audit |
| `cks.flow.scan_diff` | git diff 입력 → 각 hunk마다 invariants + guidance + co_modified | PR 자동 리뷰 |

---

## 3. `cks.flow.investigate` 상세 (대표 예시)

가장 자주 쓸 flow. 구체 알고리즘 설계.

### 3.1 입력

```json
{
  "intent": "Add JWT refresh token rotation",
  "ticket_id": "PROJ-1234",
  "max_hops": 4,
  "category_hint": "auth",
  "token_budget": 8000
}
```

### 3.2 알고리즘

```
1. semantic_search(intent, k=20)
   → 1차 후보 20

2. keyword_search(extract_symbols(intent), k=10)
   → 키워드 후보 10 (의미·키워드 양쪽 커버)

3. dedup(merge(1, 2)) by chunk_id → 후보 셋 C (~25개)

4. narrow_candidates(C, category=category_hint || None)
   → C' (필터 통과)

5. for hit in C' top-10:
     find_invariants(file=hit.file, tier_min=1)
     → invariants 누적

6. for pkg in distinct(C' .package)[:3]:
     get_conventions(package=pkg)
     → conventions 누적

7. ckg.related_symbols(C' top-5 .symbols)
     → 이웃 심볼 N개

8. ckg.co_modified(C' top-5 .files)
     → 함께 자주 수정되는 파일 셋

9. expand_in_file(C' [0].chunk_id, before=3, after=3)
     → 최상위 컨텍스트 확장

10. enrich_metadata(C' top-3, git_log_depth=5)
    → 최근 변경 히스토리

11. compose_response({
       hits: C' top-K,
       invariants: 누적 invariants,
       conventions: 누적 conventions,
       hints: {related: 7, co_modified: 8},
       recent_history: 10
    })
```

### 3.3 출력

```json
{
  "primary_hits": [/* top-K with full metadata */],
  "invariants": [/* deduped */],
  "conventions": [/* per-package */],
  "hints": {
    "related_symbols": [...],
    "co_modified_files": [...]
  },
  "recent_history": [/* commits */],
  "context_expansion": [/* surrounding chunks of top hit */],
  "trace_id": "...",
  "stats": {
    "ckv_calls": 5,
    "ckg_calls": 2,
    "cache_hits": 7,
    "total_tokens_returned": 6240,
    "duration_ms": 387
  }
}
```

### 3.4 토큰 회계

agent가 같은 정보를 grep+read로 모았다면 예상 토큰:

| 방식 | 호출 수 | 누적 토큰 |
|------|---------|-----------|
| grep+read iterative | 30~50 calls | 50,000+ |
| 개별 MCP primitive (16개 도구 직접) | 10~15 calls | 15,000~20,000 |
| `cks.flow.investigate` (1 call) | 1 call | 6,000~8,000 |

목표: 기존 LLM-only 대비 **80% 이상 토큰 절감**.

---

## 4. 캐시 레이어

### 4.1 2-tier 구조

```
agent 요청 → CKS
  └─ memCache.Get(key)
     └─ hit  → 반환
     └─ miss → diskCache.Get(key) (sqlite)
              └─ hit  → memCache.Set + 반환
              └─ miss → CKV/CKG 실행 → both caches.Set
```

### 4.2 캐시 키 설계

```
key = sha256(
  tool_name + "|" +
  canonical_args_json + "|" +
  ckv_indexed_head + "|" +
  ckg_indexed_head + "|" +
  schema_version
)
```

- `canonical_args_json`: 인자 정렬 + 정규화 (예: `k: 10` 명시화)
- `indexed_head` 포함 → DB 갱신 시 자동 무효화
- `schema_version` 포함 → 1.1 → 1.2 시 자동 무효화

### 4.3 캐시 정책

| 도구 | mem TTL | disk TTL | 비고 |
|------|---------|----------|------|
| `cks.flow.*` | 1h | 7d | flow는 결정적이므로 캐시 효과 큼 |
| `cks.context.semantic_search` | 30m | 24h | intent 미세 변경 잦음 |
| `cks.context.keyword_search` | 1h | 7d | 결정적 |
| `cks.context.find_invariants` | 1h | 7d | 거의 정적 |
| `cks.context.get_conventions` | 1h | 7d | 거의 정적 |
| `cks.context.explain_match` | 캐시 안 함 | - | 디버깅 도구, 늘 fresh |
| `cks.ops.health` | 캐시 안 함 | - | identity 확인 |

### 4.4 메모리 영향

- mem cache: 기본 capacity 1000 entry × 평균 8KB 응답 ≈ 8 MB
- disk cache: SQLite 테이블 1개, varchar key + blob value, 자동 LRU eviction

---

## 5. 쿼리 히스토리

### 5.1 동기

PR이 인간 리뷰어에게 갈 때, "agent가 왜 이 함수를 수정했나"를 추적 가능해야 함. CKS의 모든 호출은 영속 로그로 남고, `trace_id`로 PR ↔ retrieval session을 연결.

### 5.2 스키마

```sql
CREATE TABLE query_history (
  trace_id    TEXT NOT NULL,        -- agent session correlation
  call_id     INTEGER NOT NULL,     -- 1, 2, 3, ... within session
  tool        TEXT NOT NULL,        -- "cks.flow.investigate"
  args_json   TEXT NOT NULL,
  response_summary_json TEXT,       -- top-N IDs + counts (full body too big)
  ckv_calls   INTEGER,
  ckg_calls   INTEGER,
  cache_hits  INTEGER,
  duration_ms INTEGER,
  created_at  TEXT NOT NULL,
  PRIMARY KEY (trace_id, call_id)
);
```

응답 본문 전체는 별도 파일 (`history/<trace_id>/<call_id>.json`)로 저장.

### 5.3 재현 (replay)

```
cks replay --trace-id <id>
  → 모든 호출의 args + 응답을 재구성
  → CKV/CKG 호출 없이 캐시·로그만으로 시연
  → 인간 리뷰어가 "agent가 본 데이터" 정확히 추적
```

---

## 6. CKS가 CKV/CKG에 요구하는 추가 인터페이스

현재 CKV에는 다음이 없거나 부족 — CKS가 효율적으로 동작하려면 추가가 권장됨:

### 6.1 CKV 추가 후보

| 추가 항목 | 필요 이유 | 우선순위 |
|----------|----------|---------|
| `cks.context.batch_get_chunks(ids)` | flow가 LookupByIDs를 직접 호출해 N개 chunk를 가져오는 케이스 빈번 | 높음 |
| Response에 `tokens_used` 표준화 | CKS의 token_budget 회계용 | 중간 |
| `indexed_head` 헤더 (응답 envelope) | 캐시 invalidation 자동화 | 높음 |
| `cks.context.list_categories` | flow가 사용 가능 카테고리 enum 동적 조회 | 낮음 |

### 6.2 CKG 추가 필요 (별도 repo 작업)

| 항목 | 용도 |
|------|------|
| `cks.graph.related_symbols(symbols)` | hints |
| `cks.graph.co_modified(file_or_symbol)` | git history mining |
| `cks.graph.find_callers(symbol)` | impact 분석 |
| `cks.graph.find_definition(symbol)` | navigation |
| `cks.graph.find_callees(symbol)` | dependency |

---

## 7. 코드 구조 윤곽 (CKS repo)

```
code-knowledge-system/
├── cmd/
│   └── cks/
│       ├── main.go
│       ├── mcp.go        # MCP 서버 진입점
│       └── replay.go     # 히스토리 재현 CLI
├── internal/
│   ├── client/
│   │   ├── ckv.go        # CKV MCP client
│   │   └── ckg.go        # CKG MCP client
│   ├── cache/
│   │   ├── memory.go     # in-memory LRU
│   │   └── disk.go       # sqlite backed
│   ├── history/
│   │   ├── store.go      # query_history 영속
│   │   └── replay.go     # 재현 로직
│   ├── flow/
│   │   ├── investigate.go
│   │   ├── plan_change.go
│   │   ├── verify_design.go
│   │   ├── scan_diff.go
│   │   └── compose.go    # 응답 병합
│   └── orchestrator/
│       ├── orchestrator.go  # CKV+CKG+cache+history
│       └── token_budget.go
├── pkg/
│   ├── flow/             # 외부 client용 type
│   └── trace/            # trace_id propagation
└── docs/
    ├── flows/            # flow별 알고리즘 문서
    └── ARCHITECTURE.md
```

---

## 8. 단계적 구현 계획

### Phase 1: Skeleton (~1주)

- `cmd/cks` 기본 구조 + MCP 서버 진입점
- `internal/client/ckv.go`: CKV MCP client (stdio + HTTP 양쪽 지원)
- pass-through 도구만 노출 (CKV의 15개 도구 그대로 + 이름 prefix 변경)
- 기본 in-memory 캐시 (key 구조만, 정책 단순화)

### Phase 2: First Flow (~1주)

- `cks.flow.investigate` 1개 flow 완성
- 토큰 회계 + footprint 로그
- query_history 영속 (sqlite)
- CKV의 `tokens_used`, `indexed_head` 응답 확장 (CKV repo 작업)

### Phase 3: CKG 통합 (~2주)

- CKG repo 작업이 선행되어야 함
- `cks.graph.related_symbols`, `cks.graph.co_modified` 호출 통합
- `cks.flow.plan_change` 구현 (impact 분석)

### Phase 4: 측정·튜닝 (~1주)

- 토큰 회계 결과 검증 (목표: 80% 절감)
- 캐시 hit rate 측정
- Flow별 응답 시간 p50/p99

### Phase 5: 운영 도구 (~1주)

- `cks replay` CLI
- `cks history list / show` 디버그 도구
- Prometheus 메트릭 (optional)

총 직렬 진행 ~6주. CKG repo 의존성이 가장 큰 가변 요소.

---

## 9. 리스크

| 항목 | 영향 | 완화 |
|------|------|------|
| CKG repo 의존 | Phase 3 일정 가변 | Phase 1+2를 CKV-only로 진행, CKG는 후 결합 |
| 캐시 무효화 누락 | 잘못된 응답 | indexed_head 키 포함 + 빌드 후크에서 캐시 자동 flush 옵션 |
| query_history 데이터 폭증 | 디스크 압박 | trace_id별 retention (예: 30일), 압축 |
| token 회계 정확도 | 측정 의미 약화 | CKV가 응답에 `tokens_used` 추가 필수 |
| MCP client 안정성 | 세션 중단 | stdio 재연결 + circuit breaker |

---

## 10. 다음 액션

1. **이 문서 합의** (현재 단계)
2. **별도 repo 생성** (`code-knowledge-system`)
3. **Phase 1 skeleton 구현** (~1주, CKV repo와 독립 가능)
4. **CKV에 응답 envelope 확장** (이 repo 작업, 1일):
   - `tokens_used` 표준화
   - `indexed_head` 응답 헤더화
   - `batch_get_chunks` 도구 추가

---

## 참조

- `docs/plan-2026-05-29-ckv-refactor.md` — CKV Schema-First 계획
- `docs/mcp-tools.md` — CKV 15개 MCP 도구
- D3 합의 (세션 노트): CKV/CKG/CKS 책임 분리
