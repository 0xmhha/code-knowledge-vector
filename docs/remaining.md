# CKV 잔여 작업 (통합 목록 · 단일 SoT)

> **역할**: CKV의 *실행 가능한 잔여 작업*을 코드검증본으로 모은 단일 진입점.
> **작성**: 2026-07-11 (코드 대조: 브랜치 `docs/retire-ckg-node-id`, HEAD `d546d95`)
> **관계**:
> - 배경·협의·결정 근거 → [`session-handoff-2026-06-29.md §4`](./session-handoff-2026-06-29.md) (서사 SoT)
> - reindex 설계 → [`reindex-migration-design-2026-07-10.md`](./reindex-migration-design-2026-07-10.md)
> - 브랜치 주제 체크리스트 → [`retire-ckg-node-id.md`](./retire-ckg-node-id.md)
> - 4세션 협의 원문 → [`coordination-prompts-2026-06-29.md`](./coordination-prompts-2026-06-29.md)
> - 2026-05 세대 인벤토리(대부분 종결) → [`backlog.md`](./backlog.md) · [`pending-work-2026-05-21.md`](./pending-work-2026-05-21.md) (**stale**)
>
> 각 항목의 `근거`는 2026-07-11 `grep`/코드 확인 결과다. 완료 시 해당 줄을 `[x]`로 바꾸고 커밋 해시를 병기한다.

---

## 0. 추천 실행 순서 (reindex-design §7 + roadmap §12 통합)

섹션은 주제별이고, 실제 착수 순서는 아래 교차 우선순위를 따른다.

1. ✅ **`ckg_node_id` 은퇴** (§1) — main 반영(PR #17).
2. ✅ **P2 조율 재인덱싱** (§2) — P2a/P2b-1/P2b-2/P2b-3, main 반영(PR #18).
3. ✅ **P3a 증분 PR + P3b-flow** (§2) — main 반영(PR #18). P3b-docs만 잔여(저가치).
4. ✅ **Qwen3 A/B → 차원 결정** (§4) — 1024-truncate 권장(측정 완료). 대형 코퍼스 재확인 후 ADR 락 잔여.
5. **P4 재개·원자성·락** (§2) / **P5 무중단 서빙·CKS 교차확인** (§3) — 오케스트레이션=CKS 의존.
6. **품질·인프라 잔여** (§5) — Instruct prefix·D.2 prefix·multi-gran·sliding 실측·B10, throughput/측정 대기.

---

## 1. 즉시 착수 — `ckg_node_id` 은퇴 (브랜치 주제)

전체 8단계 체크리스트는 [`retire-ckg-node-id.md`](./retire-ckg-node-id.md). **CKV 코드 완료 (2026-07-11, 미커밋)** — cks 이관 대기.

- [x] 코드 20개소(comment 6 + 코드 14)에서 `CKGNodeID`·`ckg_node_id` 제거
- [x] 완료 게이트: `grep -rn "ckg_node_id\|CKGNodeID"` *.go → **0건** · build ok · test green(coreml 제외)
- 근거(2026-07-11): `pkg/types/chunk.go:196`, `internal/store/sqlitevec/store.go:149,177,296,311,355,442,489,625,663`, `internal/query/engine.go:163`, `internal/query/snippet.go:137`, `internal/build/builder.go:88,91,331,335,340,346`, `internal/ckgalign/aligner.go:2,6`(주석)
- **마이그레이션 = inline `CREATE TABLE`/인덱스에서 제거 + fresh 재빌드**(설계 정합). reindex-design §4.2("서빙 DB in-place 변경 금지")·§4.3(expand-contract) 원칙상 Open 시 self-heal `DROP COLUMN`은 **채택 안 함** — 기존 DB의 죽은 컬럼은 무해하고 서빙본은 새 버전으로 재빌드·swap된다. schema_version 범프는 불필요(죽은 컬럼 contract는 어떤 reader도 깨지 않음).
- 체크리스트 **item 3(`ckgalign.Entry.ID`/`LookupEntry` ID 제거) 제외**: 게이트 grep과 무관하고 aligner 테스트 ~17건을 깨뜨림. `Entry.ID`는 정렬 *메커니즘*(node-id ladder 관찰값)이지 은퇴 대상 저장 필드가 아님. builder는 `LookupEntry.CanonicalID`만 계속 스탬프. → 별도 선택 정리로 이관.
- `canonical_id` 커버리지 백필은 **독립 과제**(선행조건 아님).

## 2. reindex/마이그레이션 잔여 (reindex-design §7 phase 기준)

P1(sources 원장 + alignment 감지)은 3자 완료·실증됨(handoff §4.0). 아래는 설계 §7의 공식 phase 순서.
reindex-design §7은 "P1 다음 P2가 최우선"(§0.2 gap1 "CKG 재생성 시 canonical_id 조용히 stale" 방지)이라 명시.

- [x] **P2 — 조율 재인덱싱** (§7-P2, §3) — 완료(브랜치 `feat/reindex-realign`, main 위 4커밋). P2a+P2b-1/2/3.
  - [x] **P2a — canonical_id 재정렬 편입** — reindex가 `manifest.Sources.CKG.Path`에서 `ckgalign.Load` → 재임베딩 청크에 `canonical_id` 재스탬프(빌드 경로 미러링). 미로드 시 warn-and-continue(fail-loud는 P1 Open/health digest assert). 테스트 `TestReindex_PreservesCanonicalAlignment`(재정렬 전 0/7 → 후 유지).
  - [x] **P2b-1 — graph_digest mismatch 전체 재정렬** — 같은 커밋에 그래프만 재생성(digest 변경)되면 git diff가 비어도 전체 청크의 `canonical_id`를 새 그래프로 재정렬(벡터 미변경, join 키만). `store.RealignCanonical` + `realignAllCanonical`. `CanonicalAvailable` 게이트(빈 그래프가 좋은 키를 지우지 않음), 비어있지 않은 값만 갱신. 새 digest를 manifest에 기록(다음 reindex no-op). 테스트 `TestReindex_RealignsOnGraphDigestChange`(regen 후 OLD→NEW).
  - [x] **P2b-2 — 검증 게이트(§5.1) + count 재조정(§5.2, P4-count 흡수)** — reindex 종료 시 `store.Validate`: `ChunkCount`를 실측 `COUNT(*)`로 재조정(근사 드리프트 버그 제거), orphan(청크↔벡터) 0 강제(위반 시 fail-loud), canonical 커버리지 계산(ckg-aligned인데 <90%면 warn). `ReindexResult.Validation`로 노출. 테스트 `TestReindex_ReconcilesChunkCount`(56 드리프트→50 실측), `TestReindex_ValidationReport`(orphan 0 / chunks==vectors / canonical>0).
  - [x] **P2b-3 — schema 캐스케이드 자동 트리거** — 기록된 vs 현재 CKG `schema_version` 불일치 시 부분 reindex를 **거부**(`ErrSchemaCascade`)하고 `ckv build` 전면 재빌드 유도(`ErrEmbedderMismatch`와 동일 패턴). 테스트 `TestReindex_RefusesOnSchemaBump`(1.22→1.23).
- [~] **P3 — 증분 PR·docs 인제스트** (§7-P3, §2) — 진행 중.
  - [x] **P3a — 증분 PR 인제스트** — `reindex --include-pr-history`가 `sources.prs.{last_pr_number,last_merged_at}` cutoff 이후 PR만 fetch(gh, `FetchMergedPRs`)해 number>cutoff만 인덱스(dedup)·source 청크 태깅·cutoff 갱신. 코어 `ingestPRs`(gh 불요, 주입식 테스트 `TestIngestPRs_DedupsAndIndexes`). 부수: `Validate`의 canonical rate 분모를 code-symbol 종류(`symbol`/`function_split`)로 한정(PR/doc 청크가 rate 왜곡 방지).
  - [x] **P3b-flow — flow corpus content_hash 재인덱싱** — `sources.flow.content_hash` 변경 감지 시 flow 레이어를 통째로 교체(`store.DeleteFlowChunks` → `flowcorpus.Load` → 재임베딩, 제거된 레코드도 정리). content_hash 갱신. 테스트 `TestReindex_ReindexesFlowOnContentChange`(마커 레코드 추가 후 반영 확인).
  - [ ] **P3b-docs — docs-roots content_hash 재인덱싱** — `sources.docs.content_hash` 변경 시 curated docs 레이어(`chunk_kind=doc` AND `category=domain`) 교체·재walk. (in-tree markdown은 이미 코드 diff 경로가 처리.)
- [ ] **P4 — 재개·원자성·락** (§7-P4, §4.4/§5.3) — 데이터 체크포인트 원장 + reindex 원자성(swap) + advisory lock + SetManifest 트랜잭션.

## 3. 외부·협의 대기 (§7-P5 무중단 서빙)

- [ ] **CKS 재기동 결과 수신** — `pr-77-2/ckv` reload 후 `cks.ops.health` alignment 블록 공유받아 양측(CKV `CheckAlignment` / CKS assert) 동일 digest(`4be26516…`) ok 교차확인. 프롬프트: `coordination-prompts §10.10`.
- [ ] **P5 blue-green 무중단 서빙** (§4/§6) — 버전 디렉터리 `<dataset>@<commit>-<digest[:8]>/{graph-db,vector-db}` + `current` 포인터 + 원자 promote + 인스턴스-레벨 blue-green. 오케스트레이션 주관=CKS, CKV는 버전본 생산·소비.

## 4. 임베딩 모델 교체 (reindex-B)

- [x] **Qwen3 A/B PoC — 차원 실측 완료 (2026-07-12)** — `qwen3-embedding:4b` full-2560 vs truncate-1024, `testdata/queries.yaml` N=50. **1024-truncate 권장**: recall@1 0.86/0.88·MRR 0.902/0.913(손실 ~1-2%p), 저장 2.47× 절감. 기록·결정: [`qwen3-dimension-ab-2026-07-12.md`](./qwen3-dimension-ab-2026-07-12.md).
- [x] **MRL truncate 경로** — `ollama.Options.TargetDim`(`truncateNormalize`) + CLI `--embed-dim`. 테스트 `TestTruncateNormalize`.
- [x] **대형 코퍼스 재확인 (2026-07-12)** — go-stablenet 83파일/1015청크(20×)에서 full-2560 vs 1024-truncate: top-1 일치 8/10, top-5 overlap 0.81, ground-truth 3쿼리 순위 동일, 저장 ÷2.1. **1024-truncate 권장 확증**. 기록: `qwen3-dimension-ab-2026-07-12.md §6`. 부수: `ckv build --batch N` 추가.
- [x] **ADR 승격 (차원 락) — `adr/008-qwen3-embedding-1024-dim.md` (Accepted, 2026-07-12)** — qwen3-embedding:4b @ 1024-truncate 확정. 정본 재검은 Consequences에 캐비엇으로 명시.
- [ ] **embed 경로 견고화** — 개별 대형 청크(>~20-40KB)가 ollama qwen3를 크래시(배치 무관). 재시도/skip-and-warn 또는 정본 머신(bge-m3 안정)에서 빌드. 별도 과제.
- [ ] **Instruct query-prefix** — `Embedder`가 query/passage 미구분. 프리픽스 주입은 인터페이스 확장 필요(별도 품질 레버). `knownDims` 표준화 포함.
- [ ] **qwen3-embedding:0.6b(native 1024) 비교** — 모델 크기 축(본 A/B는 차원 축만 판정).

## 5. backlog 잔여 (2026-05 세대 중 미종결)

- [ ] **B10** parser fuzz/property 테스트 (5개 파서, 독립 인프라).
- [ ] **A2** `ckv model fetch` CLI (`hf` 의존 제거) / **A3** linux CI matrix / **A4** bge-code-v1 Qwen2 어댑터.
- [ ] **#7** LLM contextual prefix (Phase D.2) — bgeonnx throughput buffer 회복 후.
- [ ] **PRR-1** full PR regression — throughput 보류(현 0.74 c/s).
- [ ] **flow Phase C→F** — file:line 정렬 강화 → 빌드 오케스트레이션(일부 `build-knowledge.sh`로 해소) → 평가. CKS 표면 노출(Phase D 마지막)은 CKS 소관.
- [ ] **ADR 승격(F)** — canonical_id join / 임베딩 모델·차원(측정 후) / flow 시그니처. R1/R2 가드레일을 Consequences에 측정 근거와 함께 명시.

## 6. 종결·정정 확인

- `backlog.md` B1~B9·E·F(CKV-1~7)·G(PRR-2~5)·C10, handoff §4.A2 등은 종결(✅). 상세는 각 문서 변경 이력.
- `backlog.md`·`pending-work-2026-05-21.md`는 본 문서로 supersede(내용 삭제 아님, live 잔여는 여기서 추적).
- handoff §5 문서 드리프트 정정(ADR-006 Rejected, mcp-tools 플래그 보강, coreml Makefile 제외)은 미해결 상태로 잔존 시 여기 편입.
