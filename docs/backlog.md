# CKV 작업 Backlog

> **문서 버전**: 1.1
> **작성일**: 2026-05-20 (1.1 갱신: 2026-05-21)
> **목적**: CKV의 *모든* 추적 가능한 작업을 한 곳에 모은 broader backlog. `retrieval-quality-roadmap.md §12` (retrieval quality 우선순위 10 items) 와 `featurelist.md §0.1` (S1 implementation status master 60+ subsections) 가 *다른 차원의 일부 view*만 제공하므로, 본 문서가 두 view 위의 *통합 work tracking*을 담당.
> **연관 문서**:
> - 직접 우선순위: [`retrieval-quality-roadmap.md §12`](./retrieval-quality-roadmap.md)
> - 구현 상태: [`featurelist.md §0.1`](./featurelist.md)
> - D1 PoC follow-ups 와 사용자 결정 source 원문은 본 backlog 작성 이후 정리됨 (git history 참조).

> **역할 분리**:
> - **본 문서 (backlog.md)** — 추적 항목 inventory + 진행 상태 + 카테고리별 정리 ⬅️ *new SoT*
> - **roadmap §12** — retrieval quality slice의 *우선순위 + 의존성 의사결정*
> - **featurelist §0.1** — sub-section 단위 *implementation status* (P0/P1/P2 분류와 함께)

---

## 1. Executive Summary

본 backlog 는 7 개 카테고리, **총 45 추적 항목** (2026-05-21 기준 — F / G 신설).

| 카테고리 | 항목 수 | 차원 | 액션 책임 |
|---|---|---|---|
| **A** — D1 PoC 잔여 follow-ups | 5 (A1 ✅) | 임베딩 인프라 + 측정 | CKV (D2 일부 포함) |
| **B** — featurelist §0.1 ⚠️ 부분구현 | 10 | 기능 보강 | CKV (S1 finalize) |
| **C** — S2 이관 (작업 시점 미정) | 11 (C10 ✅) | 신기능 | CKV (S2 milestone) |
| **D** — 외부 의존 (CKV scope 외) | 4 | 통합 의존 | CKS / 외부 milestone |
| **E** — 문서 신설 | 3 | 문서화 | CKV |
| **F** — cks dogfood follow-ups | 7 (모두 ✅) | API + 운영 | CKV (완료) |
| **G** — PR-regression follow-ups | 5 (PRR-2~5 ✅, PRR-1 보류) | 평가 인프라 | CKV |

**현재 진행 중**: Group α 완료 (#1 / #3 / #5 부분 ✅), Group β 진입 가능. F / G 두 그룹은 2026-05-20~21 세션에서 전면 처리. 자세히 §4 마스터 표.

**가장 시급한 항목** (사용자 결정 또는 작업 의존성 trigger):
- **B9** Secret 회피 패턴 (`.env` / `*.pem` / `*.key`) — 사용자 글로벌 보안 룰 직접 매칭
- **B6** Error model 5 종 — API 완성도, `pkg/ckv` 표면에 명시
- **A5** fixture N=34 → N=50+ — 모든 retrieval 측정의 baseline
- **Roadmap #8** `ckv reindex` 도입 — S1.5 승격, Phase B 도입 *전* architectural 전제

---

## 2. 카테고리별 항목

### A. D1 PoC 잔여 follow-ups

출처: 2026-05-19 backlog 작성 세션의 D1 PoC follow-up 정리 (commit history 참조).

| ID | 작업 | 우선순위 | 상태 | 비고 |
|---|---|---|---|---|
| **A1** | Phase 0b — CoreML compile I/O error 해결 | High | ✅ 2026-05-20 | Root cause: `ModelFormat=NeuralNetwork` (ORT default) 의 silent FP32 → FP16 cast 가 ANE compile 단계에서 실패. fix: `CKV_COREML_MODEL_FORMAT=MLProgram` + `CKV_STATIC_SHAPES=1` 조합. commits `66bdefc` / `9e71fa6` / `292db4a` / `9ff43e6`. throughput 회복은 부분 (0.74 c/s CPU pure; ANE 친화 모델 도입은 HF 차단으로 보류 — `embeddinggemma-300m` ModelConfig 등록만 완료, 모델 파일 미보유). |
| **A2** | D1-FU-4 `ckv model fetch` CLI 완성 | Medium (D2) | ⏳ stub | `cmd/ckv/model.go` 현재 `"not yet implemented"` 반환. `hf` 의존 제거 위함. |
| **A3** | D1-FU-5 linux/amd64+arm64 CI matrix | Low | ⏳ | cross-build with `libonnxruntime` + `libtokenizers`. macOS 외 OS 미검증. |
| **A4** | D1-FU-6 bge-code-v1 Qwen2 adapter | Mid (D2) | ⏳ | ModelDim=1536, ModelMaxInput=32k, last-token pooling, Qwen2 ONNX export (~5GB). 모델 이미 `~/.cache/ckv/models/bge-code-v1/` 다운로드됨. code retrieval 정확도 잠재 우위. |
| **A5** | D1-FU-7 fixture corpus 확장 (N=34 → N=50+) | Medium | ⚠️ N=34 도달 | 현 testdata/sample 4 파일 + markdown 1 파일로 N=34가 한계. corpus 자체를 확장 (추가 sample 파일 또는 second sample repo 도입) 필요. |

### B. featurelist §0.1 ⚠️ 부분구현 (S1 finalize 후보)

출처: [`featurelist.md §0.1`](./featurelist.md) 의 ⚠️ 마킹 항목.

| ID | 작업 | 우선순위 | 상태 | 비고 |
|---|---|---|---|---|
| **B1** | §1.3 큰 함수 sliding window split | Mid | ⏳ | head-truncate만 적용. AST top-level statement 단위 split 필요. = Roadmap §12 #10 (Phase A). plan §5.4 약속. |
| **B2** | §3.4 Filter — commit_hash filtering | Low | ⏳ | metadata 저장만, 실제 filter 미연결. incremental snapshot 용도. |
| **B3** | §4.3 Snippet density 3-tier ladder | Mid | ⏳ | 현재 `budget_tokens`만. full body / signature+5lines / signature only 3단계 ladder 미구현. |
| **B4** | §5.2 인용 실재성 — commit_hash 매칭 | Low | ⏳ | file existence만, commit_hash mismatch 미검증. stale citation 감지 약함. |
| **B5** | §8.2 Envelope — `trace_id`/`dry_run` | Low | ⏳ | `budget_tokens`만. trace_id 일관성, dry_run mode 미구현. observability에 영향. |
| **B6** | §8.4 Error model 6종 중 5종 미구현 | Mid | ⏳ | `IndexUnavailable`만. `FreshnessStale`/`BudgetExceeded`/`CitationNotFound`/`SanitizeFailed`/`PolicyError` 미구현. |
| **B7** | §10.2 Symbol ID 호환 정규화 규칙 | Low | ⏳ | `ckg_node_id` 필드만. CKG와 join 위한 normalize 규칙 미합의. CKG 측과 협업 필요. |
| **B8** | §11.2 공통 플래그 (`--log-level`, `--profile`) | Low | ⏳ | `--json`만 일관 적용. log-level 환경변수, profile output 미구현. |
| **B9** | §15.2 Secret 회피 패턴 (.env / *.pem) | High (보안) | ⏳ | gitignore 호환만. `.env`/`*.pem`/`*.key` 명시 제외 패턴 미구현. **사용자 글로벌 보안 룰 의식 필요**. |
| **B10** | §16.4 Fuzz / Property tests | Low | ⏳ | parser fuzz 미구현. 랜덤 입력 panic 부재 확인. |

### C. S2 이관 (작업 시점 미정, 추적만)

출처: [`plan-S1-ckv.md §13`](./plan-S1-ckv.md) + [`featurelist.md §0.1`](./featurelist.md) "❌-S2".

> S1 stable 출시 *후* S2 milestone에서 일괄 진행 예정. 본 backlog는 *결정 누수 방지*용 추적.

| ID | 작업 | featurelist 출처 | 비고 |
|---|---|---|---|
| **C1** | `ckv reindex` (incremental, UC-V2) | §6.2 | **S2 → S1.5 승격** (사용자 결정 2026-05-19). Phase B 도입 *전* 필요. = Roadmap §12 #8. |
| **C2** | `internal/sanitize/` 전체 (UC-V13) | §9 (5 sub-section) | external caller 도입 시. plan §13. |
| **C3** | `internal/memory/` Working Memory (UC-V9/14) | §7 (5 sub-section) | `cks.memory.*` planned. plan §8.2. |
| **C4** | `ckv serve` HTTP API | §12 | MCP 외 추가 transport. |
| **C5** | `cks.ops.request_refresh` | §6.3 | freshness change 직후 부분 reindex. |
| **C6** | `cks.ops.stats` | §8.5 | chunk count, last index time 노출. |
| **C7** | `cks.context.get_context_for_task` | §8.1 | sanitize 의존. |
| **C8** | UC-V4 Pattern Similarity (code-as-query) | §4.2 | input이 코드 스니펫일 때. |
| **C9** | Embedding cache (per-text disk cache) | §2.4 | rename/이동 시 재임베딩 회피. |
| **C10** | JavaScript parser 신설 | §1.2 | ✅ 2026-05-21 (commit `e4977fa`). `internal/parse/javascript/` 신설, tree-sitter-typescript binding delegation. `.js` / `.jsx` / `.mjs` / `.cjs` 인덱싱. S2 이관 결정에서 S1 으로 끌어옴 (TS parser 패턴 재사용 비용 작음). |
| **C11** | Prometheus metrics exporter | §14.2 | latency/cache hit/sanitize counter. |

### D. 외부 의존 (CKV scope 외, 의존성만 추적)

| ID | 작업 | 책임 | 비고 |
|---|---|---|---|
| **D1** | RRF fusion + `cks-mcp` 통합 binary | CKS repo | = Roadmap §12 Phase E. CKV는 `pkg/mcp.Server.Underlying()` 표면만 노출 — 이미 완료. |
| **D2** | `cks.context.query_code` multiplex tool | CKS repo | CKV + CKG hybrid acceptance #3. |
| **D3** | mTLS auth | S6 보안 | featurelist §8.3. CKV는 caller cert SAN ↔ envelope `caller` 일치 검증만. |
| **D4** | CKG 측 BM25 corpus 확장 (qname + signature + doc-comment) | CKG repo | 현재 CKG가 이미 구현 (`pkg/bm25/scorer.go`). hybrid retrieval 정확도에 영향. CKG 측 진행도 의존. |

### E. 문서 신설

| ID | 작업 | 우선순위 | 상태 | 비고 |
|---|---|---|---|---|
| **E1** | `docs/ARCHITECTURE.md` 신설 | P1 | ⏳ | featurelist §18.2. 4-Layer 위치 + 모듈 도식. 현재 plan-S1-ckv.md가 일부 역할. |
| **E2** | `docs/SCHEMA.md` 신설 | P1 | ⏳ | featurelist §18.3. chunk metadata schema + working memory entry + sanitize_report. 현재 plan-S1-ckv.md 에 분산. |
| **E3** | ADR 디렉토리 신설 (`docs/adr/NNN-*.md`) | Mid | ⏳ | markdown parser 는 #3 에서 완료했으나 *실제 ADR 문서* 자체 미작성. 첫 ADR 후보: ADR-001 (sqlite-vec 선택), ADR-002 (bge-large-en-v1.5 pivot), ADR-003 (BM25 dual-track), ADR-004 (ckv reindex S1.5 승격), ADR-005 (CoreML MLProgram + static shapes — A1 결정사항). |

### F. cks dogfood follow-ups (모두 closed)

출처: 2026-05-19 cks dogfood 실행에서 발견된 ckv-consumer 측 gap. 모두 ckv-side 작업으로 종결.

| ID | 작업 | 상태 | 비고 |
|---|---|---|---|
| **CKV-1** | `cks.context.semantic_search` hang 재현 | ✅ 2026-05-20 (commit `42bb7f2`) | ckv-side 재현 불가. 8 concurrent queries 포함 모든 시나리오 정상 응답. cks-side composer / transport 측 후속 조사로 이전. 검증 스크립트: `testdata/mcp-repro/`. |
| **CKV-2** | public Go API (`pkg/ckv`) | ✅ 2026-05-20 (commit `7aa08d9`) | `ckv.Open` / `SemanticSearch` / `Warmup` / `Manifest` / `Close` + `MockEmbedder` factory. 9 unit tests. cks 측 subprocess proxy 우회 가능. |
| **CKV-3** | consumer-oriented docs | ✅ 2026-05-20 (commit `acaff74`) | `docs/embedder-integration.md` 신설 (~300 lines, 8 sections). |
| **CKV-4** | transport-closed root cause | ✅ 2026-05-20 (commit `7f2fab8`) | mcp-go `WithRecovery()` 미설치로 handler panic 시 process crash → stdio close. fix: `NewMCPServer` 호출에 `server.WithRecovery()` 추가. `TestServerRecoversFromHandlerPanic` 로 검증. |
| **CKV-5** | embedder warm-up | ✅ 2026-05-20 (commit `9474b4e`) | `Engine.Warmup(ctx)` (internal/query, pkg/ckv) + `cks.ops.warmup` MCP tool. 응답: `{ready, duration_ms, embedder}`. |
| **CKV-6** | health endpoint embedder status | ✅ 2026-05-20 (commit `bd8f701`) | nested `embedder: {name, dimension, status, provider, model_dir}` + `index: {chunk_count, last_built_at, indexed_head}`. status = `ready` / `stub` / `unavailable`. backward compat (flat fields 유지). |
| **CKV-7** | response schema versioning | ✅ 2026-05-21 (commit `a45654b`) | 모든 MCP tool response 의 top-level 에 `schema_version: "1"` inject. `pkg/mcp/server.go::jsonResult` 차원 자동 — 새 tool 추가 시 누락 불가능. 4 tool table-driven test. |

### G. PR-regression follow-ups

출처: 2026-05-19 PRR-1 첫 시도 후 도출된 5 개 follow-up.

| ID | 작업 | 상태 | 비고 |
|---|---|---|---|
| **PRR-1** | bgeonnx PR-regression 재측정 | ⛔ 보류 | throughput buffer 부족 (현재 0.74 chunks/s 기준 stable-net repo single PR eval ~ 6 h 이상). D1-FU-8 진짜 해결 (ANE 친화 모델 도입) 시 재개. |
| **PRR-2** | `--pr-runs N` flag 내장 | ✅ 2026-05-20 (commit `b7ff1b7`) | mean ± sample std + pass_rate + error_count. CI gate 은 mean judge_score 기준. JSON N=1 backward compat. 6 unit tests. |
| **PRR-3** | fixture 확장 (PR #70 외) | ✅ 2026-05-21 (commit `46eb0fe`) | 1 entry → 4 entries (pr70 + pr69 / 72 / 74). 모두 base_sha local clone 에서 resolvable. notes 에 각 PR 특성 명시. |
| **PRR-4** | agent prompt 강화 | ✅ 2026-05-21 (commit `a799562`) | "hints = evidence, hint 밖 file 은 concrete reason 있을 때만". invented paths 방지. |
| **PRR-5** | judge rubric 재설계 | ✅ 2026-05-21 (commit `a799562`) | 0.8 anchor 에 *valid alternative solution* 포함. 0.5 에 "right files wrong change" false-credit 방지 anti-rule. |

---

## 3. Status Legend

| 마크 | 의미 |
|---|---|
| ✅ | 완료 |
| 🔄 | 진행 중 (본 세션 또는 다른 세션) |
| 📌 | 다음 작업 예정 (선행 조건 충족) |
| ⏳ | 대기 (선행 조건 또는 결정 대기) |
| 🔴 | 신규 발견 / urgent |
| ⚠️ | 부분 완료 / blocker |
| ⛔ | 차단 |
| 📝 | 결정만 완료, 코드 미진행 |

---

## 4. 마스터 진행 상태 추적 (Roadmap §12 + 본 backlog 통합)

> Roadmap §12 retrieval-quality 항목 (#1~#10) + 본 backlog 항목 (A~E) 을 합친 통합 상태. 본 표가 *single source of truth*.

| Roadmap# / Backlog ID | 작업 (요약) | 카테고리 | 상태 | 마지막 갱신 | 참조 |
|---|---|---|---|---|---|
| #1 | fixture N=34 + why-queries.yaml | 측정 인프라 | ✅ | 2026-05-19 | commit `f1a8ac9` + `ad804be` |
| #2 | PR #70 회귀 테스트 모듈 | 평가 | ✅ | 2026-05-21 | 모듈 commit `fddecda`~`c36a9fb`. follow-up PRR-2~5 모두 closed (G 그룹) |
| #3 | markdown corpus 인덱싱 | corpus | ✅ | 2026-05-21 | commit `4a5dc3a` + chunk_kind="doc" 분류 commit `1ce9577` |
| #4 | PR/commit history corpus (Phase C) | corpus | ⏳ | — | #2 후 (git/gh fetch 모듈 재사용) |
| #5 | batch + CoreML EP (D1-FU-8) | 인프라 | ⚠️ 부분 | 2026-05-20 | A1 root cause 해소 (MLProgram + static shapes). throughput 0.74 c/s (CPU pure) 도달. 30 c/s 목표는 ANE 친화 모델 필요 — HF 차단으로 보류. |
| #6 | 룰 기반 contextual prefix (Phase D.1) | retrieval | 📌 | — | Group β, Group α 후 진행 가능 |
| #7 | LLM contextual prefix (Phase D.2) | retrieval | ⏳ | — | #5 진짜 해결 후 (throughput buffer) |
| #8 | `ckv reindex` 도입 (S1.5 승격) | 인프라 | 📝 | 2026-05-19 | commit `c0689d7` 결정만, 코드 미진행 |
| #9 | multi-granularity (Phase B) | retrieval | ⏳ | — | #8 권장 (full rebuild 비현실) |
| #10 | sliding window split (Phase A) = B1 | retrieval | ⏳ | — | 큰 함수 비율 측정 후 |
| **A1** | CoreML compile I/O error 해결 | 인프라 | ✅ | 2026-05-20 | commits `66bdefc` / `9e71fa6` / `292db4a` / `9ff43e6` |
| **A2** | `ckv model fetch` CLI (D1-FU-4) | DX | ⏳ | — | D2 scope |
| **A3** | linux/amd64+arm64 CI (D1-FU-5) | 인프라 | ⏳ | — | — |
| **A4** | bge-code-v1 Qwen2 adapter (D1-FU-6) | retrieval | ⏳ | — | D2 scope, MRR 잠재 향상 |
| **A5** | fixture N=34 → N=50+ corpus 확장 (D1-FU-7) | 측정 인프라 | ⚠️ | 2026-05-19 | testdata/sample 자체 확장 필요 |
| **B1**~**B10** | featurelist §0.1 ⚠️ 부분구현 10건 | 기능 보강 | ⏳ | — | S1 finalize 후보. B9 / B6 가 가장 시급 |
| **C1**~**C9, C11** | S2 이관 10건 | 신기능 | ⏳ | — | S2 milestone |
| **C10** | JavaScript parser | 신기능 | ✅ | 2026-05-21 | commit `e4977fa` (S2 → S1 끌어옴) |
| **D1**~**D4** | 외부 의존 4건 | 통합 | ⏳ | — | CKS / 외부 milestone |
| **E1**~**E3** | 문서 신설 3건 | 문서화 | ⏳ | — | E3 ADR 디렉토리 시작 권장 (ADR-005 까지 후보 확보) |
| **F (CKV-1~7)** | cks dogfood follow-ups | API + 운영 | ✅ | 2026-05-20~21 | 7 / 7 closed. commits `42bb7f2` / `7aa08d9` / `acaff74` / `7f2fab8` / `9474b4e` / `bd8f701` / `a45654b` |
| **G (PRR-2~5)** | PR-regression follow-ups | 평가 | ✅ | 2026-05-20~21 | 4 / 5 closed. PRR-1 만 보류 (throughput buffer 부족) |

### 4.1 즉시 액션 가능 항목 (2026-05-21 갱신)

이전 candidates (#2, #6, A1) 중 #2 / A1 closed. #6 여전히 가능.
2026-05-20~21 세션에서 closed 된 다른 항목 (F 그룹 전체, G 그룹 4건, C10) 은 이미 §4 마스터 표에 반영.

| ID | 작업 | 시작 가능 시점 | 추천 사유 |
|---|---|---|---|
| **B9** Secret 회피 패턴 | 즉시 | 보안 high, 코드 변경 작음, 사용자 글로벌 보안 룰 직접 매칭 |
| **B6** Error model 5 종 | 즉시 | API 완성도, `pkg/ckv` 표면에 명시. consumer (cks) 가 활용 |
| **A5** fixture N=34 → N=50+ | 즉시 | 모든 retrieval 측정의 baseline. #6 / #9 진행 전 권장 |
| **#6** 룰 기반 prefix | 즉시 (A5 후 측정 가능) | Roadmap §12 Group β 진입 |
| **Roadmap #8** `ckv reindex` 도입 | 즉시 (architectural) | S1.5 승격. Phase B 도입 *전* 전제 |
| **E3** ADR 디렉토리 + 첫 5 개 ADR | 즉시 | A1 결정사항을 ADR-005 로 봉인. 결정 누수 방지 |

→ 본 세션의 다음 candidate = **B9** (보안) 또는 **B6** (API 완성도) 또는 **Roadmap #8** (architectural).

---

## 5. Cross-Reference

본 backlog 항목 ↔ 다른 문서 mapping:

| Backlog ID | featurelist §0.1 | Roadmap §12 | 기타 |
|---|---|---|---|
| #1 | (fixture 영역 외) | 1 | review-direction §6.6 |
| #2 | (eval 영역 외) | 2 | review-direction Appendix C |
| #3 | §1.2 markdown (신규) | 3 | review-direction Appendix B.1.b |
| #4 | (corpus 신규 차원) | 4 | review-direction Appendix B.1.b |
| #5 / A1 | §2.3 ⚠️ | 5 | D1-FU-8 (throughput, batch + CoreML EP) |
| #6 | (chunk text prefix 신규) | 6 | — |
| #7 | (chunk text prefix 신규) | 7 | — |
| #8 / C1 | §6.2 ❌-S1.5 | 8 | autoplan §6.6 |
| #9 | (multi-granularity 신규) | 9 | — |
| #10 / B1 | §1.3 ⚠️ | 10 | plan §5.4 |
| A2 | §11.1 ⚠️ stub | — | D1-FU-4 (model fetch helper) |
| A3 | §17.2 ❌ | — | D1-FU-5 (CI matrix linux/amd64+arm64) |
| A4 | (D2 scope) | — | D1-FU-6 (bge-code-v1 Qwen2 adapter) |
| A5 | (측정 인프라) | (#1과 동일 phase) | D1-FU-7 (50+ query fixture 확장) |
| B2 | §3.4 ⚠️ | — | — |
| B3 | §4.3 ⚠️ | — | — |
| B4 | §5.2 ⚠️ | — | — |
| B5 | §8.2 ⚠️ | — | — |
| B6 | §8.4 ⚠️ | — | — |
| B7 | §10.2 ⚠️ | — | — |
| B8 | §11.2 ⚠️ | — | — |
| B9 | §15.2 ⚠️ | — | 사용자 글로벌 보안 룰 |
| B10 | §16.4 ❌ | — | — |
| C2 | §9 ❌-S2 | — | plan §13 |
| C3 | §7 ❌-planned | — | plan §13 |
| C4 | §12 ❌-S2 | — | plan §13 |
| C5 | §6.3 ❌-S2 | — | — |
| C6 | §8.5 ❌-S2 | — | — |
| C7 | §8.1 ❌-S2 | — | sanitize 의존 |
| C8 | §4.2 ❌-S2 | — | UC-V4 |
| C9 | §2.4 ❌-S2 | — | — |
| C10 | §1.2 ❌-S2 | — | review-direction §6.1 사용자 결정 |
| C11 | §14.2 ❌-S2 | — | — |
| D1 | §10.5 ❌-CKS | E | plan §7 |
| D2 | (CKS multiplex) | E | plan §7.3 |
| D3 | §8.3 ❌-S6 | — | plan §8.4 |
| D4 | (CKG 책임) | — | CKG `pkg/bm25/scorer.go` |
| E1 | §18.2 ❌ | — | — |
| E2 | §18.3 ❌ | — | — |
| E3 | (markdown parser 완료 후 ADR 문서 자체 미작성) | — | — |
| CKV-1 | (consumer 측 hang) | — | `testdata/mcp-repro/` 검증 스크립트 |
| CKV-2 | (no public API) | — | `pkg/ckv/` 신설 |
| CKV-3 | (no consumer docs) | — | `docs/embedder-integration.md` |
| CKV-4 | (mcp-go middleware) | — | `pkg/mcp/server.go::NewServer` |
| CKV-5 | (cold start) | — | `pkg/mcp/server.go::handleWarmup` |
| CKV-6 | (health flat only) | — | `pkg/mcp/server.go::handleHealth` |
| CKV-7 | (no schema versioning) | — | `pkg/mcp/server.go::jsonResult` |
| PRR-1 | (보류) | — | `internal/eval/prregress/`, throughput buffer 의존 |
| PRR-2~5 | (PR-regression eval 개선) | — | `cmd/ckv/eval.go`, `internal/eval/prregress/{agent,score}.go`, `testdata/prs.yaml` |

---

## 6. 변경 이력

| 일자 | 버전 | 변경 |
|---|---|---|
| 2026-05-20 | 1.0 | 초안 — 옵션 (b) 사용자 결정 (2026-05-20) 으로 신설. 5 카테고리 33 항목 inventory + 마스터 진행 상태 추적 표 (Roadmap §12 + 본 backlog 통합) + cross-reference. retrieval-quality-roadmap.md §12 는 retrieval slice의 우선순위 view로 역할 분리. |
| 2026-05-21 | 1.1 | 2026-05-20~21 대규모 세션 반영. **closed**: A1 (CoreML I/O — MLProgram + static shapes), C10 (JS parser), Roadmap #2 (PR-regression 모듈, follow-up G 그룹), #3 (markdown chunk_kind=doc 추가), #5 부분 (CPU 0.74 c/s 도달, ANE 친화 모델 도입은 HF 차단으로 보류). **신설 카테고리**: F (cks dogfood CKV-1~7, 모두 closed — pkg/ckv public API, MCP 5 종 확장, WithRecovery, schema_version, embedder/index nested health), G (PR-regression PRR-1~5, 4 closed + 1 보류). 인프라 신설 (메모리 가드 / CoreML 7 env / ORT 2 env / static shapes / `ckv reindex` 결정사항) 은 commit history `66bdefc..1d2c2e1` 에. **즉시 액션 가능 항목** 갱신: 이전 #2 / A1 closed 반영, 새 candidates = B9 (보안) / B6 (error model) / Roadmap #8 (`ckv reindex` architectural) / A5 (fixture 확장). |
