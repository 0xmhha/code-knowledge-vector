# Sample decisions

This fixture exercises the markdown indexer end-to-end.

## Why sqlite-vec

We picked sqlite-vec because it is embeddable, has no separate server
process, and ships with cosine-distance vec0 virtual tables out of the
box. Alternatives considered: pgvector (needs Postgres), chroma
(separate process), milvus (operationally heavy).

## Why bge-code-v1

The bge-code-v1 ONNX model gives us code-aware embeddings at 1024-d
without a Python runtime. We weighed it against bge-large-en-v1.5
(general-purpose, smaller signal on code) and chose bge-code-v1 for
the corpus we actually index.

```text
Code fences should NOT split sections — these are body text:
# totally not a heading
## another fake heading
```

## Out of scope

JavaScript parsing, README/CHANGELOG extension-less indexing, and the
RRF fuser all live in S2. See plan-S1-ckv.md §13.
