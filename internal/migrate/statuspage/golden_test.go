package statuspage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// goldenPath is the committed dry-run report for the fixture — the drift
// gate: regenerate only after an intentional contract change, in the
// same change that updates this golden.
func goldenPath(t *testing.T) string {
	return filepath.Join(repoRoot(t), "testdata", "golden", "statuspage-report.json")
}

// TestGoldenReportStable: the CLI's JSON output for the committed fixture
// must match the committed golden byte-for-byte. Any diff is a contract
// change that must be deliberate (regenerate via
// `go run ./cmd/gentestdata <fixture>`).
func TestGoldenReportStable(t *testing.T) {
	exp, err := Parse(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(Analyze(exp), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	got := string(b) + "\n"
	want, err := os.ReadFile(goldenPath(t))
	if err != nil {
		t.Fatalf("golden missing — generate with `go run ./cmd/gentestdata %s > %s`: %v",
			fixturePath(t), goldenPath(t), err)
	}
	if got != string(want) {
		t.Fatalf("golden drift:\n--- committed ---\n%s\n--- current ---\n%s", want, got)
	}
}
