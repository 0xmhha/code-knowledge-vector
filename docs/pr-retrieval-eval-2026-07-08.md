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

## 5. 다중 PR 확장 (진행 중)

`testdata/prs.yaml`의 12개 실제 PR(pr55/56/58/63/67/69/70/72/73/74/75/77) 중 PR-77 완료.
나머지 11개는 각 `base_sha`로 코드-only 인덱스(bge-m3) 빌드 후 동일 측정 → 집계(mean recall@5/@10,
MRR). base_sha가 모두 달라 PR당 ~20분 빌드. 결과는 본 문서에 추가.
