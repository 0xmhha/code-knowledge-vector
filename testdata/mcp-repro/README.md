# CKV-1 reproduction scripts

`docs/followups-from-cks-dogfood-2026-05-19.md` CKV-1 항목의 검증 절차에서 사용한 stdio
JSON-RPC 재현 스크립트. ckv MCP server가 두 "failing query"에 대해 정말로 hang하는지
직접 확인하기 위한 도구.

## 결론 (2026-05-20)

ckv-side에서는 재현되지 않는다. 두 스크립트 모두 모든 query를 100 ms 안에 응답.
세부는 docs/followups-from-cks-dogfood-2026-05-19.md CKV-1 행 참조.

## 사용법

```bash
# 1) 검증할 corpus를 ckv로 index (예: cks repo)
cd <cks-or-other-repo>
ckv build --src=. --out=.ckv-data --embedder=mock

# 2) 검증할 corpus와 ckv binary를 env로 지정
export CKV_BIN=$(which ckv)              # 또는 ./bin/ckv 절대경로
export CKV_DATA=/path/to/.ckv-data       # `ckv build` 결과
export CKV_CWD=/path/to/source-repo      # ckv가 manifest src_root resolve할 곳

# 3) serial — query 하나씩 round-trip
python3 testdata/mcp-repro/serial.py

# 4) concurrent — 8 query를 한 번에 던져 mcp-go의 multi-call 패턴 emulate
python3 testdata/mcp-repro/concurrent.py
```

정상 출력 예:

```
[init] OK
[control] 0.019s | bytes=8050 isError=False | q='MCP server tool registration'
[fail1]   0.013s | bytes=12844 isError=False | q='EvidencePack integrity hash stamping and verification'
[fail2]   0.011s | bytes=17206 isError=False | q='table-driven tests for FilesystemFetcher line range reads and path escape rejection'
```

`HANG` 라인이 보이면 ckv-side 재현이 된 것 → CKV-1 reopen 필요.
