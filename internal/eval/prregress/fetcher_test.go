package prregress

import (
	"strings"
	"testing"
)

func TestExtractBackground_HashHeader(t *testing.T) {
	body := `### Background

In Anzeon, ` + "`effectiveGasPrice`" + ` is stored alongside the receipt.
However, receipts received via snap sync are RLP-encoded.

### Solution

Emit AuthorizedTxExecuted event log.

### Changes

- params: add constant
`
	got := ExtractBackground(body)
	if !strings.Contains(got, "stored alongside the receipt") {
		t.Errorf("missing Background content: %q", got)
	}
	if strings.Contains(got, "Emit AuthorizedTxExecuted") {
		t.Errorf("Solution leaked into Background: %q", got)
	}
	if strings.Contains(got, "params: add constant") {
		t.Errorf("Changes leaked into Background: %q", got)
	}
}

func TestExtractBackground_BoldHeader(t *testing.T) {
	body := `**Background**

The chunker truncates files at 8KB.

**Solution**

Add a streaming path.
`
	got := ExtractBackground(body)
	if !strings.Contains(got, "truncates files") {
		t.Errorf("missing Background content: %q", got)
	}
	if strings.Contains(got, "streaming path") {
		t.Errorf("Solution leaked into Background: %q", got)
	}
}

func TestExtractBackground_CaseInsensitiveAndColon(t *testing.T) {
	body := `## BACKGROUND:

Cache invalidation is broken.

## solution

Fix it.
`
	got := ExtractBackground(body)
	if !strings.Contains(got, "Cache invalidation") {
		t.Errorf("missing Background content: %q", got)
	}
}

func TestExtractBackground_NoHeaderFallsBackToWholeBody(t *testing.T) {
	// Some teams skip the header entirely and write a freeform PR
	// description. We must not return an empty string — give the
	// agent everything we have.
	body := "This PR fixes a memory leak in the worker pool."
	got := ExtractBackground(body)
	if got != body {
		t.Errorf("no-header fallback: got %q, want full body", got)
	}
}

func TestExtractBackground_EmptyBody(t *testing.T) {
	if got := ExtractBackground(""); got != "" {
		t.Errorf("empty body: got %q", got)
	}
}

func TestExtractBackground_BackgroundIsLastSection(t *testing.T) {
	// If Background has no successor header, everything after it is
	// the Background.
	body := `### Background

Just this — no Solution section.
`
	got := ExtractBackground(body)
	if !strings.Contains(got, "Just this") {
		t.Errorf("last-section Background: got %q", got)
	}
}
