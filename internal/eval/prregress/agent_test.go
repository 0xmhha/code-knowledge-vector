package prregress

import (
	"reflect"
	"testing"
)

func TestExtractExpectedFiles_StandardSection(t *testing.T) {
	md := `## Approach

Some prose explaining how to fix it.

## Expected Changes

- core/types/receipt.go: Add headerGasTip parameter to DeriveFields
- core/state_transition.go: Emit AuthorizedTxExecuted event log
- params/protocol_params.go: Add event signature constant
`
	got := ExtractExpectedFiles(md)
	want := []string{
		"core/types/receipt.go",
		"core/state_transition.go",
		"params/protocol_params.go",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ExtractExpectedFiles = %v, want %v", got, want)
	}
}

func TestExtractExpectedFiles_VariousHeaderForms(t *testing.T) {
	cases := []struct {
		name string
		md   string
		want []string
	}{
		{"## Expected Change (singular)", "## Expected Change\n- a.go: foo\n", []string{"a.go"}},
		{"### Expected Files", "### Expected Files\n- b.go: bar\n", []string{"b.go"}},
		{"** bold expected changes **", "**Expected Changes**\n- c.go: baz\n", []string{"c.go"}},
		{"upper case header", "## EXPECTED CHANGES\n- d.go: x\n", []string{"d.go"}},
		{"header with colon", "## Expected Changes:\n- e.go: y\n", []string{"e.go"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ExtractExpectedFiles(tc.md)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestExtractExpectedFiles_VariousBulletMarkers(t *testing.T) {
	md := `## Expected Changes

- file_a.go: desc
* file_b.go: desc
+ file_c.go: desc
`
	got := ExtractExpectedFiles(md)
	want := []string{"file_a.go", "file_b.go", "file_c.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractExpectedFiles_StripTrailingColon(t *testing.T) {
	// Some LLMs emit "- foo.go: description", others "- foo.go : description",
	// others "- foo.go" with no description. All should yield "foo.go".
	md := `## Expected Changes

- foo.go: with colon
- bar.go : with spaced colon
- baz.go
`
	got := ExtractExpectedFiles(md)
	want := []string{"foo.go", "bar.go", "baz.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractExpectedFiles_StopsAtNextSection(t *testing.T) {
	md := `## Expected Changes

- a.go: x
- b.go: y

## Risks

- something else
- c.go: not a real file
`
	got := ExtractExpectedFiles(md)
	want := []string{"a.go", "b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v (leaked from next section)", got, want)
	}
}

func TestExtractExpectedFiles_DedupesPreservingFirstOccurrence(t *testing.T) {
	md := `## Expected Changes

- a.go: first mention
- b.go: y
- a.go: duplicate, should be ignored
`
	got := ExtractExpectedFiles(md)
	want := []string{"a.go", "b.go"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestExtractExpectedFiles_NoSectionReturnsNil(t *testing.T) {
	md := `## Approach

Just prose, no Expected Changes section.
`
	if got := ExtractExpectedFiles(md); got != nil {
		t.Errorf("no section: got %v, want nil", got)
	}
}

func TestExtractExpectedFiles_EmptyInput(t *testing.T) {
	if got := ExtractExpectedFiles(""); got != nil {
		t.Errorf("empty input: got %v, want nil", got)
	}
}
