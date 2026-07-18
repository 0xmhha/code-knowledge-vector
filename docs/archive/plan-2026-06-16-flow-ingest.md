# CKV Flow-Corpus 적재 + 빌드 오케스트레이션 — 구현 계획서

> **ARCHIVED 2026-07-19.** Plan executed; decisions live in the ADRs (`docs/adr/`) and live status in [`remaining.md`](../remaining.md). Kept for provenance.

문서 버전: 1.0
작성일: 2026-06-16
대상 코드베이스: `/Users/wm-it-25_0220/Work/github/code-knowledge-vector`
대상 입력 데이터: `<go-stablenet>/.claude/docs/corpus/corpus.jsonl`
선행 문서: [`flow-knowledge-design-2026-06-16.md`](./flow-knowledge-design-2026-06-16.md) (방향·스코프 결정)

> **목표:** go-stablenet의 큐레이션된 flow corpus(`corpus.jsonl`, 255 레코드)를 CKV에
> 구조 보존 적재하고, flow-aware 검색 primitive 4종을 노출하여, LLM이 "현상 → 원인"
> 인과 분석을 할 수 있게 한다. 더불어 다중 입력·출력 경로를 설정화한 빌드 오케스트레이션을
> 도입한다. **Phase 1 = CKV 단독** (CKG 조인·CKS 오케스트레이션은 Phase 2, 본 계획 밖).

---

## 0. 진행 순서 (확정)

크리티컬 패스: **A(타입/스키마) → B(파서/적재) → C(정렬) → D(도구)**. E(오케스트레이션)는
독립이라 병렬 가능. 스키마부터 안정화(plan-2026-05-29의 Schema-First 교훈 계승).

```
A. 타입 + 스키마 + 마이그레이션   (안전망 먼저)
B. corpus.jsonl 파서 + 적재 파이프라인
C. file:line 정렬 (corpus step ↔ CKV 코드 청크)
D. flow-aware MCP 도구 4종
E. 빌드 오케스트레이션 (스크립트 + 설정)   ← A~D와 병렬
F. 평가 + 문서
```

---

## 1. Phase A — 타입·스키마·마이그레이션

### A1. chunk kind + 메타데이터 타입

**Task:** 신규 chunk kind 2종 + 기존 invariant 확장.

**영향 파일:**
- 수정 `pkg/types/chunk.go`:
  - `ChunkFlowStep ChunkKind = "flow_step"`
  - `ChunkFlowSpine ChunkKind = "flow_spine"`
  - 신규 타입 `FlowStepMeta{FlowID, StepID, Symbol, Kind, Calls []string, Reads, Writes, Emits string, Branches []Branch, Invariants []string}`
  - 신규 타입 `Branch{When, Then, At string}`
  - 신규 타입 `FlowSpineMeta{FlowID, EntryPoint, Trigger, RootSymbol, Links []string, CalledBy []string}`
  - 기존 invariant 경로 확장: `Provenance string` (auto|curated) + `EnforcedAt []EnforcePoint{Flow, Step, Loc}`
- 수정 `pkg/types/chunk_test.go`: 직렬화 round-trip 테스트

**DoD:** 새 타입 JSON round-trip; 기존 청크 직렬화 무회귀(omitempty).

**리스크:** LOW (additive).

### A2. SQLite 마이그레이션

**Task:** 신규 메타 컬럼 1개 마이그레이션 (`004_add_flow_meta.sql`).

**영향 파일:**
- 신규 `internal/store/sqlitevec/migrations/004_add_flow_meta.sql`:
  `ALTER TABLE chunks ADD COLUMN flow_meta TEXT; ADD COLUMN enforced_at TEXT; ADD COLUMN provenance TEXT;`
- 마이그레이션 러너 **코드 변경 불필요 (검증 완료)**: `migrate.go`가 `//go:embed migrations/*.sql`로
  신규 파일을 자동 컴파일하고 `NNN_description.sql` 정규식으로 검증함. `004_*.sql` drop만으로 동작.

**DoD:** `ckv migrate --status` current=004; idempotent; 자동 backup 동작; 기존 003 인덱스에서 무손실 업그레이드.

**리스크:** LOW (기존 마이그레이션 프레임워크 C2 재사용, 러너 자동 픽업 검증 완료).

---

## 2. Phase B — corpus.jsonl 파서 + 적재

### B1. corpus 파서 패키지

**Task:** `corpus.jsonl`을 읽어 레코드별 청크로 변환. 스키마는 `corpus/SCHEMA.md` 계약.

**영향 파일:**
- 신규 `internal/flowcorpus/parser.go` (~200 lines): JSONL 라인별 디코드, `type` 분기
  (flow/step/invariant/edge). edge는 step.calls로 이미 표현되므로 검증용으로만 사용(중복).
- 신규 `internal/flowcorpus/parser_test.go`: testdata에 축소 corpus.jsonl fixture
- 신규 `internal/flowcorpus/testdata/mini-corpus.jsonl` (~10 레코드: 1 flow + 3 step + 1 invariant + edge)
- 신규 타입 → 청크 변환:
  - `step` → `ChunkFlowStep`: 임베딩 텍스트 = `prose` + " " + `symbol` + " " + branches[].when 조인
    (실패조건도 semantic_search에 걸리도록). 메타 = FlowStepMeta. Citation = {file, line→start_line=end_line=line}.
  - `flow` → `ChunkFlowSpine`: 임베딩 텍스트 = `summary`. 메타 = FlowSpineMeta.
  - `invariant` → invariant 청크(provenance=curated): 임베딩 텍스트 = statement+assumes+check.
    메타 = EnforcedAt.

**DoD:** mini-corpus → 정확한 청크 수·종류; 형식 이탈 레코드는 warn+skip (build_corpus.py 정책 계승); branches[].when이 임베딩 텍스트에 포함.

**리스크:** MEDIUM
- 위험: `symbol`이 약식(`pkg.Func`)이라 정렬 실패 (SCHEMA §66 경고)
- 완화: Phase 1은 `file:line`으로 정렬하므로 symbol 약식 무관. symbol은 메타로 보존만.

### B2. build 파이프라인 통합 + `--flow-corpus` 플래그

**Task:** `ckv build`에 corpus 적재 단계 추가.

**영향 파일:**
- 수정 `cmd/ckv/build.go`: `f.StringVar(&opts.flowCorpus, "flow-corpus", "", "path to flow corpus JSONL (schema: <go-stablenet>/.claude/docs/corpus/SCHEMA.md)")`
- 수정 `internal/build/builder.go` (또는 pipeline.go): flowCorpus 지정 시 `flowcorpus.Load` → 청크 임베딩·upsert. `--docs`(PR #3) 적재 직후 동일 패턴.
- 수정 `internal/build` 진행 표시: flow corpus 단계 로그

**DoD:** `ckv build --src X --flow-corpus mini-corpus.jsonl --out Y` → vector.db에 flow_step/flow_spine/curated-invariant 청크 존재; `--flow-corpus` 미지정 시 무변경(무회귀).

**리스크:** LOW (`--docs` 적재 경로 재사용).

---

## 3. Phase C — file:line 정렬

### C1. corpus step ↔ CKV 코드 청크 정렬

**Task:** 각 flow_step의 `file:line`을 같은 인덱스의 실제 코드 청크에 연결 → LLM이 "흐름
step → 실제 구현 코드"로 이동 가능.

**영향 파일:**
- 신규 또는 수정 `internal/flowcorpus/align.go`: `ckgalign`(#4)의 file:line 매칭 로직 재사용
  또는 동등 구현. step.line이 어느 코드 청크의 [start,end]에 들어가는지 해소.
- 수정 청크 메타: flow_step에 `aligned_chunk_id` (해당 코드 청크 ID, omitempty)

**DoD:** go-stablenet 실 corpus에서 step의 file:line이 코드 청크로 해소되는 비율 측정·로그.
미해소(코드가 drift)는 경고. **목표 ≥80% 해소** (corpus가 코드와 동기화돼 있을 때).

**리스크:** MEDIUM
- 위험: corpus의 line이 현재 코드와 drift (corpus는 사람이 갱신)
- 완화: Phase A5 freshness(아래) 와 연동 — 미해소 step은 stale 플래그.

### C2. freshness / stale 감지

**Task:** flow_step 서빙 시 인용 file:line 유효성 검사 (B4 `EnforceCitationsAt` 재사용).

**영향 파일:**
- 수정 `internal/query/` citation 검사 경로: flow_step 청크도 EnforceCitationsAt 대상에 포함.
  파일 없음/라인 범위 벗어남 → `StaleCitation=true` + warning.

**DoD:** corpus가 코드보다 뒤처진 fixture에서 stale 플래그 정상 발생.

**리스크:** LOW.

---

## 4. Phase D — flow-aware MCP 도구 4종

모두 CKV, bounded 조회 (D3: 단일 flow 내 / 단일 lookup).

### D1. `get_flow`

**입력:** `flow_id` | `entry_point` | `invariant` (셋 중 하나)
**출력:** 해당 flow의 step들을 **시퀀스 순서**(calls 체인 topological)로. 각 step의 symbol/citation/branches/invariants 포함.
**영향:** `pkg/mcp/server.go` 핸들러 + `internal/query/engine.go` `GetFlow(...)`.

### D2. `expand_flow`

**입력:** `step_id`, `direction` (upstream|downstream), `hops` (default 1)
**출력:** 인접 step + 사이 분기조건.
**영향:** 동상.

### D3. `find_branches`

**입력:** `symptom_text` (string)
**출력:** `branches`의 `when→then@at`을 증상→원인 쌍으로 검색 (BM25 over branch.when 텍스트).
**영향:** 동상. branch.when 인덱스 필요 (B1에서 임베딩 텍스트에 포함했으므로 keyword_search 재사용 가능).

### D4. `get_invariant_enforcement`

**입력:** `inv_id`
**출력:** 그 불변식의 모든 강제지점 (enforced_at: step + loc).
**영향:** 동상.

**Phase D 공통 DoD:**
- 4 도구 healthcheck 통과; mini-corpus에서 정확한 시퀀스·분기·강제지점 반환
- §4.2 예시 인과 체인이 도구 호출만으로 재현 (수동 e2e 1건)
- schema_version 호환 (additive)

**리스크:** MEDIUM (D1 시퀀스 정렬 = calls 그래프의 topological sort; cycle 처리 필요).

---

## 5. Phase E — 빌드 오케스트레이션 (A~D와 병렬)

### E1. build-profile 설정 + 스크립트

**Task:** 다중 입력·출력 경로를 머신 로컬 설정으로 외부화, 단일 스크립트로 전체 DB 생성.

**영향 파일:**
- 신규 `build-profiles.yaml.example` (커밋) — §8.3 설계 문서의 프로필 스키마
- 수정 `.gitignore`: `build-profiles.yaml` (실제 값, 머신 로컬)
- 신규 `scripts/build-knowledge.sh <profile>`:
  1. `regenerate_corpus`면 `python3 <src>/.claude/docs/corpus/tools/build_corpus.py` + `check_corpus.py`
  2. `ckv build --src .. --files-from .. --flow-corpus .. --ckg .. --out .. --embedder .. --model-name ..`
  3. (선택) 스모크 검증
- 수정 `Makefile`: `GSN_SRC`/`GSN_OUT` 하드코딩 기본값 제거 → `build-profiles.yaml` 또는 명시 인자 요구

**DoD:** `scripts/build-knowledge.sh go-stablenet`로 corpus 생성 + ckv 적재가 한 번에;
경로 변경 시 프로필만 수정; Makefile에 머신 경로 잔존 0.

**리스크:** LOW. 단 YAML 파싱(shell)은 `yq` 의존 또는 간단 파서 — 의존 추가 여부 결정.

---

## 6. Phase F — 평가 + 문서

### F1. go-stablenet 정답셋 평가

**Task:** corpus의 `calls`·`enforces` 폐포를 ground truth로 한 retrieval 평가 (SCHEMA §58-61).

**영향 파일:**
- 신규 `testdata/flow-queries.yaml`: "X 작업의 코드경로/영향" 질의 + 기대 step 집합 (corpus closure)
- 음성 케이스: `branches` 실패조건을 도구가 찾는지
- 기존 `ckv eval` 확장 또는 별도 harness

**DoD:** get_flow/find_branches가 corpus closure 대비 precision/recall 측정; 베이스라인 기록.

### F2. 문서

- 수정 `docs/mcp-tools.md`: 신규 4 도구 + `--flow-corpus` 플래그
- 수정 `docs/SCHEMA.md`: flow_step/flow_spine chunk + flow_meta 컬럼
- 신규 ADR: "flow corpus를 1급 입력으로 적재" 결정 기록

---

## 7. 측정·검증 체크포인트

**Phase A 완료 (2026-06-29, commit `7158572`):**
- [x] 타입(flow_step/flow_spine kind, FlowStepMeta/FlowSpineMeta/Branch/EnforcePoint,
  Chunk 필드) + JSON round-trip 테스트
- [x] 마이그레이션 004 idempotent + 자동 backup — go-stablenet@`0bf2f4d1b`
  (test/analysis-test-3) 신규 빌드(000–004, 19,605청크) + pr-77 003→004 업그레이드
  (백업·15,575행 보존·재실행 no-op) 양방향 실데이터 검증
- [x] store 무변경으로 기존 청크 회귀 0 (명시적 컬럼 리스트 확인)

**Phase B 완료 (2026-06-29, commits `72ef76f` 파서 / `db6789a` 빌드통합):**
- [x] go-stablenet 실 corpus.jsonl(255) 적재 → flow_spine 18 / flow_step 78 / curated-invariant 16
  (step `disc-03` line 누락 → warn+skip, edge 142 graph-only skip). store flow_meta/enforced_at/
  provenance 영속 + round-trip. corpus 입력 = `go-stablenet/.claude.backup.20260625_180533/docs/corpus/`
- [x] 기존 MCP/검색 회귀 0 (build/store/query 테스트 통과)
- [ ] (후속) bge-m3 실모델로 사람-워딩 질의 → flow_step 회수 의미검증

**Phase C 완료 시:**
- [ ] step file:line → 코드 청크 해소율 ≥80% (동기화 상태)
- [ ] drift step stale 플래그 동작

**Phase D 완료 시:**
- [ ] 4 도구 정상; §4.2 인과 체인 e2e 1건 재현
- [ ] semantic_search가 flow_step 반환 확인

**Phase E 완료 시:**
- [ ] 단일 스크립트로 corpus 생성 + 적재; 경로 전부 설정화; Makefile stale 경로 0

**사용자 시나리오 (전체):**
- "import에서 블록 거부" 현상 → get_flow/get_invariant_enforcement/expand_flow 체인으로
  원인 후보(producer/import finalize 분기 + 최근 변경)를 5분 내 도출 (수동 평가 3건)

---

## 8. 위험 요약

| Task | Risk | 완화 |
|------|------|------|
| A2 마이그레이션 러너 신규파일 픽업 | — | go:embed 자동 픽업 검증 완료 |
| B1 symbol 약식 정렬 실패 | MEDIUM | Phase 1은 file:line 정렬 → symbol 무관 |
| C1 corpus↔코드 drift | MEDIUM | freshness stale 플래그 연동 |
| D1 calls 그래프 cycle | MEDIUM | topological sort + cycle 방어 |
| E1 shell YAML 파싱 의존 | LOW | yq 의존 vs 간단 파서 결정 |

---

## 9. Phase 2 (본 계획 밖, 참조용)

- corpus step ↔ CKG node `symbol` 조인 (B7 정규화 선행)
- cross-flow(`calls_flow`) 추적 + CKV/CKG 교차 다중홉 인과 체인 → CKS 오케스트레이션
- CKG 함수 내 control-flow 엣지 (corpus 미커버 영역)

---

## 10. 참조

- [`flow-knowledge-design-2026-06-16.md`](./flow-knowledge-design-2026-06-16.md) — 방향·스코프 결정
- `<go-stablenet>/.claude/docs/corpus/SCHEMA.md` — corpus 스키마 (입력 계약)
- [`plan-2026-05-29-ckv-refactor.md`](./plan-2026-05-29-ckv-refactor.md) — invariant/convention/마이그레이션 프레임워크 (재사용)
- `internal/ckgalign/aligner.go` — file:line 정렬 (#4, 재사용)
- [`session-handoff-2026-06-29.md`](./session-handoff-2026-06-29.md) — 현행 SoT
