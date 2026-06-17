# Flow/Causal Knowledge Integration — 설계 방향 (2026-06-16)

문서 버전: 1.0 (방향 합의용 — 상세 구현 설계 아님)
작성일: 2026-06-16
대상: CKV (code-knowledge-vector), 연관 CKG / go-stablenet corpus

> **이 문서의 위치:** 사용자 요구("LLM이 grep 정적 분석이 아니라 코드 flow·sequence를
> 따라 *현상의 원인*을 찾게 하라")에 대한 **검토 결과 + 개선 방향**이다. 단계별 Task·
> 영향 파일·DoD를 담은 정식 구현 plan은 스코프 확정 후 별도 작성한다(§7 참조).

---

## 0. 한 문단 요약

**root-cause 분석은 유사도 검색이 아니라 그래프 순회다.** 따라서 벡터 DB 단독으로는
불가능하며, `현상 → 불변식 → 강제지점 → 흐름 step → 인접 step(분기) → 최근 변경`이라는
*인과 체인*을 따라갈 데이터와 검색 primitive가 필요하다. 그 데이터의 대부분은 **이미
존재한다** — go-stablenet 유지보수자들이 손으로 큐레이션한 `corpus.jsonl`(255 레코드).
그러나 현재 CKV는 이를 구조 보존하여 인덱싱하지 않는다(PR #3 `--docs`는 평면 텍스트로만
적재). 핵심 작업은 **새 분석기를 만드는 것이 아니라, 이미 검증된 큐레이션 지식을 구조
보존해 적재하고 세 레이어(흐름/구조/코드)를 `file:line`·`symbol`로 잇는 것**이다.

---

## 1. 핵심 발견 — 큐레이션된 인과 그래프가 이미 있고, 통합만 끊겨 있다

### 1.1 go-stablenet flow corpus (`<go-stablenet>/.claude/docs/corpus/`)

성숙한 자기검증 파이프라인:

```
사람 작성: flows/*.md (4 spine + 14 entry-point) + invariants.md (16 INV)   ← 1차 출처
   │  build_corpus.py  (재현 가능, best-effort 파서, 형식 이탈 시 stderr 경고)
   ▼
corpus.jsonl (255 레코드)
   │  check_corpus.py  (참조 무결성: 엣지 대상 실존 / 불변식 양방향 대칭 / 고아 INV / 라인 누락)
   ▼
3-용도 (README 명시): ① cks 적재(vector+graph)  ② 벤치마크 정답  ③ 사람용 설계 문서
```

`corpus.jsonl` 레코드 분포:

| type | 수 | 담는 정보 |
|------|----|-----------|
| `flow` | 18 | entry point / spine 메타 (`root_symbol`, `links`, `called_by`) |
| `step` | 79 | **인과 분석 최소 단위** — `symbol`+`file:line`, `calls`(다음 step=시퀀스), `reads`/`writes`/`emits`(데이터 흐름), `branches`(`{when,then,at}`=실패모드↔원인), `invariants`(연결) |
| `edge` | 142 | 파생 그래프 엣지 (`calls` / `calls_flow` / `enforces`) |
| `invariant` | 16 | `statement`+`assumes`+`enforced_at`(속성→강제지점)+`check` |

### 1.2 corpus 설계자가 명시한 적재 매핑 (`SCHEMA.md §42-61`)

- **Vector DB**: 임베딩 청크 = `step.prose` (+ `symbol`·`branches[].when`을 본문/메타에
  포함하면 실패조건도 검색됨), `invariant.statement`/`assumes`/`check`. 메타 = `id`,
  `flow`, `symbol`, `file:line`, `kind`, `invariants`, `entry_point`.
- **Graph DB**: 노드 step/invariant/flow, 엣지 calls/calls_flow/enforces.
  **cks가 코드에서 추출한 호출그래프와 `symbol`로 조인.** corpus가 더하는 것:
  (a) entry-point 그룹핑 (b) prose 설명 (c) 분기/실패 조건 (d) 불변식 링크.
- **벤치마크**: "X 작업의 코드경로/영향" 질의의 정답 = corpus의 `calls`·`enforces`
  폐포(closure). cks의 `impact_analysis`/`get_subgraph` 결과를 이 폐포와 비교해
  precision/recall 채점. 음성 케이스 = `branches`의 실패 조건.

### 1.3 결정적 한 줄

> `SCHEMA.md §67`: **"실제 cks 적재 API/스키마 매핑은 별도 통합 작업이다. 이 JSONL은
> 그 입력 포맷이며, cks가 코드에서 뽑는 심볼/호출그래프 위에 얹는 레이어다."**

→ 이 통합이 미완의 과제로 명시돼 있다. 본 문서의 제안이 곧 그 작업이다.

---

## 2. 현재 capability 경계 (코드 조사 기반)

| 능력 | CKV (벡터) | CKG (그래프) | 큐레이션 corpus |
|------|:---:|:---:|:---:|
| 유사 코드 찾기 | ✅ semantic+BM25 | △ FTS | — |
| "누가 이걸 호출?" | ❌ | ✅ find_callers (calls/invokes, depth 2) | ✅ called_by |
| **순서/시퀀스** | ❌ | ❌ (call 엣지 有, 순서 無) | ✅ step.calls 체인 |
| **분기/실패모드** | ❌ | ❌ | ✅ step.branches |
| **데이터 흐름** | ❌ | △ reads/writes_field 구문적 | ✅ step.reads/writes |
| **불변식↔강제지점** | △ invariant 청크(텍스트만) | ❌ | ✅ enforced_at |
| 변경 이력 | △ related_changes | ✅ change_history (PR breadcrumb) | — |

**CKG 자체 조사 결론**: CKG는 정적 구조 그래프로 "what calls / what depends"는 잘하나,
**데이터 흐름·함수 내 제어 흐름·시퀀스/시간 순서·root-cause는 명시적으로 모델링하지 않음**
(노드 37종·엣지 43종 보유, 단 IfStmt 등 statement 노드 사이에 control-flow 엣지 없음).

→ "grep을 넘어선" 영역에서 CKG는 *호출 관계*를 채우고, **인과·시퀀스의 나머지는
corpus가 채우도록 설계돼 있는데 그 연결이 끊겨 있다.**

---

## 3. Q1 답 — 벡터 DB 지식 시스템 best practice & CKV 개선점

### 3.1 원칙 (이 프로젝트 맥락)

1. **모델 크기보다 corpus 품질.** 임베딩 모델 업그레이드보다 *무엇을 인덱싱하느냐*가
   품질을 더 좌우. 지금 최대 레버 = 큐레이션 corpus를 1급 시민으로.
2. **사람 큐레이션 권위 지식 > 자동 추출 청크.** 자동 invariant(Tier 3 휴리스틱)는
   노이즈 있음. `invariants.md`의 16 INV는 검증된 ground truth → 신뢰도 차등.
3. **Hybrid + rerank.** 벡터+BM25 보유(NEW-9, default off). 단 **cross-encoder rerank는
   stub**. 식별자 쿼리=BM25, 개념 쿼리=벡터, 그 위 rerank가 마지막 조각.
4. **평가 주도.** fixture·메트릭 인프라는 우수하나 **실 corpus 평가 미실행**
   (PRR-1 throughput 보류, bge-m3 실측 미실행). §1.2의 벤치마크 정답이 이미 준비돼 있음.
5. **관측 가능성·freshness 유지.** footprint span, freshness/reindex는 좋은 토대.

### 3.2 CKV 개선 항목 (우선순위)

| 우선 | 개선 | 근거 |
|---|---|---|
| **P0** | 구조 보존 corpus 인제스트 (step/flow/invariant를 구조화 메타와 함께) | 최대 품질 레버 (§4) |
| **P0** | CKV↔CKG↔corpus 3-way 정렬 (`ckgalign` #4를 `symbol`/`file:line`로 확장) | LLM이 흐름+코드+호출관계를 한 번에 |
| **P1** | rerank stub 실구현 | hybrid 마지막 조각 |
| **P1** | 실 corpus 평가 실행 (bge-m3 야간 + corpus closure 정답셋) | 개선을 데이터로 |
| **P2** | invariant 청크를 16 INV로 보강, 자동추출은 보조로 강등 | ground truth 우선 |

---

## 4. Q2 답 — 흐름/시퀀스/인과 분석: 무엇을 학습하나

### 4.1 학습(인덱싱)할 3개 레이어

- **레이어 A — 큐레이션 흐름 corpus (최대 레버, 이미 존재).**
  `corpus.jsonl`을 구조 보존 적재. step → 신규 chunk kind, 임베딩 텍스트는
  `prose`+symbol+분기조건, 메타로 calls/branches/reads/writes/invariants 보존.
  invariant → 강제지점 역링크. flow → 상위 네비게이션.
- **레이어 B — CKG 정적 구조 (있음, 연결만).**
  calls/invokes 호출그래프 + change_history. corpus `symbol`을 CKG 노드에 정렬.
- **레이어 C — 실제 코드 (CKV가 이미 함).**
  step의 `file:line` 코드 청크. cross-link으로 "이 흐름 step의 실제 구현"에 도달.

### 4.2 이걸로 가능해지는 인과 분석 (예시)

```
현상: "import에서 일부 노드가 블록을 거부한다"
1. semantic/keyword_search → corpus step 'import-05' (block_validator.go:142, INV-STATE-01)
2. get_invariant_enforcement(INV-STATE-01)
   → statement: "parent state + same tx + same finalize → same root"
   → enforced_at: import-05 + producer 쪽 finalize-03
3. expand_flow(import-05)
   → branches: "IntermediateRoot != header.Root → ErrStateRootMismatch"
   → reads/writes: 어떤 상태를 읽는지
4. change_history(symbol)  [CKG]
   → 최근 finalize 경로에 닿은 PR
결론: "producer processFinalize와 import Finalize가 갈라졌다 → 최근 base fee 분배 변경 의심"
```

grep("ErrStateRootMismatch")는 *문자열 위치*만 주지만, 이 체인은 *왜 거기서 발생했는지*와
*무엇을 봐야 하는지*를 준다.

### 4.3 추가할 flow-aware primitive (CKV 범위 내, D3 위반 아님)

기존 `narrow_candidates`/`expand_in_file` 패턴의 확장:

1. **`get_flow(symptom | invariant | entry_point)`** — 관련 step을 **시퀀스 순서대로**
   (유사도 순 아님) 반환. `calls` 체인 따라감.
2. **`expand_flow(step_id, direction=upstream|downstream)`** — 앞/뒤 step + 사이 분기조건.
3. **`find_branches(symptom_text)`** — `branches`의 `when→then`을 증상→원인 쌍으로 검색.
4. **`get_invariant_enforcement(inv_id)`** — 불변식의 모든 강제지점(step+loc).

### 4.4 corpus 미커버 영역 (장기, CKG repo)

corpus는 4 spine + 14 entry-point의 "척추"만 덮음. 나머지 함수는 CKG에 함수 내
control-flow 엣지를 추가하면 시퀀스 추론 일부 가능. 단 비용 큼 → **우선순위는 corpus
인제스트가 압도적으로 높음** (검증된 고신호 vs 노이즈 있는 자동추출).

---

## 5. 권장 로드맵

| 단계 | 작업 | repo | 레버 |
|---|---|---|---|
| 1 | `corpus.jsonl` 구조 보존 인제스트 (신규 chunk kinds + 메타) | **CKV** | 🔥🔥🔥 |
| 2 | corpus step ↔ CKG node ↔ CKV chunk 3-way 정렬 (`symbol`/`file:line`) | CKV | 🔥🔥🔥 |
| 3 | flow-aware 도구 4종 (§4.3) | CKV | 🔥🔥 |
| 4 | go-stablenet 정답셋(corpus closure) + bge-m3 실측 평가 | CKV | 🔥🔥 |
| 5 | rerank stub 실구현 | CKV | 🔥 |
| 6 | (장기) CKG 함수 내 control-flow 엣지 | CKG | 🔥 |

---

## 6. 검증된 사실 인덱스 (이 문서의 근거)

- corpus 위치: `<go-stablenet>/.claude/docs/corpus/{corpus.jsonl, SCHEMA.md, README.md, flows/, invariants.md, tools/}`
- corpus.jsonl: 255 레코드 (flow 18 / step 79 / edge 142 / invariant 16)
- 적재 매핑 스펙: `corpus/SCHEMA.md §42-61` (vector/graph/benchmark)
- 미완 과제 명시: `corpus/SCHEMA.md §67`
- CKV 현 markdown 적재: PR #3 `--docs` → 평면 `doc` 청크 (구조 손실)
- CKV↔CKG 정렬 기반: `internal/ckgalign/aligner.go` (`file:line` 매칭, #4)
- CKG 능력/한계: 노드 37·엣지 43, 호출그래프·impact 有 / dataflow·control-flow·시퀀스 無
- corpus `symbol` 정규화 주의: `SCHEMA.md §66` (약식 `pkg.Func` 존재 → 조인 시 패키지 경로 정규화 필요, B7 정규화 이슈와 동일 선상)

---

## 7. 스코프 결정 (2026-06-16 확정)

상세 설계 진입 전 합의가 필요했던 결정들 — 모두 확정됨.

1. **인제스트 입력 형태** → **`corpus.jsonl` 직접 읽기**, "CKV 입력 계약"으로 일반화.
   `flows/*.md` 직접 파싱은 `build_corpus.py`의 best-effort 파서를 Go로 재구현(중복+drift)
   하므로 배제. `check_corpus.py`가 무결성 보장한 산출물만 CKV가 받음. `corpus.jsonl`을
   `graph.db`와 같은 부류의 out-of-tree 입력 아티팩트로 취급.
2. **chunk kind 신설 vs 재사용** → **2개 신설 + 기존 `invariant` 확장**.
   - `flow_step` (신설): `calls`/`branches`/`reads`/`writes`/`emits` 보유, 인과 최소 단위.
   - `flow_spine` (신설): entry-point/spine 메타(`root_symbol`/`links`/`called_by`).
   - `invariant` (확장): 큐레이션 INV는 같은 질의(`find_invariants`)에 답하므로 별도 kind
     대신 `provenance`(auto/curated) + `enforced_at` 필드 추가. curated를 신뢰도 우선 정렬.
   - 마이그레이션 1건(additive 컬럼) 추가.
3. **flow 도구 위치 (D3)** → **Phase 1 CKV 단독**. 단일 flow 내 조회·bounded 확장은 CKV
   primitive(`expand_in_file`과 동류). cross-flow(`calls_flow`) + CKV/CKG 교차 다중홉 인과
   체인은 CKS 오케스트레이션 (이번 스코프 밖, Phase 2).
4. **`symbol` 정규화 (B7)** → **Phase 1에선 불필요**. corpus step은 `file:line`(`at`)을
   가지므로 기존 `ckgalign`의 file:line 매칭으로 CKV 코드 청크와 정렬. CKG 조인이 필요한
   `symbol` 정규화는 Phase 2(B7, CKG 협업)로 연기 → B7 블로커 우회.
5. **freshness** → 기존 `EnforceCitationsAt`(B4) 재사용. flow step 서빙 시 인용 파일/라인
   존재·commit 일치 검사, drift 시 `StaleCitation` 플래그. corpus가 코드보다 뒤처지면 경고.
6. **범용성 vs 특화** → **일반화**. "문서화된 스키마를 따르는 임의 프로젝트의 flow corpus
   적재" = CKV 일반 기능. go-stablenet이 첫 producer. corpus 스키마 = CKV 입력 계약.

---

## 8. 빌드 오케스트레이션 (2026-06-16 확정)

### 8.1 문제

CKV DB 빌드의 입력은 여러 개이고 모두 머신·환경별로 변한다: go-stablenet src 경로,
포함 파일 allowlist(`--files-from`), flow corpus 경로, ckg 경로, 그리고 출력 위치(`--out`).
현재 이 값들은 Makefile env var(`GSN_SRC`/`GSN_OUT`)에 흩어져 있고, **기본값이 이전 머신
경로로 하드코딩돼 이미 drift**돼 있다(`GSN_SRC ?= /Users/wm-it-22-00661/...`).

corpus는 별도 파이프라인(go-stablenet의 `build_corpus.py`)이 생성하는 산출물이므로,
전체 생성은 다단계다:

```
0. (go-stablenet) build_corpus.py + check_corpus.py → corpus.jsonl   [corpus 생성]
1. (CKG, Phase 2)  ckg build                          → graph.db      [선택]
2. (CKV)           ckv build --src --files-from --flow-corpus --ckg --out  [통합 적재]
```

### 8.2 두 레이어 분리 (혼동 금지)

| 레이어 | 무엇 | 위치 | 성격 |
|--------|------|------|------|
| **프로젝트 설정** | 어떻게 파싱 (languages/build_roots/ignore/chunking) | `ckv.yaml`(`<src>/`, `internal/projectcfg`) | 프로젝트와 함께, 거의 불변 |
| **오케스트레이션 설정** | 어떤 프로젝트·입력 어디·출력 어디 | **신규 build-profile (머신 로컬)** | 머신별, 자주 변함 |

머신 경로를 `ckv.yaml`(프로젝트 repo에 커밋됨)에 넣지 않는다.

### 8.3 결정: 스크립트 + 선언적 설정 (바이너리 변경 없음)

CKV 바이너리는 이미 필요한 플래그(`--src/--out/--ckg/--files-from/--flow-corpus`)를 모두
가졌다. 오케스트레이션은 그 위의 얇은 레이어이므로 **스크립트 + 머신 로컬 설정 파일**로
처리한다 (바이너리에 `--profile` 내장은 멀티프로젝트가 많아지면 그때 승격).

```yaml
# build-profiles.yaml   (머신 로컬 — .gitignore; build-profiles.yaml.example만 커밋)
profiles:
  go-stablenet:
    src:          /Users/<me>/Work/github/go-stablenet
    files_from:   <src>/.claude/ckv-files.json          # 포함 파일 allowlist (선택)
    flow_corpus:  <src>/.claude/docs/corpus/corpus.jsonl
    regenerate_corpus: true     # true면 스크립트가 build_corpus.py + check_corpus.py 먼저 실행
    ckg:          /Users/<me>/.../ckg-stablenet          # Phase 2, 선택
    out:          /Users/<me>/.../ckv-stablenet          # 출력 위치도 설정값
    embedder:     ollama
    model_name:   bge-m3
```

```
scripts/build-knowledge.sh <profile>
  1) regenerate_corpus면 build_corpus.py + check_corpus.py 실행
  2) ckv build --src .. --files-from .. --flow-corpus .. --ckg .. --out .. --embedder .. --model-name ..
  3) (선택) gsn-smoke 검증
```

세 요구 충족: (a) 경로 설정화 (b) 스크립트가 DB 전부 생성 (c) out 설정 가능.
부수효과: Makefile의 stale-path 하드코딩 제거 (`.example`만 커밋, 실제 값은 gitignore).

---

→ 위 결정으로 `docs/plan-2026-06-16-flow-ingest.md`에서 상세 설계 진입.

---

## 8. 참조

- `docs/session-handoff-2026-06-15.md` — 현행 SoT
- `docs/plan-2026-05-29-ckv-refactor.md` — invariant/convention 추출기 (자동추출 레이어)
- `docs/evaluation-design-2026-05-22.md` — eval 방법 + BM25/rerank 결정
- `<go-stablenet>/.claude/docs/corpus/` — 본 제안의 입력 데이터 (1차 출처)
- code-knowledge-graph repo — CKG 호출그래프/impact (레이어 B)
