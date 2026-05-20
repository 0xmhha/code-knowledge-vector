//go:build !darwin && !linux

// Unsupported platforms leave defaultMemProvider nil, which makes both
// the pre-check and the runtime watchdog fail-open (no-op).

package build
