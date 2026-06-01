# Session Handoff — 2026-05-29 (updated 2026-06-01)

이 문서는 다른 머신·다른 세션에서 작업을 이어받기 위한 단일 진입점이다. 모든 진행 사항, 검증 결과, 미완료 항목, 다음 액션을 망라한다. 새 세션은 이 문서를 먼저 읽고 시작한다.

> **Update 2026-06-01:** CKV-Q1, CKV-Q2 완료. unclassified 21.2% → 6.9%,
> invariant 38 → 163. 신규 커밋 `545a89e`, `02bb24b`. §3, §4, §5.2 참조.

---

## ⚠️ 다음 세션 의도 (2026-06-01 사용자 결정)

**모든 §5 작업 HOLD.** CKS-1, CKV-E1/E2/E3 등 신규 기능 작업은 진행하지 않는다.

다음 세션 진입 순서:

1. **전체 코드베이스 검토** (CKV + CKS + CKG 횡단)
   - 현재 청구된 책임과 실제 구현의 정합성
   - 중복·미사용·과설계 영역 식별
   - 모듈 경계 재평가 (D3 분리가 실제로 작동하는지)

2. **리팩토링 플랜 수립**
   - 검토 결과 → 정식 plan 문서 (`docs/plan-2026-06-XX-refactor.md`)
   - 우선순위, 의존 그래프, 리스크, 측정 체크포인트

3. **리팩토링 실행** (플랜 합의 후)

4. **이후 신규 기능 작업 재개** (CKS-1 등은 리팩토링 종료 후)

→ **새 세션은 §5의 작업 목록을 진입점으로 삼지 말 것.** 위 순서대로 진행.

---

## 0. 환경 (재현 가능 상태)

| 항목 | 값 |
|------|-----|
| CKV repo | `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector` |
| CKS repo | `/Users/wm-it-22-00661/Work/github/tools/code-knowledge-system` |
| CKG repo | (별도, 위치 미확정) |
| go-stablenet | `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` |
| CKV branch | `main` |
| CKV HEAD | `8183cb8` (시간순 가장 최근 커밋) |
| Go version | 1.25.5 (toolchain 1.25.9) |
| Make 사용 | **반드시** `make build / test / lint / fmt` (직접 go 명령 금지) |
| CKV 빌드 산출물 | `bin/ckv` |

체크리스트 (새 세션에서 가장 먼저):

```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector
git pull
make build && make test
```

`make lint`는 errcheck 50건 + 잡다 5건 baseline. 신규 작업 후 카운트가 그대로면 회귀 없음.

---

## 1. 이번 세션 결과 요약 (CKV repo)

### 1.1 커밋 17건

```
8183cb8 docs: CKS orchestrator design draft (D3 architecture follow-up)        [hypothetical, 정정 필요]
8d79ead docs(mcp): document 15 MCP tools + refresh ckv mcp --help
6421c84 fix(lint): resolve session-introduced lint warnings
a7df54c style(cache): gofmt LRU implementation
ba7062a feat(mcp,embed): explain_match tool + LRU embedding cache              [B6 + C1]
41cd5cd style(query): gofmt InvariantHit struct alignment
73664d6 feat(mcp): find_invariants and get_conventions for policy + idiom lookup  [B4 + B5]
cd4bab5 feat(mcp): keyword_search MCP tool with lazy in-memory BM25 index        [B1]
ce72d91 feat(mcp): add narrow_candidates and expand_in_file primitives           [B2 + B3]
89f6f73 feat(convention): per-package AST statistics as ChunkConvention chunks  [A2]
f11d8bb feat(invariant): extract 3-tier policy statements from Go source         [A1]
be7f9a7 feat(policy): ship stablenet.yaml with 13 categories and authoring guide [A4]
4f0a4c4 feat(policy): add path-based category and modification guidance to chunks [A3 + B7]
d910e51 feat(store): add schema migration framework with auto-backup             [C2]
cbeb842 docs: add CKV refactor plan with Schema-First order                      [Plan]
74c1ae3 feat(cli): add --exclude flag to build and reindex commands             [이전 세션 잔류]
2baf745 feat(mcp): add cks.ops.index tool for triggering build/reindex          [이전 세션 잔류]
```

이번 세션 신규: 15건 (`cbeb842` 이후). `74c1ae3`, `2baf745`는 이전 세션 분.

### 1.2 Schema-First 계획 100% 완료

`docs/plan-2026-05-29-ckv-refactor.md`에 정의된 12개 Task 모두 완료:

| Phase | Task | 상태 | 커밋 |
|-------|------|------|------|
| C2 | 마이그레이션 프레임워크 + 자동 백업 | ✅ | `d910e51` |
| A3 | Category + ModificationGuidance 필드 | ✅ | `4f0a4c4` |
| B7 | Hit 스키마 1.1 | ✅ | `4f0a4c4` (A3와 함께) |
| A4 | stablenet.yaml (13 카테고리) | ✅ | `be7f9a7` |
| A1 | 3-tier invariant 추출기 | ✅ | `f11d8bb` |
| A2 | per-package AST convention | ✅ | `89f6f73` |
| C3 | AST 통계 빌드 파이프라인 통합 | ✅ | `89f6f73` (A2와 함께) |
| B2 | `narrow_candidates` MCP | ✅ | `ce72d91` |
| B3 | `expand_in_file` MCP | ✅ | `ce72d91` |
| B1 | `keyword_search` MCP (BM25) | ✅ | `cd4bab5` |
| B4 | `find_invariants` MCP | ✅ | `73664d6` |
| B5 | `get_conventions` MCP | ✅ | `73664d6` |
| B6 | `explain_match` MCP | ✅ | `ba7062a` |
| C1 | 임베딩 LRU 캐시 | ✅ | `ba7062a` |

### 1.3 CKV 현재 노출 면

**MCP 도구 15개 (`./bin/ckv mcp` 등록):**

- 검색: `semantic_search`, `keyword_search`, `vector_search`
- 정제: `narrow_candidates`, `expand_in_file`
- 메타: `find_invariants`, `get_conventions`, `explain_match`
- 보조: `embed`, `rerank` (stub), `related_changes`
- 운영: `health`, `get_freshness`, `warmup`, `index`

스키마 버전 `1.1` (additive, 1.0 파서와 호환).

**청크 종류 9 (`pkg/types/chunk.go`):**

기존 7 (`symbol`, `function_split`, `file_header`, `doc`, `pr_background`, `pr_solution`, `commit_message`) + 신규 2 (`invariant`, `convention`).

**Chunk 메타데이터 신규 필드:**

- `Category string`
- `Guidance *ModificationGuidance{AlsoReview, RequiredTests, WatchOut}`
- `Invariants []InvariantRef{ChunkID, Tier, Marker}`
- `ConventionStats map[string]any`

**적용된 SQLite 마이그레이션 4개:**

```
000_baseline.sql              # no-op
001_add_category_guidance.sql # ALTER TABLE chunks ADD category, guidance
002_add_invariant_refs.sql    # ALTER TABLE chunks ADD invariants
003_add_convention_stats.sql  # ALTER TABLE chunks ADD convention_stats
```

자동 적용 정책: `Open()`에서 백업(`<db>.bak.<ts>`) 후 적용. 수동 모드는 `CKV_DISABLE_AUTO_MIGRATE=1`.

---

## 2. CKS repo 현재 상태 (2026-05-29 시점)

`/Users/wm-it-22-00661/Work/github/tools/code-knowledge-system`

### 2.1 노출 도구 11개

```
cks.context.semantic_search    cks.context.find_symbol     cks.context.get_subgraph
cks.context.search_text        cks.context.find_callers    cks.context.impact_analysis
cks.context.get_for_task       cks.context.find_callees    cks.context.change_history
cks.ops.freshness              cks.ops.health
```

### 2.2 아키텍처 핵심

```
cmd/
  cks-agent              # CLI client agent
  cks-mcp                # MCP server
  cks-eval               # evaluation harness
  cks-glossary-gen       # glossary 생성기
  cks-inventory-check    # domain knowledge inventory 검증
  cks-entry-verify       # entry 단위 검증

internal/
  composer/              # Stage 1 (semantic) + Stage 2 (CKG) + RRF
  ckvclient/             # CKV backend interface (현재: SemanticSearch + Health + Freshness)
  ckgclient/             # CKG backend interface
  vocab/                 # 한국어 / 모호 prompt glossary
  inventory/             # domain knowledge entries (go-stablenet 15개)
  envelope/              # 응답 envelope
  observe/, footprint/   # 관측
  auditlog/              # audit trail
  mcp/                   # MCP 도구 핸들러
  config/                # 설정
  eval/                  # evaluation 구조
```

### 2.3 CKS의 `ckvclient.Client` 인터페이스 (현재)

```go
type Client interface {
    SemanticSearch(ctx, query, opts) ([]contract.Hit, error)
    Health(ctx) (Health, error)
    Freshness(ctx) (FreshnessReport, error)
    Close() error
}
```

→ **CKV 신규 6개 도구(keyword_search, narrow_candidates, expand_in_file, find_invariants, get_conventions, explain_match)는 아직 CKS에 통합되지 않음.** 이게 다음 작업의 핵심.

### 2.4 hypothetical 설계 문서 정정

`docs/cks-design-2026-05-29.md`는 CKV repo 안의 **hypothetical 초안**으로, CKS의 실제 진행 상태(11 도구, composer, vocab/inventory)와 다음 사항이 다름:

- 실제 CKS는 `cks.flow.*` 도구가 없음 (composer가 내부에서 처리)
- 실제 CKS의 ckvclient는 SemanticSearch만 노출
- CKG graph 도구는 이미 `find_symbol/callers/callees/subgraph/impact_analysis`로 통합되어 있음
- vocab/inventory가 핵심 레이어 (초안에 없음)

→ 새 세션 권장: `cks-design-2026-05-29.md`를 정정하거나 삭제하고, CKS repo의 `docs/`의 실제 문서(`integrated-workplan-2026-05-27.md`, `system-review-2026-05-27.md`)를 진실로 삼는다.

---

## 3. V1+V2+V3 검증 결과 (2026-05-29 실행)

mock embedder로 go-stablenet 빌드. 카테고리 분포, invariant/convention 수, MCP 도구 호출, 마이그레이션 안전성 확인.

### 3.1 빌드 메트릭 (2026-06-01 갱신, Q1+Q2 적용 후)

```
파일: 2,290 / 청크: 26,015 / 시간: ~9초 (mock)
chunk_kind 분포:
  symbol        22,131
  file_header    2,179
  doc            1,335
  convention       207
  invariant        163  (← 이전 38)
category 분포 (top 8, 19개 카테고리 중):
  systemcontracts  6,736 (26.0%)
  test             5,260 (20.3%)
  p2p              1,982 ( 7.7%)
  (unclassified)   1,790 ( 6.9%)  (← 이전 21.2%)
  rpc              1,254 ( 4.8%)
  crypto           1,231 ( 4.8%)
  accounts         1,141 ( 4.4%)  (신규)
  state            1,043 ( 4.0%)
```

### 3.2 목표 대비

- ✅ unclassified 6.9% (목표 ≤30%, Q1으로 21.2% → 6.9%)
- ✅ Convention 207 (목표 ≥30)
- ✅ **Invariant 163 (목표 ≥100, Q2로 38 → 163)**

Invariant tier 분포: Tier 1 = 38 (Deprecated 28 / CRITICAL 7 / IMPORTANT 2 / WARNING 1), Tier 3 = 125 (errors.New 71 / fmt.Errorf 45 / panic 6 / panic+fmt.Errorf 2 / panic(ident) 1).

### 3.3 MCP 도구 종단 테스트

- `find_invariants(category=systemcontracts)`: 2건 (WARNING, IMPORTANT) 정상 반환
- `get_conventions(package=consensus/clique)`: file_count=5, mutexes=1, channels=2 정확
- `keyword_search("ValidateBlock")`: top-3 hit (bm25=14.21, 13.36, 12.43), category/guidance 전파됨
- `semantic_search("validator quorum")`: guidance.required_tests 응답에 포함

### 3.4 마이그레이션 검증

```
ckv migrate --status:
  current=003 applied=4 pending=0 tampered=0
ckv migrate --dry-run: 동일
.bak 파일: ckv-stablenet/vector.db.bak.1780036541 (생성 확인)
```

---

## 4. V1 발견 사항 (개선 후보, 우선순위)

### 4.1 stablenet.yaml glob 보강 — ✅ 완료 (2026-06-01, `545a89e`)

6개 카테고리 추가 (evm, rawdb, types, rlp, accounts, tracers). 결과: unclassified 21.2% → 6.9%. 13 → 19 카테고리.

### 4.2 Tier 3 휴리스틱 확장 — ✅ 완료 (2026-06-01, `02bb24b`)

3가지 패턴 추가 + buggy skipTier3 로직 정정:

- `panic(err)` + 주변 3줄 내 정책 주석 — 채택, 주석 텍스트가 invariant text가 됨
- `panic(fmt.Sprintf("...키워드..."))` — 채택
- `panic(fmt.Errorf("...키워드..."))` — 채택
- 이전 lint 단순화에서 깨진 `skipTier3` 의미 복원 (`SkipTier3InTests && _test.go`)

결과: invariant 38 → 163 (+329%). Tier 3 = 125건 신규.

### 4.3 보안: mcp-go의 graceful error (이미 됨)

확인 완료, 별도 작업 불필요.

---

## 5. 다음 작업 (우선순위별)

### 5.1 CKS 통합 (가장 큰 그림)

**Task CKS-1: CKS의 ckvclient에 CKV 신규 6 도구 추가** (CKS repo 작업)

`/Users/wm-it-22-00661/Work/github/tools/code-knowledge-system/internal/ckvclient/interface.go` 인터페이스 확장:

```go
type Client interface {
    // 기존
    SemanticSearch(ctx, query, opts) ([]Hit, error)
    Health(ctx) (Health, error)
    Freshness(ctx) (FreshnessReport, error)
    Close() error

    // 신규 (CKV 1.1 응답 활용)
    KeywordSearch(ctx, query, opts) ([]Hit, error)
    NarrowCandidates(ctx, ids []string, filter Filter) ([]Hit, error)
    ExpandInFile(ctx, chunkID string, before, after int) ([]Hit, error)
    FindInvariants(ctx, file, category string, tierMin int) ([]InvariantHit, error)
    GetConventions(ctx, packagePrefix string) ([]ConventionHit, error)
    ExplainMatch(ctx, chunkID, intent string) (*Explanation, error)
}
```

각각 fake/dummy/real backend에 추가. composer pipeline이 활용할지는 별도 결정.

**Task CKS-2: CKS MCP에 신규 도구 노출** (CKS repo 작업)

CKS의 `internal/mcp/`에 새 핸들러 추가. 이름: `cks.context.keyword_search`, `cks.context.narrow_candidates`, ...

**Task CKS-3: composer pipeline 활용 결정** (설계)

신규 도구를 composer Stage 1/2에 어떻게 끼울지. 후보:

- Stage 1 semantic 후 `narrow_candidates`로 category 필터
- Stage 2 후 `find_invariants` + `get_conventions`로 보강
- 응답 envelope에 invariants/conventions 추가

### 5.2 CKV 내부 개선 (완료)

- ✅ CKV-Q1 (commit `545a89e`)
- ✅ CKV-Q2 (commit `02bb24b`)
- ✅ CKV-Q3 (commit `4621b8d`)

### 5.3 CKV envelope 확장 (CKS 통합 의존)

**Task CKV-E1: 응답에 `tokens_used` 표준화**

CKS의 token budget 회계용. 모든 도구 응답에 추가.

**Task CKV-E2: `batch_get_chunks(ids)` MCP 도구**

CKS의 flow에서 N개 ID로 chunk 일괄 조회 빈번. `narrow_candidates`와 비슷하지만 필터 없는 단순 lookup.

**Task CKV-E3: 응답 envelope에 `indexed_head` 표준화**

CKS의 캐시 키 자동 무효화용.

### 5.4 측정 작업 (PC 변경 필요)

- M1: `make eval-pr` — 12-PR fixture에서 BM25 rerank A/B
- M2: Ollama bge-m3 품질
- M3: bge-m3 ONNX 측정
- M4: embeddinggemma-300m ANE 성능
- M5: CoreML 직접 실행 성능
- M6: `make eval-ab` — Score Boosting A/B
- M7: Metadata Enrichment 비용/품질
- M8: file_header chunk 매칭 빈도

### 5.5 설계 의존 (블록됨)

- B1: Symbol ID 정규화 — CKG repo와 합의 필요
- B2: LLM Prefix 실제 구현 — LLM provider 결정
- B3: Sensitive Filter 패턴 정의
- B4: Cross-encoder reranker — 모델 선정

---

## 6. 권장 다음 세션 시작 순서

```bash
# 1. 환경 확인
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector
git pull
make build && make test

# 2. 이 문서 + 계획서 + 도구 문서 검토
cat docs/session-handoff-2026-05-29.md      # (이 파일)
cat docs/plan-2026-05-29-ckv-refactor.md
cat docs/mcp-tools.md

# 3. CKS repo 상태 확인
cd ../code-knowledge-system
git pull
ls docs/
cat docs/integrated-workplan-2026-05-27.md  # CKS의 실제 계획서

# 4. 다음 작업 선택 (위 §5 참조)
#    - 최우선: CKS-1 (ckvclient에 CKV 신규 도구 추가)
#    - 빠른 정리: CKV-Q1 + CKV-Q3 (30분 ~ 1시간)
```

---

## 7. 주의 사항 (이번 세션에서 학습)

### 7.1 Make 우선 사용

이 repo는 `make build/test/lint/fmt`이 SSoT. 직접 `go build/test` 호출 금지. 이유: `make lint`만 golangci-lint를 실행, 직접 `go vet`은 누락.

기록: `memory/use_make_targets.md`.

### 7.2 lint baseline

`make lint`는 errcheck 50건 + 잡다 5건의 기존 baseline 존재. 이는 프로젝트 전반의 `defer x.Close()` 컨벤션 (수정하지 않음). 신규 작업 후 카운트 그대로면 회귀 없음.

### 7.3 빌드 산출물 gitignore

`ckv-stablenet/`, `ckv-*-data/` 가 `.gitignore`에 있음. V1 검증 후 데이터 디렉토리는 자동 제외.

### 7.4 커밋 메시지 스타일

영어 + 요약 + 개발 진행 상태 용어(WIP/Phase/디버깅/wait) 금지 + attribution 줄 금지. 기록: `memory/commit_message_style.md`.

---

## 8. 미해결 토론 / 의사결정 대기

| 항목 | 결정 필요 | 막힘 |
|------|----------|------|
| 4.1 stablenet.yaml 보강 카테고리 5개 (accounts/evm/rawdb/types/rlp) | 사용자 승인 후 진행 가능 | - |
| 4.2 Tier 3 휴리스틱 확장 범위 | 구현 진행만 결정되면 됨 | - |
| 5.1 CKS-3 composer 활용 방식 | 별도 설계 세션 필요 | CKS-1 선행 |
| 5.3 CKV envelope 확장 3건 | CKS-1 진행 중 같이 결정 | - |
| 5.5 LLM Prefix provider 선택 | Claude / OpenAI / Ollama | 비용·프라이버시 결정 |
| CKS-1 신규 도구 응답 타입 | CKV `InvariantHit` / `ConventionHit` 재사용 vs 별도 정의 | - |

---

## 9. 핵심 파일 인덱스

코드 (CKV repo):

- `pkg/types/chunk.go` — Chunk + 메타데이터 타입
- `internal/policy/loader.go` — yaml 로더 + glob 매처
- `internal/invariant/extractor.go` — 3-tier 추출기
- `internal/convention/stats.go` — AST 통계
- `internal/embed/cache/lru.go` — LRU 캐시
- `internal/query/explain.go` — explain_match 로직
- `internal/query/service_keyword.go` — BM25 in-memory 인덱스
- `internal/store/sqlitevec/migrate.go` — 마이그레이션 러너
- `pkg/mcp/server.go` — MCP 도구 등록 (15개)

데이터/설정:

- `policy/stablenet.yaml` — 13 카테고리 정책
- `internal/store/sqlitevec/migrations/*.sql` — 4 마이그레이션

문서:

- `docs/plan-2026-05-29-ckv-refactor.md` — Schema-First 계획서
- `docs/mcp-tools.md` — 15 MCP 도구 스키마
- `docs/SCHEMA.md` — DB 스키마 + 마이그레이션 정책
- `docs/cks-design-2026-05-29.md` — **hypothetical** (정정 필요, §2.4 참조)
- `docs/session-handoff-2026-05-29.md` — **이 파일**

코드 (CKS repo):

- `internal/ckvclient/interface.go` — CKV 클라이언트 인터페이스 (확장 대상)
- `internal/mcp/*.go` — MCP 핸들러
- `internal/composer/` — Stage 1/2 파이프라인
- `internal/vocab/`, `internal/inventory/` — 도메인 지식

---

## 10. 참조 / 컨텍스트 외부

- 이전 세션 종료: `f69afe8 docs: planning snapshot for 2026-05-26 session end`
- CKV ADR 디렉토리: `docs/adr/`
- CKS 가장 최근 핵심 문서: `code-knowledge-system/docs/integrated-workplan-2026-05-27.md`
- D3 아키텍처 분리 합의: 이번 세션 초반 토론 (CKV semantic+keyword / CKG graph / CKS orchestrate)

---

이 문서는 작업 진행 시 갱신될 수 있다. 다른 세션이 큰 작업을 진행하면 새 핸드오프 문서를 만든다 (이 파일은 archive).
