# 임베딩 모델 업그레이드 추천 — bge-m3 → Qwen3-Embedding

> **시점**: 2026-06-22 (스냅샷). 실제 채택 시 ADR로 승격.
> **목적**: 현재 사용 중인 `bge-m3`보다 **정밀도가 높고**, **라이선스가 깨끗하며**,
> **Mac mini M4 (24GB)** 에서 동작하는 임베딩 모델을 평가·추천한다.
> **관련 문서**: [`embedder-integration.md`](./embedder-integration.md),
> [`retrieval-quality-roadmap.md`](./retrieval-quality-roadmap.md),
> [`adr/002-bge-large-pivot.md`](./adr/002-bge-large-pivot.md)

---

## 1. 배경 / 현재 상태

ckv는 현재 두 임베딩 경로를 운용한다:

| 경로 | 모델 | 차원 | 비고 |
|---|---|---|---|
| Ollama (`--embedder=ollama`) | `bge-m3` | 1024 | GGUF, `ollama pull bge-m3` |
| ONNX (`--embedder=bgeonnx`) | `bge-large-en-v1.5` | 1024 | CoreML/ANE 경로 ([ADR-002](./adr/002-bge-large-pivot.md)) |

`bge-m3`는 100+ 언어 · dense/sparse/multi-vector를 한 모델에서 제공하는 MIT 라이선스
워크호스지만, 2025~2026 사이 등장한 신형 임베딩 모델 대비 검색 정밀도에서 뒤처진다.
본 문서는 그 격차를 메우는 교체 후보를 정리한다.

### 제약 조건

- **하드웨어**: Mac mini M4, 통합 메모리 24GB (OS·기타 앱과 공유).
- **정밀도**: bge-m3 초과.
- **라이선스**: 상업적 사용에 제약 없을 것.
- **운영 마찰 최소화**: 가능하면 기존 Ollama 경로로 교체.

---

## 2. 결론

**Qwen3-Embedding 시리즈 (Apache 2.0)** 를 채택 후보로 추천한다.

- bge-m3보다 정밀하고, Apache 2.0으로 상업적 사용이 자유로우며,
- 이미 사용 중인 **Ollama 경로로 그대로 교체 가능**(`ollama pull qwen3-embedding:<size>`).
- 24GB M4에서는 크기 선택만 결정하면 된다.

---

## 3. 후보 비교

| 모델 | 파라미터 | 정밀도(nDCG@10) | 차원 | M4 24GB 메모리 | 라이선스 | 평가 |
|---|---|---|---|---|---|---|
| **bge-m3** (현재 기준선) | 0.57B | 0.674 | 1024 | ~1.2GB(Q4) / 2.46GB(FP) | MIT | 기준선 |
| **Qwen3-Embedding-0.6B** | 0.6B | bge-m3 상회 | **1024** (native) | ~1.2GB | Apache 2.0 | 드롭인 교체용 |
| **Qwen3-Embedding-4B** ⭐ | 4B | **0.705** | 1024 / 2560 / 4096 (MRL) | ~2.5GB(Q4) / 18GB(FP) | Apache 2.0 | **추천 (정밀/메모리 균형)** |
| **Qwen3-Embedding-8B** | 8B | MTEB 다국어 #1 (70.58) | 1024 / 2560 / 4096 (MRL) | ~5GB(Q4) / ~8GB(Q8) / 16GB(FP) | Apache 2.0 | 최고 정밀도, 전용 운용 |

> 수치 출처는 §7 참조. MRL = Matryoshka Representation Learning — 한 모델이
> 여러 차원을 지원하며, 큰 차원을 잘라 작은 차원으로 쓸 수 있다(품질 손실 작음).

### 후보 선정에서 제외/보류

- **Llama-Embed-Nemotron-8B (NVIDIA)**: 250+ 언어로 다국어 1위 자료가 있으나
  NVIDIA Open Model License 별도 검토가 필요 → "라이선스 무문제" 요건에 Apache 2.0인
  Qwen3가 더 안전하여 보류.

---

## 4. 추천 우선순위

1. **주력: Qwen3-Embedding-4B (Q4_K_M)**
   - bge-m3 대비 nDCG@10 **0.674 → 0.705**로 의미 있는 향상.
   - Q4 양자화 시 ~2.5GB라 24GB에서 여유. MRL로 차원을 **1024로 truncate하면 현재
     스키마와 호환**.
   - 정밀도 / 메모리 / 마이그레이션 비용의 균형이 가장 좋음.

2. **드롭인 안전책: Qwen3-Embedding-0.6B**
   - 네이티브 **1024차원** → 스토어 스키마·인덱스 차원 변경 없이 가장 마찰 적게 교체.
   - bge-m3와 동급 체급이면서 점수는 위. "일단 무난하게 올려보자"면 이쪽.

3. **최대 정밀도: Qwen3-Embedding-8B (Q4/Q8)**
   - MTEB 다국어 1위. 24GB에서 Q8(~8GB)까지 충분히 구동.
   - 단, ckv 메모리 가드(bge-large 기준 pre-check ~7500MB)와 build 시 동시 부하를
     고려해 인덱싱 배치를 다른 작업과 겹치지 않게 운용.

---

## 5. 교체 시 체크리스트 (이 프로젝트 기준)

1. **재인덱싱 필수** — 임베딩은 모델 간 비교 불가. 모델 변경 시 `ckv build` 전체
   재실행 + manifest의 모델/차원 메타 갱신 필요.
2. **차원 처리**
   - 0.6B: 1024 그대로 (스키마 무변경).
   - 4B/8B: MRL로 1024 truncate(스키마 유지, **권장**) 하거나, 2560/4096으로 올리며
     sqlite-vec 인덱스 차원 + `ModelConfig` 레지스트리(`internal/embed/bgeonnx/model_config.go`)
     갱신.
3. **instruction-aware 비대칭 인코딩** — Qwen3는 쿼리에 `Instruct: ...` 프리픽스를
   붙이는 방식(문서/쿼리 인코딩이 다름). bge-m3보다 이 부분이 품질에 민감하므로
   쿼리 측 프롬프트 처리를 임베더 어댑터에 반영해야 정밀도가 제대로 나온다.
4. **가장 빠른 PoC 경로** — 이미 `ollama pull bge-m3`를 쓰므로:
   ```bash
   ollama pull qwen3-embedding:4b
   ckv build --src=. --out=.ckv-data \
     --embedder=ollama --model-name=qwen3-embedding:4b
   ```
   이후 [`evaluation-design-2026-05-22.md`](./evaluation-design-2026-05-22.md) /
   [`eval-metrics.md`](./eval-metrics.md)의 평가 틀로 `testdata/queries.yaml`·
   `testdata/why-queries.yaml`에 대해 bge-m3와 recall을 A/B 비교.

---

## 6. 권장 다음 단계

1. **PoC (낮은 비용)**: Qwen3-Embedding-4B를 Ollama로 붙여 기존 평가셋에 A/B 실행,
   recall/nDCG를 bge-m3와 직접 비교.
2. **효과 확인 시**: ONNX/CoreML 네이티브 경로(ANE 가속)로 승격 — ONNX export +
   `ModelConfig` 등록 필요(더 큰 작업, PoC 검증 후 진행).
3. **채택 결정 시**: 본 문서를 ADR로 승격하고 [`adr/002-bge-large-pivot.md`](./adr/002-bge-large-pivot.md)
   계보에 연결.

---

## 7. 출처

- [Qwen3 Embedding — Qwen 공식 블로그](https://qwenlm.github.io/blog/qwen3-embedding/)
- [Qwen3 Embedding 논문 (arXiv 2506.05176)](https://arxiv.org/abs/2506.05176)
- [Qwen3-Embedding — Ollama 라이브러리](https://ollama.com/library/qwen3-embedding)
- [Qwen/Qwen3-Embedding-0.6B · Hugging Face](https://huggingface.co/Qwen/Qwen3-Embedding-0.6B)
- [Qwen3 Embedding 4B vs bge-m3 비교 — Agentset](https://agentset.ai/embeddings/compare/qwen3-embedding-4b-vs-baaibge-m3)
- [Best Ollama Embedding Models 2026 (MTEB/VRAM/차원) — Morph](https://www.morphllm.com/ollama-embedding-models)
