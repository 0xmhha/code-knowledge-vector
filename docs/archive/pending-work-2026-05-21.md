# Pending Work (snapshot 2026-05-21)

> **ARCHIVED 2026-07-19.** Superseded by [`remaining.md`](../remaining.md) (the code-verified work SoT); this doc self-flagged stale. Kept for provenance.

> ⚠️ **SUPERSEDED** — 이 스냅샷은 `session-handoff-2026-06-29.md §4`와
> [`remaining.md`](./remaining.md)(live SoT)로 대체됐다. B7(Symbol ID)은 canonical_id 단일화
> (ADR-007 + [`retire-ckg-node-id.md`](./retire-ckg-node-id.md))로 방향 확정. 역사적 참고용.

이 문서는 2026-05-21 세션 종료 시점의 **잔여 작업** 스냅샷이다. backlog.md
(전체 inventory) 와 retrieval-quality-roadmap.md (retrieval slice) 를
참조한 view — 항목별 *왜 아직 안 했는지* 와 *진입 조건* 을 한 곳에 정리.

세션 결산: 14 개 항목 closed (Tier 1 4건 + #6 + E3 + B3 + E1 + E2-chunk +
B1 + B2 + B4 + B5 + B8), 16 commits. 자세히는 [`backlog.md §1`](./backlog.md)
변경 이력 참조.

---

## 1. 코드 미구현 (backlog 잔여)

### B7 — Symbol ID 호환 정규화 규칙 (P0, ⏳)

| 항목 | 상태 |
|---|---|
| **출처** | featurelist §10.2, backlog B7 |
| **현황** | CKV chunk 의 `ckg_node_id` 필드만 저장. CKG nodes 와 join 위한 *정규화 규칙* 미합의. |
| **블로커** | **CKG repo 와의 cross-tool spec 협업 필요.** CKV 단독으로 결정 불가. |
| **CKG 측 상태 (2026-05-21)** | **코드는 구현됨, evaluation 미완료** — §3 참조 |
| **진입 조건** | CKG ↔ CKV 양측 합의된 normalize 규칙 + integration test fixture |
| **예상 LOC** | ~50 (CKV 측) + CKG 측 동등 작업 |

### B10 — Parser fuzz / property tests (P1, ⏳)

| 항목 | 상태 |
|---|---|
| **출처** | featurelist §16.4, backlog B10 |
| **현황** | parser fuzz 미구현. 랜덤/이상 입력에 대한 panic 부재 미검증. |
| **범위** | Go / TypeScript / JavaScript / Solidity / Markdown 5 개 파서. 각각 `internal/parse/*` 에 `*_fuzz_test.go` 추가. |
| **블로커** | 없음. 인프라 작업으로 독립 진행 가능. |
| **진입 조건** | 별도 세션 (fuzz harness setup, seed corpus, CI 통합) |
| **예상 LOC** | ~200 (5 파서 × 40 LOC) + CI workflow |

### Roadmap #7 — Phase D.2 LLM-generated contextual prefix (⏳)

| 항목 | 상태 |
|---|---|
| **출처** | retrieval-quality-roadmap.md §8 Phase D.2, §12 #7 |
| **현황** | Phase D.1 (rule-based prefix) ✅ impl + mock 측정 완료. D.2 (LLM 호출) 미진행. |
| **예상 효과** | MRR +0.08~0.13 (Anthropic 측정 기준 −35% failure rate) |
| **비용** | build throughput **0.2~0.4 c/s** 까지 악화 + LLM API cost ~$0.0001/chunk |
| **블로커** | **bgeonnx throughput buffer 부족.** 현재 CPU 0.74 c/s (A1 closed 후 상태) — D.2 적용 시 0.2 c/s 이하 가능, 5k 파일 빌드 6h+. 사실상 비현실적. |
| **진입 조건** | (a) ANE-친화 모델 도입 (EmbeddingGemma-300M 등) 으로 throughput 5+ c/s 회복, OR (b) 작은 corpus 한정 평가 (testdata/sample N=50) 부터 시작 |
| **예상 LOC** | ~150 (Claude CLI 또는 API 호출 + cache) |

---

## 2. C 그룹 — S2 milestone deferred

backlog C 카테고리는 S1 stable 출시 *후* S2 milestone 에서 일괄 진행
예정. 본 문서는 *기억* 용도만 — 현재 작업 대상 아님.

| ID | 항목 |
|---|---|
| C2 | `internal/sanitize/` 전체 (UC-V13) |
| C3 | `internal/memory/` Working Memory (UC-V9/14) |
| C4 | `ckv serve` HTTP API |
| C5 | `cks.ops.request_refresh` |
| C6 | `cks.ops.stats` |
| C7 | `cks.context.get_context_for_task` |
| C8 | UC-V4 Pattern Similarity (code-as-query) |
| C9 | Embedding cache (per-text disk cache) |
| C11 | Prometheus metrics exporter |

E2-WM (working memory entry 스키마) 와 E2-Sanitize (sanitize_report 스키마)
도 동일 — 그 모듈 도입 시점에 모듈 내부에서 작성.

---

## 3. 구현 완료 + Evaluation 미완료 (Critical Gap)

> 본 세션에서 *구현* 은 완료했지만 *실측* 이 누락된 항목들. CKV 자체 측정과
> CKG cross-tool integration 양쪽에 걸쳐 있음.

### 3.1 CKG ↔ CKV integration (cross-tool)

**사용자 보고 (2026-05-21)**: CKG repo 의 cross-tool 코드는 **구현 완료**,
**evaluation 미완료**.

CKV 측 노출 표면 (모두 closed):
- `pkg/mcp.Server.Underlying()` — CKS 가 자기 MCPServer 에 register
- `pkg/ckv.Engine` — in-process Go API
- `types.Filter.CommitHash` (B2) + `types.Hit.StaleCitation` (B4) — cross-tool 정합
- `Options.TraceID` (B5) — multiplex 환경의 correlation
- `Hit.Density` (B3) — RRF 입력 시 tier 정보

CKG 측 코드 가용 (사용자 보고):
- 이미 `pkg/bm25/scorer.go` (qname + signature + doc-comment corpus)
- (가정) RRF fusion / multiplex tool / Symbol ID normalization 등

**Evaluation gap (미완료 항목)**:

| 측정 항목 | 현황 | 진입 조건 |
|---|---|---|
| **BM25 + vector RRF 정확도 향상** | 측정 없음 | CKS repo 측에서 RRF 결과 vs vector-only baseline 비교. Roadmap §8 Phase E. |
| **Symbol ID join 매칭률** | 단일 파일 점검만 | CKV chunks ↔ CKG nodes 의 `(file, start_line, end_line)` join — plan §5.5 기대치 ≥90% 미검증 |
| **Citation 정합성** | CKV 측 `EnforceCitations` 만 통과 | CKG-emit 결과 + CKV-emit 결과 의 citation format consistency 미검증 |
| **PR-regression on hybrid** | vector-only PRR-1 보류 (throughput) | hybrid 도입으로 보강 가능 — 측정 시점 미정 |
| **End-to-end query_code multiplex** | CKV unit/MCP 테스트만 | CKS 측 multiplex tool 호출 → 실 응답 검증 |

**제안 evaluation 절차** (별도 세션):

1. **CKG nodes ↔ CKV chunks join 통계**
   - 동일 src_root 빌드 후 CKG `nodes` 테이블 + CKV `chunks` 테이블 (file, start, end) 매칭률 계산
   - 기준: ≥90% (코드 chunk 한정)
2. **Hybrid 정확도 측정** (CKS repo 책임 영역)
   - testdata/queries.yaml N=50 + why-queries.yaml 에 대해 vector-only / BM25-only / RRF fused 의 recall@5, MRR, citation_accuracy 비교
   - 기준: RRF fused > 각각 baseline (Anthropic −49% failure rate target)
3. **Schema compatibility 회귀 테스트**
   - CKG `pkg/bm25/scorer.go` 출력 + CKV `pkg/types.Hit` 의 `Citation` 필드 align 자동 검증
   - 한 쪽 변경 시 fail 하도록 CI gate

### 3.2 CKV 내부 — 측정 deferred 항목

| 항목 | 구현 | 측정 deferred 이유 |
|---|---|---|
| **B1 — Phase A sliding split** | ✅ `splitLongSpan` (`6dc7225`) | testdata/sample 함수 모두 짧음 → split 미발동. **bge-large + 큰 함수 corpus 실측 필요**. |
| **#6 — Phase D.1 contextual prefix** | ✅ `BuildEmbedText` (`1a5289d`) | mock embedder 측정만 (r@1 +0.060, MRR +0.053). **bge-large 측정 미완** — 실측에서 더 큰 향상 기대. |
| **PRR-1 — full PR regression eval** | 보류 (`docs/backlog.md` G 그룹) | bgeonnx CPU 0.74 c/s — 5k 파일 corpus 빌드 6h+. **throughput buffer 부족**. |
| **#5 — D1-FU-8 throughput target 30 c/s** | 부분 (A1 closed, CPU pure 0.74 c/s) | ANE 친화 모델 (EmbeddingGemma-300M) HF 차단. |

**측정 진입 조건**:

- B1 + #6 measurement: bge-large 모델 로딩 + N=50 fixture + (선택) 큰 함수 corpus 추가. 단일 세션 ~30분.
- PRR-1: throughput 5+ c/s 회복 후. 그 전엔 PR fixture 1-2 entry 한정 evaluation 만 가능.
- #5: HF policy 우회 (오프라인 모델 전송) OR Apple 측 새 ANE-친화 ONNX 모델 검토.

---

## 4. 외부 의존 (CKV scope 외)

| ID | 항목 | 책임 | 비고 |
|---|---|---|---|
| **D1** | RRF fusion + `cks-mcp` 통합 binary | CKS repo | CKV 측 `pkg/mcp.Server.Underlying()` 표면은 완료. |
| **D2** | `cks.context.query_code` multiplex tool | CKS repo | 사용자 보고 (§3.1) — CKG 측 코드 구현됨, eval 미완료. |
| **D3** | mTLS auth | S6 보안 | CKV 측은 caller cert SAN ↔ envelope `caller` 일치 검증만. |
| **D4** | CKG 측 BM25 corpus 확장 | CKG repo | CKG 가 이미 `pkg/bm25/scorer.go` 구현. |

---

## 5. 다음 세션 진입 권장 순서

trade-off 분석 기준:

| 우선 | 항목 | 이유 |
|---|---|---|
| **1** | **CKG ↔ CKV integration evaluation** (§3.1) | 사용자 직접 보고 + cross-tool gap 가장 큼. CKS repo 협업 세션 필요. |
| 2 | bge-large 측정 (B1 + #6) | CKV 단독 진행 가능, 30분 세션. retrieval-quality-roadmap Phase A/D.1 row 채움. |
| 3 | B7 Symbol ID 정규화 | §3.1 평가 결과에 따라 결정. CKG 측 코드가 reference impl 으로 활용 가능. |
| 4 | B10 Fuzz infra | 독립 인프라 작업. 보안 critical 아님 (시간 여유 있을 때). |
| 5 | #7 Phase D.2 LLM prefix | throughput buffer 회복 후 (ANE 친화 모델 도입). |
| 6 | C 그룹 | S2 milestone — S1 stable 종료 선언 후. |

---

## 6. 참조

- [`backlog.md`](./backlog.md) — 전체 inventory (Single source of truth)
- [`retrieval-quality-roadmap.md`](./retrieval-quality-roadmap.md) — retrieval slice 우선순위
- [`featurelist.md §0.1`](./featurelist.md) — feature ↔ 상태 마스터 표
- [`plan-S1-ckv.md`](./plan-S1-ckv.md) — S1 milestone plan (CKV ↔ CKG ↔ CKS 통합 설계)
- [`ARCHITECTURE.md`](./ARCHITECTURE.md) — 내부 모듈 그래프 + 파이프라인 시퀀스
- [`SCHEMA.md`](./SCHEMA.md) — chunk metadata + sqlite-vec DDL

---

## 변경 이력

| 일자 | 변경 |
|---|---|
| 2026-05-21 | 초안. 본 세션 (14 항목 closed) 후 잔여 작업 + CKG eval gap (사용자 보고) 정리. |
