# Qwen3-Embedding 차원 A/B — 1024-truncate vs full-dim 실측

> **시점**: 2026-07-12 (측정 스냅샷). 채택 확정 시 ADR 승격.
> **목적**: 협의 결정6("임베딩 차원 = 실측 후 결정, CKV 주관, 측정 전 확정 금지")을
> 실측으로 닫는다. Qwen3-Embedding은 MRL(Matryoshka Representation Learning) 학습이라
> 전체 벡터의 앞부분을 잘라 재정규화하면 그 자체로 저차원 임베딩이 된다 —
> **1024로 잘라도 쓸 만한가**를 정밀도로 판정한다.
> **관련**: `embedding-model-recommendation-2026-06-22.md`, `retrieval-quality-roadmap.md`,
> 협의 doc 결정6, `adr/002-bge-large-pivot.md`.

## 1. 방법

- **모델**: `qwen3-embedding:4b` (Ollama, native dim **2560**, l2, last-token pooling).
- **MRL truncate**: `pkg/embed/ollama` — `Options.TargetDim` → 임베딩을 앞 N차원으로
  절단 후 L2 재정규화(`truncateNormalize`). CLI `--embed-dim N`. probe는 절단 전(native)
  으로 실행해 유효성 검증에 native dim을 쓴다.
- **코퍼스**: `testdata/sample` (7파일, 50청크). **fixture**: `testdata/queries.yaml` (N=50).
- **A/B**: 같은 코퍼스를 두 차원으로 빌드 → 각 인덱스에 `ckv eval` (쿼리 임베딩도 동일 차원).
  ```
  ckv build --embedder ollama --model-name qwen3-embedding:4b            --out full   # 2560
  ckv build --embedder ollama --model-name qwen3-embedding:4b --embed-dim 1024 --out t1024
  ckv eval  ... --out full   --fixture testdata/queries.yaml -k 5
  ckv eval  ... --embed-dim 1024 --out t1024 --fixture testdata/queries.yaml -k 5
  ```

## 2. 결과 (queries.yaml, N=50)

| 지표 | full 2560 | truncate 1024 | Δ |
|---|---|---|---|
| recall@1 | **0.88** | 0.86 | −0.02 |
| recall@3 | 0.96 | 0.94 | −0.02 |
| recall@5 | 0.96 | 0.96 | 0 |
| MRR | **0.913** | 0.902 | −0.011 |
| citation@1 | 1.00 | 1.00 | 0 |
| **vector.db 크기** | 10.6 MB | **4.3 MB** | **÷2.47** |

- 정밀도 손실은 **recall@1 −2%p, MRR −1.1%p, recall@5 0** — 미미.
- 저장 비용은 **2.47배 감소**(2560/1024=2.5에 근사). 벡터 검색 비용도 차원에 비례 감소.

## 3. 결정 (권장)

**1024-truncate 권장.** 근거: full-2560 정밀도의 ~98%(recall@1 0.86/0.88, MRR 0.902/0.913)를
**40% 저장·검색 비용**으로 확보 — MRL 트레이드오프의 sweet spot. 북극성(정밀 회수) 관점에서
recall@1 −2%p는 실측상 작고, 대형 코퍼스(1M LOC)에서 2.5× 저장·지연 절감은 운영상 크다.

- **trade-off 명시**: 최대 정밀도가 절대 우선이고 저장이 제약이 아니면 full-2560이 fallback.
- 이 결정은 **아래 캐비엇 해소 후 ADR 승격**한다(측정은 끝났으나 표본이 작다).

## 4. 캐비엇

- **N=50, testdata/sample(50청크)** — 방향성. recall@5는 두 차원 모두 0.96로 천장에 근접해
  변별력이 recall@1/MRR에 집중된다. **대형 코퍼스(go-stablenet 정본) 재확인 후 ADR 락** 권장.
- **why-queries.yaml 미측정** — fixture 형식이 이 eval 경로와 맞지 않아 제외(PR/why 코퍼스 의존).
- 4b-truncate-1024만 측정. **qwen3-embedding:0.6b(native 1024)** 와의 비교(모델 크기 축)는
  별개 실험 — 본 A/B는 *차원* 축만 판정한다.

## 5. 분리된 후속 (본 결정과 독립)

- **Instruct query-prefix** — Qwen3-Embedding은 쿼리에 `Instruct: <task>\nQuery:` 프리픽스를
  권장하나, 현 `Embedder.Embed`는 query/passage를 구분하지 않는다(문서=무프리픽스). 프리픽스
  주입은 인터페이스 확장이 필요한 별도 품질 레버. `remaining.md` 참조.
- **knownDims 합의** — 허용 truncate 차원 집합(예: 512/1024/2560) 표준화.
