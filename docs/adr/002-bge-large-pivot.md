# ADR-002: Pivot from bge-code-v1 to bge-large-en-v1.5

**Status**: Accepted
**Date**: 2026-05-15

## Context

CKV's D1 PoC originally targeted `bge-code-v1` for the default
embedder. The model is code-aware, ONNX-exported, and 1024-dim — a
plausible fit on paper.

During PoC integration we discovered:

1. **Adapter complexity**: `bge-code-v1` is built on Qwen2 with a
   last-token pooling head. Our `bgeonnx` adapter (BERT + CLS pooling)
   couldn't run it without writing a second backend path.
2. **Throughput on macOS ANE**: Qwen2's transformer attention falls
   back to CPU/GPU on the Apple Neural Engine — same as BERT-based
   models — so the code-aware claim doesn't unlock the hardware
   acceleration we'd hoped for.
3. **Recall on testdata**: the E2E run with bge-large-en-v1.5 on the
   N=10 fixture hit recall@5=1.0, MRR=0.77, citation@1=1.0. Good
   enough for D1 ship. No headroom problem to solve.

The choice became "ship a working default with the existing adapter"
vs "double the adapter surface to maybe get a small recall gain on a
code-heavy corpus we don't yet have."

Measurement record:
- bge-large-en-v1.5 D1 PoC: recall@5=1.0, MRR=0.77 (N=10).
- bge-code-v1: no end-to-end measurement on CKV; never produced a
  working session in our adapter path.

## Decision

Default embedder is `bge-large-en-v1.5` (1024-dim, BERT-base, CLS
pooling). `bge-code-v1` is parked as a D2 backlog item (A4) with the
Qwen2 last-token-pooling adapter as a future enhancement when:

- A measurable code-retrieval gap appears against the default, AND
- The fixture corpus is large enough (≥200 queries) to measure that
  gap with statistical confidence.

The `bgeonnx` package keeps its model registry (`model_config.go`) so
swapping the default in the future is a one-line registration change
plus shipping the new model file.

## Consequences

**Good**:
- D1 ships with measured recall, not a half-integrated experiment.
- One embedder backend, one tokenizer path, one cache directory
  convention. Lower operational surface area.
- bge-large is English-trained and our codebase comments / queries
  are English-dominant — the alignment is favorable.

**Accepted costs**:
- We give up the *potential* recall gain that a true code-aware
  embedder might deliver on a code-heavy corpus. The gap is unmeasured.
- If we later add multilingual code (Japanese / Korean comments), we
  may need a multilingual embedder. bge-large is English-centric.

**Closed off**:
- Any near-term reliance on Qwen2-style last-token-pooling models.
  The adapter path doesn't exist yet; revisit only when measurement
  justifies the cost.

## Related

- A4 in `docs/backlog.md` tracks the deferred bge-code-v1 work.
- [ADR-005](005-coreml-mlprogram-static-shapes.md) covers the CoreML
  EP decision that depends on this embedder shape (BERT + static
  shapes).
