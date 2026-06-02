package ckv_test

// Operator-gated smoke for M2.b/c (02-ckv-refactor.md §6): validates a REAL
// bge-m3 go-stablenet index built on a machine with Ollama. The ~10h embed is
// run by the operator; this committed test makes the acceptance REPRODUCIBLE
// (mirrors ckg's gated gsn_smoke_test.go). It exercises the exact cks-facing
// surface (pkg/ckv + pkg/embed/ollama, in-process, CGO-free embedder).
//
// Run after building the index on an Ollama machine:
//
//	ollama serve & ollama pull bge-m3
//	ckv build --embedder=ollama --model-name=bge-m3 --src <go-stablenet> --out ./ckv-stablenet
//	CKV_GSN_INDEX=./ckv-stablenet go test ./pkg/ckv/ -run TestGoStablenetBgeM3Smoke -v
//
// Env:
//   CKV_GSN_INDEX     (required) dir holding vector.db + manifest.json. Skipped if unset.
//   CKV_GSN_MODEL     (optional) embedder model name; default "bge-m3".
//   CKV_OLLAMA_ENDPOINT (optional) honored by ollama.Open; default http://localhost:11434.

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/ckv"
	"github.com/0xmhha/code-knowledge-vector/pkg/embed/ollama"
)

func TestGoStablenetBgeM3Smoke(t *testing.T) {
	indexDir := os.Getenv("CKV_GSN_INDEX")
	if indexDir == "" {
		t.Skip("set CKV_GSN_INDEX to a bge-m3 go-stablenet index dir (vector.db + manifest.json) to run the M2.b/c smoke")
	}
	model := os.Getenv("CKV_GSN_MODEL")
	if model == "" {
		model = "bge-m3"
	}

	// Real, CGO-free, in-process embedder against a live Ollama daemon — the
	// M5 gate (does this Ollama serve bge-m3 embeddings?) and the cks import
	// path in one shot. A failure here is the operator's signal that Ollama
	// isn't serving the model, not a code defect.
	emb, err := ollama.Open(ollama.Options{ModelName: model})
	if err != nil {
		t.Fatalf("ollama.Open(model=%q): %v — is `ollama serve` running with `ollama pull %s`?", model, err, model)
	}
	defer func() { _ = emb.Close() }()

	engine, err := ckv.Open(indexDir, ckv.OpenOptions{Embedder: emb})
	if err != nil {
		t.Fatalf("ckv.Open(%q): %v (model identity must match the index manifest)", indexDir, err)
	}
	defer func() { _ = engine.Close() }()

	// --- M2.b: index identity (bge-m3, 1024-dim) ---
	man := engine.Manifest()
	if man.EmbeddingModel != model {
		t.Errorf("manifest embedding_model=%q, want %q", man.EmbeddingModel, model)
	}
	if model == "bge-m3" && man.EmbeddingDim != 1024 {
		t.Errorf("manifest embedding_dim=%d, want 1024 (bge-m3)", man.EmbeddingDim)
	}
	if man.EmbeddingDim != emb.Dimension() {
		t.Errorf("manifest dim=%d disagrees with live embedder dim=%d", man.EmbeddingDim, emb.Dimension())
	}
	t.Logf("M2.b identity: model=%s dim=%d chunks=%d src=%s",
		man.EmbeddingModel, man.EmbeddingDim, man.ChunkCount, man.SrcRoot)

	// --- M2.b: structured freshness reports through the cks-facing method ---
	rep, ferr := engine.Freshness()
	if ferr != nil {
		t.Fatalf("Freshness(): %v", ferr)
	}
	t.Logf("M2.b freshness: fresh=%v stale=%v indexed=%s current=%s changed=%d warnings=%v",
		rep.Fresh, rep.Stale, short(rep.IndexedHead), short(rep.CurrentHead), len(rep.ChangedFiles), rep.Warnings)

	// --- M2.c: domain queries return hits carrying policy guidance ---
	// go-stablenet domain intents (non-Ethereum: WBFT consensus, governance
	// system contracts). At least one hit across these must carry a policy
	// Category/Guidance (the operational guidance channel, 00 §4.1) and we log
	// any Tier-2 invariant chunks surfaced (00 §4.2 marker seeding).
	intents := []string{
		"validator quorum and consensus power under WBFT",
		"governance minter burn and mint authorization",
		"block validation invariant that must hold",
	}
	var totalHits, guidanceHits, invariantHits int
	for _, intent := range intents {
		resp, serr := engine.SemanticSearch(context.Background(), intent,
			ckv.SearchOptions{K: 10, Threshold: -1})
		if serr != nil {
			t.Fatalf("SemanticSearch(%q): %v", intent, serr)
		}
		if resp == nil {
			continue
		}
		totalHits += len(resp.Hits)
		for _, h := range resp.Hits {
			if h.Category != "" || h.Guidance != nil {
				guidanceHits++
			}
			if strings.Contains(strings.ToLower(string(h.SymbolKind)), "invariant") {
				invariantHits++
			}
		}
		t.Logf("M2.c query %q → %d hits", intent, len(resp.Hits))
	}

	if totalHits == 0 {
		t.Fatal("M2.c: domain queries returned 0 hits — index empty or embedder mismatch")
	}
	if guidanceHits == 0 {
		t.Errorf("M2.c: no hit across %d domain queries carried policy Category/Guidance — "+
			"is policy/stablenet.yaml applied at build time?", len(intents))
	}
	// Tier-2 invariant markers depend on go-stablenet-side seeding (00 §4.2);
	// log rather than hard-fail so this smoke is useful before seeding lands.
	t.Logf("M2.c totals: hits=%d guidance-tagged=%d invariant-chunks=%d (invariant seeding is a go-stablenet task, informational)",
		totalHits, guidanceHits, invariantHits)
}

func short(sha string) string {
	if len(sha) > 12 {
		return sha[:12]
	}
	return sha
}
