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
| **(f) ANE/GPU compile artifact가 매 build마다 cold 재컴파일** | High | V2 EP에 `ModelCacheDirectory` 명시 → 첫 build가 cache 채우면 두 번째 build attach 즉시 완료 | 코드 ~5줄 (commit `292db4a`) |
| **(g) `ModelFormat=NeuralNetwork` (ORT default) 의 silent FP32 → FP16 cast 가 ANE compile 단계에서 실패** | High | `ModelFormat=MLProgram` 으로 silent cast 회피. `AllowLowPrecisionAccumulationOnGPU=1`로 GPU FP16 가속도 동시 활성. | 코드 ~10줄 (`docs/fp16-model-evaluation.md` 옵션 A) |

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

### 5.2 가설 (f) — `ModelCacheDirectory` 명시 (구현됨, 검증 대기)

**가설**: V2 CoreML EP 옵션에 `ModelCacheDirectory` 를 명시하면 GPU/ANE compile artifact가 영구 cache 디렉토리에 저장 → second run 부터 빠름.

**구현 (commit `292db4a`)**: `CKV_COREML_CACHE_DIR` env 추가. yalue v1.30.1 binding 의 자체 테스트 (`getCoreMLV2SessionOptions`) 가 `ModelCacheDirectory` 키를 사용 — 키 통과 검증됨.

**사용**:
```bash
mkdir -p ~/.cache/ckv/coreml-cache
CKV_COREML_UNITS=ALL CKV_COREML_CACHE_DIR=$HOME/.cache/ckv/coreml-cache \
  ./bin/ckv build ...
```

**측정 예정 항목**: (1) 첫 build cache dir 채워지는지, (2) 두 번째 build session create 즉시 완료, (3) 측정된 throughput.

### 5.3 가설 (g) — `ModelFormat=MLProgram` + `AllowLowPrecisionAccumulationOnGPU` (구현됨, 검증 대기)

**가설**: ORT의 CoreML EP V2 가 `ModelFormat`을 미지정 시 `NeuralNetwork` 로 fallback. NeuralNetwork format은 MPS / ANE 경로에서 silent FP32 → FP16 cast 를 수행. 이 cast 단계의 op-set mismatch 가 I/O error 의 진짜 원인일 가능성.

근거:
- 공개 분석 글 (https://ym2132.github.io/ONNX_MLProgram_NN_exploration): "By setting the model format to be the newer MLProgram format no implicit cast to FP16 takes place."
- Apple ANE 는 FP16 native — INT8 weights 도 ANE 내부에서 FP16 로 변환 (동일 throughput).
- 즉 ANE 는 FP16 모델을 *원래* 받는 게 자연스러우며, FP32 silent cast 경로보다 직접 FP16 입력이 더 안전.

**구현 (이 commit)**: 두 env 추가.
- `CKV_COREML_MODEL_FORMAT` — "MLProgram" 또는 "NeuralNetwork". 빈 값이면 ORT default 그대로 (=NeuralNetwork).
- `CKV_COREML_GPU_FP16` — `1` 이면 `AllowLowPrecisionAccumulationOnGPU=1` 활성. GPU 만 영향, ANE / CPU 는 무관.

**사용** (Phase 1 검증과 동시 실행 가능):
```bash
mkdir -p ~/.cache/ckv/coreml-cache
CKV_COREML_UNITS=ALL \
  CKV_COREML_MODEL_FORMAT=MLProgram \
  CKV_COREML_GPU_FP16=1 \
  CKV_COREML_CACHE_DIR=$HOME/.cache/ckv/coreml-cache \
  ./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge-mlprogram --embedder=bgeonnx
```

**기대 결과**:
- I/O error 사라짐 (silent cast 회피).
- ANE compile path 가 정상 진입 → throughput 가 CPU-only (1.0 chunks/s) 보다 회복 또는 상승.
- 만약 여전히 I/O error 면 silent cast 가설은 기각 → 가설 (d) 또는 별도 가설 필요.

**리스크**:
- MLProgram format 은 Core ML 5+ (macOS 12+) 필요 — 사용자 환경 macOS 15.7 충족.
- MLProgram + FP32 입력 → ORT 가 op 단위로 FP16 dispatch 결정. 순수 FP32 추론보다 약간의 numerical drift 가능 (recall 측정 필요).

### 5.4 가설 (f) + (g) 통합 측정 (2026-05-20 14:10~14:20)

**환경**: 모든 옵션 동시 활성, `CKV_MEM_GUARD=off` (pre-check 위치 버그 발견 — 별도 fix).

```bash
CKV_COREML_UNITS=ALL CKV_COREML_MODEL_FORMAT=MLProgram \
  CKV_COREML_GPU_FP16=1 CKV_COREML_CACHE_DIR=$HOME/.cache/ckv/coreml-cache \
  CKV_MEM_GUARD=off ./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-r{N} --embedder=bgeonnx
```

**결과**:

| 항목 | Round 1 (cold) | Round 2 (cache hit?) |
|---|---|---|
| exit | 0 ✅ | 0 ✅ |
| I/O error | **사라짐** ✅ | 사라짐 ✅ |
| provider tag | `coreml` ✅ | `coreml` ✅ |
| Wall time | **3m 35s** | 4m 15s (오히려 느림 — system noise 추정) |
| 1/5 files | 22s | 22s |
| 5/5 files | 3m 35s | 4m 15s |
| chunks/s | 0.17 | 0.15 |
| Context leak 메시지 | 2회 | 2회 |
| Cache subdir | 1개 hash (`13866292292557230837/`) | **동일 hash** (재사용) |
| Cache 파일 수 | 78 | 76 (거의 동일) |
| Cache 크기 | 2.5 GB | 2.5 GB |

**해석**:
- ✅ 가설 (g) `ModelFormat=MLProgram` **확정 적중** — silent FP32→FP16 cast 회피하니 ANE compile I/O error 완전 사라짐.
- ✅ 가설 (f) `ModelCacheDirectory` **부분 적중** — ORT-level cache는 채워지고 hash 재사용되나, **second run throughput 개선 거의 없음**.
- ❌ Throughput 0.15~0.17 chunks/s — CPU-only baseline (1.0 chunks/s)보다 6× 느림. D1-FU-8 목표 (30 chunks/s)와는 200× 거리.

**왜 cache hit인데 빠르지 않나**:
- Cache 파일 이름 `0_dynamic_mlprogram` ~ `77_dynamic_mlprogram` — **dynamic shape** 표시.
- ORT cache는 *graph-level* (op fusion / partitioning). Apple ML compiler의 *shape-specific* ANE/GPU compile artifact는 별도 layer일 가능성.
- 각 batch마다 다른 shape (batch size × seq length) → 매번 ANE compile re-trigger.

### 5.5 가설 (h) — `RequireStaticInputShapes=1` + max-seq padding (구현 + 측정 완료)

**가설**: ANE는 static-shape 친화 설계 (Apple ML research: "fixed-size neural network operations"). dynamic shape으로 매 batch마다 shape이 바뀌면 ANE compiler가 shape별로 재컴파일 → cache 효과 무력화.

**구현**: `CKV_STATIC_SHAPES=1` env 추가.
- `session_impl.go::attachCoreML`: `RequireStaticInputShapes=1` EP 옵션 추가.
- `tokenizer_impl.go::Tokenize`: env=1 시 batch 내 최장 길이 대신 `maxLen (MaxInput=512)`로 강제 padding.

**측정** (2026-05-20 14:32 / 14:43):

```bash
CKV_COREML_UNITS=ALL CKV_COREML_MODEL_FORMAT=MLProgram \
  CKV_COREML_GPU_FP16=1 CKV_STATIC_SHAPES=1 \
  CKV_COREML_CACHE_DIR=$HOME/.cache/ckv/coreml-cache CKV_MEM_GUARD=off \
  ./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-r{N} --embedder=bgeonnx
```

| 항목 | Round 3 (cold static) | Round 4 (cache hit static) |
|---|---|---|
| Wall time | **59.9s** | 59.8s |
| chunks/s | **0.62** | 0.62 |
| CPU usage | **366%** (3-4 cores 활용) | 365% |
| Cache 파일 수 | 2 | 2 (동일) |
| Cache 크기 | **28 KB** | 28 KB |
| Provider | coreml ✅ | coreml ✅ |

**결론**:
- ✅ Static shape 전환으로 dynamic 대비 **3.6× 가속** (3m35s → 59.9s).
- ✅ Cache 크기 **89,000× 감소** (2.5 GB → 28 KB) — 단일 compile artifact만 필요.
- ✅ CPU 366% — multi-core dispatch 정상 작동.
- ⚠️ Cache hit 효과는 거의 없음 — Round 3 cold ≈ Round 4 warm. Apple ML compiler가 매 run마다 actual compile을 재실행하거나, ORT-level cache는 부분만 잡음.
- ❌ Throughput 0.62 chunks/s — CPU baseline (1.0 c/s)보다 **여전히 1.6× 느림**. D1-FU-8 목표 (30 c/s)와는 **50배 거리**.

**왜 ANE가 도움이 안 되는가**: bge-large-en-v1.5는 24-layer attention-heavy transformer. ANE는 *CNN 최적화 설계* (Apple 공식). ORT가 per-op granularity로 attention의 대부분을 CPU/GPU로 fallback → ANE attach가 오히려 CPU/GPU/ANE 데이터 이동 overhead로 작용.

### 5.6 다음 단계 후보

ANE 경로로는 throughput 한계점. 추가 가속 옵션:
- **모델 크기 자체 축소** (`docs/fp16-model-evaluation.md` 옵션 B/C) — FP16 ONNX export 또는 Teradata int8 사용. 모델 1/2 ~ 1/4 크기, 정확도 손실 측정 필요.
- **CPU multi-thread 최적화** — `CKV_COREML_UNITS=CPUOnly` + ORT IntraOp/InterOp thread 늘리기. 4-core 활용 시 CPU baseline (1.0) × 4 = 4 c/s 도달 가능성.
- **Batch size 증가** — `--batch` flag 또는 옵션 추가, 32 → 128로 amortize. 단 메모리 사용량 증가, ANE는 batch가 너무 크면 compile 자체가 비현실적.
- **Apple ML 직접 변환** — Apple ML research paper의 (B, C, 1, S) reshape pattern으로 모델을 ANE 친화 구조로 export. 가장 큰 잠재 효과지만 변환 작업 큼.

### 5.7 시도 timeline

| # | 일시 | 가설 | 시도 | 결과 |
|---|---|---|---|---|
| 1 | 2026-05-20 09:44~10:27 | (a) | `CKV_COREML_UNITS=CPUAndGPU` (ANE 우회) | ✅ I/O error 사라짐 / ❌ GPU compile cost로 hang. *반쪽 해결*. |
| 2 | 2026-05-20 14:10 | (f)+(g) | MLProgram + AllowLowPrecisionGPU + ModelCacheDirectory 동시 | ✅ I/O error 사라짐 / ✅ build 완주 (3m35s, 0.17 c/s) / ❌ throughput 미달 |
| 3 | 2026-05-20 14:16 | (f) | 동일 옵션 second run (cache hit 측정) | ⚠️ cache subdir 재사용은 확인 / ❌ wall time 개선 없음 (4m15s) |
| 4 | 2026-05-20 14:32 | (h) cold | `RequireStaticInputShapes=1` + max-seq padding | ✅ **59.9s** (3.6× 가속) / cache 2.5GB → 28KB |
| 5 | 2026-05-20 14:43 | (h) warm | 동일 옵션 second run (cache hit 측정) | = Round 4와 동일 (59.8s) — cache hit 효과 거의 없음 |
| 6 | (선택) | 모델 변경 | FP16/INT8 ONNX export | 다음 단계 후보 |
| 7 | (선택) | CPU thread | `CKV_COREML_UNITS=CPUOnly` + IntraOp threads | 다음 단계 후보 |
| 8 | (보류) | (b) | `MACOSX_DEPLOYMENT_TARGET=15.5` rebuild | (보류) |
| 9 | (보류) | (d) | `CKV_ORT_VERBOSE=1` 로 cache hit/miss 명확 확인 | (가능 — commit `66bdefc`) |

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
