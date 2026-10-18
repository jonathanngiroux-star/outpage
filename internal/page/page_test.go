package page

import (
	"html/template"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jonathanngiroux-star/outpage/internal/donate"
	"github.com/jonathanngiroux-star/outpage/internal/store"
)

func seed(t *testing.T) *store.Store {
	t.Helper()
	s, err := store.Open(filepath.Join(t.TempDir(), "page.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// The public page must show a component and its status with no JS needed
// to read it: name and data-status are in the server-rendered HTML.
func TestRenderShowsComponentAndStatus(t *testing.T) {
	s := seed(t)
	if _, err := s.AddComponent("API", "public REST"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetComponentStatus("API", "degraded"); err != nil {
		t.Fatal(err)
	}
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "API") || !strings.Contains(html, "public REST") {
		t.Fatalf("component missing from page:\n%s", html)
	}
	if !strings.Contains(html, `data-status="degraded"`) {
		t.Fatalf("data-status degraded missing:\n%s", html)
	}
	if !strings.Contains(html, "Degraded performance") {
		t.Fatalf("overall banner must reflect the worst component:\n%s", html)
	}
}

func TestRenderOverallBannerPerWorstComponent(t *testing.T) {
	s := seed(t)
	for _, c := range []struct{ name, status, want string }{
		{"a", "operational", "All systems operational"},
		{"b", "degraded", "Degraded performance"},
		{"c", "partial", "Partial outage"},
		{"d", "major", "Major outage"},
	} {
		if err := s.SetComponentStatus(c.name, c.status); err == nil {
			// fine — status went through
			_ = err
		}
	}
	// add in order and check banner after each step
	s2 := seed(t)
	steps := []struct {
		status string
		want   string
	}{
		{"operational", "All systems operational"},
		{"degraded", "Degraded performance"},
		{"partial", "Partial outage"},
		{"major", "Major outage"},
	}
	for i, step := range steps {
		if _, err := s2.AddComponent(string(rune('A'+i)), ""); err != nil {
			t.Fatal(err)
		}
		if err := s2.SetComponentStatus(string(rune('A'+i)), step.status); err != nil {
			t.Fatal(err)
		}
		html, err := Render(s2)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(html, step.want) {
			t.Fatalf("after %s: banner %q missing", step.status, step.want)
		}
	}
}

// Active incidents and their timelines render; resolved incidents move to
// history.
func TestRenderIncidentTimelineAndHistory(t *testing.T) {
	s := seed(t)
	if _, err := s.AddComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	inc, err := s.OpenIncident("Elevated errors", "minor", []string{"API"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddUpdate(inc.ID, "identified", "Found the bad deploy."); err != nil {
		t.Fatal(err)
	}
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Elevated errors", "Found the bad deploy.", "Investigating", "Identified"} {
		if !strings.Contains(html, want) {
			t.Fatalf("active incident missing %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "Past incidents") {
		t.Fatalf("history section must be absent while the incident is open")
	}

	if _, err := s.AddUpdate(inc.ID, "resolved", "Rollback complete."); err != nil {
		t.Fatal(err)
	}
	html, err = Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Past incidents") || !strings.Contains(html, "Rollback complete.") {
		t.Fatalf("resolved incident must appear in history:\n%s", html)
	}
	if strings.Contains(html, "Active incidents") {
		t.Fatalf("no active incidents should render an active section")
	}
}

// Operator-supplied text is escaped — the public page is unauthenticated.
func TestRenderEscapesOperatorText(t *testing.T) {
	s := seed(t)
	inc, err := s.OpenIncident("<script>alert(1)</script>", "minor", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddUpdate(inc.ID, "identified", "<img src=x onerror=alert(2)>"); err != nil {
		t.Fatal(err)
	}
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "<script>alert(1)</script>") || strings.Contains(html, "<img src=x") {
		t.Fatalf("unescaped operator text reached the public page:\n%s", html)
	}
	// The escaped title must still be readable on the page: assert the
	// canonical escape of the raw title appears.
	esc := template.HTMLEscapeString("<script>alert(1)</script>")
	if !strings.Contains(html, esc) {
		t.Fatalf("escaped incident title missing:\n%s", html)
	}
}

// The single most important privacy rule: subscriber emails NEVER appear
// in public HTML.
func TestRenderNeverLeaksSubscriberEmails(t *testing.T) {
	s := seed(t)
	if _, err := s.AddComponent("API", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddSubscriber("email", "ops-private@example.net", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.OpenIncident("Leak check", "minor", []string{"API"}); err != nil {
		t.Fatal(err)
	}
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(html, "ops-private@example.net") {
		t.Fatalf("subscriber email leaked into public HTML:\n%s", html)
	}
	// The subscriber count is fine; the address is not.
	if !strings.Contains(html, "1 subscriber") && !strings.Contains(html, "1 email subscriber") {
		t.Logf("note: page did not surface a subscriber count (acceptable)")
	}
}

// Maintenance windows render with their scheduled times.
func TestRenderMaintenanceWindow(t *testing.T) {
	s := seed(t)
	if _, err := s.AddComponent("DB", ""); err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 25, 1, 0, 0, 0, time.UTC)
	if _, err := s.OpenMaintenance("Database upgrade", []string{"DB"}, start, start.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "Database upgrade") || !strings.Contains(html, "Scheduled maintenance") {
		t.Fatalf("maintenance section missing:\n%s", html)
	}
	if !strings.Contains(html, "Oct 25, 2026 01:00 UTC") {
		t.Fatalf("window start time missing:\n%s", html)
	}
}

func TestRenderEmptyStore(t *testing.T) {
	s := seed(t)
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, "All systems operational") {
		t.Fatalf("empty page must still carry the operational banner:\n%s", html)
	}
	if !strings.Contains(html, "No components yet") {
		t.Fatalf("empty-state note missing:\n%s", html)
	}
}

// The footer carries both donation addresses on every page.
func TestRenderFooterHasDonateAddresses(t *testing.T) {
	s := seed(t)
	html, err := Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html, donate.Ethereum) || !strings.Contains(html, donate.Bitcoin) {
		t.Fatalf("footer addresses missing:\n%s", html)
	}
}

// The donate page is a full HTML document with both addresses and no form.
func TestRenderDonatePage(t *testing.T) {
	html := RenderDonate()
	if !strings.Contains(html, donate.Ethereum) || !strings.Contains(html, donate.Bitcoin) {
		t.Fatalf("donate page addresses missing:\n%s", html)
	}
	lower := strings.ToLower(html)
	if strings.Contains(lower, "<form") || strings.Contains(lower, "<input") || strings.Contains(lower, "<button") {
		t.Fatalf("donate page must not contain payment elements:\n%s", html)
	}
}
