// Package flowcorpus parses a curated flow corpus (corpus.jsonl) into CKV
// chunks. The corpus is the machine-loadable form of human-written flow docs:
// each record describes a step / flow / invariant in natural language (often
// Korean prose) tied to a precise file:line + symbol. Embedding that prose is
// the "human wording → exact code keyword" bridge — a Jira-style description
// ("수수료 위임이 어디서 검증되나?") retrieves the step that implements it.
//
// Record types (one JSON object per line, discriminated by "type"):
//
//	flow       → ChunkFlowSpine    (embed = summary)
//	step       → ChunkFlowStep     (embed = prose + symbol + branch conditions)
//	invariant  → ChunkInvariant    (embed = statement + assumes + check; curated)
//	edge       → skipped (graph-only; step.calls already carries the relation)
//
// Input contract: <go-stablenet>/.claude/docs/corpus/SCHEMA.md. Malformed or
// unknown records are counted and skipped with a warning (build_corpus.py is a
// best-effort markdown parser, so the caller should check the counts), never
// aborting the load.
package flowcorpus

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/pkg/types"
)

// Stats reports what Load produced, so the build can log counts and the caller
// can sanity-check against the source (e.g. expected flow/step/invariant totals).
type Stats struct {
	Flows      int
	Steps      int
	Invariants int
	Edges      int // counted but not chunked (graph-only)
	Skipped    int // malformed / unknown-type records
	Warnings   []string
}

// rawRecord is the union of all corpus record shapes; only the fields for the
// decoded "type" are populated. Decoding the whole line once and reading the
// relevant fields keeps the parser tolerant of additive schema fields.
type rawRecord struct {
	Type string `json:"type"`

	// flow
	ID         string   `json:"id"`
	EntryPoint string   `json:"entry_point"`
	Trigger    string   `json:"trigger"`
	Summary    string   `json:"summary"`
	RootSymbol string   `json:"root_symbol"`
	Links      []string `json:"links"`
	CalledBy   []string `json:"called_by"`

	// step
	Flow       string      `json:"flow"`
	Symbol     string      `json:"symbol"`
	File       string      `json:"file"`
	Line       int         `json:"line"`
	Kind       string      `json:"kind"`
	Calls      []string    `json:"calls"`
	Reads      string      `json:"reads"`
	Writes     string      `json:"writes"`
	Emits      string      `json:"emits"`
	Branches   []rawBranch `json:"branches"`
	Invariants []string    `json:"invariants"`
	Prose      string      `json:"prose"`

	// invariant
	Domain     string       `json:"domain"`
	Title      string       `json:"title"`
	Statement  string       `json:"statement"`
	Assumes    string       `json:"assumes"`
	Check      string       `json:"check"`
	EnforcedAt []rawEnforce `json:"enforced_at"`
}

type rawBranch struct {
	When string `json:"when"`
	Then string `json:"then"`
	At   string `json:"at"`
}

type rawEnforce struct {
	Flow string `json:"flow"`
	Step string `json:"step"`
	Loc  string `json:"loc"`
}

// Load reads and parses the corpus file at path. corpusRel is the citation
// path stamped on fileless chunks (flow / invariant records carry no code
// location); steps cite their own file:line. corpusRel should be the corpus
// file's path relative to a docs root registered with the build so query-time
// citation enforcement can resolve it.
func Load(path, corpusRel string) ([]types.Chunk, Stats, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, Stats{}, fmt.Errorf("flowcorpus: open %s: %w", path, err)
	}
	defer f.Close()
	return Parse(f, corpusRel)
}

// Parse converts a corpus JSONL stream into chunks. See Load for corpusRel.
func Parse(r io.Reader, corpusRel string) ([]types.Chunk, Stats, error) {
	var (
		chunks []types.Chunk
		st     Stats
	)
	sc := bufio.NewScanner(r)
	// Corpus prose lines can be long; raise the scanner's token cap.
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var rec rawRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			st.Skipped++
			st.Warnings = append(st.Warnings, fmt.Sprintf("line %d: bad JSON: %v", lineNo, err))
			continue
		}
		switch rec.Type {
		case "flow":
			c, ok := flowChunk(rec, corpusRel)
			if !ok {
				st.Skipped++
				st.Warnings = append(st.Warnings, fmt.Sprintf("line %d: flow missing id/summary", lineNo))
				continue
			}
			chunks = append(chunks, c)
			st.Flows++
		case "step":
			c, ok := stepChunk(rec)
			if !ok {
				st.Skipped++
				st.Warnings = append(st.Warnings, fmt.Sprintf("line %d: step %q missing id/file/line", lineNo, rec.ID))
				continue
			}
			chunks = append(chunks, c)
			st.Steps++
		case "invariant":
			c, ok := invariantChunk(rec, corpusRel)
			if !ok {
				st.Skipped++
				st.Warnings = append(st.Warnings, fmt.Sprintf("line %d: invariant missing id/statement", lineNo))
				continue
			}
			chunks = append(chunks, c)
			st.Invariants++
		case "edge":
			// Graph-only: step.calls / invariant.enforced_at already carry
			// the relation for CKV. Counted for parity, not chunked.
			st.Edges++
		default:
			st.Skipped++
			st.Warnings = append(st.Warnings, fmt.Sprintf("line %d: unknown type %q", lineNo, rec.Type))
		}
	}
	if err := sc.Err(); err != nil {
		return nil, st, fmt.Errorf("flowcorpus: scan: %w", err)
	}
	return chunks, st, nil
}

// flowChunk maps a "flow" record to a ChunkFlowSpine. Embed text = summary
// (the human one-liner describing what the entry point does).
func flowChunk(rec rawRecord, corpusRel string) (types.Chunk, bool) {
	if rec.ID == "" || rec.Summary == "" {
		return types.Chunk{}, false
	}
	text := rec.Summary
	sha := types.ContentSHA256(text)
	return types.Chunk{
		ID:            types.ChunkID("flow:"+rec.ID, 0, 0, sha),
		File:          corpusRel,
		Language:      "flow",
		SymbolName:    rec.RootSymbol,
		ChunkKind:     types.ChunkFlowSpine,
		ContentSHA256: sha,
		Category:      "domain",
		FlowSpine: &types.FlowSpineMeta{
			FlowID:     rec.ID,
			EntryPoint: rec.EntryPoint,
			Trigger:    rec.Trigger,
			RootSymbol: rec.RootSymbol,
			Links:      rec.Links,
			CalledBy:   rec.CalledBy,
		},
		Text: text,
	}, true
}

// stepChunk maps a "step" record to a ChunkFlowStep. Embed text = prose +
// symbol + each branch's "when" so a symptom phrased as a failure condition
// ("FeePayer 서명 불일치") still retrieves the step. Citation = file:line.
func stepChunk(rec rawRecord) (types.Chunk, bool) {
	if rec.ID == "" || rec.File == "" || rec.Line <= 0 {
		return types.Chunk{}, false
	}
	whens := make([]string, 0, len(rec.Branches))
	branches := make([]types.Branch, 0, len(rec.Branches))
	for _, b := range rec.Branches {
		if b.When != "" {
			whens = append(whens, b.When)
		}
		branches = append(branches, types.Branch{When: b.When, Then: b.Then, At: b.At})
	}
	var sb strings.Builder
	sb.WriteString(rec.Prose)
	if rec.Symbol != "" {
		sb.WriteString(" ")
		sb.WriteString(rec.Symbol)
	}
	if len(whens) > 0 {
		sb.WriteString(" ")
		sb.WriteString(strings.Join(whens, " "))
	}
	text := strings.TrimSpace(sb.String())
	sha := types.ContentSHA256(text)
	return types.Chunk{
		ID:            types.ChunkID(rec.File, rec.Line, rec.Line, sha),
		File:          rec.File,
		StartLine:     rec.Line,
		EndLine:       rec.Line,
		Language:      "flow",
		SymbolName:    rec.Symbol,
		ChunkKind:     types.ChunkFlowStep,
		ContentSHA256: sha,
		Category:      "domain",
		FlowStep: &types.FlowStepMeta{
			FlowID:     rec.Flow,
			StepID:     rec.ID,
			Symbol:     rec.Symbol,
			Kind:       rec.Kind,
			Calls:      rec.Calls,
			Reads:      rec.Reads,
			Writes:     rec.Writes,
			Emits:      rec.Emits,
			Branches:   branches,
			Invariants: rec.Invariants,
		},
		Text: text,
	}, true
}

// invariantChunk maps an "invariant" record to a curated ChunkInvariant.
// Embed text = title + statement + assumes + check. EnforcedAt records the
// (flow, step, loc) sites; Provenance="curated" distinguishes it from
// auto-extracted invariants.
func invariantChunk(rec rawRecord, corpusRel string) (types.Chunk, bool) {
	if rec.ID == "" || rec.Statement == "" {
		return types.Chunk{}, false
	}
	parts := make([]string, 0, 4)
	for _, s := range []string{rec.Title, rec.Statement, rec.Assumes, rec.Check} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	text := strings.Join(parts, " ")
	sha := types.ContentSHA256(text)
	enforced := make([]types.EnforcePoint, 0, len(rec.EnforcedAt))
	for _, e := range rec.EnforcedAt {
		enforced = append(enforced, types.EnforcePoint{Flow: e.Flow, Step: e.Step, Loc: e.Loc})
	}
	return types.Chunk{
		ID:            types.ChunkID("invariant:"+rec.ID, 0, 0, sha),
		File:          corpusRel,
		Language:      "flow",
		SymbolName:    rec.ID,
		ChunkKind:     types.ChunkInvariant,
		ContentSHA256: sha,
		Category:      "domain",
		Provenance:    "curated",
		EnforcedAt:    enforced,
		Text:          text,
	}, true
}
