# Session Handoff — 2026-05-22 to 2026-05-23

다음 세션이 0초에 진입할 수 있도록 본 세션의 진행 상태와 잔여 작업을
한 문서에 정리. 본 문서는 *상태 스냅샷* 이며, 영구 spec / decision
은 다음 문서를 참조한다.

- **결정 + 5 Phase 전체 spec**: [`docs/evaluation-design-2026-05-22.md`](./evaluation-design-2026-05-22.md)
- **잔여 작업 inventory (Single source of truth)**: [`docs/backlog.md`](./backlog.md)
- **아키텍처 그래프**: [`docs/ARCHITECTURE.md`](./ARCHITECTURE.md)
- **스키마 contracts**: [`docs/SCHEMA.md`](./SCHEMA.md)

---

## 1. 세션 결산 요약

| | |
|---|---|
| 본 세션 범위 | 2026-05-22 ~ 2026-05-23 |
| 본 세션 commits | 9 (코드 5 + 문서 4) |
| 본 세션 완료 작업 | Phase 1, Phase 3, Wave A1 (NEW-5), Wave A2 (NEW-1 + NEW-8) |
| 잔여 작업 | NEW-2, NEW-4, NEW-9, NEW-3, NEW-6, NEW-7 + Phase 2/4/5 |
| 차단 결정 | D1 (BM25 영구 위치) — 측정 후 결정. 임시: 3-leg BM25 |
| 차단 안 함 | Wave B (NEW-2 + NEW-4) 즉시 진입 가능 |

### 본 세션 commit 목록 (역시간 순)

```
95e0ae3  docs(eval-design): record commit hash 3f4483c for NEW-8
3f4483c  feat(glossary): auto-extract korean->english aliases from markdown (NEW-8)
ba5ba96  feat(query): --alias vocabulary bridge (NEW-1)
492eea5  docs(eval-design): mark NEW-5 complete (commit c005e04)
c005e04  test(fixture): expand PR-regression corpus from 4 to 12 entries (NEW-5)
31fff21  docs: record commit hash 69e148a for Phase 3
69e148a  feat(eval): hallucination detection framework (Phase 3, D5-A/B)
6c08190  docs: record commit hash 2f6f215 for Phase 1
2f6f215  feat(query): five sub-spans for query path (Phase 1)
867b199  docs: research evaluation method for go-stablenet target corpus
```

`867b199` 이전 commits 는 이전 세션 (Tier 1, Phase A/D.1, ARCHITECTURE,
SCHEMA, ADR, hallucination 도입 등) — backlog.md 변경 이력 참조.

---

## 2. 결정 상태 스냅샷

| ID | 결정 | 상태 | 출처 |
|---|---|---|---|
| **D1 — BM25 영구 위치** | D1-A/B/C/D 모두 보류, **3-leg BM25 임시** 후 측정 → ADR-006 결정 | 보류 | 사용자 답변 §10.9 |
| **D6 — Target corpus** | **D6-skills 기반** (stable-net 고유 ~80 files + glossary + workflows + PR corpus) | 확정 | 사용자 답변 §10.10 |
| **D2 — BM25 통합 시점** | D2-A: store.Search 이후, threshold 이전 | 권장 (자동) | §3.2 |
| **D3 — BM25 corpus** | D3-B: signature + symbol_name | 권장 (자동) | §3.3 |
| **D4 — Footprint sub-event 구조** | 5 sub-span (NEW-9 시 6번째 query.bm25.rerank 추가) | ✅ 적용됨 | Phase 1 (`2f6f215`) |
| **D5 — Hallucination 검증** | D5-A + D5-B + D5-D (D5-D 는 Phase 4 함께) | A/B 적용됨, D 잔여 | Phase 3 (`69e148a`) |

> **D1 임시 정책**: NEW-9 가 `internal/query/bm25/` 를 *임시* 로 도입.
> ADR-003 (vector-only) 의 supersede 는 측정 결과 후 ADR-006 으로 봉인.
> 코드 주석 + ADR-006 draft 에 "임시" 명시 필요.

---

## 3. 본 세션 완료 작업 — 핵심 변경 사항

### Phase 1 — query path footprint 세분화 (`2f6f215`)

기존 단일 `query.search` span 을 5 sub-span 으로 분해:

```
query.search                  -- top-level
  ├─ query.embed              -- dim, embed_intent_hash, alias_applied
  ├─ query.store.search       -- k_overfetch, candidates_out, top_chunk_id, top_score
  ├─ query.threshold.drop     -- threshold, candidates_in/out, dropped
  ├─ query.citation.enforce   -- candidates_in/out, dropped, stale, src_root
  └─ query.density.adjust     -- budget_tokens, tier_full/sig5/sig_only
```

**부수 fix**: `internal/footprint` profile aggregator 가 `.done` suffix
기반 필터링 (이전엔 `latency_ms > 0`) — sub-ms 연산도 count 집계.

### Phase 3 — Hallucination 검증 framework (`69e148a`)

- `internal/query/hallucination.go` — `VerifyHit`, `VerifyResponse`
- 3 failure modes: `file_missing` / `out_of_range` / `snippet_not_found`
- Whitespace 정규화 (tab/space cosmetics false-positive 회피)
- `internal/eval/score.go` — `PerQuery.HallucinationCount/Reason` +
  `Aggregate.HallucinationRate/Hits/TotalHits`
- `Score(q, resp, k, srcRoot)` 시그니처 (srcRoot 추가)
- CLI: `ckv eval --src <path>` 로 활성, `--max-halluc <rate>` CI gate

**Smoke**: N=50 fixture × top-K=5 = 250 hits → halluc_rate **0.000**.

### NEW-5 — PR-regression fixture 4 → 12 (`c005e04`)

8 신규 stable-net 고유 영역 fix PR:
`pr77 / pr75 / pr73 / pr67 / pr63 / pr58 / pr56 / pr55`.

각 entry 신규 필드 (모두 optional, legacy 4건 영향 0):
```yaml
intent_ground_truth: |
  PR title + Background 첫 문장
changed_symbols:
  - Function.Method
  - Symbol.Method
category: gas_policy | consensus_wbft | genesis_governance | ...
```

`prregress.Entry` struct 확장 (3 필드 추가). NEW-4 (E1/E2/E3 metric)
가 이 필드를 입력으로 사용 예정.

### NEW-1 — `--alias` vocabulary bridge (`ba5ba96`)

신규 `internal/query/expand.go`:
- `type AliasMap map[string][]string`
- `LoadAliasMap(path) (AliasMap, error)` — `aliases:` 키 YAML 파싱
- `ExpandQuery(intent, AliasMap) string` — deterministic sort 후 brackets-tagged 추가

Engine.Search 통합:
- `Options.Aliases AliasMap` 필드
- 알리아스 적용 시 `embed_intent_hash` 가 원본 `intent_hash` 와 다름
  → footprint 에서 분리 가시화
- `query.search` / `query.embed` span 에 `alias_applied` (0/1) 추가

CLI: `ckv query --alias <yaml-path>`
MCP: `cks.context.semantic_search` 의 `alias_path` arg

**Smoke** (mock embedder, testdata/sample, glossary
`"TCP socket": [listener, net.Listen]`):
- alias OFF: `Server.Listen` rank=2 score=0.438
- alias ON: **`Server.Listen` rank=1 score=0.511**

### NEW-8 — Glossary loader (`3f4483c`)

신규 `internal/glossary/` 패키지:
- `Extract(root)` — `.md` / `.markdown` 트리 walker
- `ExtractLine(line, accum)` — 라인 단위 (테스트 친화적)
- `WriteYAML(w, aliases)` — sorted output (diff-friendly)

v1 패턴 2종:
1. Markdown table row `| <한국어 키> | <영문 값> |`
2. Inline parenthetical `<한국어> (<English>)`

핵심 휴리스틱:
- `lastKoreanPhrase`: 문장 경계 (`)` / `.` / `,` / `;` / `:` / `?` / `!`
  / `\n`) 까지 backward scan, 단일 음절 particle 제거, 3 token cap.
- `isMarkdownDecorationKey`: heading / quote / code-comment / list
  마커로 시작하는 key drop.
- 60자 초과 value, pure-digit value, hangul-only value drop.

신규 CLI: `ckv glossary extract --src <dir> --out <yaml>`

**Smoke**: stable-net `.claude/docs/` (4 markdown) → **73 aliases** 추출.
예시:
```yaml
"합의 알고리즘": [WBFT, Weemix Byzantine Fault Tolerance]
"Go 모듈 경로": [github.com/ethereum/go-ethereum]
"WBFT 합의 엔진": [QBFT 기반]
"Solidity 소스": [systemcontracts/solidity/v{N}/*.sol]
```

⚠️ **v1 의도된 한계**: 명세 (§10.4.1) 에 명시 — 자동 추출은 출발점.
사용자 큐레이션 전제. 명시:
- 일부 keys 가 leading 단어 (예: `본 시스템에서 검증인`) 포함 — review 시 trim
- markdown decoration filter 후 noise 17% 감소 (90→73), but 잔여 있음

---

## 4. 신규 surface 요약 (다음 세션이 import / 호출 가능)

### Go API (consumers)

```go
// internal/query
type AliasMap map[string][]string
func LoadAliasMap(path string) (AliasMap, error)
func ExpandQuery(intent string, aliases AliasMap) string

type Options struct {
    // ... 기존 필드 ...
    Aliases AliasMap                  // NEW-1
}

type Hit struct {
    // ... 기존 필드 ...
    Density       DensityTier  `json:"density,omitempty"`        // Phase 1 (이전 세션)
    StaleCitation bool         `json:"stale_citation,omitempty"` // B4 (이전 세션)
}

func VerifyHit(h Hit, srcRoot string) HallucinationResult        // Phase 3
func VerifyResponse(resp *Response, srcRoot string) (verdicts []HallucinationResult, hallucinated int)

// internal/glossary
func Extract(root string) (map[string][]string, error)
func ExtractLine(line string, accum map[string]map[string]struct{})
func WriteYAML(w io.Writer, aliases map[string][]string) error

// internal/eval/prregress
type Entry struct {
    // ... 기존 필드 ...
    IntentGroundTruth string   `yaml:"intent_ground_truth,omitempty"`   // NEW-5
    ChangedSymbols    []string `yaml:"changed_symbols,omitempty"`       // NEW-5
    Category          string   `yaml:"category,omitempty"`              // NEW-5
}

// internal/eval
type PerQuery struct {
    // ... 기존 필드 ...
    HallucinationCount  int    `json:"hallucination_count,omitempty"`  // Phase 3
    HallucinationReason string `json:"hallucination_reason,omitempty"`
}
type Aggregate struct {
    // ... 기존 필드 ...
    HallucinationRate float64 `json:"hallucination_rate,omitempty"` // Phase 3
    HallucinationHits int     `json:"hallucination_hits,omitempty"`
    TotalHits         int     `json:"total_hits,omitempty"`
}
func Score(q Query, resp *query.Response, k int, srcRoot string) PerQuery
```

### CLI

```
ckv query --alias <yaml-path>           # NEW-1
ckv eval --src <path>                   # Phase 3 (hallucination 자동)
ckv eval --max-halluc <rate>            # Phase 3 (CI gate)
ckv glossary extract --src <dir> [--out yaml]   # NEW-8
```

### MCP `cks.context.semantic_search` arguments

```
intent          string  (required, 기존)
k               number  (기존)
language        string  (기존)
path            string  (기존)
symbol_kind     string  (기존)
commit_hash     string  (이전 세션 B2)
trace_id        string  (이전 세션 B5)
dry_run         bool    (이전 세션 B5)
alias_path      string  (NEW-1) ← 본 세션 추가
budget_tokens   number  (기존)
threshold       number  (기존)
examples_k      number  (기존)
```

### Footprint sub-spans (operator 가 grep / aggregate 가능)

```
query.search          alias_applied, intent_hash, top_file, hits
query.embed           dim, embed_intent_hash, alias_applied
query.store.search    k_overfetch, candidates_out, top_chunk_id, top_score
query.threshold.drop  threshold, candidates_in/out, dropped
query.citation.enforce  candidates_in/out, dropped, stale, src_root
query.density.adjust  budget_tokens, tier_full/sig5/sig_only, tokens_used
```

---

## 5. 잔여 작업 — Wave 단위

### Wave B — 평가 framework 강화 (D1/D6 무관 즉시 진입 가능)

#### NEW-4 — Multi-stage E1/E2/E3 메트릭 (`~250 LOC`)

**위치**: `internal/eval/prregress/score.go` 확장

**산출**:
```go
func IntentScore(plan, prTitle string) float64                       // E1
func SymbolF1(planSymbols, truthSymbols []string) (p, r, f1 float64) // E2 신규
func PlanStepsScore(planSteps, commitMessages string) float64        // E3 분리
// 기존 JudgeScore 유지 (legacy 호환)
```

**입력**: NEW-5 가 추가한 `Entry.IntentGroundTruth` + `Entry.ChangedSymbols`

**부수 작업**:
- `Runner.Run` 에 새 metric 호출 통합
- `Result.Score` 구조에 `IntentScore` / `SymbolF1{P,R,F1}` / `PlanStepsScore` 추가
- 인덱스 빌드 자체는 무관 (eval 측만)

**Entry condition**: NEW-5 완료 ✅

#### NEW-2 — `--record` interactive fixture 모드 (`~150 LOC`)

**위치**: `cmd/ckv/eval.go` 에 새 mode

**산출**:
```bash
ckv eval --record --fixture ./testdata/stablenet/queries.yaml \
  --out ./ckv-data-stablenet --src <path>
```

**흐름**:
```
사용자 입력: "거버넌스로 가스팁 바꿨는데 트랜잭션이 거절돼"
ckv:
  → top-5 결과 표시 (file:line + snippet)
  → prompt: "정답은? (1-5, 'none')"
  → 사용자 입력 → fixture YAML append (new entry)
```

**부수 작업**:
- `testdata/queries.yaml` 의 `Query` struct 에 `expected_chunks []string`,
  `recorded_via string`, `timestamp string` optional 필드 추가
- Interactive I/O 패턴 — stdin mock 으로 test 작성

**Entry condition**: 없음. NEW-5 / NEW-4 와 병행 가능.

### Wave C — Engine.Search 핵심 변경 (위험 큼, A/B 검증 필수)

#### NEW-9 — chunk-aware BM25 임시 (`~250 LOC`)

**위치**: `internal/query/bm25/` 신설

**핵심 결정**:
- CKG `pkg/bm25.Scorer` 를 *복사 / 어댑터* 로 재사용 (코드 중복 회피)
- D2-A: store.Search 이후, threshold 이전 rerank
- D3-B: corpus = symbol_name + signature (첫 라인). chunk.Text 전체는 noise.

**Engine.Search 수정**:
```go
// 새 sub-span query.bm25.rerank 추가 (Phase 1 footprint 위에 6번째)
rerankedHits, bm25Scores := bm25.Rerank(rawHits, intent)
// 다음 single-fingerprint metric: rank_changes, top1_score_delta
```

**Hit 시그니처 확장**:
- `Hit.Score.BM25Score float64 omitempty` (RRF 입력)
- `Hit.Score.HybridRank int` (rerank 후 final rank)

**ADR-006 draft 작성 권장** — "BM25 임시 통합, 측정 후 영구 결정 보류".
ADR-003 supersede 는 **측정 후**.

**테스트 영향**:
- 모든 기존 query eval baseline (recall@K, MRR) 변동 가능
- 회귀 발견 시 임시로 rerank off 옵션 (`Options.DisableBM25Rerank`) 권장

**Entry condition**: 없음 (CKG bm25 source 코드만 reference). 단,
회귀 측정 위해 Phase 1 (footprint) 가 이미 있어야 → ✅ 충족.

### Wave D — PR-aware pipeline (분리 세션 권장)

#### NEW-3 — PR corpus indexing (`~400 LOC`)

**위치**: `internal/parse/prdoc/` 신설

**산출**:
- 새 `ChunkKind`: `ChunkPRBackground` / `ChunkPRSolution` / `ChunkCommitMessage`
- `internal/parse/prdoc/parser.go` — PR description 섹션 분할
- `ckv build --include-pr-history --pr-since YYYY-MM-DD` flag
- prregress `fetcher.go` 의 gh CLI 호출 코드 재사용

**위험**:
- `pkg/types.ChunkKind` enum 확장 → 모든 switch 자리 (chunker, Stats,
  sqlite-vec store, eval render, density) 영향
- `Citation.File` 의 의미 재정의 (PR description chunk 는 file path 가
  의미가 다름) — 처리 방식 검토 필요
- gh CLI dependency 신규

**Entry condition**: Wave A/B 안정 후. 분리 세션 권장 — 회귀 격리.

#### NEW-6 — Symbol-level PR breadcrumb (`~80 LOC`)

**위치**: `pkg/types.Chunk` schema 확장

**산출**:
```go
type Chunk struct {
    // ... 기존 ...
    RecentPRs []PRRef `json:"recent_prs,omitempty"`  // R12
}
type PRRef struct {
    Number      int
    Title       string
    BaseSHA     string
    HeadSHA     string
    Summary     string
    MergedAtUTC time.Time  // *** temporal slicing key ***
}
```

**위험**:
- DB column 추가 (sqlite-vec store `initSchema` 마이그레이션)
- manifest `schema_version` bump 결정 필요 (additive 면 unbump)
- `EnforceCitationsAt`, density, hallucination 모든 Chunk reader 영향
- `SCHEMA.md` 갱신

**Entry condition**: NEW-3 완료 후 (PR corpus 가 source data).

#### NEW-7 — `cks.context.related_changes` MCP tool (`~150 LOC`)

**위치**: `pkg/mcp/server.go` 에 새 handler

**산출**:
```
input: symbol or file path
output: PR refs that touched this symbol/file (sorted by MergedAtUTC)
```

**위험**: 낮음 — 새 handler 만. 기존 tool 변경 없음.

**Entry condition**: NEW-3 + NEW-6 완료.

---

## 6. 다음 세션 진입 워크플로우

### Step 1: 본 문서 정독 (2분)

### Step 2: 작업 트리 상태 확인
```bash
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector
git log --oneline | head -10
git status
go test ./... 2>&1 | grep -E "^(ok|FAIL)"
```

기대 결과:
- HEAD = `95e0ae3` (또는 더 최신)
- working tree clean
- 23 packages all `ok`

### Step 3: 진입할 Wave 선택

**권장**: Wave B → NEW-4 (Multi-stage 메트릭). 이유:
- NEW-5 의 새 fixture 필드를 입력으로 사용 — 자연스러운 연결
- D1/D6 결정 무관 (즉시 진입 가능)
- E1/E2/E3 메트릭 없으면 Wave C (BM25) 측정 결과를 *어떤 stage* 가
  개선됐는지 알 수 없음 → NEW-4 가 NEW-9 측정의 전제

**대안 1**: NEW-2 (`--record`). interactive fixture 성장 인프라.
NEW-4 와 병행 가능 (코드 위치 다름).

**대안 2**: Wave C (NEW-9) 바로 진입 — D1 임시 결정 따라 BM25 도입.
단 메트릭 분해 없이 측정 결과 해석 어려움.

### Step 4: 측정 실행 (모든 작업 commit 후)

```bash
# 1. testdata/sample 회귀 확인 (기존 baseline 동일해야 함)
rm -rf /tmp/ckv-regress && \
  go run ./cmd/ckv build --src testdata/sample --out /tmp/ckv-regress --embedder=mock && \
  go run ./cmd/ckv eval --fixture testdata/queries.yaml --out /tmp/ckv-regress --src testdata/sample --json | \
  python3 -c "import json,sys; a=json.load(sys.stdin)['aggregate']; print(f'r@5={a[\"recall_at_5\"]:.3f} MRR={a[\"mrr\"]:.4f} halluc={a[\"hallucination_rate\"]:.3f}')"

# 기대 (현 baseline): r@5=0.740 MRR=0.4937 halluc=0.000
```

### Step 5: 잔여 commits 진행 시 메모리 파일 갱신

본 세션이 사용자 글로벌 instruction 을 따라 memory 갱신:
- `~/.claude/projects/-Users-wm-it-22-00661-Work-github-tools-code-knowledge-vector/memory/`
- 신규 작업 완료 시 commit_message_style.md 패턴 (영어 / 요약 / no
  attribution / no WIP 용어) 유지

---

## 7. 알려진 한계 / 후속 검토

| 한계 | 위치 | 후속 |
|---|---|---|
| Glossary v1 leading-determiner 캡쳐 (`본 시스템에서 검증인`) | `internal/glossary/extract.go::lastKoreanPhrase` | 사용자 review 후 trim. v2 에서 determiner 화이트리스트 가능 |
| Mock embedder 의 sub-ms latency → profile 0ms aggregation | `internal/footprint/footprint.go` | bgeonnx 실측 시 자연 해소. 코드 변경 불필요 |
| Hallucination check 가 whitespace 정규화로 너무 관대 | `internal/query/hallucination.go::stripBlanks` | 측정에서 false-negative 발견 시 strict mode 추가 |
| 12 entries fixture 중 `pr67` `pr56` 의 `changed_symbols` 는 descriptive (실제 Go symbol 아님) | `testdata/prs.yaml` | NEW-4 의 Symbol F1 metric 시 normalization 필요 |
| ADR-003 supersede 보류 — BM25 임시 도입 시점에 ADR-006 draft 필요 | `docs/adr/` | NEW-9 작업과 함께 |

---

## 8. 외부 의존 (CKV scope 외 — 정보용)

| ID | 책임 | 본 세션 상태 |
|---|---|---|
| CKG `pkg/bm25.Scorer` | CKG repo | NEW-9 의 reference. 본 세션 미사용 |
| CKG PR-aware A 옵션 | CKG repo | NEW-6 와 데이터 정합 — 분리 세션 |
| cks-T1-D1~D5 | CKS repo | Stage C 영역 |
| ANE 친화 embedder (EmbeddingGemma) | HF 차단 | throughput 회복 후 작업 가능 |

---

## 9. 변경 이력

| 일자 | 변경 |
|---|---|
| 2026-05-23 | 초안. 본 세션 (Phase 1, Phase 3, NEW-5, NEW-1, NEW-8) handoff 정리. 잔여 Wave B/C/D + entry conditions + 다음 세션 진입 워크플로우 명세. |
