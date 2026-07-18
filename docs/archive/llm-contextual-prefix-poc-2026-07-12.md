# LLM contextual prefix (Phase D.2) — PoC 실측

> **ARCHIVED 2026-07-19.** Measurement record; the go/no-go verdict is locked in [`adr/009-rule-based-prefix-default.md`](../adr/009-rule-based-prefix-default.md). Kept for provenance.

> **시점**: 2026-07-12 (측정 스냅샷).
> **목적**: retrieval-quality-roadmap Phase D.2("LLM이 청크마다 한 문장 맥락을 생성해
> 임베드 텍스트 앞에 붙인다", Anthropic Contextual Retrieval)를 **실측 후 결정** 게이트로
> 닫는다. D.1(rule-based prefix)은 이미 기본값이다 — **LLM prefix가 D.1을 이기는가**를
> 정밀도로 판정한다.
> **관련**: `retrieval-quality-roadmap.md`(Phase D), `qwen3-dimension-ab-2026-07-12.md`,
> `eval-metrics.md`, `internal/chunk/prefix.go`(D.1), `internal/llmprefix/`(D.2).

## 1. 방법

- **임베더**: `bge-m3`(Ollama, 1024-dim, l2). 대칭 모델 — passage/query 동일 인코딩이라
  prefix 효과만 격리된다.
- **생성기(D.2)**: 로컬 Ollama `llama3`(8B). 청크마다 프롬프트(`llmprefix.BuildPrompt`)로
  한 문장 설명을 생성해 임베드 텍스트 앞에 붙임. sha256(청크 본문)로 디스크 캐시
  (`<out>/.ckv-llmprefix-cache`), 생성 실패 시 D.1로 degrade.
- **코퍼스**: `testdata/sample`(7파일, 50청크). **fixture**: `testdata/queries.yaml`(N=50).
- **A/B/B'**: 같은 코퍼스·임베더를 세 임베드-텍스트 조합으로 빌드 → 각 인덱스에
  `ckv eval ... -k 5`.
  - **D.1** rule-based only: `"language: go. file: x.go. symbol: F (Function).\n\n<raw>"`
  - **D.2** LLM prose + raw: `"<llama3 한 문장>\n<raw>"` (rule-based 프리픽스 버림)
  - **D.2b** LLM prose + rule-based + raw: `"<llama3 한 문장>\n<rule-based + raw>"`

  ```
  ckv build --embedder ollama --model-name bge-m3 --src testdata/sample --out d1
  ckv build ... --out d2  --llm-prefix-model llama3          # D.2  (LLM + raw)
  ckv build ... --out d3  --llm-prefix-model llama3          # D.2b (LLM + rule + raw)
  ckv eval  ... --out d1|d2|d3 --fixture testdata/queries.yaml -k 5 --json
  ```

## 2. 결과 (queries.yaml, N=50)

| 지표 | D.1 rule-based | D.2 LLM+raw | D.2b LLM+rule+raw |
|---|---|---|---|
| recall@1 | **0.86** | 0.78 | 0.84 |
| recall@3 | 0.96 | 0.98 | 0.98 |
| recall@5 | 0.98 | 0.98 | 0.98 |
| MRR | **0.911** | 0.877 | 0.900 |
| citation@1 | 1.00 | 1.00 | 1.00 |
| build 시간 | **2.3s** | 44s | 44s (캐시 재사용 시 2s) |

- **LLM prefix는 rule-based를 못 이긴다**: D.2 recall@1 −8%p·MRR −3.4%p, D.2b도
  recall@1 −2%p·MRR −1.1%p로 여전히 열세.
- degrade된 쿼리는 전부 **자연어형**(q17 "send tokens between two accounts", q21
  "insufficient balance", q23 "404 not found", q30 "reject empty or unsafe paths" 등)이
  rank 1→2로 밀림. LLM 산문이 rule-based의 정확한 symbol/file 토큰(`symbol: Cache.Get`)을
  **희석**했다.
- **조합(D.2b) > 단독(D.2)**: rule-based 신호를 유지하면 손실 대부분 회복(recall@1
  0.78→0.84, MRR 0.877→0.900). 켠다면 조합형이 옳다 — 하지만 D.1에는 여전히 못 미친다.
- 생성 비용 **19×**(2.3s→44s, 50청크). 대형 코퍼스에서 선형 확대.

## 3. 결정 (권장)

**D.2 LLM prefix는 기본 비활성(opt-in 레버)으로만 제공한다.** 이 코퍼스에서 D.1을 이기지
못했고 생성 비용이 19×다. 레버는 구현·테스트·배선 완료(`--llm-prefix-model`), 켰을 때의
임베드 텍스트는 **조합형(LLM + rule-based + raw)**으로 확정(§2에서 raw-only보다 우수).

- **trade-off 명시**: 대형·이질 코퍼스(청크가 self-context 부족: "this function returns X"
  류)에서 강한 생성기(예: Claude) + BM25 병용 시 Anthropic 결과처럼 이득 가능 — 이 게이트는
  *작은 self-descriptive 코퍼스*에서 이득 없음을 실측했을 뿐이다.
- ADR 승격 불요(feature를 켜지 않으므로). 기본값 변경 없음.

## 4. 캐비엇

- **N=50, testdata/sample(50청크)** — 방향성. recall@5는 세 조합 모두 0.98로 천장에 근접해
  변별력이 recall@1/MRR에 집중된다. **대형 코퍼스(go-stablenet 정본) 재확인 시 결론이
  뒤집힐 여지** — Anthropic 기법의 이득 영역이 여기(작은 코퍼스)와 다르다.
- **생성기 = llama3(8B)**. 더 강한 생성기(Claude/큰 로컬 모델)면 산문 품질이 올라
  희석 효과가 줄 수 있음 — 미측정. 레버는 `Generator` 인터페이스라 교체 가능.
- **순수 벡터 eval**(BM25 rerank off). Anthropic 기법은 contextual embedding + contextual
  BM25 병용이 핵심 — 벡터 단독 측정은 기법의 절반만 본 것.
- self-descriptive 코퍼스 편향: testdata/sample 청크는 이름 붙은 함수라 이미 자기설명적 →
  situating context의 한계효용이 작다. 익명/단편 청크가 많은 코퍼스에선 다를 수 있음.

## 5. 구현 노트

- `internal/llmprefix/`: `Generator` 인터페이스(주입 가능) + `Prefixer` + 디스크캐시
  `Cached`(sha256(본문) 키, 실패 시 "") + `OllamaGenerator`(`/api/generate`, `CKV_OLLAMA_ENDPOINT`).
- 배선: `internal/build` `resolveEmbedTextFn(ctx, disablePrefix, prefixer)` — prefixer 설정 시
  `LLM prose\n + BuildEmbedText`, miss/실패 시 `BuildEmbedText`. CLI `--llm-prefix-model`
  (build/reindex). 캐시는 `<out>/.ckv-llmprefix-cache`.
- 청크 ID/저장 Text 불변 — prefix는 임베드 텍스트에만 존재(D.1과 동일 불변식).
