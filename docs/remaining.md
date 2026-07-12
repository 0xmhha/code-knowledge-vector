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
3. ✅ **P3 증분 PR·docs 인제스트** (§2) — P3a 증분 PR + P3b-flow(PR #18), P3b-docs(2026-07-12). P3 전체 완료.
4. ✅ **Qwen3 A/B → 차원 결정** (§4) — 1024-truncate 권장(측정 완료). 대형 코퍼스 재확인 후 ADR 락 잔여.
5. **P4 재개·원자성·락** (§2) / **P5 무중단 서빙·CKS 교차확인** (§3) — 오케스트레이션=CKS 의존.
6. **품질·인프라 잔여** (§5) — Instruct prefix·D.2 prefix(기본 비활성)·B10·hard fixture·레버 스윕·Phase B 프로토타입(measured **no-go**) 종결; 남은 품질 레버는 대형 코퍼스 재검증 대기(소형에선 rule-based가 이미 최적), sliding(Phase A)은 긴 함수 코퍼스 블록, throughput 대기.

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
- [x] **P3 — 증분 PR·docs 인제스트** (§7-P3, §2) — 완료(P3a/P3b-flow/P3b-docs).
  - [x] **P3a — 증분 PR 인제스트** — `reindex --include-pr-history`가 `sources.prs.{last_pr_number,last_merged_at}` cutoff 이후 PR만 fetch(gh, `FetchMergedPRs`)해 number>cutoff만 인덱스(dedup)·source 청크 태깅·cutoff 갱신. 코어 `ingestPRs`(gh 불요, 주입식 테스트 `TestIngestPRs_DedupsAndIndexes`). 부수: `Validate`의 canonical rate 분모를 code-symbol 종류(`symbol`/`function_split`)로 한정(PR/doc 청크가 rate 왜곡 방지).
  - [x] **P3b-flow — flow corpus content_hash 재인덱싱** — `sources.flow.content_hash` 변경 감지 시 flow 레이어를 통째로 교체(`store.DeleteFlowChunks` → `flowcorpus.Load` → 재임베딩, 제거된 레코드도 정리). content_hash 갱신. 테스트 `TestReindex_ReindexesFlowOnContentChange`(마커 레코드 추가 후 반영 확인).
  - [x] **P3b-docs — docs-roots content_hash 재인덱싱** (2026-07-12) — `sources.docs.content_hash` 변경 감지 시 curated docs 레이어(`chunk_kind=doc` AND `category=domain`)를 통째로 교체(`store.DeleteDocsChunks` → `--docs` roots 재walk → `reindexDocsRoots` 재임베딩). docs roots는 `docsRootsFromManifest`(manifest.DocsRoots에서 flow-corpus 디렉터리 제외)로 복원, 해시는 빌드와 동일한 `docsRootsHash`. `ReindexResult.DocsReindexed` 노출. in-tree markdown(category="")·flow 청크(다른 kind)는 미영향. 테스트 `TestDeleteDocsChunks`(store: domain-doc만 삭제, symbol/in-tree 유지)·`TestReindex_ReindexesDocsOnContentChange`(마커 섹션 추가 후 반영).
- [x] **P4 — 재개·원자성·락** (§7-P4, §4.4/§5.3) — 완료(P4a/P4b/P4c).
  - [x] **P4a — advisory lock** — `acquireDatasetLock`(`<out>/.ckv.lock` flock, non-blocking) — 동시 build/reindex 직렬화(§5.3), 크래시 시 자동 해제. Run/Reindex 진입 시 획득, 점유 시 `ErrLocked`. 테스트 `TestAcquireDatasetLock_*`·`TestRun_RefusesWhenLocked`.
  - [x] **P4b — 원자성 + SetManifest 트랜잭션** — 조사 결과 대부분 이미 원자적: `manifest.Save`는 temp+rename(원자적), reindex는 manifest-last 순서라 크래시 시 재실행으로 자기치유(멱등). 실질 갭은 in-DB `setManifestKVs`가 키별 auto-commit이던 것 → **단일 트랜잭션**으로 원자화(all-or-nothing, §4.4). 커버: `TestStatsReflectsManifest`. (전체 all-or-nothing 스왑은 §4.1 blue-green=P5 소관.)
  - [x] **P4c — 데이터 체크포인트 원장** — reindex가 `<out>/.ckv-reindex.ckpt`(append-log: head + `sha\trel`)에 완료 파일을 기록 → 중단된 run이 재실행 시 content-sha 일치 파일을 skip(`FilesResumed`). 완료 시 원장 삭제, 다른 head는 stale로 폐기. 테스트 `TestResumeCheckpoint_*`·`TestReindex_ResumeSkipsCheckpointedFile`.

## 3. 외부·협의 대기 (§7-P5 무중단 서빙)

- [ ] **CKS 재기동 결과 수신** — `pr-77-2/ckv` reload 후 `cks.ops.health` alignment 블록 공유받아 양측(CKV `CheckAlignment` / CKS assert) 동일 digest(`4be26516…`) ok 교차확인. 프롬프트: `coordination-prompts §10.10`.
- [~] **P5 blue-green 무중단 서빙** (§4/§6) — CKV-side 슬라이스 완료, 오케스트레이션은 CKS.
  - [x] **`ckv promote` (원자 promote 프리미티브)** — `build.PromoteVersion(dataset, version)`: 검증 게이트(§5.1, manifest + orphan 0) 통과 후 `<dataset>/current` 심링크를 temp+rename으로 원자 스왑. 실패 시 current 미이동. CLI `ckv promote --dataset --version`. 테스트 `TestPromoteVersion_AtomicSwapAndGate`.
  - [x] **health 버전 보고** (§6) — `Engine.ResolvedVersion()`(데이터 경로 심링크 resolve → 버전 디렉터리명) → `cks.ops.health.resolved_version`.
  - [ ] **CKS 소관**: 버전 네이밍/디렉터리 레이아웃, 언제 promote할지, 인스턴스-레벨 blue-green 전환, 보존/GC, 조율 재인덱싱 오케스트레이션.

## 4. 임베딩 모델 교체 (reindex-B)

- [x] **Qwen3 A/B PoC — 차원 실측 완료 (2026-07-12)** — `qwen3-embedding:4b` full-2560 vs truncate-1024, `testdata/queries.yaml` N=50. **1024-truncate 권장**: recall@1 0.86/0.88·MRR 0.902/0.913(손실 ~1-2%p), 저장 2.47× 절감. 기록·결정: [`qwen3-dimension-ab-2026-07-12.md`](./qwen3-dimension-ab-2026-07-12.md).
- [x] **MRL truncate 경로** — `ollama.Options.TargetDim`(`truncateNormalize`) + CLI `--embed-dim`. 테스트 `TestTruncateNormalize`.
- [x] **대형 코퍼스 재확인 (2026-07-12)** — go-stablenet 83파일/1015청크(20×)에서 full-2560 vs 1024-truncate: top-1 일치 8/10, top-5 overlap 0.81, ground-truth 3쿼리 순위 동일, 저장 ÷2.1. **1024-truncate 권장 확증**. 기록: `qwen3-dimension-ab-2026-07-12.md §6`. 부수: `ckv build --batch N` 추가.
- [x] **ADR 승격 (차원 락) — `adr/008-qwen3-embedding-1024-dim.md` (Accepted, 2026-07-12)** — qwen3-embedding:4b @ 1024-truncate 확정. 정본 재검은 Consequences에 캐비엇으로 명시.
- [x] **embed 경로 견고화 (2026-07-12)** — `embedAndUpsert`가 `embedResilient`로 배치 실패 시 이분 분할 재시도. 단일 청크 실패는 **복구 래더**로 처리: full-input 재시도(4b 크래시는 flaky-메모리라 모델 리로드 후 성공) → 점진적 truncation(12KB→4KB, 저장 Text는 불변·벡터만 잘린 입력) → 그래도 실패 시에만 skip-and-warn. systemic 장애는 tiny probe로 구분해 전파(빈 인덱스 방지), ctx 취소 전파. **검증: 이전에 skip되던 crash 파일(blockchain.go 등)이 이제 0 skip으로 완전 복구**(95/95 청크). 테스트 `TestEmbedResilient_{SkipsPoisonChunk,RecoversOversizedByTruncating,PropagatesCtxError}`.
- [x] **Instruct query-prefix (2026-07-12)** — 옵션 `types.QueryEmbedder` 인터페이스 + 레지스트리 `QueryInstruct`(qwen3만) + ollama `EmbedQuery`(qwen3 쿼리에 `Instruct: {task}\nQuery:` 래핑, passage는 raw). query.Engine이 있으면 `EmbedQuery` 사용. `CKV_DISABLE_QUERY_PREFIX=1` 토글. **측정**(go-stablenet gs-full): recall@10 3/10→**4/10**(chaincmd MISS→8 회복, handler 2→1, 회귀 없음). 테스트 3건.
- [x] **knownDims 표준화 (2026-07-12)** — 레지스트리 `ModelConfig.KnownDims`(qwen3:4b `512/1024/2560`, 0.6b `256/512/1024`, native 포함) + `registry.KnownDims(name)`. ollama `validateTargetDim`가 `--embed-dim`을 KnownDims로 검증(비표준·초과 dim 거부, 비-MRL 모델은 무제한). CLI help 갱신. 테스트 `TestKnownDims`·`TestValidateTargetDim`.
- [x] **qwen3 0.6b vs 4b-trunc-1024 비교 + 대형 재확인 (2026-07-12)** — 소표본(N=4)은 0.6b 근소 우위였으나 **대형(123파일/1834청크, N=9)에서 정정: 트레이드오프**. recall@10 동률 9/9, MRR 4b 0.748 vs 0.6b 0.683(4b 근소 우위), 저장 동일 — 단 0.6b는 **4× 작고(639MB) 0 skip 완전 안정**(4b는 7청크 크래시 skip). **ADR-008(4b@1024) 뒤집을 근거 없음**, 0.6b는 메모리·안정성 우선 배포의 강력한 대안. 기록: `qwen3-dimension-ab-2026-07-12.md §8`.

## 5. backlog 잔여 (2026-05 세대 중 미종결)

- [x] **B10** parser fuzz/property 테스트 — **이미 구현됨**(확인 2026-07-12): 5개 파서(golang/typescript/javascript/solidity/markdown) 각 `FuzzParse`(seed corpus) + 공유 `internal/parse/fuzzcheck.CheckSpans` 불변식(StartLine≥1, EndLine≥StartLine, Name/Kind/Text 비어있지 않음, panic 없음). seed는 `go test`(CI)에서 실행, `-fuzz` 런 검증(golang 8s/547K exec PASS).
- [x] **A2** `ckv model fetch` CLI — **완료**(확인 2026-07-12): `internal/embed/model.FetchModel`이 `https://huggingface.co/<repo>/resolve/main/<file>`로 직접 HTTP 다운로드(온디스크 temp+rename, 기존 파일 skip). **`hf` CLI 의존 0건**. 테스트 `TestFetchModel_*`·`TestDownloadFile_*`(httptest). (`ckv model convert`의 optimum/coremltools는 별개 = ONNX/CoreML 변환 툴, hf 아님.)
- [x] **A3** linux CI matrix — **완료**: `.github/workflows/ci.yml` `matrix.os` = linux(amd64/arm64) + macos/arm64. PR 체크의 `test (linux/*)`가 이것.
- [x] **A4** bge-code-v1 Qwen2 어댑터 (2026-07-12) — bgeonnx `lastTokenPoolNormalize`(마지막 attended 토큰 + L2, `l2Normalize`로 3중 중복 제거) 구현 + `poolByMode` 배선. `bge-code-v1` 레지스트리 엔트리(Dim 1536, MaxInput 32768, Qwen2 시그니처 `input_ids/attention_mask/position_ids` + `PositionIDsExtraInput`, last-token pooling). 테스트 `TestLastTokenPoolNormalize_*`·`TestPoolByMode_LastTokenDispatches`·`TestBGECodeV1Entry`. **캐비엇**: end-to-end ONNX 실행은 모델(~5GB, `ckv model convert`로 export) + libonnxruntime 필요(이 머신 env 부재) — pooling 수치는 단위검증, 엔트리는 기존 패턴 준수.
- [x] **#7** LLM contextual prefix (Phase D.2) — **구현·실측 완료**(2026-07-12, `llm-contextual-prefix-poc-2026-07-12.md`). `internal/llmprefix`(주입형 `Generator`/디스크캐시 `Cached`/`OllamaGenerator`) + `--llm-prefix-model` 배선(build/reindex). **PoC 결정**: testdata/sample(50청크·bge-m3)에서 LLM prefix(llama3)가 rule-based(D.1)를 **못 이김**(recall@1 0.86→0.78, 조합형도 0.84; MRR 0.911→0.877/0.900; 생성 19×). → **기본 비활성 opt-in 레버**로만 제공, 켰을 때는 조합형(LLM+rule+raw). 캐비엇: 소형 self-descriptive 코퍼스·llama3·벡터단독 편향 — 대형 코퍼스/강한 생성기/BM25 병용 시 재측정 여지.
- [x] **D1-FU-7 hard eval fixture (2026-07-12)** — 기본 `queries.yaml`이 소형 코퍼스에서 천장(bge-m3 recall@5 0.98)이라 품질 레버를 변별 못 하는 문제 해소. `testdata/queries-hard.yaml`(N=24) 신설 — zero-lexical-overlap·lexical decoy·cross-language·indirect로 설계. **실측**: recall@1 0.86→**0.58**, MRR 0.911→**0.669**, recall@5 0.98→0.88(측정 대역 확보, 10/24 non-rank-1). Phase A/B·D.2 go/no-go 측정 기반. `TestLoadHardQueriesFixture`(CI 검증). 기록: `eval-hard-fixture-2026-07-12.md`. **캐비엇**: 동일 소형 코퍼스라 *변별력*용이지 절대 벤치 아님 — 대형 코퍼스 별도.
- [x] **prefix 레버 측정 스윕 (2026-07-12)** — hard fixture로 raw/D.1/D.2를 한 번에 재측정(bge-m3). **판정**: D.1(rule-based)이 두 fixture 모두 승자 — raw 대비 recall@1 **+0.16**(hard 0.42→0.58, easy 0.70→0.86), hard MRR/recall@5 최고. **D.2 헤드룸 가설 반증**: 천장 벗어난 hard set에서도 D.2가 D.1 못 이김(recall@1 0.54<0.58) → D.2 열세는 소형-코퍼스 아티팩트 아님, 기본 off 확정, #36 캐비엇 종결. 기록: `prefix-lever-sweep-2026-07-12.md`. **함의**: 강한 D.2조차 rule-based를 못 이겨 Phase B(multi-gran) 한계이득 재고 신호 — 착수 전 프로토타입으로 hard fixture 신호 선확인 권장.
- [x] **Phase B (multi-granularity) go/no-go 프로토타입 (2026-07-12)** — 전체 구현(~250 LOC·throughput −50%) 전에 저비용 프로토타입으로 판정. `internal/chunk`에 opt-in coarse 청크 `file_full`(env `CKV_EXPERIMENTAL_FILE_FULL`, 기본 off) 추가 + coarse probe fixture(`queries-coarse.yaml` N=8) 신설. **판정 NO-GO**: coarse 청크가 도와야 할 coarse probe에서 오히려 recall@3 **1.00→0.88**(MRR 0.792→0.754), hard에서 recall@5 **0.88→0.79**·found **21→19** 회귀. baseline coarse probe가 이미 recall@3=1.00(헤드룸 없음, file_header가 소형 파일 커버). → Phase B 전체 구현 보류, file_full은 gated off 유지(대형 이질 코퍼스 재검증용, D.2와 동일). 테스트 `TestIncludeFileFullEmitsCoarseChunk`·`TestLoadCoarseQueriesFixture`. 기록: `phase-b-multigran-probe-2026-07-12.md`.
- [ ] **PRR-1** full PR regression — throughput 보류(현 0.74 c/s).
- [ ] **flow Phase C→F** — file:line 정렬 강화 → 빌드 오케스트레이션(일부 `build-knowledge.sh`로 해소) → 평가. CKS 표면 노출(Phase D 마지막)은 CKS 소관.
- [~] **ADR 승격(F)** — 대부분 완료. canonical_id join(**ADR-007**), 임베딩 차원(**ADR-008**), retrieval prefix/granularity 전략(**ADR-009**: rule-based 기본·D.2/Phase B defer, 측정 근거 명시). **잔여**: flow 시그니처 ADR(현재 미작성)만.

## 6. 종결·정정 확인

- `backlog.md` B1~B9·E·F(CKV-1~7)·G(PRR-2~5)·C10, handoff §4.A2 등은 종결(✅). 상세는 각 문서 변경 이력.
- `backlog.md`·`pending-work-2026-05-21.md`는 본 문서로 supersede(내용 삭제 아님, live 잔여는 여기서 추적).
- handoff §5 문서 드리프트 정정(ADR-006 Rejected, mcp-tools 플래그 보강, coreml Makefile 제외)은 미해결 상태로 잔존 시 여기 편입.
