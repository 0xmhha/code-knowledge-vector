package golang

import (
	"testing"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

func TestParseExtractsFuncMethodAndType(t *testing.T) {
	src := []byte(`package x

// Greet returns a greeting.
func Greet(name string) string {
	return "hello, " + name
}

type Server struct {
	addr string
}

func (s *Server) Serve() error {
	return nil
}

type Handler interface {
	Handle()
}
`)
	p := New()
	spans, err := p.Parse("x.go", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	want := map[string]types.SymbolKind{
		"Greet":         types.KindFunction,
		"Server":        types.KindStruct,
		"Server.Serve":  types.KindMethod,
		"Handler":       types.KindInterface,
	}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d (%+v)", len(spans), len(want), spans)
	}
	for _, s := range spans {
		k, ok := want[s.Name]
		if !ok {
			t.Errorf("unexpected span: %s (%s)", s.Name, s.Kind)
			continue
		}
		if s.Kind != k {
			t.Errorf("%s: got kind %s, want %s", s.Name, s.Kind, k)
		}
		if s.StartLine == 0 || s.EndLine < s.StartLine {
			t.Errorf("%s: bad line range %d-%d", s.Name, s.StartLine, s.EndLine)
		}
		if s.Text == "" {
			t.Errorf("%s: empty Text", s.Name)
		}
	}
}

func TestParseHandlesGenericReceivers(t *testing.T) {
	src := []byte(`package x

type Box[T any] struct {
	v T
}

func (b *Box[T]) Get() T {
	return b.v
}
`)
	p := New()
	spans, err := p.Parse("box.go", src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var found bool
	for _, s := range spans {
		if s.Name == "Box.Get" && s.Kind == types.KindMethod {
			found = true
		}
	}
	if !found {
		t.Errorf("generic-receiver method not extracted as Box.Get; got %+v", spans)
	}
}

func TestParseSurfacesSyntaxError(t *testing.T) {
	src := []byte(`package x
func busted( {
`)
	_, err := New().Parse("busted.go", src)
	if err == nil {
		t.Fatal("expected parse error for malformed file")
	}
}
