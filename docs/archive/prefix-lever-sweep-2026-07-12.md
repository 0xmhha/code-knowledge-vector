# Prefix lever sweep — raw vs D.1 rule-based vs D.2 LLM (hard fixture)

> **ARCHIVED 2026-07-19.** Measurement record; the go/no-go verdict is locked in [`adr/009-rule-based-prefix-default.md`](../adr/009-rule-based-prefix-default.md). Kept for provenance.

> **시점**: 2026-07-12 (측정 스냅샷).
> **목적**: hard fixture(`eval-hard-fixture-2026-07-12.md`)로 측정 대역이 열린 지금,
> 임베드-텍스트 prefix 레버 3종을 한 번에 재측정해 **어느 레버가 실제로 이득인지**
> 결정한다. 특히 D.2 PoC(`llm-contextual-prefix-poc-2026-07-12.md`)가 "천장이라 미측정"으로
> 남긴 캐비엇 — *헤드룸이 생기면 D.2가 D.1을 이기는가* — 을 닫는다.
> **관련**: `retrieval-quality-roadmap.md`(Phase D), `internal/chunk/prefix.go`(D.1),
> `internal/llmprefix/`(D.2).

## 1. 방법

- **임베더**: `bge-m3`(Ollama, 1024-dim, l2, 대칭 — 쿼리 prefix 없음, passage 측만 레버 차이).
- **코퍼스**: `testdata/sample`(7파일/50청크). **fixture**: `queries.yaml`(N=50, easy) +
  `queries-hard.yaml`(N=24, hard).
- **레버 3종** (passage 임베드 텍스트 조성):
  - **raw**: 프리픽스 없음(`CKV_DISABLE_CONTEXTUAL_PREFIX=1`, `chunk.RawEmbedText`).
  - **D.1 rule-based**(기본값): `"language: X. file: Y. symbol: Z.\n\n<raw>"`(`chunk.BuildEmbedText`).
  - **D.2 LLM**: llama3 한 문장 + D.1 + raw(`--llm-prefix-model llama3`, 조합형).
- 각 레버로 인덱스 빌드 → 두 fixture에 `ckv eval -k 5`.

## 2. 결과 (bge-m3)

### EASY (queries.yaml, N=50)
| lever | recall@1 | recall@3 | recall@5 | MRR | found |
|---|---|---|---|---|---|
| raw | 0.70 | 0.90 | 1.00 | 0.817 | 50/50 |
| **D.1 rule-based** | **0.86** | 0.96 | 0.98 | **0.911** | 49/50 |
| D.2 LLM | 0.76 | 0.98 | 1.00 | 0.862 | 50/50 |

### HARD (queries-hard.yaml, N=24)
| lever | recall@1 | recall@3 | recall@5 | MRR | found |
|---|---|---|---|---|---|
| raw | 0.42 | 0.71 | 0.75 | 0.543 | 18/24 |
| **D.1 rule-based** | **0.58** | 0.71 | **0.88** | **0.669** | 21/24 |
| D.2 LLM | 0.54 | 0.75 | 0.83 | 0.665 | 20/24 |

## 3. 판정

1. **D.1(rule-based)이 두 fixture 모두 명확한 승자.** raw 대비 recall@1 **+0.16**(easy·hard
   동일), hard에서 MRR·recall@5도 최고. 규칙 기반 프리픽스의 정확한 symbol/file 토큰이
   북극성(정밀 회수)에 직접 기여한다 — **기본값 ON 유지가 실측으로 확증**됨.
2. **D.2 "헤드룸 가설" 반증.** 천장을 벗어난 hard set에서도 D.2가 D.1을 **못 이김**
   (recall@1 0.54<0.58, MRR 0.665≈0.669, recall@5 0.83<0.88). D.2 열세는 *소형 코퍼스
   천장 아티팩트가 아니라* 실제 특성 — LLM 산문이 rule-based의 정확 토큰을 희석한다.
   → **D.2 기본 비활성 유지 확정**, PR #36 캐비엇 종결.
3. **raw가 최하** — 어떤 prefix든 무프리픽스보다 낫고, 그 중 rule-based가 최선.
4. D.2는 raw보다는 나음(hard recall@1 0.54>0.42) + higher-k에서 소폭 우위(easy recall@3
   0.98, recall@5 1.00) — 산문이 *약간의* 보조 신호는 주나 recall@1/MRR에서 D.1에 밀린다.

## 4. 결정·함의

- **prefix 기본값 = D.1 rule-based 유지**(변경 없음). raw·D.2는 열세로 실측 확인.
- **D.2 LLM prefix = opt-in·기본 off 유지**(변경 없음). 이번 스윕이 #36 캐비엇을 정리 —
  "대형 코퍼스/헤드룸에서 이득"이라는 여지 중 *헤드룸* 축은 반증됨(대형 코퍼스·강한 생성기·
  BM25 병용 축은 여전히 미측정).
- **Phase B(multi-granularity) 투자 재고 신호 [Mid]**: 강한 레버인 D.2조차 rule-based를
  못 이겼고, rule-based가 이미 핵심 신호(symbol/file)를 싸게 포착한다. 더 무거운 granularity
  작업의 한계이득이 작을 수 있어, Phase B 착수 전 소형 프로토타입으로 hard fixture 신호를
  먼저 확인하는 것을 권장.

## 5. 캐비엇

- **동일 소형 코퍼스(7파일)·bge-m3·N=24(hard)**. 방향성 강하나 절대 수치는 대형 코퍼스에서
  이동 가능. hard fixture는 *변별력*용.
- D.2 생성기 = llama3(8B). 강한 생성기(Claude 등)면 산문 품질이 올라 결과가 달라질 여지 —
  본 스윕의 반증 범위는 "llama3·헤드룸"에 한정.
- 순수 벡터 eval(BM25 rerank off). Anthropic Contextual Retrieval의 절반(contextual BM25)
  미측정.
