package statuspage

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up to the nearest go.mod (package depth varies).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, _ := os.Getwd()
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("no go.mod upward")
	return ""
}

func fixturePath(t *testing.T) string {
	return filepath.Join(repoRoot(t), "testdata", "fixtures", "statuspage", "sample-export.json")
}

func TestParseFixture(t *testing.T) {
	exp, err := Parse(fixturePath(t))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if exp.Page.Name != "Example Cloud" {
		t.Fatalf("page name = %q", exp.Page.Name)
	}
	if len(exp.Components) != 3 || len(exp.Incidents) != 2 ||
		len(exp.Maintenances) != 1 || len(exp.Subscribers) != 4 {
		t.Fatalf("fixture shape: %d components, %d incidents, %d maint, %d subs",
			len(exp.Components), len(exp.Incidents), len(exp.Maintenances), len(exp.Subscribers))
	}
	if len(exp.Metrics) != 1 {
		t.Fatalf("metrics: %d", len(exp.Metrics))
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	os.WriteFile(bad, []byte("{not json"), 0o644)
	_, err := Parse(bad)
	if err == nil || !strings.Contains(err.Error(), "bad.json") {
		t.Fatalf("Parse(garbage) = %v, want error naming the file", err)
	}
	_, err = Parse(filepath.Join(dir, "missing.json"))
	if err == nil || !strings.Contains(err.Error(), "missing.json") {
		t.Fatalf("Parse(missing) = %v, want error naming the file", err)
	}
}

// MapComponentStatus is the five-word vocabulary bridge.
func TestMapComponentStatus(t *testing.T) {
	cases := map[string]string{
		"operational":          "operational",
		"degraded_performance": "degraded",
		"partial_outage":       "partial",
		"major_outage":         "major",
		"under_maintenance":    "maintenance",
	}
	for in, want := range cases {
		if got := MapComponentStatus(in); got != want {
			t.Errorf("MapComponentStatus(%q) = %q, want %q", in, got, want)
		}
	}
	if got := MapComponentStatus("unknown_status"); got != "" {
		t.Errorf("MapComponentStatus(unknown) = %q, want empty (flagged)", got)
	}
}

// The report contract the CI fixture gate pins: counts, unmapped keys,
// risks. Machine-stable, no fake 100%.
func TestReportContractOnFixture(t *testing.T) {
	exp, err := Parse(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	rep := Analyze(exp)
	if rep.Source != "statuspage" || rep.DryRun != true {
		t.Fatalf("header: %+v", rep)
	}
	if rep.Counts.Mapped != 7 || rep.Counts.Partial != 3 || rep.Counts.Unmapped != 2 {
		t.Fatalf("counts: %+v (fixture: page partial, c7 partial, i11 partial; sms+metrics unmapped)", rep.Counts)
	}
	var unmappedKeys []string
	for _, m := range rep.Mappings {
		if m.Result == "unmapped" {
			unmappedKeys = append(unmappedKeys, m.Key)
		}
	}
	if strings.Join(unmappedKeys, ",") != "subscriber.sms,metrics.uptime" {
		t.Fatalf("unmapped keys = %v", unmappedKeys)
	}
	// Risks: webhook secrets always reported when webhooks exist; email
	// reconfirmation when email subscribers exist.
	var riskKeys []string
	for _, r := range rep.Risks {
		riskKeys = append(riskKeys, r.Key)
	}
	if !contains(riskKeys, "webhook.secrets") || !contains(riskKeys, "subscriber.reconfirm") {
		t.Fatalf("risks = %v", riskKeys)
	}
	// Spot-check one partial's dropped fields.
	for _, m := range rep.Mappings {
		if m.SourceID == "c7f8a9" {
			if m.Result != "partial" {
				t.Fatalf("c7f8a9 result = %q, want partial", m.Result)
			}
			if !contains(m.Dropped, "only_show_if_degraded") || !contains(m.Dropped, "custom_fields") {
				t.Fatalf("c7f8a9 dropped = %v", m.Dropped)
			}
		}
		if m.SourceID == "i11y2" && m.Result != "partial" {
			t.Fatalf("i11y2 result = %q, want partial (html_body drops)", m.Result)
		}
		if m.SourceID == "pg9k2xw4tq" && m.Result != "partial" {
			t.Fatalf("page result = %q, want partial (id/url/created_at drop)", m.Result)
		}
	}
}

// JSON field names are the machine contract — assert them exactly.
func TestReportJSONFieldNames(t *testing.T) {
	exp, err := Parse(fixturePath(t))
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(Analyze(exp))
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"source", "dry_run", "counts", "mappings", "risks"} {
		if _, ok := raw[key]; !ok {
			t.Fatalf("report JSON missing %q: %s", key, b)
		}
	}
	var counts map[string]int
	json.Unmarshal(raw["counts"], &counts)
	for _, key := range []string{"mapped", "partial", "unmapped"} {
		if _, ok := counts[key]; !ok {
			t.Fatalf("counts missing %q", key)
		}
	}
	s := string(b)
	for _, key := range []string{`"key"`, `"result"`, `"source_id"`, `"risks"`} {
		if !strings.Contains(s, key) {
			t.Fatalf("JSON missing %s", key)
		}
	}
}

// Empty and hostile exports do not panic and report honestly.
func TestReportOnEmptyAndUnknownStatuses(t *testing.T) {
	rep := Analyze(&Export{})
	if rep.Counts.Mapped != 0 || rep.Counts.Partial != 0 || rep.Counts.Unmapped != 0 {
		t.Fatalf("empty export: %+v", rep.Counts)
	}
	rep = Analyze(&Export{
		Components: []Component{{ID: "x", Name: "X", Status: "mystery_status"}},
	})
	if rep.Counts.Partial != 1 {
		t.Fatalf("unknown component status must be partial (flagged), got %+v", rep.Counts)
	}
	found := false
	for _, m := range rep.Mappings {
		if m.SourceID == "x" && contains(m.Dropped, "status:mystery_status") {
			found = true
		}
	}
	if !found {
		t.Fatalf("unknown status not named in dropped: %+v", rep.Mappings)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
