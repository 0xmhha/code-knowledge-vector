# Session Handoff — 2026-06-15

이 문서는 다른 머신·다른 세션에서 작업을 이어받기 위한 **현행 단일 진입점(SoT)**이다.
이전 핸드오프 [`session-handoff-2026-05-29.md`](./session-handoff-2026-05-29.md)는
2026-06-01에 멈춰 있어, 그 이후 6월에 머지된 6개 PR(R1′ 리팩토링 사이클)과
머신 경로 변경을 반영하지 못한다 → **archive로 간주**. 새 세션은 이 문서부터 읽는다.

> **요약:** 2026-05-29 핸드오프가 지시한 "신규 기능 HOLD → 코드베이스 검토 →
> 리팩토링 플랜 → 리팩토링 실행" 중 **리팩토링 실행이 완료**됐다 (R1′ 사이클,
> PR #1~#6, 6/2~6/10). 그 작업들은 이 repo의 docs가 아니라 **별도 플랜 체계
> (`00`/`02`/`03` 문서, `M2.a~d`·`S-11`·`G1` 마일스톤)**를 따랐고, 그 플랜 문서는
> 이 repo에 없다(CKS repo 또는 PR 본문 추정). 이 문서가 그 격차를 메운다.

---

## 0. 환경 (재현 가능 상태, 2026-06-15 검증)

| 항목 | 값 |
|------|-----|
| CKV repo | `/Users/wm-it-25_0220/Work/github/code-knowledge-vector` (머신 변경됨 — 이전 핸드오프의 `wm-it-22-00661` 경로는 무효) |
| Go module | `github.com/0xmhha/code-knowledge-vector` |
| go.mod | `go 1.25.5` / 설치된 toolchain `go1.26.4 darwin/arm64` |
| CKV branch | `main` |
| CKV HEAD | `0c8a28c` (Revert PR #5) |
| 빌드 산출물 | `bin/ckv` |
| Make 사용 | **반드시** `make build/test/lint/fmt` (직접 go 명령 지양) |

검증 결과 (2026-06-15 실행):

```bash
make build   # ✅ exit 0
make test    # ⚠️ internal/embed/coreml 1건만 FAIL — 환경적 baseline (아래 §7.1)
             #    그 외 모든 패키지 ok (bm25, sqlitevec, ckv, embed/ollama, mcp, types ...)
```

---

## 1. 6월 R1′ 리팩토링 사이클 결과 (PR #1~#6, 코드로 검증됨)

이전 핸드오프 이후 머지된 작업. 각 항목은 현재 코드 트리에서 존재를 확인했다.

| PR | 커밋 | 날짜 | 내용 | 코드 검증 |
|----|------|------|------|-----------|
| #1 | `c46bf8a` | 6/2 | **R1′ 리팩토링** | 아래 분해 |
| #2 | `23b22f7` | 6/2 | bge-m3 go-stablenet smoke 테스트 커밋 + excision으로 깨진 Makefile 타깃 복구 | `make rebuild-stablenet`/`gsn-smoke` 존재 |
| #3 | `8886ee4` | 6/5 | `ckv build --docs` — out-of-tree markdown corpora 인덱싱 | — |
| #4 | `2c87393` | 6/8 | **ckgalign** — `chunks.ckg_node_id` 실제 연결 | `internal/ckgalign/` 존재 ✅ |
| #5 | `ea1175b` | 6/8 | stablenet.yaml을 36 verified cks entries로 재생성 | **revert됨** ⤵ |
| #6 | `a43364e` | 6/10 | `ckv build --files-from` allowlist | `internal/filterlist/` 존재 ✅ |
| revert | `0c8a28c` | 6/10 | **#5 되돌림** (아래 §1.2) | classifier 복원 |

### 1.1 PR #1 (R1′) 분해

- **G1 — ollama embedder 승격**: `internal/embed/ollama` → `pkg/embed/ollama`.
  외부 모듈(cks)이 CGO·서브프로세스 없이 in-process로 real Embedder를 구성할 수 있게 함.
  `pkg/embed/ollama/external_smoke_test.go`가 M2.a 외부-import 게이트.
- **S-11 — 구조화된 Freshness**: `pkg/ckv.Engine.Freshness()`가 `freshness.Report`
  (IndexedHead/CurrentHead/ChangedFiles/Stale/Fresh/Warnings) 반환.
  `CheckFreshness() error`는 back-compat로 유지. `cks.ops.freshness`용.
- **governance-test invariant 인덱싱**: `internal/build/invariant_paths.go` 신설.
  `systemcontracts/test/` 경로 한정으로 Tier-3 휴리스틱을 `_test.go`에서도 ON
  (TOCTOU 순서, burn atomicity, equal-power quorum 등 load-bearing 속성 캡처).
- **LLM 전면 제거 (excision)**: `internal/judge/` 패키지 삭제 확인 ✅.
  claude-spawn judge/planner (`exec.Command("claude")`) + dead LLM-prefix API 제거.
  go.mod 의존성 변화 **0** (애초에 anthropic-sdk 미사용, 순수 subprocess였음).
  deterministic 메트릭(recall/MRR/citation/hallucination, `DeterministicScorer`)은 보존.
  → eval의 LLM judging은 이제 **agent/session 레이어**가 `prregress.JudgeScorer`
  주입으로 담당. `ckv eval --pr-fixture`는 Agent 미주입 시 fail-fast.

### 1.2 PR #5 revert 이유 (미해결 과제로 남음)

`stablenet.yaml`을 36개 verified cks entries로 재생성한 #5는 **분류기를 깨뜨려 revert**됐다.
재생성된 카테고리 셋이 catch-all `**` 경로를 도입했고, classifier의 first-match 순서상
거의 모든 파일을 한 버킷으로 흡수 → 파일이 도메인(consensus/state/txpool/cli...)으로
resolve되지 않고 policy coverage 테스트 실패. semantic path classifier 복원.
**"verified cks entries로 정책 뷰 재생성"이라는 목표 자체는 미해결** (§4-E 참조).

---

## 2. 현재 CKV 노출 면 (2026-06-15 코드 기준)

**MCP 도구 15개** (`pkg/mcp/server.go` 등록 확인):

```
검색  semantic_search  keyword_search  vector_search
정제  narrow_candidates  expand_in_file
메타  find_invariants  get_conventions  explain_match
보조  embed  rerank(stub)  related_changes
운영  health  get_freshness  warmup  index
```

**청크 종류 9** (`pkg/types/chunk.go`): symbol, function_split, file_header, doc,
pr_background, pr_solution, commit_message, invariant, convention.

**SQLite 마이그레이션 4개**: 000_baseline / 001_category_guidance /
002_invariant_refs / 003_convention_stats. (자동 적용 + `.bak` 백업;
수동 모드 `CKV_DISABLE_AUTO_MIGRATE=1`.)

**빌드 신규 플래그**: `--ckg <dir>`(이제 실동작), `--docs`, `--files-from <json>`,
`--exclude`. 임베더: `--embedder=ollama|bgeonnx`, `--model-name`.

---

## 3. 남은 작업 리스트 (우선순위별)

> 이전 문서들(backlog / pending-work / roadmap / eval-design)의 미완료 항목을
> 6월 작업으로 갱신해 통합. 완료/보류 사유 포함.

### A. 측정·평가 갭 (구현됐으나 실측 누락 — 최대 미해결 영역)

| 항목 | 상태 | 진입 조건 / 비고 |
|------|------|------------------|
| **CKG↔CKV join 매칭률** | 미측정 | #4 ckgalign으로 `ckg_node_id` join이 **실제 동작** → 미뤄졌던 매칭률 실측(기대 ≥90%) 가능. **최우선 후보.** |
| **bge-m3 go-stablenet 실측** | 미실행 | operator-gated 야간 작업(~10h, `ollama serve`+`ollama pull bge-m3`). `make rebuild-stablenet` → `make gsn-smoke`. CI 게이트 아님. |
| **B1+#6 bge-large 실측** | mock만 | 실모델 + N=50 fixture 측정 미완. |
| **PRR-1 full PR regression** | ⛔ 보류 | throughput 0.74 c/s buffer 부족. |
| **ADR-003 supersede 결정** | 보류 | NEW-9 BM25 rerank가 default-off opt-in(ADR-006 Proposed). bgeonnx 실측 후 결정. |

### B. 코드 미구현

| ID | 항목 | 상태 변동 |
|----|------|-----------|
| **B7** | Symbol ID 정규화 | #4로 in-process alignment 구현됨. 남은 건 CKG↔CKV **정규화 규칙 합의 + integration fixture**. |
| **B10** | Parser fuzz/property 테스트 | 미구현. 블로커 없음, 독립 진행 가능. |
| **#7** | LLM contextual prefix (Phase D.2) | ⏳ throughput buffer 후. R1′가 dead LLM-prefix 코드 제거 → **재구현** 필요. |
| **A2** | `ckv model fetch` CLI | 여전히 stub (`make model-fetch`는 bge-large만). |

### C. S2 이관 (추적만, 현재 대상 아님)

`internal/sanitize/`, `internal/memory/`(Working Memory), `ckv serve` HTTP API,
`cks.ops.stats`/`request_refresh`, Pattern Similarity(code-as-query),
embedding disk cache, Prometheus exporter. (backlog §C 참조.)

### D. CKS 통합 (별도 repo)

이전 핸드오프 §5.1의 CKS-1/2/3(ckvclient에 신규 6도구 추가, MCP 노출, composer 활용)은
**CKS repo 작업**. R1′의 `pkg/embed/ollama`·`Freshness()` 노출이 그 사전작업.

### E. 정책 데이터 재생성 (revert로 미해결)

PR #5의 "verified cks entries로 stablenet.yaml 재생성" 목표는 catch-all 충돌로
revert됨. **도메인 분류(first-match)를 보존하면서** invariant 지식을 통합하는
방식의 재설계 필요. (단, revert 커밋은 "그 invariant 지식은 이미 domain/graph
정책에 존재하므로 잃은 것 없음"이라고 기록 — 재시도 필요성 자체를 재평가할 것.)

---

## 4. 문서 정합성 점검 (해야 할 정리)

| 문서 | 문제 | 조치 |
|------|------|------|
| `session-handoff-2026-05-29.md` | 6월 작업 미반영, 머신 경로 무효 | archive (이 문서로 대체) |
| `plan-2026-05-29-ckv-refactor.md` | Schema-First 계획 — 완료됨(§1.2 이전 핸드오프) | 완료 마킹 |
| `backlog.md` / `pending-work-2026-05-21.md` | B7/#7 상태가 6월 작업으로 변동 | §3 기준으로 갱신 |
| `mcp-tools.md` | 15도구 정확하나 6월 빌드 플래그(`--docs/--files-from/--ckg`) 누락 | 보강 |
| `cks-design-2026-05-29.md` | hypothetical 초안 (이전 핸드오프 §2.4) | 정정/삭제 |
| **누락** | R1′ 참조 `00/02/03` 플랜, `M2/S-11/G1` 마일스톤 | CKS repo 등 출처 확인 후 링크 |

---

## 5. 권장 다음 세션 시작 순서

```bash
cd /Users/wm-it-25_0220/Work/github/code-knowledge-vector
git pull
make build && make test   # coreml 1건 FAIL은 정상 (§7.1)

# 우선순위:
# 1. (이 세션) 신규 handoff 작성 — 완료
# 2. CKG↔CKV join 매칭률 실측 (#4로 가능해짐)
# 3. R1′ 참조 플랜 출처(00/02/03) 확인 → 문서 연결
# 4. 정책 재생성(E) 재설계 여부 결정
```

---

## 7. 주의 사항

### 7.1 `make test`의 coreml 실패는 환경적 baseline

`internal/embed/coreml`는 `libtokenizers`(HuggingFace Rust)를 요구한다. 이 라이브러리가
없는 머신(현재 포함)에서 test 빌드가 실패한다. **CI는 이 패키지를 명시적으로 제외**
(`.github/workflows/ci.yml`, `go list ./... | grep -v '/internal/embed/coreml'`,
commit `abb5ae2`). 그러나 로컬 `make test`(`go test ./...`)는 제외하지 않아 항상 이
1건 실패가 보인다. **코드 회귀 아님.** (개선 후보: Makefile test 타깃도 CI와 동일하게
coreml 제외하거나 build tag 처리.)

### 7.2 Make 우선 + lint baseline

직접 `go build/test` 지양. `make lint`만 golangci-lint(옵션) 실행. errcheck 다수는
프로젝트 전반 `defer x.Close()` 컨벤션 baseline.

### 7.3 LLM 제거됨

R1′ 이후 CKV 바이너리에 LLM 호출 경로 없음. judge/planner는 agent 레이어가
`prregress.JudgeScorer` 주입으로 담당. CKV는 deterministic 메트릭만.

### 7.4 커밋 메시지 스타일

영어 + 요약 + WIP/Phase 등 진행상태 용어 금지 + attribution 줄 금지.

---

## 8. 핵심 파일 인덱스 (6월 신규 포함)

- `pkg/types/chunk.go` — Chunk + 메타데이터 (`ckg_node_id` 포함)
- `pkg/embed/ollama/` — in-process ollama embedder (G1, 승격됨)
- `internal/ckgalign/aligner.go` — ckg_node_id 4-step Lookup (#4)
- `internal/filterlist/filterlist.go` — `--files-from` allowlist (#6)
- `internal/build/invariant_paths.go` — governance-test invariant 경로 (R1′)
- `internal/policy/loader.go` + `policy/stablenet.yaml` — semantic path classifier
- `internal/invariant/extractor.go` — 3-tier invariant
- `internal/convention/stats.go` — AST 통계
- `internal/query/bm25/` — BM25 rerank (NEW-9, default off)
- `internal/store/sqlitevec/migrate.go` + `migrations/*.sql`
- `pkg/mcp/server.go` — MCP 15도구 등록
- `pkg/ckv/` — public Go API (Freshness 포함)

---

이 문서는 작업 진행 시 갱신한다. 큰 작업 진행 시 새 핸드오프를 만들고 이 파일은 archive.
