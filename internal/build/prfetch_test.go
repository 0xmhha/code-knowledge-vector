package build

import "testing"

func TestParseGHRepo(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"git@github.com:owner/repo.git", "owner/repo"},
		{"git@github.com:org/my-project.git", "org/my-project"},
		{"https://github.com/owner/repo.git", "owner/repo"},
		{"https://github.com/owner/repo", "owner/repo"},
		{"http://github.com/owner/repo.git", "owner/repo"},
		{"owner/repo", "owner/repo"},
	}
	for _, tt := range tests {
		got := parseGHRepo(tt.input)
		if got != tt.want {
			t.Errorf("parseGHRepo(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
