package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// timeSleepBrief pauses 100ms — shared by the serve-smoke poll loop.
func timeSleepBrief(t *testing.T) {
	t.Helper()
	time.Sleep(100 * time.Millisecond)
}

// readFile loads a file as string (fixture hygiene helper).
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

// contains is the substring check (fixture hygiene helper).
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// Fixture hygiene: no live secrets in any checked-in fixture.
func TestFixtureHygiene(t *testing.T) {
	root := repoRoot(t)
	fixtures, err := filepath.Glob(filepath.Join(root, "testdata", "fixtures", "**", "*.json"))
	if err != nil || len(fixtures) == 0 {
		t.Fatalf("fixtures glob: %v (%v)", err, fixtures)
	}
	tokens := []string{"AKIA", "sk_live", "dsn", "@sentry", "ingest.sentry", "secretAccessKey", "Bearer "}
	for _, f := range fixtures {
		b, err := readFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, tok := range tokens {
			if contains(b, tok) {
				t.Errorf("%s contains suspicious token %q — fixtures must be synthetic", f, tok)
			}
		}
	}
}
