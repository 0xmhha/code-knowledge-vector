# ADR-008: Qwen3-Embedding at 1024 dims (MRL-truncated) is the recommended embedding

**Status**: Accepted
**Date**: 2026-07-12

## Context

The ollama default embedder is `bge-m3` (1024-dim). The upgrade evaluation
(`docs/embedding-model-recommendation-2026-06-22.md`) recommended the
**Qwen3-Embedding** series (Apache 2.0, MRL-trained, higher retrieval precision
than bge-m3, drop-in via the existing ollama path). Two Qwen3 sizes are
registered: `qwen3-embedding:0.6b` (native 1024) and `qwen3-embedding:4b`
(native 2560).

Qwen3-Embedding is trained with Matryoshka Representation Learning, so a prefix
of its output vector is a valid lower-dimensional embedding once renormalized.
The four-session coordination left the embedding **dimension** explicitly
undecided — decision 6: "dimension = decide after measuring, CKV-owned, do not
fix before measuring". The open question was whether truncating the 4b model's
2560-dim vector down to 1024 keeps enough precision to justify the storage and
search savings.

Two measurements settled it (`docs/qwen3-dimension-ab-2026-07-12.md`):

- **N=50 (testdata/queries.yaml)**: full-2560 vs truncate-1024 — recall@1
  0.88 → 0.86, MRR 0.913 → 0.902, recall@5 unchanged (0.96); vector store
  ÷2.47.
- **~20× re-confirm (go-stablenet subset, 1015 chunks)**: top-1 file agreement
  8/10, mean top-5 overlap 0.81, identical ranks on the 3 in-corpus
  ground-truth queries; vector store ÷2.1.

Both agree: 1024-truncate keeps ~98% of full-2560 precision at ~40% of the
storage/search cost.

## Decision

**Adopt `qwen3-embedding:4b` MRL-truncated to 1024 dims as the recommended
embedding configuration** (`ckv build --embedder ollama --model-name
qwen3-embedding:4b --embed-dim 1024`).

- 1024 is the standing dimension. Full-2560 remains the fallback when maximum
  precision is paramount and storage is not a constraint (the measured gap is
  ~2%p recall@1).
- MRL truncation lives in the ollama adapter (`Options.TargetDim` /
  `--embed-dim`): truncate to the first N components, then L2-renormalize; the
  native-dim probe runs with truncation off so validation stays authoritative.
- This does not change the shipped default (`bge-m3`) yet — it fixes the target
  for the reindex-B rollout. The embedding-space identity guard (PR #12) makes
  the switch safe: an index built at one (model, dim) is rejected against
  another.

## Consequences

**Good**:
- ~98% of full-2560 precision at half the vector store — the MRL sweet spot,
  and the storage/latency win compounds on a 1M-LOC corpus.
- Apache 2.0, higher precision than bge-m3, no new backend (existing ollama
  path).

**Accepted costs**:
- **Ollama Qwen3 large-input instability**: the ollama Qwen3-Embedding endpoint
  crashes (HTTP 400 EOF) on individual very large chunks (~20–40KB single
  inputs), independent of batch size (both 4b and 0.6b; `--batch` does not help
  a single oversized chunk). ollama 0.30.9 → 0.31.2 fixed the smaller-input
  crashes but not this. A production rollout needs embed-path robustness
  (retry / skip-and-warn) or a stable build path (bge-m3 built the full corpus
  cleanly). Tracked in `remaining.md`.
- The dimension was fixed on an **approximate** large corpus (current
  go-stablenet HEAD, not the canonical `0bf2f4d1b` / pr-77-2 dataset, which is
  absent on the measuring machine). A relative A/B is commit-independent, but a
  re-check on the canonical dataset is advisable before the model actually ships
  as the default.

**Deferred (separate levers, not part of this decision)**:
- **Instruct query-prefix**: Qwen3-Embedding recommends a query-side
  `Instruct:` prefix. The `Embedder` interface does not yet distinguish query
  from passage, so this needs an interface extension.
- `qwen3-embedding:0.6b` (native 1024) vs 4b-truncate-1024 — the model-size
  axis, distinct from the dimension axis this ADR settles.
- `knownDims` standardization (e.g. 512 / 1024 / 2560).

## Realization

- MRL truncation: `pkg/embed/ollama` `Options.TargetDim` + `truncateNormalize`,
  CLI `--embed-dim` (PR #19). Test: `TestTruncateNormalize`.
- `ckv build --batch N` for embedders that reject large batches (PR #20).
- Measurements: `docs/qwen3-dimension-ab-2026-07-12.md` (N=50 §1–3, 20×
  re-confirm §6).
- Relationship to ADR-002 (bge-code-v1 → bge-large-en-v1.5): ADR-002 chose the
  ONNX/bgeonnx default; this ADR fixes the ollama upgrade target and is not a
  supersede — the two paths coexist until the reindex-B rollout consolidates.
