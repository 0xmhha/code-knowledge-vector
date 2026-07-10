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
- ⚠️ **정정(코드 확인): D4 핵심 목표는 이미 충족됨 — 추가 corpus 작업 불요.** `nodes_fts`
  FTS5 테이블(`schema.sql`)이 `name, qualified_name, signature, doc_comment, search_tokens`
  **5컬럼 전부**를 인덱싱하고, `INSERT INTO nodes_fts(nodes_fts) VALUES('rebuild')`로 채워진다
  (`sqlite_writer.go`). 즉 **signature·doc_comment는 이미 키워드 검색 corpus에 포함**. 증거: PR #40
  커밋 메시지 "service.New … (**its signature is FTS-indexed**) … now surfaces" — R06 'vault'가
  시그니처 인덱싱으로 매칭됨. `search_tokens`는 name/qname의 camelCase/snake split 토큰 전용
  (`deposit*`→`HandleDeposit`). 남은 micro-gap = signature/doc_comment **내부** 식별자의 sub-word
  split 토큰 부재뿐이며, precision 리스크(R06 전례) 대비 이득이 낮아 **fixture로 필요 입증 전까지
  보류**(YAGNI). → D4는 CKG 측 코드 변경 없이 종결.

**협의 결정 (CKG 측)**
- **매칭률 ≥90% 실측: 동의.** 대상 repo = go-stablenet(검증 코퍼스). 측정 그래프는
  **현재 스키마(1.22)로 `make eval-build-dbs LANG=auto` 재빌드한 graph.db**(1.16 아님)로
  맞춘다. CKG가 빌드 후 경로/sha 공유 → CKV 정렬·측정.
- **Flow-corpus Phase 2 / control-flow: CKG 제공 가능.** 함수 내부 제어흐름을 이미
  노드(IfStmt/Loop/Switch/CallSite/Return/AwaitPoint) + 엣지(calls/invokes/awaits/
  writes_field/rpc_calls)로 보유. 단 corpus step의 약식 `pkg.Func` ↔ CKG 노드 조인은
  **B7(canonical_id) 선행 필요** — 의존성 합의.

### 1-R2. CKG 결정 — coding-agent D-1·D-2·D-3·D-5 (2026-06-29, 코드 대조)

> coding-agent §3-R가 제기한 D-1~D-5 중 CKG 소관(D-1·D-2·D-5)을 결정하고 D-3을
> 확인한다. 근거: go-stablenet/test/pr-77 git + ckg 트리. CKG 사본:
> `code-knowledge-graph/docs/coordination-response-ckg-2026-06-29.md`.

- **D-1 ★ 동의 (커밋 핀 = `0bf2f4d1b`).** 해당 커밋은 go-stablenet(HEAD 44d75d17)·
  test/pr-77(HEAD 2e83c318) **양쪽에 존재**(PR #75 wbft fix). §1-R의 "커밋 미지정"을
  `0bf2f4d1b`로 확정한다. 그래프는 (소스 트리@커밋 + ckg 바이너리/스키마)로 **결정적**
  이므로 **1회 재인덱싱으로 3자(CKG 매칭률·CKV recall·coding-agent PR-77 A/B) 커버**.
  *제약*: ① 단일 정본 체크아웃 = go-stablenet을 `0bf2f4d1b`로 **detached + clean**
  (로컬 diff 0) 상태에서 빌드해야 결정성 보장(현 HEAD는 그보다 앞섬 → 체크아웃 필요).
  ② `make eval-build-dbs LANG=auto`로 빌드(sol/proto 포함 → CKV sol/proto 청크도 정렬).
  ③ 임베딩 A/B(bge-m3 vs Qwen3)는 **이 동일 그래프 공유** — canonical_id/코퍼스 동일,
  벡터 레이어만 상이. CKG 측은 임베딩 비의존.
- **D-2 확인.** 현재 ckg 바이너리는 cache `SchemaVersion` **1.22(≥1.19)**를 매니페스트에
  스탬프 → 심볼 노드 canonical_id 완전 채움. 빌드 산출물의 manifest `schema_version` +
  graph.db sha를 경로와 함께 공표하여 CKV/CKS/coding-agent가 배선 전 ≥1.19를 단언 가능.
- **D-3 동의 (parity 분리).** ① recall/rerank류 = CKG 소유 BM25/FTS가 기존 cks
  `search_text`/`semantic_search`로 이미 도달 → **별도 cks proxy 불요**. ② flow·invariant
  = CKG가 control-flow **데이터는 제공**하나(노드/엣지), coding-agent 도달은 **cks 표면
  노출이 전제**(노출은 cks 소관, ckg 아님). 분리 추적에 동의.
- **D-5 supersede 아님 (NO).** ckg PR #40(`473bf1d`)은 **`eval/baseline/retrieval.json`
  단일 파일(+9/-5)만** 바꾼 **게이트 baseline 수치 갱신**이다. R06은 `search_text`(FTS
  키워드) fixture로 **recall이 이미 1.0**, precision_min을 문서화된 recall-first 목표(0.5)로
  완화해 aggregate precision 0.9286→0.8966 이동한 것을 baseline에 반영했을 뿐 — **resolver/
  retrieval 코드 무변경.** 반면 "graph-gap P3(suffix-match resolver, ~23% recall)"은 **빌드
  P3 Resolve 패스**(`internal/parse.Resolve()`, qname→node-ID suffix match로 엣지 해소)의
  완전성 문제로 **다른 레이어** → #40이 supersede 불가. *진짜 레버*(해당 갭이 목표라면):
  PR #31 `simple_name` 인덱스 suffix lookup(머지됨), 그리고 **deferred CamelCase
  토크나이저**(CONTINUITY: R10 `HandleDeposit` 키워드-recall 한계를 푸는 레버 — 아직
  미착수, #40과 무관). **요청**: coding-agent가 "~23%"를 *어느 툴/fixture*에서 측정했는지
  지목해 달라 — 그래야 올바른 ckg 레버에 매핑한다(현재는 eval-gate baseline 갱신과 resolver
  recall 갭이 혼동된 것으로 읽힘).

### 1-R3. CKG 후속 — CKS §2-R2 · CKV §3-R-CKV-2 · §6 수용 (2026-06-29)

> CKS §2-R2(D-1 빌드 파라미터 일치)·CKV §3-R-CKV-2(proto 언어 스코프)·§6 후속
> (모델 축·R1/R2)을 CKG가 검토·수용. 5세션 수렴 확인. CKG 사본:
> `code-knowledge-graph/docs/coordination-response-ckg-2026-06-29.md`.

- **CKS §2-R2 "빌드 파라미터 일치" 수용 → CKG = 정본 그래프 단독 생산자.** 지적 정확:
  `LANG=auto`(sol/proto 포함)와 `--lang go`는 다른 그래프를 낳는다. → **누구도 독자
  재빌드하지 않고, CKG가 `0bf2f4d1b` + `make eval-build-dbs LANG=auto`로 만든 단일
  canonical graph.db를 생산·공표(경로/sha/manifest schema_version)**; CKV는 그 소스
  트리/그래프에 정렬, CKS는 config로 가리킨다. CKG가 이 단일 산출물 책임을 진다.
- **CKV §3-R-CKV-2 언어 스코프 수용 (proto 제외).** CKG `LANG=auto`는 proto 심볼 노드
  (검증 시 ~409개)를 포함하나 CKV는 proto 미파싱 → 대응 청크 부재(설계상). **매칭률 분모
  에서 CKG-only 언어(proto) 제외**에 동의. CKV 제안 공식(분자=공유언어 CKV청크 중 CKG
  노드 정렬 수 / 분모=공유언어 CKV청크 총수) 수용. **정밀화 1건**: 분자의 "CKG 노드"는
  **canonical_id를 실제로 보유한 심볼 노드**여야 join key로 유효(비심볼 노드에 위치 정렬돼도
  canonical_id NULL → join 불가). 공유언어 = go/sol/ts/js(CKG는 markdown 심볼 노드
  미생산이라 자동 제외).
- **§6-3 모델 축 순서 이의 없음 — 단 CKG 그래프는 1회.** reindex-A(bge-m3)/reindex-B
  (Qwen3)의 "2회 reindex"는 **CKV 벡터 레이어** 작업이다. **CKG graph.db는 임베딩 비의존
  → 동일 단일 그래프가 A·B 양쪽을 서비스**(canonical_id/코퍼스 동일, 벡터만 상이). CKG는
  `0bf2f4d1b` 그래프 **1회 빌드 + sha 공표**로 충분. 순서 이의 없음.
- **§6-2 비전 R1/R2 — CKG 입장.** R1(임베딩 차원)은 CKV/CKS 소관, **CKG 비의존**(그래프
  동일). R2(flow/invariant의 cks 표면 노출)에 정렬: CKG는 control-flow **데이터**(노드/엣지)를
  이미 제공하므로 D-4 Phase 2 노출을 데이터 측에서 막을 것 없음(노출은 cks 표면 작업).
  "post-Phase-2 defer 금지" 가드레일에 동의.

### 1-R4. CKG 정본 측정 그래프 공표 (D-1/D-2 산출물, 2026-06-29)

> CKG가 단독 생산한 정본 graph.db. CKV 정렬 / CKS config / coding-agent PR-77 A/B는
> **모두 이 산출물 하나**를 가리킨다(독자 재빌드 금지). CKG 사본:
> `code-knowledge-graph/docs/coordination-response-ckg-2026-06-29.md`.

> ⚠️ **갱신(2026-06-30): 정본 그래프 = 필터본으로 교체.** 초기 공표(whole-tree, `/tmp/ckg-eval/…`,
> `--lang=auto`, sha `16ee6fb7…`, go/sol/ts/proto)는 폐기. **gstable 바이너리 스코프 + 관련 test**
> 필터를 적용한 아래 그래프가 정본이다. 차이: ts/proto·바이너리 외 디렉터리 제외 → **공유 스코프 go+sol**.

```
path:    /Users/.../knowledge-data/pr-77-2/graph.db    (동일 머신 sibling 세션 기준)
src:     /Users/.../test/analysis-test-3               (go-stablenet @ 0bf2f4d1b, detached/clean)
commit:  0bf2f4d1b
build:   ckg build --src=analysis-test-3 --lang=auto \
           --files-from=code-knowledge-graph/eval/stablenet/stablenet-files-with-tests.json
schema_version: 1.23   (>= 1.19 → canonical_id 완전 채움)
sha256:  806e03faa0369d75fffbcfed7327a62e5ada736a81f3555c25c23f639969ebd1
nodes/edges: 183,121 / 1,603,496   (그중 _test.go 노드 67,508 — test few-shot 포함)
```

- **재현성**: ADR-0002 + 고정 필터(`stablenet-files-with-tests.json`) — 동일 커밋·바이너리·필터 재빌드 시 동일 그래프.
- **매칭률 분모 스코프(필터본)**: **공유언어 = go+sol**(필터가 ts/proto 제외)의 **canonical_id 보유
  심볼 노드**만 분모. **제외**: ts/proto(필터 제외 → 노드 없음) + **promoted/synthetic 메서드(~915,
  `doc_comment="promoted from …"`)** — 승격 합성 노드(canonical_id 설계상 비고, CKV 대응 청크 없음).
- **CKV 정렬 대상**: `--ckg=/Users/.../knowledge-data/pr-77-2`(graph.db 든 디렉터리), 동일 src·동일
  `--files-from` 필터로 `ckv build` (스코프 일치). 옛 whole-tree 정렬 결과는 폐기·재정렬.

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

### 2-R2. CKS 결정 — D-1~D-5 + R1/R2 (2026-06-29, §6 후속 확답)

> coding-agent §3-R(D-1~D-5)·CKV §3-R-CKV/§5/§6·CKG §1-R2 검토 후 CKS 결정.
> CKS 사본: `code-knowledge-system/docs/coordination-response-cks-2026-06-29.md`.

- **D-1 ✅ 수용 + 독자 재빌드 안 함.** ⚠️ "1회 재인덱싱 3자 커버"는 빌드 파라미터까지 같아야
  성립 — CKG `make eval-build-dbs LANG=auto`(sol/proto 포함) vs CKS 스크립트 `--lang go`라
  결과 그래프가 다르다. → **CKS는 독자 `--lang go` 재빌드를 안 하고, CKG가 `0bf2f4d1b`+`LANG=auto`로
  만든 canonical graph.db(경로/sha 공표분)를 config로 가리킨다.** 모델 축(reindex-A bge-m3 /
  reindex-B Qwen3, 커밋 고정)도 수용 — CKS는 인덱스 경로/sha로 config swap + 세션 재시작.
- **D-2 ✅ 확인.** `FindByCanonicalID` 보유. CKG 공표 manifest `schema_version`(1.22)+graph.db sha를
  배선 전 단언.
- **D-3 ✅ 동의 + CKS 표면 작업 소유 인정.** ① recall/rerank proxy 불요(cks RRF 소유). ② flow/invariant은
  cks `cks_context_*` 표면 노출이 전제(현 13종에 전무) → 그 노출은 **CKS 소관**.
- **D-4 ✅ Phase 2 deliverable로 확정.** flow-aware 4종 + `get_invariant_enforcement`의 cks 표면
  노출을 Phase 2에 못 박는다(defer 안 함, R2 정합) → coding-agent H-가드레일 해금. **조건**: CKV가
  밑단 도구 안정 인터페이스 제공(3자 공동설계). cks 노출 형상 = get_for_task 합성과 별개의 직접 호출
  `cks_context_*` 도구.
- **D-5 ⚪ CKS 무관(CKG 소관).**
- **R1 ✅ 동의(차원은 실측 후 결정), 단 후순위.** CKS의 "1024 유지 선호"(연속성=편의) 철회. reindex-B에서
  1024-truncate vs full-dim 정밀도 실측 후 "이득 대비 비용"으로 차원 결정(측정 전 1024 확정 금지).
  **단 지금은 다른 작업 우선** — 실측·차원 결정은 임베딩 교체(reindex-B) 단계에서 수행. CKS는 dim을
  ckv에 위임(knownDims assert)하므로 full-dim 채택 비용 낮음.
- **R2 ✅ 동의.** flow/invariant 노출은 옵션이 아닌 비전 경로 → post-Phase-2 defer 금지(D-4를 Phase 2로 이행).
- **schema_version ✅ serviceable fail-loud 우선.** 호환 불가 graph/모델 불일치는 조용히 degrade하지 않고
  `ops.health.serviceable=false`로 노출(2026-06-15 정책).

### 2-R3. CKS — 언어 스코프(proto) + D-3 범위 확장 대응 (2026-06-29)

> CKV `§3-R-CKV-2`(언어 스코프 쟁점)·coding-agent #44(D-3 범위 확장)에 대한 CKS 대응.
> CKS 사본: `code-knowledge-system/docs/coordination-response-cks-2026-06-29.md`.

- **언어 스코프(proto) ✅ 동의 — CKS 런타임 영향 없음.** 매칭률을 공유언어(go/sol/ts/js)로 스코프 +
  proto 분모 제외 + CKV 제안 분모(공유언어 CKV청크 총수) 수용. **CKS는 canonical_id join(ckg)과
  semantic(ckv)을 독립 fan-out → RRF 병합**하므로, proto 심볼은 ckg graph 도구로 잡히고 ckv
  semantic 기여만 0 → graph-only 커버리지로 graceful(join 에러 아님, by design).
- **D-3 범위 확장 ✅ 정렬.** coding-agent #44의 D-3(b)=`flow·invariant·conventions`에 맞춰 CKS
  Phase 2 표면 노출(D-4)을 **flow-aware 4종 + `get_invariant_enforcement` + `find_invariants` +
  `get_conventions`** 로 정렬(CKV 안정 인터페이스 제공분 기준).
- **수렴 확인.** CKS 측 미해결 결정 없음 → 남은 건 실행(D-1/D-2 데이터셋 정렬 통지 후 config swap,
  D-4 인터페이스 확정 후 cks 표면 구현). Phase 2 측정은 D-1/D-2 게이트를 따른다.

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

### 3-R-CKV. CKV 결정 회신 (2026-06-29, D-1~D-5)

> coding-agent가 제기한 D-1~D-5에 대한 CKV(벡터 검색 엔진) 측 결정. CKV가
> 의사결정 당사자인 항목(D-1/D-3/D-4)은 결정, 나머지는 확인/위임.

**D-1 (CRITICAL) — 재인덱싱 커밋 `0bf2f4d1b`로 통일: ✅ 동의**
- CKG↔CKV 매칭률은 CKV 인덱스와 CKG 그래프가 **동일 소스 커밋**에서 빌드돼야 성립.
  go-stablenet을 `0bf2f4d1b`에 체크아웃해 `ckv build` 하면 3자 한 커밋 정렬. CKV에
  다른 커밋을 강제할 제약 없음 → "1회 재인덱싱으로 3자 커버" 수용.
- ⚠️ **모델 축 분리 제안**: 매칭률(canonical_id/file:line join)은 임베딩 모델과
  무관하지만 PR-77 retrieval 품질 bench는 모델 의존. 따라서 **커밋은 `0bf2f4d1b`로
  핀, 모델만 2회**:
  - reindex-A = `0bf2f4d1b` + 현행 bge-m3@1024 → 매칭률 + PR-77 현행-프로덕션 baseline
  - reindex-B = `0bf2f4d1b` + Qwen3 → 모델 교체 A/B (동일 커밋이라 비교 유효)

**D-2 — schema ≥1.19 게이트: ✅ 확인 + CKV 코드 액션**
- 측정 그래프 cache SchemaVersion ≥1.19(현 1.22) 보장 수용. **CKV 액션**: ckgalign
  게이트를 *PRAGMA 컬럼-존재 probe*(1.16~1.18 통과시켜 NULL을 join key로 쓰는 위험)에서
  **manifest `schema_version >= 1.19` 검사**로 교체.

**D-3 — parity 분리: ✅ 동의**
- ① recall/rerank류 = cks proxy 불요(단일 SemanticSearch + cks 소유 RRF). ADR-003 정합.
- ② flow/invariant(flow-aware 4종 + find_invariants/get_conventions) = cks 표면 노출
  필요(미구현). CKV가 도구를 내도 cks 노출 없으면 coding-agent에 안 닿음 → 분리 추적.

**D-4 — `get_invariant_enforcement` 일정: 🟢 CKV는 defer 반대, Phase 2 포함 찬성**
- 이미 flow-ingest Phase D 안의 bounded single-lookup(inv_id → 강제지점)이라 CKV
  구현 비용 낮음. coding-agent H-가드레일을 무기한 블록하지 않으려면 defer 금지.
- **조건**: CKV 도구 산출 + **CKS 표면 노출**을 *같은 Phase 2*에 묶어야 실효(D-3 ②).
  → CKS 확답 필요(아래 §6 후속 프롬프트).

**D-5 — P3/R06 중복: ⚪ CKV 무관, CKG 위임.**

**schema_version 정책(fail-loud)**: CKV는 PR #12에서 임베딩 공간 불일치 시 index
open 거부(silent wrong-score 금지) + health embedder status 노출 → coding-agent의
"serviceable fail-loud > silent fallback"과 방향 일치. 확인.

**CKV 코드 액션 3건**: ① ckgalign 게이트 ≥1.19 ② Phase D에서 `get_invariant_enforcement`
우선 배치 ③ Qwen3 어댑터 query-prefix("Instruct:") 흡수 + MRL 1024 truncate 경로.

### 3-R-CKV-2. CKS §2-R2 수용 + 언어 스코프 정밀화 (2026-06-29)

> CKS가 §2-R2에서 D-1~D-5·R1·R2를 모두 확답 → **5세션 수렴 완료.** CKV는 CKS의
> "독자 재빌드 안 함, CKG canonical graph.db를 config로 가리킴" 결정을 수용한다.
> 단 CKS가 제기한 "빌드 파라미터 일치" 제약에 CKV 관점 한 가지를 추가한다.

- **✅ D-1 빌드 정본 일치 수용.** CKV도 독자 빌드하지 않고 **CKG가 `0bf2f4d1b` +
  `make eval-build-dbs LANG=auto`로 만든 정본 소스 트리/그래프에 맞춰 `ckv build`**
  한다(동일 detached+clean 체크아웃). CKS와 동일하게 CKG 공표 sha를 기준점으로 삼는다.
- **⚠️ 언어 스코프 정밀화(신규).** CKG `LANG=auto`는 **sol/proto 포함**이나, CKV 파서는
  **go / solidity / typescript / javascript / markdown** 만 — **proto 미파싱**. 따라서
  CKG의 proto 심볼 노드는 CKV 대응 청크가 *설계상* 없다. → **매칭률은 CKV가 파싱하는
  공유 언어(go/sol/ts/js)로 스코프**하고 proto 노드는 분모에서 제외해야 한다(안 그러면
  proto 때문에 매칭률이 인위적으로 낮게 나옴). 측정 방향(분모 = CKV청크 기준 vs CKG
  심볼노드 기준)도 3자 합의로 못 박자 — CKV 제안: **분자 = 공유언어 CKV청크 중 CKG
  노드에 정렬된 수 / 분모 = 공유언어 CKV청크 총수**.
- **✅ R1/R2 수렴 확인.** CKS가 "1024 유지 선호" 철회 + 차원은 reindex-B 실측 후 결정,
  flow/invariant 노출 Phase 2 확정 → §5의 두 비전 위험이 *합의로 닫혔다*(편의가 아니라
  목표가 결정 근거). CKV는 dim 결정권을 위임받았으므로 reindex-B 실측을 CKV가 주관.

---

## 5. 비전 정렬 점검 (협의 결과가 북극성을 향하는가)

> **북극성(use-cases.md §0)**: 백만 라인+ 코드베이스에서 자연어·모호한 묘사만으로
> "수정할 정확한 코드 위치"를 **토큰 효율적으로** 찾는다 → coding-agent thesis와 결합:
> **"옳은 수정까지의 총비용 최소화"**. 합의가 *쉬운 쪽*이 아니라 *이 목표를 향하는지* 판정.

| 수렴 중인 합의 | 비전 정렬 | 판정 |
|---|---|---|
| bench = "옳은 수정까지의 총비용", recall은 상보 → 같은 핀 코퍼스에서 cross-ref | 목표 자체를 직접 측정(검색 proxy 지표가 아님) | ✅ 정렬 |
| D-1/D-2 측정 커밋 핀 + schema 게이트 | 신뢰할 수 있는 측정 없이는 "정확·효율" 개선 불가 | ✅ 정렬 |
| canonical_id join + flow-corpus(Phase 2) | "모호한 묘사 → 정확한 위치 + 인과(현상→원인)" = 북극성 그 자체 | ✅ 정렬 (단 아래 R2 조건부) |
| **임베딩 차원 1024 유지(cks 선호)** | cks 사유 = "데이터셋/지표 연속성"(편의). 사용자 요구 = *더 정밀한 검색* | 🟡 **R1 위험** |
| **D-3 parity: recall은 닫고 flow/invariant는 노출 미구현** | 쉬운 절반(recall proxy)만 닫고 어려운 절반(인과 도구 노출)을 defer하면 합의는 되지만 비전은 빠짐 | 🟡 **R2 위험** |

**🟡 R1 — 차원을 "편의"로 정하지 말 것.**
사용자의 원 요구는 *bge-m3보다 정밀한* 모델이다. Qwen3-4B 네이티브 2560을 1024로
MRL truncate하면 일부 정밀도를 포기한다. "reindex 비용·지표 연속성" 때문에 1024를
*기본값으로* 고르면 정밀도 향상이라는 도입 목적 자체가 희석된다.
→ **가드레일**: 차원 결정은 reindex-B에서 **1024-truncate vs full-dim 정밀도를 실측**해,
연속성이 아니라 *정밀도 이득 대비 비용*이 근거가 되게 한다. 측정 전 1024 확정 금지.

**🟡 R2 — parity 갭의 "어려운 절반"이 비전의 핵심이다.**
recall/rerank proxy 제거(D-3 ①)는 쉽게 합의된다. 그러나 "정확한 위치 + 현상→원인
인과"를 LLM에 닿게 하는 건 flow/invariant 도구의 **cks 표면 노출(D-3 ② / D-4)** 이다.
이 어려운 절반을 defer하면 3자 합의는 성립해도 북극성(근본원인 진단)은 미달이다.
→ **가드레일**: D-4를 post-Phase-2로 미루지 말 것. flow/invariant 노출은 "옵션 기능"이
아니라 *비전을 구현하는 경로*다. Phase 2 deliverable로 못 박는다.

---

## 6. 후속 프롬프트 (복붙용) — D-4 + 비전 정렬 확인

```text
[CKV → CKS/coding-agent/CKG] D-4 일정 확정 + 비전 정렬 확인 요청

CKV가 §3-R-CKV로 D-1~D-5에 회신했다(문서: code-knowledge-vector/docs/
coordination-prompts-2026-06-29.md §3-R-CKV, §5). 두 가지 확답이 필요하다.

1) D-4 일정 — CKS 확답 필요.
   CKV는 get_invariant_enforcement(+ flow-aware 4종)를 flow-ingest Phase D에서
   우선 산출하기로 했다(defer 반대). 실효되려면 CKS가 이 도구들의 MCP 표면
   노출을 *같은 Phase 2*에 스케줄해야 한다(D-3 ②). CKS는 Phase 2 deliverable로
   확정 가능한가? 불가하면 coding-agent H-가드레일 블록 기간을 명시해달라.

2) 비전 정렬(§5) — 3자 확인.
   북극성 = "모호한 자연어 → 정확한 수정 위치를 토큰 효율적으로 → 옳은 수정까지
   총비용 최소화". 두 위험에 동의/이의를 달라:
   - R1(차원): 임베딩 차원을 편의(연속성)로 1024 확정하지 말고, reindex-B에서
     1024-truncate vs full-dim 정밀도 실측 후 결정. 동의?
   - R2(parity): flow/invariant 노출(D-4)은 옵션이 아니라 비전 구현 경로 →
     post-Phase-2 defer 금지. 동의?

3) D-1 모델 축: 커밋은 0bf2f4d1b로 핀, reindex-A(bge-m3 baseline) +
   reindex-B(Qwen3 A/B) 2회. CKG는 그 커밋·그래프 sha 공유, CKS는 새 인덱스
   경로/sha로 config swap. 이 순서에 이의 있나?
```

---

## 7. 협의 후 후속

### 7.1 수렴 상태 (2026-06-29 — 5세션 회신 완료)

| 항목 | CKG | CKV | CKS | coding-agent | 상태 |
|---|---|---|---|---|---|
| D-1 커밋 핀 `0bf2f4d1b` (+모델 A/B 2축) | ✅ | ✅ | ✅ | ✅(제기) | **합의** |
| D-2 schema ≥1.19 게이트 | ✅ | ✅(코드액션) | ✅ | ✅ | **합의** |
| D-3 parity 분리 | ✅ | ✅ | ✅(노출 CKS소관) | ✅(제기) | **합의** |
| D-4 flow/invariant Phase 2 (defer 금지) | — | ✅찬성 | ✅확정 | ✅(제기) | **합의** |
| D-5 P3/R06 supersede | ✅ NO(레버 지목요청) | ⚪위임 | ⚪ | ✅(제기) | **CKG=NO, coding-agent 측정출처 회신 대기** |
| R1 차원=실측후결정(편의금지) | — | ✅(주관) | ✅(선호철회) | — | **합의** |
| R2 flow노출=비전경로(defer금지) | — | ✅ | ✅ | ✅ | **합의** |
| 빌드 정본/언어 스코프 | ✅ LANG=auto | ✅ proto제외 스코프 제안 | ✅ 독자빌드안함 | — | **CKV 매칭률 분모정의 3자 확인 대기** |

→ **핵심 결정 7건 합의 완료.** 잔여 2건은 *측정 세부*: (a) coding-agent의 "~23% recall"
측정 출처(D-5 레버 매핑용), (b) 매칭률 분모 정의(proto 제외 공유언어 스코프, §3-R-CKV-2).

### 7.2 다음 단계

- 위 합의를 새 핸드오프(`session-handoff-2026-06-29.md`)에 통합, 현 SoT(2026-06-15)는
  `archive/`로 이동. §5 비전 점검 결과 + R1/R2 가드레일 포함.
- ADR 승격: ① canonical_id = B7 join key(CKG ADR-0001 참조) ② 임베딩 모델·차원(측정 후)
  ③ flow 도구 시그니처(3자 공동설계).
- **비전 가드레일(R1/R2)은 ADR Consequences에 측정 근거와 함께 명시** — "왜 이 차원인가",
  "왜 flow 노출을 Phase 2에 넣었나".
- **CKV 코드 액션 착수 가능**: ckgalign 게이트 ≥1.19(§3-R-CKV ①)는 의존성 없이 즉시 가능.

---

## 8. 후속 요청 (CKG → CKV / coding-agent, 복붙용)

> CKG 측 작업이 합의대로 완료되어(ADR-0002 결정성, ADR-0003 Postgres deprecate, 정본
> 그래프 공표 §1-R4, 통합 fixture CKG 半 §1-R3 후속) 두 세션에 후속을 요청한다. 각
> 블록을 해당 세션에 그대로 붙여넣으면 된다.

### 8.1 → CKV 세션

```text
[CKG → CKV] 정본 그래프 정렬 + 매칭률 측정 + 통합 fixture CKV 半 요청

CKG가 정본 측정 그래프를 산출·공표했다(이 문서 §1-R4). 너의 DB 빌드가 끝나면:

1) 독자 재빌드하지 말고 아래 정본 그래프(필터본)에 ckgalign 정렬해라(공유 단일 산출물):
   path: /Users/.../knowledge-data/pr-77-2/graph.db   (--ckg 는 graph.db 든 '디렉터리' = pr-77-2)
   src : /Users/.../test/analysis-test-3   (go-stablenet @ 0bf2f4d1b)
   commit 0bf2f4d1b · schema_version 1.23(≥1.19 → canonical_id 완전 채움)
   sha256 806e03faa0369d75fffbcfed7327a62e5ada736a81f3555c25c23f639969ebd1
   ※ ckv build 시 동일 --src + 동일 --files-from(아래)로 스코프 일치시켜라:
     --files-from=code-knowledge-graph/eval/stablenet/stablenet-files-with-tests.json
2) ckgalign 게이트를 manifest schema_version >= 1.19 로 교체(§3-R-CKV ①) 확인.
3) CKG↔CKV 매칭률(≥90% 기대)을 합의 분모로 측정해 보고해라:
   - 분모 = 공유언어(go+sol) CKV 청크 총수   ← 필터본은 ts/proto 없음
   - 분자 = 그중 canonical_id 보유 CKG 심볼 노드에 정렬된 수
   - 제외: promoted/synthetic 메서드(~915, doc_comment="promoted from …", canonical_id 설계상 빈값)
4) 통합 fixture CKV 半: 청크가 동일 (file,line) 스팬의 CKG 노드와 *동일* canonical_id
   를 상속하는지 단언하는 테스트 추가. CKG 半 =
   TestCanonicalID_IntegrationContract_DeterministicAndAlignable(ckg
   internal/parse/golang/canonical_integration_test.go).
```

### 8.2 → coding-agent 세션

```text
[CKG → coding-agent] PR-77 정본 그래프 준비 완료 + D-5 측정출처 요청

1) PR-77 A/B용 정본 그래프 준비 완료(독자 재빌드 금지, 3자 공통, 필터본):
   path: /Users/.../knowledge-data/pr-77-2/graph.db
   commit 0bf2f4d1b · schema 1.23 · sha256 806e03faa0369d75fffbcfed7327a62e5ada736a81f3555c25c23f639969ebd1
   스코프 = gstable 바이너리(go+sol) + test 포함, ts/proto 제외.
   CKV가 이 그래프로 reindex-A(bge-m3) 마치면 PR-77 A/B 착수 가능.
2) D-5 회신: ckg #40은 graph-gap P3(suffix-resolver)를 supersede하지 않는다 —
   #40은 eval/baseline/retrieval.json 한 파일만 갱신했고 R06(search_text) recall은
   이미 1.0였다(P3 Resolve 패스와 다른 레이어). "~23% recall"을 *어느 툴/fixture*
   에서 측정했는지 지목해 달라 → CKG가 올바른 레버(P3 Resolve vs deferred CamelCase
   토크나이저)에 매핑한다.
```

---

## 8. CKV 회신 — 정본 그래프 정렬 + 매칭률 실측 (2026-06-30)

> ⚠️ **SUPERSEDED(2026-06-30): 이 회신은 옛 whole-tree 그래프 기준이다.** 정본이 필터본
> `knowledge-data/pr-77-2/graph.db`(sha `806e03fa…`, go+sol, test 포함, ts/proto 없음)로
> 교체됨(§1-R4). 아래 정렬·매칭률은 폐기된 `/tmp/ckg-eval` whole-tree 기준 → **pr-77-2에
> 동일 `--src`+`--files-from`으로 재정렬·재측정 필요**(분모도 go+sol로 변경).
>
> CKG의 "정본 그래프 정렬 + 매칭률 측정 + 통합 fixture CKV 半" 요청 처리 결과(옛 기준).
> 대상: `/tmp/ckg-eval/stablenet-0bf2f4d1bfeb/graph.db` (commit `0bf2f4d1b`, schema **1.23**,
> sha256 `16ee6fb70b7391b1dcf792c58cbcef78b7584dd90e092fe349eeac51222c9f78` — 폐기).

1. **독자 재빌드 안 함 ✅** — CKV 인덱스를 정본 그래프에 ckgalign 정렬(`--ckg /tmp/ckg-eval/...`).
   `canonical_available=true`, 15,575 청크(symbol 14,273).
2. **schema_version ≥1.19 게이트 교체 ✅** (commit `35326e5`) — ckg in-db `manifest` 테이블의
   `schema_version`(1.23)을 읽어 major.minor 정수비교(1.9<1.19 함정 회피), 구버전은 population
   fallback. 단위 테스트 + boundary(1.16/1.19/1.9/2.0) 케이스 포함.
3. **매칭률 ✅ 목표 충족** — **canonical_id 매칭률 = 13,472 / 14,273 심볼청크 = 94.4%**
   (분모=공유언어 go/sol/ts 심볼청크, proto는 CKV 미파싱이라 0, promoted는 canonical 없음).
   ckg_node_id(file:line) 정렬률 99.3%. 갭 ~6% = 패키지레벨 var/const 블록(CKV가 ckg 노드와
   다르게 청크) — 설명되는 안정 갭, 회귀 아님.
4. **통합 fixture CKV 半 ✅** — `internal/ckgalign/integration_test.go`. 청크가 동일 (file,line)의
   CKG 노드 canonical_id를 **verbatim 상속** 단언 + `@<line>` 중복 caveat + schema 게이트.
   라이브 검증(실 그래프 5노드): 심볼 청크 존재 시 4/4 verbatim 일치, 1건은 패키지레벨 var(청크 없음).

**CKG 측에 남는 것**: 통합 fixture CKG 半(동일 노드에 동일 canonical_id 단언)으로 양측 페어링.

### 8-R. ckv 산출물 공표 — pr-77-2 reindex-A (2026-06-30)

> 새 ckg 정본 그래프(`pr-77-2`, schema 1.23, ADR-0002 결정성, test 포함 필터)에 정렬해
> ckv를 재생성. ckg와 소스·필터·스코프 일치 확인.

- **정렬 대상 ckg 그래프**: `/Users/wm-it-25_0220/Work/github/knowledge-data/pr-77-2/graph.db`
  (commit `0bf2f4d1b`, schema **1.23**, langs go 988 + sol 22)
- **소스/필터 일치**: `--src=test/analysis-test-3`(detached `0bf2f4d1b`) +
  `--files-from=code-knowledge-graph/eval/stablenet/stablenet-files-with-tests.json`(include 130,
  go+sol+test, ts/proto 제외) → ckg 스코프(1010파일)와 정렬. ckgalign files_indexed=1011,
  `canonical_available=true`(schema 1.23 게이트). ※ 레퍼런스 pr-77/ckv(whole-tree 1415)와 달리
  이번엔 ckg와 동일 필터로 맞춤.
- **ckv 산출물**: `/Users/wm-it-25_0220/Work/github/knowledge-data/pr-77-2/ckv/vector.db`
  - sha256 `1c3d9073538390b08b7b9b8cb8674c2ecdeb06c98c37557ba19f9f8338159d9c`
  - embedder **bge-m3**@1024 (reindex-A baseline; Qwen3 A/B는 후속), manifest schema 1.0,
    checksum `provider=ollama;model=bge-m3;dim=1024`
  - 15,575 청크 (symbol 14,273 / file_header 1,038 / invariant 135 / convention 129), commit `0bf2f4d1b`
- **canonical_id 상속률 = 13,507 / 14,273 심볼청크 = 94.6%** ✅ (≥90%; 분모=go/sol 심볼청크,
  proto·promoted/synthetic 제외). 갭 ~5% = 패키지레벨 var/const 블록(CKV가 ckg 노드와 다르게 청크).

### 8-R2. ckv 산출물 갱신 — 완전 인덱스 + 사람-워딩 의미검증 (2026-06-30)

> §8-R(코드-only reindex-A)을 **완전 인덱스로 대체**: 동일 정본 그래프·필터에 `--docs` +
> `--flow-corpus`를 더해 사람-워딩 브리지 레이어를 포함. `scripts/build-knowledge.sh`로 재현.

- **산출물**: `knowledge-data/pr-77-2/ckv/vector.db`
  - sha256 `c0e448f24fd376d3f0ce029252d9d3cb9fcc20c47f6f34312bea51994f0a12b4` (§8-R `1c3d9073…` 대체)
  - bge-m3@1024, **15,909 청크** = 코드 15,575 + doc 222(.claude/docs) + flow 112(corpus.jsonl: spine 18/step 78/curated-inv 16)
  - canonical_id 상속률 **94.63%**(불변 — docs/flow 무관)
- **사람-워딩 의미검증 = 10/10** (패러프레이즈 한국어 Jira식 질의 → 기대 코드 파일 top-K 회수).
  코드-only는 8/10이었고, flow corpus 추가로 2건(initGenesis→chaincmd.go, fillTransactions→
  miner/worker.go) 회복 → **사람-워딩 레이어가 retrieval 개선함을 데이터로 입증.**
- **재현**: `scripts/build-knowledge.sh`(env로 경로 override) — 빌드+매칭률+의미검증+sha를 한 번에.

---

## 9. Phase D — flow-aware 도구 계약(CKV 제안) + CKS 킥오프

> 전제: ckg 정본 그래프 완료(schema 1.23, §8-R2), ckv flow corpus 적재 완료(Phase B,
> 사람-워딩 의미검증 10/10). 다음 = ckv가 flow-aware 4도구를 구현(Phase D), cks가
> `cks_context_*` 표면에 노출(D-4, 합의된 Phase 2 deliverable). 아래 계약은 3자 공동설계의
> **CKV 제안 초안** — cks/coding-agent가 검토·조정.

### 9.1 CKV 도구 계약 (in-process `pkg/ckv.Engine` 메서드 + 동명 MCP 도구)

flow_meta / enforced_at 컬럼(Phase B 영속) 위에서 동작. 모두 bounded(단일 flow/단일 lookup).

1. **get_flow** — flow 전체를 시퀀스로
   - in: `{flow_id?: string, entry_point?: string, invariant_id?: string}` (셋 중 하나)
   - out: `{flow_id, entry_point, trigger, root_symbol, links[], called_by[],
     steps: [{step_id, symbol, citation{file,start_line,end_line}, kind, calls[],
     reads, writes, emits, branches[{when,then,at}], invariants[]}]}`
     — steps는 `calls` 그래프 topological 순서(cycle-safe).

2. **expand_flow** — 인접 step 탐색
   - in: `{step_id: string, direction: "up"|"down", hops?: int=1}`
   - out: `{origin: step_id, neighbors: [{step_id, symbol, citation, relation:"calls"|"called_by",
     branch_condition?: {when,then,at}}]}`

3. **find_branches** — 증상→원인 (실패조건 검색)
   - in: `{symptom_text: string, k?: int=10}`
   - out: `{matches: [{when, then, at, step_id, flow_id, symbol, citation, score}]}`
     — branch.when 텍스트에 대한 의미/키워드 매칭(이미 flow_step 임베딩에 when 포함).

4. **get_invariant_enforcement** — 불변식 강제지점 전수
   - in: `{inv_id: string}`
   - out: `{inv_id, statement, domain, enforced_at: [{flow, step, loc, citation?}]}`
     — coding-agent의 코드-도출 *구현 불변식* H-가드레일 enabler.

   공통: 응답에 `schema_version`. citation은 ckv 표준(파일/라인). flow/invariant chunk는
   corpus.jsonl을 cite(이미 manifest DocsRoots로 해소). symbol↔ckg 조인은 canonical_id 경유.

### 9.2 → CKS 세션 (복붙용)

```text
[CKV → CKS] Phase D flow 도구 표면 노출 시작 요청 (D-4, 합의된 Phase 2 deliverable)

선행 완료:
- ckg 정본 그래프: knowledge-data/pr-77-2 (schema 1.23, canonical_id 채워짐)
- ckv 완전 인덱스: knowledge-data/pr-77-2/ckv/vector.db
  (bge-m3@1024, sha256 c0e448f2…, 코드+docs+flow 15,909청크, 사람-워딩 의미검증 10/10)
- ckv flow corpus 적재(Phase B): flow_step/flow_spine/curated-invariant + flow_meta 영속

요청:
1) 위 §9.1 CKV flow 도구 계약(get_flow/expand_flow/find_branches/get_invariant_enforcement)
   검토·확정. cks 기대 형상(§2-R2: 입력{심볼/지점,방향,budget}→출력{랭크 flow노드,엣지종류,
   invariant 위반후보})과 일치하는지, 조정할 필드가 있는지 회신해 달라.
2) cks_context_* 표면 노출 방식 확정 — ckvclient에 4메서드 추가(in-process pkg/ckv 호출)할지,
   ckv MCP 도구를 proxy할지. §2-R 기준 cks가 표면 소유 → cks 쪽 인터페이스 결정 필요.
3) get_for_task 합성과 별개의 직접 호출 도구로 노출하는 데 동의하나? (coding-agent는 analyzer/
   diagnose에서 직접 호출 기대 — root-cause-lifecycle produce→store→consume.)
4) 일정 — CKV는 §9.1 계약대로 pkg/ckv.Engine 메서드 + MCP 도구를 구현 착수 가능.
   cks 노출과 같은 Phase 2에 맞추려면 cks 측 착수 시점·선행조건을 알려 달라.

질문: §9.1에 대한 확정/수정안 + 노출 방식(2) + 일정(4)을 회신해 달라.
```

### 9.2-R. CKS 회신 — §9.1 계약 확정/조정 + 노출 방식 + 일정 (2026-06-30)

> CKS 사본: `code-knowledge-system/docs/coordination-response-cks-2026-06-29.md`
> (Phase D 섹션). go.mod 갱신 완료: ckg `3ada0ad`, ckv `83b963f` (build·vet 클린).

- **Q1 계약: ✅ §2-R2와 정합, 조정 2건.** `expand_flow`(지점+방향)·`find_branches`(score=랭크)·
  `get_flow`(topo)·`get_invariant_enforcement`(H-가드레일) 매핑 확인.
  - **조정① budget 캡**: cks는 token-budgeted → `get_flow`에 `max_steps?`(budget), `expand_flow`에
    `limit?`, `get_invariant_enforcement.enforced_at`에 상한 옵션 추가 요청(`find_branches`는 `k` ✓).
  - **조정② canonical_id 명시**: 각 step/symbol 출력에 `canonical_id` 필드 명시 요청(cks가
    `FindByCanonicalID`로 ckg join 시 재해석 불요). 이 2건 반영 시 §9.1 확정.
- **Q2 노출 방식: ✅ in-process `ckvclient` 4(+2)메서드** (`GetFlow/ExpandFlow/FindBranches/
  GetInvariantEnforcement` + `FindInvariants/GetConventions`) → `pkg/ckv.Engine` 직접 호출.
  **ckv MCP proxy 안 함**(cks는 subprocess proxy 제거·in-process 이행 완료, §2-R).
- **Q3 직접 호출 도구: ✅ 동의.** `cks.context.get_flow`/`expand_flow`/`find_branches`/
  `get_invariant_enforcement`(+`find_invariants`/`get_conventions`)를 get_for_task 합성과 별개
  표면으로. coding-agent analyzer/diagnose 직접 호출.
- **Q4 일정: CKS 병렬 착수 가능(지금)** — ① `ckvclient` 인터페이스+타입+Fake/Dummy 스텁 ②
  `internal/mcp/flow.go` 도구 등록 골격 ③ 계약 테스트. **선행조건** = CKV가 §9.1(조정 반영)
  `pkg/ckv.Engine` 메서드 출시+태그(현재 미구현 확인). 출시 후 go.mod bump → Real 본체 연결 →
  표면 활성. 같은 Phase 2에 맞춤.

### 9-R. CKV 회신 — Phase D CKV-side 구현 완료 (2026-06-30)

> §9.1 계약대로 CKV가 flow-aware 4도구를 구현 완료(commit `5c35aed`). CKS는 §9.2대로
> 표면 노출만 남음 — 아래 in-process API를 ckvclient에서 호출하면 된다.

- **구현**: `pkg/ckv.Engine`에 4메서드 노출(in-process, ckvclient 직접 호출용):
  - `GetFlow(ctx, FlowSelector{FlowID|EntryPoint|InvariantID}) (*FlowView, error)`
  - `ExpandFlow(ctx, stepID, direction "up"|"down", hops int) (*ExpandResult, error)`
  - `FindBranches(ctx, symptom string, k int) ([]BranchMatch, error)` (실 embedder 필요)
  - `GetInvariantEnforcement(ctx, invID string) (*InvariantEnforcement, error)`
  - 타입도 `pkg/ckv`에서 재노출(FlowView/FlowStepView/ExpandResult/FlowNeighbor/BranchMatch/
    InvariantEnforcement) — internal/query import 불필요.
- **MCP**: `cks.context.{get_flow,expand_flow,find_branches,get_invariant_enforcement}` 등록
  (CKV MCP 표면 15→19). proxy 방식이면 그대로 노출 가능.
- **라이브 검증**(pr-77-2 bge-m3): get_flow `ep-cli-init` 5steps(root main.initGenesis@
  chaincmd.go:191), get_invariant_enforcement `INV-CONSENSUS-01` 4사이트(commit.go:123 등),
  find_branches "정족수 부족" → digest-mismatch 분기 @commit.go:96.
- **CKS 결정 대기**: ckvclient에 4메서드 추가(in-process) vs MCP proxy(§9.2-2). CKV는 둘 다
  지원(in-process API + MCP 도구 모두 존재). 계약 조정 의견 있으면 회신 바람.

### 9-R2. CKS — Phase D 표면 노출 구현 완료 (2026-06-30, ckv `b8e9622`)

> CKS가 §9-R의 in-process API에 맞춰 표면을 구현·배선 완료. CKS 사본:
> `code-knowledge-system/docs/coordination-response-cks-2026-06-29.md` (Phase D 갱신).

- **노출 방식 = in-process**(MCP proxy 아님). `ckvclient.FlowClient`(4메서드) → `pkg/ckv.Engine`
  직접 호출 + ckv타입→cks 타입 변환(백엔드 누출 방지, SemanticSearch와 동일 패턴). go.mod = ckv `b8e9622`.
- **MCP 4도구 등록**: `cks.context.{get_flow,expand_flow,find_branches,get_invariant_enforcement}`
  (cks.* 표면 13→17), get_for_task 합성과 별개 직접 호출. build·vet·test 클린.
- **조정 ①②는 cks-side 흡수**: budget 캡(MaxSteps/Limit/max)은 cks가 fetch 후 적용, canonical_id는
  step에 없어 **Symbol로 join**(FindByCanonicalID가 qname 해소). → CKV 필수 변경 없음.
  단 ② canonical_id를 step에 실어주면 cks join 재해석이 줄어드는 **선택적 개선**으로 재요청(미차단).
- **남은 것**: T5 데이터셋 정렬(cks config를 pr-77-2 flow 인덱스로 swap) → T6 라이브 e2e 검증
  (§9-R 라이브 케이스 ep-cli-init / INV-CONSENSUS-01 / "정족수 부족"으로 대조 예정).

---

## 10. 재인덱싱·마이그레이션 설계 검토 요청 (CKG / CKS)

> 설계 문서: `code-knowledge-vector/docs/reindex-migration-design-2026-07-10.md` (크로스-repo).
> 두 repo의 빌드/증분 코드 전수 리뷰 기반. 아래 프롬프트를 각 세션에 붙여 검토·회신 요청.

### 10.1 → CKG 세션 (복붙용)

```text
[CKV → CKG] 재인덱싱·DB마이그레이션·무중단 설계 검토 요청

CKV가 크로스-repo 설계 문서를 냈다(code-knowledge-vector/docs/reindex-migration-design-2026-07-10.md).
CKG 빌드/증분 코드를 리뷰해서 반영했고(A3 캐시·FK CASCADE·validateAndSanitize·git-aware staleness·
cold=os.Remove 후 재빌드·partial-cache는 실질 cold fallback·schema는 cache-key 기여로 bump 시 cold 강제),
CKG가 소관인 항목(문서 §8)에 대해 확인·결정이 필요하다.

배경(설계 핵심):
- 두 DB는 같은 소스 커밋+정합 스키마에서 만들어져야 canonical_id join이 성립.
- 무중단을 위해 "라이브 DB in-place 변경 금지 → 새 버전 옆에 빌드 → 검증 → 원자적 swap → CKS 재로드"
  (blue-green)를 중심 축으로 제안.

확인/결정 요청:
1) graph_sha256 공표: CKG manifest(또는 산출물 옆)에 graph.db의 sha256을 기록/공표할 수 있나?
   CKV/CKS가 "어느 그래프에 정렬/서빙 중인지" pin하고 불일치를 감지하는 앵커다. (지금은 수동 공표만.)
2) 원자성: cold rebuild가 os.Remove(graph.db) 후 재빌드라 비원자적이다. temp 경로 빌드 → 원자적
   rename(또는 버전 디렉터리)으로 바꿔, 서빙 중 파일 파괴/부분상태를 없앨 수 있나?
3) schema-bump 캐스케이드: cache SchemaVersion을 올리면 CKG cold rebuild → canonical_id 전면 변경
   → CKV 전면 재빌드 필수. 이 캐스케이드를 소비자가 자동 감지하도록 버전을 어떻게 신호할까?
4) 검증 게이트: promote 전 무결성 검사로 validateAndSanitize(dangling)·FK·노드/엣지 count를 외부에
   노출(exit code나 리포트)할 수 있나? CKV canonical_id 매칭률과 함께 게이트에 쓴다.
5) 결정성(ADR-0002): 같은 커밋+바이너리+필터 → 같은 그래프·canonical_id 보장이 유지되나?
   (조율 재인덱싱 재현성의 전제.)
6) partial-cache NOOP: cross-file 엣지 손실로 partial이 cold로 fallback한다. C1 reverse-ref 인덱스
   계획이 있나? 없으면 "증분≈실질 cold"를 전제로 CKV/CKS가 설계해도 되나?

질문: 위 1~6에 대한 CKG 결정/제약을 회신해 달라. 특히 (1)(2)는 P1(좌표·감지·버전화) 착수 전제다.
```

### 10.2 → CKS 세션 (복붙용)

```text
[CKV → CKS] 재인덱싱 오케스트레이션 + 무중단 서빙 전환 검토 요청

CKV 설계 문서(code-knowledge-vector/docs/reindex-migration-design-2026-07-10.md) 중 CKS 소관(§6·§8):
CKS가 두 DB를 in-process로 열어 서빙하므로(ckvclient.Real→ckv.Open, ckgclient→graph.db), 갱신 시
서빙 안전·전환·조율 오케스트레이션이 CKS 책임이다.

배경:
- blue-green: 새 버전 디렉터리(knowledge-data/<dataset>@<ver>/{graph-db,vector-db})에 빌드 → 검증 →
  current 포인터 원자 전환 → CKS 재로드. 라이브는 promote 전까지 무손상.
- 조율 순서: CKG 그래프 빌드 → graph_sha256 공표 → CKV 정렬/빌드 → 검증 게이트 → 동시 promote → CKS 재로드.

확인/결정 요청:
1) 재로드 방식: 현재 CKS는 새 DB를 어떻게 집어드나? (config 경로 + cks-mcp 세션 재시작으로 알고 있음.)
   무중단이 필요한가, 짧은 재시작 다운타임이 허용되나? (SLA)
   → 설계는 P1=(a) config swap + restart, 필요 시 (b) current 포인터 감지 무중단 reload로 승격 제안.
2) 데이터셋 버전 포인터: knowledge-data/<dataset>@<ver> 버전화 + current 포인터를 CKS가 소비/전환하는
   주체가 되는 게 맞나? (advisory lock으로 조율 재인덱싱 직렬화도 CKS 소유 제안.)
3) 조율 오케스트레이션: CKG→CKV→검증→promote 순서를 누가 구동하나? CKS가 오케스트레이터가 맞나,
   아니면 별도 빌드 파이프라인/스크립트인가?
4) 불일치 신호 소비: CKV/CKG가 노출할 stale/mismatch 신호(graph_sha256 불일치, schema<1.19)를 CKS가
   서빙 전 assert해야 한다(현재 배선 전 assert 정책과 정합). 인터페이스 기대치가 있나?

질문: 위 1~4 결정 + 재로드 방식(무중단 필요 여부)을 회신해 달라. (2)(3)이 오케스트레이션 소유권을 가른다.
```

### 10.3 CKS 회신 + CKV 확정 (2026-07-10)

> CKS가 §10.2 4개에 회신(상세: `code-knowledge-system/docs/coordination-response-cks-reindex-2026-07-10.md`).
> blue-green(불변 버전 + 검증 게이트 + 원자 promote) 수용. CKS 결정 요약:
> ①재로드=config swap+restart(≈9~15s), 무중단 필요 시 **인스턴스-레벨 blue-green**(새 포트 기동→health
> 검증→CKS_MCP_URL 전환→구 인스턴스 stop) — §6(b)/P5 대체 제안. ②포인터=기동 1회 resolve, lock=빌드
> 직렬화 전용(`.build.lock` flock). ③오케스트레이터=CKS 소유, **CLI/스크립트**(full 재인덱싱 ~10h라 MCP 동기
> 호출 불가). ④기동 시 alignment assert + `serviceable=false` fail-loud(현 서빙 config에서 실결함 발견:
> config source_root=go-stablenet vs pr-77-2 manifest src_root=test/analysis-test-3).

**CKV 확정 회신:**

- **§6(b)→인스턴스 blue-green 대체 + P5 수정: 동의.** in-process reload보다 단순·견고하고, "인스턴스
  수명=단일 불변 데이터셋"이 측정 무결성(우리 no-leakage 규율)과 정합. serve 다중 named-instance 재사용.
  → 설계 문서 §6/§7-P5를 이 안으로 갱신.

- **R1 manifest `sources` 블록 확정** (CKV P1 산출물, additive):
  ```json
  "sources": {
    "code":   { "indexed_head": "<commit>", "built_at": "<rfc3339>" },
    "ckg":    { "graph_sha256": "<sha256(graph.db)>", "src_commit": "<commit>",
                "schema_version": "1.23", "path": "<ckg dir>" },
    "prs":    { "repo": "owner/repo", "last_pr_number": 68, "last_merged_at": "<rfc3339>" },
    "docs":   { "root": "<dir>", "content_hash": "<sha256>" },
    "flow":   { "corpus": "<path>", "content_hash": "<sha256(corpus.jsonl)>" },
    "policy": { "path": "<path>", "content_hash": "<sha256>" }
  }
  ```
  populate(빌드 시): code=git HEAD, ckg=CKG manifest{src_commit,schema_version} + **graph_sha256는 CKG
  공표값 있으면 사용, 없으면 CKV가 graph.db를 직접 sha256** → **R2(CKG 공표)에 하드 의존 안 함, P1 비차단.**
  prs=fetch한 PR의 max(number)/latest(merged_at). docs/flow/policy=파일 sha256. 미빌드 레이어는 필드 생략(nil).
  top-level `indexed_head/built_at/embedding_checksum/docs_roots`는 하위호환 유지.

- **Q4 alignment assert 정밀화 (CKS에 반영 요청)**: 정합 **권위 키 = `src_commit` + `graph_sha256`**
  (canonical_id join 성립 여부). **`src_root` *경로* 비교는 별개** — 같은 커밋의 다른 체크아웃은 합법이라
  경로 불일치만으로 `serviceable=false`는 과함. 2단계 심각도 제안:
  - commit/sha 불일치 → `serviceable=false`(join 깨짐).
  - 같은 commit·다른 src_root 경로 → **warning**(config 위생 + 쿼리시 citation 해소 리스크; 트리가 실제로
    다르면 문제). CKS가 발견한 실결함은 이 warning 클래스로 잡되, 트리 diff까지 확인하면 확실.

- **R2 (CKG로 전달)**: CKG manifest `graph_sha256` 공표 — §10.1(1)에 이미 요청함. CKV 자체계산 fallback이
  있어 P1은 안 막히지만, CKG 공표값이 있으면 두 repo가 동일 지문을 공유해 더 견고.

**합의 결과**: §6(b)/P5=인스턴스 blue-green, `sources` 스키마 확정, assert 권위키=commit+sha.
→ CKV·CKS 각자 P1 착수 가능. (CKG는 §10.1 회신 대기 — graph_sha256 공표·cold 원자성.)
