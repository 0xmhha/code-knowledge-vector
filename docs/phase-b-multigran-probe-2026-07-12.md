# Phase B (multi-granularity) — go/no-go 프로토타입 실측

> **시점**: 2026-07-12 (측정 스냅샷).
> **목적**: roadmap Phase B("한 코드를 여러 입도로 동시 임베딩" — 2차 class/struct·3차
> file 전체 coarse 청크 추가, §3.1)를 **전체 구현(~250 LOC·throughput −50%) 전에**
> 저비용 프로토타입으로 go/no-go 판정. prefix 레버 스윕(`prefix-lever-sweep-2026-07-12.md`)이
> "강한 D.2조차 rule-based를 못 이김"으로 Phase B 한계이득에 회의적 신호를 준 것을 실측으로 닫는다.
> **관련**: `retrieval-quality-roadmap.md`(§3.1 Phase B), `eval-hard-fixture-2026-07-12.md`.

## 1. 방법

- **프로토타입**: `internal/chunk`에 opt-in coarse 청크 `file_full`(파일 전체) 추가
  (`Options.IncludeFileFull`, env `CKV_EXPERIMENTAL_FILE_FULL=1`, 기본 off). 파일당 fine
  심볼 청크 + file_header + **file_full**(coarse)이 함께 검색에 경쟁. 우리 코퍼스는 파일이
  작고 대체로 단일-타입이라 whole-file이 2차(class/struct)+3차(file)의 합리적 프록시.
- **임베더**: `bge-m3`. **코퍼스**: `testdata/sample`(7파일).
- **fixture 3종**:
  - `queries-coarse.yaml`(N=8, 신설) — 파일/타입/모듈 레벨 의도(Phase B가 **도와야** 하는 케이스).
  - `queries-hard.yaml`(N=24) — 심볼 레벨(coarse 청크가 **해치면 안 되는** 회귀 체크).
  - `queries.yaml`(N=50) — 기존 회귀 체크.
- baseline(D.1, coarse 없음) vs 프로토타입(D.1 + file_full)을 세 fixture로 비교.

## 2. 결과 (bge-m3, k=5)

### COARSE probe (N=8) — Phase B가 이득을 줘야 하는 케이스
| | recall@1 | recall@3 | recall@5 | MRR | found |
|---|---|---|---|---|---|
| D.1 baseline | 0.62 | **1.00** | 1.00 | **0.792** | 8/8 |
| D.1 + file_full | 0.62 | 0.88 | 1.00 | 0.754 | 8/8 |

### HARD (N=24) — 심볼 레벨 회귀 체크
| | recall@1 | recall@3 | recall@5 | MRR | found |
|---|---|---|---|---|---|
| D.1 baseline | 0.58 | 0.71 | **0.88** | 0.669 | **21/24** |
| D.1 + file_full | 0.62 | 0.75 | 0.79 | 0.691 | 19/24 |

### EASY (N=50) — 회귀 체크
| | recall@1 | recall@5 | MRR |
|---|---|---|---|
| D.1 baseline | 0.86 | 0.98 | 0.911 |
| D.1 + file_full | 0.86 | 0.98 | 0.912 |

청크 수: 50 → 56 (+12%, 소형 코퍼스; 실 코퍼스는 roadmap 추정 2~3배).

## 3. 판정 — **NO-GO / defer**

1. **이득이 없다 — 오히려 해가 된다.** coarse 청크가 도와야 할 coarse probe에서 recall@3이
   **1.00→0.88**로 떨어졌다(MRR 0.792→0.754). whole-file 청크가 정답 파일의 더 정밀한
   청크를 밀어내 순위를 흐렸다.
2. **헤드룸 자체가 없었다.** baseline coarse probe가 이미 recall@3=1.00 — 정답 파일이 항상
   top-3. 기존 fine 심볼 + file_header가 파일 레벨 질의를 충분히 답한다(작은 파일에서
   file_header ≈ 파일 전체).
3. **심볼 회귀.** hard에서 recall@5 **0.88→0.79**, found **21→19**(2개 심볼이 top-5 밖으로
   밀려 완전 miss). recall@1 +0.04는 bge-m3 재순위 노이즈 수준이고, recall@5/found 손실이 실질.
4. **비용은 큼.** 소형에서 +12% 청크, 실 코퍼스는 2~3배 + throughput −50%(roadmap).

→ 이득 0(또는 음), 회귀 有, 비용 大. **Phase B는 이 코퍼스에서 no-go.** prefix 스윕과 동일
결론: rule-based가 이미 symbol/file 신호를 싸게 포착해 추가 granularity의 한계이득이 없다.

## 4. 결정·후속

- **Phase B 전체 구현 보류.** `file_full` 프로토타입은 **gated off**로 유지
  (`CKV_EXPERIMENTAL_FILE_FULL`, 테스트 `TestIncludeFileFullEmitsCoarseChunk`) — D.2 LLM
  prefix와 동일 처리(측정상 열세지만 재검증용 opt-in 보존).
- **재검증 조건 [Mid]**: 파일이 크고(file_header가 파일 전체를 못 덮음) 한 파일에 여러 타입/
  많은 메서드가 있는 **대형 이질 코퍼스**. 거기선 2차(class-body) granularity가 fine/header와
  실제로 달라 이득 가능 — 본 프로토타입(whole-file)은 그 축을 근사만 함.
- roadmap Phase B 행: 이 실측(no-go on small corpus) 기록으로 갱신.

## 5. 캐비엇

- **동일 소형 코퍼스(7파일·단일-타입)·N=8 coarse**. whole-file은 2차(class-body)의 근사일
  뿐 — 다중 타입 파일에서의 class-body granularity는 미측정. 대형 코퍼스 재검증이 유효.
- 순수 벡터 eval(BM25 off), bge-m3 한정.
