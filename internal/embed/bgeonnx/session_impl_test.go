//go:build bgeonnx

// Tests for the provider-selection logic in session_impl.go. These
// only need the bgeonnx build tag (not bgeonnx_smoke) because they
// exercise pure-Go decision functions, not real ONNX sessions.

package bgeonnx

import (
	"io"
	"testing"

	ort "github.com/yalue/onnxruntime_go"
)

func TestCoreMLDisabled_RecognizesTruthyTokens(t *testing.T) {
	// Lock in the env-var taxonomy: anything strconv.ParseBool would
	// accept as true must disable CoreML. Anything else (including
	// empty, "0", "false", or accidental typos) keeps it enabled.
	cases := []struct {
		env  string
		want bool
	}{
		{"", false},
		{"0", false},
		{"false", false},
		{"FALSE", false},
		{"no", false},
		{"off", false},
		{"unrelated", false},
		{"1", true},
		{"t", true},
		{"true", true},
		{"TRUE", true},
		{"y", true},
		{"yes", true},
		{"YES", true},
		{"on", true},
		{"ON", true},
		{"  true  ", true}, // surrounding whitespace tolerated
	}
	for _, c := range cases {
		t.Run(c.env, func(t *testing.T) {
			t.Setenv("CKV_DISABLE_COREML", c.env)
			if got := coreMLDisabled(); got != c.want {
				t.Errorf("coreMLDisabled() = %v with env %q, want %v", got, c.env, c.want)
			}
		})
	}
}

func TestChooseProvider_NonDarwinAlwaysCPU(t *testing.T) {
	// On Linux/Windows the CoreML EP is unavailable; chooseProvider
	// must short-circuit so we never call the (would-panic) attacher.
	// Passing nil opts/writer also asserts the no-op closure ignores
	// both parameters — important so callers don't need to construct
	// real ORT objects on non-Apple hosts.
	for _, goos := range []string{"linux", "windows", "freebsd"} {
		t.Run(goos, func(t *testing.T) {
			fn := chooseProvider(goos, false)
			if got := fn(nil, nil); got != "cpu" {
				t.Errorf("non-darwin %q: got %q, want cpu", goos, got)
			}
		})
	}
}

func TestChooseProvider_DarwinDisabledIsCPU(t *testing.T) {
	// macOS but user set CKV_DISABLE_COREML=1: same outcome as Linux.
	// Locks in the escape-hatch contract documented in the env-var
	// comment of coreMLDisabled.
	fn := chooseProvider("darwin", true)
	if got := fn(nil, nil); got != "cpu" {
		t.Errorf("darwin+disabled: got %q, want cpu", got)
	}
}

func TestChooseProvider_DarwinEnabledReturnsAttacher(t *testing.T) {
	// We can't safely call the returned attacher without a real ORT
	// session option (nil deref). But we can prove it's *not* the
	// no-op closure by exercising the negative path: chooseProvider
	// must return a function distinct from the no-op variant. The
	// cheapest signal is "construct a real SessionOptions and let
	// AppendExecutionProviderCoreMLV2 either succeed or error; either
	// way the returned tag must NOT be 'cpu'."
	if err := initORT(); err != nil {
		t.Skipf("ORT not available: %v", err)
	}
	opts, err := ort.NewSessionOptions()
	if err != nil {
		t.Skipf("NewSessionOptions: %v", err)
	}
	defer opts.Destroy()

	fn := chooseProvider("darwin", false)
	got := fn(opts, io.Discard)
	if got == "cpu" {
		t.Errorf("darwin+enabled returned 'cpu'; expected 'coreml' or 'coreml-fallback-to-cpu'")
	}
	if got != "coreml" && got != "coreml-fallback-to-cpu" {
		t.Errorf("unexpected provider tag %q", got)
	}
}
