# PR-Regression Baseline — 2026-05-19

> **목적**: `ckv eval --pr-fixture`의 첫 end-to-end 측정. 사용자 정의 1.3 요구사항(plan ↔ actual diff 유사도 ≥80%)에 대한 현재 시스템의 위치 보고.
>
> **상태**: pipeline 완전 작동 ✅ / mock embedder threshold 미달 ❌ / bgeonnx 재측정 follow-up 등록.

---

## 1. 측정 환경

| 항목 | 값 |
|---|---|
| 대상 PR | [stable-net/go-stablenet#70](https://github.com/stable-net/go-stablenet/pull/70) — *fix: fill missing effectiveGasPrice in receipts on derivation* |
| Base commit | `aa28927fb12048a59ac34608702eef5e1be90931` |
| Source clone | `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` |
| Corpus 크기 | 1,845 source files (Go/TS/Sol) |
| Embedder | **mock** (feature-hashing, dim 64) |
| Planning agent | Claude Code CLI 2.1.144 (default model) |
| Judge | Claude Code CLI (same binary, separate prompt) |
| Top-K hints | 10 |
| Threshold | 0.80 (autoplan v1.1 Challenge 3 결정) |
| Runs | **5회 반복** (LLM noise band 측정용) |
| Run 시간 | ~90초/회 (~7.5분 총) |
| CKV commits | `fddecda` ~ `c36a9fb` (5단계 구현) |

---

## 2. 측정값 (5-run)

### 2.1 Per-run

| 지표 | r1 | r2 | r3 | r4 | r5 |
|---|---|---|---|---|---|
| LLM-judge score | 0.20 | 0.50 | 0.50 | **0.80** | 0.20 |
| file F1 | 0.40 | 0.62 | 0.00 | 0.44 | 0.38 |
| file precision | 0.43 | 0.62 | 0.00 | 0.40 | 0.38 |
| file recall | 0.38 | 0.62 | 0.00 | 0.50 | 0.38 |
| plan size (files) | 7 | 8 | 6 | 10 | 8 |
| overlap (of 8 truth) | 3 | 5 | **0** | 4 | 3 |

### 2.2 통계

| 지표 | mean | σ | min | max | range |
|---|---|---|---|---|---|
| LLM-judge | **0.44** | 0.25 | 0.20 | 0.80 | 0.60 |
| file F1 | 0.37 | 0.23 | 0.00 | 0.62 | 0.62 |

**Threshold 0.80 통과**: **1/5 (20%)**. r4만 도달. r3은 0 overlap (agent가 PR과 완전히 다른 파일 선택).

---

## 3. 핵심 발견

### 3.1 Pipeline 자체는 정상

6단계 (fetch → worktree → build → query → agent → score) 모두 안정. Run 5회 모두 errored=0, completion 정상. **수직적 통합은 검증됨**.

### 3.2 LLM noise band가 system signal보다 큼

- judge σ = 0.25 (~25%포인트)
- 같은 입력에 0.20 / 0.50 / 0.80 — N=1 측정은 무의미
- **결론**: 신뢰성 있는 측정에 5+ run 필수. 1.3 요구사항의 "유사도 80%"는 단일 측정이 아닌 **평균 기준**으로 재정의 필요.

### 3.3 mock 임베더가 1차 병목

Run 3의 0 overlap + agent의 plan 본문에서 직접 발화:

> "I don't have read access to the target repo, so I'll build the plan from standard go-ethereum receipt/snap-sync patterns plus the Anzeon-specific clue (`eth/gasprice/anzeon.go`)."

→ **mock의 hash 기반 검색이 agent에게 useful context를 제공 못함**. agent가 hints 무시하고 general knowledge로 plan 작성. **bgeonnx로 가야 진짜 측정**.

### 3.4 Agent는 문제를 정확히 이해함 (right intent, alternative solution)

평균 file overlap 3/8 — 우연 아님. 핵심 파일(`core/types/receipt.go`, `core/types/receipt_test.go`, `core/rawdb/accessors_chain.go`)이 반복 등장. judge rationale 인용:

> "Plan correctly identifies core files and the snap-sync-loses-EffectiveGasPrice problem, but proposes persisting the value via extended storage RLP encoding whereas the actual fix recomputes it from headerGasTip plus an AuthorizedTxExecuted log marker."

→ **Plan은 valid alternative**. 단지 PR이 채택한 접근과 다름. judge가 "intent match"를 봐서 0.50 부근에 머무름. 이건 임베더 한계라기보다 **PR-eval 평가 패러다임의 inherent 특성** (정답 1개가 아님).

---

## 4. 1.3 요구사항 대비 위치

| 측면 | 요구 | 현재 | 갭 |
|---|---|---|---|
| Pipeline 작동 | 동작 가능 | ✅ 완전 작동 | — |
| 유사도 ≥ 80% | 단일 측정 | mean 0.44 (1/5 통과) | **-0.36** |
| Noise band | 미명시 | σ = 0.25 | **재정의 필요** |
| Embedder 영향 | 미명시 | mock 한계 명백 | bgeonnx 측정 필요 |

**결론**: pipeline은 완성. threshold 도달은 임베더 + LLM noise 두 차원 해결 후 재측정.

---

## 5. Follow-up 작업

작업 우선순위 + 예상 비용:

| ID | 작업 | 비용 | 우선순위 |
|---|---|---|---|
| **PRR-1** | bgeonnx 임베더로 PR #70 재측정 (5-run) | ~7시간 (1.4h build × 5 = 매번 worktree 재인덱싱) 또는 ~1.5h (worktree cache + 5회 query/agent/score) | High — 진짜 가치 측정 |
| **PRR-2** | LLM noise 정량 명시 — `--pr-runs=N` flag로 multi-run 평균 내장 | ~2시간 | Mid — 측정 신뢰도 |
| **PRR-3** | Fixture 확장 (PR #70 외 4-5건 추가) | ~1일 (대상 PR 선정 + fixture entry) | Mid — 통계 신뢰 |
| **PRR-4** | Agent prompt 튜닝 — "use ckv hints, do not invent files" 강제 | ~2시간 | Low — 변동성 큼, 효과 미보장 |
| **PRR-5** | Judge rubric 재디자인 — alternative solution 인정 vs 정답 매치 | ~3시간 | Low — 평가 패러다임 결정 필요 |

**다음 자연스러운 단계**: **PRR-1** (bgeonnx 재측정) — 사용자 시간 있는 별도 turn에 진행. mock vs bge 직접 대립 포인트 측정이 임베더 선택 결정의 결정적 근거.

---

## 6. Related

- 평가 방법론: [`eval-metrics.md`](./eval-metrics.md)
- D1 PoC 측정 (recall@5/MRR): [`d1-onnx-poc.md`](./d1-onnx-poc.md) §3.3
- Plan + Decision Audit: [`review-direction-2026-05-18.md`](./review-direction-2026-05-18.md)
