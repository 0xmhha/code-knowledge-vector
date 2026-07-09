# PR 기반 retrieval 편향 측정 — 실측 기록

> **시점**: 2026-07-08 (측정 스냅샷, 진행 중 — 다중 PR 확장 예정).
> **목적**: 앞서 `build-knowledge.sh`의 "사람-워딩 의미검증 10/10"이 **자가 출제 편향**
> (질의·정답 모두 저자가 작성)일 수 있다는 우려를, **객관적 정답으로 재측정**해 확인한다.
> **방법론 출처**: [`evaluation-design-2026-05-22.md`](./evaluation-design-2026-05-22.md) (R8),
> 구현 `internal/eval/prregress/`, fixture `testdata/prs.yaml`.

## 1. 왜 이 측정인가

기존 10/10은 질의(패러프레이즈)와 기대 파일을 **저자가 직접 골랐다** → teaching-to-the-test
위험. 편향 없는 측정 = **질의도 정답도 저자가 만들지 않은 것**을 쓴다:
- **질의** = go-stablenet 실제 merged PR의 제목/본문 (사람이 쓴 글)
- **정답** = 그 PR이 *실제로 바꾼 파일* (git diff / gh, 객관)
- **인덱스** = 그 PR의 `base_sha`("수정 전 세계", 누설 방지)
- **지표** = recall@k(변경 파일이 top-k에 있나) / MRR(첫 정답 순위)

## 2. PR-77 (base_sha `0bf2f4d1b` = pr-77-2/ckv, 재빌드 0)

**정답 파일(2)**: `eth/gasprice/anzeon.go`, `core/txpool/legacypool/legacypool.go`
**인덱스**: `knowledge-data/pr-77-2/ckv` (bge-m3@1024, 완전 레시피)

| 질의 (출처) | anzeon.go 순위 | legacypool.go 순위 | recall@10 | MRR |
|---|---|---|---|---|
| 실제 PR 제목 | 1 | MISS(>20) | 0.5 | 1.0 |
| fixture intent | 2 | MISS(>20) | 0.5 | 0.5 |
| PR 본문(RemotesBelowTip 명시) | 1 | 9 | 1.0 | 1.0 |

## 3. 해석 (정직)

1. **10/10은 낙관적 — 편향 가설 부분 확정.** 객관 정답으로는 recall@10이 0.5~1.0로, 깔끔한
   10/10이 아니다. 손질한 질의/기대가 더 쉬웠다.
2. **핵심 브리지는 실효 — 최악 반증.** 주 파일(anzeon.go)이 실제 PR 제목 포함 3질의 모두 rank 1~2.
   의미 브리지 방향은 확고.
3. **놓친 파일 = 아키텍처 경계.** legacypool.go(RemotesBelowTip)는 질의가 명시해야 잡힘. 이는 순수
   벡터 검색의 한계이자 **설계상 CKG(`impact_analysis`/`find_callers`)의 몫** — CKV는 의미적 진입점을
   찾고 2차 영향 파일은 그래프가 확장. 즉 갭은 다중 백엔드 설계를 검증하고 CKV 역할을 정량화한다.

## 4. 캐비엇

- **N=1 PR, 정답 2파일** — 방향만. 통계적 결론 아님 → §5 다중 PR 확장으로 보강.
- 파일 단위(라인/심볼 아님).
- "변경 파일 전부를 벡터가 회수" metric은 다중 백엔드엔 다소 가혹(일부는 CKG 몫).

## 5. 다중 PR 확장 (완료, 2026-07-09)

`testdata/prs.yaml`의 **실제 go-stablenet PR 12개** 전부 측정. 각 PR의 `base_sha`로 코드-only
bge-m3 인덱스를 빌드(누설 방지) → 질의 = 실제 PR 제목+본문(gh, 사람 워딩) → 정답 = 그 PR이 바꾼
go/sol 비-test 파일. 파일 단위 recall@k / MRR.

> **측정 인프라 노트**: gh를 빌드 루프에 섞으니 4시간 중 일시적 gh blip으로 스윕이 중단됨 →
> gh 메타를 **앞당겨 프리페치·캐시**한 뒤 오프라인 빌드 루프로 재실행(재현 스크립트 계열).

### 5.1 결과 (12 PR)

| PR | 변경파일 | recall@5 | recall@10 | MRR | 회수된 정답(rank) |
|---|---|---|---|---|---|
| pr72 | 1 | 1.00 | 1.00 | 0.33 | api.go:3 |
| pr75 | 1 | 1.00 | 1.00 | 1.00 | backlog.go:1 |
| pr77 | 2 | 0.50 | 1.00 | 1.00 | anzeon.go:1, legacypool.go:8 |
| pr67 | 2 | 1.00 | 1.00 | 0.25 | contracts.go:4, evm.go:5 |
| pr69 | 2 | 0.50 | 0.50 | 1.00 | genesis.go:1 |
| pr74 | 2 | 0.00 | 0.50 | 0.11 | legacypool.go:9 |
| pr58 | 3 | 0.00 | 0.67 | 0.17 | genesis.go:9, backend.go:6 |
| pr55 | 5 | 0.40 | 0.40 | 1.00 | core.go:2, preprepare.go:1 |
| pr73 | 5 | 0.20 | 0.40 | 1.00 | gov_council.go:1, genesis.go:6 |
| pr70 | 6 | 0.17 | 0.17 | 1.00 | receipt.go:1 |
| pr56 | 6 | 0.50 | 0.67 | 0.50 | gov_base.go:2, gov_council.go:3, coin_adapter.go:4, gov_validator.go:6 |
| pr63 | 8 | 0.00 | 0.00 | 0.00 | (전부 MISS — GovMinter v2 하드포크 광역 변경) |

**전체 평균**: recall@5 = **0.44**, recall@10 = **0.61**, MRR = **0.61**

### 5.2 변경 파일 수별 분리 (핵심 발견)

| 그룹 | PR 수 | mean recall@5 | mean recall@10 | mean MRR |
|---|---|---|---|---|
| **≤2 파일** | 6 | 0.67 | **0.83** | 0.62 |
| **≥3 파일** | 6 | 0.21 | **0.38** | 0.61 |

- **recall@10이 파일 수에 급감**(≤2파일 0.83 → ≥3파일 0.38, 절반 이하). 광역 변경(pr63 8파일=0.0)은
  순수 벡터 검색이 못 잡음.
- **MRR은 두 그룹 거의 동일(~0.61)** — 변경 규모와 무관하게 CKV는 *정답 1개를 평균 rank ~1.6에* 회수.
  즉 **의미적 진입점은 안정적으로 찾지만, 변경 전체(blast radius)의 recall은 규모에 반비례.**

## 6. 결론 (편향 질문에 대한 답)

1. **"10/10"은 편향된 낙관치 — 확정.** 객관 정답으로는 recall@10 = 0.61(파일 단위). 자가 출제
   질의셋이 실제보다 쉬웠다.
2. **그러나 브리지는 실효 — MRR 0.61.** CKV는 사람 워딩(PR 설명)에서 관련 코드 파일을 상위권으로
   안정 회수한다(진입점). "가짜 10/10"은 아니다.
3. **핵심 통찰: CKV는 진입점, CKG는 blast radius.** recall이 다중파일 PR에서 무너지는 건 CKV의
   결함이 아니라 **설계 경계** — 나머지 파일은 CKG `impact_analysis`/`find_callers`가 확장하도록
   의도됨. 이 측정이 그 역할 분담을 수치로 정당화한다.
4. **가장 약한 지점**: 광역 하드포크/wiring 변경(pr63/pr70)에서 벡터 검색 무력 → hybrid(CKV+CKG)
   fusion이 필수임을 재확인.

## 7. 캐비엇

- N=12(작음), 파일 단위(라인/심볼 아님). 지표는 방향성.
- "변경 파일 전부 회수" metric은 다중 백엔드엔 가혹 — 일부는 CKG 몫(위 결론 3).
- 코드-only 인덱스(docs/flow 제외)로 측정(PR은 코드를 바꾸므로 무관). PR-77 §2는 full-recipe였고
  동일 결과(recall@10=1.0).
