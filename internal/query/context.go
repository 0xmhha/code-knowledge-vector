package query

import "github.com/0xmhha/code-knowledge-vector/pkg/types"

// SearchContext carries the evolving state through the search pipeline.
// Each service reads its input fields, does its work, and writes its
// output fields. The Facade (Engine.Search) assembles the final Response
// from this context.
//
// Individual services can also be called directly by passing a
// SearchContext with only the relevant fields populated.
type SearchContext struct {
	// Input (set by caller before pipeline starts)
	Intent      string
	EmbedIntent string // after alias expansion; same as Intent when no aliases
	Options     Options
	TraceID     string

	// After EmbedService
	QueryVec []float32

	// After StoreSearch
	RawHits []types.Hit

	// After Rerank + Threshold + Citation
	FilteredHits []types.Hit

	// After TestSplit
	PrimaryHits []types.Hit
	ExampleHits []types.Hit

	// After DensityAdjust
	FinalHits     []Hit
	FinalExamples []Hit
	TokensUsed    int

	// Accumulated warnings
	Warnings []string

	// Metadata
	DroppedCitations int
	StaleCitations   int
}
