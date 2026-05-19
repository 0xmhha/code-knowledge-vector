package prregress

import (
	"reflect"
	"testing"
)

func TestTruthFiles_SortsAndTrims(t *testing.T) {
	// Input order from gh API is API-driven (alphabetical-ish but not
	// guaranteed). We sort to keep downstream diffs stable across
	// reruns. Whitespace-only paths are dropped (defensive — gh has
	// never produced them, but the cost of guarding is zero).
	m := Meta{
		Files: []ChangedFile{
			{Path: "core/types/receipt.go"},
			{Path: "core/state_transition.go"},
			{Path: "  "},
			{Path: "params/protocol_params.go"},
		},
	}
	got := TruthFiles(m)
	want := []string{
		"core/state_transition.go",
		"core/types/receipt.go",
		"params/protocol_params.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TruthFiles = %v, want %v", got, want)
	}
}

func TestTruthFiles_EmptyMeta(t *testing.T) {
	got := TruthFiles(Meta{})
	if len(got) != 0 {
		t.Errorf("empty meta: got %v, want []", got)
	}
}
