# ADR-005: CoreML EP uses MLProgram format with static input shapes

**Status**: Accepted
**Date**: 2026-05-20

## Context

The bgeonnx adapter targets ONNX Runtime's CoreML Execution Provider
(EP) on macOS so the Apple Neural Engine (ANE) can accelerate
embedding inference. The first integration attempt failed: every
build under `--embedder=bgeonnx` errored at the CoreML compile step
with:

    NotImplemented: Element type (tensor(float16)) of output arg
    'output_0' of node 'Cast' is not supported

The error originated inside ORT's CoreML compile path during model
load — before any input was processed — so it wasn't a runtime data
issue. Investigation in `docs/issue-coreml-compile-io-error.md`
(now removed) identified two contributing factors:

1. **NeuralNetwork (NN) format default**: ORT's CoreML EP defaults
   to the legacy NN format, which inserts implicit FP32→FP16 Cast
   nodes for ANE compilation. Those Casts mis-fired on bge-large's
   output tensor shape and rejected the compile.
2. **Dynamic input shapes**: bge-large's exported ONNX has dynamic
   batch and sequence dimensions. ANE compile re-runs per unique
   (batch, sequence) tuple — every build batch with a new padding
   length forced a fresh compile and accumulated cache artifacts
   (78 cache directories, 2.5 GB on disk after one full build).

Even when we worked around the NN format issue, the dynamic-shape
recompile churn dragged throughput to 0.62 c/s.

Measurement record:
- NN + dynamic shapes: build fails (CoreML compile I/O error).
- MLProgram + dynamic shapes: build succeeds, 0.62 c/s, 78 cache
  dirs / 2.5 GB.
- MLProgram + static shapes (batch=1, seq=512): build succeeds,
  0.62 c/s on ANE, **3.6× faster** end-to-end vs dynamic (59.9s vs
  215s for the test corpus), 1 cache dir / 28 KB.
- CPU-only (no CoreML EP attached): 0.74 c/s — *higher* than ANE
  because bge-large's attention is CNN-unfriendly and ANE silently
  routes most ops back to CPU/GPU with overhead.

## Decision

When the CoreML EP is enabled (`CKV_DISABLE_COREML != "1"`):

1. **Format**: `MLProgram` (not `NeuralNetwork`).
   Env: `CKV_COREML_MODEL_FORMAT=MLProgram` (this is the default).
2. **Input shapes**: static.
   Env: `CKV_STATIC_SHAPES=1` forces tokenizer padding to
   `model_max_length`, making the ANE compile cache a single artifact
   reused across batches.
3. **Cache directory**: explicit.
   Env: `CKV_COREML_CACHE_DIR` overrides the default
   `~/.cache/ckv/coreml/<model>`. Lets ops separate cache lifetime
   from index lifetime.

The defaults live in `internal/embed/bgeonnx/session_impl.go` (env
var wiring) and `tokenizer_impl.go` (`CKV_STATIC_SHAPES` honored when
padding inputs).

CPU-only remains the supported fallback (`CKV_DISABLE_COREML=1`).
On bge-large specifically, CPU pure is *faster* than ANE-attached
because of the attention-heavy workload — operators should choose
based on measured throughput, not on "ANE = faster" intuition.

## Consequences

**Good**:
- Build no longer fails on macOS. CoreML EP is usable for ANE-
  friendly models (CNN-heavy, fixed-batch).
- One CoreML cache artifact per (model, shape) instead of one per
  unique batch shape. Disk usage stays bounded.
- Operators have explicit env-var knobs for the three things that
  matter (format, shape, cache dir) instead of relying on ORT
  defaults that change between versions.

**Accepted costs**:
- Static shapes mean every batch pads to `max_seq_len`, wasting
  compute on short inputs. For bge-large (`max_seq_len`=512) this
  trade is favorable on the build path (batched embedding); for
  query-time single-vector inference the waste is small in absolute
  terms.
- Two more env vars to document for operators. Defaults are sensible
  for the common case; the knobs exist for when they aren't.

**Closed off**:
- Relying on ORT's CoreML EP defaults across versions. ORT bumps
  the default occasionally; we pin the behavior we want.

## Related

- A1 in `docs/archive/backlog.md` (closed 2026-05-20) tracked the
  resolution.
- [ADR-002](002-bge-large-pivot.md) — the embedder choice this ADR
  builds on. Different embedder shape (Qwen2 last-token-pooling)
  would require revisiting the static-shape default.
- Commits `66bdefc`, `9e71fa6`, `292db4a`, `9ff43e6` implement the
  three env-var knobs plus static-shape padding.
