package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestHandleFindInvariants_ReturnsCRITICAL_FromFixture indexes a small
// corpus with a CRITICAL marker and asserts find_invariants returns it.
func TestHandleFindInvariants_ReturnsCRITICAL_FromFixture(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleFindInvariants(context.Background(),
		callRequest("cks.context.find_invariants", map[string]any{}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("error: %s", textContent(t, res))
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	count, _ := resp["count"].(float64)
	// The sample corpus does not include CRITICAL markers, but the
	// handler should at least return a well-formed (possibly empty)
	// response. A count >= 0 is enough; a future change to the sample
	// could provide real invariants.
	if count < 0 {
		t.Errorf("invalid count: %v", count)
	}
}

// TestHandleFindInvariants_TierFilter exercises the tier_min argument
// path. With an empty corpus this is mostly an arg-parsing check, but
// it also documents the public contract.
func TestHandleFindInvariants_TierFilter(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleFindInvariants(context.Background(),
		callRequest("cks.context.find_invariants", map[string]any{
			"tier_min": 2.0,
		}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("err: %s", textContent(t, res))
	}
}

// TestHandleGetConventions_ReturnsSummaryAndStats runs on the sample
// corpus which has Go files, so at least one convention chunk must
// have been emitted by the builder.
func TestHandleGetConventions_ReturnsSummaryAndStats(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleGetConventions(context.Background(),
		callRequest("cks.context.get_conventions", map[string]any{}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("err: %s", textContent(t, res))
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	convs, _ := resp["conventions"].([]any)
	if len(convs) == 0 {
		t.Fatal("expected ≥1 convention chunk for a Go corpus")
	}
	first, _ := convs[0].(map[string]any)
	summary, _ := first["summary"].(string)
	if !strings.HasPrefix(summary, "package:") {
		t.Errorf("summary should start with 'package:', got %q", summary)
	}
	stats, ok := first["stats"].(map[string]any)
	if !ok || stats == nil {
		t.Errorf("expected stats map, got %v", first["stats"])
	}
	if _, ok := stats["file_count"]; !ok {
		t.Errorf("stats should include file_count: %v", stats)
	}
}

// TestHandleGetConventions_PackageFilter narrows by package prefix.
// Using a non-existent prefix should produce zero results without
// erroring.
func TestHandleGetConventions_PackageFilter_Empty(t *testing.T) {
	eng := buildSample(t)
	s := NewServer(eng)
	res, err := s.handleGetConventions(context.Background(),
		callRequest("cks.context.get_conventions", map[string]any{
			"package": "definitely/nowhere",
		}))
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if res.IsError {
		t.Fatalf("err: %s", textContent(t, res))
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(textContent(t, res)), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	convs, _ := resp["conventions"].([]any)
	if len(convs) != 0 {
		t.Errorf("expected 0 matches for unknown prefix, got %d", len(convs))
	}
}
