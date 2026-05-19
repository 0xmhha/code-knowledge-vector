# CKV 방향성 검토 — 2026-05-18

> **문서 버전**: 1.1
> **작성일**: 2026-05-18 / **승인일**: 2026-05-19
> **상태**: ✅ **APPROVED** — Phase 4 최종 게이트 통과 (autoplan re-run 2026-05-19)
> **연관 문서**: [`plan-S1-ckv.md`](./plan-S1-ckv.md), [`featurelist.md`](./featurelist.md), [`use-cases.md`](./use-cases.md), [`d1-onnx-poc.md`](./d1-onnx-poc.md)
> **목적**: 사용자가 정의한 3개 핵심 요구사항(1.1 vector DB 구축, 1.2 RAG eval + BM25 re-rank, 1.3 PR 기반 회귀 테스트 ≥80%)에 대해 현재 CKV의 구현 상태를 4-phase 리뷰(Scope / Engineering / DevEx / Final Gate)로 평가하고 다음 액션을 도출한다.
> **모드**: `[single-voice]` — autoplan procedure를 따랐으나 외부 voice(`consult-codex` 등) 미가용으로 단일-리뷰어 평가.
>
> **2026-05-19 재실행 결과**: 1.0 본문의 모든 결론 유효. 정합성 추가 확인 후 사용자 게이트 A) As-is 승인. 사용자 확정 결정 4개 본문 반영 (§6.1·§9). commit `6f4bf1e` (ModelConfig refactor) 후 Eng 점수 7→7.5 상향.

---

## 0. 검토 대상 — 사용자 요구사항(원문 요약)

본 프로젝트(CKV)는 coding agent의 한 구성요소로, **새 기능 구현 설명 → 관련 코드 위치/키워드 후보 → CKG 그래프로 확장**의 흐름을 담당한다. 이를 위해 다음이 필수:

- **1.1** 지정 프로젝트 코드를 분석해 vector DB 생성. 적절한 chunking + embedding 적용. evaluation 결과에 따라 설정값을 변경하면서 최적값을 찾을 수 있어야 함.
- **1.2** 생성된 vector DB에 대해 RAG 방식의 evaluation agent 구축. chunking + embedding + **BM25 re-rank** 적용. 모호한 입력에도 핵심 코드를 찾아내는지 검증 가능해야 함.
- **1.3** 이미 구현된 PR을 지정해 **해당 PR 적용 이전 코드로 checkout** 후 학습 → 테스트 응답이 **실제 PR 수정 방향과 유사도 80% 이상**이 되도록 함.

### 0.1 지원 대상 언어 (Corpus 범위)

본 프로젝트가 인덱싱·검색해야 할 코드 corpus의 언어 범위:

| 확장자 | 언어 | 현재 상태 (2026-05-19) | 비고 |
|---|---|---|---|
| `*.go` | Go | ✅ 구현 (`internal/parse/golang/`) | S0/S1 1순위 |
| `*.ts`, `*.tsx` | TypeScript | ✅ 구현 (`internal/parse/typescript/`, W3-T9) | |
| `*.sol` | Solidity | ✅ 구현 (`internal/parse/solidity/`, W3-T10) | vendored tree-sitter |
| `*.js`, `*.jsx` | JavaScript | ❌ **S2 이관** (사용자 결정 2026-05-19) | `detectLanguage()`가 unknown 처리. parser 없음. S2 milestone에서 신설. |
| `*.sh`, `*.bash` | Bash | ❌ **S2 이관** (사용자 결정 2026-05-19) | featurelist §1.2에서 S2 이관으로 결정 (영구 제거 아님). |

JavaScript/Bash 파서 신설은 S2 milestone으로 이관된다 (사용자 결정 2026-05-19, featurelist §21.1). 본 문서 §3.4와 §Appendix B.1.a의 *언어 확장* 차원은 S2 작업으로 분류.

---

## 1. Executive Summary

| 요구 | 충족도 | 핵심 갭 |
|---|---|---|
| **1.1** vector DB + chunking/embedding + eval-기반 튜닝 | **70%** | (a) 설계 원칙·docs(`*.md`/ADR) corpus 미포함 (b) 자동 튜닝 loop 없음 (수동 1회성) |
| **1.2** RAG eval + BM25 re-rank + 모호 입력 대응 | **40%** (정책 충돌) | BM25는 plan §7.5로 CKG에 위임 — CKV 단독 평가는 vector-only |
| **1.3** PR-기반 회귀 테스트 ≥80% | **0%** | 전무. 메타-요구이므로 1.1·1.2 객관 검증 자체 불가 |

**가장 시급한 갭**: 1.3 (PR-regression). 이게 없으면 다른 두 요구의 "올바른 방향"을 자기-증명 불가능. **Cross-phase theme**: Scope·Engineering·DevEx 3개 phase에서 독립적으로 동일 갭 식별 → 고신뢰 신호.

**현재 측정 baseline** (`d1-onnx-poc.md §3.3`, N=10, bge-large-en-v1.5):
- recall@5 = 1.000 / MRR = 0.770 / p95 latency = 43ms (warm) / build throughput = **1.6 chunks/s**
- 1M LOC 시연에 ~35시간 → UC-V1 success criteria(<10분)과 **50× 충돌**.

---

## 2. Phase 0 — Intake & Scope Detection

### 코드베이스 상태(2026-05-18)
- Branch: `main`, 최근 commit: `924b974 feat: D1 PoC pivot to bge-large-en-v1.5 — E2E recall@5=1.0, MRR=0.77`
- Milestone 진행: M0 + M1 + M2 + M3 + M5(MCP read-only) + M6(부분) 진입, W3-W4 마무리 단계.
- 코드 레이아웃: `internal/{build,chunk,discover,embed,eval,footprint,freshness,judge,manifest,parse,projectcfg,query,store}` + `pkg/{mcp,types}` + `cmd/ckv/`.

### Scope Flag
- **UI scope**: NO → Phase 2 (Design) skip.
- **DX scope**: YES (CLI + MCP; agent + 개발자가 primary persona) → Phase 3.5 실행.
- **리뷰 스킬**: `review-{scope,engineering,devex}` 가 환경에 별도 skill로 부재 → `[single-voice]` 태그, autoplan procedure 자체의 override 규칙으로 진행.

---

## 3. Phase 1 — Scope Review

### 3.1 전제 게이트

| 전제 | 평가 |
|---|---|
| **P1** LLM의 컨텍스트 부족을 vector knowledge로 보강 | **유효**. use-cases.md UC-V3/V8 정합, 업계 검증된 패턴 |
| **P2** 비지니스 로직·설계 원칙·결정사항을 dataset에 포함 | **⚠️ 부분 지원**. 현재 `discover`/`chunk`는 source 파일만. `*.md` / ADR / docs corpus 미포함 → **가장 큰 scope 갭** |
| **P3** 키워드 후보로 CKG 그래프 확장 | **유효, 이미 설계 반영**. `plan §7.3 query_code` 흐름 정확히 일치. 단, fusion은 CKS 책임 |

### 3.2 요구사항 vs 현재 구현 매핑

| 요구 | 현재 상태 | 갭 | 분류 |
|---|---|---|---|
| 1.1 | sqlite-vec ✅, chunker ✅, bgeonnx ✅, eval harness ✅ | (a) docs corpus (b) auto-tune loop | TASTE(b) + USER CHALLENGE(a) |
| 1.2 | eval harness 50% | BM25/re-rank는 CKV scope 밖 (정책 §7.5) | **USER CHALLENGE** |
| 1.3 | 전무 | 정의·구현 모두 없음 | **USER CHALLENGE** |

### 3.3 "이미 존재하는 것" 맵

| 하위 문제 | 기존 코드 |
|---|---|
| chunking | `internal/chunk/chunk.go` (symbol + file_header) |
| embedding 인터페이스 | `internal/embed/{mock,bgeonnx}` |
| ANN store | `internal/store/sqlitevec` |
| eval metrics (recall/MRR/citation) | `internal/eval/{eval,score,fixture}.go` |
| LLM-as-judge | `internal/judge/judge.go` (Claude CLI headless) |
| MCP 표면 | `pkg/mcp` (read-only) |
| footprint logging | `internal/footprint` (JSONL) |
| per-project config | `ckv.yaml` skill hook (`internal/projectcfg`) |

### 3.4 Scope Expansion 6-원칙 자동 판정

| 항목 | 원칙 | 결정 |
|---|---|---|
| **JS 파서 신설** (`*.js`/`*.jsx` corpus 추가, §0.1) | P1 + P2 (기존 TS parser 패턴 재사용, blast radius 안, ~4 파일) | **S2 이관** (사용자 결정 2026-05-19) |
| docs corpus 추가 (`*.md`, ADR) | P1 + P2 (blast radius 안, ~5 파일) | **AUTO-APPROVE 권고** |
| BM25 인덱스를 CKV 안에 추가 | P2 borderline (FTS5 + schema migration, ~5 파일) | **USER CHALLENGE** |
| PR-regression harness (1.3) | P2 borderline (신규 모듈, ~6-8 파일) | **TASTE** |
| 자동 튜닝 loop (1.1 후반) | P2 위반 (신규 인프라) | shell wrapper로 우선 처리 후 defer |

### 3.5 "NOT in scope" (현재 plan 유지 권고)
- BM25/FTS5 — CKG 책임(`pkg/bm25/scorer.go` 존재), fork 금지 (P4 DRY)
- RRF fusion — CKS 책임(`cks-mcp` 별도 repo)
- Sanitize default-deny — S2
- HTTP API — MCP만 S1

### 3.6 실패 모드 (Scope)

| ID | Trigger | 영향 |
|---|---|---|
| FM-S0 | JS 파서 부재 → `*.js`/`*.jsx` 파일 unknown 분류·인덱싱 누락 → JS heavy repo는 S2 milestone까지 지원 불가 | Medium (S2 이관 결정으로 risk 격리됨) |
| FM-S1 | docs corpus 부재 → "knowledge dataset" 명목상 충족, 실질 미충족 | Medium |
| FM-S2 | 1.3 미구현 → "올바른 방향 검증" 불가 | **Critical** |
| FM-S3 | BM25 없이 exact-symbol query 실패 (CKS 없는 단독 사용 시) | Medium |

---

## 4. Phase 3 — Engineering Review

`[single-voice]`. Engineering phase는 P5(explicit) + P3(pragmatic) 우선.

### 4.1 아키텍처 흐름

```
ckv build → discover → parse(tree-sitter Go/TS/Sol; ✗JS 미구현 → 신설 필요) → chunk → embed(mock|bgeonnx) → store(sqlite-vec) → manifest
ckv query → embed(intent) → store.Search(k*3) → citation enforce → threshold drop → ranked top-k
ckv eval  → Run(eng, fixture) → per-query Score → recall@k/MRR/citation_accuracy → optional Claude CLI judge
ckv mcp   → stdio JSON-RPC → cks.context.semantic_search | cks.ops.{get_freshness,health}
```

**경계 분리**: 깔끔. `Embedder` 인터페이스로 모델 교체 가능. P5 충족.

### 4.2 발견 (Critical first)

#### EG-1. PR-regression test 부재 — **CRITICAL**

`internal/eval/eval.go::Run(eng, fx)`은 *현재 시점 인덱스*에 대한 정적 평가만 수행. PR-기반 회귀 평가 흐름은 미구현:

```
pre-PR commit checkout (worktree 권장)
  → ckv build
  → query = (PR title + body + issue 본문)
  → response = top-k chunks
  → ground-truth = PR이 실제 변경한 파일/심볼 set
  → similarity = |response ∩ ground_truth| / |ground_truth|  (Jaccard) 또는 F1
  → threshold gate (≥0.80)
```

신설 권고 모듈 구조:
```
internal/eval/prregress/
  ├── fetcher.go    # gh/git API로 PR 메타 수집
  ├── checkout.go   # detached worktree로 base SHA checkout
  ├── ground.go     # changed files → symbol set 추출 (CKG join 또는 diff parse)
  ├── score.go      # Jaccard / F1 / threshold gate
  └── runner.go     # 배치 PR 처리
cmd/ckv/eval.go     # --pr-fixture=prs.yaml 플래그
testdata/prs.yaml   # {pr_url, base_sha, expected_files[]} 리스트
```

**Auto-decide**: P1(완성도) + P6(action 편향) — W4-T4 또는 S1.5로 즉시 배치.

#### EG-2. BM25 부재 (1.2 정책 충돌)

`testdata/queries.yaml` q1~q10이 자연어 위주라 vector-only로 recall@5=1.0이 나오지만, 사용자 시나리오의 "PR 설명"은 일반적으로 **심볼명·식별자·error message 텍스트가 섞임** — vector embedding은 lexical exact-match에 약함. q="`ErrIndexUnavailable`을 리턴하는 곳" 같은 query는 cosine만으로 해결이 불안정.

세 가지 viable 옵션:
- **(A) CKV가 CKG의 BM25를 lib import** — DRY 준수, plan §7.5 정책 위반
- **(B) sqlite-vec와 같은 DB에 FTS5 가상 테이블 추가 + RRF 헬퍼** — sqlite-vec 표준 패턴, ~30 LOC
- **(C) CKS 통합 binary로만 처리** — 현재 plan 유지, CKV 단독 평가는 vector-only로 한정

권고(P3 + P5): **(B)**. SQLite + FTS5 + sqlite-vec 결합은 공식 example에 등장하는 자연스러운 조합. 단, 이는 plan §7.5의 책임 경계 재협상이 필요 → **USER CHALLENGE**.

#### EG-3. 임베딩 정확도 — MRR 0.77 (D1 hypothesis 0.85 미달)

bge-large-en-v1.5는 general-text 모델 (code-trained 아님). q5(`retrieve value by key`)가 top-5의 5위로 등장. 두 갈래 병행:
- 단기: fixture를 50+ queries로 확장 (D1-FU-7). N=10은 통계적 약함.
- 중기: bge-code-v1 Qwen2 adapter (D1-FU-6, D2 scope).

**Auto-decide**: P6 — recall@5=1.0이라 blocker 아님. 리포트에 flag, D2로 이관.

#### EG-4. 자동 튜닝 loop 미구현 (1.1 후반)

현재 `cmd/ckv/eval.go`는 1회성. sweep loop 없음. 세 옵션:
- **(A) shell + JSON output으로 외부 grid sweep** — 0 LOC, 즉시 가능
- (B) `ckv eval sweep --config-grid=sweep.yaml` 내장 — ~3일
- (C) Optuna/Hyperband — 신규 의존성, over-engineering

권고(P5 + P3 + P6): **(A) 부터 시작**. eval이 이미 JSON 출력 → docs에 shell template 추가만으로 즉시 충족.

### 4.3 테스트 다이어그램

| 코드 경로 | 테스트 유형 | 상태 |
|---|---|---|
| chunk symbol-level | unit (`internal/chunk/chunk_test.go`) | ✅ |
| sqlite-vec upsert/search | integration | ⚠️ 확인 필요 |
| eval Score / Summarize | unit (`internal/eval/eval_test.go`) | ✅ |
| judge CLI parse | unit (`internal/judge/judge_test.go`) | ✅ |
| bgeonnx 임베딩 numerical | smoke (build tag) | ✅ |
| **PR-regression flow** | E2E | ❌ |
| **BM25 / hybrid** | — | ❌ (scope-out) |
| **Real-repo integration** | integration | ⚠️ testdata/sample 4 파일에 그침 |
| **Determinism (동일 query 2회 동일)** | property | ⚠️ 검증 코드 확인 필요 |

### 4.4 Performance / Scale

| 지표 | 현재 | 목표 | 갭 |
|---|---|---|---|
| Build throughput | 1.6 chunks/s | UC-V1 500K LOC <10분 ≈ 170 chunks/s | **~100×** |
| Query p95 (warm) | 43 ms | ≤200 ms | 여유 (1/4) |
| Cold start | ~1.5s | — | 허용 |

**Auto-decide**: D1-FU-8(batch + CoreML EP)을 next priority (P1 + P2). 미해결 시 1.1의 "전체 프로젝트 인덱싱" 자체가 비현실적.

### 4.5 실패 모드 (Engineering)

| ID | Trigger | Severity | Mitigation |
|---|---|---|---|
| FM-E0 | JS 파서 미구현 → `*.js`/`*.jsx` 파일 인덱싱 누락 | Medium (S2 이관 결정으로 격리) | S2 milestone에서 `internal/parse/javascript/` 신설 (Appendix B.1.a §언어 확장) |
| FM-E1 | 대형 repo 인덱싱 17h+ | High | D1-FU-8 batch + CoreML EP |
| FM-E2 | exact-symbol query miss | Medium | EG-2 (B) FTS5 |
| FM-E3 | 모델 파일 missing/checksum mismatch | High | `IndexUnavailable` + `ckv model fetch` (D1-FU-4) |
| FM-E4 | 1.3 미구현 → 회귀 미탐지 | **Critical** | EG-1 즉시 |
| FM-E5 | docs corpus 없음 | Medium | discover walker 확장 |
| FM-E6 | sqlite-vec 1M chunk p95 미측정 | Medium | open decision q3 측정 필요 |

---

## 5. Phase 3.5 — DevEx Review

Primary persona = **coding agent (CKS Orchestrator)**, Secondary = **개발자/Ops (CLI)**. P5 + P2 우선.

### 5.1 Developer Journey

**Persona A — Coding Agent (vibe-coding loop)**
1. agent가 feature 설명 받음 → `cks.context.semantic_search(intent)`
2. CKS가 CKV + CKG multiplex → EvidencePack
3. agent가 `file:line` 인용 기반으로 코드 수정 제안
4. stale 시 `cks.ops.get_freshness` / `cks.ops.health` 로 감지

✅ 잘 설계됨. `pkg/mcp` read-only 표면 명료.

**Persona B — 개발자 (local CLI)**
1. `make build` → `bin/ckv`
2. `ckv build --src=. --out=./ckv-data`
3. `ckv query "intent"`
4. `ckv eval --fixture=...`

✅ README Quickstart 4단계 명료. ⚠️ bgeonnx prod 사용 시 `brew install onnxruntime` + libtokenizers.a 수동 + 2.5GB 모델 fetch + CGO_LDFLAGS → **TTHW ~10-15분**. `ckv model fetch`(D1-FU-4) 미구현이 마찰 포인트.

### 5.2 에러 메시지 품질

샘플 `grep`(`internal/embed/bgeonnx/*.go`) 기준:
- **강함**(원인+fix hint 포함): `tokenizer.json missing at %s`, `output is %T, want *Tensor[float32] — check ONNX export FP32 vs FP16`, `every input produced zero tokens — tokenizer.json likely invalid`, `row %d has all-zero attention — empty input?`
- **약함**(단순 wrap): `session options: %w`, `create ONNX session: %w`

**Auto-decide**: P6 — 도메인 에러는 production-quality, 라이브러리 wrap은 점진 개선. blocker 아님.

### 5.3 DX 스코어카드

| 차원 | Score | 근거 |
|---|---|---|
| TTHW (mock embedder) | 9/10 | 30초 |
| TTHW (bgeonnx prod) | 5/10 | 라이브러리 설치 + 2.5GB 모델 + CGO_LDFLAGS 수동 |
| 에러 메시지 품질 | 7/10 | 도메인 에러 강함, lib wrap 약함 |
| API/CLI 일관성 | 8/10 | Cobra 표준, flag naming 일관 |
| Agent-side MCP DX (read-only) | 8/10 | 명료한 `cks.context.*`, `cks.ops.*` |
| Agent-side MCP DX (read-write) | n/a | 미구현 (planned) |
| Eval-developer DX (튜닝 loop) | 4/10 | 1회성만, sweep 없음 |
| 문서 | 8/10 | README + plan + featurelist + use-cases + d1-poc + install-guide |

### 5.4 Magical Moment

**현재**: `ckv query "TCP socket bind on port"` → `server.go:22-29` 정확 인용. **이미 D1 PoC에서 시연**. ✅

**다음 후보**: `ckv eval --pr-fixture=prs.yaml --threshold=0.8` → "이 PR이 만든 변경의 80% 이상을 정확히 짚어낸다" 출력 (요구 1.3). 이게 finished feature 되는 순간 CKV의 차별성 입증.

### 5.5 TASTE — PR-eval CLI shape

- **(X) `ckv eval --pr-fixture=prs.yaml --threshold=0.8`** — batch, 기존 eval과 일관 → **추천**
- (Y) `ckv eval pr <url>` — single-shot subcommand, command tree 분기 → defer

---

## 6. Phase 4 — 최종 게이트

### 6.1 ⚠️ User Challenges (모델이 사용자 선언 방향에 disagree)

**Challenge 1 — BM25 re-rank의 위치** (from Scope/Engineering) — **사용자 결정: 개발단계 dual-track**

- 사용자 결정 (2026-05-18): "현재 CKG·CKV 모두에 BM25 적용 (개발 단계). 실 동작 확인 후 최종 위치 결정."
- 사용자 추측: BM25는 coding agent에 두고 MCP와 지속 검토·개선하는 흐름이 맞을 수도.
- **베스트 프랙티스 권고** (모델 분석):
  - **Retrieval primitives는 데이터에 가까이, Reasoning은 agent에** — 이 원칙으로 분리하면 *"rerank"라는 단어가 두 가지 다른 것*을 가리킴을 깨닫게 됨:
    - **(a) Score fusion (RRF)**: 다중 backend의 ranked list 융합. 인덱스 메타만 필요 → **CKS 위치 적절**.
    - **(b) Sparse lexical retrieval (BM25)**: index time에 token frequency 통계 필요 → **데이터(CKV/CKG) 옆에 있어야 효율적**. Network round-trip 회피 + 인덱스 일관성 보장.
    - **(c) Semantic rerank (LLM-as-reranker / cross-encoder + query reformulation)**: 후보의 의미를 재평가. 추론 필요 → **coding agent 위치 적절**. ← 사용자가 "MCP와 지속 검토·개선"이라 표현한 부분은 실제로 이쪽.
  - **두 BM25는 사실 다른 corpus**:
    - CKG의 BM25: `(qname + signature + doc_comment)` corpus — symbol-level lexical match.
    - CKV의 (제안) FTS5: `chunk text` corpus — code-body lexical match.
    - **다른 corpus = 다른 도구**. DRY 위반 아님. 단, 둘 중 하나로 *수렴할 명분이 없는 한* 유지.
- **권고 (수렴 시점에 결정할 것들)**:
  1. BM25 *retrieval*은 CKV·CKG에 그대로 유지 (lexical baseline).
  2. RRF *score fusion*은 CKS에서 multiplex (현 plan §7.5와 정합).
  3. Coding agent는 *semantic rerank* (cross-encoder 또는 LLM-as-reranker) 와 query reformulation 담당 — 이게 사용자 의도의 "지속 검토·개선" 본질.
- **틀리면 비용**: 사용자 추측대로 BM25 자체를 agent로 옮기면 (i) 모든 query마다 raw 인덱스 + tokens를 MCP wire로 전송 (50KB+) → latency 증가, (ii) BM25 계산을 agent마다 재구현 → 다중 client 환경에서 fork.
- **기본값 (2026-05-18 결정)**: 현재 dual-track 유지, 동작 검증 후 §7.5 수렴 결정 재검토. **Decision 분류**: TASTE → 검증 phase로 이관.

**Challenge 2 — 설계 원칙·결정사항 corpus** (from Scope) — 상세는 [Appendix B](#appendix-b--corpus--adr-기술-배경) 참조.
- 사용자 선언: "**설계 원칙**과 운영상의 이유로 결정한 **결정사항들** ... 지식데이터 셋을 구축"
- 핵심 용어 (Appendix A 참조): **corpus** = 인덱싱 대상 텍스트 집합, **ADR** = Architecture Decision Record (설계 결정을 코드 옆에 markdown으로 기록).
- 모델 추천: `internal/discover` + `internal/chunk` 확장하여 `*.md` / ADR / `docs/**/*.md` 를 `chunk_kind="doc"` 으로 인덱싱. blast radius 안 (~3 파일, <1일).
- **왜 필요한지** (자세히 §Appendix B): 코드 주석은 *what*만 남기고 *why*는 거의 안 남음. "왜 sqlite-vec를 골랐는지", "왜 RRF는 CKS 책임인지" 같은 의사결정은 `plan-S1-ckv.md` / ADR / commit description에만 존재. 이게 corpus에 들어가야 agent가 사용자 의도의 "결정사항을 고려한 개발" 수행 가능.
- 놓쳤을 수 있는 것: 회사 docs가 별도 repo (Confluence/Notion/Slack) 에 있으면 local walker로 불가능 — external connector가 추후 필요.
- 비용: docs에 실제 결정 사유 안 적혀 있으면 dead weight (저비용, P2 위반 아님).
- **기본값**: 명시적 거부 없으면 docs walker 추가 (Appendix B의 §B.3 구현 스케치).
- ✅ **사용자 결정 (2026-05-19)**: 추가 — 사용자 의도 정합. `*.md`/ADR/`docs/**/*.md`를 `chunk_kind="doc"`으로 인덱싱.

**Challenge 3 — PR-regression test 구체 타겟 확정** (from Scope/Engineering) — 상세는 [Appendix C](#appendix-c--pr-70-회귀-테스트-타겟-구체) 참조.

- 사용자 결정 (2026-05-18) — **구체 타겟 명시**:
  - **Repo**: `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest`
  - **Base commit (checkout 대상)**: `aa28927fb12048a59ac34608702eef5e1be90931`
  - **Target PR**: [stable-net/go-stablenet#70](https://github.com/stable-net/go-stablenet/pull/70) — `fix: fill missing effectiveGasPrice in receipts on derivation`
- 사용자 정의 **새로운 평가 흐름** (1.3 원안에서 진화):
  1. base commit으로 checkout.
  2. ckv build로 학습.
  3. agent에게 **PR #70 description의 "문제(Background)" 만 인식** 하도록 입력 (Solution 부분은 숨김).
  4. agent가 *수정을 어떻게 수행해야 할지* 검토하여 **구현 plan을 별도 문서로 작성**.
  5. **실제 PR #70 수정 코드 (diff) ↔ 문서화된 plan** 두 산출물을 비교.
  6. plan대로 구현했다면 PR#70의 수정 코드와 얼마나 유사한지 **유사도율 수치 산출**.
- **이전 안과의 차이**: 원안은 "agent의 top-k chunk 응답"과 "실제 변경 파일/심볼 집합"을 직접 Jaccard/F1 비교. 사용자 새 안은 "agent의 *plan 문서*"와 "실제 *수정 코드*"를 비교 → **plan ↔ implementation 유사도** 측정. 더 의미 있는 평가 (top-k 검색 정확도가 아니라 *end-to-end agent 행동의 정확도* 측정).
- 메트릭 선택지 (Appendix C §C.4 비교):
  - File-set Jaccard (얕음, 손쉬움)
  - Symbol-set F1 (중간, CKG join 필요)
  - **LLM-as-judge with rubric** (사용자 의도 가장 가까움) — plan과 실제 diff를 LLM이 읽고 "이 plan대로 구현하면 실제 PR과 같은 결과가 나올까" 점수
- **기본값**: Appendix C에 명시된 평가 flow를 W4-T4로 신설 + 메트릭은 LLM-judge primary + F1 secondary로 dual report.
- ✅ **사용자 결정 (2026-05-19)**: 메트릭 = **LLM-judge primary + F1 secondary** (추천안 그대로 채택). plan↔diff 의미 평가가 사용자 의도(end-to-end agent 행동 정확도)와 가장 가까움.

### 6.2 Taste Choices (auto-decided, override 가능)

| # | Phase | 결정 | 원칙 | 거부된 대안 |
|---|---|---|---|---|
| C1 | Eng | 튜닝 loop는 shell + JSON sweep (A) 우선 | P5 + P6 | (B) `ckv eval sweep` 내장 |
| C2 | DevEx | PR-eval CLI shape = `--pr-fixture=prs.yaml` (X) | P5 | (Y) `ckv eval pr <url>` subcommand |
| C3 | Eng | bge-code-v1 Qwen2는 D2 유지 | P6 | 즉시 작업 |

### 6.3 Auto-Decided

| # | Phase | 결정 | 원칙 |
|---|---|---|---|
| A0 | Scope/Eng | **JS/Bash 파서 S2 이관** — 사용자 결정 2026-05-19 (즉시 신설 reversal) | P6 (action 편향, S1 stable 우선) |
| A1 | Eng | EG-1 (1.3 PR-regression)을 W4-T4 / S1.5에 즉시 배치 | P1 + P6 |
| A2 | Eng | D1-FU-8 (batch + CoreML EP) priority 상향 | P1 + P2 |
| A3 | DevEx | 라이브러리-wrap 에러 fix-hint 개선은 flag만, blocker 아님 | P6 |

### 6.4 Review Scores

| Phase | Score | 비고 |
|---|---|---|
| Scope | 6.5/10 | docs corpus 갭 + 1.3 미구현 |
| Design | — | UI scope 없음 → skip |
| Engineering | **7.5/10** (2026-05-19 갱신) | architecture clean (commit `6f4bf1e` refactor로 model 추가 면적 5개→1개 파일로 집중), PR-regression critical gap + throughput 100× 미달 |
| DevEx | 7/10 | read-only MCP DX 우수, prod 임베더 설치 마찰 + 튜닝 loop 미구현 |

### 6.5 Cross-Phase Theme

- **Theme 1 — "1.3 PR-regression 부재"**: Scope·Engineering·DevEx 3개 phase 모두에서 독립 식별. 고신뢰도 신호. **최우선**.
- **Theme 2 — "설계 원칙 corpus 누락"**: Scope 전제 P2 + Engineering FM-E5 재등장. 작은 변경(~3 파일)으로 큰 의미 확보 가능.

### 6.6 Deferred to TODOS (auto-defer)

- BM25 in CKV (Challenge 1 결정 전 보류)
- Auto-tuning loop 내장 (`ckv eval sweep`, Choice C1의 (B))
- bge-code-v1 Qwen2 adapter (D1-FU-6, D2 scope)
- linux/amd64+arm64 CI matrix (D1-FU-5)
- 50+ query fixture 확장 (D1-FU-7)

---

## 7. Decision Audit Trail

| # | Phase | Decision | Classification | Principle | Rationale | Rejected |
|---|---|---|---|---|---|---|
| 0 | Scope/Eng | **JS/Bash 파서 S2 이관** | User Decision (reversal 2026-05-19) | P6 | S1 stable 우선; JS는 S2 milestone에서 TS parser 패턴 재사용해 신설 예정 | 즉시 신설 (v1.2 결정) |
| 1 | Scope | 전제 P1 (LLM context 부족) 채택 | Mechanical | P1 | use-cases.md 정합 + 업계 검증 | — |
| 2 | Scope | 전제 P2 (설계원칙 corpus) → USER CHALLENGE | User Challenge | P1 | 현재 미구현, 사용자 의도와 갭 | corpus 확장 안 함 |
| 3 | Scope | docs corpus 확장 → auto-approve 권고 | Mechanical (proposal) | P2 | ~5 파일, <1일 | — |
| 4 | Scope | 1.2 BM25 in CKV → 개발단계 dual-track 유지 (사용자 결정) | Taste (deferred) | P6 | 동작 검증 후 수렴 결정 재검토 | 즉시 수렴 |
| 5 | Scope | 1.3 PR-regression → PR #70 구체 타겟 + plan↔diff 유사도 새 흐름 | User Challenge → 사용자 정의 새 안 채택 | P1 + P6 | 메타-요구, 미구현 시 평가 불가 | defer 거부 |
| 6 | Eng | D1-FU-8 throughput 우선 | Mechanical | P1 + P2 | 100× 미달, 1M LOC 시연 불가 | defer 거부 |
| 7 | Eng | bge-code-v1 Qwen2는 D2 유지 | Taste | P6 | recall@5=1.0, MRR은 blocker 아님 | 즉시 작업 |
| 8 | DevEx | eval sweep은 shell + JSON (A) 우선 | Taste | P5 + P6 | 0 LOC, P2 위반 없음 | 내장 sweep (B) |
| 9 | DevEx | PR-regression CLI = `--pr-fixture` batch (X) | Taste | P5 | 기존 eval과 일관 | subcommand (Y) |
| 10 | Gate | **APPROVED — Overall A) As-is 승인** (2026-05-19) | Mechanical (user gate) | — | 4개 즉시 결정 모두 확정. autoplan PROCEDURE 옵션 A 처리. | B/B2/C/D/E |
| 11 | Scope | Challenge 2 docs corpus 추가 — **사용자 확정** (2026-05-19) | User Challenge → User Approved | P1 + P2 | 사용자 의도 정합 ("설계 원칙·결정사항 dataset 포함"). `chunk_kind="doc"` 신설. | corpus 확장 안 함 |
| 12 | Scope/Eng | Challenge 3 메트릭 = LLM-judge primary + F1 secondary — **사용자 확정** (2026-05-19) | User Challenge → User Approved | P1 | plan↔diff 의미 평가가 사용자 의도와 가장 가까움 | F1 only / Jaccard only |
| 13 | Scope/Eng | A0 JS/Bash 파서 — **S2 이관 reversal** (2026-05-19 후속 결정) | User Decision (reversal) | P6 | S1 stable 유지 우선. featurelist §21.1에 결정 row 기록. JS는 S2 milestone에서 TS parser 패턴 재사용해 신설. | 즉시 시작 (#13 이전 안) |
| 14 | Eng | Eng 점수 7→**7.5** (commit `6f4bf1e` refactor 반영) | Mechanical | P5 | 모델 추가 면적 5개→1개 파일로 집중 → architecture clean 강화 | 점수 유지 |

---

## 8. Fact-based Answer

**Fact** (확신도 None — 추론 아닌 사실):
- D1 PoC 측정값: recall@5 = 1.000, MRR = 0.770, p95 latency = 43ms (warm), build throughput = 1.6 chunks/s (`d1-onnx-poc.md §3.3`).
- `internal/eval` 모듈은 recall@k / MRR / citation accuracy / optional Claude CLI judge를 구현.
- `plan-S1-ckv.md §7.5` 명시: "BM25는 CKG의 `pkg/bm25/scorer.go`에 있으며 CKV는 자체 구현 안 함."
- `testdata/queries.yaml` = 10 queries (Go / TS / Solidity).
- `internal/discover`는 source code 파일만 walk. `*.md` 인덱싱 코드 없음.
- PR-checkout 기반 regression test 코드는 부재 (`grep -r "checkout.*pr" docs/ internal/` = 0건).

**Your Opinion**:
- **High prediction**: 1.3 PR-regression test가 single most important next step. 이게 없으면 1.1·1.2의 "올바른 방향" 자체를 객관 검증 불가.
- **High prediction**: 1.2 BM25/re-rank는 사용자 의도와 plan §7.5 정책의 충돌 — User Challenge로 명시 결정 필요.
- **Mid prediction**: 1.1의 "설계 원칙 corpus"는 `*.md` walker 추가만으로 80% 해결. ADR repo면 즉시 효과.
- **Mid prediction**: bge-large-en-v1.5의 MRR 0.77은 fixture 50+ 확장으로 신뢰구간 좁히면 0.85 근접 가능. Qwen2 adapter는 D2 유지가 ROI 우위.
- **Low prediction**: 1M LOC 시연 throughput 미달이 stakeholder demo 일정에 부딪힐 가능성 — D1-FU-8 작업 미리 분리 시작 권장.
- **None**.

---

## 9. 다음 액션 — 사용자 결정 대기

다음 옵션 중 선택해주세요:

- **A** As-is 승인 (모든 추천 수락 — Challenge 1 default (B), Challenge 2 적용, Challenge 3 W4-T4 신설)
- **B** Override와 승인 — 어떤 taste 결정/Challenge를 변경할지 지정
- **B2** User Challenge 응답 — Challenge 1·2·3 각각 수락/거부
- **C** Interrogate — 특정 결정에 대해 추가 질문
- **D** Modify — plan 자체 변경 (영향받은 phase 재실행)
- **E** Reject — 처음부터

### 결정 완료 항목 (모두 확정됨)

1. **JS/Bash 파서** — ✅ 사용자 결정 (2026-05-19 후속 reversal): **S2 이관**. featurelist §21.1 결정 row + §1.2 본문 정정 완료. (이전 v1.2 "즉시 신설" 결정 → v1.3에서 reversal)
2. **Challenge 3 메트릭** — ✅ 사용자 결정 (2026-05-19): **LLM-judge primary + F1 secondary** (Appendix C §C.4).
3. **Challenge 2 — docs corpus** — ✅ 사용자 결정 (2026-05-19): **`*.md`/ADR 인덱싱 추가** (Appendix B.1.b).
4. **Challenge 1 — BM25** — ✅ 사용자 결정 (2026-05-18): 현 dual-track (CKV+CKG 모두 보유) 유지하다가 동작 검증 후 수렴 결정 재검토.

### 2026-05-19 사용자 게이트 결과 — Overall A) As-is 승인

위 4개 결정 모두 확정. autoplan PROCEDURE 옵션 A 처리 — Phase 4 게이트 통과. 다음 워크플로우 단계는 본 문서 §6.3 Auto-Decided + 위 결정의 조합으로 도출 (이 문서 헤더 상단 참조).

---

## Appendix A — 기술 용어집

> 본 문서에 등장한 용어를 *우리 프로젝트(CKV) 맥락* 에서 정의. 일반 위키 정의가 아니라 "CKV에서 무엇을, 왜 쓰는지" 위주.

### A.1 정보 검색 (Retrieval) 기초

| 용어 | 의미 | CKV에서의 역할 |
|---|---|---|
| **Corpus** | 인덱싱·검색 대상이 되는 *텍스트 집합 전체*. 예: 책 한 권 전체, 우리 repo의 모든 `.go` 파일. | 현재 corpus = `*.go\|*.ts\|*.sol` 3개 언어. **확장 필요**: (i) `*.js`/`*.jsx` — 지원 대상 언어 정합 (§0.1), (ii) `*.md`/ADR/docs — 결정 사유 검색 (Challenge 2). 자세히 Appendix B. |
| **Document** | corpus의 *최소 단위*. CKV에선 chunk 한 개가 곧 document. | `internal/chunk/chunk.go` 출력물의 1행 = 1 document. |
| **Chunk / Chunking** | 큰 파일을 검색·임베딩하기 쉬운 작은 단위로 *자르는 과정*. | CKV는 함수/메서드/타입/contract 단위(symbol-level) + file_header fallback (50줄)로 자른다. |
| **Embedding (벡터 임베딩)** | 텍스트를 *고정 차원의 숫자 배열* (예: 1024차원 float32)로 변환. 의미가 비슷한 텍스트일수록 벡터 공간에서 가까워짐. | `bge-large-en-v1.5` (1024d) 또는 `bge-code-v1` (1024d)로 chunk 텍스트 → vector. `internal/embed/bgeonnx/`. |
| **Vector DB / ANN** | 벡터들을 저장하고 *가까운 벡터를 빠르게 검색*하는 DB. ANN = Approximate Nearest Neighbor (정확도 약간 희생하고 속도 확보). | `sqlite-vec` (SQLite extension), `internal/store/sqlitevec/`. 현재 brute-force, 1M+ chunk면 IVF로 전환 필요. |
| **Cosine similarity / distance** | 두 벡터 사이 각도로 유사도 측정. distance = 1 - similarity (작을수록 유사). | `internal/query/query.go` 에서 sqlite-vec가 반환하는 distance를 0~1 score로 정규화. |

### A.2 검색 정확도 메트릭

| 용어 | 정의 | CKV 현재 값 (D1 PoC, N=10) |
|---|---|---|
| **Recall@k** | 정답이 top-k 안에 *포함되는 query의 비율*. 1.0이면 모든 query에서 정답이 top-k에 등장. | recall@5 = 1.000, recall@1 = 0.600 |
| **MRR** (Mean Reciprocal Rank) | 정답의 *순위* 평균값 (정답이 1위 = 1.0, 2위 = 0.5, ...). 평균이 높을수록 정답이 윗쪽에 등장. | MRR = 0.770 (목표 0.85+) |
| **Citation accuracy** | top-1 hit의 `file:line` 인용이 *실제 파일+범위와 일치*하는 비율. 환각 차단 지표. | 1.000 |
| **Precision / Recall / F1** | TP/FP/FN 기반 표준 분류 메트릭. F1 = precision과 recall의 조화 평균. | 1.3 PR-regression 평가에서 사용 예정. |
| **Jaccard similarity** | 두 집합의 *교집합 / 합집합* (0~1). 빠르고 단순. | 1.3 평가의 file-set 비교 옵션. |

### A.3 검색 알고리즘 두 갈래

| 알고리즘 | 원리 | 강점 | 약점 |
|---|---|---|---|
| **Sparse retrieval (BM25)** | *단어 빈도* 기반. "이 단어가 이 document에 몇 번 나오는가 + 다른 document엔 얼마나 안 나오는가" (TF-IDF의 진화형). | 정확한 단어 일치에 강함. 빠름. deterministic. | 동의어/문맥/의미 무시 ("port"와 "socket"이 다른 단어로 취급됨). |
| **Dense retrieval (Vector)** | *임베딩 벡터*의 거리 기반. 의미적으로 비슷한 텍스트가 가까이. | 동의어·문맥·언어 횡단(Go ↔ TS) 가능. | 정확한 식별자명 매칭 약함. 모델·연산 비용 큼. |
| **Hybrid (RRF)** | 두 결과를 *순위 융합* — 동일 후보가 두 알고리즘 모두에서 상위면 더 점수. | 양쪽 강점 결합. SOTA 검색 시스템 표준. | fusion 레이어 구현 필요. |

### A.4 Fusion / Rerank 종류 (Challenge 1 이해용)

| 종류 | 무엇을 하는가 | CKV/CKS/Agent 어디에 위치? |
|---|---|---|
| **RRF (Reciprocal Rank Fusion)** | 여러 backend(BM25 / vector / graph)의 ranked list를 *순위 점수로 합산*. `score = Σ 1/(60 + rank_in_backend_i)`. 표준 RRF default rank=60. | **CKS** (retrieval orchestrator). plan §7.5. |
| **Cross-encoder rerank** | 쿼리와 후보를 *한꺼번에* 인코딩 → 더 정확하지만 느림. 보통 top-50 정도를 top-5로 재정렬할 때만 사용. | **Coding agent** 또는 CKS에 추가 모듈. 현재 미적용. |
| **LLM-as-reranker** | LLM이 후보를 보고 직접 순위 매김. 가장 비싸지만 가장 똑똑. | **Coding agent**가 적합. CKV의 `internal/judge`는 *평가* 용도(eval 안의 rubric scorer)지 *production rerank*는 아님. |
| **Query reformulation** | 원본 query를 LLM이 *다시 표현* (synonym 확장, 분해)해서 여러 sub-query로 검색. | **Coding agent**. CKV는 받은 intent를 그대로 검색. |

> Challenge 1의 핵심 통찰: 사용자가 말한 "BM25 rerank를 agent에" 라는 표현은 사실 *(c) LLM-as-reranker* 또는 *(d) query reformulation* 을 의미할 가능성이 높음. **BM25 자체는 lexical retrieval primitive이므로 데이터 옆이 best place.**

### A.5 모델 / 런타임

| 용어 | 의미 | CKV에서 |
|---|---|---|
| **Tree-sitter** | 다언어 *증분 파서 생성기*. 빠른 AST 추출 + 부분 재파싱. | 현재 `internal/parse/{golang,typescript,solidity}` 3종 구현. **`javascript/` 미구현 — 지원 대상 4언어와 정합 위해 신설 필요** (Appendix B.1). chunk 단위(함수/메서드/contract) 자르기에 사용. |
| **ONNX / ONNX Runtime** | 신경망 모델을 *프레임워크 독립* 으로 표현하는 표준 + Microsoft의 런타임. PyTorch/TF에서 export 가능. | bge 임베딩 모델을 ONNX로 export → Go에서 CGO로 호출 (`internal/embed/bgeonnx`). |
| **CGO** | Go에서 *C 코드를 호출* 하는 매커니즘. | sqlite-vec(C extension)·onnxruntime·daulet/tokenizers(Rust→C) 호출 모두 CGO. |
| **bge-large-en-v1.5** | BAAI의 *general English* 텍스트 임베딩 모델 (BERT 기반, 1024차원, CLS pooling). | 현재 default 임베더. ~2.5GB. 코드 특화 아님이 한계. |
| **bge-code-v1** | BAAI의 *코드 특화* 임베딩 모델 (Qwen2 1.5B, 1024차원, last-token pooling). | 미적용 (D2 scope). ~5.8GB. |
| **Cold start / Warm** | 첫 호출 = cold (모델 로드 + JIT 컴파일 ~1.5s), 두번째 이후 = warm (메모리 hit). | `ckv mcp`는 startup 시 모델 미리 로드 → MCP 호출은 항상 warm 상태. |
| **p95 latency** | 100번 호출 중 95번째 *느린* 호출의 지연. tail latency 지표. | 현재 query p95 = 43ms (warm). 목표 ≤200ms. |

### A.6 MCP / 시스템 통합

| 용어 | 의미 | CKV에서 |
|---|---|---|
| **MCP (Model Context Protocol)** | Anthropic이 정의한 *LLM ↔ tool* JSON-RPC 프로토콜. stdio/HTTP. | `pkg/mcp/server.go` — Claude Code가 `claude mcp add ckv` 로 등록. |
| **stdio transport** | 표준 입출력으로 JSON-RPC 주고받음. 자식 프로세스 모델. | `ckv mcp` binary가 stdin/stdout으로 통신. |
| **Citation** | 검색 결과의 *출처* — `{file, start_line, end_line, commit_hash}`. 환각 차단. | 모든 hit에 강제. citation accuracy 100% 보장. |
| **Manifest** | 인덱스에 대한 *메타데이터 파일* — `indexed_head`, `embedding_model`, `embedding_dim`, `built_at` 등. | `ckv-data/manifest.json`. 모델/스키마 mismatch 시 `IndexUnavailable` 에러. |
| **Freshness** | 인덱스의 *최신성* — `indexed_head`(인덱싱 시점 git HEAD)와 *현재* git HEAD 비교. | `cks.ops.get_freshness` MCP 호출. |
| **Footprint logging** | 모든 build/query/mcp 호출에 대한 *구조적 로그* — latency, hit count, citation drop 등. | `internal/footprint`, JSONL sink. |
| **TTHW (Time-to-Hello-World)** | 신규 개발자가 *처음 동작시키는 데까지* 걸리는 시간. DX 지표. | mock embedder = 30초, bgeonnx prod = 10-15분 (모델 download). |

### A.7 SQLite 생태계

| 용어 | 의미 | CKV에서 |
|---|---|---|
| **sqlite-vec** | SQLite에 *벡터 컬럼 + KNN 검색* 추가하는 C extension. | `internal/store/sqlitevec` — vector.db 안에 `chunk_vec USING vec0(...)` 가상 테이블. |
| **FTS5** | SQLite 내장 *full-text search* extension. BM25 score 포함. | **현재 CKV는 미사용**. Challenge 1의 (B) 옵션에서 추가 제안. |
| **Virtual table** | SQLite에서 *외부 모듈이 제공하는 테이블 인터페이스*. vec0 / fts5 모두 virtual table. | sqlite-vec의 `vec0` 가상 테이블에 1024차원 벡터 저장. |
| **WAL (Write-Ahead Logging)** | SQLite의 *동시성 모드* — 읽기와 쓰기가 lock 충돌 없이 가능. | `ckv build` 진행 중에도 `ckv query` 가능하도록 WAL 활성. |
| **Atomic rename** | POSIX `rename(2)` 보장하는 *원자적 파일 교체*. | `vector.db.tmp` → atomic rename, 부분-write로 인한 corruption 방어. |

### A.8 평가 / 회귀 테스트

| 용어 | 의미 | CKV에서 |
|---|---|---|
| **RAG (Retrieval-Augmented Generation)** | LLM이 답하기 전에 *검색을 먼저 수행* 하고 검색 결과를 컨텍스트로 사용. | 이 프로젝트 자체가 RAG의 R 부분 (retrieval) 담당. |
| **Eval fixture** | *입력 query + 정답* 쌍을 미리 만들어둔 데이터셋. | `testdata/queries.yaml` (10 queries, Go/TS/Sol). |
| **LLM-as-judge** | LLM을 *채점관*으로 사용 — query·hits·rubric을 주고 0~5 점수 받음. | `internal/judge/judge.go` — Claude CLI를 headless로 호출. |
| **Regression test** | *과거 통과한 시나리오*가 새 코드에서도 통과하는지 *재실행*. | 1.3 요구의 핵심. PR-regression = "과거 PR을 학습 base로 두고 다시 풀어보기". |
| **Ground truth** | *정답으로 받아들이는 reference*. | 1.3 평가에서: 실제 PR이 변경한 파일·심볼 set 또는 PR diff 자체. |
| **ADR (Architecture Decision Record)** | *설계 결정과 그 이유*를 markdown으로 기록한 짧은 문서 (보통 `docs/adr/NNN-title.md`). 코드는 *what*만 보여주지만 ADR은 *why*를 보존. | 현재 CKV에 ADR 디렉토리 없음. plan-S1-ckv.md가 ADR 비슷한 역할. Challenge 2 권고: 본격 ADR 도입. |

---

## Appendix B — Corpus & ADR 기술 배경

> Challenge 2 보강. 사용자 의도를 정확히 파악하고 구현 방향을 합의하기 위한 상세 설명.

### B.1 "Corpus 확장"이 정확히 무엇을 의미하는가

Corpus 확장은 **두 개의 독립 차원**으로 나뉜다. 둘은 의존 관계가 없으므로 병렬 진행 가능.

#### B.1.a 차원 1 — 언어 확장 (`*.js`/`*.jsx` 신설)

**현재 (2026-05-19)**: `internal/discover/discover.go::detectLanguage()`는 다음 매핑만 보유:
```go
.go            → "go"
.ts, .tsx      → "typescript"
.sol           → "solidity"
.js, .jsx, ... → "" (unknown — 인덱싱 skip)
```

`internal/parse/`도 `golang/`, `typescript/`, `solidity/` 3개 디렉토리만. JS 파서는 *존재하지 않음*.

**제안**: JavaScript parser 신설.
```
*.js           → tree-sitter JavaScript parser → "javascript"
*.jsx          → tree-sitter JavaScript parser (JSX dialect) → "javascript"
```

**구현 방식**: TypeScript parser가 이미 tree-sitter 기반이라 패턴 재사용. `tree-sitter-javascript` grammar(별도 패키지)를 vendor 하거나, `tree-sitter-typescript` 가 JS도 처리할 수 있으면 같은 binding 재활용 가능 (확인 필요).

**효과**: JS heavy repo (Node 서비스, React 앱 등) 도 의미 검색 가능. CKG도 동일한 언어 set을 다루므로 file alignment 일관성 유지.

#### B.1.b 차원 2 — 모드 확장 (docs/ADR 인덱싱)

**현재**: `*.md`, `*.txt`, `CODEOWNERS`, `LICENSE`, `Makefile` 등은 **인덱싱되지 않음**. walker가 소스 코드 확장자만 수집.

**제안**: walker에 다음을 추가
```
*.md             → simple markdown chunker (section heading 단위)
docs/**/*.md     → ADR + plan 문서 + use-case 문서
README.md
CHANGELOG.md
docs/adr/*.md    → ADR (있으면)
```

**chunk_kind 새 값**:
- 현재: `function | method | type | contract | event | modifier | file_header`
- 차원 1 추가: 언어 = "javascript" (kind는 기존 function/method 재사용)
- 차원 2 추가: `doc_section | adr_section`

**효과 예시**: 사용자가 "왜 sqlite-vec를 골랐나" 라고 질의 시
- 현재: 코드만 검색 → `internal/store/sqlitevec/store.go`의 import 문 정도가 top hit. *왜* 골랐는지는 코드에 없음.
- 확장 후: `docs/plan-S1-ckv.md §4` "Vector store — decision matrix" 가 top hit. **결정 사유 직접 답변 가능.**

### B.2 ADR이 무엇이고 왜 필요한가

**ADR (Architecture Decision Record)** = 짧은 markdown 문서. 통상 다음 5개 필드:
```markdown
# ADR-001: sqlite-vec를 vector store로 선택

## Status
Accepted (2026-05-08)

## Context
embedded vector store가 필요. chromem-go·sqlite-vec·LanceDB·Qdrant·pgvector 검토.

## Decision
sqlite-vec를 1차 backend로 채택.

## Consequences
- 장점: CKG와 SQLite idiom 일관, ATTACH로 join 가능
- 단점: 1M+ chunk에서 brute-force ANN 성능 미지수 → LanceDB 마이그레이션 plan 보유
- 영향 받는 코드: internal/store/sqlitevec/

## Alternatives considered
- chromem-go: pure Go지만 ~100K vector ceiling
- LanceDB: 100M+ 스케일에 적합하나 Rust dep 추가
...
```

**왜 필요한가** (CKV 맥락):
1. **사용자 의도 직답**: "이 코드 왜 이렇게 됐어?" 류 질의에 *근거와 함께* 답하려면 결정 이유가 corpus에 있어야 함.
2. **Agent의 잘못된 refactor 차단**: ADR을 read하면 agent가 "이미 검토 끝나고 reject된 방향" 으로 가는 것을 막을 수 있음. 예: agent가 "Qdrant 쓰면 더 좋지 않을까?" 제안할 때 ADR-001을 보면 *이미 alternatives에 포함되어 있고 단점이 명시* → 자동 회피.
3. **현재 plan 문서들이 사실상 ADR 역할**: `plan-S1-ckv.md §3` (임베딩 모델 결정), `§4` (vector store 결정), `§10 Open decisions` 등은 이미 ADR 형식에 가까움. 다만 *grep 가능한 형식*은 아니어서 corpus에 들어가지 않으면 검색 hit이 안 됨.

**현재 plan-S1-ckv.md만으로 부족한 이유**: 단일 거대 문서라 *섹션 단위 검색* 어려움. 인덱싱이 안 들어가면 LLM은 전체 파일을 통째로 컨텍스트에 넣어야 하는데 700+ 줄이라 토큰 비용 비쌈.

### B.3 구현 스케치

#### B.3.a 차원 1 (JS 파서 신설) — TypeScript parser 패턴 재사용

```go
// internal/discover/discover.go::detectLanguage() 에 추가
case ".js", ".jsx", ".mjs", ".cjs":
    return "javascript"

// internal/parse/javascript/javascript.go (신규)
// tree-sitter-javascript grammar binding.
// TS parser와 거의 동일 구조 — 함수/메서드/class 노드 추출.
type Parser struct{ ... }
func (p *Parser) Language() string { return "javascript" }
func (p *Parser) Parse(src []byte) ([]parse.SymbolSpan, error) { ... }

// internal/parse/javascript/binding/ (신규, vendored grammar)
// tree-sitter-javascript의 C source 파일 vendor.
// internal/parse/solidity/binding/ 와 동일 패턴.

// pkg/types/chunk.go::Language 주석 업데이트
// "go" | "typescript" | "javascript" | "solidity"
```

영향 받는 파일 (blast radius):
1. `internal/discover/discover.go` — switch case 추가 (~5 LOC)
2. `internal/parse/javascript/javascript.go` — 신규 (~120 LOC, TS parser와 유사)
3. `internal/parse/javascript/binding/` — vendored tree-sitter-javascript grammar
4. `pkg/types/chunk.go` / `pkg/types/search.go` — 주석 + 언어 validation 업데이트
5. `testdata/sample/handler.js` — sample JS 파일 추가, eval fixture에 JS query 1~2개 추가

**Total**: ~4 파일 + vendor 디렉토리 1개, ~150 LOC 자체 코드 (vendor 제외). P2 안.

> ⚠️ **확인 필요**: 현재 `tree-sitter-typescript` grammar가 plain JS 도 받는다면 별도 grammar vendor 없이 같은 binding 재활용으로 vendor 단계 skip 가능 (~50 LOC로 축소). TypeScript parser 코드 (`internal/parse/typescript/`) 확인 후 결정.

#### B.3.b 차원 2 (docs/ADR 인덱싱)

```go
// internal/discover/discover.go 에 추가 (또는 walker.go 분기)
var docPatterns = []string{
    "*.md",
    "docs/**/*.md",
    "README*",
    "CHANGELOG*",
    "ADR-*.md",
}

// internal/chunk/doc.go (신규)
// markdown 섹션 단위로 chunk
// chunk_kind = "doc_section" or "adr_section"
// symbol_name = section heading (예: "Vector store — decision matrix")

// internal/parse/markdown/parser.go (신규)
// goldmark 또는 blackfriday로 AST → heading-level chunk split
```

영향 받는 파일 (blast radius):
1. `internal/discover/discover.go` — 패턴 추가 (~20 LOC)
2. `internal/parse/markdown/parser.go` — 신규 (~80 LOC)
3. `internal/chunk/doc.go` 또는 `chunk.go` 확장 — heading 기준 chunking (~50 LOC)
4. `pkg/types/chunk.go` — `chunk_kind` enum에 doc/adr 추가
5. `testdata/sample/` — sample markdown 추가, eval fixture에 doc query 추가

**Total**: ~5 파일, ~150 LOC, blast radius 안. P2 통과.

#### 통합 Total (차원 1 + 2 모두 진행 시)
- 약 **9-10 파일 수정/신규, ~300 LOC** 자체 코드 (vendor grammar 제외).
- 두 차원은 **독립** — 한쪽이 실패해도 다른 쪽 진행 가능.
- 권장 순서: **차원 1 (JS) 먼저** — 지원 대상 언어 정합 의무 + 평가 corpus 확장 효과. 차원 2 (docs)는 그 다음.

### B.4 우리 프로젝트(CKV)에서의 활용 시나리오

| 상황 | corpus 확장 전 | 확장 후 |
|---|---|---|
| agent: "왜 mock embedder가 기본값?" | 코드 검색 → `cmd/ckv/embedder.go` flag 정의만 hit. *왜* 모름. | `docs/d1-onnx-poc.md` §3.1 "mock stays the default so existing test/eval baselines hold" 가 top hit. **이유까지 답변.** |
| agent: "BM25를 CKV 안에 두면 안 되나?" | hit 없음. agent가 막무가내로 구현 시도 가능. | `plan-S1-ckv.md §7.5` + 본 문서 §6.1 Challenge 1 hit. **이미 검토된 결정 + 베스트 프랙티스 자동 인용.** |
| agent: "S2에 뭘 해야 하지?" | `featurelist.md` 단일 grep — 모든 P1/P2가 평면적으로 나옴. | `plan-S1-ckv.md §13 Out of scope` + `§10 Open decisions` 가 top hit. **우선순위 있는 답변.** |

**핵심**: 이 확장으로 CKV가 "코드 검색"에서 "코드 + 결정 이력 검색"으로 격상되며, 사용자 요구사항 1.1 의 "지식데이터 셋" 표현에 부합.

---

## Appendix C — PR #70 회귀 테스트 타겟 (구체)

> Challenge 3 보강. 사용자가 명시한 구체 타겟과 새 평가 흐름의 상세.

### C.1 타겟 식별

| 항목 | 값 |
|---|---|
| Repository | `stable-net/go-stablenet` |
| Local clone | `/Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest` |
| Base commit (checkout 대상) | `aa28927fb12048a59ac34608702eef5e1be90931` |
| Target PR | [#70](https://github.com/stable-net/go-stablenet/pull/70) |
| PR title | `fix: fill missing effectiveGasPrice in receipts on derivation` |
| Head commit (PR 최종) | `8db8eea4b8a9a418a7f842676c05d2a4fef4d673` |
| Merged at | 2026-04-06T02:28:50Z |
| Lines changed | +333 / -38 |

### C.2 PR #70 — 문제와 해결책 분리

CKV 평가에서 **agent에게 입력으로 줄 것** = Background만:

> **Background** (agent 입력으로 줄 부분):
> "In Anzeon, `effectiveGasPrice` is stored alongside the receipt in the database. However, receipts received via snap sync are RLP-encoded, which does not include `effectiveGasPrice`, resulting in a missing value. The previous `DeriveFields` logic had no way to recompute it correctly for Anzeon without access to state."

**숨길 것** (정답이므로 agent가 보면 안 됨):
- Solution 섹션 (`AuthorizedTxExecuted` event log emit + `DeriveFields`에 `headerGasTip` 파라미터 추가).
- Changes 섹션 (구체 파일 list).
- 실제 diff.

### C.3 평가 흐름 (사용자 정의 새 안)

```
[Phase A: 환경 준비]
  1. cd go-stablenet-latest
  2. git checkout aa28927fb12048a59ac34608702eef5e1be90931 (PR base)
     ↑ 이 시점은 PR #70이 적용되기 *직전* — 문제는 존재, 해결은 안 됨.

[Phase B: CKV 인덱스 구축]
  3. ckv build --src=. --out=/tmp/ckv-stablenet-pr70-base
     ↑ corpus에 코드 + (Appendix B 확장 적용 시) docs/ADR 포함.

[Phase C: Agent에게 "문제만" 입력]
  4. Agent 호출 — prompt 구성:
     - 시스템: "당신은 stable-net 코드베이스의 개발자다. CKV MCP로 코드 검색 가능."
     - 사용자 입력: PR #70의 Background 텍스트 (위 §C.2 인용 그대로)
     - 지시: "이 문제를 해결하기 위한 구현 plan을 markdown 문서로 작성하라.
              수정할 파일·함수·라인, 추가할 코드의 의도, 새로 정의할 상수·이벤트 등 명시.
              CKV로 관련 코드를 직접 검색·인용하면서 작성."
  5. Agent가 plan.md 생성 → 디스크에 저장.

[Phase D: 정답(실제 PR #70 diff) 추출]
  6. git diff aa28927fb..8db8eea4b -- '*.go' > expected_diff.patch
     ↑ 333+/38- 변경의 raw diff. 이것이 ground truth.

[Phase E: 유사도 산출]
  7. plan.md ↔ expected_diff.patch 비교 — 메트릭 §C.4 참조.
  8. 유사도율 수치 출력 + 통과 여부 (≥80%).
```

### C.4 메트릭 선택 — 비교표

사용자 새 안은 *plan ↔ diff* 비교라 단순 file-set 비교만으로는 부족. 4가지 옵션:

| 메트릭 | 계산 방식 | 강점 | 약점 | 권장도 |
|---|---|---|---|---|
| **(M1) File-set Jaccard** | plan에 *언급된 파일* set ∩ 실제 변경 파일 set / ∪ | 빠름, 결정적 | 잘못된 파일을 *수정* 한다고 했어도 file 이름만 맞으면 통과 | 보조 |
| **(M2) Symbol-set F1** | plan에 *언급된 함수/메서드/이벤트* set 대 실제 diff hunk의 symbol set | 함수 단위 정확도 평가 | CKG join 필요 (또는 git diff hunk → symbol 추출 직접 구현) | 보조 |
| **(M3) Diff-line overlap** | plan의 "+추가/-삭제" 코드 라인을 textually 추출 → 실제 diff 라인과 fuzzy match (LCS) | 구체 코드 일치도 직접 측정 | plan에 코드 안 적고 설명만 적으면 0점 | 부정확 |
| **(M4) LLM-as-judge with rubric** | LLM에게 plan + actual_diff 둘 다 주고 rubric으로 0~100 점수 | *의도* 일치도까지 평가. plan이 "다른 표현, 같은 효과"인 경우 정확히 잡음. | 비싸고 비결정적 (재현성 위해 seed/temp 고정 필요) | **Primary** |

**권고**: M4 primary + M1/M2 dual report (수치 신뢰성 보조).

#### M4 Rubric 초안

```
LLM judge prompt:
  당신은 코드 리뷰어다. 다음 두 입력을 받는다:
  - PLAN: agent가 작성한 구현 계획 (markdown)
  - DIFF: 실제 코드 변경 (unified diff)

  PLAN대로 구현했을 때, DIFF와 *기능적으로 동일한* 결과가 나올 가능성을 평가하라.

  Rubric (0-100):
  - 100: PLAN과 DIFF의 의도가 일치 + 핵심 변경 모두 plan에 포함 + 잘못된 변경 제안 없음.
  -  80: 핵심 변경 ≥80% 일치, 부수 변경(테스트, helper) 일부 누락 OK.
  -  60: 의도는 맞지만 구현 방향 일부 차이 (다른 함수에 추가 등).
  -  40: 의도 부분 일치, 일부 핵심 변경 누락.
  -  20: 문제는 이해, 해결 방향 어긋남.
  -   0: 무관.

  Output JSON: {"score": <int>, "rationale": "<3-sentence summary>", "matched": [...], "missed": [...]}
```

### C.5 PR #70 변경 파일 (정답 set, M1/M2용)

```
core/blockchain.go            (+1/-1)
core/chain_makers.go          (+1/-1)
core/rawdb/accessors_chain.go (+6/-1)
core/state_processor_test.go  (+23/-2)   # 테스트만, M1 weight 낮춤 옵션
core/state_transition.go      (+14/-0)   # ★ 핵심 — AuthorizedTxExecuted emit
core/types/receipt.go         (+31/-1)   # ★ 핵심 — DeriveFields에 headerGasTip
core/types/receipt_test.go    (+255/-31) # 테스트
params/protocol_params.go     (+2/-1)    # ★ 핵심 — AuthorizedTxExecutedEventSig
```

세 핵심 파일(`state_transition.go`, `types/receipt.go`, `protocol_params.go`)을 plan이 모두 짚으면 *기본 점수 확보*, 나머지는 부수 변경.

### C.6 구현 권고 — `internal/eval/prregress/` 모듈

```
internal/eval/prregress/
  ├── fetcher.go   # gh CLI 호출: gh pr view <num> --json title,body,baseRefOid,headRefOid,files
  ├── checkout.go  # git worktree add <tmp> <base_sha> (main 손상 방지)
  ├── agent.go     # Claude CLI를 headless로 호출, CKV MCP 연결, plan.md 받기
  ├── extract.go   # PR description에서 Background/Problem 섹션만 추출 (정답 영역 마스킹)
  ├── compare.go   # plan.md ↔ expected_diff.patch
  │                 ├── file_jaccard(plan, diff) → float64 (M1)
  │                 ├── symbol_f1(plan, diff)    → float64 (M2)
  │                 └── llm_judge(plan, diff)    → {score, rationale, matched, missed} (M4)
  ├── runner.go    # 전체 Phase A~E orchestration
  └── prregress_test.go

cmd/ckv/eval.go
  └── --pr-fixture=testdata/prs.yaml 플래그 추가

testdata/prs.yaml (S1.5 초기에는 1개 PR만)
  - id: stablenet-pr70
    repo: /Users/wm-it-22-00661/Work/github/stable-net/go-stablenet-latest
    base_sha: aa28927fb12048a59ac34608702eef5e1be90931
    pr_number: 70
    pr_url: https://github.com/stable-net/go-stablenet/pull/70
    threshold: 0.80
    metrics: [llm_judge, file_jaccard, symbol_f1]
```

### C.7 위험 / 주의사항

| 위험 | 영향 | 완화 |
|---|---|---|
| Agent가 PR description의 Background 외 정보 (예: HEAD diff)에 *부정 노출* | 점수 신뢰성 0 | extract.go가 Solution/Changes 섹션 명시 제거 + agent 환경에서 git diff·gh pr view 차단 |
| LLM-judge 비결정성 | 같은 plan에 다른 점수 | temperature=0 + 동일 prompt + 3회 평균 |
| Plan이 너무 짧거나 너무 김 | M4 점수 왜곡 | rubric에 plan 길이 normalize 명시 |
| Stable-net repo가 사적 코드 | external eval로 공개 시 leak | 본 eval은 *로컬 전용*. CI에 올리려면 private runner 필요 |
| PR #70 단 한 건 → 통계적 약함 | 한 번 통과/실패가 일반화 어려움 | S2에서 PR 5~10건으로 확장. 본 작업은 *흐름 검증* 우선 |

### C.8 단계별 실행 plan (W4-T4 + Follow-ups)

| Step | 산출물 | 예상 LOC | 의존성 |
|---|---|---|---|
| C-1 | `internal/eval/prregress/fetcher.go` + `extract.go` | ~150 | gh CLI installed |
| C-2 | `internal/eval/prregress/checkout.go` (git worktree) | ~80 | — |
| C-3 | `internal/eval/prregress/agent.go` (Claude CLI orchestrator) | ~200 | Claude CLI |
| C-4 | `internal/eval/prregress/compare.go` (M1/M2/M4) | ~250 | — |
| C-5 | `internal/eval/prregress/runner.go` + cmd flag | ~100 | C1-C4 |
| C-6 | `testdata/prs.yaml` + PR #70 fixture | ~30 | — |
| C-7 | 첫 실행 — baseline 측정, threshold tuning | — | C1-C6 |

**Total**: ~800 LOC, ~3일 작업. S1.5 또는 W4-T4 (W3 완료 직후).

---

## 10. 변경 이력

| 일자 | 버전 | 변경 |
|---|---|---|
| 2026-05-18 | 1.0 | autoplan 4-phase 리뷰 초안 작성 (`[single-voice]`). 사용자 3개 요구사항 대비 현 구현 상태 평가, User Challenge 3건 + Taste 3건 + Auto-decision 3건 정리. |
| 2026-05-18 | 1.1 | 사용자 피드백 반영: (1) Challenge 1 — dual-track 유지 + retrieval/fusion/rerank 분리 베스트 프랙티스 권고. (2) Challenge 2 — Appendix B로 corpus·ADR 상세 분리. (3) Challenge 3 — `stable-net/go-stablenet#70` 구체 타겟 + "plan ↔ actual diff 유사도" 새 평가 흐름 + Appendix C 신설. (4) Appendix A 용어집 (8 카테고리 ~50 용어) 추가. |
| 2026-05-19 | 1.2 | 지원 대상 언어 정정: `*.js`/`*.jsx`가 corpus에 포함되어야 하나 JS 파서 미구현. §0.1 "지원 대상 언어" 신설, §3.4 Scope Expansion에 JS 파서 신설 auto-approve 추가, §4.5 FM-E0 + §3.6 FM-S0 (JS 부재) 추가, §6.3 Auto-Decided A0 추가, §7 Decision Audit Trail #0 추가, Appendix A.5 Tree-sitter 행 JS 미구현 명시, **Appendix B.1을 두 차원(언어 확장 / 모드 확장)으로 재구성**, Appendix B.3에 JS 파서 구현 스케치(차원 1) 추가. |
| 2026-05-19 | 1.3 | **JS/Bash 파서 S2 이관 reversal** (사용자 결정 — docs 정리 세션). §0.1 JS/Bash 행을 "S2 이관"으로 정정 + Bash 행 신규 추가, §3.4 Scope Expansion 표의 JS auto-approve → "S2 이관", §3.6 FM-S0 / §4.5 FM-E0 severity High→Medium (S2 격리), §6.3 A0 reversal 표기, §7 Decision #0 / #13 reversal 기록, §9 결정 완료 1번 갱신. v1.2의 "지원 대상 언어 정합 의무" 결정을 사용자가 reversal — S1 stable 유지가 우선. |
| 2026-05-19 | 1.4 | **PR-regression 구현 완료 + mock baseline 측정** (commits `fddecda` `ac5926a` `507172c` `c36a9fb`). 6단계 통합 (fetch / worktree / build / query / agent / score) + `ckv eval --pr-fixture` flag. PR #70 5-run baseline (mock embedder): judge mean 0.44±0.25, file F1 mean 0.37±0.23, threshold 0.80 통과 1/5. LLM noise band σ=0.25 정량화. 상세 결과: [`pr-regression-baseline-2026-05-19.md`](./pr-regression-baseline-2026-05-19.md). Follow-up: **PRR-1** bgeonnx 재측정 (별도 turn). |
