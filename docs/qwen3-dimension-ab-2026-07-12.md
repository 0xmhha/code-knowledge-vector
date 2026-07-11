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

## 6. 대형 코퍼스 재확인 (2026-07-12, N=50 후속)

§4 캐비엇("대형 코퍼스 재확인 후 ADR 락")을 수행했다.

- **코퍼스**: go-stablenet(`wemade/go-stablenet`) 8개 패키지 서브셋, **83파일 / 1015청크**(N=50 대비 ~20×).
  code-only, qwen3-embedding:4b, full-2560 vs truncate-1024, `--batch 8`.
- **fixture**: `scripts/semantic-validation-queries.json`(사람-워딩 한국어 10쿼리 → 기대 go 파일).

| 지표 | 결과 |
|---|---|
| top-1 파일 일치(full vs 1024) | **8/10** |
| top-5 파일 overlap(mean Jaccard) | **0.81** |
| 코퍼스-내 ground-truth 쿼리(3건) 순위 | **full=1024 완전 동일**(2/2, 1/1, 1/1) |
| vector.db 크기 | 11.4 MB → **5.4 MB** (÷2.1) |

→ **1024-truncate가 20× 큰 실 코드 코퍼스에서도 full-2560과 실질 동등**(top-1 8/10, ground-truth 동일 순위),
저장 절반. N=50 결과 재확인. **1024-truncate 권장을 유지**하며 ADR 승격 가능.

### 6.1 캐비엇 / 인프라 노트

- **쿼리 커버리지**: fixture 10쿼리 중 타겟이 이 서브셋 코퍼스에 존재하는 건 3건(나머지는 필터 범위 밖
  또는 대형파일 제외). ground-truth recall은 3건 한정, 나머지는 top-1 일치율(ground-truth-free)로 보강.
- **ollama qwen3 임베딩 안정성**: `ollama 0.30.9`는 ~7KB+ 입력에서 크래시(HTTP 400 EOF) → `0.31.2` 업그레이드로
  격리 입력은 회복. 그러나 빌드 중 **개별 대형 청크(단일 ~20-40KB 입력)는 4b·0.6b 모두 여전히 크래시** —
  대형 파일 7개(blockchain/genesis/genesis_alloc/legacypool/handler/worker/v5_udp)를 양쪽 동일 제외.
  ckv에 `ckv build --batch N` 추가(대형 청크 배치 거부하는 임베더용). 개별 청크 크래시는 배치 무관이라
  근본 해소는 embed 경로 견고화(재시도/skip) 또는 정본 머신(bge-m3 안정) 필요 — 별도 과제.
- 정본 pr-77-2 데이터셋·`0bf2f4d1b`는 이 머신에 부재, 현재 wemade HEAD로 근사(상대 A/B라 커밋 무관).
