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
