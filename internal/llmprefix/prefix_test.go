package llmprefix

import (
	"context"
	"errors"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

type mockGen struct {
	calls int
	out   string
	err   error
}

func (m *mockGen) Generate(_ context.Context, _ string) (string, error) {
	m.calls++
	return m.out, m.err
}

func TestCached_GeneratesSanitizesAndCaches(t *testing.T) {
	g := &mockGen{out: "  Parses the genesis file.\ntrailing junk  "}
	p, err := NewCached(g, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	c := types.Chunk{Text: "func initGenesis() {}", File: "cmd/x.go", Language: "go"}

	got := p.Prefix(context.Background(), c)
	if got != "Parses the genesis file." {
		t.Fatalf("prefix = %q, want single trimmed line", got)
	}
	if g.calls != 1 {
		t.Fatalf("generator calls = %d, want 1", g.calls)
	}

	// Second call for the same chunk content → cache hit, no new generation.
	if got := p.Prefix(context.Background(), c); got != "Parses the genesis file." {
		t.Fatalf("cached prefix = %q", got)
	}
	if g.calls != 1 {
		t.Fatalf("expected cache hit; generator called %d times", g.calls)
	}
}

func TestCached_GenerationErrorFallsBackToEmpty(t *testing.T) {
	p, _ := NewCached(&mockGen{err: errors.New("model down")}, t.TempDir())
	if got := p.Prefix(context.Background(), types.Chunk{Text: "x"}); got != "" {
		t.Fatalf("generation error should yield empty prefix, got %q", got)
	}
}

func TestCached_NilGeneratorIsNoOp(t *testing.T) {
	p, _ := NewCached(nil, "")
	if got := p.Prefix(context.Background(), types.Chunk{Text: "x"}); got != "" {
		t.Fatalf("nil generator should yield empty prefix, got %q", got)
	}
}
