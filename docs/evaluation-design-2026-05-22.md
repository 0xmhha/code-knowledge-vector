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

---

## 10. 본 세션 추가 정의 — cks 통합 관점 (2026-05-22, 추가 라운드)

> 이 섹션은 ckv 외부 (cks repo) 에서 진행된 통합 점검 세션의 결과를 ckv 측 문서에
> back-port 한 것이다. 작업 책임은 prregress 세션 (다른 작업자) 에게 있고, 본 섹션은
> *컨텍스트 + 추가 요구사항 + 권장 작업 명세* 만 제공한다.
>
> 본 섹션 추가의 trigger: cks 측에서 ckv/ckg/cks 통합 작업 우선순위를 점검하다가
> 사용자가 *ckv 의 진짜 역할*을 vector-only 가 아닌 **"한국어/모호 표현 → 코드 정확
> 키워드 변환 (vocabulary bridge)"** 으로 명시하면서, §1~§9 의 가정이 불완전함이
> 드러남.

### 10.1 사용자 명세 재정의 (cks 인터랙션, 2026-05-22)

§1 의 R1~R8 외에 다음이 *추가* 로 ckv 의 책임으로 정의됨:

| 신규 요구 | 본질 | §1 의 어디에 대응되는가 |
|---|---|---|
| **R9: Vocabulary bridge** | 사람의 모호/한국어 표현 → 코드 정확 용어 변환 | R2 의 *진정한 의미* 였음. semantic_search 만으로는 부족. |
| **R10: Multi-stage evaluation** | 평가가 단일 score 가 아니라 *intent / location / plan / code* 4 단계로 분해 | R3 + R8 의 진화. 단계별 문제 식별. |
| **R11: 점진 fixture 학습** | 초기 fixture + 실 사용 중 사용자가 *정답 마크* 로 fixture 증가 | R8 의 진화. fixture 정적 큐레이션 한계 인정. |
| **R12: PR-aware retrieval** | "왜 이렇게 고쳤어?" 류 query 는 코드 단독으로 불가, PR description 필요 | R1 의 corpus 확장. |
| **R13: 3-leg BM25 임시 적용** | ADR-003 (CKV vector-only) 영구 결정 보류. ckv/ckg/cks 모두에 BM25 임시 + 평가로 결정 | D1 (§3.1) 의 사용자 결정. |

핵심: ckv 는 *vector retrieval + vocabulary expansion + PR corpus indexing* 3 가지를
동시에 책임진다. §4 architecture diagram 은 vocabulary 와 PR corpus 가 빠져 있어 보강이 필요.

### 10.2 Multi-stage Evaluation 분해 (R10 구현)

prregress 모듈이 *이미* base_sha checkout → agent plan → diff 비교 패턴을 구현
했다 (`internal/eval/prregress/`, 1710 LOC, 4 entries). 사용자 명세는 이 모듈을
다음 4 단계로 *분해* 측정하는 것:

| Stage | 측정 대상 | 현재 prregress | 신규 필요 |
|---|---|---|---|
| **E1** | Intent capture — 무엇을 하려는지 LLM 이 파악했나 | ❌ 없음 | LLM plan 의 "Problem" 섹션 추출 + PR title/Background 와 임베딩 유사도 |
| **E2** | Location identification — 어디를 봐야 하는지 LLM 이 찾았나 | ⚠️ File F1 만 | + Symbol-level F1 (changed_symbols ground truth + plan 의 symbol mention 추출) |
| **E3** | Plan generation — 어떻게 고칠지 LLM 이 정리했나 | ✅ LLM-judge (plan vs diff 통합) | E3 만 분리: plan 의 *steps* 만 vs PR 의 *commit message* 만 (작업 분해 정확도) |
| **E4** | Code generation — 실제 코드 작성 | ❌ 범위 외 (coding agent 영역) | 향후 cks integration 단계에서 |

신규 메트릭 추가 필요 (`internal/eval/prregress/score.go` 확장, ~250 LOC):
- `IntentScore(plan, prTitle) float64` — E1
- `SymbolF1(planSymbols, truthSymbols) (p, r, f1 float64)` — E2 신규
- `PlanStepsScore(planSteps, commitMessages) float64` — E3 분리
- 기존 `JudgeScore` 는 E3 + E4 결합 (legacy 호환 유지)

신규 ground truth 필드 (`testdata/prs.yaml` 확장):
- `intent_ground_truth: string` — PR title + Background 첫 문장
- `changed_symbols: []string` — AST diff 로 자동 추출

### 10.3 fixture 4 → 12 자동 확장 (R12 / D6 충족)

기존 4 entries (pr69 / pr70 / pr72 / pr74) → **12 entries** 로 확장.
선정 기준: stable-net 고유 영역 + fix prefix + 도메인 카테고리 다양성.

```yaml
# 신규 entry 8 개 (기존 4 개 + 8 개 = 12 개)
prs:
  # 기존 (4 개)
  - id: pr69    # refactor: align genesis construction
  - id: pr70    # fix: fill missing effectiveGasPrice
  - id: pr72    # feat: eth_GetReceiptsByHash
  - id: pr74    # fix: increase txMaxSize to 256KB

  # 신규 (8 개) — stable-net 고유 영역 fix PR 우선
  - id: pr77    # fix: refresh AnzeonTipEnv current block when GasTip changes
    base_sha: <#75 merge commit>
    changed_symbols: [AnzeonTipEnv.SetCurrentBlock, AnzeonTipEnv.gasTipChanged, RemotesBelowTip]
    intent_ground_truth: |
      Refresh AnzeonTipEnv currentBlock when header GasTip value changes,
      not only on state root change. Without this, non-validator transactions
      after a governance gasTip change are validated with stale GasTip.
    category: gas_policy

  - id: pr75    # fix(wbft): future-view check off-by-one
    base_sha: <#74 merge commit>
    changed_symbols: [isTooFarFutureMessage]
    intent_ground_truth: |
      Off-by-one in isTooFarFutureMessage was dropping valid next-sequence
      WBFT consensus messages.
    category: consensus_wbft

  - id: pr73    # fix: sync Account.Extra with GovCouncil
    base_sha: <#72 merge commit>
    changed_symbols: [Account.Extra, AccountExtraValidMask, ValidateExtra,
                      GovCouncil.init]
    intent_ground_truth: |
      Sync blacklist/authorized account state between Account.Extra and
      GovCouncil contract storage slots at genesis init. Add Extra bit
      validation and skip-zero-address handling.
    category: genesis_governance

  - id: pr67    # fix: secp256r1 precompile Anzeon → Boho
    base_sha: <#66 merge commit>
    changed_symbols: [secp256r1 precompile registration]
    intent_ground_truth: Move secp256r1 from Anzeon hardfork to Boho hardfork
    category: hardfork

  - id: pr63    # fix: GovMinter v2 burn refund
    base_sha: <#62 merge commit>
    changed_symbols: [GovMinter._cleanupBurnDeposit, GovMinter.claimBurnRefund,
                      BohoConfig.SystemContracts]
    intent_ground_truth: |
      Add refund mechanism for native coins locked in burnBalance when
      burn proposals are cancelled/rejected/expired. GovMinter v1 → v2
      via Boho hardfork.
    category: gov_minter

  - id: pr58    # fix: chainconfig engine mismatch
    base_sha: <#56 merge commit>
    changed_symbols: [chainConfig.Engine]
    intent_ground_truth: Fix chainconfig engine field mismatch
    category: chain_config

  - id: pr56    # fix: normalize comma-separated config strings
    base_sha: <#55 merge commit>
    changed_symbols: [systemcontracts init parsers — members, validators,
                      blsPublicKeys, blacklist, authorizedAccounts, minters]
    intent_ground_truth: |
      Comma-separated config strings caused inconsistent hashAlloc/root
      due to whitespace handling. Apply split + TrimSpace + drop empty.
    category: system_contract_init

  - id: pr55    # fix: race condition on roundChangeTimer
    base_sha: <#54 merge commit>
    changed_symbols: [roundChangeTimer, roundState bigint]
    intent_ground_truth: |
      Data race on WBFT roundChangeTimer and roundState bigint access.
      Add proper locking and remove stateMu variable.
    category: consensus_wbft_concurrency
```

base_sha 는 git log 에서 *직전 PR merge commit* 으로 자동 추출 가능 (gh API
또는 `git log --merges` + grep `(#NN)` 패턴).

### 10.4 ckv 신규 작업 (cks 인터랙션 도출)

§5 Phase 1~5 외에 다음 작업이 *사용자 명세 R9/R10/R11/R12 충족*에 필요:

| ID | 작업 | 사용자 명세 | LOC | 의존 |
|---|---|---|---|---|
| **ckv-NEW-1** | `ckv query --alias <yaml>` — rule-based query expansion (vocab bridge stub) | R9 | ~50 | 없음 |
| **ckv-NEW-2** | `ckv eval --record` — interactive fixture 추가 모드 (F1) | R11 | ~150 | 없음 |
| **ckv-NEW-3** | PR corpus indexing (Phase C, backlog #4) — PR description 을 chunk 로 인덱싱 | R12 | ~400 (재사용 후) | prregress fetcher.go 재사용 |
| **ckv-NEW-4** | `internal/eval/prregress/score.go` 확장 (E1/E2/E3 메트릭 분해) | R10 | ~250 | 없음 |
| **ckv-NEW-5** | fixture 4 → 12 확장 (§10.3) | R12 | YAML만 | git/gh fetch | ✅ 2026-05-22 (commit `c005e04`) — 8 신규 entry + Entry struct에 `intent_ground_truth`/`changed_symbols`/`category` 필드 추가 (모두 optional, legacy 4건 영향 0). 2 신규 unit test. |
| **ckv-NEW-6** | Symbol-level PR breadcrumb 데이터 추가 (ckg PR-aware A 옵션의 ckv 쪽 짝) | R12 | ~80 | NEW-3 후 |
| **ckv-NEW-7** | `ckv mcp` 에 `cks.context.related_changes` tool 추가 (cks 가 wrap 할 backend) | R12 (B 옵션 backend) | ~150 | NEW-3, NEW-6 |
| **ckv-NEW-8** | Glossary loader — `.claude/docs/*.md` 파싱 후 한국어-영문 매핑 YAML 자동 추출 | R9 (D 옵션 backend) | ~150 | 없음 |
| **ckv-NEW-9** | 3-leg BM25 (사용자 결정 R13): `internal/query/bm25/` 임시 통합 — ckg `pkg/bm25.Scorer` 재사용 | R13 | ~250 | 없음 |

총 ~1530 LOC. backlog 의 #4 (Phase C) + 새로 분리된 9 개 작업.

#### 10.4.1 ckv-NEW-1: `--alias` flag 구현 명세

```go
// internal/query/expand.go (신규)
type AliasMap map[string][]string  // korean → []english_keywords

func ExpandQuery(intent string, aliases AliasMap) string {
    // 1. intent 안의 한국어/모호 표현 검색
    // 2. aliases 에서 매칭되는 keyword 들 append
    // 3. 임베딩 입력은 "<intent> [aliases: <kw1>, <kw2>, ...]" 형태
    // 또는 BM25 입력으로는 별도 분리
}
```

CLI:
```bash
ckv query "0번 블록 시스템 컨트랙트 어떻게 주입돼?" \
  --alias ./testdata/stablenet/glossary.yaml \
  --out ./ckv-data
```

glossary.yaml (사람 큐레이션 또는 NEW-8 자동 생성):
```yaml
aliases:
  "0번 블록": [genesis, genesis_block, GenesisAlloc]
  "합의 알고리즘": [consensus, wbft, WBFT]
  "시스템 컨트랙트": [system contract, systemcontracts, SystemContracts]
```

#### 10.4.2 ckv-NEW-2: `--record` 모드 명세

```bash
ckv eval --record --fixture ./testdata/stablenet/queries.yaml \
  --out ./ckv-data-stablenet --src <path>
```

흐름:
```
사용자 입력: "거버넌스로 가스팁 바꿨는데 트랜잭션이 거절돼"
ckv:
  → top-5 결과 표시 (file:line + snippet)
  → 사용자에게 prompt: "이 중 정답은? (1-5, comma-separated, 또는 'none')"
  → 사용자 입력 (예: "1,3")
  → fixture YAML 에 새 entry append:
      query: "거버넌스로 가스팁 바꿨는데 트랜잭션이 거절돼"
      expected_chunks: [<chunk_id_1>, <chunk_id_3>]
      expected_files: [...]
      timestamp: 2026-05-22T15:30:00Z
      recorded_via: interactive
```

장점: 사용자가 *실 사용 중 자연스럽게* fixture 누적. 정적 큐레이션 부담 감소.

#### 10.4.3 ckv-NEW-3: PR corpus indexing 명세 (backlog #4)

prregress 모듈의 `fetcher.go` (gh CLI 호출, 149 LOC) 재사용. 새 단계 추가:

```go
// internal/parse/prdoc/parser.go (신규)
func ParsePRDescription(prDesc string) ([]Chunk, error) {
    // PR description 을 다음 단위로 chunk:
    // 1. Background / Context 섹션 → chunk_kind="pr_background"
    // 2. Solution / Changes 섹션 → chunk_kind="pr_solution"
    // 3. 각 commit message → chunk_kind="commit_message"
    // metadata 에 pr_number, base_sha, head_sha, changed_files, changed_symbols
}
```

새 ChunkKind:
- `ChunkPRBackground` — 무엇이 문제였는지
- `ChunkPRSolution` — 어떻게 고쳤는지
- `ChunkCommitMessage` — 작업 단위 메시지

새 인덱싱 명령:
```bash
ckv build --src <repo> --include-pr-history \
  --pr-since 2025-01-01 \
  --out ./ckv-data
```

### 10.5 ckg PR-aware 통합 (사용자 결정 A+B+C 모두 채택)

ckv 가 PR corpus 를 인덱싱하더라도 ckg 측 PR-aware 메타데이터가 *symbol → PR list*
역방향 검색을 가능하게 한다. 사용자 결정: **A + B + C 모두 채택**.

| 옵션 | 위치 | 역할 | LOC |
|---|---|---|---|
| **A**: Symbol-level PR breadcrumb | ckg `pkg/store.Node` 확장 | 각 symbol 에 *그 symbol 을 변경한 PR list* metadata | ~80 (ckg) |
| **B**: PR context tool | cks `internal/fusion/` + ckv-NEW-7 backend | `cks.context.related_changes` MCP tool. ckv (PR description) + ckg (PR → symbol edge) fusion | ~250 (cks) |
| **C**: Pre-flight warning injection | coding agent layer (cks prompt 또는 coding agent repo) | agent plan generation 시 *수정 대상 symbol 의 PR history* 자동 system message 주입 | ~50 (cks + agent) |

#### 10.5.1 Temporal slicing 제약

fixture evaluation 시 *base_sha 시점 이전 PR 만* 보여야 한다 (정보 누설 방지). 즉:

```go
type PRRef struct {
    Number       int
    Title        string
    BaseSHA      string
    HeadSHA      string
    Summary      string
    MergedAtUTC  time.Time   // *** temporal slicing key ***
}

// Query 시 cutoff_sha 전달
func (n Node) RecentPRsBefore(cutoff time.Time) []PRRef {
    out := []PRRef{}
    for _, pr := range n.RecentPRs {
        if pr.MergedAtUTC.Before(cutoff) {
            out = append(out, pr)
        }
    }
    return out
}
```

prregress runner 가 *base_sha 의 commit time* 을 cutoff 로 전달 → leakage 방지.

### 10.6 평가 단계 체계 (Stage A/B/C)

사용자 명시 "작은 단위부터 검증" 원칙. cks 통합 전에 ckv/ckg 단독 평가 먼저.

#### Stage A — ckv 단독 평가 (가장 먼저)

```
측정 대상: ckv 가 사용자 R1~R13 명세를 어디까지 충족하는가
필요한 기능:
  - ckv-NEW-1 (alias) ✓ 작은 작업
  - ckv-NEW-2 (record) ✓ 점진 fixture
  - ckv-NEW-3 (PR corpus) ✓ R12 핵심
  - ckv-NEW-4 (E1/E2/E3 메트릭)
  - ckv-NEW-5 (fixture 12 개)
  - ckv-NEW-8 (glossary loader)
  - ckv-NEW-9 (3-leg BM25, ckv 쪽)
  - 기존 Phase A / D.1 bge-large 실측 (코드 ✅, 측정만)

측정 시나리오:
  1. 핵심: stable-net 고유 영역 인덱싱 + glossary aliasing + fixture 12 개 평가
  2. Multi-stage: E1 (intent) / E2 (file+symbol F1) / E3 (plan steps) 분리 측정
  3. Vocabulary bridge ablation: --alias on/off 비교
  4. Hybrid ablation: vector-only / vector+BM25 (ckv 측 NEW-9) 비교

entry conditions:
  - bge-large 모델 로딩 (libonnxruntime + bge-large 다운로드)
  - go-stablenet repo + dev 브랜치 최신 상태
  - fixture 12 개 큐레이션 완료 (10.3)
```

#### Stage B — ckg 단독 평가 (Stage A 후)

```
측정 대상: ckg 가 정확 키워드 → graph 검색을 한국어 영역에서도 잘 하는가
필요한 기능:
  - ckg 한국어 토크나이저 동작 검증 (~30 LOC, ckg 측)
  - ckg qname canonicalization 단위 (~50 LOC, ckg 측)
  - ckg PR-aware A 구현 (~80 LOC, ckg 측, §10.5)
  - go-stablenet 재인덱스 (/tmp/ckg-stablenet 갱신)

측정 시나리오:
  1. 영문 keyword → recall (이미 EV1 Phase 2 baseline 5/5)
  2. 한국어 keyword 직접 입력 시 동작 (vocabulary 미적용)
  3. fixture 12 개의 expected_symbols 를 한국어 query 와 함께 → recall
  4. PR breadcrumb 표시 정확도 (자동 grep 검증)
```

#### Stage C — cks 통합 평가 (Stage A/B 안정 후)

```
측정 대상: ckv + ckg + (cks 측 추가) 통합 시 시너지 / 회귀
필요한 기능:
  - cks-T1-D1~D5 (glossary loader, vocab resolver, dual manifest, RRF fusion, 3-leg BM25)
  - cks-NEW PR context tool (B 옵션, ~250 LOC)
  - Pre-flight warning injection (C 옵션, ~50 LOC)

측정 시나리오:
  1. Multi-stage fixture 12 개 전체 통합 측정
  2. 3-leg BM25 ablation: ckv-only / ckg-only / cks-only / 3-leg RRF
  3. Vocabulary bridge full path: glossary → resolver → expanded query → fusion
  4. PR-aware: with / without related_changes context
```

### 10.7 의존 그래프 (작업 순서)

```
[다른 세션 작업자 (prregress)]
  └─ §10 본 섹션을 읽고 진행

Stage A 준비:
  ckv-NEW-1 (alias) ─┐
  ckv-NEW-2 (record) ┤
  ckv-NEW-4 (metric) ┼─→ Stage A 시작 가능
  ckv-NEW-8 (glossary) ┤
  bge-large 모델 다운로드 ┘

  ckv-NEW-3 (PR corpus) ──→ ckv-NEW-5 (fixture 12) ──→ ckv-NEW-6 (PR breadcrumb data)
                                                       ──→ ckv-NEW-7 (related_changes tool)

  ckv-NEW-9 (BM25 ckv) ────────────────────────────→ Stage A hybrid ablation

Stage B 준비:
  ckg 한국어 토크나이저 검증
  ckg qname canonicalization
  ckg PR-aware A 구현 (ckv-NEW-6 와 데이터 정합)

Stage C 준비:
  cks-T1-D1~D5 (R-C 권장 조합)
  cks-NEW PR context tool (B 옵션)
  Pre-flight warning injection (C 옵션)
```

### 10.8 fixture 점진 학습 모드 — F1 ~ F4 (사용자 명시)

```
F1 (ckv eval --record interactive CLI) ─ Stage A 에서 즉시 사용. ckv-NEW-2.
F2 (cks MCP feedback tool)             ─ Stage C 통합 시 F1 로직 흡수.
F3 (web UI annotator)                  ─ S2 (ckv serve 도입 시).
F4 (Git PR 기반 fixture growth)        ─ 어느 단계에서나 보완용. 사람이 직접 PR.
```

F1 → F2 흐름은 *코드 공유*: ckv-NEW-2 의 record API 를 cks 가 MCP tool 로 wrap.

### 10.9 §3 D1 결정 — 사용자 답변 (2026-05-22)

§3.1 의 D1 (BM25 위치) 사용자 결정:

> "ckv, ckg, cks 모두에 bm25 기능을 임시적으로 적용하여 사용할 수 있도록 하고, 이것은
> evaluation 을 위해서야. evaluation 은 반드시 go-stablenet 프로젝트 코드를 db 로
> 적용한 데이터를 통해, query 응답과 실제 코드 사이의 gap 을 좁히는 작업을 통해
> 평가 개선해야 한다."

→ **D1-임시 (D1-A / B / C / D 어느 하나도 영구 결정 안 함)**. 3-leg BM25 임시 적용
후 측정 데이터로 ADR-006 결정.

ckv 측 작업: ckv-NEW-9 (`internal/query/bm25/` chunk-aware BM25, ckg `pkg/bm25.Scorer`
재사용). ADR-003 supersede 결정은 측정 후로 보류.

### 10.10 §3 D6 결정 — 사용자 답변 (2026-05-22)

§3.6 의 D6 (Target corpus 범위) 사용자 결정:

> "go-stablenet 프로젝트 코드에서 `.claude/skills` 에서 지정하고 있는 파일 리스트가
> 있는데, 그것들은 모두 포함되어야 함. 추가로 더 학습되어야 하는 정보들이 있을텐데..."

→ **D6-skills 기반 + 추가 학습 후보 정리**. 구체:

```yaml
# StableNet 고유 영역 (~80 files)
include:
  - consensus/wbft/**/*.go            # 39 files (7 packages)
  - systemcontracts/*.go              # 9 files
  - systemcontracts/*.sol             # Solidity 원본
  - cmd/gstable/**/*.go               # 11 files
  - cmd/genesis_generator/**/*.go     # 3 files
  - core/stablenet_genesis.go
  - core/types/{istanbul,state_account_extra,tx_fee_delegation}.go
  - core/vm/native_manager.go
  - params/{config_wbft,network_params}.go
  - eth/{handler_istanbul,quorum_protocol}.go
  - eth/gasprice/anzeon.go
  - eth/protocols/eth/qlight_deps.go

# 도메인 문서 (glossary 추출 source, ckv-NEW-8 입력)
docs:
  - CLAUDE.md
  - .claude/docs/CLAUDE_DEV_GUIDE.md
  - .claude/docs/SYSTEM_CONTRACT_FLOW.md
  - .claude/docs/BUILD_SOURCE_FILES.md
  - .claude/docs/REVIEW_GUIDE.md
  - .claude/docs/review-test-result-with-ast.md

# 워크플로우 (trigger 키워드)
workflows:
  - .claude/skills/{check-reviews,complexity,delta-log,milestone,
                    handoff,do-review,pr-review,qr-gate}/SKILL.md
  - .claude/commands/stablenet-review-code.md

# PR corpus (ckv-NEW-3 입력)
pr_corpus:
  - github.com/stable-net/go-stablenet PRs on dev branch
  - 우선: stable-net 고유 영역 fix PR ~12 개 (§10.3)
  - 점진: 모든 stable-net 고유 영역 PR
```

#### 추가 학습 후보 (사용자 질문 "어떤 정보 더 추가되면 좋을지")

| 후보 | 효과 | Stage | 비고 |
|---|---|---|---|
| G1: 빌드 참여 781 file 전체 (geth fork 포함) | go-stablenet 전체 컨텍스트 | Tier 3 | throughput 6h+, PRR-1 buffer 회복 후 |
| G2: `.git/log` (커밋 메시지 + diff stat) | history-aware query | Tier 2 | ckv Phase C 와 부분 중복 |
| G3: `tests/` 파일 (시나리오) | "어떻게 사용되는가" 예시 | Tier 1 | 작음 |
| G4: chainbench 시나리오 (`/Users/.../chainbench/tests/*.sh`) | E2E 동작 예시 | Tier 2 | 별도 corpus |
| G5: 공식 문서 / RFC | 외부 표준 매핑 | 조건부 | 사용자 확인 필요 |
| G6: GitHub PR 리뷰 코멘트 | 도메인 어휘 + 결정 근거 | Tier 2/3 | gh API 비용 |
| G7: HLD 문서 (`stablenet-ai-agent/claudedocs/`) | 시스템 모델 | 조건부 | 외부 path |

권장: 핵심 + G3 (tests). G1/G2 는 throughput 회복 후.

### 10.11 다음 세션 (prregress 작업자) 시작 권장 순서

```
Day 1:
  1. §10 본 섹션 정독
  2. ckv-NEW-5 — fixture 4 → 12 확장 (YAML 작성, ~1 시간)
  3. ckv-NEW-1 — alias flag 구현 + 5 개 alias entry seed (~3 시간)

Day 2:
  4. ckv-NEW-8 — glossary loader (CLAUDE_DEV_GUIDE.md 파싱, ~3 시간)
  5. ckv-NEW-2 — record mode 구현 (~3 시간)
  6. Stage A 1차 측정 (bge-large + fixture 12 + alias on/off)

Day 3:
  7. ckv-NEW-4 — E1/E2/E3 메트릭 분해 (~4 시간)
  8. ckv-NEW-3 — PR corpus indexing (prregress fetcher.go 재사용, ~5 시간)

Day 4-5:
  9. ckv-NEW-9 — chunk-aware BM25 임시 (3-leg 의 ckv 쪽)
  10. Stage A 2차 측정 (Multi-stage + Hybrid)
  11. 결과 → ADR-006 (ADR-003 영구 결정) 초안 작성 + 사용자 결재

Stage B / Stage C 는 별도 세션 / 다른 repo (ckg / cks)
```

### 10.12 cks 측 보고서 cross-link

본 섹션과 동기적으로 cks repo 에 작성된 종합 보고서:
- `code-knowledge-system/docs/research/knowledge-data-best-practice-2026-05-22.md`
  - R-A: Code RAG semantic gap 해결 best practice 카탈로그 (12 기법)
  - R-B: go-stablenet 권장 기술 스택 (Tier 1/2/3)
  - R-C: ckg ↔ ckv 키워드 공유 기술 (C1-C5)
  - R-D: cks 측 신규 기능 (D1-D7, ~1030 LOC)
  - 3-leg BM25 임시 적용 + 측정 후 결정 framework

cks 보고서의 R-A 카탈로그가 §10.2 Multi-stage evaluation 의 *이론적 배경* 을 제공.
cks R-D 의 cks-T1-D1~D5 가 §10.6 Stage C 의 작업 단위.

---

## 변경 이력

| 일자 | 변경 |
|---|---|
| 2026-05-22 | 초안. 사용자 요구 분해, gap 분석, 결정 포인트 + 권장안 + 5 Phase deliverable 정리. ADR-003 supersede 권고 포함. |
| 2026-05-22 (추가 라운드) | §10 신설. cks 측 통합 점검 세션 결과 back-port. 사용자 명세 R9 (vocabulary bridge) / R10 (multi-stage eval) / R11 (점진 fixture) / R12 (PR-aware) / R13 (3-leg BM25 임시) 추가. ckv-NEW-1~9 9 개 신규 작업 명세. fixture 4 → 12 확장 (§10.3). Stage A/B/C 평가 체계 (§10.6). ckg PR-aware A+B+C 통합 (§10.5). D1 + D6 사용자 결정 답변 (§10.9 / §10.10). 다음 세션 작업 순서 권장 (§10.11). |
