package ckv_test

import (
	"context"
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/ckv"
)

// FindInvariants / GetConventions are thin facades over internal/query.
// internal/query already covers the matching behavior; these tests pin
// the facade's own contract: it delegates on an open engine and guards
// a closed one (mirrors FindBranches / GetInvariantEnforcement).

func TestFindInvariants_DelegatesOnOpenEngine(t *testing.T) {
	out := buildSampleIndex(t)
	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer engine.Close()

	// The sample corpus may carry no invariants; delegation must still
	// succeed with a (possibly empty) slice and no error.
	hits, err := engine.FindInvariants(context.Background(), "", "", 0)
	if err != nil {
		t.Fatalf("FindInvariants: %v", err)
	}
	for i, h := range hits {
		if h.File == "" {
			t.Errorf("invariant %d: empty File", i)
		}
	}
}

func TestGetConventions_DelegatesOnOpenEngine(t *testing.T) {
	out := buildSampleIndex(t)
	engine, err := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer engine.Close()

	convs, err := engine.GetConventions(context.Background(), "")
	if err != nil {
		t.Fatalf("GetConventions: %v", err)
	}
	for i, c := range convs {
		if c.Package == "" && c.File == "" {
			t.Errorf("convention %d: both Package and File empty", i)
		}
	}
}

func TestFindInvariants_AfterCloseFails(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	engine.Close()

	if _, err := engine.FindInvariants(context.Background(), "", "", 0); err == nil {
		t.Fatal("expected error after Close, got nil")
	}
}

func TestGetConventions_AfterCloseFails(t *testing.T) {
	out := buildSampleIndex(t)
	engine, _ := ckv.Open(out, ckv.OpenOptions{Embedder: ckv.MockEmbedder()})
	engine.Close()

	if _, err := engine.GetConventions(context.Background(), ""); err == nil {
		t.Fatal("expected error after Close, got nil")
	}
}
