# D1 — ONNX bge-code-v1 PoC Report

> **Status**: investigation + scaffolding complete (2026-05-13)
> **Owner**: CKV maintainers
> **Outcome**: pick `yalue/onnxruntime_go` + Python tokenizer service for first cut; pure-Go tokenizer migration deferred.

---

## 1. Goal

Decide how CKV runs the production embedding model. The mock embedder
(feature-hashing, `internal/embed/mock`) has a recall@5=0.9 baseline
on testdata/sample but cannot capture real semantic relationships
between unrelated lexical surface forms — the exact gap the bge-code-v1
swap is meant to close.

Three concrete questions for D1:

1. **Which Go ONNX runtime** is production-viable in 2026 on Apple Silicon?
2. **How to tokenize** without dragging the full Python stack into the
   ckv binary surface?
3. **What to ship today** so a future contributor can complete the
   integration with a 30-minute spike (download model → run smoke).

---

## 2. Survey

### 2.1 Go ONNX runtime options

| Option | Mechanism | Pros | Cons |
|---|---|---|---|
| **`yalue/onnxruntime_go`** | CGO around `libonnxruntime` system library | Most active (commits in 2025); Apple-Silicon supported since ORT 1.16; CoreML / Metal execution providers; matches CGO precedent set by sqlite-vec | Requires user install of `libonnxruntime.dylib` (~80 MB); per-model session warmup ≈ 1-2 s |
| **`crewAIInc/onnxruntime_go`** | Fork of yalue | Same | Same; redundant — no compelling advantage |
| **`gorgonia/onnx-go`** | Pure Go ONNX interpreter | No system deps, single binary | Sparse op coverage — Transformer attention patterns frequently fail to load; project velocity stalled |
| **Python subprocess** (`sentence-transformers`) | `python -m ckv_embed` over stdin/stdout | Trivial to wire (5 LOC Python); rich model ecosystem | 2-process IPC; per-call serialization; foreign runtime in the deployment story |

**Decision**: `yalue/onnxruntime_go` as the primary runtime. Pure-Go is a dead end for code embeddings today; Python is a viable fallback but its operational profile (extra process, separate venv to manage) conflicts with CKV's "single binary, embeddable in CKS" goal stated in plan §7.

### 2.2 Tokenization

bge-code-v1 uses the same BertTokenizer family as bge-base. Options:

| Option | Mechanism | Notes |
|---|---|---|
| **`daulet/tokenizers`** | CGO binding to HuggingFace's Rust `tokenizers` crate | Loads `tokenizer.json` directly; pre-built `libtokenizers.so` available; ~10 MB. **Recommended.** |
| **`sugarme/tokenizer`** | Pure Go | Works for some BPE/WordPiece models; bge tokenizer compatibility unverified, would need a fixture pass. |
| **Python subprocess for tokenize only** | `python -m hf_tokenize` | Defeats the purpose if we already have onnxruntime working in Go. |

**Decision**: `daulet/tokenizers` for production; defer pure-Go migration to a follow-up once we have a known-good reference output to test against.

### 2.3 Model artifact

`BAAI/bge-code-v1` ships PyTorch weights upstream. ONNX export
(via `optimum-cli`) produces `model.onnx` (~520 MB FP32, ~270 MB FP16).
For first cut, FP32 keeps numerics exactly matching the reference
implementation; FP16 is a follow-up optimization.

Files we expect in `~/.cache/ckv/models/bge-code-v1/`:

```
config.json
tokenizer.json
tokenizer_config.json
special_tokens_map.json
model.onnx                         ~520 MB FP32
model.onnx.sha256                  # checksum, manifest-verified at load
```

---

## 3. What ships in this PoC

This document represents the **scaffolding** delivery. Two parts:

### 3.1 Code

- `internal/embed/bgeonnx/` — interface scaffold ready for the runtime
  swap. Today returns `ErrNotImplemented` from `Embed()` with the path
  forward documented in the source. The scaffold:
    - separates `Session` (ONNX-runtime concern) from `Tokenizer`
      (tokenizer-library concern) so each can be swapped independently
    - keeps `ModelName` / `ModelDim` / `ModelMaxInput` as compile-time
      constants the manifest already records
- `cmd/ckv` `--embedder=mock|bgeonnx` flag — mock stays the default so
  existing test/eval baselines hold; bgeonnx is opt-in.
- `internal/embed/bgeonnx/bgeonnx_smoke_test.go` (build tag `bgeonnx_smoke`)
  — invokes the real model when present, skipped otherwise.

### 3.2 Operator runbook

When the user has model + dependencies on disk:

```bash
# 1. install onnxruntime (one-time)
brew install onnxruntime              # provides libonnxruntime.dylib

# 2. fetch the model (one-time)
mkdir -p ~/.cache/ckv/models/bge-code-v1
cd ~/.cache/ckv/models/bge-code-v1
huggingface-cli download BAAI/bge-code-v1 --local-dir .
optimum-cli export onnx --model BAAI/bge-code-v1 ./onnx

# 3. point ckv at it
export CKV_MODEL_PATH=~/.cache/ckv/models/bge-code-v1
./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge --embedder=bgeonnx

# 4. eval against the bge baseline
./bin/ckv eval --out=/tmp/ckv-bge --fixture=./testdata/queries.yaml \
    --top=5 --threshold=-1 --min-recall5=0.95

# 5. run the smoke test
go test -tags bgeonnx_smoke ./internal/embed/bgeonnx/ -v
```

### 3.3 Expected outcome on first run

Hypothesis (to be confirmed when the model is in place):

- recall@5 climbs from 0.900 → 1.0 on the 10-query fixture
- MRR rises from 0.59 → ≥ 0.85
- p95 query latency ≤ 200 ms warm (plan §3 NFR)
- Index build ≈ 17 chunks/s on M-series Mac (CPU) — comfortable for
  testdata; the 1M LOC target needs a separate scaling pass.

If recall@5 drops below 0.95 vs mock, treat as a wiring bug (probably
input normalization or pooling) and re-verify before claiming the
model itself is at fault.

---

## 4. Decision Log

| Date | Decision | Rationale |
|---|---|---|
| 2026-05-13 | Pick `yalue/onnxruntime_go` over pure-Go | gorgonia/onnx-go lacks Transformer op coverage; sqlite-vec already committed CKV to CGO so the marginal complexity is zero |
| 2026-05-13 | Pick `daulet/tokenizers` for tokenization | Loads `tokenizer.json` directly; matches HuggingFace exactly so future model swaps are token-faithful |
| 2026-05-13 | Defer Python subprocess fallback | Adds a foreign runtime to CKS's single-binary story; revisit only if onnxruntime install proves painful on user machines |
| 2026-05-13 | Ship scaffolding + report, defer end-to-end inference | Honest about session constraints (no 520 MB download in tool calls); leaves a follow-up that any contributor can execute in 30 min |

---

## 5. Risks / open questions

- **Cold-start cost**: ONNX session init + first inference ≈ 1.5 s on
  M-series. For `ckv query` (interactive) this is acceptable if warm
  (session held inside `query.Engine`); for `ckv build` (one-shot) we
  pay it once per invocation — fine.
- **Determinism**: ONNX Runtime is deterministic on the same hardware
  but FP32 vs FP16 give different vectors. Pin FP32 for the regression
  baseline; explore FP16 only after the baseline is stable.
- **Tokenizer drift**: HuggingFace publishes occasional tokenizer
  patches. Pin `tokenizer.json` to a manifest checksum (`embedding_checksum`
  field — see plan §3) and refuse to load on mismatch.
- **Cross-platform**: linux/amd64 + linux/arm64 builds need their own
  `libonnxruntime.so`. The `make build` story documents the path; CI
  matrix is a follow-up.

---

## 6. Follow-up tasks

| ID | Task | Notes |
|---|---|---|
| D1-FU-1 | Wire `yalue/onnxruntime_go` Session inside `internal/embed/bgeonnx/session.go` | Build tag `bgeonnx` so existing CI without ORT stays green |
| D1-FU-2 | Wire `daulet/tokenizers` inside `internal/embed/bgeonnx/tokenizer.go` | Read `tokenizer.json` at Open() |
| D1-FU-3 | Run the runbook end-to-end on M-series Mac, capture numbers | Update §3.3 Hypothesis → actuals |
| D1-FU-4 | `ckv model fetch` command (D2) | Use HuggingFace CLI or direct CDN download with sha256 verify |
| D1-FU-5 | linux/amd64 + linux/arm64 CI build matrix | Cross-build with appropriate `libonnxruntime` |

---

## 7. References

- ONNX Runtime release notes (Apple Silicon support history): https://github.com/microsoft/onnxruntime/releases
- yalue/onnxruntime_go README: documented `SetSharedLibraryPath` for non-default install paths
- BAAI/bge-code-v1 model card: https://huggingface.co/BAAI/bge-code-v1
- HuggingFace optimum-cli ONNX export docs
