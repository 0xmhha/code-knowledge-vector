package convention

import (
	"strings"
	"testing"
)

func TestObserveFile_ErrorPatterns(t *testing.T) {
	src := []byte(`package x

import "fmt"
import "errors"
import "github.com/pkg/errors"

func A() error { return errors.New("x") }
func B() error { return fmt.Errorf("plain: %v", nil) }
func C() error { return fmt.Errorf("wrap: %w", nil) }
func D() error { return errors.Wrap(nil, "legacy") }
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	if st.Errors["errors.New"] != 1 {
		t.Errorf("errors.New = %d, want 1", st.Errors["errors.New"])
	}
	if st.Errors["fmt.Errorf_plain"] != 1 {
		t.Errorf("fmt.Errorf_plain = %d, want 1", st.Errors["fmt.Errorf_plain"])
	}
	if st.Errors["fmt.Errorf_wrap"] != 1 {
		t.Errorf("fmt.Errorf_wrap = %d, want 1", st.Errors["fmt.Errorf_wrap"])
	}
	if st.Errors["pkg/errors.Wrap"] != 1 {
		t.Errorf("pkg/errors.Wrap = %d, want 1", st.Errors["pkg/errors.Wrap"])
	}
}

func TestObserveFile_LoggerDetection(t *testing.T) {
	src := []byte(`package x

import (
	"log"
	"log/slog"
)

func A() {
	log.Printf("a")
	slog.Info("b")
}
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	if st.Loggers["stdlib_log"] != 1 {
		t.Errorf("stdlib_log = %d, want 1", st.Loggers["stdlib_log"])
	}
	if st.Loggers["slog"] != 1 {
		t.Errorf("slog = %d, want 1", st.Loggers["slog"])
	}
}

func TestObserveFile_ConstructorsAndReceivers(t *testing.T) {
	src := []byte(`package x

type Server struct{}

func NewServer() *Server { return &Server{} }
func NewWithOptions() *Server { return nil }
func (s *Server) Run() {}
func (srv *Server) Stop() {}
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	if st.NewConstructors != 2 {
		t.Errorf("NewConstructors = %d, want 2", st.NewConstructors)
	}
	if st.Receivers["s"] != 1 || st.Receivers["srv"] != 1 {
		t.Errorf("Receivers = %v", st.Receivers)
	}
}

func TestObserveFile_Concurrency(t *testing.T) {
	src := []byte(`package x

import "sync"

type Server struct {
	mu sync.Mutex
}

func A() {
	_ = make(chan int)
	_ = make(chan string, 5)
}
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	if st.Mutexes == 0 {
		t.Errorf("expected ≥1 mutex, got 0")
	}
	if st.Channels != 2 {
		t.Errorf("Channels = %d, want 2", st.Channels)
	}
}

func TestObserveFile_TestifyAndTableDriven(t *testing.T) {
	src := []byte(`package x

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestX(t *testing.T) {
	cases := []struct{ name string }{{"a"}}
	for _, tc := range cases {
		require.Equal(t, "a", tc.name)
	}
}
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x_test.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	if st.TestifyUses == 0 {
		t.Errorf("expected testify use to be detected")
	}
	if st.TableDriven == 0 {
		t.Errorf("expected table-driven to be detected")
	}
	if st.TestFiles != 1 {
		t.Errorf("TestFiles = %d, want 1", st.TestFiles)
	}
}

func TestSummary_DeterministicOutput(t *testing.T) {
	src := []byte(`package x

import "fmt"

func A() error { return fmt.Errorf("x: %w", nil) }
func B() error { return fmt.Errorf("y: %w", nil) }
func C() error { return fmt.Errorf("z") }
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	s1 := st.Summary("foo")
	s2 := st.Summary("foo")
	if s1 != s2 {
		t.Errorf("Summary must be deterministic across calls")
	}
	if !strings.Contains(s1, "errors:") {
		t.Errorf("Summary missing errors section: %s", s1)
	}
	if !strings.Contains(s1, "fmt.Errorf_wrap=2") {
		t.Errorf("Summary should show fmt.Errorf_wrap=2: %s", s1)
	}
}

func TestToMap_RoundTrip(t *testing.T) {
	src := []byte(`package x

import "fmt"

func A() error { return fmt.Errorf("%w", nil) }
`)
	a := NewAggregator()
	if err := a.ObserveFile("foo/x.go", src); err != nil {
		t.Fatalf("ObserveFile: %v", err)
	}
	st := a.Result()["foo"]
	m := st.ToMap()
	if _, ok := m["errors"]; !ok {
		t.Errorf("ToMap should include errors map: %v", m)
	}
	if m["file_count"].(int) != 1 {
		t.Errorf("file_count = %v, want 1", m["file_count"])
	}
}
