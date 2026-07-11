package ollama

import "testing"

func TestQwen3QueryText(t *testing.T) {
	got := qwen3QueryText("retrieve code", "how is genesis written")
	want := "Instruct: retrieve code\nQuery: how is genesis written"
	if got != want {
		t.Fatalf("qwen3QueryText = %q, want %q", got, want)
	}
}

// TestEmbedQuery_NoInstructIsPlainEmbed verifies that an adapter with no query
// instruct (symmetric model) applies no prefix — EmbedQuery is just Embed.
func TestEmbedQuery_NoInstructIsPlainEmbed(t *testing.T) {
	a := &Adapter{modelName: "bge-m3", dim: 4} // queryInstruct empty
	if a.queryInstruct != "" {
		t.Fatalf("expected empty queryInstruct for a symmetric model")
	}
}
