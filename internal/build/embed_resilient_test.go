package build

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// poisonEmbedder embeds any batch successfully unless one of the inputs
// contains the poison marker, in which case it fails the whole batch — mirrors
// an embedder backend (ollama Qwen3) that crashes on a specific large input.
type poisonEmbedder struct {
	poison string
	dim    int
	calls  int
}

func (p *poisonEmbedder) Identity() types.EmbeddingIdentity {
	return types.EmbeddingIdentity{Provider: "test", Model: "poison", Dim: p.dim}
}
func (p *poisonEmbedder) Name() string        { return "poison" }
func (p *poisonEmbedder) Dimension() int      { return p.dim }
func (p *poisonEmbedder) MaxInputTokens() int { return 8192 }
func (p *poisonEmbedder) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.calls++
	for _, t := range batch {
		if strings.Contains(t, p.poison) {
			return nil, fmt.Errorf("simulated embedder rejection (EOF)")
		}
	}
	out := make([][]float32, len(batch))
	for i := range out {
		out[i] = make([]float32, p.dim)
		out[i][0] = 1
	}
	return out, nil
}

// TestEmbedResilient_SkipsPoisonChunk verifies that a single chunk the embedder
// rejects is skipped (not aborting the batch) while its neighbours survive with
// their vectors correctly paired.
func TestEmbedResilient_SkipsPoisonChunk(t *testing.T) {
	emb := &poisonEmbedder{poison: "POISON", dim: 8}
	chunks := []types.Chunk{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}}
	texts := []string{"ok1", "big POISON chunk", "ok2", "ok3"}

	oc, ov, err := embedResilient(context.Background(), emb, chunks, texts)
	if err != nil {
		t.Fatalf("embedResilient: %v", err)
	}
	if len(oc) != 3 || len(ov) != 3 {
		t.Fatalf("want 3 survivors, got %d chunks / %d vecs", len(oc), len(ov))
	}
	for i, c := range oc {
		if c.ID == "b" {
			t.Fatalf("poison chunk b should have been skipped")
		}
		if len(ov[i]) != 8 {
			t.Fatalf("survivor %s has wrong vec dim %d", c.ID, len(ov[i]))
		}
	}
}

// sizeLimitEmbedder fails any batch containing an input larger than limit bytes
// — mirrors ollama's Qwen3-4b endpoint crashing on oversized chunks.
type sizeLimitEmbedder struct {
	limit int
	dim   int
}

func (e *sizeLimitEmbedder) Identity() types.EmbeddingIdentity {
	return types.EmbeddingIdentity{Provider: "test", Model: "sizelimit", Dim: e.dim}
}
func (e *sizeLimitEmbedder) Name() string        { return "sizelimit" }
func (e *sizeLimitEmbedder) Dimension() int      { return e.dim }
func (e *sizeLimitEmbedder) MaxInputTokens() int { return 8192 }
func (e *sizeLimitEmbedder) Embed(ctx context.Context, batch []string) ([][]float32, error) {
	for _, t := range batch {
		if len(t) > e.limit {
			return nil, fmt.Errorf("input %d bytes exceeds limit (simulated crash)", len(t))
		}
	}
	out := make([][]float32, len(batch))
	for i := range out {
		out[i] = make([]float32, e.dim)
		out[i][0] = 1
	}
	return out, nil
}

// TestEmbedResilient_RecoversOversizedByTruncating verifies an oversized chunk
// that the backend rejects is recovered by re-embedding a truncated input,
// rather than being skipped.
func TestEmbedResilient_RecoversOversizedByTruncating(t *testing.T) {
	// Crashes on inputs > 20 KB; truncate cap (maxEmbedRetryBytes ~12 KB) fits.
	emb := &sizeLimitEmbedder{limit: 20000, dim: 8}
	big := strings.Repeat("x", 30000)

	oc, ov, err := embedResilient(context.Background(), emb, []types.Chunk{{ID: "big"}}, []string{big})
	if err != nil {
		t.Fatalf("embedResilient: %v", err)
	}
	if len(oc) != 1 || len(ov) != 1 {
		t.Fatalf("oversized chunk should be recovered via truncation, not skipped: got %d chunks", len(oc))
	}
	if len(ov[0]) != 8 {
		t.Fatalf("recovered vector has wrong dim %d", len(ov[0]))
	}
}

// TestEmbedResilient_PropagatesCtxError verifies a cancelled context is a hard
// error, not treated as a per-input rejection to skip.
func TestEmbedResilient_PropagatesCtxError(t *testing.T) {
	emb := &poisonEmbedder{poison: "POISON", dim: 8}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := embedResilient(ctx, emb, []types.Chunk{{ID: "a"}}, []string{"ok"})
	if err == nil {
		t.Fatalf("expected error on cancelled context, got nil (chunk would be silently skipped)")
	}
}
