package statuspage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// FuzzAnalyze exercises the parser + analyzer at their own contract: any
// []byte either fails to unmarshal (fine) or produces a report without
// panicking; a valid JSON export must never crash the report builder, no
// matter how hostile the field values are. Counts must always add up.
func FuzzAnalyze(f *testing.F) {
	// Seed with the committed fixture and edge shapes. The seed runs at
	// setup (no *testing.T here), so locate the fixture by walking up.
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			break
		}
		dir = filepath.Dir(dir)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "testdata", "fixtures", "statuspage", "sample-export.json")); err == nil {
		f.Add(b)
	}
	f.Add([]byte(`{}`))
	f.Add([]byte(`{"components":[{"id":"x","name":"","status":"weird"}]}`))
	f.Add([]byte(`{"subscribers":[{"type":"sms","address":""}]}`))
	f.Add([]byte(`{"incidents":[{"updates":[{"html_body":"<b>x</b>"}]}]}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		var exp Export
		if err := json.Unmarshal(data, &exp); err != nil {
			return // invalid JSON is a clean Parse error at the CLI layer
		}
		rep := Analyze(&exp)
		if rep == nil {
			t.Fatal("Analyze returned nil")
		}
		if rep.Counts.Mapped+rep.Counts.Partial+rep.Counts.Unmapped != len(rep.Mappings) {
			t.Fatalf("counts do not add up: %+v vs %d mappings",
				rep.Counts, len(rep.Mappings))
		}
		// The report must stay JSON-serializable (the CLI marshals it).
		if _, err := json.Marshal(rep); err != nil {
			t.Fatalf("report not marshalable: %v", err)
		}
	})
}
