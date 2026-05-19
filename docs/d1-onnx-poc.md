# D1 — ONNX bge-code-v1 PoC Report

> **Status**: scaffolding (2026-05-13) → FU-1+FU-2 wired (2026-05-17) → **model pivot bge-code-v1 → bge-large-en-v1.5 + E2E measured (2026-05-18)**. PoC complete; bge-code-v1 Qwen2 adapter deferred to D2.
> **Owner**: CKV maintainers
> **Outcome**: `yalue/onnxruntime_go` + `daulet/tokenizers` + bge-large-en-v1.5 (BERT 335M, 1024d, CLS pooling) behind `-tags bgeonnx`. Default build untouched. recall@5=1.0 / MRR=0.77 / p95=43ms on 10-query fixture.

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

### 2.3 Model artifact — **bge-large-en-v1.5** (pivot 2026-05-18)

> **2026-05-18 pivot**: 원안의 `BAAI/bge-code-v1`(Qwen2 1.5B, 5.8GB, last-token pooling, 32k ctx)에서 **`BAAI/bge-large-en-v1.5`**(BERT, 1024d, CLS pooling, 512 ctx, ~2.5GB)로 전환. 결정 근거는 §4 Decision Log 2026-05-18 row. 이전 모델은 D1-FU-6(D2 scope)로 이관. 본 절은 현재 default 모델 기준으로 기술.

`BAAI/bge-large-en-v1.5`는 HuggingFace repo에 **ONNX 파일이 사전 포함**되어 있어 Python `optimum-cli` 변환 단계가 불필요. `onnx/model.onnx` ~1.3GB FP32.

Files we expect in `~/.cache/ckv/models/bge-large-en-v1.5/`:

```
config.json
tokenizer.json
tokenizer_config.json
special_tokens_map.json
1_Pooling/config.json              # CLS pooling 설정
onnx/model.onnx                    ~1.3 GB FP32  (HF repo 사전 포함)
onnx/model.onnx.sha256             # checksum, manifest-verified at load
```

> (참고) bge-code-v1 어댑터 작업 시 별도 디렉토리 `~/.cache/ckv/models/bge-code-v1/`. ModelDim=1536, ModelMaxInput=32k, last-token pooling, Qwen2 ONNX export(optimum-cli + ~5GB extra disk) 필요. 자세히 D1-FU-6.

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

Detailed step-by-step is in [`docs/d1-installation-guide.md`](./d1-installation-guide.md).
Short version:

```bash
# install system libs (one-time)
brew install onnxruntime
# libtokenizers v1.26.0 darwin-arm64 .a → ~/lib/

# fetch model (one-time, ~2.5 GB; ONNX export already bundled in repo)
mkdir -p ~/.cache/ckv/models/bge-large-en-v1.5
cd ~/.cache/ckv/models/bge-large-en-v1.5
hf download BAAI/bge-large-en-v1.5 --local-dir .

# build ckv with the production embedder
CGO_LDFLAGS="-L$HOME/lib" \
  go build -tags bgeonnx -o ./bin/ckv ./cmd/ckv

# index + eval
./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge --embedder=bgeonnx
./bin/ckv eval --out=/tmp/ckv-bge --fixture=./testdata/queries.yaml \
    --top=5 --threshold=-1 --embedder=bgeonnx

# smoke test
CGO_LDFLAGS="-L$HOME/lib" go test \
    -tags 'bgeonnx bgeonnx_smoke' ./internal/embed/bgeonnx/
```

### 3.3 Measured outcomes (2026-05-18, bge-large-en-v1.5)

| Metric | mock baseline | bge-large-en-v1.5 | D1 hypothesis | Met? |
|---|---|---|---|---|
| recall@5 | 0.900 | **1.000** | 1.0 | ✅ |
| recall@3 | — | 0.900 | — | — |
| recall@1 | — | 0.600 | — | — |
| MRR | 0.590 | **0.770** | ≥ 0.85 | ⚠️ short |
| p95 latency (warm) | — | **43 ms** | ≤ 200 ms | ✅ (1/4 of budget) |
| citation@1 | — | 1.000 | — | — |
| Index build | — | 26 chunks / 16 s ≈ **1.6 chunks/s** | ≈ 17 chunks/s | ❌ 10× slower than guess |

Notes:
- **MRR shortfall**: q5 (`retrieve value by key; report whether it was found`) lands at rank 5; top hit is `handler.ts` instead of `cache.go`. bge-large-en-v1.5 is general-text, not code-trained. A bge-code-v1 adapter would likely lift MRR but needs the Qwen2 path (deferred, see §6).
- **Build throughput miss**: 1.6 chunks/s on M-series CPU. The 17 chunks/s guess was naive — didn't account for the per-chunk ORT cold-call cost + L2 normalize CPU work. 1M LOC scaling needs (a) batching chunks per Embed() call or (b) onnxruntime CoreML execution provider.
- **N=10**: statistically weak. Expand fixture to ≥ 50 queries for a robust signal.

---

## 4. Decision Log

| Date | Decision | Rationale |
|---|---|---|
| 2026-05-13 | Pick `yalue/onnxruntime_go` over pure-Go | gorgonia/onnx-go lacks Transformer op coverage; sqlite-vec already committed CKV to CGO so the marginal complexity is zero |
| 2026-05-13 | Pick `daulet/tokenizers` for tokenization | Loads `tokenizer.json` directly; matches HuggingFace exactly so future model swaps are token-faithful |
| 2026-05-13 | Defer Python subprocess fallback | Adds a foreign runtime to CKS's single-binary story; revisit only if onnxruntime install proves painful on user machines |
| 2026-05-13 | Ship scaffolding + report, defer end-to-end inference | Honest about session constraints (no 520 MB download in tool calls); leaves a follow-up that any contributor can execute in 30 min |
| 2026-05-18 | **Pivot bge-code-v1 → bge-large-en-v1.5** | Model card audit (post-download) revealed bge-code-v1 is actually Qwen2 1.5B with last-token pooling + 32k context — completely different from our BERT/mean assumptions. bge-large-en-v1.5 matches the existing scaffold (BERT, 1024d, but CLS pooling not mean). 30 min PoC pivot vs writing a Qwen2 adapter from scratch. |
| 2026-05-18 | **Keep meanPoolNormalize alongside clsPoolNormalize** | bge-code-v1 (last-token) and bge-m3 (mean) both plausible future additions; deleting mean would require rewriting it later. Strategy pattern overkill for now — two pure functions cost ~50 LOC and zero runtime overhead. |
| 2026-05-18 | **Hardcode token_type_ids = zeros for bge-large-en-v1.5** | BERT ONNX export requires the input even for single-sequence embedders. All-zero tensor is the correct value (segment A). Future non-BERT models will need a different input signature; gate on ModelName then. |

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

| ID | Status | Task | Notes |
|---|---|---|---|
| D1-FU-1 | **done** (commit `3405124`) | Wire `yalue/onnxruntime_go` Session in `session_impl.go` | Gated by `-tags bgeonnx`. macOS dylib lookup needs `SetSharedLibraryPath` — not in the README, easy to miss. `CKV_ONNXRUNTIME_LIB` env overrides. |
| D1-FU-2 | **done** (commit `98dd373`) | Wire `daulet/tokenizers` in `tokenizer_impl.go` | Gated by `-tags bgeonnx`. Two-pass encode + pad-to-max-in-batch keeps inference cost proportional to actual sequence length. |
| D1-FU-3 | **done** (2026-05-18) | E2E measurement on M-series Mac | See §3.3. recall@5 met, MRR short, latency comfortable, build throughput miss. |
| D1-FU-4 | open (D2 scope) | `ckv model fetch` command | Use `hf download` (CLI) or direct CDN download with sha256 verify. Removes Python dependency from user workflow. bge-large-en-v1.5 ships ONNX in-repo so no convert step needed for it. |
| D1-FU-5 | open | linux/amd64 + linux/arm64 CI build matrix | Cross-build with appropriate `libonnxruntime` + `libtokenizers`. |
| D1-FU-6 | open (D2 scope) | **bge-code-v1 Qwen2 adapter** | Reuse existing 5.8GB download at `~/.cache/ckv/models/bge-code-v1/`. Needs: ModelDim=1536, ModelMaxInput=32k, last-token pooling, Qwen2 ONNX export (optimum-cli + ~5GB extra disk), per-model strategy dispatch in factory_on.go. |
| D1-FU-7 | open | Expand eval fixture to ≥ 50 queries | N=10 has weak signal. More queries + multiple languages would give better MRR / recall confidence intervals. |
| D1-FU-8 | open (perf) | Batch chunks per Embed() call | Index build is 1.6 chunks/s, 10× slower than naive guess. Batching ~32 chunks/Embed() + CoreML EP should close the gap to the 1M LOC scaling target. |

---

## 7. References

- ONNX Runtime release notes (Apple Silicon support history): https://github.com/microsoft/onnxruntime/releases
- yalue/onnxruntime_go README: documented `SetSharedLibraryPath` for non-default install paths
- BAAI/bge-code-v1 model card: https://huggingface.co/BAAI/bge-code-v1
- HuggingFace optimum-cli ONNX export docs
