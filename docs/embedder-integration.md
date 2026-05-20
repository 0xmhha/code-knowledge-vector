# CKV Embedder Integration Guide

> **대상**: ckv를 *consumer*로 사용하려는 코드 — cks 같은 downstream Go 패키지,
> 별도 CLI 도구, 통합 테스트.
> **다른 문서**: `d1-installation-guide.md` 는 ckv 자체를 빌드·실행하려는 *개발자*
> 시점. 이 문서는 *integrator* 시점.
> **버전**: 2026-05-20 시점 ckv main.

ckv는 두 가지 통합 형태를 제공한다:

| 형태 | 어떻게 | 언제 |
|---|---|---|
| **in-process (pkg/ckv)** | Go import로 `Engine`을 직접 생성 | 같은 process 안에서 검색 — cks composer 같은 *직접 통합*. 추천. |
| **subprocess (`ckv mcp`)** | binary 를 stdio MCP server로 spawn | 언어가 다르거나 process 격리가 필요한 케이스 |

이번 가이드는 두 형태 모두 다루되 **in-process를 우선**으로 설명한다.

---

## 1. Quickstart — mock embedder

> *Why mock 먼저*: 시스템 의존성 (ONNX runtime, tokenizers) 없이 통합 자체를
> 검증할 수 있다. 의미 있는 recall은 안 나오지만 *흐름* 검증에는 충분하다.

```go
package main

import (
    "context"
    "fmt"
    "log"

    "github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

func main() {
    // 1) index를 미리 build: `ckv build --src=. --out=.ckv-data --embedder=mock`
    //    (또는 build를 in-process로 — internal/build.Run 사용)

    // 2) Open + query
    engine, err := ckv.Open(".ckv-data", ckv.OpenOptions{
        Embedder: ckv.MockEmbedder(),
    })
    if err != nil {
        log.Fatal(err)
    }
    defer engine.Close()

    resp, err := engine.SemanticSearch(
        context.Background(),
        "tokenizer encoding pipeline",
        ckv.SearchOptions{K: 5, Threshold: -1},
    )
    if err != nil {
        log.Fatal(err)
    }
    for _, hit := range resp.Hits {
        fmt.Printf("%s:%d-%d  score=%.3f  %s\n",
            hit.Citation.File, hit.Citation.StartLine, hit.Citation.EndLine,
            hit.Score.Normalized, hit.Symbol)
    }
}
```

핵심 호출 3개:
- `ckv.Open(path, OpenOptions{Embedder})` — manifest 검증 + store open
- `engine.SemanticSearch(ctx, intent, SearchOptions)` — embed → retrieve → snippet
- `engine.Close()` — idempotent

`SearchOptions` 의 zero value는 `K=10, Threshold=0.4, BudgetTokens=4000` 으로
복원된다. `Threshold=-1` 은 score gate 비활성.

---

## 2. Production — bgeonnx embedder

> 진짜 semantic 검색에는 bgeonnx (ONNX runtime + bge-large-en-v1.5) 가 필요하다.
> mock은 hash-based feature라 의미 신호가 없다 (`recall@5 ≈ 0.67` 수준).

### 2.1 시스템 의존성

`d1-installation-guide.md` §1 의 절차를 그대로 따른다:

- **macOS**: `brew install onnxruntime` + libtokenizers.a 다운로드 → `~/lib/`
- **Linux**: ONNX runtime tarball + libtokenizers.a 다운로드 → `~/lib/`
- 환경변수: `export CGO_LDFLAGS="-L$HOME/lib"`

### 2.2 모델 다운로드

bge-large-en-v1.5 (1.3 GB) 를 `~/.cache/ckv/models/bge-large-en-v1.5/` 에 배치.
`hf` CLI 또는 동등한 도구로:

```bash
hf download BAAI/bge-large-en-v1.5 --local-dir ~/.cache/ckv/models/bge-large-en-v1.5
```

`d1-installation-guide.md` §3 참조.

### 2.3 consumer 측 코드

bgeonnx는 build tag (`-tags bgeonnx`) 가 필요하다. consumer 패키지가 bgeonnx
adapter를 import 하면 같은 build tag가 *consumer build* 에도 전파된다.

```go
//go:build bgeonnx

package mycks

import (
    "github.com/0xmhha/code-knowledge-vector/internal/embed/bgeonnx"
    "github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

func openProductionEngine(indexPath, modelDir string) (*ckv.Engine, func(), error) {
    adapter, err := bgeonnx.Open(bgeonnx.Options{ModelDir: modelDir})
    if err != nil {
        return nil, nil, err
    }
    engine, err := ckv.Open(indexPath, ckv.OpenOptions{Embedder: adapter})
    if err != nil {
        adapter.Close()
        return nil, nil, err
    }
    cleanup := func() {
        engine.Close()
        adapter.Close()
    }
    return engine, cleanup, nil
}
```

> **참고**: `bgeonnx` 패키지는 `internal/embed/bgeonnx`. Go internal 규칙상
> 동일 module 안에서만 import 가능. 외부 module이 bgeonnx를 직접 쓰려면
> ckv module을 vendor 하거나, bgeonnx-wrapped factory를 pkg/ckv 에 추가해야
> 한다 (현재는 미제공 — bgeonnx의 system dependency를 pkg API 표면에서
> 분리하기 위함).

### 2.4 빌드

```bash
CGO_LDFLAGS="-L$HOME/lib" go build -tags bgeonnx ./...
```

---

## 3. Subprocess MCP 통합 (legacy)

언어가 다르거나, process 격리가 필요하거나, ckv를 vendor 할 수 없는 경우:

```bash
ckv mcp --out=.ckv-data --embedder=bgeonnx
```

stdio 위에서 MCP JSON-RPC. tool 이름:
- `cks.context.semantic_search` — `intent` 필수, 옵션 (`k`, `language`,
  `path`, `symbol_kind`, `budget_tokens`, `threshold`, `examples_k`)
- `cks.ops.get_freshness`
- `cks.ops.health`

검증 스크립트는 `testdata/mcp-repro/` (serial + concurrent).

**한 줄 권고**: 동일 module 안에서 ckv를 쓸 수 있으면 pkg/ckv가 stdio buffer /
subprocess lifecycle / restart 로직을 모두 없앤다. 2026-05-20 측정 기준
in-process는 stdio MCP 대비 10× 이상 빠른 wall-time.

---

## 4. Runtime tuning — environment overrides

ckv의 모든 환경변수는 *override*다. 미설정이면 안전한 default를 사용한다.

### 4.1 시스템 / 메모리

| ENV | 의미 | Default |
|---|---|---|
| `CGO_LDFLAGS` | libtokenizers 위치 | (필요 시 `-L$HOME/lib`) |
| `CKV_ONNXRUNTIME_LIB` | libonnxruntime 절대경로 override | macOS: `/opt/homebrew/lib/libonnxruntime.dylib` |
| `CKV_MEM_GUARD` | 메모리 가드 활성/비활성 (`off` 로 비활성) | active |
| `CKV_MEM_GUARD_LOW_MB` | 런타임 watchdog threshold (MB) | 500 |

메모리 가드의 두 layer:
- **Pre-check** (`build` 시작): `EstimatedRAMMB × 1.5` 보다 가용 메모리 적으면
  refuse. bgeonnx + bge-large는 ~7500 MB 필요.
- **Watchdog** (build 진행 중): 5초 polling, free RAM < `LOW_MB` 면 batch
  size를 동적 축소 (절반씩, 최소 1).

### 4.2 ONNX runtime / CoreML EP (macOS only)

| ENV | 의미 | Default |
|---|---|---|
| `CKV_DISABLE_COREML` | CoreML EP 자체를 skip, CPU로 강제 | attach |
| `CKV_COREML_UNITS` | `MLComputeUnits`: ALL / CPUAndGPU / CPUOnly | ALL |
| `CKV_COREML_MODEL_FORMAT` | NeuralNetwork (legacy) / MLProgram (FP32 보존) | (unset → ORT default = NeuralNetwork) |
| `CKV_COREML_GPU_FP16` | `AllowLowPrecisionAccumulationOnGPU=1` | off |
| `CKV_STATIC_SHAPES` | `RequireStaticInputShapes=1` + tokenizer가 maxLen으로 padding | dynamic |
| `CKV_COREML_CACHE_DIR` | `ModelCacheDirectory` 절대경로 | ORT temp |
| `CKV_ORT_VERBOSE` | ORT env + session log level VERBOSE | off |
| `CKV_ORT_INTRA_THREADS` | 단일 op 안 thread (matmul 등) | ORT default (logical cores) |
| `CKV_ORT_INTER_THREADS` | 독립 op 사이 thread | ORT default = 1 |

### 4.3 권장 production 조합

**가장 빠른 옵션 (실측 기준)**:
```bash
# CPU pure — bge-large는 ANE-비친화 transformer라 CoreML attach overhead가
# 손해. ORT default thread (auto = P-cores 4개) 가 sweet spot.
export CKV_DISABLE_COREML=1
ckv build ...
```

**CoreML 시도 (Apple Silicon, ANE 최적화 모델로 교체 시 의미 있음)**:
```bash
export CKV_COREML_UNITS=ALL
export CKV_COREML_MODEL_FORMAT=MLProgram      # silent FP16 cast 회피
export CKV_COREML_GPU_FP16=1                  # GPU accumulation 가속
export CKV_STATIC_SHAPES=1                    # ANE는 fixed shape 친화
export CKV_COREML_CACHE_DIR=$HOME/.cache/ckv/coreml-cache
ckv build ...
```

---

## 5. Performance baseline (2026-05-20)

`testdata/sample` (5 source files, 37 chunks) 기준. 자세한 측정 history는
`docs/issue-coreml-compile-io-error.md` §5 참조.

| 설정 | Wall | chunks/s | 비고 |
|---|---|---|---|
| mock embedder | 0.02 s | ~1850 | hash-based, 의미 신호 없음 |
| bgeonnx, **CPU pure (default thread)** | **50.1 s** | **0.74** | **현재 best 실제 모델** |
| bgeonnx, ANE+static (`STATIC_SHAPES=1`) | 59.9 s | 0.62 | I/O error 해결됨, ANE attach overhead로 CPU보다 느림 |
| bgeonnx, ANE dynamic (legacy) | 5 min+ hang | — | avoid (silent FP16 cast → I/O error) |

D1-FU-8 target 30 chunks/s 는 ANE-친화 모델 (EmbeddingGemma-300M 등) 필요 —
현재 HF 접근 차단 환경에서는 보류 (`memory/throughput-investigation-2026-05-20.md`
참조).

---

## 6. Migration — subprocess MCP → in-process

cks 등 기존 subprocess proxy 사용자는 다음 한 줄로 마이그레이션 가능 (cks
`internal/ckvclient/real.go` 영역):

**Before**:
```go
// spawn ckv binary, MCP stdio transport, restart logic, timeout 처리...
client := ckvclient.NewReal(ctx, ckvclient.RealOpts{
    BinaryPath: "/path/to/ckv",
    DataPath:   ".ckv-data",
    Embedder:   "bgeonnx",
    ModelDir:   modelDir,
})
defer client.Close()
hits, err := client.SemanticSearch(ctx, query, ckvclient.SearchOpts{K: 10})
```

**After**:
```go
adapter, _ := bgeonnx.Open(bgeonnx.Options{ModelDir: modelDir})
defer adapter.Close()
engine, _ := ckv.Open(".ckv-data", ckv.OpenOptions{Embedder: adapter})
defer engine.Close()
resp, err := engine.SemanticSearch(ctx, query, ckv.SearchOptions{K: 10})
```

부수 효과:
- subprocess spawn / stdio / restart 코드 (~400 lines) 제거 가능
- `DefaultCallTimeout` 같은 stdio buffer mitigation 불필요
- CKV-1 (cks-side hang) 구조적 해소 — stdio MCP path 자체가 사라짐

---

## 7. 알려진 한계

- **HF 다운로드 차단 환경**: 회사 정책 등으로 `huggingface.co` 접근이 막힌
  경우, 모델 파일을 다른 환경에서 받아 옮겨야 한다. PyPI는 일반적으로
  접근 가능하므로 `onnxconverter-common` 같은 변환 도구는 사용 가능.
- **EmbeddingGemma 등 ANE-친화 모델**: `pkg/types/...` 의 ModelConfig
  registry에 `embeddinggemma-300m` 사전 등록은 있으나 (`bgeonnx/model_config.go`)
  모델 파일은 별도 확보 필요.
- **Linux ANE 부재**: CoreML EP는 macOS 전용. Linux는 자동으로 CPU EP만
  사용 — `CKV_COREML_*` 환경변수는 무시된다.

---

## 8. 관련 문서

- `d1-installation-guide.md` — ckv 자체 빌드/실행 가이드 (개발자 시점)
- `issue-coreml-compile-io-error.md` — CoreML 디버깅 history 와 가설 검증 로그
- `fp16-model-evaluation.md` — FP16 / INT8 모델 후보 비교
- `followups-from-cks-dogfood-2026-05-19.md` — cks consumer 측 gap 추적 (이 문서가 CKV-3 closure)
- `testdata/mcp-repro/` — stdio MCP 검증 스크립트
