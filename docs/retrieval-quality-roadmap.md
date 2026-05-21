# CKV Retrieval Quality 로드맵

> **문서 버전**: 1.0
> **작성일**: 2026-05-19
> **목적**: CKV의 retrieval 정확도를 산업계 베스트 프랙티스 수준까지 끌어올리기 위한 5가지 패턴 적용 로드맵. 각 패턴의 외부 공개 측정값 + 우리 환경에서의 예상 효과 + 적용 순서 + A/B 측정 framework 정의.
> **연관 문서**: [`plan-S1-ckv.md`](./plan-S1-ckv.md) §5 (chunking), [`eval-metrics.md`](./eval-metrics.md) (메트릭 정의), [`embedder-integration.md`](./embedder-integration.md) (consumer 통합 + env 튜닝).

---

## 1. Executive Summary

CKV의 retrieval 품질을 향상시킬 5가지 패턴이 산업계에 존재한다. **단일 측정값으로 가장 단단한 근거는 Anthropic Contextual Retrieval 공식 발표 (2024-09-19)** — vector-only baseline 대비 retrieval failure rate **−35%** (contextual prefix), **−49%** (+ BM25), **−67%** (+ Cohere rerank-3).

**현재 CKV baseline (N=10, testdata/sample)**:
- recall@5 = 1.000 (천장 도달 — 측정 공간 없음)
- recall@1 = 0.600 (향상 공간 큼)
- MRR = 0.770 (목표 0.85+, D1 hypothesis 미달)
- citation@1 = 1.000
- build throughput = 1.6 chunks/s (1M LOC ≈ 35h)

**핵심 결정 (2026-05-19, 사용자)**:
- **Build throughput 악화 허용**. 이유: 초기 인덱싱은 one-time cost. 그 뒤 incremental만 처리되므로 운영 부담 적음.
- 단, 위 결정은 *`ckv reindex` (incremental, S2 이관) 가 도입된 이후* 진정한 의미를 가짐. S1 동안은 매 코드 변경 시 full rebuild — Roadmap 적용 결정 시 S2 reindex 작업과 의존성 의식 필요.

**권고 적용 순서**: D1-FU-7 (fixture 50+ 확장) → D1-FU-8 (배치 + CoreML EP) → Phase D (Contextual prefix, ROI 1위) → Phase B (multi-granularity) → Phase C (PR/commit corpus) → Phase A (sliding split).

---

## 2. 현 CKV chunking 전략 vs 산업계 베스트 프랙티스

### 2.1 현재 (2026-05-19 기준)

```go
// internal/chunk/chunk.go + plan §5.4
1차 단위: symbol-level (function/method/type/contract/event/modifier)
2차 분할: head-truncate만 (큰 함수 sliding split deferred)
3차 fallback: file_header 첫 50줄
```

→ **단층 (function-level only) + file_header**. Multi-granularity / contextual augmentation 없음.

### 2.2 산업계 베스트 프랙티스 — 비교 대상

| 시스템 | 청킹 전략 |
|---|---|
| **Sourcegraph Cody** | Hierarchical (function + class + file + repo summary) + embed 시 path-prefix context 부착 |
| **Cursor** | symbol-level + sliding window overlap + repository graph 결합 |
| **GitHub Copilot Spaces** | 멀티-granularity + 사용자 추가 docs 통합 |
| **Voyage Code** | code-specific embedder + chunk hierarchy + commit history corpus (옵션) |
| **Anthropic Contextual Retrieval** | chunk마다 *문맥 prefix* LLM 생성 → 임베딩에 위치 정보 주입 |

---

## 3. 5가지 패턴 상세

### 3.1 패턴 1 — Hierarchical multi-granularity chunking

한 코드를 *여러 크기로 동시 임베딩*:

| 단위 | 예시 | 현 CKV | 베스트 프랙티스 |
|---|---|---|---|
| 1차 function/method | `Server.Listen()` | ✅ | ✅ |
| 2차 class/struct/contract 전체 | `Server` struct 전체 | ❌ | ✅ |
| 3차 file 전체 | `server.go` 전체 | ⚠️ 첫 50줄만 | ✅ 전체 |
| 4차 module/package 요약 | `cmd/server/` 요약 | ❌ | ✅ (옵션) |

**왜 효과 있는가**: query "어떻게 connection pool 초기화하나" 같은 *모듈 level* 질문은 함수 단위 청크로는 답 약함. *클래스/파일 단위 청크가 있어야 강함*.

**외부 측정값**:
- LlamaIndex "Building Performant RAG Applications for Production" 보고: hierarchical retrieval로 recall@10 +10~15%
- Sourcegraph Cody 블로그: "significantly better" — 정량값 미공개

**우리 환경 추정**: recall@1 0.60 → ~0.70, MRR 0.77 → ~0.83. 신뢰도 **Mid**.

**비용**:
- 청크 수 ~2~3배 증가 → DB 크기 + 임베딩 비용 ~2~3배
- query 시 chunk_kind filter로 적합 granularity 선택 가능 (search 비용은 동일)

---

### 3.2 패턴 2 — Contextual prefix (Anthropic 패턴)

각 chunk text 앞에 *그 chunk의 위치 + 관계 context* 한 문장 prefix:

```
[prefix] 이 chunk는 `package server`의 `Server` struct의 `Listen` method이다.
        같은 파일에 `New`, `Close`가 있고, `internal/net` 패키지를 import한다.

func (s *Server) Listen(addr string) error {
  ...
}
```

→ 임베딩 벡터에 *위치·관계 정보가 녹아듦* → 검색 정확도 ↑.

**외부 측정값 (단단한 근거)**:

| 설정 | retrieval failure rate (vector-only 대비) |
|---|---|
| Vector only (baseline) | 0% |
| + Contextual embeddings | **−35%** |
| + Contextual embeddings + Contextual BM25 | **−49%** |
| + Contextual embeddings + BM25 + Cohere rerank-3 | **−67%** |

출처: [Anthropic "Introducing Contextual Retrieval" (2024-09-19)](https://www.anthropic.com/news/contextual-retrieval). 5개 dataset 248 query 측정 (코드 retrieval 포함).

**우리 환경 추정**:
- MRR 0.77 → ~0.85 (D1 hypothesis 충족 가능)
- recall@1 0.60 → ~0.75
- 신뢰도 **High** (Anthropic 측정 base가 가장 단단함)

**비용**:
- 청크당 *prefix 생성 LLM 호출 1회* (Anthropic은 Haiku 권장, 비용 ~$0.0001/chunk)
- Build throughput **추가 악화**: 1.6 → 0.2~0.4 chunks/s
- 또는 *룰 기반 prefix* (package + struct + signature 자동 생성) 로 LLM 호출 회피 가능 — 효과는 일부 감소 but throughput 보존

---

### 3.3 패턴 3 — Sliding window overlap (큰 함수에)

큰 함수를 hard-split 하면 경계에서 의미 손실. 10-20% overlap 적용:

```
chunk_1: lines 1-50
chunk_2: lines 41-90    (overlap 41-50)
chunk_3: lines 81-130
```

**외부 측정값**: 단일 단단한 측정값 부족. LangChain/LlamaIndex 보고 recall +5~10% (큰 문서 한정).

**우리 환경 추정**:
- 효과는 *큰 함수 비율* 에 의존. testdata/sample은 작은 함수만 — 거의 효과 0.
- 실제 stable-net 같은 대형 repo에서 측정 필요. 신뢰도 **Low**.

**비용**: 청크 수 +10~30% (큰 함수가 많을수록 비례).

**주의**: plan §5.4 2차 분할이 이미 이 패턴을 의도 (AST top-level statement 단위로 split). 현재 head-truncate만 적용된 상태 — *완성 작업 = Phase A*.

---

### 3.4 패턴 4 — PR/commit history as separate corpus ← 사용자 instruction 핵심

코드 자체엔 *what*만, *why*는 없음. PR title + body + commit message + diff hunk를 별도 corpus로:

```yaml
chunk_kind: pr_summary
citation: {pr_url, base_sha, head_sha, file:line of changed hunk}
text: |
  PR #70: fix: fill missing effectiveGasPrice in receipts on derivation

  Background: In Anzeon, effectiveGasPrice is stored alongside receipt...
  Solution: Emit AuthorizedTxExecuted event log at end of TransitionDb...
```

**Query 예시**:
- "이 함수 왜 이렇게 구현됐어?" → vector hit → PR description 답변
- "지난번에 비슷한 문제 어떻게 해결했지?" → past PR retrieval
- "이 코드 수정하면 다른 곳에 영향 있어?" → impacted PR history → 관련 영역

**외부 측정값**: 동일 비교 측정 없음 (query 분포에 dependent). Sourcegraph가 commit history를 별도 인덱싱하는 패턴 존재 — 정량 효과 미공개.

**우리 환경 추정**:
- *기존 fixture의 recall/MRR 메트릭과 별개 차원*. 측정하려면 "why?" 류 query 10~20개 별도 fixture 신설 필요.
- LLM-as-judge로 *답변 품질* 평가 — 정량화 어렵지만 사용자 instruction의 핵심 가치.
- 신뢰도 (질적) **High** — 다른 패턴으로 대체 불가능한 유일 차원.

**비용**:
- 청크 수 +30~100% (PR 빈도에 따라). 대형 repo는 10K PR도 가능.
- git log + gh API fetch 비용 (인덱싱 시 1회).
- 부수 효과: review-direction Appendix C의 PR-regression test와 *같은 모듈* (`internal/parse/gitlog/`) 가능 — 코드 재사용.

---

### 3.5 패턴 5 — Hybrid retrieval at query time (RRF)

두 인덱스를 결합하는 시점은 *query*. 인덱스는 각자 독립.

```
query "TCP socket bind on port"
  ├── CKV (vector)       → ranked list A
  ├── CKG (BM25 over qname+sig+doc)  → ranked list B
  └── RRF (CKS)          → fused list  ← score = Σ 1/(60 + rank_in_backend)
```

**외부 측정값**:
- Anthropic 측정 (위 §3.2 표): Contextual + BM25 결합 시 **−49%** (BM25 단독 추가 효과 약 ~14% 추가)
- Microsoft Research "Reciprocal Rank Fusion outperforms Condorcet" — RRF가 다른 fusion보다 robust
- Pinecone/Weaviate hybrid search: 일반적으로 recall +20~40%

**우리 환경 추정**: MRR 0.77 → ~0.90 (CKS 통합 후). 신뢰도 **High**.

**비용**:
- CKV 단독 부담은 *없음* (RRF는 CKS 책임)
- query latency +50~200ms (BM25 단계)
- CKS repo 작업 — CKV 우선순위 외

**중요**: 패턴 5는 **CKV 단독 작업 아님**. CKS milestone에 의존 — Roadmap에서 *외부 의존 항목*으로 분류.

---

## 4. 우리 baseline의 한계 — 무엇을 *측정* 할 수 있는가

### 4.1 Ceiling Effect

| 메트릭 | 현재 값 (N=10) | 향상 공간 | 측정 가능? |
|---|---|---|---|
| recall@5 | **1.000** | 0 | ❌ ceiling |
| recall@3 | 0.900 | 0.1 | ⚠️ 작음 |
| recall@1 | 0.600 | **0.40** | ✅ |
| MRR | 0.770 | **0.23** (~0.85~0.95 가능) | ✅ |
| citation@1 | 1.000 | 0 | ❌ ceiling |

→ 현 fixture 위에서는 **recall@1 + MRR 둘만 측정 의미 있음**. 나머지는 fixture 확장 (D1-FU-7) 후 가능.

### 4.2 Fixture 한계

- N=10 → 99% CI로 "실제 recall ≥ 0.69" (eval-metrics.md §4.1 인용)
- testdata/sample 4 파일 26 chunks → 1M LOC 코퍼스의 ~1/40000 규모
- 자연어 query만, "why?" 류 없음 — 패턴 4 효과 측정 불가
- **결론**: 패턴 적용 *전에* fixture를 50+ + "why?" 류 추가가 측정의 전제조건

### 4.3 외부 측정값 적용 시 주의

- Anthropic 측정은 *자연어 + 코드 혼합 corpus*. 우리는 *코드 only* — 효과 다를 수 있음 (코드 corpus가 의미 다양성 적어 효과 감소 가능)
- *baseline 차이*: Anthropic은 100K~1M token corpus. 우리 testdata는 더 작아 ceiling 도달 빠름
- 따라서 35%/49%/67% 수치를 *그대로 적용* 하면 과대 추정

---

## 5. 적용 시 예상 수치 (시나리오 표)

> ⚠️ 모두 *추정*. 실측은 fixture 50+ 확장 후 가능.

| 시나리오 | recall@1 | recall@5 | MRR | build throughput | query p95 | 사용자 throughput 결정 영향 |
|---|---|---|---|---|---|---|
| **현재** | 0.60 | 1.00 | 0.77 | 1.6 c/s | 43ms | — |
| + Phase A (sliding split) | 0.60 | 1.00 | 0.78 | 1.5 c/s | 43ms | OK (영향 미미) |
| + Phase B (multi-granularity) | 0.65~0.70 | 1.00 | 0.80~0.83 | **0.5~0.8 c/s** | 50ms | ✅ 허용됨 |
| + Phase C (PR corpus) | 0.65~0.70 | 1.00 | 0.80~0.83 | 0.4~0.7 c/s | 55ms | ✅ 허용됨 |
| + Phase D (Contextual prefix, 룰 기반) | 0.70~0.80 | 1.00 | 0.83~0.88 | 0.4~0.6 c/s | 60ms | ✅ 허용됨 |
| + Phase D (Contextual prefix, LLM 생성) | 0.75~0.85 | 1.00 | **0.85~0.90** | **0.2~0.4 c/s** | 70ms | ✅ 허용됨 |
| + Hybrid (CKS 의 BM25 + RRF) | 0.85~0.90 | 1.00 | 0.90~0.95 | (CKV 동일) | +100ms (CKS) | CKS 책임 |

**스택 끝 (모든 패턴 적용)**:
- MRR ~0.90~0.95, recall@1 ~0.85~0.90 — D1 hypothesis 충족 + 산업계 sota 수준
- build throughput 0.2~0.4 c/s → 1M LOC ≈ **30~60시간** (한 번)
- 1만 LOC repo (testdata 정도) ≈ 20~40분 (한 번)

---

## 6. 실패율 ("환각 / 오답") 분류

사용자 instruction의 "환각현상" 본질은 multiple sources:

| 실패 종류 | 원인 | 줄여주는 패턴 | CKV 책임? |
|---|---|---|---|
| **retrieval miss** (관련 코드 못 찾음) | 청크 단위 부적절, 의미 모호 | 패턴 1, 2 | ✅ 직접 |
| **retrieval hit but shallow context** (찾았으나 관련 깊이 부족) | call/struct 관계 미포함 | 패턴 5 (CKG hybrid) | ⚠️ CKS 책임 |
| **반복 실수** ("이전에 왜 그랬는지 모름") | PR/commit 의도 corpus 부재 | **패턴 4** | ✅ 직접 — *유일한 path* |
| **잘못된 코드 생성** (받은 context를 LLM이 잘못 해석) | LLM 모델/prompt 측 | (모든 패턴이 간접 도움) | ❌ CKV 범위 밖 |

→ 사용자가 우려한 "잘못 구현된 코드" 의 *retrieval-attributable share* 는 **패턴 4 (PR corpus)** 가 가장 직접적. recall 숫자엔 잡히지 않지만 *답변의 근거 깊이* 에 결정적.

---

## 7. Build throughput 트레이드오프 — 사용자 결정 (2026-05-19)

### 7.1 결정 내용

> "build throughput 의 악화는 허용가능해. 이유는 처음 설정에 오래걸리는것이지, 그 뒤로는 크게 문제되지 않기 때문."

### 7.2 결정의 architectural assumption

이 결정은 다음 두 가정 위에 성립한다:

1. **Initial indexing = one-time cost**. 한 번 build 하면 그 인덱스를 long-running으로 사용. ✅ 일반적으로 valid (MCP server는 long-running).
2. **이후엔 incremental만**. 코드 변경 시 *변경된 파일만* 재인덱싱. ⚠️ **현재 미충족**: `ckv reindex`는 S2 이관 결정 (featurelist §0.1 / plan §13). S1 동안은 매 코드 변경 시 full rebuild.

### 7.3 의존성 — S2 reindex 작업

```
Phase B/C/D 적용 + throughput 0.2~0.4 c/s
  → 1M LOC = 30~60시간 (1회)
  → 매 코드 변경 시 30~60시간 = 비현실적

따라서:
  Phase B/C/D 도입 시점에 `ckv reindex` (S2) 동시 진행 필수
  또는 *변경 파일만 부분 reindex* 기능이 이미 운영 가능해야 함
```

### 7.4 트레이드오프 명문화

| 항목 | 허용 범위 | 결정 |
|---|---|---|
| 초기 build (1회) | 30~60시간 (1M LOC) | ✅ 허용 |
| 일상 incremental | ≤ 5분 (file 단위 변경) | ✅ 필수 — S2 reindex 의존 |
| query p95 | ≤ 200ms warm | ✅ 모든 패턴 적용해도 70ms로 OK |
| Disk usage | 2~5GB / 1M LOC | ✅ 허용 (multi-granularity로 3배 증가) |

### 7.5 권고 — Roadmap 적용 전 선행 작업

**Phase B 도입 *전에* `ckv reindex` 작업을 S1.5 또는 S2 첫 마일스톤으로 승격** 권고. 그렇지 않으면 사용자 결정의 #2 가정이 깨짐.

---

## 8. 적용 순서 — 6 Phase Roadmap

### Phase 0 — 측정 인프라 (선행)

```
0a. D1-FU-7  fixture 50+ 확장 + "why?" 류 query 10~20개 추가
0b. D1-FU-8  배치 임베딩 + CoreML EP (선택, throughput buffer 확보용)
0c. 측정 baseline 재고정 (확장된 fixture로)
```

**왜 선행**: 0a 없이는 모든 패턴 효과 측정 불가 (ceiling). 0b는 패턴 적용 시 throughput 악화의 *상쇄*. 사용자가 throughput 악화를 허용했어도 baseline buffer는 있는 게 안전.

### Phase A — Sliding window split 활성화 (W3 enhancement 완성)

```
- internal/chunk/chunk.go::maybeTruncate를 AST top-level statement 단위 split로 교체
- 큰 함수 (>1500 token) 만 영향. 작은 함수 비대상.
- chunk_id에 ":chunk:<n>" suffix 부착 (plan §5.4)
- A/B 측정: 큰 함수 비율 검증 후 효과 판단
```

**예상 효과**: MRR +0.01~0.02 (testdata 기준 거의 0, 실 repo에선 측정 필요).
**비용**: 청크 수 +5~15%, throughput -5%.
**LOC**: ~80.

### Phase B — Multi-granularity 추가

```
- internal/parse/<lang>/에서 class/struct/contract 전체 span 추출 추가
- internal/chunk/에 chunk_kind 추가: "class_body", "file_full"
- File full chunk: file_header 50줄을 *전체 파일*로 확장 (모델 max input 안 맞으면 truncate)
- internal/query/에서 chunk_kind filter 노출 (이미 일부 지원)
- pkg/mcp/server.go의 semantic_search tool에 chunk_kind hint 파라미터 추가
```

**예상 효과**: recall@1 +0.05~0.10, MRR +0.03~0.06.
**비용**: 청크 수 ~2~3배, throughput -50%, DB 크기 ~2~3배.
**LOC**: ~250.

### Phase C — PR/commit history corpus 신설 ← 사용자 instruction 핵심

```
- internal/discover/에 git log 워킹 추가
- internal/parse/gitlog/ (신규) — PR title/body/commit message/diff hunk 파싱
- gh CLI fetch (Appendix C의 prregress 모듈과 공유) 또는 git log 직접
- chunk_kind: "pr_summary", "commit_message", "pr_diff_hunk"
- citation = {pr_url, commit_sha, file:line}
- 별도 fixture 신설: testdata/why-queries.yaml (10~20개)
- internal/eval/score.go에 "why-recall" 추가 — PR/commit citation 정확도
```

**예상 효과 (기존 메트릭)**: ~0 (다른 차원).
**예상 효과 (why-fixture)**: cold start 측정 (baseline 없음). LLM-judge primary, citation 정확도 secondary.
**비용**: 청크 수 +30~100% (PR 양에 따라), throughput -20~30%.
**LOC**: ~400 (PR-regression 모듈과 코드 공유 ~150 LOC 절감).

**부수 효과**: review-direction Appendix C의 PR-regression test와 같은 git/gh fetch 모듈 공유 — *별도 작업이 아니라 한 모듈*.

### Phase D — Contextual prefix (Anthropic 패턴)

**Two-step rollout** 권고:

**D.1 — 룰 기반 prefix (cheap, deterministic)**:
```
- chunk text 앞에 자동 prefix 생성:
  "[package <pkg>] [<struct/class>] [<symbol>] <signature>"
- LLM 호출 없음, throughput 영향 미미
```
**예상 효과**: MRR +0.05~0.08, recall@1 +0.05~0.10. 신뢰도 Mid.
**비용**: throughput -5%.
**LOC**: ~60.

**D.2 — LLM-generated prefix (Anthropic 정식 패턴)**:
```
- chunk마다 Haiku/Sonnet 호출:
  prompt: "다음 chunk가 무엇을 하는지 + 어떤 모듈/상호작용에 속하는지 1-2 문장 요약"
- 응답을 chunk text 앞에 prefix
- Anthropic 측정 -35% baseline
```
**예상 효과**: MRR +0.08~0.13 (D1 hypothesis 0.85+ 충족).
**비용**: build throughput **0.2~0.4 c/s 까지 악화** (LLM 호출 시간 + API cost ~$0.0001/chunk).
**LOC**: ~150 (Claude CLI 또는 API 호출).

**결정 포인트**: D.1만 적용하고 D.2는 측정값 보고 결정. 사용자 throughput 결정으로 D.2 도입 가능.

### Phase E — Hybrid retrieval (CKS 책임, 외부 의존)

```
- CKV 측 작업 없음 — RRF는 CKS repo
- CKV가 노출하는 표면 (pkg/mcp Server.Underlying()) 만 충족
- CKS의 cks-mcp 통합 binary가 BM25 + RRF 추가
- BM25는 CKG 측 pkg/bm25/scorer.go (이미 존재)
```

**예상 효과 (CKS 통합 후)**: MRR 0.85+ → 0.90~0.95.
**CKV 비용**: 0.
**의존성**: CKS repo의 진행도. 본 로드맵 외 항목.

---

## 9. 측정 plan (A/B framework)

### 9.1 측정 절차 (Phase마다 반복)

```
1. baseline 측정:
   ckv build --src=<repo> --out=/tmp/ckv-base
   ckv eval --out=/tmp/ckv-base --fixture=testdata/queries.yaml --top=5 \
            --json > baseline.json

2. Phase X 적용 (코드 변경)

3. 변경 후 측정:
   ckv build --src=<repo> --out=/tmp/ckv-phaseX
   ckv eval --out=/tmp/ckv-phaseX --fixture=testdata/queries.yaml --top=5 \
            --json > phaseX.json

4. 비교:
   - delta_recall@1, delta_MRR
   - delta_build_throughput, delta_query_p95
   - 향상이 통계적 의미 있는가? (N=50+ 권장, 99% CI)
```

### 9.2 비교 fixture 셋

| Fixture | 용도 | 측정 메트릭 |
|---|---|---|
| `testdata/queries.yaml` (확장 50+) | 일반 retrieval | recall@k, MRR, citation@1 |
| `testdata/why-queries.yaml` (신설) | "왜?" 류 (Phase C 평가) | LLM-judge score, PR citation 정확도 |
| `testdata/prs.yaml` (review-direction Appendix C) | PR-regression | plan↔diff 유사도 (LLM-judge primary, F1 secondary) |
| `testdata/large-functions.yaml` (선택) | Phase A 평가 | 큰 함수 매칭 recall |

### 9.3 결과 기록

각 phase 측정 후 본 문서 §10에 결과 row 추가:

```markdown
| Phase | 측정일 | fixture | recall@1 | MRR | throughput | 결정 |
|---|---|---|---|---|---|---|
| baseline (현재) | 2026-05-18 | queries.yaml N=10 | 0.60 | 0.77 | 1.6 c/s | — |
| Phase A | TBD | queries.yaml N=50 | TBD | TBD | TBD | go/no-go |
| ... | | | | | | |
```

---

## 10. 측정 결과 기록

| Phase | 측정일 | fixture | recall@1 | recall@5 | MRR | throughput | go/no-go | 비고 |
|---|---|---|---|---|---|---|---|---|
| baseline | 2026-05-18 | queries.yaml N=10 | 0.600 | 1.000 | 0.770 | 1.6 c/s | — | bge-large-en-v1.5 + CoreML EP, testdata/sample |
| Phase 0a (fixture N=34 + markdown corpus) — mock | 2026-05-19 | queries.yaml N=34 | 0.294 | 0.735 | 0.485 | instant | — | mock embedder; markdown chunks 4개가 noise로 작용 |
| **Phase 0a (fixture N=34 + markdown corpus) — bgeonnx** | **2026-05-19** | **queries.yaml N=34** | **0.529** | **0.971** | **0.725** | **~1.0 c/s** | ✅ baseline | bge-large-en-v1.5 + **CPU-only** (CoreML compile I/O error → CKV_DISABLE_COREML=1). 1 miss = q5 "retrieve value by key" → top=handler.ts (cache.go 못 찾음). ceiling 해소 — recall@1·MRR 측정 공간 확보 |
| Phase 0b (batch+CoreML 정상화) | TBD | queries.yaml N=34 | 0.529 (동일) | 0.971 (동일) | 0.725 (동일) | TBD (50× 기대) | — | CoreML EP I/O error 원인 분석 + 해결 후 throughput 재측정 |
| Phase A (sliding split) — impl only | 2026-05-21 | queries.yaml N=50 | 0.360 (동일) | 1.000 | 0.494 (동일) | instant | — | `splitLongSpan` (`internal/chunk/chunk.go`), threshold = `MaxInputTokens * charsPerToken`. testdata/sample 함수가 모두 짧아 split 미발동 → metrics 동일. bge-large 실측 + 큰 함수 corpus 필요. commit `6dc7225`. |
| Phase B | TBD | 동일 | TBD | TBD | TBD | TBD | TBD | multi-granularity |
| Phase C | TBD | + why-queries.yaml | TBD | TBD | TBD | TBD | TBD | PR/commit corpus |
| Phase D.1 (rule-based prefix) — mock | 2026-05-21 | queries.yaml N=50 | 0.360 | 1.000 | 0.494 | instant | — | mock baseline N=50 r@1=0.300 → 0.360 (+0.060), r@5 0.680 → 0.740 (+0.060), MRR 0.4403 → 0.4937 (+0.053). prefix format: "language: X. file: Y. symbol: Z (Kind)." 한 줄. impl commit `1a5289d`. |
| Phase D.1 (rule-based prefix) — bgeonnx | TBD | queries.yaml N=50 | TBD | TBD | TBD | TBD | TBD | bge-large 실측은 별도 세션 (mock + prefix가 +0.053 MRR 보였으므로 bge-large 위에선 더 크게 기대) |
| Phase D.2 (LLM prefix) | TBD | 동일 | TBD | TBD | TBD | TBD | TBD | Anthropic 패턴 |
| Phase E (CKS hybrid) | TBD | 동일 | TBD | TBD | TBD | (CKV 동일) | TBD | CKS 통합 후 |

---

## 11. Fact-based Answer

**Fact** (None — 추론 아닌 사실, 출처 명시):
- Anthropic Contextual Retrieval 공식 측정 (2024-09-19, 5 dataset 248 query, 코드 retrieval 포함): vector-only 대비 contextual embeddings **−35% failure**, + contextual BM25 **−49%**, + Cohere rerank-3 **−67%**. [출처](https://www.anthropic.com/news/contextual-retrieval)
- CKV 2026-05-18 baseline: N=10, recall@5=1.000 (ceiling), recall@1=0.600, MRR=0.770, citation@1=1.000, build 1.6 chunks/s, query p95 43ms
- CKV chunking은 단층 (function-level only) + file_header 50줄. multi-granularity / contextual prefix 모두 미적용. (`internal/chunk/chunk.go`)
- `ckv reindex` (incremental) 는 S2 이관 결정 (`plan §13`, `featurelist §0.1`)

**Your Opinion**:
- **High prediction**: 사용자 throughput 결정은 *Phase B 이후 적용*에서 architectural 의미를 가짐. 단, **Phase B 도입 시점에 `ckv reindex` 작업이 S1.5 또는 S2 첫 마일스톤으로 승격되어야 함** — 그렇지 않으면 "초기 1회 + incremental만" 가정이 깨짐.
- **High prediction**: 측정 가능한 Phase별 ROI 순서 = **D.2 > B > C > D.1 > A**. 단, fixture 확장 (Phase 0a) 없이는 모두 측정 불가 — Phase 0a가 *모든 작업의 전제조건*.
- **Mid prediction**: 패턴 4 (PR corpus, Phase C) 는 사용자 instruction의 *유일한 path* (다른 패턴으로 대체 불가). 정량 측정 어려우나 질적 가치 가장 큼.
- **Mid prediction**: Phase D.1 (룰 기반 prefix) 는 D.2 도입 *전에* baseline 향상 시도용으로 적용 권장. throughput 영향 거의 없으면서 MRR +0.05~0.08 기대. cheap intervention.
- **Low prediction**: Phase A (sliding split) 는 testdata/sample에선 효과 0. 실 repo로 측정 fixture 확장 후 결정. plan §5.4 W3 enhancement 약속이지만 우선순위 낮음.
- **None**.

---

## 12. 통합 작업 우선순위 (2026-05-19)

본 §는 retrieval quality 진화(§8) 외에도 사용자 instruction(review-direction §6.1 Challenges + Appendix C PR #70) 으로 결정된 직접 작업을 합쳐 *CKV의 다음 모든 작업을 한 순서로* 정렬한다. Roadmap §8의 6-phase 보다 broader scope.

**세 source가 별도로 존재**:
- **Challenges** (1·2·3) — autoplan 시점 사용자 직접 선언, 결정사항은 [`backlog.md`](./backlog.md) 와 commit history 에 흡수.
- **Appendix C** — PR #70 회귀 테스트 구체 타겟. [`testdata/prs.yaml`](../testdata/prs.yaml) 에 4 entries (pr70/69/72/74) 로 구현됨.
- **Roadmap §8** — 본 문서, 산업계 best practice 5 패턴

세 source는 *다른 차원의 작업*을 다루므로 단순 합산이 아니라 *의존성 기준 통합*.

### 12.1 통합 우선순위 매트릭스

| # | 작업 | 차원 | 출처 | 의존성 | 예상 LOC | 측정 메트릭 |
|---|---|---|---|---|---|---|
| **1** | **fixture 50+ + why-queries 확장** | 측정 인프라 | Roadmap Phase 0a (D1-FU-7) | 없음 | ~100 (YAML + 일부 helper) | (모든 측정의 baseline) |
| **2** | **PR #70 회귀 테스트 모듈** (`internal/eval/prregress/`) | 평가 (eval) | Challenge 3, Appendix C | #1 후 (`testdata/prs.yaml` 신설) | ~800 | plan↔diff LLM-judge primary + F1 secondary |
| **3** | **docs corpus 확장** (`*.md`/ADR 인덱싱) | corpus (코드 외 결정사항) | Challenge 2, Appendix B | 없음 (병렬 가능) | ~150 | recall@k on doc-query fixture |
| **4** | **Phase C — PR/commit history corpus** | corpus (코드의 why) | Roadmap §8 Phase C | #2의 git/gh fetch 모듈 재사용 | ~400 (재사용 후) | LLM-judge on why-queries.yaml |
| **5** | **D1-FU-8 batch + CoreML EP** | 인프라 (throughput) | Roadmap §8 Phase 0b | 없음. #6~#9 적용 전 권장 | ~250 | build chunks/s |
| **6** | **Phase D.1 — 룰 기반 contextual prefix** | retrieval 품질 (cheap) | Roadmap §8 Phase D.1 | #1 후 (측정 가능) | ~60 | delta recall@1, MRR |
| **7** | **Phase D.2 — LLM contextual prefix** | retrieval 품질 (Anthropic 패턴) | Roadmap §8 Phase D.2 | #5 권장 (throughput buffer) | ~150 | delta recall@1, MRR |
| **8** | **`ckv reindex` 도입 — S1.5 승격** (사용자 결정 2026-05-19) | 인프라 (incremental) | review-direction §6.6, Roadmap §7.5 | #9 도입 *전* 필요 / featurelist+plan에서 S2→S1.5 정정 필요 | ~300 | incremental build < 5분 (file 단위) |
| **9** | **Phase B — multi-granularity** | retrieval 품질 | Roadmap §8 Phase B | #8 권장 (full rebuild 비현실) | ~250 | delta recall@1, MRR |
| **10** | **Phase A — sliding window split** | retrieval 품질 | Roadmap §8 Phase A | 큰 함수 비율 측정 후 | ~80 | 큰 함수 매칭 recall |

> Phase E (CKS hybrid, RRF) 는 CKV 외 작업이라 본 표 제외. CKS milestone에 의존.

### 12.2 그룹화 (병렬 진행 가능 묶음)

```
Group α (선행, 의존성 없음):
  #1 fixture 확장              ← 모든 측정의 전제
  #3 docs corpus 확장          ← Challenge 2
  #5 D1-FU-8 throughput        ← 인프라 buffer

Group β (Group α 후):
  #2 PR #70 회귀 테스트         ← #1 의존
  #6 Phase D.1 룰 기반 prefix   ← #1 의존 (측정)

Group γ (Group β 후):
  #4 Phase C PR/commit corpus   ← #2의 git/gh 모듈 재사용
  #7 Phase D.2 LLM prefix       ← #5 권장 (throughput buffer)
  #10 Phase A sliding split     ← #1 의존 (측정)

Group δ (인프라 + retrieval 마무리):
  #8 ckv reindex
  #9 Phase B multi-granularity  ← #8 권장
```

→ 한 사람이 진행 시 sequential, 두 사람 이상이면 같은 group 안에서 병렬.

### 12.3 "다음 1-2주" 시작 단위 — 권고

**Group α 병렬 시작** 권고 (모두 의존성 없음 + 다른 작업의 전제조건):
- **#1 fixture 확장** — YAML 작성 + git log fixture 일부. 1-2일.
- **#3 docs corpus 확장** — `internal/parse/markdown/` 신설 + walker 패턴 추가. 1일.
- **#5 D1-FU-8 throughput** — `internal/embed/bgeonnx/` batching + CoreML EP. 2-3일.

진행 시점은 사용자 결정. 각 작업 완료 시 §10 측정 결과 기록 표에 row 추가.

### 12.4 Source 역방향 매핑

| Source 항목 | 통합 # | 비고 |
|---|---|---|
| `review-direction §6.1` Challenge 1 (BM25 위치) | — | dual-track 유지 결정으로 *작업 항목 아님* (2026-05-18 사용자 결정) |
| Challenge 2 (docs corpus) | **#3** | |
| Challenge 3 (PR-regression metric LLM-judge) | **#2** | |
| Appendix C (PR #70 구체 타겟) | **#2** | |
| `review-direction §6.6` Deferred — ckv reindex | **#8** | S1.5 또는 S2 승격 권고 |
| Roadmap §8 Phase 0a (fixture 확장) | **#1** | |
| Roadmap §8 Phase 0b (batch+CoreML) | **#5** | |
| Roadmap §8 Phase A (sliding split) | **#10** | |
| Roadmap §8 Phase B (multi-granularity) | **#9** | |
| Roadmap §8 Phase C (PR/commit corpus) | **#4** | |
| Roadmap §8 Phase D.1 (rule prefix) | **#6** | |
| Roadmap §8 Phase D.2 (LLM prefix) | **#7** | |
| Roadmap §8 Phase E (CKS hybrid) | — | CKV 외 작업 |

### 12.5 진행 상태 추적

> ⚠️ **본 표는 retrieval quality 10 items만**. broader work tracking (33 items, A~E 카테고리 포함) 은 [`backlog.md §4`](./backlog.md) 가 *single source of truth*. 본 표는 Roadmap §12.1 매트릭스의 status view.

| # | 상태 | 시작일 | 완료일 | 측정 기록 (§10) | 비고 |
|---|---|---|---|---|---|
| 1 | ✅ | 2026-05-19 | 2026-05-19 | Phase 0a N=34 row | commit `f1a8ac9` (fixture) + `ad804be` (measurement). N=10 → N=34 + why-queries.yaml 12개 |
| 2 | 🔄 | — | — | — | **다른 세션 진행 중** (사용자 명시 2026-05-19). 본 세션 dispatch 금지 |
| 3 | ✅ | 2026-05-19 | 2026-05-19 | Phase 0a N=34 row (markdown corpus 포함) | commit `4a5dc3a`. `internal/parse/markdown/` 신설 + heading-level split |
| 4 | ⏳ | — | — | — | #2 후 (git/gh fetch 모듈 재사용) |
| 5 | ⚠️ 부분 | 사용자 별도 세션 | 2026-05-19 부분 | Phase 0a N=34 throughput 1.0 c/s | main `555b0c4`로 CoreML 코드 적용 완료. but N=34 측정 시 **CoreML compile I/O error** → CKV_DISABLE_COREML=1 fallback. Phase 0b 잔여 = backlog **A1** |
| 6 | ✅ | 2026-05-21 | 2026-05-21 | Phase D.1 row | rule-based prefix (`internal/chunk/prefix.go`) — mock N=50 r@1 +0.060, MRR +0.053. opt-out: `CKV_DISABLE_CONTEXTUAL_PREFIX=1`. |
| 7 | ⏳ | — | — | — | A1 (CoreML 정상화) 후 (throughput buffer 확보) |
| 8 | 📝 | — | — | — | **S1.5 승격 결정만 완료** (commit `c0689d7`). 코드 미진행. backlog **C1** |
| 9 | ⏳ | — | — | — | #8 후 |
| 10 | ✅ impl | 2026-05-21 | 2026-05-21 | Phase A row | `splitLongSpan` (`internal/chunk/chunk.go`). testdata/sample 짧은 함수 → split 미발동, metrics 동일. bge-large + 큰 함수 corpus 실측 별도 세션 필요. backlog **B1**. |

> 범례: ⏳ 대기 · 🔄 진행 중 · 📌 다음 작업 예정 · ✅ 완료 · ⚠️ 부분 · 📝 결정만 · ⛔ 차단

---

## 13. 변경 이력

| 일자 | 버전 | 변경 |
|---|---|---|
| 2026-05-19 | 1.0 | 초안 — 5 산업계 패턴 + 외부 측정값 (Anthropic 67%) + 6-phase roadmap + throughput 트레이드오프 (사용자 결정 허용) + A/B 측정 framework + §10 결과 기록 템플릿. |
| 2026-05-19 | 1.1 | **§12 통합 작업 우선순위 신설** — 3개 source(Challenges + Appendix C + Roadmap §8) 를 한 우선순위로 통합. 10 항목 매트릭스 + 4 그룹 병렬화 + source 역방향 매핑 + 진행 상태 추적 표 추가. 기존 §12 변경 이력 → §13으로 shift. 사용자 질문(2026-05-19): "현재 제안하는 작업리스트의 우선순위가 Roadmap 내용을 반영한 거야?" 에 대한 답 — 이전 3건 제안은 fixture 확장(Phase 0a)을 누락한 상태였음. §12로 통합 정합성 회복. |
| 2026-05-20 | 1.2 | **§12.5 진행 상태 정정** — Group α 완료 반영 (#1 ✅, #3 ✅, #5 ⚠️ 부분 CoreML I/O error). #2 다른 세션 🔄, #8 결정만 📝. 범례에 ⚠️·📝 추가. **broader backlog (33 items, A~E 카테고리) 는 [`backlog.md`](./backlog.md) 신설로 분리** — Roadmap §12 는 retrieval quality slice의 우선순위 view, backlog.md 는 모든 추적 항목의 SoT. 사용자 결정 옵션 (b). |
