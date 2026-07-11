package query

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/0xmhha/code-knowledge-vector/internal/ckgalign"
)

// CKG↔CKV alignment detection (reindex-migration design §3.1). Compares the
// CKG coordinates this index aligned against (manifest sources.ckg) with the
// CKG graph currently on disk at that path, so a stale/mismatched alignment is
// surfaced instead of returning wrong canonical_id joins silently.

// AlignmentStatus classifies the CKG↔CKV alignment.
type AlignmentStatus string

const (
	AlignmentOK         AlignmentStatus = "ok"          // commit + digest match
	AlignmentDegraded   AlignmentStatus = "degraded"    // commit matches, digest unverifiable / schema<1.19
	AlignmentMismatch   AlignmentStatus = "mismatch"    // commit or digest differ (join broken) — re-align needed
	AlignmentNotAligned AlignmentStatus = "not_aligned" // index built without --ckg
)

// AlignmentReport is the structured result of CheckAlignment. The authoritative
// keys are src_commit + graph_digest (design §3.1); the src_root path is NOT
// compared here (a different checkout of the same commit is legal — that
// concern belongs to the serving layer, as a warning).
type AlignmentReport struct {
	Status         AlignmentStatus `json:"status"`
	RecordedCommit string          `json:"recorded_commit,omitempty"`
	CurrentCommit  string          `json:"current_commit,omitempty"`
	RecordedDigest string          `json:"recorded_digest,omitempty"`
	CurrentDigest  string          `json:"current_digest,omitempty"`
	SchemaVersion  string          `json:"schema_version,omitempty"`
	CKGPath        string          `json:"ckg_path,omitempty"`
	Reason         string          `json:"reason,omitempty"`
	Warnings       []string        `json:"warnings,omitempty"`
}

// Serviceable reports whether the alignment is trustworthy enough to serve
// canonical_id joins: ok / degraded / not_aligned are serviceable; only
// mismatch (commit or digest divergence) is not.
func (r AlignmentReport) Serviceable() bool { return r.Status != AlignmentMismatch }

// CheckAlignment compares the recorded sources.ckg coordinates against the CKG
// graph currently at the recorded path. Returns not_aligned when the index was
// built without --ckg. Best-effort: an unreadable graph is a mismatch (the
// aligned-against graph is gone).
func (e *Engine) CheckAlignment() AlignmentReport {
	if e == nil || e.man == nil || e.man.Sources == nil || e.man.Sources.CKG == nil {
		return AlignmentReport{Status: AlignmentNotAligned}
	}
	rec := e.man.Sources.CKG
	rep := AlignmentReport{
		RecordedCommit: rec.SrcCommit,
		RecordedDigest: rec.GraphDigest,
		SchemaVersion:  rec.SchemaVersion,
		CKGPath:        rec.Path,
	}
	if rec.Path == "" {
		rep.Status = AlignmentDegraded
		rep.Reason = "ckg path not recorded; cannot re-read current graph"
		return rep
	}
	cur, err := ckgalign.ReadCoords(rec.Path)
	if err != nil {
		rep.Status = AlignmentMismatch
		rep.Reason = fmt.Sprintf("ckg graph unreadable at %s: %v", rec.Path, err)
		return rep
	}
	rep.CurrentCommit = cur.SrcCommit
	rep.CurrentDigest = cur.GraphDigest

	// commit divergence → canonical_id join potentially broken.
	if rec.SrcCommit != "" && cur.SrcCommit != "" && rec.SrcCommit != cur.SrcCommit {
		rep.Status = AlignmentMismatch
		rep.Reason = fmt.Sprintf("ckg src_commit changed: aligned %s, now %s",
			shortSHA(rec.SrcCommit), shortSHA(cur.SrcCommit))
		return rep
	}
	// digest divergence at the same commit → graph rebuilt differently.
	if rec.GraphDigest != "" && cur.GraphDigest != "" && rec.GraphDigest != cur.GraphDigest {
		rep.Status = AlignmentMismatch
		rep.Reason = "ckg graph_digest changed (graph rebuilt) — re-alignment required"
		return rep
	}
	// non-blocking degradations.
	if rec.GraphDigest == "" || cur.GraphDigest == "" {
		rep.Warnings = append(rep.Warnings, "graph_digest unavailable — commit-only verification")
	}
	if maj, min, ok := parseMajorMinorQ(rep.SchemaVersion); ok && !(maj > 1 || (maj == 1 && min >= 19)) {
		rep.Warnings = append(rep.Warnings, "ckg schema_version < 1.19 — canonical_id not populated")
	}
	if len(rep.Warnings) > 0 {
		rep.Status = AlignmentDegraded
	} else {
		rep.Status = AlignmentOK
	}
	return rep
}

func shortSHA(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}

// parseMajorMinorQ parses "MAJOR" or "MAJOR.MINOR" into ints (query-side copy;
// ckgalign has an unexported equivalent). ok=false on malformed input.
func parseMajorMinorQ(s string) (major, minor int, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, false
	}
	parts := strings.SplitN(s, ".", 3)
	maj, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil {
		return 0, 0, false
	}
	if len(parts) > 1 {
		if minor, err = strconv.Atoi(strings.TrimSpace(parts[1])); err != nil {
			return 0, 0, false
		}
	}
	return maj, minor, true
}
