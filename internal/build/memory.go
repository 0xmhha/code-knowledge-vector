// Memory guard for index builds. Two layers:
//
//   - Pre-check: refuse to load the embedder if available RAM <
//     ModelConfig.EstimatedRAMMB × headroom. Avoids OOM-killing the host
//     during model load (the one place batch size can't help — model
//     weights are a fixed cost).
//
//   - Runtime adaptive batch: while embedding is in progress, a watchdog
//     goroutine polls free RAM every pollInterval. When it crosses the
//     low-water threshold, it sets a pressure flag; embedAndUpsert
//     halves its working batch on the next iteration (down to 1).
//
// Disable both layers with CKV_MEM_GUARD=off. On platforms without a
// MemProvider (currently anything other than darwin/linux), both layers
// are no-ops (fail-open).

package build

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"
)

// MemStat is a snapshot of host memory in MB.
type MemStat struct {
	TotalMB     uint64
	AvailableMB uint64
}

// MemProvider is the OS-specific memory reader. memory_<goos>.go files
// set defaultMemProvider in init(); tests inject mocks via the same
// variable.
type MemProvider interface {
	Read() (MemStat, error)
}

// defaultMemProvider is wired by OS-specific init(). nil on unsupported
// platforms, which disables the guard.
var defaultMemProvider MemProvider

// Tunables. Exposed as package vars so tests can drop the watchdog
// poll interval without sleeping seconds.
var (
	// preCheckHeadroomFactor multiplies the embedder's estimate before
	// comparing to AvailableMB. 1.5× covers (a) compile-time spikes in
	// CoreML/ORT and (b) the unknown working set the model uses beyond
	// its weight bytes.
	preCheckHeadroomFactor = 1.5

	// watchdogPollInterval is how often the watchdog rereads memory.
	// 5s strikes a balance: long enough that vm_stat exec cost is noise
	// (~10ms), short enough to react before the next large batch.
	watchdogPollInterval = 5 * time.Second

	// watchdogLowMB is the AvailableMB threshold under which the
	// watchdog raises pressure. Override via CKV_MEM_GUARD_LOW_MB.
	defaultWatchdogLowMB uint64 = 500
)

func memGuardEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("CKV_MEM_GUARD")))
	switch v {
	case "off", "0", "false", "no":
		return false
	default:
		return true
	}
}

func watchdogLowMB() uint64 {
	if v := strings.TrimSpace(os.Getenv("CKV_MEM_GUARD_LOW_MB")); v != "" {
		var n uint64
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return defaultWatchdogLowMB
}

// memEstimatedRAMMB pulls the embedder's RAM estimate via a duck-typed
// interface. Returns 0 when the embedder doesn't expose one (e.g. the
// mock embedder) — treated by callers as "guard skipped".
func memEstimatedRAMMB(emb any) uint64 {
	if e, ok := emb.(interface{ EstimatedRAMMB() uint64 }); ok {
		return e.EstimatedRAMMB()
	}
	return 0
}

// PreCheckByEstimate verifies host memory headroom against an
// already-known RAM estimate in MB. Intended for the CLI layer, where
// the embedder isn't constructed yet but the estimate is recoverable
// from model config (e.g. bgeonnx.EstimatedRAMMB(opts)).
//
// rawNeedMB == 0 disables the check (caller doesn't know — treat as
// fail-open). CKV_MEM_GUARD=off or a missing MemProvider also disables.
func PreCheckByEstimate(rawNeedMB uint64, w io.Writer) error {
	if !memGuardEnabled() || defaultMemProvider == nil || rawNeedMB == 0 {
		return nil
	}
	needMB := uint64(float64(rawNeedMB) * preCheckHeadroomFactor)
	stat, err := defaultMemProvider.Read()
	if err != nil {
		if w != nil {
			fmt.Fprintf(w, "ckv: mem guard: read failed (%v), skipping pre-check\n", err)
		}
		return nil
	}
	if stat.AvailableMB < needMB {
		return fmt.Errorf(
			"mem guard: model needs ~%d MB headroom (%.1f× of %d MB estimate) but only %d MB available (total %d MB). "+
				"Free memory, switch to a smaller model, or set CKV_MEM_GUARD=off to skip",
			needMB, preCheckHeadroomFactor, rawNeedMB, stat.AvailableMB, stat.TotalMB,
		)
	}
	if w != nil {
		fmt.Fprintf(w, "ckv: mem guard: pre-check OK (need ~%d MB, avail %d MB, total %d MB)\n",
			needMB, stat.AvailableMB, stat.TotalMB)
	}
	return nil
}

// preCheckMemory is the embedder-driven wrapper retained as a safety
// net for callers that hit build.Run() without going through the CLI
// (in-process embedders, tests, future CKS integration). The CLI
// layer should call PreCheckByEstimate directly so the model never
// loads when memory is short.
func preCheckMemory(emb any, w io.Writer) error {
	return PreCheckByEstimate(memEstimatedRAMMB(emb), w)
}

// memSignal is the shared pressure flag between the watchdog goroutine
// and the embed loop. atomic.Bool keeps the read-side lock-free.
type memSignal struct {
	pressure atomic.Bool
}

func (s *memSignal) underPressure() bool {
	if s == nil {
		return false
	}
	return s.pressure.Load()
}

// startMemWatchdog spawns the watchdog goroutine. Returns nil when the
// guard is disabled or there's no MemProvider. Watchdog exits when ctx
// is canceled. The returned *memSignal is safe to read concurrently.
func startMemWatchdog(ctx context.Context, w io.Writer) *memSignal {
	if !memGuardEnabled() || defaultMemProvider == nil {
		return nil
	}
	sig := &memSignal{}
	low := watchdogLowMB()
	provider := defaultMemProvider
	poll := watchdogPollInterval

	go func() {
		ticker := time.NewTicker(poll)
		defer ticker.Stop()
		var loggedOnce bool
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				stat, err := provider.Read()
				if err != nil {
					continue
				}
				if stat.AvailableMB < low {
					if !sig.pressure.Swap(true) && w != nil && !loggedOnce {
						fmt.Fprintf(w, "ckv: mem guard: pressure ON (avail %d MB < %d MB) — shrinking batch\n",
							stat.AvailableMB, low)
						loggedOnce = true
					}
				} else {
					if sig.pressure.Swap(false) && w != nil {
						fmt.Fprintf(w, "ckv: mem guard: pressure OFF (avail %d MB ≥ %d MB)\n",
							stat.AvailableMB, low)
						loggedOnce = false
					}
				}
			}
		}
	}()
	return sig
}
