# CKV 평가 지표 가이드

> **목적**: CKV의 `ckv eval` 명령이 무엇을 측정하고, 왜 그것을 측정하고, 결과 숫자를 어떻게 해석할지 정리.
>
> **대상 독자**: 임베딩 모델을 교체하거나(D2 bge-code-v1 Qwen2 어댑터 등), eval fixture를 확장하거나(FU-7), 성능 회귀를 감지하려는 contributor.
>
> **요약 한 줄**: CKV는 RAG retriever다. 좋은 retriever의 기준은 **"사용자가 원하는 코드 청크를 LLM에 충분히 좁게 + 충분히 빠르게 전달했는가"**.

---

## 1. CKV의 위치 — 무엇을 평가하는가

CKV는 RAG (Retrieval-Augmented Generation) 파이프라인의 **검색 단계(retrieval)** 만 담당한다. 생성(LLM 추론)은 Claude Code 같은 외부 도구가 한다.

```
사용자 쿼리("connection pool 초기화")
   ↓
[Claude Code]
   ↓ MCP 호출
[CKV]
   ↓ 임베딩 모델로 벡터화
   ↓ vector DB 유사도 검색
top-K 코드 청크 + 인용(파일, 줄 번호)
   ↓ MCP 응답
[Claude Code → Claude Opus]
   ↓ LLM 추론
사용자에게 답변
```

평가 대상은 화살표 둘 사이 — **"top-K 청크가 얼마나 적절한가"**. LLM 추론 품질은 CKV 책임 밖.

> **왜 retrieval만 평가하나**: LLM 추론 품질은 모델/프롬프트/문맥에 따라 무한 변수. 평가 가능한 단위로 자르면 retrieval만 남는다. retrieval이 좋아야 LLM이 좋은 답을 만들 기회를 얻는다 ("garbage in, garbage out").

---

## 2. 측정 도구 — `ckv eval`

```bash
ckv eval \
  --out=/tmp/ckv-bge \
  --fixture=./testdata/queries.yaml \
  --top=5 \
  --threshold=-1 \
  --embedder=bgeonnx
```

### 입력

| 항목 | 역할 |
|---|---|
| `--out` | 이미 빌드된 vector DB 경로 (`ckv build` 결과물) |
| `--fixture` | 정답이 정해진 쿼리 모음 (YAML) |
| `--top` | 검사할 결과 개수 (K). 표준 5 |
| `--threshold` | cosine 유사도 최소 임계값. `-1`은 임계값 미적용 (모든 결과 검사) |
| `--embedder` | 쿼리 벡터화에 쓸 임베더. **vector DB 빌드 시와 같아야** (차원 일치) |

### 핵심: fixture 구조 (`testdata/queries.yaml`)

```yaml
queries:
  - id: q1
    intent: "TCP socket bind on port"     # 사용자 자연어 쿼리
    expected:
      file: server.go
      symbol: Server.Listen
      kind: Method
      line_range: [22, 29]                # 정답 코드 위치
    notes: "primary acceptance #2 fixture"
```

**"hit"의 정의**:
1. 검색 결과의 `citation.file` 이 `expected.file` 과 일치
2. 검색 결과의 line range가 `expected.line_range` 와 **겹침**

→ 청커가 함수 한 개만 잘라내든 파일 헤더를 잘라내든, **정답 줄을 포함하는 청크면 OK**. 정확한 청크 경계를 강요하지 않음 (청커 전략 진화에 유연).

---

## 3. 지표별 정의 + 의미 + 해석

### 3.1 recall@K — "정답 포함률"

**정의**: top-K 결과 안에 정답이 포함된 쿼리 비율.

```
recall@K = (정답을 top-K 안에 찾은 쿼리 수) / (총 쿼리 수)
```

**측정 예시 (K=5, N=10 fixture)**:
- 10개 쿼리 중 8개가 top-5에 정답 포함 → recall@5 = 0.8
- 10개 모두 포함 → recall@5 = 1.0

**왜 측정**:
- LLM에 top-K 청크를 보낼 때 K=5가 일반적 (context window 절약 + 노이즈 감소)
- recall@5가 낮으면 "사용자가 5개 결과를 봐도 정답이 안 보인다" → RAG 실패
- recall@5 = 1.0 보장 → Claude Code가 사용자에게 "정답 청크가 무조건 컨텍스트에 있다"고 약속 가능

**해석**:
| 값 | 의미 |
|---|---|
| 0.9+ | 운영 가능. 사용자가 거의 항상 정답을 받음 |
| 0.7~0.9 | 개선 여지. 임베더 교체 또는 청킹 전략 검토 |
| < 0.7 | retrieval 자체가 무너짐. LLM 단계는 의미 없음 |

**recall@1, recall@3, recall@5 모두 보는 이유**:
- recall@1 = 첫 결과가 정답일 확률 (top-1 자신감)
- recall@3 = 짧은 컨텍스트로 충분한지
- recall@5 = 표준 K (LLM에 보내는 청크 수)

세 숫자가 모두 같으면 retrieval 신호가 매우 명확. 차이가 크면 "top-1은 가끔 노이즈, top-5는 안전".

### 3.2 MRR (Mean Reciprocal Rank) — "정답 도달 속도"

**정의**: 정답이 처음 등장한 rank의 역수(1/rank)의 평균.

```
MRR = (1/N) × Σ (1 / rank_of_first_correct_hit)
```

**계산 예시 (10 쿼리)**:
| 쿼리 | rank | 1/rank |
|---|---|---|
| q1 | 1 | 1.000 |
| q2 | 2 | 0.500 |
| q3 | 1 | 1.000 |
| ... | ... | ... |
| q10 | 1 | 1.000 |

평균 = MRR

**왜 측정**:
- recall@5는 "top-5 안에 있나"만 봄. **순위는 무시**
- MRR은 "얼마나 위에 있나"까지 봄
- 같은 recall@5 = 1.0이라도 MRR이 1.0인 시스템은 항상 첫 결과, MRR이 0.5인 시스템은 평균 두 번째 결과
- LLM context window는 비싸므로 **상위 1-2개가 정답이면 K를 줄여 비용 절약** 가능

**해석**:
| 값 | 평균 rank | 의미 |
|---|---|---|
| 1.00 | 1 (항상) | Perfect — 첫 결과가 늘 정답 |
| 0.85+ | ~1.2 | 매우 좋음. K=3 정도로 줄여도 안전 |
| 0.70~0.85 | ~1.3~1.5 | 좋음. K=5 유지 권장 |
| < 0.70 | 1.5+ | 평균 두 번째 결과 — context 비용 ↑ |

**recall@5 = 1.0인데 MRR이 낮으면?**
- 정답은 무조건 top-5에 있지만 위치가 들쑥날쑥
- 원인 후보: 임베더가 "관련은 있지만 정답은 아닌" 결과를 더 가깝게 평가
- 해결: 모델 교체, re-ranking, 쿼리 전처리

### 3.3 p95 latency (warm) — "응답성"

**정의**: 쿼리 100개를 처리하면 95개가 그 시간 이하로 끝나는 latency.

```
1ms ─────── p50 (38ms) ─── p95 (43ms) ── max (62ms)
└── 50% 쿼리 ──┘
└──────── 95% 쿼리 ───────┘
```

**왜 p95이고 평균이 아닌가**:
- 평균은 outlier (예: 1초 걸린 쿼리 1개)에 끌려가 왜곡
- p95는 "최악의 5%도 여기까지"라는 **사용자 경험 상한**
- 운영 SLO는 거의 항상 p95 또는 p99로 정의됨

**warm vs cold**:
- **cold start**: 프로세스 시작 + 모델 로드 + 첫 추론. 대형 모델은 2-5초
- **warm**: 모델 로드 후 정상 운영 상태. 우리가 측정하는 건 warm
- 사용자 첫 쿼리만 cold (그 후엔 warm). MCP server는 long-running이라 warm 시간이 대부분

**왜 측정**:
- Claude Code는 사용자가 코드 작성 중 호출. **interactive responsiveness 필요**
- p95 < 100ms = 사용자가 지연을 거의 못 느낌
- p95 > 500ms = 사용자가 매번 잠시 멈춤 인식

**해석 (interactive 도구 기준)**:
| 값 | 사용자 경험 |
|---|---|
| < 50ms | 즉시. 키 입력 수준 |
| 50-200ms | 자연스러움. 표준 |
| 200-500ms | 살짝 지연 느낌 |
| > 500ms | 매번 멈춤 인식 |

**현재 43ms**: 대화형 도구로 충분히 빠름.

### 3.4 citation@1 — "인용 정확성"

**정의**: top-1 결과가 정답일 때, **줄 번호까지 정확히** 일치하는 비율.

```
citation@1 = (top-1 hit AND line_range overlaps) / (top-1 hits)
```

**recall@1과 차이**:
- recall@1 = 파일만 맞으면 OK
- citation@1 = 파일 + 줄 범위 모두 맞아야

**왜 측정**:
- LLM이 답변할 때 "이 코드 18-25줄 참조"처럼 인용. 줄 번호 틀리면 사용자가 헛수고
- 청커가 함수 경계를 잘 잡았는지 검증 (청크 = 의미 단위)
- 1.0이면 청커 + 임베더 조합이 의미 단위를 잘 보존

**해석**:
- 1.0 = 정답을 찾으면 인용 위치도 정확
- < 1.0 = 정답 파일은 맞지만 엉뚱한 줄. 청킹 전략 검토

### 3.5 build throughput (chunks/s) — "인덱싱 처리량"

**정의**: 초당 임베딩 계산 가능한 코드 청크 수.

```
throughput = total_chunks / build_duration_seconds
```

**현재 측정 (26 chunks / 16s)**: 1.6 chunks/s

**왜 측정**:
- `ckv build` 는 사용자가 처음 한 번 실행. 큰 repo면 오래 걸림
- 1M LOC ≈ 50K chunks 예상 (청크당 평균 20 lines)
- 1.6 chunks/s = **50K / 1.6 ≈ 8.7시간**. 야간 빌드 가능
- 17 chunks/s = 약 50분. 점심 시간에 끝남
- 0.3 chunks/s (bge-code-v1 FP32 추정) = **46시간**. 주말 시작 필요

**영향 요인**:
1. 모델 크기 (작을수록 빠름)
2. **배치 크기** (한 번에 32개 청크 처리 vs 1개씩) — FU-8
3. 하드웨어 가속 (CPU vs CoreML/Metal vs CUDA)
4. ONNX optimization level

**해석**:
| 값 | 사용 시나리오 |
|---|---|
| > 50 chunks/s | 대형 repo (1M+ LOC) 빌드 가능 |
| 10-50 chunks/s | 중형 repo (100K LOC) 점심 시간 빌드 |
| 1-10 chunks/s | 소형 repo (10K LOC) 또는 야간 빌드 |
| < 1 chunks/s | testdata/sample 같은 demo 한정 |

---

## 4. 평가 방법론 한계

### 4.1 N=10 — 통계적 신호 약함

현재 fixture는 10개 쿼리. recall@5 = 1.0이라는 결론의 신뢰도:

- 10개 중 10개 성공 → 99% 신뢰구간으로 "실제 recall ≥ 0.69"
- 50개 중 50개 성공 → 99% 신뢰구간으로 "실제 recall ≥ 0.92"
- 100개 중 100개 성공 → "실제 recall ≥ 0.96"

**결론**: 현재 수치는 **존재 증명**이지 정량적 보장은 아님. FU-7 (≥50 queries 확장)이 필요한 이유.

### 4.2 fixture가 testdata/sample 한정

- 4개 파일 (`server.go`, `cache.go`, `handler.ts`, `token.sol`), 26 청크
- **실제 사용 repo는 청크 수만 1000x+**
- recall@5 = 1.0이 작은 repo에서 성립한다고 큰 repo에서도 성립하지 않음
- 큰 코퍼스일수록 "비슷한 의미의 다른 청크"가 늘어나 retrieval 어려워짐

**향후**: 더 큰 fixture (실제 OSS repo + 손으로 만든 쿼리)로 확장.

### 4.3 자연어 쿼리 품질

- 현재 쿼리들은 "TCP socket bind on port" 같은 짧고 명확한 의도
- 실제 사용자 쿼리는 더 모호하거나 길거나 도메인 전문 용어 포함
- "벤치마크 쿼리"와 "실제 쿼리"의 분포 차이는 영원한 RAG 평가 문제

---

## 5. 결과 해석 워크플로우

새 모델 또는 새 청킹 전략을 도입한 후:

```
1. ckv build 빌드 시간 + chunks/s 기록
2. ckv eval recall@1/3/5, MRR, citation@1 측정
3. 이전 baseline과 비교 → 회귀 감지
4. 차이가 있으면:
   - recall ↓ → 임베더가 의미를 잘 못 잡음
   - MRR ↓ + recall = → 순위만 흐트러짐 (re-ranking 검토)
   - citation@1 ↓ → 청커 문제
   - throughput ↓ → 더 큰 모델로 갔거나 배치 비효율
```

**baseline 저장**: `eval` 명령이 JSON 출력을 지원하면 (FU 후보), 매 PR마다 비교 가능.

---

## 6. 현재 실측 (2026-05-18, bge-large-en-v1.5)

| 지표 | 값 | 해석 |
|---|---|---|
| recall@5 | **1.000** | top-5 안에 정답 항상 포함 ✅ |
| recall@3 | 0.900 | top-3까지만 봐도 90% 적합 |
| recall@1 | 0.600 | 첫 결과 적중률 60% (개선 여지) |
| MRR | **0.770** | 평균 rank ≈ 1.3 |
| citation@1 | 1.000 | 정답일 때 줄 번호 정확 ✅ |
| p95 latency (warm) | **43 ms** | interactive 도구로 충분 ✅ |
| build throughput | **1.6 chunks/s** | demo 한정. 대형 repo는 FU-8 필요 ❌ |

**한계 명시**: N=10, testdata/sample 한정. 큰 repo 일반화 보장 없음.

---

## 7. Related

- 평가 fixture 형식: `testdata/queries.yaml` 헤더 주석
- 임베더 비교 결과 history: `docs/d1-onnx-poc.md` §3.3
- 향후 fixture 확장: FU-7 (`docs/d1-onnx-poc.md` §6)
- build throughput 개선: FU-8 (배치 + CoreML EP)
