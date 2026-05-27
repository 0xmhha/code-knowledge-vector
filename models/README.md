# Models

This directory is for local model management. Model files (ONNX, CoreML, tokenizers) are stored here or in `~/.cache/ckv/models/`.

## Supported Models

| Model | Dimension | Format | Download |
|-------|-----------|--------|----------|
| bge-large-en-v1.5 | 1024 | ONNX | `make model-fetch` |
| embeddinggemma-300m | 768 | ONNX | `ckv model fetch embeddinggemma-300m` |
| bge-m3 (via Ollama) | 1024 | GGUF | `ollama pull bge-m3` |

## Backends

| Backend | Command | Requirements |
|---------|---------|-------------|
| ONNX Runtime | `--embedder=bgeonnx` | libonnxruntime, model.onnx |
| Ollama | `--embedder=ollama --model-name=bge-m3` | Ollama running |
| CoreML | `--embedder=coreml` | macOS, .mlpackage model |
| Mock | `--embedder=mock` | None (testing only) |

## Converting Models

```bash
# HuggingFace → ONNX
ckv model convert BAAI/bge-m3 --format onnx

# ONNX → CoreML
ckv model convert ./model.onnx --format coreml
```

## Directory Structure

```
~/.cache/ckv/models/
  ├── bge-large-en-v1.5/
  │   ├── onnx/model.onnx
  │   └── tokenizer.json
  └── embeddinggemma-300m/
      ├── onnx/model.onnx
      └── tokenizer.json
```
