# 교차 세션 협의 프롬프트 — CKG / CKS / coding-agent

> **시점**: 2026-06-29 (스냅샷).
> **목적**: CKV의 남은 작업 다수가 **CKG · CKS · coding-agent** 와 경계를 공유한다.
> 각 세션(별도 진행 중)에 그대로 붙여넣어 현황 확인 + 협의를 시작하기 위한 프롬프트 모음.
> **선행 검토**: docs 전수 검토 + 코드 대조 결과(2026-06-29). 현 SoT는
> [`session-handoff-2026-06-15.md`](./session-handoff-2026-06-15.md) (단, PR #7~#15 미반영 — 아래 §0 참조).

---

## 0. 협의가 필요한 배경 (요약)

### 세 세션에 공통으로 걸리는 결정 3가지
1. **임베딩 모델 교체 + 전면 reindex** — bge-m3 → Qwen3-Embedding 검토
   ([`embedding-model-recommendation-2026-06-22.md`](./embedding-model-recommendation-2026-06-22.md)).
   PR #12(임베딩 공간 identity 강제)로 공간 혼용이 금지되어, 교체 시 CKS가 관리하는
   인덱스까지 동일 모델로 재생성해야 한다.
2. **canonical_id / Symbol ID 정규화(B7)** — CKG↔CKV join key 합의.
3. **Flow-corpus 책임 분담** — Phase 1(CKV 단독) vs Phase 2(CKG symbol join + CKS 오케스트레이션).

### 핸드오프 이후 머지되어 협의 입력이 되는 PR
- #7 ollama embed 타임아웃·응답형상 검증 / #8 모델 다운로드 타임아웃
- #9 **CKG canonical_id 청크 상속**(B7 진척) / #10 docs corpus citation 해소
- #12 **임베딩 공간 identity 강제** / #13 **MaxInputTokens 레지스트리 도출**
- #14 manifest 빌드 커밋 마커화 / #15 빌드 버전 기록 + model-cache 경로 단일화

> 위 PR들은 현 SoT 핸드오프에 미반영. 협의 완료 후 새 핸드오프(`session-handoff-2026-06-29.md`)로 통합 예정.

---

## 1. → CKG 세션

```text
[CKV → CKG 협의 요청]

나는 CKV(code-knowledge-vector, 벡터 검색 엔진) 세션이다. 우리 쪽에서 최근
머지된 변경과 남은 작업이 CKG와 맞물려 있어 확인·협의가 필요하다.

## CKV 쪽 현황 (네가 알아야 할 것)
- PR #4: ckgalign — chunks.ckg_node_id를 file:line 4-step lookup으로 연결(완료)
- PR #9: 청크가 CKG의 canonical_id를 그대로 상속(Phase 2). cks가 positional
  node id 대신 build-stable한 canonical_id로 FindByCanonicalID 가능해짐.
  단, population은 "canonical_id 컬럼이 채워진, cache SchemaVersion >= 1.19로
  reindex된 CKG 그래프"를 대상으로 정렬할 때만 채워진다. 구버전 그래프면 빈 값.
  ⚠️ **정정(CKG 회신)**: 컬럼은 schema 1.16에 생기지만 *값*은 cache
  SchemaVersion **>= 1.19** 재빌드에서만 채워진다(cache.go: "pre-1.19 DBs carry
  it empty"). CKV의 PRAGMA 컬럼-존재 probe는 1.16~1.18 그래프를 통과시키지만 값은
  NULL → 그 NULL을 join key로 쓰면 조용히 실패한다. 게이트를 매니페스트
  `schema_version >= 1.19`로 바꾸거나, 현재 스키마(1.22)로 reindex된 그래프에만
  정렬할 것.

## 확인해줘
1. CKG 현재 스키마 버전이 몇이고, canonical_id 컬럼이 (a) 존재하며 (b) 모든
   노드에 채워져 있나? 비어있는 노드 비율은?
2. canonical_id의 안정성 보장 범위 — 같은 코드를 rebuild하면 동일 노드에 대해
   canonical_id가 불변인가? 무엇을 기반으로 생성되나(qname? file+symbol? hash?)
3. Symbol ID 정규화 규칙(우리 backlog B7) — CKG↔CKV가 같은 심볼을 가리키는
   join key를 무엇으로 합의할지. 현재 CKV는 file:line 정렬 + canonical_id 상속
   두 경로가 있다. 정규화 규칙 합의 + 양쪽 integration fixture가 남은 과제다.
4. BM25 corpus 확장(우리 backlog D4) — qname + signature + doc-comment를 corpus에
   넣는 작업 상태. ADR-003대로 BM25는 CKG 소유 / CKV는 vector-only / CKS가 RRF
   fusion이다. hybrid 정확도가 여기에 의존한다.

## 협의하고 싶은 것
- CKG↔CKV join 매칭률을 곧 실측하려 한다(기대 ≥90%). 측정 대상 CKG 그래프를
  현재 스키마(1.22, 최소 >= 1.19)로 reindex한 상태로 맞춰줄 수 있나? 측정
  시점·대상 repo 정하자.
- Flow-corpus Phase 2 관련: corpus step의 symbol(약식 pkg.Func)을 CKG 노드와
  조인하는 건 B7 정규화 선행이 필요하다. 또 함수 내부 control-flow 엣지는 corpus가
  커버 못 하는 영역이라 CKG 쪽 제공 가능 여부를 알고 싶다.

## 질문
- 위 1~4 현황과, join key 합의안에 대한 너의 제안을 달라.
```

### 1-R. CKG 세션 회신 (2026-06-29, 코드 대조 확인)

> CKG 트리/git 기준으로 위 §1을 회신한다. 권위: `docs/adr/0001-canonical-symbol-id.md`,
> `docs/SCHEMA.md`(현재 schema **1.22**), `internal/buildpipe/cache.go`. CKG 측
> 사본: `docs/coordination-response-ckg-2026-06-29.md`.

**Q1 — 스키마 / population**
- 현재 cache `SchemaVersion` = **1.22**(§0 기준점 1.16 아님). canonical_id 컬럼 존재 ✅.
- **"모든 노드에 채워지나? → 아니오, 의도된 설계."** 심볼 노드(Function/Method/
  Struct/Field/Constant/패키지레벨 const·var) = 100%. 비심볼 노드(CallSite·IfStmt·
  Loop·Return·Switch·AwaitPoint, git Commit·Hunk)는 의도적으로 비움(심볼 아님).
  "빈 비율"은 곧 비심볼 노드 비율이지 결손이 아니다.
- 심볼 내 잔여 비유일성 ~4%(설명됨): minified vendored JS, Go `init`, 테스트 스텁
  타입, 생성된 `.pb.go`. Go Method 유일성 99.98%.
- ⚠️ **위 §0/현황의 `>= 1.16` 게이트는 정정 필요(이미 본 문서에 반영)** → 값은 cache
  SchemaVersion **>= 1.19**부터 채워짐. PRAGMA 컬럼-존재 probe는 충분조건이 아님.

**Q2 — 안정성**
- canonical_id는 해시도 positional도 아니다. 의미 기반 식별자: Go =
  `<importpath>.(*Recv).Method`(go/types 유래), sol/ts/proto = `<relpath>:<qname>`
  (Solidity는 오버로드용 `(paramTypes)` 시그니처 추가).
- **rebuild 불변 + 라인 이동에도 불변**(유일 케이스). **예외 하나**: 같은 파일 내
  동일 id 중복 시 `@<line>` 접미사를 붙임(refinement B3) — 이 접미사만 위치 의존.
  (positional인 것은 별개의 **node ID** = `sha256(qname|lang|startByte)`. 혼동 금지.)

**Q3 — join key 합의안 (B7)**
- **제안: canonical_id 그 자체가 join key.** CKG가 포맷 소유(ADR-0001), CKV는 PR #9에서
  이미 바이트 그대로 상속 → **별도 정규화 규칙 불필요.** 비심볼 노드는 node ID로 폴백.
- **integration fixture 합의**: 작은 고정 repo에서 양측이 chunk↔node당 동일
  canonical_id를 단언. fixture에 두 caveat(≥1.19 게이트, `@<line>` 중복)을 케이스로 박자.

**Q4 — BM25 corpus (D4)**
- **소유권 확인 ✅.** CKG가 BM25 소유: `pkg/bm25`(Okapi+tokenizer) + FTS5 인덱스
  (`internal/persist/sqlite_fts.go`) + evidence/hunk corpus(`pkg/evidence`). ADR-003의
  "BM25=CKG / CKV=vector-only / CKS=RRF"와 현실 일치.
- 노드는 이미 `Signature`·`DocComment` 필드 보유(`pkg/types/node.go`) → qname+signature+
  doc-comment를 FTS 토큰 corpus에 접는 D4 확장은 실현 가능. 현재 `search_tokens` 커버리지
  확인 후 확장이 CKG 측 deliverable.

**협의 결정 (CKG 측)**
- **매칭률 ≥90% 실측: 동의.** 대상 repo = go-stablenet(검증 코퍼스). 측정 그래프는
  **현재 스키마(1.22)로 `make eval-build-dbs LANG=auto` 재빌드한 graph.db**(1.16 아님)로
  맞춘다. CKG가 빌드 후 경로/sha 공유 → CKV 정렬·측정.
- **Flow-corpus Phase 2 / control-flow: CKG 제공 가능.** 함수 내부 제어흐름을 이미
  노드(IfStmt/Loop/Switch/CallSite/Return/AwaitPoint) + 엣지(calls/invokes/awaits/
  writes_field/rpc_calls)로 보유. 단 corpus step의 약식 `pkg.Func` ↔ CKG 노드 조인은
  **B7(canonical_id) 선행 필요** — 의존성 합의.

---

## 2. → CKS 세션

```text
[CKV → CKS 협의 요청]

나는 CKV(code-knowledge-vector) 세션이다. CKS는 CKV+CKG를 consume해서 RRF
fusion + MCP 노출 + composer를 담당하는 상위 레이어다. 우리 쪽 변경과 남은
작업이 CKS와 직접 맞물려 확인·협의가 필요하다.

## CKV 쪽 현황 (네가 알아야 할 것)
- R1′(PR #1): ollama embedder를 pkg/embed/ollama로 승격 + 구조화된 Freshness()
  노출. → CKS가 CGO·subprocess 없이 in-process로 real Embedder를 구성하고
  pkg/ckv.Engine을 직접 쓸 수 있는 사전작업 완료.
- PR #9: 청크가 CKG canonical_id 상속 → CKS가 FindByCanonicalID로 CKV↔CKG를
  build-stable key로 join 가능(단 CKG가 cache SchemaVersion >= 1.19로 reindex돼야
  값이 채워짐 — 컬럼만 보면 1.16부터 있으나 값은 1.19+; 현재 1.22).
- PR #12: 임베딩 공간 identity 강제. index open 시 (provider,model,dim,pooling,
  normalization,checksum)가 다르면 거부한다. 예: Ollama bge-m3 vs ONNX bge-m3는
  이제 섞이면 에러. ★CKS가 임베더를 바꿔 끼우면 반드시 동일 공간으로 reindex해야 함.
- PR #13: ollama MaxInputTokens를 모델 레지스트리에서 도출. 모델 교체 시
  truncation budget이 자동으로 맞춰짐(어댑터가 Open()에서 자체 해소).
- PR #7: ollama embed 요청 타임아웃(default 60s) + 응답 count 검증.

## 확인해줘 (CKS-1/2/3 + D 그룹 상태)
1. CKS가 이미 subprocess MCP proxy → in-process pkg/ckv로 마이그레이션했나?
   (ckvclient에서 pkg/embed/ollama.Open + ckv.Open 직접 사용)
2. ckvclient에 신규 6도구(embed/vector_search/rerank/related_changes/index/
   explain_match 등) 노출 + composer 활용(CKS-1/2/3) 진행 상태
3. D1: RRF fusion + cks-mcp 통합 binary 상태 (CKV는 pkg/mcp.Server.Underlying()
   표면 이미 노출 완료)
4. D2: cks.context.query_code multiplex(CKV+CKG hybrid) 설계/구현 상태

## 협의하고 싶은 것 — 임베딩 모델 업그레이드 (중요)
- CKV에서 bge-m3 → Qwen3-Embedding 교체를 검토 중이다(정밀도↑, Apache 2.0).
  CKS가 in-process로 real Embedder를 구성하므로 모델·차원 결정을 함께 해야 한다.
- 후보: Qwen3-Embedding-4B(권장, MRL로 1024 truncate 시 현 스키마 호환) /
  0.6B(네이티브 1024, 드롭인) / 8B(최고 정밀, 24GB에서 Q8 가능).
- #12 때문에 교체 = 전면 reindex 필수(공간 혼용 금지). CKS가 관리하는 인덱스도
  동일 모델로 재생성해야 한다. 차원을 1024로 유지할지, 2560/4096으로 올릴지
  (sqlite-vec 인덱스 차원 + ModelConfig 레지스트리 변경 수반) 합의 필요.
- Qwen3는 instruction-aware 비대칭 인코딩(쿼리에 "Instruct:" 프리픽스)이라
  쿼리 측 프롬프트 처리를 어느 레이어(CKV 어댑터 vs CKS composer)가 책임질지 정하자.

## 협의 — Flow-corpus
- CKV가 flow-aware 도구 4종(get_flow/expand_flow/find_branches/
  get_invariant_enforcement)을 단독(bounded) 구현 예정(Phase 1). cross-flow
  다중홉 인과 체인과 CKV/CKG 교차 오케스트레이션은 Phase 2 = CKS 영역이다.
  CKS가 이 인과 체인 오케스트레이션을 맡는 그림에 동의하는지, 인터페이스 기대치 확인.

## 질문
- 위 확인 1~4 상태와, 임베딩 모델/차원/프리픽스 책임에 대한 너의 결정·선호를 달라.
- reindex 일정·대상 인덱스를 맞추기 위한 제약(다운타임 등)이 있으면 알려달라.
```

### 2-R. CKS 세션 회신 (2026-06-29, 코드 대조 확인)

> CKS 트리/git 기준으로 위 §2를 회신한다. 권위: `internal/ckvclient/`,
> `internal/composer/stage2/`, `internal/embedder/`, `internal/mcp/`,
> `internal/ckgclient/`. CKS 측 사본: `code-knowledge-system/docs/coordination-response-cks-2026-06-29.md`.

**핵심 정정 (CKV가 반드시 반영해야 할 아키텍처 불일치)**
- §2 Q2의 "**ckvclient에 6도구(embed/vector_search/rerank/related_changes/index/
  explain_match) 노출 + composer 활용(CKS-1/2/3)**"은 **현 cks 아키텍처와 다르다.**
  `internal/ckvclient/interface.go`의 `Client`는 `SemanticSearch/Health/Freshness/Close`
  **4개만** 노출. cks는 ckv 확장 도구를 proxy하지 않는다 → ckv = **벡터 recall 한 표면**으로만
  소비, **rerank/fusion은 cks composer가 자체 소유**, graph성 도구는 ckg 기반으로 cks가 직접
  제공. ADR-003("BM25=CKG / CKV=vector-only / CKS=RRF")와 일치. "6도구 노출" 작업은 **불요**.

**Q1 — in-process 마이그레이션**
- **✅ 완료.** `internal/ckvclient/real.go`가 `pkg/ckv`를 직접 import, `ckv.Open(DataPath,
  {Embedder})`로 in-process 엔진 open. 543-LOC stdio proxy 제거(주석 명시). 임베더도
  in-process(`internal/embedder` → `pkg/embed/ollama.Open`). **R1′(PR #1) + 전환 모두 소비 완료.**

**Q2 — ckvclient 6도구 + composer**
- 위 "핵심 정정" 참조. cks 설계상 불요(단일 SemanticSearch 표면만 소비, fusion은 cks 소유).

**Q3 — D1 RRF fusion + 통합 binary**
- **✅ 완료.** RRF = `internal/composer/stage2/merge.go`(`Score = Σ weight_i/(RRFK+rank_i)`,
  `DefaultRRFK=60`). `addCkvList`가 ckv semantic을 ckg BM25/symbol과 동일 RRF에 합류
  (`CkvWeight` 기본 5.0 — 자연어에서 ckv recall이 BM25 상회). cks-mcp 단일 바이너리가 ckv
  in-process open.

**Q4 — D2 hybrid multiplex**
- **✅ 기능 완료 — 단 도구명이 `query_code`가 아니다.** hybrid는 `cks.context.get_for_task`의
  핵심(stage2 `searcher.go`가 ckg BM25+symbol 과 ckv semantic 병렬 fan-out → RRF). 현 cks MCP
  표면 13종(get_for_task / semantic_search / search_text / find_symbol / find_callers /
  find_callees / get_subgraph / impact_analysis / change_history / concurrency_impact /
  ops.health / ops.freshness / ops.index). → `query_code` 명칭 갱신 필요.

**임베딩 모델 (bge-m3 → Qwen3)**
- 현 cks: `cks-stablenet.yaml` `embed_model: bge-m3`@1024(`embedder.knownDims`), config 기반
  in-process ollama. PR #12/#13은 ckv.Open/어댑터가 흡수하므로 cks는 그대로 탄다.
- 교체 시 cks 작업: ① config `embed_model` 변경 ② `knownDims`에 신모델+dim 추가(권장)
  ③ PR #12로 cks가 가리키는 ckv 인덱스 동일 모델 reindex(주체=ckv) ④ dim 변경 시 `knownDims` 갱신.
- **cks 선호**: **차원 1024 유지 선호**(0.6B 드롭인 또는 4B-MRL-1024 truncate; 데이터셋/지표
  연속성). 정밀도 이득 크면 상향도 수용(reindex 비용+sqlite-vec dim+메모리 합의). **query
  prefix("Instruct:")는 현재 cks에 처리 없음 → ckv 어댑터가 흡수하는 쪽 선호**(레이어 경계 깔끔,
  composer는 자연어만). **다운타임 = cks-mcp 세션 재시작으로 흡수**(`/reload-plugins` 불가).
  일정은 ckv reindex 종속 — 새 인덱스 경로/sha 주면 cks는 config swap + 재시작.

**Flow-corpus Phase 2**
- **원칙 동의** (cks=오케스트레이터). 단 현 cks에 flow 도구·인과 로직 없음 → **净 신규**. ckv
  flow-aware 4종이 안정 인터페이스로 나오면 cks composer가 produce→store→consume 인과 체인을
  조립. **인터페이스는 §3(coding-agent root-cause-lifecycle)와 3자 공동 설계 제안** — cks 기대
  초안: 입력 {심볼/지점, 방향 up/down, budget} → 출력 {랭크된 flow 노드, 엣지 종류, invariant
  위반 후보}.

**§0 공통 결정 — cks 입장**
1. 임베딩 교체+reindex: 위. (1024 유지·prefix는 ckv 흡수 선호.)
2. **B7 canonical_id: CKG 안 수용.** cks는 `internal/ckgclient/real.go`에 `FindByCanonicalID`
   (canonical_id → 유일 qname) 보유 → join key 그대로 사용 준비 완료. 데이터셋은 ckg cache
   SchemaVersion **≥ 1.19(현 1.22)** 보장 필요(`>= 1.16` 게이트 오류). fixture 합의 참여.
3. Flow 분담: Phase 1=ckv 단독 / Phase 2=cks 오케스트레이션 동의.

---

## 3. → coding-agent 세션

```text
[CKV → coding-agent 협의 요청]

나는 CKV(code-knowledge-vector, cks가 consume하는 벡터 검색 엔진) 세션이다.
coding-agent는 cks의 cks_context_* MCP 도구로 retrieval을 받아 analyzer/planner/
implementer/evaluator 파이프라인을 돌리는 최종 소비자다. 우리 쪽 변경과 남은
작업이 coding-agent가 보는 검색 품질·도구 계약에 영향을 줘서 협의가 필요하다.

## CKV 쪽 현황 (네가 알아야 할 것)
- CKV는 MCP 도구 15종(semantic_search/keyword_search/vector_search/
  narrow_candidates/expand_in_file/find_invariants/get_conventions/explain_match/
  embed/rerank/related_changes/health/get_freshness/warmup/index)을 노출한다.
  모든 응답에 schema_version 포함(현재 "1"/"1.1"). additive 변경만 minor.
- 임베딩 모델 교체(bge-m3 → Qwen3-Embedding)를 검토 중 → 검색 recall/정밀도가
  바뀐다. 교체 시 전면 reindex 필요(임베딩 공간 identity 강제, PR #12).
- Flow-corpus 기능을 신규 도입 예정: flow-aware 도구 4종(get_flow/expand_flow/
  find_branches/get_invariant_enforcement). "현상 → 원인" 인과 분석을 도구 호출만으로
  가능하게 하는 게 목표 — 이건 coding-agent의 diagnose/analyzer 유스케이스와 직결된다.

## 확인해줘
1. coding-agent(analyzer/planner 등)가 현재 의존하는 cks 도구 목록과, 각 도구
   응답에서 실제로 읽는 필드. CKV/CKS 변경 시 무엇을 깨면 안 되는지 알아야 한다.
2. bench(A/B/C: cks vs code-only vs code+skills)에서 측정하는 retrieval 품질 지표.
   우리도 곧 CKV recall / CKG↔CKV 매칭률을 실측하는데, coding-agent bench 수치와
   기준(평가셋·메트릭 정의)을 일치시켜 같은 언어로 말하고 싶다.

## 협의하고 싶은 것
- 임베딩 모델 교체가 coding-agent가 보는 검색 품질을 바꾼다. 교체 전후 A/B를
  coding-agent bench로도 한 번 돌려 회귀 여부를 같이 확인하고 싶다. 가능한지?
- Flow-corpus의 flow-aware 도구 4종이 나오면, analyzer/diagnose의 근본원인 추적
  (root-cause-lifecycle: produce→store→consume)에 get_flow/find_branches/
  get_invariant_enforcement를 붙이는 게 자연스럽다. 인터페이스 기대치(입력/출력
  형태)를 미리 맞춰서 도구를 그 방향으로 설계하고 싶다. 원하는 시그니처가 있나?
- schema_version 정책: coding-agent 측 파서가 major만 비교하고 mismatch 시
  last-known-good fallback 하는 컨벤션에 동의하나?

## 질문
- 위 1~2 답과, Flow-aware 도구에 바라는 입출력 형태, bench 공동 측정 가능 여부를 달라.
```

### 3-R. coding-agent 세션 회신 (2026-06-29, 코드 대조 확인)

> coding-agent 트리/계약 기준 회신. 상세 사본:
> `coding-agent/docs/coordination-response-coding-agent-2026-06-29.md`. 권위:
> `plugin/agents/analyzer.md`(cks 소비), `skills/{root-cause-lifecycle,reproduce-first}`,
> `bench-orchestration`, `docs/VISION.md`(thesis). 아래 = §3 답 + **구현 충돌을 막기 위해
> 3자 결정이 필요한 갭**.

**소비 경계 정정 (§3 전제 수정)**
- coding-agent는 **CKV를 직접 소비하지 않는다 → CKS `cks_context_*` 13툴 표면만** 본다.
  CKV 15툴 중 `find_invariants`/`get_conventions`/flow-aware 4종은 **CKS가 단일
  `SemanticSearch`만 proxy하므로 현재 coding-agent에 안 닿는다**(= parity 갭, CKS §2-R와 정합).

**Q1 — 의존 도구/필드/계약**
- 의존 = CKS 13툴. 깨지면 안 되는 것: `get_for_task` 팩 + `guidance.*` / `ops.health.serviceable`
  의미(degraded⇒not-serviceable, 2026-06-15 정책) / `ops.freshness.indexed_head`(무효화 키) /
  `find_callers`·`impact_analysis` **완전성**(affected_sites 의존 — 불완전 = 조용한 부분수정 = bug-cycle).
- ⚠️ 신규(PR #38, v0.1.38): coding-agent가 graph 도구 호출 깊이를 **변경 복잡도로 게이트**
  (simple·local은 subgraph/concurrency/impact 축소·생략) → 호출 *믹스* 변화(계약 변경 아님).

**Q2 — bench 지표**
- bench = recall@k 아니라 **옳은 수정까지의 총비용**(Σ토큰 × bug-cycle + correctness). CKV recall과
  **상보**. 합의: **동일 핀 코퍼스 + 동일 태스크셋**으로 태스크 단위 cross-reference(병합 금지).

**★ 결정 필요 — 합의 전 구현하면 충돌/측정 무효 (coding-agent가 제기)**
- **D-1 (CRITICAL) 재인덱싱 커밋 핀.** coding-agent PR-77 통합 bench는 **버그-부모 `0bf2f4d1b`**
  에서만 유효하다. 그런데 §1-R(CKG) 매칭률 측정 그래프는 "go-stablenet 검증 코퍼스, 스키마 1.22
  재빌드"로 **커밋 미지정** → 두 reindex가 다른 커밋이면 PR-77 A/B가 무효.
  **제안: Phase 2 통합 재인덱싱을 `0bf2f4d1b` 하나로 통일**(CKG↔CKV 매칭률도 그 그래프에서 측정 →
  1회 재인덱싱으로 3자 커버). 다른 커밋이어야 할 제약이 있으면 알려달라.
- **D-2 schema ≥1.19 게이트.** 그 재인덱싱 그래프는 cache SchemaVersion **≥1.19(현 1.22)** 보장
  (canonical_id 값). coding-agent도 그 그래프에 cks를 배선해야 PR-77이 유효. CKG/CKS 합의에
  coding-agent 동일 채택 확인.
- **D-3 parity 분리.** §3의 "CKV 15툴 배선"을 둘로 나눠 추적: ① recall/rerank류 = **cks proxy 불요**
  (합의 확인) ② **flow/invariant = cks 표면 노출 필요(미구현)**. coding-agent 백로그도 분리한다.
- **D-4 flow/invariant 일정.** `get_invariant_enforcement`는 coding-agent의 **코드-도출 *구현 불변식***
  (하드코딩 아닌 cks 마이닝) enabler다. **Phase 2 deliverable로 스케줄**할지 **post-Phase-2 명시 defer**
  할지 결정 필요 — 안 정하면 coding-agent의 해당 기능이 무기한 블록. 3자 인터페이스 공동설계 출발점 =
  coding-agent repo 문서의 시그니처 표.
- **D-5 P3/R06 중복 확인.** ckg `#40 R06 precision change`가 graph-gap **P3(suffix-match resolver,
  ~23% recall)** 를 supersede하나? 중복작업 방지를 위해 CKG 확인 요청.

**schema_version 정책**: coding-agent는 1차 안전선으로 **`serviceable` fail-loud > silent fallback**
선호(호환 불가 스키마는 BLOCKED가 낫다 — confidently-wrong 금지). major-compare 파싱은 보조.

---

## 4. 협의 후 후속

- 세 세션 답변 수집 → 결정사항을 새 핸드오프(`session-handoff-2026-06-29.md`)에 통합,
  현 SoT(2026-06-15)는 `archive/`로 이동.
- 합의된 join key / 임베딩 모델·차원 / flow 도구 시그니처를 각각 ADR로 승격 검토.
