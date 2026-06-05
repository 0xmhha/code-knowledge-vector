package main

import "testing"

func TestBuildCmd_HasDocsFlag(t *testing.T) {
	cmd := newBuildCmd()
	f := cmd.Flags().Lookup("docs")
	if f == nil {
		t.Fatal("build command missing --docs flag")
	}
	if f.Value.Type() != "stringSlice" {
		t.Errorf("--docs type = %q, want stringSlice", f.Value.Type())
	}
}
