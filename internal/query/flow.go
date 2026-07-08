package query

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Flow-aware retrieval (flow-ingest Phase D). These operate over the curated
// flow corpus (flow_step / flow_spine / curated-invariant chunks ingested via
// --flow-corpus) to let an agent trace a symptom to its cause: get_flow lays a
// flow out in call order, expand_flow walks neighbors, find_branches maps a
// symptom phrase to failure branches, and get_invariant_enforcement lists where
// an invariant is checked. All are bounded (single flow / single lookup); the
// corpus is small so the call-order model is built in-memory per request.

// FlowSelector picks a flow by exactly one of its keys.
type FlowSelector struct {
	FlowID      string
	EntryPoint  string
	InvariantID string // resolves to the first flow in the invariant's enforced_at
}

// FlowStepView is one step in a laid-out flow.
type FlowStepView struct {
	StepID     string         `json:"step_id"`
	Symbol     string         `json:"symbol,omitempty"`
	Citation   types.Citation `json:"citation"`
	Kind       string         `json:"kind,omitempty"`
	Calls      []string       `json:"calls,omitempty"`
	Reads      string         `json:"reads,omitempty"`
	Writes     string         `json:"writes,omitempty"`
	Emits      string         `json:"emits,omitempty"`
	Branches   []types.Branch `json:"branches,omitempty"`
	Invariants []string       `json:"invariants,omitempty"`
}

// FlowView is a flow's spine plus its steps in call (topological) order.
type FlowView struct {
	FlowID     string         `json:"flow_id"`
	EntryPoint string         `json:"entry_point,omitempty"`
	Trigger    string         `json:"trigger,omitempty"`
	RootSymbol string         `json:"root_symbol,omitempty"`
	Links      []string       `json:"links,omitempty"`
	CalledBy   []string       `json:"called_by,omitempty"`
	Steps      []FlowStepView `json:"steps"`
}

// FlowNeighbor is an adjacent step reached from an origin step.
type FlowNeighbor struct {
	StepID   string         `json:"step_id"`
	Symbol   string         `json:"symbol,omitempty"`
	Citation types.Citation `json:"citation"`
	Relation string         `json:"relation"` // "calls" (downstream) | "called_by" (upstream)
}

// ExpandResult is the neighborhood of one step plus that step's own branches
// (the failure conditions at the origin).
type ExpandResult struct {
	Origin         string         `json:"origin"`
	Direction      string         `json:"direction"`
	OriginBranches []types.Branch `json:"origin_branches,omitempty"`
	Neighbors      []FlowNeighbor `json:"neighbors"`
}

// BranchMatch is a failure branch surfaced for a symptom query.
type BranchMatch struct {
	When     string         `json:"when"`
	Then     string         `json:"then"`
	At       string         `json:"at"`
	StepID   string         `json:"step_id"`
	FlowID   string         `json:"flow_id"`
	Symbol   string         `json:"symbol,omitempty"`
	Citation types.Citation `json:"citation"`
	Score    float64        `json:"score"`
}

// InvariantEnforcement lists every place a curated invariant is enforced.
type InvariantEnforcement struct {
	InvID      string               `json:"inv_id"`
	Statement  string               `json:"statement,omitempty"`
	EnforcedAt []types.EnforcePoint `json:"enforced_at"`
}

// flowModel is the in-memory view assembled from the flow corpus chunks.
type flowModel struct {
	spineByFlow map[string]types.Chunk   // flow_id → flow_spine chunk
	stepsByFlow map[string][]types.Chunk // flow_id → flow_step chunks
	stepByID    map[string]types.Chunk   // step_id → flow_step chunk
	flowByEntry map[string]string        // entry_point → flow_id
	calledBy    map[string][]string      // step_id → step_ids that call it
}

func (e *Engine) loadFlowModel(ctx context.Context) (*flowModel, error) {
	chunks, err := e.store.FlowChunks(ctx)
	if err != nil {
		return nil, err
	}
	m := &flowModel{
		spineByFlow: map[string]types.Chunk{},
		stepsByFlow: map[string][]types.Chunk{},
		stepByID:    map[string]types.Chunk{},
		flowByEntry: map[string]string{},
		calledBy:    map[string][]string{},
	}
	for _, c := range chunks {
		switch {
		case c.FlowSpine != nil:
			m.spineByFlow[c.FlowSpine.FlowID] = c
			if c.FlowSpine.EntryPoint != "" {
				m.flowByEntry[c.FlowSpine.EntryPoint] = c.FlowSpine.FlowID
			}
		case c.FlowStep != nil:
			fid := c.FlowStep.FlowID
			m.stepsByFlow[fid] = append(m.stepsByFlow[fid], c)
			m.stepByID[c.FlowStep.StepID] = c
		}
	}
	// Reverse call edges for upstream traversal.
	for _, c := range m.stepByID {
		for _, callee := range c.FlowStep.Calls {
			m.calledBy[callee] = append(m.calledBy[callee], c.FlowStep.StepID)
		}
	}
	return m, nil
}

func stepView(c types.Chunk) FlowStepView {
	s := c.FlowStep
	return FlowStepView{
		StepID:     s.StepID,
		Symbol:     s.Symbol,
		Citation:   c.Citation(),
		Kind:       s.Kind,
		Calls:      s.Calls,
		Reads:      s.Reads,
		Writes:     s.Writes,
		Emits:      s.Emits,
		Branches:   s.Branches,
		Invariants: s.Invariants,
	}
}

// GetFlow lays out a flow's steps in call (topological) order. Selector must
// set exactly one of FlowID / EntryPoint / InvariantID.
func (e *Engine) GetFlow(ctx context.Context, sel FlowSelector) (*FlowView, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("get_flow: engine not ready")
	}
	m, err := e.loadFlowModel(ctx)
	if err != nil {
		return nil, err
	}
	flowID := sel.FlowID
	switch {
	case flowID != "":
	case sel.EntryPoint != "":
		flowID = m.flowByEntry[sel.EntryPoint]
	case sel.InvariantID != "":
		inv, ierr := e.store.CuratedInvariant(ctx, sel.InvariantID)
		if ierr != nil {
			return nil, ierr
		}
		if inv != nil && len(inv.EnforcedAt) > 0 {
			flowID = inv.EnforcedAt[0].Flow
		}
	default:
		return nil, fmt.Errorf("get_flow: one of flow_id/entry_point/invariant_id required")
	}
	if flowID == "" {
		return nil, fmt.Errorf("get_flow: no flow found for selector %q — %s", selectorText(sel), e.flowMissHint(ctx, m, selectorText(sel)))
	}
	steps := m.stepsByFlow[flowID]
	if len(steps) == 0 {
		return nil, fmt.Errorf("get_flow: flow %q has no steps", flowID)
	}
	ordered := topoSortSteps(steps)
	view := &FlowView{FlowID: flowID, Steps: make([]FlowStepView, 0, len(ordered))}
	if spine, ok := m.spineByFlow[flowID]; ok && spine.FlowSpine != nil {
		view.EntryPoint = spine.FlowSpine.EntryPoint
		view.Trigger = spine.FlowSpine.Trigger
		view.RootSymbol = spine.FlowSpine.RootSymbol
		view.Links = spine.FlowSpine.Links
		view.CalledBy = spine.FlowSpine.CalledBy
	}
	for _, c := range ordered {
		view.Steps = append(view.Steps, stepView(c))
	}
	return view, nil
}

// selectorText returns whichever selector key was provided, for diagnostics.
func selectorText(sel FlowSelector) string {
	switch {
	case sel.FlowID != "":
		return sel.FlowID
	case sel.EntryPoint != "":
		return sel.EntryPoint
	default:
		return sel.InvariantID
	}
}

// flowMissHint builds a recovery hint for a selector miss: the nearest flows
// by embedding similarity (best-effort), every known selector key (the corpus
// is small, so listing them is cheap), and a pointer to find_branches for
// natural-language lookup — so a caller that guessed a code symbol can
// self-correct instead of hitting a dead end.
func (e *Engine) flowMissHint(ctx context.Context, m *flowModel, text string) string {
	var b strings.Builder
	if near := e.nearestFlows(ctx, text, 3); len(near) > 0 {
		fmt.Fprintf(&b, "nearest flows by similarity: %s; ", strings.Join(near, ", "))
	}
	entries := make([]string, 0, len(m.flowByEntry))
	for ep := range m.flowByEntry {
		entries = append(entries, ep)
	}
	sort.Strings(entries)
	flows := make([]string, 0, len(m.spineByFlow))
	for id := range m.spineByFlow {
		flows = append(flows, id)
	}
	sort.Strings(flows)
	fmt.Fprintf(&b, "known entry_points: [%s]; known flow_ids: [%s]; ", strings.Join(entries, ", "), strings.Join(flows, ", "))
	b.WriteString("tip: for a natural-language or code-symbol query use find_branches(symptom_text) first, then get_flow(flow_id)")
	return b.String()
}

// nearestFlows searches the flow corpus by embedding similarity and returns
// up to k distinct flow ids in rank order, each tagged with its normalized
// similarity score ("spine-finalize(0.41)") — a low score tells the caller
// this is a same-neighborhood guess, not a match, so it can fall back to
// semantic_search/find_branches instead. Best-effort: empty on any error so
// a degraded embedder never masks the primary miss message.
func (e *Engine) nearestFlows(ctx context.Context, text string, k int) []string {
	if text == "" {
		return nil
	}
	resp, err := e.Search(ctx, text, Options{K: k * 4, Filter: types.Filter{Language: "flow"}, Threshold: -1})
	if err != nil || resp == nil {
		return nil
	}
	ids := make([]string, 0, len(resp.Hits))
	score := make(map[string]float64, len(resp.Hits))
	for _, h := range resp.Hits {
		ids = append(ids, h.ChunkID)
		score[h.ChunkID] = h.Score.Normalized
	}
	chunks, err := e.store.LookupByIDs(ctx, ids)
	if err != nil {
		return nil
	}
	byID := make(map[string]types.Chunk, len(chunks))
	for _, c := range chunks {
		byID[c.ID] = c
	}
	seen := map[string]bool{}
	var out []string
	for _, id := range ids { // preserve similarity rank
		c, ok := byID[id]
		if !ok {
			continue
		}
		var fid string
		switch {
		case c.FlowStep != nil:
			fid = c.FlowStep.FlowID
		case c.FlowSpine != nil:
			fid = c.FlowSpine.FlowID
		}
		if fid == "" || seen[fid] {
			continue
		}
		seen[fid] = true
		out = append(out, fmt.Sprintf("%s(%.2f)", fid, score[id]))
		if len(out) >= k {
			break
		}
	}
	return out
}

// topoSortSteps orders steps by their intra-flow `calls` edges (Kahn's
// algorithm). Steps in a cycle (or with unresolved edges) keep their original
// relative order, appended after the acyclic prefix — never dropped.
func topoSortSteps(steps []types.Chunk) []types.Chunk {
	idx := make(map[string]types.Chunk, len(steps))
	order := make([]string, 0, len(steps)) // stable original order
	indeg := make(map[string]int, len(steps))
	adj := make(map[string][]string, len(steps))
	for _, c := range steps {
		id := c.FlowStep.StepID
		idx[id] = c
		order = append(order, id)
		if _, ok := indeg[id]; !ok {
			indeg[id] = 0
		}
	}
	for _, c := range steps {
		from := c.FlowStep.StepID
		for _, to := range c.FlowStep.Calls {
			if _, ok := idx[to]; !ok {
				continue // edge leaves this flow (calls_flow); ignore for ordering
			}
			adj[from] = append(adj[from], to)
			indeg[to]++
		}
	}
	// Kahn, seeding zero-indegree in stable original order.
	var queue []string
	for _, id := range order {
		if indeg[id] == 0 {
			queue = append(queue, id)
		}
	}
	out := make([]types.Chunk, 0, len(steps))
	seen := map[string]bool{}
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, idx[id])
		for _, to := range adj[id] {
			indeg[to]--
			if indeg[to] == 0 {
				queue = append(queue, to)
			}
		}
	}
	// Append any cycle remnants in original order.
	for _, id := range order {
		if !seen[id] {
			out = append(out, idx[id])
		}
	}
	return out
}

// ExpandFlow returns the steps adjacent to stepID up to `hops` away, following
// downstream `calls` (direction "down") or upstream `called_by` ("up"), plus
// the origin step's own failure branches.
func (e *Engine) ExpandFlow(ctx context.Context, stepID, direction string, hops int) (*ExpandResult, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("expand_flow: engine not ready")
	}
	if direction != "up" && direction != "down" {
		return nil, fmt.Errorf("expand_flow: direction must be \"up\" or \"down\"")
	}
	if hops <= 0 {
		hops = 1
	}
	m, err := e.loadFlowModel(ctx)
	if err != nil {
		return nil, err
	}
	origin, ok := m.stepByID[stepID]
	if !ok {
		return nil, fmt.Errorf("expand_flow: step %q not found", stepID)
	}
	res := &ExpandResult{Origin: stepID, Direction: direction}
	if origin.FlowStep != nil {
		res.OriginBranches = origin.FlowStep.Branches
	}
	rel := "calls"
	if direction == "up" {
		rel = "called_by"
	}
	// BFS to `hops`.
	visited := map[string]bool{stepID: true}
	frontier := []string{stepID}
	for h := 0; h < hops && len(frontier) > 0; h++ {
		var next []string
		for _, cur := range frontier {
			var neigh []string
			if direction == "down" {
				if c, ok := m.stepByID[cur]; ok && c.FlowStep != nil {
					neigh = c.FlowStep.Calls
				}
			} else {
				neigh = m.calledBy[cur]
			}
			for _, nid := range neigh {
				if visited[nid] {
					continue
				}
				visited[nid] = true
				next = append(next, nid)
				if c, ok := m.stepByID[nid]; ok && c.FlowStep != nil {
					res.Neighbors = append(res.Neighbors, FlowNeighbor{
						StepID:   nid,
						Symbol:   c.FlowStep.Symbol,
						Citation: c.Citation(),
						Relation: rel,
					})
				}
			}
		}
		frontier = next
	}
	return res, nil
}

// FindBranches maps a symptom phrase to the failure branches of the flow steps
// most relevant to it (semantic search over flow chunks, whose embed text
// already includes each branch's "when"). Requires a real embedder.
func (e *Engine) FindBranches(ctx context.Context, symptom string, k int) ([]BranchMatch, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("find_branches: engine not ready")
	}
	if k <= 0 {
		k = DefaultK
	}
	resp, err := e.Search(ctx, symptom, Options{
		K:         k,
		Filter:    types.Filter{Language: "flow"},
		Threshold: -1,
	})
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Hits))
	score := map[string]float64{}
	for _, h := range resp.Hits {
		ids = append(ids, h.ChunkID)
		score[h.ChunkID] = h.Score.Normalized
	}
	chunks, err := e.store.LookupByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]types.Chunk, len(chunks))
	for _, c := range chunks {
		byID[c.ID] = c
	}
	var out []BranchMatch
	for _, id := range ids { // preserve hit rank
		c, ok := byID[id]
		if !ok || c.FlowStep == nil {
			continue
		}
		for _, b := range c.FlowStep.Branches {
			out = append(out, BranchMatch{
				When:     b.When,
				Then:     b.Then,
				At:       b.At,
				StepID:   c.FlowStep.StepID,
				FlowID:   c.FlowStep.FlowID,
				Symbol:   c.FlowStep.Symbol,
				Citation: c.Citation(),
				Score:    score[id],
			})
		}
	}
	return out, nil
}

// GetInvariantEnforcement lists every (flow, step, loc) where a curated
// invariant is enforced.
func (e *Engine) GetInvariantEnforcement(ctx context.Context, invID string) (*InvariantEnforcement, error) {
	if e == nil || e.store == nil {
		return nil, fmt.Errorf("get_invariant_enforcement: engine not ready")
	}
	inv, err := e.store.CuratedInvariant(ctx, invID)
	if err != nil {
		return nil, err
	}
	if inv == nil {
		return nil, fmt.Errorf("get_invariant_enforcement: invariant %q not found", invID)
	}
	return &InvariantEnforcement{
		InvID:      invID,
		Statement:  inv.Text,
		EnforcedAt: inv.EnforcedAt,
	}, nil
}
