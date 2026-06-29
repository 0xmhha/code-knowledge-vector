# CKV 문서 인덱스

이 디렉터리는 **살아있는 레퍼런스 문서**, **시점 스냅샷**, **폐기된 아카이브**가 섞여 있다.
새 세션은 아래 분류를 보고 "현재 유효한 것"부터 읽는다.

> **현행 단일 진입점(SoT):** [`session-handoff-2026-06-29.md`](./session-handoff-2026-06-29.md)
> — 새 세션은 이 문서부터 읽는다. (직전 `session-handoff-2026-06-15.md`는 archive로 이동.)

## 레퍼런스 (living — 코드와 함께 유지보수)

| 문서 | 내용 |
|------|------|
| [`ARCHITECTURE.md`](./ARCHITECTURE.md) | 패키지 구조·빌드/쿼리 파이프라인 |
| [`SCHEMA.md`](./SCHEMA.md) | 온디스크/인메모리 계약 (스키마 SoT) |
| [`mcp-tools.md`](./mcp-tools.md) | 15개 MCP 도구 입출력 사양 |
| [`backlog.md`](./backlog.md) | 작업 추적 inventory (구현 상태 기준) |
| [`featurelist.md`](./featurelist.md) | 기능 목록 + 우선순위 |
| [`use-cases.md`](./use-cases.md) | UC-V1~V15 사용 시나리오·범위 |
| [`eval-metrics.md`](./eval-metrics.md) | `ckv eval` 지표 정의 |
| [`retrieval-quality-roadmap.md`](./retrieval-quality-roadmap.md) | 검색 품질 로드맵 |
| [`embedder-integration.md`](./embedder-integration.md) | 임베더 통합 가이드 |
| [`d1-installation-guide.md`](./d1-installation-guide.md) | bge-large + ONNX 설치 가이드 |

> 주의: `featurelist`/`use-cases`/`eval-metrics`는 `backlog`/SoT 핸드오프와
> 일부 상태가 어긋나 있다(드리프트). 구현 상태의 기준은 `backlog.md`와 현행 핸드오프다.

## 스냅샷 (특정 시점에 동결된 계획/설계 — 역사 기록)

| 문서 | 시점 |
|------|------|
| [`plan-S1-ckv.md`](./plan-S1-ckv.md) | S1 슬라이스 실행 계획 (2026-05-08~19) |
| [`pending-work-2026-05-21.md`](./pending-work-2026-05-21.md) | 2026-05-21 잔여 작업 |
| [`evaluation-design-2026-05-22.md`](./evaluation-design-2026-05-22.md) | 평가 설계 |
| [`plan-2026-05-26.md`](./plan-2026-05-26.md) | 세션 마무리 계획 |
| [`plan-2026-05-29-ckv-refactor.md`](./plan-2026-05-29-ckv-refactor.md) | Schema-First 리팩토링 계획 (완료) |
| [`flow-knowledge-design-2026-06-16.md`](./flow-knowledge-design-2026-06-16.md) | 플로우 지식 설계 방향 |
| [`plan-2026-06-16-flow-ingest.md`](./plan-2026-06-16-flow-ingest.md) | 플로우 인제스트 구현 계획 |
| [`embedding-model-recommendation-2026-06-22.md`](./embedding-model-recommendation-2026-06-22.md) | 임베딩 모델 업그레이드 추천 (bge-m3 → Qwen3-Embedding) |
| [`coordination-prompts-2026-06-29.md`](./coordination-prompts-2026-06-29.md) | 교차 세션 협의 프롬프트 (CKG / CKS / coding-agent) |

## ADR (아키텍처 결정 기록)

[`adr/`](./adr/) — [`adr/README.md`](./adr/README.md)에 001~006 인덱스 및 작성 규칙.

## archive (폐기 — 현재 상태로 참조 금지)

[`archive/`](./archive/) 의 문서는 후속 문서로 대체된 동결본이다. 역사적 맥락 확인 용도로만 본다.

| 문서 | 대체 |
|------|------|
| [`archive/session-handoff-2026-05-23.md`](./archive/session-handoff-2026-05-23.md) | → `session-handoff-2026-06-29.md` |
| [`archive/session-handoff-2026-05-29.md`](./archive/session-handoff-2026-05-29.md) | → `session-handoff-2026-06-29.md` |
| [`archive/session-handoff-2026-06-15.md`](./archive/session-handoff-2026-06-15.md) | → `session-handoff-2026-06-29.md` (PR #7~#15 + 4세션 협의 미반영) |
| [`archive/cks-design-2026-05-29.md`](./archive/cks-design-2026-05-29.md) | hypothetical 초안 (실제 CKS 상태와 불일치) |
