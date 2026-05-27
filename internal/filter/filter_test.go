package filter

import "testing"

func TestScan_Clean(t *testing.T) {
	f := New(Config{
		Patterns: []Pattern{
			{ID: "aws_key", Regex: "AKIA[0-9A-Z]{16}", Severity: "critical", Action: "redact"},
		},
	})
	r := f.Scan("normal code without secrets")
	if r.Status != StatusClean {
		t.Errorf("status = %q, want CLEAN", r.Status)
	}
	if r.FilteredText != "normal code without secrets" {
		t.Errorf("text changed: %q", r.FilteredText)
	}
}

func TestScan_Redact(t *testing.T) {
	f := New(Config{
		Patterns: []Pattern{
			{ID: "aws_key", Regex: "AKIA[0-9A-Z]{16}", Severity: "critical", Action: "redact"},
		},
	})
	input := "key = AKIAIOSFODNN7EXAMPLE"
	r := f.Scan(input)
	if r.Status != StatusRedacted {
		t.Errorf("status = %q, want REDACTED", r.Status)
	}
	if r.RedactedCount != 1 {
		t.Errorf("redacted_count = %d, want 1", r.RedactedCount)
	}
	if r.FilteredText == input {
		t.Error("text should have been modified")
	}
}

func TestScan_Block(t *testing.T) {
	f := New(Config{
		Patterns: []Pattern{
			{ID: "pem_key", Regex: "-----BEGIN.*PRIVATE KEY-----", Severity: "critical", Action: "block"},
		},
	})
	r := f.Scan("data: -----BEGIN RSA PRIVATE KEY-----")
	if r.Status != StatusBlocked {
		t.Errorf("status = %q, want BLOCKED", r.Status)
	}
	if r.BlockedBy != "pem_key" {
		t.Errorf("blocked_by = %q, want pem_key", r.BlockedBy)
	}
	if r.FilteredText != "" {
		t.Error("blocked result should have empty filtered text")
	}
}

func TestScan_NilFilter(t *testing.T) {
	var f *Filter
	r := f.Scan("anything")
	if r.Status != StatusClean {
		t.Errorf("nil filter should return CLEAN, got %q", r.Status)
	}
}

func TestPassThrough(t *testing.T) {
	f := PassThrough()
	r := f.Scan("secret = AKIAIOSFODNN7EXAMPLE")
	if r.Status != StatusClean {
		t.Errorf("pass-through should return CLEAN, got %q", r.Status)
	}
}

func TestScan_InvalidRegexSkipped(t *testing.T) {
	f := New(Config{
		Patterns: []Pattern{
			{ID: "bad", Regex: "[invalid", Action: "block"},
			{ID: "good", Regex: "secret", Action: "redact"},
		},
	})
	r := f.Scan("this is a secret")
	if r.Status != StatusRedacted {
		t.Errorf("status = %q, want REDACTED (bad pattern should be skipped)", r.Status)
	}
}

func TestScan_MultipleMatches(t *testing.T) {
	f := New(Config{
		Patterns: []Pattern{
			{ID: "aws_key", Regex: "AKIA[0-9A-Z]{16}", Action: "redact"},
			{ID: "password", Regex: "password\\s*=\\s*\\S+", Action: "redact"},
		},
	})
	r := f.Scan("key=AKIAIOSFODNN7EXAMPLE password=hunter2")
	if len(r.Matches) != 2 {
		t.Errorf("matches = %d, want 2", len(r.Matches))
	}
	if r.RedactedCount != 2 {
		t.Errorf("redacted_count = %d, want 2", r.RedactedCount)
	}
}
