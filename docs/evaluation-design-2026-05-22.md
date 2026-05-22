# Evaluation Method Research — Target: go-stablenet (2026-05-22)

이 문서는 사용자 요구 (2026-05-22) 에 대한 **방법 연구** 결과다. 구현
산출물이 아니며, 사용자 결정 후 구체 task 로 분해된다.

---

## 1. 요구사항 (사용자 명시)

1. **대상 프로젝트** — `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`
   - 1,290 Go 파일, ~368K LOC, 563 Solidity 파일, 111 md
   - 빌드 참여: 160 패키지 / 781 Go 파일 (`.claude/docs/BUILD_SOURCE_FILES.md` 명시)
   - go-ethereum fork + WBFT 합의 / systemcontracts / cmd/gstable 고유 영역

2. **코드 이해에 사용된 skill** —
   `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest/.claude`
   - 8 skill (check-reviews / complexity / delta-log / do-review / handoff
     / milestone / pr-review / qr-gate)
   - 5 doc (BUILD_SOURCE_FILES / CLAUDE_DEV_GUIDE / REVIEW_GUIDE / SYSTEM_CONTRACT_FLOW / review-test-result-with-ast)

3. **요구 기능**:
   - (R1) 대상 프로젝트 → vector DB 생성
   - (R2) MCP 로 임의 정보값 입력 → 결과값 반환
   - (R3) 결과값 vs 실 코드 정확도 검증 + **할루시네이션 감지**
   - (R4) **MCP 쿼리 흐름**: 입력 → 청크/임베딩 후보 추출 → **BM25 rerank** → 최우선 응답
   - (R5) **단계별 footprint logging**: 청크 / 임베딩 / 정보 추출 / BM25 rerank — 각 step 상태 모니터링
   - (R6) log 기반 설정 튜닝 가능
   - (R7) 위 기능 정상 동작 → **다음 단계 진입 조건**
   - (R8) 정확도 극대화용 Evaluation 기능 + 테스트 준비

---

## 2. 현재 CKV 상태 vs 요구사항 Gap

### 2.1 이미 구현된 기능 (요구사항 충족 / 부분 충족)

| 요구 | CKV 코드 위치 | 상태 |
|---|---|---|
| R1 vector DB 생성 | `internal/build.Run` + `internal/store/sqlitevec` | ✅ |
| R1 빌드 참여 파일만 인덱싱 | `internal/discover.ResolveGoBuildRoots` (`build_roots` in `ckv.yaml`) | ✅ |
| R2 MCP 임의 입력 → 결과 | `pkg/mcp.Server.handleSemanticSearch` (`cks.context.semantic_search`) | ✅ |
| R3 citation 검증 | `internal/query.EnforceCitationsAt` — file existence + line sanity + commit_hash mismatch flag | ✅ 부분 (할루시네이션 자동 측정은 없음) |
| R3 LLM-judge | `cmd/ckv eval --judge` + `internal/judge` | ✅ |
| R5 footprint span | `internal/footprint.Span` + B8 `--profile` (per-event p50/p95/sum) | ✅ 골격은 있음 |
| R8 fixture-based eval | `cmd/ckv eval` (recall@k / MRR / citation_accuracy) + PR-regression 모드 | ✅ |
| Build throughput tuning | A5 fixture N=50 + Phase D.1 prefix 측정 | ✅ |

### 2.2 ⚠️ Gap (요구사항 미충족 / 핵심 빌딩블록)

| Gap | 현재 상태 | 영향 |
|---|---|---|
| **G1: BM25 rerank 부재** | CKV 는 vector-only ([ADR-003](./adr/003-bm25-dual-track.md) 결정). CKG repo 가 `pkg/bm25/scorer.go` 보유. CKV 내부 import 없음. | **R4 직접 충돌** |
| **G2: query path 단계 footprint 미세분화** | `query.search` 단일 span 만. embed / store-search / threshold-drop / citation / density 각 단계 sub-event 없음. | **R5 직접 충돌** |
| **G3: hallucination 자동 감지 framework 부재** | citation enforcement 가 file/line 만 확인. snippet 텍스트 ↔ 실 파일 byte 매칭, LLM judge integration 자동화 없음. | **R3 부분 충돌** |
| **G4: stable-net 대상 fixture 부재** | testdata/sample N=50 만. 실 코드베이스 대상 query fixture / known-answer set 없음. | **R8 직접 충돌** |
| **G5: build_roots 실 검증 없음** | ResolveGoBuildRoots impl 있지만 stable-net 같은 대규모 corpus 실 검증 미완. `.claude/docs/BUILD_SOURCE_FILES.md` 와 매칭률 측정 안 됨. | **R1 부분 충돌** |
| **G6: log → 설정 튜닝 link 없음** | 단계별 metric 이 있어야 어떤 knob 을 어떻게 돌릴지 결정 가능. 현재 footprint 는 양호하지만 *튜닝 자동화* 는 없음. | **R6 미흡** |

---

## 3. 결정 포인트 (Decision Points)

### D1: BM25 통합 위치 — **본 작업의 가장 큰 결정** ⚠️

ADR-003 (2026-05-18) 은 **CKV vector-only + CKS 측 RRF fusion** 으로 결정.
사용자 요구 R4 는 MCP 쿼리 *흐름 자체에* BM25 rerank 가 포함되어야 한다고 명시.
ADR-003 과 직접 충돌.

| 옵션 | 내용 | 장점 | 단점 |
|---|---|---|---|
| **D1-A** | **CKV 내부에 BM25 신설** — ADR-003 supersede (ADR-006). CKG `pkg/bm25/*` 를 reference 로 보고 CKV 측 fresh impl. | 단일 binary 로 R4 만족. CKS 없이 평가/운영. tokenizer 를 chunk metadata (symbol_name, language) 에 맞춤. | ADR 재정렬. BM25 코드 중복 (CKV + CKG 양쪽). 유지보수 비용. |
| **D1-B** | **CKG BM25 를 module 로 추출, CKV import** — `code-knowledge-graph/pkg/bm25` → 별도 mini-module 또는 같은 monorepo 다. | 코드 중복 회피. CKG ↔ CKV BM25 algorithm 일관성. | 두 repo 작업 동시 필요. import path 합의 cost. CKG bm25 가 *graph nodes* 대상 → chunk 에 맞게 adapter 필요. |
| **D1-C** | **CKV 는 vector-only 유지, BM25 는 CKS repo 신설로** — ADR-003 그대로. 본 작업의 R4 흐름은 *CKS repo 측 작업*. | 깔끔한 모듈 경계. ADR 안 건드림. | CKS repo 가 아직 없음 → 본 작업 시작 못 함. R7 ("다음 단계 진입") 무기한 지연. |
| **D1-D** | **하이브리드** — CKV 내부에 BM25 *evaluation 전용* 추가 (production target 은 CKS). | 단기: R4 평가 즉시 가능. 장기: CKS 도착 시 CKV BM25 deprecate path 있음. | 일시적 코드 중복. dual-track 추적 cost. |

**권장 = D1-A**.

근거:
- 사용자 요구 R4/R5/R7 이 *MCP 쿼리 흐름 안* 에 BM25 포함을 명시 → CKV-internal 만이 단일 binary 로 만족
- CKS repo 는 아직 코드가 없음 (사용자 보고: CKG 측 코드 있으나 eval 미완료) → D1-C 는 시작 불가
- CKV chunk metadata 가 (symbol_name + signature + body + commit_hash) 정렬되어 있어 chunk-aware BM25 토크나이저가 자연스럼
- ADR-003 의 원 결정 사유 ("CKV 는 vector retrieval only") 가 R4/R5 요구 앞에서 더 이상 유효하지 않음 → **ADR-006 으로 supersede** 가 정직한 경로

### D2: BM25 통합 시점 (query path 의 어디서)

```
intent → Embed → store.Search (top K' = K×3 over-fetch)
   → threshold drop → EnforceCitations → splitByTest → DensityAdjust
```

옵션:
- **D2-A: store.Search 이후, threshold 이전** — vector ANN top-K' 후보를 BM25 로 rerank → top-K 추출. 모든 hit 가 BM25 score 보유. (사용자 R4 흐름 일치)
- **D2-B: threshold + citation 이후** — citation-enforced hit 만 BM25. drop 된 hit 는 rerank 안 함. 비용 낮음. **현재 hit 수가 적어 rerank 의미 약화 가능**.
- **D2-C: 별도 hybrid path** — vector top-K + BM25 top-K 를 RRF 로 fuse. 더 정교한 hybrid. ADR-003 의 원래 비전.

**권장 = D2-A** — 사용자 R4 의 "유사도 높은 후보를 추출 후 BM25 로 rerank" 와 직접 일치.
D2-C 는 다음 단계 (Phase E 영역) — 본 작업의 단순화된 시작점은 D2-A 가 적절.

### D3: BM25 corpus 구성 (어떤 텍스트를 색인할지)

CKG 의 `pkg/bm25/scorer.go` 는 *graph node* 의 `qname + signature + doc_comment`
를 색인. CKV 는 chunk 단위 → 다른 corpus.

옵션:
- **D3-A: chunk.Text 전체** — 단순. 함수 본문 전체가 lexical pool 진입.
- **D3-B: signature + symbol_name 만** — CKG 와 정합. 짧은 corpus, lexical-symbol 검색 강점.
- **D3-C: 다중 필드 (BM25F)** — `symbol_name`, `signature`, `body` 각각 다른 weight. 가장 정교.

**권장 = D3-B for 첫 구현, D3-C 는 Phase 2** — 짧은 corpus 가 BM25 정통 사용처
이며 chunk.Text 전체는 noise (변수명, 주석) 가 ranking 을 흐림.

### D4: Footprint 단계별 sub-event 구조 (R5)

현재 단일 `query.search` span. 사용자 R5 는 *청크 / 임베딩 / 정보 추출 /
BM25 rerank* 각 단계 분리 요구.

권장 sub-event 구조 (5 layer):

```
query.search                  -- 기존, top-level span
  ├─ query.embed              -- intent → vector
  ├─ query.store.search       -- vector ANN, candidates 수
  ├─ query.threshold.drop     -- below-threshold 수
  ├─ query.bm25.rerank        -- score 분포, rank changes (D1 결정)
  ├─ query.citation.enforce   -- dropped/stale 수
  └─ query.density.adjust     -- tier 분포 (full/sig5/sig_only)
```

각 sub-event:
- `latency_ms`
- `candidates_in / candidates_out`
- 결정적 fingerprint (예: 첫 hit chunk_id 의 12 자)
- 단계별 *튜닝 노브* 와 1:1 매핑 (e.g., `query.threshold.drop` log → `--threshold` 튜닝)

이미 B8 `--profile` 로 aggregated p50/p95 dump 가능 → sub-event 추가하면
**튜닝 노브 ↔ 측정 metric 1:1 매핑 자동 성립** (R6 자동 달성).

### D5: Hallucination 검증 framework

옵션 (모두 조합 가능):

| 옵션 | 의미 | cost |
|---|---|---|
| **D5-A** | Citation file existence + line range (이미 있음) | 0 |
| **D5-B** | Snippet text **byte-exact** match against `<srcRoot>/<file>` lines `[start_line:end_line]` | 낮음 (Open + Read) |
| **D5-C** | LLM-judge: "intent + snippet 이 의미적으로 일치하는가" (이미 `--judge` impl) | 높음 (API cost) |
| **D5-D** | Negative fixture: *존재하지 않는* intent → 결과가 빈/낮은 score 여야 함 | 낮음 (fixture 작성만) |

**권장 = D5-A + D5-B + D5-D** — A 는 이미 있음. B 는 snippet 가 실 파일 hash
와 align 하지 않으면 *false citation* 으로 표시. D 는 hallucination 의
operational 정의 ("없는 것을 있다고 안 한다") 자동 측정.

D5-C 는 *옵션 플래그* 로 유지 — 비용 큼.

### D6: Target corpus 범위 (G4 해소)

옵션:
- **D6-A: stable-net 전체** (781 빌드 참여 파일, 160 패키지) — `build_roots` 적용. 첫 빌드 6h+ (현재 0.74 c/s) → 비현실적 without throughput buffer.
- **D6-B: stable-net 고유 영역만** — WBFT consensus / systemcontracts / cmd/gstable / cmd/genesis_generator. 수십 파일, 첫 빌드 ≤10분. **이 영역이 evaluation 의미가 가장 큼** (geth upstream 과 diff).
- **D6-C: stable-net 고유 + 의존 1-hop** — 고유 패키지 + 그것이 직접 의존하는 패키지. 중간 규모.
- **D6-D: 일부 파일 cherry-pick** — 사용자가 *알려진 정답* 을 가진 N=20-50 query fixture 를 만들기 위해 필요한 파일만.

**권장 = D6-B 우선, D6-A 는 throughput 확보 후** — stable-net "고유 영역"
이 evaluation 의 first-class 타겟. fork 영역은 geth 측 cross-reference 가
이미 풍부 → 정확도 평가의 신호 약함.

---

## 4. 권장 Architecture (D1-A + D2-A + D3-B + D4 + D5-ABD + D6-B 가정)

```
                       ┌──────────────────────────────────────┐
                       │  ckv mcp / ckv query / ckv eval      │
                       └──────────────┬───────────────────────┘
                                      ▼
                          internal/query.Engine
                                      │
        ┌─────────────────────────────┼───────────────────────┐
        ▼                             ▼                       ▼
    embed(intent)            store.Search top-K'        manifest/freshness
   [fp: query.embed]   [fp: query.store.search]
                                      │
                                      ▼
                       internal/query/bm25 (NEW — D1-A)
                       chunk-aware BM25 over
                       (symbol_name + signature)
                       [fp: query.bm25.rerank]
                                      │
                                      ▼
                          threshold drop + EnforceCitationsAt
                       [fp: query.threshold.drop]
                       [fp: query.citation.enforce]
                                      │
                                      ▼
                                DensityAdjust
                          [fp: query.density.adjust]
                                      │
                                      ▼
                              Response{hits, examples,
                                       metadata, warnings}
```

신규/확장 코드 위치:

| 위치 | 신규 | 설명 |
|---|---|---|
| `internal/query/bm25/` (신설) | Okapi BM25 + chunk-aware tokenizer | D1-A 본체. CKG `pkg/bm25` 참조하되 chunk 텍스트 / symbol 메타에 맞춤. |
| `internal/query/engine.go::Search` | 5 sub-span 추가 + bm25 rerank 단계 통합 | D2-A / D4 |
| `internal/query/hallucination.go` (신설) | byte-exact snippet check | D5-B |
| `testdata/stablenet/` (신설) | fixture corpus 구성 → query+expected | D6-B |
| `cmd/ckv/eval.go` | `--hallucination` flag + negative fixture 지원 | D5-D |

---

## 5. 단계별 Deliverable + Entry Conditions

### Phase 1 — query path footprint 세분화 (D4) ✅ 2026-05-22 (commit `2f6f215`)
- **산출**: 5 sub-span (`query.embed` / `query.store.search` / `query.threshold.drop` / `query.citation.enforce` / `query.density.adjust`). 각 span: `latency_ms`, `candidates_in/out`, top hit fingerprint (chunk_id 12 prefix), tier 분포. `--profile` aggregate.
- **부수 fix**: footprint profile aggregator 가 `latency_ms > 0` 대신 `.done` suffix 기반 필터링 — sub-ms 연산도 count 집계됨. 0-latency 도 정확한 신호 (sub-ms 였음).
- **검증**: `TestSearch_EmitsFiveSubSpans` (JSONL 검증) + CLI smoke (5 sub-span 모두 stderr + profile.json 에 출력)
- **튜닝 노브 ↔ metric 1:1 매핑**:
  - `query.embed` → `--embedder`, `--model-dir`, `CKV_DISABLE_CONTEXTUAL_PREFIX`
  - `query.store.search` → `overfetchFactor`, `--filter`
  - `query.threshold.drop` → `--threshold`
  - `query.citation.enforce` → `--src`
  - `query.density.adjust` → `--budget-tokens`, `--max-density`, `--signature-context-lines`

### Phase 2 — BM25 rerank 통합 (D1-A + D2-A + D3-B)
- **산출**: `internal/query/bm25/` 패키지. `Engine.Search` 가 store.Search 후 BM25 rerank.
- **검증**: testdata/sample N=50 fixture eval 에서 v-only vs v+bm25 비교 (recall@1/MRR delta 측정)
- **entry cond**: Phase 1 완료 (footprint 가 BM25 단계 trace 가능해야 튜닝)
- **LOC**: ~250 (BM25 + tokenizer + integration + test)
- **ADR**: ADR-006 "BM25 통합 - ADR-003 supersede" 작성

### Phase 3 — Hallucination 검증 framework (D5-A/B) ✅ 2026-05-22 (commit `69e148a`)
- **산출**:
  - `internal/query/hallucination.go` — `VerifyHit`, `VerifyResponse`, `HallucinationResult{Verified, Reason, ExpectedFile}`. 3 failure modes: `file_missing` / `out_of_range` / `snippet_not_found`. Whitespace 정규화로 tab/space cosmetics false-positive 회피.
  - `internal/eval/score.go` — `PerQuery.HallucinationCount/Reason` + `Aggregate.HallucinationRate/Hits/TotalHits`. `Score(q, resp, k, srcRoot)` 시그니처에 srcRoot 추가.
  - `cmd/ckv eval --src <path>` — 검증 활성. 비어있으면 metric omitted.
  - `cmd/ckv eval --max-halluc <rate>` — CI gate (default 1.0 = disabled).
  - Renderer human-friendly + JSON 양쪽에 metric 포함.
- **검증**:
  - 8 unit test (`hallucination_test.go`) — exact / signature-only / whitespace / file_missing / out_of_range / snippet_not_found / empty_src / aggregate.
  - CLI smoke: testdata/queries.yaml N=50 × top-K → 250 hits, **halluc_rate 0.000** (모든 snippet 이 실 파일에 정확히 존재). indexing pipeline 자체 정합성 추가 확인.
- **D5-D (negative fixture) 잔여**: 별도 작업 — fixture format 에 `negative: true` 플래그 추가 + Score 로직에 negative pass/fail 분기. Phase 4 (target corpus 작성) 시 함께 진행 권장.

### Phase 4 — stable-net 고유 영역 corpus + fixture (D6-B + G4)
- **산출**:
  - `testdata/stablenet/build_roots.yaml` — WBFT / systemcontracts / cmd/gstable / cmd/genesis_generator 명시
  - `testdata/stablenet/queries.yaml` — N=30-50 known-answer query
  - `testdata/stablenet/negative-queries.yaml` — D5-D negative fixture
- **검증**: stable-net 고유 영역 인덱스 빌드 + eval 통과
- **entry cond**: Phase 1 (footprint) + Phase 2 (BM25) 완료. throughput 측정.
- **참고**: Phase 4 가 *실 평가* 의 시작점. Phase 1-3 는 인프라.

### Phase 5 — 튜닝 자동화 (R6)
- **산출**: footprint profile → 권장 knob 매트릭스. `ckv eval --tune` 모드 (threshold / K' / BM25 weight grid search).
- **entry cond**: Phase 1-4 완료
- **LOC**: ~150

---

## 6. 측정 절차 (Phase 4 후 실행)

```bash
# 1. stable-net 고유 영역 인덱스 빌드
ckv build \
  --src /Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest \
  --out ./ckv-data-stablenet \
  --config testdata/stablenet/ckv.yaml \
  --embedder bgeonnx \
  --profile ./ckv-data-stablenet/build-profile.json

# 2. fixture eval (v+BM25)
ckv eval \
  --fixture testdata/stablenet/queries.yaml \
  --out ./ckv-data-stablenet \
  --json > ./ckv-data-stablenet/eval-result.json

# 3. hallucination check
ckv eval \
  --fixture testdata/stablenet/queries.yaml \
  --out ./ckv-data-stablenet \
  --hallucination --src <path> \
  --json > ./ckv-data-stablenet/halluc-result.json

# 4. negative fixture
ckv eval \
  --fixture testdata/stablenet/negative-queries.yaml \
  --out ./ckv-data-stablenet \
  --negative --json > ./ckv-data-stablenet/negative-result.json

# 5. (튜닝 모드, Phase 5)
ckv eval --tune \
  --fixture testdata/stablenet/queries.yaml \
  --out ./ckv-data-stablenet \
  --tune-grid threshold=0.3,0.4,0.5 bm25_weight=0.0,0.3,0.5,0.7 \
  --json > ./ckv-data-stablenet/tune-result.json
```

---

## 7. 사용자 결정 필요 항목

다음 두 결정이 모든 후속 작업을 가른다:

1. **D1 (BM25 위치)** — D1-A / D1-B / D1-C / D1-D 중 하나
   - 권장: **D1-A**
   - 의미: ADR-003 을 **ADR-006 으로 supersede** 하는 결정 포함

2. **D6 (Target corpus 범위)** — D6-A / D6-B / D6-C / D6-D 중 하나
   - 권장: **D6-B** (stable-net 고유 영역)
   - 의미: Phase 4 fixture 의 범위

나머지 (D2, D3, D4, D5) 는 권장 그대로 진행 가능.

---

## 8. 다음 단계 (사용자 결정 후)

승인 받은 결정에 따라 Phase 1-5 를 순차 / 병렬 진행. 각 Phase 종료 시 commit
+ backlog 갱신 + 본 문서의 단계 status update.

---

## 9. 참조

- [`adr/003-bm25-dual-track.md`](./adr/003-bm25-dual-track.md) — 본 작업이 supersede 후보
- [`backlog.md`](./backlog.md) — 전체 inventory
- [`retrieval-quality-roadmap.md`](./retrieval-quality-roadmap.md) — Phase E (CKS hybrid) 와 본 작업의 관계
- [`pending-work-2026-05-21.md`](./pending-work-2026-05-21.md) — 전 세션 잔여 작업 + CKG integration eval gap
- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — 내부 모듈 그래프 + 파이프라인
- target 프로젝트의 [`/.claude/docs/BUILD_SOURCE_FILES.md`](file:///Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest/.claude/docs/BUILD_SOURCE_FILES.md) — `build_roots` 입력 ground-truth

---

## 변경 이력

| 일자 | 변경 |
|---|---|
| 2026-05-22 | 초안. 사용자 요구 분해, gap 분석, 결정 포인트 + 권장안 + 5 Phase deliverable 정리. ADR-003 supersede 권고 포함. |
