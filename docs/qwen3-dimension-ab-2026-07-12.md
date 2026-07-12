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

- **Instruct query-prefix** — ✅ 구현·측정 완료(2026-07-12, §7).
- **knownDims 합의** — 허용 truncate 차원 집합(예: 512/1024/2560) 표준화.

## 7. Instruct query-prefix (2026-07-12)

§5의 분리 레버를 구현했다. Qwen3-Embedding은 쿼리에만 `Instruct: {task}\nQuery: {q}` 프리픽스를
권장한다(passage는 raw).

- **구현**: 옵션 `types.QueryEmbedder` 인터페이스(비대칭 모델용) + 레지스트리 `ModelConfig.QueryInstruct`
  (qwen3 엔트리만 설정, bge-*는 빈값) + ollama `EmbedQuery`(qwen3 쿼리 래핑, 그 외 = `Embed`). query.Engine의
  쿼리 임베딩 경로(`EmbedService.Run`/explain)가 `QueryEmbedder` 구현 시 `EmbedQuery`로 라우팅. 인덱스(passage)
  재빌드 불요 — query-side만. `CKV_DISABLE_QUERY_PREFIX=1`로 opt-out(A/B·디버그).
- **측정**(gs-full 인덱스, semantic-validation 10쿼리, prefix on/off): recall@10 **3/10 → 4/10**
  (chaincmd.go MISS→rank 8 회복, handler 2→1, 나머지 동일, 회귀 없음). 프리픽스는 이득/중립.
- 남은 것: `knownDims` 표준화.

## 8. 모델 크기 축 — 0.6b-native-1024 vs 4b-truncate-1024 (2026-07-12)

차원 축(§1–7)은 1024로 확정됐다. 남은 축은 *모델 크기* — 같은 1024차원을 **작은
0.6b(native 1024)** 로 뽑느냐 **큰 4b(2560→1024 truncate)** 로 뽑느냐.

- **코퍼스**: §6과 동일 go-stablenet 서브셋(83파일 / **1015청크**, 동일 필터 → 동일 코퍼스).
  두 인덱스 모두 **1024차원**. 쿼리는 각 모델로 임베딩(Instruct prefix 양쪽 적용).

| 지표 | 0.6b-native | 4b-trunc-1024 |
|---|---|---|
| recall@10 (ground-truth, 코퍼스-내 타겟) | **4/10** | **4/10** |
| chaincmd.go 순위 | 7 | 9 |
| consensus handler.go 순위 | 2 | 1 |
| core/txpool, p2p/discover 순위 | 1 / 1 | 1 / 1 |
| 찾은 타겟 평균 순위 | **2.75** | 3.0 |
| vector.db 크기 | 5.4 MB | 5.4 MB (동일) |
| **모델 크기** | **639 MB** | 2.5 GB (**~4×**) |
| 대형 입력 안정성 | 더 안정 | ~20–40KB 단일 입력서 크래시(embed 견고화로 skip) |

→ **0.6b-native-1024가 4b-truncate-1024와 실질 동등**(recall 동률, 순위 상쇄·평균은 0.6b가 근소
우위). 저장은 동일하나 **0.6b는 모델이 4× 작고 더 안정**하다. 운영 관점에서 **0.6b가 강력한
후보** — 같은 검색 품질을 훨씬 낮은 메모리·로드 비용·크래시 리스크로 얻는다.

### 8.1 캐비엇 (초기 측정)

- 위는 **N=4 측정 가능 쿼리**(나머지 6은 타겟이 서브셋 밖) — 방향성만. §8.2에서 대형 코퍼스로 재확인.

### 8.2 대형 코퍼스 재확인 (2026-07-12)

10개 semantic-validation 타겟을 **모두 포함**하는 넓은 필터로 재측정 — go-stablenet 11개 패키지
**123파일 / 1834청크**(§8 서브셋의 ~1.8×, 측정 쿼리 4→**9**). 두 인덱스 모두 1024차원, 동일 코퍼스.

| 지표 | 0.6b-native | 4b-trunc-1024 |
|---|---|---|
| recall@10 (ground-truth) | **9/10** | **9/10** (동률) |
| MRR@10 | 0.683 | **0.748** |
| 찾은 타겟 평균 순위 | **1.56** | 1.89 |
| vector.db 크기 | 10.6 MB | 10.6 MB (동일) |
| 빌드 안정성 | **0 skip** (완주) | 7청크 크래시→skip(견고화) |
| 모델 크기 | **639 MB** | 2.5 GB (~4×) |

**정정**: §8(N=4)의 "0.6b 근소 우위"는 소표본 잡음이었다. N=9 대형에서는 **4b가 MRR 근소 우위
(0.748 vs 0.683, 상대 ~9%)** — 대부분 rank 1, keystore 1건만 0.6b(2)≪4b(7)로 역전. recall은 동률.

**결론 = 트레이드오프, 어느 쪽도 압도 안 함**:
- **4b-trunc-1024**: 정밀도(MRR) 소폭 우위. 단 2.5GB + 대형 입력 크래시(견고화 의존).
- **0.6b-native-1024**: recall 동률·MRR 소폭 열세지만 **4× 작고(639MB) 완전 안정(0 skip)**, 저장 동일.

→ **ADR-008(차원 축 4b@1024)을 뒤집을 근거는 없음**(4b가 품질에서 밀리지 않음). 다만 **0.6b는
메모리·안정성이 결정적인 배포에서 품질 손실 거의 없이 쓸 수 있는 강력한 대안**. 권장 모델의
최종 선택(4b vs 0.6b)은 배포 제약(메모리 vs 최대 정밀도)에 따라 CKS/운영이 결정 — 본 측정이
두 선택지의 비용·품질을 정량화한다.

- 남은 것: `knownDims` 표준화(완료), 다중 fixture(why-queries 등) 확장은 선택.

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
