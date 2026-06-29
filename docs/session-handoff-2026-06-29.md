# Session Handoff — 2026-06-29

이 문서는 다른 머신·다른 세션에서 작업을 이어받기 위한 **현행 단일 진입점(SoT)**이다.
직전 핸드오프 [`archive/session-handoff-2026-06-15.md`](./archive/session-handoff-2026-06-15.md)는
PR #1~#6까지만 다뤄, 그 이후 머지된 #7~#15와 **CKG/CKV/CKS/coding-agent 4세션 협의
수렴**을 반영하지 못한다 → **archive로 이동**. 새 세션은 이 문서부터 읽는다.

> **요약:** (1) 2026-06-15 이후 견고화·기능 PR 9건(#7~#15) 머지. (2) CKV 남은 작업
> 다수가 CKG/CKS/coding-agent와 경계를 공유 → 4세션 협의로 **핵심 결정 7건 합의 완료**
> (커밋 핀, schema 게이트, parity 분리, flow Phase 2, 차원=실측후결정, 비전 가드레일).
> 상세 협의 기록은 [`coordination-prompts-2026-06-29.md`](./coordination-prompts-2026-06-29.md).

---

## 0. 환경 (2026-06-29)

| 항목 | 값 |
|------|-----|
| CKV repo | `/Users/wm-it-25_0220/Work/github/code-knowledge-vector` |
| Go module | `github.com/0xmhha/code-knowledge-vector` |
| CKV branch / HEAD | `main` / `0dbf1bd` |
| 빌드 | `make build/test/lint/fmt` (직접 go 명령 지양) |
| 자매 repo | code-knowledge-graph(CKG), code-knowledge-system(CKS), coding-agent |

`make test`의 `internal/embed/coreml` 1건 실패는 **환경적 baseline**(libtokenizers 부재).
CI는 명시적으로 제외(`abb5ae2`). 코드 회귀 아님. (개선 후보: Makefile도 CI처럼 coreml 제외.)

---

## 1. 2026-06-15 이후 머지 (PR #7~#15, 코드로 검증됨)

| PR | 커밋 | 내용 |
|----|------|------|
| #7 | `ac34a22` | ollama embed 요청 타임아웃(default 60s) + 응답 count 검증 |
| #8 | `460a718` | 모델 다운로드 네트워크 단계 타임아웃 바운드 |
| #9 | `c554cc5` | **CKG canonical_id 청크 상속(Phase 2)** — build-stable join key |
| #10 | `2d60405` | docs/corpus citation을 manifest DocsRoots로 해소(드롭 버그 fix) |
| #11 | `b99cd60` | stale 핸드오프 archive + docs index 갱신 |
| #12 | `485b644` | **임베딩 공간 identity 강제** — open 시 공간 불일치 거부 |
| #13 | `f15be9c` | **MaxInputTokens를 모델 레지스트리에서 도출** |
| #14 | `cd3f167` | manifest를 빌드 커밋 마커화(부분빌드 방지) |
| #15 | `44cc9e0` | 빌드 버전 기록 + model-cache 경로 단일화 |

> #12·#13은 임베딩 모델 교체(bge-m3 → Qwen3)를 **안전하게 만드는 사전 인프라**다
> (공간 혼용 차단 + 모델별 컨텍스트 윈도우 자동 반영).

---

## 2. 현재 CKV 노출 면 (코드 검증, 2026-06-29)

- **MCP 도구 15개** (`pkg/mcp/server.go`): semantic_search / keyword_search /
  vector_search / narrow_candidates / expand_in_file / find_invariants /
  get_conventions / explain_match / embed / rerank / related_changes / health /
  get_freshness / warmup / index. 모든 응답 `schema_version` 포함.
- **청크 종류 9** (`pkg/types/chunk.go`): symbol, function_split, file_header, doc,
  pr_background, pr_solution, commit_message, invariant, convention.
- **SQLite 마이그레이션 4** (`000_baseline`~`003_add_convention_stats`).
- **CLI**: build / query / reindex / eval / mcp / migrate / model(fetch·list·convert) /
  freshness / glossary.
- **파서 언어**: go / solidity / typescript / javascript / markdown (**proto 미파싱**).
- **임베더**: mock / ollama(`pkg/embed/ollama`) / bgeonnx / coreml.

---

## 3. 4세션 협의 수렴 (2026-06-29) — 핵심

> 전체 프롬프트·회신·결정은 [`coordination-prompts-2026-06-29.md`](./coordination-prompts-2026-06-29.md).
> CKG=§1-R/§1-R2, CKS=§2-R/§2-R2, coding-agent=§3-R, CKV=§3-R-CKV/§3-R-CKV-2, 비전=§5.

### 3.1 합의된 결정 7건

1. **재인덱싱 커밋 핀 = `0bf2f4d1b`** (PR-77 버그-부모, go-stablenet·test/pr-77 양쪽 존재).
   CKG가 `make eval-build-dbs LANG=auto`로 만든 **정본 graph.db**를 CKV/CKS가 가리킨다
   (각자 독자 빌드 안 함). 모델 축은 2회: **reindex-A(bge-m3 baseline)** + **reindex-B(Qwen3 A/B)**.
2. **schema ≥1.19 게이트** — canonical_id 값은 cache SchemaVersion ≥1.19(현 1.22)에서만 채워짐.
   CKV는 ckgalign 게이트를 *PRAGMA 컬럼-존재*에서 **manifest `schema_version >= 1.19`** 로 교체.
3. **B7 join key = canonical_id** (CKG ADR-0001 소유, CKV PR #9 상속, CKS `FindByCanonicalID`
   보유). 별도 정규화 규칙 불필요. 비심볼 노드는 node ID 폴백.
4. **parity 분리** — ① recall/rerank = cks proxy 불요(cks RRF 소유, ADR-003) ② flow/invariant
   = cks 표면 노출 필요(미구현, **CKS 소관**).
5. **flow/invariant 노출 = Phase 2 deliverable 확정** (defer 금지) → coding-agent H-가드레일 해금.
6. **임베딩 차원 = 실측 후 결정** — cks "1024 유지 선호(편의)" 철회. reindex-B에서
   1024-truncate vs full-dim 정밀도 실측, 이득 대비 비용으로 결정(**CKV 주관**). 측정 전 확정 금지.
7. **fail-loud** — 호환 불가 graph/모델 불일치는 silent degrade 금지, `ops.health.serviceable=false`.
   CKV는 PR #12로 이미 정합(공간 불일치 시 open 거부).

### 3.2 비전 정렬 (§5)

북극성 = "모호한 자연어 → 정확한 수정 위치를 토큰 효율적으로 → **옳은 수정까지 총비용 최소화**".
협의에서 *쉬운 합의*가 비전을 밀어내지 않도록 두 가드레일을 세웠고, **둘 다 합의로 닫힘**:
- **R1**: 차원을 편의로 1024 확정 금지 → 실측 후 결정 (결정 6).
- **R2**: flow/invariant 노출은 옵션이 아니라 *비전 구현 경로* → Phase 2 못 박음 (결정 5).

### 3.3 잔여 (측정 세부 2건)

- coding-agent "~23% recall" 측정 출처 지목 대기 (D-5 — CKG가 올바른 레버에 매핑하기 위함).
- CKG↔CKV 매칭률 **분모 정의** 3자 확인 — proto 제외 공유언어 스코프
  (CKV 제안: 분자=공유언어 CKV청크 중 CKG노드 정렬 수 / 분모=공유언어 CKV청크 총수).

---

## 3.5 DB 생성 일관성 + 사람-워딩 브리지 (2026-06-29 갭 분석)

> **문제 제기(사용자)**: ckv vector DB는 Jira 등 *사람이 쓴 워딩*을 이해하고 그것을
> *정확한 코드 키워드*에 연결해야 한다. 그런데 DB 생성에 **일관된 규칙이 미흡**하다.

**데이터 대조 (동일 커밋 `0bf2f4d1b`)**: 정본 `knowledge-data/pr-77/ckv` vs 세션 bare 빌드.

| 브리지 레이어 | pr-77 | 세션(bare) | 의미 |
|---|---|---|---|
| canonical_id/ckg_node_id (정밀 코드 심볼 키) | ✅ 13,549/15,303 | ❌ 0 | `--ckg` 누락 |
| 사람 도메인 문서(`.claude/docs`: wbft-consensus 등) | ❌ 0 | ✅ 256 | pr-77이 `--docs` 누락 |
| flow corpus(사람 인과 "현상→원인") | ❌ | ❌ | 양쪽 다 `--flow-corpus` 미적용 |

→ **두 DB 모두 불완전**. 사람-워딩→코드 브리지는 *모든 레이어*가 필요한데 ad-hoc 플래그로
빌드돼 매번 빠진다.

**검증된 정본 레시피 (= 일관된 규칙)** — analysis-test-3(go-stablenet@`0bf2f4d1b`)에서 실증:
```
ckv build --src <go-stablenet> --out <data> \
  --ckg <graph.db dir>            # canonical_id (정밀 심볼 키)   ← knowledge-data/pr-77/ckg
  --files-from gstable-files.json # 빌드소스 allowlist(130)       ← knowledge-data/pr-77/
  --docs <src>/.claude/docs       # 사람 도메인 문서              ← analysis-test-3/.claude/docs
  --flow-corpus corpus.jsonl      # 사람 인과 흐름(Phase B)       ← go-stablenet/.claude.backup*/docs/corpus
  --embedder ollama --model-name bge-m3   # (실측은 실모델 필요)
```
결과: 15,909 청크 = 심볼 14,273 + canonical_id 13,549 + 사람문서 222 + flow 112. **pr-77의 정밀
키 + pr-77이 빠뜨린 사람문서 + flow 인과**를 모두 가진 상위집합.

**누락 자료 위치 (github/ 하위)**:
- CKG 그래프(canonical_id): `knowledge-data/pr-77/ckg/graph.db` (schema 1.22)
- allowlist: `knowledge-data/pr-77/gstable-files.json`
- 사람 도메인 문서: `test/analysis-test-3/.claude/docs/*.md`
- flow corpus: `go-stablenet/.claude.backup.20260625_180533/docs/corpus/corpus.jsonl` (+SCHEMA.md)
- glossary 생성기: `code-knowledge-system/cmd/cks-glossary-gen` (**CKS 소유** — CKV `internal/glossary`는 미배선 standalone)

**조치**: 이 레시피를 **flow-ingest Phase E(build-profiles.yaml + scripts/build-knowledge.sh)** 로
코드화해 "한 스크립트 = 모든 레이어"를 보장(= 사용자가 요구한 일관된 알고리즘 규칙). glossary
배선은 CKS 소유라 3자 협의 항목.

---

## 4. 남은 작업 리스트 (협의 반영, 우선순위별)

### A. 즉시 착수 가능 (의존성 없음)
- [ ] **ckgalign 게이트 ≥1.19** (결정 2) — CKV 단독, 가장 먼저.
- [ ] **B10** parser fuzz/property 테스트.

### B. 측정 (커밋 핀 정본 그래프 준비 후)
- [ ] **CKG↔CKV 매칭률 실측** — reindex-A(`0bf2f4d1b`+bge-m3), 공유언어 스코프(§3.3).
- [ ] **bge-large/bge-m3 실모델 N=50 측정** (현재 mock baseline만).
- [ ] **PR-77 통합 bench** (coding-agent 주관, CKV recall 상보 cross-ref).

### C. 임베딩 모델 교체 (reindex-B)
- [ ] **Qwen3 A/B PoC** — `testdata/queries.yaml`·`why-queries.yaml`. 1024-truncate vs full-dim
  정밀도 실측 → 차원 결정(결정 6, CKV 주관).
- [ ] Qwen3 어댑터: query-prefix("Instruct:") 흡수 + MRL truncate 경로 + `knownDims` 합의.

### D. Flow-corpus (`plan-2026-06-16-flow-ingest.md`)
- [x] **Phase A 완료** (2026-06-29, commit `7158572`) — flow_step/flow_spine chunk kinds +
  FlowStepMeta/FlowSpineMeta/Branch/EnforcePoint 타입 + Chunk 필드(omitempty) +
  마이그레이션 004(flow_meta/enforced_at/provenance). go-stablenet@`0bf2f4d1b`
  (test/analysis-test-3)에서 신규 빌드(000–004, 19,605청크) + pr-77 인덱스 003→004
  업그레이드(백업·15,575행 보존·멱등) 양방향 검증.
- [x] **Phase B 완료** (2026-06-29, commits `72ef76f` 파서, `db6789a` 빌드통합) —
  `internal/flowcorpus` 파서(corpus.jsonl → flow_step/flow_spine/curated-invariant,
  edge는 graph-only skip, 형식이탈 warn+skip) + `--flow-corpus` 플래그 + store 컬럼
  배선(flow_meta/enforced_at/provenance, insert+양 scan 경로). step은 실코드 file:line,
  flow/invariant은 corpus.jsonl cite(corpus dir를 manifest DocsRoots에 추가 → citation 해소).
  실 corpus(255레코드) 검증: 18 spine + 78 step + 16 inv(step 1건 line누락 warn+skip),
  메타 round-trip·citation 해소 확인. **의미 검색 품질은 bge-m3 실모델 필요(mock은 구조만).**
- [ ] Phase C(file:line 정렬 강화) → E(빌드 오케스트레이션) → F(평가).
- [ ] 특히 **Phase D 4도구**(get_flow/expand_flow/find_branches/
  **get_invariant_enforcement**)는 결정 5로 Phase 2 노출 확정 → CKV가 안정 인터페이스 산출,
  CKS가 `cks_context_*` 표면 노출 (3자 공동설계). cks 기대 시그니처 초안: 입력 {심볼/지점,
  방향 up/down, budget} → 출력 {랭크된 flow 노드, 엣지 종류, invariant 위반 후보}.

### E. 코드 미구현 (기존 backlog)
- [ ] **#7(D.2)** LLM contextual prefix — throughput buffer 후 재구현.
- [ ] **A3** linux CI matrix / **A4** bge-code-v1 Qwen2 adapter.
- [ ] **PRR-1** full PR regression — throughput 보류.

### F. ADR 승격 (합의 후)
- [ ] canonical_id join / 임베딩 모델·차원(측정 후) / flow 시그니처. R1/R2 가드레일은
  Consequences에 측정 근거와 함께 명시.

---

## 5. 문서 드리프트 정리 (이 핸드오프로 갱신)

| 항목 | 정정 |
|------|------|
| A2 `ckv model fetch` | backlog "stub" 기재 → **구현됨**(PR #8/#15). 종결 처리. |
| ADR-006 | 핸드오프 §3-A "Proposed" → 실제 **Rejected**(2026-05-26). ADR-003 supersede 보류 항목 해소. |
| mcp-tools.md | 6월 빌드 플래그(`--docs/--files-from/--ckg`) 누락 — 보강 필요. |
| coreml 테스트 | Makefile도 CI처럼 제외하는 개선 후보(미해결). |

---

## 6. 권장 다음 세션 시작 순서

```bash
cd /Users/wm-it-25_0220/Work/github/code-knowledge-vector
git pull && make build && make test   # coreml 1건 FAIL은 정상

# 우선순위:
# 1. (이 세션) 핸드오프 통합 — 완료
# 2. ckgalign 게이트 ≥1.19 (즉시 착수, 의존성 없음)
# 3. CKG 정본 graph.db(0bf2f4d1b, LANG=auto) sha 수신 → 매칭률 실측
# 4. Qwen3 A/B PoC → 차원 결정 (reindex-B)
# 5. flow-ingest Phase A 착수 (스키마부터)
```

---

## 7. 핵심 파일 인덱스

- `pkg/types/chunk.go` — Chunk + 메타(`ckg_node_id`, `canonical_id`)
- `pkg/types/embed.go` — EmbeddingIdentity + Checksum (#12)
- `pkg/embed/ollama/adapter.go` — in-process ollama, MaxInputTokens 레지스트리 도출(#13)
- `internal/ckgalign/aligner.go` — canonical_id 상속(#9). **게이트 ≥1.19 수정 대상**
- `internal/build/builder.go` / `manifest/manifest.go` — manifest 커밋 마커(#14)
- `internal/store/sqlitevec/` — store + migrations 000~003
- `pkg/mcp/server.go` — MCP 15도구
- `pkg/ckv/ckv.go` — public Go API (Freshness 포함)
- `docs/coordination-prompts-2026-06-29.md` — 4세션 협의 SoT
- `docs/embedding-model-recommendation-2026-06-22.md` — Qwen3 추천

---

이 문서는 작업 진행 시 갱신한다. 큰 작업 진행 시 새 핸드오프를 만들고 이 파일은 archive.
