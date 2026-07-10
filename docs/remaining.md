전달할 CKS 프롬프트 (확정본, 복붙용)

[CKV → CKS] 서빙 CKV 데이터셋 재빌드 완료 — reload 후 P1 서빙 실동작 확인 요청

pr-77-2 서빙 CKV 인덱스를 새 ckv로 재빌드했다(신규 생성). sources 원장 + graph_digest 물림
완료 → 네 서빙측 assert가 commit-only에서 +digest로 강화된다.

1) 위치/좌표 (cks-stablenet.yaml 그대로, 경로 변경 없음):
   ckv : /Users/.../knowledge-data/pr-77-2/ckv   ← 신규 생성됨(이전엔 부재)
   ckg : /Users/.../knowledge-data/pr-77-2/graph.db
   embedder=bge-m3(1024,l2), src_commit=0bf2f4d1b, schema=1.23
   chunks=15909, canonical_id match=13507/14273=94.63%, flow/curated=112, doc=222
   sources.ckg.graph_digest = 4be26516… (그래프와 일치)
   CKV CheckAlignment(bge-m3 실측) = ok / serviceable=true / warnings=[]

2) 네가 할 것:
   a. CKS 재기동/리로드로 새 pr-77-2/ckv를 물린다 (config-swap+restart ~9-15s).
   b. cks.ops.health → alignment.status=ok, serviceable=true, graph_digest 노출 확인.
      → ledger-absent 경고가 자동 소거되는지(이제 sources 블록 존재) 확인.
   c. digest assert 실동작: recorded digest == 현 그래프 digest → ok 판정 확인.

3) 이후 진행 가능(P2): 인스턴스-레벨 blue-green(무중단 전환). 현재 config-swap+restart라
   ~9-15s 다운타임 → 버전 디렉터리 관리 시 무중단 promote 가능. 설계 §6/§4 참고.
   (CKV/CKG=버전본 생산, CKS=소비·promote 주체.)

CKV는 P2(reindex 재정렬 편입)로 넘어간다. 재기동 후 health의 alignment 블록을 공유해줘 —
양측(CKV CheckAlignment / CKS assert)이 같은 digest(4be26516…)로 ok 판정하는지 교차확인하자.

이 프롬프트를 CKS에 전달하시면, CKS는 재기동 → health 확인 → 경고 소거·digest assert 실동작 검증으로 진행할 수 있습니다.

3자 P1 루프가 실운영 데이터셋에 반영됐습니다. CKS 재기동 결과를 공유해 주시면 양측 교차확인하고, CKV는 예정대로 P2(reindex 재정렬 편입) 로 넘어가겠습니다.

