package build

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/ckgalign"
	"github.com/0xmhha/code-knowledge-vector/internal/manifest"
	"github.com/0xmhha/code-knowledge-vector/internal/parse/prdoc"
)

// prCutoff derives the PR-corpus cutoff (newest PR indexed) from the fetched
// metadata, so the manifest ledger records "PRs known up to #N / date" and a
// future incremental ingest can fetch only PRs after it.
func prCutoff(metas []prdoc.PRMeta) *manifest.PRSource {
	if len(metas) == 0 {
		return nil
	}
	src := &manifest.PRSource{Repo: metas[0].Repo}
	var latest time.Time
	for _, m := range metas {
		if m.PRNumber > src.LastPRNumber {
			src.LastPRNumber = m.PRNumber
		}
		if m.MergedAt.After(latest) {
			latest = m.MergedAt
		}
	}
	if !latest.IsZero() {
		src.LastMergedAt = latest.UTC().Format(time.RFC3339)
	}
	return src
}

// buildSourcesLedger assembles the per-layer knowledge-cutoff ledger
// (reindex-migration design §2.2). Best-effort: a hash/read failure yields an
// empty field rather than failing the build — the ledger is metadata, not a
// gate. Each sub-block is nil when that layer was not built.
func buildSourcesLedger(o Options, commit, builtAt string, prSource *manifest.PRSource) *manifest.Sources {
	s := &manifest.Sources{
		Code: &manifest.CodeSource{IndexedHead: commit, BuiltAt: builtAt},
		PRs:  prSource,
	}
	if o.CKGPath != "" {
		// Record the CKG coordinates this index aligned against — the anchor
		// for CKG↔CKV mismatch detection. graph_digest stays empty until CKG
		// publishes it; src_commit + schema_version are recorded now.
		if c, err := ckgalign.ReadCoords(o.CKGPath); err == nil {
			s.CKG = &manifest.CKGSource{
				GraphDigest:   c.GraphDigest,
				SrcCommit:     c.SrcCommit,
				SchemaVersion: c.SchemaVersion,
				Path:          absOrEmpty(o.CKGPath),
			}
		}
	}
	if o.FlowCorpus != "" {
		s.Flow = &manifest.HashedSource{Path: absOrEmpty(o.FlowCorpus), ContentHash: contentHash(o.FlowCorpus)}
	}
	if o.PolicyPath != "" {
		if _, err := os.Stat(o.PolicyPath); err == nil {
			s.Policy = &manifest.HashedSource{Path: absOrEmpty(o.PolicyPath), ContentHash: contentHash(o.PolicyPath)}
		}
	}
	if len(o.DocsRoots) > 0 {
		s.Docs = &manifest.HashedSource{Path: absRoots(o.DocsRoots)[0], ContentHash: docsRootsHash(o.DocsRoots)}
	}
	return s
}

// contentHash returns the sha256 of a file's bytes, or a tree hash for a
// directory (sha256 over sorted "relpath\x00filesha\n"). Empty on error.
func contentHash(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return ""
	}
	if !info.IsDir() {
		b, err := os.ReadFile(path)
		if err != nil {
			return ""
		}
		sum := sha256.Sum256(b)
		return hex.EncodeToString(sum[:])
	}
	return treeHash(path)
}

// treeHash hashes a directory's content: for every regular file, "relpath\x00
// filesha\n", sorted by relpath, then sha256 of the concatenation. Detects any
// add/remove/edit under the tree.
func treeHash(root string) string {
	type ent struct{ rel, sha string }
	var ents []ent
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, rerr := os.ReadFile(p)
		if rerr != nil {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			rel = p
		}
		fsha := sha256.Sum256(b)
		ents = append(ents, ent{rel: filepath.ToSlash(rel), sha: hex.EncodeToString(fsha[:])})
		return nil
	})
	sort.Slice(ents, func(i, j int) bool { return ents[i].rel < ents[j].rel })
	h := sha256.New()
	for _, e := range ents {
		h.Write([]byte(e.rel))
		h.Write([]byte{0})
		h.Write([]byte(e.sha))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// docsRootsFromManifest recovers the curated `--docs` roots a reindex must
// re-walk. manifest.DocsRoots records every citation-resolving root, which
// includes the flow-corpus directory (appended at build time for fileless flow
// citations); that directory is NOT a docs root, so it is removed here to match
// the set the build hashed into Sources.Docs.ContentHash.
func docsRootsFromManifest(man *manifest.Manifest) []string {
	roots := append([]string{}, man.DocsRoots...)
	if man.Sources != nil && man.Sources.Flow != nil && man.Sources.Flow.Path != "" {
		flowDir := filepath.Dir(man.Sources.Flow.Path)
		for i, r := range roots {
			if r == flowDir {
				roots = append(roots[:i], roots[i+1:]...)
				break
			}
		}
	}
	return roots
}

// docsRootsHash combines the tree hashes of every docs root (order-independent)
// into one content hash for the docs layer.
func docsRootsHash(roots []string) string {
	hs := make([]string, 0, len(roots))
	for _, r := range roots {
		hs = append(hs, treeHash(r))
	}
	sort.Strings(hs)
	h := sha256.New()
	for _, x := range hs {
		h.Write([]byte(x))
		h.Write([]byte{'\n'})
	}
	return hex.EncodeToString(h.Sum(nil))
}
