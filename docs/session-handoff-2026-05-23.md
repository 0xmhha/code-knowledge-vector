# Session Handoff — 2026-05-22 to 2026-05-23

다음 세션이 0초에 진입할 수 있도록 본 세션의 진행 상태와 잔여 작업을
한 문서에 정리. 본 문서는 *상태 스냅샷* 이며, 영구 spec / decision
은 다음 문서를 참조한다.

- **결정 + 5 Phase 전체 spec**: [`docs/evaluation-design-2026-05-22.md`](./evaluation-design-2026-05-22.md)
- **잔여 작업 inventory (Single source of truth)**: [`docs/backlog.md`](./backlog.md)
- **아키텍처 그래프**: [`docs/ARCHITECTURE.md`](./ARCHITECTURE.md)
- **스키마 contracts**: [`docs/SCHEMA.md`](./SCHEMA.md)

> **다른 머신에서 시작하는 경우 §0 부터 진행.**
> 동일 머신에서 이어가는 경우 §6 으로 점프.

---

## 0. Onboarding — 다른 머신에서 시작할 때

### 0.1 전제 — 시스템 prerequisite

| 항목 | 버전 / 비고 | 검증 |
|---|---|---|
| **Go toolchain** | 1.25.0 이상 (`go.mod` 의 `go 1.25.0` 선언과 호환) | `go version` |
| **git** | 2.x 이상 | `git --version` |
| **gh CLI** | 2.x 이상. prregress eval 이 PR 메타 조회에 사용. `gh auth login` 으로 GitHub 인증 완료 필요. | `gh auth status` |
| **macOS 또는 Linux** | macOS 가 primary (CoreML EP 작업). Linux 는 CPU-only baseline 만 검증됨. Windows 미검증. | `uname -s` |
| **C toolchain** | sqlite-vec + tokenizers cgo 빌드에 필요. macOS: `xcode-select --install`. Linux: `build-essential`. | `cc --version` |
| **libonnxruntime** | bgeonnx embedder 사용 시. CPU-only baseline 은 mock embedder 로 우회 가능. | `ldconfig -p | grep onnxruntime` (Linux) / `brew list onnxruntime` (macOS) |
| **Python 3** | smoke test / 측정 헬퍼에서 JSON 파싱에 사용. 빌드 자체엔 불필요. | `python3 --version` |

### 0.2 ckv repo clone + 검증

```bash
# 1. Clone
git clone <CKV_REPO_URL> code-knowledge-vector
cd code-knowledge-vector

# 2. HEAD 가 본 핸드오프 작성 시점 commit 인지 확인
git log --oneline | head -1
# 기대: 0d2a8e4 또는 그 이후 (이 핸드오프 commit hash 는 §1 commit ledger 참조)

# 3. 전체 빌드 + 테스트 (cgo 포함, 약 30-60s)
go build ./...
go test ./...
# 기대: 23 packages 모두 'ok'. 단 fail 이 있으면 §0.3 의 missing prereq 확인.

# 4. 본 핸드오프 끝의 §6 Step 2 baseline smoke 실행 (mock embedder 만)
rm -rf /tmp/ckv-onboarding && \
  go run ./cmd/ckv build --src ./testdata/sample --out /tmp/ckv-onboarding --embedder=mock && \
  go run ./cmd/ckv eval --fixture ./testdata/queries.yaml --out /tmp/ckv-onboarding --src ./testdata/sample --json | \
  python3 -c "import json,sys; a=json.load(sys.stdin)['aggregate']; print(f'r@5={a[\"recall_at_5\"]:.3f} MRR={a[\"mrr\"]:.4f} halluc={a[\"hallucination_rate\"]:.3f}')"
# 기대: r@5=0.740 MRR=0.4937 halluc=0.000
```

이 4 단계가 모두 통과하면 ckv 측 측정 인프라는 완전 동작. PR-regression /
target corpus 측정은 §0.4 / §0.5 추가 setup 후.

### 0.3 외부 의존: go-stablenet target repo

PR-regression eval (`testdata/prs.yaml`, 12 entries) + stable-net 고유
영역 corpus 빌드 (D6) 가 동작하려면 stable-net repo 가 *별도 디렉토리* 에
clone 되어 있어야 함.

```bash
# 1. Clone stable-net 어디든 (절대경로 자유)
git clone <STABLENET_REPO_URL> /path/of/your/choice/go-stablenet
cd /path/of/your/choice/go-stablenet

# 2. fixture (testdata/prs.yaml) 의 12 base_sha 가 모두 stable-net
#    의 현재 working tree 에서 reachable 한지 검증.
#    한 SHA 라도 unknown 이면 stable-net 측 dev 브랜치를 fetch 하거나
#    fixture base 가 가리키는 branch 로 checkout:
for sha in \
  aa28927fb12048a59ac34608702eef5e1be90931 \
  88fe52145e9dc07d6bad2b861468bcbbd271de60 \
  c55bf2a86e21a12bb72126ac6eb05c1974e594b8 \
  319b84d113c5e34b35bdc24899e3b0b9609dd751 \
  0bf2f4d1bfeb6605006d556957ef8c045d8f8ed8 \
  e5a6e9e14c1e1d225c341b55798f31cd07b0bfcd \
  8b895799e320ce762d441b39448ca27499b4a348 \
  c37ae123a15cb6417e94bf3943bfa2647ebff6b8 \
  ad0122af042d49022052a9783b3086d40d308db8 \
  db7c4b43cc63b322d54d59dd1633f7398402c745 \
  6e41d966316b51a4e7f17b1ab82f5e5c293e2f33 \
  8d7930a48a31601f4456fb34d526dd4a573d38e4 \
; do
  git cat-file -e "$sha^{commit}" 2>/dev/null \
    && echo "✓ $sha" \
    || echo "❌ $sha (missing — fetch dev or checkout fixture branch)"
done
# 기대: 12 개 모두 ✓. 하나라도 ❌ 면 prregress eval 부분 fail.

# 3. .claude/docs 가 있는지 확인 (NEW-8 glossary extract 가 사용)
ls .claude/docs/CLAUDE_DEV_GUIDE.md
```

stable-net repo URL 은 본 문서에 명시하지 않음 (private repo 가능성).
사용자가 자기 환경 access path 로 clone 한 후 다음 환경 변수로 등록:

```bash
# ~/.bashrc 또는 ~/.zshrc 에 추가 (영구) 또는 세션 한정 export
export CKV_STABLENET_PATH=/path/of/your/choice/go-stablenet
```

**이 env var 가 set 되어야 `testdata/prs.yaml` 의 12 entries 의
`source_path` (= `${CKV_STABLENET_PATH}`) 가 resolve 됨.** unset 시
LoadFixture 가 "source_path placeholder ${CKV_STABLENET_PATH} expanded
to empty" 로 friendly error.

검증:
```bash
echo "$CKV_STABLENET_PATH" && test -d "$CKV_STABLENET_PATH" && echo "✓ stable-net path OK" || echo "❌ unset or directory missing"
```

### 0.4 외부 의존: bgeonnx embedder 모델

mock embedder 만 사용한다면 §0.4 skip. bgeonnx 로 *실 측정* 하려면:

```bash
# 1. 모델 캐시 디렉토리 생성
mkdir -p ~/.cache/ckv/models/bge-large-en-v1.5

# 2. ONNX 모델 + tokenizer 파일 다운로드
#    공식 출처: BAAI/bge-large-en-v1.5 (HuggingFace)
#    회사 정책상 HF 직접 접근이 차단된 환경이라면 offline 전송 필요.
#    필요 파일:
#      - model.onnx                 (1024-dim BERT)
#      - tokenizer.json
#      - config.json
#      - special_tokens_map.json
#      - vocab.txt
#    경로: ~/.cache/ckv/models/bge-large-en-v1.5/<각 파일>

# 3. 모델 로딩 검증
go run ./cmd/ckv build --embedder=bgeonnx --src ./testdata/sample --out /tmp/ckv-bgeonnx-smoke
# 기대: build.done event 출력, embedder=bge-large-en-v1.5
```

CoreML EP 관련 env vars (macOS 한정):

| Env var | 의미 | 기본값 |
|---|---|---|
| `CKV_DISABLE_COREML` | `1` 이면 CoreML EP off, CPU-only fallback | unset (CoreML 사용 시도) |
| `CKV_COREML_MODEL_FORMAT` | `MLProgram` (기본, ADR-005) 또는 `NeuralNetwork` (legacy, 비호환) | `MLProgram` |
| `CKV_STATIC_SHAPES` | `1` 이면 ANE cache 안정화 — bge-large 권장 | unset |
| `CKV_COREML_CACHE_DIR` | CoreML 컴파일 캐시 경로 override | `~/.cache/ckv/coreml/<model>` |

자세한 trade-off 는 [`docs/adr/005-coreml-mlprogram-static-shapes.md`](./adr/005-coreml-mlprogram-static-shapes.md) 참조.

### 0.5 환경 변수 reference

본 세션 종료 시점까지 도입된 모든 ckv env vars 한 곳에 정리:

| Env var | 영향 | 기본값 | 출처 |
|---|---|---|---|
| `CKV_STABLENET_PATH` | `testdata/prs.yaml` 의 `${CKV_STABLENET_PATH}` placeholder resolve | (unset 시 prregress eval 실패) | 본 commit |
| `CKV_LOG_LEVEL` | slog 레벨 (`debug`/`info`/`warn`/`error`) | `info` | B8 (2026-05-21) |
| `CKV_DISABLE_SECRET_FILTER` | `1` 이면 `.env`/`*.pem`/... 차단 우회 (테스트 전용) | unset (filter active) | B9 (2026-05-21) |
| `CKV_DISABLE_CONTEXTUAL_PREFIX` | `1` 이면 Phase D.1 prefix off (A/B 측정용) | unset (prefix on) | #6 (2026-05-21) |
| `CKV_MEM_GUARD` | `off` 이면 사전 메모리 체크 skip | active | A1 (2026-05-20) |
| `CKV_MEM_GUARD_LOW_MB` | adaptive batch trigger threshold | 1024 MB | A1 (2026-05-20) |
| `CKV_DISABLE_COREML` | `1` 이면 CoreML EP off | active 시도 | A1 (2026-05-20) |
| `CKV_COREML_MODEL_FORMAT` | `MLProgram` / `NeuralNetwork` | `MLProgram` | A1 (2026-05-20) |
| `CKV_STATIC_SHAPES` | ANE compile cache 안정화 | unset | A1 (2026-05-20) |
| `CKV_COREML_GPU_FP16` | GPU FP16 accumulation 활성 | unset | A1 (2026-05-20) |
| `CKV_COREML_UNITS` | CoreML execution units (`ALL`/`CPUAndNeuralEngine`/`CPUOnly`) | `ALL` | A1 (2026-05-20) |
| `CKV_COREML_CACHE_DIR` | 컴파일 캐시 위치 override | `~/.cache/ckv/coreml/<model>` | A1 (2026-05-20) |
| `CKV_ORT_VERBOSE` | `1` 이면 ORT verbose 로그 | unset | A1 (2026-05-20) |
| `CKV_ORT_INTRA_THREADS` | ORT intra-op 스레드 수 | (ORT default) | A1 (2026-05-20) |
| `CKV_ORT_INTER_THREADS` | ORT inter-op 스레드 수 | (ORT default) | A1 (2026-05-20) |

### 0.6 외부 의존성 빠른 체크리스트

```bash
# 모든 외부 의존이 갖춰졌는지 확인하는 한 줄
echo "Go: $(go version | awk '{print $3}') | gh: $(gh --version | head -1 | awk '{print $3}') | stable-net: $([ -d \"$CKV_STABLENET_PATH\" ] && echo OK || echo missing) | bgeonnx-model: $([ -f ~/.cache/ckv/models/bge-large-en-v1.5/model.onnx ] && echo OK || echo missing) | gh-auth: $(gh auth status 2>&1 | grep -q 'Logged in' && echo OK || echo missing)"
```

mock embedder 만 사용한다면 bgeonnx-model 은 missing 이어도 OK.
prregress eval 안 돌린다면 stable-net + gh-auth 는 missing 이어도 OK.
하지만 **Wave B (NEW-4) 의 E1/E2/E3 측정** 이나 **Stage A 1차 측정** 을
하려면 stable-net + gh-auth 둘 다 필요.

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

> **2026-05-26 후속 진행**: **NEW-4 closed** (commit `53964b1`),
> **NEW-9 closed** (commit `57c8821`, default OFF). 본 §1 표는
> 2026-05-23 시점 스냅샷이며, 현재 잔여 = NEW-2 (Wave B 선택),
> NEW-3/6/7 (Wave D, PR-aware pipeline). 자세히 §5 Wave B 의 NEW-4 행,
> §5 Wave C 의 NEW-9 행, §9 변경 이력 (2026-05-26 rows).
>
> **bgeonnx 실측은 보류** — `~/.cache/ckv/models/bge-large-en-v1.5/` 미설치
> (model.onnx 1.34 GB + tokenizer.json 700 KB 필요). 즉시 실행 가능한
> A/B runbook (Step 1 모델 설치 → Step 4 ADR-006 supersede 결정 기준)
> 은 [`evaluation-design §6.4`](./evaluation-design-2026-05-22.md) 참조.

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

#### NEW-4 — Multi-stage E1/E2/E3 메트릭 ✅ 2026-05-26 (commit `53964b1`)

**위치**: `internal/eval/prregress/metrics.go` 신설 + `score.go` / `runner.go` / `fetcher.go` / `types.go` 통합 변경

**산출** (실제 구현):
```go
// internal/eval/prregress/metrics.go (신규)
func IntentScore(plan, reference string) float64                     // E1: token-F1
func IntentCosine(ctx, plan, ref string, e types.Embedder) (float64, error) // E1 optional
func SymbolF1(planSymbols, truthSymbols []string) (p, r, f1 float64) // E2
func PlanStepsScore(planSteps, commitMessages string) float64        // E3
func ExtractPlanSteps(markdown string) string                        // helper
func ExtractPlanSymbols(markdown string) []string                    // helper
// 기존 JudgeScore 유지 (legacy 호환)
```

**입력**: NEW-5 가 추가한 `Entry.IntentGroundTruth` + `Entry.ChangedSymbols` + 신규 `Meta.CommitMessages` (FetchMeta 가 `gh pr view` JSON 에 `commits` 필드 포함하여 한 호출로 수집)

**구현 결정**:
- E1 은 *pure-Go token-F1* (결정론) + *선택적 embedder cosine* (paraphrase 인식, 별도 `IntentCosine` 필드). 둘은 직교 신호로 병행 보고.
- Score struct 의 8 신규 필드 모두 `omitempty` — legacy 4 entries (pr69/pr70/pr72/pr74) 의 JSON 출력 *완전 무변경*.
- `IntentCosine` 만 `RunEntry` 에서 wire (Scorer interface 변경 없이 optional embedder 통합).

**테스트**:
- 24 metric unit test (`metrics_test.go`) + 2 integration test (`score_test.go` 의 `TestScore_PopulatesMultiStageWhenGroundTruthPresent` / `TestScore_MultiStageSilentOnLegacyEntries`)
- prregress 패키지 65 PASS / 0 FAIL
- testdata/sample mock embedder baseline `r@5=0.740 MRR=0.4937 halluc=0.000` 회귀 0

**Wave B 잔여**: NEW-2 (`--record` interactive fixture).
**Wave C unblock**: NEW-9 (BM25 임시) 측정 시 어느 stage 가 개선되는지 분해 가능.

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

#### NEW-9 — chunk-aware BM25 임시 ✅ 2026-05-26 (commit `57c8821`)

**위치**: `internal/query/bm25/` 5 파일 신설 + `pkg/types/search.go` / `internal/query/engine.go` / `cmd/ckv/{query,eval}.go` / `pkg/mcp/server.go` / `internal/eval/eval.go` 통합 변경

**구현된 결정**:
- ckg `pkg/bm25` 의 Okapi BM25 + 코드-aware tokenizer 를 *복사 + attribution* 으로 in-tree 화 (CKV 빌드가 ckg checkout 의존 안 함). 알고리즘 자체는 ckg 가 이미 hand-written / no-third-party 명시.
- **D2-A**: store.Search 직후, threshold.drop 직전. 신규 sub-span `query.bm25.rerank` 가 Phase 1 footprint 의 6번째.
- **D3-B**: corpus = `chunk.SymbolName + chunk.Text 첫 비어있지 않은 줄` (= `BuildCorpusText`). chunk.Text 전체는 noise.
- **Candidate-set BM25**: IDF 를 검색의 candidate set (k×3=30) 위에서 계산 → schema/build 변경 0. ADR-006 에 *bias caveat* 명시.
- **RRF fusion (k=60)** — `1/(60+vector_rank) + 1/(60+bm25_rank)`. tied → stable tiebreak 으로 vector 우선.
- **Default OFF** — `Options.EnableBM25Rerank` opt-in. ADR-003 baseline `r@5=0.740 MRR=0.4937` (mock embedder) **완전 보존**.

**신규 surface**:
- CLI: `ckv query --bm25-rerank`, `ckv eval --bm25-rerank`
- MCP: `cks.context.semantic_search` arg `bm25_rerank: true`
- Go: `query.Options.EnableBM25Rerank`, `eval.Options.EnableBM25Rerank`
- JSON: `Hit.Score.BM25Score` / `Hit.Score.HybridRank` (omitempty → rerank off 시 absent)

**Footprint span** `query.bm25.rerank` 필드:
```
candidates_in / candidates_out / rank_changes / top1_score_delta / top1_chunk_id / disabled
```

**측정 결과 (mock embedder, testdata/sample N=50)**:
| 상태 | r@5 | MRR | halluc |
|---|---|---|---|
| OFF (default) | 0.740 | 0.4937 | 0.000 |
| ON `--bm25-rerank` | **0.840** | 0.4983 | 0.000 |

Mock embedder 의 약한 vector signal 환경에서 BM25 가 +0.10 r@5 보강. **bgeonnx 실측은 별도 세션** — semantic baseline 이 다르므로 mock 의 delta 가 예측력 없음.

**ADR-006 (Proposed)** — `docs/adr/006-bm25-temporary-rerank.md`. ADR-003 (Accepted, vector-only) 의 supersede 는 *측정 후 결정*. 본 commit 은 measurement infrastructure 만 land.

**테스트**: 18 신규 unit test (Okapi regression / tokenizer contract / Rerank 시나리오 / RRF 수학 / Hit mutation isolation / BuildCorpusText / Stats fingerprint). 24 packages 전체 `ok`.

**Wave 잔여 (NEW-9 이후)**: Wave D (NEW-3 / NEW-6 / NEW-7) — PR-aware pipeline. *분리 세션 권장* (schema/migration 영향).

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

다른 머신이면 §0 부터. 동일 머신이면 §2 (결정 상태) → 본 §6.

### Step 2: 작업 트리 상태 확인

```bash
# ckv repo 의 working tree 안에서:
cd <path-to-ckv-clone>      # 다른 머신이면 §0.2 의 clone 위치
git log --oneline | head -10
git status
go test ./... 2>&1 | grep -E "^(ok|FAIL)"
```

기대 결과:
- HEAD = `0d2a8e4` 또는 그 이후
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
# 0. 모든 명령은 ckv repo working tree 내부에서.

# 1. testdata/sample 회귀 확인 (mock embedder, repo-relative paths)
TMP_OUT=$(mktemp -d) && \
  go run ./cmd/ckv build --src ./testdata/sample --out "$TMP_OUT" --embedder=mock && \
  go run ./cmd/ckv eval --fixture ./testdata/queries.yaml --out "$TMP_OUT" --src ./testdata/sample --json | \
  python3 -c "import json,sys; a=json.load(sys.stdin)['aggregate']; print(f'r@5={a[\"recall_at_5\"]:.3f} MRR={a[\"mrr\"]:.4f} halluc={a[\"hallucination_rate\"]:.3f}')" && \
  rm -rf "$TMP_OUT"

# 기대 (현 baseline, mock embedder, N=50): r@5=0.740 MRR=0.4937 halluc=0.000

# 2. PR-regression eval (`testdata/prs.yaml`) 은 §0.3 의
#    CKV_STABLENET_PATH 가 set 되어 있어야 함:
#      export CKV_STABLENET_PATH=/abs/path/to/go-stablenet
#      go run ./cmd/ckv eval --pr-fixture ./testdata/prs.yaml --out "$TMP_OUT"
#    unset 일 경우 LoadFixture 가 "source_path placeholder ... expanded to empty"
#    로 친절한 에러를 출력.
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
| 2026-05-23 (2차) | §0 Onboarding 신설 — 다른 머신에서 시작 가능한 prereq / clone / stable-net access / bgeonnx 모델 / env vars 정리. `testdata/prs.yaml` 의 hard-coded `/Users/...` 절대경로 → `${CKV_STABLENET_PATH}` placeholder 로 변경 + `os.ExpandEnv` 통한 LoadFixture 자동 resolve. §6 Step 2/4 의 명령들도 repo-relative + `$TMP_OUT` 패턴으로 portable 화. |
| 2026-05-23 (3차) | 발견성 + 검증 강화. (a) README.md 최상단에 본 핸드오프 cross-link callout + Documentation 섹션에 "start here" 항목 추가 — 다른 머신이 clone 후 즉시 본 문서 발견 가능. (b) §0.3 의 base SHA reachability 검증을 1 SHA spot-check 에서 12 SHA 전체 loop 로 강화 — 부분 fail 사전 발견. |
| 2026-05-26 | Wave B / NEW-4 closed (commit `53964b1`). §5 Wave B 의 NEW-4 행을 실제 구현으로 갱신 (E1 IntentScore + 선택적 IntentCosine / E2 SymbolF1 / E3 PlanStepsScore + helpers). Score 구조에 8 omitempty 필드 — legacy 4 entries JSON 무변경. `Meta.CommitMessages` 신설 + `FetchMeta` 가 `gh pr view` JSON 의 `commits` 필드 추가 수집. 24 metric unit test + 2 integration test. 다음 진입 권장 = Wave C (NEW-9 BM25) — 본 commit 으로 stage 분해 측정 가능. |
| 2026-05-26 (2차) | Wave C / NEW-9 closed (commit `57c8821`). `internal/query/bm25/` 5 파일 (Okapi + tokenize 는 ckg attribution, candidate-set Rerank + RRF k=60 은 CKV 신규). `HitScore` 에 `BM25Score` / `HybridRank` omitempty. `Engine.Search` 의 6번째 sub-span `query.bm25.rerank` (D2-A 위치). **Default OFF** (`Options.EnableBM25Rerank` opt-in) — handoff §0.2 / §6.4 mock baseline `r@5=0.740 MRR=0.4937` **완전 보존**. BM25 ON 측정 (mock embedder N=50): `r@5=0.840 MRR=0.4983` (+0.10 r@5). ADR-006 (Proposed) 작성; ADR-003 supersede 는 bgeonnx 실측 후. 18 신규 unit test. 다음 권장 = bgeonnx 실측 (별도 세션) 또는 Wave D (NEW-3 PR corpus). |
