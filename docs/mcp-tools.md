# CKV MCP Tools Reference

문서 버전: 1.1 (2026-06-30 — flow-aware 4도구 추가, 총 19)
대상 ResponseSchemaVersion: `1.1`

CKV의 MCP 서버(`ckv mcp`)가 노출하는 19개 도구의 입출력 스키마, 사용 시나리오, 호출 예시를 정리한다. coding-agent SKILL 작성자, CKS 오케스트레이터 통합자, 외부 MCP 클라이언트가 참조한다.

도구 분류:

| 분류 | 도구 | 용도 |
|------|------|------|
| 검색 | `semantic_search`, `keyword_search`, `vector_search` | 1차 retrieval |
| 정제 | `narrow_candidates`, `expand_in_file` | 후보 좁힘 / 컨텍스트 확장 |
| 메타 | `find_invariants`, `get_conventions`, `explain_match` | 정책·컨벤션·설명 |
| 흐름 | `get_flow`, `expand_flow`, `find_branches`, `get_invariant_enforcement` | flow corpus 기반 현상→원인 |
| 보조 | `embed`, `rerank`, `related_changes` | 저수준 / 보조 |
| 운영 | `health`, `get_freshness`, `warmup`, `index` | 인덱스 운영 |

응답 envelope:
- 모든 도구 응답은 JSON. 상위 필드 `schema_version: "1.1"` 포함.
- 에러는 MCP 표준 `isError: true` + `content[0].text`에 메시지.
- 검색류 응답은 `hits[]` + `count`. 메타 도구는 도구별 키.

---

## 검색

### `cks.context.semantic_search`

**용도:** Full retrieval pipeline (embed → vector search → BM25 rerank? → boost? → threshold → density compress → enrich?).

**입력:**
- `intent` (string, required): 자연어 쿼리
- `k` (number, optional, default 10): top-K
- `language` (string, optional): 언어 필터
- `path_glob` (string, optional): 경로 글로브
- `enable_bm25_rerank` (bool, optional): 후보셋 BM25 + RRF 재순위 (NEW-9)
- `examples_k` (number, optional): 테스트 청크 분리

**출력:**
```json
{
  "hits": [
    {
      "chunk_id": "...",
      "citation": {"file": "...", "start_line": 100, "end_line": 200, "commit_hash": "..."},
      "snippet": "...",
      "score": {"normalized": 0.82, "vector_distance": 0.36, "vector_rank": 1, "bm25_score": 0, "hybrid_rank": 0},
      "language": "go",
      "symbol": "ValidateBlock",
      "symbol_kind": "Method",
      "category": "consensus",
      "guidance": {"also_review": ["state", "params"], "required_tests": ["fork choice"], "watch_out": [...]}
    }
  ],
  "examples": [],
  "metadata": {"tokens_used": 123, "indexed_head_ckv": "...", "fresh": true, "trace_id": "..."}
}
```

**사용 시나리오:** "JWT 토큰 검증 로직" 같은 개념 쿼리. 정확한 심볼명을 모를 때.

---

### `cks.context.keyword_search`

**용도:** BM25 키워드 검색. 정확한 심볼명·도메인 어휘 쿼리.

**입력:**
- `query` (string, required): 토크나이즈할 쿼리 (CamelCase / snake_case 자동 분리)
- `k` (number, optional, default 10)
- `language` (string, optional)
- `path_glob` (string, optional)

**출력:** `semantic_search`와 동일한 Hit 형식 (단, `score.bm25_score`가 채워지고 `vector_distance`는 0).

**사용 시나리오:** "ValidateBlock", "ApplyTransaction" 같은 함수명. semantic이 자연어 개념, keyword가 식별자.

---

### `cks.context.vector_search`

**용도:** 사전 계산된 벡터로 직접 ANN 검색 (rerank/filter 없음).

**입력:**
- `vector_json` (string, required): JSON 배열
- `k` (number, optional, default 10)
- `language` (string, optional)

**사용 시나리오:** `embed`로 만든 벡터를 재사용한 반복 검색.

---

## 정제

### `cks.context.narrow_candidates`

**용도:** 기존 hit ID 리스트를 메타데이터 필터로 좁힘. Score는 0 (재순위 아님).

**입력:**
- `chunk_ids_json` (string, required): JSON 배열의 chunk ID
- `category` (string, optional): policy category 일치
- `language` (string, optional)
- `path_glob` (string, optional)

**출력:** 필터 통과한 hits 리스트.

**사용 시나리오:** `semantic_search`로 30개 받고 → consensus 카테고리만 좁히기.

---

### `cks.context.expand_in_file`

**용도:** 주어진 chunk_id 주변 N개 청크 (같은 파일, line 순). Score 0.

**입력:**
- `chunk_id` (string, required)
- `before` (number, optional, default 2)
- `after` (number, optional, default 2)

**출력:** 정렬된 hits 리스트.

**사용 시나리오:** 함수가 호출된 위치 주변 코드를 보고 싶을 때.

**에러:** unknown chunk_id → `isError: true`

---

## 메타

### `cks.context.find_invariants`

**용도:** invariant 청크 조회. 3-tier 신뢰도 (1 = `// CRITICAL` 등 기존 마커, 2 = 신규 `// INVARIANT:`, 3 = `panic`/`Errorf` 휴리스틱).

**입력:**
- `file` (string, optional): 정확 일치 (빈 값 = 모든 파일)
- `category` (string, optional): policy category 일치
- `tier_min` (number, optional, default 1): 최소 신뢰도 (1/2/3)

**출력:**
```json
{
  "invariants": [
    {
      "chunk_id": "...",
      "file": "systemcontracts/gov_base.go",
      "start_line": 213, "end_line": 213,
      "marker": "WARNING",
      "tier": 1,
      "text": "WARNING: Single-member governance is centralized...",
      "category": "systemcontracts",
      "guidance": {...}
    }
  ],
  "count": 1
}
```

**사용 시나리오:** "이 파일을 수정하기 전 깨지면 안 되는 invariant 확인".

---

### `cks.context.get_conventions`

**용도:** per-package AST 통계 (에러 처리, 로거, 명명, 동시성, 테스트 스타일).

**입력:**
- `package` (string, optional): 디렉토리 prefix (예: `consensus/clique`). 빈 값 = 모든 패키지

**출력:**
```json
{
  "conventions": [
    {
      "chunk_id": "...",
      "file": "consensus/clique/<convention>",
      "package": "consensus/clique",
      "summary": "package: consensus/clique. conventions summary.\nerrors: ...",
      "stats": {
        "file_count": 5, "new_constructors": 2, "mutexes": 1, "channels": 2,
        "errors": {"fmt.Errorf_wrap": 12, "errors.New": 3},
        "loggers": {"log15": 8}
      }
    }
  ],
  "count": 1
}
```

**사용 시나리오:** 새 코드 추가 전 "이 패키지의 에러 처리 컨벤션은?" 조회.

---

### `cks.context.explain_match`

**용도:** 특정 chunk_id가 왜 intent에 매칭되는지 설명.

**입력:**
- `chunk_id` (string, required)
- `intent` (string, required)

**출력:**
```json
{
  "chunk_id": "...",
  "citation": {...},
  "vector_score": {"normalized": 0.82, "cosine_distance": 0.36},
  "keyword_score": {
    "score": 4.2,
    "matched_tokens": ["validate", "block"],
    "query_tokens": ["validate", "block", "header"],
    "chunk_token_size": 156
  },
  "category": "consensus",
  "guidance": {...},
  "symbol": "ValidateBlock"
}
```

**사용 시나리오:** "왜 이 결과가 5위인가?" 디버깅 / 인간 리뷰어 설명.

---

## 보조

### `cks.context.embed`

**입력:** `text` (string, required)
**출력:** `vector` (float32 array), `dimension`

### `cks.context.rerank`

**입력:** `intent` (string), `chunk_ids_json` (string)
**출력:** 현재 stub. 후보-셋 BM25 + RRF로 정밀화 예정.

### `cks.context.related_changes`

**입력:** `file` (string)
**출력:** PR breadcrumb 리스트 (`number`, `title`, `merged_at_utc`).

---

## 운영

### `cks.ops.health`

**출력:** `embedding_model`, `embedding_dim`, `chunk_count`, `indexed_head`, `built_at`, `schema_version`.

### `cks.ops.get_freshness`

**출력:** `stale: bool`, `indexed_head`, `current_head`, `changed_files[]`.

### `cks.ops.warmup`

**용도:** Embedder 사전 로드 (bgeonnx의 모델 로드 비용 회수).

### `cks.ops.index`

**입력:** `mode` ("full" / "incremental"), `project_root` (string)
**출력:** `files_processed`, `chunks_created`, `chunks_updated`, `duration_ms`.

---

## 흐름 (flow-aware)

> 큐레이션된 flow corpus(`ckv build --flow-corpus corpus.jsonl`로 적재된 flow_step /
> flow_spine / curated-invariant 청크)를 기반으로 "현상 → 원인" 인과를 추적한다. corpus가
> 없는 인덱스에서는 빈 결과를 반환한다. 모두 READ-ONLY, bounded(단일 flow / 단일 lookup).
> in-process 소비자(cks ckvclient)는 `pkg/ckv.Engine`의 동명 메서드를 직접 호출 가능.

### `cks.context.get_flow`

**용도:** 한 flow의 step들을 호출(calls) topological 순서로 나열. 각 step의 symbol·citation·
분기·invariant 포함. cycle-safe(사이클 step은 원래 순서로 보존).

**입력:** (셋 중 하나 필수)
- `flow_id` (string): flow id (예: `ep-cli-init`)
- `entry_point` (string): 진입점 id (예: `EP-CLI-INIT`)
- `invariant_id` (string): 불변식 id → 그 invariant의 enforced_at 첫 flow로 해소

**출력:** `{flow: {flow_id, entry_point, trigger, root_symbol, links[], called_by[],
steps: [{step_id, symbol, citation{file,start_line,end_line}, kind, calls[], reads, writes,
emits, branches[{when,then,at}], invariants[]}]}, steps: <count>}`

**사용 시나리오:** "X 작업의 전체 코드 경로를 순서대로 보여줘."

### `cks.context.expand_flow`

**용도:** 한 step의 인접 step 탐색 — downstream(`direction=down`, calls) 또는 upstream
(`direction=up`, callers) — `hops`까지. origin step의 실패 분기도 함께 반환.

**입력:**
- `step_id` (string, required)
- `direction` (string, optional, default `down`): `down` | `up`
- `hops` (number, optional, default 1)

**출력:** `{result: {origin, direction, origin_branches[{when,then,at}],
neighbors: [{step_id, symbol, citation, relation: "calls"|"called_by"}]}, neighbors: <count>}`

**사용 시나리오:** "이 step 직전/직후에 무엇이 호출되나?"

### `cks.context.find_branches`

**용도:** 증상 문구를 가장 관련 있는 flow step들의 실패 분기(when→then@at)에 매핑. flow_step
임베딩 텍스트에 branch.when이 포함돼 있어 실패조건으로도 검색됨. 실 embedder 필요.

**입력:**
- `symptom_text` (string, required): 자연어 증상/실패조건
- `k` (number, optional, default 10): 분기를 뽑을 top-K flow step

**출력:** `{matches: [{when, then, at, step_id, flow_id, symbol, citation, score}], count}`

**사용 시나리오:** "이 에러/현상이 어디서 결정되나?" (현상→원인 진단).

### `cks.context.get_invariant_enforcement`

**용도:** 큐레이션된 불변식이 강제되는 모든 (flow, step, loc) 나열. coding-agent의 코드-도출
구현 불변식 가드레일 enabler.

**입력:** `inv_id` (string, required): 불변식 id (예: `INV-CONSENSUS-01`)

**출력:** `{enforcement: {inv_id, statement, enforced_at: [{flow, step, loc}]}, count}`

**사용 시나리오:** "이 불변식은 코드 어디어디서 검사되나?"

**에러(흐름 4종 공통):** 미존재 flow/step/invariant → `isError: true`. corpus 미적재 인덱스 → 빈 결과.

---

## 멀티홉 패턴 예시

agent가 jira ticket을 받았을 때 권장 호출 순서:

```
1. semantic_search(intent=ticket title, k=20)
   → 1차 후보 20개
2. narrow_candidates(chunk_ids=..., category="consensus")
   → consensus 카테고리만 5개
3. find_invariants(file=narrowed[0].file)
   → 그 파일의 invariant 확인
4. get_conventions(package=parent_dir(file))
   → 패키지 컨벤션 조회
5. expand_in_file(chunk_id=narrowed[0].chunk_id, before=2, after=2)
   → 컨텍스트 확장
6. explain_match(chunk_id=narrowed[0].chunk_id, intent=ticket title)
   → 매칭 이유 (인간 리뷰어용 audit)
```

이 6단계로 토큰 사용량은 grep+read 반복 대비 ~50% 감소 예상 (V1 측정 후 갱신).

**현상→원인 진단(flow corpus 적재 시)**:

```
1. find_branches(symptom_text=ticket 증상)
   → 증상과 일치하는 실패 분기 + 그 step의 file:line
2. get_flow(flow_id=matches[0].flow_id)
   → 그 step이 속한 전체 흐름을 호출 순서로
3. expand_flow(step_id=matches[0].step_id, direction=up)
   → 그 분기 직전 단계(원인 후보)로 거슬러
4. get_invariant_enforcement(inv_id=step.invariants[0])
   → 관련 불변식이 강제되는 다른 지점들 (영향 범위)
```

---

## 응답 스키마 호환성

- `1.0` → `1.1`: 추가 필드 (`category`, `guidance`). 기존 필드 변경·삭제 없음. 1.0 파서는 새 필드를 무시.
- `1.1` → `1.2` (예정): 추가 필드 only. major 1 계열은 호환 유지.
- major bump (`2.0`): 필드 의미 변경 또는 제거. 별도 ADR로 안내.

## 참조

- `pkg/mcp/server.go` (도구 등록 + 핸들러)
- `internal/query/engine.go` (Hit 스키마)
- `pkg/types/chunk.go` (Chunk 메타데이터)
- `docs/SCHEMA.md` (저장 스키마)
- `docs/plan-2026-05-29-ckv-refactor.md` (도구 설계 결정 기록)
