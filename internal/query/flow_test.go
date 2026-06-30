package query

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/build"
	"github.com/0xmhha/code-knowledge-vector/internal/embed/mock"
	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// buildFlowSample builds an index over testdata/sample plus a 3-step flow
// corpus (init-01 → init-02 → init-03) and one invariant, so the flow tools
// have call edges to traverse. Step citations point at real sample files so
// FindBranches' citation enforcement keeps them.
func buildFlowSample(t *testing.T) (out, src string) {
	t.Helper()
	srcAbs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "sample"))
	if err != nil {
		t.Fatal(err)
	}
	corpus := filepath.Join(t.TempDir(), "corpus.jsonl")
	lines := `{"type":"flow","id":"ep-x","entry_point":"EP-X","trigger":"x","summary":"3단계 흐름","root_symbol":"server.Main","links":[],"called_by":[]}
{"type":"step","id":"init-01","flow":"ep-x","symbol":"server.Main","file":"server.go","line":3,"kind":"go","calls":["init-02"],"reads":"cfg","writes":"state","emits":"-","branches":[{"when":"설정 파일 없음","then":"종료","at":"server.go:10"}],"invariants":["INV-X-01"],"prose":"서버를 초기화한다."}
{"type":"step","id":"init-02","flow":"ep-x","symbol":"cache.New","file":"cache.go","line":3,"kind":"go","calls":["init-03"],"reads":"-","writes":"cache","emits":"-","branches":[{"when":"캐시 용량 0","then":"기본값","at":"cache.go:12"}],"invariants":[],"prose":"캐시를 만든다."}
{"type":"step","id":"init-03","flow":"ep-x","symbol":"validator.Validate","file":"validator.go","line":3,"kind":"go","calls":[],"reads":"input","writes":"-","emits":"-","branches":[{"when":"서명 불일치","then":"거부","at":"validator.go:20"}],"invariants":[],"prose":"검증한다."}
{"type":"edge","rel":"calls","from":"ep-x:init-01","to":"ep-x:init-02"}
{"type":"invariant","id":"INV-X-01","domain":"X","title":"불변식","statement":"항상 참이어야 한다.","assumes":"전제","enforced_at":[{"flow":"ep-x","step":"init-01","loc":"server.go:5"},{"flow":"ep-x","step":"init-03","loc":"validator.go:20"}],"check":"검사"}
`
	if err := os.WriteFile(corpus, []byte(lines), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := t.TempDir()
	if _, err := build.Run(context.Background(), build.Options{
		SrcRoot:    srcAbs,
		OutDir:     outDir,
		Embedder:   mock.Default(),
		FlowCorpus: corpus,
		Now:        func() time.Time { return time.Unix(0, 0).UTC() },
	}); err != nil {
		t.Fatalf("build: %v", err)
	}
	return outDir, srcAbs
}

func openFlowEngine(t *testing.T) *Engine {
	t.Helper()
	out, _ := buildFlowSample(t)
	eng, err := Open(out, mock.Default())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { eng.Close() })
	return eng
}

func TestGetFlow_TopoOrderAndSpine(t *testing.T) {
	eng := openFlowEngine(t)
	flow, err := eng.GetFlow(context.Background(), FlowSelector{FlowID: "ep-x"})
	if err != nil {
		t.Fatalf("GetFlow: %v", err)
	}
	if flow.EntryPoint != "EP-X" || flow.RootSymbol != "server.Main" {
		t.Errorf("spine fields: entry=%q root=%q", flow.EntryPoint, flow.RootSymbol)
	}
	got := []string{}
	for _, s := range flow.Steps {
		got = append(got, s.StepID)
	}
	want := []string{"init-01", "init-02", "init-03"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("topo order = %v, want %v", got, want)
	}
	// step citation resolves to the real sample file.
	if flow.Steps[0].Citation.File != "server.go" {
		t.Errorf("step init-01 citation = %q, want server.go", flow.Steps[0].Citation.File)
	}
	if len(flow.Steps[0].Branches) != 1 || flow.Steps[0].Branches[0].At != "server.go:10" {
		t.Errorf("init-01 branches = %+v", flow.Steps[0].Branches)
	}
}

func TestGetFlow_ByEntryPointAndInvariant(t *testing.T) {
	eng := openFlowEngine(t)
	byEP, err := eng.GetFlow(context.Background(), FlowSelector{EntryPoint: "EP-X"})
	if err != nil || byEP.FlowID != "ep-x" {
		t.Fatalf("by entry_point: flow=%v err=%v", byEP, err)
	}
	byInv, err := eng.GetFlow(context.Background(), FlowSelector{InvariantID: "INV-X-01"})
	if err != nil || byInv.FlowID != "ep-x" {
		t.Fatalf("by invariant_id: flow=%v err=%v", byInv, err)
	}
}

func TestExpandFlow_DownAndUp(t *testing.T) {
	eng := openFlowEngine(t)
	down, err := eng.ExpandFlow(context.Background(), "init-01", "down", 2)
	if err != nil {
		t.Fatalf("ExpandFlow down: %v", err)
	}
	seen := map[string]bool{}
	for _, n := range down.Neighbors {
		seen[n.StepID] = true
		if n.Relation != "calls" {
			t.Errorf("down neighbor %s relation=%q, want calls", n.StepID, n.Relation)
		}
	}
	if !seen["init-02"] || !seen["init-03"] {
		t.Errorf("down 2 hops from init-01 should reach init-02+init-03, got %v", seen)
	}
	if len(down.OriginBranches) != 1 {
		t.Errorf("origin branches = %+v", down.OriginBranches)
	}

	up, err := eng.ExpandFlow(context.Background(), "init-03", "up", 2)
	if err != nil {
		t.Fatalf("ExpandFlow up: %v", err)
	}
	seen = map[string]bool{}
	for _, n := range up.Neighbors {
		seen[n.StepID] = true
		if n.Relation != "called_by" {
			t.Errorf("up neighbor %s relation=%q, want called_by", n.StepID, n.Relation)
		}
	}
	if !seen["init-02"] || !seen["init-01"] {
		t.Errorf("up 2 hops from init-03 should reach init-02+init-01, got %v", seen)
	}
}

func TestGetInvariantEnforcement(t *testing.T) {
	eng := openFlowEngine(t)
	enf, err := eng.GetInvariantEnforcement(context.Background(), "INV-X-01")
	if err != nil {
		t.Fatalf("GetInvariantEnforcement: %v", err)
	}
	if len(enf.EnforcedAt) != 2 || enf.EnforcedAt[0].Loc != "server.go:5" {
		t.Errorf("enforced_at = %+v", enf.EnforcedAt)
	}
	if _, err := eng.GetInvariantEnforcement(context.Background(), "NOPE"); err == nil {
		t.Error("expected error for unknown invariant")
	}
}

func TestFindBranches_ReturnsBranches(t *testing.T) {
	eng := openFlowEngine(t)
	// mock embedder ranking is not semantic, but the flow steps are the only
	// Language="flow" chunks, so they're retrieved and their branches surface.
	matches, err := eng.FindBranches(context.Background(), "서명 불일치로 거부됨", 10)
	if err != nil {
		t.Fatalf("FindBranches: %v", err)
	}
	if len(matches) == 0 {
		t.Fatal("expected at least one branch match")
	}
	found := false
	for _, m := range matches {
		if m.At == "validator.go:20" && m.StepID == "init-03" {
			found = true
		}
		if m.Citation.File == "" {
			t.Errorf("branch match missing citation: %+v", m)
		}
	}
	if !found {
		t.Errorf("expected the 'validator.go:20' branch among matches, got %d", len(matches))
	}
}

func TestTopoSortSteps_CycleSafe(t *testing.T) {
	mk := func(id string, calls ...string) types.Chunk {
		return types.Chunk{FlowStep: &types.FlowStepMeta{StepID: id, Calls: calls}}
	}
	// a→b→c→a is a cycle; d is acyclic. None may be dropped.
	steps := []types.Chunk{mk("a", "b"), mk("b", "c"), mk("c", "a"), mk("d")}
	out := topoSortSteps(steps)
	if len(out) != 4 {
		t.Fatalf("topo dropped nodes: got %d of 4", len(out))
	}
	seen := map[string]bool{}
	for _, c := range out {
		seen[c.FlowStep.StepID] = true
	}
	for _, id := range []string{"a", "b", "c", "d"} {
		if !seen[id] {
			t.Errorf("missing %q", id)
		}
	}
}
