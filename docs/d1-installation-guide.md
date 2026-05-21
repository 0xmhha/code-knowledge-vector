# D1 — bge-large-en-v1.5 / ONNX 의존성 설치 가이드

> **대상**: CKV에서 `--embedder=bgeonnx`를 실제로 동작시키려는 사용자.
> **결과물**: `go build -tags bgeonnx ./...` 성공 + `ckv build/eval --embedder=bgeonnx` 동작.
> **소요 시간**: ~5–10분 (네트워크 속도에 따라).
>
> **2026-05-18 업데이트**: 모델이 bge-code-v1 (Qwen2 5.8GB) → **bge-large-en-v1.5 (BERT 2.5GB)** 로 전환. **bge-large-en-v1.5는 ONNX 파일이 HF repo에 사전 포함돼 있어 Python 변환 단계가 불필요**.

코드 자체는 `bgeonnx` 빌드 태그로 게이트돼 있어, **이 가이드를 따르지 않아도 기본
빌드(`go build ./...`)는 영향 받지 않는다**. CKV의 mock embedder는 그대로 동작.

---

## 0. 무엇을, 왜 설치하는지

| 항목 | 설치 이유 | 디스크 |
|------|----------|--------|
| **`libonnxruntime`** (C++ 동적 라이브러리) | `yalue/onnxruntime_go`가 CGO로 호출. ONNX 그래프 실행 엔진 본체. | ~80 MB |
| **`libtokenizers.a`** (Rust 정적 라이브러리) | `daulet/tokenizers`가 CGO로 호출. HuggingFace Rust 토크나이저의 C ABI 래퍼. **C ABI v1.26.0 필수** (Go 모듈 v1.27.0과 정상 조합). | ~40 MB |
| **`huggingface_hub[cli]`** (Python 패키지) | 모델 다운로드용 `hf` 명령만 필요. ONNX 변환 도구는 bge-large-en-v1.5에 불필요 (HF repo에 사전 포함). | ~50 MB venv |
| **bge-large-en-v1.5 모델** | safetensors + 사전 변환된 `onnx/model.onnx` + tokenizer.json. 모델 카드 + 1_Pooling config 등. | ~2.5 GB |

런타임에 실제로 필요한 것: **libonnxruntime + libtokenizers.a + tokenizer.json + onnx/model.onnx**. Python은 다운로드 한 번에만.

---

## 1. 시스템 라이브러리 설치

### macOS (Apple Silicon / Intel)

```bash
# ONNX Runtime
brew install onnxruntime
# 결과: /opt/homebrew/lib/libonnxruntime.dylib (Apple Silicon)
#       /usr/local/lib/libonnxruntime.dylib (Intel)

# Tokenizers (daulet/tokenizers v1.27.0이 요구하는 C ABI 버전: 1.26.0)
# Homebrew formula가 없으므로 GitHub Release에서 직접 다운로드:
mkdir -p ~/lib
cd ~/lib
curl -L -o libtokenizers.darwin-arm64.tar.gz \
  https://github.com/daulet/tokenizers/releases/download/v1.26.0/libtokenizers.darwin-arm64.tar.gz
tar -xzf libtokenizers.darwin-arm64.tar.gz
# 결과: ~/lib/libtokenizers.a

# Go가 찾을 수 있도록 환경변수 설정 (~/.zshrc 또는 ~/.bashrc에 영구화 권장)
export CGO_LDFLAGS="-L$HOME/lib"
```

> Intel Mac이면 `libtokenizers.darwin-arm64` 대신 `darwin-amd64`. Apple Silicon은 Rosetta 없이 네이티브 빌드 사용.

### Linux (amd64)

```bash
# ONNX Runtime — 공식 GitHub Release
mkdir -p ~/lib
cd ~/lib
curl -L -o onnxruntime.tgz \
  https://github.com/microsoft/onnxruntime/releases/download/v1.20.0/onnxruntime-linux-x64-1.20.0.tgz
tar -xzf onnxruntime.tgz
sudo cp onnxruntime-linux-x64-1.20.0/lib/libonnxruntime.so* /usr/local/lib/
sudo ldconfig

# Tokenizers
curl -L -o libtokenizers.linux-x64.tar.gz \
  https://github.com/daulet/tokenizers/releases/download/v1.26.0/libtokenizers.linux-x64.tar.gz
tar -xzf libtokenizers.linux-x64.tar.gz
export CGO_LDFLAGS="-L$HOME/lib"
```

### 설치 검증

```bash
# macOS
ls /opt/homebrew/lib/libonnxruntime* ~/lib/libtokenizers* 2>/dev/null

# Linux
ls /usr/local/lib/libonnxruntime* ~/lib/libtokenizers* 2>/dev/null

# 둘 다 결과가 나오면 성공
```

---

## 2. Python 다운로드 도구 설치 (일회성)

`venv`로 격리해서 시스템 Python을 더럽히지 않는다. bge-large-en-v1.5는 HF repo에 ONNX가 사전 포함돼 있어 **`huggingface_hub[cli]`만 있으면 충분**.

```bash
python3 -m venv ~/.venvs/ckv-export
source ~/.venvs/ckv-export/bin/activate
pip install --upgrade pip
pip install "huggingface_hub[cli]"

# 검증
~/.venvs/ckv-export/bin/hf --help | head -5
```

> 디스크 ~50MB. 다운로드 후 venv 삭제 가능. `huggingface-cli`는 deprecated, `hf` 사용.

---

## 3. 모델 다운로드 (사전 변환된 ONNX 포함)

```bash
source ~/.venvs/ckv-export/bin/activate

# CKV가 기대하는 표준 경로
mkdir -p ~/.cache/ckv/models/bge-large-en-v1.5
cd ~/.cache/ckv/models/bge-large-en-v1.5

# HF에서 가중치 + 토크나이저 + 사전 변환 ONNX 모두 다운로드 (~2.5GB, ~1분)
hf download BAAI/bge-large-en-v1.5 --local-dir .
```

### 결과 확인

```bash
ls -lh ~/.cache/ckv/models/bge-large-en-v1.5/
# 기대 출력:
#   config.json              ~1K
#   model.safetensors        ~1.3G  (PyTorch 가중치 — 추론에 불필요, 삭제 가능)
#   pytorch_model.bin        ~1.3G  (구버전 PyTorch 가중치 — 삭제 가능)
#   onnx/model.onnx          ~1.3G  ← ORT가 실제로 읽는 파일
#   tokenizer.json           ~700K
#   tokenizer_config.json    ~400
#   vocab.txt                ~230K
#   1_Pooling/config.json    ~200   ← CLS pooling 명시
#   modules.json             ~350
```

### 디스크 절약 (선택)

ORT는 `onnx/model.onnx`만 읽고, daulet/tokenizers는 `tokenizer.json`만 읽으므로 PyTorch 가중치는 삭제해도 됨:

```bash
rm ~/.cache/ckv/models/bge-large-en-v1.5/model.safetensors
rm ~/.cache/ckv/models/bge-large-en-v1.5/pytorch_model.bin
# → 2.5GB에서 1.3GB로 줄임
```

---

## 4. CKV에서 빌드 + 실행

```bash
cd /path/to/code-knowledge-vector

# bgeonnx 태그로 빌드 (CGO 링크 발생)
CGO_LDFLAGS="-L$HOME/lib" \
  go build -tags bgeonnx -o ./bin/ckv ./cmd/ckv

# 인덱스 빌드 (mock 대신 bgeonnx)
./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge --embedder=bgeonnx

# 평가 — index와 embedder가 같은 dim이어야 함, 둘 다 bgeonnx
./bin/ckv eval --out=/tmp/ckv-bge --fixture=./testdata/queries.yaml \
  --top=5 --threshold=-1 --embedder=bgeonnx
```

### 실측 수치 (2026-05-18, bge-large-en-v1.5)

- `recall@5`: 1.000 ✅ (가설 1.0)
- `recall@3`: 0.900, `recall@1`: 0.600
- `MRR`: 0.770 (가설 0.85 살짝 미달 — q5에서 rank 5)
- p95 쿼리 latency: ~43ms warm ✅ (가설 200ms의 1/4)
- 인덱스 빌드: 1.6 chunks/s (가설 17 chunks/s 미달 — 배치 최적화 필요)

수치가 크게 차이 나면 wiring 버그 의심 (pooling, normalize, token_type_ids, tokenizer 설정).

---

## 5. 일반적인 실패 메시지

| 메시지 | 원인 | 해결 |
|--------|------|------|
| `ld: library not found for -ltokenizers` | `CGO_LDFLAGS`가 `~/lib`을 못 찾음 | `export CGO_LDFLAGS="-L$HOME/lib"` 다시 실행 (또는 빌드 명령 앞에 인라인) |
| `Error loading ONNX shared library "onnxruntime.so"` | macOS는 `.dylib`인데 `yalue/onnxruntime_go` 기본 검색은 Linux 이름 | 코드가 자동으로 `/opt/homebrew/lib/libonnxruntime.dylib` 시도. Intel Mac이면 `export CKV_ONNXRUNTIME_LIB=/usr/local/lib/libonnxruntime.dylib` |
| `tokenizers_version_1_26_0 not found` | C ABI 버전 불일치 | libtokenizers **v1.26.0** 정확히 받아야 함 (Go 모듈 v1.27.0과 C lib v1.26.0이 정상 조합) |
| `Missing Input: token_type_ids` | BERT ONNX 모델이 token_type_ids 요구 | bge-large-en-v1.5는 `session_impl.go`가 자동으로 zeros 텐서 전달. 다른 BERT 모델 사용 시 동일 |
| `onnx/model.onnx missing in ~/.cache/ckv/...` | 3단계 다운로드 실패 | `~/.cache/ckv/models/bge-large-en-v1.5/onnx/` 디렉토리에 `model.onnx`가 있는지 확인 |
| `dim mismatch (index=1024, embedder=64)` | eval 시 index는 bgeonnx로 빌드됐는데 query는 mock | `ckv eval` 명령에도 `--embedder=bgeonnx` 추가 |

---

## 6. 정리 / 롤백

설치를 되돌리려면:

```bash
brew uninstall onnxruntime                          # macOS
rm -rf ~/lib/libtokenizers*
rm -rf ~/.cache/ckv/models/bge-large-en-v1.5
rm -rf ~/.venvs/ckv-export
unset CGO_LDFLAGS
```

CKV 자체는 영향 없음 — 기본 빌드는 `bgeonnx` 태그 없이 동작.
