package ckv_test

import (
	"context"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

// The FindInvariants / GetConventions facade methods exist so in-process
// consumers (the cks find_invariants / get_conventions tools) reach CKV's
// invariant and convention indexes without spawning the mcp binary. These
// tests assert the public call path works and is nil-safe after Close;
// result counts are fixture-dependent and intentionally not asserted.

func TestFindInvariants_CallPath(t *testing.T) {
	out := buildSampleIndex(t)
	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer engine.Close()

	hits, err := engine.FindInvariants(context.Background(), "", "", 1)
	if err != nil {
		t.Fatalf("FindInvariants: %v", err)
	}
	// Any returned invariant must carry a non-zero confidence tier so the
	// caller can weight curated markers above heuristics.
	for _, h := range hits {
		if h.Tier == 0 {
			t.Errorf("invariant %s returned with zero tier", h.ChunkID)
		}
	}
}

func TestGetConventions_CallPath(t *testing.T) {
	out := buildSampleIndex(t)
	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer engine.Close()

	if _, err := engine.GetConventions(context.Background(), ""); err != nil {
		t.Fatalf("GetConventions: %v", err)
	}
}

func TestFindInvariants_AfterCloseFails(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	engine.Close()

	if _, err := engine.FindInvariants(context.Background(), "", "", 1); err == nil {
		t.Error("FindInvariants after Close: want error, got nil")
	}
	if _, err := engine.GetConventions(context.Background(), ""); err == nil {
		t.Error("GetConventions after Close: want error, got nil")
	}
}
