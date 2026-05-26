package eval

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/query"
)

// RecordOptions controls one interactive record session.
type RecordOptions struct {
	K                int
	Threshold        float64
	SrcRoot          string
	EnableBM25Rerank bool
}

// RecordSession runs an interactive loop: the user types a query intent,
// sees top-K results, selects which are correct, and the entry is appended
// to the fixture file at fixturePath. The loop continues until the user
// sends an empty line or EOF.
//
// in/out are separated from os.Stdin/os.Stdout for testability.
func RecordSession(ctx context.Context, eng *query.Engine, fixturePath string, opts RecordOptions, in io.Reader, out io.Writer) error {
	if eng == nil {
		return fmt.Errorf("record: nil engine")
	}
	k := opts.K
	if k <= 0 {
		k = DefaultK
	}

	nextID, err := nextQueryID(fixturePath)
	if err != nil {
		return fmt.Errorf("record: %w", err)
	}

	scanner := bufio.NewScanner(in)
	for {
		fmt.Fprintf(out, "\nEnter query intent (empty to quit): ")
		if !scanner.Scan() {
			break
		}
		intent := strings.TrimSpace(scanner.Text())
		if intent == "" {
			break
		}

		resp, err := eng.Search(ctx, intent, query.Options{
			K:                k,
			Threshold:        opts.Threshold,
			SrcRoot:          opts.SrcRoot,
			EnableBM25Rerank: opts.EnableBM25Rerank,
		})
		if err != nil {
			fmt.Fprintf(out, "  search error: %v\n", err)
			continue
		}
		if len(resp.Hits) == 0 {
			fmt.Fprintf(out, "  no results found.\n")
			continue
		}

		fmt.Fprintf(out, "\nTop-%d results:\n", len(resp.Hits))
		for i, h := range resp.Hits {
			snippet := firstNonEmptyLine(h.Snippet)
			fmt.Fprintf(out, "  [%d] %s:%d-%d  %s %s  (score=%.3f)\n",
				i+1, h.Citation.File, h.Citation.StartLine, h.Citation.EndLine,
				h.SymbolKind, h.Symbol,
				h.Score.Normalized)
			if snippet != "" {
				fmt.Fprintf(out, "      %s\n", snippet)
			}
		}

		fmt.Fprintf(out, "\nCorrect result? (1-%d, or 'none' to skip): ", len(resp.Hits))
		if !scanner.Scan() {
			break
		}
		sel := strings.TrimSpace(scanner.Text())
		if sel == "" || strings.EqualFold(sel, "none") {
			fmt.Fprintf(out, "  skipped.\n")
			continue
		}

		idx, err := strconv.Atoi(sel)
		if err != nil || idx < 1 || idx > len(resp.Hits) {
			fmt.Fprintf(out, "  invalid selection %q, skipping.\n", sel)
			continue
		}
		hit := resp.Hits[idx-1]
		qID := fmt.Sprintf("q%d", nextID)
		nextID++

		entry := formatEntry(qID, intent, hit)
		if err := appendToFixture(fixturePath, entry); err != nil {
			return fmt.Errorf("record: append fixture: %w", err)
		}
		fmt.Fprintf(out, "  recorded as %s → %s:%d-%d\n",
			qID, hit.Citation.File, hit.Citation.StartLine, hit.Citation.EndLine)
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("record: stdin: %w", err)
	}
	return nil
}

// nextQueryID parses the fixture to find the highest numeric query ID
// and returns max+1. If the fixture doesn't exist, returns 1.
func nextQueryID(path string) (int, error) {
	fx, err := LoadFixture(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 1, nil
		}
		return 0, err
	}
	maxN := 0
	for _, q := range fx.Queries {
		if strings.HasPrefix(q.ID, "q") {
			n, err := strconv.Atoi(q.ID[1:])
			if err == nil && n > maxN {
				maxN = n
			}
		}
	}
	return maxN + 1, nil
}

func formatEntry(id, intent string, hit query.Hit) string {
	var b strings.Builder
	b.WriteString("\n")
	fmt.Fprintf(&b, "  - id: %s\n", id)
	fmt.Fprintf(&b, "    intent: %q\n", intent)
	b.WriteString("    expected:\n")
	fmt.Fprintf(&b, "      file: %s\n", hit.Citation.File)
	if hit.Symbol != "" {
		fmt.Fprintf(&b, "      symbol: %s\n", hit.Symbol)
	}
	if hit.SymbolKind != "" {
		fmt.Fprintf(&b, "      kind: %s\n", string(hit.SymbolKind))
	}
	fmt.Fprintf(&b, "      line_range: [%d, %d]\n", hit.Citation.StartLine, hit.Citation.EndLine)
	fmt.Fprintf(&b, "    recorded_via: interactive\n")
	fmt.Fprintf(&b, "    timestamp: %q\n", time.Now().UTC().Format(time.RFC3339))
	return b.String()
}

func appendToFixture(path string, entry string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(entry)
	return err
}

func firstNonEmptyLine(text string) string {
	for _, line := range strings.SplitN(text, "\n", 10) {
		s := strings.TrimSpace(line)
		if s != "" {
			if len(s) > 100 {
				return s[:100] + "..."
			}
			return s
		}
	}
	return ""
}
