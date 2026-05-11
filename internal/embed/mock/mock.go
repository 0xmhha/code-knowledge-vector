// Package mock is a deterministic, dependency-free Embedder used for
// integration tests, the dev-loop, and CI before the real ONNX adapter
// lands. It uses the classic **feature-hashing trick**: tokens are
// hashed into dim buckets, counted, then L2-normalized.
//
// Properties:
//   - same text → same vector (deterministic)
//   - similar texts → similar vectors (shared tokens dominate)
//   - no vocab, no model file, no download
//
// Not appropriate for production retrieval — but it lets `ckv build →
// ckv query` work end-to-end so the rest of the pipeline can be wired
// and tested independently of the (heavyweight) ONNX path.
package mock

import (
	"context"
	"hash/fnv"
	"math"
	"strings"
	"unicode"
)

const (
	// Default name; instantiate with a unique name if running multiple
	// mocks side by side so the manifest reflects what was used.
	defaultName = "mock-feature-hash-v1"
	// Default dim chosen to match a "small" embedding for tests. Use
	// New(dim, ...) for other sizes.
	defaultDim = 64
)

// Embedder is the in-process feature-hashing mock.
type Embedder struct {
	name string
	dim  int
}

// New constructs an Embedder with a custom dim/name. Pass dim ≤ 0 for
// the default. The name is what gets stamped in the manifest.
func New(dim int, name string) *Embedder {
	if dim <= 0 {
		dim = defaultDim
	}
	if name == "" {
		name = defaultName
	}
	return &Embedder{name: name, dim: dim}
}

// Default returns a mock with conventional name/dim.
func Default() *Embedder { return New(defaultDim, defaultName) }

func (e *Embedder) Name() string         { return e.name }
func (e *Embedder) Dimension() int       { return e.dim }
func (e *Embedder) MaxInputTokens() int  { return math.MaxInt32 } // no truncation

// Embed produces one vector per input string. Pure-Go and synchronous;
// the context is honored only via early-exit on Done.
func (e *Embedder) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	out := make([][]float32, len(batch))
	for i, text := range batch {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		out[i] = e.vector(text)
	}
	return out, nil
}

func (e *Embedder) vector(text string) []float32 {
	v := make([]float32, e.dim)
	for _, tok := range tokenize(text) {
		idx := hashIndex(tok, e.dim)
		v[idx]++
	}
	l2normalize(v)
	return v
}

// tokenize splits on non-letter/digit. ASCII-cheap; good enough for the
// mock — we just need *some* stable tokenization, not linguistic quality.
func tokenize(text string) []string {
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_'
	})
	for i, f := range fields {
		fields[i] = strings.ToLower(f)
	}
	return fields
}

func hashIndex(s string, dim int) int {
	h := fnv.New32a()
	h.Write([]byte(s))
	return int(h.Sum32() % uint32(dim))
}

// l2normalize divides v by its L2 norm in place. No-op on the zero vector.
func l2normalize(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range v {
		v[i] /= norm
	}
}
