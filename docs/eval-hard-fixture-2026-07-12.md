# Hard eval fixture — ceiling 해소 실측

> **시점**: 2026-07-12 (측정 스냅샷).
> **목적**: 기본 fixture(`testdata/queries.yaml`)가 `testdata/sample` 소형 코퍼스에서
> retrieval **천장(ceiling)**에 도달해(bge-m3 recall@5 0.98) 품질 레버(contextual
> prefix·sliding split·multi-granularity)의 효과를 변별하지 못하는 문제를 해소한다.
> roadmap D1-FU-7("fixture 확장, 모든 패턴 측정의 선행조건").
> **관련**: `retrieval-quality-roadmap.md`(§0b·§5), `eval-metrics.md`,
> `llm-contextual-prefix-poc-2026-07-12.md`(D.2가 천장에서 이득 미측정).

## 1. 문제

기본 fixture(N=50)는 bge-m3에서 recall@5 0.98·recall@1 0.86으로 천장에 근접한다.
소형 코퍼스(7파일/50청크)에서 top-5 안에 정답이 거의 항상 들어오므로, 레버의 개선분이
측정 노이즈에 묻힌다. D.2 PoC가 "천장이라 이득 미측정"으로 끝난 근본 원인.

## 2. 방법

같은 `testdata/sample` 코퍼스에 대해 **의도적으로 어려운** fixture
(`testdata/queries-hard.yaml`, N=24)를 신설. 각 쿼리는 recall@1/MRR을 측정 대역으로
끌어내리도록 다음 레버로 설계:

- **zero-lexical-overlap**: 타깃 코드의 식별자를 쿼리에 쓰지 않음(순수 의도).
- **lexical decoy**: 쿼리 단어가 *오답 파일*의 표면 텍스트와 겹치게 함. 예) "contact
  **address**"→`server.Addr`/`token(address)` 유인(정답은 `ValidateEmail`);
  "**refuse/validate**"→`validator.*`/`validatePath` 유인(정답은 Solidity `onlyPositive`).
- **cross-language**: 정답이 예상 밖 언어. 예) "present an error"가 `handler.serverError`
  (정답)와 `client.formatError`(디코이) 사이에서 혼동.
- **indirect/negation**: 이름이 아니라 효과로 기술("자원이 풀리도록", "호스트에 묶인 caller").

빌드·eval:
```
ckv build --embedder ollama --model-name bge-m3 --src testdata/sample --out idx
ckv eval  ... --out idx --fixture testdata/queries.yaml       -k 5   # easy
ckv eval  ... --out idx --fixture testdata/queries-hard.yaml  -k 5   # hard
```

## 3. 결과 (bge-m3)

| fixture | N | recall@1 | recall@3 | recall@5 | MRR | found |
|---|---|---|---|---|---|---|
| easy (queries.yaml) | 50 | 0.86 | 0.96 | **0.98** | 0.911 | 49/50 |
| **hard** (queries-hard.yaml) | 24 | **0.58** | 0.71 | **0.88** | **0.669** | 21/24 |

- recall@1 **−0.28**(0.86→0.58), MRR **−0.242**(0.911→0.669), recall@5 −0.10(0.98→0.88).
- 24개 중 **10개가 non-rank-1**, 3개는 top-5 완전 miss(h10/h13/h17). 디코이가 의도대로
  작동: h1 "freed"→`client.js`(오답 top-1), h2 "accepting connections"→`token.sol`,
  h13 "consistent body when broke"→`token.sol`, h17 "caller pinned to host"→`server.go`.
- **측정 대역 확보**: recall@1·recall@5 모두 천장에서 이탈 → 레버가 위/아래로 움직일
  헤드룸이 생겼다.

## 4. 용도

- Phase A(sliding split)·Phase B(multi-granularity)·D.2(LLM prefix) 등 품질 레버의
  go/no-go를 이 hard fixture로 측정. easy fixture는 CI 회귀 게이트(천장 유지 = 무회귀).
- CI: `TestLoadHardQueriesFixture`가 fixture 로드·라인레인지 유효성 검증.

## 5. 캐비엇

- **N=24, 동일 소형 코퍼스**. recall@5가 0.88로 내려왔으나 여전히 7파일 코퍼스라
  후보 풀이 작다 — 대형 코퍼스에서의 절대 수치는 다를 수 있다. hard fixture는 *변별력*
  확보용이지 절대 성능 벤치가 아니다.
- 디코이 설계는 bge-m3 기준. 다른 임베더는 혼동 패턴이 달라 난이도가 이동할 수 있음
  (측정 시 baseline 재확인 필요).
- ground-truth는 단일 정답 파일:라인 가정. 일부 쿼리는 복수 파일이 그럴듯하나
  가장 정확한 타깃 하나로 라벨링(디코이 존재 자체가 설계 의도).
