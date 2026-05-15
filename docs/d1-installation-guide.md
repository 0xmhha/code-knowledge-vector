# D1 — bge-code-v1 / ONNX 의존성 설치 가이드

> **대상**: CKV에서 `--embedder=bgeonnx`를 실제로 동작시키려는 사용자.
> **결과물**: `go build -tags bgeonnx ./...` 성공 + `ckv build/eval --embedder=bgeonnx` 동작.
> **소요 시간**: ~15–30분 (네트워크 속도에 따라).

코드 자체는 `bgeonnx` 빌드 태그로 게이트돼 있어, **이 가이드를 따르지 않아도 기본
빌드(`go build ./...`)는 영향 받지 않는다**. CKV의 mock embedder는 그대로 동작.

---

## 0. 무엇을, 왜 설치하는지

| 항목 | 설치 이유 | 디스크 |
|------|----------|--------|
| **`libonnxruntime`** (C++ 동적 라이브러리) | `yalue/onnxruntime_go`가 CGO로 호출. ONNX 그래프 실행 엔진 본체. | ~80 MB |
| **`libtokenizers`** (Rust 정적/동적 라이브러리) | `daulet/tokenizers`가 CGO로 호출. HuggingFace Rust 토크나이저의 C ABI 래퍼. | ~15 MB |
| **`huggingface_hub` + `optimum[exporters]`** (Python 패키지) | **일회성** 모델 변환 도구. PyTorch 가중치를 ONNX 포맷으로 변환. 변환 후엔 더 이상 필요 없음. | ~500 MB venv |
| **bge-code-v1 PyTorch 가중치** | 모델의 "두뇌". 약 5.7억 파라미터 × FP32. | ~520 MB |
| **bge-code-v1 ONNX 변환본** | 위 가중치를 ONNX 포맷으로 변환한 파일. ORT는 이것만 읽음. | ~520 MB |

런타임에 실제로 필요한 것: **libonnxruntime + libtokenizers + tokenizer.json + model.onnx**. Python은 일회성.

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

## 2. Python 변환 도구 설치 (일회성)

`venv`로 격리해서 시스템 Python을 더럽히지 않는다.

```bash
# CKV 레포 안에서 (또는 어디든 한 곳에)
python3 -m venv ~/.venvs/ckv-export
source ~/.venvs/ckv-export/bin/activate

# 변환에 필요한 최소 패키지
pip install --upgrade pip
pip install "huggingface_hub[cli]" "optimum[exporters]" "transformers" "torch"

# 검증
huggingface-cli --version
optimum-cli --help
```

> 디스크 ~500MB. 변환 완료 후 venv 삭제 가능 (`rm -rf ~/.venvs/ckv-export`).

---

## 3. 모델 다운로드 + ONNX 변환

```bash
# venv 활성화돼 있어야 함
source ~/.venvs/ckv-export/bin/activate

# CKV가 기대하는 표준 경로
mkdir -p ~/.cache/ckv/models/bge-code-v1
cd ~/.cache/ckv/models/bge-code-v1

# 1) HuggingFace에서 가중치 + 토크나이저 다운로드 (~520MB)
huggingface-cli download BAAI/bge-code-v1 --local-dir .

# 2) PyTorch → ONNX 변환 (~5–10분, CPU)
optimum-cli export onnx \
  --model BAAI/bge-code-v1 \
  --task feature-extraction \
  ./onnx-tmp

# 3) CKV가 기대하는 경로로 ONNX 파일 이동
mv ./onnx-tmp/model.onnx ./model.onnx
mv ./onnx-tmp/tokenizer.json ./tokenizer.json 2>/dev/null || true
# tokenizer.json은 1단계에서 이미 받아져 있을 수 있음 — 중복 OK
rm -rf ./onnx-tmp
```

### 결과 확인

```bash
ls -lh ~/.cache/ckv/models/bge-code-v1/
# 기대 출력:
#   model.onnx              ~520M
#   tokenizer.json           ~5M
#   config.json              ~1K
#   tokenizer_config.json    ~1K
#   special_tokens_map.json  ~1K
```

### 사전 변환된 ONNX가 이미 있는지 (선택)

HuggingFace Hub에 누가 bge-code-v1을 미리 ONNX로 export해 올려놨다면 2단계를 건너뛸 수 있다. 검색:

```bash
huggingface-cli search bge-code-v1
# 또는 https://huggingface.co/models?search=bge-code-v1+onnx 브라우저
```

이런 모델이 있으면 `huggingface-cli download <ONNX_repo_id> --include "*.onnx"`로 직접 받기.

---

## 4. CKV에서 빌드 + 실행

```bash
# 레포 루트에서
cd /Users/wm-it-22-00661/Work/github/tools/code-knowledge-vector

# bgeonnx 태그로 빌드 (CGO 링크 발생)
CGO_LDFLAGS="-L$HOME/lib" \
  go build -tags bgeonnx -o ./bin/ckv ./cmd/ckv

# 인덱스 빌드 (mock 대신 bgeonnx)
./bin/ckv build --src=./testdata/sample --out=/tmp/ckv-bge --embedder=bgeonnx

# 평가
./bin/ckv eval --out=/tmp/ckv-bge --fixture=./testdata/queries.yaml \
  --top=5 --threshold=-1 --min-recall5=0.95
```

### 기대 수치 (D1 문서 §3.3)

- `recall@5`: 0.900 → 1.0
- `MRR`: 0.59 → ≥ 0.85
- p95 쿼리 latency: ≤ 200 ms (warm)

수치가 안 나오면 wiring 버그 의심 (pooling, normalize, tokenizer 설정).

---

## 5. 일반적인 실패 메시지

| 메시지 | 원인 | 해결 |
|--------|------|------|
| `ld: library not found for -ltokenizers` | `CGO_LDFLAGS`가 `~/lib`을 못 찾음 | `export CGO_LDFLAGS="-L$HOME/lib"` 다시 실행 |
| `dyld: Library not loaded: @rpath/libonnxruntime.dylib` | macOS가 ORT를 못 찾음 | `export DYLD_LIBRARY_PATH=/opt/homebrew/lib:$DYLD_LIBRARY_PATH` |
| `tokenizers_version_1_26_0 not found` | C ABI 버전 불일치 | libtokenizers v1.26.0 정확히 받아야 함 (Go 모듈 v1.27.0과 C lib v1.26.0이 정상 조합) |
| `model.onnx missing in ~/.cache/ckv/...` | 3단계 변환 실패 | `~/.cache/ckv/models/bge-code-v1/`에 `model.onnx`가 있는지 확인 |

---

## 6. 정리 / 롤백

설치를 되돌리려면:

```bash
brew uninstall onnxruntime                          # macOS
rm -rf ~/lib/libtokenizers*
rm -rf ~/.cache/ckv/models/bge-code-v1
rm -rf ~/.venvs/ckv-export
unset CGO_LDFLAGS
```

CKV 자체는 영향 없음 — 기본 빌드는 `bgeonnx` 태그 없이 동작.
