package build

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/0xmhha/code-knowledge-vector/internal/parse/prdoc"
)

// PRFetchOptions controls which PRs to fetch for corpus indexing.
type PRFetchOptions struct {
	Repo  string    // "owner/repo"; inferred from git remote if empty
	Since time.Time // only PRs merged after this date
	Limit int       // max PRs to fetch; 0 → 100
}

// FetchMergedPRs calls `gh pr list` to get merged PRs, then `gh pr view`
// for each to get body + commits. Returns parsed PRMeta ready for
// prdoc.Parse. Requires `gh` CLI authenticated.
func FetchMergedPRs(ctx context.Context, srcRoot string, opts PRFetchOptions) ([]prdoc.PRMeta, error) {
	if err := requireGH(ctx); err != nil {
		return nil, err
	}

	repo := opts.Repo
	if repo == "" {
		var err error
		repo, err = detectGHRepo(srcRoot)
		if err != nil {
			return nil, fmt.Errorf("cannot detect GitHub repo from %s: %w (use --pr-repo to set explicitly)", srcRoot, err)
		}
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 100
	}

	prs, err := listMergedPRs(ctx, repo, opts.Since, limit)
	if err != nil {
		return nil, err
	}

	metas := make([]prdoc.PRMeta, 0, len(prs))
	for _, pr := range prs {
		meta, err := fetchPRDetail(ctx, repo, pr)
		if err != nil {
			continue
		}
		metas = append(metas, meta)
	}
	return metas, nil
}

type prListEntry struct {
	Number   int       `json:"number"`
	Title    string    `json:"title"`
	MergedAt time.Time `json:"mergedAt"`
}

func listMergedPRs(ctx context.Context, repo string, since time.Time, limit int) ([]prListEntry, error) {
	args := []string{
		"pr", "list",
		"--repo", repo,
		"--state", "merged",
		"--json", "number,title,mergedAt",
		"--limit", fmt.Sprintf("%d", limit),
	}
	if !since.IsZero() {
		args = append(args, "--search", fmt.Sprintf("merged:>=%s", since.Format("2006-01-02")))
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var entries []prListEntry
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("parse gh pr list: %w", err)
	}
	return entries, nil
}

func fetchPRDetail(ctx context.Context, repo string, entry prListEntry) (prdoc.PRMeta, error) {
	args := []string{
		"pr", "view", fmt.Sprintf("%d", entry.Number),
		"--repo", repo,
		"--json", "title,body,commits",
	}
	cmd := exec.CommandContext(ctx, "gh", args...)
	out, err := cmd.Output()
	if err != nil {
		return prdoc.PRMeta{}, fmt.Errorf("gh pr view %d: %w", entry.Number, err)
	}
	var raw struct {
		Title   string `json:"title"`
		Body    string `json:"body"`
		Commits []struct {
			MessageHeadline string `json:"messageHeadline"`
			MessageBody     string `json:"messageBody"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return prdoc.PRMeta{}, fmt.Errorf("parse gh pr view %d: %w", entry.Number, err)
	}

	commits := make([]string, 0, len(raw.Commits))
	for _, c := range raw.Commits {
		head := strings.TrimSpace(c.MessageHeadline)
		body := strings.TrimSpace(c.MessageBody)
		switch {
		case head != "" && body != "":
			commits = append(commits, head+"\n\n"+body)
		case head != "":
			commits = append(commits, head)
		case body != "":
			commits = append(commits, body)
		}
	}

	return prdoc.PRMeta{
		Repo:           repo,
		PRNumber:       entry.Number,
		Title:          raw.Title,
		Body:           raw.Body,
		CommitMessages: commits,
		MergedAt:       entry.MergedAt,
	}, nil
}

func requireGH(ctx context.Context) error {
	if _, err := exec.LookPath("gh"); err != nil {
		return fmt.Errorf("gh CLI not found — install with: brew install gh")
	}
	cmd := exec.CommandContext(ctx, "gh", "auth", "status")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("gh CLI not authenticated — run: gh auth login")
	}
	return nil
}

func detectGHRepo(srcRoot string) (string, error) {
	cmd := exec.Command("git", "-C", srcRoot, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return parseGHRepo(strings.TrimSpace(string(out))), nil
}

func parseGHRepo(remoteURL string) string {
	// Handle SSH: git@github.com:owner/repo.git
	if strings.HasPrefix(remoteURL, "git@") {
		parts := strings.SplitN(remoteURL, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSuffix(parts[1], ".git")
		}
	}
	// Handle HTTPS: https://github.com/owner/repo.git
	remoteURL = strings.TrimSuffix(remoteURL, ".git")
	for _, prefix := range []string{"https://github.com/", "http://github.com/"} {
		if strings.HasPrefix(remoteURL, prefix) {
			return remoteURL[len(prefix):]
		}
	}
	return remoteURL
}
