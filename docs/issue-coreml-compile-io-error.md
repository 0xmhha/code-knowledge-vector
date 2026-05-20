# Issue — bgeonnx CoreML compile I/O error

> **발견일**: 2026-05-19 (N=34 baseline 측정 시)
> **현재 상태**: 🔴 open
> **영향**: bgeonnx embedder가 macOS arm64에서 CoreML EP로 모델 컴파일 실패 → `CKV_DISABLE_COREML=1` CPU fallback 강제 → throughput 1.6 → 1.0 chunks/s regression.
> **연관**: [`backlog.md`](./backlog.md) **A1**, [`retrieval-quality-roadmap.md §10`](./retrieval-quality-roadmap.md), [`d1-onnx-poc.md §3.3`](./d1-onnx-poc.md)
> **관련 commit**: main `555b0c4 feat(bgeonnx): attach CoreML execution provider on macOS`

---

## 1. Symptom

`./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge-n34 --embedder=bgeonnx` 실행 시:

```
Context leak detected, msgtracer returned -1
Context leak detected, msgtracer returned -1
Context leak detected, msgtracer returned -1
Context leak detected, msgtracer returned -1
ckv: 1/5 files (0.0 files/s, elapsed 3m23s, ETA 13m33s)
ckv: embedder bgeonnx:
  bgeonnx: default session: create ONNX session:
  Error creating C session from file:
  Error compiling model: compiler error:
  Encountered an error while compiling a neural network model:
  I/O error
```

build progress가 1/5에서 멈추고 ~3분 20초 timeout 후 fail. `Context leak detected, msgtracer returned -1` 는 Apple ANE/CoreML 측 메시지 (NSObject Runtime의 IPC tracer).

## 2. Context

| 항목 | 값 |
|---|---|
| **OS** | Darwin (macOS) — `uname -a` 확인 시 `Darwin 24.6.0` (macOS 15.0 Sequoia) |
| **arch** | arm64 (Apple Silicon, M-series) |
| **Go version** | 1.25.5 (toolchain auto-fetch via `~/.gvm/gos/go1.25.2/bin/go`) |
| **ckv build tags** | `bgeonnx` |
| **CKV commit** | (issue 발견 시점) `ad804be` ~ `555b0c4` |
| **ONNX Runtime binding** | `yalue/onnxruntime_go` (정확한 버전은 `go.mod` 참조) |
| **libonnxruntime** | `/opt/homebrew/lib/libonnxruntime.dylib` (Homebrew install) |
| **libtokenizers** | `~/lib/libtokenizers.a` (39 MB, 2026-03-11 build) |
| **모델** | `~/.cache/ckv/models/bge-large-en-v1.5/onnx/model.onnx` (~1.3 GB FP32, 2026-05-17 download) |
| **CoreML EP API** | `AppendExecutionProviderCoreML(0)` (V1, flags=0 = ALL compute units + MLProgram) — main `555b0c4` 코드 |
| **CKV_DISABLE_COREML** | unset (즉, CoreML EP attach 시도) |

### 2.1 Reproduce 절차

```bash
# 환경
export CGO_LDFLAGS="-L$HOME/lib"
unset CKV_DISABLE_COREML

# 빌드 (CoreML 시도)
./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge-repro --embedder=bgeonnx

# 결과: 위 §1 Symptom의 error 재현
```

### 2.2 우회 (CPU fallback)

```bash
CKV_DISABLE_COREML=1 \
  ./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge-cpu --embedder=bgeonnx
# 정상 동작, 37 chunks / 35.7s ≈ 1.0 chunks/s
```

## 3. Impact

| 영역 | 영향 |
|---|---|
| **build throughput** | 1.6 → 1.0 chunks/s. 1M LOC 추정 시 ~17시간 → ~28시간 (CoreML 정상 시 ~1시간 기대치 vs 현재 28시간). |
| **Phase D.2 (LLM contextual prefix)** | Roadmap §12 #7 — throughput 0.2~0.4 chunks/s 까지 추가 악화 예상. CoreML buffer 없이는 1M LOC ≥ 60시간. |
| **데모 / production 사용성** | CPU-only 인덱싱은 *demo 규모* 만 가능. 실제 repo 적용 안 됨. |
| **CKV의 architectural 가정** | "초기 build = one-time cost" (사용자 결정 2026-05-19) 가 throughput buffer 위에서 성립. CoreML 부재 시 *one-time cost*가 비현실적이 되어 **`ckv reindex` (S1.5, backlog C1)** 가 더 시급해짐. |

## 4. Hypotheses

추측 원인을 가능성 순으로:

| 가설 | 가능성 | 검증 방법 | 비용 |
|---|---|---|---|
| **(a) Apple ANE compile cache 손상** | High | `~/Library/Caches/com.apple.coreml/` 디렉토리 제거 후 재시도. 첫 호출 시 ANE 가 cache 재생성. | rm 한 번, 재build 35초 |
| **(b) macOS 15.0 ↔ libtokenizers.a 빌드 환경 15.5 불일치 영향이 CoreML compiler 영역까지** | Mid | `MACOSX_DEPLOYMENT_TARGET=15.5` 환경 변수 설정 후 ckv rebuild + retry. 또는 libtokenizers를 macOS 15.0으로 rebuild. | env var 한 줄 + rebuild |
| **(c) yalue/onnxruntime_go의 `AppendExecutionProviderCoreML(0)` (V1 API) 와 모델 호환성** | Mid | `AppendExecutionProviderCoreMLV2(map[string]string{"ModelFormat":"MLProgram","MLComputeUnits":"ALL"})` 로 API 전환. 또는 flags 변경. | 코드 ~5 줄 |
| **(d) bge-large-en-v1.5 ONNX 모델 안에 CoreML 미지원 operator** | Low | ORT verbose log enable (`ORT_LOGGING_LEVEL=VERBOSE`) — fallback operator 확인 | 환경 변수 + 로그 분석 |
| **(e) 모델 파일 권한 / 경로 / 손상** | Very Low | `ls -la` + sha256 verify. *이미 확인됨* — 1.3GB, read OK. | (검증 완료) |

## 5. Resolution Attempts

> 시도 시점·시도 내용·결과를 timestamped log로 누적.

### 5.0 사전 확인 (2026-05-20)

| 항목 | 결과 |
|---|---|
| `~/Library/Caches/com.apple.coreml/` 존재 | ❌ **부재** — 가설 (a) 가 *cache 손상*이 아니라 *cache 생성 자체 실패* 일 가능성 |
| `~/Library/Caches/com.apple.aned/` 존재 | ❌ 부재 |
| `~/Library/Caches/com.apple.neuralengine/` 존재 | ❌ 부재 |
| `~/Library/Containers/com.apple.coreml-validators/` 존재 | ❌ 부재 |
| system 도메인 ANED 데몬 | ✅ **정상** — `com.apple.aned` (PID 73552), `com.apple.aneuserd` (PID 96328) 모두 running |
| ckv binary deployment target | minos=15.0, sdk=15.5 (실행 OS는 15.7.4) |
| 모델 파일 권한 / 무결성 (가설 e) | ✅ 1.3 GB, read OK |
| 현재 코드의 CoreML API | ✅ **이미 V2** (`AppendExecutionProviderCoreMLV2(coreMLOpts)`). 가설 (c) "V2 전환" **무효** — 이미 V2 사용 중 |
| 디스크 여유 | ✅ 33 GB free |

→ user-space CoreML cache 디렉토리들이 *모두 부재*. 시스템 ANED 데몬 자체는 정상. user-process (ckv) 가 ANE compile cache write을 시도하다 *I/O 단계* 에서 실패하는 것이 가장 그럴듯.

### 5.1 가설 (a) ANE 우회 — `MLComputeUnits=CPUAndGPU` (2026-05-20)

**변경**: `internal/embed/bgeonnx/session_impl.go::attachCoreML` 에 `CKV_COREML_UNITS` 환경변수 추가, default 는 `ALL` 유지하되 override 가능.

```diff
- coreMLOpts := map[string]string{"MLComputeUnits": "ALL"}
+ units := strings.TrimSpace(os.Getenv("CKV_COREML_UNITS"))
+ if units == "" { units = "ALL" }
+ coreMLOpts := map[string]string{"MLComputeUnits": units}
```

**시도**:
```bash
CGO_LDFLAGS="-L$HOME/lib" CKV_COREML_UNITS=CPUAndGPU \
  ./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge-cpugpu3 \
  --embedder=bgeonnx
```

**결과**: ✅ **`Encountered an error while compiling a neural network model: I/O error` 사라짐**. provider tag = `coreml` (attach 성공). 단:

| 측정 | 값 |
|---|---|
| Build 1/5 files 도달 | 2분 11초 (cold GPU compile) |
| Build 2/5 files 도달 | 2분 35초 |
| 5분 14초 timeout 시점 | **여전히 2/5 — 사실상 hang** |
| Throughput (5분 partial) | < 0.1 chunks/s (CPU 단독 1.0 보다도 *10× 느림*) |
| CPU 사용 | 43% (idle 아님 — GPU compile 진행 중 추정) |

**해석**:
- ANE 단계의 *I/O error* 자체는 회피됨 (가설 a 부분 확인).
- 그러나 GPU compile 비용이 너무 큼 — bge-large-en-v1.5 (335M, 24-layer BERT) 가 Apple GPU에 *한 번에* compile 시도되며 매우 오래 걸림.
- *Cache 부재* 가 root cause 일 가능성 — 만약 ModelCacheDirectory 가 명시되어 매 build 마다 *동일 캐시* 사용 가능하면 first run만 느리고 second 부터 빠를 것.
- 현재 코드는 cache 디렉토리 명시 안 함 → ORT가 임시 위치 사용 → process 종료 시 사라짐.

→ **가설 (a) 부분 검증**. 다음 시도가 필요 = **ModelCacheDirectory 명시** (신규 가설 f).

### 5.2 신규 가설 (f) — `ModelCacheDirectory` 명시 (대기)

**가설**: V2 CoreML EP 옵션에 `ModelCacheDirectory` 를 명시하면 GPU/ANE compile artifact가 영구 cache 디렉토리에 저장 → second run 부터 빠름.

**시도 예정 코드**:
```go
coreMLOpts := map[string]string{"MLComputeUnits": units}
if cache := os.Getenv("CKV_COREML_CACHE_DIR"); cache != "" {
    coreMLOpts["ModelCacheDirectory"] = cache
}
```

**사용**:
```bash
mkdir -p ~/.cache/ckv/coreml-cache
CKV_COREML_UNITS=ALL CKV_COREML_CACHE_DIR=$HOME/.cache/ckv/coreml-cache \
  ./bin/ckv build ...
```

**리스크**:
- `ModelCacheDirectory` 옵션을 yalue/onnxruntime_go 의 V2 binding 이 지원하는지 미확인 — 미지원 시 silently ignored 또는 attach error.
- ANE/GPU compile 자체가 cache 없이도 빠른 코드 path를 가져야 하는데 매번 cold compile 한다면 cache가 의미 없을 수도.

| # | 일시 | 가설 | 시도 | 결과 |
|---|---|---|---|---|
| 1 | 2026-05-20 09:44~10:27 | (a) | `CKV_COREML_UNITS=CPUAndGPU` (ANE 우회) | ✅ I/O error 사라짐 / ❌ GPU compile cost로 hang. *반쪽 해결*. |
| 2 | (pending) | (f) | `ModelCacheDirectory` 명시 | (대기 중) |
| 3 | (pending) | (b) | `MACOSX_DEPLOYMENT_TARGET=15.5` rebuild | (대기 중) |
| 4 | (pending) | (d) | `ORT_LOGGING_LEVEL=VERBOSE` 로 정확한 fault stage 확인 | (대기 중) |

## 6. Resolution & Lessons

> 최종 해결 시 root cause + lesson 기록.

(미해결)

## 7. References

- ONNX Runtime CoreML EP 공식 문서: https://onnxruntime.ai/docs/execution-providers/CoreML-ExecutionProvider.html
- yalue/onnxruntime_go README의 EP attach 섹션
- Apple ANE compile cache 위치: `~/Library/Caches/com.apple.coreml/` (공식 문서 없음, 경험적 확인)
- `MACOSX_DEPLOYMENT_TARGET` semantics: https://developer.apple.com/library/archive/documentation/DeveloperTools/Conceptual/cross_development/Configuring/configuring.html

## 8. 변경 이력

| 일자 | 변경 |
|---|---|
| 2026-05-20 | 초안 — N=34 측정 (2026-05-19) 발견 + 가설 5건 + reproduce 절차 + impact 분석. 사용자 결정으로 가설 (a)부터 검증 진행. |
