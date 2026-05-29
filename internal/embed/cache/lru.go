// Package cache wraps a types.Embedder with a hot-path LRU cache so
// repeated embed calls on the same text return without paying the
// model cost.
//
// Use case: the agent issues semantic_search with the same intent
// multiple times during a multi-hop flow, and explain_match re-embeds
// the intent to compare each candidate. The cache keeps both hot.
//
// The cache is intentionally in-process and not persisted — the
// embeddings depend on model identity, which changes when the user
// swaps backends. Restart clears the cache, which is the correct
// behavior.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// DefaultSize is the cache capacity when the caller does not specify.
// 1024 entries × 1024-dim × 4 bytes ≈ 4 MB per cache instance for the
// stored vectors plus a small per-entry book-keeping overhead.
const DefaultSize = 1024

// Cached wraps an inner Embedder. Cache hits return immediately from
// memory; misses pass through to inner.Embed and store the result.
type Cached struct {
	inner types.Embedder
	size  int

	mu   sync.Mutex
	keys []string             // LRU recency order; index 0 = oldest
	data map[string][]float32 // key → vector
	hits int
	miss int
}

// New wraps emb with an LRU cache. size <= 0 uses DefaultSize.
func New(emb types.Embedder, size int) *Cached {
	if size <= 0 {
		size = DefaultSize
	}
	return &Cached{
		inner: emb,
		size:  size,
		data:  make(map[string][]float32, size),
	}
}

// Name / Dimension / MaxInputTokens forward to the inner embedder so
// the wrapper is a drop-in for types.Embedder.
func (c *Cached) Name() string        { return c.inner.Name() }
func (c *Cached) Dimension() int      { return c.inner.Dimension() }
func (c *Cached) MaxInputTokens() int { return c.inner.MaxInputTokens() }

// Embed checks the cache for each batch entry, calls the inner
// embedder only for misses, and stores all results before returning.
func (c *Cached) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	out := make([][]float32, len(batch))
	keys := make([]string, len(batch))
	var (
		missTexts []string
		missIdx   []int
	)

	c.mu.Lock()
	for i, t := range batch {
		k := key(t)
		keys[i] = k
		if v, ok := c.data[k]; ok {
			out[i] = v
			c.touch(k)
			c.hits++
		} else {
			missTexts = append(missTexts, t)
			missIdx = append(missIdx, i)
			c.miss++
		}
	}
	c.mu.Unlock()

	if len(missTexts) == 0 {
		return out, nil
	}
	vecs, err := c.inner.Embed(ctx, missTexts)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	for j, v := range vecs {
		i := missIdx[j]
		out[i] = v
		c.put(keys[i], v)
	}
	return out, nil
}

// Stats returns hit / miss counters. Useful for /health diagnostics
// and tests asserting the cache is being exercised.
func (c *Cached) Stats() (hits, miss, size int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.hits, c.miss, len(c.data)
}

// key derives the cache key from the input text. SHA-256 keeps the
// key length bounded regardless of text size, and the avalanche
// behaviour avoids accidental collisions.
func key(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

// touch moves k to the most-recent position. Caller must hold c.mu.
func (c *Cached) touch(k string) {
	for i, x := range c.keys {
		if x == k {
			c.keys = append(c.keys[:i], c.keys[i+1:]...)
			break
		}
	}
	c.keys = append(c.keys, k)
}

// put inserts (or refreshes) k and evicts the oldest entry when the
// cache is full. Caller must hold c.mu.
func (c *Cached) put(k string, v []float32) {
	if _, ok := c.data[k]; ok {
		c.data[k] = v
		c.touch(k)
		return
	}
	if len(c.data) >= c.size {
		oldest := c.keys[0]
		c.keys = c.keys[1:]
		delete(c.data, oldest)
	}
	c.keys = append(c.keys, k)
	c.data[k] = v
}
