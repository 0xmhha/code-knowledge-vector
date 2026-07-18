# Code Knowledge Vector (CKV) — Use Cases

> **ARCHIVED 2026-07-19.** 2026-05 status snapshot, drifted from code (default model, tool count, reindex). The status SoT is [`remaining.md`](../remaining.md). Kept for provenance.

> **문서 버전**: 1.0
> **작성일**: 2026-05-05
> **출처**: `04-cks-deep-dive.md` (Code Knowledge System 전체 설계 중 Vector DB 영역 추출)
> **자매 프로젝트**: [`code-knowledge-graph`](https://github.com/0xmhha/code-knowledge-graph) — 동일 코드베이스에 대한 그래프 기반 분석 도구

---

## 0. 프로젝트 위치

본 프로젝트(`code-knowledge-vector`, 이하 **CKV**)는 CKS(Code Knowledge System)의 **Layer 1 Storage Backend 중 Vector DB 계층 + 그 위에서 동작하는 의미 검색 파이프라인**을 단독 실행 가능한 도구로 구현한다.

```
CKS 전체
├── Layer 4: Query API ─────── (CKV·CKG·Hybrid 공통 표면, MCP/HTTP/CLI)
├── Layer 3: Retrieval Orchestrator (Pager) ── RRF·Playbook·Token Budget
├── Layer 2: Working Memory Store ── 세션/run_id 단위 캐시
└── Layer 1: Storage Backends
     ├── Graph DB ◄─ code-knowledge-graph (sibling)
     ├── Vector DB ◄─ THIS PROJECT (CKV)
     ├── BM25 / Full-text
     ├── AST Cache
     └── File / Blob Store
```

CKV의 책임 경계:
- **포함**: Vector DB 백엔드, 임베딩 파이프라인, 의미 유사도 질의, 멀티언어 chunking, MCP/CLI 노출, CKG 와의 상호운용을 위한 공통 인용(citation) 포맷.
- **제외(CKG 또는 통합 CKS 단계 책임)**: 6가지 그래프 구축, 호출/구조 관계 추적, Git 히스토리 그래프, RRF 다중 백엔드 fusion 로직(통합 단계에서 추가).

> **궁극 목표**: 백만 라인 이상 규모의 코드베이스에서, 자연어 또는 모호한 묘사만으로도 "수정해야 할 정확한 코드 위치"를 LLM이 토큰 효율적으로 찾아낼 수 있도록 하는 의미 추론 백엔드를 제공한다.

---

## 1. 페르소나(Actor)

| 페르소나 | 설명 | 주요 호출 경로 |
|---|---|---|
| **LLM Agent** | Claude Code, CS2 Planner, 외부 LLM | MCP (`cks.context.*`, `cks.memory.*`) |
| **개발자 (Local)** | 코드베이스를 처음 받은 엔지니어 | CLI (`ckv build`, `ckv query`) |
| **CI/CD 파이프라인** | Pre-commit / pre-push 훅 | CLI + HTTP |
| **CKS Orchestrator** | Retrieval Orchestrator (Layer 3) | HTTP / 내부 라이브러리 호출 |
| **Ops/SRE** | 인덱스 신선도·재인덱싱 운영 | CLI (`cks.ops.*`) |

---

## 2. 핵심 사용 시나리오 요약

| ID | 이름 | 1줄 설명 | 우선순위 | S1 충족도 (2026-05-19) |
|---|---|---|:---:|---|
| **UC-V1** | Bootstrap Indexing | 코드 경로를 받아 전체 임베딩 벡터 DB를 처음 구축 | P0 | ✅ `ckv build` |
| **UC-V2** | Incremental Update | 파일 변경 시 영향 chunk 만 재임베딩 | P0 | ❌ S2 이관 (`ckv reindex` 미구현) |
| **UC-V3** | Semantic Search (NL → Code) | 자연어 질의로 의미적으로 유사한 코드 위치 반환 | P0 | ✅ `ckv query` / `cks.context.semantic_search` |
| **UC-V4** | Pattern Similarity Search | 특정 함수/스니펫과 유사한 구현 패턴 탐색 | P0 | ❌ S2 이관 (code-as-query 모드 미구현) |
| **UC-V5** | Evidence Pack Assembly | task_type + token budget 기반 Evidence Pack 일부 생성 | P0 | ⚠️ 부분 (`semantic_search`만, `get_context_for_task` 미구현) |
| **UC-V6** | MCP Tool Exposure to LLM | LLM이 도구로 직접 호출 가능 (`cks.context.*`) | P0 | ✅ read-only 3 tool (`semantic_search`, `ops.get_freshness`, `ops.health`) |
| **UC-V7** | Cross-Language Semantic Discovery | Go/Solidity/TS/Shell 횡단 의미 검색 | P1 | ⚠️ 부분 — Go/TS/Sol만 (JS/Bash S2 이관) |
| **UC-V8** | Hybrid Query w/ Graph DB | CKG 의 graph 결과와 RRF·교차 인용으로 결합 | P0 | ❌ CKS 책임으로 이관 (plan §7) |
| **UC-V9** | Working Memory Cache | 동일 세션 내 중복 질의 캐싱 + writeback | P1 | ❌ planned (`cks.memory.*` 미구현) |
| **UC-V10** | Citation Enforcement | 모든 결과에 `file:line` 강제, 환각 차단 | P0 | ✅ citation accuracy 100% |
| **UC-V11** | Freshness & Stale Warning | 인덱스 신선도 체크, 오래된 결과 경고 | P1 | ✅ `ckv freshness` / `cks.ops.get_freshness` |
| **UC-V12** | Bootstrap / Systemization Report | 신규 프로젝트의 의미 클러스터·온보딩 정보 출력 | P2 | ❌ S4 (M7 eval & report) |
| **UC-V13** | Sanitize Evidence (default-deny) | 외부 caller 노출 전 prompt-injection 차단 | P0 | ❌ S2 이관 (plan §13) |
| **UC-V14** | Resume from Working Memory | 이전 run_id 복원해 동일 컨텍스트로 재개 | P2 | ❌ planned (UC-V9 의존) |
| **UC-V15** | Local-First Embedding | 외부 API 의존 없이 로컬 모델로 동작 | P0 | ✅ mock + bgeonnx 모두 local |

**S1 충족 요약**: ✅ 6/15 (UC-V1, V3, V6, V10, V11, V15), ⚠️ 2/15 (UC-V5, V7), ❌ 7/15 (S2/S4/CKS/planned 이관). UC-V8은 CKS repo 책임으로 별도 진행.

> **출처 매핑**: UC 번호와 04-cks-deep-dive.md 의 §번호 대응은 각 시나리오 본문 *Source* 항목에 기재.

---

## 3. 시나리오 상세

### UC-V1. Bootstrap Indexing (전체 인덱스 구축)

- **Source**: §4.2, §12.1 (Bootstrap Indexer 5-Phase 파이프라인의 D 단계 = vector index materialization)
- **Actor**: 개발자 / CI 파이프라인
- **Trigger**: `ckv build --src=/path/to/repo --out=/var/ckv/data`
- **Goal**: 주어진 코드 경로의 모든 소스 파일을 함수/클래스/컨트랙트 단위로 chunking 하여 임베딩 + 메타데이터로 저장한다.
- **Preconditions**:
  - 대상 경로가 git 레포지토리 (commit_hash 인덱싱용)
  - 지원 언어 grammar(tree-sitter) 설치 완료
- **Main Flow**:
  1. 파일 워킹 → 언어 분류
  2. tree-sitter 로 함수/메서드/contract 단위 노드 추출
  3. 큰 함수는 sliding window 분할 (예: 50줄, 10줄 overlap)
  4. 각 chunk 에 메타데이터 부착: `{file, line_range, lang, symbol, commit_hash}`
  5. 임베딩 모델로 batch encode
  6. Vector DB 에 upsert (chunk_id, vector, metadata)
  7. `indexed_head` 메타키로 현재 commit hash 기록
- **Outputs**: persistent vector store, indexing summary (chunk 수, 언어별 분포, 커버리지)
- **Success Criteria**:
  - 500K LOC 규모 레포 < 10분 (single-node, 4 CPU)
  - chunk 누락률 < 1%
  - `indexed_head == git rev-parse HEAD`

---

### UC-V2. Incremental Update (증분 인덱싱)

- **Source**: §4.6 (Incremental Indexing 동작), §12.3
- **Actor**: 파일 워처 / git 훅 / Ops
- **Trigger**: 파일 저장 이벤트, 또는 `ckv reindex --since=<commit>`
- **Goal**: 변경 파일에 한해 vector chunk 만 재계산 — 전체 재빌드 회피
- **Main Flow**:
  1. fsnotify 또는 `git diff --name-only ${indexed_head} HEAD` 로 변경 파일 목록 수집
  2. 각 변경 파일의 기존 chunk 모두 제거 (cascade by file path)
  3. 신규 파싱 → 새 chunk 생성 → 임베딩 → upsert
  4. `indexed_head` 갱신
- **Success Criteria**:
  - 단일 파일 변경 인덱스 반영 < 2초 (warm)
  - 1000 파일 변경 < 60초

---

### UC-V3. Semantic Search (자연어 → 코드)

- **Source**: §4.2 ("자연어 → 코드 매핑이 필요할 때, 정확한 심볼명을 모를 때 첫 단계")
- **Actor**: LLM, 개발자
- **Trigger**: `cks.context.semantic_search("WBFT consensus commit phase 처리 로직", k=5)`
- **Goal**: 자연어 질의 ↔ 임베딩 공간에서 가까운 코드 chunk 들을 인용과 함께 반환
- **Main Flow**:
  1. query 임베딩 생성
  2. ANN 검색 (top-K, distance threshold)
  3. metadata filter 적용 (lang, path glob, symbol kind)
  4. 결과에 `file:line` citation 강제 부여
  5. token budget 안에서 snippet density 조정 (full body / signature+5lines / signature only)
- **Outputs**: ranked list `[{chunk_id, citation, snippet, score, lang}]`
- **Success Criteria**:
  - relevance@10 ≥ eval baseline
  - citation 정확도 100% (반환된 file:line 이 실제로 존재)
  - p95 latency < 200ms (warm cache, 단일 노드)

---

### UC-V4. Pattern Similarity Search (코드 → 유사 코드)

- **Source**: §4.2 예시 ("에러 핸들링 패턴이 비슷한 다른 함수")
- **Actor**: LLM (Feature Add Playbook의 Step 2_similar)
- **Trigger**: `cks.context.semantic_search` with input = 기존 함수 본문
- **Goal**: 새 기능 구현 시 참고할 유사 구현 패턴 탐색, 또는 일관성 검증
- **Main Flow**: UC-V3 와 동일하되 query 가 자연어가 아닌 코드 스니펫
- **Success Criteria**: 사람이 평가했을 때 top-5 중 ≥ 2 개가 의미적으로 동일한 패턴

---

### UC-V5. Evidence Pack Assembly (Vector 부분 기여)

- **Source**: §6.1 (Evidence Pack 표준 형식), §6.2 (13단계 알고리즘 중 backend query)
- **Actor**: CKS Retrieval Orchestrator 또는 내부 호출
- **Trigger**: `cks.context.get_context_for_task({intent, task_type, budget_tokens, run_id})`
- **Goal**: Evidence Pack 의 `candidate_files`, `candidate_symbols`, `context_snippets` 의 vector-derived 부분을 채워 반환
- **Main Flow**:
  1. task_type → playbook 의 vector backend 사용 여부 결정
  2. semantic_search 수행
  3. citation 부여
  4. token budget 부분 할당량(`budget_ratio`) 안에서 결과 truncate
  5. caller(Layer 3)가 graph/bm25 결과와 RRF fuse
- **Outputs**: partial Evidence Pack 조각 + `tokens_used`, `freshness`, `warnings`
- **Success Criteria**: budget 초과 0%, sanitize_report 동봉

---

### UC-V6. MCP Tool Exposure to LLM

- **Source**: §7.4 (MCP Tool Groups — 5가지 namespace 중 vector 관련)
- **Actor**: LLM (Claude Code, CS2 Planner)
- **Trigger**: LLM 클라이언트가 `cks.context.semantic_search` / `cks.context.get_context_for_task` / `cks.memory.*` 도구 호출
- **Goal**: LLM 이 자연어로 의도만 표현하면 vector 백엔드를 거쳐 인용 포함 결과 반환
- **Main Flow**:
  1. MCP 서버 부팅: `ckv mcp --vector-db=/var/ckv/data`
  2. Tool registry 등록: `cks.context.*`, `cks.memory.*`, `cks.ops.*` (vector 관련 일부)
  3. envelope 검증 (mTLS, manifest_ref, budget_tokens)
  4. tool 실행 → sanitize → 응답
- **Success Criteria**: Claude Code 가 추가 설정 없이 도구 발견 및 사용

---

### UC-V7. Cross-Language Semantic Discovery

- **Source**: §8.2 (Cross-Language Linking) — vector 측 기여분
- **Actor**: LLM
- **Trigger**: "이 Solidity 이벤트를 emit 하는 Go 코드와 의미적으로 유사한 처리 로직"
- **Goal**: 단일 언어 검색을 넘어 다언어 코드베이스 전체에서 의미 매칭
- **Note**: 정밀한 xlang_calls edge 는 CKG 책임. CKV 는 **언어 무관 임베딩 공간**으로 1차 후보 제공.
- **Success Criteria**: 검색 결과의 lang 분포가 단일 언어로 편향되지 않을 때 정확도 유지

---

### UC-V8. Hybrid Query with code-knowledge-graph

- **Source**: §3 (4-Layer 구조), §6.4 결정 2 (RRF fusion)
- **Actor**: CKS 통합 단계의 Retrieval Orchestrator
- **Trigger**: 사용자/LLM 질의가 vector + graph 양쪽 backend 가 필요한 hybrid intent 인 경우
- **Goal**: CKV 의 의미 후보군과 CKG 의 구조 후보군을 RRF 로 합쳐 정밀도 향상
- **CKV 책임**:
  - 동일 `run_id`, `commit_hash`, `file:line` 표기 규약을 CKG 와 공유
  - CKG 가 반환한 symbol id 를 받아 해당 chunk 의 임베딩으로 추가 유사도 확장 가능
  - rank list + score 를 정규화된 형식으로 노출 (RRF 입력)
- **Success Criteria**: vector-only / graph-only / hybrid 3가지 모드 비교 시 hybrid 의 relevance@10 우위

---

### UC-V9. Working Memory Cache & Writeback

- **Source**: §5 (Working Memory Store), §6.4 결정 6 (자동 writeback)
- **Actor**: CKV Query Layer (자동) + LLM (명시)
- **Trigger**:
  - 자동: 동일 `(run_id, intent_hash)` 재요청
  - 명시: `cks.memory.remember_fact(...)`, `cks.memory.record_decision(...)`, `cks.memory.recall_session(run_id)`
- **Goal**: 한 세션에서 동일 질의를 두 번 임베딩/검색하지 않음 + LLM 이 발견한 사실을 다음 step 에서 재사용
- **Main Flow**:
  1. 질의 시작 시 working_memory[run_id].lookup(intent_hash)
  2. hit & not stale → 캐시 반환
  3. miss → 일반 검색 후 결과 writeback
  4. LLM 의 명시적 호출은 별도 entry 로 저장
- **Persistence**: SQLite or JSON file at `state-store/{run_id}/working_memory.json`
- **Success Criteria**: 한 run 내 cache hit rate ≥ 30% (반복 의도 비율에 비례)

---

### UC-V10. Citation Enforcement (환각 방지)

- **Source**: §6.4 결정 5 ("모든 결과에 citation 강제"), §11 KPI ("Citation accuracy 목표: 100%")
- **Actor**: CKV 모든 query 응답
- **Goal**: file path + line range 가 없는 결과는 LLM 에게 절대 반환하지 않는다
- **Main Flow**:
  1. chunk 메타데이터에 `file`, `start_line`, `end_line` 강제
  2. 응답 직전 검증 단계: citation 누락 → 결과 drop + warning
  3. citation 의 실재성 cheap check (파일 존재 + commit hash 매칭)
- **Success Criteria**: Citation accuracy 100% (eval 측정)

---

### UC-V11. Freshness Check & Stale Warning

- **Source**: §4.6 (Freshness Checker), §10 Failure Modes
- **Actor**: 모든 query 진입점
- **Trigger**: query 마다 cheap check, 또는 명시적 `cks.ops.get_freshness`
- **Goal**: 인덱스가 현재 코드와 얼마나 동기화돼 있는지 caller 에게 알리고, 필요 시 부분 재인덱싱
- **Main Flow**:
  1. `current_head = git rev-parse HEAD`
  2. `index_head = storage.get_meta('indexed_head')`
  3. `git diff --name-only ${index_head} ${current_head}`
  4. query scope 와 무관 → 그대로 응답
  5. query scope 와 관련 → on-demand 부분 재인덱싱 (UC-V2 호출)
  6. 대량 변경 → "partial stale" warning + background full re-index
- **Success Criteria**: Stale 감지 누락 0%, false stale rate < 5%

---

### UC-V12. Bootstrap / Systemization Report

- **Source**: §12.1 Phase E, §12.2 (Code Systemizer)
- **Actor**: 개발자 (`ckv bootstrap --report`), CKS Onboarding (UC-B4)
- **Goal**: 신규 프로젝트의 의미적 클러스터·진입점 후보 등 vector 관점의 systemization 정보 출력
- **Outputs (JSON)**:
  - chunk 분포 (언어별 / 디렉터리별)
  - 의미 클러스터 (k-means or HDBSCAN over embeddings)
  - 자연어 검색 시 자주 매칭될 핫 영역 후보
  - 임베딩 모델, 차원, indexed_head, 생성 시각

---

### UC-V13. Sanitize Evidence (default-deny)

- **Source**: §6.2 Step 8.5, §7 Architectural Rule
- **Actor**: 외부 caller 에 노출되는 모든 응답 직전 단계
- **Goal**: untrusted-origin 텍스트(코드 주석, commit message, Jira body 등)에 대한 prompt-injection 방어
- **Main Flow**:
  1. origin 별 sentinel 부착 (XML tag + markdown fence + language hint)
  2. `policies/sanitization_rules.yaml#cks_evidence_pack` 룰셋 평가
  3. 매치 시 `[REDACTED:{rule_id}]` 치환, 원본은 audit log
  4. fail-closed: 룰 평가 오류 → origin 통째로 redact_full
  5. `metadata.sanitize_report` 동봉
- **Success Criteria**: 알려진 6개 패턴 100% redact, fail_closed_count > 0 시 운영 알림

---

### UC-V14. Resume from Working Memory

- **Source**: §5.5 (Cross-session Recall), UC-D2
- **Actor**: LLM (재개 워크플로우)
- **Trigger**: `cks.memory.recall_session(run_id="prev-run-uuid")`
- **Goal**: 이전 세션의 facts/decisions/touched_files 를 요약 반환 → 새 세션이 컨텍스트 이어받음
- **Success Criteria**: facts/decisions 직렬화·역직렬화 무손실, sanitize 통과 후 반환

---

### UC-V15. Local-First Embedding

- **Source**: §4.2 ("v1 추천: 로컬 실행 가능한 모델", "외부 API 의존 시 비용/지연 폭발")
- **Actor**: 모든 인덱싱/질의 경로
- **Goal**: 외부 API(OpenAI 등) 없이 동작 가능. 외부 모델은 옵션.
- **Main Flow**:
  1. 기본 모델: `BAAI/bge-small-en-v1.5` 또는 `bge-code` 계열 로컬 (ONNX/GGUF)
  2. 모델 인터페이스 추상화 → 외부 provider 는 plugin 으로 교체
  3. 모델 버전 메타키 저장 (`embedding_model`, `embedding_dim`, `embedded_at`) — 모델 변경 시 전체 재임베딩 필요 여부 판단
- **Success Criteria**: airgap 환경에서 build/query 모두 성공

---

## 4. 비기능 요구사항(NFR) — 사용 시나리오 공통

| 영역 | 목표 |
|---|---|
| **확장성** | 단일 노드에서 1M LOC 인덱싱 가능, 메모리 < 8GB |
| **신선도** | 파일 저장 → 인덱스 반영 p95 < 5초 |
| **인용 정확도** | 100% (file:line 실재성) |
| **Latency p95** | semantic_search < 200ms, get_context_for_task < 1s |
| **로컬-우선** | 기본 구성에서 외부 API 의존 0 |
| **결정성** | 동일 입력·인덱스 상태에서 동일 결과 (rerank 무작위성 제거) |
| **Graceful degradation** | Vector DB 다운 시 BM25/Graph 로 fallback (Layer 3 책임이지만 CKV 가 health endpoint 노출) |
| **Audit** | 모든 sanitize 동작은 audit log 에 기록 |

---

## 5. 범위 외(Out of Scope)

CKV 단독 범위에서 다루지 않는 것 — 통합 CKS 단계 또는 CKG 책임:

- **Graph traversal** (calls, implements, references, …) → CKG
- **AST raw query** (정확한 함수 시그니처/필드) → CKG 또는 별도 AST cache 모듈
- **BM25 / 정확 매칭** → 별도 BM25 백엔드 (tantivy 등)
- **RRF fusion 자체의 구현** → CKS Retrieval Orchestrator (Layer 3)
- **Build/Test 실행** → CL2 책임
- **Workflow 실행 엔진** → CS3 책임

CKV 는 위 범위의 컴포넌트들에게 **잘 정의된 입출력**(citation, run_id, score, sanitize_report)을 제공하는 것을 자기 책임으로 한다.

---

## 6. 성공 조건 매핑 (사용자 요구 → UC)

| 사용자 요구 | 충족 UC |
|---|---|
| Vector DB 구축 가능 | UC-V1, UC-V2, UC-V12, UC-V15 |
| Query 지원 | UC-V3, UC-V4, UC-V5, UC-V6 |
| 모호한 자연어로도 정확한 코드 위치 추론 | UC-V3, UC-V4, UC-V10 |
| Graph 프로젝트와 결합해 CKS 형성 | UC-V8, UC-V9, UC-V10 (공통 인용 포맷) |
| MCP 노출 | UC-V6, UC-V13 |
| 백만 라인 규모 코드의 정확한 수정 지점 식별 | UC-V3 + UC-V8 + UC-V11 (freshness) |
