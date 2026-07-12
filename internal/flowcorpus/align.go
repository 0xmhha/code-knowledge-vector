package flowcorpus

import "github.com/0xmhha/code-knowledge-vector/pkg/types"

// CodeSpan is a code chunk's line range and ID — the alignment target for a
// flow step. Built from the source chunks of one index (symbol / function-split
// kinds), keyed by file.
type CodeSpan struct {
	Start int
	End   int
	ID    string
}

// CodeIndex maps a repo-relative file path to its code chunks' line spans.
type CodeIndex map[string][]CodeSpan

// Add records a code chunk span under its file. Callers pass only real code
// chunks (a positive start line); header/whole-file chunks are poor alignment
// targets because they span the file and would shadow the containing symbol.
func (ix CodeIndex) Add(file string, start, end int, id string) {
	if file == "" || start <= 0 || id == "" {
		return
	}
	ix[file] = append(ix[file], CodeSpan{Start: start, End: end, ID: id})
}

// AlignSteps sets AlignedChunkID on every flow_step chunk to the code chunk
// whose [Start, End] range contains the step's line, and returns how many of
// the total flow steps resolved. When several code spans contain the line the
// tightest (smallest) one wins, so a step aligns to the innermost symbol rather
// than an enclosing block. A step whose line matches no span is left unaligned
// (the corpus drifted from the code) and simply not counted as resolved.
func AlignSteps(chunks []types.Chunk, code CodeIndex) (resolved, total int) {
	for i := range chunks {
		if chunks[i].ChunkKind != types.ChunkFlowStep || chunks[i].FlowStep == nil {
			continue
		}
		total++
		if id, ok := code.containing(chunks[i].File, chunks[i].StartLine); ok {
			chunks[i].FlowStep.AlignedChunkID = id
			resolved++
		}
	}
	return resolved, total
}

// containing returns the ID of the tightest code span in file that contains
// line, or ("", false) when none does.
func (ix CodeIndex) containing(file string, line int) (string, bool) {
	best := ""
	bestWidth := 0
	for _, s := range ix[file] {
		if line < s.Start || line > s.End {
			continue
		}
		w := s.End - s.Start
		if best == "" || w < bestWidth {
			best, bestWidth = s.ID, w
		}
	}
	return best, best != ""
}
