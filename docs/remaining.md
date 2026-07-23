# CKV 잔여 작업 (통합 목록 · 단일 SoT)

> **역할**: CKV의 *실행 가능한 잔여 작업*을 코드검증본으로 모은 단일 진입점.
> **작성**: 2026-07-11 (코드 대조: 브랜치 `docs/retire-ckg-node-id`, HEAD `d546d95`)
> **갱신**: 2026-07-12 (PR #17~#47 반영, main HEAD `f595d83`) — **CKV 소관 작업 실질 완료**, 잔여는 아래 "현재 상태"의 블록 4건.
> **재검증**: 2026-07-13 (main HEAD `adf4417`, PR #48까지) — 완료 항목([x]) 코드 실재 재확인
> (`RealignCanonical`/`acquireDatasetLock`/`FilesResumed`/`PromoteVersion`/`ResolvedVersion`/`FetchMergedPRs`/
> `DeleteDocsChunks`/`DeleteFlowChunks`/`AlignedChunkID`/`EmbedQuery`/`lastTokenPoolNormalize`/`FetchModel` 전부 존재,
> `go build ./...` ok). PR #48은 `remaining.md` 동기화만(신규 코드 0) → **잔여 4블록 불변**.
> **정리**: 2026-07-20 — 완료(✅/[x]) 섹션·항목을 전량 삭제하고 실제 잔여 작업만 유지. 완료 근거는 git log/PR 이력이 SoT.
> **관계**:
> - 배경·협의·결정 근거 → [`session-handoff-2026-06-29.md §4`](./session-handoff-2026-06-29.md) (서사 SoT)
> - reindex 설계 → [`reindex-migration-design-2026-07-10.md`](./reindex-migration-design-2026-07-10.md)
> - 브랜치 주제 체크리스트 → [`retire-ckg-node-id.md`](./archive/retire-ckg-node-id.md)
> - 4세션 협의 원문 → [`coordination-prompts-2026-06-29.md`](./coordination-prompts-2026-06-29.md)
> - 2026-05 세대 인벤토리(대부분 종결) → [`backlog.md`](./archive/backlog.md) · [`pending-work-2026-05-21.md`](./archive/pending-work-2026-05-21.md) (**stale**)
>
> 완료된 항목은 체크 표시로 남기지 않고 이 문서에서 삭제한다 — 완료 근거는 커밋/PR 이력을 참조.

---

## 현재 상태 (2026-07-13 재검증) — CKV 소관 실질 완료

PR #17~#48까지 CKV 소관 작업이 실질적으로 종결됐다(2026-07-13 코드 재대조 완료). 남은 항목은 전부 **이 머신에서 착수 불가**(부재 데이터·throughput 환경·CKS 소관)다.

**블록 잔여 (착수 조건 명시):**

| 항목 | 블로커 | unblock 조건 |
|---|---|---|
| flow **F** — `get_flow`/`find_branches` precision/recall 정본 평가 | 대형 flow corpus 부재 | go-stablenet 정본 corpus(255레코드) + 정답셋을 이 머신에 확보 |
| **PRR-1** / throughput | 대형 코퍼스 부재(0.74 c/s 재현 불가; 임베딩은 이미 배치) | go-stablenet clone + PR base_sha reachable + (선택) CoreML EP용 libonnxruntime |
| **Phase A** — sliding split 실측 | 긴 함수 코퍼스 부재(testdata/sample 함수가 짧아 split 미발동) | 큰 함수 비율이 높은 실 코퍼스 |
| **CKS 소관** — 재기동 수신 · P5 오케스트레이션 · flow 도구 표면 *소비* | CKS 세션 의존 | CKS 재기동 + 양측 digest 교차확인(`coordination-prompts §10.10`) |

**설계 open 결정 (코드 작업 아님 · 2026-07-19 편입, `reindex-migration-design-2026-07-10.md §9`):**

- **도메인지식(flow/docs) 커밋별 버전관리** — 현재 flow/docs는 단일 스냅샷이라 시점(point-in-time) PR 평가에서 leakage 소지. 커밋축 버전관리 도입 여부 미결. 근거: `reindex-migration-design-2026-07-10.md §9`, `pr-retrieval-eval-2026-07-08.md`.
- **증분 in-place vs 항상 swap-only** — 증분을 서빙본에 직접 반영 vs 항상 재빌드-스왑의 비용/단순성 트레이드오프. blue-green 전면 스왑은 P5(CKS 소관)이나 트레이드오프 자체는 미결로 기록. 근거: `reindex-migration-design-2026-07-10.md §4.2/§9`.

**다음 세션 진입점**: 위 데이터셋 중 하나가 확보되면 해당 행이 즉시 언블록된다. 그 전까지 CKV 코드 측 신규 작업은 없다.

---

## 1. 외부·협의 대기 (§7-P5 무중단 서빙)

- [ ] **CKS 재기동 결과 수신** — `pr-77-2/ckv` reload 후 `cks.ops.health` alignment 블록 공유받아 양측(CKV `CheckAlignment` / CKS assert) 동일 digest(`4be26516…`) ok 교차확인. 프롬프트: `coordination-prompts §10.10`.
- [~] **P5 blue-green 무중단 서빙** (§4/§6) — CKV-side 슬라이스 완료(`ckv promote` 원자 스왑 + health 버전 보고), 오케스트레이션은 CKS.
  - [ ] **CKS 소관**: 버전 네이밍/디렉터리 레이아웃, 언제 promote할지, 인스턴스-레벨 blue-green 전환, 보존/GC, 조율 재인덱싱 오케스트레이션.

## 2. backlog 잔여 (2026-05 세대 중 미종결)

- [ ] **PRR-1** full PR regression — throughput 보류(현 0.74 c/s).
- [~] **flow Phase C→F** — Phase C~E 완료(정렬·drift 감지·도구 4종 MCP 노출·빌드 경로 외부화). **잔여 F**: flow-queries 정답셋 평가(`get_flow`/`find_branches` precision/recall) — 대형 corpus(255레코드) 의존이라 정본 데이터셋 확보 후.

## 3. 종결·정정 확인

- `backlog.md` B1~B9·E·F(CKV-1~7)·G(PRR-2~5)·C10, handoff §4.A2 등은 종결(✅). 상세는 각 문서 변경 이력.
- `backlog.md`·`pending-work-2026-05-21.md`는 본 문서로 supersede(내용 삭제 아님, live 잔여는 여기서 추적).
- handoff §5 문서 드리프트 정정(ADR-006 Rejected, mcp-tools 플래그 보강, coreml Makefile 제외)은 미해결 상태로 잔존 시 여기 편입.
