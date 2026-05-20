package build

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// staticProvider returns a fixed MemStat (and optional error). For
// transition tests, use scriptedProvider.
type staticProvider struct {
	stat MemStat
	err  error
}

func (p staticProvider) Read() (MemStat, error) { return p.stat, p.err }

// ramMock implements the duck-typed EstimatedRAMMB() method on emb.
type ramMock struct{ ram uint64 }

func (m *ramMock) EstimatedRAMMB() uint64 { return m.ram }

// noEstimateMock has no EstimatedRAMMB method — preCheck should treat
// it as "unknown, skip the guard" (fail-open).
type noEstimateMock struct{}

// withProvider swaps defaultMemProvider for the duration of t and
// restores it on cleanup. Necessary because the OS-specific init() in
// memory_<goos>.go wires a real provider that would dominate tests.
func withProvider(t *testing.T, p MemProvider) {
	t.Helper()
	save := defaultMemProvider
	defaultMemProvider = p
	t.Cleanup(func() { defaultMemProvider = save })
}

// withEnv sets env k=v for the duration of t. Empty v unsets.
func withEnv(t *testing.T, k, v string) {
	t.Helper()
	t.Setenv(k, v)
}

func TestPreCheckMemory_GuardDisabledReturnsNil(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "off")
	withProvider(t, staticProvider{stat: MemStat{TotalMB: 16000, AvailableMB: 100}})
	emb := &ramMock{ram: 5000}
	if err := preCheckMemory(emb, nil); err != nil {
		t.Fatalf("guard=off should fail-open, got %v", err)
	}
}

func TestPreCheckMemory_NoProviderReturnsNil(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "on")
	withProvider(t, nil)
	emb := &ramMock{ram: 5000}
	if err := preCheckMemory(emb, nil); err != nil {
		t.Fatalf("nil provider should fail-open, got %v", err)
	}
}

func TestPreCheckMemory_NoEstimateReturnsNil(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "on")
	withProvider(t, staticProvider{stat: MemStat{TotalMB: 16000, AvailableMB: 100}})
	if err := preCheckMemory(&noEstimateMock{}, nil); err != nil {
		t.Fatalf("embedder without estimate should fail-open, got %v", err)
	}
}

func TestPreCheckMemory_EnoughMemoryPasses(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "on")
	withProvider(t, staticProvider{stat: MemStat{TotalMB: 16000, AvailableMB: 10000}})
	var buf bytes.Buffer
	emb := &ramMock{ram: 5000} // needs 5000 × 1.5 = 7500 MB; 10000 avail
	if err := preCheckMemory(emb, &buf); err != nil {
		t.Fatalf("sufficient memory should pass, got %v", err)
	}
	if !strings.Contains(buf.String(), "pre-check OK") {
		t.Errorf("expected pre-check OK log, got %q", buf.String())
	}
}

func TestPreCheckMemory_InsufficientReturnsError(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "on")
	withProvider(t, staticProvider{stat: MemStat{TotalMB: 8000, AvailableMB: 2000}})
	emb := &ramMock{ram: 5000} // needs 7500 MB; only 2000 avail
	err := preCheckMemory(emb, nil)
	if err == nil {
		t.Fatal("insufficient memory should error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{"mem guard", "5000", "2000", "CKV_MEM_GUARD=off"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q: %s", want, msg)
		}
	}
}

func TestPreCheckMemory_ReadErrorFailsOpen(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "on")
	withProvider(t, staticProvider{err: errors.New("vm_stat blew up")})
	var buf bytes.Buffer
	emb := &ramMock{ram: 5000}
	if err := preCheckMemory(emb, &buf); err != nil {
		t.Fatalf("read failure should fail-open, got %v", err)
	}
	if !strings.Contains(buf.String(), "read failed") {
		t.Errorf("expected read-failed log, got %q", buf.String())
	}
}

// scriptedProvider lets a test drive the watchdog through a sequence
// of MemStats — one per Read() call, last value sticks.
type scriptedProvider struct {
	mu    sync.Mutex
	seq   []MemStat
	calls atomic.Int32
}

func (p *scriptedProvider) Read() (MemStat, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	idx := int(p.calls.Load())
	if idx >= len(p.seq) {
		idx = len(p.seq) - 1
	}
	p.calls.Add(1)
	return p.seq[idx], nil
}

func TestMemSignal_NilSafe(t *testing.T) {
	var s *memSignal
	if s.underPressure() {
		t.Fatal("nil memSignal should report no pressure")
	}
}

func TestStartMemWatchdog_GuardOffReturnsNil(t *testing.T) {
	withEnv(t, "CKV_MEM_GUARD", "off")
	withProvider(t, staticProvider{stat: MemStat{TotalMB: 16000, AvailableMB: 16000}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if sig := startMemWatchdog(ctx, nil); sig != nil {
		t.Errorf("guard=off should return nil sig, got %v", sig)
	}
}

func TestStartMemWatchdog_RaisesAndClears(t *testing.T) {
	// Drop poll to a ms so the test stays fast.
	savePoll := watchdogPollInterval
	watchdogPollInterval = 5 * time.Millisecond
	t.Cleanup(func() { watchdogPollInterval = savePoll })

	withEnv(t, "CKV_MEM_GUARD", "on")
	withEnv(t, "CKV_MEM_GUARD_LOW_MB", "1000")
	provider := &scriptedProvider{seq: []MemStat{
		{TotalMB: 16000, AvailableMB: 8000}, // healthy
		{TotalMB: 16000, AvailableMB: 500},  // pressure ON
		{TotalMB: 16000, AvailableMB: 500},  // pressure ON (sticky)
		{TotalMB: 16000, AvailableMB: 5000}, // pressure OFF
	}}
	withProvider(t, provider)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var buf bytes.Buffer
	sig := startMemWatchdog(ctx, &buf)
	if sig == nil {
		t.Fatal("expected non-nil sig with guard on")
	}

	// Wait long enough for watchdog to step through the script. With
	// 5 ms poll and 4 entries, 100 ms is comfortably enough.
	waitFor(t, 200*time.Millisecond, func() bool {
		return strings.Contains(buf.String(), "pressure OFF")
	}, "watchdog never logged pressure OFF after recovery")

	out := buf.String()
	if !strings.Contains(out, "pressure ON") {
		t.Errorf("expected 'pressure ON' log, got %q", out)
	}
	if !strings.Contains(out, "pressure OFF") {
		t.Errorf("expected 'pressure OFF' log, got %q", out)
	}
}

// waitFor polls cond until it's true or timeout fires. Test fails on
// timeout with msg.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal(msg)
}
