# FP16 Embedding Model Evaluation

> **작성일**: 2026-05-20
> **목적**: Apple Neural Engine (ANE) 활용을 위한 FP16 임베딩 모델 변경 옵션 비교.
> **컨텍스트**: `docs/issue-coreml-compile-io-error.md` §5 진행 중. ANE는 FP16 native — 현재 FP32 모델은 ANE compile 단계에서 silent 변환되며 I/O error 발생 의심.
> **관련**: [`backlog.md`](./backlog.md), [`retrieval-quality-roadmap.md`](./retrieval-quality-roadmap.md), [`d1-onnx-poc.md`](./d1-onnx-poc.md)

---

## 1. 핵심 사실 (조사 결과)

| 항목 | 사실 |
|---|---|
| **ANE 산술** | FP16 native. INT8 가중치도 ANE 내부에서 FP16으로 변환 후 계산 (출처: Apple ML research, InsiderLLM 가이드) |
| **ANE throughput** | INT8과 FP16이 **동일** throughput (~19 TFLOPS 실측). INT8 이점은 *모델 크기*뿐 |
| **ANE는 ONNX 직접 지원 안 함** | CoreML compiler가 내부 MIL (Machine Learning Intermediate Language)로 변환 |
| **ORT CoreML EP `ModelFormat` 기본값** | **`NeuralNetwork`** (older, default). MLProgram는 명시 필요 |
| **NeuralNetwork format 위험** | MPS/ANE 경로에서 **silent FP32 → FP16 cast** 발생. 변환 실패가 곧 I/O error의 후보 |
| **MLProgram format 장점** | Core ML 5+ (macOS 12+) 필요. **silent cast 없음**, 더 넓은 op support |
| **공식 BAAI bge-large-en-v1.5 FP16** | **없음**. `Teradata/bge-large-en-v1.5` fork에 fp32/int8/uint8 (fp16 미제공) |
| **Optimum FP16 quant CLI** | **없음**. dynamic quant는 CPU 전용 int8만. FP16은 `onnxconverter-common.convert_float_to_float16()` 또는 PyTorch `.half()` + 재export |

**중요한 함의**: 현재 `attachCoreML`이 `ModelFormat`을 명시하지 않음 → ORT default = NeuralNetwork → **silent FP16 cast가 이미 진행 중**일 가능성이 높음. ANE compile I/O error는 *FP16 변환 자체의 실패*일 수 있음 (FP32 op set 일부가 FP16-incompatible).

## 2. 세 가지 실용적 옵션

### 옵션 A — ORT-side FP16 처리 (코드만 변경, 변환 없음)

현재 FP32 `model.onnx` 유지. CoreML EP 옵션을 더 추가:

```go
coreMLOpts := map[string]string{
    "MLComputeUnits":                     units,
    "ModelFormat":                        "MLProgram",  // silent cast 회피
    "AllowLowPrecisionAccumulationOnGPU": "1",          // GPU FP16 누적
    // 기존: ModelCacheDirectory (옵션)
}
```

| 항목 | 값 |
|---|---|
| 코드 변경 | session_impl.go: 5~10줄 (env 2개 추가) |
| ONNX 변환 | 불필요 |
| 정확도 영향 | MLProgram + FP32 입력 → ORT가 op 단위로 FP16 dispatch 결정. **순수 FP32보다 약간 손실 가능**, 측정 필요 |
| ANE compile I/O error 해소 가능성 | **High** — NeuralNetwork silent cast가 진짜 원인이면 즉시 해소 |
| 모델 크기 | 1.3 GB 변화 없음 |
| 메모리 가드 영향 | EstimatedRAMMB 그대로 |
| 검증 cost | bge binary rebuild + 1회 ckv build |

### 옵션 B — FP16 ONNX 직접 export

`onnxconverter-common` 또는 PyTorch `.half()` 로 model_fp16.onnx 생성.

```python
# 변환 script (예시, ~30 lines)
from onnx import load
from onnxconverter_common import float16
model = load("model.onnx")
model_fp16 = float16.convert_float_to_float16(model, keep_io_types=True)
onnx.save(model_fp16, "model_fp16.onnx")
```

| 항목 | 값 |
|---|---|
| 코드 변경 | model_config.go 새 entry, session_impl.go에 DType 분기 (Tensor[float32] vs Tensor[uint16]) — 30~50줄 |
| ONNX 변환 | python script 1회 실행 (`onnxconverter-common` pip install 필요) |
| 정확도 영향 | 측정 가능 (D1 fixture로 recall@5 비교). 보통 1~2% 손실 |
| ANE compile I/O error 해소 가능성 | **Mid** — FP16 native라 silent cast 회피되지만, ANE op set 미지원 op는 여전히 fail 가능 |
| 모델 크기 | 1.3 GB → **0.65 GB** (절반) |
| 메모리 가드 영향 | EstimatedRAMMB 5000 → 3000으로 하향 가능 |
| 검증 cost | 변환 script + binary rebuild + recall 측정 |

### 옵션 C — Teradata int8 variant 사용 (이미 변환된 모델)

`Teradata/bge-large-en-v1.5` 의 `model_int8.onnx` 다운로드.

| 항목 | 값 |
|---|---|
| 코드 변경 | model_config.go 새 entry (input quantization 처리는 ORT가 자동) |
| ONNX 변환 | **불필요** (사전 변환됨) |
| 정확도 영향 | int8 quantization 손실 (보통 2~5%, 측정 필요) |
| ANE compile I/O error 해소 가능성 | **Mid** — ANE가 int8 → FP16 internal 변환. NeuralNetwork format이면 여전히 silent cast 영역 |
| 모델 크기 | 1.3 GB → **0.33 GB** (1/4) |
| 메모리 가드 영향 | EstimatedRAMMB 5000 → 2000으로 하향 가능 |
| 검증 cost | 모델 download (~330 MB) + binary rebuild + recall 측정 |

## 3. 권장안

**단계적 접근**: 옵션 A를 먼저, 결과 보고 B/C 추가.

근거:
1. 옵션 A의 비용은 **session_impl.go 5~10줄**과 binary rebuild 1회. 모델 파일·정확도 측정 없이 ANE I/O error 해소 검증 가능.
2. 만약 옵션 A가 I/O error를 해소하지 못하면 (silent cast 가설이 틀렸으면) → 옵션 B로 진행 (명시적 FP16 ONNX).
3. 옵션 A가 동작하지만 throughput 목표 (30+ chunks/s) 미달이면 → 옵션 C로 모델 크기까지 줄여서 메모리 압박 완화 + cache locality 개선.

옵션 B와 C는 *정확도 손실*이 있으므로 D1 fixture (N=34) recall@5/MRR 측정이 필수. 옵션 A는 *정확도 손실 무관* (모델은 FP32 그대로, ORT runtime의 dispatch만 변경).

## 4. 비교 한 줄

| 옵션 | 변경 cost | 정확도 risk | ANE 해소 가능성 | 모델 크기 |
|---|---|---|---|---|
| **A: ORT MLProgram + low precision GPU** | **5~10 lines, 변환 0** | None (FP32 유지) | **High** | 1.3 GB |
| B: FP16 ONNX 직접 export | 30~50 lines + 변환 script | Mid (~1-2%) | Mid | 0.65 GB |
| C: int8 (Teradata) 사용 | 새 entry + dtype 분기 | High (~2-5%) | Mid | 0.33 GB |

## 5. 다음 단계 (사용자 결정 대기)

1. 옵션 A를 먼저 구현 + 검증 → 결정 (recommended)
2. 옵션 B/C 직접 진행
3. 다 보류, 다른 작업 우선

## 6. References

- ONNX Runtime CoreML EP V2 옵션: https://onnxruntime.ai/docs/execution-providers/CoreML-ExecutionProvider.html
- `ONNX Runtime & CoreML May Silently Convert Your Model to FP16`: https://ym2132.github.io/ONNX_MLProgram_NN_exploration
- Apple "Deploying Transformers on the Apple Neural Engine": https://machinelearning.apple.com/research/neural-engine-transformers
- ANE FP16/INT8 throughput 분석 (InsiderLLM): https://insiderllm.com/guides/apple-neural-engine-llm-inference/
- HuggingFace Optimum quantization (int8 only): https://huggingface.co/docs/optimum-onnx/onnxruntime/usage_guides/quantization
- Teradata/bge-large-en-v1.5 (pre-quantized variants): https://huggingface.co/Teradata/bge-large-en-v1.5
