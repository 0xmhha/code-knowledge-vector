# [포인터] `ckg_node_id` 은퇴 · `canonical_id` 단일화 — CKV 작업분

- 상태: In progress (전용 세션) — CKV가 생산자로 먼저 수행
- 작성일: 2026-07-08 · 상태 갱신: 2026-07-10
- **마스터 문서**: `code-knowledge-system/docs/retire-ckg-node-id.md` (전체 배경·판정·세 repo 체크리스트)
- 관련 ADR: `docs/adr/007-canonical-id-join-key.md`
- **Cross-repo 상태(2026-07-10)**: ckg 작업분 ✅ 마감(코드 변경 없음 — `ckg_node_id`는 ckg에
  0건, 외부 이름). ckv(본 repo) 🔶 진행 중, cks 🔶 진행 중(ckv 다음). 아래 체크박스는 이 세션이
  코드 검증 후 직접 체크.

## 요지

ckv `chunks`가 `ckg_node_id`(위치 해시)와 `canonical_id`(빌드 불변) **두 식별자**를 저장하는데, 조사 결과 `ckg_node_id`는 세 repo·외부 어디에서도 조회 키로 쓰이지 않는 **죽은 필드**다. `canonical_id`로 단일화하고 ckv 공유 표면에서 `ckg_node_id`를 제거한다. (자세한 근거는 마스터 문서.)

CKV는 **생산자**이므로 이관을 **먼저** 수행한다(그래야 cks의 참조가 컴파일 에러로 드러난다).

## CKV 체크리스트

- [ ] `pkg/types/chunk.go:196` — `CKGNodeID` 필드 삭제
- [ ] `internal/build/builder.go:340,346` — `chunks[i].CKGNodeID = e.ID` 스탬프 삭제(루프 조건은 `CanonicalID`/`StartLine>0` 기준으로, `CanonicalID` 스탬프는 유지)
- [ ] `internal/ckgalign/aligner.go` — `Entry.ID` 및 `LookupEntry` 반환의 ID 제거(유일 소비처가 builder.go:346)
- [ ] `internal/query/engine.go:159` — 결과 구조체 `CKGNodeID` + `json:"ckg_node_id"` 삭제
- [ ] `internal/query/snippet.go:136` — `CKGNodeID` 매핑 삭제
- [ ] `internal/store/sqlitevec/store.go` — 스키마 `ckg_node_id`(146)·인덱스 `idx_chunks_ckg_node`(174)·INSERT(293,308,352)·SELECT/scan(431,478,519,557) 제거
- [ ] DB 마이그레이션: `CREATE TABLE`에서 제거 + `schema_version` 범프로 콜드 리빌드(결정론적 재생성이라 안전) — 권장
- [ ] `docs/adr/007-canonical-id-join-key.md`에 "ckg_node_id 은퇴" 후속 기록
- [ ] 완료 게이트: `grep -rn "ckg_node_id\|CKGNodeID"` → 0건

## 주의

- `canonical_id` 커버리지 백필(빈 canonical 축소)은 **독립 과제**이지 이 제거의 선행조건이 아니다.
- ckv 테스트에는 `CKGNodeID` 참조 0건 — 테스트 수정 불필요.
