# 재인덱싱 · DB 마이그레이션 · 무중단 전환 — 크로스-repo 설계 (CKG · CKV · CKS)

> **시점**: 2026-07-10 (설계 제안, 구현 전).
> **범위**: **CKG(그래프) · CKV(벡터) 두 DB의 갱신 + 그 DB를 서빙하는 CKS**. 어느 한 repo만의
> 문제가 아니라, CKG가 만든 canonical_id를 CKV가 소비하므로 **두 DB의 재인덱싱·마이그레이션이
> 조율돼야** 한다.
> **근거**: CKG(`code-knowledge-graph`)·CKV(`code-knowledge-vector`) 빌드/증분 코드 전수 리뷰
> (2026-07-10). 관련: `coordination-prompts-2026-06-29.md`, `pr-retrieval-eval-2026-07-08.md`,
> ckv `docs/adr/007-canonical-id-join-key.md`, ckg `docs/adr/0001·0002`, `docs/INCREMENTAL.md`.

---

## 0. 왜 이 문서인가 (발견된 홀)

데이터는 계속 늘고 바뀐다(코드 커밋, 신규 PR, 도메인지식 갱신, 스키마 진화). 그때마다 두 DB를
갱신해야 하는데, 현재 코드에는 **무결성·조율·무중단을 보장하는 스킴이 대부분 없다.**

### 0.1 두 repo 성숙도 비대칭 (코드 리뷰 결과)

| 항목 | CKG | CKV |
|---|---|---|
| 증분 캐시 | ✅ A3 per-file (`cache_key = sha256(content\|ckg-ver\|parser-ver\|schema-ver)`, mtime, node/edge ids) | ❌ 없음 (git diff만) |
| 증분 라우팅 | short-circuit / incremental / cold | reindex(코드) / full build |
| 증분 범위 | 전 노드·엣지 (단 partial-cache는 **실질 cold fallback** — cross-file 엣지 손실) | **소스코드만** (PR·docs·flow·convention·CKG정렬 제외) |
| 삭제 처리 | FK `ON DELETE CASCADE` (6 테이블) | `DeleteByFile`(chunks+vec) |
| 원자성 | cold = `os.Remove(graph.db)` 후 재빌드(비원자) / SetManifest 트랜잭션 | full build만 manifest 마커 / **reindex 마커 없음** |
| 검증 | `validateAndSanitize`(dangling edge) + FK 강제 | ❌ 없음 |
| 스키마 마이그레이션 | `store.Migrate()` | ✅ `MigrationRunner`(schema_migrations 원장 + `.bak` + dry-run + tamper 감지) |
| **조율 좌표 기록** | graph sha/commit/schema를 **수동** 공표 | ❌ **어느 CKG에 정렬했는지 미기록** |

### 0.2 핵심 갭 3가지
1. **CKG↔CKV 조율 부재**: CKV manifest에 "어느 CKG graph(commit+sha+schema)에 정렬했는지"가
   없고, reindex는 `ckgalign`을 재실행하지 않는다 → CKG 재생성 시 CKV canonical_id가 조용히 stale.
2. **재인덱싱 무결성 부재**: reindex 원자성·롤백·재개·검증·count 정합성 없음(CKV). partial-cache
   NOOP(CKG).
3. **무중단 부재**: CKS가 두 DB를 **in-process로 연 채 서빙** → in-place 변경 시 stale/corruption.

---

## 1. 변화 트리거 × 재인덱싱 매트릭스

"무엇이 바뀌면 무엇을 다시 만들어야 하는가"를 명시한다(놓치는 레이어 방지).

| 트리거 | CKG | CKV | 비고 |
|---|---|---|---|
| 소스 커밋 전진 | 증분(dirty 파일) | 재인덱싱(git diff) + **CKG 재정렬** | 현재 CKV 재정렬 누락 |
| CKG 그래프 재생성(같은 커밋) | — | **canonical_id 재정렬** | 현재 전무 |
| **CKG 스키마 bump** | **cold rebuild 강제** | **전면 재빌드 캐스케이드** | canonical_id 전면 변경 |
| 임베딩 모델 교체 | — | **전면 재빌드**(공간 identity) | PR #12 |
| 신규 PR 머지 | node_prs 갱신 | **PR 인제스트**(cutoff 이후) | 현재 CKV reindex 제외 |
| docs/flow corpus 변경 | — | **도메인지식 재인덱싱** | 현재 제외 |
| policy 변경 | — | category 재적용 | manifest에 policy 해시 없음 |
| 파일 삭제/rename | CASCADE | DeleteByFile + PR breadcrumb 유실 주의 | |

**의존 순서**: `소스 → CKG 그래프 → CKV 정렬 → PR/docs/flow`. 상류가 바뀌면 하류가 트리거된다.

---

## 2. Cutoff 지식 원장 (manifest 확장)

"이 DB는 무엇을 어디까지 학습했는가"를 기계가 읽을 수 있게. 증분·조율·무중단의 전제.

### 2.1 CKG manifest (있는 것 + 추가)
- 있음: `schema_version`, `ckg_version`, `build_timestamp`, `src_commit`(path-aware), `Files[]`
  (per-file `sha256`/`cache_key`/`mtime`/`parser_version`/`node_ids`/`edge_ids`), `Stats`.
- 추가: **`graph_sha256`** (산출물 지문 — CKV/CKS가 pin), **`content_hash`**(스키마 무관 내용 지문).

### 2.2 CKV manifest (있는 것 + 추가)
- 있음: `schema_version`, `ckv_version`, `built_at`, `src_commit`/`indexed_head`,
  `embedding_{model,dim,checksum,normalize}`, `docs_roots`, `chunk_count`.
- **추가(핵심)**:
  ```json
  "sources": {
    "code":  { "indexed_head": "<commit>", "built_at": "<date>" },
    "ckg":   { "graph_sha256": "<sha>", "src_commit": "<commit>", "schema_version": "1.23" },
    "prs":   { "repo": "owner/repo", "last_pr_number": 68, "last_merged_at": "<date>" },
    "docs":  { "root": "...", "content_hash": "<sha>" },
    "flow":  { "corpus": "corpus.jsonl", "content_hash": "<sha>" },
    "policy":{ "path": "stablenet.yaml", "content_hash": "<sha>" }
  }
  ```
- `ckg.graph_sha256`/`src_commit`가 **CKG↔CKV 불일치 감지의 앵커**. `prs.last_pr_number`가 증분 PR
  인제스트의 cutoff. 나머지 `content_hash`는 "바뀌었는지" 감지용.

---

## 3. CKG ↔ CKV 조율 프로토콜

두 DB는 **같은 소스 커밋 + 정합 스키마**에서 만들어져야 canonical_id join이 성립한다.

### 3.1 좌표 pin & 불일치 감지
- CKV 빌드/정렬 시 CKG manifest의 `{src_commit, graph_sha256, schema_version}`을 읽어 CKV manifest에
  기록.
- Open/freshness 시 **CKV.ckg.graph_sha256 ≠ 현재 CKG graph.db sha** → **stale 경고 + 재정렬 필요
  플래그**(현재는 감지 불가).
- `schema_version < 1.19` → canonical_id 신뢰 불가(ADR-007 게이트, 이미 구현).

### 3.2 조율된 갱신 순서 (원자성 §4와 결합)
```
1. CKG: 대상 커밋에서 그래프 (재)빌드 → graph_sha256 산출 → 공표
2. CKV: 그 커밋·그 graph_sha256에 맞춰 (재)빌드/정렬
3. 검증 게이트(§5): canonical_id 매칭률 ≥90% 등 통과해야 promote
4. CKS: 두 새 아티팩트로 원자적 전환(§4) + 재로드
```
- **schema bump 캐스케이드**: CKG가 cache `SchemaVersion`을 올리면(예 1.22→1.23) CKG cold rebuild →
  canonical_id 전면 변경 → **CKV도 전면 재빌드 필수**. 이 캐스케이드를 버전 비교로 자동 트리거.

### 3.3 결정성 전제 (ADR-0002)
CKG가 **결정적**(같은 커밋+바이너리+필터 → 같은 그래프·canonical_id)이어야 CKV 정렬이 재현된다.
비결정적이면 매 재인덱싱마다 canonical_id drift → CKV join 깨짐. → 조율은 항상 `--files-from`
동일 필터 + detached-clean 체크아웃 + 동일 바이너리 버전으로.

---

## 4. 운영 마이그레이션 & 무중단 전환 (blue-green) — 중심 축

**원칙: 라이브 DB를 in-place로 바꾸지 않는다. 새 아티팩트를 옆에 만들고, 검증 후, 원자적으로
승격(swap)하고, 서빙을 재로드한다.** 이 하나가 원자성·서빙안전·롤백을 동시에 해결한다.

### 4.1 불변 버전 아티팩트 + 원자적 승격
- 데이터셋 디렉터리를 **버전화**: `knowledge-data/<dataset>@<commit-or-seq>/{graph-db, vector-db}`.
- 갱신 = **새 버전 디렉터리에 빌드** → 검증 → `current` 심볼릭/포인터를 원자적으로 새 버전으로 전환.
- 크래시 시: 새 버전은 미완성이지만 `current`는 여전히 옛 버전 → **라이브 무손상**. 롤백 = 포인터를
  옛 버전으로 되돌림(옛 디렉터리 보존).
- 이는 CKG cold(`os.Remove(graph.db)`)의 비원자성과 CKV reindex의 마커 부재를 **둘 다 무력화**.

### 4.2 in-place 증분과의 관계
- 작은 코드 변경은 여전히 in-place 증분이 저렴하다. 하지만 **서빙 중인 DB에 직접 하지 않는다**:
  옵션 (a) 증분도 새 버전 디렉터리 복사본 위에서 수행 후 swap, 또는 (b) 증분은 오프라인 워커
  전용이고 서빙본은 항상 swap으로만 교체. → 정책 결정 필요(비용 vs 단순성).

### 4.3 expand-contract (스키마 호환)
- 스키마 변경은 2단계: **expand**(새 컬럼/테이블 추가, 옛·새 바이너리 모두 동작) → 데이터 이행 →
  **contract**(옛 제거). 롤아웃 중 버전 혼재를 허용.
- `schema_version` major/minor 정책이 그 계약(CKV mcp 응답 정책 재사용). additive=minor, breaking=major.

### 4.4 재개 가능(crash recovery) + 멱등
- 스키마: `schema_migrations` 원장(CKV 있음) → 재개·멱등·tamper 감지. CKG도 동급 원장 필요.
- 데이터: **체크포인트 원장** — CKG의 per-file `cache_key`가 이미 이 역할. CKV는 부재 → 파일별
  fingerprint 원장 추가로 "중단 지점부터 재개"(처음부터 아님, 중복 없이).

### 4.5 백업 / 롤백 / 보존
- 스키마 마이그레이션 전 `.bak`(CKV 있음). blue-green은 **옛 버전 디렉터리 = 자동 백업/롤백 지점**.
- 보존 정책: 최근 N개 버전 유지 후 GC.

---

## 5. 무결성 · 검증 게이트 · 동시성

### 5.1 promote 전 검증 게이트 (실패 시 swap 안 함)
- **정합성**: orphan(청크↔벡터, 노드↔엣지) 0, dangling ref 0(CKG `validateAndSanitize` 확장),
  `COUNT(*)` = 기대치(ChunkCount **재조정** — 현재 CKV의 근사 산술 버그 대체).
- **identity**: 임베딩 checksum 일치, 스키마 버전 정합.
- **조율**: canonical_id 매칭률 ≥90%(§3), CKG graph_sha256 pin 일치.
- 게이트 통과 = 새 버전을 `current`로 승격. 실패 = 옛 버전 유지 + 알림.

### 5.2 count 정합성 (CKV 버그 수정)
현재 `ChunkCount += Total-(FilesDeleted+FilesModified)`는 **파일 수를 청크 수로 오산** → drift.
→ 재인덱싱 후 **실제 `SELECT COUNT(*)`로 재조정**하거나 삭제/수정 청크 실수를 추적.

### 5.3 동시성 락
두 repo 모두 **동시 빌드 락 없음**("Concurrent builds undefined"). 조율 재인덱싱은 데이터셋 단위
**advisory lock**(파일락)으로 직렬화. 서빙(읽기)은 swap 방식이라 빌드와 무간섭.

---

## 6. 서빙(CKS) 재로드 조율

- CKS는 `ckvclient.Real → ckv.Open(DataPath)`, `ckgclient → graph.db`로 **in-process 유지**.
- 전환 프로토콜(택1):
  - **(a) config 포인터 + 세션 재시작**: 데이터 경로를 새 버전으로 바꾸고 cks-mcp 재시작(협의 doc의
    "config swap + restart"와 일치). 단순·안전. 짧은 재시작 다운타임.
  - **(b) reload 시그널**: CKS가 `current` 포인터 변화를 감지/신호받아 무중단 재오픈. 무중단이나
    구현 복잡.
- 권고: **Phase 1은 (a)**, 무중단이 요구되면 (b)로 승격.

---

## 7. 단계별 구현 (우선순위)

| Phase | 내용 | repo | 리스크 |
|---|---|---|---|
| **P1 — 좌표·감지·버전화** | CKV manifest `sources` 원장(§2.2) + CKG `graph_sha256` 공표 + 불일치/stale 감지 + 데이터셋 버전 디렉터리(blue-green 골격) | CKV+CKG | 낮음(additive) |
| **P2 — 조율 재인덱싱** | CKV reindex에 `ckgalign` 재정렬 편입 + schema 캐스케이드 자동 트리거 + 검증 게이트(§5.1) | CKV(+CKG pin) | 중 |
| **P3 — 증분 도메인지식·PR** | 증분 PR 인제스트(cutoff 이후) + docs/flow content_hash 기반 재인덱싱 + convention 재발행 | CKV | 중 |
| **P4 — 재개·원자성·락** | 데이터 체크포인트 원장 + reindex 원자성(swap) + count 재조정 + advisory lock + SetManifest 트랜잭션 | CKV+CKG | 중 |
| **P5 — 무중단 서빙** | CKS reload 프로토콜(§6b), 보존/GC, 관측성·감사 | CKS(+CKV/CKG) | 중 |

**P1이 최우선**: 지금은 CKG가 바뀌어도 CKV가 모르는(감지 불가) 상태라, **좌표 기록·불일치 감지**만
있어도 "조용히 깨지는" 최악을 막는다.

---

## 8. 크로스-repo 액션 (3자 분담)

- **CKV**: manifest `sources` 원장, ckgalign 좌표 기록·감지, reindex 재정렬 편입, PR/docs/flow 증분,
  count 재조정, reindex 원자성(swap), 체크포인트 원장.
- **CKG**: manifest에 `graph_sha256` 공표, cold/incremental 원자성(temp+rename), schema-bump→cold
  캐스케이드 신호, `validateAndSanitize` 검증 게이트 노출, per-file cache는 재사용.
- **CKS**: 데이터셋 버전 포인터 소비 + 재로드 프로토콜(§6), 조율 재인덱싱 오케스트레이션(순서 §3.2),
  advisory lock 소유.

---

## 9. 미해결 / 결정 필요
- 증분을 서빙본에 직접 vs 항상 swap-only(§4.2) — 비용/단순성 트레이드오프.
- 도메인지식(flow/docs)의 **커밋별 버전 관리** 여부(현재 단일 스냅샷 → 시점평가 leakage 문제,
  `pr-retrieval-eval-2026-07-08.md` §연관).
- CKS 재로드 (a) 재시작 vs (b) 무중단 — 요구 다운타임 SLA에 따라.
- 데이터셋 버전 네이밍/보존 정책.

---

이 문서는 3자(CKG·CKV·CKS) 공유 설계다. 합의 후 각 repo가 P1부터 착수하고, 결정사항은 각 ADR로 승격.
