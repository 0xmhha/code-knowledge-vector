# [포인터] `ckg_node_id` 은퇴 · `canonical_id` 단일화 — CKV 작업분

> **ARCHIVED 2026-07-19.** Done — decision in ADR-007; 0 `ckg_node_id`/`CKGNodeID` refs remain in `*.go`. Kept for provenance.

- 상태: **CKV 코드 완료 (2026-07-11)** — 게이트 `grep ckg_node_id|CKGNodeID` → 0. cks 이관 대기.
- 작성일: 2026-07-08 · 상태 갱신: 2026-07-11
- **마스터 문서**: `code-knowledge-system/docs/retire-ckg-node-id.md` (전체 배경·판정·세 repo 체크리스트)
- 관련 ADR: `docs/adr/007-canonical-id-join-key.md`
- **Cross-repo 상태(2026-07-10)**: ckg 작업분 ✅ 마감(코드 변경 없음 — `ckg_node_id`는 ckg에
  0건, 외부 이름). ckv(본 repo) 🔶 진행 중, cks 🔶 진행 중(ckv 다음). 아래 체크박스는 이 세션이
  코드 검증 후 직접 체크.

## 요지

ckv `chunks`가 `ckg_node_id`(위치 해시)와 `canonical_id`(빌드 불변) **두 식별자**를 저장하는데, 조사 결과 `ckg_node_id`는 세 repo·외부 어디에서도 조회 키로 쓰이지 않는 **죽은 필드**다. `canonical_id`로 단일화하고 ckv 공유 표면에서 `ckg_node_id`를 제거한다. (자세한 근거는 마스터 문서.)

CKV는 **생산자**이므로 이관을 **먼저** 수행한다(그래야 cks의 참조가 컴파일 에러로 드러난다).

## CKV 체크리스트 (2026-07-11 완료)

- [x] `pkg/types/chunk.go:196` — `CKGNodeID` 필드 삭제
- [x] `internal/build/builder.go` — `chunks[i].CKGNodeID = e.ID` 스탬프 삭제(루프 조건 `CanonicalID == ""`/`StartLine>0`, `CanonicalID` 스탬프 유지) + 주석 정정
- [~] `internal/ckgalign/aligner.go` — **`Entry.ID`/`Lookup`은 유지**(정렬 *메커니즘*, 게이트 grep과 무관, aligner 테스트 ~17건 보호). 주석의 `ckg_node_id`/`CKGNodeID` 언급만 `canonical_id`로 정정. builder는 `LookupEntry.CanonicalID`만 소비. → `Entry.ID` 실제 제거는 별도 선택 정리.
- [x] `internal/query/engine.go` — 결과 구조체 `CKGNodeID` + `json:"ckg_node_id"` 삭제
- [x] `internal/query/snippet.go` — `CKGNodeID` 매핑 삭제
- [x] `internal/store/sqlitevec/store.go` — 스키마 컬럼·인덱스 `idx_chunks_ckg_node`·INSERT(컬럼+VALUES)·ON CONFLICT·Exec·SELECT×2·scan var/Scan/assign×2 제거
- [x] DB 마이그레이션: inline `CREATE TABLE`/인덱스에서 제거 + fresh 재빌드(reindex-design §4.2 in-place 금지 준수). in-place `DROP COLUMN` 미채택 — 기존 DB의 죽은 컬럼은 무해, `schema_version` 범프 불필요.
- [x] `docs/adr/007-canonical-id-join-key.md`에 "ckg_node_id 은퇴" 후속 기록
- [x] 완료 게이트: `grep -rn "ckg_node_id\|CKGNodeID"` *.go → **0건** · `make build` ok · 영향 패키지 + 전체 37패키지 test green(coreml 환경 baseline 제외)

## 주의

- `canonical_id` 커버리지 백필(빈 canonical 축소)은 **독립 과제**이지 이 제거의 선행조건이 아니다.
- 체크리스트 원안의 "테스트 수정 불필요"는 `CKGNodeID` 참조 한정으로만 참(0건). `Entry.ID`/`Lookup`은 aligner 테스트가 ~17건 사용하므로 원안 item 3(Entry.ID 제거)은 게이트와 무관한 별도 작업으로 이관했다.
- **cks 이관**: CKV(생산자) 완료 → cks의 `ckg_node_id`/`CKGNodeID` 참조가 있으면 컴파일/조회에서 드러난다. cks 세션이 후속.
