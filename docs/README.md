# CKV 문서 인덱스

이 디렉터리는 **살아있는 레퍼런스 문서**, **설계 기록**, **폐기된 아카이브**로
나뉜다. 새 세션은 아래 분류를 보고 "현재 유효한 것"부터 읽는다.
**"지금 무엇이 참인가" = 코드 + git.**

> **작업 상태 단일 SoT:** [`remaining.md`](./remaining.md) (코드검증본).
> **서사·배경 진입점:** [`session-handoff-2026-06-29.md`](./session-handoff-2026-06-29.md)
> — 배경은 여기서, *잔여 작업의 기준*은 항상 `remaining.md`.

## 레퍼런스 (living — 코드와 함께 유지보수)

| 문서 | 내용 |
|------|------|
| [`ARCHITECTURE.md`](./ARCHITECTURE.md) | 패키지 구조·빌드/쿼리 파이프라인 |
| [`SCHEMA.md`](./SCHEMA.md) | 온디스크/인메모리 계약 (스키마 SoT) |
| [`mcp-tools.md`](./mcp-tools.md) | 19개 MCP 도구 입출력 사양 |
| [`remaining.md`](./remaining.md) | **잔여 작업 단일 SoT** (코드검증본) |
| [`eval-metrics.md`](./eval-metrics.md) | `ckv eval` 지표 정의 |
| [`embedder-integration.md`](./embedder-integration.md) | 임베더 통합 가이드 (기본 ollama/bge-m3) |
| [`d1-installation-guide.md`](./d1-installation-guide.md) | bge-large + ONNX 설치 가이드 (선택 경로 — 기본은 ollama) |

## 설계 기록 (Tier 2 — 결정 근거·서사, 유지)

| 문서 | 내용 |
|------|------|
| [`reindex-migration-design-2026-07-10.md`](./reindex-migration-design-2026-07-10.md) | reindex/마이그레이션 설계 (P2–P5 CKV-side 구현 완료; §9 open 결정은 `remaining.md`에서 추적) |
| [`flow-knowledge-design-2026-06-16.md`](./flow-knowledge-design-2026-06-16.md) | 플로우 지식 설계 근거 (결정은 ADR-010) |
| [`evaluation-design-2026-05-22.md`](./evaluation-design-2026-05-22.md) | 평가 방법론 설계 |
| [`pr-retrieval-eval-2026-07-08.md`](./pr-retrieval-eval-2026-07-08.md) | PR-retrieval 측정 (역할 분리: CKV=진입점 / CKG=blast-radius) |
| [`coordination-prompts-2026-06-29.md`](./coordination-prompts-2026-06-29.md) | 교차 세션 협의 프롬프트 (CKS 재핸드셰이크 §10.10은 open) |

> `session-handoff-2026-06-29.md`는 서사 진입점으로 유지한다(작업 상태는 `remaining.md`가 SoT).

## ADR (아키텍처 결정 기록)

[`adr/`](./adr/) — [`adr/README.md`](./adr/README.md)에 **001~010** 인덱스 및 작성 규칙.
하나의 결정 = 하나의 파일; supersede-not-delete.

## archive (폐기 — 현재 상태로 참조 금지)

[`archive/`](./archive/) 의 문서는 후속 문서·ADR·코드로 대체된 동결본이다. 역사적 맥락
확인 용도로만 본다.

**2026-07-19 정리에서 이동 (완료 스냅샷 — 결정은 ADR/`remaining.md`에 보존):**

| 문서 | 대체 |
|------|------|
| `archive/backlog.md`, `archive/pending-work-2026-05-21.md` | → `remaining.md` (supersede) |
| `archive/plan-S1-ckv.md`, `archive/plan-2026-05-26.md`, `archive/plan-2026-05-29-ckv-refactor.md`, `archive/plan-2026-06-16-flow-ingest.md` | → shipped; ADR-004/006/010 |
| `archive/retire-ckg-node-id.md` | → ADR-007 (0 code refs) |
| `archive/embedding-model-recommendation-2026-06-22.md`, `archive/qwen3-dimension-ab-2026-07-12.md` | → ADR-008 |
| `archive/eval-hard-fixture-2026-07-12.md`, `archive/prefix-lever-sweep-2026-07-12.md`, `archive/phase-b-multigran-probe-2026-07-12.md`, `archive/llm-contextual-prefix-poc-2026-07-12.md` | → ADR-009 |
| `archive/retrieval-quality-roadmap.md` | → phases shipped; `remaining.md` |
| `archive/featurelist.md`, `archive/use-cases.md` | → drifted 2026-05 snapshot; `remaining.md` is status SoT |

**이전 정리에서 이동:**

| 문서 | 대체 |
|------|------|
| `archive/session-handoff-2026-05-23.md` / `-05-29.md` / `-06-15.md` | → `session-handoff-2026-06-29.md` |
| `archive/cks-design-2026-05-29.md` | hypothetical 초안 (실제 CKS 상태와 불일치) |
