package build

import (
	"fmt"
	"io"
	"time"
)

// progressEveryFiles is the burst cap: at most one progress line per
// this many files, regardless of clock time. Tuned for typical
// throughput (0.5–2 files/s on bgeonnx) so a 1k-file index emits ~10
// lines instead of 1000.
const progressEveryFiles = 100

// progressMinDuration is the inverse: minimum elapsed time between
// progress lines, so very fast indexes (mock embedder, small repos)
// still get *some* feedback at a human-readable cadence.
const progressMinDuration = 2 * time.Second

// progress is the build pipeline's stderr-side progress reporter. It is
// independent of the footprint logger (which is JSONL on a different
// sink) so machine consumers and humans never collide. A nil receiver
// or zero total makes every Tick a no-op, which is the contract the
// builder relies on when Options.ProgressOut is unset.
type progress struct {
	w           io.Writer
	total       int
	start       time.Time
	lastEmit    time.Time
	lastEmitIdx int
	now         func() time.Time
}

// newProgress constructs a reporter. If w is nil or total ≤ 0 the
// returned *progress is still safe to call — every Tick returns false
// without writing. now defaults to time.Now; tests inject a clock.
func newProgress(w io.Writer, total int, now func() time.Time) *progress {
	if now == nil {
		now = time.Now
	}
	t := now()
	return &progress{
		w:        w,
		total:    total,
		start:    t,
		lastEmit: t,
		now:      now,
	}
}

// Tick records that `processed` files (1-indexed cumulative count) have
// been considered and emits a progress line when any of the gates fire:
//
//   - processed >= total (final line — always emit so the user sees
//     the completion summary even on fast/small indexes).
//   - processed - lastEmitIdx >= progressEveryFiles.
//   - now - lastEmit >= progressMinDuration.
//
// Returns true when it wrote a line.
func (p *progress) Tick(processed int) bool {
	if p == nil || p.w == nil || p.total <= 0 || processed <= 0 {
		return false
	}
	now := p.now()
	isFinal := processed >= p.total
	sinceLastFiles := processed - p.lastEmitIdx
	sinceLastTime := now.Sub(p.lastEmit)
	if !isFinal && sinceLastFiles < progressEveryFiles && sinceLastTime < progressMinDuration {
		return false
	}

	elapsed := now.Sub(p.start)
	rate := 0.0
	if sec := sinceLastTime.Seconds(); sec > 0 {
		rate = float64(sinceLastFiles) / sec
	}
	eta := time.Duration(0)
	if rate > 0 && !isFinal {
		remaining := p.total - processed
		eta = time.Duration(float64(remaining) / rate * float64(time.Second))
	}

	fmt.Fprintf(p.w, "ckv: %d/%d files (%.1f files/s, elapsed %s, ETA %s)\n",
		processed, p.total, rate, truncSeconds(elapsed), truncSeconds(eta))
	p.lastEmit = now
	p.lastEmitIdx = processed
	return true
}

func truncSeconds(d time.Duration) time.Duration {
	if d < 0 {
		return 0
	}
	return d.Truncate(time.Second)
}
