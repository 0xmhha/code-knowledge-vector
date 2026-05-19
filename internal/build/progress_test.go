package build

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// fakeClock is a tiny manual clock for deterministic Tick scheduling.
// Tests advance it explicitly via Add().
type fakeClock struct {
	t time.Time
}

func (c *fakeClock) Now() time.Time { return c.t }
func (c *fakeClock) Add(d time.Duration) {
	c.t = c.t.Add(d)
}

func TestProgress_NoOpWhenWriterNil(t *testing.T) {
	// Contract: nil writer (the builder's "ProgressOut unset" case) makes
	// every Tick a silent no-op. Regressing this would spam stderr in
	// library-mode embedders.
	p := newProgress(nil, 100, nil)
	if got := p.Tick(50); got {
		t.Errorf("Tick on nil writer should not emit, got %v", got)
	}
}

func TestProgress_NoOpWhenTotalZero(t *testing.T) {
	// total=0 means the caller has nothing to index; emitting a
	// "0/0 files" line is just noise. Treat as no-op.
	var buf bytes.Buffer
	p := newProgress(&buf, 0, nil)
	if got := p.Tick(1); got {
		t.Errorf("Tick on zero-total should not emit, got %v", got)
	}
	if buf.Len() != 0 {
		t.Errorf("expected empty buffer, got %q", buf.String())
	}
}

func TestProgress_EmitsFinalLineEvenForSmallTotals(t *testing.T) {
	// A 3-file index won't cross the 100-file gate or the 2s gate, but
	// the final tick must still emit — otherwise the user gets nothing.
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	p := newProgress(&buf, 3, clk.Now)

	clk.Add(50 * time.Millisecond)
	if p.Tick(1) {
		t.Errorf("intermediate tick on small total should not emit")
	}
	clk.Add(50 * time.Millisecond)
	if p.Tick(2) {
		t.Errorf("intermediate tick on small total should not emit")
	}
	clk.Add(50 * time.Millisecond)
	if !p.Tick(3) {
		t.Fatalf("final tick must emit, but did not")
	}

	out := buf.String()
	if !strings.Contains(out, "3/3 files") {
		t.Errorf("final line missing '3/3 files', got %q", out)
	}
}

func TestProgress_EmitsAfterFileGate(t *testing.T) {
	// Crossing progressEveryFiles must emit even if very little time
	// passed (e.g. mock embedder bursts through hundreds of files/s).
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	p := newProgress(&buf, 1000, clk.Now)

	clk.Add(10 * time.Millisecond)
	if p.Tick(50) {
		t.Errorf("Tick at 50 files (< gate) should not emit")
	}
	clk.Add(10 * time.Millisecond)
	if !p.Tick(progressEveryFiles) {
		t.Fatalf("Tick at gate (%d files) should emit", progressEveryFiles)
	}
	if !strings.Contains(buf.String(), "100/1000") {
		t.Errorf("expected '100/1000' line, got %q", buf.String())
	}
}

func TestProgress_EmitsAfterTimeGate(t *testing.T) {
	// Slow embedder: even a few files over progressMinDuration must
	// trigger a line so the user knows the process is alive.
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	p := newProgress(&buf, 500, clk.Now)

	clk.Add(progressMinDuration + time.Millisecond)
	if !p.Tick(5) {
		t.Fatalf("time-gate Tick should emit")
	}
	if !strings.Contains(buf.String(), "5/500") {
		t.Errorf("expected '5/500' line, got %q", buf.String())
	}
}

func TestProgress_ResetsCountersAfterEmit(t *testing.T) {
	// The file gate is *relative to the last emit*, not absolute. After
	// emitting at 100, the next emission shouldn't fire until 200, even
	// though processed continues to grow.
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	p := newProgress(&buf, 1000, clk.Now)

	clk.Add(10 * time.Millisecond)
	if !p.Tick(100) {
		t.Fatal("first gate emit failed")
	}
	clk.Add(10 * time.Millisecond)
	if p.Tick(150) {
		t.Errorf("Tick at 150 (50 since last emit) should not fire")
	}
	clk.Add(10 * time.Millisecond)
	if !p.Tick(200) {
		t.Errorf("Tick at 200 (100 since last emit) should fire")
	}
	if got := strings.Count(buf.String(), "ckv:"); got != 2 {
		t.Errorf("expected 2 emissions, got %d in %q", got, buf.String())
	}
}

func TestProgress_RateAndETACalculation(t *testing.T) {
	// 100 files in 20s → rate=5.0 files/s; 900 remaining → ETA=180s = 3m.
	// Asserts the human-facing arithmetic stays correct.
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	p := newProgress(&buf, 1000, clk.Now)

	clk.Add(20 * time.Second)
	if !p.Tick(100) {
		t.Fatal("expected emit at 100")
	}
	out := buf.String()
	if !strings.Contains(out, "5.0 files/s") {
		t.Errorf("expected rate '5.0 files/s' in %q", out)
	}
	if !strings.Contains(out, "ETA 3m0s") {
		t.Errorf("expected 'ETA 3m0s' in %q", out)
	}
	if !strings.Contains(out, "elapsed 20s") {
		t.Errorf("expected 'elapsed 20s' in %q", out)
	}
}

func TestProgress_FinalLineETAZero(t *testing.T) {
	// Final tick: nothing left to do → ETA=0s.
	var buf bytes.Buffer
	clk := &fakeClock{t: time.Unix(1000, 0)}
	p := newProgress(&buf, 200, clk.Now)

	clk.Add(progressMinDuration + time.Millisecond)
	p.Tick(100) // intermediate emit (time gate)
	clk.Add(time.Second)
	if !p.Tick(200) {
		t.Fatal("final tick should emit")
	}
	// The last line in the buffer is the final one.
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	last := lines[len(lines)-1]
	if !strings.Contains(last, "200/200") {
		t.Errorf("expected last line to be 200/200, got %q", last)
	}
	if !strings.Contains(last, "ETA 0s") {
		t.Errorf("expected 'ETA 0s' on final line, got %q", last)
	}
}
