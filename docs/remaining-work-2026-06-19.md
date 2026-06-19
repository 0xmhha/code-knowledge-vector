# 남은 작업 리스트 (2026-06-19)

문서 성격: **작업 추적 (status)**. 현재 브랜치 `feat/ckv-invariants-pkg`에서의 진행
지점과 남은 작업을 정리한 continuation 문서. 다음 세션은 이 문서로 이어받는다.

> **상위 목표:** vector/graph 기반 knowledge 시스템(ckv/ckg/cks)으로 `coding-agent`
> 플러그인을 더 smart하게 — 즉 **첫 수정을 옳게(옳은 진단→옳은 수정)** 하여 bug-cycle을
> 줄이는 것. "smart" = 옳은 수정까지의 총비용↓ (bench handoff §2의 정의).
>
> **레버:** cks가 coding-agent에 (a) 완전한 정보(parity) + (b) 정확한 진단 지식(flow
> corpus)을 전달하게 만든다. 현재 둘 다 끊겨 있다.

근거 문서:
- 시스템 통합 분석: `coding-agent/docs/knowledge-system-analysis-2026-06-17.md`
- flow corpus 설계/계획: `docs/flow-knowledge-design-2026-06-16.md`, `docs/plan-2026-06-16-flow-ingest.md`

---

## 0. 환경·접근 (다른 머신에서 시작하기 — 먼저 읽기)

**저장소 배치:** 아래 5개는 **형제 디렉토리**다 (한 부모 폴더 아래 나란히). 경로는 머신마다
다르므로 절대경로를 박지 말 것 — 원 작성 머신 예시는 `~/Work/github/<repo>` (이전 핸드오프가
구 머신 경로 하드코딩으로 깨진 전례 있음).

| repo | 역할 | 이 작업에서 |
|------|------|------------|
| `code-knowledge-vector` (ckv) | 벡터/시맨틱 검색 | **이 브랜치가 있는 곳.** CKV-side parity 완료 |
| `code-knowledge-system` (cks) | MCP 오케스트레이터 (ckv/ckg를 in-process import) | **다음 작업(Stream A CKS-side)이 일어나는 곳** |
| `code-knowledge-graph` (ckg) | 코드 그래프 | 참조 (cks가 import) |
| `coding-agent` | Claude Code 플러그인 (cks MCP 소비자) | analyzer/doctor는 여기 (Stream C/D) |
| `go-stablenet` | 대상 코드 + `.claude/docs/corpus/` (flow corpus 원본) | Stream B 입력 데이터 |

**이 작업이 담긴 위치:** ckv repo, 브랜치 **`feat/ckv-invariants-pkg`**.
→ **다른 머신에서 이어받으려면 이 브랜치가 origin에 push돼 있어야 한다.** push 안 됐으면
원 머신에서 먼저: `git push -u origin feat/ckv-invariants-pkg`.
커밋: `5fc5c28`(CKV facade) · `544f74d`(이 문서) — 둘 다 이 브랜치에만 있음(main 아님).

**필수 선행 읽기 (이 문서만으로 불충분 — 반드시 함께 읽을 것):**
- `coding-agent/docs/knowledge-system-analysis-2026-06-17.md` (커밋 `91f0baa`, **main**) —
  parity 갭의 검증된 사실: cks `ckvclient.Client`는 4메서드(SemanticSearch/Health/Freshness/
  Close)뿐 → CKV의 15 MCP 도구 중 cks 경유는 `semantic_search` 1개만; cks 기동 시
  **CKG 누락=fatal / CKV 누락=degraded(빈 결과로 계속)**; CKG는 10 도구 중 9 도달.
- `docs/flow-knowledge-design-2026-06-16.md` + `docs/plan-2026-06-16-flow-ingest.md`
  (이 repo, 이 브랜치) — Stream B 상세 (§4-bis가 CKS-side 노출).

**빌드/테스트 (ckv):** gvm 환경이라 login 셸 필수 — `zsh -lic 'make build && go test ./...'`.
`internal/embed/coreml`는 libtokenizers 부재로 로컬 test 1건 실패(환경 baseline; CI가
명시적으로 제외) — 회귀 아님, 정상.

---

## 1. 이 브랜치에서 완료된 것 (`feat/ckv-invariants-pkg`)

커밋 `5fc5c28 feat(ckv): expose FindInvariants/GetConventions via pkg/ckv`:

- `pkg/ckv/ckv.go`: `Engine.FindInvariants` / `GetConventions` facade + `InvariantHit` /
  `ConventionHit` 타입 alias. (내부 `query.Engine`엔 이미 구현돼 있던 것을 public API로 노출 —
  기존 SemanticSearch/Freshness facade 패턴 그대로.)
- `pkg/ckv/invariants_test.go`: 3 테스트 (call path + tier 검증 + close 후 에러). 통과.
- `docs/plan-2026-06-16-flow-ingest.md`: §4-bis "Phase D′ — CKS-side 노출" 추가
  (parity 발견 반영).

**의미:** 이전엔 cks가 ckv에서 `SemanticSearch`만 in-process 호출 가능했다. 이제 cks가
ckv의 invariant/convention 인덱스도 호출할 접점이 생겼다 — **Stream A(parity)의 CKV-side
선결조건 완료.** 단 cks-side 배선이 남아 실제 coding-agent는 아직 못 쓴다.

---

## 2. 작업 스트림 & 우선순위 (전체 지도)

목표=개선(improve) 기준. 의존성 순서.

| 순위 | 스트림 | 상태 | repo | 의존 |
|------|--------|------|------|------|
| **1** | **A. parity** (ckv 진단 도구를 cks로 노출) | 🔄 CKV-side 완료, CKS-side 잔여 | ckv→cks | — |
| 2 | **B. flow corpus** (corpus.jsonl 적재 + 4 도구 + cks 노출) | ⏳ | ckv→cks | A 패턴 |
| 3 | **C. analyzer** (`/diagnose`를 정책 기반 + flow 인식으로) | ⏳ | coding-agent | A, B |
| 4 | **D. 운영 신뢰성** (doctor / cks 선택 / 셋업 갭) | ⏳ 직교, 병행 가능 | coding-agent | — |
| 5 | **E. bench harness** (bug-cycle 루프 + 총비용 metric) | ⏳ defer (계측기) | coding-agent | A |
| 6 | **F. 확장성** (언어 추가 / TS 품질) | ⏳ defer | ckv/ckg | — |

---

## 3. Stream A 잔여 — CKS-side 배선 (다음 즉시 작업)

repo: `code-knowledge-system`. 스코프 확정됨. (CKV-side는 §1로 완료.)

진단에 쓰이는 도구만 **선택 노출** (14개 전부 아님 — embed/vector_search/rerank/warmup/index
같은 저수준은 analyzer가 안 씀): 1차 = `find_invariants`, `get_conventions`.

**4곳 수정:**
1. `internal/ckvclient/interface.go`: `Client`에 `FindInvariants(ctx, file, category string, tierMin int)` /
   `GetConventions(ctx, packagePrefix string)` 추가 + 신규 contract 타입
   `contract.InvariantHit` / `contract.ConventionHit` 정의. **번역 템플릿: `pkg/contract/hit.go`의
   `contract.Hit`** (ckv 타입을 cks 경계에서 값 복사 — 기존 패턴 동일).
2. `internal/ckvclient/real.go`: `r.eng.FindInvariants(...)` / `GetConventions(...)` 호출 +
   ckv 타입 → contract 타입 번역. **미러 대상: `real.go:98` `SemanticSearch`** — `:119` 결과
   루프 → `:127` `contract.Hit{...}` 채움 → `:136` `Source: contract.HitSourceCKV`. 동일 구조로
   InvariantHit/ConventionHit 번역 작성. (`r.eng`는 `*ckv.Engine` — §1에서 추가한 facade 호출.)
3. `internal/ckvclient/dummy.go` + `fake.go`: degraded(빈 슬라이스 + nil err) + 테스트 fake 구현.
   (`Client` 인터페이스를 구현하는 모든 타입에 메서드 추가 — 안 하면 컴파일 실패로 즉시 드러남.)
4. `internal/mcp/`: `cks.context.find_invariants` / `cks.context.get_conventions` 핸들러 등록.
   **미러 대상: `internal/mcp/analysis.go:36` `registerImpactAnalysis`** (tool 정의 → `s.AddTool`
   핸들러; 핸들러는 `d.CKV`로 ckv 접근 — `Deps`는 `server.go:46`, `CKV ckvclient.Client` 필드).
   `concurrency.go:23`도 동형.
   - **🔴 필수 잊지 말 것:** 새 `registerFindInvariants`/`registerGetConventions`를
     **`internal/mcp/server.go:97~109`의 등록 목록에 추가**해야 도구가 뜬다 (현재
     `registerSemanticSearch(s, d)`가 :106). 정의만 하고 목록에 안 넣으면 **조용히 미노출**.

**번역 대상 — ckv 원본 타입 (필드 그대로 contract로):**
- `InvariantHit` (`ckv/internal/query/engine.go:395`, `pkg/ckv`에서 alias 노출):
  `ChunkID, File string` · `StartLine, EndLine int` · `Marker string`(예 "CRITICAL"/"panic") ·
  `Tier types.InvariantTier`(1/2/3) · `Text string` · `Category string` ·
  `Guidance *types.ModificationGuidance` · `SourceChunk string`.
- `ConventionHit` (`engine.go:453`): `ChunkID, File, Package, Summary string` · `Stats map[string]any`.
- cks contract에선 `Tier`를 int로, `Guidance`는 평탄화(또는 `contract`에 동등 타입) — strict
  의존 피하려면 ckv `pkg/types`를 cks contract가 import하지 말고 값 복사.

**DoD:**
- cks MCP에서 두 도구 호출 가능; degraded 모드에서 빈 결과 + 에러 없음.
- coding-agent Planner(mode=diagnose)가 cks 경유로 find_invariants 호출 e2e 1건.
- 기존 cks 도구 회귀 0 (`go build` + `go test ./...`).

**의사결정 (확인 필요):** contract 타입 신설(option A) — ckv `InvariantHit`/`ConventionHit`를
cks `contract.*`로 번역. 기존 방식과 일치하므로 기본 채택 예정.

---

## 4. Stream B 잔여 — flow corpus (A 패턴 재사용)

`docs/plan-2026-06-16-flow-ingest.md` 전체가 상세 계획. 요약:
- **CKV-side (Phase A~D):** flow_step/flow_spine chunk kind + 마이그레이션 004 +
  corpus.jsonl 파서(`internal/flowcorpus`) + `--flow-corpus` 플래그 + file:line 정렬 +
  4 도구(get_flow / expand_flow / find_branches / get_invariant_enforcement).
- **CKS-side (Phase D′):** 위 4 도구를 Stream A와 동일 배선으로 cks 노출. **필수**
  (안 하면 coding-agent가 못 씀).
- **Phase E (병행):** build-profile 설정 + `scripts/build-knowledge.sh` (다중 입력·출력
  경로 외부화, Makefile stale 경로 제거).

---

## 5. Stream C~F 잔여 (요약)

- **C. analyzer:** `coding-agent`의 `/diagnose` + Planner `mode=diagnose` +
  `root-cause-lifecycle` 스킬을 **정책 기반**(상황별 specific 정책) + **flow 인식**(B의 도구
  사용)으로 확장. A·B 의존.
- **D. 운영 신뢰성:** doctor(실제 MCP 연결성 검사 — setup은 env만 봄), cks 인스턴스
  선택/전환(현재 세션당 고정), 셋업 갭(패턴 오버라이드 / Jira 필드 매핑 / 멀티repo init).
  doctor가 작고 즉시 유용 → 병행 가능.
- **E. bench harness:** `coding-agent`의 bench에 bug-cycle 재진입 루프 + compare.py 총비용
  metric + 태스크 다양화 (HANDOFF-bench-harness.md). A 이후라야 "진짜 cks" 측정. 무겁고
  go-stablenet 변경 + 데이터 오염 해저드 주의.
- **F. 확장성:** 언어 추가가 컴파일타임 하드코딩(플러그인 없음); TS 파서 품질이 Go/Sol에
  뒤짐. 도메인/정책은 이미 config-driven. go-stablenet 안정 후.

---

## 6. 다음 세션 진입 순서

1. **§0(환경·접근) 먼저** — 브랜치 push 상태 확인, 5개 형제 repo 위치 파악, 필수 선행 문서
   (coding-agent analysis) 읽기.
2. **Stream A의 CKS-side(§3)부터** — `code-knowledge-system` repo. contract 타입 신설 →
   real(`real.go:98` 미러) → dummy/fake → MCP 등록(`analysis.go:36` 미러) →
   cks `go build && go test ./...`.
3. e2e 확인(coding-agent Planner mode=diagnose → cks find_invariants) 후 Stream B(flow corpus) 착수.
4. doctor(D)는 여유 시 병행.
