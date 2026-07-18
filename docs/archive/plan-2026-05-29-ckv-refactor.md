# CKV 대규모 리팩토링·확장 계획서

> **ARCHIVED 2026-07-19.** Plan executed; decisions live in the ADRs (`docs/adr/`) and live status in [`remaining.md`](../remaining.md). Kept for provenance.

문서 버전: 1.0
작성일: 2026-05-29
대상 코드베이스: `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector`
대상 정책 데이터: `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`
작성 기준: 개발자 1명 직렬 진행, 하루 6시간 집중 작업 = 1 dev-day

---

## 0. 개요 (Executive Summary)

본 계획서는 CKV를 "범용 시맨틱 코드 검색"에서 "go-stablenet 도메인 의식형 retrieval primitive"로 확장한다. 핵심 변경은 세 가지다.

1. **데이터 확장 (Phase A)**: 청크 metadata에 도메인 카테고리·invariant·convention 통계 부여
2. **쿼리 도구 확장 (Phase B)**: BM25·필터·invariant·convention·explain을 1급 MCP 도구로 노출
3. **인프라 강화 (Phase C)**: 임베딩 캐시, 스키마 마이그레이션, AST 통계 파이프라인

CKV는 데이터·검색 primitive에만 집중하고, hints/관계 추론은 CKG·CKS가 담당한다 (D3 합의).

전체 직렬 진행 예상: **15 영업일** (~3주). 병렬 옵션 적용 시 13일.

---

## 1. 배경 및 합의된 설계 결정

### 시스템 컨텍스트

CKV는 3-컴포넌트 아키텍처의 검색 primitive 노드:
- **CKV** (vector retrieval): 시맨틱·키워드 코드 검색
- **CKG** (graph): 심볼·callgraph·co-modify 그래프 (별도 프로젝트)
- **CKS** (orchestrator): MCP 서버로 CKV+CKG 조합, 캐시, 쿼리 히스토리 (별도 프로젝트)

목표는 Claude Code plugin "coding-agent"가 jira ticket을 받아 go-stablenet 프로젝트의 코드 분석·구현·테스트·PR 생성까지 자동 수행하는 시스템 구축.

### 합의된 설계 결정 (사용자 확정)

**D1. Sensitivity → ModificationGuidance로 재정의**
- 경로 기반 카테고리 (consensus/state/crypto/p2p/rpc/txpool/params/systemcontracts/beacon/miner/cli/test)
- 각 카테고리에 3필드: `also_review` / `required_tests` / `watch_out`
- "수정 금지"가 아니라 "수정 시 같이 검토해야 할 것"

**D2. Invariant 마커: 3-tier 전략**
- Tier 1: 기존 마커 추출 (`// CRITICAL`, `// IMPORTANT`, `// WARNING`, `// Deprecated:`)
- Tier 2: 신규 컨벤션 도입 (`// INVARIANT:`, `// CONSENSUS:`, `// SECURITY:`)
- Tier 3: 휴리스틱 (`panic()`/`fmt.Errorf()` 메시지의 정책 키워드)

**D3. 아키텍처 분리**
- CKV: semantic+keyword retrieval, 코드 데이터만
- CKG: 그래프 관계 (callgraph, co-modify, related)
- CKS: 오케스트레이션, 캐시, 히스토리
- hints 계산은 CKV 범위 밖

**D4. Convention 추출**
- CKV는 AST 통계 raw data만 제공 (LLM 사용 안 함)
- 해석·요약은 coding-agent의 SKILL이 담당

**D5. 캐시**
- CKS 레벨 통합 캐시 (in-memory + sqlite warm)
- CKV는 임베딩 마이크로 캐시만 (LRU)

**D6. MCP 도구 7개 모두 노출 후 사용 통계 기반 정리**

---

## 2. 사전 조사 결과 (현 상태)

| 항목 | 사실 |
|------|------|
| 등록된 MCP 도구 | 9개 (`semantic_search`, `get_freshness`, `health`, `warmup`, `related_changes`, `embed`, `vector_search`, `rerank`, `index`) |
| `rerank` 현 동작 | 인자만 받고 안내 메시지 리턴 (스텁) |
| `Chunk` 구조체 위치 | `pkg/types/chunk.go` |
| `Chunk` 필드 | 14개. Category·Guidance·Invariant 관련 없음 |
| `ChunkKind` enum | 7개. invariant·convention 없음 |
| `Hit` 구조체 | guidance·category 필드 없음 |
| Facade 구조 | `query.Engine.Search`가 8단계 서비스 호출 |
| BM25 위치 | `internal/query/bm25/`, candidate-set rerank로만 사용 |
| 스키마 버전 | `ResponseSchemaVersion = "1"` |
| 기존 go-stablenet 마커 | `// CRITICAL`, `// IMPORTANT`, `// WARNING`, `// Deprecated:` 만 존재 |

---

## 3. 전체 의존 그래프

```
Phase A (데이터 확장)
├── A1 ChunkInvariant 추출기 ──┐
├── A2 ChunkConvention 추출기 ──┼──► Phase B (도구)
├── A3 Chunk metadata 확장 ────┤    ├── B1 keyword_search    (A3 필요)
├── A4 stablenet.yaml ─────────┘    ├── B2 narrow_candidates  (A3 필요)
                                    ├── B3 expand_in_file      (독립)
                                    ├── B4 find_invariants     (A1 필요)
                                    ├── B5 get_conventions     (A2 필요)
                                    ├── B6 explain_match       (A3 + B1 필요)
                                    └── B7 Hit 스키마 확장     (A3 필요)
                                              │
                                              ▼
                                    Phase C (인프라)
                                    ├── C1 임베딩 캐시 (독립)
                                    ├── C2 스키마 마이그레이션 (A3·B7 후)
                                    └── C3 AST 통계 파이프라인 (A2 후)
```

크리티컬 패스: **A3 → B7 → B6** (Hit 스키마가 변경되면 모든 도구가 따라감).

---

## 4. Phase A — 데이터 확장 (P0)

### A1. ChunkInvariant 추출기 (3-tier)

**Task 정의**: Go 파서가 함수/타입 청크 생성 시 invariant 문장을 함께 추출하여 `ChunkInvariant` 청크로 발행 + 원 청크의 `Invariants []InvariantRef`에 역참조.

**영향 파일**:
- 신규 `internal/invariant/extractor.go` (~250 lines)
- 신규 `internal/invariant/extractor_test.go`
- 신규 `internal/invariant/heuristic.go` (Tier 3, ~80 lines)
- 신규 `internal/invariant/testdata/*.go` (fixture 10개)
- 수정 `pkg/types/chunk.go`: `ChunkInvariant ChunkKind = "invariant"`, `InvariantRef{ChunkID, Tier int, Marker string}`
- 수정 `internal/chunk/chunk.go:Summarize`: Invariant 카운트
- 수정 `internal/build/pipeline.go:processFile`: invariant pass 호출

**시간**: 1.5 dev-day

**리스크**: MEDIUM
- 위험: Tier 3 휴리스틱이 false-positive 폭발 (테스트 mock panic 등)
- 완화: 신뢰도 점수 0~1, `_test.go` 제외, `MaxTier3PerFile = 10` 캡

**테스트 전략**:
- Unit: tier별 검출 케이스
- Golden: go-stablenet `consensus/parlia/parlia.go` 스냅샷
- 통합: 빌드 후 카운트·분포 검증

**의존성**: 없음 (병렬 가능)

**Side-effect**:
- 청크 총 개수 200~500 증가 → 임베딩 비용·인덱스 크기 영향
- schema_version 1 유지 가능 (additive)

**Definition of Done**:
- [ ] go-stablenet 빌드 시 ChunkInvariant ≥ 100
- [ ] Tier 분포: Tier1 ≥ 50%, Tier3 ≤ 30%
- [ ] 모든 invariant가 원 코드 라인 범위로 역추적 가능
- [ ] 단위 테스트 100% 통과, race detector clean

---

### A2. ChunkConvention 추출기 (AST 통계)

**Task 정의**: 패키지 단위로 AST 통계를 집계해 `ChunkConvention` 청크 1개 발행. raw data만 (D4 합의).

**수집 통계 (v1 동결)**:
1. 에러 핸들링 패턴 (`fmt.Errorf("%w: ", err)` vs `errors.New` vs `pkg/errors.Wrap` 빈도)
2. 로깅 라이브러리 (log15 / zap / slog 빈도)
3. naming convention (receiver short name, `func New*` 분포)
4. 테스트 스타일 (testify, table-driven 비율)
5. 동시성 패턴 (`sync.Mutex`, `chan`, `errgroup` 빈도)

**영향 파일**:
- 신규 `internal/convention/stats.go` (~300 lines)
- 신규 `internal/convention/stats_test.go`
- 신규 `internal/convention/testdata/` (sample 패키지 3개)
- 수정 `pkg/types/chunk.go`: `ChunkConvention ChunkKind = "convention"`, `ConventionStats map[string]any`
- 수정 `internal/build/pipeline.go`: 파일 처리 후 패키지별 누적

**시간**: 2 dev-day

**리스크**: MEDIUM
- 위험: 통계 항목 범위 폭발
- 완화: v1은 5개로 동결. 추가는 별도 ticket. `map[string]any`로 schema 안 깨고 확장 가능

**테스트 전략**:
- Unit: 각 통계 함수
- Golden: testdata 통계 스냅샷
- 통합: go-stablenet `consensus/` 통계 수동 검증

**의존성**: 없음. C3가 A2 후속.

**Side-effect**:
- 패키지당 청크 1개 증가 (~50~100개)
- Embed 호출 1회 추가/패키지

**Definition of Done**:
- [ ] go-stablenet 빌드 시 ConventionStats ≥ 30개
- [ ] 각 통계 필드가 의미 있는 값
- [ ] LLM이 통계를 읽고 한 문단 요약 가능 (수동 검증)

---

### A3. 청크 metadata 확장 (Category, ModificationGuidance)

**Task 정의**: `types.Chunk`에 `Category string`, `Guidance *ModificationGuidance` 추가. 빌드 시 경로→카테고리 매핑. Hit으로 전파.

**영향 파일**:
- 수정 `pkg/types/chunk.go`: 2개 필드 + `ModificationGuidance{AlsoReview, RequiredTests, WatchOut []string}`
- 수정 `pkg/types/chunk_test.go`: 직렬화 테스트
- 신규 `internal/policy/loader.go` (~150 lines)
- 신규 `internal/policy/loader_test.go`
- 수정 `internal/build/pipeline.go`: 청크 발행 후 `policy.Apply(chunks)` 호출
- 수정 `internal/query/engine.go:Hit`: `Category`, `Guidance` 필드
- 수정 `internal/query/service_enrich.go`: chunk → hit 변환

**시간**: 1.5 dev-day

**리스크**: MEDIUM
- 위험: 미정의 경로 처리 (기본값 "" vs "unknown")
- 완화: 빈 문자열 = "unclassified". `omitempty`. 빌드 로그에 unclassified 비율 출력

**테스트 전략**:
- Unit: 경로 매처 (정확/와일드카드/우선순위)
- 통합: stablenet.yaml 로드 후 chunks의 category 분포
- 회귀: 기존 semantic_search 응답 스키마 깨지지 않음 (snapshot)

**의존성**: A4 yaml 선행. B7로 전파.

**Side-effect**:
- 호환성: JSON `omitempty`로 깨지지 않음
- DB 스키마: chunks 테이블 컬럼 추가 → C2 마이그레이션 트리거

**Definition of Done**:
- [ ] stablenet.yaml 매칭 카테고리 비율 ≥ 70%
- [ ] Hit JSON에 category/guidance가 nil-safe로 노출
- [ ] schema_version 1 호환

---

### A4. policy/stablenet.yaml 작성

**Task 정의**: 경로 패턴 → category + guidance 매핑 yaml. CKV가 빌드 시 로드.

**스키마**:
```yaml
version: 1
categories:
  - name: consensus
    paths: ["consensus/**", "miner/**"]
    also_review: ["state", "params"]
    required_tests: ["consensus integration", "fork choice"]
    watch_out:
      - "validator set change requires hard-fork coordination"
      - "byzantine fault assumptions hardcoded in quorum math"
  - name: state
    paths: ["core/state/**", "trie/**"]
    ...
```

**카테고리 12개**: consensus / state / crypto / p2p / rpc / txpool / params / systemcontracts / beacon / miner / cli / test

**영향 파일**:
- 신규 `policy/stablenet.yaml` (~150 lines)
- 신규 `policy/README.md`
- 신규 `policy/schema.json` (선택, jsonschema 검증)

**시간**: 1 dev-day

**리스크**: LOW
- 위험: 도메인 지식 부족으로 부정확
- 완화: v1은 보수적. 운영 중 개선

**테스트 전략**:
- Schema 검증
- 매칭 비율 측정

**의존성**: A3 로더 선행 필요. 작성 자체는 병렬 가능.

**Side-effect**: 없음 (데이터 파일)

**Definition of Done**:
- [ ] 12개 카테고리 모두 정의
- [ ] go-stablenet 파일 70% 이상 매칭
- [ ] 각 카테고리에 최소 1개 also_review, 1개 required_tests, 2개 watch_out

---

## 5. Phase B — 쿼리 도구 (P0)

### B1. `keyword_search` MCP 도구

**Task 정의**: BM25를 vector 경로 없이 단독 사용하는 도구. 영구 BM25 인덱스 신규 구축.

**영향 파일**:
- 신규 `internal/query/keyword/index.go` (~200 lines)
- 신규 `internal/query/keyword/search.go` (~150 lines)
- 신규 `internal/query/keyword/*_test.go`
- 수정 `internal/build/pipeline.go`: 빌드 종료 시 `bm25.idx` dump
- 수정 `internal/query/engine.go`: `KeywordSearch(ctx, query, opts) ([]Hit, error)`
- 수정 `pkg/mcp/server.go`: `cks.context.keyword_search` 등록

**시간**: 2 dev-day

**리스크**: MEDIUM
- 위험: 50K 청크 BM25 인덱스 메모리 (수십 MB 예상)
- 완화: 측정 후 sqlite FTS5 fallback 검토

**테스트 전략**:
- Unit: tokenize/score
- 통합: keyword vs semantic 결과 비교 (golden)
- 성능: p50/p99

**의존성**: A3 권장 (filter). B6가 B1에 의존.

**Side-effect**:
- 인덱스 dir에 `bm25.idx` 추가
- 빌드 시간 +5~10%

**Definition of Done**:
- [ ] p50 < 50ms (50K 청크)
- [ ] semantic_search와 같은 Hit 스키마
- [ ] 메모리 < 200MB

---

### B2. `narrow_candidates` MCP 도구

**Task 정의**: 후보 chunk ID 리스트에 필터 적용. 멀티홉 retrieval의 1차 후보 좁히기.

**입출력**:
- 입력: `chunk_ids: []string`, `filter: {category, path, language, symbol_kind}`
- 출력: 필터 통과 chunk + metadata

**영향 파일**:
- 수정 `pkg/mcp/server.go`: `cks.context.narrow_candidates` 등록
- 수정 `internal/query/engine.go`: `NarrowCandidates(ctx, ids, filter)`
- 수정 `internal/store/sqlitevec/`: `LookupByIDs([]string)`

**시간**: 0.5 dev-day

**리스크**: LOW
- 위험: SQLite `IN` 절 한계 (999개)
- 완화: 분할 처리

**테스트 전략**:
- Unit: filter.Matches 재사용
- 통합: 100개 ID + 필터 → 정확한 부분집합

**의존성**: A3 권장.

**Side-effect**: 없음.

**Definition of Done**:
- [ ] 1000개 ID 입력 시 < 100ms
- [ ] 모든 filter 필드 동작

---

### B3. `expand_in_file` MCP 도구

**Task 정의**: chunk_id의 파일에서 인접 청크 리턴.

**입출력**:
- 입력: `chunk_id`, `before: int` (기본 2), `after: int` (기본 2)
- 출력: `chunks: []Chunk` (라인 순)

**영향 파일**:
- 수정 `pkg/mcp/server.go`: `cks.context.expand_in_file`
- 수정 `internal/query/engine.go`: `ExpandInFile(ctx, chunkID, before, after)`
- 수정 `internal/store/sqlitevec/`: `LookupByFileOrdered(file)`

**시간**: 0.5 dev-day

**리스크**: LOW

**테스트 전략**:
- Unit: 정렬·인접
- 통합: 함수 N개 + before=2 → 라인 순 5개

**의존성**: 없음.

**Side-effect**: 없음.

**Definition of Done**:
- [ ] 경계 케이스 정확
- [ ] < 50ms

---

### B4. `find_invariants` MCP 도구

**Task 정의**: 파일/카테고리 기준 ChunkInvariant 조회.

**입출력**:
- 입력: `file` 또는 `category`, `tier_min` (기본 1)
- 출력: `invariants: [{chunk_id, marker, tier, text, source_chunk_id, citation}]`

**영향 파일**:
- 수정 `pkg/mcp/server.go`: `cks.context.find_invariants`
- 수정 `internal/query/engine.go`: `FindInvariants(ctx, opts)`
- 수정 `internal/store/sqlitevec/`: ChunkKind=invariant 필터

**시간**: 0.75 dev-day

**리스크**: LOW

**테스트 전략**:
- Unit: tier_min 필터
- 통합: 특정 파일 invariants

**의존성**: **A1 필수**

**Side-effect**: 없음.

**Definition of Done**:
- [ ] file/category 필터 정확
- [ ] < 50ms

---

### B5. `get_conventions` MCP 도구

**Task 정의**: 패키지/파일 기준 ConventionStats 반환. raw data만.

**입출력**:
- 입력: `package: string` 또는 `file: string`
- 출력: `stats: map[string]any`

**영향 파일**:
- 수정 `pkg/mcp/server.go`: `cks.context.get_conventions`
- 수정 `internal/query/engine.go`: `GetConventions(ctx, pkg)`

**시간**: 0.5 dev-day

**리스크**: LOW

**테스트 전략**:
- 통합: 알려진 패키지 통계 조회

**의존성**: **A2 필수**

**Side-effect**: 없음.

**Definition of Done**:
- [ ] 5개 통계 항목 모두 포함
- [ ] 미지 패키지는 not_found 명시

---

### B6. `explain_match` MCP 도구

**Task 정의**: chunk_id가 쿼리에서 왜 매칭됐는지 설명. vector score breakdown + BM25 토큰 매칭 + boost 기여도 + category guidance.

**응답 예시**:
```json
{
  "chunk_id": "...",
  "vector_score": {"normalized": 0.82, "distance": 0.36, "rank": 3},
  "bm25_score": {"score": 4.2, "matched_tokens": ["consensus", "validator"]},
  "boost": {"signature": 1.1, "doc": 1.0, "recent": 1.2, "total_multiplier": 1.32},
  "category": "consensus",
  "guidance": {...},
  "citation": {...}
}
```

**영향 파일**:
- 수정 `pkg/mcp/server.go`: `cks.context.explain_match`
- 신규 `internal/query/explain.go` (~200 lines)
- 신규 `internal/query/explain_test.go`
- 수정 `internal/query/bm25/`: 토큰 매칭 결과 노출

**시간**: 1.5 dev-day

**리스크**: MEDIUM
- 위험: boost가 search 흐름과 결합되어 단독 호출 어려움
- 완화: boost를 순수 함수로 추출

**테스트 전략**:
- Unit: 각 점수 항목 분리
- 통합: search 후 동일 chunk_id의 explain이 search와 일치

**의존성**: **A3, B1 필수**

**Side-effect**: boost 리팩토링이 search 경로에 영향 → 회귀 테스트 필수.

**Definition of Done**:
- [ ] 각 점수가 search 결과와 매칭
- [ ] < 200ms

---

### B7. Hit 응답 스키마 확장

**Task 정의**: `query.Hit`과 MCP 응답에 `category`, `guidance` 추가. 기존 9개 도구 응답에 일관 적용.

**영향 파일**:
- 수정 `internal/query/engine.go:Hit`: 2 필드 추가
- 수정 `internal/query/service_enrich.go`: chunk → hit 복사
- 수정 `pkg/mcp/server.go`: schema_version "1" → "1.1" (minor bump)
- 수정 모든 도구 응답 직렬화 path

**시간**: 0.5 dev-day

**리스크**: LOW
- 위험: 외부 strict 파서
- 완화: cks 코드 사전 점검

**테스트 전략**:
- Snapshot: 기존 응답 + category/guidance
- 회귀: 9개 도구 응답이 schema_version 1.1과 호환

**의존성**: **A3 필수**

**Side-effect**: schema_version 1 → 1.1 (minor bump)

**Definition of Done**:
- [ ] 모든 도구 응답에 category/guidance nil-safe
- [ ] schema_version 정책 문서 업데이트

---

## 6. Phase C — 인프라 (P1)

### C1. 임베딩 마이크로 캐시 (LRU)

**Task 정의**: 동일 텍스트 임베딩 재요청 시 LRU 캐시 히트.

**영향 파일**:
- 신규 `internal/embed/cache/lru.go` (~150 lines, hashicorp/golang-lru/v2)
- 신규 `internal/embed/cache/lru_test.go`
- 수정 `internal/embed/`: 캐시 래퍼 (`WithCache(emb, size)`)
- 수정 `cmd/ckv/mcp/main.go`: 캐시 활성화

**시간**: 0.75 dev-day

**리스크**: LOW

**테스트 전략**:
- Unit: LRU evict, hit/miss 카운터
- 통합: 동일 쿼리 2회 → 두 번째 캐시 hit

**의존성**: 없음.

**Side-effect**: 메모리 증가 (1024 entry × 1024 dim × 4 byte = 4MB)

**Definition of Done**:
- [ ] 동일 텍스트 재요청 < 1ms
- [ ] hit rate metric 노출

---

### C2. 스키마 버전 + 마이그레이션 도구

**Task 정의**: A1·A2·A3에서 SQLite 컬럼 추가 필요. `migrations/` + `ckv migrate` CLI.

**영향 파일**:
- 신규 `internal/store/sqlitevec/migrations/001_add_category_guidance.sql`
- 신규 `internal/store/sqlitevec/migrations/002_add_invariant_refs.sql`
- 신규 `internal/store/sqlitevec/migrations/003_add_convention_stats.sql`
- 신규 `internal/store/sqlitevec/migrate.go` (~200 lines)
- 신규 `cmd/ckv/migrate/main.go`
- 수정 `pkg/mcp/server.go:ResponseSchemaVersion`: "1" → "1.1"

**시간**: 1.5 dev-day

**리스크**: HIGH
- 위험: 기존 인덱스의 신규 필드 백필 (category는 SQL update 가능, invariant/convention은 재추출 필요)
- 완화: 두 모드 — (a) 스키마만 (b) 데이터 백필 (재빌드 트리거). CLI 명시적 선택

**테스트 전략**:
- Unit: 각 마이그레이션 idempotency
- 통합: 기존 인덱스 → 마이그레이션 → 신규 도구 호출 nil-safe
- 회귀: 마이그레이션 전후 9개 도구 동작

**의존성**: A3·B7 후 (혹은 사전 인프라로 먼저)

**Side-effect**: 기존 사용자 업그레이드 시 마이그레이션 강제.

**Definition of Done**:
- [ ] migrate 후 schema_version 테이블 최신
- [ ] idempotent
- [ ] 미실행 시 Engine.Open에서 명확한 에러

---

### C3. AST 통계 빌드 파이프라인 통합

**Task 정의**: A2의 convention extractor를 build pipeline 정식 단계로.

**영향 파일**:
- 수정 `internal/build/builder.go`: 파일 후처리 hook
- 수정 `internal/build/pipeline.go`: convention.Aggregator 통합
- 수정 `internal/build/progress.go`: convention 단계 추가

**시간**: 0.75 dev-day

**리스크**: LOW

**테스트 전략**:
- 통합: builder_test.go에 convention 케이스
- 회귀: 빌드 시간 +10% 이내

**의존성**: **A2 필수**

**Side-effect**: 빌드 시간 +5~10%

**Definition of Done**:
- [ ] go-stablenet 빌드 +10% 이내
- [ ] convention 청크 ≥ 30개

---

## 7. 병렬 가능 Task

| 페어 | 병렬 가능 이유 |
|------|---------------|
| A1 ↔ A2 | 서로 다른 파이프라인 단계 |
| A4 ↔ A3 코드 작업 | yaml은 데이터, 로더 코드와 독립 |
| B3 ↔ B4 ↔ B5 | 서로 다른 도구, 다른 의존성 |
| C1 ↔ Phase A 전체 | 임베딩 캐시는 데이터와 독립 |

병렬 적용 시 시간 절감: 직렬 15 dev-day → 병렬 13 dev-day.

---

## 8. 측정·검증 체크포인트

### Phase A 완료 시
- [ ] go-stablenet 인덱스 빌드 성공
- [ ] 청크 카테고리 분포: unclassified ≤ 30%
- [ ] ChunkInvariant ≥ 100, ChunkConvention ≥ 30
- [ ] 기존 9개 MCP 도구 회귀 테스트 100% 통과
- [ ] 빌드 시간 +15% 이내, 인덱스 크기 +20% 이내

### Phase B 완료 시
- [ ] 7개 신규 도구 healthcheck 통과
- [ ] keyword_search p50 < 50ms (50K 청크)
- [ ] explain_match 응답이 search 결과와 일치
- [ ] schema_version 1.1 호환

### Phase C 완료 시
- [ ] 마이그레이션 idempotent + 회귀 무
- [ ] 임베딩 캐시 hit rate metric 노출
- [ ] 빌드 파이프라인 통합 후 전체 +10% 이내

### 사용자 시나리오 검증 (전체 완료 시)
- jira ticket → coding-agent → CKV semantic_search → find_invariants → get_conventions → explain_match 체인이 5분 이내
- LLM이 invariant·guidance를 무시하지 않음 (수동 평가 10건)

---

## 9. 롤백 계획

| Phase | 실패 시나리오 | 롤백 전략 |
|-------|--------------|----------|
| Phase A | A1·A2 false-positive 폭발 | git revert. yaml만 유지, 추출기는 다음 sprint |
| A3 metadata | schema 호환성 깨짐 | feature flag (`--enable-category`). 환경변수 `CKV_DISABLE_METADATA=1` |
| Phase B | 신규 도구 panic | mcp-go WithRecovery로 panic-to-error 자동. 도구별 disable (`CKV_DISABLE_TOOLS=keyword_search`) |
| C2 마이그레이션 | 데이터 손실 | 마이그레이션 전 자동 backup (`vector.db.bak.<timestamp>`). restore 명령 제공 |

---

## 10. 진행 순서 (확정: Schema-First)

### 확정 사유

크리티컬 패스가 `A3 → B7 → B6`이므로 스키마부터 안정화한다 (Section 3 의존 그래프 참조). HIGH 리스크인 C2 마이그레이션을 Day 1에 안전망으로 먼저 구축하고, 모든 후속 작업이 안전망 위에서 진행되도록 한다.

**Schema-First 선택 근거 5가지**:
1. 광범위 영향(metadata) 먼저 → 후속 도구가 두 번 수정되지 않음
2. HIGH 리스크(C2 데이터 손실 가능)를 Week 1에 처리 → 이후 모든 작업이 backup·rollback 위에서 진행
3. 회귀 위험 분산 (후반에 몰리지 않음)
4. A1·A2·A3가 각자 SQL migration 1개씩 추가하는 자연스러운 누적 흐름
5. 가장 복잡한 B6 explain_match를 충분히 안정화된 기반 위에서 구현

### 진행 순서 (확정)

```
Week 1 (스키마 + 안전망)
  Day 1:    C2 마이그레이션 프레임워크 (빈 상태, 안전망 구축)
  Day 2-3:  A3 metadata 확장 + 001_add_category_guidance.sql
  Day 4:    B7 Hit 스키마 확장 (모든 도구 일관성 확보)
  Day 5:    A4 stablenet.yaml 작성

Week 2 (데이터 확장)
  Day 6-7:  A1 ChunkInvariant 추출기 + 002_add_invariant_refs.sql
  Day 8-9:  A2 ChunkConvention 추출기 + 003_add_convention_stats.sql
  Day 10:   B2 narrow_candidates + B3 expand_in_file (작은 도구 묶음)

Week 3 (검색 도구 + 마무리)
  Day 11-12: B1 keyword_search (BM25 영구 인덱스)
  Day 13:   B4 find_invariants + B5 get_conventions
  Day 14-15: B6 explain_match + C3 파이프라인 통합 + C1 임베딩 캐시
```

### 검토 후 제외된 대안 (Basic, planner 초안)

planner의 최초 초안은 `A1 → A2 → A3 → A4 → B7 → B1 → ...` 순서로, 데이터 추출기를 먼저 개발하고 스키마는 중반에, 마이그레이션은 마지막에 두는 안이었다. 이는 자체 분석에서 명시한 크리티컬 패스(`A3 → B7 → B6`)와 일치하지 않아 제외했다.

제외 사유:
- A3 metadata 확장이 늦으면 A1·A2의 청크가 두 번 수정됨
- C2 마이그레이션이 마지막에 있으면 A1·A2·A3 모두 임시 임시 DB 스키마로 개발 → 통합 시 충돌
- B7 늦으면 B1~B5 도구가 모두 Hit 스키마 변경에 따라 재테스트 필요

---

## 11. 위험 요약 매트릭스

| Task | Risk | 영향 | 우선 완화 |
|------|------|------|-----------|
| A1 Tier 3 휴리스틱 | MEDIUM | invariant 노이즈 | 신뢰도 점수 + per-file 캡 |
| A2 통계 범위 | MEDIUM | 끝없는 확장 | v1 5개 항목 동결 |
| A3 schema 호환 | MEDIUM | cks 깨짐 | omitempty + schema_version 1.1 |
| B1 BM25 메모리 | MEDIUM | OOM | 측정 후 FTS5 fallback |
| B6 boost 결합 | MEDIUM | 회귀 | 순수 함수 추출 + 회귀 테스트 |
| C2 마이그레이션 | HIGH | 데이터 손실 | 자동 backup + dry-run |

---

## 12. 산출물 체크리스트

**코드**:
- [ ] `internal/invariant/` (A1)
- [ ] `internal/convention/` (A2)
- [ ] `internal/policy/` (A3·A4)
- [ ] `internal/query/keyword/` (B1)
- [ ] `internal/query/explain.go` (B6)
- [ ] `internal/embed/cache/` (C1)
- [ ] `internal/store/sqlitevec/migrate.go` + `migrations/*.sql` (C2)
- [ ] `cmd/ckv/migrate/` (C2)

**데이터**:
- [ ] `policy/stablenet.yaml`
- [ ] `policy/README.md`

**문서**:
- [ ] `docs/architecture-2026-05-refactor.md` (D1~D6 의사결정 기록)
- [ ] `docs/mcp-tools.md` (16개 도구 목록)
- [ ] schema_version migration note

**테스트**:
- [ ] 각 Task의 unit/통합 테스트
- [ ] e2e 시나리오 1개 (semantic_search → find_invariants → explain_match)

---

## 13. 참조

- 합의 토론 세션: 2026-05-29
- 관련 문서:
  - `docs/ARCHITECTURE.md` (현재 CKV 아키텍처)
  - `docs/SCHEMA.md` (현재 스키마)
  - `docs/backlog.md` (이전 작업 백로그)
  - `docs/plan-2026-05-26.md` (이전 계획서)
