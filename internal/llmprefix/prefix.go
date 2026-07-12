// Package llmprefix implements Anthropic-style contextual retrieval (roadmap
// Phase D.2): an LLM writes a one-sentence description of each code chunk, which
// is prepended to the chunk's embed text so the vector carries situating
// context. Generation is expensive, so results are disk-cached by chunk content
// hash and a generation failure degrades to no prefix (the caller falls back to
// the cheap rule-based prefix) rather than failing the build.
package llmprefix

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Generator produces an LLM completion for a prompt. Injectable so the prefixer
// is unit-testable without a live model and swappable across backends (ollama,
// Claude CLI, API).
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, error)
}

// Prefixer returns a short contextual prefix for a chunk's embed text, or ""
// when unavailable (the caller then falls back to the rule-based prefix).
type Prefixer interface {
	Prefix(ctx context.Context, c types.Chunk) string
}

// Cached is a disk-cached LLM prefixer. It keys prefixes by chunk content hash
// so a rebuild reuses them, and returns "" on any generation error rather than
// failing the build.
type Cached struct {
	gen      Generator
	cacheDir string
}

// NewCached returns a Cached prefixer writing its cache under cacheDir (created
// if absent). A nil generator makes Prefix a no-op ("").
func NewCached(gen Generator, cacheDir string) (*Cached, error) {
	if cacheDir != "" {
		if err := os.MkdirAll(cacheDir, 0o755); err != nil {
			return nil, fmt.Errorf("llmprefix: cache dir: %w", err)
		}
	}
	return &Cached{gen: gen, cacheDir: cacheDir}, nil
}

// Prefix returns the cached or freshly-generated one-sentence prefix for c, or
// "" if generation fails.
func (p *Cached) Prefix(ctx context.Context, c types.Chunk) string {
	if p == nil || p.gen == nil {
		return ""
	}
	key := hashKey(c)
	if v, ok := p.readCache(key); ok {
		return v
	}
	out, err := p.gen.Generate(ctx, BuildPrompt(c))
	if err != nil {
		return ""
	}
	pre := sanitize(out)
	p.writeCache(key, pre)
	return pre
}

// hashKey keys the cache by chunk content — stable across rebuilds and
// independent of the prefix itself, matching the chunk ID's content hash.
func hashKey(c types.Chunk) string {
	sum := sha256.Sum256([]byte(c.Text))
	return hex.EncodeToString(sum[:])
}

// BuildPrompt is the one-sentence contextual-description prompt for a chunk.
func BuildPrompt(c types.Chunk) string {
	lang := c.Language
	if lang == "" {
		lang = "code"
	}
	return fmt.Sprintf("You are indexing a codebase for semantic search. In ONE concise sentence, "+
		"describe what the following %s chunk from %q does and the role it plays. "+
		"Output only the sentence, with no preamble or quotes.\n\n%s", lang, c.File, c.Text)
}

// sanitize collapses the model output to a single trimmed line.
func sanitize(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}

func (p *Cached) cachePath(key string) string {
	return filepath.Join(p.cacheDir, key+".txt")
}

func (p *Cached) readCache(key string) (string, bool) {
	if p.cacheDir == "" {
		return "", false
	}
	b, err := os.ReadFile(p.cachePath(key))
	if err != nil {
		return "", false
	}
	return string(b), true
}

func (p *Cached) writeCache(key, val string) {
	if p.cacheDir == "" {
		return
	}
	tmp := p.cachePath(key) + ".tmp"
	if os.WriteFile(tmp, []byte(val), 0o644) == nil {
		_ = os.Rename(tmp, p.cachePath(key))
	}
}
